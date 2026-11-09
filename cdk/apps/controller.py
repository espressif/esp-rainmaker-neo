#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""CDK app for RainMaker Neo Controller support (a node that reads and controls its group).

Its own app because the deploy engine maps one app per stack group
(scripts/deploy.sh: cdk/apps/<group>.py, then `cdk deploy --all` within it).
`controller` is an optional group, so building these stacks inside the rmng app
would deploy them with every `make deploy-rmng` whether the deployment wants
controller support or not.

One stack, rmng-controller-base: the IAM role, IoT role alias and IoT policy.
The controller needs no compute of its own: core's data Lambdas resolve it from
the certificate behind its credential-provider credentials at the single RBAC
checkpoint. See docs/en/specs/controller.md §7.1.

Reuses the `rmng` bootstrap qualifier, so no separate `make setup` is needed.
"""
import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk

from app_common import CommonResources, apply_common_tags
from src.controller.stacks.rmng_controller_base_stack import RMNGControllerBaseStack

app = cdk.App()
apply_common_tags(app)

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="rmng",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)


def _common():
    # The API id is read from SSM inside the stack, the claim/GVA/Alexa pattern, so nothing is passed here.
    return CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix="rmng-",
    )


RMNGControllerBaseStack(
    app, "rmng-controller-base", _common(),
    synthesizer=custom_synthesizer,
    description="RainMaker Neo Controller Base Stack - IAM role, IoT role alias and IoT policy for controller nodes",
)

app.synth()
