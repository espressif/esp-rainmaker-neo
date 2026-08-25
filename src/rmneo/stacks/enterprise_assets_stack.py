# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import CfnOutput, Stack
from aws_cdk import aws_iam as iam
from constructs import Construct
from app_common import CommonResources

# The Espressif-owned central bucket every deployment loads enterprise features from
# (one directory per feature under admin-dashboard/features/, discovered by listing).
# The dashboard embeds the same constants in src/enterprise/loader/enterprise-bucket.ts.
# Prototype values: the stand-in Espressif account is `Insights Test 1` (311057549898).
ENTERPRISE_ASSETS_BUCKET = "rmng-enterprise-assets-311057549898"
ENTERPRISE_DASHBOARD_PREFIX = "admin-dashboard/"


class EnterpriseAssetsStack(Stack):
    """Customer-side half of the enterprise runtime-loading handshake
    (spec: dashboard_modules/docs/enterprise-features-runtime-loading.md
    in the enterprise superproject).

    Cross-account S3 reads need both sides to allow: Espressif's central-bucket
    policy allow-lists paying customers' browser roles (the entitlement registry),
    and this stack grants that browser role `GetObject`/`ListBucket` on the central
    bucket. Deploying this stack is the customer-side "enterprise on" switch; a
    community deployment without it gets AccessDenied and the dashboard silently
    stays in community mode.

    Prototype placement: this stack lives in the open repo for now and is expected
    to move unchanged into the esp-rainmaker-neo-enterprise superproject as an
    auto-discovered addon module (`../addon_modules`, register(ctx)). The bucket,
    its policy, and the manifest-signing KMS key live in Espressif's account (see
    dashboard_modules/scripts/provision.mjs in that superproject).
    """

    def __init__(self, scope: Construct, construct_id: str,
                 common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # The admin browser role is owned by rmng-base; importing it by its conventional name keeps enterprise knowledge out of the mandatory open stack, and destroying this stack cleanly detaches the grant.
        prefix = common_resources.prefix
        admin_browser_role = iam.Role.from_role_name(
            self, "AdminBrowserRole",
            f"{prefix}admin-cognito-identity-role-{self.region}",
        )
        iam.Policy(
            self, "EnterpriseAssetsReadPolicy",
            policy_name=f"{prefix}enterprise-assets-read-{self.region}",
            statements=[
                iam.PolicyStatement(
                    actions=["s3:GetObject"],
                    resources=[
                        f"arn:aws:s3:::{ENTERPRISE_ASSETS_BUCKET}/{ENTERPRISE_DASHBOARD_PREFIX}*",
                    ],
                ),
                # Listing powers feature discovery; the prefix condition mirrors the
                # central bucket policy's — neither side grants a full bucket listing.
                iam.PolicyStatement(
                    actions=["s3:ListBucket"],
                    resources=[f"arn:aws:s3:::{ENTERPRISE_ASSETS_BUCKET}"],
                    conditions={
                        "StringLike": {"s3:prefix": f"{ENTERPRISE_DASHBOARD_PREFIX}*"},
                    },
                ),
            ],
        ).attach_to_role(admin_browser_role)

        # Informational: which central bucket this deployment's grant points at.
        CfnOutput(self, "EnterpriseCentralBucket", value=ENTERPRISE_ASSETS_BUCKET)
