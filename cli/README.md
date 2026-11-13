# `esp-morpheus` — the ESP RainMaker Neo console client

Configure and test any ESP RainMaker Neo deployment end to end, with no device and no phone app.

**What it does**

- Signs users in
- Registers and destroys IoT nodes
- Calls the user-facing REST API
- Connects to MQTT as a user or as a simulated device
- Manages groups, sharing, mobile push platforms and Matter commissioning

**What it needs**

- **The deployment's outputs:** `rmng-outputs.json`, or the file or URL given with `--client-outputs`. Every deployment publishes this one file for the phone apps, the dashboard and this client.
- **AWS credentials, only for a few `admin` commands**, such as `admin test-data setup`. They change real AWS resources, so they refuse credentials for any account other than the deployment's. When present, any command also uses them once, to fetch a deployment's missing test config from S3 the first time that deployment is used on a machine.
- **An admin sign-in, for some other `admin` commands**, such as `admin platforms`.

It installs as a command and runs from any directory, against any deployment, with no checkout of this repository.

---

## 1. Install

```bash
pip install esp-morpheus
```

`user <id> node camera-stream` needs the heavy WebRTC stack (aiortc, OpenCV), shipped as an extra:

```bash
pip install 'esp-morpheus[camera]'
```

From a checkout, install it in place:

```bash
pip install -e ./cli
```

Needs Python 3.12 or later.

## 2. Point it at a deployment

The outputs are a JSON file keyed by stack name (`espuser-base`, `espuser-core`, `rmng-base`,
`rmng-core`, ...). Give `esp-morpheus` a path or a URL.

**A local file (default).** With no flag, `esp-morpheus` reads `rmng-outputs.json`. A relative path resolves against the repo root when run from anywhere inside a checkout (`MORPHEUS_REPO_ROOT` overrides the detected root), and against the working directory outside one.

```bash
esp-morpheus user someone@example.com
```

**A URL.** Point `--client-outputs` at a published outputs file, such as the per-region client-outputs in S3; it is fetched at startup. The bucket is always in `us-east-1`; the path names the deployment's region.

```bash
esp-morpheus --client-outputs https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com/ap-south-1/rmng-client-outputs.json \
             user someone@example.com
```

`MORPHEUS_OUTPUTS` sets the same value, so a shell session can pick a deployment once.

## 3. Pick an identity

Every command runs as some identity.

### An account that already exists

Pass any account provisioned in the deployment.

```bash
esp-morpheus user someone@example.com            # prompts for the password
```

- `user` is the end-user API only: it signs in against the end-user pool. An admin account belongs to `esp-morpheus admin`.
- `--password` belongs to `user`, so it goes before the identity; everything after the identity is the subcommand.
- The password comes from `--password`, then `RMNG_PASSWORD`, then a prompt. Prefer the prompt or the env var: `--password` is visible to other processes and lands in shell history.
- `esp-morpheus` signs in before running anything, so a wrong password is reported at once, not by whichever command runs first. A typed password gets up to three tries; one from `--password` or `RMNG_PASSWORD` fails straight away with exit code 3, since a script cannot answer a prompt.
- Exception: `auth` provisions an account that may not exist yet, so it runs even when sign-in fails. For the same reason, the interactive prompt opens on a warning.

### Or seed test users and devices

To validate a scratch deployment quickly, `admin test-data setup` creates a known set of users and nodes from `test_config.json`: it registers every user and node, creates a default `Home` group per user, and associates nodes flagged with `associate_to`.

```bash
esp-morpheus admin test-data setup
```

- On a fresh install it also writes `test_config.json` from the packaged defaults, generating passwords and device certificates. The file holds secrets: never commit or share it.
- The seeded admin is marked `"admin": true`; that is how `esp-morpheus admin` finds it without naming one.
- `node_multi` and `node_switch` are associated with `test-user1@example.com`.

It needs AWS credentials for the deployment's account (Cognito for the admin, IoT for node
certificates, S3), from any source boto3 reads. The region comes from the outputs.

```bash
export AWS_PROFILE=<profile>        # or AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
```

- "AWS account mismatch": the credentials are for a different account than the outputs name.
- A re-run reuses existing users, groups and certificates; "Node ... was not registered" means the node already exists.

The file is `~/.espressif/rainmaker-neo/morpheus/test_config.<account-id>-<region>.json`, one per deployment, inside
a checkout or not. `setup` keeps a copy in S3. Other commands read S3 only to fetch a missing file.

```bash
esp-morpheus user someone@example.com
esp-morpheus device node_rsa
```

`admin test-data destroy` removes them again: the seeded devices, their groups and any leftover `test-*` certificates, and nothing else in the deployment. `--dry-run` lists what it would delete.

### Or create the CI bot user

