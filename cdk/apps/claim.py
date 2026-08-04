#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""CDK app for the optional `claim` stack group (assisted claiming).

Deploying this group *is* the enablement: it stands up the reservation table,
the CA KMS key, the claim handler, and the admin/claim routes. It carries no
deploy-time inputs and never reads rmng-inputs.json — the claiming variant,
per-claimant quota, certificate subject/validity and CA are all set at runtime
through the superadmin configuration API (stored in SSM). Until an admin sets a
mode and mints the CA, the handlers fail closed, so the group is safe to deploy
ahead of being configured.

Because the group is billed once its KMS key exists, it is deliberately kept out
of the default `make deploy` (all-groups) sweep and deployed only when named explicitly
(`make deploy-claim`). `make publish` still ships its template, so the module is
distributed with every release; the handlers fail closed until configured
(above), so it is safe to deploy ahead of enablement.

Reuses the `rmng` CDK bootstrap qualifier so no separate `make setup` is needed;
claim resources depend on the rmng/espuser stacks (shared RestApi ids, user
pool, user-details table) resolved cross-stack via SSM.
"""
import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk
from app_common import CommonResources, apply_common_tags
from src.claim.stacks.rmng_claim_base_stack import RMNGClaimBaseStack
from src.claim.stacks.rmng_claim_core_stack import RMNGClaimCoreStack


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


claim_base_stack = RMNGClaimBaseStack(
    app, "rmng-claim-base", _common(),
    synthesizer=custom_synthesizer,
    description="RMNG Claim Base Stack - reservation table + claiming CA KMS key",
)
claim_core_stack = RMNGClaimCoreStack(
    app, "rmng-claim-core", _common(),
    synthesizer=custom_synthesizer,
    description="RMNG Claim Core Stack - claim handler Lambda + endpoint",
)
claim_core_stack.add_stack_dependency(claim_base_stack)

app.synth()
