# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    RemovalPolicy,
    aws_dynamodb as dynamodb,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, INDEX_NAMES

class NodeAdminBase(Construct):
    """Base/infrastructure resources for NodeAdmin service - DynamoDB tables"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create DynamoDB table for bulk_node_register_requests
        self.bulk_node_register_requests_table = ManagedTable(
            self,
            "BulkNodeRegisterRequestsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['NODE_REG_REQS'],
            partition_key=dynamodb.Attribute(
                name="request_id",
                type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # GSI to list all registration requests ordered by created_at (descending)
        self.bulk_node_register_requests_table.add_global_secondary_index(
            index_name=INDEX_NAMES['NODE_REG_REQS_LIST'],
            partition_key=dynamodb.Attribute(
                name="gsi_pk",
                type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="created_at",
                type=dynamodb.AttributeType.NUMBER
            ),
            projection_type=dynamodb.ProjectionType.ALL
        )

        # Table for per-node failure detail of bulk registration / update jobs.
        # Each failed node is its own item, partitioned by request_id, so the
        # 400 KB per-item limit never applies regardless of failure volume.
        # Uses ManagedTable for consistency with the sibling table above,
        # though it has no GSIs so the orchestrator wiring is a no-op.
        self.node_reg_failed_nodes_table = ManagedTable(
            self,
            "NodeRegFailedNodesTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['NODE_REG_FAILED_NODES'],
            partition_key=dynamodb.Attribute(
                name="request_id",
                type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="node_id",
                type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )
