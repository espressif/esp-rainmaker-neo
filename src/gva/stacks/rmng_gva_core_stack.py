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
from src.gva.handlers.core import GVAActionCore


class RMNGGVACoreStack(Stack):
    """Compute + endpoint for the Google Home integration — the gva_action and
    gva_cfg Lambdas and their routes.

    Owns GVA's compute now that it is a separate stack group; rmng-core no
    longer creates it. Unlike Alexa and SmartThings, whose Lambdas are invoked
    directly by their platform, GVA fulfillment is an HTTPS endpoint, so this
    follows the claim stack instead: it reads the shared API id and /v1 resource
    id from SSM, attaches /v1/integrations/gva to the existing /v1, then forces
    a stage redeploy so the new methods go live.
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
        # The shared /v1/admin resource already exists (created and published by
        # rmng-core); the gva_cfg admin API attaches under it rather than
        # recreating it, which would collide on the shared API.
        common_resources.admin_api_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_RESOURCE_ID'])
        # /v1 itself is owned by rmng-base; the fulfillment route attaches under
        # it rather than recreating it.
        v1_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_RESOURCE_ID'])

        # Logical ids for API resources are derived from the full URL path, and
        # the helper learns a parent's path only by having created it. These two
        # parents are imported, so seed the map: without it both /v1/integrations
        # and /v1/admin/integrations resolve to the bare "integrations" and
        # collide, and the ids would differ from the ones rmng-core produced.
        common_resources._api_resource_path_by_ref[v1_resource_id] = "v1"
        common_resources._api_resource_path_by_ref[common_resources.admin_api_resource_id] = "v1/admin"

        # /v1/integrations and /v1/admin/integrations are shared with the generic
        # integrations API, so rmng-core owns them and publishes their ids.
        integrations_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_INTEGRATIONS_RESOURCE_ID'])
        admin_integrations_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_V1_ADMIN_INTEGRATIONS_RESOURCE_ID'])
        common_resources._api_resource_path_by_ref[integrations_resource_id] = "v1/integrations"
        common_resources._api_resource_path_by_ref[admin_integrations_resource_id] = "v1/admin/integrations"

        # The fulfillment handler resolves the caller through the shared
        # user-auth code, which reads the pool JWKS from SSM. Without these the
        # auth service is nil and the handler panics (502) — same requirement
        # the claim stack documents.
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
        # Device control goes through the identity pool, as it did when this
        # construct lived in rmng-core.
        common_resources.identity_pool_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['IDENTITY_POOL_ID'])
        # The config API registers Google's account-linking redirect URIs on the
        # shared voice-assistant OIDC client, so it needs that client's id.
        common_resources.esp_user_va_client_id = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['ESP_USER_VA_CLIENT_ID'])

        self.gva_action_core = GVAActionCore(
            self, "GVAActionCore", common_resources,
            v1_resource_id=v1_resource_id,
            integrations_resource_id=integrations_resource_id,
            admin_integrations_resource_id=admin_integrations_resource_id,
        )

        # Publish the GVA methods to the shared API's prod stage. RestApi(
        # deploy=True) in rmng-base snapshots its deployment before these
        # methods exist and owns the stage, so — exactly as rmng-core and the
        # claim stack do for their own methods — force a fresh deployment via
        # the SDK. The timestamp makes it re-run on every deploy.
        deployment_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        deploy_params = {
            "restApiId": common_resources.api_gateway_id,
            "stageName": "prod",
            "description": f"Auto-deploy GVA routes via CDK: {deployment_timestamp}",
        }
        api_deploy = cr.AwsCustomResource(
            self, "GVAApiGatewayDeploy",
            on_create=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters=deploy_params,
                physical_resource_id=cr.PhysicalResourceId.of(f"gva-api-deploy-{deployment_timestamp}"),
            ),
            on_update=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters=deploy_params,
                physical_resource_id=cr.PhysicalResourceId.of(f"gva-api-deploy-{deployment_timestamp}"),
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
            stable_logical_id("CustomAwsSdk", "gva-api-gateway-deploy"))
        api_deploy.node.add_dependency(self.gva_action_core)

        # The fulfillment URL Google Home is configured with. Published here now
        # that this stack owns the route; rmng-core no longer emits it. The path
        # is unchanged, so existing Google Home projects keep working.
        api_gateway_url = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['API_GATEWAY_URL'])
        CfnOutput(
            self, "GVAFulfillmentUrl",
            value=f"{api_gateway_url}/{self.gva_action_core.fulfillment_path}",
            description="Fulfillment URL to configure in the Google Home project",
        )
