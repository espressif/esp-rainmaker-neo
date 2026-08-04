# Email sender resolution

## What this is

How the IdP decides which address its mail is sent from. There is no admin surface: the account's SES identities and the active-sender choice are both configured out of band, and this describes only how a send resolves the address to use.

## Why it is needed

SES sends only from an identity that is verified in the account and region. The OTP email flow therefore needs a verified identity chosen for it, and needs that choice to survive redeploys.

## Two stores: SES for identities, admin-config for the active choice

- **SES** owns which identities *exist* and whether each is *verified*. It is read directly, never from a cached copy. Verified means SES reports the status as `SUCCESS`.
- **`espuser-admin-config`** (DynamoDB, PK `config_name`, SK `subtype`) records the **active sender per category**: rows `config_name="email-sender"`, `subtype=<category>`, `value=<address>`. Only the `global` category is used.

**Global today, per-category later.** A future per-category sender (e.g. `signin`, `marketing`) is another `subtype` row, not a schema change.

## Active sender resolution

The active sender for a category is resolved **store-first**: the explicit selection in `espuser-admin-config`, else — for the `global` category only — a **fallback** to the sole verified SES identity when exactly one exists. So an account with a single verified identity needs no selection at all, while the fallback stays silent when none or several are verified, because either is ambiguous. An explicit selection always wins.

## How the OTP send path consumes it

The OTP dispatcher sends from the effective active `global` sender, resolved once and cached for the warm lambda. It fails with an explicit error when no sender resolves, and otherwise attempts the send **unconditionally** — it does **not** pre-check the SES sandbox. SES is the gate: inside the sandbox it still delivers to *verified* identities and rejects an unverified recipient, so a deployment whose recipients are verified (an itest account whose inboxes are verified identities, for instance) sends without production access. Production access is an account-level concern surfaced under post-deployment steps, never a gate on dispatch.

## Production access (SES sandbox exit)

A new SES account is in the **sandbox** and can send only to verified addresses, so mail to arbitrary users fails. Exiting is an AWS request that is rejected unless at least one identity is verified, and a request AWS has already closed cannot be re-filed — a declined review moves only through an AWS support case. The account states are `SANDBOX`, `PENDING`, `ENABLED`, `DENIED` and `FAILED`.

## Components

- The active-sender selection store: configuration keyed by category, holding the chosen address.
- The resolution path that reads that store and falls back to the sole verified identity.
- The OTP send path, which sends from the resolved identity and does not gate on the sandbox.

## Open items

- Per-category senders (e.g. `signin`, `marketing`) once more than one template exists; resolution would then be per category.
- An administrative surface for registering identities and choosing the active one. Mail is sent only by the OTP add-on, so it belongs with that add-on rather than in the core admin API.
