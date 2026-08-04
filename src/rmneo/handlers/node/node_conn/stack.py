# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    aws_iot as iot,
    Duration,
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
from arn_utils import get_table_arn, get_index_arn, get_iot_thing_arn, get_table_index_arn

class PresenceEventHandlerAPI(Construct):
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role for the presence_event_handler function
        function_name = "node_conn"
        presence_event_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add permissions for DynamoDB operations
        presence_event_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:Query"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODES_ONLINE'], region),
                get_table_index_arn(TABLE_NAMES['NODES_ONLINE'], "*", region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region)
            ]
        ))

        # Add permissions for IoT publishing
        presence_event_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:UpdateThingShadow"],
            resources=[
                get_iot_thing_arn('*', region)
            ]
        ))

        # Create Lambda function for presence_event_handler
        self.presence_event_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=presence_event_lambda_role,
            timeout=Duration.seconds(60),
            environment={},
        )

        # Create IAM role for IoT rule (used by the online rule's DynamoDB action,
        # and — in SQS mode — also by the offline rule's SQS action).
        iot_rule_role = create_iot_rule_role(
            self, "IoTRuleRole",
            role_name="node-conn-iot-rule-role",
            common_resources=common_resources,
            description="Role for IoT rule to access DynamoDB",
        )

        iot_rule_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem",
                "dynamodb:UpdateItem"
            ],
            resources=[get_table_arn(TABLE_NAMES['NODES_ONLINE'], region)]
        ))

        # Always provision the SQS path's queue, DLQ, event-source mapping, and
        # IAM (sqs:SendMessage on the IoT rule role, sqs:ReceiveMessage on the
        # lambda role) so the rule's action can be flipped at runtime via the
        # /v1/admin/iot-event-mode admin API without redeploying.
        # delivery_delay replaces the 10-s in-lambda sleep (handler.go) for the SQS
        # path: messages sit in the queue before becoming visible, so the reconnect's
        # DynamoDB PutItem can land before the lambda reads the session — same
        # stale-disconnect guard, without serialising every event in a batch.
        sqs_infra = setup_sqs_lambda_infra(
            self,
            name_prefix="node-conn",
            construct_id_prefix="PresenceEvent",
            lambda_function=self.presence_event_function,
            lambda_role=presence_event_lambda_role,
            iot_rule_role=iot_rule_role,
            delivery_delay=Duration.seconds(10),
        )
        self.node_conn_queue = sqs_infra.queue
        self.node_conn_dlq = sqs_infra.dlq
        self.iot_rule_role = iot_rule_role
        self.iot_rule_error_role = sqs_infra.error_role

        # The rule action is hard-coded to Lambda-direct so the synthesized
        # template is stable across deploys. Operators flip to SQS at runtime
        # via the /v1/admin/iot-event-mode API. Keeping this static means
        # CloudFormation sees no Actions diff on subsequent deploys and
        # preserves any runtime-set mode (so long as nothing else in this
        # rule's CDK definition changes — see docs/en/specs/iot_event_mode.md).
        offline_action = iot.CfnTopicRule.ActionProperty(
            lambda_=iot.CfnTopicRule.LambdaActionProperty(
                function_arn=self.presence_event_function.function_arn
            )
        )
        offline_error_action = sqs_infra.error_action
        offline_extra_deps = sqs_infra.dependencies

        # Filter out user (mobile-app MQTT sessions) and iotconsole (operator) events at the rule itself so they never reach the lambda or the DB.
        # IoT rule SQL requires explicit comparisons in WHERE — `NOT startswith(...)` is rejected, so compare against `false`.
        client_filter = "startswith(clientId, 'user:') = false AND startswith(clientId, 'iotconsole-') = false"

        # Create IoT Core rule for Node Offline events
        offline_rule_kwargs = {
            "sql": f"SELECT * FROM '$aws/events/presence/disconnected/#' WHERE {client_filter}",
            "actions": [offline_action],
        }
        if offline_error_action is not None:
            offline_rule_kwargs["error_action"] = offline_error_action
        offline_rule = create_iot_topic_rule(
            self, "NodeOfflineRule",
            rule_name="node_disconnected_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(**offline_rule_kwargs)
        )

        # Create IoT Core rule for Node Online events (DynamoDB write — same in both modes)
        online_rule = create_iot_topic_rule(
            self, "NodeOnlineRule",
            rule_name="node_connected_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=f"SELECT * FROM '$aws/events/presence/connected/+' WHERE {client_filter}",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        dynamo_d_bv2=iot.CfnTopicRule.DynamoDBv2ActionProperty(
                            put_item=iot.CfnTopicRule.PutItemInputProperty(
                                table_name=TABLE_NAMES['NODES_ONLINE']
                            ),
                            role_arn=iot_rule_role.role_arn
                        )
                    )
                ]
            )
        )

        # Ensure the IoT rule roles exist before the rules
        online_rule.add_resource_dependency(iot_rule_role.node.default_child)
        for dep in offline_extra_deps:
            offline_rule.add_resource_dependency(dep)

        # Grant IoT Core permission to invoke the Lambda function. Required for
        # the lambda-direct rule action; harmless when the rule is in SQS mode.
        # Always present so a runtime flip to direct works without redeploy.
        self.presence_event_function.add_permission(
            "PresenceEventInvokePermission",
            principal=iam.ServicePrincipal("iot.amazonaws.com"),
            action="lambda:InvokeFunction"
        )
