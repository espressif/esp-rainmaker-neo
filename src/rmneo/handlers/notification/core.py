# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_iam as iam,
    aws_iot as iot,
    Duration,
    CfnOutput,
    Stack,
)
from constructs import Construct
from app_common import CommonResources, create_lambda_function, create_base_lambda_role, create_iot_topic_rule, stable_logical_id, create_iot_rule_log_group
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, S3_BUCKETS, SSM_PARAMETER_PREFIXES, FUNCTION_NAMES
from arn_utils import (
    get_s3_bucket_arn, get_s3_object_arn, get_table_arn,
    get_index_arn, get_s3_bucket_resolved_name, get_ssm_parameter_prefix_arn
)

class NotificationCore(Construct):
    """Core/compute resources for Notification service - Lambda functions and IoT rules"""
    
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, *, node_data_reset_function=None, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        log_group = create_iot_rule_log_group(
            self, "NotificationsLogGroup", rule_name="notifications",
        )

        function_name = "notifications"

        direct_notifications_log_group = create_iot_rule_log_group(
            self, "DirectNotificationsLogGroup", rule_name="direct-notifications",
        )

        # Create Lambda role for the notifications function
        notifications_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        
        # Add permissions for DynamoDB operations
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:Query",
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:UpdateItem",
                "dynamodb:DeleteItem" # node_reset disassociates the node, which drops its group_device_mapping row (groupNodeDB.RemoveNode).
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region),
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], region),
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region),
                get_table_arn(TABLE_NAMES['AUTOMATIONS'], region),
            ]
        ))

        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter",
            ],
            resources=[
                get_ssm_parameter_prefix_arn('ALEXA_CONFIG', region),
                get_ssm_parameter_prefix_arn('GVA_CONFIG', region)
            ]
        ))
        
        # Add permissions for IoT operations
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:DescribeEndpoint",
                "iot:Connect",
                "iot:Publish",
                "iot:Subscribe",
                "iot:Receive",
                "iot:GetThingShadow",
                # node_reset disassociation side-effects: clear the group_id
                # thing attribute (UpdateThing), delete the old group shadow
                # (DeleteThingShadow), and clear user tags from the iparams
                # shadow (UpdateThingShadow).
                "iot:UpdateThing",
                "iot:UpdateThingShadow",
                "iot:DeleteThingShadow"
            ],
            resources=["*"]
        ))

        # Add permissions for SQS operations
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "sqs:SendMessage"
            ],
            resources=["*"]
        ))

        # Add permissions for SNS operations. DeleteEndpoint is needed to clean up SNS Platform Endpoints that APNS/GCM has marked disabled (token rotated, app uninstalled); the send path drops the matching DynamoDB row alongside.
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "sns:Publish",
                "sns:DeleteEndpoint"
            ],
            resources=["*"]
        ))

        # Add permissions for S3 operations
        bucket_name = get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], region, stack_prefix=common_resources.prefix)
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "s3:GetObject",
            ],
            resources=[
                get_s3_object_arn(bucket_name)
            ]
        ))

        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "s3:ListBucket"
            ],
            resources=[
                get_s3_bucket_arn(bucket_name)
            ]
        ))

        # Create the notifications Lambda function
        environment = {
            "FILE_BUCKET_NAME": get_s3_bucket_resolved_name(S3_BUCKETS['FILES_BUCKET_NAME'], region, stack_prefix=common_resources.prefix)
        }
        # node_reset disassociation triggers the async node_data_reset cleanup
        # (triggers, schedules, timeseries, automations) via this Lambda.
        if node_data_reset_function is not None:
            environment["NODE_DATA_RESET_FUNCTION_NAME"] = node_data_reset_function.function_name

        # node_reset removes the node from its group and percolates a
        # group_membership_change back through this same notifications Lambda
        # (node.EmitGroupMembershipChangeAsync) so Alexa/GVA drop the node. The
        # self-invoke permission is granted after the function is created below.
        environment["NOTIFICATIONS_FUNCTION_NAME"] = FUNCTION_NAMES["NOTIFICATIONS"]

        self.notifications_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=notifications_lambda_role,
            timeout=Duration.minutes(10),
            environment=environment
        )

        # Grant permission to invoke the node data reset Lambda (node_reset flow).
        if node_data_reset_function is not None:
            node_data_reset_function.grant_invoke(self.notifications_function)

        # Self-invoke: the node_reset removal path percolates a
        # group_membership_change back through this Lambda. Reference the ARN by
        # deterministic name rather than grant_invoke(self) — the latter makes
        # the function's role policy depend on the function while the function
        # depends on its role, a circular dependency.
        notifications_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["lambda:InvokeFunction"],
            resources=[
                f"arn:aws:lambda:{region}:{Stack.of(self).account}:function:"
                f"{FUNCTION_NAMES['NOTIFICATIONS']}"
            ],
        ))

        # Create error action role for IoT rules
        error_role_name = "notifications-error-role"
        error_role = iam.Role(
            self, "NotificationsErrorRole",
            role_name=f"rmng-{error_role_name}-{Stack.of(self).region}",
            assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
            inline_policies={
                "logs_policy": iam.PolicyDocument(
                    statements=[
                        iam.PolicyStatement(
                            actions=[
                                "logs:CreateLogStream",
                                "logs:PutLogEvents"
                            ],
                            resources=[
                                log_group.log_group_arn + ":*",
                                direct_notifications_log_group.log_group_arn + ":*"
                            ]
                        )
                    ]
                )
            }
        )
        error_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", error_role_name))

        # Create IoT Core rule for shadow-based notifications. Fires on a device-driven notify.version bump OR on a connectivity transition: the presence handler's online-only write never touches notify.version, so without the second branch offline/online never reaches the notification pipeline.
        # The bare 'params-' shadow (a group-less node) is excluded: recipients are
        # resolved from group membership, so an update with no group ID in its name
        # can never route a notification. Every such invoke would be pure waste.
        shadow_notify_rule = create_iot_topic_rule(
            self, "ShadowNotifyDispatchRule",
            rule_name="shadow_notify_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=f"""
                SELECT
                    topic(3) as node_id,
                    topic(6) as topic_name,
                    'shadow_update' as notification_type,
                    get(get(current, 'state'), 'reported') as curr_state,
                    get(get(previous, 'state'), 'reported') as prev_state,
                    get(get(get(get(current, 'state'), 'reported'), 'params'), 'notify') as notify
                FROM '$aws/things/+/shadow/name/+/update/documents'
                WHERE startswith(topic(6), 'params-')
                AND topic(6) <> 'params-'
                AND (
                    (
                        NOT isUndefined(get(get(get(get(get(current, 'state'), 'reported'), 'params'), 'notify'),'version'))
                        AND (
                            isUndefined(get(get(get(get(get(previous, 'state'), 'reported'), 'params'), 'notify'),'version'))
                            OR get(get(get(get(get(current, 'state'), 'reported'), 'params'), 'notify'),'version') <>
                               get(get(get(get(get(previous, 'state'), 'reported'), 'params'), 'notify'),'version')
                        )
                    )
                    OR (
                        NOT isUndefined(get(get(get(current, 'state'), 'reported'), 'online'))
                        AND (
                            isUndefined(get(get(get(previous, 'state'), 'reported'), 'online'))
                            OR get(get(get(current, 'state'), 'reported'), 'online') <>
                               get(get(get(previous, 'state'), 'reported'), 'online')
                        )
                    )
                )
                """,
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        lambda_=iot.CfnTopicRule.LambdaActionProperty(
                            function_arn=self.notifications_function.function_arn
                        )
                    )
                ],
                error_action=iot.CfnTopicRule.ActionProperty(
                    cloudwatch_logs=iot.CfnTopicRule.CloudwatchLogsActionProperty(
                        log_group_name=log_group.log_group_name,
                        role_arn=error_role.role_arn
                    )
                )
            )
        )

        # Create IoT Rule for direct notifications with basic ingest
        node_notify_rule = create_iot_topic_rule(
            self, "NodeNotifyRule",
            rule_name="node_notify_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=f"""
                SELECT 
                    topic(3) as node_id,
                    topic(5) as topic_name,
                    'direct_notification' as notification_type,
                    get(*, 'notify') as notify
                FROM 'rainmaker/nodes/+/notify/+'
                """,
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        lambda_=iot.CfnTopicRule.LambdaActionProperty(
                            function_arn=self.notifications_function.function_arn
                        )
                    )
                ],
                rule_disabled=False,
                description="Rule for direct notifications with notify topic name",
                error_action=iot.CfnTopicRule.ActionProperty(
                    cloudwatch_logs=iot.CfnTopicRule.CloudwatchLogsActionProperty(
                        log_group_name=log_group.log_group_name,
                        role_arn=error_role.role_arn
                    )
                )
            )
        )

        # Grant IoT Core permission to invoke the Lambda function for both rules
        self.notifications_function.add_permission(
            "NotificationsInvokePermission",
            principal=iam.ServicePrincipal("iot.amazonaws.com"),
            action="lambda:InvokeFunction",
            source_arn=shadow_notify_rule.attr_arn
        )

        self.notifications_function.add_permission(
            "NodeNotifyInvokePermission",
            principal=iam.ServicePrincipal("iot.amazonaws.com"),
            action="lambda:InvokeFunction",
            source_arn=node_notify_rule.attr_arn
        )

        # Output the Lambda function name
        CfnOutput(
            self, "NotificationsLambdaName",
            value=self.notifications_function.function_name,
            description="Name of the Notifications Lambda function"
        )
