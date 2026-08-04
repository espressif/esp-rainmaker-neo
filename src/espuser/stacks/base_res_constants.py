#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Base Resource Constants for ESP User infrastructure.

This file contains constants for resources defined in the ESP User base stack
that need to be referenced in the core stack.
"""

USER_TABLE_NAMES = {
    'USER_DETAILS': 'espuser-user-details',
    'OTP': 'espuser-otp',
    'REFRESH_TOKENS': 'espuser-refresh-tokens',
    'OAUTH_CLIENTS': 'espuser-oauth-clients',
    'AUTH_FLOWS': 'espuser-auth-flows',
    'ADMIN_CONFIG': 'espuser-admin-config',
    'IDENTITY_PROVIDERS': 'espuser-identity-providers',
}

USER_INDEX_NAMES = {
    'USER_DETAILS_EMAIL': 'espuser-user-details-by-email',
    'USER_DETAILS_PHONE': 'espuser-user-details-by-phone',
    'AUTH_FLOWS_BY_CODE': 'espuser-auth-flows-by-code',
}

# Mapping from index name to table name (logical key references; unchanged)
USER_INDEX_TO_TABLE = {
    'USER_DETAILS_EMAIL': 'USER_DETAILS',
    'USER_DETAILS_PHONE': 'USER_DETAILS',
    'AUTH_FLOWS_BY_CODE': 'AUTH_FLOWS',
}

# Cognito Domain Prefixes (account+region appended at use site).
# Kept short on purpose: full prefix becomes `<value>-<account>-<region>`.
USER_COGNITO_DOMAIN_PREFIXES = {
    'ADMIN_POOL': 'esp-admin',
    'USER_POOL': 'esp-user', 
}

USER_SSM_PARAMETERS = {
    'ESP_USER_API_ID': '/espuser/base/api-id',
    'ESP_USER_API_URL': '/espuser/base/api-url',
    'ESP_USER_API_ROOT_RESOURCE_ID': '/espuser/base/api-root-resource-id',
    'ESP_USER_V1_RESOURCE_ID': '/espuser/base/v1-api-resource-id',
    'ESP_ADMIN_COGNITO_AUTHORIZER_ID': '/espuser/base/admin-cognito-authorizer-id',
    'ESP_USER_ISSUER': '/espuser/base/user-issuer',
    'ESP_USER_CLIENT_ID': '/espuser/base/user-client-id',
    'ESP_MCP_CLIENT_ID': '/espuser/base/mcp-client-id',
    'ESP_MCP_CLIENT_SECRET': '/espuser/base/mcp-client-secret',
    'ESP_VA_CLIENT_ID': '/espuser/base/va-client-id',
    'ESP_VA_CLIENT_SECRET': '/espuser/base/va-client-secret',
    'ESP_ADMIN_USER_POOL_ID': '/espuser/base/admin-user-pool-id',
    'ESP_ADMIN_USER_POOL_CLIENT_ID': '/espuser/base/admin-user-pool-client-id',
    'ESP_USER_JWKS': '/espuser/base/user-jwks-json',
    'ESP_ADMIN_USER_POOL_JWKS': '/espuser/base/admin-user-pool-jwks-json',
    'ESP_USER_KMS_SIGNING_KEY_ARN': '/espuser/signing/kms-key-arn',
    # Dedicated HMAC secret for signing refresh tokens (SecureString). Isolated from the RS256 signing key; generated once at deploy and left untouched on redeploy.
    'ESP_USER_REFRESH_SECRET': '/espuser/signing/refresh-secret',
}


# First-party OIDC clients seeded into espuser-oauth-clients (single source of truth). The seed
# custom resource injects these definitions into its handler and derives its re-invoke trigger
# from them, so the resource re-fires whenever a client's config changes. The ids mirror the
# Cognito user-pool app-clients so each app keeps a stable id, but the config is ours (OAuth 2.1):
# the registry only accepts authorization_code/refresh_token grants.
SEEDED_OAUTH_CLIENTS = [
    {'client_id': 'user-pool-client', 'client_name': 'RainMaker Client', 'client_type': 'public',
     'grant_types': ['authorization_code', 'refresh_token'], 'require_pkce': True},
    {'client_id': 'va-client', 'client_name': 'Voice Assistant', 'client_type': 'confidential',
     'grant_types': ['authorization_code', 'refresh_token']},
    # MCP OAuth proxy: confidential client (secret-authenticated at the token endpoint). Its callback
    # (<MCP_BASE_URL>/oauth2/callback) is only known after rmng deploys, so redirect_uris is set then.
    {'client_id': 'mcp-oauth-client', 'client_name': 'MCP OAuth Proxy', 'client_type': 'confidential',
     'grant_types': ['authorization_code', 'refresh_token']},
]


