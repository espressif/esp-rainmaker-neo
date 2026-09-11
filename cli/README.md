# `morpheus` — the ESP RainMaker Neo console client

`morpheus` configures and exercises an ESP RainMaker Neo deployment end to end. It authenticates
users, registers and destroys IoT nodes, drives the user-facing REST API, connects to MQTT as a user
or as a simulated device, and manages groups, sharing, mobile push platforms and Matter
commissioning. It validates any deployment feature with no device and no phone app.

It installs as a command and runs from any directory, against any deployment, with no checkout of
this repository.

To reach a deployment, `morpheus` needs its **outputs**: every deployment publishes its common
outputs to one file, which phone apps, the dashboard and this client all read. `morpheus` reads
`rmng-outputs.json`, or the source given by `--client-outputs`.

Most of `morpheus` needs nothing else. Driving a deployment as a user or a node goes through the
deployment's own API and its MQTT broker — an end user signs in against the API and is handed
credentials by it — so `morpheus --client-outputs <url> user someone@example.com` works with no AWS
account, no profile, and no checkout.

**AWS credentials** are needed only by the commands that reach AWS directly: `test-data`,
`bot-user`, and `admin ses` / `admin sns`. Each of those creates, deletes or sweeps real resources,
so they check that the credentials name the account in the outputs and refuse to run against
another one. The region is not enforced: every AWS client is built with the region from the outputs,
so a differing ambient region is reported and ignored.

---

## 1. Install

```bash
pip install esp-morpheus
```

From a checkout, install it in place:

```bash
pip install -e ./cli
```

Needs Python 3.12 or later.

## 2. Point it at a deployment

The outputs are a JSON file keyed by stack name (`espuser-base`, `espuser-core`, `rmng-base`,
`rmng-core`, ...). Give `morpheus` a path or a URL.

**A local file (default).** With no flag, `morpheus` reads `rmng-outputs.json`. Inside a checkout,
a relative path resolves against the repo root, so it resolves the same from every directory;
outside one, it resolves against the working directory.

```bash
morpheus user someone@example.com
```

**A URL.** Point `--client-outputs` at a published outputs file, such as the per-region
client-outputs in S3. It is fetched at startup, so nothing is downloaded by hand.

```bash
morpheus --client-outputs https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com/ap-south-1/rmng-client-outputs.json \
         user someone@example.com
```

`MORPHEUS_OUTPUTS` sets the same value, so a shell session can pick a deployment once.

## 3. Pick an identity

Everything `morpheus` does, it does as somebody.

### An account that already exists

Pass any account provisioned in the deployment.

```bash
morpheus user someone@example.com            # prompts for the password
morpheus user --admin admin@example.com      # authenticates against the admin pool
```

Add `--admin` when the account is an admin. `--admin` and `--password` belong to `user`, so they
come before the identity; everything after it is the subcommand.

The password is read from `--password`, then `RMNG_PASSWORD`, then a prompt. Prefer the prompt or
the environment variable: a password in `--password` is visible to other processes and lands in
shell history.

`morpheus` signs in before running anything, so a wrong password is reported at the prompt rather
than by whichever command you run first. A typed one can be retyped, up to three tries; one from
`--password` or `RMNG_PASSWORD` fails straight away with exit code 3, since a script cannot answer
a prompt. `auth` is the exception — it provisions an account that may not exist yet, so it runs
even when sign-in fails, and the interactive prompt opens on a warning for the same reason.

### Or seed test users and devices

For quick validation against a scratch deployment, `test-data setup` creates a known set of users
and nodes from `test_config.json`. It registers every user and node, creates a default `Home` group
per user, and associates nodes flagged with `associate_to`.

```bash
morpheus test-data setup
```

On a fresh install this also writes `test_config.json` from the packaged defaults, generating
passwords and device certificates. It needs admin AWS credentials, since it provisions users in
Cognito. The seeded admin is marked `"admin": true`, so `--admin` is not needed to use it.

```bash
morpheus user someone@example.com
morpheus device node_rsa
```

