# Bridge Device Feature Design

## 1. Overview

This document describes the design for **bridge devices** in RainMaker. A
bridge is a standard RainMaker node that, in addition to its own functionality,
provides cloud connectivity for a large number of **bridged child devices**
that are not themselves directly reachable from AWS IoT (e.g., Zigbee, Z-Wave,
BLE-mesh, or proprietary RF endpoints behind a hub).

Each bridged child is represented in the cloud as a real AWS IoT Thing with
its own named shadows, group membership, sharing, schedules, triggers,
automations, and timeseries. The child has **no certificate** and never
connects to AWS IoT directly; all of its MQTT traffic is rewritten by IoT
Rules onto a bridge-owned topic namespace so that one bridge MQTT connection
can carry traffic for an arbitrary number of children.

The design reuses the existing Thing/group/subgroup/sharing model end-to-end.
The primary new pieces are: a naming convention (`<parent>--<suffix>` for
child Things) that encodes the parent-child relationship in the thing name;
a `to_cloud` control protocol for child creation; a topic-rewrite plane
implemented as three pure-SQL AWS IoT Rules (no enrichment Lambdas); three
additional statements on a bridge-specific IoT policy; and a
`bridge_children` DynamoDB table to support cascade operations.

---

## 2. Background

### 2.1 Existing Node Model

- A node is a registered AWS IoT Thing with a certificate. The certificate
  authenticates the node's MQTT connection.
- A node belongs to exactly one group. See [node_assoc.md](node_assoc.md).
- A node may belong to up to 3 subgroups within its group. See [group.md](group.md).
- Per-device unicast topic: `rainmaker/things/<thingID>/user/params-<groupID>[-<sg1>-<sg2>-<sg3>]/params`.
- Cloud→device notification topic: `rainmaker/things/<thingID>/from_cloud`.
- Group/subgroup control topics: `rainmaker/things/g/<groupID>/user/params-<groupID>[-<sgID>]/params`. See [group-control-feature.md](group-control-feature.md).

### 2.2 Default Device IoT Policy

The current default policy attached to all device certificates restricts a
device to its own topic space via `${iot:Connection.Thing.ThingName}` and to
its group's broadcast namespace via `${iot:Connection.Thing.Attributes[group_id]}`.
Neither of those scopes includes the topic spaces of other Things. A bridge
therefore cannot directly subscribe to traffic addressed to its children
under the existing policy.

### 2.3 Why Not Just Subscribe Per-Child?

MQTT topic filters support only `+` (single level) and `#` (multi level)
wildcards. A pattern like `rainmaker/things/brdge--*/...` is not a valid
topic filter. A bridge would therefore have to issue one explicit subscribe
per child topic. AWS IoT enforces a default subscription cap of 50 per MQTT
connection (soft-raisable), which is incompatible with the design goal of
supporting a large number of children behind one bridge. The rewrite plane
in Section 3.4 sidesteps this by funnelling all child traffic onto two
wildcard subscriptions on a bridge-owned namespace.

Outbound (bridge → cloud) shadow writes do not need a rewrite plane:
AWS IoT *policy* resource ARNs support both substitution variables and
`*` wildcards, so a single policy statement
`$aws/things/${iot:Connection.Thing.ThingName}--*/shadow/.../update`
authorizes a bridge to publish on every child's canonical shadow topic.
This is why shadow updates can use the stock AWS IoT shadow library
targeting `$aws/things/<child>/...` directly — see §3.5 and §5.2.

---

## 3. Design

### 3.1 Bridge Thing

A bridge is a normal RainMaker node:

- Registered via the existing CSV-based registration flow with its own
  certificate.
- Associated to a group via the existing `challenge_response` association
  flow (Matter NOC flow is not applicable).
- Has its own params shadow, iparams, subgroup memberships, group control
  subscriptions — identical to any other node.

A bridge is distinguished by `node_type=bridge` on its `node_details` row,
written at registration time from the value the register hook returns. (The
column is named `node_type` rather than a boolean `is_bridge` so future device
classes — gateway, virtual node, etc. — can share it.) It is a DynamoDB column,
not a Thing attribute: nothing queries device classes through Fleet Indexing,
and Thing attributes are capped at three without a thing type (see
[limits.md](limits.md)).

**Naming constraints** (enforced by the registration flow):

- **Bridge thing names** must not contain the substring `--` (double
  hyphen). Single hyphens are permitted.
- **Direct (non-bridge, non-child) device thing names** must not contain
  `--`. Existing registrations should be audited; double-hyphen is so
  unusual that collisions are extremely rare in practice.
- **Bridged child suffix** (the part after `--` in a child name, see §3.2)
  must not contain `--`.

The `--` separator is what enables pure-SQL parent extraction in the
IoT Rules (§3.4) and unambiguous policy substitution
`${iot:Connection.Thing.ThingName}--*` in the bridge IoT policy (§3.5).
Without these constraints the substitution would be ambiguous and the
rewrite rules would falsely match direct devices.

### 3.2 Child Thing

A child Thing is created server-side in response to a `to_cloud` message
from its bridge:

- Thing name: `<parent>--<suffix>` where `<parent>` is the bridge's
  `node_id` and `<suffix>` is proposed by the bridge. The separator is a
  **double hyphen**. `<suffix>` must not contain `--`.
- No certificate, no policy attachment.
- Thing attributes:
  - `parent_node_id`: the bridge's `node_id`
  - `child_local_id`: the bridge's protocol-side identifier for the child
    (used for idempotent re-creation across bridge reboots)
