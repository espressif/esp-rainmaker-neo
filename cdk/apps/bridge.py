#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""CDK app for bridge support (bridged children behind a bridge node).

Its own app because the deploy engine maps one app per stack group
(scripts/deploy.sh: cdk/apps/<group>.py, then `cdk deploy --all` within it).
`bridge` is an optional group, so building these stacks inside the rmng app
would deploy them with every `make deploy-rmng` whether the deployment wants
bridge support or not.

Split into base (bridge-children table + IoT policy) and core (Lambdas + IoT
rules), mirroring rmng's own split. The two are joined by name rather than by a
CFN cross-stack reference, so the ordering is stated explicitly below.

Reuses the `rmng` bootstrap qualifier, so no separate `make setup` is needed.
"""
import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk
from app_common import CommonResources, apply_common_tags
from src.bridge.stacks.rmng_bridge_base_stack import RMNGBridgeBaseStack
from src.bridge.stacks.rmng_bridge_core_stack import RMNGBridgeCoreStack


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


bridge_base = RMNGBridgeBaseStack(
    app, "rmng-bridge-base", _common(),
    synthesizer=custom_synthesizer,
    description="RMNG Bridge Base Stack - bridge-children DDB table + IoT policy",
)

bridge_core = RMNGBridgeCoreStack(
    app, "rmng-bridge-core", _common(),
    synthesizer=custom_synthesizer,
    description="RMNG Bridge Core Stack - Lambdas + IoT rules for bridged-child support (see docs/en/specs/bridge.md)",
)
bridge_core.add_stack_dependency(bridge_base)

app.synth()
