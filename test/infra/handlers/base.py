# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_dynamodb as dynamodb,
    RemovalPolicy
)
from app_common import CommonResources
from gsi_infra import ManagedTable # type: ignore
from constructs import Construct
from test.infra.stacks.test_constants import TABLE_NAMES

class WebhookBase(Construct):

    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources) -> None:
        super().__init__(scope, construct_id)

        # expires_at drives DynamoDB TTL. It lags (deletes up to ~48h late), so the
        # Go readers also enforce expiry explicitly; TTL is only cleanup.
        ManagedTable(
            self,
            "WebhookTestTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES["TEST_WEBHOOK_TABLE"],
            partition_key=dynamodb.Attribute(
                name="PK",
                type=dynamodb.AttributeType.STRING,
            ),
            sort_key=dynamodb.Attribute(
                name="SK",
                type=dynamodb.AttributeType.STRING,
            ),
            time_to_live_attribute="expires_at",
            removal_policy=RemovalPolicy.DESTROY,
        )
