# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from aws_cdk import (
    aws_dynamodb as dynamodb,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable
from src.rmneo.handlers.node.node_assoc.stack import CreateAssocRequestsTable
from src.rmneo.handlers.node.node_indexed_params.stack import CreateNodeIndexedParamsTable

class NodeBase(Construct):
    """Base/infrastructure resources for Node service - DynamoDB tables"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create DynamoDB table for node_details
        self.node_details_table = ManagedTable(
            self,
            "NodeDetailsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['NODE_DETAILS'],
            partition_key=dynamodb.Attribute(
                name="node_id",
                type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Create DynamoDB table for nodes_online
        self.nodes_online_table = ManagedTable(
            self,
            "NodesOnlineTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['NODES_ONLINE'],
            partition_key=dynamodb.Attribute(
                name="clientId",
                type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Create the assoc_requests table (contains DynamoDB table creation)
        CreateAssocRequestsTable(self, "CreateAssocRequestsTable", common_resources=common_resources)

        # Create the node_indexed_params table (contains DynamoDB table creation)
        CreateNodeIndexedParamsTable(self, "CreateNodeIndexedParamsTable", common_resources=common_resources)
