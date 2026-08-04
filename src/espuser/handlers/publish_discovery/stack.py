# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from aws_cdk import (
    Stack,
    CustomResource,
    aws_iam as iam,
)
from constructs import Construct
from app_common import (
    CommonResources,
    create_lambda_function,
    create_base_lambda_role,
)
from arn_utils import get_ssm_parameter_arn, get_s3_object_arn
from src.espuser.stacks.base_res_constants import USER_SSM_PARAMETERS


class PublishDiscovery(Construct):
    """Publishes the OIDC/OAuth discovery documents to S3 once, at stack create, via a custom resource backed by the publish_discovery Go Lambda. The verify key is the KMS signing key's public half; no private key exists outside KMS."""

    def __init__(
        self,
        scope: Construct,
        id: str,
        common_resources: CommonResources,
        discovery_bucket,
        issuer: str,
        api_base: str,
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

        # Write only the three discovery objects under the well-known prefix.
        lambda_role.add_to_policy(iam.PolicyStatement(
            actions=["s3:PutObject"],
            resources=[get_s3_object_arn(discovery_bucket.bucket_name, ".well-known/*")],
        ))

        if signing_kms_key is None:
            raise ValueError("PublishDiscovery requires the KMS signing key — tokens sign exclusively via kms:Sign")

        environment = {
            "USER_ISSUER": issuer,
            "ESPUSER_API_BASE": api_base,
            "ESPUSER_DISCOVERY_BUCKET": discovery_bucket.bucket_name,
            "USER_JWKS_PARA_NAME": jwks_param,
            "ESPUSER_KMS_SIGNING_KEY_ARN": signing_kms_key.key_arn,
        }
        cr_properties = {
            "Issuer": issuer,
            "ApiBase": api_base,
            "Bucket": discovery_bucket.bucket_name,
            "JwksParam": jwks_param,
            "KmsKeyArn": signing_kms_key.key_arn,
            "PublishVersion": "4",
        }
        signing_kms_key.grant(lambda_role, "kms:GetPublicKey")

        publish_function = create_lambda_function(
            self, function_name,
            common_resources,
            lambda_role=lambda_role,
            environment=environment,
        )

        # Properties make CloudFormation fire an Update (which republishes) whenever a publish input changes. JwksParam is included so a change to the JWKS parameter path re-publishes to the new location; bump PublishVersion to force a republish when the output changes for a reason not captured by the others (e.g. cache-control headers, document fields).
        CustomResource(
            self, "PublishDiscoveryResource",
            service_token=publish_function.function_arn,
            properties=cr_properties,
        )

        self.publish_function = publish_function
