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
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES, USER_INDEX_NAMES, USER_SSM_PARAMETERS


class UserAuthAPI(Construct):
    """Native auth API: POST /v1/user/auth/{token, token/refresh, signup,
    signup/verify, password-recovery, password-recovery/confirmation, signout, password}.

    Authenticates against the end-user Cognito pool and mints ESP User's OWN tokens (converting;
    Cognito tokens never reach the client). See espuser/docs/en/specs/legacy-user-auth.md.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        account = Stack.of(self).account
        function_name = "user_auth"
        role = create_base_lambda_role(self, function_name, common_resources)

        role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-idp:InitiateAuth",
                "cognito-idp:SignUp",
                "cognito-idp:ConfirmSignUp",
                "cognito-idp:ResendConfirmationCode",
                "cognito-idp:ForgotPassword",
                "cognito-idp:ConfirmForgotPassword",
            ],
            # Unscoped because the provider's pool may be in another account, so there is no ARN of
            # ours to name. These six are unauthenticated Cognito operations — anyone holding the app
            # client id can call them — so the grant confers no privilege. Step 4 of
            # external-provider.md drops the SDK path and this statement with it.
            resources=["*"],
        ))

        role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter"],
            resources=[
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_JWKS'], region),
                get_ssm_parameter_arn(USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'], region),
            ],
        ))
        role.add_to_policy(iam.PolicyStatement(
            actions=["kms:Sign"],  # kid comes from the published JWKS, not kms:GetPublicKey (paid per call)
            resources=[common_resources.esp_user_kms_signing_key_arn],
        ))

        role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query", "dynamodb:UpdateItem", "dynamodb:DeleteItem"],
            resources=[
                get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region),
                get_table_index_arn(USER_TABLE_NAMES['USER_DETAILS'], USER_INDEX_NAMES['USER_DETAILS_EMAIL'], region),
                get_table_index_arn(USER_TABLE_NAMES['USER_DETAILS'], USER_INDEX_NAMES['USER_DETAILS_PHONE'], region),
                get_table_arn(USER_TABLE_NAMES['REFRESH_TOKENS'], region),
            ],
        ))

        # The password app client is resolved from the provider registry, not from deployment config.
        role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Scan"],
            resources=[get_table_arn(USER_TABLE_NAMES['IDENTITY_PROVIDERS'], region)],
        ))

        self.user_auth_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=role,
            environment={
                "ESPUSER_KMS_SIGNING_KEY_ARN": common_resources.esp_user_kms_signing_key_arn,
                "ESPUSER_REFRESH_SECRET_PARAM": USER_SSM_PARAMETERS['ESP_USER_REFRESH_SECRET'],
            },
        )

        api_id = common_resources.esp_user_api_id
        v1 = get_or_create_api_resource(self, "V1Resource", common_resources, common_resources.esp_user_api_root_resource_id, "v1", api_id=api_id)
        user = get_or_create_api_resource(self, "V1UserResource", common_resources, v1, "user", api_id=api_id)
        auth_res = get_or_create_api_resource(self, "V1UserAuthResource", common_resources, user, "auth", api_id=api_id)

        token = get_or_create_api_resource(self, "AuthTokenResource", common_resources, auth_res, "token", api_id=api_id)
        refresh = get_or_create_api_resource(self, "AuthTokenRefreshResource", common_resources, token, "refresh", api_id=api_id)
        signup = get_or_create_api_resource(self, "AuthSignupResource", common_resources, auth_res, "signup", api_id=api_id)
        signup_verify = get_or_create_api_resource(self, "AuthSignupVerifyResource", common_resources, signup, "verify", api_id=api_id)
        recovery = get_or_create_api_resource(self, "AuthRecoveryResource", common_resources, auth_res, "password-recovery", api_id=api_id)
        recovery_confirm = get_or_create_api_resource(self, "AuthRecoveryConfirmResource", common_resources, recovery, "confirmation", api_id=api_id)
        signout = get_or_create_api_resource(self, "AuthSignoutResource", common_resources, auth_res, "signout", api_id=api_id)
        password = get_or_create_api_resource(self, "AuthPasswordResource", common_resources, auth_res, "password", api_id=api_id)

        for verb_id, resource_id in (
            ("Token", token),
            ("TokenRefresh", refresh),
            ("Signup", signup),
            ("SignupVerify", signup_verify),
            ("Recovery", recovery),
            ("RecoveryConfirm", recovery_confirm),
            ("Signout", signout),
            ("Password", password),
        ):
            create_cfn_api_method(
                self, f"UserAuth{verb_id}PostMethod", common_resources,
                resource_id, "POST", self.user_auth_function,
                authorization_type="NONE",
                api_id=api_id,
            )
            add_cors_options(
                self, f"UserAuth{verb_id}OptionsMethod", common_resources,
                resource_id, allowed_methods=["POST"],
                api_id=api_id,
            )
