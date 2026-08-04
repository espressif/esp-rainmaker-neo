# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from aws_cdk import Stack
from aws_cdk import Duration
from aws_cdk import RemovalPolicy
from aws_cdk import CfnOutput
from aws_cdk import aws_cognito as cognito
from aws_cdk import aws_iam as iam
from aws_cdk import aws_apigateway as apigateway
from aws_cdk import aws_dynamodb
from aws_cdk import aws_ssm as ssm
from aws_cdk import aws_s3 as s3
from aws_cdk import aws_ses as ses
from aws_cdk import aws_kms as kms
from aws_cdk import aws_lambda as lambda_
from aws_cdk import custom_resources as cr
from aws_cdk import aws_logs as logs
from aws_cdk import CustomResource
import sys
import os
import json
import hashlib
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from app_common import CommonResources, discover_cognito_custom_domain, discover_api_custom_domain, create_rest_api, create_ssm_string_parameter, create_cognito_user_pool, create_cognito_user_pool_client, create_cognito_user_pool_domain, create_cognito_authorizer, create_s3_bucket, create_kms_signing_key
from gsi_infra import GsiInfraCore, ManagedTable, GsiReadinessGate
from .base_res_constants import USER_TABLE_NAMES, USER_INDEX_NAMES, USER_SSM_PARAMETERS, USER_COGNITO_DOMAIN_PREFIXES, SEEDED_OAUTH_CLIENTS
from ..handlers.publish_discovery.stack import PublishDiscovery

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

        self.user_api_base_res = self.esp_user_api.root.add_resource("user")
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

        self._seed_oauth_clients()

    def _seed_oauth_clients(self) -> None:
        """Seed the ESP-User OIDC client registry (espuser-oauth-clients) for the current
        first-party apps. Inline custom-resource, idempotent conditional PutItem per client
        (an existing client is left untouched), so a re-deploy never clobbers admin edits."""

        seeded_clients_json = json.dumps(SEEDED_OAUTH_CLIENTS, sort_keys=True)
        seeded_clients_hash = hashlib.sha256(seeded_clients_json.encode()).hexdigest()

        stack = Stack.of(self)
        table_name = USER_TABLE_NAMES['OAUTH_CLIENTS']
        table_arn = f"arn:aws:dynamodb:{stack.region}:{stack.account}:table/{table_name}"
        seed_fn = lambda_.Function(
            self, "SeedOAuthClientsFunction",
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            timeout=Duration.seconds(120),
            environment={"SEEDED_CLIENTS": seeded_clients_json},
            code=lambda_.Code.from_inline("""
import json, os, time, secrets, boto3, urllib3
from botocore.exceptions import ClientError

http = urllib3.PoolManager()
# First-party OIDC clients, injected from SEEDED_OAUTH_CLIENTS (base_res_constants.py).
CLIENTS = json.loads(os.environ["SEEDED_CLIENTS"])

def send(event, context, status, data=None):
    body = json.dumps({
        "Status": status, "Reason": "See CloudWatch " + context.log_stream_name,
        "PhysicalResourceId": "seed-oauth-clients", "StackId": event["StackId"],
        "RequestId": event["RequestId"], "LogicalResourceId": event["LogicalResourceId"],
        "Data": data or {},
    }).encode("utf-8")
    http.request("PUT", event["ResponseURL"], body=body, headers={"content-type": ""})

def handler(event, context):
    try:
        data = {}
        if event["RequestType"] != "Delete":
            # The Lambda runs in the target region, so boto3 resolves it from AWS_REGION.
            table = boto3.resource("dynamodb").Table("%s")
            now = int(time.time())
            for c in CLIENTS:
                item = dict(c)
                item.setdefault("scopes", ["openid", "email", "phone", "profile"])
                item.update({"created_at": now, "updated_at": now})
                # Confidential clients get a generated plaintext secret (retrievable via list get_secret).
                if item["client_type"] == "confidential":
                    item["secret"] = secrets.token_urlsafe(32)
                # Conditional create: a client that already exists is left untouched (no-op).
                try:
                    table.put_item(Item=item, ConditionExpression="attribute_not_exists(client_id)")
                except ClientError as e:
                    if e.response["Error"]["Code"] != "ConditionalCheckFailedException":
                        raise
            # Return the current confidential-client secrets (minted above or from a prior deploy)
            # so the stack can surface them; read them back rather than trust the local values. A
            # confidential client with no secret means a stale row predating its confidential config
            # (conditional PutItem never overwrites it) — fail loudly rather than emit an empty secret,
            # which SSM rejects with an opaque PutParameter error.
            for cid in ("va-client", "mcp-oauth-client"):
                row = table.get_item(Key={"client_id": cid}).get("Item", {})
                secret = row.get("secret", "")
                if not secret:
                    raise Exception(cid + " has no secret; delete the stale row so it is re-seeded as confidential")
                data["VaClientSecret" if cid == "va-client" else "McpClientSecret"] = secret
        send(event, context, "SUCCESS", data)
    except Exception as e:
        print("seed error:", e)
        send(event, context, "FAILED")
""" % (table_name,)),
        )
        seed_fn.add_to_role_policy(iam.PolicyStatement(
            actions=["dynamodb:PutItem", "dynamodb:GetItem"],
            resources=[table_arn],
        ))

        self.seed_oauth_clients_cr = CustomResource(
            self, "SeedOAuthClients", service_token=seed_fn.function_arn,
            properties={"ClientsHash": seeded_clients_hash},
        )
        self.seed_oauth_clients_cr.node.add_dependency(self.oauth_clients_table)

    def _seed_refresh_secret(self) -> None:
        """Generate the refresh-token HMAC secret into a SecureString SSM param on first deploy.
        Inline custom resource; writes only if the param is absent, so a redeploy never rotates
        it (rotation would invalidate every outstanding refresh token). The value never enters
        the CloudFormation template."""
        stack = Stack.of(self)
        param_name = USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET']
        param_arn = f"arn:aws:ssm:{stack.region}:{stack.account}:parameter{param_name}"
        seed_fn = lambda_.Function(
            self, "SeedRefreshSecretFunction",
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            timeout=Duration.seconds(60),
            code=lambda_.Code.from_inline("""
import secrets, boto3, urllib3
from botocore.exceptions import ClientError

http = urllib3.PoolManager()
PARAM = "%s"

def send(event, context, status):
    import json
    body = json.dumps({
        "Status": status, "Reason": "See CloudWatch " + context.log_stream_name,
        "PhysicalResourceId": "seed-refresh-secret", "StackId": event["StackId"],
        "RequestId": event["RequestId"], "LogicalResourceId": event["LogicalResourceId"],
    }).encode("utf-8")
    http.request("PUT", event["ResponseURL"], body=body, headers={"content-type": ""})

def handler(event, context):
    try:
        if event["RequestType"] != "Delete":
            ssm = boto3.client("ssm")
            try:
                ssm.get_parameter(Name=PARAM, WithDecryption=True)
            except ssm.exceptions.ParameterNotFound:
                ssm.put_parameter(Name=PARAM, Value=secrets.token_urlsafe(48), Type="SecureString")
        send(event, context, "SUCCESS")
    except Exception as e:
        print("seed refresh secret error:", e)
        send(event, context, "FAILED")
""" % (param_name,)),
        )
        seed_fn.add_to_role_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter", "ssm:PutParameter"],
            resources=[param_arn],
        ))
        CustomResource(self, "SeedRefreshSecret", service_token=seed_fn.function_arn)

