# Audience scoping — which API a token opens

> Companion to [authorize-code-flow.md](authorize-code-flow.md) and
> [auth-flows.md](auth-flows.md), which specify the requests this parameter rides on, and to
> [admin-clients.md](admin-clients.md), which specifies the registry field that gates it.

## Overview

A client may name the API a token is for. That name becomes the token's `aud`, and a
resource server that checks `aud` accepts only the tokens minted for it.

The parameter is RFC 8707 `resource`, and it is accepted at `/oauth2/authorize` and at
`/oauth2/token`. A client may only name a resource it is registered for.

### Key design decisions

**One signing key means `aud` is the boundary, not the signature.** Every token this
issuer mints is signed with the same key, so a valid signature proves who minted a token
and says nothing about who it was minted for. A resource server that verifies the signature
and the issuer but not the audience accepts every other product's tokens. `aud` is the only
thing separating them.

**Absent registration means no resource may be requested, never any.** A client with no
registered resources may name none. The reverse reading — absent meaning unrestricted —
would make the parameter enforce nothing for exactly the clients nobody configured, which is
the population most likely to need it.

**At most one resource per request.** Two values are refused rather than producing one
credential good at two APIs. A client that needs two makes two requests, and a token that
leaks then opens one door.

**Omitting the parameter leaves `aud` as the `client_id`.** That is the shape a caller
that does not use this extension sees, and it is what makes the parameter additive rather
than a change every existing client must react to.

**The audience is validated before the user is sent anywhere.** An unregistered resource is
refused at the authorize request, not at the code exchange. Completing an entire login —
redirect, credentials, whatever the provider asks — only to be told the request was never
valid is not an acceptable failure.

**Exact string equality.** A resource is matched literally, like a redirect URI and for the
same reason: a prefix or suffix rule is an opening, and a normalisation rule is a second
implementation to disagree with the first.

**A rotation cannot change the audience.** The resource is remembered on the refresh
family, so every access token minted from that family carries the same `aud`. A refresh
cannot be used to widen what a credential opens.

## Pre-requisites

The client's registry row lists the resource in `allowed_resources`. Each entry is an
absolute URI with a host and no fragment; see [admin-clients.md](admin-clients.md) for the
exact rule and why it is stricter than RFC 8707 §2.

## Design

### Where the parameter is accepted

| Request | Behaviour when `resource` is present |
|---|---|
| `GET /oauth2/authorize` | Validated against the client's registry row before the login flow starts. Unregistered redirects to the client with `invalid_target`. The value is remembered on the login flow |
| `POST /oauth2/token`, `authorization_code` | The audience settled at the authorize step is applied to the issued access token |
| `POST /oauth2/token`, `refresh_token` | The audience recorded on the family is applied. Rotation keeps it |
| `POST /oauth2/token`, `client_credentials` | Validated against the registry row on the request itself, since there is no user leg to have settled it |

### What the caller receives

| Situation | `aud` on the access token |
|---|---|
| `resource` names a registered API | that resource |
| `resource` omitted | the `client_id` |
| `resource` names something unregistered | no token — the request is refused |
| two `resource` values | no token — the request is refused |

The id token is unaffected. Its audience is the client, by OpenID Connect Core definition:
it identifies the user to the application that asked, and is not a credential for a resource
server.

### Errors

| Condition | Response |
|---|---|
| Unregistered resource at `/oauth2/authorize` | Redirect to the client with `error=invalid_target`. The redirect URI has already been validated at this point, so returning the error to the client is safe |
| Unregistered resource at `/oauth2/token` | `400` with `error=invalid_target` |
| Two `resource` values | `400` with `error=invalid_target`, at both endpoints |
| A refresh whose resource has since been removed from the client | `400` with `error=invalid_target`; the family is left intact, so restoring the entitlement resumes it |

## What a resource server must check

Verifying the signature and the issuer is not enough, because every product's tokens carry
both. A resource server accepts a token only when, in this order:

1. the algorithm is the one expected and the key is named by `kid`, taken from the issuer's
   published key document rather than from the token;
2. the signature verifies against that key;
3. `iss` is this issuer;
4. **`aud` names this API** — an absent audience fails closed and is never read as
   "addressed to everyone";
5. `exp` and `nbf` place it inside its window;
6. the token is an access token and not an id token;
7. `sub` is present.

Scope is deliberately not part of that list. Which scope a request needs is a property of
the route, so it is asked of the verified caller by whoever knows the route — and a missing
scope is a `403`, because the caller proved who they are and a fresh token would be
identical.

## Security analysis

| Concern | What stands in the way |
|---|---|
| A token minted for one product accepted by another | `aud` names the callee, and each resource server checks it |
| A client minting a token for an API it has no business calling | `allowed_resources`, validated before the login begins |
| A client nobody configured minting for anything | Absent registration means none |
| One credential good at two APIs | Two `resource` values are refused |
| A refresh widening what a credential opens | The audience is fixed on the family and survives rotation |
| An unusable audience discovered only in production | Entries are validated when the registry row is written, not when a client first asks |

## Known limitations

- **Resource values are compared literally.** `https://api.example.com` and
  `https://api.example.com/` are different resources. A deployment must use one spelling
  consistently in the registry and in every client.
- **Profile claims ride the access token.** Scope-gated contact claims are stamped on the
  access token as well as the id token, so a resource server named in `aud` receives them
  whether or not it asked.

## FAQs

**Why not put several audiences in one token?** Because the value of the parameter is that
a leaked credential opens one thing. A token good at two APIs is the situation this exists
to prevent, and `aud` permitting a list in the general case does not oblige an issuer to
mint one.

**Why does an omitted `resource` not fail?** It would break every client that predates the
parameter, and the resulting token is not dangerous: its audience is the client id, which no
resource server accepts as its own name.

**A token verifies perfectly and my API still rejects it.** That is the audience check
working. Compare the token's `aud` against the exact string the API expects; a token minted
for another product, or with `resource` omitted, verifies flawlessly and is still not for
you.
