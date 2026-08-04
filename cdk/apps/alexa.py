#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import os
import json
import aws_cdk as cdk
from aws_cdk import Tags
from src.alexa.stacks.alexa_stack import AlexaStack
from app_common import CommonResources, apply_common_tags


def get_rmng_outputs():
    """Read RMNG outputs for cross-stack parameter resolution.

    Resolves through scripts.rmng_outputs so the path stays in one place (the file
    lives under scripts/). Fail loudly; publishing passes is_publish and never reaches here.
    """
    from scripts.rmng_outputs import load, resolve_source
    try:
        return load()
    except FileNotFoundError as e:
        raise SystemExit(
            f"error: {resolve_source()} not found. Deploy rmng+espuser first, or run "
            f"`./scripts/deploy.sh --fetch-and-upload` to populate it."
        ) from e

def get_rmng_region():
    return os.environ.get("RMNG_REGION") or os.environ.get("AWS_REGION", "us-east-1")

def _override_app_region(app):
    if os.environ.get("CDK_PUBLISH") != "true":
            Tags.of(app).add("AppRegion", get_rmng_region())


app = cdk.App()
apply_common_tags(app)
_override_app_region(app)

is_publish = os.environ.get("CDK_PUBLISH") == "true"
rmng_region = get_rmng_region()
rmng_outputs = {} if is_publish else get_rmng_outputs()
esp_user_base = rmng_outputs.get('espuser-base', {})
rmng_base = rmng_outputs.get('rmng-base', {})

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="alexa",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)

common_resources = CommonResources(
    api_gateway_id="",
    api_gateway_root_resource_id="",
    admin_api_resource_id="",
    cognito_authorizer_id="",
    prefix="rmng-alexa-",
)

# Parameters resolved from rmng-outputs.json unless CDK_PUBLISH=true (then empty defaults; override at deploy)
alexa_params = {
    "rmng_region": "" if is_publish else rmng_region,
    "esp_user_client_id": esp_user_base.get('EspUserClientId', ''),
    "esp_admin_user_pool_id": esp_user_base.get('EspAdminUserPoolId', ''),
    "esp_admin_user_pool_client_id": esp_user_base.get('EspAdminUserPoolClientId', ''),
    "esp_user_jwks": esp_user_base.get('EspUserJWKSParameter', ''),
    "esp_admin_user_pool_jwks": esp_user_base.get('EspAdminUserPoolJWKSParameter', ''),
    # Must match alexa_stack's CfnParameter name, or the skill lambdas deploy with an empty
    # USER_ISSUER and reject every linked token.
    "esp_user_issuer": esp_user_base.get('EspUserDiscoveryIssuer', ''),
    "identity_pool_id": rmng_base.get('IdentityPoolId', ''),
}

# Construct id sets the synth artifact name (must match Stackfile: rmng-alexa-core.template.json).
# Physical CloudFormation stack name stays region-suffixed for parity with Stackfile stack_name.
AlexaStack(
    app,
    "rmng-alexa-core",
    common_resources,
    alexa_params=alexa_params,
    synthesizer=custom_synthesizer,
    stack_name=f"rmng-alexa-core-{rmng_region}",
    description=f"RMNG Alexa Core Stack - Alexa Skill Lambda Stack ({rmng_region})",
)

app.synth()