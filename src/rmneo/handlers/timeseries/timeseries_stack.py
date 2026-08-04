# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_dynamodb as dynamodb,
    aws_iot as iot,
    aws_iam as iam,
    aws_ssm as ssm,
    Stack,
    RemovalPolicy,
)
from constructs import Construct
from app_common import CommonResources, ManagedTable, create_iot_rule_role, create_iot_topic_rule, create_ssm_string_parameter, create_iot_rule_log_group
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, SSM_PARAMETERS
from arn_utils import get_table_arn

class TimeseriesBase(Construct):
    """Base/infrastructure resources for Timeseries service - DynamoDB tables"""

    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # Create DynamoDB table for raw_ts_data
        self.raw_ts_data_table = ManagedTable(
            self,
            "RawTsDataTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['RAW_TS_DATA'],
            partition_key=dynamodb.Attribute(
                name="node_key_dt",
                type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="ts",
                type=dynamodb.AttributeType.NUMBER
            ),
            stream=dynamodb.StreamViewType.NEW_AND_OLD_IMAGES,
            point_in_time_recovery_specification=dynamodb.PointInTimeRecoverySpecification(
                point_in_time_recovery_enabled=True,
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Create DynamoDB table for processed_ts_data
        self.processed_ts_data_table = ManagedTable(
            self,
            "ProcessedTsDataTable",
            common_resources=common_resources,
            table_name=TABLE_NAMES['PROCESSED_TS_DATA'],
            partition_key=dynamodb.Attribute(
                name="node_key_dt",
                type=dynamodb.AttributeType.STRING
            ),
            sort_key=dynamodb.Attribute(
                name="interval_key",
                type=dynamodb.AttributeType.STRING
            ),
            point_in_time_recovery_specification=dynamodb.PointInTimeRecoverySpecification(
                point_in_time_recovery_enabled=True,
            ),
            removal_policy=RemovalPolicy.DESTROY,
        )

        # Store stream ARN in SSM for use in core stack
        # Cannot be hardcoded as stream ARNs include a timestamp generated at table creation time
        create_ssm_string_parameter(
            self, "RawTsDataTableStreamArnParameter",
            parameter_name=SSM_PARAMETERS['RAW_TS_DATA_STREAM_ARN'],
            string_value=self.raw_ts_data_table.table_stream_arn,
            description="DynamoDB Stream ARN for raw_ts_data table"
        )


class TimeseriesCore(Construct):
    """Core/compute resources for Timeseries service - IoT rule for data ingestion"""
    
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        log_group = create_iot_rule_log_group(
            self, "TimeseriesLogGroup", rule_name="timeseries",
        )

        # Create IAM role for IoT rule
        iot_rule_role = create_iot_rule_role(
            self, "TimeseriesIoTRuleRole",
            role_name="timeseries-iot-rule-role",
            common_resources=common_resources,
            description="Role for IoT rule to access DynamoDB for timeseries data",
        )

        # Manually grant permissions to the IoT rule role for DynamoDB operations
        iot_rule_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem",
                "dynamodb:UpdateItem"
            ],
            resources=[get_table_arn(TABLE_NAMES['RAW_TS_DATA'], region)]
        ))

        # Create error action role for IoT rules
        error_role = create_iot_rule_role(
            self, "TimeseriesErrorRole",
            role_name="timeseries-iot-rule-error-role",
            common_resources=common_resources,
        )
        error_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "logs:CreateLogStream",
                "logs:PutLogEvents"
            ],
            resources=[log_group.log_group_arn + ":*"]
        ))

        # Create IoT Core rule for timeseries data ingestion
        node_ts_rule = create_iot_topic_rule(
            self, "NodeTsRule",
            rule_name="node_ts_rule",
            topic_rule_payload=iot.CfnTopicRule.TopicRulePayloadProperty(
                sql=f"""
                SELECT
                    topic(3) as node_id,
                    topic(5) as topic_name,
                    k as key,
                    dt,
                    tz,
                    t as ts,
                    v as value,
                    cumulative,
                    concat(topic(3), '.', k, '.', dt) as node_key_dt
                FROM 'rainmaker/nodes/+/ts/+'
                """,
                aws_iot_sql_version="2016-03-23",
                actions=[
                    iot.CfnTopicRule.ActionProperty(
                        dynamo_d_bv2=iot.CfnTopicRule.DynamoDBv2ActionProperty(
                            put_item=iot.CfnTopicRule.PutItemInputProperty(
                                table_name=TABLE_NAMES['RAW_TS_DATA']
                            ),
                            role_arn=iot_rule_role.role_arn
                        )
                    )
                ],
                rule_disabled=False,
                description="Rule for timeseries data ingestion with basic ingest",
                error_action=iot.CfnTopicRule.ActionProperty(
                    cloudwatch_logs=iot.CfnTopicRule.CloudwatchLogsActionProperty(
                        log_group_name=log_group.log_group_name,
                        role_arn=error_role.role_arn
                    )
                )
            )
        )

        # Add dependency to ensure the role is created before the rule
        node_ts_rule.add_resource_dependency(iot_rule_role.node.default_child)
        node_ts_rule.add_resource_dependency(error_role.node.default_child)

        # Store the rule reference for potential use in other constructs
        self.node_ts_rule = node_ts_rule
