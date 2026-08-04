# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_dynamodb as dynamodb,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources
from gsi_infra import ManagedTable
from src.rmneo.stacks.base_res_constants import TABLE_NAMES


class AdminConfigBase(Construct):
    """Base/infrastructure resources for shared admin runtime configuration.

    Provisions the `rmng-admin-configs` DynamoDB table — a single-table store
    for runtime-set admin configuration that needs to survive CloudFormation
    redeploys (the moral equivalent of Terraform's lifecycle.ignore_changes
    for AWS CFN).

    Each runtime-flippable feature picks its own opaque `config_key` (the
    only schema constraint), stores whatever attributes it needs under that
    key, and provides its own applier lambda that reads the row and
    restores the live state on every stack update — see
    `docs/en/specs/iot_event_mode.md` §4.4 for the full pattern.

    Current consumers:
      - iot_event_mode (config_key="iot_event_mode")
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        self.admin_config_table = ManagedTable(
            self, "AdminConfigTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['ADMIN_CONFIG'],
            partition_key=dynamodb.Attribute(
                name="config_key",
                type=dynamodb.AttributeType.STRING,
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )
