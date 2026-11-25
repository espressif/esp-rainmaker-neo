# OAuth Client Registry

## What this is

The admin-only API that registers and manages the OAuth/OIDC **clients** the IdP issues tokens to — the mobile app, the dashboard, the Alexa skill, and any future first- or third-party relying party. It is CRUD over the `espuser-oauth-clients` table: create a client, list them (optionally with their secrets), patch a client, and delete it. This spec is the authoritative record of the client registry as built.

## Why it is needed

Onboarding a new app or changing its redirect URIs must not require a code deploy. The client registry is the runtime configuration surface every OAuth flow reads: `/oauth2/authorize` checks `redirect_uris`/`require_pkce`, `/oauth2/token` authenticates confidential clients against the stored `secret`, and the OTP direct-token flow checks the client exists. These facts are data in the registry, not code.

## Access control

- **Authorizer**: the admin Cognito pool authorizer (`CognitoAuthorizer`), not our RS256 token — clients are configured by ESP admins, who authenticate against Cognito (D2/D4), never by end users.
- **Claim**: the handler verifies the bearer token against the admin user pool's JWKS; a token that pool did not issue is rejected `403`. Admin-pool membership is the whole privilege, so every admin may register clients (D47).
- Path prefix `/v1/admin/clients` (D47 API-surface table).

## Naming, and who owns a client

**`<product>-<surface>`.** The owner must be readable from the id alone.

| | |
|---|---|
| ✅ | `esp-account-dashboard`, `esp-account-itest`, `secvuln-web`, `rainmaker-mobile` |
| ❌ | `user-pool-client`, `billing-service`, `admin-client` — a bare noun says nothing about whose it is |

This is not tidiness. ESP Account's dashboard authenticated as `user-pool-client` for months
because the name gave no hint that the row belonged to RainMaker. Its redirect URIs and its
`allowed_resources` were appended to somebody else's client, which meant RainMaker's client
could request a token for the ESP Account API. Nobody chose that; the name simply did not
say who it was.

**Every client belongs to exactly one product, and products do not share one.** Separate
registrations are what make per-client revocation, scope ceilings and resource allowlists
possible at all -- they cost nothing, because single sign-on is a property of the session
and not of the client (see [sso-sessions.md](sso-sessions.md)).

## Key rules (enforced on write)

These OAuth 2.1 client invariants are enforced at create/patch so the admin UI fails fast:

1. **Client secrets are stored in plaintext and are retrievable.** A confidential client's secret is stored as-is in the row and returned both at Create and by List when the caller passes `get_secret=true`. **This is a deliberate, weaker-than-hashing posture** (a table read exposes usable secrets) chosen so an admin can look a lost secret back up instead of rotating. It is acceptable here only because the table is admin-only and there is no dynamic/third-party client registration; if that changes, move to hashing (secret shown once) or encryption-at-rest. There is no rotate endpoint — to replace a secret, delete and recreate the client.
2. **`redirect_uris` are exact-match strings** — no wildcards, no path-prefix matching (RFC 9700 / OAuth 2.1). Each is validated at write time: absolute, fragment-free, and no wildcards; `https` must carry a host; plain `http` only for loopback (`localhost`, `127.0.0.1`, `::1`) per RFC 8252 §7.3; a private-use scheme must actually address something; and `javascript:`, `data:`, `vbscript:`, `file:`, `blob:` and `about:` are refused outright — they name code or a local file, not a destination. The registry is the last place a mistake here is cheap; every one of those rejections would otherwise register happily and then either never match or send a browser somewhere it should not go.
3. **`grant_types` may not contain `implicit` or `password`** (ROPC) — rejected. `response_types` is `["code"]` only.
4. **`public` clients** may not carry a secret and have `require_pkce` forced `true`. **`confidential`** clients get a generated secret. The `token_endpoint_auth_method` is **derived unless the row states one** — `none` for public, `client_secret_basic` for confidential; see rule 6. (M2M is not a distinct type: it is a confidential client holding the `client_credentials` grant. That grant is **rejected for public clients at write time** — a public client has no credentials to present, so the grant is meaningless for it.)
5. **`allowed_providers` restricts which identity providers a client may be entered
   through.** Absent means **every** provider, not none -- the reverse of
   `allowed_resources` below, deliberately. A resource is an entitlement, so absent must
   fail closed; a provider list is a menu, and an absent menu read as "none" would stop
   every client registered before the field existed from logging in. A blank entry is
   rejected at write time.

   **Enforced in three places, because there are three ways in.** The login page draws only
   the buttons a client admits; `/user/login/federation/start` refuses a provider it does not,
   and that one is the gate -- the URL can be typed, so a menu is not an access control; and
   the SSO shortcut refuses to answer a client from a session established through a provider
   it forbids, or the restriction would hold for the first login of a browser and evaporate
   for every one after it. The provider list fails **open** on an unreadable registry (the gate
   still refuses); the gate fails **closed**.
