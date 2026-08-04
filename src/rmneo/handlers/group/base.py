# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_dynamodb as dynamodb,
    Aws,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, INDEX_NAMES

class GroupBase(Construct):
    """Base/infrastructure resources for Group service - DynamoDB tables"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create DynamoDB table for groups
        self.groups_table = ManagedTable(
            self, "GroupsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['GROUPS'],
            partition_key=dynamodb.Attribute(name="group_id", type=dynamodb.AttributeType.STRING),
            sort_key=dynamodb.Attribute(name="sub_group_id", type=dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Group Device Mapping Table
        self.group_device_mapping_table = ManagedTable(
            self, "GroupDeviceMappingTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['GROUP_DEVICE_MAPPING'],
            partition_key=dynamodb.Attribute(name="group_id", type=dynamodb.AttributeType.STRING),
            sort_key=dynamodb.Attribute(name="node_id", type=dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Add a global secondary index on node_id
        self.group_device_mapping_table.add_global_secondary_index(
            index_name=INDEX_NAMES['GROUP_DEVICE_MAPPING_NODE_ID'],
            partition_key=dynamodb.Attribute(name="node_id", type=dynamodb.AttributeType.STRING),
            projection_type=dynamodb.ProjectionType.ALL
        )

        # Sharing Requests Table
        self.sharing_requests_table = ManagedTable(
            self, "SharingRequestsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['SHARING_REQUESTS'],
            partition_key=dynamodb.Attribute(name="user_id", type=dynamodb.AttributeType.STRING),
            sort_key=dynamodb.Attribute(name="sharing_request_id", type=dynamodb.AttributeType.STRING),
            time_to_live_attribute="expiration_time",
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Automations Table
        self.automations_table = ManagedTable(
            self, "AutomationsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['AUTOMATIONS'],
            partition_key=dynamodb.Attribute(name="group_id", type=dynamodb.AttributeType.STRING),
            sort_key=dynamodb.Attribute(name="automation_id", type=dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )
