# User Management, Authentication & Credentials

## 1. Overview

This document describes the design for user identity, authentication, and
credential issuance on the ESP RainMaker Neo platform — the layer every other feature sits
on top of. Its job is to answer three questions for each request:

1. **Who is calling?** An authenticated principal — an **end user** proven by an
   ESP User OIDC access token, or an **admin** proven by a Cognito ID token —
   resolved to a stable ESP RainMaker Neo `user_id`.
2. **What temporary AWS credentials may they hold?** Short-lived credentials
   minted through a Cognito Identity Pool, and — for device messaging and
   per-node services — further scoped credentials minted through STS
   `AssumeRole` with an inline session policy.
3. **What are they allowed to do?** Determined by RBAC permissions carried on
   the request context and enforced in the DB layer.

The design has two distinct credential planes that must not be conflated:

- **The control plane** — HTTPS calls to the ESP RainMaker Neo REST API (API Gateway).
  These are authorized either by a login token on the credential-vending
  endpoint — an **ESP User OIDC access token** for end users, a **Cognito ID
  token** for admins — or by **AWS SigV4** signatures made with Identity-Pool
  credentials (everything else).
- **The data plane** — the MQTT connection to AWS IoT Core, plus per-node
  services (S3 file storage, KVS video). These require **STS-minted
  credentials** whose inline session policy narrows a broad IoT role down to
  exactly the topics / resources the caller is entitled to.

This spec is the design-level companion to the user-facing
`docs/01-user-management.md`. It complements the node-side companions
(`node_reg.md`, `node_connection.md`, `node_assoc.md`) and cross-references
`notifications-push.md` for the push-endpoint registration surface.

### Key design decisions

1. **One admin Cognito pool + one end-user OIDC issuer, one Identity Pool.**
   End users authenticate against the ESP User OIDC issuer (passwordless);
   admins authenticate against a Cognito admin user pool. A single Identity
   Pool federates both — the OIDC issuer as a registered IAM OIDC identity
   provider, the admin pool as a Cognito provider — and, via a role-mapping
   rule on the `custom:super_admin` claim, hands each identity a different IAM
   role at credential-exchange time. Admin privilege is therefore an
   *infrastructure* fact (which role you can assume), not only an application
   flag.
2. **Credential exchange is explicit, not implicit.** A client does not sign
   API calls with its login tokens. It first exchanges them — an access token
   plus its matching id token, OIDC for end users or Cognito for admins — for
   Identity-Pool credentials at one gated endpoint, then uses those credentials
   to SigV4-sign every other call. This keeps the login token off the hot path and lets API
   Gateway authorize the bulk of the surface with `AWS_IAM`.
3. **The broad IoT role is never handed out directly.** `AssumeRole` always
   attaches an inline **session policy** that intersects with the role's own
   policy. The role grants `iot:Connect/Publish/Subscribe/Receive` on `*`; the
   session policy restricts each issued credential to the topics of the groups
   / subgroups (MQTT mode) or the S3/KVS resources of a single node (services
   mode) that the caller is entitled to.
4. **Identity resolution is centralized.** Every Lambda handler builds its
   request context through one shared routine, which extracts the Cognito
   username from the request, resolves it to a user identity, and seeds the
   accessor used for all downstream RBAC checks.
5. **RBAC is enforced in the DB layer, not the handler.** Handlers construct
   the context and call DB methods; the DB methods decide access.

---

## 2. Identity model

### 2.1 Identity providers: admin Cognito pool + end-user OIDC issuer

ESP RainMaker Neo authenticates two populations through two different identity providers,
both federated by one Identity Pool (§2.2):

- **End users** authenticate against the **ESP User OIDC issuer**
  (passwordless). A successful login yields OIDC tokens; ESP RainMaker Neo consumes the
  **access token** for credential vending. Token issuance, the passwordless
  OTP flow, `sub` derivation, and JWKS publication are owned by the ESP User
  provider — see
  `src/espuser/docs/specs/auth-flows.md`
  and
  `src/espuser/docs/specs/oidc-oauth2.md`.
  ESP RainMaker Neo only *consumes* that access token to vend AWS credentials and resolve
  the caller.
