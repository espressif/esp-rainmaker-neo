# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from aws_cdk import Aws, CfnOutput, Fn
from aws_cdk import (
    Stack,
    CfnParameter,
    aws_iam as iam,
)
from src.rmneo.stacks.base_res_constants import TABLE_NAMES

from app_common import CommonResources, create_lambda_function, create_base_lambda_role

from arn_utils import (
    get_table_arn,
    get_index_arn,
    get_identity_pool_arn,
    get_topic_arn,
    get_iot_thing_arn,
    get_ssm_parameter_prefix_arn,
    get_user_pool_arn,
)


class STStack(Stack):
    """Standalone stack containing the SmartThings Schema App Lambda.

    This stack deploys the st_action Lambda function for the SmartThings Schema.
    The Configuration API is deployed in the rmng stack.
    SmartThings requires the Lambda to be present in specific regions
    (us-east-1 for NA, eu-west-1 for EU, ap-northeast-1 for AP).
    Infrastructure (DynamoDB, Cognito, SSM, IoT) lives in the rmng stack,
    which may be in a different region. Cross-region ARNs are constructed
    using the RmngRegion CloudFormation parameter.
    """

    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, st_params: dict = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        st_params = st_params or {}

        # ---------------------------------------------------------------
        # CloudFormation Parameters (defaults resolved from rmng-outputs.json via st_params)
        # ---------------------------------------------------------------
        rmng_region_param = CfnParameter(
            self, "RmngRegion",
            type="String",
            description="AWS region where the rmng stack is deployed",
            default=st_params.get("rmng_region", "")
        )
        esp_user_issuer_param = CfnParameter(
            self, "EspUserIssuer",
            type="String",
            description="ESP User Issuer (from rmng-base outputs)",
            default=st_params.get("esp_user_issuer", "")
        )
        esp_user_client_id_param = CfnParameter(
            self, "EspUserClientId",
            type="String",
            description="ESP User Client ID",
            default=st_params.get("esp_user_client_id", "")
        )
        esp_admin_user_pool_id_param = CfnParameter(
            self, "EspAdminUserPoolId",
            type="String",
            description="ESP Admin User Pool ID",
            default=st_params.get("esp_admin_user_pool_id", "")
        )
        esp_admin_user_pool_client_id_param = CfnParameter(
            self, "EspAdminUserPoolClientId",
            type="String",
            description="ESP Admin User Pool Client ID",
            default=st_params.get("esp_admin_user_pool_client_id", "")
        )
        esp_user_jwks_param = CfnParameter(
            self, "EspUserJwks",
            type="String",
            description="ESP User JWKS SSM parameter name",
            default=st_params.get("esp_user_jwks", "")
        )
        esp_admin_user_pool_jwks_param = CfnParameter(
            self, "EspAdminUserPoolJwks",
            type="String",
            description="ESP Admin User Pool JWKS SSM parameter name",
            default=st_params.get("esp_admin_user_pool_jwks", "")
        )
        identity_pool_id_param = CfnParameter(
            self, "IdentityPoolId",
            type="String",
            description="Cognito Identity Pool ID (from rmng-base outputs)",
            default=st_params.get("identity_pool_id", "")
        )

        rmng_region = rmng_region_param.value_as_string
        # Baked into the template when known, because CloudFormation retains a previously-set
        # parameter value across updates and can leave USER_ISSUER empty. The CfnParameter remains
        # the fallback for the publish flow.
        esp_user_issuer = st_params.get("esp_user_issuer") or esp_user_issuer_param.value_as_string
        esp_user_client_id = esp_user_client_id_param.value_as_string
        esp_admin_user_pool_id = esp_admin_user_pool_id_param.value_as_string
        esp_admin_user_pool_client_id = esp_admin_user_pool_client_id_param.value_as_string
        esp_user_jwks = esp_user_jwks_param.value_as_string
        esp_admin_user_pool_jwks = esp_admin_user_pool_jwks_param.value_as_string
        identity_pool_id = identity_pool_id_param.value_as_string

        # Create Lambda role with necessary permissions (stable id; binary lives under build/st_action/)
        binary_name = "st_action"
        st_action_lambda_role = create_base_lambda_role(self, "action", common_resources)

        # IAM roles are global per account. Each backend (rmng_region) deploys its own
        # set of smartthings stacks across the 3 SmartThings regions, so the role name
        # must include rmng_region too — otherwise a new backend's stack collides with
        # the existing role owned by a prior backend's stack in the same region.
        st_action_lambda_role.node.default_child.add_property_override(
            "RoleName", Fn.join("", [f"{common_resources.prefix}action-role-", rmng_region, "-", Aws.REGION]))

        # AWS Lambda name includes rmng backend region so names are unique across
        # accounts/deployments. Region is a CFN token resolved at deploy time, so
        # we hand-stitch the prefix (instead of relying on create_lambda_function's
        # auto-prefix path which would only apply to a non-token string).
        aws_lambda_name = Fn.join("", [f"{common_resources.prefix}action-", rmng_region])

        # DynamoDB read permissions (cross-region to rmng stack)
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:GetItem",
                "dynamodb:Query",
                "dynamodb:BatchGetItem",
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], rmng_region),
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], rmng_region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], rmng_region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', rmng_region),
                get_table_arn(TABLE_NAMES['NODES_ONLINE'], rmng_region),
                get_table_arn(TABLE_NAMES['GROUPS'], rmng_region),
            ]
        ))

        # DynamoDB UpdateItem on Node_Details_DB (st_en flag)
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:UpdateItem",
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], rmng_region),
            ]
        ))

        # Callback token storage in rmng-user-endpoints: PutItem on account link
        # (grantCallbackAccess), Query + DeleteItem on unlink (integrationDeleted)
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem",
                "dynamodb:Query",
                "dynamodb:DeleteItem",
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], rmng_region),
            ]
        ))

        # IoT permissions for device control and shadow access (cross-region)
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:Publish",
                "iot:GetThingShadow",
                "iot:UpdateThingShadow",
            ],
            resources=[
                get_topic_arn('rainmaker/nodes/*', rmng_region),
                get_iot_thing_arn('*', rmng_region),
            ]
        ))

        # Add permissions for SSM Parameter Store (SmartThings config + JWKS for token validation)
        ssm_resources = [
            get_ssm_parameter_prefix_arn('ST_CONFIG', rmng_region),
        ]
        if esp_user_jwks:
            ssm_resources.append(f"arn:aws:ssm:{rmng_region}:{Aws.ACCOUNT_ID}:parameter{esp_user_jwks}")
        if esp_admin_user_pool_jwks:
            ssm_resources.append(f"arn:aws:ssm:{rmng_region}:{Aws.ACCOUNT_ID}:parameter{esp_admin_user_pool_jwks}")
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter",
            ],
            resources=ssm_resources
        ))

        # Cognito Identity permissions (cross-region)
        st_action_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-identity:GetId",
            ],
            resources=[
                get_identity_pool_arn(identity_pool_id, rmng_region),
            ]
        ))

        # Add Cognito User Pool permissions (for token validation via GetUser)
        cognito_resources = []
        if esp_admin_user_pool_id:
            cognito_resources.append(get_user_pool_arn(esp_admin_user_pool_id, rmng_region))
        if cognito_resources:
            st_action_lambda_role.add_to_policy(iam.PolicyStatement(
                actions=[
                    "cognito-idp:GetUser",
                    "cognito-idp:AdminGetUser"
                ],
                resources=cognito_resources
            ))

        environment = {
            "AWS_ACCOUNT_ID": Aws.ACCOUNT_ID,
            "RMNG_REGION": rmng_region,
            "USER_ISSUER": esp_user_issuer,
            "USER_CLIENT_ID": esp_user_client_id,
            "USER_JWKS_PARA_NAME": esp_user_jwks,
            "ADMIN_USER_POOL_ID": esp_admin_user_pool_id,
            "ADMIN_USER_POOL_CLIENT_ID": esp_admin_user_pool_client_id,
            "ADMIN_USER_POOL_JWKS_PARA_NAME": esp_admin_user_pool_jwks,
            "IDENTITY_POOL_ID": identity_pool_id,
        }

        self.st_action_function = create_lambda_function(
            self,
            binary_name,
            common_resources,
            lambda_role=st_action_lambda_role,
            environment=environment,
            region=rmng_region,
            aws_function_name=aws_lambda_name,
        )

        # Grant the SmartThings platform account (148790070172) permission to invoke the Lambda
        self.st_action_function.add_permission(
            "SmartThingsInvoke",
            principal=iam.AccountPrincipal("148790070172"),
            action="lambda:InvokeFunction",
        )

        CfnOutput(self, "STSchemaAppFunctionArn", value=self.st_action_function.function_arn)
