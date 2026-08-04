# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_lambda as lambda_,
    aws_iam as iam,
    aws_lambda_event_sources as lambda_event_sources,
    aws_dynamodb as dynamodb,
    aws_ssm as ssm,
    Duration,
    Stack,
)
from constructs import Construct
from app_common import CommonResources, create_lambda_function, create_base_lambda_role
from src.rmneo.stacks.base_res_constants import TABLE_NAMES, SSM_PARAMETERS
from arn_utils import get_table_arn, get_table_stream_arn

class TimeseriesStreamProcessorBase(Construct):
    """Base/infrastructure resources for Timeseries Stream Processor - placeholder for future infrastructure"""
    
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        # Currently no base/infrastructure resources needed for stream processor
        pass

class TimeseriesStreamProcessorCore(Construct):
    """Core/compute resources for Timeseries Stream Processor - Lambda function with DynamoDB stream"""
    
    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        region = Stack.of(self).region
        # Read stream ARN from SSM Parameter Store
        raw_ts_data_table_stream_arn = ssm.StringParameter.value_for_string_parameter(
            self, SSM_PARAMETERS['RAW_TS_DATA_STREAM_ARN']
        )

        # Create Lambda role for the ts_stream_processor function
        function_name = "ts_stream_processor"
        ts_stream_processor_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        
        # Add permissions for DynamoDB operations on processed_ts_data table
        ts_stream_processor_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:UpdateItem",
                "dynamodb:Query"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['PROCESSED_TS_DATA'], region),
                f"{get_table_arn(TABLE_NAMES['PROCESSED_TS_DATA'], region)}/index/*"
            ]
        ))

        # Add permissions to read from the raw_ts_data DynamoDB Stream using imported ARN
        ts_stream_processor_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:DescribeStream",
                "dynamodb:GetRecords",
                "dynamodb:GetShardIterator",
                "dynamodb:ListStreams"
            ],
            resources=[
                raw_ts_data_table_stream_arn
            ]
        ))

        # Create the Lambda function
        self.ts_stream_processor_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=ts_stream_processor_lambda_role,
            timeout=Duration.minutes(1)
        )

        # Import the table with stream information for event source
        raw_ts_data_table = dynamodb.Table.from_table_attributes(
            self, "ImportedRawTsDataTable",
            table_arn=get_table_arn(TABLE_NAMES['RAW_TS_DATA'], region),
            table_stream_arn=raw_ts_data_table_stream_arn
        )

        # Add the DynamoDB stream as an event source for the Lambda function
        stream_event_source = lambda_event_sources.DynamoEventSource(
            table=raw_ts_data_table,
            starting_position=lambda_.StartingPosition.TRIM_HORIZON,
            batch_size=25,  # Process up to 25 records at a time
            max_batching_window=Duration.seconds(5),  # Wait up to 5 seconds to collect records
            retry_attempts=3,  # Retry failed records 3 times
            parallelization_factor=1,  # Keep it simple with no parallelization for now
            report_batch_item_failures=True,  # Enable partial batch failure reporting
            filters=[
                lambda_.FilterCriteria.filter({
                    "eventName": lambda_.FilterRule.is_equal("INSERT")
                })
            ]
        )

        # Lambda::EventSourceMapping has no physical name → safe to move across
        # files (CFN delete-create, no "already exists" conflict). Brief deploy
        # gap where DDB stream events aren't consumed; stream retains them.
        self.ts_stream_processor_function.add_event_source(stream_event_source)

        # NOTE: The ts_stream_processor function processes INSERT events from the raw_ts_data 
        # table's DynamoDB stream and writes aggregated results to the processed_ts_data table.
        # The processed_ts_data table does not have streams enabled since it's a destination
        # table for aggregated results, not a trigger for further processing.
        
        # The ts_stream_processor function uses infrastructure-level event filtering
        # to only process INSERT events from the raw_ts_data table, which is more efficient
        # than filtering in application code.