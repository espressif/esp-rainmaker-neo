# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    Stack,
)
from constructs import Construct
from app_common import CommonResources, create_container
from src.rmneo.stacks.base_res_constants import S3_BUCKETS, TABLE_NAMES
from arn_utils import get_s3_object_arn, get_table_arn, get_iot_thing_arn, get_s3_bucket_resolved_name, get_kvs_channel_arn

class CreateNodeRegisterPolicy(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Node Register Policy
        self.policy = iam.Policy(
            self,
            "NodeRegisterPolicy",
            statements=[
                iam.PolicyStatement(
                    actions=[
                        "iot:RegisterCertificateWithoutCA",
                        "iot:UpdateCertificate",
                        "iot:DeleteCertificate",
                        "iot:DescribeCertificate",
                        "iot:CreateThing",
                        "iot:DeleteThing",
                        "iot:AttachThingPrincipal",
                        "iot:DetachThingPrincipal",
                        "iot:ListThingPrincipals",
                        "iot:AddThingToThingGroup",
                        "iot:AttachPolicy",
                        "iot:UpdateThing",
                        "iot:UpdateThingShadow"
                    ],
                    resources=["*"]
                ),
                iam.PolicyStatement(
                    actions=[
                        "dynamodb:GetItem",
                        "dynamodb:PutItem",
                        "dynamodb:UpdateItem"
                    ],
                    resources=[get_table_arn(TABLE_NAMES['NODE_REG_REQS'], region)]
                ),
                iam.PolicyStatement(
                    actions=[
                        "dynamodb:BatchWriteItem",
                        "dynamodb:Query"
                    ],
                    resources=[get_table_arn(TABLE_NAMES['NODE_REG_FAILED_NODES'], region)]
                ),
                iam.PolicyStatement(
                    actions=[
                        "dynamodb:GetItem",
                        "dynamodb:PutItem"
                    ],
                    resources=[get_table_arn(TABLE_NAMES['NODE_DETAILS'], region)]
                ),
                iam.PolicyStatement(
                    actions=["iot:UpdateThingShadow"],
                    resources=[
                        get_iot_thing_arn('*', region)
                    ]
                ),
                iam.PolicyStatement(
                    actions=[
                        # GetObject reads the input CSV; PutObject writes the
                        # cert-bearing failed-rows CSV at end-of-job (§3.5.5).
                        "s3:GetObject",
                        "s3:PutObject"
                    ],
                    resources=[get_s3_object_arn(get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], region, stack_prefix=common_resources.prefix))]
                ),
                iam.PolicyStatement(
                    actions=[
                        "kinesisvideo:CreateSignalingChannel",
                        "kinesisvideo:DescribeSignalingChannel",
                    ],
                    resources=[get_kvs_channel_arn("rmng-v1-*", region)]
                ),
                # Node-register lifecycle hook: the bulk container's registration
                # path synchronously invokes the optional node-register hook by
                # convention name (no-op if not deployed).
                iam.PolicyStatement(
                    actions=["lambda:InvokeFunction"],
                    resources=[
                        f"arn:aws:lambda:{region}:{Stack.of(self).account}:function:rmng-node-register-hook",
                    ]
                ),
            ]
        )

class RegisterContainer(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create Node Register Policy
        self.node_register_policy = CreateNodeRegisterPolicy(self, "CreateNodeRegisterPolicy", common_resources=common_resources)

        # Create Bulk Node Container using the generic function
        self.container_params = create_container(
            self,
            "BulkNodeRegister",
            common_resources,
            policy=self.node_register_policy.policy,
            ecs_container_name="bulk-node-register",
            binary_name="bulk_container",
        )
