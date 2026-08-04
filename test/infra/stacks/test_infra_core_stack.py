# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from datetime import datetime, timezone

from aws_cdk import (
    Stack,
    aws_ssm as ssm,
    aws_iam as iam,
    custom_resources as cr,
)

from app_common import CommonResources
from constructs import Construct
from test.infra.stacks.test_constants import SSM_PARAMETERS
from test.infra.handlers.core import WebhookCore

class TestInfraCoreStack(Stack):
    def __init__(self, scope: Construct, construct_id:str, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        self.common_resources = CommonResources(prefix="rmng-test-")

        # Must be assigned before WebhookCore/WebhookApi is constructed - it reads
        # these ids off common_resources during __init__.
        self.common_resources.api_gateway_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ID']
        )
        self.common_resources.api_gateway_root_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ROOT_RESOURCE_ID']
        )

        webhook_core = WebhookCore(
            self,
            "WebhookCore",
            self.common_resources
        )

        # create_rest_api(deploy=True) in the base stack snapshots the "prod" stage
        # before this stack adds the /v1 methods, so those methods aren't served until
        # the stage is redeployed. Force a fresh deployment once the methods exist.
        # Mirrors rmng_core_stack's ApiGatewayDeploy: CfnDeployment with a stage_name is
        # unreliable against a base-owned stage, so call the SDK directly. The per-synth
        # timestamp makes the resource re-run every deploy, always picking up the latest methods.
        deployment_timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        deploy_call = cr.AwsSdkCall(
            service="APIGateway",
            action="createDeployment",
            parameters={
                "restApiId": self.common_resources.api_gateway_id,
                "stageName": "prod",
                "description": f"Auto-deploy via CDK: {deployment_timestamp}",
            },
            physical_resource_id=cr.PhysicalResourceId.of(f"api-deploy-{deployment_timestamp}"),
        )
        api_deploy = cr.AwsCustomResource(
            self,
            "ApiGatewayDeploy",
            on_create=deploy_call,
            on_update=deploy_call,
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
        # Redeploy only after every /v1 method in WebhookCore exists.
        api_deploy.node.add_dependency(webhook_core)