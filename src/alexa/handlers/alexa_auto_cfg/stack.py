# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import Aws, Duration, Fn, Stack, aws_iam as iam
from constructs import Construct

from app_common import (
    CommonResources,
    add_cors_options,
    create_base_lambda_role,
    create_cfn_api_method,
    create_lambda_function,
    get_or_create_api_resource,
    stable_logical_id,
)
from arn_utils import get_ssm_parameter_arn, get_ssm_parameter_prefix_arn, get_table_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES
from src.rmneo.stacks.base_res_constants import SSM_PARAMETERS


class AlexaAutoCfgAPI(Construct):
    """Automatic Alexa setup: one POST that creates a Smart Home skill, configures it at Amazon over SMAPI (see src/alexa/smapi), and configures Neo to use it.

        POST /v1/admin/integrations/alexa/auto-configuration
        PUT|DELETE /v1/admin/integrations/alexa/lwa-connection

    Configuring Neo applies the same config as manual mode (alexa_cfg), so it needs the same OIDC / SSM / cross-region Lambda-permission grants."""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *,
                 alexa_resource_id, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "alexa_auto_cfg"
        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Admin check reads the user details table.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)],
        ))

        # The worker (runConfigWorker) registers the account-linking redirect URIs on the OIDC va-client row, the same row alexa_cfg updates in manual mode.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:UpdateItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)],
        ))

        # Account linking hands va-client's secret to Alexa. Read from the rmng base mirror, like OIDC_VA_CLIENT_ID, so the EspUserVaClientSecret override applies and id and secret always belong to one client.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[get_ssm_parameter_arn(SSM_PARAMETERS['ESP_USER_VA_CLIENT_SECRET'], region)],
        ))

        # testmock reads where the itest's LWA profile is answered; absent outside test accounts.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[get_ssm_parameter_prefix_arn('TEST_MOCK', region)],
        ))

        # The worker stores the AcceptGrant client id/secret + skill id in SSM, and the API path the connected Amazon account, which a disconnect deletes.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:PutParameter", "ssm:GetParameter", "ssm:DeleteParameter"],
            resources=[get_ssm_parameter_prefix_arn('ALEXA_CONFIG', region)],
        ))

        # The worker grants the Alexa invoke permission on the skill Lambda (rmng-alexa-skill-<rmng_region>) in each Alexa region.
        alexa_skill_fn_name = Fn.join("", ["rmng-alexa-skill-", region])
        lambda_role.add_to_policy(iam.PolicyStatement(
            # GetPolicy lists setup grants a dead run left behind, so they can be cleared.
            actions=["lambda:AddPermission", "lambda:RemovePermission", "lambda:GetPolicy"],
            resources=[Fn.join("", ["arn:aws:lambda:*:", Aws.ACCOUNT_ID, ":function:", alexa_skill_fn_name])],
        ))

        # The API path async-invokes THIS function to run the config pipeline. Grant on a
        # constructed ARN (not self.function.function_arn) to avoid a role<->function
        # circular dependency. create_lambda_function kebab-cases the name
        # ({prefix}{function_name with _ -> -}), so match that exactly.
        own_fn_arn = f"arn:aws:lambda:{region}:{Aws.ACCOUNT_ID}:function:{common_resources.prefix}{function_name.replace('_', '-')}"
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[own_fn_arn],
        ))

        self.function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=lambda_role,
            # API path returns fast; the same Lambda, async-invoked, runs the slow
            # build/config pipeline in the background (two Alexa builds, ~30-60s each).
            timeout=Duration.seconds(900),
            environment={
                "RMNG_REGION": region,
                "AWS_ACCOUNT_ID": Aws.ACCOUNT_ID,
                # Account linking points at the ESP User OIDC provider: its authorize and
                # token endpoints hang off the API base, and va-client is the shared
                # voice-assistant client the redirect URIs are registered on.
                "ESP_USER_API_URL": common_resources.esp_user_api_url,
                "OIDC_VA_CLIENT_ID": common_resources.esp_user_va_client_id,
                "OIDC_VA_CLIENT_SECRET_PARAM": SSM_PARAMETERS['ESP_USER_VA_CLIENT_SECRET'],
            },
        )

        # The worker publishes its own FAILED status, so an async retry would only re-run a timed-out pipeline from the start.
        self.function.configure_async_invoke(retry_attempts=0)
        # PutFunctionEventInvokeConfig overwrites in place, so a path-derived rename would delete the live config (aws-rules.md, Table 1).
        self.function.node.find_child("EventInvokeConfig").node.default_child.override_logical_id(
            stable_logical_id("LambdaEventInvokeConfig", function_name))

        # /v1/admin/integrations/alexa is created by alexa_cfg in this same stack and
        # passed in; re-creating the chain here duplicates the logical id and fails synth.
        auto_cfg = get_or_create_api_resource(self, "AdminAlexaAutoCfgResource", common_resources,
                                                         alexa_resource_id, "auto-configuration")

        create_cfn_api_method(self, "AlexaAutoCfgPostMethod", common_resources, auto_cfg, "POST", self.function)
        add_cors_options(self, "AlexaAutoCfgOptionsMethod", common_resources, auto_cfg, allowed_methods=["POST"])

        lwa_connection = get_or_create_api_resource(self, "AdminAlexaLWAConnectionResource", common_resources,
                                                    alexa_resource_id, "lwa-connection")
        create_cfn_api_method(self, "AlexaLWAConnectionPutMethod", common_resources, lwa_connection, "PUT", self.function)
        create_cfn_api_method(self, "AlexaLWAConnectionDeleteMethod", common_resources, lwa_connection, "DELETE", self.function)
        add_cors_options(self, "AlexaLWAConnectionOptionsMethod", common_resources, lwa_connection, allowed_methods=["PUT", "DELETE"])
