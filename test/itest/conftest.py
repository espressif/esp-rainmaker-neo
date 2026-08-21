# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Shared fixtures, helpers, and configuration for integration tests.

Run all tests: pytest test/itest/ -v -s
Run a specific test: pytest test/itest/ -v -s -k "test_name"
If some tests start failing due to mqtt connections, try running: pytest test/itest/ -v -s -m "not unsafe"
"""
import pytest
import json
import requests
from urllib.parse import urlparse
from scripts.rmng_outputs import find_outputs
from scripts.rmng_outputs import load as load_rmng_outputs
from py_sdk.test_user import User, user_log

from py_sdk.test_device import Device, generate_key_and_cert, split_combined_cert_pem, validate_tags
from py_sdk.test_group import Group
from test.itest.config_sources import describe_sources, load_json_config, repo_path
from test.itest.email_utils import (
    ITEST_CONFIG_ENV_VAR,
    ITEST_CONFIG_REL_PATH,
    generate_mailosaur_email,
    generate_random_email,
    generate_test_password,
)
from py_sdk.test_matter import (
    build_nocsr_elements_tlv,
    sign_attestation_data,
    do_initiate,
    do_verify_with_nocsr_elements,
    do_confirm,
    do_matter_dev_assoc,
)
from cryptography.hazmat.primitives.asymmetric import ec, rsa
from cryptography.hazmat.primitives import serialization, hashes
from cryptography import x509
import uuid
import time
from queue import Empty

import boto3
import subprocess
import sys
import os
import csv
import datetime
import tempfile
import random
import threading


# Read configuration from rmng-outputs.json (merged CDK outputs)
rmng_outputs = load_rmng_outputs()

# The Alexa stack is deployed once per Alexa smart-home region; each publishes its own
# AlexaSkillFunctionArn. Order here is the fixture's parameter order.
ALEXA_STACK_REGIONS = ['us-east-1', 'eu-west-1', 'us-west-2']

# Extract ESP User stack outputs from merged file
esp_user_base_outputs = rmng_outputs.get('espuser-base', {})
esp_user_core_outputs = rmng_outputs.get('espuser-core', {})
rmng_base_outputs = rmng_outputs.get('rmng-base', {})
rmng_core_outputs = rmng_outputs.get('rmng-core', {})

# Read RMNG stack outputs
IDENTITY_POOL_ID = rmng_base_outputs['IdentityPoolId']
API_GATEWAY_URL = rmng_base_outputs['ApiGatewayUrl']
IOT_ENDPOINT = rmng_base_outputs['IoTEndpointUrl']
REGION = rmng_base_outputs['StackRegion']
boto3.setup_default_session(region_name=REGION)

IOT_USER_ROLE_ARN = rmng_base_outputs['IoTUserRoleArn']
CREDENTIAL_PROVIDER_ENDPOINT = rmng_base_outputs['CredentialProviderEndpoint']
# The stack emits the aliases as a comma-separated list (newest last); the
# credential-provider tests authenticate with the current alias, i.e. the last.
DEVICE_FILE_ROLE_ALIAS = rmng_base_outputs['NodeFileRoleAliases'].split(',')[-1]
DEVICE_VIDEO_ROLE_ALIAS = rmng_base_outputs['NodeVideoRoleAliases'].split(',')[-1]
FILES_BUCKET_NAME = rmng_base_outputs['FilesBucketName']


def pkce_pair():
    """Return (verifier, S256 challenge) for a PKCE code exchange (RFC 7636)."""
    import base64
    import hashlib
    import secrets
    verifier = base64.urlsafe_b64encode(secrets.token_bytes(32)).rstrip(b"=").decode()
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    return verifier, challenge




def cognito_hosted_login(session, hosted_authorize_url, email, password):
    """Script the Cognito hosted-UI password login: follow to the login form, POST the
    credentials with the CSRF token, and return the redirect back to our federation callback."""
    page = session.get(hosted_authorize_url, allow_redirects=True)
    assert page.status_code == 200, f"hosted UI login page failed: {page.status_code}"
    csrf = session.cookies.get("XSRF-TOKEN")
    assert csrf, "hosted UI did not set the XSRF-TOKEN cookie"
    login = session.post(page.url, data={"_csrf": csrf, "username": email, "password": password},
                         allow_redirects=False)
    assert login.status_code == 302, \
        f"hosted UI login failed for {email}: {login.status_code} {login.text[:300]}"
    return login.headers["Location"]


def flow_id_from_cookie(response):
    """Extract the esp_flow_id the authorize 302 set in a cookie."""
    return response.cookies.get("esp_flow_id")


def _get_alexa_region_arns():
    """Return list of (aws_region, arn) for the Alexa skill lambdas.

    Keyed off the region inside each ARN rather than the outputs layout, so this reads both the
    flat and the per-region-nested shapes and does not assume the Alexa stacks are published under
    RMNG's own region."""
    by_region = {}
    for arn in find_outputs(rmng_outputs, "AlexaSkillFunctionArn"):
        parts = arn.split(":")
        if len(parts) > 3:
            by_region[parts[3]] = arn
    return [(r, by_region[r]) for r in ALEXA_STACK_REGIONS if r in by_region]


ALEXA_REGION_ARNS = _get_alexa_region_arns()


@pytest.fixture(
    params=ALEXA_REGION_ARNS if ALEXA_REGION_ARNS else [
        pytest.param((None, None), marks=pytest.mark.skip(reason="No rmng-alexa-core regions in rmng-outputs.json"))
    ],
    ids=[r for r, _ in ALEXA_REGION_ARNS] if ALEXA_REGION_ARNS else ["no-alexa"],
)
def alexa_region_arn(request):
    """Parametrized fixture: yields (region, arn) for each Alexa skill Lambda region."""
    return request.param


# HTTP API Gateway URL for MCP + OAuth endpoints (no /prod stage prefix)
MCP_API_URL = rmng_core_outputs.get('McpHttpApiUrl', API_GATEWAY_URL).rstrip('/')

# End users are passwordless (OIDC + email OTP) and have no Cognito pool, so there is
# End users are passwordless OIDC (no Cognito pool/client). Admin auth still uses the admin Cognito pool.
END_USER_POOL_ID = esp_user_base_outputs.get('EspEndUserPoolId', '')
ADMIN_USER_POOL_ID = esp_user_base_outputs.get('EspAdminUserPoolId') or rmng_base_outputs.get('AdminUserPoolId', '')
ADMIN_CLIENT_ID = esp_user_base_outputs.get('EspAdminUserPoolClientId') or rmng_base_outputs.get('AdminUserPoolClientId', '')
USER_API_GATEWAY_URL = esp_user_base_outputs.get('EspUserApiUrl', '')

# Hardcoded values (previously from test_config.json)
CA_CERT = """-----BEGIN CERTIFICATE-----
MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
ADA5MQswCQYDVQQGEwJVUzEPMA0GA1UEChMGQW1hem9uMRkwFwYDVQQDExBBbWF6
b24gUm9vdCBDQSAxMB4XDTE1MDUyNjAwMDAwMFoXDTM4MDExNzAwMDAwMFowOTEL
MAkGA1UEBhMCVVMxDzANBgNVBAoTBkFtYXpvbjEZMBcGA1UEAxMQQW1hem9uIFJv
b3QgQ0EgMTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALJ4gHHKeNXj
ca9HgFB0fW7Y14h29Jlo91ghYPl0hAEvrAIthtOgQ3pOsqTQNroBvo3bSMgHFzZM
9O6II8c+6zf1tRn4SWiw3te5djgdYZ6k/oI2peVKVuRF4fn9tBb6dNqcmzU5L/qw
IFAGbHrQgLKm+a/sRxmPUDgH3KKHOVj4utWp+UhnMJbulHheb4mjUcAwhmahRWa6
VOujw5H5SNz/0egwLX0tdHA114gk957EWW67c4cX8jJGKLhD+rcdqsq08p8kDi1L
93FcXmn/6pUCyziKrlA4b9v7LWIbxcceVOF34GfID5yHI9Y/QCB/IIDEgEw+OyQm
jgSubJrIqg0CAwEAAaNCMEAwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMC
AYYwHQYDVR0OBBYEFIQYzIU07LwMlJQuCFmcx7IQTgoIMA0GCSqGSIb3DQEBCwUA
A4IBAQCY8jdaQZChGsV2USggNiMOruYou6r4lK5IpDB/G/wkjUu0yKGX9rbxenDI
U5PMCCjjmCXPI6T53iHTfIUJrU6adTrCC2qJeHZERxhlbI1Bjjt/msv0tadQ1wUs
N+gDS63pYaACbvXy8MWy7Vu33PqUXHeeE6V/Uq2V8viTO96LXFvKWlJbYK8U90vv
o/ufQJVtMVT8QtPHRh8jrdkPSHCa2XV4cdFyQzR1bldZwgJcJmApzyMZFo6IQ6XU
5MsI+yMRQ+hDKXJioaldXgjUkK642M4UwtBV8ob2xJNDd2ZhwLnoQdeXeGADbkpy
rqXRfboQnoZsG4q5WTP468SQvvG5
-----END CERTIFICATE-----"""
DEBUG = False