`admin bot-user create` creates an IAM user with AdministratorAccess for CI and writes its access keys to `bot-iam-user-credentials.json`, next to `test_config.json`. The name defaults to `bot`; another name is saved as `ci_bot_user` in `test_config.json`, so `delete` and `show` find it again.

```bash
esp-morpheus admin bot-user create      # or: esp-morpheus admin bot-user create rmng-ci
esp-morpheus admin bot-user show
esp-morpheus admin bot-user delete
```

One bot at a time:

- `create` refuses a new name until `delete` removes the current bot. There is a single credentials file, so a second bot would overwrite the first one's keys and leave it in IAM with a live admin key nothing tracks. A name IAM no longer has is stale and does not block `create`.
- A second `create` under the same name fails rather than invalidate the key CI is using. `create --rotate` replaces an existing user's keys; it is the way back when the credentials file is lost.

## 4. One command, or a prompt

Every command runs two ways. Name it on the command line and `esp-morpheus` runs it and exits:

```bash
esp-morpheus user someone@example.com group list
```

Name no subcommand and `esp-morpheus` opens a prompt bound to that identity, with the `esp-morpheus user someone@example.com` prefix implicit:

```
$ esp-morpheus user someone@example.com
User context: someone@example.com
Type help for commands, q to leave.
someone@example.com > group create My Home
someone@example.com > api get v1/user/nodes
someone@example.com > q
```

- Tab completes the command tree.
- `help` lists the commands; `help <command>` prints its full help. Both come from the tree the dispatcher uses, so neither can drift.
- A bad command, a failed call or `Ctrl-C` returns you to the prompt; `q`, `quit` or `Ctrl-D` leaves.

## 5. The command tree

```
esp-morpheus [--client-outputs SRC] [--json] [--raw] [-v]
  user [--password PW] <identity> [SUBCOMMAND...]             # no subcommand -> prompt
      auth  connect  subscribe  publish  read-shadow  upload-file
      api                    get | post | put | patch | delete
      group                  create | list | rename | add-capabilities | share
      group subgroup         create | rename | add-node | remove-node | share
      node                   assoc | remove | claim
      matter                 initiate | verify | confirm | get-noc
      sharing                list | accept | reject
      push                   register-ios | register-android | register
  device <node> [SUBCOMMAND...]                               # no subcommand -> prompt
      connect  shadow-connect  subscribe  publish  to-cloud  group-info
      set-node-config  direct-notify
  admin [IDENTITY] [--password PW] [SUBCOMMAND...]            # no subcommand -> prompt
      integrations           alexa | gva | smartthings
      platforms              register-ios | register-android | list
                             update-ios | update-android | delete
      nodes                  register | bulk-register | bulk-status
      iot-event-mode         get | set
      claiming               enable
      ses                    setup-sender | request-production
      sns                    request-production
      test-data              setup | destroy
                             gen-device <name> <rsa|ec> [--stdout] [--force]
      bot-user               create [NAME] [--rotate] | delete [NAME] | show
      guide                  alexa | gva | smartthings | ios | android
  app-sim [--password PW] <identity>
      list  select  stats  update  update-group  update-subgroup  prov
      schedule               set | get | delete
      automation             create | add-trigger | add-action | complete
                             list | get | delete
  device-sim <node>
      update-params  update-tags
```

There are **two layers**:

- `user` and `device` are the raw operations: one user-side or device-side call at a time.
- `app-sim` and `device-sim` sit on top and run the sequence of raw operations a real phone app or a real node performs.

`admin` is the third tree, and the only privileged one: everything that needs AWS credentials or the admin API is under it, so the rest of `esp-morpheus` reaches only what an ordinary account reaches. `guide` and `test-data gen-device` need neither, but sit there because each one's other half is an admin command: `guide` prints the console side of a setup, and the node `gen-device` writes is inert until `test-data setup` registers it.

### `user` — the user API and a user's MQTT session

```
auth                                       # authenticate and register the user
group list                                 # groups, with their nodes and subgroup ids
group create My Home                       # create a group
group create --matter My Home              # create it as a Matter fabric
node assoc node_rsa <group_id>             # associate a device into a group
node camera-stream <node_id> --stats-only  # view a camera's live video over KVS WebRTC
api get v1/user/nodes                      # raw GET against the user API
connect                                    # assume role, then connect to MQTT
subscribe node_rsa params local            # subscribe to a node's named shadows
push register-ios com.app.id <token>       # register a push endpoint
sharing list                               # pending shares
```

### `admin` — deployment-wide operations

Its commands split in two:

- `integrations`, `platforms`, `nodes`, `iot-event-mode` and `claiming` call the admin API, which returns 403 for a regular user, so they need an admin identity. Name one before the command; without it, `esp-morpheus` uses the admin `test_config.json` flags.
- `ses`, `sns`, `test-data` and `bot-user` call AWS directly and need no identity, only credentials for the account the outputs name.

