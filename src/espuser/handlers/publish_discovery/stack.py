# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    CustomResource,
    Stack,
    aws_iam as iam,
)
from constructs import Construct

from app_common import (
    CommonResources,
    create_base_lambda_role,
    create_lambda_function,
)
from arn_utils import get_ssm_parameter_arn
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS


class PublishDiscovery(Construct):
    """Writes the JWKS to SSM once per deploy, via a custom resource backed by the publish_discovery Go Lambda. The verify key is the KMS signing key's public half; no private key exists outside KMS."""

    def __init__(
        self,
        scope: Construct,
        id: str,
        common_resources: CommonResources,
        signing_kms_key=None,
        **kwargs,
    ) -> None:
        super().__init__(scope, id, **kwargs)

        region = Stack.of(self).region
        function_name = "publish_discovery"
        jwks_param = USER_SSM_PARAMETERS['ESP_USER_JWKS']

        lambda_role = create_base_lambda_role(self, function_name, common_resources)

        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["ssm:GetParameter", "ssm:PutParameter"],
            resources=[get_ssm_parameter_arn(jwks_param, region)],
        ))

        if signing_kms_key is None:
            raise ValueError("PublishDiscovery requires the KMS signing key — tokens sign exclusively via kms:Sign")

        environment = {
            "USER_JWKS_PARA_NAME": jwks_param,
            "ESPUSER_KMS_SIGNING_KEY_ARN": signing_kms_key.key_arn,
        }
        signing_kms_key.grant(lambda_role, "kms:GetPublicKey")

        publish_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment=environment,
        )

        # A new key ARN or parameter path changes these properties, so CloudFormation fires an Update and the JWKS is republished.
        CustomResource(
            self, "PublishDiscoveryResource",
            service_token=publish_function.function_arn,
            properties={"JwksParam": jwks_param, "KmsKeyArn": signing_kms_key.key_arn},
        )

        self.publish_function = publish_function
