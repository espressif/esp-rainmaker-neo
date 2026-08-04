# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda,
    aws_apigateway,
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
    add_cors_options
)
from src.rmneo.stacks.base_res_constants import S3_BUCKETS
from arn_utils import get_s3_bucket_arn, get_s3_object_arn, get_s3_bucket_resolved_name

class FileCore(Construct):
    """Core/compute resources for File service - Lambda function and API integration"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)
        
        region = Stack.of(self).region
        # Create Lambda role with S3 permissions
        bucket_name = get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], region, stack_prefix=common_resources.prefix)
        function_name = "admin_files"
        file_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        file_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "s3:PutObject",
                "s3:GetObject",
                "s3:PutObjectAcl"
            ],
            resources=[
                get_s3_object_arn(bucket_name)
            ]
        ))

        file_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "s3:ListBucket",
                "s3:GetBucketLocation"
            ],
            resources=[
                get_s3_bucket_arn(bucket_name)
            ]
        ))
        
        # Create Lambda Function
        self.file_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=file_lambda_role,
            environment={
                "FILE_BUCKET_NAME": bucket_name
            }
        )

        # Create /v1 (share v1 if already created)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        # Create /v1/admin (share with other admin endpoints)
        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin"
        )

        # Create /v1/admin/files
        files_resource_id = get_or_create_api_resource(
            self, "AdminFilesResource", common_resources,
            v1_admin_parent_id, "files"
        )

        # Create /v1/admin/files/upload-urls
        upload_urls_resource_id = get_or_create_api_resource(
            self, "AdminUploadUrlsResource", common_resources,
            files_resource_id, "upload-urls"
        )

        # Create /v1/admin/file-templates
        file_templates_resource_id = get_or_create_api_resource(
            self, "AdminFileTemplatesResource", common_resources,
            v1_admin_parent_id, "file-templates"
        )

        # Create /v1/admin/file-templates/{templateType}
        template_type_resource_id = get_or_create_api_resource(
            self, "AdminTemplateTypeResource", common_resources,
            file_templates_resource_id, "{templateType}"
        )

        # Create methods using CFn
        create_cfn_api_method(
            self, "UploadUrlsMethod", common_resources,
            upload_urls_resource_id, "POST", self.file_function
        )
        
        add_cors_options(
            self, "UploadUrlsOptionsMethod", common_resources,
            upload_urls_resource_id, allowed_methods=["POST"]
        )

        create_cfn_api_method(
            self, "FileTemplatesMethod", common_resources,
            template_type_resource_id, "GET", self.file_function
        )
        
        add_cors_options(
            self, "FileTemplatesOptionsMethod", common_resources,
            template_type_resource_id, allowed_methods=["GET"]
        )
