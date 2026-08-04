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


class AlexaStack(Stack):
    """Standalone stack containing Alexa Skill Lambda Stack.

    This stack deploys its own Lambda functions for Alexa Skill.
    The Configuration API is deployed in the rmng stack.
    Alexa Skill lambda is deployed separately from rmng-base as AWS mandates that this lambda must be present in 3 regions.
    Infrastructure (DynamoDB, Cognito, SSM, IoT) lives in the rmng stack,
    which may be in a different region.  Cross-region ARNs are constructed
    using the RmngRegion CloudFormation parameter.
    """


    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, *, alexa_params: dict = None, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        alexa_params = alexa_params or {}

        # ---------------------------------------------------------------
        # CloudFormation Parameters (defaults resolved from rmng-outputs.json via alexa_params)
        # ---------------------------------------------------------------
        rmng_region_param = CfnParameter(
            self, "RmngRegion",
            type="String",
            description="AWS region where the rmng stack is deployed",
            default=alexa_params.get("rmng_region", "")
        )
        esp_user_issuer_param = CfnParameter(
            self, "EspUserIssuer",
            type="String",
            description="ESP User Issuer (from rmng-base outputs)",
            default=alexa_params.get("esp_user_issuer", "")
        )
        esp_user_client_id_param = CfnParameter(
            self, "EspUserClientId",
            type="String",
            description="ESP User Client ID",
            default=alexa_params.get("esp_user_client_id", "")
        )
        esp_admin_user_pool_id_param = CfnParameter(
            self, "EspAdminUserPoolId",
            type="String",
            description="ESP Admin User Pool ID",
            default=alexa_params.get("esp_admin_user_pool_id", "")
        )
        esp_admin_user_pool_client_id_param = CfnParameter(
            self, "EspAdminUserPoolClientId",
            type="String",
            description="ESP Admin User Pool Client ID",
            default=alexa_params.get("esp_admin_user_pool_client_id", "")
        )
        esp_user_jwks_param = CfnParameter(
            self, "EspUserJwks",
            type="String",
            description="ESP User JWKS SSM parameter name",
            default=alexa_params.get("esp_user_jwks", "")
        )
        esp_admin_user_pool_jwks_param = CfnParameter(
            self, "EspAdminUserPoolJwks",
            type="String",
            description="ESP Admin User Pool JWKS SSM parameter name",
            default=alexa_params.get("esp_admin_user_pool_jwks", "")
        )
        identity_pool_id_param = CfnParameter(
            self, "IdentityPoolId",
            type="String",
            description="Cognito Identity Pool ID (from rmng-base outputs)",
            default=alexa_params.get("identity_pool_id", "")
        )

        rmng_region = rmng_region_param.value_as_string
        # Baked into the template when known, because CloudFormation retains a previously-set
        # parameter value across updates and can leave USER_ISSUER empty. The CfnParameter remains
        # the fallback for the publish flow.
        esp_user_issuer = alexa_params.get("esp_user_issuer") or esp_user_issuer_param.value_as_string
        esp_user_client_id = esp_user_client_id_param.value_as_string
        esp_admin_user_pool_id = esp_admin_user_pool_id_param.value_as_string
        esp_admin_user_pool_client_id = esp_admin_user_pool_client_id_param.value_as_string
        esp_user_jwks = esp_user_jwks_param.value_as_string
        esp_admin_user_pool_jwks = esp_admin_user_pool_jwks_param.value_as_string
        identity_pool_id = identity_pool_id_param.value_as_string

        # The discovery manufacturer name is deliberately not a stack parameter: it is set at
        # runtime through the Alexa configuration API, which stores it in SSM under
        # /rmng/alexa/ (read below), so rebranding a deployment needs no redeploy.

        # Create Lambda role with necessary permissions (stable id; binary lives under build/alexa_skill/)
        binary_name = "alexa_skill"
        alexa_skill_lambda_role = create_base_lambda_role(self, "skill", common_resources)

        # IAM roles are global per account. Each backend (rmng_region) deploys its own
        # set of alexa stacks across the 3 alexa regions, so the role name must include
        # rmng_region too — otherwise a new backend's stack collides with the existing
        # role owned by a prior backend's stack in the same alexa region.
        alexa_skill_lambda_role.node.default_child.add_property_override(
            "RoleName", Fn.join("", [f"{common_resources.prefix}skill-role-", rmng_region, "-", Aws.REGION]))

        # AWS Lambda name includes rmng backend region so names are unique across
        # accounts/deployments. Region is a CFN token resolved at deploy time, so
        # we hand-stitch the prefix (instead of relying on create_lambda_function's
        # auto-prefix path which would only apply to a non-token string).
        aws_lambda_name = Fn.join("", [f"{common_resources.prefix}skill-", rmng_region])

        # Add permissions for DynamoDB access
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:Query",
                "dynamodb:GetItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_GROUP_MAPPING'], rmng_region),
                get_table_arn(TABLE_NAMES['GROUP_DEVICE_MAPPING'], rmng_region),
                get_index_arn('GROUP_DEVICE_MAPPING_NODE_ID', rmng_region),
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], rmng_region),
                get_table_arn(TABLE_NAMES['GROUPS'], rmng_region),
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], rmng_region),
                get_table_arn(TABLE_NAMES['USER_DETAILS'], rmng_region),
                get_table_arn(TABLE_NAMES['NODES_ONLINE'], rmng_region),
            ]
        ))

        # Add specific UpdateItem permission for node_details table
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:UpdateItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['NODE_DETAILS'], rmng_region),
            ]
        ))

        # Add PutItem permission for user endpoints table (AcceptGrant writes the Alexa OAuth bundle)
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "dynamodb:PutItem"
            ],
            resources=[
                get_table_arn(TABLE_NAMES['USER_ENDPOINTS'], rmng_region),
            ]
        ))

        # Add IoT permissions for device control
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "iot:Publish",
                "iot:UpdateThingShadow",
                "iot:GetThingShadow"
            ],
            resources=[
                get_topic_arn('rainmaker/nodes/*', rmng_region),
                get_iot_thing_arn('*', rmng_region)
            ]
        ))

        # Add permissions for SSM Parameter Store (Alexa config + JWKS for token validation)
        ssm_resources = [
            get_ssm_parameter_prefix_arn('ALEXA_CONFIG', rmng_region),
        ]
        if esp_user_jwks:
            ssm_resources.append(f"arn:aws:ssm:{rmng_region}:{Aws.ACCOUNT_ID}:parameter{esp_user_jwks}")
        if esp_admin_user_pool_jwks:
            ssm_resources.append(f"arn:aws:ssm:{rmng_region}:{Aws.ACCOUNT_ID}:parameter{esp_admin_user_pool_jwks}")
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "ssm:GetParameter"
            ],
            resources=ssm_resources
        ))

        # Add Cognito Identity permissions
        alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-identity:GetId"
            ],
            resources=[
                get_identity_pool_arn(identity_pool_id, rmng_region)
            ]
        ))

        # Add Cognito User Pool permissions (for token validation via GetUser)
        cognito_resources = []
        if esp_admin_user_pool_id:
            cognito_resources.append(get_user_pool_arn(esp_admin_user_pool_id, rmng_region))
        if cognito_resources:
            alexa_skill_lambda_role.add_to_policy(iam.PolicyStatement(
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

        self.alexa_skill_function = create_lambda_function(
            self,
            binary_name,
            common_resources,
            lambda_role=alexa_skill_lambda_role,
            environment=environment,
            region=rmng_region,
            aws_function_name=aws_lambda_name,
        )

        # Add Alexa Smart Home as a trigger
        self.alexa_skill_function.add_permission(
            "AlexaSkillInvoke",
            principal=iam.ServicePrincipal("alexa-connectedhome.amazon.com"),
            event_source_token="YOUR_SKILL_ID",
            action="lambda:InvokeFunction"
        )

        common_resources.alexa_skill_function = self.alexa_skill_function.function_arn

        CfnOutput(self, "AlexaSkillFunctionArn", value=self.alexa_skill_function.function_arn)