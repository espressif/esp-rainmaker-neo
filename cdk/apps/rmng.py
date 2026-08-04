#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import os
import json
import importlib
from dataclasses import dataclass
from typing import Callable

import aws_cdk as cdk
from src.rmneo.stacks.rmng_base_stack import RMNGBaseStack
from src.rmneo.stacks.rmng_core_stack import RMNGCoreStack
from src.rmneo.stacks.admin_dashboard_stack import AdminDashboardStack
from app_common import CommonResources, apply_common_tags

# Directory (sibling of this app) holding separately-distributed optional add-on modules. Overridable via env for alternate layouts.
OPTIONAL_MODULES_DIR = os.environ.get("RMNG_OPTIONAL_MODULES_DIR", "addon_modules")


def make_common_resources(prefix):
    """Build a CommonResources with the standard empty wiring and given prefix."""
    return CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix=prefix,
    )


@dataclass
class ModuleContext:
    """Everything an optional module needs to synthesize its stacks, handed to
    its register() function. Core exposes only these generic seams — it holds no
    knowledge of what any module builds."""
    app: cdk.App
    synthesizer: cdk.IStackSynthesizer
    base_stack: cdk.Stack
    inputs: dict
    common_resources: Callable[[str], CommonResources]


def discover_optional_modules():
    """Yield each optional add-on module that exposes a register() entrypoint.

    Optional modules live in a separately-distributed `optional_modules` folder that sits alongside this app. Core knows nothing about any specific module: it simply scans that folder for
    sub-packages, and each sub-package that defines a top-level `register(ctx)`
    callable is wired in. When the folder is absent (open-source checkout), this
    yields nothing and the app synthesizes as pure core.

    A module opts in by defining, in its package `__init__.py`:

        def register(ctx):
            stack = MyStack(ctx.app, "rmng-my", ctx.common_resources("rmng-my-"),
                            synthesizer=ctx.synthesizer, ...)
            stack.add_stack_dependency(ctx.base_stack)
    """
    root = _bootstrap.REPO_ROOT.parent / OPTIONAL_MODULES_DIR
    if not root.is_dir():
        return
    for pkg in sorted(p for p in root.iterdir() if (p / "__init__.py").exists()):
        try:
            module = importlib.import_module(f"{OPTIONAL_MODULES_DIR}.{pkg.name}")
        except ModuleNotFoundError:
            continue
        register = getattr(module, "register", None)
        if callable(register):
            yield pkg.name, register

def get_rmng_inputs():
    """Read RMNG input configuration from rmng-inputs.json"""
    try:
        with open('rmng-inputs.json', 'r') as f:
            inputs = json.load(f)
        return inputs
    except FileNotFoundError:
        print("Warning: rmng-inputs.json not found. Using default configuration.")
        return {}
    except json.JSONDecodeError as e:
        print(f"Error reading rmng-inputs.json: {e}")
        return {}
        
app = cdk.App()
apply_common_tags(app)

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="rmng",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)

# A published template must not carry any operator's opt-ins, so publishing
# synthesizes with empty inputs. Every optional feature reads its own key off
# this dict and defaults to off when absent -- core names none of them.
rmng_inputs = {} if os.environ.get('CDK_PUBLISH') == 'true' else get_rmng_inputs()

common_resources_base = CommonResources(
    api_gateway_id="",
    api_gateway_root_resource_id="",
    admin_api_resource_id="",
    cognito_authorizer_id="",
    prefix="rmng-",
)

common_resources_core = CommonResources(
    api_gateway_id="",
    api_gateway_root_resource_id="",
    admin_api_resource_id="",
    cognito_authorizer_id="",
    prefix="rmng-",
)

common_resources_admin_dashboard = CommonResources(
    api_gateway_id="",
    api_gateway_root_resource_id="",
    admin_api_resource_id="",
    cognito_authorizer_id="",
    prefix="rmng-",
)


base_stack = RMNGBaseStack(
    app,
    "rmng-base",
    common_resources_base,
    synthesizer=custom_synthesizer,
    description="RMNG Base Stack - Storage, Networking, and Infrastructure Resources"
)

core_stack = RMNGCoreStack(
    app,
    "rmng-core",
    common_resources_core,
    synthesizer=custom_synthesizer,
    description="RMNG Core Stack - Compute Resources (Lambda, ECS, API Gateway Integrations)"
)

core_stack.add_stack_dependency(base_stack)

# Optional add-on modules
module_ctx = ModuleContext(
    app=app,
    synthesizer=custom_synthesizer,
    base_stack=base_stack,
    inputs=rmng_inputs,
    common_resources=make_common_resources,
)
for name, register in discover_optional_modules():
    register(module_ctx)

if not os.environ.get("DASHBOARD_SKIP"):
    admin_dashboard_stack = AdminDashboardStack(
        app,
        "rmng-admin-dashboard",
        common_resources_admin_dashboard,
        synthesizer=custom_synthesizer,
        description="RMNG Admin Dashboard - Frontend Deployment",
    )
    admin_dashboard_stack.add_stack_dependency(base_stack)


app.synth()
