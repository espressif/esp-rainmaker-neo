# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import os

from aws_cdk import (
    Stack,
    CfnOutput,
    Duration,
    aws_apigateway as apigateway,
    aws_apigatewayv2 as apigwv2,
    aws_ssm as ssm,
    aws_iam as iam,
    custom_resources as cr,
)
from constructs import Construct
from app_common import CommonResources, stable_logical_id, get_or_create_api_resource, create_ssm_string_parameter
from src.rmneo.stacks.base_res_constants import IOT_RESOURCES, SSM_PARAMETERS, TABLE_NAMES
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS, USER_TABLE_NAMES
from arn_utils import get_table_arn, get_index_arn, get_ssm_parameter_arn
from datetime import datetime
from src.rmneo.handlers.hello_world.core import HelloWorldCore
from src.rmneo.handlers.user.core import UserCore
from src.rmneo.handlers.group.core import GroupCore
from src.rmneo.handlers.file.core import FileCore
from src.rmneo.handlers.node.core import NodeCore
from src.rmneo.handlers.nodeadmin.core import NodeAdminCore
from src.rmneo.handlers.timeseries.core import ServiceCore
from src.rmneo.handlers.notification.core import NotificationCore
from src.rmneo.handlers.integration.core import IntegrationCore
from src.rmneo.handlers.admin.core import IotEventModeCore
from src.rmneo.handlers.admin.rmng_admin_creds.stack import AdminCredsCore
from src.mcp.handlers.core import McpOAuthConstruct, McpOAuthConfig

