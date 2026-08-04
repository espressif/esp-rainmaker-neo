# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Duration,
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
    aws_iam as iam,
    aws_s3 as s3,
)
from constructs import Construct
from app_common import CommonResources, create_lambda_function, create_cfn_api_method, get_or_create_api_resource, add_cors_options


class HelloWorldMod(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Lambda Function
        self.hello_world_function = create_lambda_function(self, "hello_world", common_resources)

        # Create API Gateway resources using CFn to avoid cyclic dependencies
        # Share v1 resource if already created
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        # Create hello resource under v1
        hello_resource_id = get_or_create_api_resource(
            self, "HelloResource", common_resources,
            v1_parent_id, "hello"
        )

        # Create method using CFn
        create_cfn_api_method(
            self, "HelloMethod", common_resources,
            hello_resource_id, "GET", self.hello_world_function
        )
        
        add_cors_options(
            self, "HelloOptionsMethod", common_resources,
            hello_resource_id, allowed_methods=["GET"]
        )

