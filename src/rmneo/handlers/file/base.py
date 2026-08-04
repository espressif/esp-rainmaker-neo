# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from aws_cdk import aws_s3 as s3
from app_common import CommonResources, create_s3_bucket
from src.rmneo.stacks.base_res_constants import S3_BUCKETS

class FileBase(Construct):
    """Base/infrastructure resources for File service - S3 bucket"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create S3 bucket for files. Prefix MUST match S3_BUCKETS['FILES_BUCKET_NAME']
        # because the rmng_base_stack IAM policies for DeviceFileRole / IoT credential
        # provider are derived from that same constant via get_s3_bucket_resolved_name().
        self.files_bucket = create_s3_bucket(
            self,
            "NodeCertsBucket",
            common_resources,
            S3_BUCKETS['FILES_BUCKET_NAME'],
        )

        # CORS for browser-based firmware uploads from the admin dashboard
        self.files_bucket.add_cors_rule(
            allowed_methods=[s3.HttpMethods.PUT, s3.HttpMethods.POST, s3.HttpMethods.GET, s3.HttpMethods.DELETE],
            allowed_origins=["*"],
            allowed_headers=["*"],
            max_age=3600,
        )
