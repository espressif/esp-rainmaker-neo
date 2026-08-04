# Triggers & Automations

## 1. Overview

This document describes the design of the **triggers** and **automations**
features on the ESP RainMaker Neo platform. Together they let a deployment turn device-side
conditions into cross-node "if this, then that" behaviour without a round trip
through any external rules service.

The feature is split across two independently-addressable resources:

- **Triggers** are *per-node* named conditions. A trigger definition lives on a
  single node, is stored as one of that node's service configurations, and is
  pushed down to the device firmware over MQTT. The firmware is what actually
  evaluates the physical condition (e.g. "Light.Power turned on"); the cloud
  only stores the definition and forwards it to the device.
- **Automations** are *group-scoped* rules. An automation references trigger
  IDs in a boolean condition (`and` / `or`), and when that condition is
  satisfied it performs a list of **actions** — each action sets a parameter on
  some node in the group to a value.

The split matters: a trigger says *what to watch* on one device; an automation
says *what combination of watched conditions should cause what changes* across
the group. Triggers are the sensors, automations are the wiring.

A separate **runtime engine** in the notification pipeline closes the loop. When
a device reports that one of its triggers fired, it publishes a notification; the
engine records the new trigger value into the automation's runtime state,
re-evaluates the boolean condition, and — if the condition now holds and the
automation is enabled — executes the actions by writing to the target nodes'
desired shadows.

### Key design points

1. **Two resources, two scopes, two service dispatchers.** Triggers ride the
   generic *node service* dispatcher (`.../nodes/{nodeId}/triggers`);
   automations ride the generic *group service* dispatcher
   (`.../service/automations`). Neither has a bespoke Lambda — both are
   registered services behind a shared router.
2. **The cloud validates structure, not semantics.** Trigger CRUD enforces only
   that `triggers` is an array of objects with unique string `id`s; every other
   field is passed through to the firmware verbatim. Automation CRUD validates
   only the `status` enum. Condition/action *meaning* is enforced at runtime, or
   on the device, not at write time.
3. **Server-generated automation IDs.** An automation's ID is a short
   3-character token minted server-side on create (`POST`), returned to the
   caller, and used as the DynamoDB sort key.
4. **Config vs. runtime state are separate DynamoDB columns.** The client-
   supplied automation definition lives in `payload`; the engine's internal
   trigger-value bookkeeping lives in a separate `state` string that is never
   exposed through the API.
5. **Condition evaluation and action execution are runtime-only.** The condition
   evaluator and action executor are invoked exclusively by the runtime engine in
   the notification pipeline when a trigger fires — never from the CRUD path.
6. **Read permission is deliberately separated from list/edit.** A distinct
   `group:getautomation` grant governs read/list so that subgroup-shared users,
   who can enumerate sub-entities, still cannot read a group's automations.
7. **Automations self-clean when a referenced node leaves the group.** Node
   removal cascades into automation cleanup: an automation whose *trigger
   condition* references the departing node is deleted outright; one that only
   *acts on* it has those action targets stripped (and is deleted if nothing
   remains).

---

## 2. Background

### 2.1 What a trigger is

A trigger is one entry in a node's `triggers` service configuration. The cloud
stores the whole `triggers` payload as node service data (alongside schedules,
timeseries config, etc. — see [schedules.md](schedules.md) for the sibling
node-service pattern) and forwards it to the device.

The stored shape is:

```json
{
  "triggers": [
    { "id": "t1", "path": "Light.Power", "operator": "eq", "value": true },
    { "id": "t2", "path": "0x1.c.s.0x6.a.0x0", "operator": "eq", "value": true }
  ]
}
```

The cloud validation in the trigger service enforces exactly three things:

1. The payload is an object.
2. It has a `triggers` field that is an array.
3. Each element is an object carrying a **unique string `id`**.

Everything beyond `id` — `path`, `operator`, `value`, and any other
field — is opaque to the cloud and interpreted by the firmware. (The firmware
accepts the operators `eq`, `ne`, `gt`, `lt`, `ge`, `le`; ordered operators
are valid only for numeric parameters, and there is no `type` field — the
value type is inferred from the JSON type of `value`.) The swagger
`NodeTrigger` schema reflects this with `additionalProperties: true` and only
`id` required.

