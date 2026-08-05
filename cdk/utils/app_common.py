#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional

# This module lives at <repo>/cdk/utils/, so every repo-relative path below is anchored here rather than counted out at each use.
REPO_ROOT = Path(__file__).resolve().parents[2]

# cloud-components is consumed via git submodule; expose its cdk_go/ on sys.path
# so the re-exports below (and any rmng caller that imports from app_common)
# resolve to the moved ManagedTable / GSI_MANAGED_BY_TAG_* symbols.
sys.path.insert(0, str(REPO_ROOT / "esp-cloud-common" / "cdk_go"))

from aws_cdk import (
    ArnFormat,
    Aws,
    Stack,
    Duration,
    Token,
    CfnCondition,
    CustomResource,
    Fn,
    Tags,
    aws_dynamodb as dynamodb,
    aws_lambda as lambda_,
    aws_apigateway as apigateway,
    aws_iam as iam,
    aws_iot as iot,
    aws_kms as kms,
    aws_lambda_event_sources as event_sources,
    aws_s3 as s3,
    aws_sqs as sqs,
    RemovalPolicy,
    aws_ecs as ecs,
    aws_ec2 as ec2,
    aws_logs as logs,
    aws_s3_assets as s3_assets,
    aws_dynamodb as dynamodb,
    aws_ssm as ssm,
    aws_cognito as cognito,
    aws_apigatewayv2 as apigwv2,
    aws_cloudfront as cloudfront,
    custom_resources as cr,
)
from constructs import Construct
from dataclasses import dataclass
import os
import re
from arn_utils import get_lambda_integration_uri, get_api_gateway_invoke_arn, get_user_pool_arn, get_ssm_parameter_arn, get_table_arn


def get_rmng_inputs() -> dict:
    """Read operator-supplied deploy inputs from rmng-inputs.json (written by gather_stack_inputs.py).

    Keyed by Stackfile stack id, e.g. {"espuser-core": {"admin_emails": "..."}}. Returns {} when the
    file is absent or unreadable, so a CDK app falls back to its own defaults.
    """
    try:
        with open('rmng-inputs.json', 'r') as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


# Logical-ID stabilisation
#
# stable_logical_id(resource_type, name) produces a CFN-safe (PascalCase alphanumeric) logical ID that is independent of construct path. Refactors that move constructs under different parents no longer trigger CFN-side resource replacement.
#
# Example:
#   stable_logical_id("DDBTable", "rmng-users")
#     → "DDBTableRmngUsers"
#   stable_logical_id("IAMRole", "admin_files_lambda_role")
#     → "IAMRoleAdminFilesLambdaRole"


def stable_logical_id(resource_type: str, name: str) -> str:
    # PascalCase each segment of `name` (split on non-alphanumerics), then concatenate.
    segments = re.split(r'[^a-zA-Z0-9]+', name)
    pascal_name = ''.join(seg[:1].upper() + seg[1:] for seg in segments if seg)
    return f"{resource_type}{pascal_name}"


def _read_rmng_version() -> str:
    """First non-empty line of the repo-root VERSION file, or 'unknown' if absent."""
    try:
        with open(REPO_ROOT / "VERSION") as f:
            for line in f:
                line = line.strip()
                if line:
                    return line
    except FileNotFoundError:
        pass
    return "unknown"


def apply_common_tags(app) -> None:
    """Tag every stack/resource: AppRegion (AWS_REGION) and RMNGVersion (from VERSION).

    Skipped when CDK_PUBLISH=true (publish/synth): published templates are region-agnostic, so a
    baked-in AppRegion would be wrong for whoever deploys them. Tags apply only on a real deploy.
    """
    Tags.of(app).add("RMNGVersion", _read_rmng_version())
    if os.environ.get("CDK_PUBLISH") != "true":
        Tags.of(app).add("AppRegion", os.environ.get("AWS_REGION", "unknown"))


from gsi_infra import ManagedTable, GSI_MANAGED_BY_TAG_KEY, GSI_MANAGED_BY_TAG_VALUE 

class CommonResources:
    def __init__(self, api_gateway_id: str = None, api_gateway_root_resource_id: str = None, admin_api_resource_id: str = None, cognito_authorizer_id: str = None, prefix: str = ""):
        # CFn references to avoid cyclic dependencies
        self.api_gateway_id = api_gateway_id
        self.api_gateway_root_resource_id = api_gateway_root_resource_id
        self.admin_api_resource_id = admin_api_resource_id
        self.cognito_authorizer_id = cognito_authorizer_id
        # Project prefix prepended to physical AWS resource names owned by this stack.
        self.prefix = prefix
        # GSI orchestrator wiring (set by RMNGBaseStack on construction).
        self.gsi_state_machine_arn: str = None
        self.gsi_trigger_lambda_arn: str = None
        # Cache for API Gateway resources keyed by (parent_id, path_part, api_id)
        self._api_resource_cache = {}
        # Maps each created CfnResource's .ref token → its full URL path
        # ("v1/groups/{groupId}/subgroups"), used to produce stable, path-
        # independent logical IDs for resources and methods.
        self._api_resource_path_by_ref = {}

def lambda_log_group_arn(scope: Construct, aws_function_name: str) -> str:
    """ARN of `/aws/lambda/<aws_function_name>` and its streams, for a logging PolicyStatement.

    `aws_function_name` may end in `*` for functions whose physical name CloudFormation
    generates, where the exact name is not known at synth time.
    """
    return Stack.of(scope).format_arn(
        service="logs",
        resource="log-group",
        resource_name=f"/aws/lambda/{aws_function_name}:*",
        arn_format=ArnFormat.COLON_RESOURCE_NAME,
    )


def create_base_lambda_role(scope: Construct, lambda_name: str, common_resources: CommonResources = None) -> iam.Role:
    """Execution role for a Lambda. The logging grant is added by create_lambda_function, which
    is the first point that knows the physical function name the log group is derived from."""

    iam_role_name = None
    if common_resources is not None:
        kebab_replace = lambda_name.replace("_", "-")
        region = Stack.of(scope).region
        iam_role_name = f"{common_resources.prefix}{kebab_replace}-role-{region}"

    role = iam.Role(
        scope,
        lambda_name + "_lambda_role",
        role_name=iam_role_name,
        assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
    )

    # Stabilise the IAM role's logical ID. The inline DefaultPolicy auxiliary L1
    # remains path-derived (CDK aux L1 — refactor cost still applies if the
    # role's construct parent changes).
    stable_id = stable_logical_id("IAMRole", lambda_name + "_lambda_role")
    role.node.default_child.override_logical_id(stable_id)

    return role


def common_api_policy(lambda_role: iam.Role, common_resources: CommonResources, region: str = Aws.REGION):
    """
    Add IAM policy for Cognito User Pool operations needed for authentication and authorization.
    This grants permissions to read user attributes from Cognito User Pool, which is required for:
    - user.NewContextWithAPIRequest() - to authenticate users and create request context
    - user.User.IsSuperAdmin() - to check if a user is a super admin
    
    Also grants permissions to read JWKS from SSM Parameter Store, which is required for:
    - cognito.NewCognitoService() - to fetch and parse JWKS for token validation
    
    Args:
        lambda_role: The IAM role to add the policy to
        common_resources: CommonResources object containing user pool IDs and JWKS parameter names
    """
    resources = []

    if common_resources and hasattr(common_resources, 'esp_admin_user_pool_id') and common_resources.esp_admin_user_pool_id:
        resources.append(get_user_pool_arn(common_resources.esp_admin_user_pool_id, region))
    
    # Only add policy if we have at least one user pool
    if resources:
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-idp:GetUser",
                "cognito-idp:AdminGetUser"
            ],
            resources=resources
        ))
    
    # Collect JWKS SSM parameter ARNs
    jwks_resources = []
    
    # Check for ESP user pool JWKS parameters
    if common_resources and hasattr(common_resources, 'esp_user_jwks') and common_resources.esp_user_jwks:
        jwks_param = common_resources.esp_user_jwks
        jwks_resources.append(get_ssm_parameter_arn(jwks_param, region))
        jwks_resources.append(f"arn:aws:ssm:{region}:{Aws.ACCOUNT_ID}:parameter{jwks_param}")
    
    if common_resources and hasattr(common_resources, 'esp_admin_user_pool_jwks') and common_resources.esp_admin_user_pool_jwks:
        admin_jwks_param = common_resources.esp_admin_user_pool_jwks
        jwks_arn = get_ssm_parameter_arn(admin_jwks_param, region)
        if jwks_arn not in jwks_resources:
            jwks_resources.append(jwks_arn)
            jwks_resources.append(f"arn:aws:ssm:{region}:{Aws.ACCOUNT_ID}:parameter{admin_jwks_param}")
    
    # RLOG: persistent log-level config read at Lambda cold start
    rlog_arn = f"arn:aws:ssm:{region}:{Aws.ACCOUNT_ID}:parameter/rmng/rlog/*"
    jwks_resources.append(rlog_arn)

    # Only add SSM policy if we have at least one SSM parameter
    if jwks_resources:
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter"
            ],
            resources=jwks_resources
        ))

    # NewContextWithAPIRequest resolves an OIDC end-user caller by reading their
    # espuser-user-details row (GetItem by user_id); admins still resolve via Cognito.
    # Every end-user lambda needs this read now that end users are OIDC-federated.
    from src.rmneo.stacks.base_res_constants import TABLE_NAMES
    lambda_role.add_to_policy(iam.PolicyStatement(
        actions=["dynamodb:GetItem"],
        resources=[get_table_arn(TABLE_NAMES['USER_DETAILS'], region)]
    ))


