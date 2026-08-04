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
    ADMIN_USERNAME      - Super admin username
    ADMIN_PASSWORD      - Super admin password
"""

import argparse
import json
import os
import sys

import boto3
import requests
from botocore.auth import SigV4Auth
from botocore.awsrequest import AWSRequest

from test.test_user import _admin_cognito_auth


def load_config(config_path):
    """Load API URLs and region from rmng-outputs.json config file."""
    with open(config_path) as f:
        data = json.load(f)

    base = data.get("rmng-base", {})
    esp = data.get("espuser-base", {})

    return {
        "api_gateway_url": base.get("ApiGatewayUrl", "").rstrip("/"),
        "user_api_gateway_url": esp.get("EspUserApiUrl", "").rstrip("/"),
        "region": base.get("StackRegion", "us-east-1"),
        "admin_client_id": esp.get("EspAdminUserPoolClientId", "")
                           or base.get("AdminUserPoolClientId", ""),
    }


def signin(region, admin_client_id, username, password):
    """Sign in as super admin against the admin Cognito pool and return the ID token.

    Admins have no admin-auth API; they authenticate against Cognito directly.
    """
    if not admin_client_id:
        print("ERROR: Admin Cognito client id missing from config (EspAdminUserPoolClientId)")
        sys.exit(1)

    resp = _admin_cognito_auth(
        region, admin_client_id, "USER_PASSWORD_AUTH",
        {"USERNAME": username, "PASSWORD": password},
    )
    if not resp.ok:
        print(f"ERROR: Admin sign-in failed: {resp.text}")
        sys.exit(1)

    id_token = resp.json().get("id_token")
    if not id_token:
        print("ERROR: No id_token in Cognito auth result.")
        sys.exit(1)

    print("Signed in successfully.")
    return id_token


def get_aws_credentials(api_url, id_token):
    """Exchange ID token for temporary AWS credentials via /v1/user/credentials."""
    url = f"{api_url}/v1/user/credentials"
    headers = {"Authorization": f"Bearer {id_token}"}

    resp = requests.post(url, headers=headers)
    if resp.status_code != 200:
        print(f"ERROR: Failed to get AWS credentials ({resp.status_code}): {resp.text}")
        sys.exit(1)

    creds = resp.json()
    for field in ("access_key_id", "secret_access_key", "session_token"):
        if field not in creds:
            print(f"ERROR: Missing '{field}' in credentials response.")
            sys.exit(1)

    print("Obtained AWS credentials.")
    return creds


def register_node(api_url, region, creds, cert_pem, ca_cert_pem=None,
                   thing_groups=None, tags=None):
    """Register a node certificate via POST /v1/admin/nodes with SigV4-signed request."""
    url = f"{api_url}/v1/admin/nodes"

    body = {"cert": cert_pem}
    if ca_cert_pem:
        body["ca_cert"] = ca_cert_pem
    if thing_groups:
        body["admin_group_names"] = thing_groups
    if tags:
        body["tags"] = tags

    json_body = json.dumps(body)

    # Create and sign the request with SigV4
    session = boto3.Session(
        aws_access_key_id=creds["access_key_id"],
        aws_secret_access_key=creds["secret_access_key"],
        aws_session_token=creds["session_token"],
        region_name=region,
    )

    aws_request = AWSRequest(method="POST", url=url, data=json_body)
    SigV4Auth(session.get_credentials(), "execute-api", region).add_auth(aws_request)
    prepared = aws_request.prepare()

    resp = requests.post(
        prepared.url,
        headers=dict(prepared.headers),
        data=json_body,
    )

    return resp


def main():
    parser = argparse.ArgumentParser(
        description="Register a node certificate via the admin API.",
    )
    parser.add_argument("--cert", required=True,
                        help="Path to the node certificate PEM file")
    parser.add_argument("--ca-cert",
                        help="Path to the CA certificate PEM file")
    parser.add_argument("--username", default=os.environ.get("ADMIN_USERNAME"),
                        help="Super admin username (or set ADMIN_USERNAME env var)")
    parser.add_argument("--password", default=os.environ.get("ADMIN_PASSWORD"),
                        help="Super admin password (or set ADMIN_PASSWORD env var)")
    parser.add_argument("--thing-groups", nargs="+",
                        help="IoT Thing Group names to add the node to")
    parser.add_argument("--tags", nargs="+",
                        help="Tags in key:value format (e.g. env:prod owner:admin)")
    parser.add_argument("--config", default="rmng-outputs.json",
                        help="Path to rmng-outputs.json config file (default: rmng-outputs.json)")

    args = parser.parse_args()

    # Load config
    if not os.path.isfile(args.config):
        print(f"ERROR: Config file not found: {args.config}")
        sys.exit(1)

    config = load_config(args.config)
    api_url = config["api_gateway_url"]
    user_api_url = config["user_api_gateway_url"]
    region = config["region"]

    if not api_url:
        print("ERROR: ApiGatewayUrl not found in config file")
        sys.exit(1)
    if not user_api_url:
        print("ERROR: EspUserApiUrl not found in config file")
        sys.exit(1)

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

    # Authenticate
    id_token = signin(region, config["admin_client_id"], args.username, args.password)
    aws_creds = get_aws_credentials(api_url, id_token)

    # Register node
    print("Registering node certificate...")
    resp = register_node(
        api_url=api_url,
        region=region,
        creds=aws_creds,
        cert_pem=cert_pem,
        ca_cert_pem=ca_cert_pem,
        thing_groups=args.thing_groups,
        tags=args.tags,
    )

    if resp.status_code in (200, 201):
        result = resp.json()
        print("Node registered successfully.")
        print(f"  Node ID: {result.get('node_id', 'N/A')}")
        print(f"  Status:  {result.get('status', 'N/A')}")
    else:
        print(f"ERROR: Registration failed ({resp.status_code}): {resp.text}")
        sys.exit(1)


if __name__ == "__main__":
    main()
