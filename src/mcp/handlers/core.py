# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Duration,
    Stack,
    aws_iam as iam,
    aws_lambda as lambda_,
    aws_secretsmanager as secretsmanager,
    aws_apigatewayv2 as apigwv2,
    aws_apigatewayv2_integrations as apigwv2_integ,
)
from constructs import Construct
from app_common import stable_logical_id, add_http_api_routes, create_http_api, discover_api_custom_domain, create_lambda_log_group, lambda_log_group_arn


class McpOAuthConfig:
    """Configuration for the McpOAuthConstruct."""

    def __init__(
        self,
        # ESP User OIDC issuer the proxy brokers end-user auth to; the JWKS SSM param the
        # MCP verifier reads; and the registry client id the proxy presents (public PKCE).
        user_issuer: str = None,
        user_jwks_parameter: str = None,
        mcp_oidc_client_id: str = None,
        mcp_oidc_client_secret: str = None,
        # Paths to pre-built Go binaries
        mcp_binary_path: str = None,
        oauth_proxy_binary_path: str = None,
        # MCP Lambda customization
        mcp_extra_env: dict = None,
        mcp_extra_policies: list = None,
        mcp_timeout: Duration = Duration.seconds(10),
        # Family prefix prepended to Lambda physical names (e.g., "rmng-").
        prefix: str = "",
    ):
        self.user_issuer = user_issuer
        self.user_jwks_parameter = user_jwks_parameter
        self.mcp_oidc_client_id = mcp_oidc_client_id
        self.mcp_oidc_client_secret = mcp_oidc_client_secret
        self.mcp_binary_path = mcp_binary_path
        self.oauth_proxy_binary_path = oauth_proxy_binary_path
        self.mcp_extra_env = mcp_extra_env or {}
        self.mcp_extra_policies = mcp_extra_policies or []
        self.mcp_timeout = mcp_timeout
        self.prefix = prefix