def create_rest_api(scope: Construct, id: str, rest_api_name: str, description: str) -> apigateway.RestApi:
    """
    Create a REST API Gateway with standard CORS preflight options, throttling,
    and default 4XX/5XX CORS gateway responses.

    Args:
        scope: The CDK scope
        id: Unique construct identifier
        rest_api_name: Name of the REST API
        description: Description of the REST API

    Returns:
        The created RestApi
    """
    # Stabilise the RestApi L1. Aux L1s still path-derived:
    #   - Deployment, Stage (created because deploy=True)
    #   - Account (CloudWatch role wiring, created lazily by CDK)
    #   - CloudWatchRole (if logging is enabled at stack level)
    # The 3 CfnGatewayResponse L1s below are explicit L1s — their own override
    # is applied next to each.
    api = apigateway.RestApi(
        scope, id,
        rest_api_name=rest_api_name,
        description=description,
        deploy=True,
        default_cors_preflight_options=apigateway.CorsOptions(
            allow_credentials=False,
            allow_headers=[
                "Content-Type",
                "X-Amz-Date",
                "Authorization",
                "X-Api-Key",
                "X-Amz-Security-Token",
                "X-Amz-User-Agent",
                "X-Amz-Content-Sha256",
                "X-Requested-With"
            ],
            allow_methods=["GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE"],
            allow_origins=["*"],
            max_age=Duration.days(1)
        ),
        deploy_options=apigateway.StageOptions(
            stage_name="prod",
            throttling_rate_limit=2000,
            throttling_burst_limit=5000,
        ),
    )
    api.node.default_child.override_logical_id(stable_logical_id("ApiGwRest", id))
    # RestApi emits a "<id>Endpoint<hash>" output hardcoded to the execute-api URL.
    # It ignores any custom domain and duplicates ApiGatewayUrl/EspUserApiUrl, so drop it.
    api.node.try_remove_child("Endpoint")
    # Stabilise the auto-created Stage and Deployment too. Stage uniqueness on
    # AWS is (RestApi, StageName) — with the RestApi stable and StageName="prod"
    # fixed, a path-derived Stage logical ID would conflict on rename.
    # Deployment has no physical name but pinning it keeps diffs minimal.
    api.deployment_stage.node.default_child.override_logical_id(
        stable_logical_id("ApiGwStage", f"{id}-prod"))
    api.latest_deployment.node.default_child.override_logical_id(
        stable_logical_id("ApiGwDeployment", id))

    # Add gateway response for unauthorized requests
    unauthorized = apigateway.CfnGatewayResponse(
        scope, f"{id}UnauthorizedResponse",
        rest_api_id=api.rest_api_id,
        response_type="UNAUTHORIZED",
        response_parameters={
            "gatewayresponse.header.WWW-Authenticate": "'Bearer realm=\"Cognito\"'"
        },
        status_code="401"
    )
    unauthorized.override_logical_id(stable_logical_id("ApiGwResponse", f"{id}-unauthorized"))

    # Add CORS headers to default 4XX/5XX gateway responses so browsers
    # can read error details instead of seeing an opaque CORS failure
    default_4xx = apigateway.CfnGatewayResponse(
        scope, f"{id}Default4xxResponse",
        rest_api_id=api.rest_api_id,
        response_type="DEFAULT_4XX",
        response_parameters={
            "gatewayresponse.header.Access-Control-Allow-Origin": "'*'",
            "gatewayresponse.header.Access-Control-Allow-Headers": "'Content-Type,X-Amz-Date,Authorization,X-Api-Key,X-Amz-Security-Token'",
        },
    )
    default_4xx.override_logical_id(stable_logical_id("ApiGwResponse", f"{id}-default-4xx"))
    default_5xx = apigateway.CfnGatewayResponse(
        scope, f"{id}Default5xxResponse",
        rest_api_id=api.rest_api_id,
        response_type="DEFAULT_5XX",
        response_parameters={
            "gatewayresponse.header.Access-Control-Allow-Origin": "'*'",
            "gatewayresponse.header.Access-Control-Allow-Headers": "'Content-Type,X-Amz-Date,Authorization,X-Api-Key,X-Amz-Security-Token'",
        },
    )
    default_5xx.override_logical_id(stable_logical_id("ApiGwResponse", f"{id}-default-5xx"))

    return api


def create_lambda_layer(scope: Construct, id: str, layer_path: str, description: str) -> lambda_.LayerVersion:
    """
    Create a Lambda Layer from a local directory.

    Args:
        scope: The CDK scope
        id: Unique identifier for the layer
        layer_path: Absolute path to the layer's root directory. Layer content is
            owned by the consuming module, not by core, so the caller resolves the
            path (typically relative to its own __file__) instead of naming a
            directory under a core-owned layers/ folder.
        description: Description of the layer

    Returns:
        lambda_.LayerVersion: The created layer
    """
    layer = lambda_.LayerVersion(
        scope,
        id,
        code=lambda_.Code.from_asset(layer_path),
        compatible_runtimes=[lambda_.Runtime.PROVIDED_AL2023],
        compatible_architectures=[lambda_.Architecture.ARM_64],
        description=description
    )
    # Use `id` (caller-supplied, unique within scope) rather than the layer path
    # so that two consumers loading the same binary layer get distinct IDs.
    layer.node.default_child.override_logical_id(stable_logical_id("LambdaLayer", id))
    return layer

def create_lambda_log_group(
    scope: Construct,
    construct_id: str,
    *,
    purpose: str,
    aws_function_name: str,
    retention: logs.RetentionDays = logs.RetentionDays.ONE_WEEK,
) -> logs.LogGroup:
    """CDK-managed `/aws/lambda/<aws_function_name>` group to pass as Function(log_group=...).

    Replaces the deprecated `log_retention` prop, which capped retention via a LogRetention
    custom resource (a deploy-time Lambda) rather than a real CloudFormation resource.

    `purpose` seeds the logical ID and must be a literal string; `aws_function_name` may be a
    CDK token (the Alexa stack builds its name with Fn.join), which is why the two are separate.

    MIGRATION: a stack that already deployed this Lambda owns an unmanaged log group of the same
    name, and CloudFormation rejects the change set with AWS::EarlyValidation::ResourceExistenceCheck.
    Clear them first with scripts/delete_unmanaged_lambda_log_groups.py. `cdk deploy
    --import-existing-resources` does NOT work here: auto-import demands a DeletionPolicy of Retain
    or RetainExceptOnCreate, and RemovalPolicy.DESTROY below is deliberate — retaining would leave
    the group behind on `cdk destroy` and collide again on the next deploy. Retention is 7 days, so
    clearing costs at most a week of logs. See managed_log_group on create_lambda_function for the
    per-function opt-out.
    """
    log_group = logs.LogGroup(
        scope, construct_id,
        log_group_name=f"/aws/lambda/{aws_function_name}",
        retention=retention,
        removal_policy=RemovalPolicy.DESTROY,
    )
    log_group.node.default_child.override_logical_id(
        stable_logical_id("LambdaLogGrp", purpose))
    return log_group


