# Notifications

## What is the notification system

A unified dispatch layer that turns device-originated events (shadow updates and direct `notify/…` topics) into outbound messages on one or more delivery channels — mobile push, webhooks (Alexa, GVA, …), and automation triggers. Devices declare *what* happened and *which* channels should react; the dispatcher routes from there.

## Why it is needed

Devices should not know about APNS, GCM, OAuth bearer tokens, or which user has which app installed. They publish a shadow update or a direct notification on the `notify/…` topic; the cloud decides who needs to be told and how to talk to them.

This also lets us add new delivery channels (e.g. SMS, email, a new voice assistant) without touching device firmware or the dispatcher — a new channel is just a new entry in the service registry.

## Pre-requisites

- Node is registered and bound to a group/subgroup.
- User has access to that group/subgroup (see [group.md](group.md)).
- For user-specific channels: the user has registered with the relevant channel (see [notifications-push.md](notifications-push.md), [notifications-webhooks.md](notifications-webhooks.md)).

## Architecture

```
Device --(MQTT)--> IoT Core ----> Notifications Lambda
                                         |
                                         |  build dispatch event
                                         |  validate node-group
                                         |  resolve user list (group fanout)
                                         |
                                         v
                                    Service registry
                                  /     |       |       |       \
                               push   alexa   gva   automation   ...
                                |       |     |
                          SNS Publish   HTTP POST + OAuth
```

## Topic naming

Both the notify topic and the named shadow are built from the node's **membership** (group + all its subgroups, sorted, from `getGroupInfo`) and verified against it by step 3 of the [Dispatch flow](#dispatch-flow). Read each row left-to-right: membership decides the string, and the string decides the fanout.

| Node membership          | Notify topic (direct notification)              | Named shadow (shadow update)                                  | Fanout recipients                            |
| ------------------------ | ----------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------- |
| group + subgroup(s)      | `rainmaker/nodes/abc123/notify/g1abc-s01-s02`   | `$aws/things/abc123/shadow/name/params-g1abc-s01-s02/update` | group-level users + those subgroups' users   |
| group only (no subgroup) | `rainmaker/nodes/abc123/notify/g1abc`           | `$aws/things/abc123/shadow/name/params-g1abc/update`         | group-level users only as no subgroups                       |

