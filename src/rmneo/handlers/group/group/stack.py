# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
    aws_dynamodb as dynamodb,
    aws_iam as iam,
    RemovalPolicy,
    Stack
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
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, NODE_LIFECYCLE_HOOKS, FUNCTION_NAMES
from arn_utils import get_table_arn, get_index_arn, get_topic_arn
from aws_cdk import Aws

class GroupAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, node_data_reset_function=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role
        function_name = "group"
        group_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        group_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:*"],
            resources=[
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region),
                get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region),
                get_table_arn(TABLE_NAMES['SHARING_REQUESTS'], region),
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], region),
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
                get_table_arn(TABLE_NAMES['AUTOMATIONS'], region),
                get_table_arn(TABLE_NAMES['USER_DETAILS'], region),
                # Sharing resolves the invitee's user name (email / E.164 phone)
                # to a user id through these GSIs. A table ARN does not cover its
                # indexes, so both must be granted or every share-by-name call
                # fails with AccessDenied at runtime.
                get_index_arn('USER_DETAILS_EMAIL', region),
                get_index_arn('USER_DETAILS_PHONE', region)
            ]
        ))
        group_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:Publish"],
            resources=[get_topic_arn('rainmaker/nodes/*/from_cloud', region)]
        ))
        group_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:GetThingShadow", "iot:DeleteThingShadow", "iot:UpdateThingShadow", "iot:UpdateThing"],
            resources=["*"]
        ))
        # Node-left-group lifecycle hook: the DELETE /v1/groups/{groupId}/
        # nodes/{nodeId} path runs nodelifecycle.OnNodeLeftGroup after disassoc.
        # Core invokes the hook Lambda by its conventional name (a core-owned
        # constant; no CFN cross-stack dep, no dependency on any downstream
        # module). The Lambda is created by a separately-deployed downstream
        # stack; if absent, the invoke no-ops.
        group_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{Aws.REGION}:{Aws.ACCOUNT_ID}:function:"
                f"{NODE_LIFECYCLE_HOOKS['NODE_LEFT_GROUP']}",
            ],
        ))
        # Group-membership percolation: DELETE /v1/groups/{groupId}/nodes/{nodeId}
        # async-invokes the notifications Lambda so Alexa/GVA drop the node
        # (node.EmitGroupMembershipChangeAsync). Referenced by deterministic name
        # (the notifications Lambda is created later in rmng-core).
        group_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{Aws.REGION}:{Aws.ACCOUNT_ID}:function:"
                f"{FUNCTION_NAMES['NOTIFICATIONS']}",
            ],
        ))

        # Lambda Function
        environment = {}
        environment["NODE_DATA_RESET_FUNCTION_NAME"] = node_data_reset_function.function_name
        environment["NODE_LEFT_GROUP_HOOK_FUNCTION_NAME"] = \
            NODE_LIFECYCLE_HOOKS["NODE_LEFT_GROUP"]
        environment["NOTIFICATIONS_FUNCTION_NAME"] = FUNCTION_NAMES["NOTIFICATIONS"]

        self.group_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=group_lambda_role,
            environment=environment,
        )

        # Grant permission to invoke the node data reset Lambda
        node_data_reset_function.grant_invoke(self.group_function)
        common_resources.group_function = self.group_function

        # Create group service Lambda role
        service_function_name = "group_service"
        group_service_lambda_role = create_base_lambda_role(self, service_function_name, common_resources)
        group_service_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:*"],
            resources=[
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region),
                get_table_arn(TABLE_NAMES['AUTOMATIONS'], region)
            ]
        ))

        # Create group service Lambda function
        self.group_service_function = create_lambda_function(
            self, service_function_name,
            common_resources,
            lambda_role=group_service_lambda_role,
        )


        # Create API Gateway resources using CFn to avoid cyclic dependencies
        # Create /v1/groups (share v1 if already created)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        # Create base groups resource (primary owner): /v1/groups
        group_resource_id = get_or_create_api_resource(
            self, "GroupResource", common_resources,
            v1_parent_id, "groups"
        )
        
        create_cfn_api_method(
            self, "GroupPostMethod", common_resources,
            group_resource_id, "POST", self.group_function
        )
        
        create_cfn_api_method(
            self, "GroupGetMethod", common_resources,
            group_resource_id, "GET", self.group_function
        )
        
        add_cors_options(
            self, "GroupOptionsMethod", common_resources,
            group_resource_id, allowed_methods=["GET", "POST"]
        )

        # Add group ID resource (primary owner)
        group_id_resource_id = get_or_create_api_resource(
            self, "GroupIdResource", common_resources,
            group_resource_id, "{groupId}"
        )
        
        create_cfn_api_method(
            self, "GroupIdDeleteMethod", common_resources,
            group_id_resource_id, "DELETE", self.group_function
        )
        
        create_cfn_api_method(
            self, "GroupIdPatchMethod", common_resources,
            group_id_resource_id, "PATCH", self.group_function
        )
        
        add_cors_options(
            self, "GroupIdOptionsMethod", common_resources,
            group_id_resource_id, allowed_methods=["DELETE", "PATCH"]
        )

        # /v1/groups/{groupId}/matter-nocs - Get User NOC for Matter groups
        matter_noc_resource_id = get_or_create_api_resource(
            self, "MatterNocResource", common_resources,
            group_id_resource_id, "matter-nocs"
        )

        create_cfn_api_method(
            self, "MatterNocPostMethod", common_resources,
            matter_noc_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "MatterNocOptionsMethod", common_resources,
            matter_noc_resource_id, allowed_methods=["POST"]
        )

        # /v1/groups/{groupId}/capabilities - Enable capabilities (e.g. convert to Matter fabric)
        group_capabilities_resource_id = get_or_create_api_resource(
            self, "GroupCapabilitiesResource", common_resources,
            group_id_resource_id, "capabilities"
        )

        create_cfn_api_method(
            self, "GroupCapabilitiesPostMethod", common_resources,
            group_capabilities_resource_id, "POST", self.group_function
        )

        add_cors_options(
            self, "GroupCapabilitiesOptionsMethod", common_resources,
            group_capabilities_resource_id, allowed_methods=["POST"]
        )

        # Add sharing-requests resource: /v1/groups/{groupId}/sharing-requests
        group_sharing_requests_resource_id = get_or_create_api_resource(
            self, "GroupSharingRequestsResource", common_resources,
            group_id_resource_id, "sharing-requests"
        )
        create_cfn_api_method(
            self, "GroupSharingRequestsPostMethod", common_resources,
            group_sharing_requests_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "GroupSharingRequestsOptionsMethod", common_resources,
            group_sharing_requests_resource_id, allowed_methods=["POST"]
        )

        # Add users resource: /v1/groups/{groupId}/users
        group_users_resource_id = get_or_create_api_resource(
            self, "GroupUsersResource", common_resources,
            group_id_resource_id, "users"
        )
        create_cfn_api_method(
            self, "GroupUsersGetMethod", common_resources,
            group_users_resource_id, "GET", self.group_function
        )
        add_cors_options(
            self, "GroupUsersOptionsMethod", common_resources,
            group_users_resource_id, allowed_methods=["GET"]
        )
        # Add user ID resource: /v1/groups/{groupId}/users/{userId}
        group_user_id_resource_id = get_or_create_api_resource(
            self, "GroupUserIdResource", common_resources,
            group_users_resource_id, "{userId}"
        )
        create_cfn_api_method(
            self, "GroupUserIdDeleteMethod", common_resources,
            group_user_id_resource_id, "DELETE", self.group_function
        )
        
        add_cors_options(
            self, "GroupUserIdOptionsMethod", common_resources,
            group_user_id_resource_id, allowed_methods=["DELETE"]
        )

        # Add subgroups resource: /v1/groups/{groupId}/subgroups (POST = create subgroup)
        subgroups_resource_id = get_or_create_api_resource(
            self, "SubgroupsResource", common_resources,
            group_id_resource_id, "subgroups"
        )
        create_cfn_api_method(
            self, "SubgroupsPostMethod", common_resources,
            subgroups_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "SubgroupsOptionsMethod", common_resources,
            subgroups_resource_id, allowed_methods=["POST"]
        )

        # Add service resource under group/{groupId}/service/{serviceName}
        service_base_id = get_or_create_api_resource(
            self, "ServiceBaseResource", common_resources,
            group_id_resource_id, "service"
        )
        
        service_name_resource_id = get_or_create_api_resource(
            self, "ServiceNameResource", common_resources,
            service_base_id, "{serviceName}"
        )
        
        create_cfn_api_method(
            self, "ServiceGetMethod", common_resources,
            service_name_resource_id, "GET", self.group_service_function
        )
        
        create_cfn_api_method(
            self, "ServicePostMethod", common_resources,
            service_name_resource_id, "POST", self.group_service_function
        )

        create_cfn_api_method(
            self, "ServiceDeleteMethod", common_resources,
            service_name_resource_id, "DELETE", self.group_service_function
        )

        add_cors_options(
            self, "ServiceOptionsMethod", common_resources,
            service_name_resource_id, allowed_methods=["GET", "POST", "DELETE"]
        )
        
        # Add resource ID path for supporting resource-specific operations
        resource_id_resource_id = get_or_create_api_resource(
            self, "ResourceIdResource", common_resources,
            service_name_resource_id, "{resourceId}"
        )
        
        create_cfn_api_method(
            self, "ResourceIdGetMethod", common_resources,
            resource_id_resource_id, "GET", self.group_service_function
        )
        
        create_cfn_api_method(
            self, "ResourceIdPutMethod", common_resources,
            resource_id_resource_id, "PUT", self.group_service_function
        )
        
        create_cfn_api_method(
            self, "ResourceIdDeleteMethod", common_resources,
            resource_id_resource_id, "DELETE", self.group_service_function
        )
        
        add_cors_options(
            self, "ResourceIdOptionsMethod", common_resources,
            resource_id_resource_id, allowed_methods=["GET", "PUT", "DELETE"]
        )

        # Add subgroup resource under subgroups: /v1/groups/{groupId}/subgroups/{subGroupId}
        subgroup_resource_id = get_or_create_api_resource(
            self, "SubgroupResource", common_resources,
            subgroups_resource_id, "{subGroupId}"
        )
        
        create_cfn_api_method(
            self, "SubgroupPatchMethod", common_resources,
            subgroup_resource_id, "PATCH", self.group_function
        )

        create_cfn_api_method(
            self, "SubgroupDeleteMethod", common_resources,
            subgroup_resource_id, "DELETE", self.group_function
        )
        
        add_cors_options(
            self, "SubgroupOptionsMethod", common_resources,
            subgroup_resource_id, allowed_methods=["PATCH", "DELETE"]
        )

        # Add sharing-requests resource: /v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests
        subgroup_sharing_requests_resource_id = get_or_create_api_resource(
            self, "SubgroupSharingRequestsResource", common_resources,
            subgroup_resource_id, "sharing-requests"
        )
        create_cfn_api_method(
            self, "SubgroupSharingRequestsPostMethod", common_resources,
            subgroup_sharing_requests_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "SubgroupSharingRequestsOptionsMethod", common_resources,
            subgroup_sharing_requests_resource_id, allowed_methods=["POST"]
        )

        # Add users resource: /v1/groups/{groupId}/subgroups/{subGroupId}/users
        subgroup_users_resource_id = get_or_create_api_resource(
            self, "SubgroupUsersResource", common_resources,
            subgroup_resource_id, "users"
        )
        create_cfn_api_method(
            self, "SubgroupUsersGetMethod", common_resources,
            subgroup_users_resource_id, "GET", self.group_function
        )
        add_cors_options(
            self, "SubgroupUsersOptionsMethod", common_resources,
            subgroup_users_resource_id, allowed_methods=["GET"]
        )
        # Add user ID resource: /v1/groups/{groupId}/subgroups/{subGroupId}/users/{userId}
        subgroup_user_id_resource_id = get_or_create_api_resource(
            self, "SubgroupUserIdResource", common_resources,
            subgroup_users_resource_id, "{userId}"
        )
        create_cfn_api_method(
            self, "SubgroupUserIdDeleteMethod", common_resources,
            subgroup_user_id_resource_id, "DELETE", self.group_function
        )

        add_cors_options(
            self, "SubgroupUserIdOptionsMethod", common_resources,
            subgroup_user_id_resource_id, allowed_methods=["DELETE"]
        )

        # Add nodes resource under subgroup: /v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}
        subgroup_nodes_resource_id = get_or_create_api_resource(
            self, "SubgroupNodesResource", common_resources,
            subgroup_resource_id, "nodes"
        )
        subgroup_node_resource_id = get_or_create_api_resource(
            self, "SubgroupNodeResource", common_resources,
            subgroup_nodes_resource_id, "{nodeId}"
        )
        
        create_cfn_api_method(
            self, "SubgroupNodePutMethod", common_resources,
            subgroup_node_resource_id, "PUT", self.group_function
        )

        create_cfn_api_method(
            self, "SubgroupNodeDeleteMethod", common_resources,
            subgroup_node_resource_id, "DELETE", self.group_function
        )

        add_cors_options(
            self, "SubgroupNodeOptionsMethod", common_resources,
            subgroup_node_resource_id, allowed_methods=["PUT", "DELETE"]
        )

        # Create /v1/sharing-requests (received, {requestId}/accept, {requestId}/reject)
        sharing_requests_resource_id = get_or_create_api_resource(
            self, "SharingRequestsResource", common_resources,
            v1_parent_id, "sharing-requests"
        )
        sharing_requests_received_resource_id = get_or_create_api_resource(
            self, "SharingRequestsReceivedResource", common_resources,
            sharing_requests_resource_id, "received"
        )
        create_cfn_api_method(
            self, "SharingRequestsReceivedGetMethod", common_resources,
            sharing_requests_received_resource_id, "GET", self.group_function
        )
        
        add_cors_options(
            self, "SharingRequestsReceivedOptionsMethod", common_resources,
            sharing_requests_received_resource_id, allowed_methods=["GET"]
        )
        sharing_requests_request_id_resource_id = get_or_create_api_resource(
            self, "SharingRequestsRequestIdResource", common_resources,
            sharing_requests_resource_id, "{requestId}"
        )
        sharing_requests_accept_resource_id = get_or_create_api_resource(
            self, "SharingRequestsAcceptResource", common_resources,
            sharing_requests_request_id_resource_id, "accept"
        )
        create_cfn_api_method(
            self, "SharingRequestsAcceptPostMethod", common_resources,
            sharing_requests_accept_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "SharingRequestsAcceptOptionsMethod", common_resources,
            sharing_requests_accept_resource_id, allowed_methods=["POST"]
        )
        sharing_requests_reject_resource_id = get_or_create_api_resource(
            self, "SharingRequestsRejectResource", common_resources,
            sharing_requests_request_id_resource_id, "reject"
        )
        create_cfn_api_method(
            self, "SharingRequestsRejectPostMethod", common_resources,
            sharing_requests_reject_resource_id, "POST", self.group_function
        )
        
        add_cors_options(
            self, "SharingRequestsRejectOptionsMethod", common_resources,
            sharing_requests_reject_resource_id, allowed_methods=["POST"]
        )
