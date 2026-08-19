# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from datetime import datetime

from aws_cdk import (
    Stack,
    aws_ssm as ssm,
    aws_iam as iam,
    custom_resources as cr,
)
from constructs import Construct
from app_common import CommonResources, stable_logical_id
from src.rmneo.stacks.base_res_constants import SSM_PARAMETERS
from src.alexa.handlers.alexa_cfg.stack import AlexaCfgAPI


class RMNGAlexaCfgCoreStack(Stack):
    """The Alexa configuration API — `/v1/admin/integrations/alexa/configuration`.

    Separate from `rmng-alexa-core` because that stack deploys the skill Lambda
    to each Alexa region (NA/EU/FE), while this API is a single set of routes on
    the shared API Gateway: creating it per region would collide on the same
    REST API. Separate from `rmng-core` so the Alexa integration owns all of its
    own resources and a deployment without Alexa carries none of them.

    Follows the claim/GVA pattern: the shared API ids come from SSM, the routes
    attach to the existing /v1/admin, and a custom resource redeploys the prod
    stage so they go live.
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
        common_resources.admin_api_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_RESOURCE_ID'])
        # Logical ids for API resources are derived from the full URL path, and
        # the helper learns a parent's path only by having created it. This
        # parent is imported, so seed the map or the ids would differ from the
        # ones rmng-core produced for the same routes.
        common_resources._api_resource_path_by_ref[common_resources.admin_api_resource_id] = "v1/admin"

        # /v1/admin/integrations is shared with the generic integrations API, so
        # rmng-core owns it and publishes its id.
        admin_integrations_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_INTEGRATIONS_RESOURCE_ID'])
        common_resources._api_resource_path_by_ref[admin_integrations_resource_id] = "v1/admin/integrations"

        # The handler resolves the caller through the shared user-auth code,
        # which reads the pool JWKS from SSM; without these the auth service is
        # nil and the handler panics (502).
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
        # The config API registers Alexa's account-linking redirect URIs on the
        # shared voice-assistant OIDC client, so it needs that client's id.
        common_resources.esp_user_va_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_VA_CLIENT_ID'])

        self.alexa_cfg = AlexaCfgAPI(self, "AlexaCfgAPI", common_resources,
                                     admin_integrations_resource_id=admin_integrations_resource_id)

        # Publish the routes to the shared API's prod stage. rmng-base owns the
        # stage and snapshots its deployment before these methods exist, so —
        # as rmng-core, claim and GVA all do — force a fresh deployment.
        deployment_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        deploy_params = {
            "restApiId": common_resources.api_gateway_id,
            "stageName": "prod",
            "description": f"Auto-deploy Alexa cfg routes via CDK: {deployment_timestamp}",
        }
        api_deploy = cr.AwsCustomResource(
            self, "AlexaCfgApiGatewayDeploy",
            on_create=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters=deploy_params,
                physical_resource_id=cr.PhysicalResourceId.of(f"alexa-cfg-api-deploy-{deployment_timestamp}"),
            ),
            on_update=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters=deploy_params,
                physical_resource_id=cr.PhysicalResourceId.of(f"alexa-cfg-api-deploy-{deployment_timestamp}"),
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
            stable_logical_id("CustomAwsSdk", "alexa-cfg-api-gateway-deploy"))
        api_deploy.node.add_dependency(self.alexa_cfg)
