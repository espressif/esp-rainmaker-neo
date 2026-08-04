# Notifications — Mobile Push

> See [notifications.md](notifications.md) for the dispatcher and service-registry model that this channel plugs into. This page covers only the push-specific surface.

## What is mobile push

Outbound notifications delivered to a user's installed mobile app via APNS (iOS) or GCM (Android). ESP RainMaker Neo does not talk to APNS or GCM directly — it goes through **AWS SNS Mobile Push**, which holds the platform credentials and the device endpoints, and exposes a single `Publish` API.

## Why it is needed

- One credential rotation surface (SNS) instead of two SDKs.
- Per-device SNS endpoint ARNs are stable identifiers, so they can be stored against the user without re-registering on every app launch.
- Multi-platform messages (`{"default": ..., "APNS": ..., "GCM": ...}`) let one Publish reach any device the user has registered.

## Pre-requisites

- A super admin has registered a push integration via `POST /v1/admin/integrations?integration_type=apns|apns_sandbox|gcm` (creates an SNS Platform Application — see [Register Integration](#register-integration)).
- The mobile app has obtained an APNS/GCM device token from the OS.
- The user is authenticated and has registered the device via `PUT /v1/integrations/{integrationId}/endpoints` (creates an SNS Platform Endpoint and returns an `endpoint_id` — see [Register Endpoint](#register-endpoint)).

## Vocabulary

The notifications surface uses three layers:

| Layer                | What it is                                                                                                                                   | Examples                                                 |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| **Endpoint**         | One user's delivery target on an integration. User-created (or created internally for OAuth-style integrations). Addressed by `endpoint_id`. | One iPhone install, one linked Alexa account             |
| **Integration type** | The channel family / protocol.                                                                                                               | `apns`, `apns_sandbox`, `gcm`, `alexa`, `gva`, `webhook` |
| **Integration**      | One configured instance of an integration type. Admin-created. Addressed by an opaque `integration_id`.                                      | An APNS app, a GCM project, the Alexa skill              |

`integration_id` is documented as opaque in the public contract. Today the handler still encodes it as `<type>_<app-name>` (`apns_com.rainmaker.app`, `gcm_my-firebase-project`); callers must not parse it.

## Architecture

```
                     +-------- SNS Platform Application ---------+
                     |   APNS_<bundle_id>  /  GCM_<project_id>   |
                     |   credentials: .p8 / service-account JSON |
                     +-------------+-----------------------------+
                                   |
                            CreatePlatformEndpoint
                                   |
                                   v
                     +-------- SNS Platform Endpoint -----------+
                     |   Endpoint ARN stored on the endpoint row|
                     +-------------+----------------------------+
                                   |
   notifications Lambda            |
                                   v
                          sns:Publish(TargetArn = endpoint ARN,
                                      Message  = multi-platform JSON,
                                      MessageStructure = json)
                                   |
                                   v
                          APNS / GCM --> phone
```

Naming note: the SNS API never adopted Google's GCM→FCM rename — `Platform=GCM` is the only accepted Firebase platform name. The internal platform value, stored `integration_id` prefix (`GCM_<project>`), platform application ARNs (`app/GCM/<project>`), and the multi-platform Publish envelope key all therefore say `GCM`, even though Firebase itself is now branded FCM.

## APIs

The notifications surface is part of the unified integrations tree. Push integrations live alongside Alexa, GVA, and webhook integrations under `/v1/admin/integrations`; user-facing endpoint registration lives under `/v1/integrations/{integrationId}/endpoints`. The full surface is specified in the platform API document (`docs/api/Api_Swagger.yaml`); the sections below cover the push-relevant pieces.

### Register Integration

**API**: `POST /v1/admin/integrations?integration_type=apns|apns_sandbox|gcm`

**Access Control**: Super-admin only — non-admin callers get `403`.

#### Obtaining the credentials

**Android (GCM)** — produces the service-account JSON sent inline as the request body:

1. Log in to https://console.firebase.google.com/
2. Create or choose a project.
3. Go to Settings → Service accounts.
4. On the 'Firebase Admin SDK' tab, click 'Generate new private key'. The downloaded JSON is posted verbatim as the request body.

**iOS (APNS)** — produces the `.p8` key plus its identifiers:

1. Log in to https://developer.apple.com/account/resources
2. Create an App ID of type App: add the bundle ID (becomes `bundle_id`), enable Push Notifications, and note the Team ID (becomes `team_id`).
3. Create a Key of type 'Apple Push Notifications service', pointed at that App ID. Download the `.p8` (its body becomes `authentication_key`) and note the Key ID (becomes `key_id`).

**Request body** (push variant of `IntegrationConfigRequest`):

The body shape depends on `integration_type`.

APNS / APNS_SANDBOX:

```json
{
  "authentication_key": "<.p8 body>",
  "key_id": "ABC123",
  "team_id": "TEAM123",
  "bundle_id": "com.rainmaker.app"
}
```

GCM — the Google service-account JSON fields are sent **inline as the body**; paste the downloaded Firebase JSON as the request body:

```json
{
  "type": "service_account",
  "project_id": "my-firebase-project",
  "private_key_id": "abc123",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "fcm@my-firebase-project.iam.gserviceaccount.com",
  "client_id": "1234567890",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/...",
  "universe_domain": "googleapis.com"
}
```

| Field                                                  | Required for           | Notes                                                                                                                                     |
| ------------------------------------------------------ | ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| service-account fields (`type`, `project_id`, `private_key_id`, `private_key`, `client_email`, `client_id`, `auth_uri`, `token_uri`, `auth_provider_x509_cert_url`, `client_x509_cert_url`, `universe_domain`) | `gcm` | The full Google service-account JSON, sent inline. **All** fields are required. `project_id` becomes the platform-app-name. |
| `authentication_key`, `key_id`, `team_id`, `bundle_id` | `apns` | Token-based auth credentials. `bundle_id` becomes the platform-app-name in the SNS ARN.                                                   |

`apns_sandbox` is selected via the `integration_type=apns_sandbox` query parameter, not a body field.

**Process**:

1. Validate the body shape for the chosen `integration_type` (all four APNS fields, or a Google service-account JSON with all fields present).
2. Build the SNS attribute map by remapping request fields to SNS attribute names:
  - APNS / APNS_SANDBOX: `authentication_key` → `PlatformCredential`, `key_id` → `PlatformPrincipal`, `team_id` → `ApplePlatformTeamID`, `bundle_id` → `ApplePlatformBundleID`.
  - GCM: the marshalled Google service-account JSON → `PlatformCredential`.
3. Call `sns:CreatePlatformApplication`.
4. Derive `integration_id` from the integration type and the platform-app-name (`apns_<bundle_id>`, `apns_sandbox_<bundle_id>`, `gcm_<project_id>`).

**Response** (`201 Created`):

```json
{ "integration_id": "apns_com.rainmaker.app" }
```

The corresponding SNS Platform Application ARN is constructed at runtime from the components, never stored:

```
arn:aws:sns:<region>:<account>:app/<PLATFORM_TYPE>/<platform_app_name>
```

### List Integrations

**API**: `GET /v1/admin/integrations` (optionally filtered by `?integration_type=...`)

**Access Control**: Super-admin only.

**Process**:

1. For push types: call `sns:ListPlatformApplications`, following `NextToken` until exhausted. Parse each returned ARN to extract type + platform-app-name; combine into `integration_id`.
2. For non-push types: read from their respective config stores (SSM for Alexa / GVA).

**Response**: an `integrations` array where each entry is the full per-type detail (same shape as `GET /v1/admin/integrations/{integrationId}`). For push integrations this includes the stored credential payload (Google service-account JSON for GCM; bundle_id only for APNS — the .p8 key is secret and never returned).

### Get Integration

**API**: `GET /v1/admin/integrations/{integrationId}`

**Access Control**: Super-admin only.

**Response (APNS variant)** — only `bundle_id` is recoverable from SNS; `key_id` / `team_id` are not stored on the platform application and are not returned:

```json
{
  "integration_id": "apns_com.rainmaker.app",
  "integration_type": "apns",
  "bundle_id": "com.rainmaker.app"
}
```

**Response (GCM variant)** — the full Google service-account JSON is unmarshalled out of the stored `PlatformCredential` and returned verbatim, including `private_key`:

```json
{
  "integration_id": "gcm_my-firebase-project",
  "integration_type": "gcm",
  "type": "service_account",
  "project_id": "my-firebase-project",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "fcm@my-firebase-project.iam.gserviceaccount.com",
  "...": "remaining service-account fields"
}
```

For APNS the `.p8` body is never returned (SNS does not expose `PlatformCredential` for APNS in a recoverable form). For GCM the service-account JSON — including the private key — **is** returned.

> Security note: the GCM response returns sensitive credential material verbatim (the service-account private key). Keep this endpoint restricted to super-admins and avoid logging or caching its response.

### Update Integration Credentials

**API**: `PUT /v1/admin/integrations/{integrationId}`

**Access Control**: Super-admin only.

**Request body**: same shape as the matching variant of `IntegrationConfigRequest` for the integration's type.

For GCM, the new key's `project_id` must match the existing integration's project — otherwise the request is rejected.

**Process**:

1. Look up the integration by `integration_id` to discover its type.
2. Build the SNS attribute map from the request body.
3. Call `sns:SetPlatformApplicationAttributes`.

**Response**: `{"message": "success"}`. Existing endpoints are unaffected — credential rotation does not re-register endpoints.

### Delete Integration

**API**: `DELETE /v1/admin/integrations/{integrationId}`

**Access Control**: Super-admin only.

**Process**:

1. Look up the integration; validate it is a push integration.
2. Call `sns:DeletePlatformApplication`.

**Response**: `{"message": "success"}`. Existing endpoints become orphaned and start failing on the next publish.

> Known limitation: user endpoint rows referencing this `integration_id` are not cascade-deleted. They remain pointing at SNS endpoints under the deleted Platform Application and silently fail on every subsequent push until removed.

### Register Endpoint

**API**: `PUT /v1/integrations/{integrationId}/endpoints`

**Access Control**: Authenticated user.

PUT is idempotent: re-sending the same body is a no-op (SNS `CreatePlatformEndpoint` is idempotent for matching `(platform-app, token)` pairs, and the DynamoDB write is a `PutItem`-by-composite-key). Sending a different token creates a new endpoint row; the stale one self-heals on next send (see [self-healing on disabled endpoints](#self-healing-on-disabled-endpoints)).

An "endpoint" is one user's delivery target on a given integration. For push, that's one app installation (one phone). Re-installing the app yields a new device token and (on registration) a new `endpoint_id`.

**Request** (push variant of `RegisterEndpointRequest`):

```json
{
  "delivery_credentials": {
    "app_token": "<APNS or GCM token from the OS>"
  },
  "locale": "en-US"
}
```

**Process**:

1. Look up the integration by `integration_id` to recover its type and platform-app-name.
2. Call `sns:CreatePlatformEndpoint` with the constructed Platform Application ARN and the raw `app_token`. SNS returns an **Endpoint ARN**.
3. Persist the endpoint row with the SNS Endpoint ARN as its delivery handle, plus the locale.
4. Return the `endpoint_id` — for push this **is** the SNS Endpoint ARN (not a separate server-generated id).

**Response**: the body serializes the full `EndpointStatusResponse`, so `status` is always present (empty on this path):

```json
{ "status": "", "endpoint_id": "arn:aws:sns:<region>:<account>:endpoint/APNS/<app>/<uuid>" }
```

### Unregister Endpoint

**API**: `DELETE /v1/integrations/{integrationId}/endpoints/{endpointId}`

**Access Control**: Authenticated user.

**Process**:

1. Look up the endpoint row; for push, recover the stored SNS Endpoint ARN.
2. Call `sns:DeleteEndpoint`.
3. Delete the endpoint row.

**Response**: `{ "status": "" }` (the handler returns an empty `EndpointStatusResponse`; `endpoint_id` is omitted when empty).

## Storage model

Endpoints live in the `rmng-user-endpoints` DynamoDB table. One row per (user_id, integration_id, endpoint_id) — the composite key supports multiple endpoints per user per integration (two phones → two `apns_com.rainmaker.app` rows; two linked Amazon accounts → two `alexa` rows).

| Column                                                             | Type         | Notes                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------------------------------ | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `integration_token`                                                | map          | OAuth-style rows only (alexa, gva, webhook). A **nested map** holding `access_token`, `refresh_token`, `access_expires_at`, `token_type` and `region`. `region` is the AWS region at link time, used to select the Alexa event gateway. Absent on push rows.                                              |
| `endpoint_id`                                                      | string       | The natural identifier for this endpoint within the integration. Per-type: push → SNS Platform Endpoint ARN; alexa → Amazon user_id from LWA `/user/profile`; webhook / gva → the integration_id (one row per user today; see [notifications.md](notifications.md#endpoint-identity-per-integration)). |
| `integration_endpoint`                                             | string       | Sort key. Composite `<integration_id>#<endpoint_id>` — the SK that lets a single user own multiple endpoints per integration.                                                                                                                                                                          |
| `integration_id`                                                   | string       | Duplicated out of the SK for convenience (e.g. `APNS_com.rainmaker.app`, `alexa`, `webhook`). Casing is the internal uppercase form; translation to/from the lowercase public contract happens at the handler boundary.                                                                           |
| `locale`                                                           | string       | Locale code supplied at registration (e.g. `en-US`). Used by the send path to pick a localized title and body.                                                                                                                                                                                         |
| `sns_endpoint_arn`                                                 | string       | Push only. The SNS Platform Endpoint ARN — the `TargetArn` passed to `sns:Publish`. Empty for OAuth-style rows.                                                                                                                                                                                        |
| `user_id`                                                          | string       | Partition key. The Cognito identity that owns this endpoint.                                                                                                                                                                                                                                           |

Read patterns:
- **All endpoints for a user** — `Query` on the partition key alone; returns every endpoint row for the user across every integration. Used by the push send path.
- **A user's endpoints on one integration** — `Query` with `begins_with(integration_endpoint, "<integration_id>#")`; returns all of a user's endpoints on a single integration. Used by OAuth-channel send paths (Alexa, webhook) to fan out to every linked account.
- **A single endpoint** — `GetItem` on the exact composite key. Used by DELETE.

No GSIs today. Admin "cascade-delete on integration deletion" is not implemented and would require a scan or a new GSI on `integration_id` — see the [known limitation](#delete-integration) noted above.

## Send path

Triggered by the dispatcher when the incoming event's `notify` map contains a `"push"` key.

1. Stamp `type`, `ts` (unix timestamp) and the channel-agnostic event data into the outbound message.
2. For each user in the resolved list, load **every** endpoint row (`GetUserEntries(user_id)` — one Query, no per-type filter at the DB layer). The per-row dispatch in step 4 skips rows whose `integration_id` is not a push prefix (`APNS_`, `GCM_`); they remain in the result set but no Publish is issued.
3. Resolve a localized title and body for the event name, using the locale stored on the first push endpoint row (a user with multiple installs is expected to have consistent locales).
4. For each endpoint row:
  - Pick a platform-specific format (APNS or GCM) from the integration's type.
  - Format the platform-specific JSON body and any SNS message attributes.
  - Wrap in a multi-platform SNS envelope. The platform key is the uppercase SNS platform name:
    ```json
    {
      "default": "<title>: <text>",
      "APNS":    "<formatted APNS JSON>"
    }
    ```
    (or the `GCM` key, depending on the integration's type).
  - Call `sns:Publish` with `TargetArn` set to the row's stored SNS Endpoint ARN, `MessageStructure` `json`, plus any per-platform message attributes.
5. Per-row errors are logged but do not abort the loop.

## Body schemas

### APNS payload

```text
{
  "aps": {
    "alert":           { "title": "<title>", "body": "<text>" },
    "sound":           "default",
    "mutable-content": 1,
    "category":        "<category>",
    "thread-id":       "<grouping_id>"
  },
  "event_data": { ... }
}
```

- `category` and `thread-id` are omitted if empty.
- `event_data` is the channel-agnostic extra data attached by the dispatch step; omitted if absent.
- Priority is mapped to the SNS message attribute `AWS.SNS.MOBILE.APNS.PRIORITY`, not the body:
  - `low` → `1`
  - `normal` → `5`
  - `high` → `10`

### GCM payload

```text
{
  "data": {
    "title":      "<title>",
    "body":       "<text>",
    "event_data": { ..., "notif_grp_id": "<grouping_id>" }
  },
  "android": { "priority": "<normal|high>" }
}
```

- `android.priority` is omitted if priority is empty or `low` (GCM has no native `low`).
- GCM has no analogue of APNS thread IDs, so the grouping ID is folded into `event_data.notif_grp_id`.
- `category` is APNS-only and ignored for GCM.

## Marshal

The push channel currently produces a payload only for **shadow update** notifications. The fields it sets are:

| Field       | Value                                                                             |
| ----------- | --------------------------------------------------------------------------------- |
| category    | `node_alert`                                                                      |
| event data  | `{ "nodeID": <originating node's ID> }`                                           |
| event name  | `node_alert`                                                                      |
| grouping ID | `<nodeID>.node.alert` (drives APNS `thread-id` and GCM `event_data.notif_grp_id`) |

Title and body are filled in afterwards by the localized-message lookup keyed on the event name. Direct notifications are not currently marshalled to push — the marshal step fails and the dispatcher continues.

> Known limitation: only `{nodeID}` is substitutable in push message templates today. Any other placeholder is left unsubstituted, since no other fields (shadow params, delta, notify payload) are forwarded into the substitution map.

## Self-healing on disabled endpoints

APNS and GCM mark a device endpoint disabled when the underlying device token goes stale (app uninstalled, user revoked notifications, token rotated). SNS surfaces this on the next `sns:Publish` as `EndpointDisabledException`. The send path catches this specific error and:

1. Calls `sns:DeleteEndpoint` to drop the SNS Platform Endpoint.
2. Calls `UnregisterClient(integration_id, endpoint_id)` to delete the matching `rmng-user-endpoints` row.
3. Logs the cleanup and continues with the rest of the fanout — the rest of the user's devices still get the notification.

The mobile client re-registers on its next cold start (apps typically PUT the endpoint on every launch), creating a fresh enabled row. Net effect: stale endpoints self-heal on the first send that hits them; no background sweep is needed.

The notifications lambda's IAM role grants `sns:DeleteEndpoint` and `dynamodb:DeleteItem` on `rmng-user-endpoints` for exactly this self-healing path.
