# Changelog

All notable changes to the ESP RainMaker Neo cloud backend are documented here.


## [0.8.1] (2026-10-27)

### Before you upgrade

- **If you build your own phone app** 
  — HTTP error code change: a secondary user adding a node to a group now gets 403 Forbidden. It used to get 500 for a new node and 200 for a node already in the group.
- **If you use the command-line tool** 
  — Command name change: the tool is now `esp-morpheus` instead of `morpheus`, matching its package name. Update any scripts that call it; existing settings and environment variables still work.

### Added

- **User identity**
  - Single sign-on — sign in once to use every app on the deployment. Users see where they are signed in, can sign out a lost phone from another browser, and the global sign-out ends every app.
  - Resource indicators (RFC 8707) — an app can ask for a token for one API, so a leaked token cannot be used against the others.
- **Node identity**
  - Certificate update jobs — listed separately, so past updates are visible, and created and tracked from the admin dashboard; registration jobs now list only registrations.
- **Node configuration**
  - Smaller server configuration replies — a node asks only for the keys it needs. The full reply was too large for small nodes such as the ESP32-C2.
- **Integrations**
  - Alexa — renaming a device renames it in Alexa too.
- **Deployment**
  - App assets — a CDN on your deployment where an admin can publish the ESP RainMaker Home web app and its over-the-air app updates, if they choose to.
  - `make outputs` — rebuilds the deployment's output file without a deploy, for CI jobs that only run tests.
- **Tooling**
  - Admin dashboard: branding (beta) — publish a brand made in App Studio to your deployment's client configuration under Settings → Branding.
  - `esp-morpheus` — provisioning now runs without prompts, without ESP-IDF and through assisted claiming; camera nodes' live video can be watched from the command line.

### Changed

- **Deployment**
  - Time-series tables no longer keep point-in-time backups.
  - Removing a deployment also deletes its signing keys and node ID table instead of leaving them in the account.
- **CI**
  - Merges are blocked on the full Go lint set (including gosec and errcheck) and Python lint set (including bugbear and undefined names).

### Fixed

- **User identity**
  - ID tokens carry the right sign-in time.
  - Sign-up and password emails keep your configured sender after an upgrade.
  - A new user in no group yet gets 409 from assume-role, not a server error.
- **User node management**
  - A secondary user adding a node to a group could leave it in no group: the node left its old group before the add was refused. The old owner's schedules and history were then never wiped and carried over to the next owner.
  - Secondary and sub-group users changing a subgroup's nodes get 403, not a server error.
  - Bridges — adding one to its first group could delete the child nodes it had just added, and a factory reset left its child nodes in the old owner's group.
- **Node identity**
  - A certificate update job removed a node's file, video and bridge permissions while still reporting success.
  - Cameras onboarded by assisted claiming could never stream video.
  - A node that becomes a bridge on re-claim now gets its bridge permissions; asking for bridge support on a deployment without it returns 400, not a server error.
- **Node configuration**
  - Nodes could not fetch server configuration on deployments outside `us-east-1`.
- **User messaging**
  - Nodes that don't use notifications no longer fill the logs with errors.
- **Integrations**
  - One node reporting power in an unexpected format stopped state updates to Alexa, Google Home and SmartThings for every device.
  - SmartThings — failed account links no longer duplicate devices, and lights with colour temperature 0 refresh again.
- **Deployment**
  - Fresh installs no longer roll back the Alexa, Google Home and SmartThings stacks on a timing issue, and `make setup` works on a new account.
  - `make destroy` reported success but deleted nothing. It now deletes the stacks after you type the region name to confirm, stops at the first stack group that fails, and cleans up test data again; `CONFIRM=1` skips the confirmation in CI.
- **Tooling**
  - Admin dashboard — no login loop on an expired session, and Settings no longer crashes without a voice assistant.
  - `esp-morpheus` — provisioning works with security version 2 and with nodes in a subgroup.

### Security

- Updated dependencies with published security advisories: Go 1.26.10, `x/net`, PyJWT, fastmcp, zod, urllib3, axios, follow-redirects, certifi, seroval and source-map-js.

## [0.8.0] (2026-09-22)

First tagged release!

### Added

- **User identity**
  - ESP User — a standalone OIDC provider for end users: discovery, JWKS, authorization-code with PKCE, refresh-token rotation, token, userinfo and revoke.
  - Brokered federation — upstream identity providers brokered rather than passed through, plus attachable external providers.
  - Native auth — `/v1/user/auth/*` for phone apps: signup verification, password recovery and sign-out.
  - Scoped session credentials — identity-pool credentials, or `AssumeRole` with a per-session policy limited to the groups a user may reach.
- **Admin identity**
  - Admin authentication — on Cognito, separate from the end-user provider.
- **Node identity**
  - Assisted claiming — claim-initiate and claim-verify APIs, a KMS-backed certificate issuer, so a node gets its identity without per-device secrets in the factory.
  - Node registration — single and bulk; bulk runs as an asynchronous CSV job on ECS Fargate.
  - Node connection lifecycle — govern connect, disconnect and recovery.
- **User node management**
  - Node association — user–node binding, ownership teardown, and group and subgroup sharing.
  - Groups and subgroups — group permissions, node management and access control.
  - Matter commissioning — including per-group NOC issuance.
- **Node control**
  - Local and remote control — local within network range, remote over MQTT from anywhere.
  - Schedules — time-based periodic node actions.
  - Automations — node-to-node automations: a parameter change on one node triggers an action on another.
  - Group and subgroup control — one command reaches every node in the group or subgroup.
  - Matter control
  - Bridge nodes — a node providing cloud connectivity for non-IP children behind a hub (Zigbee, Z-Wave, BLE-mesh or proprietary RF).
- **Node data**
  - Time series — storage and query of historical parameters: raw, latest and aggregates.
  - Indexed parameters — a fleet-indexed per-node shadow of slow-moving facts; what makes admin node search possible.
  - Node file storage — S3-backed, with presigned upload URLs.
  - Camera streaming — through Kinesis Video Streams.
- **User messaging**
  - Push notifications — iOS and Android through AWS SNS Mobile Push, with per-user device endpoints.
- **OTA**
  - Firmware rollouts — to a node, a group or a query-defined fleet, orchestrated by AWS IoT Jobs.
  - Image management — versioned images in the files bucket, with metadata read from the binary.
  - Rollout control — monitoring, cancel and delete from the dashboard.
- **Integrations**
  - Alexa Smart Home
  - Google Voice Assistant
  - Samsung SmartThings
- **AI assistants**
  - MCP server — read and control devices, groups and schedules from an assistant; the OAuth proxy accepts Client ID Metadata Documents (CIMD), so clients need no pre-registration.
- **Deployment**
  - Node scalability modes — high-volume MQTT handlers fed direct from an IoT rule, or through SQS for batching; switchable at runtime.
- **Tooling**
  - Admin dashboard — UI for all admin operations.
  - `morpheus` — a console client that exercises all paths as a user/admin or a simulated node.

[Unreleased]: https://github.com/espressif/esp-rainmaker-neo/compare/v0.8.1...HEAD
[0.8.1]: https://github.com/espressif/esp-rainmaker-neo/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/espressif/esp-rainmaker-neo/releases/tag/v0.8.0
