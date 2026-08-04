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
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES
from arn_utils import get_table_arn

class IntegrationCore(Construct):
    """Core/compute resources for the Integrations admin API - Lambda function and API integration"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role with necessary permissions
        function_name = "integration"
        integration_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add permissions for SNS Platform Applications
        integration_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "sns:CreatePlatformApplication",
                "sns:GetPlatformApplicationAttributes",
                "sns:SetPlatformApplicationAttributes",
                "sns:DeletePlatformApplication",
                "sns:ListPlatformApplications"
            ],
            resources=["*"]  # SNS platform applications require wildcard resource
        ))

        # Add permissions for DynamoDB User Details Table
        integration_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Create Lambda function
        self.integration_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=integration_lambda_role
        )

        # Route tree:
        #   /v1/admin/integrations               (POST, GET)
        #   /v1/admin/integrations/{integrationId} (GET, PUT, DELETE)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin"
        )

        integrations_resource_id = get_or_create_api_resource(
            self, "IntegrationsResource", common_resources,
            v1_admin_parent_id, "integrations"
        )

        create_cfn_api_method(
            self, "IntegrationsPostMethod", common_resources,
            integrations_resource_id, "POST", self.integration_function
        )
        create_cfn_api_method(
            self, "IntegrationsGetMethod", common_resources,
            integrations_resource_id, "GET", self.integration_function
        )

        add_cors_options(
            self, "IntegrationsOptionsMethod", common_resources,
            integrations_resource_id, allowed_methods=["POST", "GET"]
        )

        integration_id_resource_id = get_or_create_api_resource(
            self, "IntegrationIdResource", common_resources,
            integrations_resource_id, "{integrationId}"
        )

        create_cfn_api_method(
            self, "IntegrationGetOneMethod", common_resources,
            integration_id_resource_id, "GET", self.integration_function
        )
        create_cfn_api_method(
            self, "IntegrationPutMethod", common_resources,
            integration_id_resource_id, "PUT", self.integration_function
        )
        create_cfn_api_method(
            self, "IntegrationDeleteMethod", common_resources,
            integration_id_resource_id, "DELETE", self.integration_function
        )

        add_cors_options(
            self, "IntegrationIdOptionsMethod", common_resources,
            integration_id_resource_id, allowed_methods=["GET", "PUT", "DELETE"]
        )

        # Non-admin: /v1/integrations (GET). Reuses the "integrations" resource
        # (shared via common_resources cache); handler gates on the path.
        public_integrations_resource_id = get_or_create_api_resource(
            self, "PublicIntegrationsResource", common_resources,
            v1_parent_id, "integrations"
        )

        create_cfn_api_method(
            self, "PublicIntegrationsGetMethod", common_resources,
            public_integrations_resource_id, "GET", self.integration_function
        )

        add_cors_options(
            self, "PublicIntegrationsOptionsMethod", common_resources,
            public_integrations_resource_id, allowed_methods=["GET"]
        )
