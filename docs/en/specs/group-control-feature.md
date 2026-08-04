# Group Control Feature Design

## 1. Overview

This document describes the design for simultaneous control of multiple devices
through a single MQTT publish from the phone app. The feature allows a user to
define a **control group** (e.g., "first floor lights") and send one command
that all devices in that group receive and act on concurrently.

The design maps control groups onto the existing **subgroup** abstraction. No
new data model is introduced. The primary changes are: a new MQTT topic
namespace for group commands, a single additional statement in the device IoT
policy, and a lightweight AWS IoT Thing attribute set on group join/leave.

Group control commands are **addressed by device type** rather than by device
name. A single broadcast carries an envelope keyed by device type
(e.g., `esp.device.light`); each receiving node applies the payload only to
its own devices whose type matches one of the top-level keys.

---

## 2. Background

### 2.1 Existing Group and Subgroup Model

- A **group** (`pgrp`) is the top-level container for devices in a single
  home/site. A device belongs to exactly one group.
- A **subgroup** is an optional organizational subdivision within a group.
  A device can belong to up to **3 subgroups** simultaneously (stored as
  `subgrp1`, `subgrp2`, `subgrp3` in the `rmng-group-node-assoc` DynamoDB
  table).
- Group IDs and subgroup IDs are randomly generated opaque strings.

### 2.2 Individual Device Control Topic

The phone app sends commands to a single device on the topic (referred to as
the **unicast topic** throughout this document):

```
rainmaker/nodes/<nodeID>/user/params-<groupID>[-<sg1>-<sg2>-<sg3>]/params
```

The suffix `params-<groupID>[-<subgroupIDs>]` is called the **shadow name**.
Subgroup IDs in the shadow name are always sorted **alphabetically** and joined
with `-`.

Examples:
```
rainmaker/nodes/node-abc/user/params-grp1/params
rainmaker/nodes/node-abc/user/params-grp1-sgX/params
rainmaker/nodes/node-abc/user/params-grp1-sgX-sgY/params
```

The device subscribes to exactly one unicast topic, which encodes **all** of
its subgroup memberships simultaneously. Unicast payloads are device-name
keyed (`{ "<deviceId>": { "<paramId>": <value> } }`) — this remains
unchanged.

### 2.3 IAM-Level Access Control (assume_role)

When a user calls `POST /assume_role`, the Lambda generates a temporary IAM
session policy scoped to the groups the user has access to. For users with
**full group access**, the policy includes a unicast publish pattern covering
every device in the group:

```
# Unicast — full group access
arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/*/user/params-<groupID>*/*
```

Group control topics use a separate path namespace under
`nodes/groups/<gid>/...` (terminating in a `control` segment) and require
their own IAM resource patterns. **Subgroup-only access** is expressed
exclusively against the group control namespace — see Section 3.2.