Any write (`PUT`) or delete replaces or clears the whole `triggers` array
(there is no per-trigger endpoint), bumps the node's trigger service version,
and pushes the current trigger set to the device over MQTT. The version bump
lets a device that reconnects ask for the latest trigger version
(`getTriggerVer`) and the latest trigger set (`getTriggerDetails`).

### 2.2 What an automation is

An automation is one item in the `rmng-automations` table, owned by a group.
Its client-facing (flattened) shape is:

```json
{
  "id": "a1b",
  "name": "Turn on porch light at sunset",
  "description": "Triggered when ambient light falls below threshold",
  "status": "enabled",
  "conditions": { "and": ["nodeA~a1b~0"] },
  "actions": {
    "targets": [
      { "node": "nodeB", "path": "Light.Power", "value": true }
    ]
  }
}
```

- `conditions` is a boolean combination of **trigger IDs** (§2.3). `and`
  requires all listed IDs to be true; `or` requires any one; when both are
  present the result is `(all of and) OR (any of or)`.
- `actions.targets` is a list of `{node, path, value}` writes performed when the
  condition fires.
- `status` is `enabled` or `disabled`; a disabled automation is stored and
  evaluated but never executes its actions.

### 2.3 The trigger→automation link: the composite trigger ID

The strings inside `conditions.and` / `conditions.or` are **not** the free-form
per-node trigger `id`s from §2.1. They are composite runtime IDs of the form:

```
<nodeID>~<automationID>~<triggerIndex>
```

This is the wire format the device emits when a trigger fires and the format the
runtime engine parses. It encodes three facts in one string: which **node** the
trigger belongs to, which **automation** it participates in, and which **trigger
index** on that node fired. The engine splits on `~`, requires exactly three
non-empty components, and rejects anything else.

Because the automation ID is embedded in the composite trigger ID, a trigger ID
is meaningful only in the context of the automation it names. This is why the
runtime can find every automation referencing a node just by prefix-matching
`nodeID~` against the condition strings — the node ID is always the first
`~`-delimited segment.

---

## 3. Design

### 3.1 Trigger CRUD — the per-node service

**Routing.** Triggers are dispatched by the generic node-service router. The
REST path segment `triggers` (plural) is remapped to the internal singular
registry name `trigger`. The same handler dispatches all node services; the
plural→singular remap keeps the DB column name and the device-facing MQTT shape
stable while presenting a RESTful plural URL.

**Endpoints** (`/v1/groups/{groupId}/nodes/{nodeId}/triggers`):

| Method | Behaviour | Permission |
|---|---|---|
| `GET` | Return the node's trigger config; `{"triggers": []}` when unset | `node:get` |
| `PUT` | Validate + replace the trigger set, bump version, push to device | `node:putconfig` |
| `DELETE` | Clear the trigger set, bump version, push to device | `node:deleteconfig` |

There is no create-vs-update distinction and no per-trigger addressing: `PUT`
replaces the entire array. Both `PUT` and `DELETE` push the current trigger set
to the device over MQTT after the DB write, so the device is always notified of
the current set. If that MQTT push fails, the operation returns an error even
though the DB write already committed — the DB write and the device notification
are treated as a paired side effect.

The group-access precondition is enforced first by the router (the caller must
have access to `{groupId}`); the per-node permission (`node:get` /
`node:putconfig` / `node:deleteconfig`) is then checked inside the service.

### 3.2 Automation CRUD — the group service

**Routing.** Automations are dispatched by the generic group-service router,
registered under the service name `automations`. The router understands an
optional `{resourceId}` path segment (the automation ID) and maps HTTP verbs to
service operations:

| Method + path | Operation |
|---|---|
| `POST /service/automations` | Create (server generates ID) |
| `GET /service/automations` | List all in group |
| `GET /service/automations/{automationId}` | Get one |
| `PUT /service/automations/{automationId}` | Update one |
| `DELETE /service/automations` | Delete all in group |
| `DELETE /service/automations/{automationId}` | Delete one |

The automation service opts in to resource-ID support, which is what enables the
`{automationId}` variants in the shared router.

