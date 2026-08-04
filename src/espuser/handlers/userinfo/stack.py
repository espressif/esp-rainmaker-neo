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
    add_cors_options,
)
from arn_utils import get_table_arn
from src.espuser.stacks.base_res_constants import (
    USER_TABLE_NAMES,
    USER_SSM_PARAMETERS,
)


class UserinfoAPI(Construct):
    """OIDC UserInfo endpoint (GET /oauth2/userinfo).

    Wires the userinfo Go Lambda. It verifies the presented access token against the
    published JWKS (over HTTPS, so no signing-key access) and returns the subject's
    scope-authorized claims from espuser-user-details. Bearer-protected by the token
    itself, so no API Gateway authorizer. See espuser/docs/en/specs/auth-flows.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "userinfo"
        userinfo_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Resolve the token subject to a user row (direct GetItem on user_id).
        userinfo_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region),
            ],
        ))

        # ESPUSER_ISSUER (used to fetch the JWKS to verify the RS256 token) is injected by create_lambda_function.
        self.userinfo_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=userinfo_lambda_role,
            environment={},
        )

        # API Gateway resources via CFn to avoid cyclic dependencies. The tree is
        # /oauth2/userinfo, unauthenticated at the gateway (the access token is the credential;
        # the lambda verifies it).
        oauth2_parent_id = get_or_create_api_resource(
            self, "OAuth2Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "oauth2",
            api_id=common_resources.esp_user_api_id
        )

        userinfo_resource_id = get_or_create_api_resource(
            self, "OAuth2UserinfoResource", common_resources,
            oauth2_parent_id, "userinfo",
            api_id=common_resources.esp_user_api_id
        )

        # OIDC Core §5.3.1: the UserInfo endpoint MUST support both GET and POST.
        for verb in ("GET", "POST"):
            create_cfn_api_method(
                self, f"OAuth2Userinfo{verb.title()}Method", common_resources,
                userinfo_resource_id, verb, self.userinfo_function,
                authorization_type="NONE",  # Unauthenticated at the gateway; the lambda verifies the bearer token
                api_id=common_resources.esp_user_api_id
            )

        add_cors_options(
            self, "OAuth2UserinfoOptionsMethod", common_resources,
            userinfo_resource_id, allowed_methods=["GET", "POST"],
            api_id=common_resources.esp_user_api_id
        )
