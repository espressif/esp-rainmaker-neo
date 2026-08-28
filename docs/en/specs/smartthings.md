# SmartThings

## What is SmartThings

SmartThings is the Samsung Smart Home integration that allows users to control their RainMaker devices via the Samsung SmartThings app. It implements the [SmartThings Schema](https://developer.smartthings.com/docs/devices/cloud-connected/st-schema) (cloud-to-cloud) connector, handling device discovery, state refresh, device commands, and proactive state callbacks.

## Why it is needed

To enable Samsung SmartThings app control for RainMaker devices. Users link their RainMaker account with SmartThings via OAuth against the ESP User OIDC identity provider and can then discover, control and monitor their devices from the SmartThings app. SmartThings also receives proactive state callbacks when device state changes physically, keeping the SmartThings app in sync.

## Pre-requisites

- User is registered and authenticated
- SmartThings Developer Center account with an Organization
- SmartThings configuration stored via the Config API

## Architecture

Two Lambda functions serve the SmartThings integration:

1. **`st_action`** — Schema App Lambda handling all SmartThings Schema interactions (discovery, state refresh, commands, callback token management). Invoked **directly** by the SmartThings cloud (not via API Gateway) via a resource-based policy granting the SmartThings platform account (`148790070172`) invoke permission.
2. **`st_cfg`** — Configuration API for managing SmartThings credentials (Client ID/Secret in SSM) and registering the SmartThings OAuth callback URLs on the voice-assistant OIDC client. Invoked via API Gateway (deployed with the core `rmng` stacks).

The Schema App is deployed multi-region by the standalone CDK app `cdk/apps/smartthings.py` (Stackfile group `smartthings`, `make deploy-smartthings`): one `rmng-st-core-<region>` stack in each SmartThings geo — `us-east-1` (North America), `eu-west-1` (Europe) and `ap-northeast-1` (Asia Pacific) — each deploying the Lambda `rmng-st-action-<rmng_region>` and exporting its ARN as `STSchemaAppFunctionArn`.

## Configuration

### OIDC VA Client

Account linking runs against the ESP User OIDC identity provider, not Cognito. A confidential OAuth 2.1 client (`va-client`) is seeded into the ESP User client registry (`espuser-oauth-clients`) for voice-assistant account linking:
- **OAuth flow**: Authorization Code Grant (`grant_types`: `authorization_code`, `refresh_token`)
- **Scopes**: `openid`, `email`, `phone`, `profile`
- **Redirect URIs**: registered dynamically by the config API (there are no hardcoded initial values); each POST unions its URIs onto the client's existing set, so the SmartThings, Alexa and Google Voice redirect URIs coexist on the shared `va-client` row. The SmartThings config API registers the three fixed SmartThings callback URLs (`https://c2c-us.smartthings.com/oauth/callback`, `https://c2c-eu.smartthings.com/oauth/callback`, `https://c2c-ap.smartthings.com/oauth/callback`) — they are not part of the request body.
- **Client ID**: `va-client` — the registry client id (also available in SSM `/espuser/base/va-client-id`)
- **Client Secret**: the generated secret for `va-client`, retrievable via the superadmin clients API (`GET /v1/admin/clients?get_secret=true`) or SSM `/espuser/base/va-client-secret`
- **OIDC endpoints**: the `authorization_endpoint` and `token_endpoint` published in the discovery document (`/.well-known/openid-configuration`). Both are served on the ESP User API Gateway base (`EspUserApiUrl`): `<api-url>/oauth2/authorize` and `<api-url>/oauth2/token`.

### SmartThings Developer Center Setup

This must be done before calling the Store Configuration API.

#### Step 1: Create a Schema Cloud Connector

1. Go to [SmartThings Developer Center](https://developer.smartthings.com/)
2. Click **Device Integrations** in the left sidebar
3. Create a **Product** and add a Cloud Connector to it → **ST Schema**. The Product is the
   container the connector lives in, and adding the Schema App to it links the two automatically —
   this is not a device profile, and nothing per-device-type is created here (see
   [Step 5](#step-5-device-handler-types)).
4. Fill in the App Name (e.g., "RMNG Smart Home")
5. **App Icon**: use `assets/smartthings_logo.png` (the Neo logo, 512×512 PNG). SmartThings uses this as the icon shown during account linking (and for the 2x/3x icon URLs).

#### Step 2: Configure Target ARN

The Schema App Lambda is deployed by the standalone `rmng-st-core` stack into each SmartThings region. In rmng-outputs.json the stack is keyed `rmng-st-core-<rmng-region>` and each SmartThings region holds its own `STSchemaAppFunctionArn` under `regions`.

| Field | Value |
|---|---|
| North America | `regions.us-east-1.STSchemaAppFunctionArn` (e.g., `arn:aws:lambda:us-east-1:<account>:function:rmng-st-action-<rmng-region>`) |
| Europe | `regions.eu-west-1.STSchemaAppFunctionArn` |
| Asia Pacific | `regions.ap-northeast-1.STSchemaAppFunctionArn` |

At least one region is required.

> Note: You would need this Lambda deployed in all 3 regions to support users from all locations. If any region is unavailable, the users from there won't be able to link their account.

#### Step 3: Configure Device Cloud Credentials (OAuth)

| Field | Value | Source |
|---|---|---|
| Client ID | `va-client` | The OIDC client id (SSM `/espuser/base/va-client-id`) |
| Client Secret | The `va-client` secret | Superadmin clients API (`GET /v1/admin/clients?get_secret=true`) or SSM `/espuser/base/va-client-secret` |
| OAuth URL | `<api-url>/oauth2/authorize` | The discovery document's `authorization_endpoint` |
| OAuth Scope | `openid email phone profile` | — |
| Token URL | `<api-url>/oauth2/token` | The discovery document's `token_endpoint` |
| Alert Notification Email | Your platform operator email | — |

#### Step 4: Save and Store SmartThings Credentials

After saving, SmartThings provides you with:
- **SmartThings Client ID** — used by the Schema App to authenticate callback requests
- **SmartThings Client Secret** — used with the Client ID for callback authentication

Store these via the [Config API](#smartthings-config-api):

```bash
curl -X POST https://<ApiGatewayUrl>/v1/admin/integrations/smartthings/configuration -H "Content-Type: application/json" -d '{"client_id": "<SmartThings Client ID>", "client_secret": "<SmartThings Client Secret>"}'
```

(The request must be authenticated as a super admin — SigV4-signed like the other admin configuration APIs.)

#### Step 5: Device Handler Types

A Schema App picks each device's presentation from the `deviceHandlerType` in the discovery
response. Every type we emit is one of the pre-made `c2c-*` handlers, which SmartThings
[provides](https://developer.smartthings.com/docs/devices/cloud-connected/device-handler-types) —
nothing has to be authored for them, and a connector discovers and controls devices with no
Device Profile of its own.

A **custom Device Profile** is needed only when no pre-made handler covers the capabilities; its
profile ID then goes in `deviceHandlerType` in place of a `c2c-*` name. The Product created in
[Step 1](#step-1-create-a-schema-cloud-connector) is just the connector's container — it carries
no per-device-type configuration of its own.

`getDeviceHandlerType` in `src/smartthings/handle_discovery.go` derives the type from the node's
parameter types, so this table is the mapping that code implements.

| Device Type | Handler Type | Capabilities |
|---|---|---|
| Light (on/off only) | `c2c-switch` | Switch, Health Check |
| Dimmable Light | `c2c-dimmer` | Switch, Switch Level, Health Check |
| RGB Light | `c2c-rgb-color-bulb` | Switch, Switch Level, Color Control, Health Check |
| CCT Light | `c2c-color-temperature-bulb` | Switch, Switch Level, Color Temperature, Health Check |
| RGBW Light | `c2c-rgbw-color-bulb` | Switch, Switch Level, Color Control, Color Temperature, Health Check |
| Fan | `c2c-fan` | Switch, Fan Speed, Health Check |
| Thermostat | `c2c-thermostat` | Thermostat Mode, Thermostat Heating Setpoint, Health Check |
| Switch | `c2c-switch` | Switch, Health Check |

#### Step 6: Test with Schema Invitations

Before publishing to the SmartThings catalog:

1. In the Schema Cloud Connector settings, generate an **invitation link**
2. Share the link with test users
3. Test users open the link on their phone and install the integration
4. Monitor `interactionResult` CloudWatch logs for errors reported by SmartThings

#### Step 7: WWST Certification (Production)

For production catalog listing:

1. Complete end-to-end testing using Schema Invitations
2. Apply for [Works with SmartThings (WWST) certification](https://developer.smartthings.com/docs/certification/overview)
3. Ensure all interaction types pass SmartThings validation
4. Submit for review

## SmartThings Config API

### Store Configuration

**API**: `POST /v1/admin/integrations/smartthings/configuration`

**Authorization**: Super admin only (SigV4); other callers receive `403`.

**Request**:
```json
{
  "client_id": "<SmartThings Client ID>",
  "client_secret": "<SmartThings Client Secret>"
}
```

**Validation**:
- `client_id` must be non-empty and ≤256 characters
- `client_secret` must be non-empty and ≤256 characters
- Validation failures return `400` with `{"message": "<what failed>", "field": "<field name>"}`

**Process**:
1. Validate super-admin authorization
2. Validate input fields
3. Register the three SmartThings callback URLs on the OIDC `va-client` registry row, unioning them onto its existing set (env var: `OIDC_VA_CLIENT_ID`, value `va-client`)
4. Store Client ID at SSM `/rmng/smartthings/client_id` (String)
5. Store Client Secret at SSM `/rmng/smartthings/client_secret` (SecureString)

**Response**:
```json
{
  "message": "SmartThings configuration stored successfully"
}
```

### Get Configuration

**API**: `GET /v1/admin/integrations/smartthings/configuration`

**Response** (the client secret is never returned):
```json
{
  "client_id": "<SmartThings Client ID>"
}
```

### Delete Configuration

**API**: `DELETE /v1/admin/integrations/smartthings/configuration`

**Process**:
1. Delete `/rmng/smartthings/client_id` and `/rmng/smartthings/client_secret` from SSM

**Response**:
```json
{
  "message": "SmartThings configuration deleted successfully"
}
```

Any HTTP method other than GET/POST/DELETE returns `405`.

## Per-Node Enable/Disable

Each node has an `st_en` service-data key in the node details table indicating SmartThings is enabled for it — the same pattern as Alexa (`alexa_en`/`getAlexaEn`) and GVA (`gva_en`/`getGVAEn`).

- **Default**: `false` (disabled)
- **Auto-enable on discovery**: when a `discoveryRequest` returns a node's devices, the Schema App sets `st_en: true` and pushes the status to the device via the `getSTEn` node event (`{"event": ["getSTEn"], "data": {"getSTEn": {"enabled": true}}}`). Discovery is **not** filtered by `st_en`; every qualifying device is returned and enabled.
- **What the flag drives**: once `st_en` is true, the device firmware includes `"smartthings"` in the `notify` map of its shadow updates, which is what triggers proactive state callbacks (see below). A node with `st_en: false` simply never emits SmartThings notifications.

## Schema Interactions

### Authentication

SmartThings sends an ESP User access token (issued to the voice-assistant client at account linking) in `request.authentication.token`. The Lambda validates it and extracts the user ID. For invalid/expired/missing tokens, the response includes `isAuthenticated: false`.

Interactions that **require** authentication: `discoveryRequest`, `stateRefreshRequest`, `commandRequest`

Interactions that **do not** require authentication: `grantCallbackAccess` (validated separately via the code exchange), `integrationDeleted`, `interactionResult`

### Device ID Format

Devices are identified as `<nodeID>#<deviceName>` (e.g., `ABC123#Light`). The separator is `#` because it cannot appear in a node ID, so the node is always recoverable even when the device name contains one. This composite ID maps a SmartThings device back to a specific RainMaker node and device within that node.

### Device Cookie

Each device in a `discoveryResponse` carries a `deviceCookie` holding its RainMaker param name per param type, e.g. `{"esp.param.power": "Power", "esp.param.brightness": "Brightness"}`. SmartThings stores it and echoes it back on every `commandRequest`, so a command resolves the param to publish without re-reading the node config.

`stateRefreshRequest` does **not** carry the cookie, so state refresh still reads the shadow and config as before.

The cookie is only ever a param-name lookup. Authorization is unaffected: the caller is still resolved from the token and checked against their groups for every device, so a request cannot reach a node by carrying a crafted cookie. A device SmartThings knows about has always been through discovery and therefore carries a cookie; a command that arrives without one reports the missing param as a `DEVICE-ERROR`.

### Interaction Types

| Interaction Type | Handler | Description |
|---|---|---|
| `discoveryRequest` | `HandleDiscovery` | Device discovery |
| `stateRefreshRequest` | `HandleStateRefresh` | Current state query |
| `commandRequest` | `HandleCommand` | Device control |
| `grantCallbackAccess` | `HandleGrantCallbackAccess` | Store callback tokens |
| `integrationDeleted` | `HandleIntegrationDeleted` | Remove callback tokens |
| `interactionResult` | `HandleInteractionResult` | Log errors from SmartThings |

### Capability Mapping

| RainMaker Param Type | SmartThings Capability | Attribute | Value Range |
|---|---|---|---|
| `esp.param.power` | `st.switch` | `switch` | on/off |
| `esp.param.brightness` | `st.switchLevel` | `level` | 0-100 |
| `esp.param.hue` | `st.colorControl` | `hue` | 0-360 |
| `esp.param.saturation` | `st.colorControl` | `saturation` | 0-100 |
| `esp.param.cct` | `st.colorTemperature` | `colorTemperature` | 2200-6500 K |
| `esp.param.speed` | `st.fanSpeed` | `fanSpeed` | 0-100 |
| `esp.param.temperature` / `esp.param.setpoint-temperature` | `st.thermostatHeatingSetpoint` | `heatingSetpoint` | 0-100 °C |
| _(always included)_ | `st.healthCheck` | `healthStatus` | online/offline |

## Proactive State Callbacks

### Dispatcher integration

State callbacks are delivered through the shared [notifications dispatcher](notifications.md). Specifically:

- Registered service name: **`smartthings`**
- Service kind: **user-specific**. The dispatcher resolves the user list for the originating group/subgroup before fanning out per-user.
- Trigger: a shadow update where the dispatch event's `notify` map contains a `"smartthings"` key. Direct notifications (`notify/…` topics) are not supported by the SmartThings channel — only `shadow_update` events are marshalled.
- SmartThings is a bespoke adapter — it owns the per-user callback URL, the ST Schema token-refresh flow, and the `stateCallback` envelope shape, rather than reusing the generic webhook scaffold. See [notifications-webhooks.md](notifications-webhooks.md) for context on the two implementation patterns.

### Obtaining callback tokens (`grantCallbackAccess`)

After a user links their account, SmartThings sends a `grantCallbackAccess` interaction containing an authorization `code`, the SmartThings `clientId`, and the callback URLs (`oauthToken`, `stateCallback`). The Schema App exchanges the code for callback access/refresh tokens by POSTing an `accessTokenRequest` to the `oauthToken` URL, authenticating with the `clientId` **plus the Client Secret read from SSM** (`/rmng/smartthings/client_secret`) — `grantCallbackAccess` itself does **not** include the secret. The resulting tokens are stored per-user in the `rmng-user-endpoints` table as a `smartthings` integration row (see [Token storage](#token-storage)).

> SmartThings sends `grantCallbackAccess` automatically as part of account linking. `requestGrantCallbackAccess: true` on a `discoveryResponse` is **only** for re-requesting tokens after a refresh failure — do not set it during normal discovery, or SmartThings will reject the link.

### Token storage

Per-user SmartThings callback credentials live in the same `rmng-user-endpoints` DynamoDB table that backs push registration, keyed by user and endpoint:

| Column                 | Value for SmartThings                                                                  |
| ---------------------- | -------------------------------------------------------------------------------------- |
| `user_id`              | partition key, the user identity                                                        |
| `integration_endpoint` | sort key, `smartthings#<endpoint_id>`                                                   |
| `integration_id`       | `smartthings`                                                                           |
| `endpoint_id`          | the `stateCallback` URL (base64url-encoded) — the endpoint's natural identifier, so re-linking against the same regional endpoint overwrites the same row |
| `integration_token`    | nested map: `{access_token, refresh_token, access_expires_at}`                          |
| `token_callback_url`   | the `oauthToken` URL captured at `grantCallbackAccess` time, used for later token refreshes. Optional table attribute, introduced for SmartThings — its token endpoint arrives at link time rather than being a fixed constant |

SmartThings sends both URLs per region, so the stored values differ by where the user linked from:

| Region | `stateCallback` / `oauthToken` host |
|---|---|
| North America | `https://c2c-us.smartthings.com` |
| Europe | `https://c2c-eu.smartthings.com` |
| Asia Pacific | `https://c2c-ap.smartthings.com` |

Both arrive in the `grantCallbackAccess` payload and are stored per user, so no path is hardcoded — only the three OAuth redirect URIs registered by the config API are (`https://c2c-<region>.smartthings.com/oauth/callback`, see `st_cfg_main.go`).

On `integrationDeleted`, the user's `smartthings` rows are removed.

### Sending state callbacks

When a device shadow updates (and the device emits `"smartthings"` in its `notify` map):

1. The shadow update triggers the notifications dispatcher
2. The SmartThings notification service marshals the update to SmartThings capability states — only devices present in the delta are reported; if no device changed, the marshal step returns an empty result and the callback is skipped
3. For each target user, for each stored callback endpoint:
   - Check token expiry → refresh via the stored refresh token against the row's `token_callback_url` if needed (requires `ssm:GetParameter` on `/rmng/smartthings/*` for the client credentials)
   - POST the state callback to the user's stored `stateCallback` URL
4. Always includes `st.healthCheck` with connectivity status (`online`/`offline`)

The callback body must be a full ST Schema envelope — `headers` (with `interactionType: "stateCallback"`, `schema: "st-schema"`, `version: "1.0"`, `requestId`), `authentication` (the user's callback access token as a Bearer token), and `deviceState`. Omitting the `headers`/`authentication` envelope causes SmartThings to reject the callback with `BAD-REQUEST "Invalid or unspecified schema"`.

### Error semantics

The SmartThings channel is best-effort:
- A user with no stored callback tokens is skipped and logged — other users still get their callback.
- A failed token refresh or a non-2xx response for one (user, endpoint) pair is logged and skipped — the loop continues.
- The dispatcher only sees a hard failure if the marshal step itself fails, in which case `smartthings` is dropped for that event but other channels (push, Alexa, GVA, …) still fire.

There is no retry, no DLQ, and no delivery receipt — observability comes from logs and the `interactionResult` errors SmartThings reports back to the Schema App.

## Adding Support for New Device Types

When a new RMNG device type needs SmartThings support, changes are required in both the codebase and the SmartThings Developer Center.

### Code Changes

1. **Add param type constant** in `src/smartthings/utils.go`:
   ```go
   ParamTypeNewParam = "esp.param.new-param"
   ```

2. **Update `GetSTCapabilities`** in `src/smartthings/utils.go` — add a case mapping the new param type to the SmartThings capability:
   ```go
   case ParamTypeNewParam:
       capabilitySet[CapabilityNewCapability] = true
   ```

3. **Add capability constant** in `src/smartthings/types.go`:
   ```go
   CapabilityNewCapability = "st.newCapability"
   ```

4. **Update `getDeviceHandlerType`** in `src/smartthings/handle_discovery.go` — add logic to return the correct handler type when the new capability is present.

5. **Update `mapShadowToSTStates`** in `src/smartthings/handle_state_refresh.go` — add a case to map the shadow value to the SmartThings attribute format.

6. **Add command handler** in `src/smartthings/handle_command.go` — add a case in `executeSingleCommand` and implement the command-to-MQTT mapping function.

7. **Update tests** — add test cases for the new capability in unit tests.

8. **Build and deploy**:
   ```bash
   make deploy-smartthings
   ```

### SmartThings Console Changes

Usually none: pick a handler type from the [table above](#step-5-device-handler-types) that covers
the new capability and the app renders it. Only if no pre-made `c2c-*` handler covers it do you
create a custom Device Profile in the Workspace and return its profile ID as `deviceHandlerType`.

### Capability Reference

SmartThings capabilities are documented at: https://developer.smartthings.com/docs/devices/capabilities/capabilities-reference

Common capabilities for IoT devices:
- `st.switch` — on/off control
- `st.switchLevel` — dimming (0-100)
- `st.colorControl` — hue/saturation
- `st.colorTemperature` — CCT in Kelvin
- `st.fanSpeed` — fan speed percentage
- `st.thermostatMode` — heating/cooling mode
- `st.thermostatHeatingSetpoint` — temperature setpoint
- `st.healthCheck` — device connectivity (always included)
- `st.doorControl` — open/close
- `st.lock` — lock/unlock
- `st.motionSensor` — motion detected
- `st.contactSensor` — open/closed

## Testing

Until the connector is WWST-certified it is a **test integration**, so Developer Mode is required in the SmartThings app to see it — the same idea as an Alexa beta invite or a Google Home test action.

> **Important**: Add at least **one device** to the RainMaker account *before* linking, otherwise SmartThings links against an empty home and you will need to unlink and relink later.

### Enable Developer Mode

1. Open the SmartThings app (logged in with your Samsung account)
2. Go to **Menu / Settings** → tap **About SmartThings**
3. Long-press on the "About SmartThings" row for 5–10 seconds
4. Toggle **Developer Mode** to ON when the hidden menu appears
5. Force-close and restart the app to flush cached metadata

### Link Your Test Integration

1. In the app, go to **Devices** tab → tap **+ (Add Device)**
2. Select **Partner Devices** (or "By Brand")
3. Look at the top of the list for **My Testing Devices** (only visible with Developer Mode active)
4. Tap your Schema App integration (e.g., "RMNG Smart Home")
5. Select the **Setup App** row under "My setup apps" first (ensures the OAuth account-linking handshake executes correctly)
6. Sign in with your RainMaker credentials via the ESP User OIDC sign-in page
7. After successful OAuth, SmartThings sends `grantCallbackAccess` and `discoveryRequest` to the Lambda
8. Your RainMaker devices appear in the app (typically within a few seconds), and state changes made from the device or the RainMaker app are reflected back in SmartThings

### Re-trigger Discovery

To refresh the device list after adding new nodes (or to recover from a failed link):
1. Remove the integration from the app
2. Re-add it via **My Testing Devices** (repeats the OAuth + discovery flow)

### End-to-End Verification Walkthrough

Ordered checklist for a developer verifying the whole pipeline on a fresh deployment, with the observable checkpoint at each hop. Prerequisite: a deployed `espuser` + `rmng` stack group and AWS credentials for that account.

1. **Deploy the Schema App stacks.** First time only, bootstrap the three SmartThings regions, then deploy:

   ```bash
   ./scripts/deploy.sh --setup --stack-group smartthings
   make deploy-smartthings
   ```

   *Checkpoint*: `rmng-st-core-<rmng_region>` exists in `us-east-1`, `eu-west-1` and `ap-northeast-1`, and `rmng-outputs.json` (regenerated automatically after the deploy) contains `STSchemaAppFunctionArn` under a `regions` map for each.

2. **Create the connector** in the SmartThings Developer Center ([Developer Center Setup](#smartthings-developer-center-setup), steps 1–3): register the three Lambda ARNs from step 1 as target ARNs and copy the OAuth URLs from the deployment's OIDC discovery document.

3. **Store the SmartThings credentials** via the [Config API](#smartthings-config-api) with a super-admin account. This also registers the three `c2c-*.smartthings.com` callback URLs on the OIDC `va-client` row.

   *Checkpoint*: `GET /v1/admin/integrations/smartthings/configuration` returns the `client_id`; the `espuser-oauth-clients` row for `va-client` lists the three redirect URIs.

4. **Bring up a test user and device.** Use `cli/morpheus.py` to create a user and register a node (see `cli/README.md`), then run the device simulator:

   ```bash
   python3 test/device_sim.py --device <device-id-from-test_config.json>
   ```

   *Checkpoint*: the simulator boots and requests `getSTEn`; before discovery it prints `SmartThings enabled status updated: False`.

5. **Link the integration** in the SmartThings app ([Link Your Test Integration](#link-your-test-integration)), signing in as the user from step 4.

   *Checkpoint*: CloudWatch logs of `rmng-st-action-<rmng_region>` (in the region SmartThings picked for the account's geo) show a `grantCallbackAccess` followed by a `discoveryRequest`; the devices appear in the app; the simulator prints `SmartThings enabled status updated: True` (discovery auto-enables `st_en`).

6. **Control: SmartThings → device.** Toggle the device in the app.

   *Checkpoint*: the simulator prints the received parameter update; the app reflects the commanded state from the `commandRequest` response.

7. **State callback: device → SmartThings.** Change a parameter from the simulator's interactive prompt (or the RainMaker app).

   *Checkpoint*: the simulator's shadow update carries `notify: {smartthings: true, version: N}`; `rmng-notifications` logs show the SmartThings service dispatching a `stateCallback`; the tile in the SmartThings app updates within a few seconds.

8. **Run the integration tests** against the deployment:

   ```bash
   make itest ITEST_ARGS=test/itest/test_smartthings.py
   ```

   This covers config-API CRUD plus discovery/command/state-refresh by invoking each regional Schema App Lambda directly. The Schema tests skip with `No rmng-st-core regions in rmng-outputs.json` if step 1 was missed.
