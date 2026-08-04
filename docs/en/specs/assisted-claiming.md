# Assisted Claiming Feature Design

## 1. Overview

Assisted claiming adds a cloud-signed node onboarding path alongside the
existing ones. Instead of an operator generating a CA, signing a device
certificate offline and uploading the finished PEM, the device generates its own
P-256 key pair, sends a Certificate Signing Request, and receives a certificate
signed by a CA whose private key lives in AWS KMS. The private key never leaves
the device, and no per-device certificate has to be produced or flashed at
manufacture.

The node ID is assigned by the cloud *before* the CSR is produced, so
certificate identity is determined by a server-side record rather than by
anything the caller supplies.

### Key Design Decisions

**A claim confers no permission over the node.** It assigns a node ID and vends
a certificate; that is all. Device control comes from the primary user-node
mapping, established separately by challenge-response — proof of physical
possession. The reservation is a `{device, claimant} -> node_id` lookup and must
never be read as an authorization record; any future capability scoped to a
node's owner should key on the primary mapping, not on who claimed it. There is
no tenancy concept in this model.

**The node ID comes from the reservation, never from the CSR.** The CSR
contributes only its public key; its subject is discarded and the Common Name
rebuilt server-side. This removes a class of defect: there is no Common Name to
validate and no way for a caller to influence the identity it receives.

**The claim key carries the caller.** Keyed `{mac_addr, claimant_id}`, because
this variant cannot prove possession — the caller simply asserts a MAC. Without
the caller dimension, one caller claiming another's MAC would resolve to the
same node ID and replace the certificate on a device already in service: an
unauthenticated denial of service against someone else's hardware.

**Certificates are revoked by deactivation, not expiry.** Leaves are issued for
100 years with no renewal path, so expiry is not a control. Replaced
certificates are detached and deactivated but never deleted — a deleted
certificate's identity is free to be registered again by anyone.

**The quota is a lifetime cap.** Reservations are never deleted, so removing a
node from an account does not return its slot. A reclaimable quota would be no
bound at all, since a caller could mint, release and mint again without limit.

**One certificate serves two roles.** Omitting `ExtendedKeyUsage` is what allows
the issued certificate to be both the AWS IoT client certificate and (when
Matter is enabled) a Matter attestation certificate: IoT does not require
`clientAuth`, and the Matter attestation profile does not permit an EKU at all.

## 2. Background

### 2.1 What claiming replaces, and what it does not

The established superadmin onboarding paths — single-node certificate upload on
`POST /v1/admin/nodes` and bulk registration from a certificate CSV — are
unaffected and remain superadmin-only in every deployment mode. Assisted
claiming is an additional path, enabled per deployment.

### 2.2 Variants

| Variant | Possession proof | Node ID keyed on | Quota |
|---|---|---|---|
| `user_authenticated` | none — the caller asserts a MAC | `{mac_addr, claimant_id}` | yes |
| `device_attested` | challenge/response against the device secret | `mac_addr` (1:1) | no |

`device_attested` is specified but **not implemented**; synthesis refuses to
deploy it. Its `device_secrets` wiring, challenge issuance and validation remain
to be built.

A deployment runs one variant. The two have different claim-key shapes, so with
both live the same device could receive two different node IDs depending on
which path claimed it.

### 2.3 What a claiming-CA signature attests

Under `user_authenticated`, a certificate signed by the claiming CA attests that
*some authenticated user of this deployment asked for this node ID*. It is
**not** evidence that the hardware exists or that the claimant possesses it. A
deployment needing that guarantee needs `device_attested`, where the signature
additionally means the device answered a challenge against the secrets service.

## 3. Design

### 3.1 Endpoints

Neither endpoint takes query parameters. Both are created with API Gateway's
default **`AWS_IAM`** authorization, so callers **SigV4-sign** them with
Identity-Pool credentials obtained from `POST /v1/user/credentials`. 

**`POST /v1/claim/initiate`** — assigns a node ID to a MAC address.

```text
Request:  { "mac_addr": "AA:BB:CC:DD:EE:FF" }
201/200:  { "node_id": "1b4e28ba-2fa1-4d3b-a3f5-ce6f8a9b0c2d" }
```

`201` for a new reservation, `200` for an existing one. Idempotent per claim key
so a factory-erased device returns as the same node. The response carries only
server-generated fields; `mac_addr` is not echoed.

Errors: `400` malformed MAC, `401` unauthenticated, `403` quota reached, `404`
claiming disabled, `405` non-POST, `500` reservation store unavailable.

**`POST /v1/claim/verify`** — exchanges a CSR for a signed certificate.