`test-data destroy` removes them again: the seeded devices, their groups, and any leftover `test-*`
certificates. It deletes only test-created things and leaves the rest of the deployment alone.

### Or create the CI bot user

`bot-user create` creates an IAM user with AdministratorAccess for CI, and writes its access keys to
`bot-iam-user-credentials.json` next to `test_config.json`. The name defaults to `bot`. Pass another
one, and `morpheus` saves it as `ci_bot_user` in `test_config.json`, so `delete` and `show` find it
again.

```bash
morpheus bot-user create            # or: morpheus bot-user create rmng-ci
morpheus bot-user show
morpheus bot-user delete
```

One bot at a time. There is a single credentials file, so a second user under another name would
overwrite the keys of the first and leave it in IAM with a live admin key nothing tracks. `create`
refuses a new name until `delete` removes the current bot. A name IAM no longer has is stale, and
does not block a create.

A second `create` under the same name fails rather than invalidate the key CI is using. Use
`create --rotate` to replace the keys of an existing user, which is the way back when the
credentials file is lost.

## 4. One command, or a prompt

Every command runs two ways. Name it on the command line and `morpheus` runs it and exits:

```bash
morpheus user someone@example.com group list
```

Name no subcommand and `morpheus` opens a prompt bound to that identity, where the
`morpheus user someone@example.com` prefix is implicit:

```
$ morpheus user someone@example.com
User context: someone@example.com
Type help for commands, q to leave.
someone@example.com > group create My Home
someone@example.com > api get v1/user/nodes
someone@example.com > q
```

Tab completes the command tree. `help` lists the commands and `help <command>` prints its full help,
both generated from the same tree the dispatcher uses, so neither can drift. A bad command, a failed
call and `Ctrl-C` all return you to the prompt; `q`, `quit` and `Ctrl-D` leave.

## 5. The command tree

```
morpheus [--client-outputs SRC] [--json] [--raw] [-v]
  user [--password PW] [--admin] <identity> [SUBCOMMAND...]   # no subcommand -> prompt
      auth  connect  subscribe  publish  read-shadow  upload-file
      api                    get | post | put | patch | delete
      group                  create | list | rename | add-capabilities | share
      group subgroup         create | rename | add-node | remove-node | share
      node                   assoc | remove | claim
      matter                 initiate | verify | confirm | get-noc
      sharing                list | accept | reject
      push                   register-ios | register-android | register
      admin integrations     alexa | gva | smartthings
      admin platforms        register-ios | register-android | list
                             update-ios | update-android | delete
      admin nodes            register | bulk-register | bulk-status
      admin iot-event-mode   get | set
      admin claiming         enable
      admin ses              setup-sender | request-production
      admin sns              request-production
  device <node> [SUBCOMMAND...]                               # no subcommand -> prompt
      connect  shadow-connect  subscribe  publish  to-cloud  group-info
      set-node-config  direct-notify
  app-sim [--password PW] [--admin] <identity>
      list  select  stats  update  update-group  update-subgroup  prov
      schedule               set | get | delete
      automation             create | add-trigger | add-action | complete
                             list | get | delete
  device-sim <node>
      update-params  update-tags
  test-data                  setup | destroy
  bot-user                   create [NAME] [--rotate] | delete [NAME] | show
  gen-device <name> <rsa|ec> [--stdout] [--force]
  guide                      alexa | gva | smartthings | ios | android
```

There are **two layers**. `user` and `device` are the raw operations: one user-side or device-side
call at a time. `app-sim` and `device-sim` sit on top and run the sequence of raw operations a real
phone app or a real node performs.

The `admin` group appears only for an admin identity. `guide` needs no identity at all.

### `user` — the user API and a user's MQTT session

```
auth                                       # authenticate and register the user
group list                                 # groups, with their nodes and subgroup ids
group create My Home                       # create a group
group create --matter My Home              # create it as a Matter fabric
node assoc node_rsa <group_id>             # associate a device into a group
api get v1/user/nodes                      # raw GET against the user API
connect                                    # assume role, then connect to MQTT
subscribe node_rsa params local            # subscribe to a node's named shadows
push register-ios com.app.id <token>       # register a push endpoint
sharing list                               # pending shares
```

