# ESP RainMaker Neo Cloud Specifications

ESP RainMaker Neo is Espressif's next-generation RainMaker platform. This is the
specification for its **cloud backend**: an AWS serverless deployment (CDK, Go
Lambdas, IoT Core) that owns identity, node lifecycle, groups, timeseries,
notifications and the voice-assistant integrations.

It documents *how the backend behaves* — data models, flows, access control and
the contracts it holds with nodes and apps. The node side of those same contracts
is specified in the ESP RainMaker Neo firmware documentation.

Each page is self-contained and covers one feature area. Start with
[User Identity & Credentials](specs/user_auth.md) and
[Node Association](specs/node_assoc.md) — almost everything else assumes the
identity and ownership model they establish.

```{toctree}
:hidden:
:caption: User Identity
:maxdepth: 1

specs/user_auth
specs/espuser/oidc-oauth2
specs/espuser/authorize-code-flow
specs/espuser/auth-flows
specs/espuser/federation
specs/espuser/external-provider
specs/espuser/legacy-user-auth
specs/espuser/email-sender
```

```{toctree}
:hidden:
:caption: Node Identity
:maxdepth: 1

specs/assisted-claiming
specs/node_reg
specs/node_connection
```

```{toctree}
:hidden:
:caption: User Node Management
:maxdepth: 1

specs/node_assoc
specs/group
```

```{toctree}
:hidden:
:caption: Node Control
:maxdepth: 1

specs/node_params_messaging
specs/schedules
specs/automations
specs/group-control-feature
```

```{toctree}
:hidden:
:caption: Node Data
:maxdepth: 1

specs/device_management
specs/timeseries
specs/s3-device-file-storage
specs/kvs-camera-streaming
```

```{toctree}
:hidden:
:caption: User messaging
:maxdepth: 1

specs/notifications
specs/notifications-push
specs/notifications-webhooks
```

```{toctree}
:hidden:
:caption: Admin Identity
:maxdepth: 1

specs/espuser/admin_auth
specs/admin/authentication
specs/admin/rbac
```

```{toctree}
:hidden:
:caption: Admin User Management
:maxdepth: 1

specs/espuser/admin-clients
```

```{toctree}
:hidden:
:caption: Admin Node Management
:maxdepth: 1

specs/admin/data-model
specs/admin/fleet-indexing
specs/admin/apis
specs/admin/dashboard
```

```{toctree}
:hidden:
:caption: Admin OTA
:maxdepth: 1

specs/ota
```

```{toctree}
:hidden:
:caption: Admin Platform
:maxdepth: 1

specs/iot_event_mode
specs/limits
```

```{toctree}
:hidden:
:caption: Voice assistants
:maxdepth: 1

specs/alexa
specs/gva
specs/smartthings
```

```{toctree}
:hidden:
:caption: AI assistants
:maxdepth: 1

specs/mcp
```

```{toctree}
:hidden:
:caption: Contributing
:maxdepth: 1

contribute/contributor-agreement
contribute/style-guide
contribute/documenting-code
contribute/testing
specs/deploy-publish
specs/admin/data-population
```

## User Identity

- [User Identity & Credentials](specs/user_auth.md) — Cognito user pools, the
  identity pool, the three IAM roles, and the two credential planes
  (identity-pool credentials vs. `AssumeRole` with a per-session policy).

The remaining pages are the ESP User provider itself — the endpoint-level detail
behind that overview:

- [OIDC Discovery & JWKS](specs/espuser/oidc-oauth2.md) — the discovery document
  and the published signing keys.
- [Authorization Endpoint](specs/espuser/authorize-code-flow.md) — the browser
  login: the authorization-code + PKCE handshake, and the flow record behind it.
- [Token Endpoint](specs/espuser/auth-flows.md) — refresh-token rotation and the
  token, userinfo and revoke endpoints.
- [Brokered Federation](specs/espuser/federation.md) — upstream identity
  providers, brokered rather than passed through.
- [External Identity Providers](specs/espuser/external-provider.md) — the
  external providers a deployment can attach.
- [Native Auth](specs/espuser/legacy-user-auth.md) — the `/v1/user/auth/*`
  surface kept for backward compatibility.
- [Email Senders](specs/espuser/email-sender.md) — how the sender address for an
  outbound OTP is chosen.

## Node Identity

- [Assisted Claiming](specs/assisted-claiming.md) — how a node acquires its
  identity: the claim-initiate and claim-verify APIs, the reservation table,
  and the KMS-backed certificate issuer behind them.
- [Node Registration](specs/node_reg.md) — node registration, including the
  asynchronous bulk CSV job that runs on ECS Fargate.
- [Node Connection Lifecycle](specs/node_connection.md) — the connection
  lifecycle: the actors involved, the clocks that govern them, and what each
  timeout does.

## User Node Management

- [Node Association](specs/node_assoc.md) — user–node association: how a node is
  claimed, what makes the claim secure, and how ownership is torn down.
- [Groups](specs/group.md) — the group model, group permissions and access
  control, naming rules, capacity limits, and the group APIs.

## Node Control

- [Node Parameters & Messaging](specs/node_params_messaging.md) — device↔cloud
  messaging: shadow vs. `to_cloud`/`from_cloud` vs. indexed params.
- [Schedules](specs/schedules.md) — the schedule data model and payload shape,
  its access control, and the `schedules` (API) ↔ `Schedules` (firmware) key
  translation.
- [Triggers & Automations](specs/automations.md) — per-node triggers (pushed to
  the node as service config) and group-scoped automations (evaluated in the
  cloud).
- [Group Control](specs/group-control-feature.md) — one publish controlling many
  devices, mapped onto subgroups and addressed by device type.

## Node Data

- [Indexed Parameters](specs/device_management.md) — the `iparams` indexed-params
  shadow: who writes each section, the DynamoDB mirror rule, and the document
  shape.
- [Time Series](specs/timeseries.md) — the ingest path (MQTT → IoT rule →
  DynamoDB → stream → aggregator) and the read path.
- [Device File Storage](specs/s3-device-file-storage.md) — per-device file
  storage in S3.
- [Camera Streaming](specs/kvs-camera-streaming.md) — Kinesis Video Streams
  camera streaming.

## User messaging

- [Notifications](specs/notifications.md) — the dispatcher and service-registry
  model that the individual channels plug into.
- [Mobile Push Notifications](specs/notifications-push.md) — the mobile push
  channel.
- [Outbound Webhooks](specs/notifications-webhooks.md) — the outbound webhook
  channel.

## Admin Identity

- [Admin Authentication](specs/espuser/admin_auth.md) — the provider side of
  admin login.
- [Admin Sessions & Permissions](specs/admin/authentication.md) — how an admin
  session is established, and the IAM permissions it carries.
- [RBAC & Authorization](specs/admin/rbac.md) — roles, and what each may do.

## Admin User Management

- [OAuth Client Registry](specs/espuser/admin-clients.md) — the runtime
  configuration surface every OAuth flow reads: redirect URIs, PKCE
  enforcement, and confidential-client secrets.

## Admin Node Management

- [Data Model](specs/admin/data-model.md) — the tables behind the admin plane.
- [Fleet Indexing](specs/admin/fleet-indexing.md) — fleet indexing and shadow
  access.
- [Admin APIs](specs/admin/apis.md) — the admin lambda endpoints, plus the
  regular user APIs an admin may call.
- [Admin Dashboard](specs/admin/dashboard.md) — the dashboard itself.

## Admin OTA

- [OTA Firmware Updates](specs/ota.md) — AWS IoT Jobs and Streams driven
  entirely from the dashboard with admin credentials, the two-section job
  document, and the two roles that keep the image readable by the service
  without making the service assumable by an operator.

## Admin Platform

- [Node Scalability](specs/iot_event_mode.md) — SQS-backed lambdas and the
  runtime mode flip.
- [Limits and Quotas](specs/limits.md) — the AWS service limits this deployment
  operates against, and what each means at ESP RainMaker Neo scale.

## Voice assistants

- [Alexa Smart Home](specs/alexa.md) — the Alexa Smart Home integration.
- [Google Voice Assistant](specs/gva.md) — the Google Voice Assistant
  integration.
- [Samsung SmartThings](specs/smartthings.md) — the SmartThings integration.

## AI assistants

- [MCP Server](specs/mcp.md) — the MCP server surface.

## Contributing

Want to contribute? Start with `CONTRIBUTING.md` at the repository root — it is
the single source of truth for setup, conventions, the CLA and what a pull
request needs. (`CONTRIBUTING.md`, `SECURITY.md` and `LICENSE` live at the root
because GitHub surfaces them from there; everything longer-form lives under
`docs/`.)

These pages go deeper on individual topics:

- [Contributor Agreement](contribute/contributor-agreement.md) — the CLA.
- [Style Guide](contribute/style-guide.md) — how these specs are written.
- [Code Documentation](contribute/documenting-code.md) — documentation
  expectations for code.
- [Testing](contribute/testing.md) — what a change is expected to test.
- [Deployment](specs/deploy-publish.md) — the two deployment flows (self-deploy
  vs. the published installer template) and the operator inputs each takes.
- [Data Population](specs/admin/data-population.md) — populating a deployment
  with data.

## API reference

The HTTP and MQTT surfaces are **not** part of this build. They are specified as
OpenAPI and AsyncAPI documents under `docs/api/` and published as here: https://api.docs.neo.rainmaker.espressif.com

That page links the HTTP, MQTT and event references, each serving the raw YAML
alongside its rendering. The specs describe the API contract; every deployment
serves that contract at its own API Gateway hostname.