# ============================================================================
# Graceful-skip guards for tests that need external credentials.
#
# Each guard turns "creds not configured" into a pytest.skip (not an error), so
# the suite runs cleanly on a fresh checkout. Depend on the fixture from a test
# (or from another fixture, e.g. the user-pool fixtures below) to gate it.
# ============================================================================

# ============================================================================
# Push-notification credentials (APNs / Firebase) for the itest suite.
#
# Resolved by config_sources: this repo's test/test_data/notify_data.json, else the
# RMNG_NOTIFY_DATA_JSON env blob (CI — one masked variable instead of many), else the
# superproject's copy at ../test/test_data/. Each fixture below SKIPS the test when its
# creds are absent AND returns the value, so a test just declares the fixture it needs
# and uses its value directly.
# ============================================================================

NOTIFY_DATA_REL_PATH = "test_data/notify_data.json"
NOTIFY_DATA_ENV_VAR = "RMNG_NOTIFY_DATA_JSON"


def pytest_configure(config):
    """Materialize the JSON-blob env vars to their gitignored files if the files don't exist.

    CI supplies creds as one masked variable each (RMNG_NOTIFY_DATA_JSON / RMNG_ITEST_CONFIG_JSON); this writes
    them to disk once at startup so every xdist worker (and email_utils) reads a single source of truth.
    An existing local file is never overwritten — fill the file directly to override the env blob. Nothing is
    written when only the superproject supplies the blob; the loaders read that copy in place.
    """
    for env_var, rel_path in (
        (NOTIFY_DATA_ENV_VAR, NOTIFY_DATA_REL_PATH),
        (ITEST_CONFIG_ENV_VAR, ITEST_CONFIG_REL_PATH),
    ):
        path = repo_path(rel_path)
        blob = os.environ.get(env_var)
        if blob and not os.path.exists(path):
            try:
                parsed = json.loads(blob)
            except json.JSONDecodeError:
                continue
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w") as f:
                json.dump(parsed, f, indent=2)


def _notify_data() -> dict:
    return load_json_config(NOTIFY_DATA_REL_PATH, NOTIFY_DATA_ENV_VAR)


def _apns_credentials():
    apns = _notify_data().get("apns") or {}
    if all(apns.get(k) for k in ("key", "key_id", "team_id", "bundle_id")):
        return {k: apns[k] for k in ("key", "key_id", "team_id", "bundle_id")}
    return None


def _firebase_service_account():
    sa = (_notify_data().get("firebase") or {}).get("service_account")
    if isinstance(sa, dict) and sa.get("private_key") and sa.get("client_email"):
        return sa
    return None


_ITEST_CONFIG_SOURCES = describe_sources(ITEST_CONFIG_REL_PATH, ITEST_CONFIG_ENV_VAR)
_NOTIFY_DATA_SOURCES = describe_sources(NOTIFY_DATA_REL_PATH, NOTIFY_DATA_ENV_VAR)


@pytest.fixture
def require_mailosaur():
    """Skip when Mailosaur email credentials are not configured."""
    from test.itest.email_utils import _get_mailosaur_credentials
    if _get_mailosaur_credentials() is None:
        pytest.skip(f"Mailosaur credentials not configured ({_ITEST_CONFIG_SOURCES})")


@pytest.fixture
def apns_credentials() -> dict:
    """APNs platform creds {key, key_id, team_id, bundle_id}; skips the test if not fully configured."""
    creds = _apns_credentials()
    if creds is None:
        pytest.skip(f"APNs credentials not configured (apns in {_NOTIFY_DATA_SOURCES})")
    return creds


@pytest.fixture
def apns_device_token() -> str:
    """APNs device token for end-to-end push; skips the test if absent."""
    token = (_notify_data().get("apns") or {}).get("device_token")
    if not token:
        pytest.skip(f"APNs device token not configured (apns.device_token in {_NOTIFY_DATA_SOURCES})")
    return token


@pytest.fixture
def firebase_service_account() -> dict:
    """Firebase (FCM) service-account dict; skips the test if absent."""
    sa = _firebase_service_account()
    if sa is None:
        pytest.skip(f"Firebase service account not configured (firebase.service_account in {_NOTIFY_DATA_SOURCES})")
    return sa


@pytest.fixture
def firebase_device_token() -> str:
    """Firebase (FCM) device token for end-to-end push; skips the test if absent."""
    token = (_notify_data().get("firebase") or {}).get("device_token")
    if not token:
        pytest.skip(f"Firebase device token not configured (firebase.device_token in {_NOTIFY_DATA_SOURCES})")
    return token


# ============================================================================
# Thread-safe resource pools for users and devices
# ============================================================================

class ResourcePool:
    def __init__(self, initializer, resetter=None, deinitializer=None):
        self._initializer = initializer
        self._resetter = resetter
        self._deinitializer = deinitializer
        self._lock = threading.Lock()
        self._free = []

    def acquire(self):
        with self._lock:
            if self._free:
                return self._free.pop()
        # Initialize outside lock to avoid long holds
        resource = self._initializer()
        return resource

    def release(self, resource):
        if self._resetter:
            try:
                self._resetter(resource)
            except Exception as e:
                print(f"Warning resetting pooled resource: {e}")
                return  # Don't return corrupted resource to pool
        with self._lock:
            self._free.append(resource)

    def drain_free(self):
        items = []
        try:
            with self._lock:
                while self._free:
                    items.append(self._free.pop())
        except Exception:
            items = []
        for item in items:
            try:
                if self._deinitializer:
                    self._deinitializer(item)
                elif self._resetter:
                    self._resetter(item)
            except Exception as e:
                print(f"Warning deinitializing pooled resource: {e}")


def _init_user():
    """Initialize a regular end user, provisioned via Cognito (AdminCreateUser) and signed in
    through the Native /v1/user/auth/token converting API. This needs no SES/OTP —
    a unique non-Mailosaur email suffices. OTP-specific tests use their own SES-verified fixture.
    Tries to authenticate an existing user first, only provisioning a new one if that fails.
    """
    email = generate_random_email()

    user = User(email, generate_test_password(), REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT, end_user_pool_id=END_USER_POOL_ID)
    user.mailosaur_email = email

    # Try to authenticate first - if successful, user already exists and we can reuse it
    try:
        response = user.signin()
        if response.status_code == 200:
            # Verify that tokens were actually set - if not, the user might exist but be in a bad state
            if user.token:
                # User exists and authentication succeeded - reuse it
                print(f"[User] Reusing existing user: {email}")
                # Still need to register client and get credentials
                try:
                    user.register_client("ios-dummy", "ios-user-device-token")
                except Exception as e:
                    # Client might already be registered, that's okay
                    print(f"[User] Note: Client registration: {e}")
                # Get credentials - if this fails, we'll fall through to create a new user
                creds = user.get_aws_credentials()
                if creds:
                    return user
                else:
                    print(f"[User] Failed to get credentials for existing user, will create new user")
            else:
                print(f"[User] Signin succeeded but no token received, will create new user")
    except Exception as e:
        print(f"[User] Authentication failed (user may not exist): {e}")

    # User doesn't exist yet - provision via the passwordless email OTP login, which
    # JIT-creates the user on first verify. register_user_via_lambda reads the OTP code
    # from this user's Mailosaur inbox (user.mailosaur_email set above).
    print(f"[User] Creating new user: {email}")
    user.register_user_via_lambda(email=user.username, password=user.password)
    creds = user.get_aws_credentials()
    if not creds:
        raise Exception("Failed to obtain AWS credentials after registration. User may need to authenticate first.")
    user.register_client("ios-dummy", "ios-user-device-token")
    return user



def _reset_user(user):
    # Clean up groups created by this user between tests
    try:
        delete_user_groups(user)
    except Exception as e:
        print(f"Warning cleaning user groups: {e}")
    # Best-effort disconnect MQTT if connected
    try:
        user.mqtt_disconnect_and_wait()
    except Exception:
        pass

    # Delete Mailosaur messages for this user
    try:
        from test.itest.email_utils import delete_mailosaur_messages_for_email
        deleted_count = delete_mailosaur_messages_for_email(
            user.mailosaur_email
        )
        if deleted_count > 0:
            print(f"Cleaned up {deleted_count} Mailosaur message(s) for {user.mailosaur_email}")
    except Exception as e:
        print(f"Warning: Could not clean up Mailosaur messages for {user.mailosaur_email}: {e}")


