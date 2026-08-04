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
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn, get_index_arn, get_iot_thing_arn


class UserNodeTagsAPI(Construct):
    """Creates the user_node_tags Lambda function.

    API Gateway routes (/v1/groups/{groupId}/nodes/{nodeId}/tags) are wired
    in ServiceCore which owns the /v1/groups/{groupId}/nodes/{nodeId} resource hierarchy.
    """
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region

        # Create Lambda role
        function_name = "node_tags"
        user_node_tags_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # IoT shadow read/write for the indexed shadow (iparams)
        user_node_tags_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:GetThingShadow", "iot:UpdateThingShadow"],
            resources=[get_iot_thing_arn('*', region)]
        ))

        # DynamoDB permissions for group ownership verification and node-in-group
        # membership check (GROUP_DEVICE_MAPPING GetItem in GetGroupNode).
        user_node_tags_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:Query"],
            resources=[
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
            ]
        ))

        # GSI for group lookup
        user_node_tags_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region)]
        ))

        # Create Lambda function (API routes are wired in ServiceCore)
        self.user_node_tags_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=user_node_tags_lambda_role,
        )