- Added to the bridge's main group via the standard ADD NODE TO GROUP FLOW
  (see [node_assoc.md](node_assoc.md#add-node-to-group-flow)).
- **Tethered to the bridge's main group.** Child cannot be moved to a
  different group, only between subgroups within the bridge's group.

### 3.3 Bridge Namespace

Cloud→bridge traffic for children is rewritten onto a bridge-owned topic
namespace so the bridge can subscribe to all of its children's traffic
with two wildcard subscriptions (independent of N children):

```
rainmaker/bridges/<parent>/children/<child>/from_cloud
rainmaker/bridges/<parent>/children/<child>/user/<shadow_name>/params
```

Bridge→cloud shadow operations for children go directly to the canonical
AWS IoT shadow topics (no bridge-namespace topic, no rewrite rule — see
§3.4 and §3.5):

```
$aws/things/<child>/shadow/name/<shadow_name>/update   # param write
$aws/things/<child>/shadow/name/<shadow_name>/delete   # subgroup migration teardown
```

The bridge also publishes to the control-plane topic in its own namespace:

```
rainmaker/bridges/<parent>/to_cloud
```

The bridge subscribes to two filters covering all of its children's
inbound traffic:

```
rainmaker/bridges/<parent>/children/+/from_cloud
rainmaker/bridges/<parent>/children/+/user/+/params
```

### 3.3.1 Bounded Subscription Count

A core design property of the rewrite plane is that the **bridge's MQTT
subscription count is constant** — independent of the number of children
(N), the number of subgroups in the group (K), and the bridge's own
subgroup memberships. This is required because AWS IoT enforces a default
cap of 50 subscriptions per MQTT connection.

A bridge holds exactly **six** subscriptions at all times:

| # | Topic filter | Purpose | Substitution |
|---|---|---|---|
| 1 | `rainmaker/nodes/<self>/from_cloud` | Bridge's own cloud→device channel | `<self>` = bridge `node_id` |
| 2 | `rainmaker/nodes/<self>/user/+/params` | Bridge's own unicast across all shadow-name variants | `<self>` = bridge `node_id` |
| 3 | `rainmaker/nodes/groups/<groupID>/control` | Group-level broadcast for the bridge's group (per [group-control-feature.md](group-control-feature.md) §3.1) | `<groupID>` = bridge's current group_id |
| 4 | `rainmaker/nodes/groups/<groupID>/subgroups/+/control` | Subgroup commands for any subgroup in the bridge's group | `<groupID>` = bridge's current group_id |
| 5 | `rainmaker/bridges/<self>/children/+/from_cloud` | All children's cloud→device traffic | `<self>` = bridge `node_id` |
| 6 | `rainmaker/bridges/<self>/children/+/user/+/params` | All children's unicast traffic across all shadow-name variants | `<self>` = bridge `node_id` |

The `+` over the shadow-name segment in (2) and (6) is what eliminates
per-subgroup reconciliation: when the bridge's own subgroup set changes
(unicast shadow name changes) or any child's subgroup set changes (the
child's unicast shadow name changes), the bridge's existing wildcard
subscription continues to match without any subscribe/unsubscribe churn.

The `+` over the subgroup-ID segment in (4) absorbs every subgroup
broadcast in the group under one subscription. This is a deviation
from the per-subgroup reconciliation pattern that
[group-control-feature.md](group-control-feature.md) §3.5 specifies
for non-bridge devices.

**Trade-off vs. non-bridge devices**: a non-bridge device using the
wildcard in (4) would receive every subgroup broadcast in its group and
filter client-side, which wastes bandwidth on a constrained device. For
a bridge that already needs to multiplex traffic for many children, the
wildcard is strictly cheaper. The wildcard is therefore a **bridge-only
firmware pattern**; the per-subgroup reconciliation in
`group-control-feature.md` §3.5 remains the rule for non-bridge devices.

**Policy coverage**: filters (1) and (2) are covered by the base device
policy; filters (3) and (4) by the group-control statement
(group-control-feature.md §3.3) that resolves to
`topicfilter/rainmaker/nodes/groups/${self.attributes[group_id]}/*control`;
filters (5) and (6) by the bridge IoT policy additions in §3.5. No
additional policy work is needed.

**Reconciliation triggers**: a subscribe/unsubscribe is required only on
two events:

- Bridge moves to a different group → unsubscribe (3) and (4) for the
  old group, subscribe (3) and (4) for the new group. Driven by
  `getGroupInfo`.
- Bridge first connect / re-connect → subscribe all six (filters 3 and
  4 deferred until the first `getGroupInfo` arrives with `pgrp`).

Neither child creation, child removal, child subgroup changes, nor
bridge subgroup changes cause any subscription churn.

### 3.4 IoT Rules

Three IoT Rules support the bridge integration:

- **Rules A and B** translate cloud→child unicast topics into the
  bridge-owned namespace so the bridge can pick them up on its two
  wildcard subscriptions.
- **Rule D** routes the bridge's `to_cloud` control messages to the
  bridge control Lambda, with authorization enforced in the rule's
  WHERE clause.

All three are **pure SQL** — no enrichment Lambdas on the hot path. The
naming convention `<parent>--<suffix>` (§3.1, §3.2) is what enables this
for Rules A and B: the parent is extracted from the child's thing name in
SQL via `substring()` and `indexof()`, so no DynamoDB lookup or
Thing-attribute query is required.

There is no Rule C: bridge→cloud child shadow updates go directly to
`$aws/things/<child>/shadow/...`, authorized by the bridge IoT policy
substitution (§3.5).

#### Rule A: Cloud-to-bridge `from_cloud` rewrite (pure SQL)

```sql
SELECT * FROM 'rainmaker/things/+/from_cloud'
WHERE indexof(topic(3), '--') >= 0
```

**Republish destination:**
```
rainmaker/bridges/${substring(topic(3), 0, indexof(topic(3), '--'))}/children/${topic(3)}/from_cloud
```

- `topic(3)` is the source thing name (e.g. `brdge--A`).
- `indexof(topic(3), '--')` returns the position of the double-hyphen
  separator (e.g. `5`).
- `substring(topic(3), 0, 5)` extracts the parent (e.g. `brdge`).
- Republish QoS: 1. Rule IAM role: `iot:Publish` on
  `arn:...topic/rainmaker/bridges/*/children/*/from_cloud`.

Direct devices (whose thing names contain no `--`) fail the WHERE clause,
so the rule is a no-op for them. No cross-impact on existing flows.

#### Rule B: Cloud-to-bridge unicast params rewrite (pure SQL)

```sql
SELECT * FROM 'rainmaker/things/+/user/+/params'
WHERE indexof(topic(3), '--') >= 0
```

**Republish destination:**
```
rainmaker/bridges/${substring(topic(3), 0, indexof(topic(3), '--'))}/children/${topic(3)}/user/${topic(5)}/params
```

- `topic(5)` is the shadow-name segment (`params-<groupID>[-<sg>]`),
  carried through verbatim. AWS IoT `topic(n)` is 1-indexed over every
  `/`-segment, so for `rainmaker/nodes/<thing>/user/<shadow>/params`:
  `topic(3)`=thing, `topic(4)`=`user`, `topic(5)`=shadow-name segment.
- Republish QoS: 1. Rule IAM role: `iot:Publish` on
  `arn:...topic/rainmaker/bridges/*/children/*/user/*/params`.

#### Bridge-to-cloud shadow updates: no rule

Child shadow updates from the bridge go directly to the canonical AWS IoT
named-shadow topic `$aws/things/<child>/shadow/name/<shadow_name>/update`.
Authorization is in the bridge IoT policy (§3.5) via substitution
`${iot:Connection.Thing.ThingName}--*`, which — combined with the
naming constraints in §3.1 — pins shadow write access to that bridge's
own children. See §5.2 for the fire-and-forget update model.

#### Rule D: Bridge-to-cloud `to_cloud` control plane (pure SQL gate, Lambda action)

The `control_lambda` is the *action target* (it implements `add_child` /
`remove_child` — see §4) and is not avoidable. But the authorization check
is in SQL, not in a separate enrichment Lambda:

```sql
SELECT topic(3) AS thing_name, * AS data FROM 'rainmaker/bridges/+/to_cloud'
WHERE clientid() = topic(3)
```

- `clientid()` is the publisher's MQTT clientid, pinned by the base IoT
  policy to the bridge's own `node_id`. The WHERE clause guarantees the
  Lambda only sees `to_cloud` messages whose topic prefix matches the
  authenticated bridge.
- The Lambda receives the validated publisher identity as
  `event.ThingName` via the shared `PublishInputEvent` struct (same shape
  as `node_to_cloud_rule`'s Lambda input); it does not need to re-verify
  clientid.

#### Summary: no enrichment Lambdas

| Rule | Source → Destination | Lambda? |
|---|---|---|
| A | `rainmaker/things/+/from_cloud` → bridge namespace | No (pure SQL Republish) |
| B | `rainmaker/things/+/user/+/params` → bridge namespace | No (pure SQL Republish) |
| (none) | `$aws/things/<child>/shadow/...` | No rule; direct publish from bridge |
| D | `rainmaker/bridges/+/to_cloud` → `control_lambda` | Yes, but Lambda **is** the action target (control plane), not enrichment; authorization gate is in SQL WHERE |

### 3.5 Bridge IoT Policy Additions

Three new statements are added to a **bridge-specific** IoT policy, attached to
bridge certificates only. The selector is the registration request itself: the
register hook attaches this policy when the requested capabilities contain
`bridge`, and classifies the node `node_type=bridge` in the same step. The
classification is the hook's output, not what selects the policy. The base
default policy is unchanged.

> **Deployment note**: AWS IoT enforces a 2048-byte hard limit on the
> stored policy document. The blocks below are organized by purpose
> for review; the deployed `src/bridge/bridge_policy.json` consolidates
> statements where Action + Effect match (the four inbound statements
> here collapse to one `iot:Subscribe` + one `iot:Receive` because the
> ARN prefix differs, and the three Publish blocks collapse to a single
> `iot:Publish` statement). Sids are dropped from the deployed copy.
> The semantics are identical; only the JSON layout differs.

**Inbound (bridge namespace subscribes):**

```json
{
  "Effect": "Allow",
  "Action": ["iot:Subscribe", "iot:Receive"],
  "Resource": [
    "arn:aws:iot:<region>:<account>:topicfilter/rainmaker/bridges/${iot:Connection.Thing.ThingName}/children/+/from_cloud",
    "arn:aws:iot:<region>:<account>:topicfilter/rainmaker/bridges/${iot:Connection.Thing.ThingName}/children/+/user/+/params"
  ]
}
```

**Outbound (control plane to_cloud):**

```json
{
  "Effect": "Allow",
  "Action": ["iot:Publish"],
  "Resource": [
    "arn:aws:iot:<region>:<account>:topic/rainmaker/bridges/${iot:Connection.Thing.ThingName}/to_cloud",
    "arn:aws:iot:<region>:<account>:topic/$aws/rules/*/rainmaker/bridges/${iot:Connection.Thing.ThingName}/to_cloud"
  ]
}
```

Both the direct topic and the basic-ingest variant
(`$aws/rules/bridge_to_cloud_rule/rainmaker/bridges/<self>/to_cloud`)
are granted. Basic ingest skips the broker entirely and invokes only
the rule named in the topic path, so a bridge using basic ingest
**must** address `bridge_to_cloud_rule` directly — addressing
`node_to_cloud_rule`'s basic-ingest prefix would silently drop
`add_child` / `remove_child` because the default node handler ignores
them.

The rule is named generically (`bridge_to_cloud_rule`, mirroring
`node_to_cloud_rule`) so future bridge-only control-plane operations
can share the same Lambda + topic without a firmware-facing rename.
The Lambda dispatches on the `event` field.

**Outbound (children — bridge acts on behalf of each child):**

```json
{
  "Sid": "ChildrenPublish",
  "Effect": "Allow",
  "Action": ["iot:Publish"],
  "Resource": [
    "arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/ts/*",
    "arn:aws:iot:<region>:<account>:topic/$aws/rules/*/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/ts/*",
    "arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/to_cloud",
    "arn:aws:iot:<region>:<account>:topic/$aws/rules/*/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/to_cloud",
    "arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/notify/*",
    "arn:aws:iot:<region>:<account>:topic/$aws/rules/*/rainmaker/nodes/${iot:Connection.Thing.ThingName}--*/notify/*"
  ]
}
```

This grants the bridge `iot:Publish` on the three canonical RainMaker
publish-side topics for each of its children — **time-series**
(`/ts/*`), **control plane** (`/to_cloud`), and **per-device
notifications** (`/notify/*`) — covering both the direct and
basic-ingest (`$aws/rules/<rule>/...`) variants. The shape mirrors
the equivalent statement in the base device policy
(`src/node/node_policy.json`), with `--*` inserted between the
substitution and the trailing segment to scope the grant strictly to
descendants of this bridge (per the §3.1 naming constraints, `<self>--*`
cannot match any other Thing).

The existing per-feature IoT rules handle these publishes identically
to a direct device's, with `thing_name = topic(3)` resolving to the
child:

- `node_to_cloud_rule` (filter `rainmaker/nodes/+/to_cloud`) →
  `publish_input_event_handler`. Reply goes back on
  `<child>/from_cloud`, rewritten to the bridge by Rule A. This is
  the mechanism the bridge uses to issue child
  `getSchedVer` / `getSchedDetails` / `getTriggerVer` /
  `getTriggerDetails` after a reconnect — see §5.9.
- `node_ts_rule` (filter `rainmaker/nodes/+/ts/+`) → DynamoDB
  `raw_ts_data` ingestion. Time-series rows per child are indexed by
  `node_key_dt = topic(3) + '.' + k + '.' + dt`, identical to direct
  devices.
- `node_notify_rule` (filter `rainmaker/nodes/+/notify/*`) →
  webhook / Alexa proactive event dispatch.

Why not a broader wildcard like
`$aws/rules/*/rainmaker/nodes/<self>--*/*`? Two reasons:

1. Asymmetry with the base device policy makes side-by-side review
   harder. Enumeration keeps the bridge statement diffable against
   `node_policy.json:33–43`.
2. A trailing `/*` is a greedy string wildcard that would also match
   `<self>--*/jobs/*`, `<self>--*/streams/*`, and any future
   topic shape the cloud may introduce — including ones with
   different security semantics. Children are explicitly out of
   scope for AWS IoT Jobs and Streams in v1 (§8.1, §8.5);
   enumeration prevents accidental privilege leakage if those
   feature gaps are filled later.

**Outbound (child shadow operations — direct to `$aws`):**

```json
{
  "Effect": "Allow",
  "Action": ["iot:Publish"],
  "Resource": [
    "arn:aws:iot:<region>:<account>:topic/$aws/things/${iot:Connection.Thing.ThingName}--*/shadow/name/*/*"
  ]
}
```

For a bridge `brdge`, this statement resolves at connect time to:
```
topic/$aws/things/brdge--*/shadow/name/*/*
```

The trailing `*` covers `update`, `delete`, and `get` — all named-shadow
action topics. `delete` is needed for subgroup shadow migration (the old
`params-<groupID>[-<sg>]` shadow must be removed when a child moves
groups). The scope is still named shadows only (`shadow/name/…`); unnamed
(classic) shadows are not covered. The naming constraints in §3.1 (no `--`
in bridge names, direct device names, or child suffixes) make `brdge--*`
unambiguous: it can only match descendants of `brdge`.

The bridge does **not** subscribe to `/update/accepted` or
`/update/rejected` — see §5.2 for the fire-and-forget update model that
keeps the subscription count bounded.

A bridge cannot read or write traffic addressed to children of any other
bridge, because the substitution pins the parent segment to its own
`node_id`.

### 3.6 `bridge_children` Table

A new DynamoDB table tracks the parent-child relationship for fast lookup
during cascade operations.

| Attribute | Type | Description |
|---|---|---|
| `parent_node_id` | String (PK) | The bridge's `node_id` |
| `child_node_id` | String (SK) | The child's `node_id` (`<parent>--<suffix>`) |
| `child_local_id` | String | Bridge-protocol identifier for idempotent re-creation |
| `created_at` | Number | Unix timestamp |

The table is the authoritative source for cascade-delete operations
(query by `parent_node_id`). No GSI on `child_node_id` is needed —
the rewrite-plane rules in §3.4 extract the parent directly from the
child's name via SQL, so no inverse lookup is performed at runtime.

The relationship is also expressed as Thing attributes (`parent_node_id`,
`child_local_id`) on each child Thing, primarily for human/debug visibility
and for queries that already work with IoT Thing attributes. (AWS IoT
calls these "Thing names" but the values are RainMaker `node_id`s — we
use the RainMaker term for attribute keys and DynamoDB columns.)

### 3.7 Online State

Children have no MQTT connection of their own, so AWS IoT presence events
never fire for them and `connectivity.connected` is always `false` in Fleet
Indexing. The user-facing online status is therefore reported by the bridge:

- The bridge updates each child's `iparams` shadow with an `online` field as
  it observes the child come/go on the bridge-side protocol.
- Core's presence handler, which already consumes
  `$aws/events/presence/disconnected/+`, calls the bridge cascade in-process
  (`src/bridge/hooks`). The cascade filters on `node_type=bridge` and, for a
  bridge, writes `online=false` to every child found in `bridge_children`.
  Nothing symmetric happens on `connected` — the bridge must reconfirm each
  child's reachability after it reconnects and report freshly.

### 3.8 Publish-Rate Quotas

AWS IoT Core enforces a soft quota of **inbound publish requests per
second per MQTT connection** (Message Broker → *Inbound publishes per
second per connection*). Persistent over-rate triggers two broker-side
failure modes: individual publishes dropped without PUBACK, and
ultimately a broker-initiated disconnect of the client.

For a direct device this is a non-issue — single-device publish rates
are orders of magnitude below the ceiling. For a bridge multiplexing
traffic for N children over one MQTT connection, the per-connection
ceiling is the binding constraint. The flows in this design that
produce per-N publish patterns:

| § | Flow | Publishes per N children |
|---|---|---|
| 5.2 | Child param updates fanned in from the protocol side (e.g. a scene change toggles many children at once) | Up to N back-to-back |
| 5.9 | Bridge reconnect: `getSchedVer` + `getTriggerVer` per child | 2N back-to-back |
| 5.10 | Bridge reconnect: re-report `online=true` per reachable child | N back-to-back |

**Firmware must rate-limit outbound publishes to stay below the broker's
per-connection ceiling**, with headroom for the bridge's own shadow
writes and transient spikes. A token-bucket or sliding-window limiter
shared across all publishes (the bridge's own + all children + control
plane) is the natural implementation. The exact threshold the firmware
keeps below tracks AWS IoT's published quota at deployment time, not a
number hard-coded in this spec.

**Bursts must be spread, not packed.** The reconnect fan-outs in §5.9
and §5.10 are deterministic per-N bursts; the firmware must spread them
over time rather than emit back-to-back. Both flows tolerate seconds of
delay — schedule/trigger version sync and online re-report are not
time-critical.

**Throttle and disconnect response.** On a publish error or unexpected
disconnect, firmware should back off (exponential, capped) before
retrying. Persistent publish-rate-induced disconnects indicate the
limiter is misconfigured and warrant local logging.

> **Reference**: [AWS IoT Core service quotas](https://docs.aws.amazon.com/general/latest/gr/iot-core.html)
> — *Message Broker and Operations*. The quota is soft-raisable via a
> service-quota request, but raising it provides only diminishing
> returns once the bridge's own limiter is in place.

---

## 4. Bridge `to_cloud` Control Protocol

### 4.1 Message Envelope

The bridge publishes JSON messages to `rainmaker/bridges/<parent>/to_cloud`.
Rule D (Section 3.4) routes them to the bridge control Lambda.

```json
{
  "action": "<action_name>",
  "request_id": "<uuid>",
  "...": "..."
}
```

The Lambda responds on `rainmaker/things/<parent>/from_cloud` with:

```text
{
  "event": ["bridgeAck"],
  "bridgeAck": {
    "request_id": "<uuid>",
    "status": "success" | "error",
    "error": "<message>",
    "...": "..."
  }
}
```

### 4.2 Action: `add_child`

Bridge requests creation of a child Thing.

**Request**:
```json
{
  "action": "add_child",
  "request_id": "<uuid>",
  "child_suffix": "A",
  "child_local_id": "0x00158D00012345AB"
}
```

**Process**:

1. Extract `<parent>` from the validated publisher identity passed by
   Rule D (the WHERE clause in §3.4 has already confirmed the publisher
   clientid equals `<parent>`).
2. Compute child `node_id` `<parent>--<child_suffix>` (double-hyphen
   separator).
3. **Idempotency check** — query `bridge_children` by `parent_node_id`
   (PK) and filter the result for rows whose `child_local_id` matches:
   - If a child already exists with the same `child_local_id`, return
     success with the existing child's `node_id`. Do not create a duplicate.
   - If a child already exists with the proposed `child_suffix` but a
     different `child_local_id`, return error `child_suffix_in_use`.

   No GSI is required: the per-bridge child count is bounded and the
   Query+filter is cheap.
4. Validate `child_suffix` matches `^[a-zA-Z0-9_]{1,32}$` (alphanumerics
   and underscore only — no `-`, which guarantees no `--` and keeps the
   child name's separator structure unambiguous).
5. Look up the bridge's current group via `group_device_mapping`. If the
   bridge is not in a group, return error `bridge_not_associated`.
6. Create the AWS IoT Thing `<parent>--<child_suffix>` with attributes
   `parent_node_id=<parent>` and `child_local_id=<child_local_id>`. No
   certificate is attached.
7. Write `bridge_children` entry.
8. Run **ADD NODE TO GROUP FLOW** with `node_id=<child>` and the bridge's
   `group_id`. This adds the child to `group_device_mapping`, sets the
   `group_id` Thing attribute, grants `NodeAll` permission to the bridge's
   owning users, and would normally publish `getGroupInfo` to the child's
   `from_cloud` — that publish is rewritten by Rule A back to the bridge.
9. Respond with `{status: "success", child_node_id: "<parent>--<child_suffix>"}`.

### 4.3 Action: `remove_child`

Bridge requests removal of a previously created child.

**Request**:
```json
{
  "action": "remove_child",
  "request_id": "<uuid>",
  "child_node_id": "<parent>--A"
}
```

**Process**:

1. Verify `child_node_id` starts with `<parent>--` (parent extracted from
   the validated publisher identity passed by Rule D).
2. Look up `bridge_children` entry. If absent, return success (idempotent).
3. Run **REMOVE NODE FROM GROUP FLOW** for the child (see [node_assoc.md](node_assoc.md#remove-node-from-group-flow)).
4. Asynchronously invoke `node_data_reset` Lambda for the child (cleans
   triggers, schedules, timeseries, automations).
5. Delete the AWS IoT Thing.
6. Delete the `bridge_children` entry.
7. Respond with `{status: "success"}`.

### 4.4 No Other Actions in v1

Online-state reporting and shadow updates are handled by the bridge
writing directly to `$aws/things/<child>/shadow/name/{params-...,iparams}/update`
(§5.2) rather than through a `to_cloud` control action. The
`to_cloud` channel is reserved for operations that require server-side
side effects beyond a shadow write, namely child Thing lifecycle
(`add_child`, `remove_child`).

---

## 5. Lifecycle

### 5.1 Child Discovery and Creation

```
1. Bridge discovers a child on its protocol-side network
2. Bridge publishes to rainmaker/bridges/<parent>/to_cloud:
     {action: "add_child", request_id, child_suffix, child_local_id}
3. Rule D validates publisher clientid, invokes bridge control Lambda
4. Lambda creates child Thing, writes bridge_children, runs
   ADD NODE TO GROUP FLOW
5. ADD NODE TO GROUP FLOW publishes getGroupInfo to
   rainmaker/things/<child>/from_cloud
6. Rule A rewrites that publish to
   rainmaker/bridges/<parent>/children/<child>/from_cloud
7. Bridge receives the getGroupInfo on its existing subscription, records
   the child's group/subgroup state in its local table
8. Lambda responds with bridgeAck on rainmaker/things/<parent>/from_cloud
```

### 5.2 Child Param Update (Device → Cloud)

```
1. Bridge observes a param change on the protocol side
2. Bridge publishes directly to
     $aws/things/<child>/shadow/name/params-<groupID>/update
   with the standard shadow update payload {state: {reported: {...}}}
   at QoS 1. No subscription to /update/accepted or /update/rejected.
3. AWS IoT shadow service applies the update and fires
   $aws/things/<child>/shadow/name/params-<groupID>/update/document
4. Existing ESP RainMaker Neo rules for indexed params, timeseries, and automations
   consume the document event identically to a direct device's update
```

Authorization is via the bridge IoT policy substitution
`$aws/things/${iot:Connection.Thing.ThingName}--*/shadow/name/*/update`,
which scopes write access to descendants of the bridge's own `node_id`.

**Fire-and-forget — the bridge does not subscribe to `/update/accepted`
or `/update/rejected` for any child.** Rationale:

- A stock shadow library issues per-child SUBSCRIBE for these response
  topics, which would blow the 50-subscription-per-connection AWS IoT
  cap as soon as the bridge has more than ~20 children. Scaling to many
  children requires the bridge's subscription count to stay constant
  (see §3.3.1).
- The bridge is the sole writer to each child's `params-<groupID>`
  shadow, so version-based optimistic concurrency is unnecessary.
- QoS 1 delivers MQTT-layer retry for transient network failures. Any
  failure mode that QoS 1 does not catch (extremely rare AWS IoT shadow
  service rejections) self-heals on the next genuine state change,
  which overwrites the stale state.
- Periodic full-state heartbeats were considered and rejected: shadow
  operations are billed per write, and a heartbeat-driven design is
  uneconomic at fleet scale for a recovery path that is already covered
  by QoS 1 and natural state-change traffic.

**Payload constraints** the bridge must respect before each publish:

- Payload size ≤ 8 KB (AWS IoT shadow document limit).
- Well-formed JSON.
- `state.reported` is an object.

The bridge's **own** shadow (its own iparams, its own params) is not
subject to this design — the bridge uses the stock AWS IoT shadow
library for its own Thing.

A mass protocol-side state change (Zigbee scene, side-network group
command) can fan in to N near-simultaneous shadow updates from this
section; firmware must respect §3.8's per-connection publish ceiling
when emitting them.

### 5.3 Child Param Update (Cloud → Device)

```
1. User app publishes a command to
     rainmaker/things/<child>/user/params-<groupID>/params
2. Rule B rewrites to
     rainmaker/bridges/<parent>/children/<child>/user/params-<groupID>/params
3. Bridge receives the message, extracts <child> from the topic, dispatches
   the payload to the protocol-side device
```

### 5.4 Group Broadcast / Subgroup Command

```
1. User app publishes to one of (group-control-feature.md §3.1):
     - Group-level:  rainmaker/nodes/groups/<groupID>/control
     - Subgroup:     rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
2. Bridge receives via subscription #3 (group-level) or #4 (subgroup)
   from §3.3.1.
3. Bridge parses the topic to extract the targeted scope:
     - Path ending `/control`                          → group-level broadcast
     - Path containing `subgroups/<sgID>/control`      → subgroup-level
4. Bridge dispatches:
     a. To itself, if and only if the targeted scope intersects
        self.subgroups (or the scope is the group broadcast).
     b. To each child C, if and only if the targeted scope intersects
        children[C].subgroups (or the scope is the group broadcast).
   Payload is device-type-keyed (group-control-feature.md §3.2);
   each dispatched device applies only the top-level keys matching
   its own device type.
```

**The bridge does not need to be a member of `<sgID>` to receive or relay
subgroup commands.** Subscription #4 is a wildcard over the subgroup-ID
segment, so the bridge sees every subgroup broadcast in its group
regardless of its own subgroup membership. This is the load-bearing
property that allows children to participate in subgroups independently
of the bridge: a subgroup containing only children (and not the bridge)
is fully addressable, with the bridge acting as a pure relay for that
subgroup.

The bridge keeps `self.subgroups` updated from `getGroupInfo` messages
on subscription #1, and `children[C].subgroups` updated from each child's
`getGroupInfo` messages arriving on subscription #5 (the rewrite plane,
Section 5.6).

**Authorization** is unchanged. The user's IAM session policy from
`assume_role` already gates which subgroup topics the user can publish
to; an unauthorized publish never reaches the bridge. The bridge seeing
a message for a subgroup it is not in is not a privilege issue — the
bridge and its children share the same group-level trust boundary
(`group-control-feature.md` §5.3).

### 5.5 Child Added to / Removed from Subgroup

Standard group APIs (Section "Add Node to Subgroup" / "Remove Node from
Subgroup" in [group.md](group.md)) are used by the user/app to manage child
subgroup membership. These APIs operate on the child's `group_device_mapping`
entry and publish `getGroupInfo` to `rainmaker/things/<child>/from_cloud`.

```
1. App calls POST /group/<groupID>/<subGroupID>/<child>
2. group_device_mapping row updated for <child>
3. Shadow migrated to new shadow name (params-<groupID>-<sgID>)
4. getGroupInfo published to rainmaker/things/<child>/from_cloud
5. Rule A rewrites to bridge namespace
6. Bridge updates its local child → subgroup table
```

The bridge is the only MQTT subscriber on the child's behalf. Group control
permissions and IoT policy substitution for the **child** are irrelevant
because the child does not connect.

### 5.6 `getGroupInfo` for Children

Whenever the cloud pushes `getGroupInfo` to a child (group/subgroup change,
explicit request response), Rule A rewrites it to the bridge namespace. The
bridge processes the payload by:

- Updating its local table of child → (group, subgroups).
- The child has no firmware subscription set to reconcile — the bridge is
  the only subscriber on its behalf.

### 5.7 Bridge Restart / Re-Discovery (Idempotent)

When a bridge restarts and re-discovers a previously-known child, it
publishes the same `add_child` request with the previously-used
`child_local_id`. The Lambda matches the existing entry in
`bridge_children` and returns the existing child name without creating a
duplicate. The bridge does not need to persist child suffixes across
reboots — the cloud is authoritative.

### 5.8 Bridge Disassociation / Group Move (Cascade Delete)

The cascade is triggered from `DELETE /v1/groups/{groupId}/nodes/{nodeId}`
when `nodeId` is a bridge (`node_type=bridge`), and from the assoc flow when
a bridge is being moved to a new group (= disassoc + assoc).

```
1. Disassoc/assoc handler detects bridge via the node_type=bridge column
2. Synchronously invokes bridge_cascade_delete Lambda async
3. Standard REMOVE NODE FROM GROUP FLOW runs for the bridge itself
4. node_data_reset is queued for the bridge

bridge_cascade_delete Lambda (async, fan-out):
5. Query bridge_children by parent_node_id → list of children
6. For each child (in parallel, batched):
   a. REMOVE NODE FROM GROUP FLOW
   b. Delete AWS IoT Thing
   c. Delete bridge_children entry
7. Asynchronously invoke node_data_reset with the full child node_id list
   (single invocation with the batch)
```

After cascade, the bridge re-associates to its new group (or remains
unassociated). On reconnect to its protocol-side network it re-discovers
children and recreates them via `add_child` against the new group.

> Because cascade-delete fires `node_data_reset` for every child, all
> per-child triggers, schedules, timeseries, and automations are wiped.
> Users moving a bridge to a new home will lose all configuration on
> bridged children. This is intentional per the design choice that
> children are tethered to the bridge's group and re-discovered fresh
> after a move.

### 5.9 Child Schedule / Trigger Version Sync on Bridge Reconnect

A direct device that boots or reconnects publishes `getSchedVer` /
`getTriggerVer` on its own `to_cloud` to learn whether its locally
cached schedule/trigger version is stale. The
`publish_input_event_handler` Lambda replies on `from_cloud` with the
current cloud version; if the device's local version is stale it
follows up with `getSchedDetails` / `getTriggerDetails` and rebuilds
local state from the response.

A bridge does the same for each of its children, on the **child's**
canonical `to_cloud` topic:

```
1. Bridge reconnects to AWS IoT (with persisted child → version table)
2. For each child C, bridge publishes to
     rainmaker/nodes/<C>/to_cloud
   with {event: ["getSchedVer"]}
3. Existing publish_input_event_handler Lambda (filter
   `rainmaker/nodes/+/to_cloud`, thing_name=topic(3)=C) replies on
     rainmaker/nodes/<C>/from_cloud
   with {event: ["getSchedVer"], getSchedVer: {version: <N>}}
4. Rule A rewrites that reply to
     rainmaker/bridges/<parent>/children/<C>/from_cloud
5. Bridge receives on subscription #5, compares <N> to its locally
   cached version. On mismatch, publishes
     {event: ["getSchedDetails"]} on rainmaker/nodes/<C>/to_cloud,
   processes the reply (also rewritten by Rule A), and pushes the
   updated schedule to the protocol-side child
6. Symmetric flow for triggers via getTriggerVer / getTriggerDetails
```

The same path covers cloud-initiated updates while the bridge is
**online**: `ScheduleService.Put` → `SendScheduleDetails` →
`rainmaker/nodes/<C>/from_cloud` → Rule A → bridge receives the new
schedule without polling. The `to_cloud` path in this section
exists to recover changes that landed during a bridge offline window.

Authorization is at the IoT policy layer (§3.5 `ChildrenPublish`);
the bridge cannot publish on any other parent's children. No Lambda
change is needed — the handler already
treats `thing_name = topic(3)` as authoritative, and the
naming-constraint-pinned policy substitution guarantees that
`topic(3)` for a publish from a bridge is always one of its own
children.

The bridge persists `(child → schedule_version, trigger_version)` so
that on reconnect it can skip the `getSched/TriggerDetails` round-trip
when the cloud version matches its cache. Without persistence, the
bridge can still operate by always treating reconnect as
version-mismatch and pulling fresh state — at the cost of one extra
round-trip per child per reconnect.

The reconnect publish burst in step 2 (one `getSchedVer` per child, and
correspondingly one `getTriggerVer` per child) is a per-N pattern; the
firmware must spread it across time per §3.8.

### 5.10 Bridge Disconnect (Online State Cascade)

```
1. AWS IoT presence event $aws/events/presence/disconnected/<parent>
2. Core's presence handler drops the event if the session is stale, then calls
   the in-process bridge cascade
3. Cascade reads node_type for <parent>; returns unless it is bridge
4. Query bridge_children by parent_node_id
5. For each child, update iparams shadow with online=false (bounded fan-out)
```

The reverse (`connected` → online=true) is **not** automatic. The bridge,
on reconnect, must reconfirm each child's reachability over the protocol
side and report `online=true` per child via a direct shadow update to
`$aws/things/<child>/shadow/name/iparams/update` (see §5.2 for the
same fire-and-forget shadow update model). This is a per-N burst and is
subject to the publish-rate spreading requirement in §3.8.

---

## 6. Security Analysis

### 6.1 Bridge Isolation (Enforced at IoT Policy Layer)

The bridge's three new IoT policy statements (§3.5) are all pinned to
`${iot:Connection.Thing.ThingName}` in the topic prefix. A bridge cannot
subscribe to or publish on any other bridge's children namespace, nor on
another bridge's `to_cloud` control plane, nor on another bridge's
children's shadows.

### 6.2 Child Creation Authorization (Enforced at IoT Rule Layer)

Rule D validates that the source-topic publisher's MQTT clientid matches
the `<parent>` segment of the `to_cloud` topic. A malicious bridge cannot
forge a `to_cloud` message claiming to be from another bridge because AWS
IoT does not allow connecting with another Thing's clientid (enforced by
the base policy on the other bridge's certificate).

### 6.3 Cross-Parent Shadow Writes (Enforced at IoT Policy Layer)

The bridge IoT policy statement for shadow publishes uses the substitution
`$aws/things/${iot:Connection.Thing.ThingName}--*/shadow/name/*/update`. At
connect time, AWS IoT substitutes the bridge's own thing name, so a bridge
`brdge` can publish only on `$aws/things/brdge--*/shadow/...`.

This is airtight because of the `--` naming constraints in §3.1 (no `--`
in bridge names, no `--` in direct device names, no `--` in child
suffixes). With those constraints, `brdge--*` cannot match any other
bridge or any direct device or any other bridge's descendants — it can
only match things created by `<parent>=brdge`'s `add_child` calls.

A bridge cannot publish to `$aws/things/<other_thing>/shadow/...` for any
thing that does not begin with its own thing name + `--`. AWS IoT enforces
this at the policy layer at every publish — no rule WHERE clause needed.

If the `--` constraints are ever relaxed, the substitution becomes
ambiguous and this security property breaks. The registration validation
in §3.1 is therefore load-bearing.

### 6.4 Child Has No Credentials

A child Thing has no certificate, so there is no credential to leak. All
traffic to/from the child transits the bridge's MQTT session, which is
authenticated by the bridge's certificate.

### 6.5 IAM Session Policy for App Users — Unchanged

`assume_role` generates session policies based on group/subgroup
membership. Children appear in `group_device_mapping` like any other node,
so the existing `topic/rainmaker/things/*/user/params-<groupID>*/*` pattern
already covers user→child unicast publishes. The app does not interact
with the bridge namespace directly.

### 6.6 Abuse Vector: Bridge Spamming `add_child`

There is no per-bridge cap on children in v1. A misbehaving bridge could
in principle create many Thing records. Mitigations:

- The bridge control Lambda should rate-limit per-parent `add_child`
  calls (e.g., max 10/s, max 1000 within 24h) with the limits configurable
  per SKU.
- Alerting on `bridge_children` row count per `parent_node_id` above a
  threshold.
- Future hardening: a configurable per-SKU max-children cap.

### 6.7 Cross-Subgroup Isolation Within the Same Group

As with normal devices (see [group-control-feature.md](group-control-feature.md#53-cross-subgroup-isolation-within-the-same-home-enforced-at-firmware-layer)),
this is enforced at the bridge firmware layer rather than at the IoT
policy layer. The bridge sees commands for all subgroups in its group and
is trusted to deliver them only to the correct children.

---

## 7. What Does Not Change

| Component | Why |
|---|---|
| Node registration CSV flow | Bridges register exactly like other nodes; children skip registration entirely |
| Node association `challenge_response` flow | Used by bridges; children skip association (created via `add_child`) |
| `group_device_mapping` schema | Children are normal rows in this table |
| `user_group_mapping` schema | Sharing works through the existing flow for children |
| `assume_role` and IAM session policy generation | Existing patterns cover children via the standard `rainmaker/things/*/...` topic shape |
| Group control topic structure (`rainmaker/things/g/...`) | Bridge subscribes per existing rules; forwards to children locally |
| `getGroupInfo` payload format | Reused unchanged; rewritten by Rule A onto bridge namespace |
| `node_data_reset` Lambda | Called per-child during cascade delete; no schema change |
| Shadow naming convention (`params-<groupID>[-<sg1>...]`) | Reused for children |
| Triggers, schedules, automations, timeseries services | All operate server-side on the child's shadow/node_id; work unchanged |
| User/app MQTT topic conventions | App sees children identically to direct devices |

---

## 8. Out of Scope (v1)

The following are **explicitly not supported** for bridged children in v1.
Each entry includes the reason and the upgrade path if/when scoped in.

### 8.1 AWS IoT Jobs

Children cannot be Job targets. A job created targeting a child Thing
directly (or via a Thing Group that includes children) will be created in
AWS IoT, but the bridge will never see it and the execution will stay
`QUEUED` indefinitely.

- **Admin/API enforcement**: the job-create admin API must reject child
  node IDs (those with the `parent_node_id` attribute) and the admin UI
  must hide them from job-target pickers.
- **Upgrade path**: add two more IoT Rules covering
  `$aws/things/<child>/jobs/*` topics in both directions, plus two more
  bridge policy statements. The bridge_children table and parent linkage
  are already in place.

### 8.2 OTA / Firmware Updates for Children

RainMaker does not orchestrate firmware updates for bridged children. If
the bridge protocol supports OTA (Zigbee OTA, etc.), it is handled
out-of-band by the bridge vendor.

- **UX**: the app and admin UI must surface a note on child device detail
  pages: "Firmware updates for this device are managed by its bridge."
- **Upgrade path**: when Jobs support lands, the standard ESP RainMaker Neo OTA flow
  works for children as soon as the bridge firmware implements the Jobs
  protocol on their behalf.

### 8.3 Fleet Indexing for Children

Fleet Indexing's `connectivity.connected` will always be `false` for
children. Children are excluded from any fleet-index-backed query.

- **Admin tooling**: child enumeration uses DynamoDB queries against
  `bridge_children` and `group_device_mapping`, not Fleet Index.
- **Filter rule**: any admin UI that lists "all nodes" via Fleet Index
  must add a filter excluding Things with the `parent_node_id`
  attribute set, or render bridged children from a separate path.
- **Upgrade path**: enable shadow indexing on `iparams` and treat
  `iparams.online` as the source-of-truth field for children when
  querying.

### 8.4 Cloud-Anchored Matter Fabric Membership for Children

What is **not** supported: a bridged child being a member of the
cloud-anchored Matter fabric described in [group.md](group.md) and
[node_assoc.md](node_assoc.md). Specifically, the cloud does not issue
a Matter Device NOC for a child, the child is not a leaf of the group's
`root_ca`, and `nocsr_elements` association does not run for a child.
The cloud has no private key for the child and no attestation signature
to verify; even on a Matter-capable group, a child is created as a
non-Matter-fabric node from the cloud's perspective.

What **is** supported: the child being a Matter device on the **bridge's
private protocol-side network**. The bridge is free to act as a Matter
commissioner/admin operating a local Matter fabric anchored at the
bridge, or to participate in any Matter fabric on its side and re-expose
bridged endpoints to the cloud. From the cloud boundary, this is
indistinguishable from any other bridge protocol — the child is a
RainMaker node with a shadow, and the bridge translates Matter cluster
reads/writes ↔ RainMaker params.

This protocol-agnosticism is a strength of the bridge model: Matter,
Zigbee, Z-Wave, Thread, BLE-mesh, and proprietary RF are all equivalent
at the cloud boundary. The bridge's choice of side-network protocol
does not affect any cloud schema, API, or topic.

The **bridge itself** (a regular RainMaker node) can still participate
in the cloud-anchored Matter fabric if it is associated to a
Matter-capable group, via the standard `challenge_response` or
`nocsr_elements` association flow in [node_assoc.md](node_assoc.md).
This is independent of whether its children speak Matter, Zigbee, or
anything else. A common deployment is a bridge that is itself a
Matter-fabric member (cloud-anchored) while also acting as a Matter
admin for a local fabric of bridged endpoints — the two fabrics are
distinct and only the cloud-anchored one involves the RainMaker NOC
flow.

### 8.5 Secure Tunneling, Device Defender, Fleet Provisioning

Not applicable by construction — children have no MQTT session, no
traffic to monitor, no provisioning event.

### 8.6 Per-Bridge Child Cap

No hard limit on the number of children per bridge in v1. The bridge
control Lambda performs basic rate-limiting (Section 6.6). A future
SKU-level cap can be added without data-model changes.

---

## 9. Topic Reference Summary

```
# Bridge's own topics (unchanged from a standard device)
rainmaker/nodes/<parent>/from_cloud
rainmaker/nodes/<parent>/user/params-<groupID>[-<sg1>-<sg2>-<sg3>]/params

# Group control (per group-control-feature.md §3.1)
rainmaker/nodes/groups/<groupID>/control                                            # group broadcast (all members)
rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control                           # subgroup command

# Bridge namespace (new)
rainmaker/bridges/<parent>/to_cloud                                                 # bridge → cloud, control plane (Rule D — basic-ingest capable via $aws/rules/bridge_to_cloud_rule/...)
rainmaker/bridges/<parent>/children/<child>/from_cloud                              # cloud → bridge (rewritten from child's from_cloud by Rule A)
rainmaker/bridges/<parent>/children/<child>/user/<shadow_name>/params               # cloud → bridge (rewritten from child's unicast by Rule B)

# Child shadow updates — bridge publishes directly to canonical $aws topic
$aws/things/<child>/shadow/name/<shadow_name>/update                                # bridge → cloud, fire-and-forget QoS 1

# Child's own topics — bridge publishes on behalf of each child via
# substitution `rainmaker/nodes/<self>--*/...` in the §3.5
# ChildrenPublish statement. Reply paths land on subscription #5 via
# Rule A; ingestion paths (ts, notify) consume server-side identical
# to a direct device's traffic.
rainmaker/nodes/<child>/from_cloud                                                  # cloud → child (always rewritten by Rule A; bridge sees on sub #5)
rainmaker/nodes/<child>/user/params-<groupID>[-...]/params                          # cloud → child unicast (rewritten by Rule B; sub #6)
rainmaker/nodes/<child>/to_cloud                                                    # bridge → cloud (control plane: getSchedVer, etc. — §5.9)
rainmaker/nodes/<child>/ts/<key>                                                    # bridge → cloud (time-series ingestion — node_ts_rule)
rainmaker/nodes/<child>/notify/<topic_suffix>                                       # bridge → cloud (per-child notifications — node_notify_rule)
```

A bridge maintains exactly **6 wildcard MQTT subscriptions** (see §3.3.1),
independent of the number of children, the number of subgroups in the
group, and the bridge's own subgroup memberships. Subscribe/unsubscribe is
triggered only by group move and (re)connect — never by child or subgroup
changes.

---

## 10. FAQs

1. **Can a child have its own certificate later (promotion to direct device)?**
   - Not in v1. To convert a child to a direct device, remove it from the
     bridge and re-register it via the standard registration flow.

2. **Can two bridges share a child?**
   - No. A child is owned by exactly one parent, enforced by the
     `bridge_children` table and the parent prefix in the child thing
     name.

3. **What happens if a bridge tries to create a child whose computed name collides with an existing direct device?**
   - The IoT `CreateThing` call fails with `ResourceAlreadyExistsException`.
     The control Lambda returns `child_suffix_in_use`. The bridge should
     retry with a different `child_suffix`. Because child thing names are
     prefixed with the bridge's own `node_id`, collisions with foreign
     devices are not normally possible, but a collision with a previously-
     leaked child of the same bridge could occur and is handled by the
     idempotency check on `child_local_id`.

4. **What happens to a child's automations when the bridge moves to a new group?**
   - They are deleted by `node_data_reset` during cascade delete. After
     re-discovery in the new group, the child is a fresh node with no
     automations. This is by design.

5. **Can a user share a single child with another user without sharing the bridge?**
   - Yes. Children participate in subgroup-based sharing identically to
     direct devices. The user can place a single child in a subgroup and
     share that subgroup.

6. **Does the bridge need to re-publish `getGroupInfo` requests at startup on behalf of each child?**
   - No. The cloud is authoritative; the bridge keeps `child → (group, subgroups)`
     state locally and persists it. On reconnect, the bridge can either
     trust its persisted state, or for paranoia, publish a single
     getGroupInfo request per child on the child's unicast topic — these
     are rewritten back to the bridge and refresh the local table.

7. **What is the maximum size of `child_local_id`?**
   - Bounded by DynamoDB attribute size limits. In practice,
     bridge-protocol identifiers are short (e.g., Zigbee EUI-64 is 8
     bytes).

8. **Why the double-hyphen separator instead of single?**
   - Pure-SQL parent extraction in IoT Rules A and B requires an
     unambiguous delimiter that no other thing name in the system uses.
     Single `-` is common in arbitrary thing names; `--` essentially
     never appears. With `--` as the separator, the rules can use
     `indexof(topic(3), '--') >= 0` as a one-line filter and
     `substring(topic(3), 0, indexof(topic(3), '--'))` to extract the
     parent — no Thing-attribute lookup, no enrichment Lambda. The
     naming constraints in §3.1 lock in this property.

---

## 11. Deployment and upgrade

Bridge is an optional stack group (`bridge` in `cdk/Stackfile.yaml`), built by
`cdk/apps/bridge.py` and deployed with `make deploy-bridge`. It is two stacks:
`rmng-bridge-base` owns the durable resources (the `rmng-bridge-children` table
and the `rmng-bridge-policy` IoT policy) and `rmng-bridge-core` the compute
(Lambdas + IoT rules). Core references base by name, not by a CloudFormation
cross-stack reference, so base must exist first; the app states that ordering.

Two of the lifecycle hooks run inside core rather than as Lambdas of their own:
the node-register hook installs itself into `nodelifecycle` and is linked by the
binaries that register nodes, and the presence cascade is called by core's own
presence handler, which already holds the disconnect event. Only
`add_remove_child` (driven by its IoT rules) and the node-left-group cascade
(deliberately kept asynchronous, since a bridge's child count is unbounded —
§8) remain separate Lambdas.

### Upgrading from the single `rmng-bridge` stack

Deployments made before the base/core split own everything under one stack named
`rmng-bridge`. CloudFormation permits exactly one owning stack per physical
resource, so `rmng-bridge-base` cannot create `rmng-bridge-children` or
`rmng-bridge-policy` while that stack still holds them, and the deploy fails with
`already exists in stack .../rmng-bridge`.

```bash
# 1. Retire the pre-split stack. This deletes the bridge-children table with it:
#    existing parent/child rows are lost and children must be re-added.
aws cloudformation delete-stack --stack-name rmng-bridge --region <region>
aws cloudformation wait stack-delete-complete --stack-name rmng-bridge --region <region>

# 2. Its Lambdas leave unmanaged log groups behind, which the new stack declares
#    as CloudFormation resources and so cannot create over. Clear only the ones
#    the new templates name — point the script at this group's output directory,
#    never at a shared cdk.out holding other groups' templates.
make synth-bridge
python3 scripts/delete_unmanaged_lambda_log_groups.py cdk/cdk.out.bridge --region <region>   # dry run
python3 scripts/delete_unmanaged_lambda_log_groups.py cdk/cdk.out.bridge --region <region> --apply --yes

# 3. Deploy the split stacks, then core, which now carries the in-process hooks.
make deploy-bridge
make deploy-rmng
```

Step 1 removes `rmng-bridge-policy` along with the stack, so bridges registered
against it lose their permissions until re-registered; registration re-attaches
the policy through the node-register hook.
