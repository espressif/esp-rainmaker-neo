# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0


from aws_cdk import (
    Stack,
    aws_ssm as ssm,
)
from constructs import Construct
from app_common import CommonResources, create_api_deployment
from src.rmneo.stacks.base_res_constants import SSM_PARAMETERS
from src.smartthings.handlers.st_cfg.stack import STCfgAPI


class RMNGSTCfgCoreStack(Stack):
    """The SmartThings configuration API — `/v1/admin/integrations/smartthings/configuration`.

    Separate from `rmng-st-core` because that stack deploys the Schema App
    Lambda to each region, while this API is a single set of routes on the
    shared API Gateway: creating it per region would collide on the same REST
    API. Separate from `rmng-core` so the SmartThings integration owns all of
    its own resources and a deployment without SmartThings carries none of them.

    Follows the Alexa/GVA pattern: the shared API ids come from SSM, the routes
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
        # The config API registers the SmartThings callback URLs on the shared
        # voice-assistant OIDC client, so it needs that client's id.
        common_resources.esp_user_va_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_VA_CLIENT_ID'])

        self.st_cfg = STCfgAPI(self, "STCfgAPI", common_resources,
                               admin_integrations_resource_id=admin_integrations_resource_id)

        # Publish the routes to the shared API's prod stage — rmng-base owns that stage
        # and snapshotted it before these methods existed. See create_api_deployment.
        api_deploy = create_api_deployment(
            self, "STCfgApiGatewayDeploy",
            api_id=common_resources.api_gateway_id,
            description="Auto-deploy SmartThings cfg routes via CDK",
            logical_name="st-cfg-api-gateway-deploy",
        )
        api_deploy.node.add_dependency(self.st_cfg)