Because these resources are emitted **per group**, the policy grows with the
caller's group count and is bounded by STS's 2048-character session-policy
limit. Each full-access group costs 4 resources and each shared subgroup 2,
which caps a user at roughly 4 groups per session. Adding a per-group resource
pattern here therefore lowers that ceiling for every user — see
[group.md](group.md#capacity-limits).

### 2.4 The getGroupInfo Mechanism

The cloud notifies a device of its current group/subgroup membership by
publishing to `rainmaker/nodes/<nodeID>/from_cloud` with the following
payload:

```json
{
  "event": ["getGroupInfo"],
  "getGroupInfo": {
    "pgrp": "<groupID>",
    "subgrps": ["<sgID1>", "<sgID2>"]
  }
}
```

`subgrps` is absent (or an empty array) when the device has no subgroup
memberships. `pgrp` is absent when the device does not belong to any group.

This notification is **pushed by the cloud automatically** whenever the
device's membership changes:

- when a device is added to a group
- when a device is added to or removed from a subgroup

The device can also **request** this information proactively at startup by
publishing `{"event": ["getGroupInfo"]}` on its **to-cloud** topic,
`rainmaker/nodes/<nodeID>/to_cloud` — the single inbound device→cloud event
channel (`node_to_cloud_rule` selects `FROM 'rainmaker/nodes/+/to_cloud'`). That
topic is keyed on the node id alone, so a device can ask without knowing its
group membership or its current shadow name. The cloud responds on `from_cloud`
with the current authoritative state. See
[node_params_messaging.md](node_params_messaging.md) for the full to-cloud event
set.

---

## 3. Design

### 3.1 Group Control MQTT Topic Namespace

Group control topics live under the path `groups/<groupID>/...` with a
terminating `control` segment, creating an unambiguous namespace separate
from params topics:

```
# Group-level broadcast (targets ALL devices in the group)
rainmaker/nodes/groups/<groupID>/control

# Subgroup-level command (targets all devices in one specific subgroup)
rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
```

**Topic shape rationale:**

1. The leading `nodes/groups/<groupID>/` segment scopes the entire namespace to
   one group, so a single IoT policy statement suffices for cross-group
   isolation (Section 3.3).
2. The literal `control` segment cleanly separates group control from any
   other group-scoped per-node traffic that might be added later (e.g., shadow
   topics, telemetry).
3. The `subgroups/` collection segment matches the REST convention used
   elsewhere (e.g., `GET /v1/groups/{groupId}/subgroups/{subGroupId}`) and keeps
   the topic structure self-describing.
4. A group control topic cannot collide with a unicast topic because real AWS
   IoT thing names cannot contain `/`, so `nodes/<nodeID>/...` and
   `nodes/groups/<groupID>/...` are disjoint namespaces.

**Subgroup topic semantics:** the subgroup component is always a **single
subgroup ID**. This is different from the unicast shadow name, which encodes
all of a device's subgroups simultaneously. One subgroup control topic exists
per subgroup; a device in multiple subgroups subscribes to each one
independently.

### 3.2 Phone App: Publishing a Group Command

The phone app publishes **once** to the appropriate group control topic. All
devices subscribed to that topic receive the message simultaneously via AWS IoT
Core's fan-out.

**Payload shape (device-type addressed):**

```json
{
  "esp.device.light": {
    "params": {
      "esp.param.power": true,
      "esp.param.brightness": 75
    }
  },
  "esp.device.fan": {
    "params": {
      "esp.param.power": false
    }
  }
}
```

Top-level keys are device types. Each device's value is an object that holds
nested sub-keys describing what to apply — currently only `params` is defined,
with room for additional sub-keys (e.g., `cmd`, `meta`) in future without
another topic rename. Each receiving node applies the payload only to its own
devices whose type matches a top-level key; other devices on the node are
ignored.

Examples:

```
# Send a command to all devices in the group (broadcast)
rainmaker/nodes/groups/grp1/control

# Send a command to all devices in subgroup sgX only
rainmaker/nodes/groups/grp1/subgroups/sgX/control
```

**IAM session policy coverage — new patterns required.**

The existing `nodes/*/user/params-<groupID>*/*` patterns in the assume-role
session policy cover only the unicast topic and do **not** match the new group
control namespace. New publish-scope patterns must be added:

```
# Full group access — both group-level and any subgroup
arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/groups/<groupID>/control
arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/groups/<groupID>/subgroups/*/control

# Subgroup-only access
arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
```

The single-segment wildcard above is the IAM `*` wildcard used in IoT topic
ARNs — it matches any character sequence including `/`, which is why the
broadest "full group access" pattern can use one ARN per resource type. For
subgroup-only access, no wildcard is needed because the exact subgroup ID is
known.

A user with subgroup-only access does **not** get permission to publish on the
group-level broadcast topic. This matches the existing semantics for the
unicast topic patterns.

### 3.3 Device IoT Policy

The existing default device IoT policy is **extended with one additional
statement**. The full modified policy is:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["iot:Connect"],
      "Resource": "arn:aws:iot:<region>:<account>:client/${iot:Connection.Thing.ThingName}"
    },
    {
      "Effect": "Allow",
      "Action": ["iot:Publish", "iot:Subscribe", "iot:Receive"],
      "Resource": [
        "arn:aws:iot:<region>:<account>:topic/rainmaker/nodes/${iot:Connection.Thing.ThingName}/*",
        "arn:aws:iot:<region>:<account>:topicfilter/rainmaker/nodes/${iot:Connection.Thing.ThingName}/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": ["iot:Subscribe", "iot:Receive"],
      "Resource": "arn:aws:iot:<region>:<account>:topicfilter/rainmaker/nodes/groups/${iot:Connection.Thing.Attributes[group_id]}/*control"
    }
  ]
}
```

The third statement is new. At MQTT connection time, AWS IoT substitutes
`${iot:Connection.Thing.Attributes[group_id]}` with the value of the
`group_id` attribute stored on the AWS IoT Thing. The IAM `*` wildcard
matches any character sequence including `/`, so the single `*control`
suffix covers both the group-level broadcast (`groups/<gid>/control`, where
`*` matches the empty string) and any subgroup
(`groups/<gid>/subgroups/<sgID>/control`, where `*` matches `subgroups/<sgID>/`).
A single resource is used here to keep the device policy under the AWS IoT
2048-byte size limit.

**Behaviour when `group_id` attribute is absent or empty:** The substitution
produces `topicfilter/rainmaker/nodes/groups//*control`, which matches no
real topic. The statement grants no effective permission. This is the
correct behaviour for devices not yet assigned to a group.

