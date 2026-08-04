# Limits

**S**oft limits are adjustable through AWS Support; **h**ard ones are not. Several
defaults are lower in some regions — check the
[AWS IoT Core quotas](https://docs.aws.amazon.com/general/latest/gr/iot-core.html)
page for the deployment's region before designing against a number.

## AWS limits


| Limit                                      | Value      | S/H   | Notes                                                                                                             |
| ------------------------------------------ | ---------- | ----- | ----------------------------------------------------------------------------------------------------------------- |
| Concurrent connections per account         | 500,000    | S     |                                                                                                                   |
| Subscriptions per connection               | 50         | S     | Binds an app holding many, not a node                                                                             |
| Topic subscriptions per SUBSCRIBE          | 8          | H     | A node with many subgroups splits its subscribe                                                                   |
| MQTT payload                               | 128 KB     | H     | One params publish, node config, or timeseries batch                                                              |
| Shadow document                            | 8 KB       | H     | Whole reported state; grows with device/param count                                                               |
| Shadow JSON depth                          | 8          | H     | `state.reported.params.<device>.<param>` is 5                                                                     |
| Shadow name                                | 64 B       | H     | `params-<group>[-<sub>…]` is ≤25 B — why ids are short                                                            |
| Topic slashes                              | 7          | H     | `.../groups/<g>/subgroups/<s>/control` uses 6. Basic Ingest's `$aws/rules/<rule>/` prefix is exempt               |
| Thing name                                 | 128 B      | H     | Node IDs are 36-char UUIDs                                                                                        |
| Indexable thing attributes (no thing type) | **3**      | H     | Things are created without a thing type, so 3 applies — not 50. `group_id` uses one                               |
| Thing groups per thing                     | 10         | H     | Admin groups only                                                                                                 |
| Thing group hierarchy depth                | 7          | H     |                                                                                                                   |
| Direct child groups per thing group        | 100        | H     |                                                                                                                   |
| Dynamic thing groups per account           | 100        | S     |                                                                                                                   |
| QoS 1 retry duration                       | 1 h        | H     | How long a `getGroupInfo` push to an offline node stays queued; past it the node recovers via its startup request |
| Unacked outbound publishes per client      | 100        | H     |                                                                                                                   |
| In-flight shadow messages per thing        | 10         | H     |                                                                                                                   |
| Rules per account / actions per rule       | 1000 / 10  | S / H | This deployment defines 5 rules, 1–2 actions each                                                                 |
| Inline session policy (STS `AssumeRole`)   | 2048 chars | H     | ≈4 groups or 8 shared subgroups per user — see below                                                              |




## ESP RainMaker Neo limits


| Limit                                | Value     | Notes                                                                                                                                                                                                                                      |
| ------------------------------------ | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Main groups per node                 | **1**     | A second association moves the node: old group unlinked, shadow, `group_id` attribute and user tags migrated. See [node_assoc.md](node_assoc.md)                                                                                           |
| Subgroups per node                   | **3**     | `subgrp1`/`subgrp2`/`subgrp3` slots; a fourth add is rejected with `"node is already in 3 subgroups"`                                                                                                                                      |
| Subgroups per group, nodes per group | unbounded | Only subgroups shared *to* a user cost session-policy budget                                                                                                                                                                               |
| Full-access groups per MQTT session  | 4         | Consequence of the 2048-char session policy; exceeding it fails credential issuance outright. See [user_auth.md](user_auth.md) §3.3, [group.md](group.md#capacity-limits)                                                                  |
| Shared subgroups per MQTT session    | ~8        | Same budget, cheaper per entry                                                                                                                                                                                                             |
| Sharing request validity             | 24 h      | From creation                                                                                                                                                                                                                              |
| Association request validity         | 5 min     | DynamoDB TTL *and* a deadline the handlers enforce on read, since TTL deletion is only guaranteed within ~48 h. Refreshed on each status update, so the Matter verify → confirm leg gets a fresh 5 min. See [node_assoc.md](node_assoc.md) |
| Node IDs per claimant                | 20        | Lifetime cap, operator-configurable; reservations are never deleted, so a removed node does not return its slot. See [assisted-claiming.md](assisted-claiming.md)                                                                          |

