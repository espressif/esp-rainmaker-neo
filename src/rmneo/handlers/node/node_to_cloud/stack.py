# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    aws_iot as iot,
    Aws,
    Stack,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    setup_sqs_lambda_infra,
    create_iot_rule_role,
    create_iot_topic_rule,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn, get_index_arn, get_topic_arn, get_s3_object_arn

class PublishInputEventHandlerAPI(Construct):
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role for the publish_input_event_handler function
        function_name = "node_to_cloud"
        publish_input_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add IoT publish permission
        publish_input_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:Publish"],
            resources=[get_topic_arn('rainmaker/nodes/*/from_cloud', region)]
        ))

        # Add DynamoDB permissions for group_device_mapping table
        publish_input_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region)
            ]
        ))

        # Add DynamoDB permissions for node_details table
        publish_input_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem",
                "dynamodb:GetItem",
                "dynamodb:UpdateItem",
                "dynamodb:DeleteItem"
            ],
            resources=[get_table_arn(TABLE_NAMES['NODE_DETAILS'], region)]
        ))

        # Read-only access to the sanitized client outputs written by
        # upload_rmng_outputs.py, so the getServerConfig event can serve them.
        public_assets_bucket = f"rmng-public-assets-{Aws.ACCOUNT_ID}"
        publish_input_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["s3:GetObject"],
            resources=[get_s3_object_arn(public_assets_bucket, f"{region}/*")]
        ))

        # Create Lambda function for publish_input_event_handler
        self.publish_input_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=publish_input_lambda_role,
        )

        # Always provision the SQS path's queue, DLQ, event-source mapping, and
        # IAM (sqs:SendMessage on the IoT rule role, sqs:ReceiveMessage on the
        # lambda role) so the rule's action can be flipped at runtime via the
        # /v1/admin/iot-event-mode admin API without redeploying.
        iot_rule_role = create_iot_rule_role(
            self, "IoTRuleRole",
            role_name="node-to-cloud-iot-rule-role",
            common_resources=common_resources,
            description="Role for IoT rule to send messages to SQS",
        )
        sqs_infra = setup_sqs_lambda_infra(
            self,
            name_prefix="node-to-cloud",
            construct_id_prefix="PublishInputEvent",
            lambda_function=self.publish_input_function,
            lambda_role=publish_input_lambda_role,
            iot_rule_role=iot_rule_role,
        )
        self.node_to_cloud_queue = sqs_infra.queue
        self.node_to_cloud_dlq = sqs_infra.dlq
        self.iot_rule_role = iot_rule_role
        self.iot_rule_error_role = sqs_infra.error_role

        # The rule action is hard-coded to Lambda-direct so the synthesized
        # template is stable across deploys. Operators flip to SQS at runtime
        # via the /v1/admin/iot-event-mode API. Keeping this static means
        # CloudFormation sees no Actions diff on subsequent deploys and
        # preserves any runtime-set mode (so long as nothing else in this
        # rule's CDK definition changes — see docs/en/specs/iot_event_mode.md).
        rule_action = iot.CfnTopicRule.ActionProperty(
            lambda_=iot.CfnTopicRule.LambdaActionProperty(
                function_arn=self.publish_input_function.function_arn
            )
        )

        rule_payload_kwargs = {
            "sql": "SELECT topic(3) as thing_name, * as data FROM 'rainmaker/nodes/+/to_cloud'",
            "aws_iot_sql_version": "2016-03-23",
            "actions": [rule_action],
        }
        if sqs_infra.error_action is not None:
            rule_payload_kwargs["error_action"] = sqs_infra.error_action

        node_to_cloud_rule = create_iot_topic_rule(
            self, "NodeToCloudRule",
            rule_name="node_to_cloud_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(**rule_payload_kwargs)
        )

        for dep in sqs_infra.dependencies:
            node_to_cloud_rule.add_resource_dependency(dep)

        # Grant IoT Core permission to invoke the Lambda function. Required for
        # the lambda-direct rule action; harmless when the rule is in SQS mode.
        # Always present so a runtime flip to direct works without redeploy.
        self.publish_input_function.add_permission(
            "PublishInputInvokePermission",
            principal=iam.ServicePrincipal("iot.amazonaws.com"),
            action="lambda:InvokeFunction"
        )