- **Admins** authenticate against a Cognito **admin user pool**, whose users
  may carry a custom Cognito attribute `custom:super_admin`. Authentication
  (sign-in, MFA, token refresh) is handled by Cognito itself; ESP RainMaker Neo consumes
  the admin **ID token**.

End-user endpoints use **no** API Gateway authorizer (type `NONE`); the handler
verifies the bearer token itself against the appropriate JWKS. This is uniform
across the end-user surface:

- **`POST /v1/user/credentials`** — the handler branches on the access token's
  `iss`: ESP User OIDC tokens are verified against the ESP User issuer's JWKS,
  admin Cognito tokens against the admin pool's JWKS. The caller supplies an
  access token *and* the matching id token (§3.1). One endpoint accepts both
  token families, which a `COGNITO_USER_POOLS` authorizer could not do.
- **`GET /v1/users/{userId}`** — the handler verifies the OIDC access token
  against the ESP User JWKS before returning the profile.

### 2.2 The Identity Pool

A single Cognito **Identity Pool** (unauthenticated identities disabled)
federates both providers: the **ESP User OIDC issuer**, registered as an IAM
OIDC identity provider (keyed by the issuer URL, its tokens verified against
the issuer's JWKS), for end users; and the **admin pool's client**, a Cognito
provider, for admins. Exchanging a valid login token at this pool produces
**temporary AWS credentials** (access key, secret key, session token).

For end users, the Identity Pool selects the OIDC provider by matching the
federated **id token's** `iss` (the ESP User issuer) against the registered
provider and its **`aud`** against the provider's configured client id(s); a
token from any other issuer, or with a non-matching audience, is not honored
(RFC 8707 audience scoping governs which client ids are accepted). This works
because the ESP User id token is minted with `aud` = the client id (the
same audience the OIDC provider is configured with); federation depends on that,
so a future change to the token's audience must keep it in the provider's
client-id list. The end-user
role trust targets the **Identity Pool** — the STS `AssumeRoleWithWebIdentity`
principal is `cognito-identity.amazonaws.com`, conditioned on the identity-pool
id — **not** the login provider, so the role's trust depends only on the
Identity Pool.

The pool decides *which IAM role* those credentials belong to via a
**role-mapping rule** keyed on the admin pool's `custom:super_admin` claim:

- Any authenticated identity defaults to **`DeviceUsersRole`**
  (`rmng-cognito-identity-role-<region>`). **Every federated end user maps
  here unconditionally** — there is no per-user or claim-driven role branching
  on the OIDC-federated end-user path.
- An identity from the **admin pool** whose token carries
  `custom:super_admin == "true"` is mapped to **`AdminDeviceUsersRole`**
  (`rmng-admin-cognito-identity-role-<region>`) instead. This `super_admin`
  role-mapping is **admin-only**: end users never reach it, because they do not
  federate through the admin surface.
- If the rule does not match, the identity falls back to the default
  authenticated role.

So **super-admin vs. regular-user is decided at the Identity-Pool
role-mapping layer**, before any ESP RainMaker Neo Lambda runs. The application-level
super-admin check (§5.3) is a second, in-code gate over the same fact,
surfaced through the admin's Cognito claims.

### 2.3 The three IAM roles

| Role | Trust | What it grants |
|---|---|---|
| **`DeviceUsersRole`** (end users, OIDC-federated) | `AssumeRoleWithWebIdentity` from `cognito-identity.amazonaws.com`, scoped to this pool + `amr=authenticated` (trust targets the Identity Pool, not the OIDC login provider) | `cognito-identity:GetCredentialsForIdentity`, `sts:TagSession` on the IoT role, `execute-api:Invoke` on the API. **Explicitly denies `sts:AssumeRole`.** |
| **`AdminDeviceUsersRole`** (super-admins) | Same federated trust | Everything `DeviceUsersRole` has, **plus** IoT management (`iot:ListThings`, `DescribeThing`, `SearchIndex`, thing-group and job/stream management) and S3 access for firmware upload / node-registration CSV download. Also denies `sts:AssumeRole`. |
| **`IoTUserRole`** (`<node-role>-<region>`) | `sts:AssumeRole` from the `iot.amazonaws.com` service principal, **and** (added during assume-role setup, §4.2) from the assume-role Lambda's role | Broad `iot:Connect/Publish/Receive/Subscribe` on `*`, `cognito-identity:GetCredentialsForIdentity`, and S3. This is the role the assume-role Lambda vends **through a narrowing session policy** — never directly. |

The two Identity-Pool roles both `execute-api:Invoke` (so their SigV4
credentials can call the API) and both **deny `sts:AssumeRole`** — a client
cannot escalate by assuming `IoTUserRole` itself. Only the assume-role Lambda,
whose role is added to `IoTUserRole`'s trust policy, can assume it, and it
always does so with a scoped session policy.

---

## 3. Authentication → credentials flow

The end-to-end path from login to an MQTT connection:

```
(1) Login                End user ⇄ ESP User OIDC issuer      → OIDC access token (JWT)
                         Admin    ⇄ Cognito admin user pool    → Cognito ID token (JWT)
(2) Credential exchange  POST /v1/user/credentials           → Identity-Pool creds
     (handler verifies bearer by iss:                          (DeviceUsers or AdminDeviceUsers role)
      OIDC end-user RS256/JWKS or admin Cognito)
(3) API calls            SigV4-signed with (2)'s creds        → AWS_IAM-authorized endpoints
(4) Data-plane creds     POST /v1/…/assumed-roles             → IoTUserRole creds + inline session policy
     (SigV4-signed with (2)'s creds)                            (MQTT scope, or per-node S3/KVS scope)
(5) MQTT / services      Connect to IoT Core / S3 / KVS       using (4)'s scoped creds
```

Two facts make this coherent:

- Step (2) is the **only** control-plane call authorized by login tokens — an
  end user's OIDC pair or an admin's Cognito pair. Every other
  endpoint (`assumed-roles`, `integrations/.../endpoints`, the node/group APIs)
  is created with API Gateway's default `AWS_IAM` authorization, so callers
  SigV4-sign them with the step-(2) credentials.
- The step-(4) credentials are what actually connect to the MQTT broker and
  reach per-node services. The Identity-Pool credentials from step (2) alone
  do not carry a usable IoT scope — see `node_connection.md` for the data-plane
  lifecycle those credentials feed into.

> Note on route names: informal shorthand elsewhere calls these "`/user/creds`"
> and "`/assume_role`". The verified routes are **`POST /v1/user/credentials`**
> and **`POST /v1/…/assumed-roles`** (§4).

### 3.1 Step 2 — credential exchange (`/v1/user/credentials`)

The endpoint is `POST /v1/user/credentials`, the one credential-vending
endpoint. It carries **no** gateway authorizer (the method is declared with
`authorization_type="NONE"`); the handler verifies the presented tokens itself.

**Two tokens are required, arriving by different routes:**

| Token | Where | Why |
|---|---|---|
| **access token** | `Authorization` header | Obtaining AWS credentials is an authorization action, so the caller presents an RFC 6749 / RFC 6750 bearer credential. |
| **id token** | `id_token` in the JSON body | The Identity-Pool exchange (`GetId` / `GetCredentialsForIdentity`) accepts an OIDC **id** token and nothing else. |

The handler verifies the **pair** — the
access token establishes who is calling, and the id token must belong to that
same authenticated session before it is federated.

Two token families are admitted, distinguished by the access token's `iss`
claim:

- **End users** present ESP User OIDC tokens, verified against the ESP User
  issuer's published JWKS — RS256 only, `alg: none` rejected (RFC 8725),
  `iss` = the ESP User issuer, unexpired. See
  `src/espuser/docs/specs/auth-flows.md` and
  `src/espuser/docs/specs/oidc-oauth2.md`.
- **Admins** present Cognito tokens, validated against the admin pool's JWKS
  when the token's `iss` is that pool.

One endpoint accepts both token families because a `COGNITO_USER_POOLS`
authorizer cannot verify the OIDC issuer's RS256 tokens; in-handler verification
routed by `iss` covers both without a gateway authorizer.

The handler:

1. Reads `IDENTITY_POOL_ID` from the environment (`500` if unset).
2. Extracts the bearer access token from the `Authorization` header; missing → `401`.
3. Parses the body for `id_token`; missing or unparseable → `400`.
4. Verifies the pair against the issuer-appropriate JWKS (OIDC or admin):
   - unreadable or untrusted `iss` → `401` (the caller is not authenticated);
   - both tokens verify but do not belong to the same session → `403` (the
     caller is authenticated but not entitled to this exchange).
5. Exchanges the **id token** for Identity-Pool credentials; a failure of the
   exchange itself → `500`.

Credential exchange against the Identity Pool is a two-step sequence:

- The token's `iss` claim is parsed to derive the login key: for admins the
  Cognito user pool (`cognito-idp.<region>.amazonaws.com/<userPoolID>`, taken
  *from the token*, not configuration); for end users the ESP User OIDC issuer,
  matched to the registered OIDC provider on `iss` + `aud` (§2.2).
- **`GetId`** exchanges `{loginKey: token}` for an Identity-Pool `IdentityId`.
- **`GetCredentialsForIdentity`** exchanges the identity + login for temporary
  credentials.

The role mapping (§2.2) is applied by Cognito inside
`GetCredentialsForIdentity`, so the returned credentials already belong to the
correct role — `DeviceUsersRole` unconditionally for every federated end user,
`AdminDeviceUsersRole` only for a `super_admin` admin identity. The handler
returns:

```json
{ "access_key_id": "...", "secret_access_key": "...",
  "session_token": "...", "expiration": 1720000000 }
```

The credential-exchange Lambda's own role holds only
`cognito-identity:GetId` and `cognito-identity:GetCredentialsForIdentity`.

### 3.2 Step 4 — assume-role (`/v1/…/assumed-roles`)

This single Lambda serves four routes and operates in **two modes**,
selected by whether a `nodeId` path parameter is present:

| Route | Mode | Who | Session policy |
|---|---|---|---|
| `POST /v1/assumed-roles` | MQTT | any authenticated user | MQTT topics for **all groups/subgroups the caller can access** |
| `POST /v1/groups/{groupId}/assumed-roles` | MQTT | super-admin only | MQTT topics scoped to that **one group** |
| `POST /v1/groups/{groupId}/subgroups/{subGroupId}/assumed-roles` | MQTT | super-admin only | MQTT topics scoped to that **one subgroup** |
| `POST /v1/groups/{groupId}/nodes/{nodeId}/assumed-roles` | **services** | user (or admin) with access to the node | **S3/KVS** for that **one node** — no IoT statements |

Common path (both modes):

1. Parse the body (`{ "tags": {...}, "services": [...] }`). Services mode is
   selected when a `nodeId` path parameter is present.
2. Validate the request: in MQTT mode, `services` must be **absent**; in
   services mode, `groupId` + `nodeId` are required and `services` must be a
   non-empty subset of `{"s3","kvs"}` (any other value → 400).
3. Build the request context from the request.
4. `GetCallerIdentity` to obtain the account ID used to template resource ARNs.
5. Build the mode-specific session policy (§3.3 / §3.4).
6. `sts:AssumeRole` on the IoT user role with `RoleSessionName="AssumeIoTRoleSession"`,
   the request's `tags` as session tags, and the built policy as the inline
   `Policy`.
7. Return `{ access_key, secret_key, session_token, expiration }`.

The Lambda role holds `sts:AssumeRole` on `IoTUserRole`, `sts:TagSession`,
`iam:GetRole`, and read access to the group / user-group / user-details /
group-device-mapping tables needed for authorization.

### 3.3 MQTT-mode session policy

The MQTT-mode session policy composes a document whose base allows
`cognito-identity:GetCredentialsForIdentity` on the caller's own identity and
`iot:Connect` **scoped to the caller's own client ids**, then **appends topic
ARNs per group and per subgroup** the caller may access.

`iot:Connect` is granted on `client/user:<email>:*` and
`client/user:<phone>:*` — the caller's own identifiers only, taken from the
request context, never from the body. The bare `user:<email|phone>` form is
deliberately **not** granted: every client must append a per-session suffix, so
one user's sessions (phone, dashboard) never collide and no client can claim the
canonical bare id. Because `:` cannot appear in an email address or an E.164
phone number, the trailing wildcard only ever matches the caller's own sessions.
A caller whose profile carries **neither** an email nor a phone number gets an
empty resource list and therefore **no `iot:Connect` grant at all** — the
credentials are issued but cannot connect to the broker.

Topic ARNs are appended as follows:

- **Full-group access** contributes, for each group: a shadow-filter resource
  (`.../shadow/name/params-<group>*/*`), a unicast params publish resource
  (`.../rainmaker/nodes/*/user/params-<group>*/*`), a group-control topic, and
  a subgroup-control wildcard. Because a full-group grant already covers all
  its subgroups, per-subgroup ARNs are skipped for those groups.
- **Subgroup-only access** contributes, for each shared subgroup of a group the
  caller does *not* fully own, a shadow-filter and a subgroup-control topic
  ARN.

The set of groups/subgroups the caller may access is resolved as follows:

- **No `groupId` (the `/v1/assumed-roles` route):** the caller's full access
  map is returned. This is the normal per-user MQTT-credential path.
- **With `groupId` (admin routes):** the caller must be a super-admin (else
  403). The group (and subgroup, if given) is validated to exist, and the
  access map is set to exactly that group or subgroup. This lets an admin mint
  credentials scoped to a specific group/subgroup without belonging to it.

The resulting credentials, used to connect to IoT Core, can publish/subscribe
only within the caller's entitled topic space — the enforcement point for the
group-control and shadow flows described in `group-control-feature.md` and
`node_connection.md`.

**Known limitation — accessible-group ceiling.** STS caps the inline
`AssumeRole` `Policy` parameter at **2048 characters**, and the MQTT document
grows by roughly 343 characters per full-access group on about 503 characters of
fixed overhead. That puts the ceiling at **4 full-access groups per session**.
A caller above the ceiling is rejected with **HTTP 500** and
`"Unable to issue credentials: too many accessible groups to encode in a
session policy"` — the handler measures the document and fails fast rather than
letting STS return a `ValidationError` that is indistinguishable from a
transient fault in the logs. Such a caller cannot obtain MQTT credentials at
all. See also `limits.md`.

### 3.4 Services-mode session policy (per-node S3/KVS)

The services-mode session policy builds a document with **no IoT statements at
all** — only the requested per-node services, scoped to the single
`nodeId`:

- **`s3`** (only if a files bucket is configured): `s3:ListBucket` on the
  bucket conditioned to prefix `node-data/<nodeId>/*`, plus `s3:GetObject` /
  `s3:DeleteObject` on `node-data/<nodeId>/*`. See `s3-device-file-storage.md`.
- **`kvs`**: `kinesisvideo:ConnectAsViewer`,
  `GetSignalingChannelEndpoint`, `DescribeSignalingChannel`,
  `GetIceServerConfig` on channel `rmng-v1-<nodeId>/*`. See
  `kvs-camera-streaming.md`.

Authorization is done up-front, using O(1) lookups:

1. A `GetItem` on `group_device_mapping` confirms the node actually belongs to
   the group (404-equivalent → 403 if not).
2. **Super-admins** are allowed for any node in a real group.
3. Otherwise, the caller's `user_group_mapping` row for the group is fetched:
   full-group access passes; subgroup access passes only if the node's
   subgroup tag is one of the caller's shared subgroups.

This keeps camera/file credentials tightly bound to a node the caller can
already see, and deliberately withholds any MQTT scope from a services-mode
credential.

---

## 4. API surface & wiring

### 4.1 Routes and authorization

| Method | Route | Auth |
|---|---|---|
| `POST` | `/v1/user/credentials` | `NONE` — handler verifies the access token (header) and `id_token` (body) as a pair, branching on `iss`: ESP User OIDC (RS256/JWKS) or admin Cognito (admin-pool JWKS) |
| `GET` | `/v1/users/{userId}` | `NONE` — handler verifies the OIDC access token against the ESP User JWKS |
| `POST` | `/v1/assumed-roles` | `AWS_IAM` (SigV4) |
| `POST` | `/v1/groups/{groupId}/assumed-roles` | `AWS_IAM` |
| `POST` | `/v1/groups/{groupId}/subgroups/{subGroupId}/assumed-roles` | `AWS_IAM` |
| `POST` | `/v1/groups/{groupId}/nodes/{nodeId}/assumed-roles` | `AWS_IAM` |
| `PUT` | `/v1/integrations/{integrationId}/endpoints` | `AWS_IAM` |
| `DELETE` | `/v1/integrations/{integrationId}/endpoints/{endpointId}` | `AWS_IAM` |

The API-Gateway resources for `/v1`, `/groups/{groupId}`, `/nodes/{nodeId}`,
`/subgroups/{subGroupId}` are shared across the API definition, so the
assume-role endpoints reuse the same nodes/groups resource tree as the node
and group APIs rather than duplicating it.

### 4.2 Trust-policy bootstrap

`IoTUserRole` is created by the base infrastructure trusting only
`iot.amazonaws.com`. The assume-role setup extends that trust — via a custom
resource calling `iam:UpdateAssumeRolePolicy` — to also allow the assume-role
Lambda's own role to `sts:AssumeRole` it. Without this step the Lambda could
not mint the scoped IoT credentials at all.

---

## 5. Request context & RBAC bootstrap

### 5.1 Building the request context

Every API-Gateway Lambda that acts on behalf of a user builds its request
context from the incoming API-Gateway request:

1. The caller is recovered from the request's authentication-provider field.
   For an admin, this splits on the `CognitoSignIn:` marker to recover the
   Cognito sub. For an end user, the Identity Pool's federated identity is
   `<issuer>:<sub>`, and the meaningful key is the OIDC **`sub`** — the opaque
   `user_id` the ESP User provider assigns — so caller resolution takes the
   segment after the last `:`. Because the `sub` is stable per identity, the
   same user resolves to the same caller across logins. There is **no
   fallback**: a missing or malformed provider string yields no identity and the
   request fails as unauthorized — it does not degrade to a placeholder caller.
   (This field is populated by API Gateway for both token- and IAM-authorized
   requests, which is why the `AWS_IAM` endpoints can still recover the user.)
2. The username is resolved against the auth layer to load the user's profile
   (including whether they are a super-admin).
3. It sets the logging user context and returns a request context whose
   **accessor** is a freshly built user identity.

The returned user identity seeds one baseline permission — any authenticated
user may create a group (a group-create allow on `*`); all other permissions
are loaded lazily against the accessed resources.

### 5.2 The user accessor and RBAC

The user accessor exposes the caller's `user_id` and the RBAC permission set
the context consults. Authorization runs through an `IsAuthorized(action,
resource)` check against the accessor's permissions, which returns an error
when denied — and this check is invoked from the **DB layer**, not the
handler. Handlers build the context; the DB layer decides.

Loading node permissions is the typical pre-flight before a node operation:
the user's group/subgroup access and the group's nodes are loaded so
subsequent DB calls have the permission set populated.

### 5.3 Super-admin resolution

The super-admin check returns the flag set by the auth layer: admin-pool
identities whose token carries `super_admin == "true"` resolve to super-admin;
regular-pool users never do. This is the same `custom:super_admin` fact the
Identity-Pool role mapping keys on (§2.2) — one deciding the IAM role, the
other deciding in-code branches such as the admin assume-role routes (§3.3).

### 5.4 User resolution and identity helpers

The user layer also provides:

- **Resolving a user from a path parameter** — either by user ID or by share
  code — backed by the authoritative `user_details` table (minted at signup),
  not the endpoints table. A handler that accepts a user reference typically
  tries it as an ID first, then as a share code.
- **Reading the caller's own share code** from `user_details`.
- **Resolving a `user_id` directly from a raw token** (used where a full
  API-Gateway request context is unavailable), requiring the user-pool config.

---

## 6. Client / endpoint registration (push & integrations)

**DB table:** `rmng-user-endpoints`.

This surface registers the addressable *endpoints* a user can be notified on.
It is the credential-registration counterpart to the delivery paths in
`notifications-push.md` and the OAuth-style integrations (Alexa / GVA /
webhooks). A single HTTP-method switch serves:

- **`PUT /v1/integrations/{integrationId}/endpoints`** — register (or replace)
  an endpoint. Idempotent: re-sending the same body is a no-op; different
  credentials replace the stored ones.
- **`DELETE /v1/integrations/{integrationId}/endpoints/{endpointId}`** —
  unregister one specific endpoint (a user may hold several per integration).

Both build the request context and 401 if the caller cannot be resolved.

**Integration-type handling.** The public lowercase type prefix is mapped to
the internal stored form, which also reports whether it is a push integration:

- **Push** (`apns`, `apns_sandbox`, `gcm`) → the internal ID uses the
  uppercase SNS platform prefix (`APNS`/`APNS_SANDBOX`/`GCM`). On `PUT` the
  handler creates an SNS platform endpoint with the app token and stores the
  returned SNS endpoint ARN; `endpoint_id` is the encoded ARN. On `DELETE` it
  first deletes the SNS platform endpoint, then removes the row.
- **OAuth-style** (`alexa`, `gva`, `webhook_*`) → the `access_token` /
  `refresh_token` / `expires_at` / `token_type` bundle is stored in a nested
  `integration_token` attribute; `endpoint_id` is derived from the integration
  ID.

Persistence goes through the client-registration handler into the
`rmng-user-endpoints` table (one row per `(user_id, integration_id,
endpoint_id)`; composite sort key `<integration_id>#<endpoint_id>`).

The Lambda role holds `dynamodb:PutItem/Query/DeleteItem` on
`rmng-user-endpoints` and `sns:CreatePlatformEndpoint`/`DeleteEndpoint` on the
three push platform applications.

The response echoes the `endpoint_id`; callers **must persist it** to later
`DELETE` the specific endpoint they registered.

---

## 7. Security analysis

1. **No direct escalation to the IoT role.** Both Identity-Pool roles
   explicitly `Deny sts:AssumeRole` (§2.3). Only the assume-role Lambda can
   assume `IoTUserRole`, and only ever with a narrowing session policy.
2. **Least privilege by session policy, not by role.** The broad
   `iot:*`-on-`*` grant lives on `IoTUserRole`, but every credential handed to
   a client is the *intersection* of that role and an inline policy scoped to
   the caller's groups/subgroups (MQTT) or a single node's S3/KVS (services).
   A client cannot widen its own scope.
3. **Admin routes double-gate.** The admin assume-role routes require a
   super-admin check in code (§3.3), and admin identities are already the only
   ones the Identity Pool maps to the admin role (§2.2). Node services-mode
   authorization independently verifies group/subgroup membership per node
   (§3.4).
4. **Token verification boundary.** `/v1/user/credentials` carries no gateway
   authorizer; the handler branches on the token's `iss` and verifies it — an
   end-user OIDC access token against the ESP User issuer's JWKS, or an admin ID
   token against the admin pool's JWKS (RS256, `iss`, `exp`) — before any
   exchange. `GET /v1/users/{userId}` likewise verifies the OIDC access token
   in-handler against the ESP User JWKS. All other endpoints rely on SigV4
   (`AWS_IAM`) over Identity-Pool credentials.
5. **Push-endpoint isolation.** Endpoint rows are keyed on the caller's
   `user_id` (derived from context, not the body), so a user can register /
   delete only their own endpoints.

---

## 8. Known limitations

- **Accessible-group ceiling on MQTT credentials.** A caller with more than 4
  full-access groups cannot be issued MQTT credentials: the session policy
  exceeds STS's 2048-character inline-policy limit (§3.3).

- **No identity-ID → username mapping store.** Identity-to-username resolution
  reads the request's authentication-provider string rather than a dedicated
  mapping table, so it depends on API Gateway populating that field.

---

## 9. Out of scope

- **Cognito admin user-pool provisioning** (sign-up, password/MFA policy,
  hosted UI) — owned by Cognito configuration, not this feature.
- **ESP User OIDC token issuance** — the passwordless OTP flow, `sub`
  derivation, PKCE/authorize-code mechanics, id-token claim contents, and JWKS
  publication are owned by the ESP User provider (see
  `src/espuser/docs/specs/auth-flows.md`,
  `src/espuser/docs/specs/oidc-oauth2.md`).
  This spec covers only how ESP RainMaker Neo *consumes* the resulting access token to vend
  AWS credentials and resolve/authorize the caller.
- **The MQTT connection lifecycle** the assume-role credentials feed into —
  see `node_connection.md`.
- **Group / subgroup membership modeling** that the MQTT session policy reads
  — see `group.md` and `group-control-feature.md`.
- **Notification delivery** over the endpoints registered in §6 — see
  `notifications-push.md` and `notifications-webhooks.md`.
- **Node registration and association** (how nodes and users come to share a
  group in the first place) — see `node_reg.md` and `node_assoc.md`.
