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


class AdminCredsCore(Construct):
    """Vends short-lived, scoped AWS credentials to the admin dashboard so it can
    read and adjust the rmng-owned post-deployment settings directly from the
    browser: Lambda reserved/provisioned concurrency and per-product
    ServiceQuotas increases.

    Mirrors rmneo/handlers/admin/iot_event_mode: reached via AWS_IAM (SigV4) using the
    dashboard's identity-pool creds, super-admin resolved from the request
    identity. The lambda assumes the admin-creds role and returns temp credentials
    narrowed by the inline session policy in rmng_admin_creds_main.go — the role's
    attached policy is the outer ceiling, the session policy the intersection."""

    ADMIN_CREDS_ROLE_PURPOSE = "admin-creds-role"

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "rmng_admin_creds"

        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        admin_creds_role = self._create_admin_creds_role(lambda_role.role_arn, region, common_resources.prefix)

        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["sts:AssumeRole"],
            resources=[admin_creds_role.role_arn],
        ))

        # create_lambda_function -> common_api_policy already grants the Cognito
        # GetUser/AdminGetUser + JWKS reads that NewContextWithAPIRequest and
        # IsSuperAdmin need, so no extra grant is required here.
        # function_name is "rmng_admin_creds" to give the build target a distinct
        # basename (the Makefile keys on the _main.go basename), but the deployed
        # name is overridden to "{prefix}admin-creds" so it isn't "rmng-rmng-...".
        self.admin_creds_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment={"ADMIN_CREDS_ROLE_ARN": admin_creds_role.role_arn},
            aws_function_name=f"{common_resources.prefix}admin-creds",
        )

        # POST /v1/admin/credentials — AWS_IAM (SigV4) like iot-event-mode; the
        # handler enforces super-admin. /v1 and /v1/admin are shared via the
        # common_resources cache with the other admin constructs.
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1",
        )
        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin",
        )
        credentials_resource_id = get_or_create_api_resource(
            self, "CredentialsResource", common_resources,
            v1_admin_parent_id, "credentials",
        )

        create_cfn_api_method(
            self, "AdminCredsPostMethod", common_resources,
            credentials_resource_id, "POST", self.admin_creds_function,
        )

        add_cors_options(
            self, "AdminCredsOptionsMethod", common_resources,
            credentials_resource_id, allowed_methods=["POST"],
        )

    def _create_admin_creds_role(self, lambda_role_arn: str, region: str, prefix: str) -> iam.Role:
        """Bespoke role assumed by the admin_creds lambda. role_name is set
        explicitly and the logical ID pinned (aws-rules.mdc Table 1)."""
        admin_creds_role = iam.Role(
            self, "RmngAdminCredsRole",
            role_name=f"{prefix}{self.ADMIN_CREDS_ROLE_PURPOSE}-{region}",
            assumed_by=iam.ArnPrincipal(lambda_role_arn),
        )
        admin_creds_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", self.ADMIN_CREDS_ROLE_PURPOSE))

        # A read only: the page reports the limit and the operator raises it in Service Quotas.
        admin_creds_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:GetAccountSettings"],
            resources=["*"],
        ))
        return admin_creds_role
