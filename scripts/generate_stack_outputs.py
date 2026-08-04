#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
generate_stack_outputs.py

Reads cdk/Stackfile.yaml to determine stacks and their regions, queries CloudFormation
for outputs of deployed stacks, and writes rmng-outputs.json.

For rmng-alexa-core (deployed in multiple regions), stack_name is rmng-alexa-core-${APP_REGION}
(resolved with --region). CloudFormation stack rmng-alexa-core-<app-region> is queried in each
explicit region; the JSON key is the resolved name (e.g. rmng-alexa-core-ap-south-1) and each region
holds that stack's full CloudFormation outputs in regions.<region>.
"""

import json
import os
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError

_SCRIPT_DIR = Path(__file__).resolve().parent
_REPO_ROOT = _SCRIPT_DIR.parent
if str(_SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(_SCRIPT_DIR))

from cfn_stack_parser import load_stackfile, resolve_stack_name


def get_stack_outputs(cf_client, stack_name):
    """Return output key/value map, or None when the stack is missing in that region."""
    try:
        response = cf_client.describe_stacks(StackName=stack_name)
        stacks = response.get("Stacks", [])
        if not stacks:
            print(f"Warning: Stack '{stack_name}' not found.")
            return None

        outputs = stacks[0].get("Outputs", [])
        return {output["OutputKey"]: output["OutputValue"] for output in outputs}
    except ClientError as e:
        if "does not exist" in str(e):
            print(f"Warning: Stack '{stack_name}' does not exist in this region.")
            return None
        raise


def get_regions_for_stack(stack_def):
    """Regions listed in the Stackfile for explicit mode; empty means use the single default region."""
    if stack_def.regions.mode == "explicit":
        return list(stack_def.regions.explicit.keys())
    return []  # Caller will use default region


def cf_stack_name(stack_def, app_region: str) -> str:
    """Resolve ${APP_REGION} in stack_name (e.g. rmng-alexa-core-ap-south-1)."""
    return resolve_stack_name(
        stack_def.stack_name,
        {"APP_REGION": app_region},
    )


def main():
    """Walk Stackfile stacks, query CloudFormation per region rules, merge into rmng-outputs.json."""
    region = os.environ.get("AWS_REGION", "us-east-1")
    stackfile = _REPO_ROOT / "cdk/Stackfile.yaml"
    output_path = Path("rmng-outputs.json")

    if not stackfile.exists():
        print(f"Error: Stackfile not found: {stackfile}")
        sys.exit(1)

    stacks = load_stackfile(stackfile)

    # Load existing outputs if the file exists
    output_data = {}
    if output_path.exists():
        try:
            with open(output_path, "r") as f:
                output_data = json.load(f)
            print(f"Loaded existing data from {output_path}")
        except json.JSONDecodeError:
            print(f"Warning: {output_path} is not valid JSON. Starting fresh.")

    updated_count = 0

    for stack_def in stacks:
        regions = get_regions_for_stack(stack_def)
        name = cf_stack_name(stack_def, region)

        if regions:
            # Multi-region stack (e.g. rmng-alexa-core): same CF stack name in each AWS region.
            # Loop variable deliberately NOT named `region` — that shadowed the default region
            # and made every later single-region stack query the last multi-region entry.
            regions_dict = {}
            for stack_region in regions:
                cf_client = boto3.client("cloudformation", region_name=stack_region)
                print(f"  Fetching: {name} in {stack_region}")
                outputs = get_stack_outputs(cf_client, name)
                if outputs is not None:
                    regions_dict[stack_region] = outputs
                    print(f"    -> Found {len(outputs)} outputs for {stack_region}")

            if regions_dict:
                output_data[name] = {"regions": regions_dict}
                updated_count += 1
        else:
            # Single-region stack: use default region
            cf_client = boto3.client("cloudformation", region_name=region)
            print(f"  Fetching: {name} in {region}")
            outputs = get_stack_outputs(cf_client, name)
            if outputs is not None:
                output_data[name] = outputs
                updated_count += 1
                print(f"    -> Found {len(outputs)} outputs")

    if updated_count > 0:
        with open(output_path, "w") as f:
            json.dump(output_data, f, indent=2)
            f.write("\n")
        print(f"\nSuccessfully updated {output_path} with {updated_count} stack(s).")
    else:
        print("\nNo stack outputs were found/updated.")


if __name__ == "__main__":
    main()