**This is a single static policy covering all devices and all groups.** No
per-group policy or Thing Group resource is created, so the **device** policy
carries no size or policy-count concern however many groups exist.

> **This does not extend to the user side.** The *user* session policy issued by
> `POST /v1/assumed-roles` is built per caller and enumerates ARNs per accessible
> group, so it grows with group count and is bounded by STS's 2048-character
> limit. That bound is a real product limit — roughly 4 groups per user — and is
> specified in [group.md](group.md#capacity-limits) and
> [user_auth.md](user_auth.md#33-mqtt-mode-session-policy). Any new per-group ARN
> added to the user policy lowers that ceiling for every user.

### 3.4 AWS IoT Thing Attribute: `group_id`

The `group_id` attribute on the AWS IoT Thing is the mechanism by which the
static policy (Section 3.3) is scoped to the correct group at runtime.

| Event | `group_id` attribute value |
|---|---|
| Device has no group | Absent or empty string |
| Device joins group `grp1` | `grp1` |
| Device added to/removed from a subgroup | Unchanged (still `grp1`) |
| Device removed from group | Cleared (empty string or removed) |
| Device moves to a different group | Updated to new groupID |

The attribute is managed via the AWS IoT `UpdateThing` API call. This is a
single cheap API call; it does not create any new AWS IoT resources.

**Subgroup changes do not affect this attribute.** The attribute only encodes
group-level membership. Subgroup-level subscription management is handled
entirely by the firmware via `getGroupInfo` (Section 3.5).

### 3.5 Firmware: Subscription Reconciliation

The firmware treats `getGroupInfo` as the **single authoritative source** for
which group control topics to subscribe to. Reconciliation runs every time a
`getGroupInfo` payload is received, whether proactively requested at startup or
pushed by the cloud.

**Subscription set computation:**

Given `getGroupInfo` payload with `pgrp = "grp1"` and `subgrps = ["sgX", "sgY"]`:

```
required_subscriptions = {
    "rainmaker/nodes/groups/grp1/control",                   // group broadcast
    "rainmaker/nodes/groups/grp1/subgroups/sgX/control",     // subgroup sgX
    "rainmaker/nodes/groups/grp1/subgroups/sgY/control"      // subgroup sgY
}
```

If `pgrp` is absent, `required_subscriptions` is empty.

**Reconciliation algorithm:**

```
subscribe(required_subscriptions - current_subscriptions)
unsubscribe(current_subscriptions - required_subscriptions)
current_subscriptions = required_subscriptions
```

**Message processing:** On receiving a message on a group control topic, the
firmware:

1. Parses the payload as a map of device type → control envelope.
2. For each top-level key, finds local devices whose device type matches.
3. Applies the nested `params` (or other recognized sub-keys) to each match.
4. Ignores top-level keys that match no local device.

The unicast topic continues to use the existing device-name-keyed payload
format and its existing handler. Group control messages and unicast messages
are processed by **different** handlers because their payload shapes differ.

**Startup sequence:**

```
1. Connect to AWS IoT broker
2. Subscribe to: rainmaker/nodes/<nodeID>/from_cloud
3. Publish getGroupInfo request to rainmaker/nodes/<nodeID>/to_cloud
4. Receive getGroupInfo response via from_cloud
5. Run reconciliation → subscribe/unsubscribe group control topics
6. Subscribe to the unicast params topic for the resolved shadow name
```

The request topic depends only on the node id, so the device does not need a
valid shadow name to ask — which is what makes this recoverable after the device
was offline during a membership change. The unicast subscription is established
from the resolved group info rather than from a remembered shadow name; the
firmware documentation is authoritative for the exact ordering on the device
side.

---

## 4. Lifecycle

### 4.1 Device Joins a Group

Triggered when a device is added to a group.

```
1. DB: rmng-group-node-assoc entry created
2. Shadow: migrated to new shadow name
3. Thing attribute: group_id set to groupID
4. Notification: getGroupInfo pushed to device

Device receives getGroupInfo:
5. Firmware reconciles → subscribes to:
     rainmaker/nodes/groups/<groupID>/control
```

### 4.2 Device Added to a Subgroup

Triggered when a device is added to a subgroup.

```
1. DB: subgrpN column added in rmng-group-node-assoc
2. Shadow: migrated to new shadow name (adds sgID)
3. Thing attribute: unchanged — the group has not changed
4. Notification: getGroupInfo pushed to device

Device receives getGroupInfo with updated subgrps:
5. Firmware reconciles → subscribes to new subgroup control topic:
     rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
```

### 4.3 Device Removed from a Subgroup

Triggered when a device is removed from a subgroup.

```
1. DB: subgrpN column removed from rmng-group-node-assoc
2. Shadow: migrated to new shadow name (removes sgID)
3. Thing attribute: unchanged — the group has not changed
4. Notification: getGroupInfo pushed to device

Device receives getGroupInfo with updated subgrps:
5. Firmware reconciles → unsubscribes from removed subgroup control topic:
     rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
```

### 4.4 Device Removed from a Group

```
1. DB: rmng-group-node-assoc entry deleted
2. Thing attribute: group_id cleared (set to "")
3. Group shadow deleted and user tags cleared from iparams
4. Notification: getGroupInfo pushed to device

Device receives getGroupInfo with pgrp absent:
5. Firmware reconciles → unsubscribes from all group control topics
6. IoT policy: group_id attribute is now empty →
   the group-control policy statement grants no permissions
```

Steps 2–4 are one synchronous sequence, so a device that is online sees the
attribute clear and the notification arrive together.

### 4.5 Subgroup Deleted

Deleting a subgroup iterates over every device holding that subgroup membership
and removes each one from it. Each individual removal follows §4.3 and triggers
its own `getGroupInfo` push to that device. There is no bulk notification
mechanism.

### 4.6 Group Deleted

Deleting a group enumerates the group's devices from `rmng-group-node-assoc`
first, clearing each device's `group_id` Thing attribute and pushing
`getGroupInfo` to it, then batch-deletes the association entries.

### 4.7 Device Offline During a Membership Change

When a membership change occurs while the device is offline:

1. The `getGroupInfo` push publishes to `from_cloud` with QoS 1. AWS IoT Core
   will queue the message for delivery when the device reconnects (subject to
   the configured message queue limits).
2. If the queued message expires before the device reconnects, the device
   recovers via its startup `getGroupInfo` request (step 4 of the startup
   sequence in Section 3.5). The cloud always responds with the current
   authoritative state.
3. There is no scenario where a device remains permanently out of sync with its
   group control subscriptions after reconnecting.

---

## 5. Security Analysis

### 5.1 Cross-home isolation (enforced at IoT policy layer)

The `group_id` attribute is device-specific and set to the device's own
groupID. The policy statement resolves to
`topicfilter/rainmaker/nodes/groups/<ownGroupID>/*control`. A device cannot
subscribe to another home's group control topics because its `group_id`
attribute does not contain that home's groupID.

### 5.2 Unicast command isolation (enforced at IoT policy layer)

Unicast topics have the form `nodes/<nodeID>/user/*`. The second policy
statement (existing) uses `${iot:Connection.Thing.ThingName}`, which resolves
only to the device's own nodeID. The third statement (new) is scoped under
`nodes/groups/<groupID>/`. Because real nodeIDs cannot contain `/`, the
namespaces `nodes/<nodeID>/` and `nodes/groups/<groupID>/` are disjoint. A
device cannot subscribe to another device's unicast topic.

### 5.3 Cross-subgroup isolation within the same home (enforced at firmware layer)

All devices in the same group share the same `group_id` attribute value. The
third policy statement therefore grants all of them subscribe permission to
`nodes/groups/<groupID>/*control`, which encompasses all subgroup control
topics within that group. A device could in principle subscribe to a sibling
subgroup's control topic.

**This is accepted by design** for the following reasons:

- Subgroup IDs are randomly generated and not discoverable by sibling devices
  through any platform mechanism.
- All devices in the same group are in the same physical deployment (e.g., one
  home) and are within the same trust boundary.
- Exploiting this requires firmware modification, which implies physical access
  to the device — a threat that exists independently through other attack
  vectors.
- The user-facing security boundary (which phone app users can publish to which
  group control topics) is enforced at the IAM session policy layer and is
  unaffected.

### 5.4 User publish scoping (enforced at IAM session policy layer)

The IAM session policy generated at assume-role time restricts which group
control topics a user can publish to. With the new IAM resource patterns
(Section 3.2):

- A user with **full group access** to `grp1` gets resources covering both
  `topic/rainmaker/nodes/groups/grp1/control` and
  `topic/rainmaker/nodes/groups/grp1/subgroups/*/control`. They can publish to
  the group broadcast and any subgroup.
- A user with **subgroup-only access** to `sgX` within `grp1` gets only
  `topic/rainmaker/nodes/groups/grp1/subgroups/sgX/control`. They cannot
  publish on the group-level broadcast topic, and they cannot publish on a
  sibling subgroup's topic such as
  `nodes/groups/grp1/subgroups/sgY/control`.

### 5.5 Cross-device-type filtering (enforced at firmware layer)

Because group control payloads are addressed by device type, a single broadcast
may carry settings for multiple device types. Each receiving node applies the
payload only to its own devices whose device type matches a top-level key.
Top-level keys that don't match any local device are silently ignored. There is
no platform mechanism by which a node could coerce another node into accepting
a message whose top-level key does not match the target's device type, because
the filtering is done locally on each node from its own device-type registry.

---

## 6. What Does Not Change

The following components require **no modification**:

| Component | Why |
|---|---|
| `getGroupInfo` response format (`pgrp`, `subgrps` fields) | Already contains all information the firmware needs |
| The group-add notification path | Already pushes `getGroupInfo` to the device; no change needed |
| The subgroup-update notification path | Already pushes `getGroupInfo` and migrates the shadow; no change needed |
| Shadow naming convention (`params-<groupID>-<sg1>-<sg2>`) | Unchanged; only applies to unicast topics and device shadows |
| Individual device unicast topic structure and payload shape | Unchanged |
| Assume-role authentication and credential flow | Unchanged |
| `rmng-user-group-assoc` and `rmng-group-node-assoc` DB schemas | Unchanged |
| The user-accessible-groups lookup | Unchanged |

---

## 7. Where This Lives

| Concern | Where it is enforced |
| --- | --- |
| `group_id` Thing attribute set / cleared, `getGroupInfo` push | The node add / remove side-effect path, run synchronously with the membership change |
| Add / remove node, group and subgroup lifecycle | The group and node control-plane APIs |
| Device group-control permissions | The device IoT policy, `rmng-base-node-policy` |
| User publish scoping | The MQTT session policy minted by `POST /v1/assumed-roles` |
| `getGroupInfo` request handling | The to-cloud event handler |

---

## 8. Topic Reference Summary

```
# Unicast (individual device command — existing, unchanged; device-name keyed payload)
rainmaker/nodes/<nodeID>/user/params-<groupID>[-<sg1>-<sg2>-<sg3>]/params
  └─ subgroups sorted alphabetically, all subgroups combined in one topic

# Group broadcast (all devices in the group; device-type keyed payload)
rainmaker/nodes/groups/<groupID>/control

# Subgroup command (all devices in one specific subgroup; device-type keyed payload)
rainmaker/nodes/groups/<groupID>/subgroups/<sgID>/control
  └─ one topic per subgroup, NOT a combined topic

# Cloud-to-device notification channel (existing, unchanged)
rainmaker/nodes/<nodeID>/from_cloud
```

A device with `pgrp=grp1` and `subgrps=[sgX, sgY]` subscribes to:
```
rainmaker/nodes/node-abc/user/params-grp1-sgX-sgY/params  ← unicast
rainmaker/nodes/groups/grp1/control                       ← group broadcast
rainmaker/nodes/groups/grp1/subgroups/sgX/control         ← subgroup sgX
rainmaker/nodes/groups/grp1/subgroups/sgY/control         ← subgroup sgY
```