```text
Request:  { "mac_addr": "...", "csr": "-----BEGIN CERTIFICATE REQUEST-----...",
            "capabilities": ["s3"] }
201:      { "node_id": "...", "certificate": "...", "ca_certificate": "..." }
```

The CSR must be PEM, ECDSA P-256, at most 8 KB, and its self-signature is
verified. `capabilities` must be re-supplied on a re-claim, since they are
applied to the new certificate. Tags are **not** accepted from the request.

Errors: `400` bad body or CSR, `403` no reservation for this caller, `404`
claiming disabled, `405` non-POST, `500` lookup/signing/binding failure.

### 3.2 MAC normalization

Separators (`:`, `-`, `.`) are stripped and the value upper-cased; the result
must be 12 or 16 hexadecimal characters. Only the normalized form is stored or
queried. The MAC identifies a device within the reservation key, so storing
spellings verbatim would give one physical device several node IDs, certificates
and quota slots depending on how a client wrote the address.

### 3.3 Node identity

Node IDs are canonical RFC 4122 version-4 UUIDs — the standard 36-character
hyphenated lowercase form (`1b4e28ba-2fa1-4d3b-a3f5-ce6f8a9b0c2d`). This is the
same format the DAC and RainMaker pre-provisioning services emit, so a node's ID
reads identically whichever path minted it. One value is the IoT Thing name, the
MQTT client ID and the certificate Common Name. A claimed node that later joins a
Matter fabric has its Matter operational Node ID derived from this value the same
way every other non-Matter-native node does; the node ID is not itself a Matter
Node ID.

Because verify ignores the CSR subject, a device holding a stale node ID would
receive a valid certificate and then fail to connect, since the IoT policy
requires the client ID to equal the Thing name. The device must adopt `node_id`
from the initiate response before installing the certificate.

### 3.4 Certificate profile

| Field | Value |
|---|---|
| Version / key / signature | v3, ECDSA P-256, `ecdsa-with-SHA256` |
| Subject | `CN = node_id`, plus operator-configured subject attributes (§3.9) and Matter VID/PID when enabled |
| BasicConstraints | critical, `CA=false` |
| KeyUsage | critical, `digitalSignature` |
| ExtendedKeyUsage | **absent** — see Key Design Decisions |
| SKID / AKID | present |
| Serial | random, ≤ 20 octets |
| Validity | 100 years by default, operator-configurable (§3.9), clamped to the CA's expiry |

The clamp matters: the CA is minted once and leaves are issued from then on, so
without it every leaf would outlive its issuer by however long the CA had been
in service, and chain validation would break on the CA's expiry date while the
leaf still looked valid. The default configuration gives the CA 20 years of
headroom beyond the leaf lifetime so leaves get their full term in practice.

### 3.5 Signing key custody

The claiming CA private key is a non-exportable KMS asymmetric key
(`ECC_NIST_P256`, `SIGN_VERIFY`), used through a signing shim that calls
`kms:Sign` with `MessageType: DIGEST`. Beyond non-extractability this makes every
issuance a CloudTrail event attributable to a principal and request ID, which a
key held in a database cannot provide.

The CA certificate cannot be produced at synth time — it must be signed by the
KMS key, which only exists after deployment. It is therefore minted at runtime
through the superadmin bootstrap API (§3.9), not at deploy time. Mint-once is
enforced by the write itself (SSM no-overwrite), not a check, so concurrent or
repeated mint calls cannot replace an existing CA; replacing it is a separately
authorized rotation. When no subject is configured, the CA subject is derived
from the signing key's account and region so deployments do not all mint CAs
with an identical subject but different keys.

### 3.6 Certificate binding and re-claim

Binding reuses the shared node register/update helpers. On a first claim the
Thing is created and the certificate attached; on a re-claim the new certificate
is attached and every previously attached one detached and deactivated.

Re-claim is the ordinary path after a factory erase, not a rare operator
correction, so the replacement carries the claim's capabilities and fires the
same registration hook a first registration fires. Without that, a device with
`s3`/`kvs`/`bridge` would silently lose those policies and fail later at the
credential provider with AccessDenied rather than visibly at the point of change.

### 3.7 Provenance tags

Claimed nodes are stamped with admin tags reusing the keys the dashboard already
registers as searchable fleet-index fields:

| Tag | Value |
|---|---|
| `registered_from` | `claim`, or `auth-claim` under `device_attested` |
| `created_by` | internal user ID of whoever drove the claim |
| `registered_at` | RFC 3339 UTC, first registration only |

These are set by the server and not accepted from the request: letting a caller
supply `registered_from` would let them label their node as
dashboard-registered, which is exactly the claim an administrator needs to
trust. `registered_at` is not re-stamped on re-claim, so it keeps meaning
"entered the fleet"; the replacement certificate's own `notBefore` records when
it was re-issued.

