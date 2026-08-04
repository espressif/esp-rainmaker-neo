# Admin — OAuth Client Registry

## What this is

The superadmin-only API that registers and manages the OAuth/OIDC **clients** the IdP issues tokens to — the mobile app, the dashboard, the Alexa skill, and any future first- or third-party relying party. It is CRUD over the `espuser-oauth-clients` table: create a client, list them (optionally with their secrets), patch a client, and delete it. This spec is the authoritative record of the client registry as built.

## Why it is needed

Onboarding a new app or changing its redirect URIs must not require a code deploy. The client registry is the runtime configuration surface every OAuth flow reads: `/oauth2/authorize` checks `redirect_uris`/`require_pkce`, `/oauth2/token` authenticates confidential clients against the stored `secret`, and the OTP direct-token flow checks the client exists. These facts are data in the registry, not code.

## Access control

- **Authorizer**: the admin Cognito pool authorizer (`CognitoAuthorizer`), not our RS256 token — clients are configured by ESP admins, who authenticate against Cognito (D2/D4), never by end users.
- **Claim**: the handler additionally requires `custom:super_admin == true` on the token; a plain admin is rejected `403`. Client registration is a superadmin operation (D47).
- Path prefix `/v1/admin/clients` (D47 API-surface table).

## Key rules (enforced on write)

These OAuth 2.1 client invariants are enforced at create/patch so the admin UI fails fast:

1. **Client secrets are stored in plaintext and are retrievable.** A confidential client's secret is stored as-is in the row and returned both at Create and by List when the caller passes `get_secret=true`. **This is a deliberate, weaker-than-hashing posture** (a table read exposes usable secrets) chosen so an admin can look a lost secret back up instead of rotating. It is acceptable here only because the table is superadmin-only and there is no dynamic/third-party client registration; if that changes, move to hashing (secret shown once) or encryption-at-rest. There is no rotate endpoint — to replace a secret, delete and recreate the client.
2. **`redirect_uris` are exact-match strings** — no wildcards, no path-prefix matching (RFC 9700 / OAuth 2.1).
3. **`grant_types` may not contain `implicit` or `password`** (ROPC) — rejected. `response_types` is `["code"]` only.
4. **`public` clients** may not carry a secret and have `require_pkce` forced `true`. **`confidential`** clients get a generated secret. The `token_endpoint_auth_method` is **derived, not stored** — `none` for public, implied by the secret for confidential. (M2M is not a distinct type: it is a confidential client with the `client_credentials` grant, which arrives with the token-endpoint M2M slice — `client_credentials` is not an accepted grant yet.)

## APIs

All requests carry the admin Cognito token in `Authorization`. Errors use the API's generic `{ "message": ... }` shape (these are admin config endpoints, not an OAuth protocol surface).

### Create Client

**API**: `POST /v1/admin/clients`

**Request**:
| field | example | notes |
|---|---|---|
| `client_name` | `RainMaker Mobile` |  |
| `client_type` | `public` |  |
| `redirect_uris` | `["com.espressif.rainmaker://callback"]` | exact-match set; custom schemes allowed for native apps |
| `grant_types` | `["authorization_code", "refresh_token"]` |  |
| `response_types` | `["code"]` |  |
| `scopes` | `["openid", "profile", "email", "phone"]` |  |
| `require_pkce` | `true` |  |
> **Scope of this slice.** The body carries only fields with a live consumer today. Reserved for later slices (not accepted yet): `jwks_uri`, `audiences`, `post_logout_redirect_uris`, `branding_id`, and `token_ttls` — (M2M/`private_key_jwt`, RFC 8707 resource indicators, RP-initiated logout, hosted-UI branding, and per-client TTLs are later slices). They will be added to the schema when their features land; adding fields is Native.

**Process**:
1. Authorize (admin Cognito token + `custom:super_admin`).
2. Validate the Key Rules above; reject on the first violation with `400` and a specific message.
3. Generate an opaque `client_id` (a caller-supplied `client_id` is honored for the seed path so `rm_mobile` etc. are stable — collision-checked with a conditional write).
4. For `confidential`: generate a high-entropy secret and store it (plaintext) on the row.
5. `PutItem` conditional on `attribute_not_exists(client_id)`; stamp `created_at`/`updated_at`.

