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
from src.rmneo.stacks.base_res_constants import TABLE_NAMES
from arn_utils import get_table_arn, get_index_arn, get_topic_arn
from src.rmneo.handlers.timeseries.timeseries_stack import TimeseriesCore
from src.rmneo.handlers.timeseries.ts_stream_processor.stack import TimeseriesStreamProcessorCore

class ServiceCore(Construct):
    """Core/compute resources for Service - Lambda function and API integration"""

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, user_node_tags_function=None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        # Create Lambda role
        function_name = "node_service"
        service_lambda_role = create_base_lambda_role(self, function_name, common_resources)
        service_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:PutItem",
                "dynamodb:UpdateItem",
                "dynamodb:DeleteItem",
                "dynamodb:Query",
                "dynamodb:Scan"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], region),
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], region),
                get_table_arn(TABLE_NAMES['GROUPS'], region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], region),
                get_table_arn(TABLE_NAMES['RAW_TS_DATA'], region),
                get_table_arn(TABLE_NAMES['PROCESSED_TS_DATA'], region)
            ]
        ))
        
        # Purging a node's timeseries data batch-deletes it, and BatchWriteItem is a distinct action
        # from DeleteItem. Granted only on the two timeseries tables, the sole batch-delete target
        # in this lambda.
        service_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:BatchWriteItem"],
            resources=[
                get_table_arn(TABLE_NAMES['RAW_TS_DATA'], region),
                get_table_arn(TABLE_NAMES['PROCESSED_TS_DATA'], region)
            ]
        ))

        # Also allow using GSIs on the tables
        service_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:Query"],
            resources=[
                get_index_arn('USER_GROUP_MAPPING_GROUP_ID', region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', region)
            ]
        ))

        # Add IoT publish permission for device communication
        service_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["iot:Publish"],
            resources=[get_topic_arn('rainmaker/nodes/*/from_cloud', region)]
        ))

        # Create Lambda function
        self.service_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=service_lambda_role,
        )

        # Add API Gateway integration
        service_integration = apigateway.LambdaIntegration(self.service_function)
        
        # Create nested API Gateway resources using CFn to avoid cyclic dependencies
        # Create the nested resource path: /v1/groups/{groupId}/nodes/{nodeId}/{serviceName}
        
        # Get v1 resource
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )
        # Get groups resource (created by GroupAPI, retrieved from cache)
        group_resource_id = get_or_create_api_resource(
            self, "GroupResource", common_resources,
            v1_parent_id, "groups"
        )
        # Get group ID resource (created by GroupAPI, retrieved from cache)
        group_id_resource_id = get_or_create_api_resource(
            self, "GroupIdResource", common_resources,
            group_resource_id, "{groupId}"
        )
        
        node_resource_id = get_or_create_api_resource(
            self, "NodeResource", common_resources,
            group_id_resource_id, "nodes"
        )
        
        node_id_resource_id = get_or_create_api_resource(
            self, "NodeIdResource", common_resources,
            node_resource_id, "{nodeId}"
        )

        # DELETE /v1/groups/{groupId}/nodes/{nodeId} — disassociate node from group
        # Handled by the group Lambda (available via common_resources)
        create_cfn_api_method(
            self, "NodeIdDeleteMethod", common_resources,
            node_id_resource_id, "DELETE", common_resources.group_function
        )
        add_cors_options(
            self, "NodeIdOptionsMethod", common_resources,
            node_id_resource_id, allowed_methods=["DELETE"]
        )

        # Create service name resource for other services: /v1/groups/{groupId}/nodes/{nodeId}/{serviceName}
        service_resource_id = get_or_create_api_resource(
            self, "ServiceNameResource", common_resources,
            node_id_resource_id, "{serviceName}"
        )
        
        # Create methods using CFn
        create_cfn_api_method(
            self, "ServiceGetMethod", common_resources,
            service_resource_id, "GET", self.service_function
        )
        
        create_cfn_api_method(
            self, "ServicePutMethod", common_resources,
            service_resource_id, "PUT", self.service_function
        )
        
        create_cfn_api_method(
            self, "ServiceDeleteMethod", common_resources,
            service_resource_id, "DELETE", self.service_function
        )
        
        add_cors_options(
            self, "ServiceOptionsMethod", common_resources,
            service_resource_id, allowed_methods=["GET", "PUT", "DELETE"]
        )

        # Create timeseries resource: /v1/groups/{groupId}/nodes/{nodeId}/timeseries
        timeseries_resource_id = get_or_create_api_resource(
            self, "TimeseriesResource", common_resources,
            node_id_resource_id, "timeseries"
        )
        
        # Create timeseries sub-resources: raw, latest, aggregates
        timeseries_raw_resource_id = get_or_create_api_resource(
            self, "TimeseriesRawResource", common_resources,
            timeseries_resource_id, "raw"
        )
        
        timeseries_latest_resource_id = get_or_create_api_resource(
            self, "TimeseriesLatestResource", common_resources,
            timeseries_resource_id, "latest"
        )
        
        timeseries_aggregates_resource_id = get_or_create_api_resource(
            self, "TimeseriesAggregatesResource", common_resources,
            timeseries_resource_id, "aggregates"
        )
        
        # Create GET methods for timeseries sub-resources
        create_cfn_api_method(
            self, "TimeseriesRawGetMethod", common_resources,
            timeseries_raw_resource_id, "GET", self.service_function
        )
        
        add_cors_options(
            self, "TimeseriesRawOptionsMethod", common_resources,
            timeseries_raw_resource_id, allowed_methods=["GET"]
        )
        
        create_cfn_api_method(
            self, "TimeseriesLatestGetMethod", common_resources,
            timeseries_latest_resource_id, "GET", self.service_function
        )
        
        add_cors_options(
            self, "TimeseriesLatestOptionsMethod", common_resources,
            timeseries_latest_resource_id, allowed_methods=["GET"]
        )
        
        create_cfn_api_method(
            self, "TimeseriesAggregatesGetMethod", common_resources,
            timeseries_aggregates_resource_id, "GET", self.service_function
        )
        
        add_cors_options(
            self, "TimeseriesAggregatesOptionsMethod", common_resources,
            timeseries_aggregates_resource_id, allowed_methods=["GET"]
        )

        # Create user node tags resource: /v1/groups/{groupId}/nodes/{nodeId}/tags
        if user_node_tags_function:
            tags_resource_id = get_or_create_api_resource(
                self, "UserNodeTagsResource", common_resources,
                node_id_resource_id, "tags"
            )

            create_cfn_api_method(
                self, "UserNodeTagsGetMethod", common_resources,
                tags_resource_id, "GET", user_node_tags_function
            )

            create_cfn_api_method(
                self, "UserNodeTagsPutMethod", common_resources,
                tags_resource_id, "PUT", user_node_tags_function
            )

            add_cors_options(
                self, "UserNodeTagsOptionsMethod", common_resources,
                tags_resource_id, allowed_methods=["GET", "PUT"]
            )

        # Create timeseries core resources (IoT rules)
        self.timeseries_core = TimeseriesCore(self, "TimeseriesCore", common_resources)
        
        # Create stream processor core resources (Lambda function with DynamoDB stream)
        self.timeseries_stream_processor_core = TimeseriesStreamProcessorCore(
            self, "TimeseriesStreamProcessorCore", common_resources
        )
        # Ensure stream processor depends on timeseries core
        self.timeseries_stream_processor_core.node.add_dependency(self.timeseries_core)
