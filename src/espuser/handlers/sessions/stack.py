# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    aws_dynamodb as aws_dynamodb,
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
from arn_utils import get_ssm_parameter_arn, get_table_arn
from src.espuser.stacks.base_res_constants import (
    USER_INDEX_NAMES,
    USER_SSM_PARAMETERS,
    USER_TABLE_NAMES,
)


class SessionsAPI(Construct):
    """The account's own session surface, plus RP-Initiated Logout.

        GET    /v1/user/sessions          my browsers, with the products on each
        DELETE /v1/user/sessions/{sessionId}    end one -- the lost-phone case
        DELETE /v1/user/sessions          end all of them, this browser included
        GET    /oauth2/logout             RP-Initiated Logout 1.0, advertised in discovery

    One binary, two trees. They share every dependency: the same two tables, the same token
    verification, the same session-to-family join. Splitting them would be two cold starts
    and two IAM policies to keep in step for no gain.

    Unauthenticated at the gateway throughout. The REST paths carry a bearer token the
    lambda verifies and scope-gates; /oauth2/logout carries a browser cookie, which is a
    credential API Gateway has no opinion about.

    See espuser/docs/specs/sso-sessions.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "sessions"
        sessions_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        sessions_table_arn = get_table_arn(USER_TABLE_NAMES['SESSIONS'], region)
        refresh_table_arn = get_table_arn(USER_TABLE_NAMES['REFRESH_TOKENS'], region)

        # Query on the index, then point reads and deletes on the base table. The index is
        # KEYS_ONLY, so the Query returns session hashes and the rows are fetched after --
        # which is why both the table ARN and the index ARN are needed.
        sessions_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[
                f"{sessions_table_arn}/index/{USER_INDEX_NAMES['SESSIONS_BY_USER']}",
                refresh_table_arn,
            ],
        ))
        sessions_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:UpdateItem", "dynamodb:DeleteItem"],
            resources=[sessions_table_arn],
        ))
        sessions_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:DeleteItem"],
            resources=[refresh_table_arn],
        ))
        # A grant missing here does not fail loudly: the session layer degrades to "no session"
        # so it can never block a login, so logout would resolve nothing, delete nothing, clear
        # the cookie, redirect, and look exactly like a successful sign-out. Shipping an env var
        # without its grant is the bug that cost a day.
        sessions_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                # Granted because the binary links refreshtoken and the env var is set. An
                # unused grant costs nothing; a second variable-without-grant would fail the
                # same silent way, and that failure mode has now cost a day.
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'], region),
            ],
        ))
        # Logout reads the client row to validate post_logout_redirect_uri (exact match, the
        # open-redirect defence) and the provider row for its end-session endpoint.
        sessions_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['OAUTH_CLIENTS'], region),
                get_table_arn(USER_TABLE_NAMES['IDENTITY_PROVIDERS'], region),
            ],
        ))

        self.sessions_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=sessions_lambda_role,
            environment={
                "ESPUSER_REFRESH_SECRET_PARAM": USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
                # Our own return URL, handed to the upstream provider at sign-out. The
                # provider matches it exactly against what it has registered, so it is ONE
                # URL for the whole deployment however many products exist -- the same shape
                # as the single federation callback the login leg registers. Unset disables
                # the upstream hop rather than sending a provider somewhere wrong.
                "ESPUSER_LOGOUT_DONE_URL": f"{common_resources.esp_user_api_url}/oauth2/logout/done",
            },
        )

        # ── /v1/user/sessions and /v1/user/sessions/{sessionId} ──────────────────────
        v1_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "v1",
            api_id=common_resources.esp_user_api_id,
        )
        user_id = get_or_create_api_resource(
            self, "V1UserResource", common_resources, v1_id, "user",
            api_id=common_resources.esp_user_api_id,
        )
        sessions_id = get_or_create_api_resource(
            self, "V1UserSessionsResource", common_resources, user_id, "sessions",
            api_id=common_resources.esp_user_api_id,
        )
        sessions_sid_id = get_or_create_api_resource(
            self, "V1UserSessionsSidResource", common_resources, sessions_id, "{sessionId}",
            api_id=common_resources.esp_user_api_id,
        )

        for resource_id, label, verbs in (
            (sessions_id, "Sessions", ("GET", "DELETE")),
            (sessions_sid_id, "SessionsSid", ("DELETE",)),
        ):
            for verb in verbs:
                create_cfn_api_method(
                    self, f"V1User{label}{verb.title()}Method", common_resources,
                    resource_id, verb, self.sessions_function,
                    authorization_type="NONE",  # the bearer token is the credential; the lambda verifies it
                    api_id=common_resources.esp_user_api_id,
                )
            add_cors_options(
                self, f"V1User{label}OptionsMethod", common_resources,
                resource_id, allowed_methods=list(verbs),
                api_id=common_resources.esp_user_api_id,
            )

        # ── /oauth2/logout ───────────────────────────────────────────────────────────
        # GET only: __Host-esp_session is SameSite=Lax, so a cross-site POST would carry no session and end nothing.
        oauth2_id = get_or_create_api_resource(
            self, "OAuth2Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "oauth2",
            api_id=common_resources.esp_user_api_id,
        )
        logout_id = get_or_create_api_resource(
            self, "OAuth2LogoutResource", common_resources, oauth2_id, "logout",
            api_id=common_resources.esp_user_api_id,
        )
        create_cfn_api_method(
            self, "OAuth2LogoutGetMethod", common_resources,
            logout_id, "GET", self.sessions_function,
            authorization_type="NONE",
            api_id=common_resources.esp_user_api_id,
        )
        add_cors_options(
            self, "OAuth2LogoutOptionsMethod", common_resources,
            logout_id, allowed_methods=["GET"],
            api_id=common_resources.esp_user_api_id,
        )

        # ── /oauth2/logout/done ──────────────────────────────────────────────────────
        # Where the upstream provider hands the browser back. This is the ONE sign-out URL
        # a deployment registers with its provider, mirroring the single federation callback
        # the login leg registers: the provider knows ESP User and never needs to learn about
        # the products behind it. GET only, and unauthenticated -- the browser arrives from
        # the provider carrying nothing but the short-lived destination cookie.
        logout_done_id = get_or_create_api_resource(
            self, "OAuth2LogoutDoneResource", common_resources, logout_id, "done",
            api_id=common_resources.esp_user_api_id,
        )
        create_cfn_api_method(
            self, "OAuth2LogoutDoneGetMethod", common_resources,
            logout_done_id, "GET", self.sessions_function,
            authorization_type="NONE",
            api_id=common_resources.esp_user_api_id,
        )
        add_cors_options(
            self, "OAuth2LogoutDoneOptionsMethod", common_resources,
            logout_done_id, allowed_methods=["GET"],
            api_id=common_resources.esp_user_api_id,
        )
