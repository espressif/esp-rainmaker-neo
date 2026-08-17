# Building ESP RainMaker Neo from source

This guide covers **Option 3** of the [README](README.md): building and deploying
the cloud yourself, and modifying it. For the public deployment or the packaged
installer, see the [documentation](https://docs.neo.rainmaker.espressif.com/) instead.

This repository holds the cloud backend, its infrastructure, the admin dashboard, docs, and the test tooling. It is broken down below by component — **Backend**, **Dashboard**, **Apps**, **Firmware**, **Voice assistants**, **CLI** — each with the tests that cover it.

## Repository layout

```
esp-rainmaker-neo/
├── src/                      all Go code, plus the CDK constructs that deploy it
│   ├── rmneo/                core product
│   │   ├── handlers/         deployed Lambdas, grouped by subdomain; each leaf
│   │   │                     directory name == binary name == Lambda name, and
│   │   │                     holds its *_main.go + stack.py (core.py/base.py sit
│   │   │                     one level up, per subdomain)
│   │   ├── db/               one package per DynamoDB table + shared core
│   │   ├── node/ group/ service/ user/ nodeadmin/ notification/ file/
│   │   │                     domain libraries (pure Go)
│   │   └── stacks/           domain-wide CDK stacks
│   ├── espuser/              user management: auth, OAuth, IdP, scopes
│   ├── alexa/                Alexa integration (deploys to Alexa regions)
│   ├── claim/                assisted claiming (its own CDK app)
│   ├── mcp/                  MCP server + OAuth proxy (proxy is its own Go module)
│   ├── utils/                cross-domain primitives (rlog, jwtutil, validation, …)
│   ├── awsutils/             AWS SDK wrappers (kmsutil, sesutil, espdynamodb, …)
│   ├── test/                 Go test support (mock/, testutil/)
│   ├── tools/rmng-lint/      repo-specific static analysers
│   └── esp-cloud-common/     shared Go module (git submodule)
├── cdk/                      apps/ (one per stack group) · utils/ (shared
│                             constructs) · cdk/Stackfile.yaml · outputs/ · cdk.out/
├── dashboard/                admin dashboard (React + Vite + Tailwind)
├── cli/                      morpheus.py — drive a deployment interactively
├── py_sdk/                   Python SDK
├── test/                     integration tests, simulators, itest infra stacks
├── docs/                     documentation source (Sphinx) + api/ (OpenAPI/AsyncAPI)
├── scripts/                  scripts
├── tools/                    operational helpers
├── deployment/               Dockerfile and CI definitions
├── assets/                   logos and static assets
├── Makefile                  build, test, deploy and publish targets
└── cdk.json                  CDK config (must stay at the repo root)
```

Two conventions worth knowing:

- **A Lambda's name comes from its `*_main.go` filename**, not its directory
  (`Makefile` uses `notdir`). Directories are free to rename; renaming a
  `*_main.go` renames a deployed AWS resource. `addon_modules/` is the exception —
  there the Lambda takes its *directory* name.
- **Python lives only in `handlers/` and `stacks/`.** Everything else under
  `src/` is Go.

## Backend

Prerequisites:

- Your own AWS account
- [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) (latest)
- [Go 1.26.6](https://go.dev/doc/install)
- [Python 3.12](https://www.python.org/downloads/)
- [Node.js 24](https://nodejs.org/)

```shell
# One-time: submodules + Python environment
git submodule update --init --recursive
python3 -m venv myenv && source myenv/bin/activate
pip3 install -r requirements.txt

# Point at the right account/region (optional if your defaults are correct)
export AWS_REGION=ap-south-1
export AWS_PROFILE=dev

# First deploy: sets up the S3 bucket and bootstraps CDK
make setup
# Subsequent builds + deploys
make deploy
```

`make deploy` builds the Go Lambdas **and** the dashboard, then deploys both.

To understand the backend, start from the [specs and API references](README.md#specs--documents) and the documentation under [docs/](docs/).

**Tests**

```shell
make lint        # rmng-lint over ./src/...
make test        # Ginkgo unit tests + coverage, for rmng and its submodules

# Integration tests: one-time setup per environment — deploys the test infra
# and seeds test users/nodes via morpheus.py --setup-test-data
make itest-setup
make itest       # pytest test/itest/, HTML report in build/tests/
```

## Dashboard

React + TypeScript + Vite + Tailwind, in [dashboard/](dashboard/). You only need the steps below to work on the dashboard itself with hot reload — a plain `make deploy` already builds and ships it.

Prerequisites: **Node.js 24** (ships npm 11.x)

```shell
cd dashboard
npm install

npm run dev      # http://localhost:5174
```

See the [dashboard README](dashboard/README.md) for the full dev-vs-deployed config story.

**Tests** — from `dashboard`:

```shell
npm run lint         # eslint + i18n key check
npm run typecheck    # tsc --noEmit
npm test             # vitest run
```

## Apps

The **ESP RainMaker Home** app (React Native + Expo, built on TypeScript SDKs) is released from [espressif/esp-rainmaker-home](https://github.com/espressif/esp-rainmaker-home) — install it from the store links in the [README](README.md), select the **ESP RainMaker Neo** platform, then point it at your own deployment by scanning the QR code displayed in the admin dashboard.

**Tests** — none here; the app is tested in its own repo. What this repo can do is exercise the same APIs the app uses, via the integration suite and the CLI. You can also use the app simulator, [test/app_sim.py](test/app_sim.py), to test your own deployment.

## Firmware

Device firmware lives in the [firmware SDK](https://github.com/espressif/esp-rainmaker-neo-firmware). To try a device against the public deployment, flash a prebuilt binary with [ESP Launchpad](https://espressif.github.io/esp-launchpad/?flashConfigURL=https://espressif.github.io/esp-rainmaker-neo-firmware/launchpad.toml) — it flashes over WebSerial, so use a Chrome- or Edge-based browser.

For a device on *your* deployment, build an example from the SDK and flash it alongside a **factory partition** — the `rmaker_creds` NVS binary carrying the node id, its IoT certificate and key, and your endpoints. Generate it from the admin dashboard, flash `bin/<node-id>.bin` per device, and upload the `node_certs.csv` to register the nodes.

**Tests** — no hardware needed: [test/device_sim.py](test/device_sim.py) and the CLI's simulated-device mode speak the same MQTT protocol a real node does.

## Voice assistants (Optional)

Both integrations are cloud-side and live in this repo, so they deploy with everything else. You can add more device types or customise interactions and redeploy. Use the admin dashboard to configure the voice assistants.

- **Alexa Smart Home** — See [docs/en/specs/alexa.md](docs/en/specs/alexa.md), which also covers driving skill creation and updates over SMAPI.
- **Google Voice Assistant** — See [docs/en/specs/gva.md](docs/en/specs/gva.md), which also covers creating and updating the Google Home Action.

**Tests** — covered by the integration suite; the specs above flag the manual account-linking steps that can't be automated.

## CLI (Morpheus)

`cli/morpheus.py` is an interactive CLI to exercise a deployment end-to-end — as a user, a super admin, or a simulated device — without needing a phone app or real hardware.

It reuses the same Python 3.12 venv as the backend, and needs AWS credentials for the target account/region plus the deployment's outputs — either a local `rmng-outputs.json` at the repo root, or the published URL:

```shell
python3 cli/morpheus.py --user user@example.com
```

See the [CLI guide](cli/README.md).

**Tests** — the CLI has no suite of its own. `make itest-setup` calls `morpheus.py --setup-test-data` to seed test users and nodes to get started.
