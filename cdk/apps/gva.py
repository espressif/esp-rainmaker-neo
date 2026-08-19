#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""CDK app for the Google Home (GVA) integration.

Split out of `rmng-core` so each voice-assistant integration owns its own
compute, matching Alexa and SmartThings. Unlike those two, whose Lambdas are
invoked directly by their platform, GVA fulfillment is an HTTPS endpoint on the
shared API Gateway — so this follows the `claim` pattern: the shared RestApi id
and /v1 resource id are resolved cross-stack via SSM, the routes attach to the
existing API, and a custom resource redeploys the prod stage so they go live.

The fulfillment path is unchanged (`/v1/integrations/gva` on the same API
Gateway), so Google Home projects configured against an earlier deployment keep
working.

Reuses the `rmng` CDK bootstrap qualifier, so no separate `make setup` is
needed; GVA resources depend on the rmng/espuser stacks (shared RestApi ids,
user pool, identity pool) resolved cross-stack via SSM.
"""
import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk
from app_common import CommonResources, apply_common_tags
from src.gva.stacks.rmng_gva_core_stack import RMNGGVACoreStack


app = cdk.App()
apply_common_tags(app)

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="rmng",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)


def _common():
    return CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix="rmng-",
    )


RMNGGVACoreStack(
    app, "rmng-gva-core", _common(),
    synthesizer=custom_synthesizer,
    description="RMNG GVA Core Stack - Google Home fulfillment Lambda + endpoint",
)

app.synth()
