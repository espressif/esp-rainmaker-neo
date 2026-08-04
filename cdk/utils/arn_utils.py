#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
ARN Utilities for RMNG infrastructure.

This module provides functions to generate AWS ARNs dynamically
whenever required, reducing hardcoded constants.
"""

from aws_cdk import Aws
from src.rmneo.stacks.base_res_constants import (
    TABLE_NAMES, INDEX_NAMES, INDEX_TO_TABLE, 
    IOT_RESOURCES, SSM_PARAMETER_PREFIXES, S3_BUCKETS
)

def get_table_arn(table_name: str, region: str) -> str:
    """Generate DynamoDB table ARN."""
    return f"arn:aws:dynamodb:{region}:{Aws.ACCOUNT_ID}:table/{table_name}"


def get_table_index_arn(table_name: str, index_name: str, region: str) -> str:
    """Generate DynamoDB table index ARN."""
    return f"arn:aws:dynamodb:{region}:{Aws.ACCOUNT_ID}:table/{table_name}/index/{index_name}"


def get_index_arn(index_key: str, region: str) -> str:
    """Generate DynamoDB index ARN from index key."""
    table_key = INDEX_TO_TABLE[index_key]
    table_name = TABLE_NAMES[table_key]
    index_name = INDEX_NAMES[index_key]
    return get_table_index_arn(table_name, index_name, region)


def get_table_stream_arn(table_name: str, region: str) -> str:
    """Generate DynamoDB table stream ARN pattern."""
    return f"arn:aws:dynamodb:{region}:{Aws.ACCOUNT_ID}:table/{table_name}/stream/*"


def get_iam_role_arn(role_name: str) -> str:
    """Generate IAM Role ARN. IAM is global — ARN must not contain region."""
    return f"arn:aws:iam::{Aws.ACCOUNT_ID}:role/{role_name}"


def get_iot_thing_arn(thing_name: str, region: str) -> str:
    """Generate IoT Thing ARN."""
    return f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:thing/{thing_name}"


def get_topic_arn(topic_pattern: str, region: str) -> str:
    """Generate IoT Topic ARN."""
    return f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:topic/{topic_pattern}"


def get_app_platform_endpt_arn(platform: str, region: str) -> str:
    """Generate SNS platform application endpoint ARN."""
    return f"arn:aws:sns:{region}:{Aws.ACCOUNT_ID}:app/{platform}/*"


def get_ssm_parameter_arn(parameter_name: str, region: str) -> str:
    """Generate SSM Parameter ARN."""
    # Strip leading slash if present to avoid double slashes in ARN
    param_name = parameter_name.lstrip('/')
    return f"arn:aws:ssm:{region}:{Aws.ACCOUNT_ID}:parameter/{param_name}"


def get_ssm_parameter_prefix_arn(prefix_key: str, region: str) -> str:
    """Generate SSM Parameter prefix ARN (with wildcard)."""
    prefix = SSM_PARAMETER_PREFIXES[prefix_key]
    # Strip leading slash if present to avoid double slashes in ARN
    prefix = prefix.lstrip('/')
    return f"arn:aws:ssm:{region}:{Aws.ACCOUNT_ID}:parameter/{prefix}*"


def get_s3_bucket_resolved_name(purpose: str, region: str, stack_prefix: str = "") -> str:
    """Return the fully-resolved S3 bucket name including the Account-Regional Namespace suffix.

    `stack_prefix` mirrors `CommonResources.prefix` applied by `create_s3_bucket` in
    app_common.py — the physical bucket name is `{stack_prefix}{purpose}-<account>-<region>-an`.
    """
    return f"{stack_prefix}{purpose}-{Aws.ACCOUNT_ID}-{region}-an"


def get_s3_bucket_arn(bucket_name: str) -> str:
    """Generate S3 bucket ARN. S3 ARNs in IAM must not contain region (use arn:aws:s3:::bucket)."""
    return f"arn:aws:s3:::{bucket_name}"


def get_s3_object_arn(bucket_name: str, key_pattern: str = "*") -> str:
    """Generate S3 object ARN. S3 ARNs in IAM must not contain region (use arn:aws:s3:::bucket/key)."""
    return f"arn:aws:s3:::{bucket_name}/{key_pattern}"



def get_iot_role_alias_arn(role_alias_name: str, region: str) -> str:
    """Generate IoT Role Alias ARN."""
    return f"arn:aws:iot:{region}:{Aws.ACCOUNT_ID}:rolealias/{role_alias_name}"


def get_user_pool_arn(user_pool_id: str, region: str) -> str:
    """Generate Cognito User Pool ARN."""
    return f"arn:aws:cognito-idp:{region}:{Aws.ACCOUNT_ID}:userpool/{user_pool_id}"


def get_identity_pool_arn(identity_pool_id: str, region: str) -> str:
    """Generate Cognito Identity Pool ARN."""
    return f"arn:aws:cognito-identity:{region}:{Aws.ACCOUNT_ID}:identitypool/{identity_pool_id}"


def get_api_gateway_invoke_arn(api_id: str, region: str, http_method: str = "*", resource_path: str = "*") -> str:
    """Generate API Gateway invoke ARN."""
    return f"arn:aws:execute-api:{region}:{Aws.ACCOUNT_ID}:{api_id}/*/{http_method}/{resource_path}"


def get_lambda_integration_uri(function_arn: str, region: str) -> str:
    """Generate Lambda integration URI for API Gateway."""
    return f"arn:aws:apigateway:{region}:lambda:path/2015-03-31/functions/{function_arn}/invocations"



def get_kvs_channel_arn(channel_pattern: str, region: str) -> str:
    """Generate KVS signaling channel ARN."""
    return f"arn:aws:kinesisvideo:{region}:{Aws.ACCOUNT_ID}:channel/{channel_pattern}"
