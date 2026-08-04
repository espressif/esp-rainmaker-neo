# ESP User token endpoints (token, userinfo, revoke, refresh)

> The **authorization-code + PKCE browser flow** is
> [authorize-code-flow.md](authorize-code-flow.md); **upstream-provider federation**
> (Cognito, social) is [federation.md](federation.md). This spec owns the
> issuer-side token endpoints those flows all land on: the refresh-token service,
> `POST /oauth2/token`, `GET /oauth2/userinfo`, and `POST /oauth2/revoke`.

## Refresh tokens

Refresh tokens rotate on every use, per RFC 9700 §4.14.2 (BCP 240) / OAuth 2.1; the grant is RFC 6749 §6. For public clients (mobile / SPA) rotation is effectively required.

**Redeeming a refresh token returns a brand-new one; the old one is spent.** The client trades its refresh token at `/oauth2/token` for a new access token and a new refresh token.

**The token is signed and self-describing, not opaque.** A refresh token is a signed structure carrying `user_id`, `client_id`, `family_id`, and a monotonic `counter`, with an HMAC over those fields (keyed by the ESP User signing secret; RFC 8725 JWT-BCP signing discipline applies to the same key). The server verifies the signature first — a bad signature is rejected without any store read. Because the token carries its own `user_id`, the server never needs a lookup to locate the caller's partition. The fields are signed (tamper-proof) but not encrypted; `user_id` is the internal deterministic id, not PII.

**Family = one login's rotation chain, tracked by a single counter.** One login produces a family (`family_id`); each rotation increments the family's `counter`. The store holds **one row per family** — not one per token — whose value is the current `counter` (plus `user_id`, `scope`, `expires_on`). A presented token is current iff its `counter` equals the row's `counter`; any lower counter is a spent (or replayed) token. Revoked tokens are therefore **never stored** — revocation is the absence or advance of the family row, not a per-token tombstone.

**Rotation is atomic.** Redemption verifies the signature, reads the family row, checks the presented `counter` matches, then advances the counter with a conditional write (`counter = n+1` only `if counter = n`), so concurrent redemptions race and exactly one wins. The winner's new token carries `counter = n+1`; the loser's presented `counter = n` no longer matches and is rejected.

**Replay = theft (RFC 9700 §4.14.2).** Re-presenting a token whose `counter` is below the family's current `counter` is reuse — the chain has already moved on. The server deletes the family row (killing the current token too), writes `refresh_reuse_detected`, and returns `invalid_grant`; the user re-authenticates and the thief is locked out. Clean expiry (DynamoDB TTL) is not theft.

**Revocation is family-wide, and user-wide is one query.** `/oauth2/revoke` (RFC 7009 §2.1: revoking a token MAY revoke the underlying grant) deletes the presented token's family row, ending that login.

**revoking every session for a user** — "sign out everywhere", password reset, compromise response — is a single `Query(user_id)` + delete of all its family rows, with **no GSI**. Access tokens are stateless RS256 JWTs and cannot be revoked server-side (the RFC 7009 §2.2 access-token SHOULD does not apply); they expire on their own within the hour. TODO: add an API for signout later

**Storage**:

- TableName: `espuser-refresh-tokens`
- Key: `user_id` (PK), `client_id#family_id` (SK) — one row per family
- Attributes: `counter` (current rotation counter), `user_id`, `client_id`, `family_id`, `scope`, `expires_on`
- `user_id` (PK): every family for a user shares the partition, so user-wide revoke is one `Query(user_id)`; the signed token carries `user_id`, so the caller always supplies the partition key with no lookup
- `client_id#family_id` (SK): identifies one login's family within the user; a refresh token is always tied to one client
- `counter`: the family's current rotation number; a token is valid iff its signed `counter` equals this. Advanced by a conditional write on rotation; no per-token rows and no spent-token tombstones
- `scope`: carried on the family row so a rotation can re-mint the access/id tokens without a separate lookup
- `expires_on`: DynamoDB TTL attribute; the refresh-token service (not the store) stamps it

The issued token is `base64(user_id | client_id | family_id | counter) . HMAC(secret, ·)`: the server verifies the HMAC, parses the fields, and does a direct `GetItem` on PK `user_id` + SK `client_id#family_id`, then compares the token's `counter` to the row's — no GSI, no per-token storage. The signature (not obscurity) is what makes the token unforgeable.

## Token endpoint (`POST /oauth2/token`)

The standard OAuth 2.0 token endpoint (RFC 6749 §3.2). One endpoint dispatched on `grant_type`; the request is `application/x-www-form-urlencoded`. This slice implements **`refresh_token`** only; `authorization_code`, `client_credentials`, and token-exchange (RFC 8693, native social) are separate later slices and MUST be rejected with `unsupported_grant_type` until built.

### refresh_token grant

