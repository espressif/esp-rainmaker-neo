# Admin Dashboard

**Tech stack:** React 19 + Vite + TanStack Router + Zustand + React Query + AWS SDK + Tailwind CSS

**Terminology:** "Nodes" and "Node ID" throughout (matching ESP RainMaker Neo specs). "Thing" only in internal code/AWS SDK calls.

## Route Structure

The route tree is declared once; the router, the sidebar and the breadcrumbs are
all projected from that one declaration.

```
/                                    — redirects to /login
/login, /forgot-password, /set-password, /logout
/error, /oauth-preview
/static/terms-of-use, /static/privacy-policy   — public, render without runtime config

/home                                — redirects to /home/node-management/nodes
  /home/node-management
    /nodes                           — Node list
      /$thingName                    — Node details; redirects to /overview
        /overview  /tags  /attributes  /ota-jobs
    /node-groups                     — Node Group list
      /new                           — Create group
      /$groupName                    — redirects to /nodes
        /nodes  /ota-jobs
    /register                        — Registration jobs
      /new                           — Register nodes (single + bulk)
    /generate                        — Generate nodes
  /home/ota                          — redirects to /home/ota/images
    /images                          — OTA images
      /new                           — Upload OTA image
    /jobs                            — OTA jobs
      /new                           — Create OTA job
      /$jobId                        — redirects to /overview
        /overview  /nodes
  /home/settings                     — redirects to /voice-assistants
    /voice-assistants                — redirects to /alexa
      /alexa  /gva
    /push-notifications
    /post-deployment
  /home/account-settings             — /profile, /preferences, /password
```

## Nodes Page

**Default columns:** Node ID, Status (with "since" timestamp), Groups (linked to group detail)

**Hidden columns (toggle via Columns):** Device Type, Model, Firmware, Thing Type

**Status column:** Merged status + timestamp. Prefers `connectivity` data (AWS IoT Core real-time, if indexing enabled) over iparams shadow `online` field. Shows colored dot (green=online, gray=offline) with "since <date>" below.

**Status filter dropdown:** All / Online / Offline — appends to SearchIndex query.

**Detail page tabs:**
- **Overview** — Node ID, ARN, Online Status, Last Status Update, Groups (linked)
- **Tags** — Admin/Device/User tags in tables with Key, Value, Last Updated columns. Collapsible raw JSON.
- **Attributes** — IoT Thing attributes (currently only `group_id`)
- **OTA Jobs** — Job execution history with status badges

**Advanced search:**
- Predefined fields: Created By, Registered From, Registration Batch, Device Model, Device Type, Firmware Version, Online Status, Room, User Location
- Custom tag support: type any tag key → pick source (Admin/Device/User) → shadow path auto-constructed
- Value suggestions: via `GetBucketsAggregation` (graceful fallback if no IAM permission)

**API calls:** `SearchIndexCommand` (primary, returns shadow + connectivity inline), `ListThingsCommand` (fallback), `DescribeThingCommand`, `GetBucketsAggregationCommand` (value suggestions, graceful fallback)

## Node Groups Page

**List columns:** Group Name, Description, Parent Group, ARN (hidden by default), Delete action

**Search:** Uses `SearchIndex` on `AWS_ThingGroups` index as primary search method (falls back to `ListThingGroupsCommand` if indexing unavailable). Supports both simple name search and advanced search with fields: Group Name, Description, Parent Group. Value suggestions via `GetBucketsAggregation`.

**Detail page:**
- **Overview section** — ARN, Description (editable inline via `UpdateThingGroup` with error feedback), Created date, Parent Group (linked with hierarchy breadcrumb), Child Groups (linked), Node count
- **Node list** — with add/remove functionality, "Load more" only shown when more pages exist

**API calls:** `SearchIndexCommand` (primary, `AWS_ThingGroups` index), `ListThingGroupsCommand` (fallback), `GetBucketsAggregationCommand` (value suggestions), `DescribeThingGroupCommand`, `ListThingsInThingGroupCommand`, `AddThingToThingGroupCommand`, `RemoveThingFromThingGroupCommand`, `UpdateThingGroupCommand`

## OTA Pages

Images and jobs are separate pages (`/home/ota/images`, `/home/ota/jobs`), each
with a create route, and a job detail page with **Overview** and **Nodes** tabs.

**Shows:**
- OTA images: Name, Size, Uploaded date; upload via `/images/new`
- OTA jobs: Job ID, Status, Created At; create via `/jobs/new`
- Job detail: Overview (job config + Device Metrics chart) and per-node execution status

**API calls:** `ListObjectsV2Command` (S3), `ListJobsCommand`, `DescribeJobCommand`, `CreateJobCommand`, `CreateStreamCommand`


## Known gaps


### Already Accessible (No IAM Changes Needed)

