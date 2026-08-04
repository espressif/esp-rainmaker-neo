# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from datetime import datetime

from aws_cdk import (
    Stack,
    CfnOutput,
    aws_ssm as ssm,
    aws_iam as iam,
    custom_resources as cr,
)
from constructs import Construct
from app_common import CommonResources, stable_logical_id
from src.rmneo.stacks.base_res_constants import SSM_PARAMETERS
from src.claim.handlers.core import ClaimCore
from src.rmneo.handlers.nodeadmin.bulk_container.stack import CreateNodeRegisterPolicy


class RMNGClaimCoreStack(Stack):
    """Compute + endpoint for assisted claiming — the claim_handler Lambda and
    its routes.

    Owns claiming's compute now that it is a separate stack group; rmng-core no
    longer creates it. Deliberately reuses the SAME shared API Gateway and the
    SAME sigv4/IAM auth as every other REST API (no new API Gateway, no Cognito
    authorizer): it reads the shared API id + /v1 resource id from SSM and
    attaches /v1/claim/{initiate,verify} to the existing /v1, then forces a
    stage redeploy so the new methods go live.
    """

    def __init__(self, scope: Construct, construct_id: str,
                 common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)
        self.common_resources = common_resources

        # Shared API Gateway references (published by rmng-base / rmng-core).
        common_resources.api_gateway_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ID'])
        common_resources.api_gateway_root_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_ROOT_RESOURCE_ID'])
        v1_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_RESOURCE_ID'])
        # The shared /v1/admin resource already exists (created and published by
        # rmng-core, used by its own admin routes); the admin claiming API
        # attaches under it rather than recreating it, which would collide on the
        # shared API.
        common_resources.admin_api_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_RESOURCE_ID'])

        # The claim handler resolves the caller through the shared user-auth code
        # (user.NewContextWithAPIRequest), which reads the Cognito pool JWKS from
        # SSM. So the handler needs the same user-pool / JWKS common_resources the
        # other REST handlers get — both for its env vars (set by
        # create_lambda_function) and for create_base_lambda_role's user-pool /
        # JWKS SSM grants. Without these the auth service is nil and the handler
        # panics (502).
        common_resources.esp_user_issuer = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_ISSUER'])
        common_resources.esp_user_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_CLIENT_ID'])
        common_resources.esp_user_jwks = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_JWKS'])
        common_resources.esp_admin_user_pool_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID'])
        common_resources.esp_admin_user_pool_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID'])
        common_resources.esp_admin_user_pool_jwks = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'])

        # Certificate binding reuses the shared node register/update helpers, so
        # the claim role needs the same IoT/node grants registration uses. That
        # policy is created in nodeadmin; reconstruct an equivalent one here
        # rather than taking a cross-stack dependency on it.
        node_register_policy = CreateNodeRegisterPolicy(
            self, "CreateNodeRegisterPolicy", common_resources=common_resources)

        self.claim_core = ClaimCore(
            self, "ClaimCore", common_resources,
            node_register_policy=node_register_policy.policy,
            v1_resource_id=v1_resource_id,
        )

        # Publish the claim methods to the shared API's prod stage. RestApi(
        # deploy=True) in rmng-base snapshots its deployment before these
        # methods exist and owns the stage, so — exactly as rmng-core does for
        # its own methods — force a fresh deployment via the SDK. The timestamp
        # makes it re-run on every deploy.
        deployment_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        api_deploy = cr.AwsCustomResource(
            self, "ClaimApiGatewayDeploy",
            on_create=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.api_gateway_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy claim routes via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"claim-api-deploy-{deployment_timestamp}"),
            ),
            on_update=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.api_gateway_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy claim routes via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"claim-api-deploy-{deployment_timestamp}"),
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
            stable_logical_id("CustomAwsSdk", "claim-api-gateway-deploy"))
        api_deploy.node.add_dependency(self.claim_core)

        # Availability flag for clients: present ⇒ assisted claiming is deployed.
        # Absent from the published outputs when the claim group is not deployed.
        CfnOutput(
            self, "AssistedClaiming",
            value="true",
            description="Assisted claiming is enabled on this deployment",
        )
