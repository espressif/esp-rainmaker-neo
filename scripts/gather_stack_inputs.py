#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Gather operator-supplied deploy inputs declared in cdk/Stackfile.yaml and persist them to rmng-inputs.json.

For the stacks in the requested --stack-group, every parameter marked `prompt: true` in the Stackfile is
resolved in this precedence: env var -> existing value in rmng-inputs.json (left unchanged) -> TTY prompt,
then written back to rmng-inputs.json under the stack id ({"espuser-core": {"admin_emails": "..."}}),
where the CDK app reads it at synth time. Each parameter's env var and rmng-inputs.json key are derived
from its name (AdminEmails -> RMNG_ADMIN_EMAILS / admin_emails). Non-interactive runs (CI) never block: an
unset, non-existing value is simply skipped.

A new stack only has to declare its prompt parameters in the Stackfile; no code change is needed here.
"""
import argparse
import json
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from cfn_stack_parser import load_stackfile  # noqa: E402

INPUTS = "rmng-inputs.json"


def _inputs_key(name: str) -> str:
    """AdminEmails -> admin_emails."""
    return re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", name).lower()


def _env_var(name: str) -> str:
    """AdminEmails -> RMNG_ADMIN_EMAILS."""
    return "RMNG_" + _inputs_key(name).upper()


def _load_inputs() -> dict:
    try:
        with open(INPUTS) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return {}


def _prompt_params_for_group(stackfile: str, group: str) -> list:
    """Return (stack_id, ParameterDef) pairs for prompt-enabled params in `group`."""
    stacks = load_stackfile(stackfile)
    pairs = []
    for stack in stacks:
        if stack.group != group:
            continue
        for p in stack.parameters:
            if p.prompt:
                pairs.append((stack.stack_id, p))
    return pairs


def main() -> int:
    parser = argparse.ArgumentParser(description="Gather Stackfile prompt inputs into rmng-inputs.json.")
    parser.add_argument("--stack-group", required=True, help="CDK stack group being deployed (e.g. espuser)")
    parser.add_argument("--stackfile", default="cdk/Stackfile.yaml", help="Path to the Stackfile")
    args = parser.parse_args()

    pairs = _prompt_params_for_group(args.stackfile, args.stack_group)
    if not pairs:
        return 0

    data = _load_inputs()
    interactive = sys.stdin.isatty()
    changed = False

    for stack_id, param in pairs:
        key = _inputs_key(param.name)
        existing = data.get(stack_id, {}).get(key)
        raw = os.environ.get(_env_var(param.name), "").strip()
        if not raw:
            if existing:
                print(f"{stack_id}.{key} already set in {INPUTS}; leaving unchanged.")
                continue
            if interactive:
                raw = input(f"{param.description or param.name}: ").strip()
        if not raw:
            continue
        data.setdefault(stack_id, {})[key] = raw
        changed = True
        print(f"Set {stack_id}.{key} in {INPUTS}: {raw}")

    if changed:
        with open(INPUTS, "w") as f:
            json.dump(data, f, indent=2)

    return 0


if __name__ == "__main__":
    sys.exit(main())
