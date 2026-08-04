---
name: deploy
description: Run make deploy targeting the region/profile from AWS_REGION/AWS_PROFILE if set, else the default AWS profile and its configured region. Supports per-stack-group deploys.
allowed-tools: Bash, Read, Grep
---

Deploy the CDK stacks via the Makefile.

## Resolve the target first

The Makefile already resolves the target:

- `REGION` = `$AWS_REGION` if set, else `aws configure get region`
- `PROFILE` = `$AWS_PROFILE` if set, else `default`

1. Print the resolution before deploying and state it to the user:
   `echo "region=${AWS_REGION:-$(aws configure get region)} profile=${AWS_PROFILE:-default}"`
2. Sanity-check the credentials actually work and match the intended account:
   `aws sts get-caller-identity` (with the resolved profile). If this fails or the
   region resolves to empty, stop and report — do not deploy blind.
3. **Always ask the user to confirm the resolved account/region/profile and the
   stack groups about to be deployed, and wait for an explicit yes before running
   any `make deploy*` target, unless the user has already explicitly approved the deployment.** A deploy mutates live infrastructure; confirmation
   is per-invocation and is never inferred from an earlier approval in the session.
   If the resolved profile is anything other than a development one, say so
   explicitly in the confirmation prompt.

## Deploy

- Whole platform: `make deploy` (= `deploy-all`; builds all lambdas, gathers stack
  inputs per group, builds the admin dashboard, deploys every stack group from
  `Stackfile.yaml`, then uploads outputs).
- Single stack group: `make deploy-<group>` (groups come from `Stackfile.yaml`,
  e.g. `rmng`, `espuser`, `alexa`, `acc`, `claim`). Prefer this when the user names
  a component — it's much faster than `deploy-all`.
- **`claim` is intentionally excluded from `deploy-all`** (it creates a billable KMS
  CA key). Deploy it only when the user explicitly asks: `make deploy-claim`.

Deploys are long-running: run them with a generous timeout (or in the background)
and stream/report progress rather than letting the command time out silently.

## After

- Report which stack groups deployed, into which account/region/profile, and surface
  any CloudFormation errors verbatim.
- On a stuck custom resource or rollback, point at `scripts/unstick_custom_resource.py`
  rather than retrying blindly.
- Do not run `make destroy` or modify `Stackfile.yaml` as part of this skill.
