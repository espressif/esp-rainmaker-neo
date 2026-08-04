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
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options
)
from arn_utils import get_iot_thing_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES
from arn_utils import get_table_arn


class AdminNodeTagsAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region

        # Create Lambda role with minimal permissions
        function_name = "admin_node_tags"
        admin_node_tags_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # IoT shadow read/write for the indexed shadow (iparams)
        admin_node_tags_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:GetThingShadow", "iot:UpdateThingShadow"],
            resources=[get_iot_thing_arn('*', region)]
        ))

        # Cognito user lookup for admin verification
        admin_node_tags_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Create Lambda function
        self.admin_node_tags_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=admin_node_tags_lambda_role,
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

        # /v1/admin/nodes/{nodeId}
        node_id_resource_id = get_or_create_api_resource(
            self, "NodeIdResource", common_resources,
            nodes_resource_id, "{nodeId}"
        )

        # /v1/admin/nodes/{nodeId}/tags
        tags_resource_id = get_or_create_api_resource(
            self, "TagsResource", common_resources,
            node_id_resource_id, "tags"
        )

        create_cfn_api_method(
            self, "TagsGetMethod", common_resources,
            tags_resource_id, "GET", self.admin_node_tags_function
        )

        create_cfn_api_method(
            self, "TagsPutMethod", common_resources,
            tags_resource_id, "PUT", self.admin_node_tags_function
        )

        add_cors_options(
            self, "TagsOptionsMethod", common_resources,
            tags_resource_id, allowed_methods=["GET", "PUT"]
        )
