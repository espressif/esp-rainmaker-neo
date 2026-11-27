# Token and other OAuth 2.0 Flows

> The **authorization-code + PKCE browser flow** is
> [authorize-code-flow.md](authorize-code-flow.md); **upstream-provider federation**
> (Cognito, social) is [federation.md](federation.md). This spec owns the
> issuer-side token endpoints those flows all land on: the refresh-token service,
> `POST /oauth2/token`, `GET /oauth2/userinfo`, and `POST /oauth2/revoke`. Which API a
> token is minted for is [resource-indicators.md](resource-indicators.md); the browser
> session that lets a second application skip the login is
> [sso-sessions.md](sso-sessions.md).

## Refresh tokens

Refresh tokens rotate on every use, per RFC 9700 §4.14.2 (BCP 240) / OAuth 2.1; the grant is RFC 6749 §6. For public clients (mobile / SPA) rotation is effectively required.

**Redeeming a refresh token returns a brand-new one; the old one is spent.** The client trades its refresh token at `/oauth2/token` for a new access token and a new refresh token.

**The token is signed and self-describing, not opaque.** A refresh token is a signed structure carrying `user_id`, `client_id`, `family_id`, and a monotonic `counter`, with an HMAC-SHA256 over those fields. The HMAC key is a dedicated secret in SSM (`ESPUSER_REFRESH_SECRET_PARAM`), separate from the RS256 access-token signing key. The server verifies the signature first — a bad signature is rejected without any store read. Because the token carries its own `user_id`, the server never needs a lookup to locate the caller's partition. The fields are signed (tamper-proof) but not encrypted; `user_id` is the internal account id, not PII.