```bash
esp-morpheus admin platforms list
esp-morpheus admin admin@example.com platforms list
esp-morpheus admin                             # a prompt over the same tree
```

```
platforms register-ios key.p8 <key_id> <team_id> <bundle_id> --sandbox
platforms register-android service-account.json
integrations alexa setup-auto
integrations smartthings setup st-config.json
nodes bulk-register nodes.csv --tags created_by:ci
iot-event-mode set sqs
claiming enable
ses setup-sender otp@company.com           # SES mails a link; the owner opens it
ses setup-sender --mailosaur               # mint a test address and open the link for you
test-data setup
bot-user create
```

### `device` — a physical node

Acts as a node using the certificate and key from `nodes[]`, over MQTT/TLS.

```
connect                                    # connect and subscribe to from_cloud
group-info                                 # the node's group and subgroup ids
publish params {"Light":{"power":true}}    # update a named shadow
to-cloud {"temp":25}                       # publish to the node-to-cloud topic
direct-notify --file notification.json
set-node-config node_config.json
```

### `device-sim <thing_name>` — the device simulator

A node that reports params and tags. Unlike `app-sim`, it needs a `test_config.json` entry, since a node authenticates with a certificate and key, which cannot be typed at a prompt. The entry also needs `node_cfg` and `node_tags`; only `node_multi` and `node_switch` qualify in the default config.

```
update-params {"Colour Light":{"Power":true}}
update-tags {"location":"hall"}
```

It connects with the node's certificate and needs no AWS credentials. To control it from the phone app:

- Sign in as the node's `associate_to` user (`test-user1@example.com` for `node_multi`); the password is in `test_config.json`. There is no QR to scan: the simulator has no BLE, and `test-data setup` already associated it.
- Or, to use your own account, share the group and accept in the app:

```bash
esp-morpheus user test-user1@example.com group list
esp-morpheus user test-user1@example.com group share <group_id> primary --username <your-email>
```

App changes print in the device-sim terminal; `update-params` sends changes back.

### `app-sim <identity>` — the app simulator

The mobile app for a user: groups, automations, schedules and BLE provisioning. It resolves and checks IDENTITY exactly as `esp-morpheus user` does, prompting for a password, so it drives any account in the deployment, not only those in `test_config.json`.

```
list                                       # the user's groups
select <group_id>                          # choose the active home
update <node_id> {"Light":{"power":true}}
automation create Evening lights
schedule get <node_id>
prov                                       # BLE provisioning
```

## 6. Output

By default a command prints a curated view of its result. Two flags change that:

| Flag | Effect |
| --------- | ------------------------------------------------------------------------ |
| `--raw` | Print each payload as JSON: what the API returned, not just the fields the command shows. |
| `--json` | The same JSON, alone on stdout; every message goes to stderr. |
| `-v` | Add the API request trace, printed verbatim. |

The request trace is off by default and prints bodies verbatim, so a token can be decoded and its claims checked. It therefore shows real credentials (`POST /v1/user/credentials` answers with a live secret key and session token): redirect it, don't paste it around.

## 7. Scripting

With `--json`, a command pipes straight into `jq`:

```bash
esp-morpheus --json user someone@example.com api get v1/user/nodes | jq -e '.nodes'
```

A script can branch on the exit code without parsing text:

| Code | Meaning |
| ---- | ------------------------------------------------ |
| 0 | Success |
| 1 | The command ran and failed |
| 2 | Usage error: an unknown command or a bad argument |
| 3 | Authentication or authorisation failed |

## 8. Where state lives

Inside a checkout, `esp-morpheus` keeps using the existing `cli/.*.command_history` and `.sim/`. Installed, it uses XDG directories and writes nothing to the working directory.

| What | In a checkout | Installed |
| ------------------- | -------------------------- | --------------------------------- |
| `test_config.<account-id>-<region>.json` | `~/.espressif/rainmaker-neo/morpheus/` | `~/.espressif/rainmaker-neo/morpheus/` |
| Command history | `cli/.<context>.command_history` | `~/.cache/morpheus/` |
| Simulator caches | `.sim/<name>/` | `~/.cache/morpheus/sim/<name>/` |
| Vendored `esp_prov` | `.vendor/` | `~/.cache/morpheus/vendor/` |

`MORPHEUS_CONFIG_DIR` and `MORPHEUS_CACHE_DIR` override the directories.

## 9. The Python SDK

The same distribution ships the SDK the CLI is built on, so a test or a script can drive a deployment directly:

```python
from esp_morpheus.sdk.user import User
from esp_morpheus.outputs import RmngSettings
```

In a clone of this repo, `py_sdk.test_user` and the other pre-rename paths still work and name the same modules, with no install: `py_sdk/__init__.py` finds `cli/src` itself.
