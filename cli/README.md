# Getting Started with `morpheus.py`

`morpheus.py` is an interactive CLI for configuring and exercising an ESP RainMaker Neo deployment end-to-end: it authenticates users against Cognito, registers/destroys IoT nodes, drives the user-facing REST API, connects to MQTT as a user or a simulated device, and manages groups, sharing, mobile push platforms, Matter association, and licenses. It allows developers to test and validate any deployment based feature, without needing a device or phone app around.

The script talks to a *specific* deployment. To do that it needs the following things:

1. **Deployment outputs** — Each ESP RainMaker Neo deployment captures it commonly used outputs into an output file. This file is used by phone apps, dashboard, this CLI or any other clients to communicate with the deployment. This CLI requires this file to point at a specific deployment. It will either use a local `rmng-outputs.json` (local) or a published URL via the `--client-outputs` parameter.
2. **AWS Credentials** - To keep things simple, in the code, this CLI also relies on directly accessing AWS resources using Python's boto3 module. The CLI expects that the terminal, from where the CLI is executed, can perform actions on the AWS Account and region, that the client outputs point to. It checks this on startup and refuses to run against a mismatched account or region; pass `--skip-account-check` to override.

---

## 1. Prerequisites

- Python 3.12
- AWS credentials in your environment (`aws configure` / `AWS_PROFILE` / env vars), for the AWS account and region.
- A deployed ESP RainMaker Neo environment, and its outputs file.

## 2. Install dependencies

```bash
python3 -m venv myenv
source myenv/bin/activate
pip install -r requirements.txt
```

## 3. Get the deployment outputs

`morpheus.py` reads the merged stack outputs from a JSON file keyed by stack name (`espuser-base`, `espuser-core`, `rmng-base`, `rmng-core`, …). You can supply it two ways:

**Local file (default).** With no flag, `morpheus.py` reads `rmng-outputs.json` from the repo root, so it resolves the same whichever directory you run from:

```bash
python3 morpheus.py --user username@example.com
```

**From a URL.** Point `--client-outputs` at a published outputs file (e.g. the per-region client-outputs in S3) and it's fetched over HTTP at startup — no manual download:

```bash
python3 morpheus.py --client-outputs https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com/ap-south-1/rmng-client-outputs.json --user user@example.com
```

`--client-outputs` accepts a local path **or** an `http(s)://` URL. A relative path resolves against the repo root, not your working directory. When omitted, the default is `rmng-outputs.json` at the repo root.

## 4. Pick an identity

Everything the CLI does, it does as somebody. There are two ways to get that identity.

### Use an account that already exists

Pass any account already provisioned in the deployment.

```bash
python3 morpheus.py --user someone@example.com           # prompts for the password
python3 morpheus.py --user admin@example.com --is-admin     # authenticate against the admin pool
```

Add `--is-admin` when the account is a super admin, so it authenticates against the admin pool rather than the end-user one.

The password is read from `--password`, then `RMNG_PASSWORD`, then an interactive prompt. Prefer the prompt or the environment variable: a password passed in `--password` is visible to other processes on the machine and is kept in your shell history.

This covers the user side only. The CLI can also simulate a device, more about that later in this README.

### Or seed test users and devices

For quick validation against a scratch deployment, `--setup-test-data` creates a known set of users and nodes from `test_config.json`: it registers every user and node, creates a default `Home` group per user, associates nodes flagged with `associate_to`. This helps you quickly get started on development with a set of test entities.

```bash
python3 morpheus.py --setup-test-data
```

On a fresh checkout this also writes `test_config.json` from `test_config.default.json`, generating passwords and device certificates. It needs admin AWS credentials, since it provisions users in Cognito. The seeded super admin is marked `"super_admin": true` in that file, so `--is-admin` is not needed to use it.

Since we created both test users and devices through `test_config.json`, we can now act as a user or as a device.

```bash
python3 morpheus.py --user somebode@example.com
python3 morpheus.py --device node_rsa
```

`--destroy-test-data` removes them again: the seeded devices, their groups, and any leftover `test-*` certificates. It deletes only test-created things, leaving the rest of the deployment alone.

## 5. Entering a context

