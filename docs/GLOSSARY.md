# ESP RainMaker Neo — Glossary

This file defines product terms and their canonical spellings/capitalizations for use in
specs, READMEs, design docs, and other written artifacts in this repository.

Engineering conventions (identifier casing, refactoring rules, multi-region rules,
stack-family prefixes) live in the project's Cursor rules — not here.

The Term column lists the canonical form first, with accepted variants and identifier forms in brackets.

| Term | Definition |
| --- | --- |
| Admin (`admin`) | Administrative user who can do node management. |
| Admin Node Group | Grouping of Nodes owned by Admins. |
| Attribute (`Attributes`) | Metadata property on Nodes, Devices, or Services. |
| Automation (`Automations`, `automation`) | A user-defined rule that runs Actions on a Node/Device when its Conditions are met. Comprises Triggers, Conditions, and Actions. Group-scoped. Stored in the `automations` DDB table. |
| CA (Certification Authority) | Signs certificates. First mention: `CA (Certification Authority)`. |
| Client (`Clients`) | Application or interface through which a User interacts with IoT devices (mobile app, CLI, dashboard, voice assistant). |
| Cloud | The ESP RainMaker Neo cloud platform powered by AWS. Used when referring to the product cloud specifically. |
| Command-Response | Feature for queuing cloud commands to offline Devices. |
| Configuration (`config`, `Config Version`; never `cfg`) | The JSON document a Node publishes on `node/<node_id>/config` describing its Devices, Parameters, Services, and Attributes. Also used for admin-set integration configs (e.g., GVA Configuration, Alexa Configuration). Prose uses `Configuration`; identifiers use `config` (e.g., `gva-config` Lambda). |
| Control (`Controls`) | A user-issued command directed at a Node or Device to change a Parameter (e.g., turn on, set brightness). Distinguished from Node-emitted Updates. |
| CSR (Certificate Signing Request) | Submitted to CA for signing. First mention: `CSR (Certificate Signing Request)`. |
| Desired State (`state.desired`) | The values the Cloud wants the Node to reach (queued user Controls). Counterpart to Reported State. Maps to AWS IoT Shadow `state.desired`. |
| Device (`Devices`, `device_id`) | Logical user-controllable entity (switch, lightbulb, thermostat). One Node has one or more Devices. |
| Group (`Groups`, `group_id`, `Group ID`) | Grouping of Devices owned/shared by Users. |
| GVA (`gva`, Google Voice Assistant) | Google Voice Assistant Smart Home integration. Lambda surfaces: `gva-action` (directive handler), `gva-config` (admin configuration). Always uppercase in identifiers and prose. |
| MCP (`mcp`, Model Context Protocol) | Protocol for AI-agent access to ESP RainMaker Neo. Lambda surfaces: `mcp-server`, `mcp-oauth-proxy`. Always uppercase in identifiers and prose. |
| MQTT (`mqtt`) | Transport protocol for Node–Cloud communication. Uppercase always. |
| MQTT Topic | Path used to publish/subscribe (e.g., `node/<node_id>/config`). Topics formatted as inline code. |
| Node (`Nodes`, `node`, `node_id`; AWS-side: `Thing`, `thing_name`, `ThingName`) | A single ESP32-series chip with identifier and credentials from Cloud. Capitalized when referring to the concept. In AWS IoT primitives this is called a Thing — use `Node` in product/spec/docs/resource names; `Thing` only when invoking AWS IoT APIs directly. Go code may retain `thing_name` for AWS-side identifiers; do not surface `thing` in public APIs, docs, or resource names. |
| Node Connection (formerly "presence event") | Cloud-side event stream for Node MQTT connect / disconnect lifecycle. |
| Node ID (`Node ID` in prose, `node_id` in identifiers; never `nodeId`, `node-id`) | Unique identifier per Node established during registration. |
| Node Tag (`Node Tags`) | Tag set on a Node for organization. Can be set by an Admin (admin-scoped) or the User (user-scoped). |
| OTA (`ota`, Over-The-Air; never `Ota`) | Over-The-Air firmware update. First mention: `OTA (Over-The-Air)`. Uppercase always. |
| OTA Job (`OTA Jobs`) | A cloud-managed OTA distribution task. |
| Parameter (`Parameters`, `params`, `iparams` for initial parameters) | Control/monitoring capability of a Device (power state, brightness, temperature). Prose uses `Parameters`; identifiers and MQTT topic segments use `params`. `iparams` (initial parameters) allowed in Go field names and DDB columns; new resource names use `init-params` or `initial-parameters`. |
| Parent Admin Node Group | An Admin Node Group that itself contains other Admin Node Groups; the top of a hierarchy in Admin Node Group nesting. |
| Primary Device | The Device within a Node that serves as the main control interface. |
| Registration (`registration`) | Process during which Nodes receive certificates and credentials. Capitalized as a named process. |
| Registration Job (`Registration Jobs`) | Bulk Node-registration task. |
| Reported State (`state.reported`) | The values a Node has confirmed via its Shadow. Counterpart to Desired State. Maps to AWS IoT Shadow `state.reported`. |
| Schedule (`Schedules`) | A time-based rule a Node executes locally (managed by the Node's Schedule Service). Distinct from Automation: Schedules are Node-resident; Automations are Cloud-evaluated. |
| Service (`Services`) | Entity similar to a Device in structure but for ops not necessarily user-visible (e.g., Time, Local Control). |
| Shadow (`shadow`, AWS IoT Device Shadow) | The JSON document the Cloud maintains per Node holding `reported` (Node-asserted) and `desired` (Cloud-requested) state. ESP RainMaker Neo uses named shadows per Parameter group. Capitalize when used as a named concept; lowercase in code paths (`shadow_db`, MQTT topic `$aws/things/+/shadow/...`). |
| Subgroup (`Subgroups`; never `sub-group`, `sub group`) | One word, capitalized concept. Nested group inside a Group. |
| Time-Series Data (`timeseries` in identifiers, `ts` allowed in `ts-stream-processor` only) | Timestamped Parameter values with statistics. |
| TLS (`tls`, Transport Layer Security) | Uppercase always. |
| Trigger (`Triggers`) | A fire-event linked to an Automation, identified by `<nodeID>~<automationID>~<triggerIndex>`. Activates evaluation of the Automation's Conditions. |
| Update (`Updates`) | A Node-emitted message reporting its own Parameter values to the Cloud. Counterpart to a Control. Maps to the Shadow `reported` state in AWS IoT terms. |
| User (`Users`, `user_id`; `End user` only when contrasting with Admin) | End-user account. |
| User App Client (`User App Clients`, `user-app-clients` table) | A single installed mobile-app instance for a User, identified by `user_id` + mobile device token + platform (iOS/Android). One User can have many. Used for push notification delivery and sharing-code lookup. Not to be confused with the OAuth `app-client` Lambda (which registers API consumers). |
| User-Node Association (`User-Node Associations`) | Association between User accounts and Nodes. |
| Wi-Fi Provisioning (`Wi-Fi`, hyphenated) | Setup stage during which Nodes receive Wi-Fi credentials. |
| X.509 Certificate | Mutual-auth credential between Node and Cloud. |
