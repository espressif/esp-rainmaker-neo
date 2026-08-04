# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from src.rmneo.handlers.nodeadmin.admin_node_reg.stack import RegisterAPI
from src.rmneo.handlers.nodeadmin.admin_nodes_update.stack import UpdateAPI
from src.rmneo.handlers.nodeadmin.admin_node_groups.stack import NodeGroupsAPI
from src.rmneo.handlers.nodeadmin.admin_node_tags.stack import AdminNodeTagsAPI
from src.rmneo.handlers.nodeadmin.bulk_container.stack import RegisterContainer

class NodeAdminCore(Construct):
    """Core/compute resources for NodeAdmin service - ECS container and Lambda API"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create the bulk node register container (ECS/Fargate service)
        self.register_container = RegisterContainer(self, "RegisterContainer", common_resources=common_resources)

        # Create AdminNodesRegisterAPI (Lambda function and API integration)
        self.admin_nodes_register_api = RegisterAPI(
            self, "AdminNodesAPI",
            common_resources=common_resources,
            node_register_policy=self.register_container.node_register_policy.policy,
            container_params=self.register_container.container_params
        )

        # Create AdminNodesUpdateAPI: shares the bulk container task definition
        # with the registration flow; the JOB_TYPE env var passed at RunTask time
        # is what selects update behavior in the container.
        self.admin_nodes_update_api = UpdateAPI(
            self, "AdminNodesUpdateAPI",
            common_resources=common_resources,
            node_register_policy=self.register_container.node_register_policy.policy,
            container_params=self.register_container.container_params
        )

        # Create AdminNodeGroupsAPI (Lambda function and API integration)
        self.admin_node_groups_api = NodeGroupsAPI(
            self, "AdminNodeGroupsAPI",
            common_resources=common_resources,
        )

        # Create AdminNodeTagsAPI (Lambda function and API integration)
        self.admin_node_tags_api = AdminNodeTagsAPI(
            self, "AdminNodeTagsAPI",
            common_resources=common_resources,
        )
