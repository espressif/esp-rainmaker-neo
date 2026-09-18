# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""RMNG bridge base (storage/infra) stack.

Owns the bridge's durable, name-addressed resources:
  - rmng-bridge-children DynamoDB table (parent_node_id PK, child_node_id SK)
  - rmng-bridge-policy IoT policy (additions-only — attached alongside the
    base node policy on bridge certs)

Compute (Lambdas + IoT rules) lives in RMNGBridgeCoreStack. Core references
this stack's table + policy purely by name (no CFN cross-stack dep), mirroring
the rmng-base / rmng-core split. Core must therefore deploy after base so the
table + policy exist at runtime; the module entrypoint enforces that ordering
via add_dependency.
"""

import json
import os

from aws_cdk import (
    CfnOutput,
    RemovalPolicy,
    Stack,
    aws_dynamodb as dynamodb,
    aws_iot as iot,
)
from constructs import Construct

from app_common import (
    CommonResources,
    ManagedTable,
    stable_logical_id,
)
from src.bridge.stacks.base_res_constants import BRIDGE_RESOURCES


def _load_bridge_policy_document(region: str, account: str) -> dict:
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), "bridge_policy.json")
    with open(path) as f:
        rendered = f.read().replace("__REGION__", region).replace("__ACCOUNT__", account)
    return json.loads(rendered)


class RMNGBridgeBaseStack(Stack):
    """Storage/infra stack: bridge_children table + bridge IoT policy."""

    def __init__(
        self,
        scope: Construct,
        construct_id: str,
        common_resources: CommonResources,
        **kwargs,
    ) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = self.region
        account = self.account

        # ---------------------------------------------------------------
        # bridge_children DDB table
        # ---------------------------------------------------------------
        bridge_children_table = ManagedTable(
            self,
            "BridgeChildrenTable",
            common_resources=common_resources,
            table_name=BRIDGE_RESOURCES["BRIDGE_CHILDREN_TABLE"],
            partition_key=dynamodb.Attribute(
                name="parent_node_id", type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="child_node_id", type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # ---------------------------------------------------------------
        # Bridge IoT policy (additions-only, attached alongside the
        # default node policy at registration / via bridge_register.py).
        # ---------------------------------------------------------------
        bridge_policy = iot.CfnPolicy(
            self,
            "BridgePolicy",
            policy_name=BRIDGE_RESOURCES["BRIDGE_POLICY_NAME"],
            policy_document=_load_bridge_policy_document(region, account),
        )
        bridge_policy.override_logical_id(
            stable_logical_id("IoTPolicy", BRIDGE_RESOURCES["BRIDGE_POLICY_NAME"])
        )

        # ---------------------------------------------------------------
        # CfnOutputs — consumed by test/bridge_register.py and tests.
        # ---------------------------------------------------------------
        CfnOutput(self, "BridgePolicyName", value=BRIDGE_RESOURCES["BRIDGE_POLICY_NAME"])
        CfnOutput(self, "BridgeChildrenTableName", value=bridge_children_table.table_name)
