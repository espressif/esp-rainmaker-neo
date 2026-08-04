# ESP RainMaker Neo Cloud Specifications

ESP RainMaker Neo is Espressif's next-generation RainMaker platform. This is the
specification for its **cloud backend**: an AWS serverless deployment (CDK, Go
Lambdas, IoT Core) that owns identity, node lifecycle, groups, timeseries,
notifications and the voice-assistant integrations.

It documents *how the backend behaves* — data models, flows, access control and
the contracts it holds with nodes and apps. The node side of those same contracts
is specified in the ESP RainMaker Neo firmware documentation.

Each page is self-contained and covers one feature area. Start with
[User Management, Authentication & Credentials](specs/user_auth.md) and
[Node association](specs/node_assoc.md) — almost everything else assumes the
identity and ownership model they establish.

```{toctree}
:hidden:
:caption: Identity and access
:maxdepth: 1

specs/user_auth
specs/node_assoc
specs/group
```

```{toctree}
:hidden:
:caption: Admin Dashboard
:maxdepth: 1

specs/admin/index
specs/admin/authentication
specs/admin/apis
specs/admin/data-model
specs/admin/rbac
specs/admin/dashboard
specs/admin/fleet-indexing
specs/admin/data-population
```

```{toctree}
:hidden:
:caption: Node lifecycle
:maxdepth: 1

specs/assisted-claiming
specs/node_reg
specs/node_connection
specs/node_params_messaging
specs/device_management
```

```{toctree}
:hidden:
:caption: Features
:maxdepth: 1

specs/schedules
specs/automations
specs/group-control-feature
specs/timeseries
specs/notifications
specs/notifications-push
specs/notifications-webhooks
specs/s3-device-file-storage
specs/kvs-camera-streaming
```

```{toctree}
:hidden:
:caption: Voice assistants
:maxdepth: 1

specs/alexa
specs/gva
```

```{toctree}
:hidden:
:caption: Platform
:maxdepth: 1

specs/iot_event_mode
specs/deploy-publish
specs/limits
```

```{toctree}
:hidden:
:caption: Contributing
:maxdepth: 1

contribute/contributor-agreement
contribute/style-guide
contribute/documenting-code
contribute/testing
```

## Identity and access

- [user_auth](specs/user_auth.md) — Cognito user pools, the identity pool, the
  three IAM roles, and the two credential planes (identity-pool credentials vs.
  `AssumeRole` with a per-session policy).
- [node_assoc](specs/node_assoc.md) — user–node association: how a node is
  claimed, what makes the claim secure, and how ownership is torn down.
- [group](specs/group.md) — the group model, group permissions and access
  control, naming rules, capacity limits, and the group APIs.

## Admin Dashboard

Everything an admin or super admin can reach. See
[the section overview](specs/admin/index.md) for the reading order; the pages
cover [authentication and permissions](specs/admin/authentication.md),
[the admin APIs](specs/admin/apis.md), [the data model](specs/admin/data-model.md),
[RBAC](specs/admin/rbac.md), [the dashboard itself](specs/admin/dashboard.md),
[fleet indexing](specs/admin/fleet-indexing.md) and, for contributors,
[populating a deployment with data](specs/admin/data-population.md).

## Node lifecycle

- [assisted-claiming](specs/assisted-claiming.md) — how a node acquires its
  identity: the claim-initiate and claim-verify APIs, the reservation table,
  and the KMS-backed certificate issuer behind them.
- [node_reg](specs/node_reg.md) — node registration, including the asynchronous
  bulk CSV job that runs on ECS Fargate.
- [node_connection](specs/node_connection.md) — the connection lifecycle: the
  actors involved, the clocks that govern them, and what each timeout does.
- [node_params_messaging](specs/node_params_messaging.md) — device↔cloud
  messaging: shadow vs. `to_cloud`/`from_cloud` vs. indexed params.
- [device_management](specs/device_management.md) — the `iparams` indexed-params
  shadow: who writes each section, the DynamoDB mirror rule, and the document
  shape.

## Features

- [schedules](specs/schedules.md) — the schedule data model and payload shape,
  its access control, and the `schedules` (API) ↔ `Schedules` (firmware) key
  translation.
- [automations](specs/automations.md) — per-node triggers (pushed to the node as
  service config) and group-scoped automations (evaluated in the cloud).
- [group-control-feature](specs/group-control-feature.md) — one publish
  controlling many devices, mapped onto subgroups and addressed by device type.
- [timeseries](specs/timeseries.md) — the ingest path (MQTT → IoT rule →
  DynamoDB → stream → aggregator) and the read path.
- [notifications](specs/notifications.md) — the dispatcher and service-registry
  model that the individual channels plug into.
- [notifications-push](specs/notifications-push.md) — the mobile push channel.
- [notifications-webhooks](specs/notifications-webhooks.md) — the outbound
  webhook channel.
- [s3-device-file-storage](specs/s3-device-file-storage.md) — per-device file
  storage in S3.
- [kvs-camera-streaming](specs/kvs-camera-streaming.md) — Kinesis Video Streams
  camera streaming.

## Voice assistants

- [alexa](specs/alexa.md) — the Alexa Smart Home integration.
- [gva](specs/gva.md) — the Google Voice Assistant integration.

## Platform

- [iot_event_mode](specs/iot_event_mode.md) — SQS-backed lambdas and the runtime
  mode flip.
- [deploy-publish](specs/deploy-publish.md) — the two deployment flows
  (self-deploy vs. the published installer template) and the operator inputs
  each takes.
- [limits](specs/limits.md) — the AWS service limits this deployment operates
  against, and what each means at ESP RainMaker Neo scale.

## Contributing

Want to contribute? Start with `CONTRIBUTING.md` at the repository root — it is
the single source of truth for setup, conventions, the CLA and what a pull
request needs. (`CONTRIBUTING.md`, `SECURITY.md` and `LICENSE` live at the root
because GitHub surfaces them from there; everything longer-form lives under
`docs/`.)

These pages go deeper on individual topics: the
[contributor agreement](contribute/contributor-agreement.md), the
[style guide](contribute/style-guide.md),
[documentation expectations](contribute/documenting-code.md) and
[testing](contribute/testing.md).

## API reference

The HTTP and MQTT surfaces are **not** part of this build. They are specified as
OpenAPI and AsyncAPI documents under `docs/api/` and published as here: https://api.docs.neo.rainmaker.espressif.com

That page links the HTTP, MQTT and event references, each serving the raw YAML
alongside its rendering. The specs describe the API contract; every deployment
serves that contract at its own API Gateway hostname.
