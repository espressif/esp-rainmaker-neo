# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
    aws_iot as iot,
    aws_dynamodb as dynamodb,
    Stack,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable, create_iot_topic_rule, stable_logical_id, create_iot_rule_log_group
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn

class CreateNodeIndexedParamsTable(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create DynamoDB table for node_iparams
        self.node_iparams_table = ManagedTable(
            self,
            "NodeIndexedParamsTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['NODE_IPARAMS'],
            partition_key=dynamodb.Attribute(
                name="node_id",
                type=dynamodb.AttributeType.STRING
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )



class NodeShadowUpdateToDB(Construct):
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        log_group = create_iot_rule_log_group(
            self, "ShadowUpdateLogGroup", rule_name="iparams-shadow-update",
        )

        shadow_update_ddb_role_name = "iparams-shadow-ddb-role"
        shadow_update_ddb_role = iam.Role(
            self, "ShadowUpdateDynamoDBRole",
            role_name=f"rmng-{shadow_update_ddb_role_name}-{Stack.of(self).region}",
            assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
            inline_policies={
                "dynamodb_policy": iam.PolicyDocument(
                    statements=[
                        iam.PolicyStatement(
                            actions=[
                                "dynamodb:PutItem"
                            ],
                            resources=[get_table_arn(TABLE_NAMES['NODE_IPARAMS'], region)]
                        )
                    ]
                )
            }
        )
        shadow_update_ddb_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", shadow_update_ddb_role_name))

        shadow_update_logs_role_name = "iparams-shadow-logs-role"
        shadow_update_logs_role = iam.Role(
            self, "ShadowUpdateLogsRole",
            role_name=f"rmng-{shadow_update_logs_role_name}-{Stack.of(self).region}",
            assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
            inline_policies={
                "logs_policy": iam.PolicyDocument(
                    statements=[
                        iam.PolicyStatement(
                            actions=[
                                "logs:CreateLogStream",
                                "logs:PutLogEvents"
                            ],
                            resources=[log_group.log_group_arn + ":*"]
                        )
                    ]
                )
            }
        )
        shadow_update_logs_role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", shadow_update_logs_role_name))

        # Create IoT Core rule for direct shadow updates to DynamoDB
        create_iot_topic_rule(
            self, "ShadowUpdateToDynamoDBRule",
            rule_name="iparams_index_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql="SELECT topic(3) as node_id, get(get(current,'state'),'reported') as iparams FROM '$aws/things/+/shadow/name/iparams/update/documents'",
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        dynamo_d_bv2=iot.CfnTopicRule.DynamoDBv2ActionProperty(
                            put_item=iot.CfnTopicRule.PutItemInputProperty(
                                table_name=TABLE_NAMES['NODE_IPARAMS'],
                            ),
                            role_arn=shadow_update_ddb_role.role_arn
                        )
                    )
                ],
                error_action=iot.CfnTopicRule.ActionProperty(
                    cloudwatch_logs=iot.CfnTopicRule.CloudwatchLogsActionProperty(
                        log_group_name=log_group.log_group_name,
                        role_arn=shadow_update_logs_role.role_arn
                    )
                )
            )
        )

