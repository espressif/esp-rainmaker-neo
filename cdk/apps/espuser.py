# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import _bootstrap  # noqa: F401 — sys.path setup; must precede every repo-local import

import os
import importlib
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Callable

import aws_cdk as cdk
from src.espuser.stacks.esp_user_base_stack import EspUserBaseStack
from src.espuser.stacks.esp_user_core_stack import EspUserCoreStack
from app_common import apply_common_tags, get_rmng_inputs, CommonResources

OPTIONAL_MODULES_DIR = "addon_modules"

@dataclass
class EspUserModuleContext:
    """Seam handed to each enterprise add-on's register_espuser(). Mirrors rmng.py's
    ModuleContext but for the espuser (identity) app: the add-on gets the app, the shared
    synthesizer, the base/core stacks to depend on, inputs, and a per-prefix CommonResources
    factory. A single deploy_timestamp is shared so every add-on re-snapshots the same API build."""
    app: cdk.App
    synthesizer: cdk.IStackSynthesizer
    base_stack: cdk.Stack
    core_stack: cdk.Stack
    inputs: dict
    deploy_timestamp: str
    common_resources: Callable[[str], CommonResources]

def discover_espuser_modules():
    """Yield (name, register_espuser) for each add-on package exposing that entrypoint.

    The espuser app uses register_espuser (not register) so that add-ons meant for the rmng core
    app — src_acc, src_bridge, which define register — are not synthesized onto the identity app,
    and vice-versa. Absent addon_modules folder (OSS checkout) → nothing, pure Cognito-only core."""
    root = _bootstrap.REPO_ROOT.parent / OPTIONAL_MODULES_DIR
    if not root.is_dir():
        return
    for pkg in sorted(p for p in root.iterdir() if (p / "__init__.py").exists()):
        try:
            module = importlib.import_module(f"{OPTIONAL_MODULES_DIR}.{pkg.name}")
        except ModuleNotFoundError:
            continue
        register = getattr(module, "register_espuser", None)
        if callable(register):
            yield pkg.name, register

def make_common_resources(prefix: str) -> CommonResources:
    return CommonResources(
        api_gateway_id="",
        api_gateway_root_resource_id="",
        admin_api_resource_id="",
        cognito_authorizer_id="",
        prefix=prefix,
    )

app = cdk.App()
apply_common_tags(app)

custom_synthesizer = cdk.DefaultStackSynthesizer(
    qualifier="espuser",
    file_assets_bucket_name="cdk-${Qualifier}-assets-${AWS::AccountId}-${AWS::Region}",
)

rmng_inputs = {} if os.environ.get('CDK_PUBLISH') == 'true' else get_rmng_inputs()
admin_emails = rmng_inputs.get('espuser-core', {}).get('admin_emails', '')
if isinstance(admin_emails, str):
    admin_emails = [e.strip() for e in admin_emails.split(',') if e.strip()]

esp_user_base_stack = EspUserBaseStack(app, "espuser-base", synthesizer=custom_synthesizer)
esp_user_core_stack = EspUserCoreStack(app, "espuser-core", admin_emails=admin_emails, synthesizer=custom_synthesizer)
esp_user_core_stack.add_stack_dependency(esp_user_base_stack)

module_ctx = EspUserModuleContext(
    app=app,
    synthesizer=custom_synthesizer,
    base_stack=esp_user_base_stack,
    core_stack=esp_user_core_stack,
    inputs=rmng_inputs,
    deploy_timestamp=datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    common_resources=make_common_resources,
)
for name, register_espuser in discover_espuser_modules():
    register_espuser(module_ctx)

app.synth()

