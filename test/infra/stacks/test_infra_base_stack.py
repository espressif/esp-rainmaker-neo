# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    CfnOutput,
    aws_apigateway as apigateway,
    aws_secretsmanager as secretsmanager,
)
from app_common import create_rest_api, CommonResources, create_ssm_string_parameter
from constructs import Construct
from gsi_infra import GsiInfraCore, GsiReadinessGate # type: ignore
from test.infra.stacks.test_constants import SSM_PARAMETERS
from test.infra.handlers.base import WebhookBase

class TestInfraBaseStack(Stack):
    def __init__(self, scope: Construct, construct_id: str, **kwargs) -> None :
        super().__init__(scope, construct_id, **kwargs)

        self.common_resources = CommonResources(prefix="rmng-test-")

        self.gsi_infra = GsiInfraCore(
            self,
            "GsiInfra",
            common_resources=self.common_resources
        )

        self.api_gateway_cfn = create_rest_api(
            self,
            "RMNGTestInfraApi",
            rest_api_name="RMNGTestInfraApi",
            description="RMNG Test Infra API Gateway"
        )

        self.common_resources.api_gateway_id = self.api_gateway_cfn.rest_api_id
        self.common_resources.api_gateway_root_resource_id = self.api_gateway_cfn.root.resource_id

        WebhookBase(
            self,
            "WebhookBase",
            self.common_resources
        )

        create_ssm_string_parameter(
            self,
            "TestInfraApiGatewayIdParameter",
            parameter_name=SSM_PARAMETERS["API_GATEWAY_ID"],
            string_value=self.api_gateway_cfn.rest_api_id,
            description="API Gateway ID for RMNG Test Infra"
        )

        create_ssm_string_parameter(
            self,
            "TestInfraApiGatewayRootResourceIdParameter",
            parameter_name=SSM_PARAMETERS["API_GATEWAY_ROOT_RESOURCE_ID"],
            string_value=self.api_gateway_cfn.root.resource_id,
            description="API Gateway Root Resource ID for RMNG Test Infra"
        )

        # Invoke URL the notifications Lambda targets in mock mode. create_rest_api
        # deploys a "prod" stage, so this is a live URL. Captured via --outputs-file.
        CfnOutput(
            self,
            "ApiGatewayUrl",
            value=f"https://{self.api_gateway_cfn.rest_api_id}.execute-api.{self.region}.amazonaws.com/prod",
        )

        # The execute-api URL is public, so an API key gates every /v1 method
        # (defense-in-depth with the mock's in-Lambda JWT check). The value is
        # generated at deploy; the notification itest resolves it from the secret ARN
        # output and sets it on rmng-notifications so its requests carry x-api-key.
        mock_api_key_secret = secretsmanager.Secret(
            self,
            "MockApiKeySecret",
            generate_secret_string=secretsmanager.SecretStringGenerator(
                password_length=40,
                exclude_punctuation=True,
            ),
        )
        mock_api_key = apigateway.CfnApiKey(
            self,
            "MockGatewayApiKey",
            enabled=True,
            value=mock_api_key_secret.secret_value.unsafe_unwrap(),
        )
        mock_usage_plan = apigateway.CfnUsagePlan(
            self,
            "MockUsagePlan",
            api_stages=[
                apigateway.CfnUsagePlan.ApiStageProperty(
                    api_id=self.api_gateway_cfn.rest_api_id,
                    stage="prod",
                )
            ],
        )
        # ApiStages references the stage by name, so CloudFormation infers no
        # dependency on the Stage resource; order it explicitly or the plan is
        # created first ("API Stage not found").
        mock_usage_plan.add_resource_dependency(self.api_gateway_cfn.deployment_stage.node.default_child)
        apigateway.CfnUsagePlanKey(
            self,
            "MockUsagePlanKey",
            key_id=mock_api_key.ref,
            key_type="API_KEY",
            usage_plan_id=mock_usage_plan.ref,
        )
        # CfnOutput can't resolve a {{resolve:secretsmanager:...}} dynamic reference, so
        # export the secret ARN and let the itest fixture fetch the plaintext via boto3.
        # Also keeps the raw key out of the CloudFormation outputs.
        CfnOutput(
            self,
            "MockApiKeySecretArn",
            value=mock_api_key_secret.secret_arn,
        )

        self.gsi_readiness = GsiReadinessGate(
            self, "GsiReadiness",
            common_resources=self.common_resources,
        )