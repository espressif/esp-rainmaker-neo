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
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, IOT_RESOURCES
from arn_utils import get_table_arn
from src.espuser.stacks.base_res_constants import USER_TABLE_NAMES


class UpdateAPI(Construct):
    """API + Lambda for the bulk node-update flow.

    Shares the same Fargate task definition and node_register_policy as the
    registration flow -- the container picks register-vs-update behavior at
    runtime based on the JOB_TYPE env var the Lambda passes when it calls
    ECS RunTask. CDK-wise this means the only new pieces are the Lambda, its
    role, and the API Gateway resources under /v1/admin/nodes/update-jobs/.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, node_register_policy: iam.Policy = None, container_params: dict = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region

        admin_nodes_update_lambda_role = create_base_lambda_role(self, "admin_nodes_update_lambda_role")

        # Reuse the shared policy: it already grants the DynamoDB access on
        # node_reg_reqs / node_reg_failed_nodes that this Lambda needs, plus
        # S3 GetObject on the input bucket for the failed-nodes retry-csv path.
        admin_nodes_update_lambda_role.attach_inline_policy(node_register_policy)

        # IoT thing-group operations needed by CreateAdminGroupIfNotExists
        # when the Lambda validates / creates admin groups before kicking off
        # the container.
        admin_nodes_update_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:ListThingGroups",
                "iot:CreateThingGroup",
                "iot:DescribeThingGroup",
            ],
            resources=["*"]
        ))

        admin_nodes_update_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ecs:RunTask"],
            resources=[container_params["task_definition_arn"]]
        ))

        # PassRole scoped to exactly the two roles ecs:RunTask passes (the task
        # role and the task execution role), and only when passed to ECS tasks.
        # Previously "*" with no condition, which let this Lambda pass any role
        # in the account to a task it launches (privilege escalation).
        admin_nodes_update_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iam:PassRole"],
            resources=[
                container_params["task_role_arn"],
                container_params["execution_role_arn"],
            ],
            conditions={"StringEquals": {"iam:PassedToService": "ecs-tasks.amazonaws.com"}},
        ))

        admin_nodes_update_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem"],
            resources=[get_table_arn(USER_TABLE_NAMES['USER_DETAILS'], region)]
        ))

        self.admin_nodes_update_function = create_lambda_function(
            self, "admin_nodes_update",
            common_resources,
            lambda_role=admin_nodes_update_lambda_role,
            environment={
                "BULK_NODE_REG_TASK_SUBNET_IDS": container_params["public_subnet_ids"],
                "BULK_NODE_REG_TASK_SECURITY_GROUP_ID": container_params["security_group_id"],
                "BULK_NODE_REG_TASK_CLUSTER_ARN": container_params["cluster_arn"],
                "BULK_NODE_REG_TASK_TASK_DEFINITION_ARN": container_params["task_definition_arn"],
                "BULK_NODE_REG_TASK_CONTAINER_NAME": container_params["container_name"],
                "DEFAULT_THING_POLICY_NAME": IOT_RESOURCES['DEFAULT_THING_POLICY_NAME'],
            }
        )

        # API Gateway tree under /v1/admin/nodes/update-jobs.
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

        update_jobs_resource_id = get_or_create_api_resource(
            self, "UpdateJobsResource", common_resources,
            nodes_resource_id, "update-jobs"
        )
        create_cfn_api_method(
            self, "UpdateJobsPostMethod", common_resources,
            update_jobs_resource_id, "POST", self.admin_nodes_update_function
        )
        add_cors_options(
            self, "UpdateJobsOptionsMethod", common_resources,
            update_jobs_resource_id, allowed_methods=["POST"]
        )

        # /v1/admin/nodes/update-jobs/{requestId}
        request_id_resource_id = get_or_create_api_resource(
            self, "UpdateJobsRequestIdResource", common_resources,
            update_jobs_resource_id, "{requestId}"
        )
        create_cfn_api_method(
            self, "UpdateJobsRequestIdGetMethod", common_resources,
            request_id_resource_id, "GET", self.admin_nodes_update_function
        )
        add_cors_options(
            self, "UpdateJobsRequestIdOptionsMethod", common_resources,
            request_id_resource_id, allowed_methods=["GET"]
        )

        # /v1/admin/nodes/update-jobs/{requestId}/failed-nodes
        failed_nodes_resource_id = get_or_create_api_resource(
            self, "UpdateJobsFailedNodesResource", common_resources,
            request_id_resource_id, "failed-nodes"
        )
        create_cfn_api_method(
            self, "UpdateJobsFailedNodesGetMethod", common_resources,
            failed_nodes_resource_id, "GET", self.admin_nodes_update_function
        )
        add_cors_options(
            self, "UpdateJobsFailedNodesOptionsMethod", common_resources,
            failed_nodes_resource_id, allowed_methods=["GET"]
        )