class CreateDiscoveryStorage(Construct):
    """Storage for the static OIDC/OAuth discovery documents.

    Tier 1 (default): a bucket whose objects are publicly readable over HTTPS at
    the S3 REST endpoint, so the issuer can be the S3 URL and no CloudFront is
    needed. Tier 2 (custom domain) front this bucket with CloudFront/OAC later;
    that does not change this storage. See espuser/docs/en/specs/oidc-oauth2.md.

    `api_base` is the API Gateway base URL the `/oauth2/*` endpoints live on; it
    is advertised in the discovery documents, which are served from the issuer
    (S3) host — the two hosts differ.

    The KMS signing key lives here too, so the key and the JWKS that advertises it are created
    and destroyed together.
    """
    def __init__(self, scope: Construct, id: str, *, common_resources, api_base: str, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region

        self.discovery_bucket = create_s3_bucket(
            self, "DiscoveryBucket", common_resources, "oauth",
            block_public_access=s3.BlockPublicAccess(
                block_public_acls=False,
                ignore_public_acls=False,
                block_public_policy=False,
                restrict_public_buckets=False,
            ),
            removal_policy=RemovalPolicy.DESTROY,
            auto_delete_objects=True,
        )

        self.discovery_bucket.add_to_resource_policy(iam.PolicyStatement(
            actions=["s3:GetObject"],
            resources=[self.discovery_bucket.arn_for_objects(".well-known/*")],
            principals=[iam.AnyPrincipal()],
        ))

        # Account-Regional Namespace means AWS composes the physical name, so read it off the bucket
        # rather than recomposing it here — this resolves to a CFN token, not a literal.
        self.issuer = f"https://{self.discovery_bucket.bucket_name}.s3.{region}.amazonaws.com"

        self.signing_key = create_kms_signing_key(
            self, "OIDCSigningKey",
            alias_name=f"{common_resources.prefix}oidc-signing-key",
            description="ESP User OIDC RS256 token signing key",
            key_spec=kms.KeySpec.RSA_2048,
            removal_policy=RemovalPolicy.RETAIN,
        )
        create_ssm_string_parameter(
            self, "EspUserKmsSigningKeyArnParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_KMS_SIGNING_KEY_ARN'],
            string_value=self.signing_key.key_arn,
            description="ARN of the KMS asymmetric key that signs ESP User OIDC tokens",
        )

        self.publish_discovery = PublishDiscovery(
            self, "PublishDiscovery", common_resources,
            discovery_bucket=self.discovery_bucket,
            issuer=self.issuer,
            api_base=api_base,
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
    def __init__(self, scope: Construct, id: str, *, federation_callback_url: str, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)
        region = Stack.of(scope).region

        user_pool = create_cognito_user_pool(
            self, "UserPool",
            user_pool_name='ESP-End-Users',
            feature_plan=cognito.FeaturePlan.LITE,
            self_sign_up_enabled=True,
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
        token_config = self.config.get('token_validity', {})

        admin_user_pool = create_cognito_user_pool(
            self, "AdminUserPool",
            user_pool_name='ESP-Admin-Users',
            self_sign_up_enabled=False,
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

        # Resolved before the end-user pool and CreateDiscoveryStorage so both the identity
        # providers' callback and the published .well-known documents advertise the custom-domain
        # /oauth2/* endpoints when one is mapped.
        self.api_custom_domain, esp_user_api_url, esp_user_api_base_url = discover_api_custom_domain(
            self, "EspUserApiCustomDomainDiscovery",
            name="espuser-api-custom-domain-discovery",
            api_id=self.common_base_resources.esp_user_api.rest_api_id,
            api_type="REST",
            default_url=self.common_base_resources.esp_user_api.url,
        )

        # base_url has no trailing slash, so the "/path" suffix joins correctly whether or not a
        # custom domain is mapped. Federated sign-in breaks if this host is not the one the IdPs
        # are registered against, so it has to be the resolved hostname, not the execute-api one.
        federation_callback_url = f"{esp_user_api_base_url}/oauth2/federation/callback"
        self.end_user_pool = CreateEndUserPoolResources(
            self, "CreateEndUserPoolResources",
            federation_callback_url=federation_callback_url,
        )
        self._publish_end_user_pool_params()

        self.discovery_storage = CreateDiscoveryStorage(
            self, "CreateDiscoveryStorage",
            common_resources=self.common_resources,
            api_base=esp_user_api_url,
        )

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
            value=esp_user_api_url,
            description="ESP User API Gateway URL"
        )

        CfnOutput(self, "EspUserIssuer",
            value=self.discovery_storage.issuer,
            description="ESP User Issuer"
        )

        # discover_api_custom_domain returns the base with no trailing slash (both the
        # custom-domain and the rstrip'd default branch), so the separator has to be explicit —
        # interpolating the path directly yielded `...espressif.comoauth2/token`.
        oauth2_base = f"{esp_user_api_url}/oauth2"

        CfnOutput(self, "EspUserAuthorizeUrl",
            value=f"{oauth2_base}/authorize",
            description="User OAuth2 Authorize URL"
        )
        CfnOutput(self, "EspUserTokenUrl",
            value=f"{oauth2_base}/token",
            description="User OAuth2 Token URL"
        )
        CfnOutput(self, "EspUserUserInfoUrl",
            value=f"{oauth2_base}/userinfo",
            description="User OAuth2 UserInfo URL"
        )
        CfnOutput(self, "EspUserRevokeUrl",
            value=f"{oauth2_base}/revoke",
            description="User OAuth2 Revoke URL"
        )

        CfnOutput(self, "EspUserClientId",
            value="user-pool-client",
            description="ESP User Client ID"
        )

        CfnOutput(self, "EspMcpClientId",
            value="mcp-oauth-client",
            description="ESP User MCP OAuth Client ID"
        )

        CfnOutput(self, "EspMcpClientSecret",
            value=self.common_base_resources.user_tables.seed_oauth_clients_cr.get_att_string("McpClientSecret"),
            description="ESP User MCP OAuth Client Secret [visibility:private]"
        )

        CfnOutput(self, "EspVaClientId",
            value="va-client",
            description="ESP User Voice Assistant Client ID"
        )

        CfnOutput(self, "EspVaClientSecret",
            value=self.common_base_resources.user_tables.seed_oauth_clients_cr.get_att_string("VaClientSecret"),
            description="ESP User Voice Assistant Client Secret [visibility:private]"
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
            description="Admin OAuth2 Token URL"
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
            string_value=esp_user_api_url,
            description="ESP User API Gateway URL"
        )

        create_ssm_string_parameter(
            self, "EspUserIssuerParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_ISSUER'],
            string_value=self.discovery_storage.issuer,
            description="ESP User Issuer"
        )

        create_ssm_string_parameter(
            self, "EspUserClientIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_CLIENT_ID'],
            string_value="user-pool-client",
            description="ESP User Client ID"
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
            self, "EspVaClientIdParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_VA_CLIENT_ID'],
            string_value="va-client",
            description="ESP VA Client ID"
        )

        create_ssm_string_parameter(
            self, "EspVaClientSecretParameter",
            parameter_name=USER_SSM_PARAMETERS['ESP_VA_CLIENT_SECRET'],
            string_value=self.common_base_resources.user_tables.seed_oauth_clients_cr.get_att_string("VaClientSecret"),
            description="ESP VA Client Secret"
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
            # Preserve an operator's enable/disable choice across redeploys; default to enabled only
            # on first create. Connection config below is still refreshed on every reseed.
            table.put_item(Item={
                'provider_name': 'cognito',
                'type': 'oidc',
                'enabled': existing.get('enabled', True) if existing else True,
                'display_name': 'Cognito',
                'issuer': os.environ['COGNITO_ISSUER'],
                'client_id': os.environ['COGNITO_CLIENT_ID'],
                'scopes': 'openid profile email phone aws.cognito.signin.user.admin',
                'client_secret': os.environ['COGNITO_CLIENT_SECRET'],
                'password_grant': True,
                'token_endpoint_auth': 'client_secret_basic',
                'attribute_mapping': {
                    'external_sub': 'sub',
                    'email': 'email',
                    'email_verified': 'email_verified',
                    'phone_number': 'phone_number',
                    'name': 'name',
                    'locale': 'locale',
                    'picture': 'picture',
                },
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
                "SeedVersion": "6",
            },
        )
        cr.node.add_dependency(self.gsi_readiness)
