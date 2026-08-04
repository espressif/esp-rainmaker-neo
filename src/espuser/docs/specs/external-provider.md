# External identity providers

Every upstream identity provider is configuration, not deployment. A provider is one row in
`espuser-identity-providers`, reached over the public internet, and our deployment holds no authority
over it. A provider in another AWS account, another organisation, or a non-AWS product is described
the same way. Cognito is one such row.

## The provider row

| Field | Meaning |
| --- | --- |
| `provider_name` | Key; the value `/oauth2/federation/start?provider=` takes |
| `type` | `oidc` for a brokered upstream, `otp` for a hosted login page |
| `enabled` | Whether `/oauth2/authorize` offers it. Unset counts as disabled |
| `display_name` | Label for the login chooser |
| `issuer` | The provider's OIDC issuer; endpoints and keys are discovered from it |
| `authorize_url` | Where a login begins: the upstream authorize endpoint, or the hosted page for an `otp` row |
| `client_id` | The one app client we hold for this provider |
| `client_secret` | That client's secret |
| `scopes` | Space-separated, requested on the upstream authorize |
| `token_endpoint_auth` | How the client authenticates at the token endpoint: `client_secret_basic` or `client_secret_post` |
| `password_grant` | Whether `client_id` also accepts a direct username/password exchange |
| `attribute_mapping` | Our claim name → the provider's claim name |
| `token_url`, `userinfo_url`, `jwks_url` | Optional pins that override discovery |

Nothing else about a provider exists anywhere: no SSM parameters, no Lambda environment variables, no
IAM grant naming it, no deploy step. Adding a provider is one write to this table.

## One confidential client per provider

A provider is reached through exactly one app client, and it is confidential. The brokered
authorization-code flow and the legacy password grant share it.

It is confidential because a public client can be driven by anyone who learns its id — client ids are
not secret — which would expose sign-up, password reset and password sign-in on the provider's pool to
the internet. Only our issuer holds the secret. Clients of ours never talk to the provider at all,
so they never need a client of their own.

Sharing the client means the provider requires proof of possession on operations that are otherwise
unauthenticated. Cognito's form of that proof is a keyed hash of the username and client id under the
client secret, sent with every such call.

## The secret is stored as given

`client_secret` holds the secret in the row. DynamoDB encrypts at rest and the table is reachable only
through the IAM grants on the issuer's own Lambdas, so the trust boundary is the table's access policy.

## Keys and endpoints come from the issuer

The provider's authorize, token, userinfo and key-set endpoints are read from its discovery document,
cached per issuer for the container lifetime. Its verify keys are then fetched from the key-set URL and
cached per URL, so upstream key rotation is picked up on cold start. A row may pin any of these as a
URL, which a provider that publishes no discovery document needs and nothing else does.

## The password grant

The legacy `/v1/user/auth/*` surface needs six things from a provider: password sign-in, sign-up,
resend of a sign-up code, sign-up confirmation, password-reset start, and password-reset confirmation.
All six are unauthenticated provider operations — they take the app client id and no AWS credential —
which is what lets the surface work against a provider in any account.

The first enabled provider that sets `password_grant` and carries a `client_id` serves the surface.
When none does, it fails closed: the routes exist but authenticate nothing.

Operations that presume ownership of the provider's pool are not available. Nothing on this path is
given a pool identifier, so an operation that would administer the pool has nothing to address.
Operator-driven user management against an external provider would require an explicit cross-account
role and is not offered.

## The bundled pool

Our own CDK creates a Cognito pool and seeds it as one ordinary row, so a fresh deployment has a
working login without manual configuration. It is a default, not a dependency: deleting the row leaves
a system with no provider configured, and adding a different row is the same operation for Cognito as
for anything else.

## Related

- [federation.md](federation.md) — the brokered authorization-code flow
- [legacy-user-auth.md](legacy-user-auth.md) — the password surface and its token conversion
