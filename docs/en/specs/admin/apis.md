# Admin APIs

## Admin Lambda API Endpoints


All require super admin authentication (`IsSuperAdmin` check).

### Node Registration

| Endpoint                                 | Method | Purpose                                                          | Response                                             |
| ---------------------------------------- | ------ | ---------------------------------------------------------------- | ---------------------------------------------------- |
| `/v1/admin/nodes/registration-jobs/{id}` | GET    | Poll bulk job status                                             | `{status, total_nodes, success_count, failed_count}` |
| `/v1/admin/nodes/registration-jobs`      | POST   | Bulk register from S3 CSV (async ECS task)                       | `{request_id}` (HTTP 202)                            |
| `/v1/admin/nodes`                        | POST   | Register single node (cert + admin groups + parent group + tags) | `{node_id}`                                          |

**Single registration request:**
```json
{
  "cert": "-----BEGIN CERTIFICATE-----...",
  "ca_cert": "optional",
  "checksum": "optional MD5",
  "admin_group_names": ["group1", "group2"],
  "admin_parent_group_name": "optional parent group",
  "tags": ["key1:value1", "key2:value2"]
}
```

When `admin_parent_group_name` is provided, each group in `admin_group_names` is validated/created under that parent. If a group already exists under a different parent, the request fails. If the parent doesn't exist, it is created first.

**Bulk CSV format:** `node_id,certs,admin_groups`

Per-node `admin_groups` from CSV are always created flat (no parent). The `admin_parent_group_name` in the API body only applies to the common `admin_group_names`.

**Bulk registration statuses:** `requested` -> `started` -> `data_loaded` -> `completed`

The `requested` status is created by the Lambda handler immediately, so status queries return meaningful data before the ECS container starts.


### Node Group Lookup

| Endpoint                          | Method | Purpose                                     | Response              |
| --------------------------------- | ------ | ------------------------------------------- | --------------------- |
| `/v1/admin/nodes/{nodeId}/groups` | GET    | Get node's ESP RainMaker Neo group + subgroup membership | `{group, sub_groups}` |


### Mobile Push Integration Management

| Endpoint                              | Method | Purpose                                      |
| ------------------------------------- | ------ | -------------------------------------------- |
| `/v1/admin/integrations`              | POST   | Create integration (`apns`/`apns_sandbox`/`gcm`) |
| `/v1/admin/integrations`              | GET    | List all integrations                        |
| `/v1/admin/integrations/{integrationId}` | GET    | Get one integration                       |
| `/v1/admin/integrations/{integrationId}` | PUT    | Update integration credentials            |
| `/v1/admin/integrations/{integrationId}` | DELETE | Delete integration                        |

There is also a non-admin `GET /v1/integrations`, which returns the public
summary list (no credentials); the same Lambda serves both and gates on the
path.

**`integration_type` is a query parameter, not a body field**, and the public
values are **lowercase**: `apns`, `apns_sandbox`, `gcm`. The handler maps them
to the SNS platform names (`APNS`, `APNS_SANDBOX`, `GCM`) internally.

**Integration ID format:** `{integration_type}_{platform_app_name}` — e.g.
`apns_com.example.app`, `apns_sandbox_com.example.app`, `gcm_my-project`. For
non-push integrations (`alexa`, `gva`, …) the id is just the type, with no
suffix.

**Create request (`?integration_type=apns`):**
```json
{
  "authentication_key": "...",
  "key_id": "Apple signing key ID",
  "team_id": "Apple team ID",
  "bundle_id": "com.example.app"
}
```

**Create request (`?integration_type=gcm`)** — a flat Google service-account
JSON, the same shape GVA uses (not an `api_key` string):
```json
{
  "type": "service_account",
  "project_id": "my-project",
  "private_key_id": "...",
  "private_key": "-----BEGIN PRIVATE KEY-----...",
  "client_email": "...@my-project.iam.gserviceaccount.com",
  "client_id": "...",
  "token_uri": "https://oauth2.googleapis.com/token"
}
```

`GET` (one) returns per-type fields only: for APNS just the `bundle_id` (the
auth key is secret), for GCM the full service-account JSON.



## Regular User APIs Callable by Admin


`AdminDeviceUsersRole` holds `execute-api:Invoke` scoped to the ESP RainMaker Neo API Gateway
(`RMBaseApi`), so an admin can call every ESP RainMaker Neo endpoint and nothing outside it.
Key ones not currently in the dashboard:

### Groups & Subgroups

| Endpoint                                     | Method         | Data                                                          |
| -------------------------------------------- | -------------- | ------------------------------------------------------------- |
| `/v1/groups/{groupId}/matter-nocs`           | POST           | Get Matter NOC                                                |
| `/v1/groups/{groupId}/service/{serviceName}` | GET/PUT/DELETE | Group services (automations, schedules)                       |
| `/v1/groups/{groupId}/sharing-requests`      | POST           | Send sharing request                                          |
| `/v1/groups/{groupId}/subgroups`             | POST           | Create subgroup                                               |
| `/v1/groups/{groupId}`                       | PATCH          | Update group                                                  |
| `/v1/groups`                                 | GET            | All ESP RainMaker Neo groups (with subgroups, capabilities, matter config) |

### Node Services & Telemetry

| Endpoint                                                    | Method         | Data                         |
| ----------------------------------------------------------- | -------------- | ---------------------------- |
| `/v1/groups/{groupId}/nodes/{nodeId}/{serviceName}`         | GET/PUT/DELETE | Node service data            |
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/aggregates` | GET            | Aggregated metrics over time |
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/latest`     | GET            | Latest value per metric      |
| `/v1/groups/{groupId}/nodes/{nodeId}/timeseries/raw`        | GET            | Raw timeseries data points   |

### Node Association

| Endpoint                                                | Method | Data                      |
| ------------------------------------------------------- | ------ | ------------------------- |
| `/v1/groups/{groupId}/node-assoc-requests/{id}/confirm` | POST   | Confirm association       |
| `/v1/groups/{groupId}/node-assoc-requests/{id}/verify`  | POST   | Verify association        |
| `/v1/groups/{groupId}/node-assoc-requests`              | POST   | Initiate node association |

### Credentials & Sharing

| Endpoint                           | Method | Data                                          |
| ---------------------------------- | ------ | --------------------------------------------- |
| `/v1/assumed-roles`                | POST   | Get scoped AWS credentials per group/subgroup |
| `/v1/sharing-requests/{id}/accept` | POST   | Accept sharing                                |
| `/v1/sharing-requests/received`    | GET    | Pending sharing requests                      |
| `/v1/user/credentials`             | POST   | Get user credentials                          |

### Integrations

| Endpoint                                     | Method          | Data                          |
| -------------------------------------------- | --------------- | ----------------------------- |
| `/v1/admin/integrations/alexa/configuration` | POST            | Configure Alexa skill         |
| `/v1/admin/integrations/gva/configuration`   | GET/POST/DELETE | Google Voice Assistant config |

### File Management

| Endpoint                                  | Method | Data                   |
| ----------------------------------------- | ------ | ---------------------- |
| `/v1/admin/file-templates/{templateType}` | GET    | File templates         |
| `/v1/admin/files/upload-urls`             | POST   | Pre-signed upload URLs |
