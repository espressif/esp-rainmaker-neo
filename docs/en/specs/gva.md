# GVA (Google Voice Assistant)

## What is GVA

GVA is the Google Smart Home integration that allows users to control their RainMaker devices via Google Assistant. It implements the [Google Smart Home API](https://developers.google.com/assistant/smarthome/overview) fulfillment webhook, handling device discovery, state queries, and device control commands.

## Why it is needed

To enable voice control and Google Home app integration for RainMaker devices. Users link their RainMaker account with Google via OAuth against the ESP User OIDC identity provider and can then say things like "Hey Google, turn on the light" to control their devices.

## Pre-requisites

- User is registered and authenticated
- GVA configuration

## Architecture

Two Lambda functions serve the GVA integration:

1. **`gva_action`** - Handles Smart Home fulfillment (SYNC, QUERY, EXECUTE, DISCONNECT intents)
2. **`gva_cfg`** - Handles GVA configuration management (store/get/delete credentials)

## Configuration

### OIDC VA Client

Account linking runs against the ESP User OIDC identity provider, not Cognito. A confidential OAuth 2.1 client (`va-client`) is seeded into the ESP User client registry (`espuser-oauth-clients`) for voice-assistant account linking:
- **OAuth flow**: Authorization Code Grant (`grant_types`: `authorization_code`, `refresh_token`)
- **Scopes**: `openid`, `email`, `phone`, `profile`
- **Redirect URIs**: registered dynamically by the config API (there are no hardcoded initial values); each POST unions its URIs onto the client's existing set, so the Google and Alexa redirect URIs coexist on the shared `va-client` row
- **Client ID**: `va-client` — the registry client id (also available in SSM `/espuser/base/va-client-id`)
- **Client Secret**: the generated secret for `va-client`, retrievable via the superadmin clients API (`GET /v1/admin/clients?get_secret=true`) or SSM `/espuser/base/va-client-secret`
- **OIDC endpoints**: the `authorization_endpoint` and `token_endpoint` published in the discovery document (`/.well-known/openid-configuration`). Both are served on the ESP User API Gateway base (`EspUserApiUrl`): `<api-url>/oauth2/authorize` and `<api-url>/oauth2/token`.

### Google Console Setup

This must be done before calling the Store Configuration API.

#### Step 1: Create Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com)
2. Create a new project (or use an existing one)
3. Note the **Project ID** (e.g., `your-project-id`) — this is the `project_id` for the config API

#### Step 2: Create Google Home Project

1. Go to [Google Home Developer Console](https://console.home.google.com/projects)
2. Create a new project, link it to the Google Cloud project from Step 1
3. Add a **Cloud-to-cloud** integration
4. Under **Develop > Setup**:
   - **OAuth Client ID**: `va-client` (the OIDC client id; SSM `/espuser/base/va-client-id`)
   - **OAuth Client Secret**: the `va-client` secret from the superadmin clients API (`GET /v1/admin/clients?get_secret=true`) or SSM `/espuser/base/va-client-secret`
   - **Authorization URL**: the discovery document's `authorization_endpoint` (`<api-url>/oauth2/authorize`)
   - **Token URL**: the discovery document's `token_endpoint` (`<api-url>/oauth2/token`)
   - **Fulfillment URL**: `https://<api_gateway_url>/v1/integrations/gva` where api_gateway_url is from rmng-outputs.json (ApiGatewayUrl)
   - **Scopes**: `openid`, `email`, `phone`, `profile`

#### Step 3: Enable HomeGraph API (for Report State)

1. Go to https://console.cloud.google.com/apis/library/homegraph.googleapis.com
2. On the drop-down menu at the top, select your smart home project created in previous steps
3. Click **Enable**

#### Step 4: Create Service Account (for Report State)

A **dedicated** service account must be created for Report State. Do NOT reuse the Firebase Admin SDK default service account (`firebase-adminsdk-*`) — it can authenticate but cannot write to the Home Graph.

1. Go to Google Service Account page: https://console.cloud.google.com/projectselector2/iam-admin/serviceaccounts?supportedpurview=project
2. Select your smart home project created in the previous step
3. Click **Create Service Account**
4. In the Service Account Name field, enter a suitable name (e.g., `homegraph-agent`)
5. In Description, enter "Service Account for report state token creation"
6. Click **Create and Continue**
7. In the next step, select the role type **Service Accounts** and select the role **Service Account OpenID Connect Identity Token Creator**
8. Click **Continue** and then click **Done**

Generate a JSON key:
1. Click the created service account email
2. Go to the **Keys** tab and click **Add Key**
3. Select **Create new key**
4. Select **JSON** key type
5. A file will be downloaded with the required credentials

This JSON is uploaded via the Store Configuration API (see below). The notification Lambda uses it to obtain OAuth2 tokens with scope `https://www.googleapis.com/auth/homegraph` for calling the [Report State API](https://developers.home.google.com/cloud-to-cloud/integration/report-state).

## GVA config

### Store Configuration

The configuration is stored by calling the admin configuration API with the service account JSON key file downloaded from the Google Cloud Console (see Step 4 above) as the request body. The API validates the payload, registers the Google redirect URI on the OIDC `va-client` registry row (unioning it onto the client's existing set), and persists the JSON to SSM (`/rmng/gva/service_account_json`).

**API**: `POST /v1/admin/integrations/gva/configuration`

**Request**: The entire service account JSON is sent as the request body.
```json
{
  "type": "service_account",
  "project_id": "<your-project-id>",
  "private_key_id": "<key-id>",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "<name>@<project-id>.iam.gserviceaccount.com",
  "client_id": "<client-id>",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/<encoded-email>",
  "universe_domain": "googleapis.com"
}
```

**Validation**: `project_id`, `client_email`, and `private_key` must be present.

**Process**:
1. Validate required fields (`project_id`, `client_email`, `private_key`)
2. Calculate redirect URI: `https://oauth-redirect.googleusercontent.com/r/<project_id>`
3. Register the redirect URI on the OIDC `va-client` registry row, unioning it onto its existing set (env var: `OIDC_VA_CLIENT_ID`, value `va-client`)
4. Store the entire service account JSON as a single SSM parameter (`/rmng/gva/service_account_json`)

**Response**:
```json
{
  "message": "GVA client configuration stored successfully",
  "project_id": "my-project-id",
  "redirect_uris": ["https://oauth-redirect.googleusercontent.com/r/my-project-id"]
}
```

### Get Configuration

**API**: `GET /v1/admin/integrations/gva/configuration`

**Process**:
1. Retrieve the service account JSON from SSM (`/rmng/gva/service_account_json`)
2. Calculate redirect URI from `project_id`
3. Return the full service account JSON with `redirect_uris` appended

**Response**:
```json
{
  "type": "service_account",
  "project_id": "<your-project-id>",
  "private_key_id": "<key-id>",
  "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
  "client_email": "<name>@<project-id>.iam.gserviceaccount.com",
  "client_id": "<client-id>",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/<encoded-email>",
  "universe_domain": "googleapis.com",
  "redirect_uris": ["https://oauth-redirect.googleusercontent.com/r/my-project-id"]
}
```

### Delete Configuration

**API**: `DELETE /v1/admin/integrations/gva/configuration`

**Process**:
1. Delete the service account JSON from SSM (`/rmng/gva/service_account_json`)

**Response**:
```json
{
  "message": "GVA client configuration deleted successfully"
}
```

## Smart Home Intents

### Authentication

Google sends an ESP User access token (issued to the voice-assistant client at account linking) in the `Authorization: Bearer <token>` header. The Lambda extracts the user ID using this token to identify the user. No API Gateway authorizer is used — the `POST /v1/integrations/gva` endpoint has `authorization_type: NONE` since Google sends the token in the request body/headers and the Lambda validates it directly.

### Device ID Format

Devices are identified as `<node_id>.<device_name>` (e.g., `ABC123.Light1`). This composite ID maps a Google device back to a specific RainMaker node and device within that node.

### Custom Data

During SYNC, each device carries `customData` containing:
- `groupID` — the group the node belongs to (for permission loading)
- `paramMap_<Trait>` — maps GVA traits to RainMaker parameter names (e.g., `paramMap_OnOff: "Power"`)

This custom data is sent back by Google on QUERY and EXECUTE requests, avoiding additional lookups.

All intents are served by a single webhook endpoint.

**API**: `POST /v1/integrations/gva`

**Authorization**: Bearer token in `Authorization` header (ESP User access token)

The Lambda routes requests based on `inputs[0].intent`:

| Intent                      | Handled as        |
| --------------------------- | ----------------- |
| `action.devices.DISCONNECT` | Account unlinking |
| `action.devices.EXECUTE`    | Device control    |
| `action.devices.QUERY`      | State queries     |
| `action.devices.SYNC`       | Device discovery  |

### SYNC (Device Discovery)

Google sends SYNC when the user links their account or requests a device refresh.

**Request**:
```json
{
  "requestId": "uuid",
  "inputs": [{ "intent": "action.devices.SYNC", "payload": {} }]
}
```

**Process**:
1. Get user ID from the ESP User access token
2. List all groups for the user
3. For each group, iterate over `nodeIDs`
4. For each node, fetch its node configuration
5. For each device in the node config:
   - Generate device ID: `<node_id>.<device_name>`
   - Map RainMaker device type to GVA device type
   - Determine traits and attributes from device parameters
   - Build `customData` with group ID and parameter mappings

**Response**:
```json
{
  "requestId": "uuid",
  "payload": {
    "agentUserId": "user-id",
    "devices": [
      {
        "id": "node123.Light1",
        "type": "action.devices.types.LIGHT",
        "traits": ["action.devices.traits.OnOff", "action.devices.traits.Brightness"],
        "name": { "name": "Light1" },
        "willReportState": true,
        "deviceInfo": {
          "manufacturer": "ESP32",
          "model": "RainMaker Device",
          "swVersion": "1.1.0"
        },
        "attributes": {},
        "customData": {
          "groupID": "abc123",
          "paramMap_OnOff": "Power",
          "paramMap_Brightness": "Brightness"
        }
      }
    ]
  }
}
```

#### Device Type Mapping

| RainMaker Type                               | GVA Type                                |
| -------------------------------------------- | --------------------------------------- |
| `esp.device.fan`, `fan`                      | `action.devices.types.FAN`              |
| `esp.device.lightbulb`, `lightbulb`, `light` | `action.devices.types.LIGHT`            |
| `esp.device.outlet`, `outlet`                | `action.devices.types.OUTLET`           |
| `esp.device.switch`, `switch`                | `action.devices.types.SWITCH`           |
| `esp.device.thermostat`, `thermostat`        | `action.devices.types.THERMOSTAT`       |
| Unknown / nil                                | `action.devices.types.SWITCH` (default) |

#### Trait Mapping

| RainMaker Param Type                                           | GVA Trait                                  | GVA Command                                             |
| -------------------------------------------------------------- | ------------------------------------------ | ------------------------------------------------------- |
| `esp.param.brightness`, `brightness`                           | `action.devices.traits.Brightness`         | `action.devices.commands.BrightnessAbsolute`            |
| `esp.param.hue`, `esp.param.saturation`, `saturation`, `color` | `action.devices.traits.ColorSetting`       | `action.devices.commands.ColorAbsolute`                 |
| `esp.param.power`, `power`, `switch`                           | `action.devices.traits.OnOff`              | `action.devices.commands.OnOff`                         |
| `esp.param.speed`, `fanspeed`                                  | `action.devices.traits.FanSpeed`           | `action.devices.commands.SetFanSpeed`                   |
| `esp.param.temperature`, `temperature`                         | `action.devices.traits.TemperatureSetting` | `action.devices.commands.ThermostatTemperatureSetpoint` |

#### Trait Attributes

- **ColorSetting**: `{ "colorModel": "hsv" }`
- **FanSpeed**: Ordered speed names: `low` (slow), `medium` (mid), `high` (fast)
- **TemperatureSetting**: Modes: `heat`, `cool`, `heatcool`, `auto`, `off`. Unit: Celsius

**Sequence Diagram**:

```mermaid
sequenceDiagram
    title GVA SYNC - Device Discovery

    participant Google as Google Assistant
    participant Lambda as gva_action Lambda
    participant ConfigSvc as ConfigService
    participant GroupDB as DynamoDB (groups)
    participant UserGroupDB as DynamoDB (rmng-user-group-assoc)

    Google->>Lambda: POST /v1/integrations/gva<br/>{intent: SYNC, Bearer token}
    activate Lambda

    Lambda->>Lambda: Extract user ID from token

    Lambda->>UserGroupDB: List groups for user
    UserGroupDB->>Lambda: groups with node_ids

    loop For each node in each group
        Lambda->>ConfigSvc: Get node config (node_id)
        ConfigSvc->>Lambda: NodeCfg (devices, params)
        Lambda->>Lambda: Map devices to GVA types/traits<br/>Build customData with paramMap
    end

    Lambda->>Google: {agentUserId, devices[]}

    deactivate Lambda
```

### QUERY (State Query)

Google sends QUERY to get current device state.

**Request**:
```json
{
  "requestId": "uuid",
  "inputs": [{
    "intent": "action.devices.QUERY",
    "payload": {
      "devices": [
        { "id": "node123.Light1", "customData": { "groupID": "abc123", "paramMap_OnOff": "Power" } }
      ]
    }
  }]
}
```

**Process**:
1. Get user ID from the ESP User access token
2. For each queried device:
   - Parse device ID into `node_id` and `device_name`
   - Load node permissions using `groupID` from `customData`
   - Get device parameters from the IoT shadow
   - Convert parameter values to GVA state format using trait parameter mappings from `customData`

**State Conversion**:

| Trait              | RainMaker Param     | GVA State Key                                      | Conversion                                                      |
| ------------------ | ------------------- | -------------------------------------------------- | --------------------------------------------------------------- |
| Brightness         | Brightness (int)    | `brightness`                                       | Direct integer (0-100)                                          |
| ColorSetting       | Hue, Saturation     | `color.spectrumHsv`                                | Hue direct, saturation/value divided by 100                     |
| FanSpeed           | Speed (int)         | `currentFanSpeedPercent`, `currentFanSpeedSetting` | Percentage + named setting (<=33: low, <=66: medium, >66: high) |
| OnOff              | Power (bool)        | `on`                                               | Direct boolean                                                  |
| TemperatureSetting | Temperature (float) | `thermostatTemperatureSetpoint`, `thermostatMode`  | Direct float, mode defaults to "heat"                           |

**Response**:
```json
{
  "requestId": "uuid",
  "payload": {
    "devices": {
      "node123.Light1": {
        "status": "SUCCESS",
        "online": true,
        "on": true,
        "brightness": 75
      }
    }
  }
}
```

**Error Response** (device not found):
```json
{
  "requestId": "uuid",
  "payload": {
    "devices": {
      "node123.Light1": {
        "status": "ERROR",
        "errorCode": "deviceNotFound"
      }
    }
  }
}
```

**Sequence Diagram**:

```mermaid
sequenceDiagram
    title GVA QUERY - State Query

    participant Google as Google Assistant
    participant Lambda as gva_action Lambda
    participant IoT as AWS IoT Shadow

    Google->>Lambda: POST /v1/integrations/gva<br/>{intent: QUERY, devices[], Bearer token}
    activate Lambda

    Lambda->>Lambda: Extract user ID from token

    loop For each device
        Lambda->>Lambda: Parse device ID (node_id.device_name)
        Lambda->>Lambda: Load node permissions (groupID from customData)
        Lambda->>IoT: Get device params from shadow
        IoT->>Lambda: Device parameters
        Lambda->>Lambda: Convert params to GVA state<br/>using paramMap from customData
    end

    Lambda->>Google: {devices: {device_id: {status, state}}}

    deactivate Lambda
```

### EXECUTE (Device Control)

Google sends EXECUTE when the user issues a voice command.

**Request**:
```json
{
  "requestId": "uuid",
  "inputs": [{
    "intent": "action.devices.EXECUTE",
    "payload": {
      "commands": [{
        "devices": [{ "id": "node123.Light1", "customData": { "groupID": "abc123", "paramMap_OnOff": "Power" } }],
        "execution": [{ "command": "action.devices.commands.OnOff", "params": { "on": true } }]
      }]
    }
  }]
}
```

**Process**:
1. Extract user ID from access token
2. For each command, for each device, for each execution:
   - Parse device ID into `node_id` and `device_name`
   - Load node permissions using `groupID` from `customData`
   - Route to command handler based on `execution.command`
   - Map GVA params to RainMaker params using `customData` parameter mappings
   - Publish the mapped values to the device's desired shadow

**Command mapping**:

| Command                         | GVA Params                                                                              | RainMaker Publish                                                                                  | GVA State Response                                                                     |
| ------------------------------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `BrightnessAbsolute`            | `{ "brightness": int }`                                                                 | `{ "<device>": { "<paramMap_Brightness>": int } }`                                                 | `{ "brightness": int, "online": true }`                                                |
| `ColorAbsolute`                 | `{ "color": { "spectrumHsv": { "hue": float, "saturation": float, "value": float } } }` | `{ "<device>": { "<hueParam>": int, "<satParam>": int(sat*100), "<brightParam>": int(val*100) } }` | `{ "color": { "spectrumHsv": {...} }, "online": true }`                                |
| `OnOff`                         | `{ "on": bool }`                                                                        | `{ "<device>": { "<paramMap_OnOff>": bool } }`                                                     | `{ "on": bool, "online": true }`                                                       |
| `SetFanSpeed`                   | `{ "fanSpeed": string }` or `{ "fanSpeedPercent": float }`                              | `{ "<device>": { "<paramMap_FanSpeed>": int } }`                                                   | `{ "currentFanSpeedPercent": int, "currentFanSpeedSetting": string, "online": true }`  |
| `ThermostatTemperatureSetpoint` | `{ "thermostatTemperatureSetpoint": float }`                                            | `{ "<device>": { "<paramMap_TemperatureSetting>": float } }`                                       | `{ "thermostatTemperatureSetpoint": float, "thermostatMode": "heat", "online": true }` |

Fan speed named values: `low` = 25%, `medium` = 50%, `high` = 100%.

**Response**:
```json
{
  "requestId": "uuid",
  "payload": {
    "commands": [
      {
        "ids": ["node123.Light1"],
        "status": "SUCCESS",
        "states": { "on": true, "online": true }
      }
    ]
  }
}
```

**Error Response**:
```json
{
  "requestId": "uuid",
  "payload": {
    "commands": [
      {
        "ids": ["node123.Light1"],
        "status": "ERROR",
        "errorCode": "unknownError"
      }
    ]
  }
}
```

**Sequence Diagram**:

```mermaid
sequenceDiagram
    title GVA EXECUTE - Device Control

    participant Google as Google Assistant
    participant Lambda as gva_action Lambda
    participant IoT as AWS IoT Shadow/MQTT

    Google->>Lambda: POST /v1/integrations/gva<br/>{intent: EXECUTE, commands[], Bearer token}
    activate Lambda

    Lambda->>Lambda: Extract user ID from token

    loop For each command > device > execution
        Lambda->>Lambda: Parse device ID (node_id.device_name)
        Lambda->>Lambda: Load node permissions (groupID from customData)
        Lambda->>Lambda: Map GVA command params<br/>to RainMaker params via customData
        Lambda->>IoT: Publish to desired shadow<br/>{device_name: {param: value}}
        IoT->>Lambda: Success
    end

    Lambda->>Google: {commands: [{ids, status, states}]}

    deactivate Lambda
```

### DISCONNECT (Account Unlinking)

Google sends DISCONNECT when the user unlinks their RainMaker account from Google. After unlinking, Google stops sending SYNC/QUERY/EXECUTE intents for that user.

**Request**:
```json
{
  "requestId": "uuid",
  "inputs": [{ "intent": "action.devices.DISCONNECT", "payload": {} }]
}
```

**Process**:
1. Get user ID from the ESP User access token
2. Delete the user's GVA link row from `rmng-user-endpoints` (best-effort: a
   delete failure is logged and the intent still succeeds, since Google has
   already unlinked on its side)
3. Return an empty acknowledgement

**Response**: Per the Google Smart Home spec, the webhook returns an empty JSON body.
```json
{}
```

## Proactive Notifications (Report State)

When a device's state changes (typically a physical interaction or any cloud-side update that the user did not initiate via Google), ESP RainMaker Neo calls Google's HomeGraph **Report State** API to keep the Google Home graph in sync. This is what powers "OK Google, is the light on?" returning the right answer after a wall-switch press.

### Dispatcher integration

Report State is delivered through the shared [notifications dispatcher](notifications.md). Specifically:

- Registered service name: **`gva`**
- Service kind: **user-specific**. The dispatcher resolves the user list for the originating group/subgroup, and `agentUserId = <userID>` is set on each outbound request.
- Trigger: a shadow update where the dispatch event's `notify` map contains a `"gva"` key. Direct notifications (`notify/…` topics) are not supported by the GVA channel — only `shadow_update` events are marshalled.
- GVA is a bespoke adapter — it owns the URI, the service-account JWT auth, and the HomeGraph body shape, rather than reusing the generic webhook scaffold. See [notifications-webhooks.md](notifications-webhooks.md) for context on the two implementation patterns.

### Authentication model

Unlike Alexa, GVA does **not** use per-user OAuth. Report State is authenticated with a single service-account JWT scoped to the HomeGraph API:

- Service-account JSON is stored in SSM (see [Step 4: Create Service Account](#step-4-create-service-account-for-report-state)).
- On each invocation, the SSM parameter is read (with caching), used to mint a JWT, and exchanged for a Google access token scoped to `https://www.googleapis.com/auth/homegraph`.
- One token is reused across all users in a single dispatch — fetched once and applied per-user.

No per-user OAuth **token** is stored for GVA — the `agentUserId` field in the
request body is what Google uses to look up the linked HomeGraph account. There
*is* however a per-user **link row** in `rmng-user-endpoints`
(`integration_id = "gva"`), written on the first `SYNC` and deleted on
`DISCONNECT`, because Google exposes no link/unlink callback. Report State and
Request Sync use that row to decide which users to notify, so a user with no row
receives nothing. The row carries a placeholder OAuth bundle only because the
table requires one on non-push rows; it is never used to authenticate.

### Process

1. A shadow update notification is received with the node's current state and delta.
2. Get node configuration to determine devices and their capabilities.
3. Compute the set of **changed devices** from the delta — only these are reported, matching Google's expectation that Report State carries deltas, not full state.
4. For each changed device, build a HomeGraph state map using the device's traits and current params.
5. If no devices changed (e.g. the delta only touched a non-device shadow field), the marshal step returns an empty result and the dispatcher logs "No changed device states to report, skipping Report State".
6. Mint a HomeGraph access token.
7. For each target user, set `agentUserId = userID` on the request and POST to `https://homegraph.googleapis.com/v1/devices:reportStateAndNotification` with `Authorization: Bearer <homegraph token>`.

### Report State Structure

```json
{
  "requestId": "uuid",
  "agentUserId": "<ESP RainMaker Neo user ID>",
  "payload": {
    "devices": {
      "states": {
        "node123.Light1": {
          "on": true,
          "brightness": 75,
          "online": true
        }
      }
    }
  }
}
```

The keys of `payload.devices.states` are `<nodeID>.<deviceName>` values matching exactly what was returned in the most recent SYNC for the same `agentUserId` (Google requires Report State device IDs to match the SYNC IDs). State values follow Google's per-trait schema (see [Trait Mapping](#trait-mapping)).

### Error semantics

The GVA channel is best-effort:
- A failed token mint aborts the entire dispatch (no users get reported), but only that one notification — other channels (`push`, `alexa`, …) still fire and other notifications are unaffected.
- Per-(user, device) HTTP failures are logged and skipped — the loop continues.
- The dispatcher only sees a hard failure if the marshal step itself fails, in which case `gva` is dropped for that event.

There is no retry, no DLQ, and no delivery receipt — observability comes from logs and the HomeGraph diagnostics in Google Cloud Console.

## Testing

Once the Google Console setup is complete and the service account configuration has been stored via `POST /v1/admin/integrations/gva/configuration` (see [Store Configuration](#store-configuration)):

1. Ensure the RainMaker user account has at least one node with supported devices.
2. On a mobile device, open the **Google Home** app signed in with the Google account linked to your Google Home project.
3. Tap **+** (Add) → **Set up device** → **Works with Google**.
4. Search for and select your integration, then link the RainMaker account through the OAuth/Cognito sign-in flow.
5. After linking, Google issues a SYNC request and your RainMaker devices appear in the Google Home app. You can then control them and issue voice commands.

For sharing an unpublished integration with specific test accounts, see [Beta Sharing](#beta-sharing) below.

## Beta Sharing

To share the Google Home action with specific users for testing:

### Step 1: Add Testers to Google Cloud Project

1. Go to Google Cloud Console: https://console.cloud.google.com -> **IAM & Admin** -> **IAM**
2. Click **Grant Access**
3. Add the tester's Google account email address
4. Assign the **Viewer** role
5. Click **Save**

### Step 2: Tester Activates the Test Action

The tester must perform these steps:

1. Go to Google Home Developer Console: https://console.home.google.com and sign in with the same Google account that was added to the Cloud project
2. Make sure you can see the same project
3. Navigate to the **Test** tab and click **Test** or **Retest** — this step is required to make the action visible in the Google Home app

> **Important**: The test action will remain **hidden** in the Google Home app until the tester completes the above steps. Simply having Cloud IAM access is not enough.

### Step 3: Tester Links the Integration

4. Open the **Google Home app** on their phone
5. Tap **+** (Add) -> **Set up device** -> **Works with Google**
6. Search for your integration — it should appear with a **[test]** tag in front of the name
7. Tap it and **link their account** (sign in via the OAuth/Cognito flow)
8. Devices should now appear in their Google Home app

**Troubleshooting**:
- If the action doesn't appear in the Google Home app, make sure the tester has clicked **Test**/**Retest** in the Developer Console (Step 2.3 above).
- It can take a few minutes for permissions to propagate.
- Try **force closing and reopening** the Google Home app if the integration doesn't appear.