6. **`post_logout_redirect_uris` get exactly the rules `redirect_uris` get** (rule 2) — same kind of destination, reached the same way, so validating only one of the two would just make the weaker list the one an attacker uses. Exact-match, no wildcards, validated at write
   time -- rule 2 applied to the other list of URLs this server will send a browser to.
   Absent means none, never any. A logout naming an unregistered URI is still **performed**;
   the browser lands on the issuer's own page instead. Refusing to forward is a safe
   outcome; refusing to *sign out* over a cosmetic problem would not be.
7. **`token_endpoint_auth_method` is derived, not stored** — `none` for a public client,
   `client_secret_basic` for a confidential one. A row may state one to override the derived
   value, which is how `private_key_jwt` will be expressed when it arrives.

   Nothing else is admitted "for later". Back-channel logout, RFC 8693 delegation, per-client
   refresh caps and first-party consent-skipping are all planned and none has a column: DynamoDB
   is schemaless, so the step that needs a field adds it then, and a column nothing reads is a
   field reviewers must keep deciding about.

8. **`allowed_resources` are absolute URIs with a host and without a fragment**, validated at write time. RFC 8707 §2 requires only an absolute URI without a fragment; **requiring an authority is ours and is deliberately stricter**, because `https:/api.example.com` (one slash) is a legal absolute URI with an empty host that would register happily and then never match the `https://api.example.com` a client sends. A non-authority form such as `urn:` is therefore rejected; revisit if a deployment needs one. They are the resource identifiers the client may request an access token for; the token's `aud` is the one it asks for. **Absent means none, never any** — otherwise the parameter enforces nothing for exactly the clients nobody configured. Exact string equality, like `redirect_uris` and for the same reason.

   **`default_resource` is at most one of them**: the resource an access token is for when a request names none (RFC 9068 §3). A value outside `allowed_resources` is refused at write time. Absent means a request that names no resource is refused with `invalid_target`; no token is ever addressed to the client id.