**Response** (`201`; `client_secret` present only for confidential):
| field | example | notes |
|---|---|---|
| `client_id` | `rm_mobile` |  |
| `client_secret` | `s3cr3t...` |  |
| `client_type` | `public` |  |

### List Clients

**API**: `GET /v1/admin/clients`

Returns every client. By default the `secret` is omitted; pass **`?get_secret=true`** to include each confidential client's plaintext `client_secret` in the rows (this is how a lost secret is recovered — there is no per-client Get and no rotate). Supports `page_size` / pagination like other list endpoints. Whether a client has a secret is implied by `client_type` (`confidential` ⇒ yes, `public` ⇒ no).

### Update Client

**API**: `PUT /v1/admin/clients/{client_id}`

**Full replace** of the mutable fields (client name, redirect URIs, grant types, scopes, `require_pkce`): the body is the complete desired state, so an omitted field resets to empty/default. `client_id`, `client_type`, and the secret are immutable and rejected in the body. `client_name` is required. Re-runs the Key-Rule validation on the result. `404` if unknown.

### Delete Client

**API**: `DELETE /v1/admin/clients/{client_id}`

**Hard delete**: permanently removes the client row. Any tokens already issued keep validating until they expire (they are self-contained JWTs), but the client can no longer authorize, exchange, or use OTP. Returns `200`.

## Seeding the current clients (deploy-time)

The clients that exist today are created by a **deploy-time custom resource** in the **base stack** ([esp_user_base_stack.py](../../esp_user_base_stack.py)) (not in the clients-API/core stack). Each create is a conditional `PutItem` on `attribute_not_exists(client_id)`, so **a client that already exists is left untouched** — re-deploys never clobber a client an admin has since edited.

Seed set = ESP-User OIDC clients for the current first-party apps, with `client_id` fixed so the apps and tests keep a stable id. Each is an OAuth 2.1 `authorization_code`/`refresh_token` client:

| client_id | client_type | notable config |
| --- | --- | --- |
| `user-pool-client` | public | `authorization_code`+`refresh_token`, `require_pkce=true` |
| `va-client` | confidential | `authorization_code`; a plaintext `secret` is generated |

> The registry accepts only `authorization_code`/`refresh_token` grants.

## Consumers of the registry

- **OTP direct-token** ([auth-flows.md](auth-flows.md#direct-token-otp-native-first-party)): `POST /v1/auth/otp/initiate` looks the client up in the registry and rejects an **unknown** client with `invalid_client`. **Any registered client may use direct-token OTP** — there is no per-client gate. This is safe because client registration is superadmin-only (there is no dynamic/third-party registration — RFC 7591 is deferred), so every registered client is first-party by construction. If third-party or dynamic registration is ever added, a per-client gate must be reintroduced before then.
- **Token / authorize** (later slices): confidential-client auth against the stored `secret`, `redirect_uris`/`require_pkce` enforcement.

## Storage

- TableName: `espuser-oauth-clients`
- **Keys**: `client_id` (PK), no SK.
- Attributes written by this slice: `client_name`, `client_type`, `secret` (plaintext), `redirect_uris`, `grant_types`, `scopes`, `require_pkce`, `created_at`, `updated_at`. `token_endpoint_auth_method` and `response_types` are derived at read time (`response_types` is always `["code"]`), not stored; there is no `status` (delete is a hard delete) and no `has_secret` (implied by `client_type`). The full design also reserves `jwks_uri`, `audiences`, `post_logout_redirect_uris`, `branding_id`, `token_ttls`, `allowed_providers` (federation), `skip_consent` (consent screen), and a per-client direct-token flag for later slices.
- No GSI: keyed by `client_id`; List scans the table.

## Standards reference

- **OAuth 2.1 / RFC 9700 (BCP 240)** — exact redirect match, code+PKCE, no implicit/ROPC. Client invariants enforced on write.
- **RFC 6749 §2** — client types (`public`/`confidential`) and `token_endpoint_auth_method`.
- **RFC 7591/7592 (Dynamic Client Registration)** — explicitly **deferred**; this API is admin-managed, not self-service DCR.
