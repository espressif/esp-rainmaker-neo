# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
)
from constructs import Construct
from src.rmneo.handlers.hello_world.hello_world.stack import HelloWorldMod
from app_common import CommonResources

class HelloWorldCore(Construct):
    """Core/compute resources for HelloWorld - Lambda function and API integration"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.hello_world_mod = HelloWorldMod(self, id, common_resources)