### `user <admin> admin` — deployment-wide operations

Same prompt, but for an admin, so authentication routes through the admin pool. These return 403 for
a regular user.

```
admin platforms register-ios key.p8 <key_id> <team_id> <bundle_id> --sandbox
admin platforms register-android service-account.json
admin platforms list
admin integrations alexa setup-auto
admin integrations smartthings setup st-config.json
admin nodes bulk-register nodes.csv --tags created_by:ci
admin iot-event-mode set sqs
admin claiming enable
admin ses setup-sender otp@company.com     # SES mails a link; the owner opens it
admin ses setup-sender --mailosaur         # mint a test address and open the link for you
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

A node that reports params and tags. Unlike `app-sim`, this one does need a `test_config.json`
entry: a node authenticates with a certificate and key, which cannot be typed at a prompt. The
entry also needs `node_cfg` and `node_tags`; only `node_multi` and `node_switch` qualify in the
default config.

```
update-params {"Light":{"brightness":80}}
update-tags {"location":"hall"}
```

### `app-sim <identity>` — the app simulator

The mobile app for a user: groups, automations, schedules and BLE provisioning. It resolves and
checks IDENTITY exactly as `morpheus user` does, prompting for a password, so it drives any account
in the deployment rather than only the ones in `test_config.json`.

```
list                                       # the user's groups
select <group_id>                          # choose the active home
update <node_id> {"Light":{"power":true}}
automation create Evening lights
schedule get <node_id>
prov                                       # BLE provisioning; needs IDF_PATH
```

## 6. Output

By default a command prints a curated view of its result. Two flags change that:

| Flag | Effect |
| --------- | ------------------------------------------------------------------------ |
| `--raw` | Print each payload as JSON, to see what the API returned rather than the fields the command chose to show. |
| `--json` | The same JSON, plus stdout reserved for it — every message goes to stderr. |
| `-v` | Add the API request trace, printed verbatim. |

The request trace is off unless you ask for it, and prints bodies verbatim so a token can be
decoded and its claims checked. That means it shows real credentials — `POST /v1/user/credentials`
answers with a live secret key and session token — so redirect it rather than pasting it around.

## 7. Scripting

`--json` puts the payload on stdout and every other word on stderr, so a command pipes straight into
`jq`:

```bash
morpheus --json user someone@example.com api get v1/user/nodes | jq -e '.nodes'
```

Exit codes say what happened, so a script can branch without parsing text:

| Code | Meaning |
| ---- | ------------------------------------------------ |
| 0 | Success |
| 1 | The command ran and failed |
| 2 | Usage error: an unknown command or a bad argument |
| 3 | Authentication or authorisation failed |

## 8. Where state lives

Inside a checkout, `morpheus` keeps using the paths that are already there: `cli/test_config.json`,
`cli/.*.command_history` and `.sim/`. Installed, it uses XDG directories and writes nothing to the
working directory.

| What | In a checkout | Installed |
| ------------------- | -------------------------- | --------------------------------- |
| `test_config.json` | `cli/` | `~/.config/morpheus/` |
| Command history | `cli/.<context>.command_history` | `~/.cache/morpheus/` |
| Simulator caches | `.sim/<name>/` | `~/.cache/morpheus/sim/<name>/` |
| Vendored `esp_prov` | `.vendor/` | `~/.cache/morpheus/vendor/` |

`MORPHEUS_CONFIG` overrides the config file, `MORPHEUS_CONFIG_DIR` and `MORPHEUS_CACHE_DIR` the
directories.

## 9. The Python SDK

The same distribution ships the SDK the CLI is built on, so a test or a script can drive a
deployment directly:

```python
from esp_morpheus.sdk.user import User
from esp_morpheus.outputs import RmngSettings
```

From a clone of this repo, `py_sdk.test_user` and the other pre-rename paths keep working and
name the same modules; they need no install, because `py_sdk/__init__.py` finds `cli/src` itself.
