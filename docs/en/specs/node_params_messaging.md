# Node Parameters & Device↔Cloud Messaging

## What this covers

The *plumbing* by which a RainMaker node's parameters and configuration move
between an application, the cloud, and the device firmware. There are two
distinct data planes:

- **Device shadow** — the AWS IoT named shadow that holds a node's parameters.
  Firmware writes its live state into the shadow's `reported` section; an app
  reads that section to learn current state.
- **`to_cloud` request/response channel** — an MQTT topic on which firmware
  asks the cloud questions ("what group am I in?", "what schedule version do
  you hold?", "is Alexa enabled?") and receives answers on a paired
  `from_cloud` topic, brokered by the to-cloud request handler.

A third, derived plane — **indexed params (`iparams`)** — mirrors a slice of the
reported shadow into DynamoDB so operators can query params without a
per-node shadow read.

This is a design/lifecycle companion to the user-facing
`docs/05-device-app-messaging.md`. It describes the wire shapes, topics, and
handlers, not the payload semantics of any particular feature carried over
them.

### Out of scope (covered elsewhere)

| Concern | Where |
|---|---|
| Connect/disconnect/`online` tracking | `node_connection.md` |
| Group add/remove shadow *migration* (moving params between group-named shadows) | `group.md`, `node_assoc.md` |
| The node-config schema itself (`setNodeConfig` payload content) | config service |
| Schedule / trigger payload content and versioning rules | `schedules.md`, `automations.md` |
| Alexa / Google Voice enablement logic | `alexa.md`, voice-assistant specs |

This spec covers only how those payloads are *transported* and where the
handlers sit. Where a `to_cloud` event delegates to one of those features, the
event is listed here but its body is not described.

## Actors

| Actor | Role |
|---|---|
| **Node (FW)** | Writes live parameter state into its `reported` shadow. Publishes `to_cloud` requests. Subscribes to `from_cloud` and to its `user/.../params` topic to receive param intents and request responses. |
| **App / client** | Sets a parameter by publishing a param-set intent to the device's user-params topic. Reads current state from the `reported` shadow. |
| **AWS IoT Shadow service** | Stores named-shadow documents; merges partial updates; computes `desired`/`reported` deltas; emits `.../update/documents` events. |
| **To-cloud request handler Lambda** | Consumes `to_cloud` requests, dispatches each named event to its handler, assembles one response, and publishes it to `from_cloud`. |
| **`node_to_cloud_rule` IoT rule** | Routes `rainmaker/nodes/+/to_cloud` to the handler Lambda (or to a queue in the scalable deployment mode). |
| **`iparams_index_rule` IoT rule** | Mirrors the `iparams` reported shadow into the `NODE_IPARAMS` DynamoDB table — no Lambda in the path. |

## Data plane A — device shadow (parameters)

### Shadow model

A node's shadow document follows the standard AWS IoT structure: a `state` with
a `reported` section (device → cloud state) and a `desired` section (app →
device intent, see note below), plus server-set `metadata` and `version`. Each
section can carry:

- `params` — the per-device parameter map
- typed tags — admin / device / user scopes
- `online` and disconnect info — connection state (see `node_connection.md`)

A node's parameters live in a **group-scoped named shadow**, not the classic
shadow. The name is derived from group membership:

```
params-<groupID>[-<subgroupID>...]   // subgroups sorted alphabetically
```

Resolving the name fetches the node's group if not already known and errors if
the node belongs to no group — an unassociated node has no param shadow. (The
mechanics of *moving* params between shadows on a group change are in
`group.md` / `node_assoc.md`.)

### Reading current state (device → cloud)

Firmware publishes its live parameter values into the `reported` section of the
group shadow. An app reads them back by calling `GetThingShadow` on the
group-named shadow and returning the `reported` block. A missing shadow
(`ResourceNotFoundException`) is treated as empty, not an error.

A narrow accessor reads the reported shadow and returns a single device's params
as a map, or an empty map if the device or params are absent.

Reads are RBAC-gated: reading a node's shadow requires read-shadow permission on
the node.

### Setting a parameter (app → device)

The desired-state intent is **not** written to the shadow's `desired` section.
A helper to do so exists "for completeness" but is never used in practice.
Instead, the param-set intent is delivered as a **direct MQTT publish** to the
node's user-params topic:

```
rainmaker/nodes/<thing>/user/<shadowName>/params
        where <shadowName> = params-<groupID>[-<subgroupID>...]
```

The device is subscribed to this topic and applies the new param values, then
publishes the resulting state back into its `reported` shadow — which closes the
loop for any reader.

```
App           publish  params → rainmaker/nodes/<thing>/user/params-<grp>/params
Device        applies params, updates its firmware state
Device        write   reported shadow (Params) via $aws/things/<thing>/shadow/name/params-<grp>/update
App/reader    GetThingShadow(name=params-<grp>) → reads reported.Params
```

This param-set publish is the shared primitive behind the app-facing and
integration surfaces — voice assistants, automations, and the MCP tool layer all
route through it. Publishing requires publish-to-device permission; writing the
reported shadow requires write-shadow permission.

**Offline device behaviour.** Because the intent is a plain MQTT publish and
*not* a shadow `desired` write, it is fire-and-forget: there is no
store-and-forward. The publish path does **not** check the node's connection
state, so setting a parameter on an offline device does not fail and does not
report the device as *unreachable* — the call succeeds as long as the broker
accepts the publish. If the device is offline at that moment it simply never
receives the message, and — unlike a `desired`-shadow delta, which a device
reconciles on reconnect — there is nothing queued for it to pick up later, so the
change is lost rather than applied when it comes back. The `reported` shadow
therefore never updates, and a reader keeps seeing the previous value. Knowing a
device is offline is a **separate** concern: callers read the node's `online` /
connectivity state (see `node_connection.md`) to judge whether delivery is
likely. Only *stateful* callers that publish and then wait for the
`reported`-shadow echo with a timeout surface an explicit unreachable/timeout
result — for example the Alexa / Google Voice control flow (see `alexa.md`). The
bare param-set primitive does not.

Shadow writes populate either the `reported` or `desired` section and call
`UpdateThingShadow`; the Shadow service merges the update into the stored
document. Partial updates merge (not replace), and a JSON `null` for a key
deletes it — clearing user tags relies on this by sending an explicit
`{"data":{"user":{"t":null}}}` that omit-empty marshaling would otherwise drop.

A field-level delta between an old and new shadow state can be computed where
only the changed subset should be acted on.

## Data plane B — the `to_cloud` request/response channel

### Topics and routing

Firmware publishes a request to:

```
rainmaker/nodes/<thing>/to_cloud
```

The `node_to_cloud_rule` IoT rule matches `rainmaker/nodes/+/to_cloud`, projects
the thing name from the third topic segment and the full message body, and
targets the to-cloud request handler Lambda. Responses are published back to:

```
rainmaker/nodes/<thing>/from_cloud
```

at QoS 1. The handler holds `iot:Publish` only on
`rainmaker/nodes/*/from_cloud`.

### Request shape and dispatch

The request body carries an `event` array — a list of named requests batched
into one message. The handler iterates the array, dispatches each name, and
accumulates results into a single response, which is published once at the end
(only when at least one event produced output).

```
Device → to_cloud   { "event": ["getGroupInfo","getSchedVer"], ... }
Lambda              dispatch each name → append to response
Lambda → from_cloud { "event": ["getGroupInfo","getSchedVer"],
                      "getGroupInfo": {...}, "getSchedVer": {...} }
```

If any of `getAlexaEn`, `getGVAEn`, `getSchedVer`, `getSchedDetails`,
`getTriggerDetails`, `getTriggerVer` appear, the handler reads the node's
details **once** up front and reuses it across those events, avoiding a
per-event DB read.

Per-event errors are logged and the loop continues — one failing event does not
abort the batch. Non-string entries are skipped.

### Supported `to_cloud` event names

| Event | What it returns / does | Feature spec |
|---|---|---|
| `getGroupInfo` | Node's parent group (`pgrp`) and subgroups (`subgrps`) | `group.md` |
| `hello` | Echoes the supplied `hello` object back (liveness / handshake) | — |
| `setNodeConfig` | Persists node config via the config service; replies `{status: success\|error}` | config service |
| `getAlexaEn` | Whether Alexa is enabled for the node | `alexa.md` |
| `getGVAEn` | Whether Google Voice is enabled | voice-assistant spec |
| `getSchedVer` | Current schedule version held by the cloud | `schedules.md` |
| `getSchedDetails` | Full schedule payload + version | `schedules.md` |
| `getTriggerVer` | Current trigger version | `automations.md` |
| `getTriggerDetails` | Full trigger payload + version | `automations.md` |
| `getTimeSync` | Current server time as `{time: <epoch ms>}` — fast coarse clock sync so the node need not wait for SNTP; accuracy bounded by delivery latency | — |

An unrecognized event name is not fatal: it is logged as a warning — most often
a firmware typo or an event not yet implemented — and the rest of the batch
proceeds.

This spec enumerates the events and where they route; the *content* of the
config/schedule/trigger/voice payloads belongs to those feature specs.

### The `hello` / config-refresh pattern

A common firmware flow on (re)connect is to batch a `hello` with the version
queries (`getSchedVer`, `getTriggerVer`, `getGroupInfo`, `getAlexaEn`, …) in one
`to_cloud` message. The device compares the returned versions against what it
holds locally and, when they differ, issues a follow-up `getSchedDetails` /
`getTriggerDetails` to pull the full payload. The messaging layer imposes no
ordering beyond "one response per request batch"; the reconciliation logic lives
in firmware and the respective feature handlers.

## Deployment modes — direct vs. scalable

The to-cloud request handler's business logic is identical across two deployment
modes, selected at build/deploy time; firmware and the wire protocol are
unchanged between them.

- **Direct** (default): the `node_to_cloud_rule` IoT rule invokes the handler
  Lambda directly — one invoke per message. IoT Core is granted
  `lambda:InvokeFunction`.
- **Scalable**: an alternate deployment mode that consumes events via a queue.
  The rule writes to a Standard SQS queue (with a DLQ); the Lambda consumes
  batches through an event-source mapping. Each record is validated (non-empty
  thing name and data) and processed independently, returning per-record
  batch-item failures so only failed records are retried. Records arrive
  **without ordering guarantees**; the queue absorbs bursts that would otherwise
  fan out as concurrent direct invocations.

## Data plane C — indexed params (`iparams`)

Alongside the group-scoped param shadow, each node has a fixed-name indexed
shadow named `iparams`. Its `reported` section is read and written like any
shadow section; it also carries the node's typed tags (admin / device / user)
and is where user-tag clears operate.

To make params queryable without a per-node `GetThingShadow`, the
`iparams_index_rule` IoT rule mirrors this shadow into DynamoDB **with no Lambda
in the path** — a direct IoT→DynamoDB action. The rule matches the shadow's
`.../update/documents` event topic, takes the node id from the third topic
segment, and projects the reported block from the shadow document.

Every time the `iparams` shadow is updated, the Shadow service emits an
`.../update/documents` event; the rule extracts the reported block and
`PutItem`s it into the `NODE_IPARAMS` table (partition key `node_id`), so the
latest reported `iparams` for any node is a single-key read. The rule role holds
only `dynamodb:PutItem` on that table; failures route to a CloudWatch Logs error
action.

```
Device → $aws/things/<thing>/shadow/name/iparams/update   (write reported)
Shadow → $aws/things/<thing>/shadow/name/iparams/update/documents  (event)
Rule   → PutItem NODE_IPARAMS { node_id: <thing>, iparams: {reported...} }
Query  → GetItem NODE_IPARAMS by node_id
```

## Topic reference

| Topic | Direction | Purpose |
|---|---|---|
| `rainmaker/nodes/<thing>/to_cloud` | device → cloud | Batched requests to the to-cloud request handler |
| `rainmaker/nodes/<thing>/from_cloud` | cloud → device | Responses / cloud-initiated notifications |
| `rainmaker/nodes/<thing>/user/params-<grp>[-<sub>...]/params` | app → device | Parameter-set intent |
| `$aws/things/<thing>/shadow/name/params-<grp>[-<sub>...]/update` | device → cloud | Reported param state |
| `$aws/things/<thing>/shadow/name/iparams/update` | device → cloud | Indexed params + tags |
| `$aws/things/<thing>/shadow/name/iparams/update/documents` | shadow → rule | Feeds `iparams_index_rule` → `NODE_IPARAMS` |