`morpheus.py` drops you into an interactive prompt scoped to a user or device. Type commands at the prompt; `q` / `quit` exits the context.

There are **two layers** here. `--user` and `--device` are the lower-level **raw operations** — you drive one user-side or device-side call at a time from the prompt. `--app-sim` and `--device-sim` sit on top of them and are **simulators**: each runs the sequence of those raw operations that we recommend a real phone app (`--app-sim`) or a real device (`--device-sim`) performs.

| Command | Layer | Enters | Notes |
| -------------------------------------------- | --------- | ---------------- | ------------------------------------------------------------- |
| `python3 morpheus.py --user user@example.com` | raw | User CLI | Auth, groups, API calls, MQTT, sharing, push, Matter, license |
| `python3 morpheus.py --device node_rsa` | raw | Device CLI | Connect, shadows, to-cloud, group info, direct notify |
| `python3 morpheus.py --app-sim user@example.com` | simulator | App simulator | Recommended sequence of `--user` ops; needs a user id from config |
| `python3 morpheus.py --device-sim node_multi` | simulator | Device simulator | Recommended sequence of `--device` ops; needs a device id from config |

All four contexts accept `--client-outputs <path-or-url>` to target a specific deployment; the simulators are handed the same resolved source as the raw contexts.

`get` / `post` / `put` / `patch` / `delete <path> [data]` let you hit any API route directly. Type an unknown command at any prompt to print its full command list.

### Raw operations

Lower-level contexts — you drive one user-side or device-side call at a time.

#### `--user` — User CLI

Acts as an end user. Main context for the user-facing REST API and a user's MQTT session. Takes any existing account, or a seeded one by index or name.

```bash
python3 morpheus.py --user user@example.com
```

```
auth                                  # authenticate + register the user
list_groups                           # fetch & cache the user's groups
create_group My Home                  # create a group
assoc node_rsa <group_id>             # associate a device into a group
get v1/user/nodes                     # raw GET against the user API
connect                               # assume role + connect to MQTT
subscribe node_rsa params local       # subscribe to a node's named shadows
register_client_ios com.app.id <token>  # register a push endpoint
```

#### `--user <super-admin>` — Super-admin CLI

Same prompt as the User CLI, but for a super admin, so auth routes through the admin pool. Adds deployment-wide admin commands — these return 403 for a regular user. Pass `--is-admin` for an existing account; a seeded one is already marked `"super_admin": true` in the config.

```bash
python3 morpheus.py --user admin@example.com --is-admin
```

```
register_ios_platform key.p8 <key_id> <team_id> <bundle_id> sandbox
register_android_platform service-account.json
list_mobile_platforms
alexa_setup
setup_ses_sender
```

#### `--device` — Device CLI

Acts as a physical node using the cert/key from `nodes[]`, connecting to AWS IoT over MQTT/TLS.

```bash
python3 morpheus.py --device node_rsa
```

```
connect                               # connect + subscribe to from_cloud topic
get_group_info                        # fetch the node's group/subgroup IDs
publish params {"Light":{"power":true}}   # update a named shadow
to_cloud {"temp":25}                  # publish to the node->cloud topic
direct_notify file:test/direct_notification_example.json
```

### Simulators

Higher-level contexts that sit on top of the raw operations — each runs the sequence of raw calls we recommend a real device or phone app performs.

#### `--device-sim <thing_name>` — Device simulator

A higher-level fake node that reports params and tags. Requires the node's `test_config.json` entry to have `node_cfg` and `node_tags` (only `node_multi` / `node_switch` qualify in the default config).

```bash
python3 morpheus.py --device-sim node_multi
```

```
update_params {"Light":{"brightness":80}}   # push a param update
update_tags {"location":"hall"}              # push a tag update
```

#### `--app-sim <user-id>` — App simulator

Simulates the mobile app for a user: groups, automations, schedules, BLE provisioning, bridges.

```bash
python3 morpheus.py --app-sim user@example.com
```

```
list                                  # list the user's groups
select <group_id>                     # choose the active home
update <node_id> {"Light":{"power":true}}
automation ...                        # drive an automation
prov ...                              # BLE provisioning
```
