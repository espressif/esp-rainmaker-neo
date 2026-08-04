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
from arn_utils import get_table_arn, get_table_index_arn, get_ssm_parameter_arn
from src.espuser.stacks.base_res_constants import (
    USER_TABLE_NAMES,
    USER_INDEX_NAMES,
    USER_SSM_PARAMETERS,
)


class AuthorizeAPI(Construct):
    """Browser authorization-code + PKCE login: GET /oauth2/authorize and the service-served
    login UI at GET /oauth2/login. authorize validates the request against the client registry,
    writes a LOGIN flow record, and 302s to the login page (flow_id cookie); the page drives OTP
    against /v1/auth/otp/*. See espuser/docs/en/specs/authorize-code-flow.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "authorize"
        authorize_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        authorize_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['AUTH_FLOWS'], region)],
        ))

        # Client registry: GetItem to validate client_id / redirect_uri / scopes at authorize.
        authorize_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region)],
        ))

        authorize_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:Scan"],
            resources=[get_table_arn(USER_TABLE_NAMES['IDENTITY_PROVIDERS'], region)],
        ))

        authorize_lambda_role.add_to_policy(iam.PolicyStatement(
            # UpdateItem too: resolving a login records a verified contact the account did not have.
            actions=["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query", "dynamodb:UpdateItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region),
                get_table_index_arn(USER_TABLE_NAMES['USER_DETAILS'], USER_INDEX_NAMES['USER_DETAILS_EMAIL'], region),
                get_table_index_arn(USER_TABLE_NAMES['USER_DETAILS'], USER_INDEX_NAMES['USER_DETAILS_PHONE'], region),
            ],
        ))

        authorize_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'], region),
            ],
        ))

        self.authorize_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=authorize_lambda_role,
            environment={
                # Explicit `/` separator: the api-url SSM parameter carries no trailing slash, so
                # interpolating the path directly produced `...prodoauth2/federation/callback` — a
                # redirect_uri the hosted UI rejects with 400.
                "ESPUSER_FEDERATION_CALLBACK_URL": f"{common_resources.esp_user_api_url}/oauth2/federation/callback",
                "ESPUSER_REFRESH_SECRET_PARAM": USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
            },
        )

        # API Gateway resources via CFn to avoid cyclic dependencies. Both /oauth2/authorize
        # and /oauth2/login are GET and unauthenticated (the login page is the entry point).
        oauth2_parent_id = get_or_create_api_resource(
            self, "OAuth2Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "oauth2",
            api_id=common_resources.esp_user_api_id
        )

        for verb_id, path_part in (("Authorize", "authorize"), ("Login", "login")):
            resource_id = get_or_create_api_resource(
                self, f"OAuth2{verb_id}Resource", common_resources,
                oauth2_parent_id, path_part,
                api_id=common_resources.esp_user_api_id
            )

            create_cfn_api_method(
                self, f"OAuth2{verb_id}GetMethod", common_resources,
                resource_id, "GET", self.authorize_function,
                authorization_type="NONE",  # Browser entry point; no bearer yet
                api_id=common_resources.esp_user_api_id
            )

            add_cors_options(
                self, f"OAuth2{verb_id}OptionsMethod", common_resources,
                resource_id, allowed_methods=["GET"],
                api_id=common_resources.esp_user_api_id
            )

        # Unauthenticated: the HMAC-signed upstream state is the credential on the callback.
        federation_parent_id = get_or_create_api_resource(
            self, "OAuth2FederationResource", common_resources,
            oauth2_parent_id, "federation",
            api_id=common_resources.esp_user_api_id
        )
        for verb_id, path_part in (("Start", "start"), ("Callback", "callback")):
            resource_id = get_or_create_api_resource(
                self, f"Federation{verb_id}Resource", common_resources,
                federation_parent_id, path_part,
                api_id=common_resources.esp_user_api_id
            )
            create_cfn_api_method(
                self, f"Federation{verb_id}GetMethod", common_resources,
                resource_id, "GET", self.authorize_function,
                authorization_type="NONE",
                api_id=common_resources.esp_user_api_id
            )
            add_cors_options(
                self, f"Federation{verb_id}OptionsMethod", common_resources,
                resource_id, allowed_methods=["GET"],
                api_id=common_resources.esp_user_api_id
            )