**Deletion does not cascade to triggers.** Both `DELETE` variants only remove
rows from the `rmng-automations` table — delete-all removes every automation in
the group, delete-one removes a single automation. Neither touches any node's
`triggers` configuration. Triggers are a **decoupled, per-node resource** (§2.1):
an automation merely *references* trigger IDs, it does not own them. After an
automation is deleted, the trigger definitions remain on their nodes and the
firmware keeps evaluating them — they are simply no longer wired into a group
rule. Triggers are removed only through their own
`DELETE /v1/groups/{groupId}/nodes/{nodeId}/triggers` endpoint, or when a node
leaves the group (§3.6).

**ID generation.** `POST` mints a 3-character token whose first character is a
lowercase letter and remaining two are lowercase-alphanumeric. Create delegates
to the same code path as update once the ID is minted, and returns
`{"automation_id": "<id>", "message": "success"}` so the caller can address the
new automation. `PUT` on an existing ID replaces the automation wholesale.

**Input parsing / validation.** The service coerces the body to a map and
validates:

- `status`, if present, is empty / `enabled` / `disabled`;
- **every action target node is a member of the automation's group** — a foreign
  target is rejected with `400`;
- **every condition trigger id names a node in the group** — the node segment of
  each `<nodeID>~<automationID>~<triggerIndex>` id is checked, and a foreign node
  is rejected with `400`.

Both membership gates matter because automations execute under a system actor
whose authorization passes for any node, so an unvalidated foreign reference
would become cross-tenant device control on trigger. `executeActionTarget`
re-checks membership at execution time as the backstop for nodes that leave the
group after the automation is written.

Beyond those checks the condition and action *structure* is stored as-is and
interpreted at runtime, not schema-validated on write.

**Flattening.** Stored items are `{group_id, automation_id, payload, state}`.
The API representation is built by spreading `payload` fields alongside `id`,
and derives `status` (defaulting to `enabled`) when the payload does not carry
one. `GET` (list) wraps results as `{"automations": [...]}`, always present and
empty when none are configured.

### 3.3 The payload-vs-state DynamoDB split

**Table** `rmng-automations`:

| Attribute | Key | Role |
|---|---|---|
| `group_id` | Partition key | Owning group |
| `automation_id` | Sort key | Unique within the group |
| `payload` | — | The client-supplied definition (name, description, conditions, actions, status) |
| `state` | — | Internal runtime trigger-value map — **never** exposed via the API |

`payload` is what the client wrote; it is the source of truth for conditions and
actions. `state` is a JSON string maintained by the runtime engine, holding a
`trigger_values` map (composite trigger ID → last-known boolean) plus a cached
copy of the conditions for evaluation.

On create, `state` is seeded from the payload's conditions: every trigger ID is
pulled out of `and`/`or` and initialised to `false`. Automations with no
conditions store an empty `state`. Thereafter the runtime engine (§3.4) is the
only writer of `state`.

Keeping the two apart means a client read never sees runtime bookkeeping, and a
runtime state update (an `UpdateItem` on the `state` attribute only) never
clobbers the client's definition.

### 3.4 The runtime engine

The runtime engine in the notification pipeline is the piece that ties device
trigger reports to action execution. It is a *notification service*: it receives
a direct notification originating from a device and reacts to it. This is a
deliberate cross-location boundary — the CRUD services define the data model and
the evaluator/executor implementations, but they are **driven** from the
notification pipeline, not from the API handlers.

**Flow:**

1. A device publishes a direct notification carrying
   `notifyData["automation"]["trigger"]`, an array of `{id, value}` where `id`
   is a composite trigger ID (§2.3) and `value` is a boolean.
2. For each entry the engine parses the composite ID, verifies the embedded
   `nodeID` matches the notification's sender (rejecting spoofed cross-node IDs),
   and groups updates by `automationID`.
3. For each affected automation it:
   - loads the automation item,
   - merges the new trigger values into `state.trigger_values`,
   - back-fills `state.conditions` from the payload if absent,
   - persists the updated `state`, then
   - evaluates and executes the actions.

**Evaluate + execute:**

