#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Merge CDK outputs from all cdk-outputs*.json files into a single file.
Remove duplicate output values while preserving all stack structures.
"""
import json
import sys
import glob
from collections import OrderedDict


def load_json_file(filepath):
    """Load JSON file and return its contents."""
    try:
        with open(filepath, 'r') as f:
            return json.load(f)
    except FileNotFoundError:
        print(f"Warning: {filepath} not found, skipping.", file=sys.stderr)
        return {}
    except json.JSONDecodeError as e:
        print(f"Error: Failed to parse {filepath}: {e}", file=sys.stderr)
        sys.exit(1)

def main():
    """Main function to merge CDK outputs."""
    output_file = "rmng-outputs.json"
    
    # Find all cdk-outputs*.json files
    output_files = sorted(glob.glob("build/cdk/cdk-outputs*.json"))
    
    if not output_files:
        print("Error: No cdk-outputs*.json files found.", file=sys.stderr)
        sys.exit(1)
    
    # Load all output files
    merged_outputs = OrderedDict()
    for output_file_path in output_files:
        outputs = load_json_file(output_file_path)
        if outputs:
            merged_outputs.update(outputs)
    
    if not merged_outputs:
        print("Error: No outputs found to merge.", file=sys.stderr)
        sys.exit(1)
    
    # Write merged outputs to file
    with open(output_file, 'w') as f:
        json.dump(merged_outputs, f, indent=2)
    
    print(f"Successfully merged {len(output_files)} CDK output file(s) into {output_file}")
    sys.exit(0)


if __name__ == "__main__":
    main()