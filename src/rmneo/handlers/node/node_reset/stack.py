# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    Duration,
    Stack,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn, get_topic_arn


class NodeDataResetLambda(Construct):
    """Lambda invoked asynchronously during re-association to clean up old node data
    (triggers, schedules, timeseries, automations)."""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "node_reset"
        role = create_base_lambda_role(self, function_name, common_resources)

        # DynamoDB permissions for node_details, timeseries, and automations
        role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:*"],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
                get_table_arn(TABLE_NAMES['AUTOMATIONS'], region),
                get_table_arn(TABLE_NAMES['RAW_TS_DATA'], region),
                get_table_arn(TABLE_NAMES['PROCESSED_TS_DATA'], region),
            ]
        ))

        # IoT Publish for trigger/schedule device notifications
        role.add_to_policy(iam.PolicyStatement(
            actions=["iot:Publish"],
            resources=[get_topic_arn('rainmaker/nodes/*/from_cloud', region)]
        ))

        self.function = create_lambda_function(
            self, function_name, common_resources,
            lambda_role=role,
            timeout=Duration.seconds(900),  # Longer timeout for batch deletes
        )