1. If the automation's status is `disabled`, stop — no execution.
2. Feed `state.conditions` and `state.trigger_values` to the condition evaluator.
3. If conditions are **not** met, stop.
4. If met, extract `actions` from the **payload** (not state) and hand them to
   the action executor.

A failure inside evaluate-and-execute is logged but not propagated: the
trigger-value update is considered successful regardless, so a transient action
failure does not lose the recorded trigger state.

**Condition evaluation.** The condition evaluator is pure boolean logic over the
`trigger_values` map:

- `and`: every listed trigger ID must be present and `true`; a missing ID is
  treated as `false` (fails the AND). An empty `and` array evaluates to `true`.
- `or`: at least one listed trigger ID must be present and `true`; a missing ID
  is skipped. An empty `or` array evaluates to `false`.
- Both present: `(and result) OR (or result)`.
- Neither present, or nil/empty conditions, or nil trigger values: `false`.

This evaluator is **runtime-only**. It is constructed by the notification
pipeline and never invoked from CRUD — creating or updating an automation never
evaluates its conditions.

**Action execution.** The action executor converts the payload `actions` into
`{targets: [{node, path, value}]}` and, for each target:

- validates that `node` and `path` are non-empty (`value` may be any type,
  including nil),
- splits `path` into `<deviceId>.<paramId>`,
- builds `{device: {param: value}}` and writes the target node's desired shadow.

Individual target failures are logged and skipped; one bad target does not abort
the rest of the batch.

### 3.5 The action path model

Action target `path` follows the same two data-model conventions used across
RainMaker:

- **Default data model:** `<deviceId>.<paramId>`, e.g. `Light.Power`.
- **Matter data model:** the full dotted key chain into the desired shadow,
  e.g. `0x1.c.s.0x6.a.0x0` (attribute write) or `0x1.c.s.0x6.c.0x1` (command
  invoke). Each `.`-separated segment is one nesting key: endpoint → `c`
  (clusters) → `s` (server clusters) → cluster → `a` (attribute) / `c` (command)
  → attribute/command ID. The path is self-describing — the `a`/`c` segment
  distinguishes an attribute write from a command invoke, so no separate marker
  is needed.

The executor dispatches on the path: a Matter path (any path beginning `0x`) is
expanded segment-by-segment into the device's nested desired-shadow payload

```text
{"0x<endpoint>":{"c":{"s":{"0x<cluster>":{"<a|c>":{"0x<attribute|command>":<value>}}}}}}
```

placing `value` at the leaf. The `value` is passed through with the same JSON
type it was stored with (bool, number, string, list, object, or nil), matching
standard automations; a TLV command value is carried — and emitted — as a
string. Both models write through the same `PublishToDeviceDesired` sink.

A path that does not begin with `0x` is treated as a default `<deviceId>.<paramId>`
write. Automations can both *watch* Matter triggers (the composite trigger ID is
model-agnostic) and *drive* Matter attributes and commands as actions.

### 3.6 Cleanup when a node leaves a group

When a node is removed from a group (or a group is deleted), the node-data-reset
flow runs a system-context cleanup. Alongside deleting the node's own service
data (triggers, schedules, timeseries), it repairs automations in the affected
group:

- **Single-node removal:** for each automation in the group:
  - if the node appears in the **trigger conditions** (prefix-match on
    `nodeID~`), the whole automation is deleted — a condition referencing a node
    that is gone can never be satisfied meaningfully;
  - otherwise, if the node appears only in **action targets**, those targets are
    stripped; if no targets remain the automation is deleted, else it is updated
    with the cleaned payload.
- **Group deletion:** skips the per-node path and wipes every automation for the
  group in one call.

The node's own `triggers` service data is deleted by the same reset flow as part
of the generic node-service teardown, so there is no orphaned trigger config
left behind. See [node_reg.md](node_reg.md) for the registration/lifecycle
context these resets sit within.

---

## 4. Access Control and Security

### 4.1 Trigger permissions

Trigger CRUD reuses the standard node-config permissions, checked inside the
service against the target node:

| Operation | Permission |
|---|---|
| Read (`GET`) | `node:get` |
| Write (`PUT`) | `node:putconfig` |
| Delete (`DELETE`) | `node:deleteconfig` |