def create_lambda_function(
    scope: Construct,
    function_name: str,
    common_resources: CommonResources,
    *,
    lambda_role: iam.Role = None,
    environment: dict = None,
    timeout: Duration = Duration.seconds(30),
    layers: list = None,
    region: str = None,
    aws_function_name: str = None,
    log_retention: logs.RetentionDays = logs.RetentionDays.ONE_WEEK,
    managed_log_group: bool = True,
) -> lambda_.Function:
    """Create a Lambda from build/{function_name}/.

    function_name is the purpose/binary name, used for asset path and construct id
    (passed verbatim). The deployed AWS Lambda name is `{common_resources.prefix}
    {function_name}` — the prefix is set when CommonResources is constructed
    (e.g., prefix="rmng-" in cdk/apps/rmng.py produces Lambda "rmng-hello_world" from
    purpose "hello_world").

    aws_function_name (optional) overrides the auto-derived AWS name. Used when
    the deployed name is a CDK token (e.g., Alexa Skill stack uses
    Fn.join("", ["rmng-alexa-alexa_skill_", rmng_region]) so the region is
    embedded via CloudFormation resolution).

    log_retention caps the /aws/lambda/<name> log group's retention (default 7
    days) so application log groups don't accumulate forever and inflate
    CloudWatch cost. Override per-lambda when a function genuinely needs longer
    history. CDK-managed custom resources (AwsCustomResource and provider
    frameworks; deploy-time only) are intentionally left at CloudWatch's
    never-expire default.

    managed_log_group=True (the default) declares the log group as a
    CloudFormation resource, which is the only way to enforce log_retention.
    Set it to False for a function whose /aws/lambda/<name> group already exists
    outside CloudFormation: CFN cannot create over an existing group and fails
    the deploy with 'already exists'. Lambda then auto-creates the group on first
    invocation and log_retention does not apply — logs never expire, so treat
    False as a temporary migration escape hatch, not a steady state.
    """

    # AWS-deployed Lambda physical name = <common_resources.prefix><purpose>
    # unless caller supplied an explicit aws_function_name (e.g., region-suffixed
    # Alexa name). `prefix` is set when CommonResources is constructed in the cdk/apps/ entry point.
    prefix = getattr(common_resources, "prefix", "") if common_resources else ""
    kebab_replace = function_name.replace("_", "-")
    resolved_aws_name = aws_function_name if aws_function_name is not None else f"{prefix}{kebab_replace}"

    if lambda_role is None:
        # Create role with function name
        lambda_role = create_base_lambda_role(scope, function_name, common_resources)

    # Replaces the AWSLambdaBasicExecutionRole managed policy, which granted these on `*`.
    # Applied here rather than in create_base_lambda_role because resolved_aws_name — and so
    # the log group name — is only known once aws_function_name has been defaulted.
    lambda_role.add_to_policy(
        iam.PolicyStatement(
            effect=iam.Effect.ALLOW,
            actions=["logs:CreateLogStream", "logs:PutLogEvents"],
            resources=[lambda_log_group_arn(scope, resolved_aws_name)],
        )
    )


    # Use stack's deployment region for IAM policy ARNs (SSM, Cognito, DynamoDB are region-scoped).
    # This avoids mismatch when AWS_REGION env differs from actual deployment region.
    policy_region = region if region is not None else Stack.of(scope).region

    # Add common API policy for authentication and authorization
    common_api_policy(lambda_role, common_resources, policy_region)

    if environment is None:
        environment = {}

    base_env = {
        "AWS_ACCOUNT_ID": Aws.ACCOUNT_ID,
        "USER_CLIENT_ID": common_resources.esp_user_client_id if common_resources and hasattr(common_resources, 'esp_user_client_id') and common_resources.esp_user_client_id else "",
        "USER_JWKS_PARA_NAME": common_resources.esp_user_jwks if common_resources and hasattr(common_resources, 'esp_user_jwks') and common_resources.esp_user_jwks else "",
        "ADMIN_USER_POOL_ID": common_resources.esp_admin_user_pool_id if common_resources and hasattr(common_resources, 'esp_admin_user_pool_id') and common_resources.esp_admin_user_pool_id else "",
        "ADMIN_USER_POOL_CLIENT_ID": common_resources.esp_admin_user_pool_client_id if common_resources and hasattr(common_resources, 'esp_admin_user_pool_client_id') and common_resources.esp_admin_user_pool_client_id else "",
        "ADMIN_USER_POOL_JWKS_PARA_NAME": common_resources.esp_admin_user_pool_jwks if common_resources and hasattr(common_resources, 'esp_admin_user_pool_jwks') and common_resources.esp_admin_user_pool_jwks else "",
        "USER_ISSUER": common_resources.esp_user_issuer if common_resources and hasattr(common_resources, 'esp_user_issuer') and common_resources.esp_user_issuer else "",
    }
    # Allow caller to override environment variables
    base_env.update(environment)
    binary_path = str(REPO_ROOT / "build" / function_name)

    # Create the Lambda function with the binary directory as an asset
    lambda_props = {
        "function_name": resolved_aws_name,
        "handler": "bootstrap",
        "code": lambda_.Code.from_asset(path=binary_path),
        "runtime": lambda_.Runtime.PROVIDED_AL2023,
        "architecture": lambda_.Architecture.ARM_64,
        "memory_size": 128,
        "timeout": timeout,
        "role": lambda_role,
        "environment": base_env,
    }

    # Omitting log_group entirely (rather than passing None) leaves the Lambda service to
    # create /aws/lambda/<name> implicitly, which is what makes the opt-out collision-free.
    if managed_log_group:
        lambda_props["log_group"] = create_lambda_log_group(
            scope, f"{function_name}_log_group",
            purpose=function_name,
            aws_function_name=resolved_aws_name,
            retention=log_retention,
        )

    if layers:
        lambda_props["layers"] = layers

    func = lambda_.Function(scope, function_name, **lambda_props)
    # Stabilise the Lambda function's logical ID. Auxiliary L1s that the L2
    # Function construct creates and that still use path-derived logical IDs:
    #   - EventInvokeConfig (if onSuccess/onFailure configured)
    #   - Version, Alias (if versioning configured)
    # The lambda's own IAM role logical ID is stabilised separately by
    # create_base_lambda_role when the caller doesn't pass lambda_role=.
    func.node.default_child.override_logical_id(stable_logical_id("LambdaFunc", function_name))
    return func


@dataclass
class SqsLambdaInfra:
    """Result of setup_sqs_lambda_infra. The IoT topic rule that fans out events to
    `queue` should use `rule_action` and `error_action`, and call add_resource_dependency for
    each entry in `dependencies` to ensure the rule's roles exist first.
    `error_role` is the IAM role assumed by IoT for the CloudWatch Logs error action;
    callers that need to grant iam:PassRole (e.g. the iot-event-mode admin lambda
    when calling ReplaceTopicRule) need both `iot_rule_role` and this role."""
    queue: sqs.Queue
    dlq: sqs.Queue
    rule_action: iot.CfnTopicRule.ActionProperty
    error_action: iot.CfnTopicRule.ActionProperty
    error_role: iam.Role
    dependencies: list


def setup_sqs_lambda_infra(
    scope: Construct,
    name_prefix: str,
    construct_id_prefix: str,
    *,
    lambda_function: lambda_.Function,
    lambda_role: iam.Role,
    iot_rule_role: iam.Role,
    delivery_delay: Duration = None,
) -> SqsLambdaInfra:
    """Wire a Standard SQS queue (with DLQ) as a batch event source for `lambda_function`,
    and prepare an IoT topic-rule action that publishes to that queue plus a CloudWatch
    Logs error action for the rule.

    The caller owns the IoT rule itself (SQL/topic differs per use site) and the
    `iot_rule_role`; this function adds sqs:SendMessage to it. Use the returned
    `rule_action`, `error_action`, and `dependencies` when building the rule.

    Names: queue=f"{name_prefix}-queue", dlq=f"{name_prefix}-dlq",
    log group=f"IoTRules/{construct_id_prefix}".

    delivery_delay: how long SQS holds new messages before they become visible.
    Use this instead of an in-lambda sleep when the handler needs a grace period
    (e.g. presence_event_handler waits for a reconnect's DynamoDB write to land).
    """
    dlq = sqs.Queue(
        scope, f"{construct_id_prefix}DLQ",
        queue_name=f"{name_prefix}-dlq",
        retention_period=Duration.days(14),
    )
    dlq.node.default_child.override_logical_id(
        stable_logical_id("SQSQueue", f"{name_prefix}-dlq"))
    queue_kwargs = dict(
        queue_name=f"{name_prefix}-queue",
        visibility_timeout=Duration.minutes(3),
        dead_letter_queue=sqs.DeadLetterQueue(max_receive_count=3, queue=dlq),
    )
    if delivery_delay is not None:
        queue_kwargs["delivery_delay"] = delivery_delay
    queue = sqs.Queue(scope, f"{construct_id_prefix}Queue", **queue_kwargs)
    queue.node.default_child.override_logical_id(
        stable_logical_id("SQSQueue", f"{name_prefix}-queue"))

    lambda_role.add_to_policy(iam.PolicyStatement(
        actions=[
            "sqs:ReceiveMessage",
            "sqs:DeleteMessage",
            "sqs:GetQueueAttributes",
        ],
        resources=[queue.queue_arn],
    ))

    iot_rule_role.add_to_policy(iam.PolicyStatement(
        actions=["sqs:SendMessage"],
        resources=[queue.queue_arn],
    ))

    # Lambda::EventSourceMapping has no physical name → safe to move across files (CFN delete-create, no "already exists" conflict).
    lambda_function.add_event_source(
        event_sources.SqsEventSource(
            queue,
            batch_size=10,
            max_batching_window=Duration.seconds(1),
            report_batch_item_failures=True,
        )
    )

    error_log_group = logs.LogGroup(
        scope, f"{construct_id_prefix}IoTRuleLogGroup",
        log_group_name=f"IoTRules/{construct_id_prefix}",
        retention=logs.RetentionDays.ONE_WEEK,
        removal_policy=RemovalPolicy.DESTROY,
    )
    error_log_group.node.default_child.override_logical_id(
        stable_logical_id("LogGrp", f"iot-rules-{construct_id_prefix}"))
    # Inline DefaultPolicy aux L1 still path-derived.
    error_role = iam.Role(
        scope, f"{construct_id_prefix}IoTRuleErrorRole",
        assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
        description="Role for IoT rule error logging",
    )
    error_role.node.default_child.override_logical_id(
        stable_logical_id("IAMRole", f"{construct_id_prefix}-iot-rule-error-role"))
    error_role.add_to_policy(iam.PolicyStatement(
        actions=["logs:CreateLogStream", "logs:PutLogEvents"],
        resources=[error_log_group.log_group_arn],
    ))

    rule_action = iot.CfnTopicRule.ActionProperty(
        sqs=iot.CfnTopicRule.SqsActionProperty(
            queue_url=queue.queue_url,
            role_arn=iot_rule_role.role_arn,
            use_base64=False,
        )
    )
    error_action = iot.CfnTopicRule.ActionProperty(
        cloudwatch_logs=iot.CfnTopicRule.CloudwatchLogsActionProperty(
            log_group_name=error_log_group.log_group_name,
            role_arn=error_role.role_arn,
        )
    )

    return SqsLambdaInfra(
        queue=queue,
        dlq=dlq,
        rule_action=rule_action,
        error_action=error_action,
        error_role=error_role,
        dependencies=[iot_rule_role.node.default_child, error_role.node.default_child],
    )


