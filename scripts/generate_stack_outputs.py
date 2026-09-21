#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
generate_stack_outputs.py

Reads cdk/Stackfile.yaml to determine stacks and their regions, queries CloudFormation
for outputs of deployed stacks, and writes rmng-outputs.json.

Descriptions come back with the values, so outputs marked [visibility:private] are recorded under PRIVATE_PATHS_KEY here rather than costing upload_rmng_outputs.py a second describe of every stack.

For rmng-alexa-core (deployed in multiple regions), stack_name is rmng-alexa-core-${APP_REGION}
(resolved with --region). CloudFormation stack rmng-alexa-core-<app-region> is queried in each
explicit region; the JSON key is the resolved name (e.g. rmng-alexa-core-ap-south-1) and each region
holds that stack's full CloudFormation outputs in regions.<region>.
"""

import json
import os
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import boto3
from botocore.exceptions import ClientError

# Every stack is an independent describe_stacks, and this script runs once per deploy wave,
# so the whole thing is latency. Mirrors the pool in scripts/publish_cdk_assets.py.
MAX_WORKERS = 8

# Marks an output that must never reach the public rmng-public-assets bucket. Imported by everything that acts on it, so the annotation has one spelling.
PRIVATE_MARKER = "[visibility:private]"

# Acknowledges that an output whose name reads like a secret (token, credential, ...) really is safe to publish. Carries no behaviour; scripts/check_output_markers.py requires one marker or the other so the call is always deliberate.
PUBLIC_MARKER = "[visibility:public]"

# Underscore keeps it clear of the stack-name namespace; upload_rmng_outputs.py strips it from the published copy.
PRIVATE_PATHS_KEY = "_private_output_paths"

_SCRIPT_DIR = Path(__file__).resolve().parent
_REPO_ROOT = _SCRIPT_DIR.parent
if str(_SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(_SCRIPT_DIR))

from cfn_stack_parser import load_stackfile, resolve_stack_name


def get_stack_outputs(cf_client, stack_name):
    """Return (output key/value map, keys marked private), or None when the stack is missing."""
    try:
        response = cf_client.describe_stacks(StackName=stack_name)
        stacks = response.get("Stacks", [])
        if not stacks:
            print(f"Warning: Stack '{stack_name}' not found.")
            return None

        outputs = stacks[0].get("Outputs", [])
        values = {output["OutputKey"]: output["OutputValue"] for output in outputs}
        private = sorted(
            output["OutputKey"] for output in outputs
            if PRIVATE_MARKER in (output.get("Description") or "")
        )
        return values, private
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

    # Plan the queries first, then run them all at once. Each entry is
    # (stack_name, aws_region); the multi-region stacks (e.g. rmng-alexa-core) contribute
    # one entry per region under the same CloudFormation stack name.
    #
    # Loop variable deliberately NOT named `region` — that shadowed the default region and
    # made every later single-region stack query the last multi-region entry.
    queries = []
    multi_region_names = set()
    for stack_def in stacks:
        regions = get_regions_for_stack(stack_def)
        name = cf_stack_name(stack_def, region)
        if regions:
            multi_region_names.add(name)
            queries.extend((name, stack_region) for stack_region in regions)
        else:
            queries.append((name, region))

    # One client per region, built here rather than inside the workers: constructing a client
    # is what costs (a fresh connection pool, so a fresh TLS handshake per call), and boto3
    # clients are safe to *call* from several threads but not safe to *create* from them.
    clients = {r: boto3.client("cloudformation", region_name=r) for _, r in queries}

    for name, stack_region in queries:
        print(f"  Fetching: {name} in {stack_region}")
    with ThreadPoolExecutor(max_workers=MAX_WORKERS) as pool:
        fetched = list(pool.map(
            lambda q: get_stack_outputs(clients[q[1]], q[0]),
            queries,
        ))

    # Merge single-threaded, so the output file's shape stays exactly as it was.
    regions_by_name: dict = {}
    # Path into output_data per private output, e.g. ["espuser-base", "EspMcpClientSecret"]. Rebuilt every run so an output that stops being private cannot linger.
    private_paths: list = []
    for (name, stack_region), result in zip(queries, fetched):
        outputs, private_keys = result if result is not None else (None, [])
        if name in multi_region_names:
            if outputs is not None:
                regions_by_name.setdefault(name, {})[stack_region] = outputs
                private_paths.extend([name, "regions", stack_region, k] for k in private_keys)
                print(f"    -> Found {len(outputs)} outputs for {name} in {stack_region}")
            continue

        if outputs is not None:
            output_data[name] = outputs
            private_paths.extend([name, k] for k in private_keys)
            updated_count += 1
            print(f"    -> Found {len(outputs)} outputs for {name}")
        elif output_data.pop(name, None) is not None:
            # The dashboard reads presence of a stack's entry as "this
            # integration is deployed", so a stack that has been destroyed
            # must lose its entry instead of lingering from an earlier run.
            updated_count += 1
            print(f"    -> {name} gone; dropped its stale outputs")

    for name, regions_dict in regions_by_name.items():
        output_data[name] = {"regions": regions_dict}
        updated_count += 1

    # A run that only flips a marker changes nothing else, so count it as an update or it never reaches disk.
    private_paths = sorted(private_paths)
    if output_data.get(PRIVATE_PATHS_KEY) != private_paths:
        updated_count += 1
    output_data[PRIVATE_PATHS_KEY] = private_paths
    if private_paths:
        print(f"\n{len(private_paths)} output(s) marked {PRIVATE_MARKER}, recorded for redaction:")
        for path in sorted(private_paths):
            print(f"  {' > '.join(path)}")

    if updated_count > 0:
        with open(output_path, "w") as f:
            json.dump(output_data, f, indent=2)
            f.write("\n")
        print(f"\nSuccessfully updated {output_path} with {updated_count} stack(s).")
    else:
        print("\nNo stack outputs were found/updated.")


if __name__ == "__main__":
    main()
