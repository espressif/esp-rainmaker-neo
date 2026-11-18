# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Test script to verify admin user's ability to call AWS IoT ListThings REST API.

Usage:
    source .venv/bin/activate
    python3 test/helpers/test_admin_list_things.py
"""
import json
import sys
from pathlib import Path

# Run as a script, so only test/helpers/ is on sys.path; adding the repo root makes py_sdk and scripts importable from any directory.
REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from py_sdk.test_user import User  # noqa: E402
from scripts.rmng_outputs import TEST_CONFIG_PATH, RmngSettings  # noqa: E402


def load_admin_user():
    """The first admin: true entry in test_config.json."""
    try:
        with open(TEST_CONFIG_PATH) as f:
            config = json.load(f)
    except FileNotFoundError:
        sys.exit(f"ERROR: {TEST_CONFIG_PATH} not found. Run `esp-morpheus admin test-data setup` first.")

    admin_user = next((u for u in config.get('users', []) if u.get('admin', False)), None)
    if not admin_user:
        sys.exit(f"ERROR: No admin user found in {TEST_CONFIG_PATH}")
    return admin_user


def signin_admin(settings, admin_user):
    """An admin User holding AWS credentials from POST /v1/user/credentials."""
    print(f"Signing in as admin: {admin_user['name']}")
    user = User.from_settings(settings, admin_user['name'], admin_user['password'], is_admin=True)
    if not user.get_aws_credentials():
        sys.exit("ERROR: Admin sign-in failed (see preceding log lines)")
    print(f"Got AWS credentials (AccessKeyId: {user.credentials['AccessKeyId'][:10]}...)")
    return user


def call_list_things(user):
    """Call AWS IoT ListThings using the IoT control plane API."""
    print("\nAttempting ListThings via IoT control plane API...")

    try:
        iot_client = user.boto3_session.client('iot')
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

    try:
        settings = RmngSettings.from_source()
    except FileNotFoundError as e:
        sys.exit(f"ERROR: {e.filename} not found. Run `make deploy` first.")
    user = signin_admin(settings, load_admin_user())
    success = call_list_things(user)

    print("\n" + "=" * 60)
    if success:
        print("RESULT: Admin user CAN call ListThings")
    else:
        print("RESULT: Admin user CANNOT call ListThings")
    print("=" * 60)

    return 0 if success else 1


if __name__ == "__main__":
    sys.exit(main())
