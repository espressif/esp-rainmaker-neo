# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import os

from aws_cdk import (
    Stack,
    Fn,
    CfnCondition,
    aws_cognito as cognito,
    aws_apigateway as apigateway,
    aws_iam as iam,
    CfnOutput,
    CfnParameter,
    CustomResource,
    custom_resources as cr,
    aws_iot as iot,
    aws_lambda as lambda_,
    aws_ssm as ssm,
    Duration
)

from constructs import Construct
from app_common import CommonResources, create_rest_api, create_ssm_string_parameter, stable_logical_id, create_iot_role_alias, discover_api_custom_domain, discover_iot_custom_domain
from arn_utils import get_s3_bucket_resolved_name, get_s3_bucket_arn, get_s3_object_arn, get_kvs_channel_arn, get_api_gateway_invoke_arn
from src.rmneo.handlers.user.base import UserBase
from src.rmneo.handlers.group.base import GroupBase
from src.rmneo.handlers.file.base import FileBase
from src.rmneo.handlers.node.base import NodeBase
from src.rmneo.handlers.nodeadmin.base import NodeAdminBase
from gsi_infra import GsiInfraCore, ManagedTable, GsiReadinessGate
from src.rmneo.handlers.timeseries.base import ServiceBase
from src.rmneo.handlers.notification.base import NotificationBase
from src.alexa.handlers.base import AlexaSkillBase
from src.rmneo.handlers.gva.base import GVAActionBase
from src.rmneo.handlers.integration.base import IntegrationBase
from src.rmneo.handlers.admin.admin_config.base import AdminConfigBase
from src.rmneo.handlers.hello_world.base import HelloWorldBase
from src.rmneo.stacks.base_res_constants import IOT_RESOURCES, SSM_PARAMETERS, S3_BUCKETS
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS

# Placeholder URL used during CDK deployment to satisfy validation requirements
# This must match PlaceholderCallbackURL in test/testutil/cognito_utils.go
PLACEHOLDER_CALLBACK_URL = "https://placeholder.example.com/callback"



def allow(actions, resources):
    return {"Effect": "Allow", "Action": actions, "Resource": resources}

# XXX Security Review
# This is the role assigned to every user authenticated via the identity pool.
# It cannot reach AWS IoT directly — sts:AssumeRole is explicitly denied below,
# so the only path to IoT/S3/KVS access is the assume_role Lambda's own execution
# role narrowing IoTUserRole via an inline session policy (see AssumeRoleAPI).
class CreateDeviceUsersRole(Construct):
    def __init__(self, scope: Construct, id: str, identity_pool: str, iot_user_role: iam.Role, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        device_users_role_name = "cognito-identity-role"
        device_users_role = iam.Role(
            self, "DeviceUsersRole",
            role_name=f"rmng-{device_users_role_name}-{Stack.of(self).region}",
            assumed_by=
                iam.FederatedPrincipal(
                    "cognito-identity.amazonaws.com",
                    conditions={
                        "StringEquals": {
                            "cognito-identity.amazonaws.com:aud": identity_pool
                        },
                        "ForAnyValue:StringLike": {
                            "cognito-identity.amazonaws.com:amr": "authenticated"
                        }
                    },
                    assume_role_action="sts:AssumeRoleWithWebIdentity"
                )
        )
        device_users_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", device_users_role_name))

        self.device_users_role = device_users_role

        # Add policy to the authenticated role
        device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=[
                "cognito-identity:GetCredentialsForIdentity"
            ],
            resources=["*"]
        ))

        # Allow DeviceUsersRole to Tag Session
        device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["sts:TagSession"],
            resources=[iot_user_role.role_arn]
        ))

        # execute-api:Invoke is granted later, in RMNGBaseStack, once RMBaseApi
        # exists — scoped to that one API Gateway rather than "*".

        device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.DENY,
            actions=["sts:AssumeRole"],
            resources=["*"],
        ))

