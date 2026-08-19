# Deploy & Publish: Two Deployment Flows and Operator Inputs

## 1. Overview

ESP RainMaker Neo is deployed two different ways, and the CDK apps behave differently in each:

- **Self-deploy** — the maintainer runs `make deploy` (locally or from CI)
  to `cdk deploy` the stacks straight into a target account.
- **Installer / published template** — the maintainer runs `make publish`
  to synthesize region-agnostic CloudFormation templates and upload them as a
  versioned artifact. A *different* account's operator later deploys that
  template with plain CloudFormation (no CDK), supplying their own inputs as
  CloudFormation stack parameters.

A single environment variable, `CDK_PUBLISH`, distinguishes the two at synth
time. When `CDK_PUBLISH=true`, the apps drop account-specific inputs so nothing
private is baked into a redistributable template, and instead expose the same
inputs as `CfnParameter`s the deploying account fills in.

---

## 2. The `CDK_PUBLISH` switch

`CDK_PUBLISH=true` is exported by exactly two `make` commands, both of
which only *synthesize* — they never deploy:

- `make synth` — writes `cdk-output.yaml`.
- `make publish` — synthesizes to `cdk.out.<group>` and
  uploads the templates + assets as the installer artifact.

The real `cdk deploy` paths (plain `make` and the alexa multi-region loop)
never set it, so **`CDK_PUBLISH` is false on every actual deploy** and true only
while producing a distributable artifact.

The apps read it to decide whether the template is account-specific or
redistributable:

- `cdk/apps/espuser.py` / `cdk/apps/rmng.py` / `cdk/apps/alexa.py` — resolve
  operator inputs and cross-stack values from local files unless publishing, in
  which case parameters get empty defaults to be overridden at deploy.
- `cdk/utils/app_common.py` — skips the region gate for published
  (region-agnostic) templates.

---

## 3. Operator input chain (self-deploy)

Prompt-style inputs declared in `cdk/Stackfile.yaml` reach the template through this
chain, all before `cdk deploy`:

```
RMNG_<PARAM> (env) → gather_stack_inputs.py → rmng-inputs.json
    │  the CDK app reads it at synth
    ▼
stack construct → resource property
```

Each parameter's env var and `rmng-inputs.json` key are derived from its name
(`FooBar` → `RMNG_FOO_BAR` / `foo_bar`). Resolution precedence per parameter:
env var if non-empty, else an existing value in `rmng-inputs.json` (left
unchanged), else a TTY prompt. Non-interactive runs (CI) never block — an unset
value with no existing entry is skipped, leaving the input empty.

---

## 4. Operator inputs on published templates

Published templates expose prompt inputs as CloudFormation parameters with
empty defaults; the deploying account supplies its own values at create/update
time. A parameter value, when set, takes precedence over the synth-time value;
otherwise the synth-time value applies.

Synth-time values belong in the template *body*, not in a parameter default:
CloudFormation keeps a parameter's stored value across stack updates and ignores
a changed default, so a default-carried value would not take effect on an
existing stack. A value in the body changes the template, which lets dependent
resources (e.g. custom resources keyed on a trigger property) re-run when the
value changes.

---

## 5. Migrating resources between stacks

When a construct moves from one stack to another — as the voice-assistant
integrations did when Alexa and Google Home gained their own stacks —
CloudFormation does not transfer the resources. The old stack deletes them and
the new stack creates them, because each stack owns only what its own template
declares. Two consequences follow.

**Deploy the losing stack first.** API Gateway routes and Lambda function names
are unique per API and per account+region, so if the new stack is created while
the old one still declares the same route or function name, the create fails
with an already-exists error. Deploying `rmng-core` first releases them.

**The endpoint is briefly absent.** Between the two deploys the routes do not
exist and calls to them return 404. This affects the integration's own
endpoints only — for Alexa and Google Home that is the configuration API and,
for Google Home, the fulfillment endpoint. Device control paths and the shared
API are untouched. Google Home fulfillment resumes without reconfiguring the
Google Home project: the path is unchanged, so only its availability lapses.

For the Alexa and Google Home split specifically:

```bash
make deploy          # rmng-core sheds the integration resources
make deploy-gva      # Google Home fulfillment + config API
make deploy-alexa    # skill Lambda (3 regions) + config API (backend region)
```

Outputs move with their stack. `GVAFulfillmentUrl` is published by
`rmng-gva-core` rather than `rmng-core`; consumers read the new location and
fall back to the old one so a deployment that predates the split keeps working
(`scripts/rmng_outputs.py`). The dashboard uses the presence of each stack in
the published outputs to decide whether to offer that integration at all, so an
assistant whose stack is not deployed simply does not appear.
