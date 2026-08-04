# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import Aws, Fn
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
from arn_utils import get_table_arn, get_ssm_parameter_prefix_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES

class AlexaCfgAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role with necessary permissions
        function_name = "alexa_cfg"
        alexa_cfg_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Read + update the OIDC va-client registry row to (re)register Alexa's redirect URIs.
        alexa_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:UpdateItem"
            ],
            resources=[
                get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)
            ]
        ))

        # Add permissions for DynamoDB User Details Table
        alexa_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Add permissions for SSM Parameter Store. DeleteParameter is needed to clear the
        # manufacturer name: SSM rejects an empty value, so resetting the brand to the default
        # deletes the parameter rather than overwriting it.
        alexa_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:PutParameter",
                "ssm:GetParameter",
                "ssm:DeleteParameter"
            ],
            resources=[
                get_ssm_parameter_prefix_arn('ALEXA_CONFIG', region)
            ]
        ))

        # AddPermission targets skill Lambdas in each Alexa region; function name is rmng-alexa-skill-<rmng_region>
        # where rmng_region matches this stack's region (same as RmngRegion passed into Alexa stacks).
        alexa_skill_fn_name = Fn.join("", ["rmng-alexa-skill-", region])
        alexa_cfg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "lambda:AddPermission",
                "lambda:RemovePermission"
            ],
            resources=[
                Fn.join("", ["arn:aws:lambda:*:", Aws.ACCOUNT_ID, ":function:", alexa_skill_fn_name]),
            ],
        ))

        # Create Lambda function
        self.alexa_cfg_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=alexa_cfg_lambda_role,
            environment={
                "USER_ISSUER": common_resources.esp_user_issuer,
                "RMNG_REGION": region,
                "OIDC_VA_CLIENT_ID": common_resources.esp_user_va_client_id
            }
        )

        # Create API Gateway resources: /v1/admin/integrations/alexa/configuration
        # (Admin-tier rename — was /v1/integrations/alexa/configuration.)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin"
        )

        admin_integrations_parent_id = get_or_create_api_resource(
            self, "AdminIntegrationsResource", common_resources,
            v1_admin_parent_id, "integrations"
        )

        alexa_resource_id = get_or_create_api_resource(
            self, "AdminAlexaResource", common_resources,
            admin_integrations_parent_id, "alexa"
        )

        alexa_cfg_resource_id = get_or_create_api_resource(
            self, "AdminAlexaCfgResource", common_resources,
            alexa_resource_id, "configuration"
        )

        create_cfn_api_method(
            self, "AlexaCfgPostMethod", common_resources,
            alexa_cfg_resource_id, "POST", self.alexa_cfg_function
        )

        create_cfn_api_method(
            self, "AlexaCfgGetMethod", common_resources,
            alexa_cfg_resource_id, "GET", self.alexa_cfg_function
        )

        add_cors_options(
            self, "AlexaCfgOptionsMethod", common_resources,
            alexa_cfg_resource_id, allowed_methods=["GET", "POST"]
        ) 