def create_s3_bucket(scope: Construct, id: str, common_resources: CommonResources, purpose: str,
                     **bucket_kwargs) -> s3.Bucket:
    """Create a general-purpose S3 bucket. Physical name prefix is composed as
    `{common_resources.prefix}{purpose}` and AWS appends `-<account>-<region>-an`
    via the Account-Regional Namespace opt-in, which is also what makes the name
    globally unique — callers must not hand-roll an account/region suffix.

    `bucket_kwargs` passes through to `s3.Bucket`, overriding the defaults below,
    so a caller can supply e.g. its own `block_public_access`.
    """
    bucket_name_prefix = f"{common_resources.prefix}{purpose}"
    bucket_kwargs.setdefault("removal_policy", RemovalPolicy.DESTROY)
    bucket_kwargs.setdefault("auto_delete_objects", True)
    bucket_kwargs.setdefault("encryption", s3.BucketEncryption.S3_MANAGED)
    bucket = s3.Bucket(scope, id, **bucket_kwargs)
    
    # Opt into S3 Account-Regional Namespace via L1 escape hatch.
    # aws-cdk-lib 2.198 predates the 2025 feature and has no native kwargs for
    # BucketNamePrefix / BucketNamespace; remove once CDK is bumped.
    cfn_bucket = bucket.node.default_child
    cfn_bucket.add_property_override("BucketNamePrefix", bucket_name_prefix)
    cfn_bucket.add_property_override("BucketNamespace", "account-regional")
    cfn_bucket.add_property_deletion_override("BucketName")
    cfn_bucket.override_logical_id(stable_logical_id("S3Bucket", bucket_name_prefix))
    # Pin Custom::S3AutoDeleteObjects — its Delete handler empties the bucket.
    auto_delete = bucket.node.try_find_child("AutoDeleteObjectsCustomResource")
    if auto_delete is not None:
        auto_delete.node.default_child.override_logical_id(
            stable_logical_id("CustomS3AutoDelete", bucket_name_prefix))
    # Pin BucketPolicy — PutBucketPolicy + DeleteBucketPolicy on rename strips
    # the bucket's policy silently.
    policy = bucket.node.try_find_child("Policy")
    if policy is not None:
        policy.node.default_child.override_logical_id(
            stable_logical_id("S3BucketPolicy", bucket_name_prefix))
    return bucket


def create_kms_signing_key(
    scope: Construct,
    id: str,
    *,
    alias_name: str,
    description: str,
    key_spec: kms.KeySpec,
    removal_policy: RemovalPolicy = RemovalPolicy.RETAIN,
) -> kms.Key:
    """Create an asymmetric SIGN_VERIFY KMS key with a separate Alias, both logical-ID pinned.

    Defaults to RETAIN: signing keys cannot be regenerated, so a path-derived logical ID would let a
    construct move orphan the live key and mint a new one — every signature already issued stops
    verifying, and the retained key lingers untracked and billed.

    `alias_name` is the bare alias (no `alias/` prefix) and doubles as the logical-ID input, so the
    IDs track the physical alias rather than the construct path. Key rotation is not enabled:
    unsupported for asymmetric keys.
    """
    key = kms.Key(
        scope, id,
        description=description,
        key_spec=key_spec,
        key_usage=kms.KeyUsage.SIGN_VERIFY,
        enable_key_rotation=False,
        removal_policy=removal_policy,
    )
    key.node.default_child.override_logical_id(stable_logical_id("KMSKey", alias_name))
    alias = kms.Alias(
        scope, f"{id}Alias",
        alias_name=f"alias/{alias_name}",
        target_key=key,
    )
    alias.node.default_child.override_logical_id(stable_logical_id("KMSAlias", alias_name))
    return key


def create_iot_rule_log_group(
    scope: Construct,
    construct_id: str,
    *,
    rule_name: str,
) -> logs.LogGroup:
    """Create a CloudWatch LogGroup for an IoT topic rule's error action.
    Physical name is `iot-rules/<rule_name>` (globally-unique per region) so the
    logical ID is stabilised to avoid 'already exists' on refactor.
    """
    log_group_name = f"iot-rules/{rule_name}"
    log_group = logs.LogGroup(
        scope, construct_id,
        log_group_name=log_group_name,
        retention=logs.RetentionDays.ONE_WEEK,
        removal_policy=RemovalPolicy.DESTROY,
    )
    log_group.node.default_child.override_logical_id(
        stable_logical_id("LogGrp", log_group_name))
    return log_group


def create_sqs_queue_with_dlq(
    scope: Construct,
    construct_id: str,
    *,
    queue_name: str,
    visibility_timeout: Duration,
    max_receive_count: int = 3,
    dlq_retention: Duration = Duration.days(14),
) -> sqs.Queue:
    """Create a Standard SQS queue paired with a DLQ. Names are
    `<queue_name>` and `<queue_name>-dlq` (the helper strips a trailing
    '-queue' before adding '-dlq' so `psu-queue` → `psu-dlq`). Both logical
    IDs are stabilised.
    """
    dlq_base = queue_name[:-len("-queue")] if queue_name.endswith("-queue") else queue_name
    dlq_name = f"{dlq_base}-dlq"
    dlq = sqs.Queue(
        scope, f"{construct_id}DLQ",
        queue_name=dlq_name,
        retention_period=dlq_retention,
    )
    dlq.node.default_child.override_logical_id(stable_logical_id("SQSQueue", dlq_name))
    queue = sqs.Queue(
        scope, construct_id,
        queue_name=queue_name,
        visibility_timeout=visibility_timeout,
        dead_letter_queue=sqs.DeadLetterQueue(max_receive_count=max_receive_count, queue=dlq),
    )
    queue.node.default_child.override_logical_id(stable_logical_id("SQSQueue", queue_name))
    return queue


def create_iot_rule_role(
    scope: Construct,
    construct_id: str,
    *,
    role_name: str,
    common_resources: CommonResources,
    description: str = None,
) -> iam.Role:
    """Create an IAM role for an AWS IoT topic rule (assumed_by iot.amazonaws.com).
    Caller adds policies via `.add_to_policy(...)`. The inline DefaultPolicy
    auxiliary L1 (created when the caller adds policies) stays path-derived.
    """
    region = Stack.of(scope).region
    role = iam.Role(
        scope, construct_id,
        role_name=f"{common_resources.prefix}{role_name}-{region}",
        assumed_by=iam.ServicePrincipal("iot.amazonaws.com"),
        description=description,
    )
    role.node.default_child.override_logical_id(stable_logical_id("IAMRole", role_name))
    return role


def create_iot_topic_rule(
    scope: Construct,
    id: str,
    *,
    rule_name: str,
    topic_rule_payload: iot.CfnTopicRule.TopicRulePayloadProperty,
) -> iot.CfnTopicRule:
    """Create an AWS IoT topic rule with a stable logical ID derived from `rule_name`.
    `rule_name` is the AWS-required snake_case identifier (e.g. `node_connected_rule`).
    """
    rule = iot.CfnTopicRule(
        scope, id,
        rule_name=rule_name,
        topic_rule_payload=topic_rule_payload,
    )
    rule.override_logical_id(stable_logical_id("IoTTopicRule", rule_name))
    return rule


def create_iot_role_alias(
    scope: Construct,
    id: str,
    *,
    role_alias: str,
    role_arn: str,
) -> iot.CfnRoleAlias:
    """Create an AWS IoT role alias with a stable logical ID derived from `role_alias`.
    `role_alias` is consumed by device firmware via the IoT Credential Provider URL
    `/role-aliases/<alias>/credentials` — treat it as a fielded public contract.
    """
    alias = iot.CfnRoleAlias(
        scope, id,
        role_alias=role_alias,
        role_arn=role_arn,
    )
    alias.override_logical_id(stable_logical_id("IoTRoleAlias", role_alias))
    return alias


def create_cognito_authorizer(
    scope: Construct,
    id: str,
    *,
    authorizer_name: str,
    user_pool_arn: str,
    rest_api_id: str,
) -> apigateway.CfnAuthorizer:
    """Create a Cognito User Pool authorizer for an API Gateway REST API with a
    stable logical ID derived from `authorizer_name`.
    """
    auth = apigateway.CfnAuthorizer(
        scope, id,
        rest_api_id=rest_api_id,
        type="COGNITO_USER_POOLS",
        name=authorizer_name,
        identity_source="method.request.header.Authorization",
        provider_arns=[user_pool_arn],
    )
    auth.override_logical_id(stable_logical_id("ApiGwAuthorizer", authorizer_name))
    return auth


def create_http_api(
    scope: Construct,
    id: str,
    *,
    api_name: str,
    cors_preflight: apigwv2.CorsPreflightOptions = None,
) -> apigwv2.HttpApi:
    """Create an HTTP API Gateway v2 with a stable logical ID derived from `api_name`.
    The auto-created `$default` Stage is stabilised too — Stage uniqueness is
    (HttpApi, StageName) and StageName="$default" is fixed, so a path-derived
    Stage logical ID would conflict on rename.
    """
    api = apigwv2.HttpApi(scope, id, api_name=api_name, cors_preflight=cors_preflight)
    api.node.default_child.override_logical_id(stable_logical_id("ApiGwV2Api", api_name))
    api.default_stage.node.default_child.override_logical_id(
        stable_logical_id("ApiGwV2Stage", f"{api_name}-default"))
    return api


