# Email Senders

## What this is

How ESP User decides which address its mail is sent from. There is no admin surface: the account's SES identities and the active-sender choice are both configured out of band, and this describes only how a send resolves the address to use.

## Why it is needed

SES sends only from an identity that is verified in the account and region. Mail ESP User sends through SES therefore needs a verified identity chosen for it, and needs that choice to survive redeploys.

## Two stores: SES for identities, admin-config for the active choice

- **SES** owns which identities *exist* and whether each is *verified*. It is read directly, never from a cached copy. Verified means SES reports the status as `SUCCESS`.
- **`espuser-admin-config`** (DynamoDB, PK `config_name`, SK `subtype`) records the **active sender per category**: rows `config_name="email-sender"`, `subtype=<category>`, `value=<address>`.

## Active sender resolution

The active sender for a category is resolved **store-first**: the explicit selection in `espuser-admin-config`, else — for the `global` category only — a **fallback** to the sole verified SES identity when exactly one exists. So an account with a single verified identity needs no selection at all, while the fallback stays silent when none or several are verified, because either is ambiguous. An explicit selection always wins. A row of any other category is returned as stored, with no fallback.

## Sending

A sender sends from the resolved `global` address and fails with an explicit error when none resolves. It does **not** check whether the SES account is in the sandbox: SES is the gate. Inside the sandbox SES still delivers to *verified* identities and rejects an unverified recipient, so a deployment whose recipients are verified identities sends without production access.

## Mail from the bundled Cognito pool

The bundled pool's own mail (sign-up and password-reset codes) does not use this resolution. It is sent as the pool's email configuration says: Cognito's default sender on a first deploy, or an SES identity set on the pool outside CDK. Every deploy reads the pool's current email configuration and writes it back, so a sender switched by hand survives redeploys.
