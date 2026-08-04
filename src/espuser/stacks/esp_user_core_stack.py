# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    CfnOutput,
    CfnParameter,
    CustomResource,
    Duration,
    Stack,
    aws_apigateway as apigateway,
    aws_iam as iam,
    aws_lambda as lambda_,
    custom_resources as cr,
)
from app_common import get_or_create_api_resource
from constructs import Construct
from app_common import CommonResources
from datetime import datetime
from ..handlers.user_common.stack import UserCommonAPI
from ..handlers.token.stack import TokenAPI
from ..handlers.userinfo.stack import UserinfoAPI
from ..handlers.revoke.stack import RevokeAPI
from ..handlers.clients.stack import ClientsAPI
from ..handlers.authorize.stack import AuthorizeAPI
from ..handlers.user_auth.stack import UserAuthAPI
from ..handlers.espuser_admin_creds.stack import AdminCredsAPI
from .base_res_constants import USER_SSM_PARAMETERS
from aws_cdk import aws_ssm as ssm

# Comma-separated list of one or more email-shaped tokens (each at least a@b.c), whitespace around commas allowed. Blank is NOT accepted.
ADMIN_EMAILS_ALLOWED_PATTERN = r"^\s*[^@\s,]+@[^@\s,]+\.[^@\s,]+(\s*,\s*[^@\s,]+@[^@\s,]+\.[^@\s,]+)*\s*$"

