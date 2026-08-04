# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
from aws_cdk import (
    aws_lambda as lambda_,
    aws_iam as iam,
    custom_resources as cr,
    Stack
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options,
    stable_logical_id,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, IOT_RESOURCES, S3_BUCKETS
from arn_utils import get_table_arn, get_iam_role_arn, get_s3_bucket_resolved_name

class AssumeRoleAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Get the region from the stack
        region = Stack.of(self).region
        iot_user_role_name = f"{IOT_RESOURCES['NODE_ROLE_NAME']}-{region}"

        function_name = "assume_role"
        assume_role_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["sts:AssumeRole"],
            resources=[get_iam_role_arn(iot_user_role_name)]
        ))
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["sts:TagSession"],
            resources=["*"]
        ))
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iam:GetRole"],
            resources=["*"]
        ))
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region)]
        ))
        # Admin assume_role needs access to groups table
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:Query"],
            resources=[get_table_arn(TABLE_NAMES['GROUPS'], region)]
        ))
        # Admin assume_role needs access to user_details table for super admin check
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:DescribeTable"],
            resources=[get_table_arn(TABLE_NAMES['USER_DETAILS'], region)]
        ))
        # Per-node assume-role authorization (authorizeNodeAccess) does a point
        # GetItem on group_device_mapping to confirm the node belongs to the
        # group; the index Query backs node-ID lookups for the session policy.
        assume_role_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:Query"],
            resources=[
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region) + "/index/*"
            ]
        ))
        
        # Lambda Function
        self.assume_role_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=assume_role_lambda_role,
            environment={
                "IOT_USER_ROLE_ARN": get_iam_role_arn(iot_user_role_name),
                "FILES_BUCKET_NAME": get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], region, stack_prefix=common_resources.prefix)
            }
        )

        # Create API Gateway resources: /v1/assumed-roles (share v1 if already created)
        parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        assumed_roles_resource_id = get_or_create_api_resource(
            self, "AssumedRolesResource", common_resources,
            parent_id, "assumed-roles"
        )

        create_cfn_api_method(
            self, "AssumeRoleMethod", common_resources,
            assumed_roles_resource_id, "POST", self.assume_role_function
        )

        add_cors_options(
            self, "AssumeRoleOptionsMethod", common_resources,
            assumed_roles_resource_id, allowed_methods=["POST"]
        )

        # Admin-scoped assume-role endpoints carry the target group/subgroup as
        # path params, mirroring the existing group/subgroup API convention.
        # The /v1/groups, /{groupId}, /subgroups, /{subGroupId} resources are
        # shared via the common_resources cache with GroupAPI (same stack), so
        # get_or_create reuses them rather than creating duplicates.
        groups_resource_id = get_or_create_api_resource(
            self, "GroupsResource", common_resources,
            parent_id, "groups"
        )
        group_id_resource_id = get_or_create_api_resource(
            self, "GroupIdResource", common_resources,
            groups_resource_id, "{groupId}"
        )

        # /v1/groups/{groupId}/assumed-roles (admin: scope to a specific group)
        group_assumed_roles_resource_id = get_or_create_api_resource(
            self, "GroupAssumedRolesResource", common_resources,
            group_id_resource_id, "assumed-roles"
        )
        create_cfn_api_method(
            self, "GroupAssumeRoleMethod", common_resources,
            group_assumed_roles_resource_id, "POST", self.assume_role_function
        )
        add_cors_options(
            self, "GroupAssumeRoleOptionsMethod", common_resources,
            group_assumed_roles_resource_id, allowed_methods=["POST"]
        )

        # /v1/groups/{groupId}/nodes/{nodeId}/assumed-roles (user: per-node S3/KVS
        # credentials). The /nodes, /{nodeId} resources are shared via
        # common_resources with the nodes API, so get_or_create reuses them.
        nodes_resource_id = get_or_create_api_resource(
            self, "NodesResource", common_resources,
            group_id_resource_id, "nodes"
        )
        node_id_resource_id = get_or_create_api_resource(
            self, "NodeIdResource", common_resources,
            nodes_resource_id, "{nodeId}"
        )
        node_assumed_roles_resource_id = get_or_create_api_resource(
            self, "NodeAssumedRolesResource", common_resources,
            node_id_resource_id, "assumed-roles"
        )
        create_cfn_api_method(
            self, "NodeAssumeRoleMethod", common_resources,
            node_assumed_roles_resource_id, "POST", self.assume_role_function
        )
        add_cors_options(
            self, "NodeAssumeRoleOptionsMethod", common_resources,
            node_assumed_roles_resource_id, allowed_methods=["POST"]
        )

        # /v1/groups/{groupId}/subgroups/{subGroupId}/assumed-roles (admin: scope to a subgroup)
        subgroups_resource_id = get_or_create_api_resource(
            self, "SubgroupsResource", common_resources,
            group_id_resource_id, "subgroups"
        )
        subgroup_id_resource_id = get_or_create_api_resource(
            self, "SubgroupResource", common_resources,
            subgroups_resource_id, "{subGroupId}"
        )
        subgroup_assumed_roles_resource_id = get_or_create_api_resource(
            self, "SubgroupAssumedRolesResource", common_resources,
            subgroup_id_resource_id, "assumed-roles"
        )
        create_cfn_api_method(
            self, "SubgroupAssumeRoleMethod", common_resources,
            subgroup_assumed_roles_resource_id, "POST", self.assume_role_function
        )
        add_cors_options(
            self, "SubgroupAssumeRoleOptionsMethod", common_resources,
            subgroup_assumed_roles_resource_id, allowed_methods=["POST"]
        )

        # Update IoT User Role assume policy to allow this lambda role to assume it
        self._update_iot_user_role_assume_policy(assume_role_lambda_role.role_arn)

    def _update_iot_user_role_assume_policy(self, lambda_role_arn):
        """Update IoT User Role assume role policy to allow lambda roles to assume it"""
        policy_statements = [
            {
                "Effect": "Allow",
                "Principal": {
                    "Service": "iot.amazonaws.com"
                },
                "Action": "sts:AssumeRole"
            }
        ]
        
        policy_statements.append({
            "Effect": "Allow",
            "Principal": {
                "AWS": lambda_role_arn
            },
            "Action": "sts:AssumeRole"
        })
        
        policy_document = {
            "Version": "2012-10-17",
            "Statement": policy_statements
        }
        
        policy_document_str = json.dumps(policy_document)
        
        region = Stack.of(self).region
        iot_user_role_name = f"{IOT_RESOURCES['NODE_ROLE_NAME']}-{region}"
        
        update_assume_role = cr.AwsCustomResource(
            self, "UpdateIoTUserRoleAssumePolicy",
            on_create=cr.AwsSdkCall(
                service="IAM",
                action="updateAssumeRolePolicy",
                parameters={
                    "RoleName": iot_user_role_name,
                    "PolicyDocument": policy_document_str
                },
                physical_resource_id=cr.PhysicalResourceId.of("UpdateIoTUserRoleAssumePolicy")
            ),
            on_update=cr.AwsSdkCall(
                service="IAM",
                action="updateAssumeRolePolicy",
                parameters={
                    "RoleName": iot_user_role_name,
                    "PolicyDocument": policy_document_str
                },
                physical_resource_id=cr.PhysicalResourceId.of("UpdateIoTUserRoleAssumePolicy")
            ),
            policy=cr.AwsCustomResourcePolicy.from_sdk_calls(
                resources=cr.AwsCustomResourcePolicy.ANY_RESOURCE
            ),
        )
        update_assume_role.node.default_child.node.default_child.override_logical_id(
            stable_logical_id("CustomAwsSdk", "update-iot-user-role-assume-policy"))

