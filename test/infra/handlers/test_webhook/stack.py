# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    aws_iam as iam,
    aws_lambda as lambda_,
    aws_secretsmanager as secretsmanager,
)
from constructs import Construct
from app_common import CommonResources, get_or_create_api_resource, create_lambda_function, create_base_lambda_role, create_cfn_api_method, add_cors_options
from arn_utils import get_table_arn
from test.infra.stacks.test_constants import TABLE_NAMES

class WebhookApi(Construct):

    def _create_webhook_test_apis(self, common_resources: CommonResources, v1_parent_id: str, lambda_function: lambda_.Function) -> None:
        """
            Create following APIs
            POST /v1/token    - Issue mock access/id tokens
            POST /v1/data     - Capture a payload keyed by uuid
            GET /v1/validate - Read back a captured payload 
        """
        token_resource_id = get_or_create_api_resource(self, "TokenResource", common_resources, v1_parent_id, "token", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "TokenPostMethod",
            common_resources,
            token_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "TokenOptionsMethod",
            common_resources,
            token_resource_id,
            allowed_methods=["POST"]
        )


        data_resource_id = get_or_create_api_resource(self, "DataResource", common_resources, v1_parent_id, "data", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "DataPostMethod",
            common_resources,
            data_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "DataOptionsMethod",
            common_resources,
            data_resource_id,
            allowed_methods=["POST"]
        )

        validate_resource_id = get_or_create_api_resource(self, "ValidateResource", common_resources, v1_parent_id, "validate", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "ValidateGetMethod",
            common_resources,
            validate_resource_id,
            "GET",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "ValidateOptionsMethod",
            common_resources,
            validate_resource_id,
            allowed_methods=["GET"]
        )


    def _create_alexa_test_apis(self, common_resources: CommonResources, v1_parent_id: str, lambda_function: lambda_.Function) -> None:
        """
            Create following APIs
            POST /v1/alexa/token    - Issue mock access/id tokens
            POST /v1/alexa/data     - Capture a payload keyed by uuid
            GET /v1/alexa/validate - Read back a captured payload 
        """
        alexa_parent_id = get_or_create_api_resource(self, "AlexaResource", common_resources, v1_parent_id, "alexa", common_resources.api_gateway_id)

        token_resource_id = get_or_create_api_resource(self, "AlexaTokenResource", common_resources, alexa_parent_id, "token", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "AlexaTokenPostMethod",
            common_resources,
            token_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "AlexaTokenOptionsMethod",
            common_resources,
            token_resource_id,
            allowed_methods=["POST"]
        )

        data_resource_id = get_or_create_api_resource(self, "AlexaDataResource", common_resources, alexa_parent_id, "data", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "AlexaDataPostMethod",
            common_resources,
            data_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "AlexaDataOptionsMethod",
            common_resources,
            data_resource_id,
            allowed_methods=["POST"]
        )

        validate_resource_id = get_or_create_api_resource(self, "AlexaValidateResource", common_resources, alexa_parent_id, "validate", common_resources.api_gateway_id)
        create_cfn_api_method(
            self,
            "AlexaValidateGetMethod",
            common_resources,
            validate_resource_id,
            "GET",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "AlexaValidateOptionsMethod",
            common_resources,
            validate_resource_id,
            allowed_methods=["GET"]
        )



    def _create_gva_test_apis(self, common_resources: CommonResources, v1_parent_id: str, lambda_function: lambda_.Function) -> None:
        """
            Create following APIs
            POST /v1/gva/token    - Issue mock access/id tokens
            POST /v1/gva/data     - Capture a payload keyed by uuid
            GET /v1/gva/validate - Read back a captured payload 
        """
        gva_parent_id = get_or_create_api_resource(self, "GvaResource", common_resources, v1_parent_id, "gva", common_resources.api_gateway_id)

        # /gva/token
        token_resource_id = get_or_create_api_resource(self, "GvaTokenResource", common_resources, gva_parent_id, "token", common_resources.api_gateway_id)
        # POST /gva/token
        create_cfn_api_method(
            self,
            "GvaTokenPostMethod",
            common_resources,
            token_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "GvaTokenOptionsMethod",
            common_resources,
            token_resource_id,
            allowed_methods=["POST"]
        )

        # /gva/data
        data_resource_id = get_or_create_api_resource(self, "GvaDataResource", common_resources, gva_parent_id, "data", common_resources.api_gateway_id)
        # POST /gva/data
        create_cfn_api_method(
            self,
            "GvaDataPostMethod",
            common_resources,
            data_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "GvaDataOptionsMethod",
            common_resources,
            data_resource_id,
            allowed_methods=["POST"]
        )

        # /gva/validate
        validate_resource_id = get_or_create_api_resource(self, "GvaValidateResource", common_resources, gva_parent_id, "validate", common_resources.api_gateway_id)
        # GET /gva/validate
        create_cfn_api_method(
            self,
            "GvaValidateGetMethod",
            common_resources,
            validate_resource_id,
            "GET",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "GvaValidateOptionsMethod",
            common_resources,
            validate_resource_id,
            allowed_methods=["GET"]
        )

    def _create_smartthings_test_apis(self, common_resources: CommonResources, v1_parent_id: str, lambda_function: lambda_.Function) -> None:
        """
            Create following APIs
            POST /v1/smartthings/token    - Answer a Schema accessTokenRequest
            POST /v1/smartthings/data     - Capture a state callback keyed by bearer token
            GET /v1/smartthings/validate - Read back a captured state callback
        """
        st_parent_id = get_or_create_api_resource(self, "StResource", common_resources, v1_parent_id, "smartthings", common_resources.api_gateway_id)

        # /smartthings/token
        token_resource_id = get_or_create_api_resource(self, "StTokenResource", common_resources, st_parent_id, "token", common_resources.api_gateway_id)
        # POST /smartthings/token
        create_cfn_api_method(
            self,
            "StTokenPostMethod",
            common_resources,
            token_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "StTokenOptionsMethod",
            common_resources,
            token_resource_id,
            allowed_methods=["POST"]
        )

        # /smartthings/data
        data_resource_id = get_or_create_api_resource(self, "StDataResource", common_resources, st_parent_id, "data", common_resources.api_gateway_id)
        # POST /smartthings/data
        create_cfn_api_method(
            self,
            "StDataPostMethod",
            common_resources,
            data_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "StDataOptionsMethod",
            common_resources,
            data_resource_id,
            allowed_methods=["POST"]
        )

        # /smartthings/validate
        validate_resource_id = get_or_create_api_resource(self, "StValidateResource", common_resources, st_parent_id, "validate", common_resources.api_gateway_id)
        # GET /smartthings/validate
        create_cfn_api_method(
            self,
            "StValidateGetMethod",
            common_resources,
            validate_resource_id,
            "GET",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "StValidateOptionsMethod",
            common_resources,
            validate_resource_id,
            allowed_methods=["GET"]
        )

    def _create_matter_test_apis(self, common_resources: CommonResources, v1_parent_id: str, lambda_function: lambda_.Function) -> None:
        """
            Create the following APIs
            POST /v1/pair
            POST /v1/{endpointId}/command
            GET  /v1/{endpointId}/command
        """
        # /pair
        pair_resource_id = get_or_create_api_resource(self, "PairResource", common_resources, v1_parent_id, "pair", common_resources.api_gateway_id)
        # POST /pair
        create_cfn_api_method(
            self,
            "PairPostMethod",
            common_resources,
            pair_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        add_cors_options(
            self,
            "PairOptionsMethod",
            common_resources,
            pair_resource_id,
            allowed_methods=["POST"]
        )

        # /{endpointId}
        endpoint_id_parent_id = get_or_create_api_resource(self, "EndpointResource", common_resources, v1_parent_id, "{endpointId}", common_resources.api_gateway_id)
        # /{endpointId}/command
        command_resource_id = get_or_create_api_resource(self, "CommandResource", common_resources, endpoint_id_parent_id, "command", common_resources.api_gateway_id)
        # POST /{endpointId}/command
        create_cfn_api_method(
            self,
            "CommandPostMethod",
            common_resources,
            command_resource_id,
            "POST",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )
        create_cfn_api_method(
            self,
            "CommandGetMethod",
            common_resources,
            command_resource_id,
            "GET",
            lambda_function,
            api_key_required=True,
            authorization_type="NONE"
        )

        add_cors_options(
            self,
            "CommandOptionsMethod",
            common_resources,
            command_resource_id,
            allowed_methods=["GET", "POST"]
        )


    def __init__(self, scope: Construct, construct_id: str, common_resources: CommonResources) -> None :
        super().__init__(scope, construct_id)
        region = Stack.of(self).region

        test_webhook_function_name = "test_webhook"

        test_webhook_role = create_base_lambda_role(self, test_webhook_function_name, common_resources)
        test_webhook_role.add_to_policy(iam.PolicyStatement(
            actions=["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query", "dynamodb:DeleteItem"],
            resources=[
                get_table_arn(TABLE_NAMES['TEST_WEBHOOK_TABLE'], region)
            ]
        ))


        # Signing key for the mock's JWTs. Generated at deploy (never hardcoded);
        # issuance and verification share this one env var since both run in this
        # Lambda. It guards a test double, so a plain env var is sufficient.
        jwt_secret = secretsmanager.Secret(
            self,
            "WebhookJwtSecret",
            generate_secret_string=secretsmanager.SecretStringGenerator(
                password_length=48,
                exclude_punctuation=True,
            ),
        )

        self.test_webhook_function = create_lambda_function(
            self,
            test_webhook_function_name,
            common_resources,
            lambda_role=test_webhook_role,
            environment={"JWT_SECRET": jwt_secret.secret_value.unsafe_unwrap()},
        )

        v1_parent_id = get_or_create_api_resource(
            self,
            "V1Resource",
            common_resources,
            common_resources.api_gateway_root_resource_id,
            "v1",
            common_resources.api_gateway_id
        )

        self._create_webhook_test_apis(common_resources, v1_parent_id, self.test_webhook_function)
        self._create_alexa_test_apis(common_resources, v1_parent_id, self.test_webhook_function)
        self._create_gva_test_apis(common_resources, v1_parent_id, self.test_webhook_function)
        self._create_smartthings_test_apis(common_resources, v1_parent_id, self.test_webhook_function)
        self._create_matter_test_apis(common_resources, v1_parent_id, self.test_webhook_function)
        