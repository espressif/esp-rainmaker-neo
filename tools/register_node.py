#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Standalone script to register node certificates via /v1/admin/nodes API.

Usage:
    python tools/register_node.py --cert /path/to/cert.pem --username admin@example.com --password 'password'
    python tools/register_node.py --cert /path/to/cert.pem --ca-cert /path/to/ca.pem --thing-groups "GroupA" "GroupB"
    python tools/register_node.py --cert /path/to/cert.pem --tags "env:prod" "owner:admin"
    python tools/register_node.py --cert /path/to/cert.pem --config /path/to/rmng-outputs.json

Environment variables (optional):
    ADMIN_USERNAME      - Admin username
    ADMIN_PASSWORD      - Admin password
"""

import argparse
import os
import sys
from pathlib import Path
from types import SimpleNamespace

# Run as a script, so only tools/ is on sys.path; adding the repo root makes py_sdk and scripts importable from any directory.
REPO_ROOT = Path(__file__).resolve().parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from py_sdk.test_user import User  # noqa: E402
from scripts.rmng_outputs import RmngSettings  # noqa: E402


def signin(settings, username, password):
    """An admin User holding AWS credentials, signed in against the admin Cognito pool."""
    if not settings.admin_client_id:
        print("ERROR: Admin Cognito client id missing from config (EspAdminUserPoolClientId)")
        sys.exit(1)

    user = User.from_settings(settings, username, password, is_admin=True)
    if not user.get_aws_credentials():
        print("ERROR: Admin sign-in failed (see preceding log lines)")
        sys.exit(1)

    print("Signed in successfully.")
    return user


def main():
    parser = argparse.ArgumentParser(
        description="Register a node certificate via the admin API.",
    )
    parser.add_argument("--cert", required=True,
                        help="Path to the node certificate PEM file")
    parser.add_argument("--ca-cert",
                        help="Path to the CA certificate PEM file")
    parser.add_argument("--username", default=os.environ.get("ADMIN_USERNAME"),
                        help="Admin username (or set ADMIN_USERNAME env var)")
    parser.add_argument("--password", default=os.environ.get("ADMIN_PASSWORD"),
                        help="Admin password (or set ADMIN_PASSWORD env var)")
    parser.add_argument("--thing-groups", nargs="+",
                        help="IoT Thing Group names to add the node to")
    parser.add_argument("--tags", nargs="+",
                        help="Tags in key:value format (e.g. env:prod owner:admin)")
    parser.add_argument("--config",
                        help="Path to rmng-outputs.json config file (default: the one at the repo root)")

    args = parser.parse_args()

    try:
        settings = RmngSettings.from_source(os.path.abspath(args.config) if args.config else None)
    except FileNotFoundError as e:
        sys.exit(f"ERROR: {e.filename} not found. Run `make deploy` first.")

    if not args.username or not args.password:
        print("ERROR: --username and --password required (or set ADMIN_USERNAME / ADMIN_PASSWORD)")
        sys.exit(1)

    # Read certificate files
    if not os.path.isfile(args.cert):
        print(f"ERROR: Certificate file not found: {args.cert}")
        sys.exit(1)
    with open(args.cert) as f:
        cert_pem = f.read().strip()

    ca_cert_pem = None
    if args.ca_cert:
        if not os.path.isfile(args.ca_cert):
            print(f"ERROR: CA certificate file not found: {args.ca_cert}")
            sys.exit(1)
        with open(args.ca_cert) as f:
            ca_cert_pem = f.read().strip()

    user = signin(settings, args.username, args.password)

    print("Registering node certificate...")
    device = SimpleNamespace(node_thing_name=os.path.basename(args.cert), node_cert=cert_pem, node_ca_cert=ca_cert_pem)
    if not user.register_node(device, tags=args.tags, admin_group_names=args.thing_groups):
        print("ERROR: Registration failed (see preceding log lines)")
        sys.exit(1)


if __name__ == "__main__":
    main()
