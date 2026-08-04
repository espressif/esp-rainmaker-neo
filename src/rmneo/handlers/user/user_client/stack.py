# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda,
    aws_apigateway,
    aws_iam as iam,
    Stack,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn, get_app_platform_endpt_arn

class RegisterClient(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "user_client"
        register_client_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        register_client_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Query", "dynamodb:DeleteItem"],
            resources=[get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], region)]
        ))

        register_client_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["sns:CreatePlatformEndpoint", "sns:DeleteEndpoint"],
            resources=[
                get_app_platform_endpt_arn('APNS', region),
                get_app_platform_endpt_arn('APNS_SANDBOX', region),
                get_app_platform_endpt_arn('GCM', region)
            ]
        ))

        # Lambda Function
        self.register_client_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=register_client_lambda_role,
            environment={
                "USER_TABLE_NAME": TABLE_NAMES['USER_ENDPOINTS'],
            },
        )

        # Create API Gateway resources: /v1/app-platforms/{appPlatformId}/clients (share v1 if already created)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        integrations_resource_id = get_or_create_api_resource(
            self, "IntegrationsResource", common_resources,
            v1_parent_id, "integrations"
        )
        integration_id_resource_id = get_or_create_api_resource(
            self, "IntegrationIdResource", common_resources,
            integrations_resource_id, "{integrationId}"
        )
        endpoints_resource_id = get_or_create_api_resource(
            self, "EndpointsResource", common_resources,
            integration_id_resource_id, "endpoints"
        )
        # DELETE now needs to address one specific endpoint within an integration (multi-endpoint per user). PUT stays on the collection (register a new endpoint), DELETE moves to /endpoints/{endpointId}.
        endpoint_id_resource_id = get_or_create_api_resource(
            self, "EndpointIdResource", common_resources,
            endpoints_resource_id, "{endpointId}"
        )

        create_cfn_api_method(
            self, "RegisterEndpointPutMethod", common_resources,
            endpoints_resource_id, "PUT", self.register_client_function
        )
        create_cfn_api_method(
            self, "UnregisterEndpointDeleteMethod", common_resources,
            endpoint_id_resource_id, "DELETE", self.register_client_function
        )

        add_cors_options(
            self, "RegisterEndpointOptionsMethod", common_resources,
            endpoints_resource_id, allowed_methods=["PUT"]
        )
        add_cors_options(
            self, "RegisterEndpointIdOptionsMethod", common_resources,
            endpoint_id_resource_id, allowed_methods=["DELETE"]
        )