class RMNGCoreStack(Stack):
    """Compute/Core stack containing Lambda functions, API integrations, and other compute resources"""

    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # create common resources for core stack
        common_resources.api_gateway_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ID']
        )
        api_gateway_url = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_URL']
        )
        common_resources.api_gateway_root_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ROOT_RESOURCE_ID']
        )
        common_resources.identity_pool_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['IDENTITY_POOL_ID']
        )

        common_resources.esp_user_issuer = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_ISSUER']
        )
        common_resources.esp_user_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_CLIENT_ID']
        )
        common_resources.esp_mcp_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_MCP_CLIENT_ID']
        )
        common_resources.esp_mcp_client_secret = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_MCP_CLIENT_SECRET']
        )
        common_resources.esp_user_va_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_VA_CLIENT_ID']
        )
        common_resources.esp_admin_user_pool_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID']
        )
        common_resources.esp_admin_user_pool_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID']
        )
        common_resources.esp_user_jwks = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_JWKS']
        )
        common_resources.esp_admin_user_pool_jwks = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS']
        )

        # Create core resources for each service in dependency order
        # File service creates bucket name needed by other services
        self.file_core = FileCore(self, "FileCore", common_resources)

        # User and Group services provide foundational API structures
        self.user_core = UserCore(self, "UserCore", common_resources)

        # Create NodeDataResetLambda early so it can be used by GroupCore
        from src.rmneo.handlers.node.node_reset.stack import NodeDataResetLambda
        self.node_data_reset = NodeDataResetLambda(self, "NodeDataResetLambda", common_resources=common_resources)

        self.group_core = GroupCore(self, "GroupCore", common_resources, node_data_reset_function=self.node_data_reset.function)

        # Node services depend on user/group structures
        self.node_core = NodeCore(self, "NodeCore", common_resources, node_data_reset_function=self.node_data_reset.function)
        self.nodeadmin_core = NodeAdminCore(self, "NodeAdminCore", common_resources)

        # Assisted claiming lives in the separate `claim` stack group
        # (rmng-claim-core); rmng-core no longer creates its Lambda or routes.
        # It attaches its routes to this shared API under /v1, so publish the
        # /v1 resource id for that cross-stack reference (the id + root are
        # already published by rmng-base).
        v1_resource_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        create_ssm_string_parameter(
            self, "ApiGatewayV1ResourceIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_V1_RESOURCE_ID'],
            string_value=v1_resource_id,
            description="Shared API Gateway /v1 resource ID (for cross-stack route attachment)",
        )

        # /v1/admin is created here (shared by this stack's own admin routes —
        # gva/alexa config, iot-event-mode, node admin) and its id published so a
        # separate stack (rmng-claim-core) can attach admin routes under it.
        # Recreating /v1/admin from another stack collides on the shared API, so
        # the id must be referenced, exactly like /v1 above.
        v1_admin_resource_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_resource_id, "admin"
        )
        create_ssm_string_parameter(
            self, "ApiGatewayV1AdminResourceIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_RESOURCE_ID'],
            string_value=v1_admin_resource_id,
            description="Shared API Gateway /v1/admin resource ID (for cross-stack admin route attachment)",
        )

        # /v1/integrations and /v1/admin/integrations are shared parents: the
        # generic integrations API owns them, and the voice-assistant stacks
        # attach their own children (alexa, gva) underneath. Publish both ids so
        # those stacks reference them instead of recreating them, which would
        # collide on the shared API.
        v1_integrations_resource_id = get_or_create_api_resource(
            self, "V1IntegrationsResource", common_resources,
            v1_resource_id, "integrations"
        )
        create_ssm_string_parameter(
            self, "ApiGatewayV1IntegrationsResourceIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_V1_INTEGRATIONS_RESOURCE_ID'],
            string_value=v1_integrations_resource_id,
            description="Shared API Gateway /v1/integrations resource ID (for cross-stack route attachment)",
        )
        v1_admin_integrations_resource_id = get_or_create_api_resource(
            self, "V1AdminIntegrationsResource", common_resources,
            v1_admin_resource_id, "integrations"
        )
        create_ssm_string_parameter(
            self, "ApiGatewayV1AdminIntegrationsResourceIdParameter",
            parameter_name=SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_INTEGRATIONS_RESOURCE_ID'],
            string_value=v1_admin_integrations_resource_id,
            description="Shared API Gateway /v1/admin/integrations resource ID (for cross-stack route attachment)",
        )

        # Service APIs depend on group structures (must come after GroupCore)
        self.service_core = ServiceCore(
            self, "ServiceCore", common_resources,
            user_node_tags_function=self.node_core.user_node_tags_api.user_node_tags_function
        )

        # Voice assistant APIs depend on user/node structures

        # Notification service depends on other services being available
        self.notification_core = NotificationCore(self, "NotificationCore", common_resources, node_data_reset_function=self.node_data_reset.function)

        # Admin services
        self.integration_core = IntegrationCore(self, "IntegrationCore", common_resources)
        # superAdmin API to flip presence/publish_input IoT rules between
        # Lambda-direct and SQS at runtime (see rmneo/handlers/admin/iot_event_mode/).
        self.iot_event_mode_core = IotEventModeCore(
            self, "IotEventModeCore", common_resources,
            presence_handler=self.node_core.presence_event_handler_api,
            publish_input_handler=self.node_core.publish_input_event_handler_api,
        )
        # superAdmin API vending read-only creds for the rmng-owned values the dashboard's
        # post-deployment page reports (see rmneo/handlers/admin/rmng_admin_creds/).
        self.admin_creds_core = AdminCredsCore(self, "AdminCredsCore", common_resources)

        region = Stack.of(self).region
        # MCP + OAuth Proxy (reusable construct)
        mcp_config = McpOAuthConfig(
            # End-user auth is OIDC-only. The OAuth proxy brokers auth to this issuer
            # as a public PKCE registry client (mcp-oauth-client, seeded in espuser-base).
            # The construct wires USER_ISSUER / USER_JWKS_PARA_NAME / MCP_CLIENT_ID from these.
            user_issuer=common_resources.esp_user_issuer,
            user_jwks_parameter=USER_SSM_PARAMETERS['ESP_USER_JWKS'],
            mcp_oidc_client_id=common_resources.esp_mcp_client_id,
            mcp_oidc_client_secret=common_resources.esp_mcp_client_secret,
            mcp_binary_path=os.path.join(os.path.dirname(__file__), "..", "..", "..", "build", "mcp_server"),
            oauth_proxy_binary_path=os.path.join(os.path.dirname(__file__), "..", "..", "..", "build", "mcp_oauth_proxy"),
            mcp_extra_policies=[
                iam.PolicyStatement(
                    actions=["dynamodb:GetItem", "dynamodb:Query", "dynamodb:BatchGetItem"],
                    resources=[
                        get_table_arn(TABLE_NAMES['GROUPS'], region),
                        get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                        get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                        get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region),
                        get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region),
                    ]
                ),
                iam.PolicyStatement(
                    actions=["iot:GetThingShadow", "iot:UpdateThingShadow", "iot:Publish"],
                    resources=["*"],
                ),
                iam.PolicyStatement(
                    actions=["ssm:GetParameter"],
                    resources=[get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_JWKS'], region)],
                ),
            ],
            prefix=common_resources.prefix,
        )
        self.mcp_oauth = McpOAuthConstruct(self, "McpOAuth", mcp_config)
        common_resources.http_api = self.mcp_oauth.http_api

        # Register the proxy's callback on the espuser registry client. The seeded
        # mcp-oauth-client starts with NO redirect_uris because its callback
        # (<MCP_BASE_URL>/oauth2/callback) is only known once this stack resolves the MCP
        # domain (see SEEDED_OAUTH_CLIENTS in espuser/base_res_constants.py); until it
        # is registered, /oauth2/authorize rejects the proxy's redirect_uri with 400.
        # ADD unions into the string set (idempotent, keeps operator-added URIs), and the
        # condition refuses to conjure a phantom client if seeding has not run. on_update
        # re-fires whenever the discovered MCP base URL changes, since it is a parameter.
        mcp_callback_call = cr.AwsSdkCall(
            service="DynamoDB",
            action="updateItem",
            parameters={
                "TableName": USER_TABLE_NAMES['OAUTH_CLIENTS'],
                "Key": {"client_id": {"S": mcp_config.mcp_oidc_client_id}},
                "UpdateExpression": "ADD redirect_uris :uris",
                "ConditionExpression": "attribute_exists(client_id)",
                "ExpressionAttributeValues": {
                    ":uris": {"SS": [f"{self.mcp_oauth.api_endpoint}/oauth2/callback"]},
                },
            },
            physical_resource_id=cr.PhysicalResourceId.of("mcp-callback-registration"),
        )
        mcp_callback_registration = cr.AwsCustomResource(
            self, "McpCallbackRegistration",
            on_create=mcp_callback_call,
            on_update=mcp_callback_call,
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(
                    actions=["dynamodb:UpdateItem"],
                    resources=[get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)],
                ),
            ]),
        )
        mcp_callback_registration.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "mcp-callback-registration"))

        # Hello World service (simple example)
        self.hello_world_core = HelloWorldCore(self, "HelloWorldCore", common_resources)

        # Use AwsCustomResource to deploy the API Gateway after all resources are created.
        # CfnDeployment with stage_name is unreliable because it conflicts with the stage
        # created by RestApi(deploy=True) in the base stack — CloudFormation can silently
        # fail to reassociate the stage, requiring manual "Deploy API" in the console.
        # AwsCustomResource calls the SDK directly, which always works.
        deployment_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        api_deploy = cr.AwsCustomResource(
            self, "ApiGatewayDeploy",
            on_create=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.api_gateway_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"api-deploy-{deployment_timestamp}"),
            ),
            on_update=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.api_gateway_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"api-deploy-{deployment_timestamp}"),
            ),
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(
                    actions=["apigateway:POST"],
                    resources=["arn:aws:apigateway:*::/restapis/*/deployments"],
                ),
                iam.PolicyStatement(
                    actions=["apigateway:PATCH"],
                    resources=["arn:aws:apigateway:*::/restapis/*/stages/prod"],
                ),
            ]),
        )
        api_deploy.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "api-gateway-deploy"))
        # Ensure deployment happens after all API methods are created
        api_deploy.node.add_dependency(self.file_core)
        api_deploy.node.add_dependency(self.user_core)
        api_deploy.node.add_dependency(self.group_core)
        api_deploy.node.add_dependency(self.node_core)
        api_deploy.node.add_dependency(self.nodeadmin_core)
        api_deploy.node.add_dependency(self.service_core)
        api_deploy.node.add_dependency(self.notification_core)
        api_deploy.node.add_dependency(self.integration_core)
        api_deploy.node.add_dependency(self.iot_event_mode_core)
        api_deploy.node.add_dependency(self.admin_creds_core)
        api_deploy.node.add_dependency(self.hello_world_core)


        # Outputs from core stack (forwarding from common_resources)
        CfnOutput(self, "BulkNodeClusterArn", value=self.nodeadmin_core.register_container.container_params["cluster_arn"])
        CfnOutput(self, "BulkNodeTaskDefinitionArn", value=self.nodeadmin_core.register_container.container_params["task_definition_arn"])
        CfnOutput(self, "BulkNodeSubnetIds", value=self.nodeadmin_core.register_container.container_params["public_subnet_ids"])
        CfnOutput(self, "BulkNodeSecurityGroupId", value=self.nodeadmin_core.register_container.container_params["security_group_id"])

        CfnOutput(self, "McpHttpApiUrl", value=self.mcp_oauth.api_url)