Rules:
- The notification kind is decided by the source: the `notify/` topic segment marks a direct notification (the group string is the segment *after* `notify/`, with no prefix); the `params-` shadow-name prefix marks a shadow update.
- Group ID must be non-empty (it is the first hyphen-separated token of the group string).
- The publishing node must actually belong to the named group — enforced by the dispatcher (see [Dispatch flow](#dispatch-flow), step 3).

## Notification kinds

The dispatcher builds a channel-agnostic event from the incoming topic:

### Shadow update

Triggered by a delta on a `params-…` shadow.

Carries:
- node ID
- shadow name (full topic name)
- delta — diff between previous and current reported state
- current reported state

### Direct notification

Triggered by a publish on a `rainmaker/nodes/<nodeID>/notify/…` topic.

Carries:
- node ID
- the device-supplied notify payload

In both cases, group and subgroup IDs are populated from the topic/shadow name and travel with the event.

## Event payload

The dispatcher Lambda receives an event of the form:

```text
{
  "node_id": "abcd1234",
  "topic_name": "params-g1abc-s01",
  "notification_type": "shadow_update",
  "curr_state": { "params": { ... }, "data": { ... }, "online": true },
  "prev_state": { "params": { ... } },
  "notify": {
    "version": "1.0",
    "push":       { ... },
    "alexa":      { ... },
    "gva":        { ... },
    "automation": { ... }
  }
}
```

| Field                      | Meaning                                                                                                                                                                                                                                                                                                                                                                           |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `curr_state`, `prev_state` | Required for `shadow_update`. The delta is computed from the diff.                                                                                                                                                                                                                                                                                                                |
| `notification_type`        | `shadow_update`, `direct_notification`, or `group_membership_change`. Decides how the event is built. For direct notifications the input value is `direct_notification` and the value emitted in webhook bodies is `direct`. `group_membership_change` comes from the control plane when a node joins or leaves a group, carrying an action of `added` or `removed`, so voice assistants can re-discover or drop the device.                                                                                                                                |
| `notify.version`           | Reserved — skipped by the dispatcher.                                                                                                                                                                                                                                                                                                                                             |
| `notify`                   | Map of channel → channel-specific config. Each non-`version` key names a registered service.                                                                                                                                                                                                                                                                                      |

Unknown service keys are logged and skipped, never failed.

## Service registry

Each channel registered in the registry is one of two kinds:

- **Generic** — invoked once per dispatch. Used for channels that don't fan out to users (e.g. `automation`).
- **User-specific** — invoked once per user, after the dispatcher resolves the user list for the originating group/subgroup.

| Service        | Kind          | Notes                                                                                    |
| -------------- | ------------- | ---------------------------------------------------------------------------------------- |
| `alexa`        | user-specific | Alexa Smart Home ChangeReport. See [alexa.md](alexa.md).                                 |
| `automation`   | generic       | Internal automation triggers.                                                            |
| `gva`          | user-specific | Google HomeGraph Report State. See [gva.md](gva.md).                                     |
| `push`         | user-specific | Mobile push via SNS. See [notifications-push.md](notifications-push.md).                 |
| `node_reset`   | generic       | Server-side action, not a delivery channel: a node reporting a self factory-reset is disassociated and its data cleaned up. Valid only as a direct notification. |
| `webhook_mock` | generic       | Registered **only** when `webhook_mock_base_url` is set, which the integration-test fixture does to route Alexa/GVA traffic at an in-cloud mock. Absent in production. |

Each service supplies its own marshal step that converts the channel-agnostic event into a channel-specific payload (a push message, a ChangeReport, a HomeGraph request, …). See the per-channel specs for those shapes.

## Dispatch flow

The notifications Lambda runs once per incoming event:

1. **Build** the channel-agnostic event from the topic name and state. Reject events with unknown `notification_type` or missing required state.
2. **Parse** group and subgroup IDs from the topic/shadow name.
3. **Validate node-group alignment** — fetch the node's actual group/subgroup membership and confirm it matches what the topic name claims. If it does not, drop the event silently. This is the security gate against a device fabricating a topic that targets a group it does not belong to.
4. **Iterate** over the keys of `notify` (skipping `version`):
   - Look up the service in the registry. Unknown service → log and continue.
   - Run the channel's marshal step. Failure → log and continue.
   - **Generic** service → invoke once with the marshalled payload.
   - **User-specific** service → resolve the user list for the originating group/subgroup, then invoke the service per-user. Empty user list → log and continue.
5. Errors from any individual service are logged but do not abort the loop — one failing channel must not block the others.

### Group fanout semantics

The user list comes from the same access-control model as the rest of the platform (see [group.md](group.md)). Two classes of user exist in a group:
- **Group-level users** (primary/secondary owners, not scoped to a subgroup) are **always** in the recipient list, for every event in the group.
- **Subgroup-scoped users** are included **only** when the publishing node is in a subgroup they are scoped to (the node's subgroup set intersects theirs).

A node's recipient set is **fixed by its membership** — there is no per-node choice or fallback:
- Node in one or more subgroups → group-level owners **plus** the users of those subgroups.
- Node in no subgroup → group-level owners only.

## Per-channel specs

- [notifications-push.md](notifications-push.md) — mobile push (APNS, GCM) via SNS Mobile Push.
- [notifications-webhooks.md](notifications-webhooks.md) — outbound webhooks with OAuth (Alexa, GVA, custom).
- Automation is registered as a service for completeness, but its mechanics are an internal feature, not an outbound delivery channel.

## Endpoint identity per integration

Every delivery channel a user has registered is one *endpoint*. An endpoint is the smallest addressable delivery target — what `PUT /v1/integrations/{integrationId}/endpoints` creates and what the response's `endpoint_id` names. The natural identifier differs by integration type, and so does what "multi" means (i.e. when the same user can have more than one row for the same integration).

| Integration | What "multi" means                                                              | endpoint_id                                                                                                                                                                  |
| ----------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Alexa       | Multiple Amazon accounts the same user has linked (rare — most users link once) | Amazon user ID from LWA `/user/profile`                                                                                                                                      |
| APNS / GCM  | Multiple devices the same user installs the app on                              | SNS Endpoint ARN (per device token)                                                                                                                                          |
| GVA         | Multiple Google accounts the same user has linked (also rare)                   | The `integration_id` doubles as the `endpoint_id` today, which implicitly caps a user at one linked Google account. Lifting the cap needs the Google user id captured at link time. |
| Webhook     | Multiple webhook subscriptions per user                                         | The `integration_id` doubles as the `endpoint_id` today, so there is one row per user per webhook channel. User-supplied URLs would key on the URL or a generated subscription id instead. |

## Multiple integrations per type

The same question one layer up: can an admin register more than one *integration* of the same integration type? Differs by type, because the integration's identity and config store differ.

| Integration type    | Multiple per deployment?            | Why                                                                                                                                                                                                                                                                                                                            |
| ------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Alexa               | No — singleton                      | Config lives at fixed SSM paths (`/rmng/alexa/client_id`, `client_secret`, `skill_id`). `POST /v1/integrations/alexa/configuration` overwrites in place; there is no per-skill identifier in the integration_id (`alexa`, no suffix). Supporting two skills would require keying the SSM path and the integration_id by skill. |
| APNS / APNS_SANDBOX | Yes — one per `bundle_id`           | `integration_id = apns_<bundle_id>`; each maps to its own SNS Platform Application. Two iOS apps (different bundles) coexist naturally. Re-registering the same bundle_id replaces the credentials, it does not create a second integration.                                                                                   |
| GCM                 | Yes — one per Firebase `project_id` | `integration_id = gcm_<project_id>`; same model as APNS.                                                                                                                                                                                                                                                                       |
| GVA                 | No — singleton                      | Same shape: fixed SSM path (`/rmng/gva/service_account_json`), integration_id is the bare `gva`.                                                                                                                                                                                                                               |
| Webhook             | No — fixed set at deploy time       | Channels are compiled into the service registry; there is no admin API to add one (see [notifications-webhooks.md](notifications-webhooks.md)). A generic webhook-registration API would give each registered webhook its own integration_id and make this row "yes".                                                                                                                                                                                |