class CreateAdminDeviceUsersRole(Construct):
    """Role for admin users authenticated via the identity pool.
    Includes all DeviceUsersRole permissions plus IoT management capabilities like ListThings."""
    def __init__(self, scope: Construct, id: str, identity_pool: str, iot_user_role: iam.Role, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        files_bucket = get_s3_bucket_resolved_name(S3_BUCKETS["FILES_BUCKET_NAME"], region, stack_prefix=common_resources.prefix)

        admin_device_users_role_name = "admin-cognito-identity-role"
        admin_device_users_role = iam.Role(
            self, "AdminDeviceUsersRole",
            role_name=f"rmng-{admin_device_users_role_name}-{Stack.of(self).region}",
            assumed_by=
                iam.FederatedPrincipal(
                    "cognito-identity.amazonaws.com",
                    conditions={
                        "StringEquals": {
                            "cognito-identity.amazonaws.com:aud": identity_pool
                        },
                        "ForAnyValue:StringLike": {
                            "cognito-identity.amazonaws.com:amr": "authenticated"
                        }
                    },
                    assume_role_action="sts:AssumeRoleWithWebIdentity"
                )
        )
        admin_device_users_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", admin_device_users_role_name))

        self.admin_device_users_role = admin_device_users_role

        # Same permissions as DeviceUsersRole
        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["cognito-identity:GetCredentialsForIdentity"],
            resources=["*"]
        ))

        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["sts:TagSession"],
            resources=[iot_user_role.role_arn]
        ))

        # execute-api:Invoke is granted later, in RMNGBaseStack, once RMBaseApi
        # exists — scoped to that one API Gateway rather than "*".

        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.DENY,
            actions=["sts:AssumeRole"],
            resources=["*"],
        ))

        # Admin-specific: IoT management permissions
        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=[
                "iot:ListThings",
                "iot:DescribeThing",
                "iot:SearchIndex",
                "iot:ListThingGroups",
                "iot:CreateThingGroup",
                "iot:DeleteThingGroup",
                "iot:CreateDynamicThingGroup",
                "iot:DeleteDynamicThingGroup",
                "iot:UpdateDynamicThingGroup",
                "iot:DescribeThingGroup",
                "iot:ListThingsInThingGroup",
                "iot:AddThingToThingGroup",
                "iot:RemoveThingFromThingGroup",
                "iot:UpdateThingGroup",
                "iot:GetBucketsAggregation",
                "iot:ListJobs",
                "iot:ListJobExecutionsForThing",
                "iot:ListJobExecutionsForJob",
                "iot:DescribeJobExecution",
                "iot:CreateJob",
                "iot:DescribeJob",
                "iot:CancelJob",
                "iot:DeleteJob",
                "iot:CreateStream",
                "iot:DeleteStream",
            ],
            resources=["*"]
        ))

        # Admin-specific: list deployed CloudFormation stacks so the dashboard can detect which optional modules are present and surface their features only when their stack is actually deployed. 
        # ListStacks does not support resource-level scoping, so resources must be "*".
        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["cloudformation:ListStacks"],
            resources=["*"]
        ))

        # Admin-specific: S3 permissions for firmware upload and node registration CSV download
        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:PutObject", "s3:GetObject", "s3:ListBucket", "s3:PutObjectTagging", "s3:GetObjectTagging"],
            resources=[
                f"arn:aws:s3:::{files_bucket}",
                f"arn:aws:s3:::{files_bucket}/ota/*",
                f"arn:aws:s3:::{files_bucket}/system/node_certs/*",
            ]
        ))

        # OTA service role that AWS IoT assumes to access S3 firmware and create jobs
        ota_service_role_name = "ota-service-role"
        ota_service_role = iam.Role(
            self, "OtaServiceRole",
            role_name=f"rmng-{ota_service_role_name}-{Stack.of(self).region}",
            assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
        )
        ota_service_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", ota_service_role_name))

        ota_service_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:GetObject", "s3:GetObjectVersion"],
            resources=[f"arn:aws:s3:::{files_bucket}/ota/*"],
        ))

        ota_service_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=[
                "iot:CreateJob",
                "iot:DescribeJob",
                "iot:DeleteJob",
                "iot:GetOTAUpdate",
                "iot:CreateStream",
                "iot:DescribeStream",
                "iot:DeleteStream",
            ],
            resources=["*"],
        ))

        ota_service_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["iam:PassRole"],
            resources=[ota_service_role.role_arn],
        ))

        self.ota_service_role = ota_service_role

        # Admin-specific: PassRole for the OTA service role
        admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["iam:PassRole"],
            resources=[ota_service_role.role_arn],
        ))


