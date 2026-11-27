#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Base Resource Constants for ESP User infrastructure.

This file contains constants for resources defined in the ESP User base stack
that need to be referenced in the core stack.
"""
import base64
from pathlib import Path

USER_TABLE_NAMES = {
    'USER_DETAILS': 'espuser-user-details',
    'OTP': 'espuser-otp',
    'REFRESH_TOKENS': 'espuser-refresh-tokens',
    'OAUTH_CLIENTS': 'espuser-oauth-clients',
    'AUTH_FLOWS': 'espuser-auth-flows',
    'ADMIN_CONFIG': 'espuser-admin-config',
    'IDENTITY_PROVIDERS': 'espuser-identity-providers',
    'SESSIONS': 'espuser-sessions',
}

USER_INDEX_NAMES = {
    'USER_DETAILS_EMAIL': 'espuser-user-details-by-email',
    'USER_DETAILS_PHONE': 'espuser-user-details-by-phone',
    'AUTH_FLOWS_BY_CODE': 'espuser-auth-flows-by-code',
    'SESSIONS_BY_USER': 'espuser-sessions-by-user',
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
    'HOME_APP_CLIENT_ID': '/espuser/base/home-app-client-id',
    'ESP_MCP_CLIENT_ID': '/espuser/base/mcp-client-id',
    'ESP_MCP_CLIENT_SECRET': '/espuser/base/mcp-client-secret',
    'ESP_ADMIN_USER_POOL_ID': '/espuser/base/admin-user-pool-id',
    'ESP_ADMIN_USER_POOL_CLIENT_ID': '/espuser/base/admin-user-pool-client-id',
    'ESP_USER_JWKS': '/espuser/base/user-jwks-json',
    'ESP_ADMIN_USER_POOL_JWKS': '/espuser/base/admin-user-pool-jwks-json',
    'ESP_USER_KMS_SIGNING_KEY_ARN': '/espuser/signing/kms-key-arn',
    # Dedicated HMAC secret for signing refresh tokens (SecureString). Isolated from the RS256 signing key; generated once at deploy and left untouched on redeploy.
    'ESP_USER_REFRESH_SECRET': '/espuser/signing/refresh-secret',
    # The sign-in page's footer links, which the operator sets; "none" hides a link.
    'ESP_USER_SUPPORT_URL': '/espuser/base/support-url',
    'ESP_USER_PRIVACY_URL': '/espuser/base/privacy-url',
    'ESP_USER_TERMS_URL': '/espuser/base/terms-url',
}


HOME_APP_CLIENT_ID = 'rainmaker-neo-home-app-client'
# The app returns here from both sign-in and sign-out.
HOME_APP_REDIRECT_URI = 'rainmaker://com.espressif.novahome/success'
# Shown beside the app's name on the consent screen and in a person's session lists; a 112px PNG because the seed hands its Lambda every client in one environment variable, which Lambda caps at 4 KB.
HOME_APP_LOGO_URI = 'data:image/png;base64,' + base64.b64encode((Path(__file__).resolve().parents[3] / 'assets' / 'rainmaker_home_logo.png').read_bytes()).decode()


def first_party_client_ids(home_app_client_id: str) -> list:
    """The apps the identity pool federates and USER_CLIENT_ID pins for /v1/user/credentials, listed once so the two cannot drift: the Home app client espuser-base publishes, which is HOME_APP_CLIENT_ID."""
    return [home_app_client_id]


# The scopes of a client that acts while the person is away (an assistant link, an MCP connector); offline_access keeps its grant through every sign-out (OIDC Core 1.0 s11).
OFFLINE_CLIENT_SCOPES = ['openid', 'email', 'phone', 'profile', 'offline_access']

# First-party OIDC clients seeded into espuser-oauth-clients (single source of truth). The seed
# custom resource injects these definitions into its handler and derives its re-invoke trigger
# from them, so the resource re-fires whenever a client's config changes. The config is ours
# (OAuth 2.1): the registry only accepts authorization_code/refresh_token grants.
SEEDED_OAUTH_CLIENTS = [
    # MCP OAuth proxy: confidential client (secret-authenticated at the token endpoint). Its callback
    # (<MCP_BASE_URL>/oauth2/callback) is only known after rmng deploys, so redirect_uris is set then.
    {'client_id': 'mcp-oauth-client', 'client_name': 'MCP OAuth Proxy', 'client_type': 'confidential', 'application_type': 'web',
     'grant_types': ['authorization_code', 'refresh_token'], 'scopes': OFFLINE_CLIENT_SCOPES},
    # The Espressif RainMaker Home app, and the client the password API issues to; an app with its own scheme adds its redirect through the admin API, and the seed never removes it.
    # A native app: each of its sign-ins gets a session of its own, so a browser sign-out on the phone leaves it signed in.
    {'client_id': HOME_APP_CLIENT_ID, 'client_name': 'RainMaker Home', 'client_type': 'public', 'application_type': 'native',
     'grant_types': ['authorization_code', 'refresh_token'], 'require_pkce': True, 'first_party': True,
     'redirect_uris': [HOME_APP_REDIRECT_URI], 'logo_uri': HOME_APP_LOGO_URI},
]

# The seeded clients whose secret espuser-base surfaces as a stack output, by the seed's attribute name.
SEEDED_CLIENT_SECRET_ATTRIBUTES = {'mcp-oauth-client': 'McpClientSecret'}

# The APIs each seeded client calls, its default first. Each API's own stack grants its resource once the API is discovered, since neo and MCP only exist after espuser deploys.
SEEDED_CLIENT_APIS = {
    'mcp-oauth-client': ['mcp'],
    HOME_APP_CLIENT_ID: ['neo', 'espuser'],
}


def seeded_client_grants(api: str) -> list:
    """The seeded clients that call api, each with whether it is that client's default: "espuser" -> [("rainmaker-neo-home-app-client", False)]."""
    return [(client_id, apis[0] == api) for client_id, apis in SEEDED_CLIENT_APIS.items() if api in apis]


