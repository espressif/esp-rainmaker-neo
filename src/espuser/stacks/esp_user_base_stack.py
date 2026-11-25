# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import os
import sys

from aws_cdk import (
    CfnOutput,
    CustomResource,
    Duration,
    RemovalPolicy,
    Stack,
    aws_cognito as cognito,
    aws_dynamodb,
    aws_iam as iam,
    aws_kms as kms,
    aws_lambda as lambda_,
)
from constructs import Construct

sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from app_common import (
    CommonResources,
    create_cognito_authorizer,
    create_cognito_user_pool,
    create_cognito_user_pool_client,
    create_cognito_user_pool_domain,
    create_kms_signing_key,
    create_rest_api,
    create_ssm_string_parameter,
    create_write_once_secret,
    discover_api_custom_domain,
    discover_cognito_custom_domain,
    discover_cognito_email_configuration,
    grant_client_resources,
    seed_oauth_clients,
)
from gsi_infra import GsiInfraCore, GsiReadinessGate, ManagedTable

from ..handlers.publish_discovery.stack import PublishDiscovery
from .base_res_constants import (
    HOME_APP_CLIENT_ID,
    SEEDED_CLIENT_SECRET_ATTRIBUTES,
    SEEDED_OAUTH_CLIENTS,
    USER_COGNITO_DOMAIN_PREFIXES,
    USER_INDEX_NAMES,
    USER_SSM_PARAMETERS,
    USER_TABLE_NAMES,
    seeded_client_grants,
)


