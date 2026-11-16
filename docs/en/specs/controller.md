# RainMaker Neo Controller

## 1. Overview

A **RainMaker Neo Controller** is an ordinary RainMaker Neo node that can observe and control the *other* nodes in
the single group it belongs to — much like the phone app, but running on a device with no human
user and no user credentials.

Within its one group a controller can:

1. **Read the group's nodes and their config** over the existing user REST APIs.
2. **Read params** of any node in the group over MQTT (named-shadow get and observe).
3. **Set params** at node, subgroup or group level over MQTT, and listen to group events.

It is a node by registration: own certificate, own shadow, joins a group like any other node. Its
group-wide reach rests on two things — a dedicated IoT policy (`rmng-controller-policy`) scoped to
its group through the `group_id` Thing attribute, and the SigV4 credentials the IoT credential
provider issues for its certificate, which is what the REST path identifies it by.

There is **no user refresh token on the device**. The certificate is the only root of trust, and the
controller learns which group it belongs to from the MQTT group info every node receives rather than
from provisioning.

### Goals

- Reuse the existing user REST APIs and RBAC for reads. Only one additive endpoint is needed
  (§3.4), because the existing list endpoint is keyed on the caller's memberships.
- No user credential on the device. Revocation is deactivating the certificate.
- Per-controller, per-group isolation enforced server-side from authoritative state.

### Non-goals

- Cross-group control. A controller belongs to exactly one group, like any node.
- Group-level write or admin — create, delete, share, edit membership. Read and control only.

## 2. Background

### 2.1 Two planes

