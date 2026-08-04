# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    Stack,
)
from aws_cdk import aws_ssm as ssm
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options
)
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES, USER_SSM_PARAMETERS
from arn_utils import get_table_arn

class UserCommonAPI(Construct):
    """GET /v1/users/{userId} — authenticated end-user profile lookup.

    Unauthenticated at the gateway; the OIDC RS256 access token is the credential
    and is verified in-handler against the published JWKS (same pattern as userinfo).
    """
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "user_common"
        user_common_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # GetItem reads the caller's user_details row for GET /v1/users/{userId}.
        user_common_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region),
            ]
        ))

        # ESPUSER_ISSUER (used to fetch the JWKS to verify the RS256 token) is injected by create_lambda_function.
        self.user_common_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=user_common_lambda_role,
            environment={},
        )

        # Create API Gateway resources using CFn to avoid cyclic dependencies
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "v1",
            api_id=common_resources.esp_user_api_id
        )

        # GET /v1/users/{userId} — authenticated profile lookup ("me" resolves to caller)
        users_parent_id = get_or_create_api_resource(
            self, "V1UsersResource", common_resources,
            v1_parent_id, "users",
            api_id=common_resources.esp_user_api_id
        )

        user_by_id_resource_id = get_or_create_api_resource(
            self, "V1UsersUserIdResource", common_resources,
            users_parent_id, "{userId}",
            api_id=common_resources.esp_user_api_id
        )

        create_cfn_api_method(
            self, "UserProfileGetMethod", common_resources,
            user_by_id_resource_id, "GET", self.user_common_function,
            authorization_type="NONE",  # Unauthenticated at the gateway; the lambda verifies the OIDC bearer token
            api_id=common_resources.esp_user_api_id
        )

        add_cors_options(
            self, "UserProfileOptionsMethod", common_resources,
            user_by_id_resource_id, allowed_methods=["GET"],
            api_id=common_resources.esp_user_api_id
        )
