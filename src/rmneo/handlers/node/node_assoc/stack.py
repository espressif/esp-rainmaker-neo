# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
    aws_dynamodb,
    aws_iam as iam,
    Stack,
    Aws,
    RemovalPolicy,
)
from constructs import Construct
from app_common import (
    CommonResources,
    ManagedTable,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, NODE_LIFECYCLE_HOOKS, FUNCTION_NAMES
from arn_utils import get_table_arn, get_index_arn, get_topic_arn, get_iot_thing_arn

class AssociateNodeAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, node_data_reset_function=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "node_assoc"
        associate_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:*"],
            resources=[
                get_table_arn(TABLE_NAMES['ASSOC_REQUESTS'], region),
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region)
            ]
        ))
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:ListThingPrincipals", "iot:DescribeCertificate", "iot:UpdateThing", "iot:DescribeThing"],
            resources=["*"]
        ))
        # node_details table — GetItem to read node_type (via GetNodeType,
        # not DescribeThing) for node-lifecycle hook dispatch.
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(TABLE_NAMES['NODE_DETAILS'], region)]
        ))
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:GetThingShadow", "iot:DeleteThingShadow", "iot:UpdateThingShadow"],
            resources=[get_iot_thing_arn('*', region)]
        ))
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:Publish"],
            resources=[get_topic_arn('rainmaker/nodes/*/from_cloud', region)]
        ))
        # Capability lambdas follow the convention rmng-<capability>-capability (see invokeCapabilityLambda); grant invoke on any of them, no-op if absent.
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[f"arn:aws:lambda:{Aws.REGION}:{Aws.ACCOUNT_ID}:function:rmng-*-capability"]
        ))
        # Node-left-group lifecycle hook: fires when a node is moved to a new
        # group via this associate flow. Core invokes the hook Lambda by its
        # conventional name (a core-owned constant — no CFN cross-stack ref and
        # no dependency on any downstream module). The Lambda is created by a
        # separately-deployed downstream stack; if absent, the invoke no-ops.
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{Aws.REGION}:{Aws.ACCOUNT_ID}:function:"
                f"{NODE_LIFECYCLE_HOOKS['NODE_LEFT_GROUP']}",
            ],
        ))
        # Group-membership percolation: confirming a node association adds it to a
        # group and async-invokes the notifications Lambda so Alexa/GVA discover
        # the node (node.EmitGroupMembershipChangeAsync). Referenced by name (the
        # notifications Lambda is created later in rmng-core).
        associate_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{Aws.REGION}:{Aws.ACCOUNT_ID}:function:"
                f"{FUNCTION_NAMES['NOTIFICATIONS']}",
            ],
        ))

        # Lambda Function
        environment = {}
        environment["NODE_DATA_RESET_FUNCTION_NAME"] = node_data_reset_function.function_name
        environment["NODE_LEFT_GROUP_HOOK_FUNCTION_NAME"] = NODE_LIFECYCLE_HOOKS["NODE_LEFT_GROUP"]
        environment["NOTIFICATIONS_FUNCTION_NAME"] = FUNCTION_NAMES["NOTIFICATIONS"]

        self.associate_function = create_lambda_function(
            self, function_name, common_resources,
            lambda_role=associate_lambda_role,
            environment=environment,
        )

        # Grant permission to invoke the node data reset Lambda
        node_data_reset_function.grant_invoke(self.associate_function)


        # Create API Gateway resources: /v1/groups/{groupId}/node-assoc-requests and .../{requestId}/verify
        # Get v1 resource
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        # Get groups resource
        group_resource_id = get_or_create_api_resource(
            self, "GroupResource", common_resources,
            v1_parent_id, "groups"
        )
        # Get group ID resource (created by GroupAPI, retrieved from cache)
        group_id_resource_id = get_or_create_api_resource(
            self, "GroupIdResource", common_resources,
            group_resource_id, "{groupId}"
        )
        node_assoc_requests_resource_id = get_or_create_api_resource(
            self, "NodeAssocRequestsResource", common_resources,
            group_id_resource_id, "node-assoc-requests"
        )

        create_cfn_api_method(
            self, "InitiatePostMethod", common_resources,
            node_assoc_requests_resource_id, "POST", self.associate_function
        )
        
        add_cors_options(
            self, "NodeAssocRequestsOptionsMethod", common_resources,
            node_assoc_requests_resource_id, allowed_methods=["POST"]
        )

        request_id_resource_id = get_or_create_api_resource(
            self, "RequestIdResource", common_resources,
            node_assoc_requests_resource_id, "{requestId}"
        )
        verify_resource_id = get_or_create_api_resource(
            self, "VerifyResource", common_resources,
            request_id_resource_id, "verify"
        )

        create_cfn_api_method(
            self, "VerifyPostMethod", common_resources,
            verify_resource_id, "POST", self.associate_function
        )
        
        add_cors_options(
            self, "VerifyOptionsMethod", common_resources,
            verify_resource_id, allowed_methods=["POST"]
        )

        # /v1/groups/{groupId}/node-assoc-requests/{requestId}/confirm - Confirm association (Matter groups)
        confirm_resource_id = get_or_create_api_resource(
            self, "ConfirmResource", common_resources,
            request_id_resource_id, "confirm"
        )

        create_cfn_api_method(
            self, "ConfirmPostMethod", common_resources,
            confirm_resource_id, "POST", self.associate_function
        )
        
        add_cors_options(
            self, "ConfirmOptionsMethod", common_resources,
            confirm_resource_id, allowed_methods=["POST"]
        )

class CreateAssocRequestsTable(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create DynamoDB table with TTL
        self.assoc_requests_table = ManagedTable(
            self, "AssocRequestsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['ASSOC_REQUESTS'],
            partition_key=aws_dynamodb.Attribute(name="request_id", type=aws_dynamodb.AttributeType.STRING),
            time_to_live_attribute="expiration_time",
            billing_mode=aws_dynamodb.BillingMode.PAY_PER_REQUEST,
            removal_policy=RemovalPolicy.DESTROY,
        )
