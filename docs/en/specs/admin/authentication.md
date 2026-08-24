# Authentication and Permissions

How an admin session is established, and what the IAM role behind it can do.
[user_auth.md](../user_auth.md) is the authority for the identity model as a
whole; this page covers the admin half of it.

## Identity providers

ESP RainMaker Neo uses **two separate Cognito User Pools**:

| Population | Provider | Federated as | Self-signup |
| --- | --- | --- | --- |
| Admins | Cognito user pool **`ESP-Admin-Users`** | a Cognito provider on the pool's `admin-client` | **Disabled** — admins are provisioned |
| End users | the **ESP User OIDC issuer** | an IAM OIDC identity provider, keyed on the issuer URL | Enabled |

The `ESP-End-Users` Cognito pool exists, but end users do not present its tokens
to ESP RainMaker Neo: it sits behind the ESP User issuer, which is what the Identity Pool
trusts. An admin token and an end-user tofperefore differ in issuer, which is
how `POST /v1/user/credentials` tells them apart.

The admin pool declares two custom attributes: `custom:super_admin` (max 4
characters, so the value is the string `"true"`) and `custom:user_id`.

### Admin sign-in factors

Admins seeded by the registration custom resource are created with **no password**:
`AdminCreateUser` omits `TemporaryPassword`, which is legal because the pool
advertises a passwordless factor, and which is why such an account never enters
`FORCE_CHANGE_PASSWORD`. Their only first factor is the emailed one-time code.

Admins seeded before that change keep the password they were given and gain
`EMAIL_OTP` alongside it — Cognito offers no way to remove a password once set, so
they hold both factors permanently.

A passwordless admin can adopt a password from **Account settings → Password**.
`ChangePassword` accepts no `PreviousPassword` when the user has none, and the
dashboard picks the form from `GetUserAuthFactors`. Afterwards they hold both
factors like any pre-existing admin.

One-time password factors are incompatible with **required** MFA, and under the
pool's optional MFA an admin who enrols MFA stops being eligible for OTP sign-in.
Nothing in the dashboard enrols MFA today.

