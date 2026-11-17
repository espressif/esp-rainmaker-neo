# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0


from aws_cdk import (
    CfnOutput,
    Stack,
    aws_ssm as ssm,
)
from constructs import Construct

from app_common import (
    CommonResources,
    DiscoveredApi,
    create_api_deployment,
    create_oauth_client,
)
from src.alexa.handlers.alexa_auto_cfg.stack import AlexaAutoCfgAPI
from src.alexa.handlers.alexa_cfg.stack import AlexaCfgAPI
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS, USER_TABLE_NAMES
from src.rmneo.stacks.base_res_constants import (
    ALEXA_CLIENT_ID,
    ALEXA_OAUTH_CLIENT,
    SSM_PARAMETERS,
    VOICE_ASSISTANT_CLIENT_SECRET_PARAMS,
)


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
        # Automatic mode configures account linking itself, so it needs the OIDC provider's
        # API base to derive the authorize and token endpoints.
        common_resources.esp_user_api_url = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_API_URL'])

        self.alexa_cfg = AlexaCfgAPI(self, "AlexaCfgAPI", common_resources,
                                     admin_integrations_resource_id=admin_integrations_resource_id)

        neo_api = DiscoveredApi(
            ssm.StringParameter.value_for_string_parameter(self, SSM_PARAMETERS['API_GATEWAY_URL']),
            ssm.StringParameter.value_for_string_parameter(self, SSM_PARAMETERS['RMNG_API_RESOURCE']),
            ssm.StringParameter.value_for_string_parameter(self, SSM_PARAMETERS['RMNG_API_AUDIENCES']))
        create_oauth_client(
            self, "AlexaOAuthClient",
            name="alexa-oauth-client",
            table_name=USER_TABLE_NAMES['OAUTH_CLIENTS'],
            client=ALEXA_OAUTH_CLIENT,
            secret_param=VOICE_ASSISTANT_CLIENT_SECRET_PARAMS[ALEXA_CLIENT_ID],
            api=neo_api,
        )
        CfnOutput(self, "EspAlexaClientId", value=ALEXA_CLIENT_ID, description="ESP User Amazon Alexa Client ID")

        # Automatic mode: one POST that creates and fully configures the skill over SMAPI,
        # hung off the /v1/admin/integrations/alexa resource alexa_cfg just created.
        self.alexa_auto_cfg = AlexaAutoCfgAPI(
            self, "AlexaAutoCfgAPI", common_resources,
            alexa_resource_id=self.alexa_cfg.alexa_resource_id)

        # Publish the routes to the shared API's prod stage — rmng-base owns that stage
        # and snapshotted it before these methods existed. See create_api_deployment.
        api_deploy = create_api_deployment(
            self, "AlexaCfgApiGatewayDeploy",
            api_id=common_resources.api_gateway_id,
            description="Auto-deploy Alexa cfg routes via CDK",
            logical_name="alexa-cfg-api-gateway-deploy",
        )
        api_deploy.node.add_dependency(self.alexa_cfg)
        api_deploy.node.add_dependency(self.alexa_auto_cfg)
