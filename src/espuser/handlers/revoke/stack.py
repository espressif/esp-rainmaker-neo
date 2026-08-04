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


class RevokeAPI(Construct):
    """OAuth 2.0 token-revocation endpoint (POST /oauth2/revoke, RFC 7009).

    Wires the revoke Go Lambda. Per D13 it revokes only the refresh token and its family
    (access tokens are short-lived JWTs, never tracked). Returns a uniform 200 so it is
    not a token-validity oracle. See espuser/docs/en/specs/auth-flows.md (Revoke endpoint).
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "revoke"
        revoke_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Refresh-token family table: revoke deletes the family the presented token identifies (DeleteItem).
        revoke_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:DeleteItem",
            ],
            resources=[
                get_table_arn(USER_TABLE_NAMES['REFRESH_TOKENS'], region),
            ],
        ))

        # Read the refresh-secret (SecureString) HMAC key to verify the presented refresh token before revoking its family.
        revoke_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'], region),
            ],
        ))

        # Client registry: GetItem to authenticate the client presenting Basic credentials (RFC 7009 §2.1).
        revoke_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region),
            ],
        ))

        self.revoke_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=revoke_lambda_role,
            environment={
                "ESPUSER_REFRESH_SECRET_PARAM": USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
            },
        )

        # /oauth2/revoke via CFn to avoid cyclic dependencies. Authorization is NONE at the gateway: client auth is HTTP Basic against the registry, which an authorizer can't express, so the Lambda enforces it.
        oauth2_parent_id = get_or_create_api_resource(
            self, "OAuth2Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "oauth2",
            api_id=common_resources.esp_user_api_id
        )

        revoke_resource_id = get_or_create_api_resource(
            self, "OAuth2RevokeResource", common_resources,
            oauth2_parent_id, "revoke",
            api_id=common_resources.esp_user_api_id
        )

        create_cfn_api_method(
            self, "OAuth2RevokePostMethod", common_resources,
            revoke_resource_id, "POST", self.revoke_function,
            authorization_type="NONE",  # Client auth is HTTP Basic, enforced in the Lambda (see above)
            api_id=common_resources.esp_user_api_id
        )

        add_cors_options(
            self, "OAuth2RevokeOptionsMethod", common_resources,
            revoke_resource_id, allowed_methods=["POST"],
            api_id=common_resources.esp_user_api_id
        )
