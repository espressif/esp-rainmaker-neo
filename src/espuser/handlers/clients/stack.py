# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
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
    add_cors_options,
)
from arn_utils import get_table_arn, get_ssm_parameter_arn
from src.espuser.stacks.base_res_constants import (
    USER_TABLE_NAMES,
    USER_SSM_PARAMETERS,
)


class ClientsAPI(Construct):
    """Superadmin OAuth client registry API (/v1/admin/clients).

    Wires the clients Go Lambda behind the admin Cognito authorizer (superadmin claim
    checked in-handler). Create / list / update / delete over espuser-oauth-clients.
    The registry is seeded in the base stack, next to the Cognito clients.
    See espuser/docs/en/specs/admin-clients.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "clients"
        clients_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Client registry table: create, get (for patch), scan (list), update (patch), delete.
        clients_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:UpdateItem",
                "dynamodb:DeleteItem",
                "dynamodb:Scan",
            ],
            resources=[
                get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region),
            ],
        ))

        # Verify the admin token in-handler (super_admin claim) against the admin pool JWKS.
        clients_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'], region),
            ],
        ))

        self.clients_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=clients_lambda_role,
            environment={
                "ADMIN_USER_POOL_ID": common_resources.esp_admin_user_pool_id,
                "ADMIN_USER_POOL_CLIENT_ID": common_resources.esp_admin_user_pool_client_id,
                "ADMIN_USER_POOL_JWKS_PARA_NAME": USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS'],
            },
        )

        # API tree /v1/admin/clients(/{client_id}), all behind the admin authorizer; CFn methods avoid cyclic deps.
        v1_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "v1",
            api_id=common_resources.esp_user_api_id
        )
        admin_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_id, "admin", api_id=common_resources.esp_user_api_id
        )
        clients_id = get_or_create_api_resource(
            self, "V1AdminClientsResource", common_resources,
            admin_id, "clients", api_id=common_resources.esp_user_api_id
        )
        client_by_id = get_or_create_api_resource(
            self, "V1AdminClientByIdResource", common_resources,
            clients_id, "{client_id}", api_id=common_resources.esp_user_api_id
        )

        def admin_method(method_id, resource_id, verb):
            create_cfn_api_method(
                self, method_id, common_resources,
                resource_id, verb, self.clients_function,
                authorization_type="COGNITO_USER_POOLS",
                authorizer_id=common_resources.esp_admin_cognito_authorizer_id,
                api_id=common_resources.esp_user_api_id
            )

        admin_method("ClientsPostMethod", clients_id, "POST")
        admin_method("ClientsGetMethod", clients_id, "GET")
        admin_method("ClientByIdPutMethod", client_by_id, "PUT")
        admin_method("ClientByIdDeleteMethod", client_by_id, "DELETE")

        add_cors_options(self, "ClientsOptionsMethod", common_resources, clients_id,
                         allowed_methods=["POST", "GET"], api_id=common_resources.esp_user_api_id)
        add_cors_options(self, "ClientByIdOptionsMethod", common_resources, client_by_id,
                         allowed_methods=["PUT", "DELETE"], api_id=common_resources.esp_user_api_id)
