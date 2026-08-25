#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import importlib
import json
import os
import re
from collections.abc import Callable
from dataclasses import dataclass
from typing import Optional

import aws_cdk as cdk

from app_common import CommonResources, apply_common_tags
from src.rmneo.stacks.admin_dashboard_stack import AdminDashboardStack
from src.rmneo.stacks.rmng_base_stack import RMNGBaseStack
from src.rmneo.stacks.rmng_core_stack import RMNGCoreStack

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


# Ids double as capability strings on registered nodes and as i18n key fragments.
_CAPABILITY_ID_RE = re.compile(r"^[a-z][a-z0-9-]{0,31}$")

# CloudFormation caps an output value at 1024 bytes.
_CAPABILITY_MAX_BYTES = 1024


@dataclass
class ModuleContext:
    """Everything an optional module needs to synthesize its stacks, handed to
    its register() function. Core exposes only these generic seams — it holds no
    knowledge of what any module builds."""
    app: cdk.App
    synthesizer_factory: Callable[[], cdk.IStackSynthesizer]
    base_stack: cdk.Stack
    inputs: dict
    common_resources: Callable[[str], CommonResources]

    @property
    def synthesizer(self) -> cdk.IStackSynthesizer:
        """A *fresh* synthesizer on every access -- see custom_synthesizer(). A module
        reading ctx.synthesizer once per stack therefore gets one per stack, which is
        what keeps each add-on's asset manifest to its own assets."""
        return self.synthesizer_factory()

    def add_dashboard_capability(
        self,
        stack: cdk.Stack,
        *,
        id: str,
        label_fallback: str,
        icon: str = "",
        description_fallback: Optional[str] = None,
        label_key: Optional[str] = None,
        description_key: Optional[str] = None,
    ) -> None:
        """Surface a node-registration capability card in the admin dashboard.

        Emitted as a `DashboardCapability<Id>` output on `stack`, so the card
        exists exactly while that stack is deployed: the outputs pipeline
        publishes it into the deployment's client-outputs document, where the
        dashboard collects every `DashboardCapability*` key into
        EXTRA_CAPABILITIES. Output values are world-readable — never put
        secrets in a capability.

        `id` is the capability string the module's register hook receives for
        nodes registered with this card selected. The fallbacks are the floor
        presentation for deployments where no plugin loads; translations come
        from the module's dashboard plugin under i18n keys the dashboard
        derives by convention (`enterprise-<id>:capabilities.<id>.label` /
        `.description`) — pass `label_key`/`description_key` only to override
        that convention (e.g. a module contributing several capabilities).
        `icon` names an entry in the dashboard's enterprise icon registry.
        """
        if not _CAPABILITY_ID_RE.match(id):
            raise ValueError(
                f"dashboard capability id {id!r} must match {_CAPABILITY_ID_RE.pattern}"
            )
        entry = {"id": id, "labelFallback": label_fallback, "icon": icon}
        if label_key:
            entry["labelKey"] = label_key
        if description_key:
            entry["descriptionKey"] = description_key
        if description_fallback:
            entry["descriptionFallback"] = description_fallback
        value = json.dumps(entry, separators=(",", ":"))
        if len(value.encode()) > _CAPABILITY_MAX_BYTES:
            raise ValueError(
                f"dashboard capability {id!r} serializes to {len(value.encode())} bytes "
                f"(CloudFormation output cap is {_CAPABILITY_MAX_BYTES})"
            )
        suffix = "".join(part.capitalize() for part in id.split("-"))
        cdk.CfnOutput(stack, f"DashboardCapability{suffix}", value=value)


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
        with open('rmng-inputs.json') as f:
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

# A fresh synthesizer per stack, never one shared instance: DefaultStackSynthesizer keeps its
# asset manifest on the instance and reusableBind() inherits that same object through the
# prototype chain, so one shared synthesizer makes every stack publish every *other* stack's
# assets too. rmng-base was shipping a 41-asset, 429 MB manifest for the 7 assets it references.
def custom_synthesizer() -> cdk.DefaultStackSynthesizer:
    return cdk.DefaultStackSynthesizer(
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
    synthesizer=custom_synthesizer(),
    description="RMNG Base Stack - Storage, Networking, and Infrastructure Resources"
)

core_stack = RMNGCoreStack(
    app,
    "rmng-core",
    common_resources_core,
    synthesizer=custom_synthesizer(),
    description="RMNG Core Stack - Compute Resources (Lambda, ECS, API Gateway Integrations)"
)

core_stack.add_stack_dependency(base_stack)

# Optional add-on modules
module_ctx = ModuleContext(
    app=app,
    synthesizer_factory=custom_synthesizer,
    base_stack=base_stack,
    inputs=rmng_inputs,
    common_resources=make_common_resources,
)
for _name, register in discover_optional_modules():
    register(module_ctx)

if not os.environ.get("DASHBOARD_SKIP"):
    admin_dashboard_stack = AdminDashboardStack(
        app,
        "rmng-admin-dashboard",
        common_resources_admin_dashboard,
        synthesizer=custom_synthesizer(),
        description="RMNG Admin Dashboard - Frontend Deployment",
    )
    admin_dashboard_stack.add_stack_dependency(base_stack)


app.synth()
