# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from src.rmneo.handlers.node.node_assoc.stack import AssociateNodeAPI
from src.rmneo.handlers.node.node_reset.stack import NodeDataResetLambda
from src.rmneo.handlers.node.node_conn.stack import PresenceEventHandlerAPI
from src.rmneo.handlers.node.node_to_cloud.stack import PublishInputEventHandlerAPI
from src.rmneo.handlers.node.node_indexed_params.stack import NodeShadowUpdateToDB
from src.rmneo.handlers.node.node_tags.stack import UserNodeTagsAPI

class NodeCore(Construct):
    """Core/compute resources for Node service - Lambda functions and API integrations"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, node_data_reset_function=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create AssociateNodeAPI (Lambda function and API integration)
        self.associate_node_api = AssociateNodeAPI(
            self, "AssociateNodeAPI", common_resources=common_resources,
            node_data_reset_function=node_data_reset_function,
        )

        # Create PresenceEventHandlerAPI (Lambda function and IoT rule)
        self.presence_event_handler_api = PresenceEventHandlerAPI(self, "PresenceEventHandlerAPI", common_resources=common_resources)

        # Create PublishInputEventHandlerAPI (Lambda function and IoT rule)
        self.publish_input_event_handler_api = PublishInputEventHandlerAPI(self, "PublishInputEventHandlerAPI", common_resources=common_resources)

        # Create NodeShadowUpdateToDB (Lambda function and IoT rule)
        NodeShadowUpdateToDB(self, "NodeShadowUpdateToDB", common_resources=common_resources)

        # Create UserNodeTagsAPI Lambda (API routes are wired in ServiceCore
        # which owns the /v1/groups/{groupId}/nodes/{nodeId} resource hierarchy)
        self.user_node_tags_api = UserNodeTagsAPI(self, "UserNodeTagsAPI", common_resources=common_resources)