def create_cognito_user_pool(
    scope: Construct,
    id: str,
    *,
    user_pool_name: str,
    **kwargs,
) -> cognito.UserPool:
    """Create a Cognito user pool with a stable logical ID derived from
    user_pool_name (the globally-unique pool name like "rmng-user-pool").
    Extra kwargs are forwarded to cognito.UserPool (e.g. sign_in_aliases,
    password_policy, mfa, account_recovery, etc.).

    Settings shared by every RMNG pool (removal policy, email/phone sign-in
    aliasing, auto-verification, verification messages) are applied as
    defaults; pass the kwarg explicitly to override.
    """
    kwargs.setdefault("removal_policy", RemovalPolicy.DESTROY)
    kwargs.setdefault("auto_verify", cognito.AutoVerifiedAttrs(email=True, phone=True))
    kwargs.setdefault("keep_original", cognito.KeepOriginalAttrs(email=True, phone=True))
    kwargs.setdefault("user_verification", cognito.UserVerificationConfig(
        email_subject="Verify your email",
        email_body="Your verification code is {####}",
        email_style=cognito.VerificationEmailStyle.CODE,
        sms_message="Your verification code is {####}",
    ))
    kwargs.setdefault("sign_in_aliases", cognito.SignInAliases(email=True, phone=True))
    kwargs.setdefault("sign_in_case_sensitive", False)
    kwargs.setdefault("password_policy", cognito.PasswordPolicy(
        min_length=8,
        require_uppercase=True,
        require_lowercase=True,
        require_digits=True,
        require_symbols=True,
    ))
    kwargs.setdefault("mfa", cognito.Mfa.OPTIONAL)
    kwargs.setdefault("mfa_second_factor", cognito.MfaSecondFactor(sms=True, otp=True))
    pool = cognito.UserPool(scope, id, user_pool_name=user_pool_name, **kwargs)
    pool.node.default_child.override_logical_id(stable_logical_id("CognitoUserPool", user_pool_name))
    return pool


def create_cognito_user_pool_client(
    scope: Construct,
    id: str,
    *,
    user_pool: cognito.IUserPool,
    user_pool_client_name: str,
    **kwargs,
) -> cognito.UserPoolClient:
    """Create a Cognito user pool client with a stable logical ID derived from
    user_pool_client_name (e.g. "rmng-user-pool-client", "mcp-oauth-client").

    Settings shared by every RMNG client (non-enumerating error responses,
    password/SRP auth flows, writable standard attributes) are applied as
    defaults; pass the kwarg explicitly to override.
    """
    kwargs.setdefault("prevent_user_existence_errors", True)
    kwargs.setdefault("auth_flows", cognito.AuthFlow(user_password=True, user_srp=True))
    kwargs.setdefault("write_attributes", cognito.ClientAttributes().with_standard_attributes(
        address=True, birthdate=True, email=True, family_name=True, fullname=True,
        gender=True, given_name=True, locale=True, middle_name=True, nickname=True,
        phone_number=True, preferred_username=True, profile_page=True, profile_picture=True,
        timezone=True, website=True,
    ))
    kwargs.setdefault("enable_token_revocation", True)
    kwargs.setdefault("access_token_validity", Duration.hours(1))
    kwargs.setdefault("id_token_validity", Duration.hours(1))
    client = cognito.UserPoolClient(
        scope, id,
        user_pool=user_pool,
        user_pool_client_name=user_pool_client_name,
        **kwargs,
    )
    client.node.default_child.override_logical_id(
        stable_logical_id("CognitoUserPoolClient", user_pool_client_name))
    return client


def create_cognito_user_pool_domain(
    scope: Construct,
    id: str,
    *,
    user_pool: cognito.IUserPool,
    domain_prefix: str,
    **kwargs,
) -> cognito.UserPoolDomain:
    """Create a Cognito user pool domain (Cognito-hosted prefix style)
    with a stable logical ID derived from the construct ``id``.

    Note: ``domain_prefix`` typically embeds ``scope.account``/``scope.region``
    tokens, whose stringified counters shift between synths. Feeding it into
    ``stable_logical_id`` produced logical IDs that drifted on every synth
    (e.g. ``…TokenAWSAccountId7TokenAWSRegion11`` → ``…TokenAWSAccountId9TokenAWSRegion13``),
    which in turn made CloudFormation see an Add + Remove pair with the same
    physical domain name on every redeploy and fail ``ResourceExistenceCheck``.
    We base the logical ID on the caller-provided ``id`` instead, which is a
    plain ASCII string and stable across synths.
    """
    domain = user_pool.add_domain(id, cognito_domain=cognito.CognitoDomainOptions(
        domain_prefix=domain_prefix,
    ), **kwargs)
    # add_domain returns a UserPoolDomain L2 whose default_child is CfnUserPoolDomain.
    # We pin the logical ID to the construct `id` (a plain ASCII string) instead
    # of `domain_prefix` because the prefix embeds CDK tokens for account/region
    # whose stringified counter shifts every synth and broke redeploys with
    # `ResourceExistenceCheck`.
    domain.node.default_child.override_logical_id(
        stable_logical_id("CognitoUserPoolDomain", id))
    return domain


def add_http_api_routes(
    http_api: apigwv2.HttpApi,
    *,
    path: str,
    methods: list,
    integration,
) -> list:
    """Add routes to an HTTP API gateway v2 and stabilise each returned route's
    logical ID using `<path>-<method>`. Wraps `http_api.add_routes(...)`.
    """
    routes = http_api.add_routes(path=path, methods=methods, integration=integration)
    for method, route in zip(methods, routes):
        route.node.default_child.override_logical_id(
            stable_logical_id("ApiGwV2Route", f"{path}-{method.value}")
        )
    return routes


def create_ssm_string_parameter(
    scope: Construct,
    id: str,
    *,
    parameter_name: str,
    string_value: str,
    description: str = None,
) -> ssm.StringParameter:
    """Create an SSM string parameter with a stable logical ID derived from
    `parameter_name` (the globally-unique SSM path, e.g. `/rmng/base/api-gateway-id`).
    """
    param = ssm.StringParameter(
        scope, id,
        parameter_name=parameter_name,
        string_value=string_value,
        description=description,
    )
    param.node.default_child.override_logical_id(stable_logical_id("SSMParam", parameter_name))
    return param


# CloudFormation re-invokes a custom resource only when its properties change, but the
# discovery handlers below must observe what is attached in AWS *now*: custom domains are
# created out-of-band by misc/scripts/custom_domains.py and the documented flow is
# "attach the domains, then redeploy so the stacks discover and publish them". A fresh
# per-synth salt in every discovery resource's properties turns each deploy into an
# Update, so the handlers re-run and stale hostnames/URLs cannot survive a redeploy.
# The handlers ignore the property itself.
_DISCOVERY_SALT = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


_API_DOMAIN_DISCOVERY_CODE = """
import json
import boto3
import cfnresponse
from botocore.config import Config

# GetDomainNames/GetBasePathMappings hit API Gateway's account-region control plane, and the api + mcp discovery custom resources across stacks call it concurrently during a parallel deploy, self-throttling to TooManyRequestsException. Adaptive mode adds a client-side rate limiter plus exponential backoff with jitter, and the higher attempt cap rides out the throttle instead of failing the stack after the default 4 retries.
_RETRY = Config(retries={'max_attempts': 10, 'mode': 'adaptive'})

def _discover_rest(api_id):
    client = boto3.client('apigateway', config=_RETRY)
    for page in client.get_paginator('get_domain_names').paginate():
        for item in page.get('items', []):
            candidate = item['domainName']
            try:
                mappings = client.get_base_path_mappings(domainName=candidate)
            except client.exceptions.NotFoundException:
                continue
            for mapping in mappings.get('items', []):
                if mapping.get('restApiId') == api_id:
                    raw_path = mapping.get('basePath', '')
                    return candidate, ('' if raw_path in ('(none)', None) else raw_path)
    return '', ''

def _discover_http(api_id):
    client = boto3.client('apigatewayv2', config=_RETRY)
    next_token = None
    while True:
        kwargs = {'MaxResults': '500'}
        if next_token:
            kwargs['NextToken'] = next_token
        page = client.get_domain_names(**kwargs)
        for item in page.get('Items', []):
            candidate = item['DomainName']
            try:
                mappings = client.get_api_mappings(DomainName=candidate)
            except client.exceptions.NotFoundException:
                continue
            for mapping in mappings.get('Items', []):
                if mapping.get('ApiId') == api_id:
                    return candidate, mapping.get('ApiMappingKey', '') or ''
        next_token = page.get('NextToken')
        if not next_token:
            return '', ''

def handler(event, context):
    # Log the raw event first - carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place.
    physical_id = event.get('PhysicalResourceId')
    try:
        if event['RequestType'] in ['Create', 'Update']:
            props = event['ResourceProperties']
            api_id = props['ApiId']
            default_url = props['DefaultUrl']
            if props['ApiType'] == 'REST':
                domain_name, base_path = _discover_rest(api_id)
            else:
                domain_name, base_path = _discover_http(api_id)
            print(f'Discovered domain={domain_name!r} base_path={base_path!r}')
            # Resolve the fallback here rather than with a CfnCondition: CloudFormation
            # forbids referencing resource attributes from the Conditions block.
            if domain_name:
                base_url = 'https://' + domain_name
                # base_path is empty for a root mapping, so no trailing slash.
                url = base_url + '/' + base_path if base_path else base_url
            else:
                url = default_url.rstrip('/')
                base_url = default_url.rstrip('/')
            cfnresponse.send(event, context, cfnresponse.SUCCESS,
                             {'DomainName': domain_name, 'BasePath': base_path,
                              'Url': url, 'BaseUrl': base_url}, physical_id)
        else:
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
    except Exception as e:
        print(f'Error: {str(e)}')
        cfnresponse.send(event, context, cfnresponse.FAILED, {}, physical_id)
"""


