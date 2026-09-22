# Changelog

All notable changes to the ESP RainMaker Neo cloud backend are documented here.


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

[Unreleased]: https://github.com/espressif/esp-rainmaker-neo/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/espressif/esp-rainmaker-neo/releases/tag/v0.8.0