**Family = one login's rotation chain, tracked by a single counter.** One login produces a family (`family_id`); each rotation increments the family's `counter`. The store holds **one row per family** — not one per token — whose value is the current `counter` (plus the login's `scope`, `resources`, `sid`, `auth_time` and `expires_on`). A presented token is current iff its `counter` equals the row's `counter`; any lower counter is a spent (or replayed) token. Revoked tokens are therefore **never stored** — revocation is the absence or advance of the family row, not a per-token tombstone.

**Rotation is atomic.** Redemption verifies the signature, reads the family row, checks the presented `counter` matches, then advances the counter with a conditional write (`counter = n+1` only `if counter = n`), so concurrent redemptions race and exactly one wins. The winner's new token carries `counter = n+1`; the loser's presented `counter = n` no longer matches and is rejected.

**Replay = theft (RFC 9700 §4.14.2).** Re-presenting a token whose `counter` is below the family's current `counter` is reuse — the chain has already moved on. The server deletes the family row (killing the current token too) and returns `invalid_grant`; the user re-authenticates and the thief is locked out. Clean expiry (DynamoDB TTL) is not theft.

**A lost response is not theft.** A client whose refresh response never arrived re-presents the token it still holds, which is exactly one step behind. When that token's `counter` is the family's current `counter` minus one and the family rotated no more than 60 seconds ago, the server returns the current token again instead of deleting the family. Any older token, or the same token after those 60 seconds, is reuse.

**Revocation is family-wide, and user-wide is one query.** `/oauth2/revoke` (RFC 7009 §2.1: revoking a token MAY revoke the underlying grant) deletes the presented token's family row, ending that login.

**Revoking every session for a user** — sign out everywhere (`DELETE /v1/user/sessions`), a password change or a password reset — is a single `Query(user_id)` + delete of the family rows, with **no GSI**. Sign out everywhere and a password reset keep `offline_access` grants; a password change deletes them too ([sso-sessions.md](sso-sessions.md#when-refresh-families-are-deleted)). Access tokens are stateless RS256 JWTs and cannot be revoked server-side (the RFC 7009 §2.2 access-token SHOULD does not apply); they expire on their own within the hour.

**Storage**:

- TableName: `espuser-refresh-tokens`
- Key: `user_id` (PK), `client_family` = `client_id#family_id` (SK) — one row per family
- Attributes: `counter` (current rotation counter), `client_id`, `family_id`, `scope`, `resources`, `sid`, `auth_time`, `expires_on`, `rotated_at`, `created_at`
- `user_id` (PK): every family for a user shares the partition, so user-wide revoke is one `Query(user_id)`; the signed token carries `user_id`, so the caller always supplies the partition key with no lookup
- `client_id#family_id` (SK): identifies one login's family within the user; a refresh token is always tied to one client
- `counter`: the family's current rotation number; a token is valid iff its signed `counter` equals this. Advanced by a conditional write on rotation; no per-token rows and no spent-token tombstones
- `scope`, `resources`, `sid`, `auth_time`: carried on the family row so a rotation can re-mint the access/id tokens for the same login without a separate lookup
- `expires_on`: DynamoDB TTL attribute; the refresh-token service (not the store) stamps it
- `rotated_at`: when the family last rotated; it bounds the 60-second lost-response window

The issued token is `base64(user_id | client_id | family_id | counter) . HMAC(secret, ·)`: the server verifies the HMAC, parses the fields, and does a direct `GetItem` on PK `user_id` + SK `client_id#family_id`, then compares the token's `counter` to the row's — no GSI, no per-token storage. The signature (not obscurity) is what makes the token unforgeable.

## Token endpoint (`POST /oauth2/token`)

The standard OAuth 2.0 token endpoint (RFC 6749 §3.2). One endpoint dispatched on `grant_type`; the request is `application/x-www-form-urlencoded`. Implemented: **`authorization_code`**, **`refresh_token`** and **`client_credentials`**; any other `grant_type` is rejected with `unsupported_grant_type`. `client_credentials` requires a confidential client registered for the grant and returns an access token only — no refresh token, no ID token; see [admin-clients.md](admin-clients.md) for the registry fields that gate it.

**Client authentication.** A confidential client authenticates with HTTP Basic (`client_secret_basic`) or with `client_id` and `client_secret` in the form body (`client_secret_post`); when both are sent, Basic wins. A public client sends its `client_id` and no secret.

### refresh_token grant

Redeems a refresh token for a fresh token set, rotating the refresh token (see [Refresh tokens](#refresh-tokens) for the rotation / reuse-detection mechanics — this section is only the HTTP surface).

**Request** (form-encoded):

```
grant_type=refresh_token
refresh_token=a1b2c3d4.9f8e7d6c...
client_id=rm_mobile
```

> `client_id` is REQUIRED: a refresh token is bound to the client it was issued to, and a token presented by any other client is refused. Public clients send no secret; a confidential client additionally authenticates as above. An optional `resource` picks which of the sign-in's APIs the new access token is for ([resource-indicators.md](resource-indicators.md)).

**Process**:

1. Parse the form body; require `grant_type=refresh_token`, `refresh_token`, `client_id`.
2. Verify the token's HMAC and that it was issued to this client, then read its family row by `user_id` + `client_id#family_id`. A missing family is `invalid_grant`.
3. Pick the access token's resource. A refusal is `invalid_target`, and nothing is spent.
4. An expired family is deleted (`invalid_grant`). Then compare counters: a stale token is reuse, so the family is deleted and the answer is `invalid_grant` (see [Refresh tokens](#refresh-tokens), including the 60-second lost-response window).
5. A family whose session has ended is deleted (`invalid_grant`). Otherwise, atomically rotate: advance the family's `counter` with a conditional write and issue the token carrying the new `counter`; a concurrent rotation that already advanced it makes this one `invalid_grant`.
6. Re-mint an RS256 access token (1h) and, if the family's stored `scope` includes `openid`, an id token — using the `user_id`, `scope`, `sid` and `auth_time` carried on the family.

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

**Errors** — the OAuth token endpoint uses the RFC 6749 §5.2 error object `{ "error": "<code>", "error_description": "<detail>" }`, NOT the API's generic `{ "message": ... }`. `error` is one of the RFC-defined codes below.

- `400 invalid_request` — missing/malformed `grant_type`, `refresh_token`, or `client_id`.
- `400 invalid_grant` — unknown, expired, spent, or revoked refresh token, one issued to another client, or one whose session has ended (uniform; no distinction between them, no reuse oracle).
- `400 invalid_target` — the `resource` is not one this sign-in covers or the client may still request ([resource-indicators.md](resource-indicators.md)).
- `400 unsupported_grant_type` — any `grant_type` outside `authorization_code`, `refresh_token` and `client_credentials`.
- `400 unauthorized_client` — `client_credentials` from a public client, or from a client not registered for that grant.
- `400 invalid_scope` — `client_credentials` asking for a scope the client is not registered for.
- `401 invalid_client` — a confidential client that fails its auth method.
- `500 server_error` — a failure on our side.

## Userinfo endpoint (`GET /oauth2/userinfo`)

The OIDC Core 1.0 §5.3 UserInfo endpoint. A client presents the access token it holds and gets back the end-user's claims — the canonical way to read profile data instead of trusting whatever an id_token happened to carry at login. Protected by the access token itself (bearer), so it needs no separate authorizer. Per §5.3.1 it accepts **both `GET` and `POST`**; on a form `POST` the token may instead ride the `access_token` body field (RFC 6750 §2.2).

**Request**:

```
GET /oauth2/userinfo
Authorization: Bearer <access_token>
```

> Equivalently `POST /oauth2/userinfo` with `Authorization: Bearer …`, or `POST` `application/x-www-form-urlencoded` with `access_token=…`.

**Process**:

1. Extract the bearer access token — from the `Authorization` header, or the `access_token` form field on a `POST`. A missing/blank one is `401 invalid_token`.
2. Verify it against the published JWKS (RS256, our issuer) as any resource server would, except that any of our APIs may be its `aud`: userinfo describes the sign-in, not an API (OIDC Core §5.3). An invalid/expired/wrong-signature token, an ID token, or a `client_credentials` token is `401 invalid_token`.
3. Return the claims carried in the access token. They were stamped when the token was minted, gated by its `scope`: `email` with `email` or `profile`; `phone_number` with `phone` or `profile`; `name`, `locale` and `picture` with `profile`. No user record is read.

**Response** (`200`, `application/json`):

| field | example | notes |
|---|---|---|
| `sub` | `9aXb7...` | always |
| `user_id` | `9aXb7...` | always; the same value as `sub` |
| `is_admin` | `false` | always; `false` for an ESP User token |
| `is_super_admin` | `false` | always; `false` for an ESP User token |
| `email` | `user@example.com` | `email` or `profile` scope |
| `phone_number` | `+15551234567` | `phone` or `profile` scope |
| `name` | `Jane Doe` | `profile` scope |
| `locale` | `en-US` | `profile` scope |
| `picture` | `https://example.com/jane.png` | `profile` scope |

> A scope-gated claim appears only when the token's scope authorizes it and the account had the value when the token was minted. Contacts are verified at sign-in, so no `email_verified` / `phone_number_verified` flags are emitted.

**Errors** — this is a bearer-protected resource endpoint, so failures use the RFC 6750 `WWW-Authenticate` challenge, not the OAuth token-endpoint error body:

- `401 invalid_token` — missing, malformed, expired, or unverifiable access token (`WWW-Authenticate: Bearer error="invalid_token"`).

## Revoke endpoint (`POST /oauth2/revoke`)

The RFC 7009 token-revocation endpoint. RFC 7009 §2.1 permits revoking a token to also revoke **"related tokens and the underlying authorization grant"**, so this endpoint revokes the presented token's **whole family** — the login's entire rotation chain — via `RevokeFamily`. Because rotation makes any earlier token already-spent, a single-token revoke would usually be a near no-op; family revocation is the useful, RFC-endorsed behavior. Theft detection (a *replay*, per RFC 9700 §4.14.2) calls the same `RevokeFamily`.

The one cascade RFC 7009 §2.1 endorses — a refresh token additionally invalidating the access tokens minted from it — does not apply here: access tokens are short-lived RS256 JWTs we do not track, so there is nothing to revoke; they expire on their own within the hour. A presented *access* token is therefore accepted and ignored. There is no introspection.

**The client must identify itself** (RFC 7009 §2.1/§5 → RFC 6749 §3.2.1). A **confidential** client authenticates with its secret via HTTP Basic (`Authorization: Basic base64(client_id:secret)`); a **public** client identifies with `client_id` — either the Basic username or the `client_id` form parameter — and presents no secret (RFC 6749 §3.2.1: a public client is identified, not authenticated). The request-body secret method (`client_secret_post`) is not accepted (§2.3.1 NOT RECOMMENDED). A request with neither Basic nor `client_id` is `400 invalid_request`; a bad/unknown client or a confidential client with a wrong secret is `401 invalid_client`. The token alone locates its family; the authenticated `client_id` must be the client the token was issued to (RFC 7009 §2.1: the server verifies that the token was issued to the requesting client).

**Request** (form-encoded body + Basic header):

```
Authorization: Basic base64(client_id:client_secret)
token=a1b2c3d4.9f8e7d6c...
token_type_hint=refresh_token   (optional)
```

**Process**:

1. Parse the form body; require `token` (else `400 invalid_request`). `token_type_hint` is advisory and ignored.
2. Authenticate the client from the Basic header, or take the public client's `client_id` from the body, against the registry: an unknown client or a confidential client with a missing/wrong secret is `401 invalid_client` (uniform — no oracle for which clients exist). A registered public client passes with an empty secret; a public client that presents a non-empty secret is rejected.
3. If `token` parses as one of our refresh tokens issued to the authenticated client, revoke **its whole family** — the login the token belongs to (RFC 7009 §2.1: revoking a token MAY revoke the underlying grant). A refresh token issued to another client is refused with `400 invalid_grant` and its family is untouched. A malformed token or an access token is a no-op (access tokens are stateless RS256 JWTs, not revocable server-side).
4. If the family carried a `sid` and no other family of the person still does, delete that user agent row when it is a cookie-less `app` row; a `browser` row stays, because its cookie can still sign in ([sso-sessions.md](sso-sessions.md#when-the-user-agent-row-is-deleted)). A failure here is logged and does not fail the revocation.
5. Otherwise return `200` whether or not the token existed (RFC 7009 §2.2 — no oracle; an attacker learns nothing about validity). Only a malformed *request* (missing `token`) is `400 invalid_request`. A storage failure while reading or deleting the family is `503 temporarily_unavailable` (§2.2.1), so the client keeps the token and retries.

**Response**: `200` with an empty body (RFC 7009 §2.2).

**Errors** — the endpoint follows RFC 7009 §2.2.1 and reuses the RFC 6749 §5.2 error object:

- `400 invalid_request` — the `token` parameter is missing, or neither Basic credentials nor a `client_id` parameter was supplied.
- `401 invalid_client` — an unknown client, or a confidential client that fails its auth.
- `400 invalid_grant` — the refresh token was issued to another client (RFC 7009 §2.1, RFC 6749 §5.2).
- `503 temporarily_unavailable` — storage failed while reading or deleting the family; the token may still work (RFC 7009 §2.2.1).
- Unknown, malformed, expired, or already-revoked tokens are **not** errors — they return `200`.

## Standards reference

- **[OAuth 2.1 (draft)](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/) / [RFC 9700 (BCP 240)](https://www.rfc-editor.org/rfc/rfc9700)** — code + PKCE; no implicit and no password grant. Refresh rotation + reuse detection ([§4.14.2](https://www.rfc-editor.org/rfc/rfc9700#section-4.14.2)).
- **[RFC 7009](https://www.rfc-editor.org/rfc/rfc7009)** — token revocation.
- **[OIDC Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)** — `auth_time` semantics; the UserInfo endpoint ([§5.3](https://openid.net/specs/openid-connect-core-1_0.html#UserInfo)).
- Identifier canonicalization: emails are lowercased before any lookup, dispatch, or
  user creation (one canonical form end to end); E.164 phone numbers have no case.

## FAQs

1. **A user has both email and phone — do both work?**
   - Yes. Either resolves to the same `user_id` via the `by-email` / `by-phone` GSI.

2. **Do I get all three tokens?**
   - From the `authorization_code` and `refresh_token` grants: an access token and a refresh token, plus an `id_token` only if `openid` is in scope. From `client_credentials`: an access token only.

3. **Is the refresh token a JWT?**
   - No. Access / id tokens are stateless JWTs; the refresh token is an HMAC-signed string backed by its family row, so it can be rotated and revoked. Clients treat it as opaque.
