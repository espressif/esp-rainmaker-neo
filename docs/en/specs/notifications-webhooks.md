# Notifications — Outbound Webhooks

> See [notifications.md](notifications.md) for the dispatcher and service-registry model that this channel plugs into. This page covers only the webhook-specific surface.

## What is a webhook channel

An outbound HTTP POST to a third-party endpoint (Alexa, Google's HomeGraph, …) carrying a per-user notification body. Every webhook channel:

- Is **user-specific** — the dispatcher fans out the notification to each user with access to the originating group, and the channel does one HTTP call per user.
- Authenticates per-user — usually OAuth bearer tokens stored against the user, occasionally service-account JWTs scoped to the project.
- Has its own body schema (Alexa ChangeReport vs. Google Report State).

## Why it is needed

Voice-assistant integrations and any third-party pipeline that wants to react to ESP RainMaker Neo state changes need a push, not a poll. A webhook is the cheapest way: store a per-user OAuth token, POST when something happens.

## Pre-requisites

- A channel for the target platform is registered with the service registry. Current set: `alexa`, `gva`. **The set is fixed at deploy time** — the admin-API surface only configures credentials for existing channels (`POST /v1/integrations/alexa/configuration`, `POST /v1/integrations/gva/configuration`); there is no API to register a brand-new webhook channel, so adding one (Slack, Teams, IFTTT, or any platform whose contract fits the bearer-token model) requires a code change.
- For OAuth-based platforms (Alexa): the user has completed account linking and the resulting refresh-token bundle is stored against the user as one or more endpoint rows (see [Token storage](#token-storage)).
- For service-account-based platforms (GVA): the service-account JSON is stored in SSM at the agreed parameter name; no per-user credential is required.

## Architecture

```
notifications Lambda
   per webhook service:
     for each userID in the resolved list:
       load every endpoint row for (user_id, integration_id)
         — Alexa users may have multiple linked Amazon accounts;
           one row per linked account, each with its own OAuth bundle.
       for each endpoint row:
         refresh access token if expired (writes refreshed copy back to the same row)
         build the per-user body (inject the access token / endpoint_id where needed)
         POST body to platform endpoint
              \-- Authorization: Bearer <accessToken>
              \-- Content-Type:  application/json
```

## Token storage

Per-user OAuth bundles are stored in the `rmng-user-endpoints` DynamoDB table — the **same table used for push device endpoints**. The row's `integration_id` discriminates push vs OAuth-style rows, and the composite sort key (`integration_endpoint = "<integration_id>#<endpoint_id>"`) allows multiple endpoints per user per integration.

The OAuth fields live in a **nested `integration_token` map**: `integration_token.{access_token, refresh_token, access_expires_at, token_type, region}`. `region` records the AWS region at link time and selects the Alexa event gateway.

| Column                                                             | Push rows                                    | OAuth (webhook) rows                                                                                                                                                                                                     |
| ------------------------------------------------------------------ | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `integration_token` (map: `access_token`, `refresh_token`, `access_expires_at`, `token_type`, `region`) | absent                    | the OAuth bundle, nested under this one attribute                                                                                                                                                                        |
| `endpoint_id`                                                      | SNS Platform Endpoint ARN                    | Alexa: Amazon user_id from LWA `/user/profile`. GVA: today the `integration_id` (one row per user — see [notifications.md](notifications.md#endpoint-identity-per-integration) for the multi-account endpoint-identity model). |
| `integration_endpoint` (SK)                                        | `<integration_id>#<sns-endpoint-arn>`        | `<integration_id>#<endpoint_id>` (per type — see below)                                                                                                                                                                  |
| `integration_id`                                                   | `APNS_<bundle>`, `GCM_<project>`             | `alexa`, `gva`, …                                                                                                                                                                                                        |
| `locale`                                                           | optional locale code                         | optional                                                                                                                                                                                                                 |
| `sns_endpoint_arn`                                                 | SNS Platform Endpoint ARN                    | empty                                                                                                                                                                                                                    |
| `user_id` (PK)                                                     | Cognito identity                             | Cognito identity                                                                                                                                                                                                         |

See [notifications-push.md](notifications-push.md#storage-model) for the full schema.

## Token refresh flow

Before each outbound POST (one POST per `(user_id, integration_id, endpoint_id)` row):

1. Look up the exact endpoint row for `(user_id, integration_id, endpoint_id)`. Missing → return error.
2. Read the OAuth fields off the row's nested `integration_token` attribute; DynamoDB unmarshals the map straight into the struct.
3. If the current time is past `access_expires_at`:
   - Call the platform's refresh endpoint with the stored `refresh_token` (mechanism is adapter-specific — usually a POST to the OAuth `/token` endpoint).
   - Update `integration_token` with the new access/refresh tokens and `access_expires_at = now + expires_in`.
   - `PutItem` the same row back.
4. Return the live access token to the caller.

Refresh, update, and read-back of the latest token for a given `(user_id, integration_id, endpoint_id)` are handled as a single operation.

Notes and limitations:
- If the platform's refresh response omits `expires_in`, the new token is recorded with `expires_at = now`, so it is treated as already expired and refreshed again on next use.
- Refresh failures are handled per endpoint — the per-endpoint loop catches them and continues to the next endpoint, so a single stale linked account does not block delivery to the user's other linked accounts.

## Send path

The dispatcher hands every webhook channel the marshalled notification and the user list. Every channel follows the same per-user per-endpoint shape:

1. Load every OAuth-style endpoint row for `(user_id, integration_id)`. For Alexa this returns one row per Amazon account the user has linked; for GVA today it returns at most one row.
2. For each endpoint row:
   - Refresh the access token if expired (see [Token refresh flow](#token-refresh-flow)). On failure: log, skip this endpoint, continue with the user's other endpoints.
   - Apply any per-endpoint transformation the channel needs — e.g. inject a user-identifying field into the body, or (for Alexa) slot the access token into `event.endpoint.scope.token`.
   - POST the body with `Content-Type: application/json` and `Authorization: Bearer <accessToken>`. On non-2xx: log, skip, continue.

There is no retry on non-2xx — the channel is best-effort and per-endpoint failures are only visible in logs.

## Body schemas

### Default webhook body

Shadow update:
```text
{
  "state":             { ... reported state ... },
  "node_id":           "abcd1234",
  "topic_name":        "params-g1abc",
  "notification_type": "shadow_update"
}
```

Direct:
```text
{
  "notify_data":       { ... },
  "node_id":           "abcd1234",
  "topic_name":        "notify-g1abc",
  "notification_type": "direct"
}
```

Per-user fields can be added or rewritten on top of this base shape before the POST goes out.

### Alexa (`alexa`)

A smart-home `ChangeReport` event. The adapter slots the per-user access token into `event.endpoint.scope.token` before posting.

```text
{
  "event": {
    "header":   { "namespace": "Alexa", "name": "ChangeReport", ... },
    "endpoint": { "scope": { "type": "BearerToken", "token": "<user access token>" }, "endpointId": "..." },
    "payload":  { "change": { ... } }
  },
  "context": { "properties": [ ... ] }
}
```

Posted to the regional Alexa event gateway. Alexa Smart Home skills run in three AWS regions, and the gateway is selected from the region recorded on the user's link row: `us-east-1` → `https://api.amazonalexa.com/v3/events`, `eu-west-1` → `https://api.eu.amazonalexa.com/v3/events`, `us-west-2` → `https://api.fe.amazonalexa.com/v3/events`. A user linked in any other region has no gateway and ChangeReport delivery fails for them. Tokens are refreshed against `https://api.amazon.com/auth/o2/token` using stored client credentials from [the Alexa config](alexa.md).

### GVA (`gva`)

Google HomeGraph `ReportStateAndNotification` request. Authenticated with a service-account JWT (no per-user OAuth). The service-account JSON is read once from SSM and used to mint a Google access token scoped to HomeGraph. See [gva.md](gva.md) for the body schema and HomeGraph specifics.

## Error handling

Same as push: best-effort, per-user failures are logged and swallowed, the dispatcher only sees a hard failure if the marshal step fails. There is no retry, no DLQ, no delivery receipt — observability is logs and downstream platform dashboards.