The dashboard reads shadows through the fleet index and never queries
`rmng-nodes`, and `iot:DescribeThing` carries no creation date — so without
these tags none of this provenance is visible to an administrator. `admin_id`
and `reg_ts` on the node row are unaffected and remain available to any future
admin API.

These fields are searchable but are not fleet-index *custom fields* — AWS caps
those at 5 and three are already spent on device type/model/fw_version. So no
aggregations or suggestions, and no range query on `registered_at` until it is
promoted.

### 3.8 Data model

`rmng-node-id-reservations`, partition `claimant_id`, sort `mac_addr`.

| Attribute | Role |
|---|---|
| `claimant_id` | claiming caller's internal user ID, or a per-MAC sentinel under `device_attested` |
| `mac_addr` | normalized device address |
| `node_id` | assigned node ID |
| `ca_id` | which CA signed the current certificate |
| `created_at` | Unix epoch |

No GSIs. The claimant is the partition key, so the only query beyond a point
read — counting a caller's reservations for quota — is a base-table partition
`Query`, run with a consistent read so a burst cannot under-count past the cap.
This keeps the table free of GSI backfill at deploy time, and stays free of a
hot partition only because every `claimant_id` is high-cardinality: a real user
ID, or the `device_attested` sentinel sharded per MAC. A future no-caller
variant must shard its claimant the same way rather than reuse one fixed value.

The reservation is not an authorization record. It exists to keep node IDs
stable and idempotent, to bound minting, and to isolate callers from each other.
There is no delete method on the data layer, and the initiate Lambda has no
`dynamodb:DeleteItem` grant.

### 3.9 CA configuration and bootstrap

The whole claiming feature is configured and the CA minted at runtime through a
superadmin-only API, not at deploy time. This keeps operator-chosen configuration
out of the deploy inputs (the claim group has no `rmng-inputs.json` dependency)
and mirrors the other admin configuration endpoints: every call is gated on the
caller being a superadmin, and a regular user is refused.

Claiming configuration is a single JSON document held in SSM:

- `mode` — the claiming variant. Empty/omitted means claiming is configured off,
  and the initiate/verify handlers fail closed. It must name an implemented
  variant (`user_authenticated`) when set, validated at the config API.
- `max_nodes_per_claimant` — the per-caller lifetime quota for the
  `user_authenticated` variant; 0/omitted uses the default (§5.5).
- the subject shared by the CA and every leaf (country, state, locality,
  organization, organizational unit, email), the CA common name, the CA validity
  and the leaf validity.

Absent or empty fields fall back to the built-in defaults (§3.4). The leaf
Common Name is always the node ID and is never taken from configuration. The
`mode` and quota take effect on subsequent claims immediately.

| Endpoint | Effect |
|---|---|
| `POST /v1/admin/claiming/config` | store the certificate configuration |
| `GET /v1/admin/claiming/config` | read the configuration and CA status |
| `POST /v1/admin/claiming/ca` | mint the CA — mint-once; `{"force": true}` rotates |
| `GET /v1/admin/claiming/ca` | return the CA certificate and status |

Minting reads the current configuration, signs a self-signed CA with the KMS key
(§3.5) and publishes the certificate. Mint-once is enforced by the write itself,
so the first call is the only one that mints and a repeat reports the existing CA
unchanged. Rotation is an explicit `force` on the mint call — the sole action
that overwrites the published CA, and therefore the one that leaves every
certificate already issued by the previous CA unverifiable against the published
one. There is no delete: the CA is never removed through the API, so a rotation
is always deliberate and a teardown never revokes the fleet.

Leaf configuration is read at issuance, so a change to the subject or leaf
validity takes effect on subsequent certificates with no redeploy. CA
configuration takes effect only at the next mint or rotation, since the CA is
minted once. Until an admin has minted the CA, `verify` fails closed while
`initiate` still reserves node IDs. The signing key is provisioned by CDK at
deploy time (§3.5, §4); the API configures and mints but never creates the key.

## 4. IAM and CDK

Claiming is **off by default**: its asymmetric KMS key is billed monthly whether
or not a certificate is ever issued, so the `make deploy` all-groups sweep skips
the `claim` group and it is deployed only when named explicitly
(`make deploy-claim`). Its template is still published by `make publish`, so the
module ships with every release and can be enabled per install.

Enablement is a runtime step, not a deploy-time gate. Deploying the group stands
up the infrastructure but leaves claiming inert: the initiate/verify handlers
fail closed until a superadmin sets a `mode` in the claiming configuration (§3.9)
**and** mints the CA. The variant and the per-claimant quota live in that
runtime configuration document, not in `rmng-inputs.json` — the claim group has
no dependency on that file at all.