class McpOAuthConstruct(Construct):
    """Deploys MCP + OAuth Proxy as a self-contained HTTP API.

    Creates:
    1. HTTP API Gateway v2 with CORS
    2. OAuth Proxy Lambda + Cognito UserPoolClient + routes
    3. MCP Lambda + routes

    Does NOT depend on CommonResources or app_common helpers.
    """

    def __init__(self, scope: Construct, id: str, config: McpOAuthConfig, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        account_id = Stack.of(self).account

        # --- HTTP API Gateway v2 ---
        # TODO: The purpose for using this, instead of REST API Gateway was that we could send the WWW-Authenticate
        # custom header in the Unauthorized response. Check if that is truly the case, or we can use the REST API Gateway instead.
        self.http_api = create_http_api(
            self, "McpHttpApi",
            api_name="mcp-http-api",
            cors_preflight=apigwv2.CorsPreflightOptions(
                allow_methods=[apigwv2.CorsHttpMethod.GET, apigwv2.CorsHttpMethod.POST],
                allow_origins=["*"],
                allow_headers=["Content-Type", "Authorization"],
                max_age=Duration.days(1),
            ),
        )
        self.custom_domain, self.api_url, self.api_endpoint = discover_api_custom_domain(
            self, "McpCustomDomainDiscovery",
            name="mcp-custom-domain-discovery",
            api_id=self.http_api.api_id,
            api_type="HTTP",
            default_url=self.http_api.api_endpoint,
        )

        # --- OAuth Proxy Lambda ---
        oauth_proxy_role_name = "mcp-oauth-proxy-role"
        oauth_proxy_role = iam.Role(
            self, "oauth_proxy_lambda_role",
            role_name=f"{config.prefix}{oauth_proxy_role_name}-{region}",
            assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
        )
        oauth_proxy_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", oauth_proxy_role_name))
        oauth_proxy_role.add_to_policy(iam.PolicyStatement(
            actions=["logs:CreateLogStream", "logs:PutLogEvents"],
            resources=[lambda_log_group_arn(self, f"{config.prefix}mcp-oauth-proxy")],
        ))

        # Stable HMAC secret for signing the OAuth `state` parameter. Independent
        # of any client secret and pinned by a fixed logical id + secret name so
        # in-flight signed states survive redeploys. Passed as an env value at
        # synth (matches how client secrets were previously injected).
        state_secret = secretsmanager.Secret(
            self, "OAuthStateSecret",
            secret_name=f"{config.prefix}mcp-oauth-state-secret",
            generate_secret_string=secretsmanager.SecretStringGenerator(
                password_length=48, exclude_punctuation=True,
            ),
        )
        state_secret.node.default_child.override_logical_id(
            stable_logical_id("SecretsManagerSecret", "mcp-oauth-state-secret"))

        oauth_proxy_env = {
            "AWS_ACCOUNT_ID": account_id,
            # The DISCOVERED base (custom domain when one is mapped, else execute-api):
            # MCP_BASE_URL drives the OAuth callback/metadata URLs clients are given, so
            # pointing it at the raw execute-api host while clients connect via the custom
            # domain would hand out redirect URIs on the wrong hostname.
            "MCP_BASE_URL": self.api_endpoint,
            "OAUTH_STATE_SECRET": state_secret.secret_value.unsafe_unwrap(),
        }
        if config.user_issuer:
            oauth_proxy_env["USER_ISSUER"] = config.user_issuer
        if config.mcp_oidc_client_id:
            oauth_proxy_env["MCP_OIDC_CLIENT_ID"] = config.mcp_oidc_client_id
        if config.mcp_oidc_client_secret:
            oauth_proxy_env["MCP_OIDC_CLIENT_SECRET"] = config.mcp_oidc_client_secret

        oauth_proxy_purpose = "mcp-oauth-proxy"
        oauth_proxy_aws_name = f"{config.prefix}{oauth_proxy_purpose}"
        self.oauth_proxy_function = lambda_.Function(
            self, "oauth_proxy",
            function_name=oauth_proxy_aws_name,
            handler="bootstrap",
            code=lambda_.Code.from_asset(path=config.oauth_proxy_binary_path),
            runtime=lambda_.Runtime.PROVIDED_AL2023,
            architecture=lambda_.Architecture.ARM_64,
            memory_size=128,
            timeout=Duration.seconds(10),
            role=oauth_proxy_role,
            environment=oauth_proxy_env,
            log_group=create_lambda_log_group(
                self, "oauth_proxy_log_group",
                purpose=oauth_proxy_purpose,
                aws_function_name=oauth_proxy_aws_name,
            ),
        )
        self.oauth_proxy_function.node.default_child.override_logical_id(
            stable_logical_id("LambdaFunc", oauth_proxy_purpose))

        # OAuth Proxy routes.
        # ApiGatewayV2::Integration has no physical name → safe to move across
        # files (CFN delete-create, no "already exists" conflict). Brief deploy
        # gap where routes return 502; auto-recovers.
        oauth_integration = apigwv2_integ.HttpLambdaIntegration(
            "OAuthProxyIntegration", self.oauth_proxy_function
        )

        oauth_proxy_route_specs = [
            ("/.well-known/oauth-protected-resource", [apigwv2.HttpMethod.GET]),
            ("/.well-known/oauth-authorization-server", [apigwv2.HttpMethod.GET]),
            ("/.well-known/test-cimd.json", [apigwv2.HttpMethod.GET]),
            ("/oauth2/authorize", [apigwv2.HttpMethod.GET]),
            ("/oauth2/callback", [apigwv2.HttpMethod.GET]),
            ("/oauth2/token", [apigwv2.HttpMethod.POST]),
        ]
        for path, methods in oauth_proxy_route_specs:
            add_http_api_routes(
                self.http_api,
                path=path,
                methods=methods,
                integration=oauth_integration,
            )

        # --- MCP Lambda ---
        mcp_role_name = "mcp-server-role"
        mcp_role = iam.Role(
            self, "mcp_lambda_role",
            role_name=f"{config.prefix}{mcp_role_name}-{region}",
            assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
        )
        mcp_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", mcp_role_name))
        mcp_role.add_to_policy(iam.PolicyStatement(
            actions=["logs:CreateLogStream", "logs:PutLogEvents"],
            resources=[lambda_log_group_arn(self, f"{config.prefix}mcp-server")],
        ))

        # Add project-specific extra policies
        for stmt in config.mcp_extra_policies:
            mcp_role.add_to_policy(stmt)

        # The MCP verifier resolves the issuer + JWKS from these, and requires end-user
        # tokens to carry aud == the OIDC client id (MCP_CLIENT_ID).
        mcp_env = {
            "AWS_ACCOUNT_ID": account_id,
            "USER_ISSUER": config.user_issuer or "",
            "USER_JWKS_PARA_NAME": config.user_jwks_parameter or "",
            "MCP_CLIENT_ID": config.mcp_oidc_client_id or "",
        }
        mcp_env.update(config.mcp_extra_env)

        mcp_purpose = "mcp-server"
        mcp_aws_name = f"{config.prefix}{mcp_purpose}"
        self.mcp_function = lambda_.Function(
            self, "mcp",
            function_name=mcp_aws_name,
            handler="bootstrap",
            code=lambda_.Code.from_asset(path=config.mcp_binary_path),
            runtime=lambda_.Runtime.PROVIDED_AL2023,
            architecture=lambda_.Architecture.ARM_64,
            memory_size=128,
            timeout=config.mcp_timeout,
            role=mcp_role,
            environment=mcp_env,
            log_group=create_lambda_log_group(
                self, "mcp_log_group",
                purpose=mcp_purpose,
                aws_function_name=mcp_aws_name,
            ),
        )
        self.mcp_function.node.default_child.override_logical_id(
            stable_logical_id("LambdaFunc", mcp_purpose))

        # MCP routes
        mcp_integration = apigwv2_integ.HttpLambdaIntegration(
            "MCPIntegration", self.mcp_function
        )

        add_http_api_routes(
            self.http_api,
            path="/v1/mcp",
            methods=[apigwv2.HttpMethod.GET, apigwv2.HttpMethod.POST, apigwv2.HttpMethod.OPTIONS],
            integration=mcp_integration,
        )
