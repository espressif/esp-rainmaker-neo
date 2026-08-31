# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import Stack, CfnOutput, CfnCondition, CustomResource
from constructs import Construct
from aws_cdk import (
    Aws,
    Duration,
    Fn,
    aws_cloudfront as cloudfront,
    aws_cloudfront_origins as origins,
    aws_iam as iam,
    aws_s3_deployment as s3deploy,
    custom_resources as cr,
)

from app_common import CommonResources, stable_logical_id, create_cloudfront_behavior, create_cloudfront_distribution, create_cloudfront_oac, create_s3_bucket, discover_cloudfront_custom_domain


BUCKET_NAME_PREFIX = "rmng-admin-dashboard"
LOCAL_FRONTEND_BUILD = "./dashboard/dist"
DEPLOYMENT_MEMORY_LIMIT_MIB = 1024


class AdminDashboardStack(Stack):
    """
    A single deployable unit for RMNG Admin Dashboard
    """

    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs):
        super().__init__(scope, construct_id, **kwargs)

        # The dashboard reads every runtime value from the published rmng-client-outputs.json, so
        # the deployed site only needs its URL. That file always lives in rmng-public-assets-<account>
        # (us-east-1), partitioned by <region>/ — the same location upload_rmng_outputs.py publishes to.
        client_outputs_url = Fn.join("", [
            "https://rmng-public-assets-",
            Aws.ACCOUNT_ID,
            ".s3.us-east-1.amazonaws.com/",
            Aws.REGION,
            "/rmng-client-outputs.json",
        ])

        # purpose + prefix reproduces BUCKET_NAME_PREFIX, so the logical ID is unchanged (no bucket replacement).
        frontend_bucket = create_s3_bucket(self, "AdminDashboardBucket", common_resources, "admin-dashboard")

        dashboard_oac = create_cloudfront_oac(
            self, "AdminDashboardS3OAC", name=f"{BUCKET_NAME_PREFIX}-oac")
        s3_origin = origins.S3BucketOrigin.with_origin_access_control(
            bucket=frontend_bucket,
            origin_access_control=dashboard_oac,
        )

        # TODO: per-path behaviors like app_assets.py — CACHING_OPTIMIZED applies to index.html too, so an edge can serve an entry point pointing at a deleted bundle.
        cloudfront_behavior = create_cloudfront_behavior(self, origin=s3_origin)

        # Cloudfront Error responses for Single Page Applications (SPAs)
        error_responses=[
            cloudfront.ErrorResponse(
                http_status=403,
                response_http_status=200,
                response_page_path="/index.html",
                ttl=Duration.seconds(0),
            ),
            cloudfront.ErrorResponse(
                http_status=404,
                response_http_status=200,
                response_page_path="/index.html",
                ttl=Duration.seconds(0),
            ),
        ]

        # Stable logical ID keeps the AWS-allocated DistributionId (and therefore the
        # <id>.cloudfront.net domain name) constant across refactors.
        cloudfront_distribution = create_cloudfront_distribution(
            self,
            "AdminDashboardDistribution",
            distribution_name="rmng-admin-dashboard",
            default_behavior=cloudfront_behavior,
            default_root_object="index.html",
            error_responses=error_responses,
        )
        # This will be injected in frontend.
        config_json = {"SERVER_URL": client_outputs_url}

        # cloudfront sources
        cloudfront_sources = [
            # 1. Compiled Frontend Code (Note: Run: npm run build in build directory)
            s3deploy.Source.asset(LOCAL_FRONTEND_BUILD),

            # 2. Dynamically generated config file injected at the root
            s3deploy.Source.json_data("config.json", config_json)
        ]
        

        # Deploy the frontend code and invalidate the cache.
        bucket_deployment = s3deploy.BucketDeployment(
            self,
            "DeploymentAdminDashboardFiles",
            sources            = cloudfront_sources,
            destination_bucket = frontend_bucket,
            distribution       = cloudfront_distribution,
            distribution_paths = ["/*"],
            memory_limit       = DEPLOYMENT_MEMORY_LIMIT_MIB,
        )

        deployment_cr = next(
            (child for child in bucket_deployment.node.children if isinstance(child, CustomResource)), None)
        if deployment_cr is None:
            raise RuntimeError("BucketDeployment 'DeploymentAdminDashboardFiles' has no CustomResource child")
        deployment_cr.node.default_child.override_logical_id(
            stable_logical_id("CustomCDKBucketDeploy",
                              f"rmng-admin-dashboard-{DEPLOYMENT_MEMORY_LIMIT_MIB}mib"))

        custom_domain, dashboard_host = discover_cloudfront_custom_domain(
            self, "AdminDashboardCustomDomain",
            name="admin-dashboard-custom-domain",
            distribution_id=cloudfront_distribution.distribution_id,
            default_host=cloudfront_distribution.distribution_domain_name,
        )

        CfnOutput(
            self,
            "FrontendUrl",
            value=f"https://{dashboard_host}",
            description="Admin Dashboard URL",
        )