- `ClaimBase` (base stack): reservation table and CA key, both `RETAIN`.
  Destroying either is unrecoverable — the key cannot be regenerated, and losing
  the table would re-assign every claimed device a fresh node ID, orphaning the
  Thing, certificate and shadow it already has.
- `ClaimCore` (core stack): the claim Lambda and its routes, plus the superadmin
  CA configuration and bootstrap API (§3.9). Certificate identity and validity
  are set through that API at runtime, never through `rmng-inputs.json`.
- `kms:Sign` and `kms:GetPublicKey` are granted to the claim handler (leaf
  issuance) and the CA bootstrap Lambda (CA minting), and to no other principal,
  via the SSM-published key ARN rather than a cross-stack export.
- claim-initiate holds `GetItem`/`PutItem`/`Query` on the reservation table
  (the `Query` is the quota count on the base table — no index ARN, since there
  is no GSI) — deliberately no `DeleteItem`.

## 5. Security Analysis

### 5.1 Identity cannot be influenced by the caller

The node ID comes from the reservation and the certificate subject is rebuilt
server-side, so a CSR requesting any other identity still yields a certificate
naming the reserved node.

### 5.2 Cross-caller isolation

Two callers claiming the same MAC receive distinct node IDs, so there is no way
to ask for another user's node. Presenting a legitimately issued certificate as
another user's node is refused at the broker, because the IoT policy binds the
client ID to the Thing the certificate is attached to.

### 5.3 Fail-closed issuance

A reservation-lookup error, an entitlement mismatch, a signing failure or a
binding failure all abort before a certificate reaches the caller. A signed but
unbound certificate is never returned, so nobody obtains usable key material for
a Thing that was not successfully bound.

### 5.4 Revocation on re-claim

The previous certificate is detached and deactivated, and an integration test
confirms against a live broker that it no longer connects. Without this, anyone
holding a superseded certificate would retain access indefinitely.

### 5.5 Abuse bounds

Per-caller lifetime quota (default 20, overridable at runtime via the
`max_nodes_per_claimant` field of the claiming configuration). Under
`device_attested` the secrets service is the abuse control and there is no user
quota — its claim key carries no caller to count against.

## 6. Tests

### 6.1 Unit

Certificate profile pinned field by field, including the absence of
`ExtendedKeyUsage` and the leaf-never-outlives-CA clamp; claim-key isolation;
MAC normalization equivalence; quota including repeat-claim exemption;
concurrent-claim convergence on one reservation; fail-closed paths; mint-once
idempotence; provenance tag construction.

### 6.2 Integration

Against a deployed environment: full claim; **MQTT connect with the issued
certificate**; the same certificate authenticating the primary user-node mapping
by challenge-response; re-claim replacing the certificate and the superseded one
no longer connecting; cross-user impersonation refused at the broker; unique
node per `{user, MAC}`; admin visibility of all three provenance tags;
`registered_at` surviving a re-claim. CA bootstrap is exercised through the
superadmin API — configuring identity, a first mint, an idempotent repeat, and a
forced rotation that replaces the published CA — and a non-admin caller is
refused.

The quota test is skipped by default — it permanently consumes the pooled test
user's lifetime allowance. Run with `RUN_CLAIM_QUOTA_TEST=1` against a throwaway
user. The suite's own claims are budgeted to stay under the default cap for one
run; the capability probe deliberately uses an invalid MAC so asking whether
claiming is enabled costs no quota.

## 7. What Does Not Change

Superadmin single-node and bulk registration, node association, OTA, shadow and
group flows are untouched. A deployment with claiming disabled creates no
claiming resources at all — no KMS key, no table, no Lambdas, no routes — and
behaves exactly as before.

## 8. Out of scope

- **IoT CA registration.** Needed only for JITP/JITR and CA-validated
  `RegisterCertificate`. Registration here is CA-less, so it adds no
  authenticity: the backend mints the certificate and binds it in one operation,
  and the IoT policy resolves through the certificate-to-Thing attachment rather
  than the subject.
- **`device_attested`.** Specified in §2.2; blocked at synth.
- **Matter DAC issuance.** The profile is compatible by construction (no EKU),
  but the PAI, Certification Declaration and VID/PID chain are separate work.
- **Reclaiming quota.** Deliberate; see Key Design Decisions.

## 9. Future work

- Promote `registered_at` to a fleet-index custom field if range queries
  ("claimed since X") are wanted.
- An admin API over `rmng-nodes` with a by-admin GSI, which would give
  `admin_id` and `reg_ts` a reader.
- CA rotation: `ca_id` is recorded per node so a second CA can be introduced and
  the first retired without ambiguity.