| Capability | Plane | Mechanism |
|---|---|---|
| Read nodes and config (#1) | REST | RBAC over group membership; identity from the certificate behind its credentials |
| Read params (#2) | MQTT | named-shadow get/observe on `params-<group_id>` |
| Set params and events (#3) | MQTT | unicast and group/subgroup control publishes |

The asymmetry that shapes the design: REST identity in RMNG comes from the OIDC/Cognito principal
(`rmneo/principal → FromAPIRequest`), not from the IAM or certificate principal. A
certificate-only device is therefore invisible to the data APIs as they stand — hence §3.3, which
teaches the checkpoint to recognise the certificate principal.

## 3. Design

### 3.1 Registration

Registered like any node, with the `controller` capability:

- Single node: `POST /v1/admin/nodes` with `"capabilities": ["controller"]`.
- Bulk: the same value on the registration job, applied to every node in the CSV — so controllers
  are registered in their own batch.
- Assisted claiming: `capabilities` on the claim-verify request, carried through to the same
  registration path. Re-claim after a factory erase must restate it, since the capability policies
  are attached to the replacement certificate (see `assisted-claiming.md` §3.6).

The controller's node-register hook (`src/controller/hooks`) attaches `rmng-controller-policy` for
the capability, through the same `nodelifecycle.CapabilityPolicy` helper bridge uses. If the
controller stack is not deployed the policy does not exist, and registration answers `400`
(capability unavailable) rather than failing opaquely. Core also records the registered capabilities on the `node_details` row as
`reg_capabilities`.

Note what core does **not** do: it never writes `node_type`. That field belongs to the node-register
lifecycle hook, which a separately-deployed optional stack owns, and core stores it without
interpreting it. Admitting the controller by its capability policy keeps that boundary intact — see
§7.1.

### 3.2 `rmng-controller-policy`

Attached alongside `rmng-base-node-policy`. Every MQTT resource is scoped to the controller's group
through `${iot:Connection.Thing.Attributes[group_id]}`, an attribute trusted backend code sets on
group join and leave and which the device has no way to assert for itself. Statements:

1. **Read params** — Subscribe/Receive/Publish on
   `*/$aws/things/*/shadow/name/params-${…group_id…}*/*`.
2. **Set params, node level** — Publish on
   `topic/rainmaker/nodes/*/user/params-${…group_id…}*/*`.
3. **Set params, group and subgroup** — Publish/Subscribe/Receive on
   `*/rainmaker/nodes/groups/${…group_id…}/control` and `.../subgroups/*/control`.
4. **Credential provider** — `iot:AssumeRoleWithCertificate` on the controller role alias.

This is the same full-group MQTT permission set `assume_role` already grants a user, expressed as a
static device policy. Source: `src/controller/controller_policy.json`.

Statement 1's Publish covers every shadow topic under those names, not only `…/get`, so a controller
can also write a group node's reported state (`…/update`) or delete its shadow (`…/delete`). An app
user's `assume_role` credentials carry the same grant, so narrowing it is a change to both, not to the
controller alone.

Policy variables resolve at connection time, so a controller that has just been added to a group
must reconnect before the new scope takes effect, and one that has been removed keeps its old scope
on the open connection until it reconnects.

### 3.3 Identity: the certificate behind the credentials

1. The controller obtains SigV4 credentials from the **AWS IoT Credential Provider** (certificate →
   controller role alias). Only a certificate carrying `rmng-controller-policy` may use that alias,
   and the register hook attaches it only for the `controller` capability.
2. It signs every data call with them. The credential provider names the role session after the
   **certificate ID** (the SHA-256 of the certificate), so API Gateway hands the Lambda
   `assumed-role/<role>/<certificate id>` as the caller's `UserArn`.
3. The checkpoint (§3.4) resolves that certificate ID to the thing the certificate is attached to
   (`iot:ListPrincipalThings`), which is the node id. A certificate never moves to another thing, so
   the lookup is cached for the container's life.
4. The controller learns its group over MQTT, from the `from_cloud` group info every node receives,
   which it needs anyway for the topics of §3.6.

There is no token and nothing to mint: the certificate is resolved on every request.

### 3.4 RBAC: one checkpoint

The data APIs are unchanged. `rmneo/principal.FromAPIRequest`, the single place every handler
builds its RBAC context, is where the caller's kind is decided: a request SigV4-signed by a
non-Cognito IAM principal yields a `DeviceActor` principal identified by the node its certificate is
attached to (§3.3); anything else is resolved as a user from the Cognito identity the gateway
attached. A non-Cognito signer whose session is not named after an attached certificate is refused
(`401`), never resolved as a user. Package `user` knows only about users.

The device's group is **not** fixed before the request. The checkpoint reads the node's current row in
the group-node mapping and grants `group:get` and `group:listsubentities` on that group and nothing
else. A node in no group gets no context (`401`). A node moved between groups therefore follows the
move on its very next request, with the same credentials. The MQTT plane follows on the next
connection, when the policy re-reads the `group_id` Thing attribute (§3.2). It costs one query per
request, the same one the group listing used to make.

Below the checkpoint, `group.ListGroupForUser` resolves "which groups can the caller see" for either
principal — a user from their user-group-assoc rows, a device from its node's membership — and is
what every group-scoped handler calls. The code behind `GET /v1/groups` would serve a controller
too, returning its one group, but the edge never lets one reach it: the role's `execute-api` grant
is `GET /v1/groups/*`, which in an execute-api ARN requires a segment after `groups/`, so API
Gateway refuses the bare list route (§3.5). `GET /v1/groups/{groupId}` is therefore the one
additive endpoint and the controller's only way to read its group: the group-keyed form a caller
who already knows its group id, from its MQTT group info, uses. A caller with no access and a
group that does not exist both answer `404`, so the response never reveals whether a group exists.

### 3.5 What makes the REST surface read-only

Worth being precise, because it is not the RBAC grant. Once any call lists a group's nodes, the
group-membership flow grants node-level access per node — today that grant is `node:*`, the same as
for a shared secondary user. What confines the controller to reads is the **edge**: its role's
`execute-api` permission allows only `GET` on `/v1/groups/*`, so a write method is refused by API
Gateway before a Lambda ever runs. Only the controller role holds that grant, and only a
certificate carrying `rmng-controller-policy` can assume it.

Within `GET /v1/groups/*`, the controller reads its group (`/v1/groups/{groupId}`) and, for each node
of the group, `config`, `schedules`, `triggers`, `timeseries/*` and `tags`. It cannot read the group's
automations or its users: the controller grant has no `group:getautomation` or
`group:listprimaryusers`. The bare `GET /v1/groups` is refused at the edge (§3.4).

### 3.6 MQTT topics (group `g`, node `n`, subgroup `sg`)

| Operation | Direction | Topic |
|---|---|---|
| Get params | Pub `…/get`, Sub `…/get/accepted`, `…/update/documents`, `…/update/delta` | `$aws/things/n/shadow/name/params-g[-sg…]/…` |
| Set params (node) | Publish | `rainmaker/nodes/n/user/params-g[-sg…]/params` |
| Set params (group) | Publish | `rainmaker/nodes/groups/g/control` |
| Set params (subgroup) | Publish | `rainmaker/nodes/groups/g/subgroups/sg/control` |
| Group events | Subscribe | `rainmaker/nodes/groups/g/control`, `rainmaker/nodes/groups/g/subgroups/sg/control` |

### 3.7 Refresh

The certificate is the root of trust; nothing long-lived is stored. The only short-lived layer is the
**SigV4 credentials** from the credential provider — re-fetch them from the credentials endpoint on
expiry. If `group_id` changes, the controller learns it from MQTT group info and re-subscribes to the
group topics; REST access already follows the new group on the next request (§3.4).

### 3.8 Bootstrap inputs

The device already has the MQTT endpoint and the credential-provider endpoint, provisioned as for
any node. The only new value is the **REST API base URL**, which is not derivable from the IoT
endpoint. It is written into NVS as `rmaker_creds/api_base_url` by the dashboard's Generate flow
when the operator opts in, or set via firmware menuconfig. `group_id` is **not** a bootstrap input —
it comes from the MQTT group info every node receives.

## 4. Resources

The controller is its own optional stack group, `controller`, deployed by `cdk/apps/controller.py`
and skipped by `make deploy-rmng`. Core references none of its stacks.

In `rmng-controller-base`: the controller IAM role (trusting `credentials.iot.amazonaws.com`) and
role alias `rmng-controller-role-v1`, with `execute-api:Invoke` on `GET /v1/groups/*`; and the
`rmng-controller-policy` IoT policy. The controller has no compute of its own.

In core, and generic: every data Lambda may call `iot:ListPrincipalThings` on certificates, the
lookup §3.3 needs. Registration needs no IAM change: the register hook runs in-process inside the
registration and claim Lambdas, whose shared node-register policy already grants
`iot:AttachPolicy` on `*`.

## 5. Firmware

`examples/esp-idf/rmng_controller` in rmng-sdk, plus the reusable `components/rmng-aws-sign`
component (credential provider, SigV4 signer, signed-request helper) shared with file storage and
KVS. The controller uses it with `service="execute-api"`.

## 6. Security analysis

### 6.1 Per-controller, per-group isolation
Each controller has its own certificate and its own `group_id` Thing attribute, and the single
policy self-scopes per connection. Group ids are globally unique, so `params-<group_id>` namespaces
never collide across tenants.

### 6.2 Membership is not forgeable
`group_id` is set only by trusted backend code on join and leave. The device has no MQTT operation
to change its own Thing attribute, and the certificate policy grants no IoT control-plane API.

### 6.3 Identity is certificate-bound
The node id comes from the certificate the credential provider named the session after, resolved by
AWS IoT, so a device cannot assert another node's id. App users' Identity Pool credentials carry a
Cognito identity and are always resolved as users (§3.4). The one way around it is an IAM principal
that can assume a role with `execute-api` rights and pick its own session name; that takes
account-admin IAM rights.

### 6.4 Capability, not classification
Who may act as a controller is decided by the certificate's policy: only a certificate carrying
`rmng-controller-policy`, attached for the `controller` capability, can assume the controller role.
Nothing consults `node_type`, which an optional stack owns and core does not interpret.

### 6.5 No user credentials on the device
The device holds only its X.509 key, already required for MQTT. Revoking the certificate stops the
credential provider issuing SigV4 credentials; those already issued expire within their lifetime.

### 6.6 Cross-subgroup access
Like a full-group user, a controller reaches every subgroup of its group. Subgroup-internal
isolation is a firmware concern, consistent with the rest of the device plane.

## 7. Extending this

### 7.1 A second controller kind

A different kind of controller — a Matter controller, say — layers on this baseline rather than
replacing it. Three seams matter:

- **Classification.** Because core never writes `node_type`, an optional stack's node-register hook
  attaches its own policy and may classify the node however it likes. Hooks accumulate and all of
  them run, so two capability modules do not contend; `node_type` carries one value, so the first
  non-empty classification wins. This controller registers a hook (`src/controller/hooks`) and
  deliberately classifies nothing, since its capability policy is what admits it.
- **The capability gate.** A second kind adds its own capability, policy and role alias rather than
  a second `node_type` literal.
- **The checkpoint.** Core resolves any credential-provider session to its node and grants
  identically whatever the node's kind, so adding a kind is additive and cannot widen access: scope
  is a property of live membership.

The IoT policies are orthogonal and additive, so a node holding two controller capabilities is not a
policy conflict.

### 7.2 Controller-initiated membership changes

The controller is read and control only (§1). Nothing in the identity model prevents more:
membership editing is an additive permission (`group:editnodes`) on the controller principal plus
the corresponding route. The certificate-bound identity and per-group isolation already hold.

### 7.3 When the membership lookup should become lazy

The checkpoint resolves a device's group eagerly, on every request, before the handler knows
whether it needs it (§3.4). Today that costs nothing extra: the controller's `execute-api` policy
admits only the group routes, every one of which calls `group.ListGroupForUser`, and that call
performed the same single-item read in the earlier design. The read moved earlier; the count per
request is unchanged. It is one GSI lookup on the group-node mapping by node id, small next to the
Lambda invocation around it, and doing it at the door is what yields a clean `401` for a device that
belongs to no group. The certificate-to-thing lookup (§3.3) runs once per certificate per Lambda
container, so IoT sees it only on cold starts.

The trade-off changes if a device is ever granted a route that does not involve groups, since the
eager read would then be wasted there. The fix at that point is lazy, memoised resolution: the
`DeviceActor` holds a resolver rather than a stored group id, `ScopedGroupID` runs the read on first
use and caches it for the rest of the request. The cost is a less tidy accessor that must surface or
swallow an error, and a node in no group is refused with `403` at first group access instead of
`401` at the door. Two alternatives were considered and rejected: carrying the group in a cached
claim reintroduces the staleness §3.4 removed, and a warm-container cache keyed by node id does the
same across requests for the sake of a read that is already cheap.

### 7.4 Unifying credential vending

RMNG vends caller credentials through three paths that differ on two axes — the authenticating
principal, and what is vended:

| Path | Principal | Transport | Vends |
|---|---|---|---|
| `POST /v1/assumed_roles` | user (JWT → SigV4) | API Gateway | scoped AWS credentials |
| device-file / KVS credentials | device X.509 | IoT Credential Provider (mTLS) | AWS credentials |
| controller data calls | device certificate (→ credential provider → SigV4) | API Gateway | nothing; the certificate is resolved per request |

The checkpoint (§3.4) recognises the certificate principal directly: the credential provider names
the role session after the certificate ID, which identifies the device without any vended token.

## 8. Verification

`test/itest/test_controller.py` covers:

- registration attaching the policy, and a plain node not getting it;
- REST, signed with credential-provider credentials: reads of the group and of a group node's
  config, a write refused at the edge, and another group refused;
- MQTT: a node's shadow read, unicast, group and subgroup control, receiving group broadcasts, and
  every policy statement refusing another group;
- removal from the group: REST refused on the next request, MQTT kept on the open connection and
  refused after reconnecting, with no wider shadow access once `group_id` is cleared;
- a node that is both a bridge and a controller.

`test/itest/test_assisted_claiming.py` covers a re-claim adding and removing the controller policy.
The controller tests skip themselves when `rmng-controller-base` reports no `ControllerRoleAliases`
output.
