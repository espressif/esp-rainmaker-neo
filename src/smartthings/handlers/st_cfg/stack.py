# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda,
    aws_apigateway as apigateway,
    aws_iam as iam,
    Stack
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
from arn_utils import get_table_arn, get_ssm_parameter_prefix_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES


class STCfgAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources,
                 admin_integrations_resource_id: str = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region

        # Create Lambda role for SmartThings configuration
        function_name = "st_cfg"
        st_cfg_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add SSM permissions for storing/retrieving/deleting SmartThings client credentials
        st_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter",
                "ssm:PutParameter",
                "ssm:DeleteParameter"
            ],
            resources=[
                get_ssm_parameter_prefix_arn('ST_CONFIG', region)
            ]
        ))

        # Read + update the OIDC va-client registry row to (re)register the SmartThings callback URLs.
        st_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:UpdateItem"
            ],
            resources=[
                get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)
            ]
        ))

        # Create Lambda function for SmartThings configuration management
        self.st_cfg_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=st_cfg_lambda_role,
            environment={
                "OIDC_VA_CLIENT_ID": common_resources.esp_user_va_client_id
            }
        )

        # Create API Gateway resources: /v1/admin/integrations/smartthings/configuration
        # /v1 and /v1/admin are owned by rmng-base; when their id is already
        # resolved (the separate stack reads it from SSM) attach under it rather
        # than recreating them, which would collide on the shared API.
        if common_resources.admin_api_resource_id:
            v1_admin_parent_id = common_resources.admin_api_resource_id
        else:
            v1_parent_id = get_or_create_api_resource(
                self, "V1Resource", common_resources,
                common_resources.api_gateway_root_resource_id, "v1"
            )

            v1_admin_parent_id = get_or_create_api_resource(
                self, "V1AdminResource", common_resources,
                v1_parent_id, "admin"
            )

        # /v1/admin/integrations is a shared parent owned by rmng-core (the generic
        # integrations API). Attach under it when its id is supplied rather than
        # recreating it, which would collide on the shared API.
        admin_integrations_parent_id = admin_integrations_resource_id or get_or_create_api_resource(
            self, "AdminIntegrationsResource", common_resources,
            v1_admin_parent_id, "integrations"
        )

        admin_st_parent_id = get_or_create_api_resource(
            self, "AdminSTResource", common_resources,
            admin_integrations_parent_id, "smartthings"
        )

        st_cfg_resource_id = get_or_create_api_resource(
            self, "AdminSTCfgResource", common_resources,
            admin_st_parent_id, "configuration"
        )

        # Create methods using CFn
        create_cfn_api_method(
            self, "STCfgPostMethod", common_resources,
            st_cfg_resource_id, "POST", self.st_cfg_function
        )

        create_cfn_api_method(
            self, "STCfgGetMethod", common_resources,
            st_cfg_resource_id, "GET", self.st_cfg_function
        )

        create_cfn_api_method(
            self, "STCfgDeleteMethod", common_resources,
            st_cfg_resource_id, "DELETE", self.st_cfg_function
        )

        add_cors_options(
            self, "STCfgOptionsMethod", common_resources,
            st_cfg_resource_id, allowed_methods=["GET", "POST", "DELETE"]
        )
