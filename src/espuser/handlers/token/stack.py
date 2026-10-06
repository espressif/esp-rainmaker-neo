# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    aws_iam as iam,
)
from constructs import Construct

from app_common import (
    CommonResources,
    add_cors_options,
    create_base_lambda_role,
    create_cfn_api_method,
    create_lambda_function,
    get_or_create_api_resource,
)
from arn_utils import get_ssm_parameter_arn, get_table_arn, get_table_index_arn
from src.espuser.stacks.base_res_constants import (
    USER_INDEX_NAMES,
    USER_SSM_PARAMETERS,
    USER_TABLE_NAMES,
)


class TokenAPI(Construct):
    """OAuth 2.0 token endpoint (POST /oauth2/token).

    Wires the token Go Lambda. Serves the refresh_token grant (rotation + reuse detection)
    and the authorization_code grant (browser-login code exchange, PKCE); client_credentials
    / token-exchange are later slices. See espuser/docs/en/specs/authorize-code-flow.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "token"
        token_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Refresh-token family table: GetItem the family, PutItem a new family (mint on
        # authorization_code), conditional UpdateItem to advance the counter (rotate), and
        # DeleteItem the family on reuse detection.
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:UpdateItem",
                "dynamodb:DeleteItem",
            ],
            resources=[
                get_table_arn(USER_TABLE_NAMES['REFRESH_TOKENS'], region),
            ],
        ))

        # Auth-flows: the authorization_code grant resolves the code via the by-code GSI (Query)
        # and consumes it by deleting the flow record (single-use).
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[get_table_index_arn(USER_TABLE_NAMES['AUTH_FLOWS'], USER_INDEX_NAMES['AUTH_FLOWS_BY_CODE'], region)],
        ))
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:DeleteItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['AUTH_FLOWS'], region)],
        ))

        # OAuth client registry: client authentication (RFC 6749 §2.3) reads the client on every grant.
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)],
        ))

        # The refresh grant clamp bumps the user agent's expires_at so a family can never outlive its
        # user agent: a Query on the by-user index to resolve the family's sid to a session row, then a
        # GetItem (the provider ceiling) and an UpdateItem to roll retention. A missing grant degrades
        # to "no bump" (the family keeps its plain TTL) rather than failing the token call.
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[get_table_index_arn(USER_TABLE_NAMES['SESSIONS'], USER_INDEX_NAMES['SESSIONS_BY_USER'], region)],
        ))
        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:UpdateItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['SESSIONS'], region)],
        ))

        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'], region),
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_JWKS'], region),
            ],
        ))

        token_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["kms:Sign"],  # the kid comes from the published JWKS; GetPublicKey is paid per call
            resources=[common_resources.esp_user_kms_signing_key_arn],
        ))

        self.token_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=token_lambda_role,
            environment={
                "ESPUSER_REFRESH_SECRET_PARAM": USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
                "ESPUSER_KMS_SIGNING_KEY_ARN": common_resources.esp_user_kms_signing_key_arn,
            },
        )

        # API Gateway resources via CFn to avoid cyclic dependencies. The tree is
        # /oauth2/token, unauthenticated (the refresh token + client_id are the credential).
        oauth2_parent_id = get_or_create_api_resource(
            self, "OAuth2Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "oauth2",
            api_id=common_resources.esp_user_api_id
        )

        token_resource_id = get_or_create_api_resource(
            self, "OAuth2TokenResource", common_resources,
            oauth2_parent_id, "token",
            api_id=common_resources.esp_user_api_id
        )

        create_cfn_api_method(
            self, "OAuth2TokenPostMethod", common_resources,
            token_resource_id, "POST", self.token_function,
            authorization_type="NONE",  # Unauthenticated: the refresh token + client_id are the credential
            api_id=common_resources.esp_user_api_id
        )

        add_cors_options(
            self, "OAuth2TokenOptionsMethod", common_resources,
            token_resource_id, allowed_methods=["POST"],
            api_id=common_resources.esp_user_api_id
        )
