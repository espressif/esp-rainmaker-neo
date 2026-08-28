# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from datetime import datetime

from aws_cdk import (
    Aws,
    Stack,
    aws_iam as iam,
    custom_resources as cr,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn


class IotEventModeCore(Construct):
    """superAdmin REST API to flip the rule action on node_disconnected_rule,
    node_to_cloud_rule, and node_ts_batch_rule between Lambda-direct and SQS
    at runtime, without redeploying. Both action paths are pre-provisioned by
    the node and timeseries handler stacks.

    Drift across CloudFormation deploys is handled via a "reapply" custom
    resource (see docs/en/specs/iot_event_mode.md §4.4): the runtime-set mode
    is persisted to the rmng-admin-configs DynamoDB table, and an
    AwsCustomResource invokes this lambda on every stack create/update to
    restore the live rule state from that row."""

    def __init__(
        self,
        scope: Construct,
        id: str,
        common_resources: CommonResources,
        *,
        presence_handler,
        publish_input_handler,
        timeseries_handler,
        **kwargs,
    ) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "iot_event_mode"
        lambda_role = create_base_lambda_role(self, function_name)

        presence_rule_arn = f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:rule/node_disconnected_rule"
        publish_input_rule_arn = f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:rule/node_to_cloud_rule"
        timeseries_rule_arn = f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:rule/node_ts_batch_rule"
        admin_config_table_arn = get_table_arn(TABLE_NAMES['ADMIN_CONFIG'], region)

        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:GetTopicRule", "iot:ReplaceTopicRule"],
            resources=[presence_rule_arn, publish_input_rule_arn, timeseries_rule_arn],
        ))

        # ReplaceTopicRule with role-bearing actions (the SQS action's roleArn
        # and the CloudWatch Logs error_action's roleArn) requires
        # iam:PassRole on those roles. Constrained to iot.amazonaws.com so
        # this credential cannot be used to attach the roles elsewhere.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iam:PassRole"],
            resources=[
                presence_handler.iot_rule_role.role_arn,
                presence_handler.iot_rule_error_role.role_arn,
                publish_input_handler.iot_rule_role.role_arn,
                publish_input_handler.iot_rule_error_role.role_arn,
                timeseries_handler.iot_rule_role.role_arn,
                timeseries_handler.iot_rule_error_role.role_arn,
            ],
            conditions={
                "StringEquals": {"iam:PassedToService": "iot.amazonaws.com"},
            },
        ))

        # Durable runtime-set mode lives in rmng_admin_config under the
        # config_key "iot_event_mode". GetItem on the reapply path,
        # UpdateItem on the runtime PUT path.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:UpdateItem"],
            resources=[admin_config_table_arn],
        ))

        environment = {
            "PRESENCE_LAMBDA_ARN": presence_handler.presence_event_function.function_arn,
            "NODE_CONN_QUEUE_URL": presence_handler.node_conn_queue.queue_url,
            "PRESENCE_IOT_RULE_ROLE_ARN": presence_handler.iot_rule_role.role_arn,
            "PUBLISH_INPUT_LAMBDA_ARN": publish_input_handler.publish_input_function.function_arn,
            "NODE_TO_CLOUD_QUEUE_URL": publish_input_handler.node_to_cloud_queue.queue_url,
            "PUBLISH_INPUT_IOT_RULE_ROLE_ARN": publish_input_handler.iot_rule_role.role_arn,
            "TIMESERIES_LAMBDA_ARN": timeseries_handler.timeseries_ingest_function.function_arn,
            "TIMESERIES_QUEUE_URL": timeseries_handler.timeseries_ingest_queue.queue_url,
            "TIMESERIES_IOT_RULE_ROLE_ARN": timeseries_handler.iot_rule_role.role_arn,
        }

        self.iot_event_mode_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment=environment,
        )

        # API: /v1/admin/iot-event-mode (GET, PUT)
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1",
        )
        v1_admin_parent_id = get_or_create_api_resource(
            self, "V1AdminResource", common_resources,
            v1_parent_id, "admin",
        )
        iot_event_mode_resource_id = get_or_create_api_resource(
            self, "IotEventModeResource", common_resources,
            v1_admin_parent_id, "iot-event-mode",
        )

        create_cfn_api_method(
            self, "IotEventModeGetMethod", common_resources,
            iot_event_mode_resource_id, "GET", self.iot_event_mode_function,
        )
        create_cfn_api_method(
            self, "IotEventModePutMethod", common_resources,
            iot_event_mode_resource_id, "PUT", self.iot_event_mode_function,
        )

        add_cors_options(
            self, "IotEventModeOptionsMethod", common_resources,
            iot_event_mode_resource_id, allowed_methods=["GET", "PUT"],
        )

        # Reapply custom resource: restores runtime-set mode after each stack
        # update. The physical_resource_id includes a synth-time timestamp so
        # CloudFormation invokes the lambda on every deploy, not just when
        # the resource's properties change. Mirrors the API Gateway deployer
        # idiom in src/rmng_core_stack.py.
        reapply_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        reapply_call = cr.AwsSdkCall(
            service="Lambda",
            action="invoke",
            parameters={
                "FunctionName": self.iot_event_mode_function.function_name,
                "InvocationType": "RequestResponse",
                "Payload": '{"action":"reapply"}',
            },
            physical_resource_id=cr.PhysicalResourceId.of(
                f"iot-event-mode-reapply-{reapply_timestamp}"
            ),
        )
        reapply = cr.AwsCustomResource(
            self, "IotEventModeReapply",
            on_create=reapply_call,
            on_update=reapply_call,
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(
                    actions=["lambda:InvokeFunction"],
                    resources=[self.iot_event_mode_function.function_arn],
                ),
            ]),
        )
        # Run after all rules are written so the reapply pass sees the
        # post-CFN state and can heal it back to the runtime-set mode.
        reapply.node.add_dependency(presence_handler)
        reapply.node.add_dependency(publish_input_handler)
        reapply.node.add_dependency(timeseries_handler)
        # And after the lambda itself exists.
        reapply.node.add_dependency(self.iot_event_mode_function)
