# Admin Authentication

## What is admin authentication

How **admin** users of the platform sign in and are identified. Admins are separate from end users: end users use the passwordless ESP User identity provider, while **admins use a dedicated Cognito user pool directly**. There is no passwordless flow for admins and no application database record for them.

## Why it is needed

Admin sign-in, token refresh, sign-out, and password management are exactly what a managed user pool already provides. Owning an API in front of that adds a hop and a surface to maintain for no added behaviour, so admin apps talk to the pool directly. What the platform still needs is a small amount of admin *identity* that the rest of the system reads from the admin's token — this is provisioned once, when the admin account is confirmed.

## Pre-requisites

- The admin user pool exists and admin accounts are created in it (self-signup is disabled; admins are provisioned).

## Key rules

- Admins authenticate directly against the admin user pool. There is no platform admin-auth API.
- Admins have no application database record. An admin is represented solely by their user-pool account.
- Every admin carries an internal **user id** on their account. It travels in the admin's token and is the only source of admin identity the rest of the platform reads. A `custom:super_admin` flag also exists on the pool's schema, but nothing authorises on it — it is inert data.
- The internal user id is **stable and derived from the admin's login identity** (email, or phone) — the same identity always maps to the same user id, with no stored mapping. Normalisation is case-insensitive (trim + lowercase). The **same derivation is used for end users**, so an identity has one user id everywhere.
- Admin authorisation is **membership of the admin user pool** and nothing further. A token the admin pool issued is an admin token; there is no second, higher tier and no per-admin privilege claim.

## Identity provisioning

Because the user id is derived deterministically from the login identity, it is known before the account exists. It is therefore set in the **same operation that creates the admin account** rather than in a later step or a login-time side effect. An admin's identity is present the moment the account is usable.

## Access control

- Admin-only platform surfaces (admin configuration, node administration, assume-role, integrations, and similar) authorise by establishing that the caller's verified token came from the admin pool. Every admin reaches every admin surface.
- The OAuth-client registry authorises the same way — it is unaffected by how admins obtain their tokens.

## Design decisions

- **One admin tier.** Admin-pool membership is the whole privilege. Provisioning an account into the admin pool is already a deliberate, out-of-band act, so a second in-band privilege flag added a tier to keep in sync without adding a boundary. `custom:super_admin` remains on the pool schema as an inert flag so existing accounts and tooling are undisturbed, but no code reads it for an authorisation decision.
- **Admins use Cognito directly, not the OIDC provider.** The IdP is for end users; admin auth is standard managed-pool auth.
- **No admin-auth API.** Sign-in / refresh / sign-out / password flows are the pool's own operations, called by the admin client directly.
- **No admin database record.** Admins never appear in the end-user details store or elsewhere; the admin check reads the verified token's issuing pool, so nothing depends on an admin row existing.
- **Deterministic, stored-nowhere user id.** The internal user id is derived from the normalised login identity, so it is stable across account re-creation with no mapping to keep. Trade-off accepted: changing an admin's login identity changes their user id.
- **One derivation for everyone.** End users derive their user id the same way, so identity is consistent across admin and end-user surfaces. End users still keep a details record; admins do not.
- **Set at account creation, not at login**, so it is present the moment the account is usable and is not a fragile login-time side effect. No separate attribute-update step.

## Out of scope

- End-user login and end-user identity (see [auth-flows.md](auth-flows.md)).
- The OAuth-client registry ([admin-clients.md](admin-clients.md)).
