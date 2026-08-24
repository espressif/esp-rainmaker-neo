#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import os
import json
import aws_cdk as cdk
from aws_cdk import Tags
from src.smartthings.stacks.st_stack import STStack
from src.smartthings.stacks.rmng_st_cfg_core_stack import RMNGSTCfgCoreStack
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

# CDK bootstrap qualifiers are capped at 10 characters, hence "sthing".
custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="sthing",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)

common_resources = CommonResources(
    api_gateway_id="",
    api_gateway_root_resource_id="",
    admin_api_resource_id="",
    cognito_authorizer_id="",
    prefix="rmng-st-",
)


def _cfg_common_resources():
    """Fresh CommonResources for the configuration API.

    Deliberately the plain "rmng-" prefix, not this app's "rmng-st-": the config
    Lambda was created in rmng-core as `rmng-st-cfg`, and the prefix feeds the
    physical function name. Reusing the app's prefix would rename it to
    `rmng-st-st-cfg` and replace the function on deploy.
    """
    return CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix="rmng-",
    )

# Parameters resolved from rmng-outputs.json unless CDK_PUBLISH=true (then empty defaults; override at deploy)
st_params = {
    "rmng_region": "" if is_publish else rmng_region,
    "esp_user_client_id": esp_user_base.get('EspUserClientId', ''),
    "esp_admin_user_pool_id": esp_user_base.get('EspAdminUserPoolId', ''),
    "esp_admin_user_pool_client_id": esp_user_base.get('EspAdminUserPoolClientId', ''),
    "esp_user_jwks": esp_user_base.get('EspUserJWKSParameter', ''),
    "esp_admin_user_pool_jwks": esp_user_base.get('EspAdminUserPoolJWKSParameter', ''),
    # Must match st_stack's CfnParameter name, or the Schema App lambdas deploy with an empty
    # USER_ISSUER and reject every linked token.
    "esp_user_issuer": esp_user_base.get('EspUserDiscoveryIssuer', ''),
    "identity_pool_id": rmng_base.get('IdentityPoolId', ''),
}

# Construct id sets the synth artifact name (must match Stackfile: rmng-st-core.template.json).
# Physical CloudFormation stack name stays region-suffixed for parity with Stackfile stack_name.
STStack(
    app,
    "rmng-st-core",
    common_resources,
    st_params=st_params,
    synthesizer=custom_synthesizer,
    stack_name=f"rmng-st-core-{rmng_region}",
    description=f"RMNG SmartThings Core Stack - Schema App Lambda Stack ({rmng_region})",
)

# The configuration API is a single set of routes on the shared API Gateway, so
# unlike the Schema App Lambda it is created once rather than per region.
# deploy.sh loops this app with AWS_REGION set to each region and RMNG_REGION
# pinned to the backend region; the two match only on the pass that targets the
# backend, which is where the shared API lives. On a publish synth there is no
# loop, so the stack is always emitted.
deploy_region = os.environ.get("AWS_REGION")
if is_publish or not deploy_region or deploy_region == rmng_region:
    RMNGSTCfgCoreStack(
        app,
        "rmng-st-cfg-core",
        _cfg_common_resources(),
        synthesizer=custom_synthesizer,
        description="RMNG SmartThings Cfg Core Stack - SmartThings configuration API",
    )

app.synth()
