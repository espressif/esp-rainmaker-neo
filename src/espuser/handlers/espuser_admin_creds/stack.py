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
    stable_logical_id,
)


class AdminCredsAPI(Construct):
    """Vends short-lived, scoped AWS credentials to the admin dashboard so it can
    read and adjust the espuser-owned post-deployment settings directly from the
    browser: SNS SMS sandbox status and its verified destination numbers, the SMS
    monthly spend limit, and the SES/SNS ServiceQuotas increases.

    SES is not reachable from here: mail is sent only by the OTP add-on, so no SES action is
    vended to the browser. The lambda assumes the admin-creds role and returns temp
    credentials narrowed by the inline session policy in
    espuser_admin_creds_main.go — the role's attached policy is the outer ceiling,
    the session policy the intersection the browser actually gets."""

    ADMIN_CREDS_ROLE_PURPOSE = "admin-creds-role"

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "espuser_admin_creds"

        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        admin_creds_role = self._create_admin_creds_role(lambda_role.role_arn, region, common_resources.prefix)

        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["sts:AssumeRole"],
            resources=[admin_creds_role.role_arn],
        ))

        # function_name is "espuser_admin_creds" to give the build target a distinct
        # basename (the Makefile keys on the _main.go basename), but the deployed
        # name is overridden so it isn't "espuser-espuser-...".
        self.admin_creds_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment={"ADMIN_CREDS_ROLE_ARN": admin_creds_role.role_arn},
            aws_function_name=f"{common_resources.prefix}admin-creds",
        )

        # POST /v1/admin/credentials behind the admin authorizer; the handler
        # additionally requires the custom:super_admin claim.
        v1_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "v1",
            api_id=common_resources.esp_user_api_id
        )
        admin_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_id, "admin", api_id=common_resources.esp_user_api_id
        )
        credentials_id = get_or_create_api_resource(
            self, "V1AdminCredentialsResource", common_resources,
            admin_id, "credentials", api_id=common_resources.esp_user_api_id
        )

        create_cfn_api_method(
            self, "AdminCredsPostMethod", common_resources,
            credentials_id, "POST", self.admin_creds_function,
            authorization_type="COGNITO_USER_POOLS",
            authorizer_id=common_resources.esp_admin_cognito_authorizer_id,
            api_id=common_resources.esp_user_api_id
        )

        add_cors_options(
            self, "AdminCredsOptionsMethod", common_resources, credentials_id,
            allowed_methods=["POST"], api_id=common_resources.esp_user_api_id
        )

    def _create_admin_creds_role(self, lambda_role_arn: str, region: str, prefix: str) -> iam.Role:
        """Bespoke role assumed by the admin_creds lambda. role_name is set
        explicitly and the logical ID pinned (aws-rules.mdc Table 1)."""
        admin_creds_role = iam.Role(
            self, "EspUserAdminCredsRole",
            role_name=f"{prefix}{self.ADMIN_CREDS_ROLE_PURPOSE}-{region}",
            assumed_by=iam.ArnPrincipal(lambda_role_arn),
        )
        admin_creds_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", self.ADMIN_CREDS_ROLE_PURPOSE))

        # Sandbox destination numbers are managed from the dashboard because verifying one is a
        # self-contained step; leaving a sandbox or moving a spend limit stays an AWS-side action,
        # so no account-level write is granted.
        admin_creds_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ses:GetAccount",
                "sns:GetSMSAttributes",
                "sns:GetSMSSandboxAccountStatus",
                "sns:ListSMSSandboxPhoneNumbers",
                "sns:CreateSMSSandboxPhoneNumber",
                "sns:VerifySMSSandboxPhoneNumber",
                "sns:DeleteSMSSandboxPhoneNumber",
                "sms-voice:DescribeSpendLimits",
                "sms-voice:DescribeAccountAttributes",
                "sms-voice:DescribeVerifiedDestinationNumbers",
                "sms-voice:CreateVerifiedDestinationNumber",
                "sms-voice:SendDestinationNumberVerificationCode",
                "sms-voice:VerifyDestinationNumber",
                "sms-voice:DeleteVerifiedDestinationNumber",
            ],
            resources=["*"],
        ))
        return admin_creds_role
