# Data Model


## DynamoDB Tables

### `user_details`
| Field            | Type    | Purpose               |
| ---------------- | ------- | --------------------- |
| `email`          | String  | Indexed               |
| `is_super_admin` | Boolean | Super admin flag      |
| `phone`          | String  | Indexed               |
| `provider`       | String  | `"COGNITO"`           |
| `user_id` (PK)   | String  | Cognito identity      |
| `user_type`      | String  | `"USER"` or `"ADMIN"` |


### `groups` (Index Overloading Pattern)
| Field               | Type          | Purpose                                                                   |
| ------------------- | ------------- | ------------------------------------------------------------------------- |
| `cap_<name>`        | String (JSON) | Capability data (e.g. `cap_matter` with fabric_id, root_ca, ipk, CAT IDs) |
| `capabilities`      | List          | e.g. `["matter"]` — main group only                                       |
| `group_id` (PK)     | String        | Group identifier                                                          |
| `group_name`        | String        | Display name                                                              |
| `sub_group_id` (SK) | String        | `"NONE"` for main group, else subgroup ID                                 |


### `rmng-user-group-assoc`
| Field            | Type   | Purpose                                                                             |
| ---------------- | ------ | ----------------------------------------------------------------------------------- |
| `access_type`    | String | `"primary"` (full + share), `"secondary"` (limited), `"subentity"` (subgroups only) |
| `group_id` (SK)  | String | Group identifier                                                                    |
| `sub_entity_ids` | List   | Subgroup IDs for subentity access                                                   |
| `user_id` (PK)   | String | User identifier                                                                     |

**GSI:** `group_id` index for finding all users with group access


### `rmng-group-node-assoc`
| Field                           | Type   | Purpose                               |
| ------------------------------- | ------ | ------------------------------------- |
| `group_id` (PK)                 | String | Group identifier                      |
| `node_id` (SK)                  | String | Device identifier                     |
| `subgrp1`, `subgrp2`, `subgrp3` | String | Optional subgroup memberships (max 3) |

**GSI:** `node_id` index for reverse lookup

**Constraint:** A node can belong to max 3 subgroups and only 1 main group.


### `users`
| Field                 | Type   | Purpose                                    |
| --------------------- | ------ | ------------------------------------------ |
| `mobile_device_token` | String | Push notification token                    |
| `platform`            | String | `ios` or `android`                         |
| `user_id` (PK)        | String | User identifier                            |


### `rmng-node-reg-reqs`
| Field               | Type   | Purpose                                               |
| ------------------- | ------ | ----------------------------------------------------- |
| `admin_group_names` | List   | Groups to assign                                      |
| `cert_file_s3_path` | String | S3 location of cert CSV                               |
| `created_at`        | Number | Timestamp                                             |
| `failed_count`      | Number | Failed                                                |
| `last_updated_at`   | Number | Timestamp                                             |
| `request_id` (PK)   | String | ECS Task ID                                           |
| `status`            | String | `requested` / `started` / `data_loaded` / `completed` |
| `success_count`     | Number | Succeeded                                             |
| `tags`              | List   | Tags to apply                                         |
| `total_nodes`       | Number | Expected count                                        |
| `user_id`           | String | Admin who initiated                                   |


### `rmng-node-assoc-reqs`
| Field             | Type    | Purpose                              |
| ----------------- | ------- | ------------------------------------ |
| `challenge`       | String  | Verification challenge               |
| `expiration_time` | Number  | 5-minute TTL                         |
| `group_id`        | String  | Target group                         |
| `is_matter_group` | Boolean | Matter flag                          |
| `matter_node_id`  | String  | Set for Matter groups                |
| `node_id`         | String  | Set after verify                     |
| `request_id` (PK) | String  | UUID                                 |
| `status`          | String  | `pending` / `verified` / `confirmed` |
| `user_id`         | String  | Initiating user                      |