Redeems a refresh token for a fresh token set, rotating the refresh token (see [Refresh tokens](#refresh-tokens) for the rotation / reuse-detection mechanics — this section is only the HTTP surface).

**Request** (form-encoded):

```
grant_type=refresh_token
refresh_token=a1b2c3d4.9f8e7d6c...
client_id=rm_mobile
```

> `client_id` is REQUIRED: the token is scoped per client (SK `client_id#refresh_hash`), so the server needs it to locate the token's row. Public clients send no secret; a confidential client additionally authenticates per its `token_endpoint_auth_method`.

**Process**:

1. Parse the form body; require `grant_type=refresh_token`, `refresh_token`, `client_id`.
2. Split the token, look up its row by `(family_id, client_id#refresh_hash)`. A missing/spent/revoked token is `invalid_grant` — and a spent/revoked one revokes the whole family (reuse = theft) and writes `refresh_reuse_detected`.
3. Atomically rotate: mint the next token in the family, mark the old one spent.
4. Re-mint an RS256 access token (1h) and, if the token's stored `scope` includes `openid`, an id token — using the `user_id` + `scope` carried on it.
5. Write `refresh` to the audit log.

**Response** (`200`, `application/json`):

| field | example | notes |
|---|---|---|
| `access_token` | `eyJhbGciOiJSUzI1Ni...` |  |
| `token_type` | `Bearer` |  |
| `expires_in` | `3600` |  |
| `refresh_token` | `a1b2c3d4.5e6f7a8b...` |  |
| `id_token` | `eyJhbGciOiJSUzI1Ni...` |  |
| `scope` | `openid email` |  |

> `refresh_token` is always a new value (the presented one is now spent). `id_token` is present only if `openid` is in the token's scope. `scope` echoes the token's stored scope.

**Errors** — the OAuth token endpoint uses the RFC 6749 §5.2 error object `{ "error": "<code>", "error_description": "<detail>" }`, NOT the API's generic `{ "message": ... }`. `error` is one of the RFC-defined codes below; the OTP endpoints (`/v1/auth/otp/*`) are our own non-OAuth surface and keep `{ "message": ... }`.

- `400 invalid_request` — missing/malformed `grant_type`, `refresh_token`, or `client_id`.
- `400 invalid_grant` — unknown, expired, spent, or revoked refresh token (uniform; no distinction between them, no reuse oracle).
- `400 unsupported_grant_type` — any `grant_type` other than `refresh_token` in this slice.
- `401 invalid_client` — a confidential client that fails its auth method.

- TODO: confirm where the `refresh` audit event is persisted.

## Userinfo endpoint (`GET /oauth2/userinfo`)

The OIDC Core 1.0 §5.3 UserInfo endpoint. A client presents the access token it holds and gets back the end-user's claims — the canonical way to read profile data instead of trusting whatever an id_token happened to carry at login. Protected by the access token itself (bearer), so it needs no separate authorizer. Per §5.3.1 it accepts **both `GET` and `POST`**; on a form `POST` the token may instead ride the `access_token` body field (RFC 6750 §2.2).

**Request**:

```
GET /oauth2/userinfo
Authorization: Bearer <access_token>
```

> Equivalently `POST /oauth2/userinfo` with `Authorization: Bearer …`, or `POST` `application/x-www-form-urlencoded` with `access_token=…`.

**Process**:

1. Extract the bearer access token — from the `Authorization` header, or the `access_token` form field on a `POST`. A missing/blank one is `401` (`WWW-Authenticate: Bearer`).
2. Verify it against the published JWKS (RS256, our issuer) exactly as any resource server would. An invalid/expired/wrong-signature token is `401 invalid_token`.
3. `sub` is the `user_id`. Look the user up in `espuser-user-details`.
4. Return the claims the token's `scope` authorizes — `sub` always; `email` only if `email` (or `profile`) is in scope; `phone_number` only if `phone` is in scope. We hold no other profile attributes in v1.

**Response** (`200`, `application/json`):

| field | example | notes |
|---|---|---|
| `sub` | `9aXb7...` |  |
| `email` | `user@example.com` |  |
| `phone_number` | `+15551234567` |  |

> `sub` is always present; `email` / `phone_number` appear only when the token's scope authorizes them and the user record has them. Contacts are OTP-verified by construction (D14), so no `email_verified` / `phone_number_verified` flags are emitted.

**Errors** — this is a bearer-protected resource endpoint, so failures use the RFC 6750 `WWW-Authenticate` challenge, not the OAuth token-endpoint error body:

- `401 invalid_token` — missing, malformed, expired, or unverifiable access token (`WWW-Authenticate: Bearer error="invalid_token"`).

## Revoke endpoint (`POST /oauth2/revoke`)

The RFC 7009 token-revocation endpoint. RFC 7009 §2.1 permits revoking a token to also revoke **"related tokens and the underlying authorization grant"**, so this endpoint revokes the presented token's **whole family** — the login's entire rotation chain — via `RevokeFamily`. Because rotation makes any earlier token already-spent, a single-token revoke would usually be a near no-op; family revocation is the useful, RFC-endorsed behavior. Theft detection (a *replay*, per RFC 9700 §4.14.2) calls the same `RevokeFamily`.

The one cascade RFC 7009 §2.1 endorses — a refresh token additionally invalidating the access tokens minted from it — does not apply here: access tokens are short-lived RS256 JWTs we do not track (D13), so there is nothing to revoke; they expire on their own within the hour. A presented *access* token is therefore accepted and ignored. There is no introspection.

**The client must identify itself** (RFC 7009 §2.1/§5 → RFC 6749 §3.2.1). A **confidential** client authenticates with its secret via HTTP Basic (`Authorization: Basic base64(client_id:secret)`); a **public** client identifies with `client_id` — either the Basic username or the `client_id` form parameter — and presents no secret (RFC 6749 §3.2.1: a public client is identified, not authenticated). The request-body secret method (`client_secret_post`) is not accepted (§2.3.1 NOT RECOMMENDED). A request with neither Basic nor `client_id` is `400 invalid_request`; a bad/unknown client or a confidential client with a wrong secret is `401 invalid_client`. `client_id` is not used to locate the token (the token alone identifies its row).

**Request** (form-encoded body + Basic header):

```
Authorization: Basic base64(client_id:client_secret)
token=a1b2c3d4.9f8e7d6c...
token_type_hint=refresh_token   (optional)
```

**Process**:

1. Authenticate the client from the Basic header against the registry: an unknown client or a confidential client with a missing/wrong secret is `401 invalid_client` (uniform — no oracle for which clients exist). A registered public client passes with an empty secret; a public client that presents a non-empty secret is rejected.
2. Parse the form body; require `token`. `token_type_hint` is advisory and may be ignored.
3. If `token` parses as one of our refresh tokens, revoke **its whole family** — the login the token belongs to (RFC 7009 §2.1: revoking a token MAY revoke the underlying grant). This ends the session on every device fed by that family. A malformed token or an access token is a no-op (access tokens are stateless RS256 JWTs, not revocable server-side).
4. Always return `200` regardless of whether the token existed (RFC 7009 §2.2 — no oracle; an attacker learns nothing about validity). Only a truly malformed *request* (missing `token`) is `400 invalid_request`.

**Response**: `200` with an empty body (RFC 7009 §2.2).

**Errors** — the endpoint follows RFC 7009 §2.2.1 and reuses the RFC 6749 §5.2 error object:

- `400 invalid_request` — neither Basic credentials nor a `client_id` parameter was supplied.
- `401 invalid_client` — an unknown client, or a confidential client that fails its auth.
- `400 invalid_request` — the `token` parameter is missing.
- Unknown, malformed, expired, or already-revoked tokens are **not** errors — they return `200`.

## Standards reference

- **NIST SP 800-63B** — 6-digit ≥ 20-bit CSPRNG out-of-band secret, hashed, single-use, lockout under the 100-failure ceiling; SMS deprecated as a high-AAL factor.
- **OAuth 2.1 / RFC 9700 (BCP 240)** — code + PKCE for the in-flow path; no implicit, no ROPC, no passwords. Refresh rotation + reuse detection. Direct-token is a documented first-party-only exception.
- **OIDC Core 1.0** — `auth_time` / `nonce` semantics.
- Identifier canonicalization: emails are lowercased before any lookup, dispatch, or
  user creation (one canonical form end to end); E.164 phone numbers have no case.

## FAQs

1. **Does OTP login create a user if none exists?**
   - Yes. A first-time contact is JIT-provisioned; the OTP itself is the proof of verification.

2. **A user has both email and phone — do both work?**
   - Yes. Either resolves to the same `user_id` via the `by-email` / `by-phone` GSI.

3. **Do I get all three tokens?**
   - In-flow returns a redirect; tokens come from the later `/oauth2/token` exchange. Direct-token returns them directly — `id_token` only if `openid` was requested.

4. **Why does in-flow verify return a redirect instead of tokens?**
   - `verify` only proves identity. Consent and code issuance still belong to `/oauth2/authorize`, and tokens must reach the client via the PKCE-bound code exchange, not the browser.

5. **What stops a re-send from overwriting an in-flight code (and resetting the lockout)?**
   - Each initiate mints a new `flow_id`, so a re-send is a new record; it cannot touch the prior challenge's `otp_attempts`.

6. **Is the refresh token a JWT?**
   - No. Access / id tokens are stateless JWTs; the refresh token is an opaque, server-stored string so it can be rotated and revoked.