def discover_api_custom_domain(
    scope: Construct,
    id: str,
    *,
    name: str,
    api_id: str,
    api_type: str,
    default_url: str,
) -> tuple:
    """Discover the custom domain mapped to an API Gateway API at deploy time.

    Returns ``(custom_domain, url, base_url)`` where ``custom_domain`` is "" when no
    custom domain is mapped and the URLs fall back to ``default_url`` in that case.
    ``url`` keeps the trailing slash; ``base_url`` omits it, for callers appending
    "/path" suffixes.

    Discovery is used instead of a CfnParameter because a parameter the installer
    forgets to re-supply reverts to its default, which would silently drop the
    custom domain from outputs on upgrade. ``api_type`` is "REST" or "HTTP".
    """
    if api_type not in ("REST", "HTTP"):
        raise ValueError(f"api_type must be 'REST' or 'HTTP', got {api_type!r}")

    shared_id = "ApiCustomDomainDiscoveryShared"
    stack = Stack.of(scope)
    fn = stack.node.try_find_child(shared_id)
    if fn is None:
        role = iam.Role(
            stack, f"{shared_id}Role",
            assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
            managed_policies=[
                iam.ManagedPolicy.from_aws_managed_policy_name(
                    "service-role/AWSLambdaBasicExecutionRole"),
            ],
        )
        role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "api-custom-domain-discovery"))
        role.add_to_policy(iam.PolicyStatement(
            actions=["apigateway:GET"],
            resources=["arn:aws:apigateway:*::/domainnames", "arn:aws:apigateway:*::/domainnames/*"],
        ))

        fn = lambda_.Function(
            stack, shared_id,
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            role=role,
            code=lambda_.Code.from_inline(_API_DOMAIN_DISCOVERY_CODE),
            timeout=Duration.seconds(120),
        )
        fn.node.default_child.override_logical_id(
            stable_logical_id("LambdaFunc", "api-custom-domain-discovery"))

    resource = CustomResource(
        scope, id,
        service_token=fn.function_arn,
        properties={"ApiId": api_id, "ApiType": api_type, "DefaultUrl": default_url,
                    "RediscoverOn": _DISCOVERY_SALT},
    )
    resource.node.default_child.override_logical_id(stable_logical_id("CustomResource", name))

    # The handler applies the fallback and returns the final URLs. CloudFormation
    # forbids referencing resource attributes from the Conditions block, so this cannot
    # be a CfnCondition on the discovered domain.
    return (resource.get_att_string("DomainName"),
            resource.get_att_string("Url"),
            resource.get_att_string("BaseUrl"))


_IOT_DOMAIN_DISCOVERY_CODE = """
import json
import boto3
import cfnresponse

def handler(event, context):
    # Log the raw event first - carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place.
    physical_id = event.get('PhysicalResourceId')
    try:
        if event['RequestType'] in ['Create', 'Update']:
            props = event['ResourceProperties']
            service_type = props['ServiceType']
            default_endpoint = props['DefaultEndpoint']
            client = boto3.client('iot')
            domain_name = ''
            marker = None
            while True:
                kwargs = {'serviceType': service_type}
                if marker:
                    kwargs['marker'] = marker
                page = client.list_domain_configurations(**kwargs)
                for item in page.get('domainConfigurations', []):
                    name = item['domainConfigurationName']
                    detail = client.describe_domain_configuration(domainConfigurationName=name)
                    if detail.get('domainType') != 'CUSTOMER_MANAGED':
                        continue
                    if detail.get('domainConfigurationStatus') != 'ENABLED':
                        continue
                    domain_name = detail.get('domainName', '')
                    if domain_name:
                        break
                marker = page.get('nextMarker')
                if domain_name or not marker:
                    break
            print(f'Discovered {service_type} domain={domain_name!r}')
            # Fallback resolved here; CloudFormation forbids referencing resource
            # attributes from the Conditions block.
            cfnresponse.send(event, context, cfnresponse.SUCCESS,
                             {'DomainName': domain_name,
                              'Endpoint': domain_name or default_endpoint}, physical_id)
        else:
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
    except Exception as e:
        print(f'Error: {str(e)}')
        cfnresponse.send(event, context, cfnresponse.FAILED, {}, physical_id)
"""


def discover_iot_custom_domain(
    scope: Construct,
    id: str,
    *,
    name: str,
    service_type: str,
    default_endpoint: str,
) -> tuple:
    """Discover the IoT custom domain for ``service_type``, at deploy time.

    Returns ``(custom_domain, endpoint)`` where ``custom_domain`` is "" when no
    enabled customer-managed domain configuration exists and ``endpoint`` then falls
    back to ``default_endpoint`` (the AWS-assigned ATS hostname).

    ``service_type`` must be "DATA" (the MQTT data endpoint); see the check below.

    NOTE: devices must trust the custom domain's server certificate. Firmware that
    pins the AWS-managed CA will fail to connect to a custom domain, so changing the
    device-facing hostname is a fleet-wide firmware concern, not just DNS.
    """
    # Only DATA is accepted: IoT rejects createDomainConfiguration for every other
    # serviceType ("CreateDomainConfiguration only supports DATA Service Type"), despite
    # the SDK model also listing CREDENTIAL_PROVIDER and JOBS. Discovery for those would
    # always find nothing.
    if service_type != "DATA":
        raise ValueError(
            f"IoT custom domains support only serviceType 'DATA', got {service_type!r}")

    shared_id = "IotCustomDomainDiscoveryShared"
    stack = Stack.of(scope)
    fn = stack.node.try_find_child(shared_id)
    if fn is None:
        role = iam.Role(
            stack, f"{shared_id}Role",
            assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
            managed_policies=[
                iam.ManagedPolicy.from_aws_managed_policy_name(
                    "service-role/AWSLambdaBasicExecutionRole"),
            ],
        )
        role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "iot-custom-domain-discovery"))
        role.add_to_policy(iam.PolicyStatement(
            actions=["iot:ListDomainConfigurations", "iot:DescribeDomainConfiguration"],
            resources=["*"],
        ))

        fn = lambda_.Function(
            stack, shared_id,
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            role=role,
            code=lambda_.Code.from_inline(_IOT_DOMAIN_DISCOVERY_CODE),
            timeout=Duration.seconds(120),
        )
        fn.node.default_child.override_logical_id(
            stable_logical_id("LambdaFunc", "iot-custom-domain-discovery"))

    resource = CustomResource(
        scope, id,
        service_token=fn.function_arn,
        properties={"ServiceType": service_type,
                    "DefaultEndpoint": default_endpoint,
                    "RediscoverOn": _DISCOVERY_SALT},
    )
    resource.node.default_child.override_logical_id(stable_logical_id("CustomResource", name))

    return (resource.get_att_string("DomainName"),
            resource.get_att_string("Endpoint"))


_COGNITO_DOMAIN_DISCOVERY_CODE = """
import json
import boto3
import cfnresponse

def handler(event, context):
    # Log the raw event first - carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place.
    physical_id = event.get('PhysicalResourceId')
    try:
        if event['RequestType'] in ['Create', 'Update']:
            props = event['ResourceProperties']
            pool = boto3.client('cognito-idp').describe_user_pool(
                UserPoolId=props['UserPoolId'])['UserPool']
            # Absent (not empty) while the pool uses the Cognito-hosted prefix domain.
            domain_name = pool.get('CustomDomain') or ''
            print('Discovered custom domain=' + repr(domain_name))
            # A custom domain is already fully qualified, so the Cognito-hosted suffix
            # must NOT be appended to it. Resolved here because CloudFormation forbids
            # referencing resource attributes from the Conditions block.
            host = domain_name or props['DefaultHost']
            cfnresponse.send(event, context, cfnresponse.SUCCESS,
                             {'DomainName': domain_name, 'OAuthHost': host}, physical_id)
        else:
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
    except Exception as e:
        print('Error: ' + str(e))
        cfnresponse.send(event, context, cfnresponse.FAILED, {}, physical_id)
"""


def discover_cognito_custom_domain(
    scope: Construct,
    id: str,
    *,
    name: str,
    user_pool_id: str,
    prefix_domain: str,
) -> tuple:
    """Return (custom_domain, oauth_host) for a user pool, resolved at deploy time.

    custom_domain is "" when the pool still uses the Cognito-hosted prefix domain;
    oauth_host then falls back to <prefix>.auth.<region>.amazoncognito.com. Uses
    describeUserPool rather than a CfnParameter so the value tracks what is actually
    configured in AWS and cannot be lost on upgrade.
    """
    shared_id = "CognitoCustomDomainDiscoveryShared"
    stack = Stack.of(scope)
    fn = stack.node.try_find_child(shared_id)
    if fn is None:
        role = iam.Role(
            stack, f"{shared_id}Role",
            assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
            managed_policies=[
                iam.ManagedPolicy.from_aws_managed_policy_name(
                    "service-role/AWSLambdaBasicExecutionRole"),
            ],
        )
        role.node.default_child.override_logical_id(
            stable_logical_id("IAMRole", "cognito-custom-domain-discovery"))
        role.add_to_policy(iam.PolicyStatement(
            actions=["cognito-idp:DescribeUserPool"],
            resources=["*"],
        ))
        fn = lambda_.Function(
            stack, shared_id,
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            role=role,
            code=lambda_.Code.from_inline(_COGNITO_DOMAIN_DISCOVERY_CODE),
            timeout=Duration.seconds(120),
        )
        fn.node.default_child.override_logical_id(
            stable_logical_id("LambdaFunc", "cognito-custom-domain-discovery"))

    default_host = Fn.join("", [
        prefix_domain, ".auth.", Stack.of(scope).region, ".amazoncognito.com"])
    resource = CustomResource(
        scope, id,
        service_token=fn.function_arn,
        properties={"UserPoolId": user_pool_id, "DefaultHost": default_host,
                    "RediscoverOn": _DISCOVERY_SALT},
    )
    resource.node.default_child.override_logical_id(
        stable_logical_id("CustomResource", name))
    return (resource.get_att_string("DomainName"),
            resource.get_att_string("OAuthHost"))