class CreateCommonBaseResources(Construct):
    """Creates common resources"""
    def __init__(self, scope: Construct, id: str, *, common_resources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.esp_user_api = create_rest_api(
            self, "EspUserApi",
            rest_api_name="EspUserApi",
            description="ESP User REST API Gateway for user management endpoints",
        )

        self.user_tables = CreateUserTables(
            self, "CreateUserTables",
            common_resources=common_resources,
        )

        self.admin_api_base_res = self.esp_user_api.root.add_resource("admin")

        self.jwks_fetcher_lambda = lambda_.Function(
            self, "JWKSFetcherLambda",
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            code=lambda_.Code.from_inline("""
import json
import urllib3
import cfnresponse

def handler(event, context):
    # Log the raw event first — carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place (None on Create → cfnresponse
    # assigns one). Returning a NEW id on Update would make CloudFormation replace + delete the old one.
    physical_id = event.get('PhysicalResourceId')
    try:
        if event['RequestType'] in ['Create', 'Update']:
            user_pool_id = event['ResourceProperties']['UserPoolId']
            region = event['ResourceProperties']['Region']
            http = urllib3.PoolManager()
            url = f'https://cognito-idp.{region}.amazonaws.com/{user_pool_id}/.well-known/jwks.json'
            response = http.request('GET', url)
            jwks = json.loads(response.data.decode('utf-8'))
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {'JWKS': json.dumps(jwks)}, physical_id)
        else:
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
    except Exception as e:
        print(f'Error: {str(e)}')
        cfnresponse.send(event, context, cfnresponse.FAILED, {}, physical_id)
"""),
            timeout=Duration.seconds(30),
        )

class CreateUserTables(Construct):
    """Creates DynamoDB tables for user management"""
    def __init__(self, scope: Construct, id: str, *, common_resources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.user_details_table = ManagedTable(
            self, "UserDetailsTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['USER_DETAILS'],
            partition_key=aws_dynamodb.Attribute(name="user_id", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        self.user_details_table.add_global_secondary_index(
            index_name=USER_INDEX_NAMES['USER_DETAILS_EMAIL'],
            partition_key=aws_dynamodb.Attribute(name="email", type=aws_dynamodb.AttributeType.STRING),
            projection_type=aws_dynamodb.ProjectionType.INCLUDE,
            non_key_attributes=["phone"]
        )

        self.user_details_table.add_global_secondary_index(
            index_name=USER_INDEX_NAMES['USER_DETAILS_PHONE'],
            partition_key=aws_dynamodb.Attribute(name="phone", type=aws_dynamodb.AttributeType.STRING),
            projection_type=aws_dynamodb.ProjectionType.INCLUDE,
            non_key_attributes=["email"]
        )

        self.auth_flows_table = ManagedTable(
            self, "AuthFlowsTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['AUTH_FLOWS'],
            partition_key=aws_dynamodb.Attribute(name="flow_id", type=aws_dynamodb.AttributeType.STRING),
            time_to_live_attribute="expires_on",
            removal_policy=RemovalPolicy.DESTROY,
        )

        self.auth_flows_table.add_global_secondary_index(
            index_name=USER_INDEX_NAMES['AUTH_FLOWS_BY_CODE'],
            partition_key=aws_dynamodb.Attribute(name="code", type=aws_dynamodb.AttributeType.STRING),
            projection_type=aws_dynamodb.ProjectionType.ALL
        )

        self.refresh_tokens_table = ManagedTable(
            self, "RefreshTokensTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['REFRESH_TOKENS'],
            partition_key=aws_dynamodb.Attribute(name="user_id", type=aws_dynamodb.AttributeType.STRING),
            sort_key=aws_dynamodb.Attribute(name="client_family", type=aws_dynamodb.AttributeType.STRING),
            time_to_live_attribute="expires_on",
            removal_policy=RemovalPolicy.DESTROY,
        )

        self._seed_refresh_secret()

        self.oauth_clients_table = ManagedTable(
            self, "OAuthClientsTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['OAUTH_CLIENTS'],
            partition_key=aws_dynamodb.Attribute(name="client_id", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        self.admin_config_table = ManagedTable(
            self, "AdminConfigTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['ADMIN_CONFIG'],
            partition_key=aws_dynamodb.Attribute(name="config_name", type=aws_dynamodb.AttributeType.STRING),
            sort_key=aws_dynamodb.Attribute(name="subtype", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        self.identity_providers_table = ManagedTable(
            self, "IdentityProvidersTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['IDENTITY_PROVIDERS'],
            partition_key=aws_dynamodb.Attribute(name="provider_name", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # The authorization server's own user agent row (SSO). Keyed by the SHA-256 of the cookie
        # value; expires_at is the rolling retention deadline and doubles as the TTL attribute,
        # so a user agent the person stops using is swept once its retention lapses.
        self.sessions_table = ManagedTable(
            self, "SessionsTable",
            common_resources=common_resources,
            table_name=USER_TABLE_NAMES['SESSIONS'],
            partition_key=aws_dynamodb.Attribute(name="session_hash", type=aws_dynamodb.AttributeType.STRING),
            time_to_live_attribute="expires_at",
            removal_policy=RemovalPolicy.DESTROY,
        )

        # "Which sessions does this person have, and end that one." The base table is keyed
        # by the SHA-256 of the cookie value, and that hash cannot be derived from a sid --
        # so without this index a session is reachable only from the browser holding its
        # cookie, and signing out a user agent you are not on is impossible, not merely awkward.
        #
        # KEYS_ONLY is the whole cost story. expires_at is rewritten on every /oauth2/authorize
        # and every token refresh, and DynamoDB writes an index entry only when a key or a
        # PROJECTED attribute changes. Projecting nothing keeps the hot path free: entries
        # change on session create and delete only, twice per session lifetime. It is also the
        # only way a back-channel token refresh can reach a user agent from its sid.
        self.sessions_table.add_global_secondary_index(
            index_name=USER_INDEX_NAMES['SESSIONS_BY_USER'],
            partition_key=aws_dynamodb.Attribute(name="user_id", type=aws_dynamodb.AttributeType.STRING),
            sort_key=aws_dynamodb.Attribute(name="sid", type=aws_dynamodb.AttributeType.STRING),
            projection_type=aws_dynamodb.ProjectionType.KEYS_ONLY,
        )

        self._seed_oauth_clients()

    def _seed_oauth_clients(self) -> None:
        """Seed the ESP-User OIDC client registry (espuser-oauth-clients) for the clients this deployment ships.

        A missing client is created; an existing one only gains the declared fields and redirects it lacks. A client that declares scopes regains its declared scopes on every seed run, so an operator may add scopes to it but not remove them; any other client's scopes are never touched.
        """
        self.seed_oauth_clients_cr = seed_oauth_clients(
            self, "SeedOAuthClients",
            name="espuser-oauth-clients-seed",
            table_name=USER_TABLE_NAMES['OAUTH_CLIENTS'],
            clients=SEEDED_OAUTH_CLIENTS,
            secret_attributes=SEEDED_CLIENT_SECRET_ATTRIBUTES,
        )
        self.seed_oauth_clients_cr.node.add_dependency(self.oauth_clients_table)

    def _seed_refresh_secret(self) -> None:
        """Rotating this secret would invalidate every outstanding refresh token, hence write-once."""
        create_write_once_secret(self, "SeedRefreshSecret",
                                 parameter_name=USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
                                 physical_id="seed-refresh-secret")

class CreateDiscoveryStorage(Construct):
    """The OIDC signing key and the JWKS published from it.

    The discovery documents are not stored anywhere: the well-known Lambda builds them per request
    from the issuer and serves them, with the JWKS from SSM, at the ESP User API's /.well-known/*.
    The key and the JWKS that advertises it are created and destroyed together. See
    src/espuser/docs/specs/oidc-oauth2.md.
    """
    def __init__(self, scope: Construct, id: str, *, common_resources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.signing_key = create_kms_signing_key(
            self, "OIDCSigningKey",
            alias_name=f"{common_resources.prefix}oidc-signing-key",
            description="ESP User OIDC RS256 token signing key",
            key_spec=kms.KeySpec.RSA_2048,
        )
        create_ssm_string_parameter(
            self, "EspUserKmsSigningKeyArnParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_KMS_SIGNING_KEY_ARN'],
            string_value=self.signing_key.key_arn,
            description="ARN of the KMS asymmetric key that signs ESP User OIDC tokens",
        )

    def publish(self, *, common_resources, issuer: str) -> None:
        """Record `issuer` and publish the JWKS to SSM."""
        self.issuer = issuer
        self.publish_discovery = PublishDiscovery(
            self, "PublishDiscovery", common_resources,
            signing_kms_key=self.signing_key,
        )

class CreateEndUserPoolResources(Construct):
    """Upstream identity provider for the federation broker and credential store for the
    /v1/user/auth/* password APIs. Clients never receive Cognito tokens — the issuer converts on the
    way back (see federation.md / legacy-user-auth.md).

    One confidential client, `espuser-idp-broker`, serves both the brokered redirect flow and the
    legacy password grant. Only our issuer holds its secret, so nobody who merely learns the client id
    can drive sign-up or password reset against the pool.
    """
    def __init__(self, scope: Construct, id: str, *, federation_callback_url: str,
                 federation_logout_callback_url: str, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)
        region = Stack.of(scope).region

        user_pool = create_cognito_user_pool(
            self, "UserPool",
            user_pool_name='ESP-End-Users',
            feature_plan=cognito.FeaturePlan.LITE,
            self_sign_up_enabled=True,
        )
        user_pool.node.default_child.email_configuration = discover_cognito_email_configuration(
            self, "UserPoolEmailDiscovery",
            name="end-user-pool-email-discovery",
            user_pool_logical_id=user_pool.node.default_child.logical_id,
        )

        domain = create_cognito_user_pool_domain(
            self, "CognitoDomain",
            user_pool=user_pool,
            domain_prefix=f"{USER_COGNITO_DOMAIN_PREFIXES['USER_POOL']}-{Stack.of(scope).account}-{region}",
        )

        broker_client = create_cognito_user_pool_client(
            self, "BrokerClient",
            user_pool=user_pool,
            user_pool_client_name="espuser-idp-broker",
            o_auth=cognito.OAuthSettings(
                flows=cognito.OAuthFlows(authorization_code_grant=True),
                scopes=[cognito.OAuthScope.OPENID, cognito.OAuthScope.EMAIL, cognito.OAuthScope.PHONE,
                        cognito.OAuthScope.PROFILE, cognito.OAuthScope.COGNITO_ADMIN],
                callback_urls=[federation_callback_url],
                # The ONE sign-out URL this pool needs to know, however many products exist
                # -- the counterpart of the single callback above. At sign-out the issuer
                # hands the browser here so the pool ends its own session, then forwards to
                # whichever product asked. Declared here rather than set by hand because
                # UpdateUserPoolClient replaces a client's whole configuration: a manual
                # edit that forgets one field silently drops the callback URL and every
                # login fails with redirect_mismatch.
                logout_urls=[federation_logout_callback_url],
            ),
            generate_secret=True,
            refresh_token_validity=Duration.days(3650),
        )

        self.user_pool = user_pool
        self.broker_client = broker_client
        self.domain = domain
        self.issuer = f"https://cognito-idp.{region}.amazonaws.com/{user_pool.user_pool_id}"

class CreateAdminBaseResources(Construct):
    """Creates Admin related resources"""
    def __init__(self, scope: Construct, id: str, rest_api=None, config: dict = None, jwks_fetcher_lambda=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.config = config or {}

        admin_user_pool = create_cognito_user_pool(
            self, "AdminUserPool",
            user_pool_name='ESP-Admin-Users',
            self_sign_up_enabled=False,
            # SignInPolicy is rejected below Essentials, and CDK's default is
            # "ESSENTIALS for a newly created pool; LITE otherwise" — ambiguous for a
            # pool that already exists, so it is pinned rather than inferred.
            feature_plan=cognito.FeaturePlan.ESSENTIALS,
            # PASSWORD covers both the plain-password and SRP flows, so adding EMAIL_OTP
            # takes nothing away. sms_otp is deliberately absent: it needs a registered
            # SNS origination number, and phone sign-in is separate later work.
            # Note: OTP first factors are incompatible with *required* MFA, and under the
            # pool's optional MFA an admin who enrols MFA stops being OTP-eligible.
            sign_in_policy=cognito.SignInPolicy(
                allowed_first_auth_factors=cognito.AllowedFirstAuthFactors(
                    password=True,
                    email_otp=True,
                ),
            ),
            custom_attributes={
                "user_id": cognito.StringAttribute(
                    mutable=True,
                    max_len=256,
                    min_len=0
                ),
                "super_admin": cognito.StringAttribute(
                    mutable=True,
                    max_len=4,
                    min_len=0
                )
            }
        )

        admin_domain = create_cognito_user_pool_domain(
            self, "AdminDomain",
            user_pool=admin_user_pool,
            domain_prefix=f"{USER_COGNITO_DOMAIN_PREFIXES['ADMIN_POOL']}-{scope.account}-{scope.region}",
        )

        admin_client = create_cognito_user_pool_client(
            self, "AdminClient",
            user_pool=admin_user_pool,
            user_pool_client_name="admin-client",
            # Overriding auth_flows replaces the shared default wholesale, so
            # user_password and user_srp are respelled to keep existing sign-in intact.
            # user=True is ALLOW_USER_AUTH, without which passwordless is unavailable.
            auth_flows=cognito.AuthFlow(user_password=True, user_srp=True, user=True),
            # The default 3 minutes has to cover email delivery plus the admin finding
            # and typing the code. Permitted range is 3-15.
            auth_session_validity=Duration.minutes(5),
            refresh_token_validity=Duration.days(30),
        )

        admin_jwks_fetcher = CustomResource(
            self, "AdminJWKSFetcher",
            service_token=jwks_fetcher_lambda.function_arn,
            properties={
                "UserPoolId": admin_user_pool.user_pool_id,
                "Region": Stack.of(scope).region
            }
        )

        admin_jwks_param = create_ssm_string_parameter(
            self, "AdminJWKSParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'],
            string_value=admin_jwks_fetcher.get_att_string('JWKS'),
            description="JWKS for Cognito Admin User Pool (used by voice assistants)"
        )

        self.admin_user_pool = admin_user_pool
        self.admin_client = admin_client
        self.admin_domain = admin_domain

        self.admin_oauth_issuer = f"https://cognito-idp.{scope.region}.amazonaws.com/{admin_user_pool.user_pool_id}"
        self.admin_jwks_param = admin_jwks_param

        self.admin_cognito_authorizer = create_cognito_authorizer(
            self, "AdminCognitoAuthorizer",
            authorizer_name="EspAdminCognitoAuthorizer",
            user_pool_arn=f"arn:aws:cognito-idp:{scope.region}:{scope.account}:userpool/{admin_user_pool.user_pool_id}",
            rest_api_id=rest_api.rest_api_id,
        )

class EspUserBaseStack(Stack):
    """Stack containing all user-related base resources including Cognito, DynamoDB tables, and API gateway"""
    def __init__(self, scope: Construct, construct_id: str, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        self.common_resources = CommonResources(prefix="espuser-")
        self.gsi_infra = GsiInfraCore(
            self, "GsiInfra",
            common_resources=self.common_resources,
        )

        self.common_base_resources = CreateCommonBaseResources(
            self, "CreateCommonBaseResources",
            common_resources=self.common_resources,
        )

        self.admin_base_resources = CreateAdminBaseResources(
            self, "CreateAdminBaseResources",
            rest_api=self.common_base_resources.esp_user_api,
            jwks_fetcher_lambda=self.common_base_resources.jwks_fetcher_lambda
        )

        self.discovery_storage = CreateDiscoveryStorage(
            self, "CreateDiscoveryStorage",
            common_resources=self.common_resources,
        )

        # Resolved before the end-user pool, so its callbacks use the custom-domain /oauth2/* endpoints.
        # The issuer is the API's one custom domain, else its own execute-api URL; the deploy fails when more than one domain is mapped.
        esp_user_api = discover_api_custom_domain(
            self, "EspUserApiCustomDomainDiscovery",
            name="espuser-api-custom-domain-discovery",
            api_id=self.common_base_resources.esp_user_api.rest_api_id,
            api_type="REST",
            default_url=self.common_base_resources.esp_user_api.url,
            is_issuer=True,
        )

        # url has no trailing slash, so the "/path" suffix joins correctly whether or not a
        # custom domain is mapped. Federated sign-in breaks if this host is not the one the IdPs
        # are registered against, so it has to be the resolved hostname, not the execute-api one.
        federation_callback_url = f"{esp_user_api.url}/oauth2/federation/callback"
        federation_logout_callback_url = f"{esp_user_api.url}/oauth2/federation/logout/callback"
        self.end_user_pool = CreateEndUserPoolResources(
            self, "CreateEndUserPoolResources",
            federation_callback_url=federation_callback_url,
            federation_logout_callback_url=federation_logout_callback_url,
        )
        self._publish_end_user_pool_params()

        self.discovery_storage.publish(common_resources=self.common_resources, issuer=esp_user_api.url)

        self.gsi_readiness = GsiReadinessGate(
            self, "GsiReadiness",
            common_resources=self.common_resources,
        )

        for base in (
            self.common_base_resources,
            self.admin_base_resources,
            self.end_user_pool,
        ):
            self.gsi_readiness.node.add_dependency(base)


        self.admin_pool_custom_domain, admin_oauth_host = discover_cognito_custom_domain(
            self, "AdminUserPoolCustomDomain",
            name="admin-user-pool-custom-domain",
            user_pool_id=self.admin_base_resources.admin_user_pool.user_pool_id,
            prefix_domain=self.admin_base_resources.admin_domain.domain_name,
        )

        # End-user hosted-UI host: the broker resolves the upstream's endpoints from the
        # issuer discovery document at runtime, but the output must still report the
        # custom domain when one is mapped so operators and integrations see real hostnames.
        self.end_user_pool_custom_domain, end_user_oauth_host = discover_cognito_custom_domain(
            self, "EndUserPoolCustomDomain",
            name="end-user-pool-custom-domain",
            user_pool_id=self.end_user_pool.user_pool.user_pool_id,
            prefix_domain=self.end_user_pool.domain.domain_name,
        )

        self._seed_identity_providers()

        CfnOutput(self, "EspUserApiUrl",
            value=esp_user_api.url,
            description="ESP User API Gateway URL"
        )

        CfnOutput(self, "EspUserApiResource",
            value=esp_user_api.resource,
            description="RFC 8707 resource a client requests to get an access token for the ESP User API"
        )

        espuser_client_resources = grant_client_resources(
            self, "EspUserApiClientResources",
            name="espuser-api-client-resources",
            table_name=USER_TABLE_NAMES['OAUTH_CLIENTS'],
            grants=[(client_id, esp_user_api, is_default)
                    for client_id, is_default in seeded_client_grants('espuser')],
        )
        espuser_client_resources.node.add_dependency(self.common_base_resources.user_tables.seed_oauth_clients_cr)

        CfnOutput(self, "EspUserApiAudiences",
            value=esp_user_api.audiences,
            description="Comma-separated audiences the ESP User API accepts in an access token"
        )

        CfnOutput(self, "EspUserIssuer",
            value=self.discovery_storage.issuer,
            description="ESP User Issuer"
        )

        # discover_api_custom_domain returns the base with no trailing slash (both the
        # custom-domain and the rstrip'd default branch), so the separator has to be explicit —
        # interpolating the path directly yielded `...espressif.comoauth2/token`.
        oauth2_base = f"{esp_user_api.url}/oauth2"

        CfnOutput(self, "EspUserAuthorizeUrl",
            value=f"{oauth2_base}/authorize",
            description="User OAuth2 Authorize URL"
        )
        CfnOutput(self, "EspUserTokenUrl",
            value=f"{oauth2_base}/token",
            description="User OAuth2 Token URL [visibility:public]"
        )
        CfnOutput(self, "EspUserUserInfoUrl",
            value=f"{oauth2_base}/userinfo",
            description="User OAuth2 UserInfo URL"
        )
        CfnOutput(self, "EspUserRevokeUrl",
            value=f"{oauth2_base}/revoke",
            description="User OAuth2 Revoke URL"
        )

        CfnOutput(self, "HomeAppClientId",
            value=HOME_APP_CLIENT_ID,
            description="RainMaker Home app client ID: its browser sign-in uses it and the password API issues to it"
        )

        CfnOutput(self, "EspMcpClientId",
            value="mcp-oauth-client",
            description="ESP User MCP OAuth Client ID"
        )

        CfnOutput(self, "EspMcpClientSecret",
            value=self.common_base_resources.user_tables.seed_oauth_clients_cr.get_att_string("McpClientSecret"),
            description="ESP User MCP OAuth Client Secret [visibility:private]"
        )

        CfnOutput(self, "EspAdminCognitoAuthorizerId",
            value=self.admin_base_resources.admin_cognito_authorizer.ref,
            description="ESP Admin Cognito Authorizer ID"
        )

        CfnOutput(self, "EspAdminUserPoolId",
            value=self.admin_base_resources.admin_user_pool.user_pool_id,
            description="ESP Admin User Pool ID"
        )
        CfnOutput(self, "EspAdminUserPoolDomain",
            value=self.admin_base_resources.admin_domain.domain_name,
            description="ESP Admin User Pool Domain"
        )

        CfnOutput(self, "EspAdminUserPoolOAuthIssuer",
            value=self.admin_base_resources.admin_oauth_issuer,
            description="ESP Admin User Pool OAuth Issuer"
        )

        CfnOutput(self, "EspAdminAuthorizeUrl",
            value=f"https://{admin_oauth_host}/oauth2/authorize",
            description="Admin OAuth2 Authorize URL"
        )
        CfnOutput(self, "EspAdminTokenUrl",
            value=f"https://{admin_oauth_host}/oauth2/token",
            description="Admin OAuth2 Token URL [visibility:public]"
        )
        CfnOutput(self, "EspAdminUserInfoUrl",
            value=f"https://{admin_oauth_host}/oauth2/userInfo",
            description="Admin OAuth2 UserInfo URL"
        )
        CfnOutput(self, "EspAdminRevokeUrl",
            value=f"https://{admin_oauth_host}/oauth2/revoke",
            description="Admin OAuth2 Revoke URL"
        )

        CfnOutput(self, "EspAdminUserPoolClientId",
            value=self.admin_base_resources.admin_client.user_pool_client_id,
            description="ESP Admin User Pool Client ID"
        )

        CfnOutput(self, "EspEndUserPoolDomain",
            value=self.end_user_pool.domain.domain_name,
            description="End-user pool Cognito domain prefix"
        )

        CfnOutput(self, "EspUserJWKSParameter",
            value=USER_SSM_PARAMETERS['ESP_USER_JWKS'],
            description="ESP User JWKS SSM Parameter Name"
        )

        CfnOutput(self, "EspAdminUserPoolJWKSParameter",
            value=USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'],
            description="ESP Admin User Pool JWKS SSM Parameter Name"
        )

        CfnOutput(self, "EspUserDiscoveryIssuer",
            value=self.discovery_storage.issuer,
            description="ESP User OIDC issuer (discovery documents base URL)"
        )

        create_ssm_string_parameter(
            self, "EspUserApiIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_API_ID'],
            string_value=self.common_base_resources.esp_user_api.rest_api_id,
            description="ESP User API Gateway ID"
        )

        create_ssm_string_parameter(
            self, "EspUserApiRootResourceIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_API_ROOT_RESOURCE_ID'],
            string_value=self.common_base_resources.esp_user_api.root.resource_id,
            description="ESP User API Gateway Root Resource ID"
        )

        create_ssm_string_parameter(
            self, "EspAdminCognitoAuthorizerIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_ADMIN_COGNITO_AUTHORIZER_ID'],
            string_value=self.admin_base_resources.admin_cognito_authorizer.ref,
            description="ESP Admin Cognito Authorizer ID"
        )

        create_ssm_string_parameter(
            self, "EspUserApiUrlParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_API_URL'],
            string_value=esp_user_api.url,
            description="ESP User API Gateway URL"
        )

        create_ssm_string_parameter(
            self, "EspUserIssuerParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_ISSUER'],
            string_value=self.discovery_storage.issuer,
            description="ESP User Issuer"
        )

        create_ssm_string_parameter(
            self, "HomeAppClientIdParameter",
            parameter_name=USER_SSM_PARAMETERS['HOME_APP_CLIENT_ID'],
            string_value=HOME_APP_CLIENT_ID,
            description="RainMaker Home app client ID"
        )

        create_ssm_string_parameter(
            self, "EspMcpClientIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_MCP_CLIENT_ID'],
            string_value="mcp-oauth-client",
            description="ESP User MCP OAuth Client ID"
        )

        create_ssm_string_parameter(
            self, "EspMcpClientSecretParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_MCP_CLIENT_SECRET'],
            string_value=self.common_base_resources.user_tables.seed_oauth_clients_cr.get_att_string("McpClientSecret"),
            description="ESP User MCP OAuth Client Secret"
        )

        create_ssm_string_parameter(
            self, "EspAdminUserPoolIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID'],
            string_value=self.admin_base_resources.admin_user_pool.user_pool_id,
            description="ESP Admin User Pool ID"
        )

        create_ssm_string_parameter(
            self, "EspAdminUserPoolClientIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID'],
            string_value=self.admin_base_resources.admin_client.user_pool_client_id,
            description="ESP Admin User Pool Client ID"
        )

    def _publish_end_user_pool_params(self) -> None:
        """Only the credentials the core-stack lambdas cannot discover at runtime; everything else
        about the upstream lives on its identity-provider registry row."""
        p = self.end_user_pool
        CfnOutput(self, "EspEndUserPoolId", value=p.user_pool.user_pool_id, description="End-user Cognito pool id")

    def _seed_identity_providers(self) -> None:
        """Gives the broker an enabled upstream from the first deploy. Refreshes the connection
        config (issuer/client/secret) on reseed — recreating the pool changes those — but preserves
        an operator's enabled/disabled choice, so a redeploy never re-enables a disabled provider."""
        table = USER_TABLE_NAMES['IDENTITY_PROVIDERS']
        region = Stack.of(self).region
        account = Stack.of(self).account
        seed_fn = lambda_.Function(
            self, "SeedIdentityProvidersFunction",
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            timeout=Duration.minutes(2),
            environment={
                "PROVIDERS_TABLE": table,
                "COGNITO_ISSUER": self.end_user_pool.issuer,
                "COGNITO_CLIENT_ID": self.end_user_pool.broker_client.user_pool_client_id,
                "COGNITO_CLIENT_SECRET": self.end_user_pool.broker_client.user_pool_client_secret.unsafe_unwrap(),
                # Hosted-UI base (https://<prefix>.auth.<region>.amazoncognito.com) so the seed can
                # build the upstream sign-out URL below. Without it /oauth2/logout ends OUR session but
                # never Cognito's, and the next sign-in silently re-authenticates.
                "COGNITO_HOSTED_UI_URL": self.end_user_pool.domain.base_url(),
            },
            code=lambda_.Code.from_inline("""
import os, json, boto3, urllib3

# Self-contained CloudFormation custom-resource response (no cfnresponse dependency).
def _respond(event, context, status, physical_id):
    body = json.dumps({
        'Status': status,
        'Reason': f'See CloudWatch log stream: {context.log_stream_name}',
        'PhysicalResourceId': physical_id,
        'StackId': event['StackId'],
        'RequestId': event['RequestId'],
        'LogicalResourceId': event['LogicalResourceId'],
        'Data': {},
    }).encode('utf-8')
    urllib3.PoolManager().request('PUT', event['ResponseURL'], body=body,
                                  headers={'content-type': '', 'content-length': str(len(body))})

def handler(event, context):
    physical_id = event.get('PhysicalResourceId') or 'seed-identity-providers'
    try:
        if event['RequestType'] != 'Delete':
            # Endpoints and the JWKS come from the issuer's discovery document, so pinning only
            # identity and client config keeps the row valid as the upstream evolves. The secret
            # itself stays in SSM; only its parameter name is stored.
            table = boto3.resource('dynamodb').Table(os.environ['PROVIDERS_TABLE'])
            existing = table.get_item(Key={'provider_name': 'cognito'}).get('Item')
            attribute_mapping = {
                'email': 'email',
                'email_verified': 'email_verified',
                'phone_number': 'phone_number',
                'name': 'name',
                'locale': 'locale',
                'picture': 'picture',
                'external_sub': 'sub',
            }

            # Preserve an operator's enable/disable choice across redeploys; default to enabled only
            # on first create. Connection config below is still refreshed on every reseed.
            table.put_item(Item={
                'provider_name': 'cognito',
                'type': 'oidc',
                # Its password is typed on our page and checked against this pool, so it is never a redirect.
                'password_verifier': 'cognito',
                'enabled': existing.get('enabled', True) if existing else True,
                # The button label names the credential, not the vendor: nobody signing in knows what Cognito is.
                'display_name': 'Email / password',
                'issuer': os.environ['COGNITO_ISSUER'],
                'client_id': os.environ['COGNITO_CLIENT_ID'],
                'scopes': 'openid profile email phone aws.cognito.signin.user.admin',
                'client_secret': os.environ['COGNITO_CLIENT_SECRET'],
                'token_endpoint_auth': 'client_secret_basic',
                # Cognito's sign-out is not the OIDC-standard end_session_endpoint: it lives on the
                # hosted-UI domain and names its return parameter logout_uri, not
                # post_logout_redirect_uri. Both fields are on the row so /oauth2/logout hands the
                # browser to Cognito to end ITS session too -- otherwise sign-out ends ours alone and
                # the next login silently re-authenticates. client_id is baked in because Cognito
                # requires it on the logout URL.
                'end_session_url': f"{os.environ['COGNITO_HOSTED_UI_URL']}/logout?client_id={os.environ['COGNITO_CLIENT_ID']}",
                'end_session_redirect_param': 'logout_uri',
                'attribute_mapping': attribute_mapping,
            })
        _respond(event, context, 'SUCCESS', physical_id)
    except Exception as e:
        print('Error:', e)
        _respond(event, context, 'FAILED', physical_id)
"""),
        )
        seed_fn.add_to_role_policy(iam.PolicyStatement(
            actions=["dynamodb:PutItem", "dynamodb:GetItem"],
            resources=[f"arn:aws:dynamodb:{region}:{account}:table/{table}"],
        ))
        cr = CustomResource(
            self, "SeedIdentityProviders",
            service_token=seed_fn.function_arn,
            properties={
                "Issuer": self.end_user_pool.issuer,
                "ClientId": self.end_user_pool.broker_client.user_pool_client_id,
                # 9: the built-in row is an oidc row whose password_verifier is cognito.
                "SeedVersion": "9",
            },
        )
        cr.node.add_dependency(self.gsi_readiness)
