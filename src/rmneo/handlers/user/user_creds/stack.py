# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    aws_iam as iam,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
    create_cfn_api_method,
    get_or_create_api_resource,
    add_cors_options,
)


class UserCredsAPI(Construct):
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        function_name = "user_creds"
        user_creds_lambda_role = create_base_lambda_role(self, function_name, common_resources)

        # Add Cognito Identity permissions
        user_creds_lambda_role.add_to_policy(iam.PolicyStatement(
            actions=[
                "cognito-identity:GetId",
                "cognito-identity:GetCredentialsForIdentity"
            ],
            resources=["*"]
        ))

        # It verifies the bearer token in-handler: OIDC RS256 against the issuer's JWKS, or an
        # admin Cognito token against the admin-pool JWKS. ESPUSER_ISSUER + the admin pool id/JWKS
        # param and the ssm:GetParameter grant all come from create_lambda_function.
        self.user_creds_function = create_lambda_function(
            self,
            function_name,
            common_resources,
            lambda_role=user_creds_lambda_role,
            environment={
                "IDENTITY_POOL_ID": common_resources.identity_pool_id,
            }
        )

        # Create API Gateway resources using CFn to avoid cyclic dependencies
        # Share v1 resource if already created
        v1_parent_id = get_or_create_api_resource(
            self, "V1Resource", common_resources,
            common_resources.api_gateway_root_resource_id, "v1"
        )

        # Create nested resource path: /v1/user/credentials
        # First, create or get the user resource under v1
        user_resource_id = get_or_create_api_resource(
            self, "V1UserResource", common_resources,
            v1_parent_id, "user"
        )

        # Create credentials resource under user resource
        user_credentials_resource_id = get_or_create_api_resource(
            self, "UserCredentialsResource", common_resources,
            user_resource_id, "credentials"
        )

        # No gateway authorizer: the handler verifies the bearer token itself (OIDC RS256
        # or admin Cognito, routed by the token's `iss`).
        create_cfn_api_method(
            self, "UserCredentialsPostMethod", common_resources,
            user_credentials_resource_id, "POST", self.user_creds_function,
            authorization_type="NONE",
        )

        add_cors_options(
            self, "UserCredentialsOptionsMethod", common_resources,
            user_credentials_resource_id, allowed_methods=["POST"]
        )