_CLOUDFRONT_DOMAIN_DISCOVERY_CODE = """
import json
import boto3
import cfnresponse

def handler(event, context):
    # Log the raw event first - carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place.
    physical_id = event.get('PhysicalResourceId')
    try:
        if event['RequestType'] in ['Create', 'Update']:
            props = event['ResourceProperties']
            config = boto3.client('cloudfront').get_distribution(
                Id=props['DistributionId'])['Distribution']['DistributionConfig']
            aliases = (config.get('Aliases') or {}).get('Items') or []
            domain_name = aliases[0] if aliases else ''
            print('Discovered alias=' + repr(domain_name))
            # Fallback resolved here; CloudFormation forbids referencing resource
            # attributes from the Conditions block.
            cfnresponse.send(event, context, cfnresponse.SUCCESS,
                             {'DomainName': domain_name,
                              'Host': domain_name or props['DefaultHost']}, physical_id)
        else:
            cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
    except Exception as e:
        print('Error: ' + str(e))
        cfnresponse.send(event, context, cfnresponse.FAILED, {}, physical_id)
"""


def discover_cloudfront_custom_domain(
    scope: Construct,
    id: str,
    *,
    name: str,
    distribution_id: str,
    default_host: str,
) -> tuple:
    """Return (custom_domain, host) for a distribution's first alternate domain name.

    custom_domain is "" when no alias is configured; host then falls back to
    default_host (the <id>.cloudfront.net name).
    """
    role = iam.Role(
        scope, f"{id}Role",
        assumed_by=iam.ServicePrincipal("lambda.amazonaws.com"),
        managed_policies=[
            iam.ManagedPolicy.from_aws_managed_policy_name(
                "service-role/AWSLambdaBasicExecutionRole"),
        ],
    )
    role.node.default_child.override_logical_id(stable_logical_id("IAMRole", name))
    role.add_to_policy(iam.PolicyStatement(
        actions=["cloudfront:GetDistribution"],
        resources=["*"],
    ))
    fn = lambda_.Function(
        scope, f"{id}Lambda",
        runtime=lambda_.Runtime.PYTHON_3_12,
        handler="index.handler",
        role=role,
        code=lambda_.Code.from_inline(_CLOUDFRONT_DOMAIN_DISCOVERY_CODE),
        timeout=Duration.seconds(120),
    )
    fn.node.default_child.override_logical_id(stable_logical_id("LambdaFunc", name))

    resource = CustomResource(
        scope, id,
        service_token=fn.function_arn,
        properties={"DistributionId": distribution_id, "DefaultHost": default_host,
                    "RediscoverOn": _DISCOVERY_SALT},
    )
    resource.node.default_child.override_logical_id(
        stable_logical_id("CustomResource", name))
    return (resource.get_att_string("DomainName"),
            resource.get_att_string("Host"))


def create_cloudfront_distribution(
    scope: Construct,
    id: str,
    *,
    distribution_name: str,
    default_behavior: cloudfront.BehaviorOptions,
    default_root_object: str = None,
    error_responses: list = None,
    **kwargs,
) -> cloudfront.Distribution:
    """CloudFront Distribution with a stable logical ID. Pinning the logical ID
    keeps `DistributionId` (and `<id>.cloudfront.net`) constant across refactors.
    """
    dist = cloudfront.Distribution(
        scope, id,
        default_behavior=default_behavior,
        default_root_object=default_root_object,
        error_responses=error_responses,
        **kwargs,
    )
    dist.node.default_child.override_logical_id(
        stable_logical_id("CFDistribution", distribution_name))
    return dist


def create_container(
    scope: Construct,
    id: str,
    common_resources: CommonResources,
    policy: iam.Policy,
    ecs_container_name: str,
    binary_name: str,
    cpu: int = 512,
    memory_mib: int = 1024,
) -> dict:
    """
    Creates a Fargate container with the specified parameters.

    Args:
        scope: The CDK scope
        id: The CDK id
        common_resources: Common resources object
        policy: IAM policy to attach to the task role
        binary_name: Name of the binary to run in the container
        cpu: CPU units for the task (default: 512)
        memory_mib: Memory in MiB for the task (default: 1024)

    Returns:
        Dictionary with all exported parameters
    """
    region = Aws.REGION
    # Create CloudWatch Log Group
    log_group = logs.LogGroup(
        scope,
        f"{id}LogGroup",
        log_group_name=f"/ecs/{ecs_container_name}",
        removal_policy=RemovalPolicy.DESTROY,
        retention=logs.RetentionDays.ONE_WEEK
    )
    log_group.node.default_child.override_logical_id(
        stable_logical_id("LogGrp", f"ecs-{ecs_container_name}"))

    # Create Task Role
    task_role_name = f"{ecs_container_name}-task-role"
    task_role = iam.Role(
        scope,
        f"{id}TaskRole",
        role_name=f"{common_resources.prefix}{task_role_name}-{Stack.of(scope).region}",
        assumed_by=iam.ServicePrincipal("ecs-tasks.amazonaws.com")
    )
    # Inline DefaultPolicy (attached below) stays path-derived.
    task_role.node.default_child.override_logical_id(
        stable_logical_id("IAMRole", task_role_name))

    # Attach the provided policy
    task_role.attach_inline_policy(policy)

    # Create Task Execution Role
    execution_role_name = f"{ecs_container_name}-exec-role"
    execution_role = iam.Role(
        scope,
        f"{id}ExecutionRole",
        role_name=f"{common_resources.prefix}{execution_role_name}-{Stack.of(scope).region}",
        assumed_by=iam.ServicePrincipal("ecs-tasks.amazonaws.com"),
        managed_policies=[
            iam.ManagedPolicy.from_aws_managed_policy_name("service-role/AmazonECSTaskExecutionRolePolicy")
        ]
    )
    execution_role.node.default_child.override_logical_id(
        stable_logical_id("IAMRole", execution_role_name))

    # Add permissions for logging
    execution_role.add_to_policy(iam.PolicyStatement(
        actions=[
            "logs:CreateLogStream",
            "logs:PutLogEvents"
        ],
        resources=[f"{log_group.log_group_arn}:*"]
    ))

    # Create VPC for Fargate task (cost-optimized: public subnets only).
    # VPC L1 is stabilised below; aux L1s (Subnet, RouteTable, IGW, etc.) have
    # no physical names → safe to move (CFN replaces them cleanly). Caveat:
    # moving DOES kill running Fargate tasks (no graceful drain) and produces
    # new subnet/SG IDs — anything storing those externally must update.
    vpc = ec2.Vpc(
        scope,
        f"{id}VPC",
        max_azs=1,
        nat_gateways=0,
        subnet_configuration=[
            ec2.SubnetConfiguration(
                name="PublicSubnet",
                subnet_type=ec2.SubnetType.PUBLIC,
                cidr_mask=28
            )
        ]
    )
    vpc.node.default_child.override_logical_id(
        stable_logical_id("VPC", f"{ecs_container_name}-vpc"))

    # Create security group for Fargate task
    security_group = ec2.SecurityGroup(
        scope,
        f"{id}SecurityGroup",
        vpc=vpc,
        description=f"Security group for {ecs_container_name} task",
        allow_all_outbound=True
    )
    security_group.node.default_child.override_logical_id(
        stable_logical_id("SecGrp", f"{ecs_container_name}-sg"))

    # Create ECS Cluster
    cluster = ecs.Cluster(
        scope,
        f"{id}Cluster",
        vpc=vpc,
        cluster_name=f"{ecs_container_name}-cluster"
    )
    cluster.node.default_child.override_logical_id(
        stable_logical_id("ECSCluster", f"{ecs_container_name}-cluster"))

    # Create Task Definition. Aux L1s still path-derived: the container
    # definition (added via add_container) and its log-driver wiring.
    task_definition = ecs.FargateTaskDefinition(
        scope,
        f"{id}TaskDefinition",
        family=ecs_container_name,
        cpu=cpu,
        memory_limit_mib=memory_mib,
        task_role=task_role,
        execution_role=execution_role,
        runtime_platform=ecs.RuntimePlatform(
            cpu_architecture=ecs.CpuArchitecture.ARM64,
            operating_system_family=ecs.OperatingSystemFamily.LINUX
        )
    )
    task_definition.node.default_child.override_logical_id(
        stable_logical_id("ECSTaskDef", ecs_container_name))

    # Upload the binary as an asset
    binary_asset = s3_assets.Asset(
        scope,
        f"{id}BinaryAsset",
        path=str(REPO_ROOT / "build" / binary_name)
    )

    # Add Container to Task Definition using Alpine
    container = task_definition.add_container(
        f"{id}Container",
        container_name=ecs_container_name,
        image=ecs.ContainerImage.from_registry("alpine:latest"),
        logging=ecs.LogDrivers.aws_logs(
            stream_prefix=f"{ecs_container_name}",
            log_group=log_group
        ),
        command=[
            "/bin/sh", "-c",
            "apk add --no-cache aws-cli unzip && " +
            f"aws s3 cp s3://{binary_asset.s3_bucket_name}/{binary_asset.s3_object_key} asset.zip && " +
            "unzip asset.zip && " +
            "chmod +x ./bootstrap && " +
            "./bootstrap"
        ],
        environment={
            "AWS_REGION": region
        }
    )

    # Grant read permissions to the task role for the binary asset
    binary_asset.grant_read(task_role)

    # Return all the exported parameters
    return {
        "task_role_arn": task_role.role_arn,
        "execution_role_arn": execution_role.role_arn,
        "cluster_arn": cluster.cluster_arn,
        "task_definition_arn": task_definition.task_definition_arn,
        "public_subnet_ids": ",".join([subnet.subnet_id for subnet in vpc.public_subnets]),
        "security_group_id": security_group.security_group_id,
        "container_name": container.container_name
    }

