# Admin Authentication

## What is admin authentication

How **admin / superadmin** users of the platform sign in and are identified. Admins are separate from end users: end users use the passwordless ESP User identity provider, while **admins use a dedicated Cognito user pool directly**. There is no passwordless flow for admins and no application database record for them.

## Why it is needed

Admin sign-in, token refresh, sign-out, and password management are exactly what a managed user pool already provides. Owning an API in front of that adds a hop and a surface to maintain for no added behaviour, so admin apps talk to the pool directly. What the platform still needs is a small amount of admin *identity* that the rest of the system reads from the admin's token — this is provisioned once, when the admin account is confirmed.

## Pre-requisites

- The admin user pool exists and admin accounts are created in it (self-signup is disabled; admins are provisioned).

## Key rules

- Admins authenticate directly against the admin user pool. There is no platform admin-auth API.
- Admins have no application database record. An admin is represented solely by their user-pool account.
- Every admin carries two identity attributes on their account: an internal **user id** and a **superadmin** flag. Both travel in the admin's token and are the only source of admin identity the rest of the platform reads.
- The internal user id is **stable and derived from the admin's login identity** (email, or phone) — the same identity always maps to the same user id, with no stored mapping. Normalisation is case-insensitive (trim + lowercase). The **same derivation is used for end users**, so an identity has one user id everywhere.
- Superadmin authorisation is decided from the token's superadmin claim, never from a database lookup.

## Identity provisioning

Because the user id is derived deterministically from the login identity, it is known before the account exists. It is therefore set in the **same operation that creates the admin account** — alongside the superadmin flag — rather than in a later step or a login-time side effect. An admin's identity is present the moment the account is usable.

## Access control

- Superadmin-only platform surfaces (admin configuration, node administration, assume-role, integrations, and similar) authorise by reading the superadmin flag from the admin's verified token.
- The superadmin OAuth-client registry authorises the same way — it is unaffected by how admins obtain their tokens.

## Design decisions

- **Admins use Cognito directly, not the OIDC provider.** The IdP is for end users; admin auth is standard managed-pool auth.
- **No admin-auth API.** Sign-in / refresh / sign-out / password flows are the pool's own operations, called by the admin client directly.
- **No admin database record.** Admins never appear in the end-user details store or elsewhere; superadmin checks read the token claim, so nothing depends on an admin row existing.
- **Deterministic, stored-nowhere user id.** The internal user id is derived from the normalised login identity, so it is stable across account re-creation with no mapping to keep. Trade-off accepted: changing an admin's login identity changes their user id.
- **One derivation for everyone.** End users derive their user id the same way, so identity is consistent across admin and end-user surfaces. End users still keep a details record; admins do not.
- **Set at account creation, not at login**, so it is present the moment the account is usable and is not a fragile login-time side effect. No separate attribute-update step.

## Out of scope

- End-user login and end-user identity (see [auth-flows.md](auth-flows.md)).
- The superadmin OAuth-client registry ([admin-clients.md](admin-clients.md)) — kept as-is.
