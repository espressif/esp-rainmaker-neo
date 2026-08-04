# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
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
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, INDEX_NAMES, IOT_RESOURCES
from arn_utils import get_table_arn, get_table_index_arn, get_kvs_channel_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES

class RegisterAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, node_register_policy: iam.Policy = None, container_params: dict = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role with necessary permissions
        function_name = "admin_node_reg"
        admin_nodes_reg_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        admin_nodes_reg_lambda_role.attach_inline_policy(node_register_policy)

        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:RegisterCACertificate",
                "iot:ListThingGroups",
                "iot:CreateThingGroup",
                "iot:DescribeThingGroup",
            ],
            resources=[
                "*"
            ]
        ))

        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ecs:RunTask",
            ],
            resources=[container_params["task_definition_arn"]]
        ))

        # Node-register lifecycle hook (nodelifecycle.OnNodeRegister): the register
        # flow synchronously invokes this optional hook by convention name so a
        # separately-deployed stack can attach capability-specific IoT policies.
        # No-op if the function is not deployed; the grant is harmless when absent.
        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{region}:{Stack.of(self).account}:function:rmng-node-register-hook",
            ]
        ))

        # PassRole scoped to exactly the two roles ecs:RunTask passes (the task
        # role and the task execution role), and only when passed to ECS tasks.
        # Previously "*" with no condition, which let this Lambda pass any role
        # in the account to a task it launches (privilege escalation).
        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iam:PassRole",
            ],
            resources=[
                container_params["task_role_arn"],
                container_params["execution_role_arn"],
            ],
            conditions={"StringEquals": {"iam:PassedToService": "ecs-tasks.amazonaws.com"}},
        ))

        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        # Permission to query the list GSI on the registration requests table
        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[get_table_index_arn(TABLE_NAMES['NODE_REG_REQS'], INDEX_NAMES['NODE_REG_REQS_LIST'], region)]
        ))

        admin_nodes_reg_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "kinesisvideo:CreateSignalingChannel",
                "kinesisvideo:DescribeSignalingChannel",
            ],
            resources=[get_kvs_channel_arn("rmng-v1-*", region)]
        ))

        # Create Lambda function
        self.admin_nodes_reg_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=admin_nodes_reg_lambda_role,
            environment={
                "BULK_NODE_REG_TASK_SUBNET_IDS": container_params["public_subnet_ids"],
                "BULK_NODE_REG_TASK_SECURITY_GROUP_ID": container_params["security_group_id"],
                "BULK_NODE_REG_TASK_CLUSTER_ARN": container_params["cluster_arn"],
                "BULK_NODE_REG_TASK_TASK_DEFINITION_ARN": container_params["task_definition_arn"],
                "BULK_NODE_REG_TASK_CONTAINER_NAME": container_params["container_name"],
                "DEFAULT_THING_POLICY_NAME": IOT_RESOURCES['DEFAULT_THING_POLICY_NAME'],
                "DEVICE_FILE_POLICY_NAME": IOT_RESOURCES['DEVICE_FILE_POLICY_NAME'],
                "DEVICE_VIDEO_POLICY_NAME": IOT_RESOURCES['DEVICE_VIDEO_POLICY_NAME'],
            }
        )

        # Create API Gateway resources using CFn to avoid cyclic dependencies
        # Create /v1/admin/nodes (share v1 if already created)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        # Create /v1/admin (share if already created)
        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin"
        )

        # Create /v1/admin/nodes
        nodes_resource_id = get_or_create_api_resource(
            self, "NodesResource", common_resources,
            v1_admin_parent_id, "nodes"
        )

        # Add POST method to /v1/admin/nodes (single node registration)
        create_cfn_api_method(
            self, "NodesPostMethod", common_resources,
            nodes_resource_id, "POST", self.admin_nodes_reg_function
        )

        add_cors_options(
            self, "NodesOptionsMethod", common_resources,
            nodes_resource_id, allowed_methods=["POST"]
        )

        # Create /v1/admin/nodes/registration-jobs
        registration_jobs_resource_id = get_or_create_api_resource(
            self, "RegistrationJobsResource", common_resources,
            nodes_resource_id, "registration-jobs"
        )

        # Add POST method to /v1/admin/nodes/registration-jobs (bulk registration)
        create_cfn_api_method(
            self, "RegistrationJobsPostMethod", common_resources,
            registration_jobs_resource_id, "POST", self.admin_nodes_reg_function
        )

        # Add GET method to /v1/admin/nodes/registration-jobs (list all jobs)
        create_cfn_api_method(
            self, "RegistrationJobsGetMethod", common_resources,
            registration_jobs_resource_id, "GET", self.admin_nodes_reg_function
        )

        add_cors_options(
            self, "RegistrationJobsOptionsMethod", common_resources,
            registration_jobs_resource_id, allowed_methods=["POST", "GET"]
        )

        # Create /v1/admin/nodes/registration-jobs/{requestId}
        request_id_resource_id = get_or_create_api_resource(
            self, "RequestIdResource", common_resources,
            registration_jobs_resource_id, "{requestId}"
        )

        # Add GET method to /v1/admin/nodes/registration-jobs/{requestId} (status check)
        create_cfn_api_method(
            self, "RequestIdGetMethod", common_resources,
            request_id_resource_id, "GET", self.admin_nodes_reg_function
        )

        add_cors_options(
            self, "RequestIdOptionsMethod", common_resources,
            request_id_resource_id, allowed_methods=["GET"]
        )

        # Create /v1/admin/nodes/registration-jobs/{requestId}/failed-nodes
        # Paginated JSON list of per-node failure detail for the job.
        failed_nodes_resource_id = get_or_create_api_resource(
            self, "FailedNodesResource", common_resources,
            request_id_resource_id, "failed-nodes"
        )
        create_cfn_api_method(
            self, "FailedNodesGetMethod", common_resources,
            failed_nodes_resource_id, "GET", self.admin_nodes_reg_function
        )
        add_cors_options(
            self, "FailedNodesOptionsMethod", common_resources,
            failed_nodes_resource_id, allowed_methods=["GET"]
        )
