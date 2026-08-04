---
name: sync-docs
description: Sync API definitions (swagger YAMLs) and specs (docs/en/specs/) with the code on the current branch, documenting any new/changed features the branch introduces.
allowed-tools: Bash, Read, Edit, Write, Grep, Glob
---

Bring the documentation in line with the code on the **current branch**.

## What "docs" means here

- **API definitions** — the OpenAPI/AsyncAPI YAMLs in `misc/swagger/`:
  `Api_Swagger.yaml` (platform API), `User_Api_Swagger.yaml` (sign-up/auth),
  `MQTT_Node.yaml`, `MQTT_User.yaml`, `Push_User.yaml`, `Test_Webhook.yaml`.
- **Specs** — the feature specs in `docs/en/specs/` (one markdown file per feature area,
  plus `docs/en/specs/admin/`).

## Spec structure

One file per feature area, kebab_or_snake-cased after the feature (`group.md`,
`node_assoc.md`, `notifications-webhooks.md`). A single `# Title` H1, then H2 sections
in this order:

```
# <Feature>

## Overview                   — what it is and what problem it answers
### Key design decisions      — the load-bearing choices, each with its why
## Pre-requisites             — preconditions (auth, registration, config)
## Architecture               — a flow diagram plus a short walkthrough
## Design                     — the core; one subsection per component or flow
## APIs                       — routes, request/response shapes, status codes
## Access Control             — per-role permissions (primary / secondary / subentity)
## Data model                 — tables, keys and columns
## Security analysis          — the isolation each layer enforces
## Limits                     — capacity and shape bounds, and what exceeding one costs
## Known limitations          — what does not work, and what a client sees
## Out of scope               — owned elsewhere, with a pointer
## FAQs
```

Only `Overview` and `Key design decisions` are universal; include the rest when the
feature has them, and keep the surviving sections in this relative order. Cross-link
siblings with a blockquote note under the H1 rather than duplicating shared material.

`Key design decisions` is the most valuable part of a spec — a reader should be able to
understand a constraint without reading the code.

### Conventions

- Fenced code blocks for topics, IAM policies, CSV shapes, and CLI examples; ASCII
  diagrams for flows (no image assets).
- Markdown tables for data models and per-column semantics.
- Concrete identifiers, not placeholders — real table names (`rmng-user-endpoints`),
  real topics, real key formats (`<integration_id>#<endpoint_id>`).
- Endpoint details live in the spec's `APIs`/`Endpoints` section **and** in swagger;
  keep them consistent, with swagger authoritative for exact schemas.
- Known gaps are stated as limitations in prose ("a caller above this cannot be issued
  credentials at all"), never as `TODO:` markers or proposed fixes.
- **No implementation detail.** Out: Go snippets; handler, function, class and struct
  names (`HandleAcceptGrant`, `CapabilityHandler`, `flipRule`); source paths and
  `**File:**` / `**Files:**` / `**Defined in:**` blocks. Answer "where is this enforced"
  in words, not with a path. In: AWS/CDK config literals, IAM policy detail, JSON payload
  and shadow shapes, DynamoDB attribute names, MQTT topics and event names, HTTP routes
  and status codes, IoT rule SQL, CLI examples. Test coverage may be described as prose
  (what is verified), never as test function or file names.
- **Describe the present, not a diff.** No "an earlier design…", "previously…", "no
  longer…", "this feature introduces…", or `not X` parentheticals correcting an old name.
  Rationale is welcome as a property of the design, not as a rejection of what shipped
  before. Git history carries the diff.
- The product is **ESP RainMaker Neo** in prose — never "RMNG". Leave `RMNG_*` env vars,
  `rmng-*` resource names and `RMBaseApi` as they are.
- New spec files must be registered in `docs/en/index.md` in **two** places, or Sphinx
  drops them: (1) the hidden `{toctree}` under the right `:caption:` — *Identity and
  access*, *Admin Dashboard*, *Node lifecycle*, *Features*, *Voice assistants*,
  *Platform* — as a path without the `.md` extension (`specs/<name>`), and (2) a prose
  bullet under the matching `## <Caption>` heading: `- [<name>](specs/<name>.md) — <one
  line on what it covers>`. Admin specs additionally get a bullet in
  `docs/en/specs/admin/index.md`.

## Procedure

1. **Find what the branch changed**:
   - `git merge-base HEAD origin/main` then `git diff <base>...HEAD --stat` to scope the change.
   - Focus on handler code (`src/`, `src_*/`, `addon_modules/`), request/response structs,
     new routes/lambdas in `cdk/`, and new config keys.
2. **Map code changes to doc surfaces**:
   - New/changed endpoint, method, request/response field, error code, or auth scheme →
     the matching swagger YAML. Follow the file's existing tag organisation and
     security-scheme declarations (`CognitoAuthorizer`, `sigv4`, etc.).
   - New/changed feature behaviour, flows, limits, or architecture → the matching file in
     `docs/en/specs/` (create a new spec file only when no existing one covers the area;
     mirror the tone and structure of neighbouring specs).
3. **Cross-check both directions**:
   - Every endpoint the branch adds/modifies appears in swagger with accurate schemas
     (verify field names/types against the actual Go request/response structs, do not guess).
   - Every user-visible behaviour change is reflected in the relevant spec.
   - Remove/adjust doc content for anything the branch deleted or renamed.
4. **Validate**: if the swagger YAML was touched, sanity-check it parses
   (`python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" misc/swagger/<file>`).

## Rules

- Derive schemas from the code (structs, validators, handler logic) — never invent fields.
- Keep swagger `description` text consistent with the file's existing voice; keep the
  SPDX header intact.
- Do not commit; leave the changes in the working tree and summarise what was synced
  (endpoint → file mapping) so the user can review.