class CreateIdentityPool(Construct):
    def __init__(self, scope: Construct, id: str, iot_user_role: iam.Role, common_resources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # IAM OIDC identity provider for the ESP User issuer. End users present an
        # RS256 access token from this issuer; the identity pool federates it to the
        # default authenticated role (DeviceUsersRole). ClientIDList is the set of
        # OIDC client_ids (token `aud`) end users authenticate with.
        # thumbprint_list is required by the API but ignored for the OIDC providers
        # STS trusts via its own CA store; the standard root-CA thumbprint is used.
        # Only the first-party client is federated. esp_mcp_client_id and esp_user_va_client_id are
        # deliberately NOT here: both audiences are delegated to third parties (MCP clients, Alexa
        # and Google), and federating an audience is what makes its tokens exchangeable for the role
        # above. Neither integration calls /v1/user/credentials, so neither needs federating.
        esp_user_oidc_provider = iam.CfnOIDCProvider(
            self, "EspUserOidcProvider",
            url=common_resources.esp_user_issuer,
            client_id_list=[
                common_resources.esp_user_client_id,
            ],
            thumbprint_list=["9e99a48a9960b14926bb7f3b02e22da2b0ab7280"],
        )
        esp_user_oidc_provider.override_logical_id(
            stable_logical_id("IAMOIDCProvider", "espuser-issuer"))
        # url and both client ids are Fn::If intrinsics (CfnParameter override vs SSM lookup), so
        # synth's CFN template validator cannot measure them and emits a spurious F3033
        # "length 0 is below minimum 1". They resolve to non-empty strings at deploy time. The
        # rule is plugin-sourced, so Annotations.acknowledge_warning cannot silence it.

        # Role mapping below: only the admin Cognito pool has one, and it exists solely to
        # promote a `custom:super_admin == true` identity to AdminDeviceUsersRole. The OIDC
        # provider has no role mapping — every federated end user lands on the pool's
        # default `authenticated` role (DeviceUsersRole), regardless of the token's `aud`.

        # Create Cognito Identity Pool
        identity_pool = cognito.CfnIdentityPool(
            self, "IdentityPool",
            identity_pool_name="rmng-identity-pool",
            allow_unauthenticated_identities=False,
            open_id_connect_provider_arns=[esp_user_oidc_provider.attr_arn],
            cognito_identity_providers=[
                cognito.CfnIdentityPool.CognitoIdentityProviderProperty(
                    client_id=common_resources.esp_admin_user_pool_client_id,
                    provider_name=f"cognito-idp.{scope.region}.amazonaws.com/{common_resources.esp_admin_user_pool_id}",
                ),
            ]
        )
        identity_pool.override_logical_id(
            stable_logical_id("IdPool", "identity-pool"))

        # Default role for regular users authenticated via the identity pool
        device_users_role = CreateDeviceUsersRole(self, "DeviceUsersRole", identity_pool.ref, iot_user_role)
        # Admin role with additional IoT management permissions
        admin_device_users_role = CreateAdminDeviceUsersRole(self, "AdminDeviceUsersRole", identity_pool.ref, iot_user_role, common_resources)

        # Pin IdentityPoolRoleAttachment — SetIdentityPoolRoles + Delete on
        # rename clears all roles from the pool silently.
        identity_pool_role_attachment = cognito.CfnIdentityPoolRoleAttachment(
            self, "IdentityPoolRoleAttachment",
            identity_pool_id=identity_pool.ref,
            roles={
                "authenticated": device_users_role.device_users_role.role_arn
            },
            role_mappings={
                "adminPoolMapping": cognito.CfnIdentityPoolRoleAttachment.RoleMappingProperty(
                    type="Rules",
                    identity_provider=f"cognito-idp.{scope.region}.amazonaws.com/{common_resources.esp_admin_user_pool_id}:{common_resources.esp_admin_user_pool_client_id}",
                    ambiguous_role_resolution="AuthenticatedRole",
                    rules_configuration=cognito.CfnIdentityPoolRoleAttachment.RulesConfigurationTypeProperty(
                        rules=[
                            cognito.CfnIdentityPoolRoleAttachment.MappingRuleProperty(
                                claim="custom:super_admin",
                                match_type="Equals",
                                value="true",
                                role_arn=admin_device_users_role.admin_device_users_role.role_arn
                            )
                        ]
                    )
                )
            }
        )
        identity_pool_role_attachment.override_logical_id(
            stable_logical_id("CognitoIdPoolRoleAttach", "identity-pool"))

        # Store references to created resources
        self.identity_pool = identity_pool
        self.device_users_role = device_users_role.device_users_role
        self.admin_device_users_role = admin_device_users_role.admin_device_users_role
        self.ota_service_role = admin_device_users_role.ota_service_role

class RMNGBaseStack(Stack):
    """Infrastructure/Base stack containing storage, networking, and foundational AWS resources"""

    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # CloudFormation parameters for ESP User values (for direct CFN deployment)
        # These parameters allow providing values via CloudFormation parameters when deploying directly
        esp_user_issuer = CfnParameter(
            self, "EspUserIssuer",
            type="String",
            description="ESP User Issuer (leave empty to read from SSM /espuser/base/user-issuer)",
            default=""
        )
        esp_user_client_id_param = CfnParameter(
            self, "EspUserClientId",
            type="String",
            description="ESP User Pool Client ID (leave empty to read from SSM /espuser/base/user-client-id)",
            default=""
        )
        esp_mcp_client_id_param = CfnParameter(
            self, "EspMcpClientId",
            type="String",
            description="ESP User MCP OAuth Client ID (leave empty to read from SSM /espuser/base/mcp-client-id)",
            default=""
        )
        esp_mcp_client_secret_param = CfnParameter(
            self, "EspMcpClientSecret",
            type="String",
            description="ESP User MCP OAuth Client Secret (leave empty to read from SSM /espuser/base/mcp-client-secret)",
            default="",
            no_echo=True
        )
        esp_user_va_client_id_param = CfnParameter(
            self, "EspUserVaClientId",
            type="String",
            description="ESP User VA Client ID (leave empty to read from SSM /espuser/base/va-client-id)",
            default=""
        )
        esp_user_va_client_secret_param = CfnParameter(
            self, "EspUserVaClientSecret",
            type="String",
            description="ESP User VA Client Secret (leave empty to read from SSM /espuser/base/va-client-secret)",
            default="",
            no_echo=True
        )
        esp_admin_user_pool_id_param = CfnParameter(
            self, "EspAdminUserPoolId",
            type="String",
            description="ESP Admin User Pool ID (leave empty to read from SSM /espuser/base/admin-user-pool-id)",
            default=""
        )
        esp_admin_user_pool_client_id_param = CfnParameter(
            self, "EspAdminUserPoolClientId",
            type="String",
            description="ESP Admin User Pool Client ID (leave empty to read from SSM /espuser/base/admin-user-pool-client-id)",
            default=""
        )
        esp_user_jwks_param = CfnParameter(
            self, "EspUserJwks",
            type="String",
            description="ESP User Pool JWKS SSM Parameter Name (leave empty to read from SSM /espuser/base/user-jwks-json)",
            default=""
        )
        esp_admin_user_pool_jwks_param = CfnParameter(
            self, "EspAdminUserPoolJwks",
            type="String",
            description="ESP Admin User Pool JWKS SSM Parameter Name (leave empty to read from SSM /espuser/base/admin-user-pool-jwks-json)",
            default=""
        )

        # ESP values: read from SSM (written by espuser-base) by default,
        # but allow CfnParameter override when a non-empty value is provided.
        esp_param_overrides = [
            ("HasEspUserIssuer", esp_user_issuer, USER_SSM_PARAMETERS['ESP_USER_ISSUER'], "esp_user_issuer"),
            ("HasEspUserClientId", esp_user_client_id_param, USER_SSM_PARAMETERS['ESP_USER_CLIENT_ID'], "esp_user_client_id"),
            ("HasEspMcpClientId", esp_mcp_client_id_param, USER_SSM_PARAMETERS['ESP_MCP_CLIENT_ID'], "esp_mcp_client_id"),
            ("HasEspMcpClientSecret", esp_mcp_client_secret_param, USER_SSM_PARAMETERS['ESP_MCP_CLIENT_SECRET'], "esp_mcp_client_secret"),
            ("HasEspUserVaClientId", esp_user_va_client_id_param, USER_SSM_PARAMETERS['ESP_VA_CLIENT_ID'], "esp_user_va_client_id"),
            ("HasEspUserVaClientSecret", esp_user_va_client_secret_param, USER_SSM_PARAMETERS['ESP_VA_CLIENT_SECRET'], "esp_user_va_client_secret"),
            ("HasEspAdminUserPoolId", esp_admin_user_pool_id_param, USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID'], "esp_admin_user_pool_id"),
            ("HasEspAdminUserPoolClientId", esp_admin_user_pool_client_id_param, USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID'], "esp_admin_user_pool_client_id"),
        ]

        for condition_id, cfn_param, ssm_path, attr_name in esp_param_overrides:
            condition = CfnCondition(self, condition_id,
                expression=Fn.condition_not(Fn.condition_equals(cfn_param.value_as_string, ""))
            )
            ssm_value = ssm.StringParameter.value_for_string_parameter(self, ssm_path)
            setattr(common_resources, attr_name,
                Fn.condition_if(condition.logical_id, cfn_param.value_as_string, ssm_value).to_string()
            )

        # JWKS parameters store an SSM parameter PATH (not the JWKS content itself).
        # Lambdas use this path at runtime to fetch JWKS via ssm:GetParameter.
        # The default is the esp-user SSM path; the installer can override via CfnParameter.
        for condition_id, cfn_param, default_path, attr_name in [
            ("HasEspUserJwks", esp_user_jwks_param, USER_SSM_PARAMETERS['ESP_USER_JWKS'], "esp_user_jwks"),
            ("HasEspAdminUserPoolJwks", esp_admin_user_pool_jwks_param, USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'], "esp_admin_user_pool_jwks"),
        ]:
            condition = CfnCondition(self, condition_id,
                expression=Fn.condition_not(Fn.condition_equals(cfn_param.value_as_string, ""))
            )
            setattr(common_resources, attr_name,
                Fn.condition_if(condition.logical_id, cfn_param.value_as_string, default_path).to_string()
            )

        # Create IoTUser role
        iot_user_role = iam.Role(
            self, "IoTUserRole",
            assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
            role_name=f"{IOT_RESOURCES['NODE_ROLE_NAME']}-{self.region}"
        )
        iot_user_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "iot-node-role"))

        # Apply the specific IoT policy
        iot_user_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["cognito-identity:GetCredentialsForIdentity"],
            resources=[f"arn:aws:cognito-identity:{self.region}::identitypool/*:identity/${{cognito-identity.amazonaws.com:sub}}"]
        ))

        iot_user_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["iot:Connect", "iot:Publish", "iot:Receive", "iot:Subscribe"],
            resources=["*"]
        ))

        # S3 permissions for user file access — the assume-role Lambda's session
        # policy further restricts to specific node prefixes within node-data/
        files_bucket_name = get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], self.region, stack_prefix=common_resources.prefix)

        iot_user_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:ListBucket"],
            resources=[get_s3_bucket_arn(files_bucket_name)],
            conditions={
                "StringLike": {
                    "s3:prefix": "node-data/*"
                }
            }
        ))

        iot_user_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:GetObject", "s3:DeleteObject"],
            resources=[get_s3_object_arn(files_bucket_name, "node-data/*")]
        ))

        # KVS viewer permissions — broad resource, restricted per-user by session policy in assume-role Lambda
        iot_user_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=[
                "kinesisvideo:ConnectAsViewer",
                "kinesisvideo:GetSignalingChannelEndpoint",
                "kinesisvideo:DescribeSignalingChannel",
                "kinesisvideo:GetIceServerConfig"
            ],
            resources=[get_kvs_channel_arn("rmng-v1-*", self.region)]
        ))

        # Create Device File Role for IoT Credential Provider S3 access

        device_file_role = iam.Role(
            self, "DeviceFileRole",
            assumed_by=iam.ServicePrincipal("credentials.iot.amazonaws.com"),
            role_name=f"{IOT_RESOURCES['DEVICE_FILE_ROLE_NAME']}-{self.region}"
        )
        device_file_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "node-file-role"))

        device_file_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:PutObject", "s3:GetObject", "s3:DeleteObject"],
            resources=[get_s3_object_arn(files_bucket_name, "node-data/${credentials-iot:ThingName}/*")]
        ))

        device_file_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["s3:ListBucket"],
            resources=[get_s3_bucket_arn(files_bucket_name)],
            conditions={
                "StringLike": {
                    "s3:prefix": "node-data/${credentials-iot:ThingName}/*"
                }
            }
        ))

        self.device_file_role = device_file_role

        # Create IoT Role Alias for Device File Role — maps device certificate auth to IAM role
        device_file_role_alias = create_iot_role_alias(
            self, "DeviceFileRoleAlias",
            role_alias=IOT_RESOURCES['DEVICE_FILE_ROLE_ALIAS'],
            role_arn=device_file_role.role_arn,
        )

        id_pool = CreateIdentityPool(self, "IdentityPool", iot_user_role=iot_user_role, common_resources=common_resources)

        # Don't add any assume role policy statements here - we'll add them in AssumeRoleAPI

        CfnOutput(self, "UserIssuer", value=common_resources.esp_user_issuer)
        CfnOutput(self, "UserClientId", value=common_resources.esp_user_client_id)
        CfnOutput(self, "IdentityPoolId", value=id_pool.identity_pool.ref)

        self.identity_pool = id_pool
        self.iot_user_role = iot_user_role

        # API Gateway (base infrastructure)
        self.api_gateway_cfn = create_rest_api(
            self, "RMBaseApi",
            rest_api_name="RMBaseApi",
            description="RMNG Base API Gateway",
        )

        # execute-api:Invoke, scoped to RMBaseApi only. DeviceUsersRole/
        # AdminDeviceUsersRole never need to invoke any other API Gateway
        rmng_api_invoke_arn = get_api_gateway_invoke_arn(self.api_gateway_cfn.rest_api_id, self.region)
        id_pool.device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["execute-api:Invoke"],
            resources=[rmng_api_invoke_arn],
        ))
        id_pool.admin_device_users_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["execute-api:Invoke"],
            resources=[rmng_api_invoke_arn],
        ))

        # Create admin resource using CFn
        self.admin_resource_cfn = apigateway.CfnResource(
            self, "AdminResource",
            rest_api_id=self.api_gateway_cfn.rest_api_id,
            parent_id=self.api_gateway_cfn.root.resource_id,
            path_part="admin"
        )

        # Create Cognito Authorizer using CFn to avoid dependency issues
        self.cognito_authorizer_cfn = apigateway.CfnAuthorizer(
            self, "CognitoAuthorizer",
            rest_api_id=self.api_gateway_cfn.rest_api_id,
            name="CognitoAuthorizer",
            type="COGNITO_USER_POOLS",
            provider_arns=[
                f"arn:aws:cognito-idp:{self.region}:{self.account}:userpool/{common_resources.esp_admin_user_pool_id}"
            ],
            identity_source="method.request.header.Authorization"
        )

        # Default thing policy for shadow + group control access.
        #
        # The document lives in rmneo/node/node_policy.json, only `__REGION__` and
        # `__ACCOUNT__` are substituted at synth time.
        policy_path = os.path.join(
            os.path.dirname(os.path.dirname(__file__)), "node", "node_policy.json"
        )
        with open(policy_path) as policy_file:
            policy_template = policy_file.read()
        policy_rendered = (policy_template
                           .replace("__REGION__", self.region)
                           .replace("__ACCOUNT__", self.account))
        default_thing_policy = iot.CfnPolicy(
            self, "DefaultThingPolicy",
            policy_name=IOT_RESOURCES['DEFAULT_THING_POLICY_NAME'],
            policy_document=json.loads(policy_rendered),
        )
        default_thing_policy.override_logical_id(
            stable_logical_id("IoTPolicy", "default-thing-policy"))

        # Separate policy for device file S3 access via IoT Credential Provider.
        # Kept separate from DefaultThingPolicy to stay within the 2048-byte policy size limit.
        # Both policies are attached to device certificates; permissions are unioned.
        device_file_policy = iot.CfnPolicy(
            self, "DeviceFilePolicy",
            policy_name=IOT_RESOURCES['DEVICE_FILE_POLICY_NAME'],
            policy_document={
                "Version": "2012-10-17",
                "Statement": [
                    allow(["iot:AssumeRoleWithCertificate"], [
                        f"arn:aws:iot:{self.region}:{self.account}:rolealias/{IOT_RESOURCES['DEVICE_FILE_ROLE_ALIAS']}"
                    ]),
                ],
            }
        )
        device_file_policy.override_logical_id(
            stable_logical_id("IoTPolicy", "node-file-policy"))

        # Create Device Video Role for IoT Credential Provider KVS access
        device_video_role = iam.Role(
            self, "DeviceVideoRole",
            assumed_by=iam.ServicePrincipal("credentials.iot.amazonaws.com"),
            role_name=f"{IOT_RESOURCES['DEVICE_VIDEO_ROLE_NAME']}-{self.region}"
        )
        device_video_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "node-video-role"))

        device_video_role.add_to_policy(iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=[
                "kinesisvideo:ConnectAsMaster",
                "kinesisvideo:GetSignalingChannelEndpoint",
                "kinesisvideo:DescribeSignalingChannel",
                "kinesisvideo:GetIceServerConfig"
            ],
            resources=[get_kvs_channel_arn("rmng-v1-${credentials-iot:ThingName}/*", self.region)]
        ))

        self.device_video_role = device_video_role

        # Create IoT Role Alias for Device Video Role — maps device certificate auth to IAM role
        device_video_role_alias = create_iot_role_alias(
            self, "DeviceVideoRoleAlias",
            role_alias=IOT_RESOURCES['DEVICE_VIDEO_ROLE_ALIAS'],
            role_arn=device_video_role.role_arn,
        )

        # Separate policy for device KVS access via IoT Credential Provider.
        # Kept separate from DefaultThingPolicy and DeviceFilePolicy to stay within the 2048-byte policy size limit.
        # All policies are attached to device certificates; permissions are unioned.
        device_video_policy = iot.CfnPolicy(
            self, "DeviceVideoPolicy",
            policy_name=IOT_RESOURCES['DEVICE_VIDEO_POLICY_NAME'],
            policy_document={
                "Version": "2012-10-17",
                "Statement": [
                    allow(["iot:AssumeRoleWithCertificate"], [
                        f"arn:aws:iot:{self.region}:{self.account}:rolealias/{IOT_RESOURCES['DEVICE_VIDEO_ROLE_ALIAS']}"
                    ]),
                ],
            }
        )
        device_video_policy.override_logical_id(
            stable_logical_id("IoTPolicy", "node-video-policy"))

        # Role aliases are emitted as a comma-separated list so new alias
        # versions can be appended over time without breaking the node, which
        # splits on the comma. Today the list holds only the current v1 alias.
        CfnOutput(
            self, "NodeVideoRoleAliases",
            description="IoT role alias names for device KVS access via Credential Provider (comma-separated, newest last)",
            value=",".join([IOT_RESOURCES['DEVICE_VIDEO_ROLE_ALIAS']])
        )

        # Output the default thing policy ARN
        CfnOutput(
            self, "DefaultThingPolicyName",
            description="The name of the default thing policy for shadow access",
            value=default_thing_policy.policy_name
        )

        # Use AWSCustomResource to get the IoT ATS endpoint
        iot_endpoint = cr.AwsCustomResource(
            self, "IoTEndpoint",
            on_create=cr.AwsSdkCall(
                service="Iot",
                action="describeEndpoint",
                parameters={
                    "endpointType": "iot:Data-ATS"
                },
                physical_resource_id=cr.PhysicalResourceId.of("IoTEndpoint")
            ),
            policy=cr.AwsCustomResourcePolicy.from_sdk_calls(
                resources=cr.AwsCustomResourcePolicy.ANY_RESOURCE
            ),
        )
        iot_endpoint.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "iot-endpoint"))

        # Create Custom Resource for Fleet Indexing
        fleet_indexing_params = {
            "thingIndexingConfiguration": {
                "thingIndexingMode": "REGISTRY",
                "namedShadowIndexingMode": "ON",
                "filter": {
                    "namedShadowNames": [ "iparams" ]
                },
                # Custom fields are required for GetBucketsAggregation on shadow fields.
                # SearchIndex works without these, but aggregation does not.
                # AWS IoT hard limit: max 5 custom fields. Chosen are fields with bounded
                # value sets that benefit most from suggestions (device identity).
                # Free-form fields (room, location, created_by) are searchable but won't
                # show suggestions.
                "customFields": [
                    { "name": "shadow.name.iparams.reported.data.device.t.type",  "type": "String" },
                    { "name": "shadow.name.iparams.reported.data.device.t.model", "type": "String" },
                    { "name": "shadow.name.iparams.reported.data.device.t.fw_version", "type": "String" },
                ]
            },
            "thingGroupIndexingConfiguration": {
                "thingGroupIndexingMode": "ON"
            }
        }
        fleet_indexing = cr.AwsCustomResource(
            self, "fleet-indexing-resource",
            on_create=cr.AwsSdkCall(
                service="Iot",
                action="UpdateIndexingConfigurationCommand",
                parameters=fleet_indexing_params,
                physical_resource_id=cr.PhysicalResourceId.of("fleet-indexing-resource")
            ),
            on_update=cr.AwsSdkCall(
                service="Iot",
                action="UpdateIndexingConfigurationCommand",
                parameters=fleet_indexing_params,
                physical_resource_id=cr.PhysicalResourceId.of("fleet-indexing-resource")
            ),
            policy=cr.AwsCustomResourcePolicy.from_sdk_calls(
                resources=cr.AwsCustomResourcePolicy.ANY_RESOURCE
            ),
        )
        fleet_indexing.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "fleet-indexing"))

        # Store shared resources in SSM Parameter Store for cross-stack references - eliminates cross-stack dependencies
        create_ssm_string_parameter(
            self, "ApiGatewayIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_ID'],
            string_value=self.api_gateway_cfn.rest_api_id,
            description="API Gateway ID for RMNG"
        )

        create_ssm_string_parameter(
            self, "ApiGatewayRootResourceIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_ROOT_RESOURCE_ID'],
            string_value=self.api_gateway_cfn.root.resource_id,
            description="API Gateway Root Resource ID"
        )

        create_ssm_string_parameter(
            self, "AdminResourceIdParameter",
            parameter_name=SSM_PARAMETERS['ADMIN_RESOURCE_ID'],
            string_value=self.admin_resource_cfn.ref,
            description="API Gateway /admin Resource ID"
        )

        create_ssm_string_parameter(
            self, "CognitoAuthorizerIdParameter",
            parameter_name=SSM_PARAMETERS['COGNITO_AUTHORIZER_ID'],
            string_value=self.cognito_authorizer_cfn.ref,
            description="Cognito Authorizer ID"
        )

        create_ssm_string_parameter(
            self, "IdentityPoolIdParameter",
            parameter_name=SSM_PARAMETERS['IDENTITY_POOL_ID'],
            string_value=self.identity_pool.identity_pool.ref,
            description="Cognito Identity Pool ID"
        )

        create_ssm_string_parameter(
            self, "IotDataAtsEndpointParameter",
            parameter_name=SSM_PARAMETERS['IOT_DATA_ATS_ENDPOINT'],
            string_value=iot_endpoint.get_response_field("endpointAddress"),
            description="AWS-assigned IoT Core Data-ATS endpoint hostname. Never a custom domain; see the IoTEndpointUrl output for the resolved device-facing hostname.",
        )

        create_ssm_string_parameter(
            self, "OtaServiceRoleArnParameter",
            parameter_name=SSM_PARAMETERS['OTA_SERVICE_ROLE_ARN'],
            string_value=self.identity_pool.ota_service_role.role_arn,
            description="IAM role ARN assumed by AWS IoT for OTA updates (S3 firmware access)",
        )

        create_ssm_string_parameter(
            self, "EspUserIssuerParameter",
            parameter_name=SSM_PARAMETERS['ESP_USER_ISSUER'],
            string_value=common_resources.esp_user_issuer,
            description="ESP User Issuer"
        )
        create_ssm_string_parameter(
            self, "EspUserClientIdParameter",
            parameter_name=SSM_PARAMETERS['ESP_USER_CLIENT_ID'],
            string_value=common_resources.esp_user_client_id,
            description="ESP User Client ID"
        )
        create_ssm_string_parameter(
            self, "EspUserVaClientIdParameter",
            parameter_name=SSM_PARAMETERS['ESP_USER_VA_CLIENT_ID'],
            string_value=common_resources.esp_user_va_client_id,
            description="ESP User VA Client ID"
        )
        create_ssm_string_parameter(
            self, "EspUserVaClientSecretParameter",
            parameter_name=SSM_PARAMETERS['ESP_USER_VA_CLIENT_SECRET'],
            string_value=common_resources.esp_user_va_client_secret,
            description="ESP User VA Client Secret"
        )
        create_ssm_string_parameter(
            self, "EspMcpClientIdParameter",
            parameter_name=SSM_PARAMETERS['ESP_MCP_CLIENT_ID'],
            string_value=common_resources.esp_mcp_client_id,
            description="ESP User MCP OAuth Client ID"
        )
        create_ssm_string_parameter(
            self, "EspMcpClientSecretParameter",
            parameter_name=SSM_PARAMETERS['ESP_MCP_CLIENT_SECRET'],
            string_value=common_resources.esp_mcp_client_secret,
            description="ESP User MCP OAuth Client Secret"
        )
        create_ssm_string_parameter(
            self, "EspAdminUserPoolIdParameter",
            parameter_name=SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID'],
            string_value=common_resources.esp_admin_user_pool_id,
            description="ESP Admin User Pool ID"
        )
        create_ssm_string_parameter(
            self, "EspAdminUserPoolClientIdParameter",
            parameter_name=SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID'],
            string_value=common_resources.esp_admin_user_pool_client_id,
            description="ESP Admin User Pool Client ID"
        )
        create_ssm_string_parameter(
            self, "EspUserJwksParameter",
            parameter_name=SSM_PARAMETERS['ESP_USER_JWKS'],
            string_value=common_resources.esp_user_jwks,
            description="ESP User JWKS SSM Parameter Name"
        )
        create_ssm_string_parameter(
            self, "EspAdminUserPoolJwksParameter",
            parameter_name=SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'],
            string_value=common_resources.esp_admin_user_pool_jwks,
            description="ESP Admin User Pool JWKS SSM Parameter Name"
        )

        # Create CommonResources for base stack. No claiming_* fields: assisted
        # claiming is its own stack group and rmng-base neither reads nor
        # threads that config.
        self.common_resources = CommonResources(
            api_gateway_id=self.api_gateway_cfn.rest_api_id,
            api_gateway_root_resource_id=self.api_gateway_cfn.root.resource_id,
            admin_api_resource_id=self.admin_resource_cfn.ref,
            cognito_authorizer_id=self.cognito_authorizer_cfn.ref,
            prefix="rmng-",
        )

        self.common_resources.identity_pool_id = self.identity_pool.identity_pool.ref

        self.file_base = FileBase(self, "FileBase", self.common_resources)

        create_ssm_string_parameter(
            self, "FilesBucketNameParameter",
            parameter_name=SSM_PARAMETERS['FILES_BUCKET_NAME'],
            string_value=self.file_base.files_bucket.bucket_name,
            description="RMNG files S3 bucket name (firmware OTA prefix, uploads)",
        )

        # Fire-and-forget GSI creation: each Custom::GsiIndex returns as soon as
        # the orchestrator execution is started, so no per-GSI CloudFormation ~1h
        # cap applies (a table with several GSIs is built serially by DynamoDB and
        # can exceed that cap on a slow account). Stack-level readiness is enforced
        # by GsiReadinessGate below (a native WaitCondition, up to 12h).
        self.gsi_infra = GsiInfraCore(
            self, "GsiInfra",
            common_resources=self.common_resources,
        )
        # Expose the GSI-management infra ARNs so separately-deployed optional
        # stacks can add GSIs to core tables (e.g. via ManagedTable's GSI custom
        # resource) without owning the tables.
        create_ssm_string_parameter(
            self, "GsiTriggerLambdaArnParameter",
            parameter_name=SSM_PARAMETERS['GSI_TRIGGER_LAMBDA_ARN'],
            string_value=self.common_resources.gsi_trigger_lambda_arn,
            description="GSI trigger Lambda ARN (for optional stacks adding GSIs to core tables)"
        )
        create_ssm_string_parameter(
            self, "GsiStateMachineArnParameter",
            parameter_name=SSM_PARAMETERS['GSI_STATE_MACHINE_ARN'],
            string_value=self.common_resources.gsi_state_machine_arn,
            description="GSI state machine ARN (for optional stacks adding GSIs to core tables)"
        )
        self.user_base = UserBase(self, "UserBase", self.common_resources)
        self.group_base = GroupBase(self, "GroupBase", self.common_resources)
        self.node_base = NodeBase(self, "NodeBase", self.common_resources)
        self.nodeadmin_base = NodeAdminBase(self, "NodeAdminBase", self.common_resources)
        # Assisted-claiming resources live in the separate `claim` stack group
        # (rmng-claim-base); rmng-base no longer creates them.
        self.service_base = ServiceBase(self, "ServiceBase", self.common_resources)
        self.notification_base = NotificationBase(self, "NotificationBase", self.common_resources)
        self.alexa_skill_base = AlexaSkillBase(self, "AlexaSkillBase", self.common_resources)
        self.gva_action_base = GVAActionBase(self, "GVAActionBase", self.common_resources)
        self.integration_base = IntegrationBase(self, "IntegrationBase", self.common_resources)
        self.admin_config_base = AdminConfigBase(self, "AdminConfigBase", self.common_resources)
        self.hello_world_base = HelloWorldBase(self, "HelloWorldBase", self.common_resources)

        # Stack-level barrier: hold deploy completion until every ManagedTable
        # and its GSIs are ACTIVE && !Backfilling — via a native WaitCondition
        # (up to 12h), not a custom resource, so a slow control-plane can't time
        # the deploy out. The construct scans this stack for ManagedTables itself
        # — must be the last construct created.
        self.gsi_readiness = GsiReadinessGate(
            self, "GsiReadiness",
            common_resources=self.common_resources,
        )
        # Explicit dependencies on every *Base construct so CFN orders the
        # readiness gate after each one's resources, regardless of where the
        # auto-scan in GsiReadinessGate picks tables up.
        for base in (
            self.user_base,
            self.group_base,
            self.node_base,
            self.nodeadmin_base,
            self.service_base,
            self.notification_base,
            self.alexa_skill_base,
            self.gva_action_base,
            self.integration_base,
            self.hello_world_base,
        ):
            self.gsi_readiness.node.add_dependency(base)

        api_custom_domain, api_gateway_url, _ = discover_api_custom_domain(
            self, "ApiCustomDomainDiscovery",
            name="api-custom-domain-discovery",
            api_id=self.api_gateway_cfn.rest_api_id,
            api_type="REST",
            default_url=f"https://{self.api_gateway_cfn.rest_api_id}.execute-api.{self.region}.amazonaws.com/prod/",
        )

        create_ssm_string_parameter(
            self, "ApiGatewayUrlParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_URL'],
            string_value=api_gateway_url,
            description="API Gateway base URL (custom domain when configured, else execute-api). No trailing slash.",
        )

        CfnOutput(
            self, "ApiGatewayUrl",
            description="The URL of the API Gateway endpoint",
            value=api_gateway_url
        )

        CfnOutput(
            self, "IoTUserRoleArn",
            description="The ARN of the IoTUser role",
            value=iot_user_role.role_arn
        )

        iot_custom_domain, iot_endpoint_url = discover_iot_custom_domain(
            self, "IoTDataCustomDomainDiscovery",
            name="iot-data-custom-domain-discovery",
            service_type="DATA",
            default_endpoint=iot_endpoint.get_response_field("endpointAddress"),
        )

        CfnOutput(
            self, "IoTEndpointUrl",
            description="The endpoint hostname for AWS IoT (custom domain when configured, else ATS)",
            value=iot_endpoint_url
        )

        CfnOutput(
            self, "StackRegion",
            description="The region where this stack is deployed",
            value=self.region
        )

        CfnOutput(
            self, "StackAccountId",
            description="The account ID where this stack is deployed",
            value=self.account
        )

        # Add outputs
        CfnOutput(self, "VA_ClientId", value=common_resources.esp_user_va_client_id)
        CfnOutput(self, "VA_ClientSecret", value=common_resources.esp_user_va_client_secret, description="ESP User Voice Assistant Client Secret [visibility:private]")
        CfnOutput(self, "AdminUserPoolId", value=common_resources.esp_admin_user_pool_id)
        CfnOutput(self, "AdminUserPoolClientId", value=common_resources.esp_admin_user_pool_client_id)

        CfnOutput(self, "OtaServiceRoleArn", value=id_pool.ota_service_role.role_arn)

        # Use AwsCustomResource to get the IoT Credential Provider endpoint
        iot_credential_provider_endpoint = cr.AwsCustomResource(
            self, "IoTCredentialProviderEndpoint",
            on_create=cr.AwsSdkCall(
                service="Iot",
                action="describeEndpoint",
                parameters={
                    "endpointType": "iot:CredentialProvider"
                },
                physical_resource_id=cr.PhysicalResourceId.of("IoTCredentialProviderEndpoint")
            ),
            policy=cr.AwsCustomResourcePolicy.from_sdk_calls(
                resources=cr.AwsCustomResourcePolicy.ANY_RESOURCE
            ),
        )
        iot_credential_provider_endpoint.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "iot-credential-provider-endpoint"))

        CfnOutput(
            self, "NodeFileRoleAliases",
            description="IoT role alias names for device file S3 access via Credential Provider (comma-separated, newest last)",
            value=",".join([IOT_RESOURCES['DEVICE_FILE_ROLE_ALIAS']])
        )

        # No custom-domain discovery here: IoT rejects createDomainConfiguration for
        # anything but serviceType DATA ("CreateDomainConfiguration only supports DATA
        # Service Type"), so the credential provider always uses its AWS-assigned host.
        CfnOutput(
            self, "CredentialProviderEndpoint",
            description="AWS IoT Credential Provider endpoint for device certificate-based auth",
            value=iot_credential_provider_endpoint.get_response_field("endpointAddress")
        )

        CfnOutput(
            self, "FilesBucketName",
            description="S3 bucket name for device file storage",
            value=self.file_base.files_bucket.bucket_name
        )
        CfnOutput(self, "GsiStateMachineArn", value=self.common_resources.gsi_state_machine_arn)
        CfnOutput(self, "GsiTriggerLambdaArn", value=self.common_resources.gsi_trigger_lambda_arn)