def _init_admin_user():
    """Initialize an admin user with a unique email address.
    Tries to authenticate with existing admin user first, only creates new user if authentication fails.

    The backend lambda force-creates and confirms the user in Cognito, so no emailed
    verification code is read here — a unique (non-Mailosaur) email is all that is needed.
    """
    email = generate_random_email()

    password = generate_test_password()
    user = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL, IOT_ENDPOINT, admin_user_pool_id=ADMIN_USER_POOL_ID, admin_client_id=ADMIN_CLIENT_ID, is_super_admin=True)
    user.mailosaur_email = email

    # Try to authenticate first - if successful, admin user already exists and we can reuse it
    try:
        response = user.signin(is_admin=True)
        if response.status_code == 200:
            # Verify that tokens were actually set - if not, the user might exist but be in a bad state
            if user.token:
                # Admin user exists and authentication succeeded - reuse it
                print(f"[User] Reusing existing admin user: {email}")
                # Still need to register client and get credentials
                try:
                    user.register_client("ios-dummy", "ios-user-device-token")
                except Exception as e:
                    # Client might already be registered, that's okay
                    print(f"[User] Note: Client registration: {e}")
                # Get credentials - if this fails, we'll fall through to create a new user
                creds = user.get_aws_credentials()
                if creds:
                    return user
                else:
                    print(f"[User] Failed to get credentials for existing admin user, will create new user")
            else:
                print(f"[User] Admin signin succeeded but no token received, will create new user")
    except Exception as e:
        print(f"[User] Admin authentication failed (user may not exist): {e}")

    # Admin user doesn't exist or authentication failed - create new admin user.
    # Admins are provisioned directly in the admin Cognito pool (no admin-auth API, no DB record).
    print(f"[User] Creating new admin user: {email}")
    user.create_super_admin_via_cognito(email=email, password=password)
    user.register_client("ios-dummy", "ios-user-device-token")
    user.get_aws_credentials()
    return user


_node_registrar_provider = None


def node_registrar_identity():
    """CognitoAuthenticationProvider string of a real superadmin, for direct-invoke node
    registration. The admin-node-reg handler resolves its caller from this identity and enforces
    the superadmin gate (the main-era "unknown-user" backdoor — any identity-less direct invoke
    became a superadmin — is gone). Provisions a fixed superadmin in the admin pool once per
    session (idempotent across sessions: creation tolerates UsernameExists) and caches the string.
    """
    global _node_registrar_provider
    if _node_registrar_provider:
        return _node_registrar_provider

    email = "node-registrar-itest@example.com"
    # password=False: this identity is only ever a provider string for a direct invoke, so it must not
    # have a password that could be used to sign in. Its user_id is pinned rather than derived, so
    # nodes registered by earlier runs keep resolving to the same caller.
    provisioner = User(email, "", REGION, IDENTITY_POOL_ID, API_GATEWAY_URL, USER_API_GATEWAY_URL,
                       IOT_ENDPOINT, admin_user_pool_id=ADMIN_USER_POOL_ID, admin_client_id=ADMIN_CLIENT_ID)
    assert provisioner.create_super_admin_via_cognito(
        email=email, password=False, user_id="node-registrar-itest",
    ), "failed to provision the node-registrar identity"

    cognito = boto3.client("cognito-idp", region_name=REGION)
    username = cognito.admin_get_user(UserPoolId=ADMIN_USER_POOL_ID, Username=email)["Username"]
    pool = f"cognito-idp.{REGION}.amazonaws.com/{ADMIN_USER_POOL_ID}"
    _node_registrar_provider = f"{pool},{pool}:CognitoSignIn:{username}"
    return _node_registrar_provider