| Feature                                                    | Data Source                                                            | Notes                                                  |
| ---------------------------------------------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------ |
| **Add/remove things from groups** in thing detail view     | `iot:AddThingToThingGroup`, `iot:RemoveThingFromThingGroup`            | Permission exists, partially wired                     |
| **Alexa/GVA integration config**                           | `/v1/integrations/alexa\|gva/configuration`                            | Voice assistant setup                                  |
| **Cancel OTA job**                                         | `iot:CancelJob`                                                        | Permission exists, not wired up                        |
| **Connectivity details**                                   | `SearchIndex` connectivity fields                                      | `connected`, `disconnectReason`, `timestamp`           |
| **DescribeThing extra fields**                             | `iot:DescribeThing`                                                    | thingId, billingGroupName, defaultClientId, version    |
| **Device telemetry charts**                                | `GET /v1/.../timeseries/raw\|latest\|aggregates`                       | Historical + real-time sensor data                     |
| **Download firmware binary**                               | `s3:GetObject` on `ota/*`                                              | Permission exists, not wired up                        |
| **Firmware file ETag/hash**                                | `s3:ListObjectsV2`                                                     | Content integrity verification                         |
| **Full iparams shadow display**                            | `SearchIndex` returns full shadow JSON                                 | Currently only selected fields shown                   |
| **Group automations/schedules**                            | `GET /v1/groups/{groupId}/service/{serviceName}`                       | Group-level service configs                            |
| **Job process details**                                    | `iot:DescribeJob` jobProcessDetails                                    | Detailed queue/in-progress/success/fail/timeout counts |
| **Job timeout & abort config**                             | `iot:DescribeJob`                                                      | Currently not displayed                                |
| **Mobile push platform management**                        | `POST/GET/PUT/DELETE /v1/admin/app-platforms`                          | APNS + GCM credential management                       |
| **Node registration UI** (single + bulk + status tracking) | `POST /v1/admin/nodes`, `POST/GET /v1/admin/nodes/registration-jobs/*` | Full CRUD for device provisioning                      |
| **Node service details**                                   | `GET /v1/.../nodes/{nodeId}/{serviceName}`                             | Device-reported service params                         |
| **Node's ESP RainMaker Neo group membership**                           | `GET /v1/admin/nodes/{nodeId}/groups`                                  | Shows group + subgroup IDs                             |
| **ESP RainMaker Neo group hierarchy**                                   | `GET /v1/groups`                                                       | Groups + subgroups + capabilities + Matter config      |
| **Sharing management**                                     | `GET /v1/sharing-requests/received`, accept/reject                     | Admin view of pending shares                           |

### Requires IAM Changes

| Feature                                  | Missing Permission              | Change Needed                                                                             |
| ---------------------------------------- | ------------------------------- | ----------------------------------------------------------------------------------------- |
| ~~Edit group description~~               | ~~`iot:UpdateThingGroup`~~      | **Done** — Added to `AdminDeviceUsersRole`, dashboard shows error feedback                |
| ~~Value suggestions in filters~~         | ~~`iot:GetBucketsAggregation`~~ | **Done** — Added to `AdminDeviceUsersRole`, works for both Things and ThingGroups indices |
| Index additional shadow names            | Fleet Indexing config           | Update `namedShadowNames` filter                                                          |
| List named shadows per device            | `iot:ListNamedShadowsForThing`  | Add to `AdminDeviceUsersRole`                                                             |
| Read any named shadow (not just iparams) | `iot:GetThingShadow`            | Add to `AdminDeviceUsersRole`                                                             |
| Scope admin access to specific groups    | Resource conditions             | Add IAM conditions / scoped policies                                                      |
| Update device shadow from dashboard      | `iot:UpdateThingShadow`         | Add to `AdminDeviceUsersRole`                                                             |

### Requires CDK Infrastructure Changes

| Feature                                               | Change                                                                      | Impact                                                                                       |
| ----------------------------------------------------- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| ~~Group search by substring, list with descriptions~~ | ~~Add `thingGroupIndexingConfiguration: { thingGroupIndexingMode: "ON" }`~~ | **Done** — Dashboard uses `SearchIndex` on `AWS_ThingGroups` with `listThingGroups` fallback |
| Reliable online/offline status                        | Add `thingConnectivityIndexingMode: "STATUS"`                               | Dashboard already handles gracefully                                                         |
- **Reliable online/offline status** needs
  `thingConnectivityIndexingMode: "STATUS"` on the fleet-indexing configuration.
  Until then the dashboard prefers `connectivity` data when present and falls
  back to the `iparams` shadow's `online` field.
- **Only the `iparams` named shadow is readable.** `AdminDeviceUsersRole` holds
  no `iot:GetThingShadow` or `iot:ListNamedShadowsForThing`, and fleet indexing
  filters `namedShadowNames` to `iparams`, so other named shadows
  (`params-<group>`, `notify`, …) are not visible here.
- **Admin access is not scoped to specific groups.** A super-admin sees the
  whole fleet; per-group scoping would need IAM resource conditions.
- **Shadows are read-only from the dashboard** — no `iot:UpdateThingShadow`.