**Email-quota lockout.** The pool uses Cognito's default email sender, capped
around 50 emails/day per pool. `selectOtp` (the `SELECT_CHALLENGE` step) is
unauthenticated and sends one email per call, so anyone who knows one admin
address can loop `USER_AUTH` → `SELECT_CHALLENGE` and exhaust that quota.
Before this feature, exhausting it blocked only `/forgot-password`; now it also
blocks sign-in outright for every admin whose *sole* factor is `EMAIL_OTP` — the
`AdminTempPassword` break-glass output no longer exists to fall back on.
Mitigations, either of which closes the vector: configure an Amazon SES sending
account for the pool (`EmailSendingAccount: DEVELOPER`, its own daily limits
under SES rather than Cognito's default sender), or ensure at least one admin
per pool keeps a password, which sign-in via `USER_PASSWORD_AUTH` does not
consume any email quota to use.

## From admin login to AWS credentials

1. The admin signs in against `ESP-Admin-Users` and receives Cognito tokens. The
   pool allows two first factors, `PASSWORD` and `EMAIL_OTP`, and the dashboard
   uses the choice-based `USER_AUTH` flow: it submits the address alone, Cognito
   answers with the factors that admin actually has, and only those are offered.
   A one-time code is answered with `RespondToAuthChallenge`, first selecting
   `EMAIL_OTP` and then supplying `EMAIL_OTP_CODE`.
2. `POST /v1/user/credentials` exchanges them for Identity-Pool credentials. The
   route carries **no gateway authorizer**; the handler verifies the access token
   (header) and `id_token` (body) against the admin pool's JWKS as a pair. See
   [user_auth.md](../user_auth.md) §3.1.
3. Every other admin endpoint is `AWS_IAM`, so the admin **SigV4-signs** it with
   those credentials. No bearer token is presented to the admin APIs.

### Role selection

The Identity Pool has exactly **one** role mapping, on the admin pool's client:

- A token from the admin pool carrying `custom:super_admin == "true"` maps to
  **`AdminDeviceUsersRole`**.
- Everything else — including **every** federated end user — falls through to the
  pool's default `authenticated` role, **`DeviceUsersRole`**. There is no
  role mapping on the OIDC provider and no per-`aud` branching.

So admin privilege is an infrastructure fact: which role the identity pool will
hand you, decided before any ESP RainMaker Neo Lambda runs.

### Super-admin resolution in the handlers

Being on `AdminDeviceUsersRole` gets a caller to the endpoint; the handler still
re-checks super-admin status, and **where it reads that from depends on the
path**:

- **SigV4 admin endpoints** (the normal case). There is no token in the request,
  only the API Gateway identity. The handler resolves the caller and calls
  Cognito **`AdminGetUser`**, reading the `custom:super_admin` **user attribute**.
  Its value is authoritative.
- **Token-verifying endpoints** (`/v1/user/credentials` and the ESP-User
  surface). The `custom:super_admin` claim is read from the verified Cognito
  token. Cognito emits custom attributes in the **ID token**, not the access
  token.

Either way the handler answers `403` when the caller is not a super admin.

The `user_details` table declares an `is_super_admin` attribute, but no ESP RainMaker Neo code
path reads or writes it — only the test harness sets it. Do not treat it as a
source of truth.

## AdminDeviceUsersRole

Trust: `sts:AssumeRoleWithWebIdentity` by `cognito-identity.amazonaws.com`,
conditioned on this identity pool's id and an `amr` of `authenticated`. The role
trusts the **pool**, not the login provider.

### AWS IoT Core (all on `*`)

| Action | What it enables |
| --- | --- |
| `iot:ListThings`, `iot:DescribeThing` | Enumerate devices; per-device registry detail |
| `iot:SearchIndex`, `iot:GetBucketsAggregation` | Query the fleet index (registry + `iparams` shadow + connectivity); value suggestions for filters |
| `iot:ListThingGroups`, `iot:DescribeThingGroup`, `iot:ListThingsInThingGroup` | Read group hierarchy and membership |
| `iot:CreateThingGroup`, `iot:UpdateThingGroup`, `iot:DeleteThingGroup` | Manage groups, including editing a description |
| `iot:CreateDynamicThingGroup`, `iot:UpdateDynamicThingGroup`, `iot:DeleteDynamicThingGroup` | Manage dynamic (query-backed) groups |
| `iot:AddThingToThingGroup`, `iot:RemoveThingFromThingGroup` | Assign / unassign a device |
| `iot:ListJobs`, `iot:DescribeJob`, `iot:CreateJob`, `iot:CancelJob`, `iot:DeleteJob` | OTA job lifecycle |
| `iot:ListJobExecutionsForJob`, `iot:ListJobExecutionsForThing`, `iot:DescribeJobExecution` | Per-job and per-device execution status |
| `iot:CreateStream`, `iot:DeleteStream` | IoT streams for OTA firmware delivery |

### S3

| Action | Resource |
| --- | --- |
| `s3:PutObject`, `s3:GetObject`, `s3:PutObjectTagging`, `s3:GetObjectTagging` | `<files-bucket>/ota/*`, `<files-bucket>/system/node_certs/*` |
| `s3:ListBucket` | `<files-bucket>` |

The bucket name is resolved per deployment and region, so it is not a fixed
string. OTA firmware and the node-registration certificate CSVs are the two
prefixes granted.

### Other

| Effect | Action | Resource | Purpose |
| --- | --- | --- | --- |
| Allow | `cognito-identity:GetCredentialsForIdentity` | `*` | Refresh credentials |
| Allow | `execute-api:Invoke` | `RMBaseApi` | Call any ESP RainMaker Neo API endpoint, and nothing outside it |
| Allow | `sts:TagSession` | IoT user role | Tag assumed sessions |
| Allow | `iam:PassRole` | OTA service role | Required to create an OTA job |
| Allow | `cloudformation:ListStacks` | `*` | Detect which optional module stacks are deployed, so the dashboard surfaces a feature only when its stack exists. `ListStacks` takes no resource scope. |
| **Deny** | `sts:AssumeRole` | `*` | Blocks credential escalation into any other role |

The `sts:AssumeRole` deny is explicit, so it wins over any allow — an admin
cannot widen its own scope by assuming a broader role.

### Not granted

| Missing action | Consequence |
| --- | --- |
| `iot:GetThingShadow` | Shadows are not readable through the REST API; the dashboard sees only what the fleet index projects |
| `iot:UpdateThingShadow` | Shadows are read-only from the dashboard. IoT Data Plane also requires an IoT policy attached to the Cognito identity, so adding the IAM action alone would not be enough |
| `iot:DeleteThingShadow` | Shadows cannot be deleted |
| `iot:ListNamedShadowsForThing` | Shadow names cannot be enumerated |
| `iot:DescribeIndex` | The fleet-indexing configuration is not inspectable |
| Any `dynamodb:*` | No direct table access — every read and write goes through a Lambda API, which is where RBAC is enforced |

## OTA service role

A separate role, assumed by **`iot.amazonaws.com`** rather than by the admin.
`AdminDeviceUsersRole` holds `iam:PassRole` on it, which is what lets an admin
create an OTA job that AWS IoT then executes under this role:

- `s3:GetObject`, `s3:GetObjectVersion` on `<files-bucket>/ota/*`
- `iot:CreateJob`, `iot:DescribeJob`, `iot:DeleteJob`, `iot:GetOTAUpdate`
- `iot:CreateStream`, `iot:DescribeStream`, `iot:DeleteStream`
- `iam:PassRole` on itself