def _init_device(key_type):
    thing_name = f"test-{key_type}-device-{uuid.uuid4()}"
    private_key_pem, cert_pem = generate_key_and_cert(thing_name, key_type)
    device = Device(thing_name, private_key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    device.register_test_node(caller_identity=node_registrar_identity())
    return device


@pytest.fixture
def bare_device():
    """Register bare, un-associated test device(s) with guaranteed teardown.

    A "bare" device is registered but NOT associated with any user/group and
    has no capabilities by default — the opposite of `associated_device`. This
    is exactly what the negative/isolation tests need: a node whose group_id
    IoT attribute is absent and whose cert has no capability policies attached.

    Returns a callable so a single test can make several bare devices (e.g. the
    multi-group shadow-access test needs three). Every device it creates is
    destroyed in post-yield teardown — which pytest runs on pass, failure, AND
    error — so an inline-registered node can never leak into the node count.
    """
    created = []

    def _make(thing_name=None, key_type="rsa", capabilities=None, admin_group_names=None):
        if thing_name is None:
            thing_name = f"test-{key_type}-device-{uuid.uuid4()}"
        private_key_pem, cert_pem = generate_key_and_cert(thing_name, key_type)
        device = Device(thing_name, private_key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
        device.register_test_node(admin_group_names=admin_group_names, capabilities=capabilities, caller_identity=node_registrar_identity())
        created.append(device)
        return device

    yield _make

    for device in created:
        _destroy_device(device)


def _reset_device(device):
    # Ensure disconnected and clear transient state
    try:
        device.disconnect()
    except Exception:
        pass
    try:
        device.group_id = None
        device.subgroup_ids = None
    except Exception:
        pass

    # Drain message queues
    device.clear_queues()

    # Clear callbacks
    device.clear_callbacks()

def _destroy_device(device):
    try:
        device.disconnect()
    except Exception:
        pass
    try:
        device.destroy_test_node()
    except Exception:
        pass

def associate_device_with_group(device, user, user1_group_api,
                                group_name="Test Associated Group", node_config=None):
    """
    Connect a device, associate it with a group, and configure it.

    Uses retry logic to handle policy propagation delays after device registration.
    Pass `group_name` when the caller needs a unique group (e.g. two isolated
    tenants in one test) and `node_config` when the test drives specific params.
    Returns the created group_id.
    """

    # Use retry mechanism to handle policy propagation delays after device registration
    assert connect_device_with_retry(device, max_retries=3, base_delay=2), "Failed to connect the device after retries"

    # Clear queues and callbacks (to ensure clear state)
    device.clear_queues()
    device.clear_callbacks()

    # Create group and associate device
    group_id = user1_group_api.create_group(group_name)
    result = user.do_user_node_assoc(device, group_id)
    assert result is None, f"Association failed with error: {result}"
    # Validate group info propagation
    assert device.wait_for_group_info(), "Device failed to receive group info"
    assert device.group_id == group_id, f"Expected group ID {group_id}, but got {device.group_id}"
    device.set_node_config(node_config if node_config is not None else {
        "device_type": "light_bulb",
        "firmware_version": "1.0"
    })
    return group_id

def _init_associated_device():
    device = device_rsa_pool.acquire()
    user = user_pool.acquire()
    user1_group_api = Group(user)
    group_id = associate_device_with_group(device, user, user1_group_api)
    return [device, group_id, user, user1_group_api]


def _reset_associated_device(resource):
    try:
        device, group_id, user, user1_group_api = resource
    except Exception:
        return
    try:
        device.disconnect()
    except Exception:
        pass
    try:
        user.mqtt_disconnect_and_wait()
    except Exception:
        pass

    # Delete the old group
    # We do this to delete the dirty state of associated group from previous tests
    # TODO: In future implementations,we should keep the same group and perform deletion on granular level (like delete subgroups, delete other users, delete triggers, etc)
    try:
        user1_group_api.delete_group(group_id, warn_error=True)
    except Exception as e:
        print(f"Warning: failed to delete group {group_id} during reset: {e}")

    # Rebuild the environment
    try:
        new_group_id = associate_device_with_group(device, user, user1_group_api)
        resource[1] = new_group_id
    except Exception as e:
        raise RuntimeError(f"Reset failed: could not rebuild associated device environment: {e}")

    # We never return the user and device to their original device/user pools

def _destroy_associated(resource):
    try:
        device, group_id, user, user1_group_api = resource
    except Exception:
        return
    try:
        device.disconnect()
    except Exception:
        pass
    try:
        device.destroy_test_node()
    except Exception:
        pass
    try:
        user1_group_api.delete_group(group_id, warn_error=True)
    except Exception:
        pass
    try:
        _reset_user(user)
    except Exception:
        pass


def _destroy_user(user):
    """Session-end destructor: reset first, then remove the account itself.

    Distinct from _reset_user, which runs between tests and must leave the user usable. Without this
    every run leaves its provisioned users behind in Cognito and in espuser-user-details.
    """
    _reset_user(user)
    try:
        user.delete_user_by_email(user.username)
    except Exception as e:  # noqa: BLE001
        print(f"Warning destroying pooled user: {e}")


user_pool = ResourcePool(_init_user, _reset_user, _destroy_user)
admin_user_pool = ResourcePool(_init_admin_user, _reset_user, _destroy_user)
device_ec_pool = ResourcePool(lambda: _init_device('ec'), _reset_device, _destroy_device)
device_rsa_pool = ResourcePool(lambda: _init_device('rsa'), _reset_device, _destroy_device)
associated_device_pool = ResourcePool(_init_associated_device, _reset_associated_device, _destroy_associated)


@pytest.fixture(scope="session", autouse=True)
def _drain_pools_at_session_end():
    """
    Session-end destructor that drains each ResourcePool's free list and performs
    a stronger cleanup for pooled objects.
    - Users: run reset cleanup (disconnect MQTT, delete created groups)
    - Devices: disconnect and destroy the IoT thing/certs
    - Associated tuples: destroy device, delete the created group, reset the user
    """
    yield

    try:
        user_pool.drain_free()
    except Exception:
        pass
    try:
        admin_user_pool.drain_free()
    except Exception:
        pass
    try:
        device_ec_pool.drain_free()
    except Exception:
        pass
    try:
        device_rsa_pool.drain_free()
    except Exception:
        pass
    try:
        associated_device_pool.drain_free()
    except Exception:
        pass

def delete_user_groups(user):
    # Ensure fresh credentials before listing groups
    user.get_aws_credentials()

    group_api = Group(user)
    groups = group_api.list_groups()
    for group in groups.get('groups', []):
        print("Deleting group", group['group_id'])
        group_api.delete_group(group['group_id'], warn_error=True)

@pytest.fixture
def session_valid_device_ec():
    device = device_ec_pool.acquire()
    print("[Device] Acquired device:", device.node_thing_name)
    try:
        yield device
    finally:
        device_ec_pool.release(device)


@pytest.fixture
def unregistered_device_ec():
    """Lightweight device with key-certificates only (no AWS IoT registration).

    Use this for tests that only need device.generate_csr() and device.node_thing_name,
    such as pure Matter node association tests where use_device_key=False.

    This is fast enough, so no need to use a pool.
    """
    thing_name = f"test-ec-device-{uuid.uuid4()}"
    private_key_pem, cert_pem = generate_key_and_cert(thing_name, 'ec')
    device = Device(thing_name, private_key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    yield device


@pytest.fixture
def matter_group(test_user1):
    """Fixture that creates a Matter group.

    Yields a dict with:
        - user: The test user
        - group_id: The Matter group ID
        - root_ca: The group's root CA certificate
        - group_api: The Group API helper
    """
    user = test_user1
    group_api = Group(user)
    group_id = None

    try:
        # Create Matter group
        response_data = group_api.create_matter_group("Test Matter Group Fixture")
        group_id = response_data["group_id"]
        matter = response_data["matter"]
        root_ca = matter["root_ca"]

        yield {
            "user": user,
            "group_id": group_id,
            "root_ca": root_ca,
            "group_api": group_api,
        }

    finally:
        if group_id:
            group_api.delete_group(group_id)


# A minimal Light so the Light.Power/Brightness paths the cross-tenant tests
# reference resolve to a real param on the target node.
_TENANT_NODE_CONFIG = {
    "devices": [{
        "id": "Light",
        "type": "esp.device.lightbulb",
        "params": [
            {"id": "Power", "type": "esp.param.power", "properties": ["read", "write"]},
            {"id": "Brightness", "type": "esp.param.brightness", "properties": ["read", "write"]},
        ],
    }]
}


def _make_isolated_tenant(user, device, group_name):
    """Associate a (pooled) node into a new group owned by `user`.

    The device is supplied by the caller so it can be drawn from device_rsa_pool;
    associate_device_with_group overwrites its node config with _TENANT_NODE_CONFIG.

    Returns a dict: {user, group_api, group_id, device, node_id}.
    """
    group_api = Group(user)
    group_id = associate_device_with_group(
        device, user, group_api,
        group_name=group_name, node_config=_TENANT_NODE_CONFIG)
    return {
        "user": user,
        "group_api": group_api,
        "group_id": group_id,
        "device": device,
        "node_id": device.node_thing_name,
    }


@pytest.fixture
def two_tenants(test_user1, test_user2, session_valid_device_rsa, session_valid_device_rsa_2):
    """Two fully isolated tenants A and B, each with their own group + node.

    A == test_user1, B == test_user2. Neither shares anything with the other.
    This is the substrate for the cross-tenant / privilege-escalation tests
    spread across the feature test modules: tenant A attempts to reach tenant
    B's node/group/resource and must always be denied.

    Users and both nodes are pooled; only the per-tenant groups are created and
    torn down here (group deletion also removes the node associations, returning
    the pooled devices to a clean state).

    Yields (tenant_a, tenant_b). Guaranteed teardown of both groups.
    """
    tenant_a = _make_isolated_tenant(test_user1, session_valid_device_rsa, f"PEsc Tenant A {uuid.uuid4()}")
    tenant_b = _make_isolated_tenant(test_user2, session_valid_device_rsa_2, f"PEsc Tenant B {uuid.uuid4()}")
    try:
        yield tenant_a, tenant_b
    finally:
        for t in (tenant_a, tenant_b):
            try:
                t["group_api"].delete_group(t["group_id"], warn_error=True)
            except Exception:
                pass


@pytest.fixture
def session_valid_device_rsa():
    device = device_rsa_pool.acquire()
    print("[Device] Acquired device:", device.node_thing_name)
    try:
        yield device
    finally:
        device_rsa_pool.release(device)



@pytest.fixture
def session_valid_device_rsa_2():
    # A second, distinct rsa device from the SAME pool. pytest caches fixtures
    # by name, so a test needing two live pooled devices must ask for two
    # differently-named fixtures; both acquire()/release() the shared free-list.
    device = device_rsa_pool.acquire()
    print("[Device] Acquired device:", device.node_thing_name)
    try:
        yield device
    finally:
        device_rsa_pool.release(device)

@pytest.fixture
def test_user1():
    user = user_pool.acquire()
    print("[User] Acquired user:", user.sub)
    try:
        yield user
    finally:
        user_pool.release(user)

@pytest.fixture
def test_user2():
    user = user_pool.acquire()
    print("[User] Acquired user:", user.sub)
    try:
        yield user
    finally:
        user_pool.release(user)

@pytest.fixture
def test_user3():
    user = user_pool.acquire()
    print("[User] Acquired user:", user.sub)
    try:
        yield user
    finally:
        user_pool.release(user)

@pytest.fixture
def test_user4():
    user = user_pool.acquire()
    print("[User] Acquired user:", user.sub)
    try:
        yield user
    finally:
        user_pool.release(user)


@pytest.fixture
def test_user5():
    user = user_pool.acquire()
    print("[User] Acquired user:", user.sub)
    try:
        yield user
    finally:
        user_pool.release(user)


@pytest.fixture
def super_admin_user():
    user = admin_user_pool.acquire()
    print("[User] Acquired super-admin user:", user.sub)
    try:
        yield user
    finally:
        admin_user_pool.release(user)


_EMAIL_SENDER_CONFIG = {"config_name": "email-sender", "subtype": "global"}


@pytest.fixture
def verified_email_sender():
    """Pin a SES-verified global email sender for the test's duration.

    SES rejects a send whose FROM address is not verified, so any flow that mails via SES (OTP
    login, and Cognito signup once it routes through SES) needs a verified sender selected in the
    espuser-admin-config table (config_name=email-sender, subtype=global). The auto-fallback the
    code uses when the row is absent only fires when exactly one SES identity is verified; a shared
    test account carries many, so it never fires and dispatch fails. This selects a dedicated
    verified Mailosaur sender, then restores the prior row (or deletes it) on teardown. Mutates
    shared account config, so callers must carry @pytest.mark.xdist_group("env_mut").
    """
    from test.itest.email_utils import generate_mailosaur_email, ensure_ses_verified
    sender = generate_mailosaur_email(user_index="sender")
    if not sender or not ensure_ses_verified(sender):
        pytest.skip("no SES-verifiable Mailosaur sender available")

    table = boto3.resource("dynamodb", region_name=REGION).Table("espuser-admin-config")
    prior = table.get_item(Key=_EMAIL_SENDER_CONFIG).get("Item")
    table.put_item(Item={**_EMAIL_SENDER_CONFIG, "value": sender})
    try:
        yield sender
    finally:
        if prior is not None:
            table.put_item(Item=prior)
        else:
            table.delete_item(Key=_EMAIL_SENDER_CONFIG)


# ENABLE_TEST_CIMD is never baked into a deployment, so tests needing the test CIMD document turn
# it on for their duration and restore after. Mutates shared lambda config → callers must also
# carry @pytest.mark.xdist_group("env_mut").
MCP_OAUTH_PROXY_FN = "rmng-mcp-oauth-proxy"


def _wait_for_lambda_update(lambda_client, function_name):
    for _ in range(30):
        if lambda_client.get_function_configuration(
                FunctionName=function_name).get("LastUpdateStatus") == "Successful":
            return
        time.sleep(2)
    raise TimeoutError(f"Lambda {function_name} update did not complete in time")


@pytest.fixture(scope="module")
def enable_test_cimd():
    lambda_client = boto3.client("lambda", region_name=REGION)
    original_env = lambda_client.get_function_configuration(
        FunctionName=MCP_OAUTH_PROXY_FN).get("Environment", {}).get("Variables", {})

    lambda_client.update_function_configuration(
        FunctionName=MCP_OAUTH_PROXY_FN,
        Environment={"Variables": {**original_env, "ENABLE_TEST_CIMD": "true"}},
    )
    _wait_for_lambda_update(lambda_client, MCP_OAUTH_PROXY_FN)
    try:
        yield
    finally:
        # original_env carries no ENABLE_TEST_CIMD, so restoring it turns the endpoint back off.
        lambda_client.update_function_configuration(
            FunctionName=MCP_OAUTH_PROXY_FN,
            Environment={"Variables": original_env},
        )
        _wait_for_lambda_update(lambda_client, MCP_OAUTH_PROXY_FN)


@pytest.fixture
def valid_device(session_valid_device_ec):
    return session_valid_device_ec

@pytest.fixture
def valid_device_rsa(session_valid_device_rsa):
    return session_valid_device_rsa


@pytest.fixture
def associated_device():
    # Use pooled associated device-resource for faster, parallel-safe reuse
    resource = associated_device_pool.acquire()
    device, group_id, pooled_user, user1_group_api = resource
    print("associated_device is ", device.node_thing_name, " and its group is ", group_id)
    print("associated device's user:", pooled_user.sub)

    assert device.connect()
    try:
        yield device, group_id, pooled_user, user1_group_api
    finally:
        associated_device_pool.release(resource)

@pytest.fixture
def two_devices_same_group(test_user1, session_valid_device_rsa, session_valid_device_rsa_2):
    """Two pooled devices associated with the same group.

    Yields:
        (device1, device2, group_id, user, group_api)

    Both devices are connected and associated with group_id before yielding.
    Useful for group-control broadcast tests. Devices are pooled (their fixtures
    reset+release them); only the group is torn down here, which also removes the
    associations.
    """
    user = test_user1
    group_api = Group(user)
    group_id = group_api.create_group("Group Control Broadcast Test Group")

    devices = [session_valid_device_rsa, session_valid_device_rsa_2]
    try:
        for device in devices:
            assert connect_device_with_retry(device, max_retries=3, base_delay=2), \
                f"Failed to connect device {device.node_thing_name}"
            result = user.do_user_node_assoc(device, group_id)
            assert result is None, f"Association failed for {device.node_thing_name}: {result}"
            assert device.wait_for_group_info(), \
                f"Device {device.node_thing_name} did not receive group info after association"

        # Reconnect both devices so the IoT policy re-evaluates with the
        # newly-set group_id thing attribute (policy variables are resolved
        # at connection time, not subscribe time).
        for device in devices:
            device.disconnect()
            device.clear_queues()
            assert device.connect(), \
                f"Failed to reconnect device {device.node_thing_name} after association"

        yield devices[0], devices[1], group_id, user, group_api
    finally:
        group_api.delete_group(group_id, warn_error=True)


@pytest.fixture
def shared_group(test_user2, associated_device):
    """
    Flow controller for group sharing phases with subtest-friendly API.

    Provides methods to move between ordered phases while keeping a stable
    test context, so parallel tests don't depend on execution order.
    """
    device, group_id, test_user1, user1_group_api = associated_device

    class SharedGroupFlow:
        def __init__(self):
            self._state = "begin"
            self._group_id = group_id
            self._device = device
            self._test_user1 = test_user1
            self._user1_group_api = user1_group_api
            self._test_user2 = test_user2
            self._user2_group_api = Group(test_user2)

        def details(self):
            return {
                'state': self._state,
                'group_id': self._group_id,
                'user2_group_api': self._user2_group_api,
                'user1_group_api': self._user1_group_api,
                'device': self._device,
                'test_user2': self._test_user2,
                'test_user1': self._test_user1,
            }

        def begin(self):
            # Ensure unshared state if previously shared
            if self._state == "shared":
                try:
                    self._user1_group_api.unshare_group(self._group_id, self._test_user2.user_id)
                except Exception:
                    pass
            self._state = "begin"
            return self.details()

        def share_primary(self):
            if self._state != "shared":
                self._user1_group_api.share_group(self._group_id, self._test_user2.username, "primary")
                accept_sharing_request_for(self._test_user2, self._group_id, "")
                groups = self._user2_group_api.list_groups()
                assert any(group['group_id'] == self._group_id for group in groups['groups']), "Shared group not found in test_user2's groups"
                self._state = "shared"
            return self.details()

        def unshare_primary(self):
            if self._state == "shared":
                self._user1_group_api.unshare_group(self._group_id, self._test_user2.user_id)
            self._state = "unshared"
            return self.details()

    flow = SharedGroupFlow()
    yield flow

def run_shared_group_stages(shared_group, subtests, body, stages=None):
    """
    Helper to iterate shared-group stages with subtests and correct sequencing managed internally.
    body(stage, data) will be invoked inside the subtest context for each stage.
    """
    if stages is None:
        stages = ["share_begin", "primary_share", "primary_unshare"]

    for stage in stages:
        if stage == "share_begin":
            data = shared_group.begin()
        elif stage == "primary_share":
            data = shared_group.share_primary()
        elif stage == "primary_unshare":
            data = shared_group.unshare_primary()
        else:
            raise ValueError(f"Unknown stage: {stage}")

        with subtests.test(msg=stage):
            body(stage, data)

def run_device_with_2_subgroups_stages(flow, subtests, body, stages=None):
    """
    Helper to iterate subgroup add/remove stages for device_with_2_subgroups with subtests.
    body(stage, data) will be invoked inside the subtest context for each stage.
    """
    if stages is None:
        stages = [
            "subgroup_add_begin",
            "subgroup_add_1",
            "subgroup_add_2",
            "subgroup_remove_1",
            "subgroup_remove_2",
        ]

    for stage in stages:
        if stage == "subgroup_add_begin":
            data = flow.subgroup_add_begin()
        elif stage == "subgroup_add_1":
            data = flow.subgroup_add_1()
        elif stage == "subgroup_add_2":
            data = flow.subgroup_add_2()
        elif stage == "subgroup_remove_1":
            data = flow.subgroup_remove_1()
        elif stage == "subgroup_remove_2":
            data = flow.subgroup_remove_2()
        else:
            raise ValueError(f"Unknown stage: {stage}")

        with subtests.test(msg=stage):
            body(stage, data)

@pytest.fixture
def shared_subgroup(test_user2, associated_device):
    """
    Session-scoped flow controller for subgroup sharing phases.

    Provides methods to move between ordered phases with stable test context,
    enabling parallel-safe execution while preserving per-stage subtest reporting.
    """
    device, group_id, test_user1, user1_group_api = associated_device

    class SharedSubgroupFlow:
        def __init__(self):
            self._state = "begin"
            self._group_id = group_id
            self._device = device
            self._test_user1 = test_user1
            self._user1_group_api = user1_group_api
            self._test_user2 = test_user2
            self._user2_group_api = Group(test_user2)
            self._subgroup_id = None

        def _ensure_subgroup(self):
            if self._subgroup_id is None:
                subgroup_name = f"Test Subgroup {uuid.uuid4()}"
                subgroup_id = self._user1_group_api.create_subgroup(self._group_id, subgroup_name)
                assert subgroup_id is not None, "Failed to create subgroup"
                self._user1_group_api.add_node_to_subgroup(self._group_id, subgroup_id, self._device.node_thing_name)
                self._subgroup_id = subgroup_id

        def details(self):
            return {
                'state': self._state,
                'group_id': self._group_id,
                'subgroup_id': self._subgroup_id,
                'user2_group_api': self._user2_group_api,
                'user1_group_api': self._user1_group_api,
                'device': self._device,
                'test_user2': self._test_user2,
                'test_user1': self._test_user1,
            }

        def begin(self):
            # If previously shared, bring back to base state by unsharing
            if self._state == "shared" and self._subgroup_id is not None:
                try:
                    self._user1_group_api.unshare_subgroup(self._group_id, self._subgroup_id, self._test_user2.user_id)
                except Exception:
                    pass
            self._ensure_subgroup()
            self._state = "begin"
            return self.details()

        def share_subgroup(self):
            self._ensure_subgroup()
            if self._state != "shared":
                self._user1_group_api.share_subgroup(self._group_id, self._subgroup_id, self._test_user2.username)
                accept_sharing_request_for(self._test_user2, self._group_id, self._subgroup_id)
                groups = self._user2_group_api.list_groups()
                shared_group = next((group for group in groups['groups'] if group['group_id'] == self._group_id), None)
                assert shared_group is not None, "Shared group not found in test_user2's groups"
                assert any(subgroup['subgroup_id'] == self._subgroup_id for subgroup in shared_group.get('subgroups', [])), "Shared subgroup not found in test_user2's groups"
                self._state = "shared"
            return self.details()

        def unshare_subgroup(self):
            self._ensure_subgroup()
            if self._state == "shared":
                self._user1_group_api.unshare_subgroup(self._group_id, self._subgroup_id, self._test_user2.user_id)
            self._state = "unshared"
            # Verify access removed
            groups = self._user2_group_api.list_groups()
            shared_group = next((group for group in groups['groups'] if group['group_id'] == self._group_id), None)
            assert shared_group is None or not any(subgroup['subgroup_id'] == self._subgroup_id for subgroup in shared_group.get('subgroups', [])), "Unshared subgroup still found in test_user2's groups"
            return self.details()

    flow = SharedSubgroupFlow()
    yield flow

def run_shared_subgroup_stages(shared_subgroup, subtests, body, stages=None):
    """
    Helper to iterate subgroup-sharing stages with subtests and correct sequencing managed internally.
    body(stage, data) will be invoked inside the subtest context for each stage.
    """
    if stages is None:
        stages = ["share_begin", "subgroup_share", "subgroup_unshare"]

    for stage in stages:
        if stage == "share_begin":
            data = shared_subgroup.begin()
        elif stage == "subgroup_share":
            data = shared_subgroup.share_subgroup()
        elif stage == "subgroup_unshare":
            data = shared_subgroup.unshare_subgroup()
        else:
            raise ValueError(f"Unknown stage: {stage}")

        with subtests.test(msg=stage):
            body(stage, data)


@pytest.fixture
def device_with_2_subgroups(associated_device):
    """
    Flow controller for subgroup add/remove sequence used by multiple tests.
    Provides staged methods and stable details for each stage.
    """
    device, main_group_id, test_user1, user1_group_api = associated_device

    class DeviceWith2SubgroupsFlow:
        def __init__(self):
            self._state = "begin"
            self._group_id = main_group_id
            self._device = device
            self._user1_group_api = user1_group_api
            self._test_user1 = test_user1
            self._subgroups = []
            self._removed_subgroup = None

        def details(self):
            return {
                'state': self._state,
                'group_id': self._group_id,
                'device': self._device,
                'user1_group_api': self._user1_group_api,
                'test_user1': self._test_user1,
                'subgroups': list(self._subgroups),
                'removed_subgroup': self._removed_subgroup
            }

        def subgroup_add_begin(self):
            self._state = "begin"
            return self.details()

        def subgroup_add_1(self):
            subgroup_id = self._user1_group_api.create_subgroup(self._group_id, "Test Subgroup")
            self._user1_group_api.add_node_to_subgroup(self._group_id, subgroup_id, self._device.node_thing_name)
            self._subgroups = [subgroup_id]
            self._state = "subgroup_add_1"
            return self.details()

        def subgroup_add_2(self):
            # If first subgroup not present, ensure it
            if not self._subgroups:
                self.subgroup_add_1()
            subgroup_id = self._user1_group_api.create_subgroup(self._group_id, "Test Subgroup 2")
            self._user1_group_api.add_node_to_subgroup(self._group_id, subgroup_id, self._device.node_thing_name)
            self._subgroups = [self._subgroups[0], subgroup_id]
            self._state = "subgroup_add_2"
            return self.details()

        def subgroup_remove_1(self):
            if not self._subgroups:
                self.subgroup_add_1()
            self._user1_group_api.remove_node_from_subgroup(self._group_id, self._subgroups[0], self._device.node_thing_name)
            self._removed_subgroup = self._subgroups[0]
            self._subgroups = self._subgroups[1:]
            self._state = "subgroup_remove_1"
            return self.details()

        def subgroup_remove_2(self):
            if len(self._subgroups) < 1:
                # Ensure at least one subgroup exists (and device added) before removal
                self.subgroup_add_2()
            self._user1_group_api.remove_node_from_subgroup(self._group_id, self._subgroups[0], self._device.node_thing_name)
            self._removed_subgroup = self._subgroups[0]
            self._subgroups = []
            self._state = "subgroup_remove_2"
            return self.details()

    return DeviceWith2SubgroupsFlow()


@pytest.fixture
def user_with_1_dev_each_in_2_groups(test_user1, session_valid_device_rsa, session_valid_device_rsa_2):
    # Create two groups
    user1_group_api = Group(test_user1)
    group1_id = user1_group_api.create_group("Test Group 1")
    group2_id = user1_group_api.create_group("Test Group 2")

    def set_node_config_checked(device, config):
        """set_node_config with retries. The 5s ack window is sometimes missed on a
        cold node-config lambda; the call is idempotent, so retry rather than let a
        device silently proceed unconfigured (discovery then sees fewer endpoints)."""
        for _ in range(3):
            if device.set_node_config(config):
                return
        pytest.fail(f"set_node_config never acknowledged for {device.node_thing_name}")

    # Two pooled rsa devices with different configs. set_node_config below
    # overwrites whatever config a reused device carried from a prior test.
    device1 = session_valid_device_rsa
    assert device1.connect(), "Failed to connect the device"
    set_node_config_checked(device1, {
        "devices": [{
            "id": "Light1",
            "type": "esp.device.lightbulb",
            "params": [
                {"id": "Power", "type": "esp.param.power"},
                {"id": "Brightness", "type": "esp.param.brightness"},
                {"id": "Name", "type": "esp.param.name", "data_type": "string"}
            ]
        }, {
            "id": "Light2",
            "type": "esp.device.lightbulb",
            "params": [
                {"id": "Power", "type": "esp.param.power"},
                {"id": "Name", "type": "esp.param.name", "data_type": "string"}
            ]
        }]
    })

    device2 = session_valid_device_rsa_2
    assert device2.connect(), "Failed to connect the device"
    set_node_config_checked(device2, {
        "devices": [{
            "id": "Switch1",
            "type": "esp.device.switch",
            "params": [
                {"id": "Power", "type": "esp.param.power"}
            ]
        }],
        "info": {
            "fw_version": "1.1.0"
        }
    })

    # Associate devices with groups
    result = test_user1.do_user_node_assoc(device1, group1_id)
    assert result == None, f"Association failed with error: {result}"
    result = test_user1.do_user_node_assoc(device2, group2_id)
    assert result == None, f"Association failed with error: {result}"
    yield device1, device2, group1_id, group2_id, test_user1
    # Cleanup. Both devices are pooled — their fixtures reset+release them, so
    # only the groups are torn down here (which also removes the associations).
    user1_group_api.delete_group(group1_id)
    user1_group_api.delete_group(group2_id)


@pytest.fixture(scope="function")
def test_device_new():
    """
    Fixture that creates a fresh test device for each test.
    Using function scope to ensure each test gets a clean device.
    """
    # Create a unique device name using UUID
    device_name = f"test-device-{uuid.uuid4()}"

    # Create the device with RSA key pair
    device = Device(device_name, *generate_key_and_cert(device_name, 'rsa'),
                   CA_CERT, IOT_ENDPOINT, REGION, DEBUG)

    yield device  # Provide the device to the test

    # Cleanup after the test
    try:
        device.disconnect()
    except:
        pass  # Ignore disconnect errors
    try:
        device.destroy_test_node()
    except:
        pass  # Ignore cleanup errors


@pytest.fixture
def node_csv_uploader(request):
    """
    Fixture that generates and uploads node registration CSV, then cleans up the S3 file.
    Usage:
        s3_path = node_csv_uploader(user, nodes)
        # or
        s3_path, certs = node_csv_uploader(user, nodes, return_certs=True)
    """
    uploaded_files = []

    def _upload(user, nodes, return_certs=False):
        result = generate_and_upload_node_csv(user, nodes, return_certs)
        # Track S3 path for cleanup
        if return_certs:
            s3_path, certs = result
            uploaded_files.append(s3_path)
            return s3_path, certs
        else:
            s3_path = result
            uploaded_files.append(s3_path)
            return s3_path

    yield _upload

    # Cleanup: Delete all uploaded S3 files
    for s3_path in uploaded_files:
        try:
            s3_parts = s3_path.replace("s3://", "").split("/", 1)
            if len(s3_parts) == 2:
                bucket, key = s3_parts
                s3 = boto3.client('s3', region_name=REGION)
                s3.delete_object(Bucket=bucket, Key=key)
                print(f"Fixture cleanup: Deleted S3 file: {s3_path}")
        except Exception as e:
            print(f"Fixture cleanup warning: Failed to delete S3 file {s3_path}: {e}")


# ============================================================================
# Helper functions
# ============================================================================

def accept_sharing_request_for(user, group_id, subgroup_id):
    # Try up to 3 times with 2 second delays
    max_retries = 3
    retry_delay = 2

    # Ensure user has valid credentials
    if not user.token:
        user_log(f"Getting new token for user {user.username}")
        user.get_cognito_token()
    if not user.credentials:
        user_log(f"Getting new credentials for user {user.username}")
        user.get_aws_credentials()

    for attempt in range(max_retries):
        user_log(f"Attempt {attempt + 1}/{max_retries} to find sharing request for group {group_id}")
        sharing_requests = user.get_sharing_requests()
        if sharing_requests is None:
            user_log("Failed to retrieve sharing requests")
            if attempt < max_retries - 1:
                time.sleep(retry_delay)
                continue
            assert False, "Failed to retrieve sharing requests"

        user_log(f"Retrieved sharing requests: {sharing_requests}")

        # Find the correct sharing request for the group we just shared
        sharing_request_id = None
        for request in sharing_requests:
            user_log(f"Checking request: {request}")
            # Handle both None and empty string for subgroup_id
            request_subgroup = request['subgroup_id']
            if request['group_id'] == group_id and (request_subgroup == subgroup_id or (subgroup_id is None and request_subgroup == '')):
                sharing_request_id = request['sharing_request_id']
                user_log(f"Found matching sharing request: {sharing_request_id}")
                break

        if sharing_request_id is not None:
            # Found the request, process it
            user_log(f"Processing sharing request {sharing_request_id}")
            assert user.process_sharing_request(sharing_request_id, 'accept')
            return True

        # If we didn't find the request and this isn't the last attempt, wait and retry
        if attempt < max_retries - 1:
            user_log(f"Sharing request not found, waiting {retry_delay} seconds before retry")
            time.sleep(retry_delay)

    assert False, f"Sharing request for group {group_id} and subgroup {subgroup_id} not found after {max_retries} attempts"


def assert_subgroup_in_group(groups, group_id, subgroup_id):
    for group in groups['groups']:
        if group['group_id'] == group_id:
            assert any(subgroup['subgroup_id'] == subgroup_id for subgroup in group['subgroups']), f"Shared subgroup {subgroup_id} not found in group {group_id}"
            return
    assert False, f"Group {group_id} not found in user's groups"


def validate_user_group_dynamodb_entry(user_id, group_id, expected_item):
    # Use boto3 to verify DynamoDB entry
    dynamodb = boto3.resource('dynamodb', region_name=REGION)
    user_group_table = dynamodb.Table('rmng-user-group-assoc')

    # Get the entry for test_user2
    response = user_group_table.get_item(
        Key={
            'user_id': user_id,
            'group_id': group_id
        }
    )
    response['Item']['sub_entity_ids'] = sorted(response['Item']['sub_entity_ids'])
    assert response['Item'] == expected_item


def connect_device_with_retry(device, max_retries=2, base_delay=1):
    """Helper function to connect device with retry mechanism"""
    for attempt in range(max_retries):
        try:
            if device.connect():
                return True
            if attempt < max_retries - 1:
                time.sleep(base_delay * (2 ** attempt))  # Exponential backoff
        except Exception as e:
            print(f"Connection attempt {attempt + 1} failed: {str(e)}")
            if attempt < max_retries - 1:
                time.sleep(base_delay * (2 ** attempt))
    return False


# ---------------------------------------------------------------------------
# Shadow / presence helpers
#
# Shared by the device-status, association and event-pipeline suites, which all
# need to read a named shadow or wait for the presence lambda to write one.
# ---------------------------------------------------------------------------

def iot_data_client():
    """boto3 iot-data client bound to this deployment's IoT endpoint."""
    return boto3.client("iot-data", region_name=REGION, endpoint_url=f"https://{IOT_ENDPOINT}")


def get_shadow(thing_name, shadow_name):
    """Shadow document, or None when the shadow does not exist."""
    iot_data = iot_data_client()
    try:
        response = iot_data.get_thing_shadow(thingName=thing_name, shadowName=shadow_name)
    except iot_data.exceptions.ResourceNotFoundException:
        return None
    return json.loads(response["payload"].read())


def reported_state(thing_name, shadow_name):
    """The shadow's reported state, or {} when the shadow does not exist."""
    return (get_shadow(thing_name, shadow_name) or {}).get("state", {}).get("reported", {})


def wait_for_reported_online(thing_name, shadow_name, expected, timeout=60):
    """Poll a shadow until reported.online is `expected`.

    Returns the last value seen, so a failing caller can report what the shadow
    was stuck at rather than just that it timed out. The default timeout clears
    the presence lambda's 10s offline delay plus the round-trip through
    nodes-online and the shadow write, which runs ~25s in practice.
    """
    deadline = time.time() + timeout
    while True:
        online = reported_state(thing_name, shadow_name).get("online")
        if online is expected or time.time() >= deadline:
            return online
        time.sleep(2)


def wait_for_shadow_absent(thing_name, shadow_name, timeout=30):
    """Poll until the named shadow no longer exists."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if get_shadow(thing_name, shadow_name) is None:
            return True
        time.sleep(2)
    return False


def request_from_cloud(device, event_name, attempts=3, timeout=20):
    """Publish a to_cloud event and return the matching from_cloud answer.

    Re-asks like firmware does: the reply crosses the IoT rule, the lambda (via
    SQS when the deployment is in that mode) and back, which exceeds 20s under
    load — the SDK's own setNodeConfig ack waits 30s for the same reason.
    """
    for _ in range(attempts):
        while not device.from_cloud_queue.empty():
            device.from_cloud_queue.get_nowait()
        if not device.publish_to_cloud({"event": [event_name]}):
            continue
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                message = device.from_cloud_queue.get(timeout=2)
            except Empty:
                continue
            if event_name in message:
                return message[event_name]
    return None


def wait_for_node_session(thing_name, timeout=30):
    """Wait for node_connected_rule to record the broker session in rmng-nodes-online.

    Returns (sessionIdentifier, versionNumber), or (None, None) on timeout, so a
    caller can build an exactly-matching disconnect event. Presence disconnects
    are dropped outright when there is no row, so the disconnect path is only
    reachable once this lands.
    """
    table = boto3.resource("dynamodb", region_name=REGION).Table("rmng-nodes-online")
    deadline = time.time() + timeout
    while time.time() < deadline:
        item = table.get_item(Key={"clientId": thing_name}).get("Item")
        if item and item.get("sessionIdentifier"):
            return item["sessionIdentifier"], int(item.get("versionNumber", 0))
        time.sleep(2)
    return None, None


def generate_and_upload_node_csv(user, nodes, return_certs=False):
    """
    Generate node registration CSV with real certificates and upload it to the API.
    Args:
        user (User): User object to use for upload
        nodes (list): List of node dicts with keys: node_id, city, type, model, subtype, key_type
        return_certs (bool): If True, return generated certificates along with S3 path
    Returns:
        str or tuple: S3 path of the uploaded CSV file, or (s3_path, certs_dict) if return_certs=True
    """
    print("Generating and uploading node registration CSV with real certificates...")

    # Create test/test_data directory if it doesn't exist
    test_data_dir = os.path.join('build', 'test')
    os.makedirs(test_data_dir, exist_ok=True)

    # Generate certificates and create CSV content
    csv_data = []
    csv_data.append(["node_id", "certs", "city", "type", "model", "subtype"])
    generated_certs = {}  # Store generated certificates

    for node in nodes:
        print(f"Generating {node['key_type'].upper()} certificate for {node['node_id']}...")

        # Generate key and certificate using the function from test_device
        private_key_pem, cert_pem = generate_key_and_cert(
            thing_name=node['node_id'],
            key_type=node['key_type']
        )
        node_cert, _ = split_combined_cert_pem(cert_pem)

        # Store certificates if requested
        if return_certs:
            generated_certs[node['node_id']] = {
                'private_key': private_key_pem,
                'cert': cert_pem
            }

        # Add row data
        csv_data.append([
            node['node_id'],
            node_cert,
            node['city'],
            node['type'],
            node['model'],
            node['subtype']
        ])

        print(f"Certificate generated for {node['node_id']} ({len(cert_pem)} characters)")

    # Create CSV file in test/test_data directory
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    csv_filename = os.path.join(test_data_dir, f"node_registration_upload_{timestamp}.csv")
    try:
        # Write to CSV file
        with open(csv_filename, 'w', newline='', encoding='utf-8') as csvfile:
            writer = csv.writer(csvfile)
            writer.writerows(csv_data)

        file_size = os.path.getsize(csv_filename)
        print(f"\nGenerated CSV file: {csv_filename} ({file_size} bytes)")

        # Upload the file using the new upload_file method
        print("Uploading CSV file to S3...")
        success, result = user.upload_file(csv_filename, 'node_cert')
        assert success, f"File upload failed: {result}"
        s3_path = result
        print(f"CSV file uploaded successfully to {s3_path}!")

        if return_certs:
            return s3_path, generated_certs
        else:
            return s3_path
    finally:
        # Clean up temporary file
        try:
            if os.path.exists(csv_filename):
                os.remove(csv_filename)
                print(f"Cleaned up temporary file: {csv_filename}")
        except Exception as e:
            print(f"Warning: Could not clean up {csv_filename}: {e}")


# ============================================================================
# Matter Certificate Helper Functions
# ============================================================================

# Matter OIDs from spec
MATTER_FABRIC_ID_OID = "1.3.6.1.4.1.37244.1.5"
MATTER_NODE_ID_OID = "1.3.6.1.4.1.37244.1.1"
MATTER_CAT_ID_OID = "1.3.6.1.4.1.37244.1.6"
MATTER_RCAC_ID_OID = "1.3.6.1.4.1.37244.1.4"


def parse_certificate_pem(cert_pem):
    """Parse a PEM-encoded certificate."""
    return x509.load_pem_x509_certificate(cert_pem.encode())


def verify_certificate_signed_by(cert_pem, root_ca_pem):
    """Verify that a certificate was signed by the given Root CA."""
    cert = parse_certificate_pem(cert_pem)
    root_ca = parse_certificate_pem(root_ca_pem)

    try:
        root_ca.public_key().verify(
            cert.signature,
            cert.tbs_certificate_bytes,
            ec.ECDSA(cert.signature_hash_algorithm)
        )
        return True
    except Exception:
        return False


def extract_matter_oids(cert_pem):
    """Extract Matter-specific OIDs from certificate subject."""
    cert = parse_certificate_pem(cert_pem)
    result = {'fabric_id': None, 'node_id': None, 'cat_id': None, 'rcac_id': None}

    for attr in cert.subject:
        oid = attr.oid.dotted_string
        if oid == MATTER_FABRIC_ID_OID:
            result['fabric_id'] = attr.value
        elif oid == MATTER_NODE_ID_OID:
            result['node_id'] = attr.value
        elif oid == MATTER_CAT_ID_OID:
            result['cat_id'] = attr.value
        elif oid == MATTER_RCAC_ID_OID:
            result['rcac_id'] = attr.value

    return result


@pytest.fixture(scope="session")
def chromium_browser():
    """Provision + hand over a headless Chromium, installing the browser binary on first use so
    a browser test needs no manual `playwright install`. Skips if the install/launch cannot
    complete, so suites that only need HTTP-level coverage are unaffected."""
    import subprocess
    import sys

    playwright_sync = pytest.importorskip("playwright.sync_api")

    def _launch(p):
        return p.chromium.launch(headless=True)

    with playwright_sync.sync_playwright() as p:
        try:
            browser = _launch(p)
        except Exception:  # noqa: BLE001
            subprocess.run([sys.executable, "-m", "playwright", "install", "chromium"], check=True)
            try:
                browser = _launch(p)
            except Exception as e:  # noqa: BLE001
                pytest.skip(f"chromium unavailable ({e})")
        yield browser
        browser.close()


def complete_federation_login(client_id, redirect_uri, username, password, scope,
                              provider="cognito", follow_final=False,
                              client_secret=None, secret_via="basic"):
    """Drive a full brokered login and return the token response JSON.

    authorize -> federation start -> the provider's hosted UI -> our callback -> code exchange.
    Raises AssertionError with the failing step's status, so a caller sees where it broke. Set
    follow_final to get the callback response instead of exchanging, for tests that expect no code.
    A confidential client passes client_secret; secret_via picks how it authenticates at the token
    endpoint — "basic" (HTTP Basic, what Alexa sends) or "post" (form-body credentials, what Google
    account linking sends).
    """
    import requests as _requests
    from urllib.parse import urlparse as _urlparse, parse_qs as _parse_qs

    verifier, challenge = pkce_pair()
    session = _requests.Session()

    authz = session.get(f"{USER_API_GATEWAY_URL}/oauth2/authorize", params={
        "response_type": "code",
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "scope": scope,
        "state": "fed-state",
        "code_challenge": challenge,
        "code_challenge_method": "S256",
    }, allow_redirects=False)
    assert authz.status_code == 302, f"authorize: {authz.status_code} {authz.text[:200]}"
    assert flow_id_from_cookie(authz), "authorize must set the esp_flow_id cookie"

    fed = session.get(f"{USER_API_GATEWAY_URL}/oauth2/federation/start",
                      params={"provider": provider}, allow_redirects=False)
    assert fed.status_code == 302, f"federation start: {fed.status_code} {fed.text[:200]}"

    our_callback = cognito_hosted_login(session, fed.headers["Location"], username, password)
    cb = session.get(our_callback, allow_redirects=False)
    if follow_final:
        return cb

    assert cb.status_code == 302, f"federation callback: {cb.status_code} {cb.text[:300]}"
    code = _parse_qs(_urlparse(cb.headers["Location"]).query).get("code", [None])[0]
    assert code, f"callback carried no code: {cb.headers['Location']}"

    data = {
        "grant_type": "authorization_code",
        "code": code,
        "code_verifier": verifier,
        "client_id": client_id,
        "redirect_uri": redirect_uri,
    }
    auth = None
    if client_secret is not None:
        if secret_via == "basic":
            auth = (client_id, client_secret)
        else:
            data["client_secret"] = client_secret
    token = _requests.post(f"{USER_API_GATEWAY_URL}/oauth2/token", data=data, auth=auth)
    assert token.status_code == 200, f"token exchange: {token.status_code} {token.text[:300]}"
    return token.json()


def decode_jwt_claims(token):
    """Decode a JWT payload without verifying; signature coverage lives in test_oidc_discovery."""
    import base64 as _b64
    import json as _json
    payload = token.split(".")[1]
    payload += "=" * (-len(payload) % 4)
    return _json.loads(_b64.urlsafe_b64decode(payload))


@pytest.fixture
def provision_end_user():
    """Factory for provider accounts whose contacts are already verified, so no email or SMS delivery
    is involved. A factory rather than a plain fixture because a test may need several, with different
    attributes; everything made through it is removed afterwards, including the user rows the logins
    created.
    """
    made = []

    def _make(username, attributes, password):
        cognito = boto3.client('cognito-idp', region_name=REGION)
        cognito.admin_create_user(
            UserPoolId=END_USER_POOL_ID, Username=username, MessageAction='SUPPRESS',
            UserAttributes=[{"Name": k, "Value": v} for k, v in attributes.items()],
        )
        cognito.admin_set_user_password(
            UserPoolId=END_USER_POOL_ID, Username=username, Password=password, Permanent=True,
        )
        made.append((username, attributes.get("email"), attributes.get("phone_number")))
        return username

    yield _make

    from boto3.dynamodb.conditions import Key
    cognito = boto3.client('cognito-idp', region_name=REGION)
    table = boto3.resource('dynamodb', region_name=REGION).Table('espuser-user-details')
    user_ids = set()
    for username, email, phone in made:
        try:
            cognito.admin_delete_user(UserPoolId=END_USER_POOL_ID, Username=username)
        except Exception:  # noqa: BLE001
            pass
        # Ids are opaque, so the row is found through the contact indexes rather than computed.
        for index, attribute, value in (
            ('espuser-user-details-by-email', 'email', (email or "").lower()),
            ('espuser-user-details-by-phone', 'phone', phone or ""),
        ):
            if not value:
                continue
            try:
                items = table.query(IndexName=index,
                                    KeyConditionExpression=Key(attribute).eq(value)).get('Items', [])
                user_ids.update(i['user_id'] for i in items)
            except Exception:  # noqa: BLE001
                pass
    for user_id in user_ids:
        try:
            table.delete_item(Key={'user_id': user_id})
        except Exception:  # noqa: BLE001
            pass
# ============================================================================
# Webhook mock toggle (test-infra deployed by `make itest-setup`)
# ============================================================================

# Path to the test-infra outputs written by `make itest-setup`.
_TEST_INFRA_OUTPUTS = "build/cdk/cdk-outputs-test.json"
_NOTIFICATIONS_LAMBDA = "rmng-notifications"


def _load_mock_infra():
    """Return (base_url, api_key) for the test webhook mock, or None if not deployed."""
    try:
        with open(_TEST_INFRA_OUTPUTS) as f:
            outputs = json.load(f).get("rmng-test-infra-base", {})
    except (FileNotFoundError, json.JSONDecodeError):
        return None
    base_url = outputs.get("ApiGatewayUrl")
    secret_arn = outputs.get("MockApiKeySecretArn")
    if not base_url or not secret_arn:
        return None
    api_key = boto3.client("secretsmanager").get_secret_value(
        SecretId=secret_arn
    )["SecretString"]
    return base_url, api_key


def _set_notifications_env(**pairs):
    """Patch rmng-notifications env vars via drx.py (VAR=VALUE ...)."""
    args = [f"{k}={v}" for k, v in pairs.items()]
    subprocess.run(
        [sys.executable, "tools/drx.py", "update-env", _NOTIFICATIONS_LAMBDA, *args],
        check=True,
    )


@pytest.fixture(scope="session")
def webhook_mock():
    """Enable the DynamoDB webhook mock on the notifications Lambda for the run.

    Yields (base_url, api_key): the test-infra API Gateway URL (deployed by
    `make itest-setup`) and the API key gating every /v1 method. Points
    rmng-notifications at the mock with the key, and disables it on teardown —
    even if a test fails — by blanking webhook_mock_base_url, the sole toggle the
    Lambda reads. Skips the requesting test(s) when the infra isn't deployed. The
    test sends the api_key on its own validate GETs; the notifications Lambda gets
    it via the webhook_mock_api_key env var.
    """
    infra = _load_mock_infra()
    if infra is None:
        pytest.skip(
            "test webhook mock not deployed; run `make itest-setup` first"
        )
    base_url, api_key = infra

    _set_notifications_env(
        webhook_mock_base_url=base_url,
        webhook_mock_api_key=api_key,
    )
    try:
        yield base_url, api_key
    finally:
        _set_notifications_env(webhook_mock_base_url="")
