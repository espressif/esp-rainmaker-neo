# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Test script to verify admin user's ability to call AWS IoT ListThings REST API.

Usage:
    source .venv/bin/activate
    python3 test_admin_list_things.py
"""
import json
import sys
import os
import requests
from botocore.auth import SigV4Auth
from botocore.awsrequest import AWSRequest
import boto3

from scripts.rmng_outputs import RmngSettings
from test.test_user import _admin_cognito_auth

_settings = RmngSettings.from_source()

IOT_ENDPOINT = _settings.iot_endpoint
REGION = _settings.region
API_GATEWAY_URL = _settings.api_gateway_url
IDENTITY_POOL_ID = _settings.identity_pool_id
ADMIN_USER_POOL_ID = _settings.admin_user_pool_id
ADMIN_USER_POOL_CLIENT_ID = _settings.admin_client_id
USER_API_GATEWAY_URL = esp_user_base_outputs.get('EspUserApiUrl', '')

# Read test config
with open('test_config.json', 'r') as f:
    config = json.load(f)

# Find admin user
admin_user = None
for user in config.get('users', []):
    if user.get('super_admin', False):
        admin_user = user
        break

if not admin_user:
    print("ERROR: No super_admin user found in test_config.json")
    sys.exit(1)

USERNAME = admin_user['name']
PASSWORD = admin_user['password']


def signin_admin():
    print(f"Signing in as admin: {USERNAME}")
    resp = _admin_cognito_auth(
        REGION, ADMIN_USER_POOL_CLIENT_ID, "USER_PASSWORD_AUTH",
        {"USERNAME": USERNAME, "PASSWORD": PASSWORD},
    )
    if not resp.ok:
        print(f"ERROR: Signin failed: {resp.text}")
        sys.exit(1)
    return resp.json().get("id_token")


def get_credentials(id_token):
    """Get AWS credentials via /user/creds endpoint."""
    url = f"{API_GATEWAY_URL}/v1/user/credentials"
    headers = {"Authorization": f"Bearer {id_token}"}
    print(f"Getting credentials from: {url}")
    response = requests.get(url, headers=headers)
    if response.status_code != 200:
        print(f"ERROR: Failed to get credentials: {response.status_code} {response.text}")
        sys.exit(1)
    creds = response.json()
    print(f"Got AWS credentials (AccessKeyId: {creds['access_key_id'][:10]}...)")
    return creds


def call_list_things(creds):
    """Call AWS IoT ListThings using the IoT control plane API."""
    print(f"\nAttempting ListThings via IoT control plane API...")

    session = boto3.Session(
        aws_access_key_id=creds['access_key_id'],
        aws_secret_access_key=creds['secret_access_key'],
        aws_session_token=creds['session_token'],
        region_name=REGION
    )

    try:
        iot_client = session.client('iot')
        response = iot_client.list_things()
        things = response.get('things', [])
        print(f"SUCCESS: ListThings returned {len(things)} things")
        for thing in things:
            print(f"  - {thing.get('thingName', 'unknown')}")
        return True
    except Exception as e:
        print(f"FAILED: {e}")
        return False


def main():
    print("=" * 60)
    print("Admin User ListThings Test")
    print("=" * 60)

    id_token = signin_admin()
    creds = get_credentials(id_token)
    success = call_list_things(creds)

    print("\n" + "=" * 60)
    if success:
        print("RESULT: Admin user CAN call ListThings")
    else:
        print("RESULT: Admin user CANNOT call ListThings")
    print("=" * 60)

    return 0 if success else 1


if __name__ == "__main__":
    sys.exit(main())
