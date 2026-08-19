# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    aws_apigateway as apigateway,
    aws_ssm as ssm,
    Duration,
    Stack,
)
from constructs import Construct
from app_common import CommonResources, create_lambda_function, create_base_lambda_role, get_or_create_api_resource, add_cors_options
from src.gva.handlers.gva_cfg.stack import GVACfgAPI
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, SSM_PARAMETERS
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS
from arn_utils import (
    get_table_arn, get_index_arn, get_user_pool_arn, get_identity_pool_arn,
    get_api_gateway_invoke_arn, get_lambda_integration_uri,
    get_topic_arn, get_iot_thing_arn, get_ssm_parameter_prefix_arn, get_ssm_parameter_arn
)

class GVAActionCore(Construct):
    """Core/compute resources for GVA Action - Lambda function and API integration"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources,
                 v1_resource_id: str = None, integrations_resource_id: str = None,
                 admin_integrations_resource_id: str = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)
        
        region = Stack.of(self).region
        # Create Lambda role with necessary permissions
        function_name = "gva_action"
        gva_action_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add permissions for DynamoDB access
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:Query",
                "dynamodb:GetItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region),
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], region),
                get_table_arn(TABLE_NAMES['NODES_ONLINE'], region),
            ]
        ))

        # Add specific UpdateItem permission for node_details table
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:UpdateItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
            ]
        ))

        # Add PutItem permission for user table
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], region),
            ]
        ))

        # Add IoT permissions for device control
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:Publish",
                "iot:UpdateThingShadow",
                "iot:GetThingShadow"
            ],
            resources=[
                get_topic_arn('rainmaker/nodes/*', region),
                get_iot_thing_arn('*', region)
            ]
        ))

        # Add permissions for SSM Parameter Store
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter"
            ],
            resources=[
                get_ssm_parameter_prefix_arn('GVA_CONFIG', region),
                # The handler resolves the caller from an OIDC access token, which it
                # verifies against the JWKS this parameter holds.
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_JWKS'], region)
            ]
        ))

        # Add Cognito Identity permissions
        gva_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-identity:GetId"
            ],
            resources=[
                get_identity_pool_arn(common_resources.identity_pool_id, region)
            ]
        ))

        # Create Lambda function
        self.gva_action_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=gva_action_lambda_role,
            environment={
                "IDENTITY_POOL_ID": common_resources.identity_pool_id,
                "USER_ISSUER": common_resources.esp_user_issuer,
                "USER_JWKS_PARA_NAME": USER_SSM_PARAMETERS['ESP_USER_JWKS']
            }
        )

        # Create API Gateway resources: /v1/integrations/gva (share v1, integrations, and gva if already created)
        # Define path parts for dynamic path construction
        v1_path_part = "v1"
        integrations_path_part = "integrations"
        gva_path_part = "gva"
        
        # The shared API's /v1 is owned by rmng-core; when a parent id is
        # supplied (the separate GVA stack passes it from SSM) attach under it
        # rather than recreating /v1, which would collide on the API.
        v1_parent_id = v1_resource_id or get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, v1_path_part
        )
        
        # /v1/integrations is a shared parent owned by rmng-core (the generic
        # integrations API). Attach under it when its id is supplied rather than
        # recreating it, which would collide on the shared API.
        integrations_parent_id = integrations_resource_id or get_or_create_api_resource(
            self, "IntegrationsResource", common_resources,
            v1_parent_id, integrations_path_part
        )
        
        # Share gva resource if already created (created by GVACfgAPI)
        gva_parent_id = get_or_create_api_resource(
            self, "GVAResource", common_resources,
            integrations_parent_id, gva_path_part
        )

        # Construct fulfillment path dynamically from path parts
        self.fulfillment_path = f"{v1_path_part}/{integrations_path_part}/{gva_path_part}"

        # Integration URI for Lambda function
        integration_uri = get_lambda_integration_uri(self.gva_action_function.function_arn, region)

        # Create POST method for GVA webhook using CFn constructs
        gva_post_method = apigateway.CfnMethod(
            self, "GVAPostMethod",
            rest_api_id=common_resources.api_gateway_id,
            resource_id=gva_parent_id,
            http_method="POST",
            authorization_type="NONE",  # Override base API's IAM auth
            integration=apigateway.CfnMethod.IntegrationProperty(
                type="AWS_PROXY",
                integration_http_method="POST",
                uri=integration_uri,
                integration_responses=[
                    apigateway.CfnMethod.IntegrationResponseProperty(
                        status_code="200",
                        response_parameters={
                            "method.response.header.Access-Control-Allow-Origin": "'*'",
                            "method.response.header.Access-Control-Allow-Headers": "'Content-Type,Authorization'",
                            "method.response.header.Access-Control-Allow-Methods": "'POST,OPTIONS'"
                        }
                    )
                ]
            ),
            method_responses=[
                apigateway.CfnMethod.MethodResponseProperty(
                    status_code="200",
                    response_parameters={
                        "method.response.header.Access-Control-Allow-Origin": True,
                        "method.response.header.Access-Control-Allow-Headers": True,
                        "method.response.header.Access-Control-Allow-Methods": True
                    }
                )
            ]
        )

        # Add OPTIONS method for CORS preflight using reusable function
        add_cors_options(
            self, "GVAOptionsMethod", common_resources,
            gva_parent_id, allowed_methods=["POST"]
        )

        # Grant API Gateway permission to invoke Lambda
        self.gva_action_function.add_permission(
            "GVAApiGatewayInvoke",
            principal=iam.ServicePrincipal("apigateway.amazonaws.com"),
            action="lambda:InvokeFunction",
            source_arn=get_api_gateway_invoke_arn(common_resources.api_gateway_id, region, "POST", "v1/integrations/gva")
        )

        # Create GVA configuration API
        GVACfgAPI(self, "GVACfgAPI", common_resources,
                  admin_integrations_resource_id=admin_integrations_resource_id)
