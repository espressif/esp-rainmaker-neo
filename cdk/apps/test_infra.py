# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import aws_cdk as cdk
from test.infra.stacks.test_infra_base_stack import TestInfraBaseStack
from test.infra.stacks.test_infra_core_stack import TestInfraCoreStack

app = cdk.App()

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="rmng-test",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}"
)

test_infra_base_stack = TestInfraBaseStack(
    app,
    "rmng-test-infra-base",
    synthesizer=custom_synthesizer,
    description="RMNG Test Infra Base Stack - Storage and Infrastructure Resources"
)

test_infra_core_stack = TestInfraCoreStack(
    app,
    "rmng-test-infra-core",
    synthesizer=custom_synthesizer,
    description="RMNG Test Infra Core Stack - Compute Resources (Lambda, ECS, API Gateway Integrations)"
)

test_infra_core_stack.add_stack_dependency(test_infra_base_stack)

app.synth()