# CFn API Gateway Helper Functions to avoid cyclic dependencies

def _create_cfn_api_resource(
    scope: Construct,
    id: str,
    common_resources: CommonResources,
    parent_id: str,
    path_part: str,
    api_id: str = None,
):
    """Create API Gateway resource using CFn to avoid cyclic dependencies.

    Logical ID is derived from the resource's full URL path
    ("v1/groups/{groupId}/subgroups"), making it path-independent in the
    construct tree. The mapping is maintained in
    `common_resources._api_resource_path_by_ref` so that `create_cfn_api_method`
    can produce method IDs from `<url-path>-<verb>`. Resources whose
    `parent_id` is from outside the helper (e.g. an imported root) fall back to
    an empty parent URL, so the URL starts at the bare `path_part`.
    """
    # Use provided api_id or default to main API gateway
    rest_api_id = api_id if api_id else common_resources.api_gateway_id
    parent_url = common_resources._api_resource_path_by_ref.get(parent_id, "")
    full_url = f"{parent_url}/{path_part}".lstrip("/")
    resource = apigateway.CfnResource(
        scope, id,
        rest_api_id=rest_api_id,
        parent_id=parent_id,
        path_part=path_part
    )
    common_resources._api_resource_path_by_ref[resource.ref] = full_url
    resource.override_logical_id(stable_logical_id("ApiGwResource", full_url))
    return resource

def get_or_create_api_resource(
    scope: Construct,
    id: str,
    common_resources: CommonResources,
    parent_id: str,
    path_part: str,
    api_id: str = None,
):
    """
    Get existing API resource ID from common_resources cache or create a new one using CFn.
    Resources are cached by (parent_id, path_part, api_id) to avoid duplicates.
    Returns the Resource ID (Ref).
    """
    # Initialize cache if it doesn't exist (for backward compatibility)
    if not hasattr(common_resources, '_api_resource_cache'):
        common_resources._api_resource_cache = {}

    cache_key = (parent_id, path_part, api_id)
    if cache_key in common_resources._api_resource_cache:
        return common_resources._api_resource_cache[cache_key]

    resource = _create_cfn_api_resource(scope, id, common_resources, parent_id, path_part, api_id)
    resource_ref = resource.ref
    common_resources._api_resource_cache[cache_key] = resource_ref
    return resource_ref

def create_cfn_api_method(scope: Construct, id: str, common_resources: CommonResources, resource_id: str,
                         http_method: str, lambda_function, authorization_type: str = "AWS_IAM",
                         authorizer_id: str = None, api_id: str = None, api_key_required: bool = False,
                         authorization_scopes: list[str] = None):
    """Create API Gateway method using CFn to avoid cyclic dependencies.

    Logical ID is derived as `<full-url-path>-<verb>` using the URL-path
    registry populated by `_create_cfn_api_resource`. If `resource_id` is from
    outside the helper (imported, root), the URL path falls back to empty and
    the logical ID becomes just `ApiGwMethod-<verb>` — call sites with imported
    resources should expect a less-stable ID for those few methods.

    authorization_scopes selects which Cognito token a COGNITO_USER_POOLS
    authorizer accepts, and there is no middle setting:

      - omitted  -> API Gateway validates the **ID token**. An access token is
                    rejected with 401 before the Lambda is ever invoked.
      - provided -> API Gateway validates the **access token** and requires its
                    `scope` claim to contain one of these values. An ID token,
                    which carries no `scope` claim, is then rejected with 401.

    So an endpoint that wants to authenticate callers by access token must set
    this; changing only the handler is not enough.
    """
    # Use stack's deployment region for integration URI and Lambda permission.
    # Mismatch (e.g. AWS_REGION=us-east-1 vs deployment in ap-south-1) causes Lambda to reject API Gateway invocations.
    region = Stack.of(scope).region
    # Use provided api_id or default to main API gateway
    rest_api_id = api_id if api_id else common_resources.api_gateway_id
    integration_uri = get_lambda_integration_uri(lambda_function.function_arn, region)
    
    method_props = {
        "rest_api_id": rest_api_id,
        "resource_id": resource_id,
        "http_method": http_method,
        "authorization_type": authorization_type,
        "api_key_required": api_key_required,
        "integration": apigateway.CfnMethod.IntegrationProperty(
            type="AWS_PROXY",
            integration_http_method="POST",
            uri=integration_uri
        )
    }
    
    if authorizer_id:
        method_props["authorizer_id"] = authorizer_id

    if authorization_scopes:
        method_props["authorization_scopes"] = authorization_scopes

    method = apigateway.CfnMethod(scope, id, **method_props)
    url_path = common_resources._api_resource_path_by_ref.get(resource_id, "")
    method.override_logical_id(stable_logical_id("ApiGwMethod", f"{url_path}-{http_method}"))

    # Grant API Gateway permission to invoke Lambda (source_arn must match API Gateway's deployment region).
    # No stable logical ID, Lambda::Permission has no physical name → safe to move across files (CFN delete-create, no "already exists" conflict).
    lambda_function.add_permission(
        f"{id}ApiGatewayInvoke",
        principal=iam.ServicePrincipal("apigateway.amazonaws.com"),
        action="lambda:InvokeFunction",
        source_arn=get_api_gateway_invoke_arn(rest_api_id, region, http_method, "*")
    )

    return method

def add_cors_options(scope: Construct, id: str, common_resources: CommonResources,
                     resource_id: str, allowed_methods: list[str], api_id: str = None):
    """
    Add CORS OPTIONS method to an API Gateway resource.

    Creates an unauthenticated OPTIONS method with MOCK integration to handle CORS preflight requests.
    The method is handled entirely by API Gateway without invoking Lambda.

    Args:
        scope: The CDK scope
        id: Unique identifier for the OPTIONS method
        common_resources: CommonResources object containing API Gateway ID
        resource_id: The API Gateway resource ID to add the OPTIONS method to
        allowed_methods: List of HTTP methods allowed (e.g., ["GET", "POST"])
        api_id: Optional API Gateway ID (defaults to common_resources.api_gateway_id)
    
    Returns:
        The created CfnMethod
    """
    # Use provided api_id or default to main API gateway
    rest_api_id = api_id if api_id else common_resources.api_gateway_id
    
    # Standard CORS headers matching default configuration
    cors_headers = "Content-Type,X-Amz-Date,Authorization,X-Api-Key,X-Amz-Security-Token,X-Amz-User-Agent,X-Amz-Content-Sha256,X-Requested-With"
    # Convert allowed_methods to uppercase comma-separated string, always include OPTIONS
    cors_methods_list = [method.upper() for method in allowed_methods]
    if "OPTIONS" not in cors_methods_list:
        cors_methods_list.append("OPTIONS")
    cors_methods = ",".join(cors_methods_list)
    allowed_origins = '*'
    max_age_seconds = int(Duration.days(1).to_seconds())

    # Create OPTIONS method with MOCK integration for CORS preflight
    options_method = apigateway.CfnMethod(
        scope, id,
        rest_api_id=rest_api_id,
        resource_id=resource_id,
        http_method="OPTIONS",
        authorization_type="NONE",  # CORS preflight must be unauthenticated
        integration=apigateway.CfnMethod.IntegrationProperty(
            type="MOCK",
            passthrough_behavior="WHEN_NO_MATCH",  # Allow preflight requests without Content-Type
            request_templates={
                "application/json": "{\"statusCode\": 200}"
            },
            integration_responses=[
                apigateway.CfnMethod.IntegrationResponseProperty(
                    status_code="200",
                    response_parameters={
                        "method.response.header.Access-Control-Allow-Headers": f"'{cors_headers}'",
                        "method.response.header.Access-Control-Allow-Methods": f"'{cors_methods}'",
                        "method.response.header.Access-Control-Allow-Origin": f"'{allowed_origins}'",
                        "method.response.header.Access-Control-Max-Age": f"'{max_age_seconds}'"
                    }
                )
            ]
        ),
        method_responses=[
            apigateway.CfnMethod.MethodResponseProperty(
                status_code="200",
                response_parameters={
                    "method.response.header.Access-Control-Allow-Headers": True,
                    "method.response.header.Access-Control-Allow-Methods": True,
                    "method.response.header.Access-Control-Allow-Origin": True,
                    "method.response.header.Access-Control-Max-Age": True
                }
            )
        ]
    )
    url_path = common_resources._api_resource_path_by_ref.get(resource_id, "")
    options_method.override_logical_id(stable_logical_id("ApiGwMethod", f"{url_path}-OPTIONS"))

    return options_method

