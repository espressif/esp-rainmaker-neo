# Hosted Sign-in Pages

## What this is

The pages a person sees while signing in to, consenting for, or signing out of ESP User: one small
Preact application served by ESP User itself under `/user/`. Applications never call these pages;
browsers reach them from `/oauth2/authorize` and `/oauth2/logout`.

## Why it is needed

- **The password is typed on our own page** and checked server-side, so no client ever sees it and
  the browser leaves our origin only for a provider that signs people in on its own page.
- **The pages never hold a token.** They finish a sign-in by returning a redirect that carries an
  authorization code, so tokens reach the client only through its PKCE-bound code exchange.
- **One origin allows a strict policy.** Every script, style and image comes from the issuer, so the
  page can refuse everything else.

## Routes

| Route | Served by | Purpose |
| --- | --- | --- |
| `GET /user/login` | authorize Lambda | The sign-in page for the flow named by the `__Host-esp_flow_id` cookie |
| `POST /user/login/password` | authorize Lambda | A password typed on the page ([authorize-code-flow.md](authorize-code-flow.md#sign-in-page-service-served)) |
| `GET /user/login/federation/start` | authorize Lambda | A provider's button, carrying the `flow_id` the page was drawn for: starts the upstream leg ([federation.md](federation.md#get-userloginfederationstart)) |
| `GET`, `POST /user/login/consent` | authorize Lambda | The consent screen and the person's answer ([authorize-code-flow.md](authorize-code-flow.md#consent--clients-we-do-not-ship)) |
| `GET /user/login/restart` | authorize Lambda | "Start again": re-runs the original `/oauth2/authorize` request of an expired sign-in |
| `GET /user/logout` | authorize Lambda | The "Sign out?" question, or the signed-out page ([sso-sessions.md](sso-sessions.md#the-sign-out-question)) |
| `POST /user/logout` | sessions Lambda | The person's yes to "Sign out?" |

Every route sits under `/user/` because the page's references are relative, which keeps whatever
stage or base path the browser arrived on.

## Screens

| Screen | Drawn when | Calls |
| --- | --- | --- |
| Sign in | A live flow | `POST /user/login/password`; a provider button navigates to `/user/login/federation/start`; the one-time code is requested and verified through the `otp` provider |
| Create an account, verify it, reset a password | Chosen from the sign-in screen for a password provider | `POST /v1/user/auth/signup`, `/signup/verify`, `/password-recovery`, `/password-recovery/confirmation`, each with the `provider` the page showed ([legacy-user-auth.md](legacy-user-auth.md)) |
| Consent | A sign-in held for consent | `POST /user/login/consent` |
| Sign out? | `/oauth2/logout` could not end the session at once | `POST /user/logout` |
| Signed out | A sign-out finished with no destination | none |
| Error | The request cannot proceed (no flow, an expired flow, a bad client or redirect) | "Start again" when there is a flow to restart |

A sign-in or consent answer returns `{ "redirect_to" }`, and the page navigates there: the client's
`redirect_uri` with the `code` and `state`, or the consent screen.

## What the page is told

The page is described in one block delivered with the HTML,
`<script type="application/json" id="esp-page-data">`, so the form is on screen at first paint and
there is no second endpoint to authorize. The browser never executes `application/json`, so the
block needs no CSP nonce.

| Field | What the page does with it |
| --- | --- |
| `client.name`, `client.logo_uri` | Name the relying party, and on the consent screen show its logo |
| `providers[]` | One entry per provider this client may use, in `provider_name` order: `name`, `display_name`, `method` (`password` for our form, `redirect` for the provider's own page, never the row's `type`) and, for a brand's own button (a row named `google`, `apple` or `github`), `brand`, else a checked `logo` |
| `otp_enabled` | Whether to offer code sign-in at all |
| `flow_id` | The sign-in the page was drawn for, sent back with every answer so one a newer sign-in replaced is refused (`flow_replaced`) |
| `login_hint`, `idp` | Prefill the username, preselect a provider. Suggestions from the relying party, never decisions — nothing is authorized on them |
| `restart_url` | Where "start again" points; present only when there is a flow to restart |
| `consent` | `username` and the requested `scopes`, for the consent screen |
| `logout` | `username` and the `csrf_token` the sign-out answer must carry |
| `signed_out` | Draw the signed-out page |
| `error` | `code` and `message` of the terminal error; never a client name or a redirect URI |
| `links` | The operator's `support`, `privacy` and `terms` pages for the footer, from the SSM parameters `/espuser/base/support-url`, `/espuser/base/privacy-url` and `/espuser/base/terms-url`, which the deploy creates as `none`; only absolute `https` URLs are passed, so `none` is not drawn, and the page reads them at most every five minutes, so a change shows within that |

## Which providers are drawn

An `oidc` row whose `password_verifier` is `cognito`, with a `client_id`, is answered on our own
form; an `oidc` row naming no `password_verifier` is a redirect button; an `otp` row offers a
one-time code; any other row is left out. With one password pool the form asks for an email and
password and names no pool; with several, each is a button beside the redirects, and the pressed
one is sent as the `provider` of `POST /user/login/password`.

Brand marks ship inside the app, never from the registry, because markup from an operator-written
row on a sign-in page is markup somebody else controls. A row named `google`, `apple` or `github`
gets that brand's own "Sign in with" button; any other draws a plain "Continue with" button labelled
with its `display_name`, showing the row's `logo` when it is a valid inline image (drawn as `<img>`,
which runs no script) and a shield otherwise.

## Security

Every page response carries:

```
Content-Security-Policy: default-src 'none'; script-src 'sha256-<script>'; style-src 'sha256-<stylesheet>'; img-src 'self' data:; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'
X-Frame-Options: DENY
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
Cache-Control: no-store
```

- The page carries its script and stylesheet inline, and the policy admits exactly those bytes by
  their SHA-256 hashes, so a page is one response and no other script can run.
- No CDN, no analytics, no remote fonts. A client or provider logo is an inline `data:` image,
  which is why `img-src` admits `data:` ([admin-clients.md](admin-clients.md)).
- The page never mints a token, never sees signing keys, and never puts claims in a URL.
- The flow and session cookies are `__Host-` prefixed, `HttpOnly`, `Secure` and `SameSite=Lax`, so
  the page's script cannot read them and another site cannot post an answer with them.
- `frame-ancestors 'none'` and `X-Frame-Options: DENY` stop another site framing the consent or
  sign-out question to borrow a click.

## Build and delivery

The app lives in `src/espuser/hosted-ui/` (Preact, TypeScript, built by Vite with
`make hosted-ui-build`). The build output in `dist/` is compiled into the authorize Lambda with
`go:embed`, which inlines the script and stylesheet into every page at start-up, so the UI and the backend that serves it are one artifact: they cannot deploy out of
step, a rollback of one is a rollback of both, and there is no bucket or distribution to
invalidate. A Lambda built without the bundle answers the page routes with `500 The sign-in page is
not available.` and serves the OAuth endpoints as usual.