9. **`first_party` marks an app we ship ourselves** — the RainMaker app, a mobile client — as opposed to a
   delegated partner registered in the same table (the voice-assistant clients such as `rainmaker-neo-alexa-client`, the MCP proxy
   `mcp-oauth-client`). It decides whether a sign-in needs the person's consent (see
   [authorize-code-flow.md](authorize-code-flow.md#consent--clients-we-do-not-ship)), and it is what account-management endpoints gate on: `GET`/`DELETE /v1/user/sessions` and `/v1/user/connected-apps`
   admit only a first-party client's user token, so a partner delegated *device* control cannot list or end
   a person's sign-ins. **Absent means `false`** — first-party is opt-in, set deliberately at registration
   and never derived, so a client made before this field, or any partner, is correctly not one. Read at the
   account-management edge (not stamped into the token) so revoking it takes effect on the next call.
   Setting it is admin-only, like every field here.

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
| `application_type` | `native` | `web` or `native` (OIDC Dynamic Client Registration 1.0 §2). A native app's code exchange gets a cookie-less session of its own, so a browser sign-out leaves it signed in ([sso-sessions.md](sso-sessions.md)). A native client must be `public` (RFC 8252 §8.4: an app cannot keep a secret), and each of its `redirect_uris` must be a private-use scheme or an `http` loopback address, never `https` (OIDC Dynamic Client Registration 1.0 §2); its `post_logout_redirect_uris` follow rule 2 only. Optional, default `web`; a client registered before the field reads as `web` |
| `first_party` | `true` | marks an app **we ship ourselves** (vs a delegated partner); a first-party client signs in with no consent screen and is the only kind that may call account-management endpoints such as `GET /v1/user/sessions`. Optional, default `false` |
| `logo_uri` | `data:image/png;base64,…` | RFC 7591 §2 logo, shown on the consent screen. Must be a base64 `data:` URI of a PNG, JPEG or WebP image, at most 4 KiB decoded: inline because the sign-in page's CSP is `img-src 'self' data:`, and raster only because SVG can carry script. Optional; without it the screen shows a generic app icon |
> **Scope of this slice.** The body carries only fields with a live consumer today. Reserved for later slices (not accepted yet): `jwks_uri`, `audiences`, `branding_id`, and `token_ttls` — (M2M/`private_key_jwt`, hosted-UI branding, and per-client TTLs are later slices). `allowed_resources`, `default_resource`, `allowed_providers`, `post_logout_redirect_uris` and `logo_uri` are accepted now: RFC 8707 resource indicators, RP-Initiated Logout and the consent screen have landed, and a field the logout endpoint validates against but no API can write is a check that can never pass. They will be added to the schema when their features land; adding fields is Native.

**Process**:
1. Authorize (token verified against the admin Cognito pool).
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

**Full replace** of the mutable fields (client name, redirect URIs, post-logout redirect URIs, grant types, scopes, allowed resources, default resource, allowed providers, `require_pkce`, `first_party`, `logo_uri`): the body is the complete desired state, so an omitted field resets to empty/default. `application_type` is the exception: sent, it replaces the stored type; omitted, it keeps it, so an ordinary edit never turns a native app web. `client_id`, `client_type`, and the secret are immutable and rejected in the body. `client_name` is required. Re-runs the Key-Rule validation on the result. `404` if unknown.

### Delete Client

**API**: `DELETE /v1/admin/clients/{client_id}`

**Hard delete**: permanently removes the client row. Any tokens already issued keep validating until they expire (they are self-contained JWTs), but the client can no longer authorize, exchange, or use OTP. Returns `200`.

## Seeding the current clients (deploy-time)

The clients that exist today are created by a **deploy-time custom resource** in the **base stack** (not the clients-API/core stack). A missing client is created with a conditional `PutItem` on `attribute_not_exists(client_id)`. An existing one only gains the declared fields it lacks (`SET field = if_not_exists(field, value)`), so **a value already on the row, an admin's edit included, is never replaced** (redirects and scopes are only added to, below), and a row that lacks a declared field gets it (e.g. `first_party` on a hand-made `rainmaker-neo-home-app-client`). A client the seed does not declare is never touched: a deployment that has `user-pool-client` or `va-client` rows keeps them exactly as they are, no deploy grants them a resource, and an operator removes them through this API. The secret and resources are never touched by the seed, and `redirect_uris` is only added to (a set `ADD`), so an app that registers its own callback through this API keeps it and the seeded one alongside. A row written outside this API can hold its redirects as a DynamoDB list, which `ADD` cannot extend; the seed writes that row's redirects back as the set union, only if no registry write moved its `updated_at` since the seed read it. A client that declares scopes regains its declared scopes on every seed run (a deploy that changes the client's definition or `SeedVersion`), so an operator may add scopes to it but not remove them: its stored list keeps its order and any scope an admin added, and gains the declared scopes it lacks at the end, under the same `updated_at` condition, so a declared scope removed through this API, such as `offline_access` on an assistant's client, comes back as a removed seeded redirect does. Any other client's scopes are never touched: a client that declares none is created with `openid email phone profile`, which is how the Home app is seeded, and an operator's narrowing of that list stays. The voice assistants' clients and `mcp-oauth-client` declare `openid email phone profile offline_access`, so an assistant link or MCP connector can hold an offline grant. Every seeded client declares its `application_type`, the Home app `native` and the assistants and `mcp-oauth-client` `web`, so a deployed row that predates the field gains it on the next deploy.

Each voice assistant's client is created the same way by that assistant's own stack (`rmng-alexa-cfg-core`, `rmng-gva-core`, `rmng-st-cfg-core`), so a deployment without the assistant carries no client. That seed also writes the client's secret to a SecureString SSM parameter, `/rmng/<assistant>/esp-user-client-secret`, which is how an operator gets it into the assistant's console without reading this API. It is rewritten whenever the seed runs (a deploy that changes the client's definition or `SeedVersion`), so a secret changed through this API is not copied until then. It is never a stack output.

Seed set = ESP-User OIDC clients for the current first-party apps, with `client_id` fixed so the apps and tests keep a stable id. Each is an OAuth 2.1 `authorization_code`/`refresh_token` client:

| client_id | client_type | notable config | resources (default first) |
| --- | --- | --- | --- |
| `rainmaker-neo-home-app-client` | public | `application_type=native`, `require_pkce=true`, `first_party=true`; redirect and post-logout redirect `rainmaker://com.espressif.novahome/success` (the Espressif RainMaker Home app); `logo_uri` the app's icon from `assets/rainmaker_home_logo.png`; the password API's client, published as espuser-base `EspUserClientId` | neo, ESP User |
| `rainmaker-neo-alexa-client` | confidential | created by `rmng-alexa-cfg-core`; a plaintext `secret` is generated; redirects added by Alexa's config API | neo |
| `rainmaker-neo-gva-client` | confidential | created by `rmng-gva-core`; a plaintext `secret` is generated; redirects added by Google Home's config API | neo |
| `rainmaker-neo-smartthings-client` | confidential | created by `rmng-st-cfg-core`; a plaintext `secret` is generated; redirects added by SmartThings' config API | neo |
| `mcp-oauth-client` | confidential | `application_type=web`; a plaintext `secret` is generated; its callback is added by rmng-core | MCP server |

> The registry accepts only `authorization_code`/`refresh_token` grants.

### Resources

An API's names are known only once its stack has deployed it (neo and MCP after ESP User), so each stack grants its own: espuser-base adds every name in `EspUserApiAudiences`, rmng-base `RmngApiAudiences`, rmng-core `McpAudiences` (the execute-api URL and, when a custom domain is mapped, the domain's), to the clients `SEEDED_CLIENT_APIS` (`base_res_constants.py`) names for that API; each voice assistant's stack grants neo's names to its own client when it creates it. Each grant adds the names missing from `allowed_resources` and sets `default_resource` to the API's `…Resource` only on a client that has none; it never removes anything, skips a client an admin deleted, and runs again whenever the names change (a custom domain mapped later). The write is conditioned on the row's `updated_at`, so an admin edit made at the same moment is reread rather than lost.

## Consumers of the registry

- **OTP direct-token** ([auth-flows.md](auth-flows.md)): `POST /v1/auth/otp/initiate` looks the client up in the registry and rejects an **unknown** client with `invalid_client`. **Any registered client may use direct-token OTP** — there is no per-client gate. This is safe because client registration is admin-only (there is no dynamic/third-party registration — RFC 7591 is deferred), so every registered client is one an admin vetted and placed here. (This is broader than the `first_party` flag: a delegated partner such as `rainmaker-neo-alexa-client` is registered-and-trusted for the paths it is granted, yet is *not* first-party, so it is still refused at account-management endpoints. Two different questions — "did we put this client here?" versus "is it our own app?") If third-party or dynamic registration is ever added, a per-client gate must be reintroduced before then.
- **Token / authorize** (later slices): confidential-client auth against the stored `secret`, `redirect_uris`/`require_pkce` enforcement.

## Storage

- TableName: `espuser-oauth-clients`
- **Keys**: `client_id` (PK), no SK.
- Attributes written by this slice: `client_name`, `client_type`, `secret` (plaintext), `redirect_uris`, `grant_types`, `scopes`, `require_pkce`, `first_party`, `created_at`, `updated_at`. `token_endpoint_auth_method` and `response_types` are derived at read time (`response_types` is always `["code"]`), not stored; there is no `status` (delete is a hard delete) and no `has_secret` (implied by `client_type`). The full design also reserves `jwks_uri`, `audiences`, `post_logout_redirect_uris`, `branding_id`, `token_ttls`, `allowed_providers` (federation), `skip_consent` (consent screen), and a per-client direct-token flag for later slices.
- No GSI: keyed by `client_id`; List scans the table.

## Standards reference

- **OAuth 2.1 / RFC 9700 (BCP 240)** — exact redirect match, code+PKCE, no implicit/ROPC. Client invariants enforced on write.
- **RFC 6749 §2** — client types (`public`/`confidential`) and `token_endpoint_auth_method`.
- **RFC 7591/7592 (Dynamic Client Registration)** — explicitly **deferred**; this API is admin-managed, not self-service DCR.
