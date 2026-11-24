# Single sign-on — the authorization server's own session, and the user agents behind it

> Companion to [authorize-code-flow.md](authorize-code-flow.md), which specifies the
> `/oauth2/authorize` request itself, and [federation.md](federation.md), which specifies the
> upstream leg a user agent is created from. Audience scoping is in
> [resource-indicators.md](resource-indicators.md); the legacy password surface is
> [legacy-user-auth.md](legacy-user-auth.md). The enterprise OTP addon's own surface is
> `addon_modules/espuser_pro/docs/specs/otp-login.md`.
>
> **Status.** Built. The session cookie, the `/oauth2/authorize` shortcut, `prompt`/`max_age`/
> `id_token_hint`, the session API and `GET /oauth2/logout`; the **two clocks** (`expires_at` +
> `cookie_expires_at`); a **user agent row for every first-party login** — federation, the sign-in page's password form, browser and
> native OTP, and the legacy password surface; and the **grant clamp** that keeps a refresh
> family from outliving the user agent it belongs to. The one-time data backfill for a deployment that
> already has rows under the old schema is in [Migration](#migration).

## Overview

ESP User keeps one durable record per sign-in origin — a **user agent** — so that a person who signed
in at one application reaches the next one without authenticating again, so the "Signed-in user agents"
screen is accurate for as long as they keep using the product, and so a single sign-out ends every
product opened from that user agent. `/oauth2/authorize` consults the user agent and, when it is usable,
answers with an authorization code immediately: the provider-selection page and the upstream leg
are both skipped.

The record is stored server-side in `espuser-sessions`; a browser holds only an opaque cookie that
points at it. A native app holds nothing at all — its user agent row exists so the app can be listed
and signed out remotely, and it is reachable only by `sid`.

### Key design decisions

**The session cookie is not where tokens live.** Each layer below answers a different question and
has a lifetime of its own; conflating any two of them is the usual mistake, and fusing the middle
three into one row that died on the shortest of them is the defect this design removes:

| Layer | Scope | Answers | Lifetime |
|---|---|---|---|
| Authentication event | one login | *when, and how, did this person prove who they are?* | a fact — `auth_time`, never moved by use |
| User agent (the `espuser-sessions` row) | one browser, or one app install | *is this a known, signed-in origin?* | `expires_at` — rolling |
| Session cookie | one browser | *may this browser skip the login page?* | `cookie_expires_at` — rolling, front channel only |
| Access token | one API call | *may this caller do this?* | 1 h, absolute |
| Refresh family | one application on one user agent | *may this client get another access token?* | rolling, clamped to its user agent |

**Two clocks on the row, and they are not the same thing.**

| Clock | Question it answers | Bumped by | Consequence when it passes |
|---|---|---|---|
| `expires_at` | is this user agent still known? | **any** activity — a browser `/oauth2/authorize` *or* an app's token refresh | the row is swept; the user agent is gone |
| `cookie_expires_at` | can this browser still skip the login page? | **only** a response that actually carries `Set-Cookie` — every `/oauth2/authorize`, and a first-party verify | the browser drops the cookie; SSO is lost, the user agent is not |

They diverge for exactly one reason: `Set-Cookie` reaches a browser only on a response a browser
processes, while `expires_at` is a server-side write that any back-channel call can make. A user agent
used only through an app therefore stays *known* indefinitely while its browser cookie lapses. That
state is a value we can read, so `GET /v1/user/sessions` can say "signed in, but this browser must
sign in again" instead of guessing.

`cookie_expires_at` is a **record of what we told the browser, never a control.** The browser
enforces `Max-Age` itself; we cannot. So a cookie presented after `cookie_expires_at` has passed —
clock skew, a client that ignores `Max-Age` — is still resolved by its hash and still works, because
the row is the authority. Gating the shortcut on it would enforce a client-side value twice and
refuse legitimate sessions. Its only jobs are an honest user agent list and a diagnosable seam.

By construction `cookie_expires_at ≤ expires_at`: both are `now + RETAIN_TTL` from their last
bump, and cookie bumps are a strict subset of retain bumps.

**A user agent is a sign-in origin, not a browser.** The row records where a login
came from; a cookie is only how a *browser* re-presents it. Two kinds of row, one schema:

| Kind | `session_hash` | Cookie | SSO possible | Created by |
|---|---|---|---|---|
| Cookie-bearing | `SHA-256(cookie value)` | issued | yes | federation callback · password sign-in on the page · browser OTP verify |
| Cookie-less | `SHA-256(a random value that is then discarded)` | none | **no, by construction** | native OTP verify · legacy password sign-in · a native app's code exchange |

A cookie-less row is keyed by the hash of a secret **nobody holds** — generated, hashed, dropped.
Nothing can present it, so the shortcut can never take it, with no extra branch in the authorize
path and no nullable key. The row is reachable only the way the user agent list reaches it: through
`espuser-sessions-by-user` by `user_id` + `sid`.

**Every first-party login establishes a user agent, not only federation.** A login
with no user agent row is not merely missing from a screen: it gets no SSO, its refresh families carry
no `sid` so `DELETE /v1/user/sessions/{sessionId}` cannot reach them, and `GET /oauth2/logout` — which
deletes families *by* `sid` — does nothing at all. For a deployment whose default login is a
one-time code, wiring only federation covers the minority path. See
[Which surfaces establish what](#which-surfaces-establish-what).

**The cookie is opaque and the record is server-side.** The cookie carries a random identifier and
nothing else; its meaning is a row in `espuser-sessions`. That is what makes a session revocable,
which a self-describing token could never be.

**The row is keyed by the SHA-256 of the cookie, and carries a separate public `sid`.** Reading the
table yields no usable cookie. `sid` is the name the user agent is referred to by elsewhere — stamped
onto every refresh family created through it, so the families opened from one origin are
recognisable as a group.

**`auth_time` is a fact, never a timer.** It records when the person last actually authenticated,
and it is updated *only* by a real authentication — a first login, a re-login on the same user agent, or
a step-up. For a federated login it is inherited from the upstream ID token: a person who
authenticated at 09:00 and whose provider silently re-recognised them at 12:00 authenticated at
09:00. For a first-party login it is the verify moment, because the authentication happened here.
Rolling either clock never touches it, so `max_age` and step-up cannot be satisfied by an old login.

**Single sign-on is derived, not stored.** "May this browser skip the login page?" is answered by:
the browser presented a cookie, the row exists, and `prompt`/`max_age`/`id_token_hint`/
`allowed_providers` permit it. There is no `sso_expires_on` field.

**A grant may not outlive its user agent.** See
[User agents and refresh families](#user-agents-and-refresh-families). This is what keeps an orphaned
family — a signed-in app with no user agent to end it from — from living on: the one that can still be minted, from a code redeemed after sign-out, is refused with `invalid_grant` and deleted at its first rotation.

**A provider row can only ever shorten.** A provider may declare `session_max_ttl_seconds`, and the
effective retention is the lower of that and the deployment-wide value. Without it, adopting a
session of our own would keep people signed in past the point the upstream sanctioned.

**Every failure degrades to "no session".** A missing cookie, an unknown cookie, an expired row, a
failed write — all produce the ordinary login. The user agent layer can cost single sign-on; it can
never block a sign-in and never fails open.

**`prompt=login` is always honoured.** A relying party that asks for a fresh authentication gets the
full login leg regardless of what user agent exists. A shortcut that could be taken in spite of
`prompt=login` would make the parameter meaningless.

## Pre-requisites

- The `espuser-sessions` table and its `espuser-sessions-by-user` index.
- At least one login path that establishes a user agent: a federated provider, the password form of a row whose `password_verifier` is `cognito`, the enterprise OTP addon,
  or the legacy password surface. A deployment with none of them keeps no sessions and loses only
  single sign-on.
- For a first-party user agent, the `identity_providers` row the login authenticated against (its `provider_name`, such as `otp` or `cognito`)
  — it is where `session_max_ttl_seconds` lives.

## Architecture

```
  FIRST LOGIN — application A, no user agent yet
  browser ──▶ /oauth2/authorize ──▶ no cookie presented
                                     │
                                     ▼
                          provider selection ──▶ upstream provider
                                                        │
                          federation callback ◀─────────┘
                                     │
                   writes espuser-sessions row (auth_time from the upstream id token)
                                     │
              302 redirect_uri?code=… + Set-Cookie: __Host-esp_session
                                     ▼
                              browser holds the cookie

  FIRST LOGIN — the same browser, one-time code instead of a provider
  browser ──▶ /oauth2/authorize ──▶ login page (same origin)
  page    ──▶ POST /v1/auth/otp/verify   (same-origin fetch, carries any prior cookie)
                                     │
                   writes espuser-sessions row (auth_time = now)
                                     │
              { redirect_to: … } + Set-Cookie: __Host-esp_session
                                     ▼
                              browser holds the cookie

  SECOND APPLICATION — B, same browser
  browser ──▶ /oauth2/authorize ──▶ cookie presented automatically (same origin)
                                     │
                          ┌──────────┴───────────┐
              user agent usable                  not usable, or prompt asks for interaction
                    │                                     │
        302 redirect_uri?code=…                  the full login leg above
        (no provider page, no upstream hop)
        + Set-Cookie (rolled)

  A NATIVE APP — no browser anywhere in the call
  app ──▶ POST /v1/auth/otp/verify  or  POST /v1/user/auth/token
                                     │
                   writes a COOKIE-LESS espuser-sessions row
                                     │
                        token set, stamped with the user agent's sid
                                     ▼
                  listed on the user agent screen; endable by sid; never SSO
```

The cookie reaches `/oauth2/authorize` because the browser is navigating to the issuer's own origin.
The application never sees it, and never could: what an application receives back is an
authorization code.

## Design

### Establishing a user agent

Two random values are generated: the cookie value, which only the browser holds, and the `sid`,
which is the user agent's public name. The row stores `session_hash = SHA-256(cookie value)`, the user agent
identity (`user_agent`, `ip_address`, optional `user_agent_name`), the authentication event
(`auth_time`/`amr`/`acr`/`provider`), `created_at = now`, and both clocks.

For a **cookie-less** user agent the cookie value is generated, hashed and discarded; `cookie_expires_at`
is not written.

**Re-authentication rotates rather than adds.** When the browser arrived holding a live user agent for
the *same* user — a `prompt=login`, a `max_age` too tight, a step-up, or simply signing in again —
the new record inherits the old `sid` and `created_at`, and the old row is deleted once the new one
exists.

Both halves matter and they pull in different directions:

- *the old row is deleted*, so the previous cookie value stops working the moment the new one is
  issued. Signing in again is what a person does when they think their session was stolen; without
  this it changes nothing, and the stolen cookie stays good until its retention runs out.
- *the `sid` and `created_at` are carried across*, so every refresh family already created under it
  stays attached and "first signed in" does not reset. A fresh `sid` would orphan every product
  signed in before the re-login, and one sign-out would stop reaching them.

The user agent list stays honest as a consequence: one browser is one row, not one row per login.

A prior user agent belonging to a **different** user is left alone. The cookie is being overwritten
either way, so that row can never be presented again — but it is still the other account's record of
a browser they signed in on, still listed and still theirs to end, and destroying it on a login it
did not authorise is not this path's business.

A cookie-less establish ignores any presented cookie entirely: an app's call is not evidence about a
browser.

### The two clocks — what each event updates

Two channels decide what an event can touch. The **front channel** is a
response a browser processes — a navigation to `/oauth2/authorize`, or the sign-in page's own
same-origin `fetch`. The **back channel** is an app's HTTP client: `POST /oauth2/token`,
`POST /v1/user/auth/token`, a native `verify`.

| Event | Channel | `expires_at` | `cookie_expires_at` | Cookie re-issued |
|---|---|---|---|---|
| First login — federation callback | front | set | set | yes |
| First login — password sign-in on the page | front | set | set | yes |
| First login — browser OTP verify | front | set | set | yes |
| SSO shortcut / new-app login — `/oauth2/authorize` | front | **bumped** | **bumped** | **yes** |
| App token refresh — `/oauth2/token` (`refresh_token`) | back | **bumped** | untouched | no |
| App first exchange — `/oauth2/token` (`authorization_code`) | back | **bumped** | untouched | no |
| Native OTP verify / legacy sign-in | back | set | never written | no |

`last_seen_at` is updated by every row in that table, and is what the user agent list renders as "last
active".

Bumping `expires_at` from a back-channel call is an `UpdateItem` keyed via the refresh token's
`sid` — no browser required. Re-issuing the cookie is impossible there, and that asymmetry is the
whole reason the second clock exists.

### Which surfaces establish what

| Surface | Channel | User agent row | Cookie | `sid` in tokens | SSO afterwards |
|---|---|---|---|---|---|
| Federation callback | front | yes | issued | yes | yes |
| Password sign-in on the page (`POST /user/login/password`) | front | yes | issued | yes | yes |
| Browser OTP verify (sign-in page) | front | yes | issued | yes | yes |
| Native OTP verify (`flow_type: native`) | back | yes, cookie-less | none | yes | no |
| Legacy `POST /v1/user/auth/token` | back | yes, cookie-less | none | yes | no |
| `authorization_code` exchange | back | a cookie-less row for a native client; none for a web client (inherits the flow's `sid`) or an `offline_access` grant (no `sid`) | no | yes | — |
| `refresh_token` rotation | back | none — bumps `expires_at` | no | inherits | — |
| `client_credentials` | back | **never** — there is no human, and a machine token must carry no `sid` | none | no | — |

`Set-Cookie` on the page's password sign-in and OTP verify works because those requests are **same-origin**: the sign-in page
is served by this issuer and posts to a relative path on the same API, so `fetch`'s default
`credentials: "same-origin"` sends the existing cookie and the response's `Set-Cookie` is stored. It
is a property of the deployment, not of the page. A login page hosted on **another** origin gets a
user agent row and no cookie — `SameSite=Lax` withholds it from a cross-site POST — so it loses SSO and
nothing else.

### Which surfaces can have single sign-on

Single sign-on is a property of **the surface a person signs in through**, never of the credential
type. A cookie is the whole mechanism, so the only question is whether a browser is present to hold
one — which is why the two back-channel rows above can never have it. Nothing is being withheld
from them: two sandboxed apps share no store that a cookie could live in.

On a phone, an app that wants SSO uses the system browser (RFC 8252): `/oauth2/authorize` opened in
`ASWebAuthenticationSession` (iOS) or a Chrome Custom Tab (Android), with the code returned to a
private-use scheme (§7.1) or a claimed HTTPS link. Both of those components **share the system
browser's cookie jar**, so a second app opening the same flow with `prompt=none` is answered
silently — genuine cross-app single sign-on, with no server change. `SFSafariViewController` and any
embedded `WKWebView` have per-app cookie stores and get none (and RFC 8252 §8.12 forbids the latter
anyway), so the client's choice of component, not this spec, decides whether a phone gets SSO.

A client registered as `application_type: native` gets a cookie-less row of its own when it redeems the code. The row carries the approving browser row's `provider`, `auth_time`, `amr`, `acr` and cap (`max_expires_at`), and the family and its tokens carry the app row's `sid`, so signing out of the browser leaves the app signed in, while sign out everywhere, a password event or removing the app from the list ends it. A code whose approving browser row has ended is refused with `invalid_grant`, because nothing would later end the app's login. A failed write of the app row leaves the login under the browser's row. A web client's login stays under the browser's row. Giving a native app its own row is ESP User's rule: OIDC Dynamic Client Registration 1.0 §2 defines `application_type`, and no standard says which session a native app's login belongs to.

### Consulting a session (the SSO shortcut)

`/oauth2/authorize` consults the user agent before the provider-selection step. The shortcut is taken —
a code issued with no upstream round trip — only when all of the following hold:

- a cookie was presented and names a row that exists and whose `expires_at` has not passed;
- the request's `prompt` is not `login` or `select_account`;
- `max_age`, when present, is satisfied by the row's `auth_time`;
- `id_token_hint`, when present, names the subject the row belongs to;
- the client's `allowed_providers`, if set, admits the provider the row was authenticated with — the
  same check as at `/user/login/federation/start`, so a shortcut cannot bypass a provider restriction.

A shortcut for a client without `first_party` holds the sign-in for consent instead of issuing a code (see [authorize-code-flow.md](authorize-code-flow.md#consent--clients-we-do-not-ship)). On a successful shortcut both clocks are bumped and the cookie is re-issued. A failure to record
that costs the extension and nothing else — the login still succeeds.

### The request parameters that override a session

| Parameter | Effect |
|---|---|
| `prompt=none` | Never shows a page. A usable user agent yields a code; anything else redirects with `login_required`, or `consent_required` for a client that needs consent |
| `prompt=login` | Always re-authenticates, whatever user agent exists |
| `prompt=consent` | Keeps the shortcut and always shows the consent screen |
| `prompt=select_account` | Disables the shortcut, so the ordinary login runs |
| `max_age` | The row's `auth_time` must be no older than the given number of seconds. `>=` is the boundary, so `max_age=0` always re-authenticates; an unparseable value counts as unsatisfied |
| `id_token_hint` | The user agent must belong to the subject the hint names. One that names someone else, or does not verify, drops the shortcut rather than switching user. Expiry is not checked — the same reasoning as at the logout endpoint, below |

### User agents and refresh families

A refresh family created through a user agent carries that user agent's `sid` and keeps it across every
rotation, so the families opened from one origin share a name. This is a **session family**. An
offline grant is the exception: it carries no `sid` and none of this section applies to it (see
[Offline grants](#offline-grants)).

The rule that makes the user agent list trustworthy:

> **`expires_at` (user agent) ≥ `expires_on` of every live family under it.**

Maintained by two things together:

1. minting or rotating a family bumps the user agent's `expires_at`, so both clocks are reset by the
   same events; and
2. **a session family's expiry is clamped to its user agent's retention** —
   `family.expires_on = min(now + RefreshTokenTTL, user agent.expires_at)`.

The clamp is what makes the invariant hold *by construction, for every provider*, rather than by the
constant relationship `RETAIN_TTL > RefreshTokenTTL` alone. That relationship is not enough on its
own: a provider row's `session_max_ttl_seconds` shortens `expires_at` and would otherwise put the
row *behind* the families minted under it — the row swept while its families are alive, exactly the
orphan this design exists to remove. With the clamp, a provider cap also becomes *meaningful*:
capping a provider's user agents at 30 days genuinely ends the app logins opened through them at 30 days.

Consequently a live family has its user agent row, so `GET /v1/user/sessions` cannot report a
signed-in application it has no way to sign out. The one exception is a family minted from a code redeemed after sign-out, which is refused with `invalid_grant` and deleted at its first rotation. A rotation refuses and deletes a family the same way whenever its session has ended: the by-user index lists no row for its `sid`, or every listed row is past `expires_at`. A listed row that cannot be read yet, as while a re-sign-in replaces it, does not count as ended, and the family rotates at `now + RefreshTokenTTL`. A family with no `sid` — an offline grant, or a
session family minted before sessions existed or after a failed session write — is unclamped.

**Constraint for implementers:** `RefreshTokenTTL` must stay `≤ RETAIN_TTL`.

### Offline grants

A client that asks for `offline_access` (OIDC Core 1.0 §11) gets a refresh family that no sign-out
ends: refresh tokens issued with `offline_access` are normally not revoked at logout (OIDC
Back-Channel Logout 1.0 §2.7). An assistant link or a connector acts while the person is away, and
a sign-out in a browser must not unlink it.

- **What makes a family offline:** its stored `scope` contains `offline_access`. There is no other
  attribute.
- **Who can get one:** a client whose `scopes` row lists `offline_access`. Any other client asking
  for it is redirected with `error=invalid_scope`.
- **Consent, always:** asking for `offline_access` shows the consent screen, even for a client we
  ship (OIDC Core 1.0 §11), so `prompt=none` with it returns `consent_required`.
- **Code flow only:** the native and legacy password sign-ins return no code and show no consent, so
  they drop `offline_access` from the scope and mint an ordinary session family.
- **Minted with no `sid`:** the family has none, and neither its access tokens nor its ID tokens carry
  a `sid` claim. Nothing that deletes by `sid` reaches it, and the clamp never applies.
- **Lifetime:** `RefreshTokenTTL` (365 days), rolling on every rotation, and not bounded by any user
  agent.
- **Redeemed only while its approving session lives:** a code with `offline_access` whose browser row has ended is refused, since the grant would outlive the sign-out.
- **Renewed only while its client may hold it:** a client whose registration no longer allows `offline_access` gets `invalid_grant` on the next refresh, and the grant is not spent.
- **Where it is listed:** under its client in `GET /v1/user/connected-apps`, never under a user agent.
- **What ends one:** being disconnected from the connected apps list
  (`DELETE /v1/user/connected-apps/{clientId}`), a password change, revoking its own token
  (`POST /oauth2/revoke`), reuse detection on rotation, and expiry. Signing out of a browser, ending a
  user agent from the list, sign out everywhere and a password reset do not.

### Step-up: re-proving identity for sensitive actions

Because a user agent can stay signed in indefinitely, identity is re-proven **per sensitive action**
rather than by expiring the user agent. A client guarding a sensitive operation sends `prompt=login` (or
`max_age=0`) to `/oauth2/authorize`; that disables the shortcut unconditionally, and the fresh
authentication updates `auth_time`/`amr`/`acr` on the *same* row (same `sid`, same `created_at`).

This is where `amr`/`acr` earn their place: a step-up requiring multi-factor checks the fresh `amr`.
Wiring specific endpoints to demand step-up is out of scope here; the model is designed so that it
is a per-endpoint change with no further session work.

## The cookie

```
Set-Cookie: __Host-esp_session=<opaque>; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=<RETAIN_TTL>
```

| Attribute | Why |
|---|---|
| `__Host-` prefix | A browser honours the prefix only for a `Secure`, `Path=/`, `Domain`-less cookie, and a cookie carrying `Domain` cannot use it. On a shared API-gateway hostname this is what stops a neighbouring deployment from planting a cookie this one would accept |
| opaque value | 256 bits of randomness. It is an identifier, not an assertion; the meaning is the row |
| `HttpOnly` | Script on the issuer's own origin cannot read it, so a cross-site scripting flaw there cannot exfiltrate the session |
| `Secure` | Required by the prefix, and it is a bearer credential |
| `SameSite=Lax` | `Strict` would withhold the cookie from the cross-site top-level navigation that single sign-on consists of, disabling the feature. `Lax` still withholds it from cross-site subrequests and from `fetch` on another origin |
| no `Domain` | Required by the prefix; a widened cookie would be offered to every sibling host |
| `Max-Age` | A rolling **inactivity** window re-issued on every front-channel event, and recorded on the row as `cookie_expires_at`. 400 days is the browser ceiling — Chrome 104+ and RFC 6265bis clamp anything larger — so it is never set higher |

**A cookie in the jar never implies a live user agent.** The row is the source of truth and the lookup
fails closed when it is gone.

## Lifetimes

| Constant | Value | Meaning |
|---|---|---|
| `RETAIN_TTL` | 400 d | The row's retention and the cookie's `Max-Age`; a rolling inactivity window. |
| `RefreshTokenTTL` | 365 d | Refresh family lifetime, rolling. A session family is clamped to its user agent's `expires_at`; an offline grant is not. Must stay `≤ RETAIN_TTL` |
| `AccessTokenTTL` | 1 h | Access token lifetime, absolute, unrevocable |
| `IDTokenTTL` | 1 h | ID token lifetime |

There is **no idle timeout and no absolute cap** by default. A provider row's optional
`session_max_ttl_seconds` remains the one operator lever: when set, retention becomes
`min(now + RETAIN_TTL, auth_time + session_max_ttl_seconds)`. It can only ever shorten, and the
clamp above is what keeps a short value safe.

## Data model

`espuser-sessions`, partition key `session_hash`, TTL attribute **`expires_at`**.

| Attribute | Meaning |
|---|---|
| `session_hash` | SHA-256 of the cookie value. The cookie itself is never stored. For a cookie-less user agent, the hash of a value that was discarded |
| `sid` | The user agent's public identifier, carried on refresh families created through it |
| `user_id` | Who the user agent belongs to |
| `provider` | Which provider authenticated the most recent login: a federated row's name, `otp`, or the password row's name |
| `auth_time` | When the person last actually authenticated. A fact — never moved by use |
| `amr` | How they authenticated (RFC 8176): the upstream's values, or `otp` / `otp,sms` / `pwd` for a first-party login |
| `acr` | The authentication context the provider asserted. Empty for a first-party login: an ACR names a class a consumer must be able to interpret, and an invented value nothing reads is noise on a security-relevant field |
| `origin` | `browser` or `app` — what kind of sign-in origin this row records, and therefore whether a cookie was ever issued for it. Absent reads as `browser` |
| `expires_at` | The row's retention deadline and the DynamoDB TTL attribute. Bumped by any activity |
| `cookie_expires_at` | When the browser will drop the cookie. Bumped only when a `Set-Cookie` is issued; absent on a cookie-less row. A record, not a control |
| `last_seen_at` | Most recent activity of any kind, rendered as "last active". Distinct from `created_at` |
| `created_at` | When the user agent was first seen. Carried across re-authentication so "first signed in" does not reset |
| `user_agent` | The browser's own claim about itself, captured at the login that created the row and never updated. A **hint so a human recognises a row**, never a security control: it is self-reported and browsers are actively reducing it, so absent is ordinary |
| `ip_address` | The source address at that same moment. Same status: a hint, not a control |
| `user_agent_name` | The label the person gave this login, supplied at the enterprise OTP initiate. Absent on every other path |

Expiry is enforced when the row is read. The DynamoDB TTL sweep is a cleaner, not the gate: a
swept-late row is unusable from the instant it expires, whatever remains on disk.

### Index: `espuser-sessions-by-user`

Partition key `user_id`, sort key `sid`, projection **KEYS_ONLY**.

The base table is keyed by the SHA-256 of the cookie value, and a `sid` cannot be turned back into
that hash. Without this index a user agent is reachable **only from the browser holding its cookie** — so
listing a person's user agents, or ending one they are not currently using, is impossible rather than
merely awkward, and a cookie-less user agent would be unreachable entirely. Revoking a user agent's refresh
families is not a substitute: its cookie survives, and the next visit silently signs it straight
back in.

`KEYS_ONLY` is the whole cost story. The retention bump rewrites `expires_at` on **every**
`/oauth2/authorize` and every token refresh, and DynamoDB writes an index entry only when a key or a
*projected* attribute changes. Projecting nothing keeps that hot path free: entries change on user agent
create and delete only. Reads then cost one query plus a handful of point reads on the base table,
because a person has single-digit user agents.

## The session API

`account:sessions` scope throughout. A dedicated scope rather than a rider on `openid`: being able
to sign somebody in is not a reason to enumerate every user agent they use.

| Method | Path | Result |
|---|---|---|
| `GET` | `/v1/user/sessions` | Every live user agent, with the refresh families grouped under each by `sid` |
| `DELETE` | `/v1/user/sessions/{sessionId}` | Ends one user agent and its families. `204`, or `404` when the caller has no such session |
| `DELETE` | `/v1/user/sessions` | Ends every user agent, the caller's own included |
| `GET` | `/v1/user/connected-apps` | Every client holding a live offline grant: `client_id`, the earliest grant's `created_at` (its `rotated_at` when absent) and the latest `last_used`, most recently used first; a grant past `expires_on` is left out; `[]` when none |
| `DELETE` | `/v1/user/connected-apps/{clientId}` | Ends every login that client holds for the person, session logins included, and an app row left with none. `204`, or `404`, ending nothing, when the caller holds no offline grant for it |

The response body names the session id `session_id`, matching the snake_case every other field on
this surface uses. The path parameter stays `{sessionId}`, matching the camelCase every other path
parameter in the API uses (`{nodeId}`, `{groupId}`). The token claim and the stored attribute keep
the OIDC-standard name `sid`.

The list groups families under user agents by `sid`; `current` is true when the caller's access-token
`sid` matches the row's. `origin` distinguishes a browser from an app: `user_agent_type` renders a browser as "Chrome on macOS" and an app as "Android app", because a native `User-Agent` names an SDK and only the OS in it is worth showing. `cookie_expires_at` in the past on a row still listed is what "this browser must sign in again" is read from — the client compares that one field, and an app row simply has none.

The endpoint accepts only a **first-party user token**. A machine (`client_credentials`) token
carries a client id where the user would be, so it fails verification and is refused `401
invalid_token`. A valid user token from a client that is not first-party -- a third party was
delegated user-agent control, not account management -- is authenticated but not allowed, and is
refused `403 access_denied` before the scope gate. A failure to read the client registry is `500
server_error`: it is ours, not the caller's.

**"First-party" is a property of the client, read at the edge.** Which app counts as first-party is not a
hardcoded id but the `first_party` flag on the caller's `espuser-oauth-clients` row
([admin-clients.md](admin-clients.md)): the gate loads the client the token names and admits it only when
that flag is set. `rainmaker-neo-home-app-client`, and any app we ship such as a mobile client, carries it; the voice-assistant clients
and `mcp-oauth-client` — partners delegated device control — do not. It is read here rather than stamped
into the token so that revoking a client's first-party standing takes effect on its next call, not an hour
later at token expiry. The flag is admin-only, so a client can never mark itself first-party.

Offline grants belong to no user agent, so the session list never shows them; the connected apps
list is where they are seen and ended. Disconnecting a client ends every login it holds for the person, its session logins too, so removing an app leaves nothing of it behind. A client the caller has not connected answers the same `404` as an unknown client id, whether or not anyone else has connected it.

`session_hash` never appears in a response — it is the hash of a secret, and the delete path
re-resolves a `sid` through the index rather than trusting a client to return an internal key.

A `sid` belonging to another user answers **`404`, never `403`**. The two must be indistinguishable,
or the endpoint becomes an oracle for guessing valid user agent identifiers.

Ownership is enforced by the query itself — every read and delete is keyed on `user_id` as well as
`sid` — rather than by a comparison a handler could forget to write.

A native app reaches this endpoint only if it asked for `account:sessions` when it began its login
and its client row admits that scope; otherwise it holds a user agent row it can neither see nor delete.

## When refresh families are deleted

A refresh family — an application's standing access — is deleted **only** by a deliberate act or its
own expiry. It is **never** deleted by a user agent timing out, a cookie lapsing, or an idle period.

| Trigger | Scope | Path |
|---|---|---|
| Sign-out — one user agent | every family with that `sid` | `GET /oauth2/logout` · `POST /user/logout` · `DELETE /v1/user/sessions/{sessionId}` |
| Sign-out — everywhere | every session family for the user, with or without a `sid`; offline grants stay | `DELETE /v1/user/sessions` |
| Disconnect a connected app | every family of that client for the user | `DELETE /v1/user/connected-apps/{clientId}` |
| Password change | every family for the user, offline grants included | `POST /v1/user/auth/password` |
| Password reset | every session family of the account the pool's verified contacts name; offline grants stay; nothing when the new password's sign-in asks for a second step | `POST /v1/user/auth/password-recovery/confirmation` |
| Token revocation | the presented token's family | `POST /oauth2/revoke` (RFC 7009) |
| Reuse / theft detected | that one family | rotation sees a replayed or stale counter |
| Natural expiry | that one family | `expires_on` passes with no rotation |

**Not a trigger:** `expires_at` expiry (which cannot occur while a family is alive — the
invariant), a lapsed cookie, `prompt=none` failing, or any period of inactivity. A family is the
application's login; deleting it because a *browser* went quiet would sign a person out of an app
they use daily, which is the coupling this design removes. Standing access is revoked deliberately,
never by a passive clock.

## When the user agent row is deleted

| Trigger | Path |
|---|---|
| Sign-out (either scope) | after the families, deliberately in that order |
| Password change | every row of the user, after the families |
| Password reset | every row of the account the pool's verified contacts name, after the families |
| Retention expiry | `expires_at` passes — total silence, no browser and no app. By the invariant every family under it expired earlier, so the user agent is genuinely gone |
| An app's last login ended | `POST /oauth2/revoke`, `POST /v1/user/auth/signout` or a disconnect deletes a family and no other family of the person carries its `sid`: an `app` row goes with it, since nothing can sign in through it again; a `browser` row stays, because its cookie still can. A failure here is logged and the revocation still succeeds |

**Not a trigger:** a lapsed cookie. The cookie is a browser credential, not the row.

## Which endpoint ends what

Each exists because none of the others can do its job: only a browser navigation carries the session cookie, only a signed-in screen can reach a user agent it is not on, and a third-party client may end its own login but never the person's other sessions.

| Endpoint | Standard | Called by | Identifies by | Ends |
|---|---|---|---|---|
| `GET /oauth2/logout` | OIDC RP-Initiated Logout 1.0 | a browser app, as a top-level navigation | the session cookie, ended at once only with an `id_token_hint` minted under it; otherwise the person is asked first | this browser: its user agent row and every family under its `sid`, then the upstream provider's session |
| `POST /user/logout` | — | the sign-out page, on the person's yes | the session cookie and its `csrf_token` | the same as `GET /oauth2/logout` |
| `GET /oauth2/federation/logout/callback` | — | the upstream provider, handing the browser back | the `__Host-esp_logout_to` memo | nothing; forwards to the validated `post_logout_redirect_uri` |
| `DELETE /v1/user/sessions/{sessionId}` | — | a signed-in user agent list | `sid` | one user agent and its families, from anywhere |
| `DELETE /v1/user/sessions` | — | "sign out everywhere" | the access token's subject | every user agent and session family of the person; offline grants stay |
| `DELETE /v1/user/connected-apps/{clientId}` | — | a connected apps list | the access token's subject and the client id | every login of that client |
| `POST /oauth2/revoke` | RFC 7009 | the OAuth client the token was issued to; another client gets `400 invalid_grant` (§2.1), and a storage failure gets `503` with the family left in place (§2.2.1) | a refresh token | that token's family: one application's login, and the app's cookie-less user agent row when no other family carries its `sid` |
| `POST /v1/user/auth/signout` | — (legacy) | pre-OIDC RainMaker apps | a refresh token issued to the Home app client; any other gets `401` | the same as `/oauth2/revoke` called as the Home app client; `global` is refused |

Which to call:

| Caller | Endpoint |
|---|---|
| A web app signing the person out of this browser | `GET /oauth2/logout` with the `id_token_hint` it holds; without one the person is asked first |
| A native app or SDK ending its own login | `POST /oauth2/revoke` |
| A user agent list | `DELETE /v1/user/sessions/{sessionId}`, `DELETE /v1/user/sessions` |
| A connected apps list | `DELETE /v1/user/connected-apps/{clientId}` |
| The RainMaker app | `POST /v1/user/auth/signout`, until it moves to `/oauth2/revoke` |

An access token already issued is never recalled by any of these; it lives out its hour.

## Ending a session: `GET`/`POST /oauth2/logout`

RP-Initiated Logout 1.0 §2, advertised as `end_session_endpoint` in discovery.

§2 also requires a form `POST`. A cross-site `POST` carries no `SameSite=Lax` cookie (RFC 6265bis §5.6.7.1), so the `POST` ends nothing itself: it answers `303 See Other` to `?<the §2 parameters>` (the first value of each) and the browser repeats it as the `GET` below. A body that is not `application/x-www-form-urlencoded` gets `400 invalid_request`.

A `GET` that destroys state is not REST, and that is deliberate. Three things force a top-level
browser navigation, and none survives a cross-origin `fetch()`:

1. `__Host-esp_session` is `SameSite=Lax`, so it rides top-level navigations **only**. A cross-site
   fetch would not carry it and the row could not be found.
2. `Set-Cookie` from a cross-origin fetch response will not reliably clear it.
3. Step 6 below has to send the browser onward to the upstream. Only a navigation can.

The same navigation is open to any page on the web, and the cookie rides it, so the URL alone cannot decide a sign-out. The session ends at once only when the request shows it comes from an application signed in through this very session: an `id_token_hint` that verifies, is an ID token (`token_use` `id`; an access token carries the same `sid` but reaches every API the app calls), and whose `sid` and `sub` are the cookie's. Anything less with a live session is put to the person: §2 says the OP MUST ask when no hint is given or it does not belong to the current session, and §6 names requests without a valid hint as a means of denial of service.

In order:

1. Resolve the destination (below) and the user agent from the cookie — this browser holds the secret, so no index is used. The row is read without rolling its retention, because this response never re-issues the cookie. Nothing is destroyed before both are known, so a rejected redirect never leaves a half-ended session behind. A hint that cannot be checked at all (the signing keys are unreadable), or a session row that cannot be read, answers `500 server_error` and ends nothing: an unreadable session is not an absent one.
2. No live user agent: nothing to end. Clear a cookie that no longer resolves and redirect to the validated destination.
3. Anything but a hint that verifies and names this user agent's `sid` and subject: redirect to the sign-out page (`/user/logout`, relative so the stage or base path survives) with the §2 parameters, touching no cookie. See [The sign-out question](#the-sign-out-question).
4. Delete every refresh family created under that `sid`. **Families first:** clearing the cookie and
   then failing would leave the person signed out of the login but still signed in to everything it
   opened, which is the worse half to leave standing.
5. Delete the row and clear the cookie.
6. Redirect to the provider row's `end_session_url`, if it has one, and finally to the **validated** `post_logout_redirect_uri`. With no validated value, the final step is our own `/user/logout` with no query, whose page says the browser is signed out.

This endpoint can only ever end a **cookie-bearing** user agent — it is the cookie that names the row.
A cookie-less user agent is ended from the user agent list, which is correct: signing out of a browser must
not sign out a phone. A first-party user agent has no upstream session to end, so its provider row
carries no `end_session_url` and step 6 goes straight to the destination.

`post_logout_redirect_uri` is matched exactly against the client's registered
`post_logout_redirect_uris`, the same rule as `redirect_uri` and for the same reason: an endpoint
that forwards a browser anywhere is an open redirect wearing the issuer's own hostname. An
unvalidated value is **ignored, not rejected** — refusing to forward is safe, but refusing to *sign
out* over a cosmetic problem would leave the session alive.

The client is `client_id`, or, when that is absent, the audience of a verified `id_token_hint`. When both are sent and `client_id` is not the hint's audience, the return URL is ignored: RP-Initiated Logout 1.0 §2 requires the two to agree, and a mismatch means a caller is quoting another client's registered URL.

An `id_token_hint` is a **hint**, never a credential. The cookie is what authorizes the logout; the
hint is what lets it happen without asking. With a live user agent, four outcomes:

| Hint | Outcome |
|---|---|
| verifies, names this user agent's subject and `sid` | sign out at once |
| absent | ask the person. RP-Initiated Logout marks the hint RECOMMENDED, not required, so an app that omits it still signs out, one click later |
| verifies, names another `sid`, none, or somebody else | ask the person: the token belongs to another session, an older one, or another person, which §2 calls suspect; nothing ends, the cookie included, until they answer |
| does not verify | ask the person, and never return the browser to the client afterwards: a broken hint is an error in the request, and §4 forbids redirecting to the RP after one |

With no live user agent nothing is asked, because nothing would end; a hint that does not verify still forfeits the return to the client.

**Expiry is deliberately not checked.** An ID token lives an hour and a browser tab outlives it, so
by the time somebody clicks "sign out" the token they were last issued is routinely stale — that is
the normal case, not an attack. Checking `exp` would send nearly every real sign-out through the question, when a stale token still proves what a fresh one does. Signature and issuer are verified, which is all a hint is asked to establish: that we minted it, and for which session.

### The sign-out question

`GET /user/logout` is a page of the sign-in app, served by the authorize Lambda like `/user/login` and `/user/login/consent`. It reads the cookie's row again, without rolling it; with no live session there is nothing to ask, and it sends the browser back to `/oauth2/logout` with the same query, or, with no query, says the browser is signed out. A row it cannot read is a `500` error page, never that redirect: `/oauth2/logout` reads the row on its own, and if that read succeeded the browser would bounce between the two. Otherwise it draws *Sign out?* with the account it would end and a `csrf_token`, and keeps the query in its own URL.

- **Sign out** posts the parameters the page was opened with, and `csrf_token`, as JSON to `POST /user/logout`, so `id_token_hint` stays out of that request's URL. The sessions Lambda resolves the destination from it exactly as the `GET` does, checks the token and runs steps 4 to 6 whoever the hint names, answering `{"redirect_to": …}` with the cookies rather than a redirect because the page calls it with `fetch`. An empty `redirect_to` (no destination, or a hint that did not verify) and the page says the person is signed out.
- **Cancel** sends nothing. The session stays and the page says so ("You're still signed in"); it does not go back in the browser's history, which can replay an app's old callback URL. It never goes on to the client's `post_logout_redirect_uri`: that URL means "after a sign-out", and an app that correlates `state` would take the arrival as one.

What stops another page answering for the person:

| Attacker | What stands in the way |
|---|---|
| Another site's `POST` to `/user/logout` | `SameSite=Lax`: a cross-site `POST` carries no session cookie, so there is nothing to end. The answer then sets no cookie, because a top-level cross-site `POST` may still set or clear a Lax cookie (RFC 6265bis §5.7) |
| A page on another host of the same site, whose `POST` does carry the cookie | `csrf_token` is an HMAC-SHA256 keyed by the cookie value, which only the browser holds (the server stores its hash). That page can read neither the cookie (`HttpOnly`) nor the sign-out page (same-origin policy), so it cannot produce the token, and the token stops matching once the cookie rotates |
| A page framing the question to borrow a click | `frame-ancestors 'none'` and `X-Frame-Options: DENY` |

### The upstream hop: one registered URL, however many products

Ending our session leaves the provider's own untouched, and a provider that still holds a cookie
hands back a code without prompting. The person clicks *Sign out*, clicks *Sign in*, and is straight
back in without typing anything — signed out in every technical sense and not at all in the sense
they meant.

So after our row dies, the browser goes to the provider's end-session endpoint. What the provider is
handed is **our** return URL, never the calling product's:

```
  app ──▶ /oauth2/logout?id_token_hint=…&post_logout_redirect_uri=https://test1/bye
            │  (or the person's yes on /user/logout, through POST /user/logout)
            │  validate the return URL, kill families + row,
            │  clear __Host-esp_session, remember the destination in __Host-esp_logout_to
            ▼
       provider /logout?client_id=<ours>&logout_uri={issuer}/oauth2/federation/logout/callback
            │  provider clears ITS cookie
            ▼
       /oauth2/federation/logout/callback   ── reads the memo, clears it ──▶ https://test1/bye
```

This mirrors the login leg exactly. The provider holds **one** callback URL for logins and **one**
sign-out URL for logouts; it knows this authorization server and nothing about the products behind
it. Handing it each product's own page instead would mean registering every new product with the
provider too — and the provider is frequently administered by someone else.

The memo is a cookie rather than a query parameter because a provider matches the return URL
**exactly** against what it has registered; nothing may be appended to it. It is `__Host-` prefixed
and `HttpOnly` so only this server can write it, `SameSite=Lax` so it survives the provider's
hand-back (a top-level navigation), and short-lived so a browser abandoned mid-chain forgets it.

`/oauth2/federation/logout/callback` redirects to the caller's `post_logout_redirect_uri`, carried in the memo, and never decides. That value was matched against the client's registered `post_logout_redirect_uris` before the row was destroyed; the endpoint re-checks only that it is an absolute URI naming a place rather than code, because anything that forwards a browser is one mistake from being an open redirect wearing the issuer's hostname. With no memo, or one that fails that check, it redirects to `/user/logout`, whose page says the browser is signed out.

Absent an `end_session_url` on the provider row, none of this happens: `/oauth2/logout` redirects straight to the validated `post_logout_redirect_uri`, or, when none was sent or it did not match, to `/user/logout` with no query, whose page says the browser is signed out. A browser with no session reaching `/user/logout` with a sign-out's parameters goes back to `/oauth2/logout` instead, which still owes the app its return; the bare redirect cannot loop. `/user/logout` answers the same destination as `redirect_to`, empty when there is none.

### The upstream hop, without a provider branch

Two fields on the provider row, so a new upstream is a row and never a code change:

| Field | Meaning |
|---|---|
| `end_session_url` | Where to send the browser. May already carry query parameters — a Cognito row bakes its `client_id` in here, because Cognito's logout endpoint is not the OIDC-standard shape |
| `end_session_redirect_param` | The query parameter that upstream uses for "come back here". Defaults to `post_logout_redirect_uri`; a Cognito row sets `logout_uri` |

Absent means no hop: a deployment that never fills these behaves exactly as it did before.

## Security analysis

| Concern | What stands in the way |
|---|---|
| A stolen cookie | A bearer credential for one browser, bounded by a rolling retention window, revocable because the record is server-side, and endable remotely from the user agent list |
| A planted cookie (session fixation) | The `__Host-` prefix makes a cookie set by a neighbouring host on the shared gateway domain unusable here, and the server never adopts a presented value — a matching row donates only its `sid` and `created_at`, and a new cookie is always minted. The flow cookie `__Host-esp_flow_id` carries the prefix for the same reason: a plantable flow id means a victim's login completes into the attacker's flow and the code is delivered to the attacker's registered `redirect_uri` |
| A cookie still valid after the person signed in again | Re-authentication deletes the row the presented cookie named, so the value it replaced stops resolving |
| A cookie-less row being presented for SSO | Its key is the hash of a discarded value: no cookie exists to present, and the lookup fails closed. `origin` makes the property auditable from the data as well as from the argument |
| A long-lived user agent with no calendar cap | Accepted deliberately, and safe only because the cookie is revocable **and** sensitive actions step up. The cap is replaced by revocation plus per-action re-authentication, not by a short timeout |
| A single one-time code buying a long-lived user agent | The code is the first factor, as it is today; what changes is retention, not assurance. Bounded by remote revocation — which first-party users gain here for the first time — and by step-up. `amr` records `otp` and `sms` separately, so an SMS-authenticated user agent can be capped or step-up-gated without a code change |
| A client's provider restriction bypassed via SSO | `allowed_providers` is checked at the shortcut as well as at `/user/login/federation/start` |
| Script reading the session | `HttpOnly` |
| A table read yielding a usable cookie | Only the hash is stored |
| Keeping someone signed in past what the provider allowed | Retention is bounded by `auth_time` plus the lower of the deployment value and the provider's declared maximum |
| Re-federation quietly extending recency | `auth_time` is set only by a real authentication; rolling either clock never touches it |
| A capped provider orphaning live grants | The clamp: a family cannot be minted or rotated past its user agent's `expires_at` |
| An orphaned family — a signed-in app with no user agent | Prevented by the invariant; the one minted from a code redeemed after sign-out is refused and deleted at its first rotation |
| A browser timeout signing applications out | It cannot: families are deleted only by the deliberate acts listed above |
| A third-party client minting a user agent | Only a human login establishes one; `client_credentials` never does, and the native path is gated on the client's `allow_direct_token` |
| A relying party unable to demand fresh authentication | `prompt=login` and `max_age` are honoured before any shortcut is considered |
| Another site signing a person out (logout CSRF) | Only an `id_token_hint` minted under the cookie's own session ends it unasked; anything else is the person's decision on `/user/logout`, whose answer needs the cookie and its `csrf_token`. See [The sign-out question](#the-sign-out-question) |

## Known limitations

- **An app-only user agent loses its cookie while staying known.** Only a front-channel event re-issues
  the cookie, so a user agent used solely through an app keeps its row, its app logins and its user-agent-list
  entry, but the browser must sign in again. `cookie_expires_at` is what makes this state visible
  rather than mysterious.
- **A late browser re-login after the cookie lapsed creates a second row.** With no cookie to match,
  the re-login mints a new `sid`; the app's still-live family stays under the old row. For genuinely
  distinct contexts that is correct — two user agents — and for one browser it is a cosmetic duplicate a
  client may avoid by revoking its old token first.
- **Re-installing an app without signing out creates a second cookie-less row.** An app that revokes its login first removes its row; otherwise the old one lingers until its retention passes or the person deletes it, and `user_agent_name`, `user_agent_type` and `signed_in_at` are what tell them apart.
- **`amr`/`acr` are recorded and nothing consumes them yet.** Reserved with a named consumer
  (step-up), not dead fields.
- **Step-up is specified but not wired to any endpoint.** Until it is, a long-lived user agent is
  bounded only by revocation and retention.
- **Back-channel logout is not implemented.** Ending a user agent deletes the families under it, which is
  complete for an application holding its own tokens. It does not tell a product's *server* to drop a
  session it already holds, so the first product that runs a BFF is what makes the fan-out necessary.
  Adding it needs a `backchannel_logout_uri` field on the client registry, which does not exist yet.
- **A user agent ended elsewhere is not pushed to open tabs.** A tab keeps its access token until it
  expires and its refresh fails; nothing notifies it sooner.
- **Access tokens outlive revocation.** They are verified by signature, so a user agent ended now does
  not invalidate an access token already issued; the window is that token's remaining lifetime.

## Migration

The model above is the current state of the code; this is the one-time data step for a deployment that already holds rows under the old schema. Existing session rows carry `expires_on` and no `origin`; reads enforce `expires_at`, so no stale row is ever *usable*, but a one-shot backfill copying `expires_on` → `expires_at` is recommended before the first deploy so pre-existing sessions are not swept early. Absent `origin` reads as `browser` (the federation callback was the only writer before this model). Refresh families with `sid == ""` stay unclamped; the offline grants among them are listed by `GET /v1/user/connected-apps`. No change to the `espuser-refresh-tokens` schema.

## Out of scope

Provider-side session lifetime, which is the provider's own policy and is not discoverable over
OpenID Connect — hence `session_max_ttl_seconds` being an operator's assertion on the provider row.
Back-channel logout to product BFFs; push-to-open-tabs on remote sign-out; risk-based
re-authentication; wiring specific endpoints to step-up; a first-party `acr` vocabulary; renaming a
user agent from the dashboard; the device authorization grant (RFC 8628), which is what a keyboard-less
user agent needs and is not what a phone app needs.
