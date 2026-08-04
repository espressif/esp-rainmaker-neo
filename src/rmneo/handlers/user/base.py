# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_dynamodb,
    Aws,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable
from src.rmneo.handlers.user.assume_role.stack import AssumeRoleAPI
from src.rmneo.handlers.user.user_client.stack import RegisterClient
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, INDEX_NAMES

class UserBase(Construct):
    """Base/infrastructure resources for User service - DynamoDB tables"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # User Group Mapping Table
        self.user_group_mapping_table = ManagedTable(
            self, "UserGroupMapping",
            common_resources=common_resources,
            table_name=TABLE_NAMES['USER_GROUP_MAPPING'],
            partition_key=aws_dynamodb.Attribute(name="user_id", type=aws_dynamodb.AttributeType.STRING),
            sort_key=aws_dynamodb.Attribute(name="group_id", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Add a GSI with group_id as the partition key and only user_id in the projection
        self.user_group_mapping_table.add_global_secondary_index(
            index_name=INDEX_NAMES['USER_GROUP_MAPPING_GROUP_ID'],
            partition_key=aws_dynamodb.Attribute(name="group_id", type=aws_dynamodb.AttributeType.STRING),
            projection_type=aws_dynamodb.ProjectionType.INCLUDE,
            non_key_attributes=["user_id", "sub_entity_ids", "access_type"]
        )

        # User Endpoints Table — one row per (user_id, integration_endpoint) backing notification delivery. integration_endpoint is composed as `<integration_id>#<endpoint_id>` so a single user can have multiple endpoints per integration (e.g. multiple GCM devices, multiple linked Amazon accounts). RemovalPolicy.DESTROY so cdk deploy can replace the table when the sort-key schema changes (DynamoDB requires a replace for SK/PK changes; existing data is disposable).
        self.user_table = ManagedTable(
            self, "UserTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['USER_ENDPOINTS'],
            partition_key=aws_dynamodb.Attribute(name="user_id", type=aws_dynamodb.AttributeType.STRING),
            sort_key=aws_dynamodb.Attribute(name="integration_endpoint", type=aws_dynamodb.AttributeType.STRING),
            removal_policy=RemovalPolicy.DESTROY,
        )
