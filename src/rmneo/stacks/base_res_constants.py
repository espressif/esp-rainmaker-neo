#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Base Resource Constants for RMNG infrastructure.

This file contains constants for resources defined in the base stack
that need to be referenced in the core stack.
"""

# DynamoDB Table Names
TABLE_NAMES = {
    'USER_GROUP_MAPPING': 'rmng-user-group-assoc',
    'USER_ENDPOINTS': 'rmng-user-endpoints',
    # user_details lives in the espuser-base CDK stack (espuser/base_res_constants.py)
    # but rmng-side lambdas need to read it to join email/phone onto group user lists.
    # Name MUST match espuser/base_res_constants.py['USER_DETAILS'].
    'USER_DETAILS': 'espuser-user-details',

    'GROUPS': 'rmng-groups',
    'GROUP_DEVICE_MAPPING': 'rmng-group-node-assoc',
    'SHARING_REQUESTS': 'rmng-sharing-reqs',
    'AUTOMATIONS': 'rmng-automations',

    'NODE_DETAILS': 'rmng-nodes',
    'NODES_ONLINE': 'rmng-nodes-online',
    'ASSOC_REQUESTS': 'rmng-node-assoc-reqs',
    'NODE_IPARAMS': 'rmng-node-init-params',
    'NODE_REG_REQS': 'rmng-node-reg-reqs',
    'NODE_REG_FAILED_NODES': 'rmng-node-reg-failed-nodes',
    # Assisted claiming only: node-ID reservations mapping a claim key to the
    # cloud-assigned node ID (see docs/en/specs/assisted-claiming.md).
    # Created only when claiming is enabled.
    'NODE_ID_RESERVATIONS': 'rmng-node-id-reservations',

    'RAW_TS_DATA': 'rmng-raw-ts-data',
    'PROCESSED_TS_DATA': 'rmng-processed-ts-data',

    # Shared runtime-set admin configuration. Single-row-per-feature key/value
    # store consumed by the reapply custom-resource pattern (see
    # docs/en/specs/iot_event_mode.md §4.4 and rmneo/handlers/admin/admin_config_base.py).
    'ADMIN_CONFIG': 'rmng-admin-configs',
}

# DynamoDB Index Names
# Pattern: <table-name>-by-<attribute> for partition-key lookups;
# <table-name>-list for single-shard list-views.
INDEX_NAMES = {
    'USER_GROUP_MAPPING_GROUP_ID': 'rmng-user-group-assoc-by-group-id',
    'USER_DETAILS_EMAIL': 'espuser-user-details-by-email',
    # Name MUST match espuser/base_res_constants.py['USER_DETAILS_PHONE'].
    'USER_DETAILS_PHONE': 'espuser-user-details-by-phone',
    'GROUP_DEVICE_MAPPING_NODE_ID': 'rmng-group-node-assoc-by-node-id',
    'GROUP_DEVICE_MAPPING_ALIAS_INDEX': 'rmng-group-node-assoc-by-alias',
    'NODE_REG_REQS_LIST': 'rmng-node-reg-reqs-list',
    # rmng-node-id-reservations has no GSIs: claimant_id is its partition key,
    # so quota counting is a base-table query (see claim/handlers/base.py).
}

# Mapping from index name to table name (logical key references; unchanged)
INDEX_TO_TABLE = {
    'USER_GROUP_MAPPING_GROUP_ID': 'USER_GROUP_MAPPING',
    'USER_DETAILS_EMAIL': 'USER_DETAILS',
    'USER_DETAILS_PHONE': 'USER_DETAILS',
    'GROUP_DEVICE_MAPPING_NODE_ID': 'GROUP_DEVICE_MAPPING',
    'GROUP_DEVICE_MAPPING_ALIAS_INDEX': 'GROUP_DEVICE_MAPPING',
    'NODE_REG_REQS_LIST': 'NODE_REG_REQS',
}

# IoT Resources
# Note: NODE_ROLE_NAME has the region appended (e.g., 'rmng-iot-node-role-us-east-1').
# IAM is account-global, so the region suffix is mandatory to avoid cross-region collision.
IOT_RESOURCES = {
    'NODE_ROLE_NAME': 'rmng-iot-node-role',
    'DEFAULT_THING_POLICY_NAME': 'rmng-base-node-policy',
    'DEVICE_FILE_POLICY_NAME': 'rmng-node-file-policy',
    'DEVICE_FILE_ROLE_NAME': 'rmng-node-file-role',
    'DEVICE_FILE_ROLE_ALIAS': 'rmng-node-file-role-v1',
    'DEVICE_VIDEO_POLICY_NAME': 'rmng-node-video-policy',
    'DEVICE_VIDEO_ROLE_NAME': 'rmng-node-video-role',
    'DEVICE_VIDEO_ROLE_ALIAS': 'rmng-node-video-role-v1',
}

# Node-lifecycle hook Lambda names (see rmneo/node/nodelifecycle). Core owns the names by convention and async-invokes them if they exist; an optional stack creates the actual Lambdas. Absent => the hook is a no-op.
NODE_LIFECYCLE_HOOKS = {
    'NODE_LEFT_GROUP': 'rmng-node-left-group-hook',
}

# SSM Parameter Names (kebab path segments)
SSM_PARAMETERS = {
    # Assisted claiming only. The CA certificate is public material — the
    # private key is a non-exportable KMS key and never appears here or in any
    # other store (see docs/en/specs/assisted-claiming.md Req 11.3). Used by the CDK
    # to create the key-ARN parameter and to grant ssm access; the runtime
    # readers (claim handler and admin API) name these paths as constants in
    # claim/ca_bootstrap — keep the two in sync.
    'CLAIMING_CA_KEY_ARN': '/rmng/base/claiming-ca-key-arn',
    'CLAIMING_CA_CERT_PEM': '/rmng/base/claiming-ca-cert-pem',
    'CLAIMING_CONFIG': '/rmng/base/claiming-config',

    'API_GATEWAY_ID': '/rmng/base/api-gateway-id',
    'API_GATEWAY_URL': '/rmng/base/api-gateway-url',
    'API_GATEWAY_ROOT_RESOURCE_ID': '/rmng/base/api-gateway-root-resource-id',
    # The shared API's /v1 resource, published by rmng-core so a separate stack
    # (e.g. rmng-claim-core) can attach routes under /v1 without recreating it.
    'API_GATEWAY_V1_RESOURCE_ID': '/rmng/base/api-gateway-v1-resource-id',
    # /v1/admin, published by rmng-core so a separate stack (rmng-claim-core)
    # can attach admin routes under it without recreating it (which collides).
    'API_GATEWAY_V1_ADMIN_RESOURCE_ID': '/rmng/base/api-gateway-v1-admin-resource-id',
    'API_GATEWAY_V1_INTEGRATIONS_RESOURCE_ID': '/rmng/base/api-gateway-v1-integrations-resource-id',
    'API_GATEWAY_V1_ADMIN_INTEGRATIONS_RESOURCE_ID': '/rmng/base/api-gateway-v1-admin-integrations-resource-id',
    'ADMIN_RESOURCE_ID': '/rmng/base/admin-api-resource-id',
    'COGNITO_AUTHORIZER_ID': '/rmng/base/cognito-authorizer-id',
    'IDENTITY_POOL_ID': '/rmng/base/identity-pool-id',
    'RAW_TS_DATA_STREAM_ARN': '/rmng/base/raw-ts-data-stream-arn',
    'IOT_DATA_ATS_ENDPOINT': '/rmng/base/iot-data-ats-endpoint',
    'FILES_BUCKET_NAME': '/rmng/base/files-bucket-name',
    'OTA_SERVICE_ROLE_ARN': '/rmng/base/ota-service-role-arn',
    'ESP_USER_ISSUER': '/rmng/base/esp-user-issuer',
    'ESP_USER_CLIENT_ID': '/rmng/base/esp-user-client-id',
    'ESP_MCP_CLIENT_ID': '/rmng/base/esp-mcp-client-id',
    'ESP_MCP_CLIENT_SECRET': '/rmng/base/esp-mcp-client-secret',
    'ESP_USER_VA_CLIENT_ID': '/rmng/base/esp-user-va-client-id',
    'ESP_USER_VA_CLIENT_SECRET': '/rmng/base/esp-user-va-client-secret',
    'ESP_ADMIN_USER_POOL_ID': '/rmng/base/esp-admin-user-pool-id',
    'ESP_ADMIN_USER_POOL_CLIENT_ID': '/rmng/base/esp-admin-user-pool-client-id',
    'ESP_USER_JWKS': '/rmng/base/esp-user-jwks',
    'ESP_ADMIN_USER_POOL_JWKS': '/rmng/base/esp-admin-user-pool-jwks',
    'GSI_TRIGGER_LAMBDA_ARN': '/rmng/base/gsi-trigger-lambda-arn',
    'GSI_STATE_MACHINE_ARN': '/rmng/base/gsi-state-machine-arn',
}

# SSM Parameter Prefixes
SSM_PARAMETER_PREFIXES = {
    'GVA_CONFIG': '/rmng/gva/',
    'ALEXA_CONFIG': '/rmng/alexa/',
    'ST_CONFIG': '/rmng/smartthings/',
}

# S3 Bucket Name Pattern
S3_BUCKETS = {
    'FILES_BUCKET_NAME': 'esp-rm-files',
}

# Lambda function names (deployed name = prefix + purpose; see
# app_common.create_lambda_function). Referenced by deterministic name for
# cross-lambda async invokes without a CFN cross-stack dependency.
FUNCTION_NAMES = {
    'NOTIFICATIONS': 'rmng-notifications',
}