class EspUserCoreStack(Stack):
    """Stack containing user APIs that depends on EspUserBaseStack"""
    def __init__(self, scope: Construct, construct_id: str, admin_emails: list[str] = None, **kwargs) -> None:
        super().__init__(scope, construct_id, **kwargs)

        common_resources = CommonResources(
            api_gateway_id="",
            api_gateway_root_resource_id="",
            admin_api_resource_id="",
            cognito_authorizer_id="",
            prefix="espuser-",
        )
        common_resources.esp_user_api_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_API_ID']
        )
        common_resources.esp_user_api_root_resource_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_API_ROOT_RESOURCE_ID']
        )
        common_resources.esp_admin_cognito_authorizer_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_ADMIN_COGNITO_AUTHORIZER_ID']
        )
        common_resources.esp_user_issuer = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_ISSUER']
        )
        common_resources.esp_user_client_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_CLIENT_ID']
        )
        common_resources.esp_admin_user_pool_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_ID']
        )
        common_resources.esp_admin_user_pool_client_id = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_CLIENT_ID']
        )
        common_resources.esp_user_jwks = USER_SSM_PARAMETERS['ESP_USER_JWKS']
        common_resources.esp_admin_user_pool_jwks = USER_SSM_PARAMETERS['ESP_ADMIN_USER_POOL_JWKS']
        common_resources.esp_user_kms_signing_key_arn = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_KMS_SIGNING_KEY_ARN']
        )
        common_resources.esp_user_api_url = ssm.StringParameter.value_for_string_parameter(
            self, USER_SSM_PARAMETERS['ESP_USER_API_URL']
        )

        v1_resource_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.esp_user_api_root_resource_id, "v1",
            api_id=common_resources.esp_user_api_id,
        )
        ssm.StringParameter(
            self, "EspUserV1ResourceIdParam",
            parameter_name=USER_SSM_PARAMETERS['ESP_USER_V1_RESOURCE_ID'],
            string_value=v1_resource_id,
        )

        user_common_api = UserCommonAPI(self, "UserCommonAPI", common_resources)

        token_api = TokenAPI(self, "TokenAPI", common_resources)
        userinfo_api = UserinfoAPI(self, "UserinfoAPI", common_resources)
        revoke_api = RevokeAPI(self, "RevokeAPI", common_resources)
        clients_api = ClientsAPI(self, "ClientsAPI", common_resources)
        authorize_api = AuthorizeAPI(self, "AuthorizeAPI", common_resources)

        user_auth_api = UserAuthAPI(self, "UserAuthAPI", common_resources)

        self._register_admin_users(common_resources, [e.strip() for e in (admin_emails or []) if e and e.strip()])

        admin_creds_api = AdminCredsAPI(self, "AdminCredsAPI", common_resources)

        deployment_timestamp = datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
        esp_user_api_deploy = cr.AwsCustomResource(
            self, "EspUserApiGatewayDeploy",
            on_create=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.esp_user_api_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"esp-user-api-deploy-{deployment_timestamp}"),
            ),
            on_update=cr.AwsSdkCall(
                service="APIGateway",
                action="createDeployment",
                parameters={
                    "restApiId": common_resources.esp_user_api_id,
                    "stageName": "prod",
                    "description": f"Auto-deploy via CDK: {deployment_timestamp}",
                },
                physical_resource_id=cr.PhysicalResourceId.of(f"esp-user-api-deploy-{deployment_timestamp}"),
            ),
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(
                    actions=["apigateway:POST"],
                    resources=["arn:aws:apigateway:*::/restapis/*/deployments"],
                ),
                iam.PolicyStatement(
                    actions=["apigateway:PATCH"],
                    resources=["arn:aws:apigateway:*::/restapis/*/stages/prod"],
                ),
            ]
            ),
        )

        esp_user_api_deploy.node.add_dependency(user_common_api)
        esp_user_api_deploy.node.add_dependency(token_api)
        esp_user_api_deploy.node.add_dependency(userinfo_api)
        esp_user_api_deploy.node.add_dependency(revoke_api)
        esp_user_api_deploy.node.add_dependency(clients_api)
        esp_user_api_deploy.node.add_dependency(authorize_api)
        esp_user_api_deploy.node.add_dependency(user_auth_api)
        esp_user_api_deploy.node.add_dependency(admin_creds_api)

    def _register_admin_users(self, common_resources, admin_emails: list[str]) -> None:
        region = Stack.of(self).region
        account = Stack.of(self).account
        pool_id = common_resources.esp_admin_user_pool_id
        pool_arn = f"arn:aws:cognito-idp:{region}:{account}:userpool/{pool_id}"
        param_name = "/espuser/admin-temp-password"
        param_arn = f"arn:aws:ssm:{region}:{account}:parameter{param_name}"

        generated = cr.AwsCustomResource(
            self, "AdminTempPasswordGenerate",
            on_create=cr.AwsSdkCall(
                service="SecretsManager",
                action="getRandomPassword",
                parameters={
                    "PasswordLength": 16,
                    "RequireEachIncludedType": True,
                    "ExcludeCharacters": "\"'`\\/@ ",
                },
                physical_resource_id=cr.PhysicalResourceId.of("admin-temp-password-generate"),
            ),
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(actions=["secretsmanager:GetRandomPassword"], resources=["*"]),
            ]),
        )

        store_password = cr.AwsCustomResource(
            self, "AdminTempPasswordStore",
            on_create=cr.AwsSdkCall(
                service="SSM",
                action="putParameter",
                parameters={
                    "Name": param_name,
                    "Value": generated.get_response_field("RandomPassword"),
                    "Type": "SecureString",
                },
                physical_resource_id=cr.PhysicalResourceId.of("admin-temp-password-store"),
                ignore_error_codes_matching="ParameterAlreadyExists",
            ),
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(actions=["ssm:PutParameter"], resources=[param_arn]),
            ]),
        )
        store_password.node.add_dependency(generated)

        read_password = cr.AwsCustomResource(
            self, "AdminTempPasswordRead",
            on_create=cr.AwsSdkCall(
                service="SSM",
                action="getParameter",
                parameters={"Name": param_name, "WithDecryption": True},
                physical_resource_id=cr.PhysicalResourceId.of("admin-temp-password-read"),
            ),
            on_update=cr.AwsSdkCall(
                service="SSM",
                action="getParameter",
                parameters={"Name": param_name, "WithDecryption": True},
                physical_resource_id=cr.PhysicalResourceId.of("admin-temp-password-read"),
            ),
            policy=cr.AwsCustomResourcePolicy.from_statements([
                iam.PolicyStatement(actions=["ssm:GetParameter"], resources=[param_arn]),
            ]),
        )
        read_password.node.add_dependency(store_password)
        temp_password = read_password.get_response_field("Parameter.Value")

        # The synth-time list from rmng-inputs.json becomes the parameter DEFAULT (none when the
        # inputs are empty, so the installer flow — which always passes the parameter — must name
        # at least one email). Note the default only applies to stack CREATION: on updates CDK
        # keeps the previous parameter value, so re-seeding an existing stack after editing
        # rmng-inputs.json requires passing AdminEmails explicitly once.
        admin_emails_param = CfnParameter(
            self, "AdminEmails",
            type="String",
            description=(
                "Comma-separated list of admin email addresses to register as super-admins in "
                "this account. At least one valid email address is required."
            ),
            allowed_pattern=ADMIN_EMAILS_ALLOWED_PATTERN,
            constraint_description="AdminEmails must be a comma-separated list of one or more valid email addresses.",
            **({"default": ",".join(admin_emails)} if admin_emails else {}),
        )
        admin_emails_prop = admin_emails_param.value_as_string

        register_fn = lambda_.Function(
            self, "AdminUserRegisterFn",
            runtime=lambda_.Runtime.PYTHON_3_12,
            handler="index.handler",
            timeout=Duration.minutes(5),
            code=lambda_.Code.from_inline("""
import json
import boto3
import cfnresponse

def handler(event, context):
    # Log the raw event first - carries ResponseURL, which unstick_custom_resource.py needs if this hangs.
    print("CR_EVENT " + json.dumps(event))
    # Echo the existing PhysicalResourceId so Update/Delete stay in place (None on Create -> cfnresponse
    # assigns one). Returning a NEW id on Update would make CloudFormation replace + delete the old one.
    physical_id = event.get('PhysicalResourceId')
    if event['RequestType'] == 'Delete':
        cfnresponse.send(event, context, cfnresponse.SUCCESS, {}, physical_id)
        return

    # Never let an exception escape without a response, or CloudFormation waits the full hour then fails.
    try:
        props = event['ResourceProperties']
        pool_id = props['UserPoolId']
        temp_password = props['TempPassword']
        # Accept a list or a comma-separated string: iterating a string would register one bogus user per character.
        raw_emails = props.get('AdminEmails', [])
        if isinstance(raw_emails, str):
            raw_emails = raw_emails.split(',')
        emails = [e.strip() for e in raw_emails if e.strip()]
        idp = boto3.client('cognito-idp')
        results = []
        for email in emails:
            try:
                # TemporaryPassword is the fallback credential: if the permanent set below fails the account is still reachable through Cognito's NEW_PASSWORD_REQUIRED challenge rather than locked behind an unknown password.
                idp.admin_create_user(
                    UserPoolId=pool_id,
                    Username=email,
                    TemporaryPassword=temp_password,
                    UserAttributes=[
                        {'Name': 'email', 'Value': email},
                        {'Name': 'email_verified', 'Value': 'true'},
                        {'Name': 'custom:super_admin', 'Value': 'true'},
                    ],
                    MessageAction='SUPPRESS',
                )
            except idp.exceptions.UsernameExistsException:
                # Never re-seed an existing admin - that would clobber a password they have already changed.
                results.append({'email': email, 'status': 'exists'})
                continue
            except Exception as e:
                results.append({'email': email, 'error': str(e)})
                continue

            try:
                # Promote the same value to a permanent password so the account lands CONFIRMED. A temporary password leaves it FORCE_CHANGE_PASSWORD, and the admin dashboard has no NEW_PASSWORD_REQUIRED challenge screen, so sign-in would fail outright.
                idp.admin_set_user_password(
                    UserPoolId=pool_id,
                    Username=email,
                    Password=temp_password,
                    Permanent=True,
                )
                results.append({'email': email, 'status': 'created'})
            except Exception as e:
                # The user exists and the password still works, only via the first-sign-in challenge - report it instead of failing the stack.
                results.append({'email': email, 'status': 'created_force_change_password', 'error': str(e)})
        cfnresponse.send(event, context, cfnresponse.SUCCESS, {'Results': json.dumps(results)}, physical_id)
    except Exception as e:
        cfnresponse.send(event, context, cfnresponse.FAILED, {'Error': str(e)}, physical_id)
"""),
        )
        register_fn.add_to_role_policy(iam.PolicyStatement(
            actions=["cognito-idp:AdminCreateUser", "cognito-idp:AdminSetUserPassword"],
            resources=[pool_arn],
        ))

        registration = CustomResource(
            self, "AdminUserRegistration",
            service_token=register_fn.function_arn,
            properties={
                "UserPoolId": pool_id,
                "TempPassword": temp_password,
                "AdminEmails": admin_emails_prop,
                "Trigger": admin_emails_prop,  # re-fire when the effective list changes
            },
        )
        registration.node.add_dependency(read_password)

        CfnOutput(
            self, "AdminTempPassword",
            description="Shared admin sign-in password, usable as-is (change via the dashboard when convenient) [visibility:private]",
            value=temp_password,
        )
        CfnOutput(
            self, "AdminUserRegistrationResults",
            description="Per-admin registration status [visibility:private]",
            value=registration.get_att_string("Results"),
        )
