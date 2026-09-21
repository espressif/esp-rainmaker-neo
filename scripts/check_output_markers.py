#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
check_output_markers.py

Guards the two ways a stack output leaks into the public rmng-public-assets bucket. Runs at synth time against the templates, with no AWS access.

1. A near-miss marker. Redaction turns on the exact substring [visibility:private], so "[visibility: private]" or "[private]" read as an intent to redact, match nothing, and publish the value while looking protected in the source.

2. A secret-sounding output with no marker at all. An output *named* secret, password, token, credential or key must carry [visibility:private] to be redacted, or [visibility:public] to record that it really is safe -- an ARN, a URL, a resource name. Neither marker fails the build, so the call is always deliberate rather than defaulted. Only the name is matched: a description mentioning a secret-ish service ("via Credential Provider") says nothing about what the value is.

Usage
    python3 scripts/check_output_markers.py [cdk.out.rmng cdk.out.espuser ...]

Defaults to every cdk.out.* directory in the repo root and under cdk/ (aws_cdk resolves CDK_OUTDIR relative to the app's own directory, so the synth jobs land theirs in cdk/).
"""

import json
import re
import sys
from pathlib import Path

_SCRIPT_DIR = Path(__file__).resolve().parent
_REPO_ROOT = _SCRIPT_DIR.parent

# Import the marker rather than restate it: a check holding its own copy of the string it checks is the drift it exists to prevent.
sys.path.insert(0, str(_SCRIPT_DIR))
from generate_stack_outputs import PRIVATE_MARKER, PUBLIC_MARKER  # noqa: E402

# Deliberately broad: a false positive costs one annotation, a false negative publishes a secret.
_NEAR_MISS = re.compile(r"visibility|\[\s*private\s*\]|\bprivate\b", re.IGNORECASE)
_SECRET_WORD = re.compile(r"secret|password|passwd|credential|api[-_ ]?key|private[-_ ]?key|token|passphrase", re.IGNORECASE)


def audit(template_path):
    """(near-miss markers, unmarked secret-sounding outputs) in one template."""
    stack = template_path.name[: -len(".template.json")]
    try:
        template = json.loads(template_path.read_text())
    except (OSError, json.JSONDecodeError) as e:
        print(f"[ERROR] Could not read {template_path}: {e}")
        sys.exit(1)

    near_miss, unmarked, marked = [], [], 0
    for key, spec in (template.get("Outputs") or {}).items():
        description = spec.get("Description") or ""
        if PRIVATE_MARKER in description:
            marked += 1
            continue
        # A valid public marker contains the word "visibility", so take it out before looking for a botched private one.
        residual = description.replace(PUBLIC_MARKER, "")
        if _NEAR_MISS.search(residual):
            near_miss.append((stack, key, description))
            continue
        if PUBLIC_MARKER in description:
            continue
        if _SECRET_WORD.search(key):
            unmarked.append((stack, key, description))
    return near_miss, unmarked, marked


def main():
    if len(sys.argv) > 1:
        out_dirs = [Path(arg) for arg in sys.argv[1:]]
    else:
        out_dirs = sorted(_REPO_ROOT.glob("cdk.out.*")) + sorted((_REPO_ROOT / "cdk").glob("cdk.out.*"))

    templates = [t for d in out_dirs for t in sorted(d.glob("*.template.json"))]
    if not templates:
        print(f"[ERROR] No *.template.json found in: {', '.join(str(d) for d in out_dirs) or '(none)'}")
        print(f"[ERROR] Synthesize first, e.g. CDK_OUTDIR=cdk.out.rmng python3 cdk/apps/rmng.py")
        sys.exit(1)

    near_miss, unmarked, marked = [], [], 0
    for template in templates:
        t_near, t_unmarked, t_marked = audit(template)
        near_miss += t_near
        unmarked += t_unmarked
        marked += t_marked

    print(f"[INFO] Scanned {len(templates)} template(s): {marked} output(s) carry {PRIVATE_MARKER}.")

    if near_miss:
        print()
        print(f"[ERROR] {len(near_miss)} output description(s) look like a redaction marker but do not match.")
        print(f"[ERROR] These outputs ARE published to the public rmng-public-assets bucket.")
        print(f"[ERROR] Use the exact string {PRIVATE_MARKER} to redact, or reword to drop the hint:")
        for stack, key, description in near_miss:
            print(f'    {stack} > {key}: "{description}"')

    if unmarked:
        print()
        print(f"[ERROR] {len(unmarked)} output(s) named like a secret carry no visibility marker,")
        print(f"[ERROR] so they are published to the public rmng-public-assets bucket. Add {PRIVATE_MARKER}")
        print(f"[ERROR] to the description to redact, or {PUBLIC_MARKER} to record that it is safe to publish:")
        for stack, key, description in unmarked:
            print(f'    {stack} > {key}: "{description}"')

    if near_miss or unmarked:
        sys.exit(1)

    print(f"[INFO] No near-miss markers, no unmarked secret-sounding outputs.")


if __name__ == "__main__":
    main()
