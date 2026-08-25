#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""CDK app for the optional `ent` stack group (enterprise dashboard assets).

Deploying this group *is* the customer-side enablement: it grants the admin
browser role read access to the Espressif-owned central enterprise assets
bucket (per-feature signed manifests + runtime-loaded plugin bundles,
discovered by listing). Real entitlement is enforced by the central bucket's
policy in Espressif's account; without both grants the dashboard gets
AccessDenied and silently stays in community mode, exactly like the bridge.

Prototype placement: lives in the open repo for now; moves unchanged into the
esp-rainmaker-neo-enterprise superproject as an auto-discovered addon module
(`../addon_modules`, register(ctx)) — at which point this app disappears with
it. The Espressif-side resources (bucket, bucket policy, signing key) are
provisioned by dashboard_modules/scripts/provision.mjs in that superproject.

Kept out of the default `make deploy` (all-groups) sweep because deploying it
flips enterprise mode on — it should happen only when named explicitly
(`make deploy-ent`).

Reuses the `rmng` CDK bootstrap qualifier so no separate `make setup` is needed;
depends on rmng-base for the admin browser role it grants against.
"""
import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk
from app_common import CommonResources, apply_common_tags
from src.rmneo.stacks.enterprise_assets_stack import EnterpriseAssetsStack


app = cdk.App()
apply_common_tags(app)

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="rmng",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)

EnterpriseAssetsStack(
    app, "rmng-ent-base",
    CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix="rmng-",
    ),
    synthesizer=custom_synthesizer,
    description="RMNG Enterprise Base Stack - browser-role read grant on the central enterprise assets bucket",
)

app.synth()