The group-access check in the router runs first, so a caller must both have
access to the group in the path and hold the node permission.

### 4.2 Automation permissions — three distinct grants

Automation DB operations are gated by **three separate** group permissions
rather than one:

| Permission | Guards |
|---|---|
| `group:getautomation` | Get and list (read/list) |
| `group:editautomation` | Create and update, including runtime state updates |
| `group:deleteautomation` | Delete one and delete-all |

**Why read is separated.** Group access comes in tiers. Primary access carries
the `group:*` wildcard; secondary access is granted all three automation
permissions explicitly; but **sub-entity access** (a subgroup-shared user) is
granted only `group:listsubentities` and `group:updatesubgroup` — deliberately
**not** `group:getautomation`. Because read/list is gated on its own dedicated
permission (and not folded into `group:listsubentities`), a subgroup-shared user
who can enumerate a group's sub-entities still cannot read or list its
automations. Collapsing read into the generic list permission would have leaked
automation definitions to that tier.

The runtime engine operates under a system actor, which satisfies these checks
for its state updates and action execution.

### 4.3 Trust boundary at the runtime engine

The runtime engine does not blindly trust the trigger IDs a device sends. Every
composite trigger ID must parse into exactly three non-empty parts, and the
embedded `nodeID` must equal the sender of the notification; a mismatch is
logged and the trigger is dropped. This prevents a compromised or misbehaving
device from flipping trigger values attributed to a *different* node's triggers.

### 4.4 Action blast radius

Action execution writes to the desired shadow of arbitrary nodes named in
`actions.targets`. Those targets are whatever the automation author stored; the
executor does not re-check per-target node authorization at runtime (it runs as
the system actor). The meaningful control point is therefore **write time** —
who may create/edit automations in the group (`group:editautomation`) — plus the
node-removal cleanup (§3.6) that strips targets referencing departed nodes.

---

## 5. Where code and swagger diverge

These are documented so integrators code against observed behaviour, not just
the schema:

1. **Not-found automations return `500`, not `404`.** Swagger documents `404`
   for `GET`/`DELETE` on a missing `{automationId}`. In practice the group-service
   router maps any service-layer error — including the DB layer's `automation not
   found` — to `500 InternalServerError`. There is no dedicated 404 mapping for a
   missing automation.

2. **Per-automation permission failures surface as `500`, not `403`.** The
   swagger documents `403` on these paths. The router returns an explicit `403`
   only for the *group-access* precondition. A caller who has group access but
   lacks `group:getautomation` / `group:editautomation` / `group:deleteautomation`
   hits the DB-layer authorization check, whose error is again wrapped as `500`.

---

## 6. Out of scope

- **Firmware trigger evaluation.** How a device decides that a trigger fired
  (the `type`/`path`/`operator`/`value` semantics) is firmware behaviour; the
  cloud stores and forwards the definition, validating only `id` uniqueness and
  the group membership of referenced nodes (§3.2).
- **Per-trigger addressing.** There is no endpoint to add or remove a single
  trigger; `PUT` replaces the whole array.
- **Automation condition/action schema validation at write time.** The CRUD
  layer validates `status` and group membership of every referenced node (§3.2),
  but not the shape of a condition or action. A structurally malformed payload is
  caught — or silently skipped — at runtime, not rejected on write.
- **Scheduled automations.** Time-based rules are handled by the schedules
  feature; see [schedules.md](schedules.md). Automations here are event-driven,
  fired by device trigger reports.

---

## 7. Future work

- **Align HTTP status codes with swagger.** Map `automation not found` to `404`
  and per-automation permission failures to `403` in the group-service router,
  resolving the divergences in §5.
- **Write-time *structural* validation.** Group membership of referenced nodes
  is already checked on write (§3.2); validating that trigger IDs are
  well-formed and that action paths are supported would close the remaining
  runtime-only failures.
- **Richer condition logic.** The evaluator supports a single flat
  `(and) OR (or)` combination; nested or negated conditions would require an
  evaluator extension and a state-model change.
- **Per-target *permission* checks at execution time.** Group membership is
  re-checked per target at execution; re-checking the originating user's write
  permission as well would tighten the blast radius described in §4.4 further.
