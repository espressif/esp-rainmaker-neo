# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    aws_iam as iam,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, INDEX_NAMES
from arn_utils import get_table_arn, get_table_index_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES


class NodeGroupsAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create Lambda role with minimal permissions
        function_name = "admin_node_groups"
        admin_node_groups_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        region = Stack.of(self).region
        admin_node_groups_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_table_index_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], INDEX_NAMES['GROUP_DEVICE_MAPPING_NODE_ID'], region),
            ]
        ))

        admin_node_groups_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Create Lambda function
        self.admin_node_groups_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=admin_node_groups_lambda_role,
        )

        # Create API Gateway resources
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin"
        )

        nodes_resource_id = get_or_create_api_resource(
            self, "NodesResource", common_resources,
            v1_admin_parent_id, "nodes"
        )

        # Create /v1/admin/nodes/{nodeId}
        node_id_resource_id = get_or_create_api_resource(
            self, "NodeIdResource", common_resources,
            nodes_resource_id, "{nodeId}"
        )

        add_cors_options(
            self, "NodeIdOptionsMethod", common_resources,
            node_id_resource_id, allowed_methods=["GET"]
        )

        # Create /v1/admin/nodes/{nodeId}/groups
        groups_resource_id = get_or_create_api_resource(
            self, "GroupsResource", common_resources,
            node_id_resource_id, "groups"
        )

        create_cfn_api_method(
            self, "GroupsGetMethod", common_resources,
            groups_resource_id, "GET", self.admin_node_groups_function
        )

        add_cors_options(
            self, "GroupsOptionsMethod", common_resources,
            groups_resource_id, allowed_methods=["GET"]
        )
