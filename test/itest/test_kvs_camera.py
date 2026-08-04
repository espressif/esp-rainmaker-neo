# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Integration tests for KVS camera streaming.

Tests device KVS credential acquisition and access isolation using IoT Credential Provider,
and user KVS viewer access via assume-role credentials.
"""
import json
import pytest
import uuid
import tempfile
import os

import boto3
import requests
from botocore.exceptions import ClientError

from py_sdk.test_device import Device, generate_key_and_cert
from py_sdk.test_group import Group
from test.itest.conftest import (
    node_registrar_identity,
    CREDENTIAL_PROVIDER_ENDPOINT,
    DEVICE_VIDEO_ROLE_ALIAS,
    REGION,
    CA_CERT,
    IOT_ENDPOINT,
    DEBUG,
)


@pytest.fixture
def kvs_device(test_user1):
    """Register a device with capabilities=["kvs"], associate it with a group, and yield.

    The registration Lambda creates the signaling channel and attaches rmng-node-video-policy.
    """
    from test.itest.conftest import associate_device_with_group

    thing_name = f"test-kvs-{uuid.uuid4().hex[:8]}"
    private_key_pem, cert_pem = generate_key_and_cert(thing_name, 'rsa')
    device = Device(thing_name, private_key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    device.register_test_node(capabilities=["kvs"], caller_identity=node_registrar_identity())

    user = test_user1
    group_api = Group(user)
    group_id = associate_device_with_group(device, user, group_api)

    try:
        yield device, group_id, user, group_api
    finally:
        device.disconnect()
        device.destroy_test_node()
        group_api.delete_group(group_id, warn_error=True)


def _get_device_kvs_credentials(device):
    """Get temporary KVS credentials from the IoT Credential Provider.

    Returns the raw credentials dict or raises on failure.
    """
    # Ensure the rmng-node-video-policy is attached
    iot_client = boto3.client("iot", region_name=REGION)
    principals = iot_client.list_thing_principals(thingName=device.node_thing_name)
    for principal_arn in principals.get("principals", []):
        try:
            iot_client.attach_policy(
                policyName="rmng-node-video-policy",
                target=principal_arn,
            )
        except iot_client.exceptions.ResourceAlreadyExistsException:
            pass

    with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as cert_file:
        cert_file.write(device.node_cert)
        cert_path = cert_file.name

    with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as key_file:
        key_file.write(device.node_key)
        key_path = key_file.name

    try:
        url = f"https://{CREDENTIAL_PROVIDER_ENDPOINT}/role-aliases/{DEVICE_VIDEO_ROLE_ALIAS}/credentials"
        headers = {"x-amzn-iot-thingname": device.node_thing_name}

        response = requests.get(
            url,
            headers=headers,
            cert=(cert_path, key_path),
            verify=True,
        )
        if response.status_code != 200:
            print(f"[KVS Cred Provider] Status: {response.status_code}, Body: {response.text}")
        response.raise_for_status()
        return response.json()["credentials"]
    finally:
        os.unlink(cert_path)
        os.unlink(key_path)


# ---------------------------------------------------------------------------
# Device flow tests
# ---------------------------------------------------------------------------

def test_device_obtain_kvs_credentials(kvs_device):
    """Device obtains temporary KVS credentials from Credential Provider."""
    device, group_id, user, user_group_api = kvs_device
    creds = _get_device_kvs_credentials(device)
    assert "accessKeyId" in creds
    assert "secretAccessKey" in creds
    assert "sessionToken" in creds


def test_device_describe_own_channel(kvs_device):
    """Device uses KVS credentials to describe its own signaling channel."""
    device, group_id, user, user_group_api = kvs_device
    creds = _get_device_kvs_credentials(device)

    kvs_client = boto3.client(
        "kinesisvideo",
        region_name=REGION,
        aws_access_key_id=creds["accessKeyId"],
        aws_secret_access_key=creds["secretAccessKey"],
        aws_session_token=creds["sessionToken"],
    )

    channel_name = f"rmng-v1-{device.node_thing_name}"
    response = kvs_client.describe_signaling_channel(ChannelName=channel_name)
    channel_info = response["ChannelInfo"]
    assert channel_info["ChannelName"] == channel_name
    assert channel_info["ChannelType"] == "SINGLE_MASTER"


def test_device_cross_device_isolation(kvs_device, session_valid_device_rsa):
    """Device A cannot describe device B's signaling channel."""
    device_a, group_id, user, user_group_api = kvs_device
    device_b = session_valid_device_rsa

    creds_a = _get_device_kvs_credentials(device_a)

    kvs_client = boto3.client(
        "kinesisvideo",
        region_name=REGION,
        aws_access_key_id=creds_a["accessKeyId"],
        aws_secret_access_key=creds_a["secretAccessKey"],
        aws_session_token=creds_a["sessionToken"],
    )

    channel_name_b = f"rmng-v1-{device_b.node_thing_name}"
    with pytest.raises(ClientError) as exc_info:
        kvs_client.describe_signaling_channel(ChannelName=channel_name_b)
    assert exc_info.value.response["Error"]["Code"] == "AccessDeniedException"


# ---------------------------------------------------------------------------
# Negative test: device without KVS capability
# ---------------------------------------------------------------------------

def test_device_without_kvs_capability_denied(bare_device):
    """Device registered without capabilities=["kvs"] cannot get KVS credentials.

    The IoT Credential Provider should return non-200 because the rmng-node-video-policy
    is not attached to the device's certificate.
    """
    # Register a device WITHOUT capabilities (no rmng-node-video-policy attached)
    device = bare_device(thing_name=f"test-no-kvs-cap-{uuid.uuid4().hex[:8]}", capabilities=None)  # No KVS capability

    try:
        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as cert_file:
            cert_file.write(device.node_cert)
            cert_path = cert_file.name

        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as key_file:
            key_file.write(device.node_key)
            key_path = key_file.name

        try:
            url = f"https://{CREDENTIAL_PROVIDER_ENDPOINT}/role-aliases/{DEVICE_VIDEO_ROLE_ALIAS}/credentials"
            headers = {"x-amzn-iot-thingname": device.node_thing_name}

            response = requests.get(
                url,
                headers=headers,
                cert=(cert_path, key_path),
                verify=True,
            )

            # Should fail — device doesn't have rmng-node-video-policy attached
            assert response.status_code != 200, (
                f"Expected credential provider to deny access for device without KVS capability, "
                f"but got status {response.status_code}"
            )
        finally:
            os.unlink(cert_path)
            os.unlink(key_path)
    finally:
        device.destroy_test_node()


# ---------------------------------------------------------------------------
# User flow tests
# ---------------------------------------------------------------------------

def _user_kvs_client(user, group_id, node_id):
    """Assume the per-node KVS role for the given user and return a boto3 KVS client."""
    data = json.dumps({"services": ["kvs"]})
    path = f"/v1/groups/{group_id}/nodes/{node_id}/assumed-roles"
    response = user.make_api_request('POST', path, data=data)
    assert response.status_code == 200, f"assume_role failed: {response.status_code} {response.text}"
    assumed = response.json()
    return boto3.client(
        "kinesisvideo",
        region_name=REGION,
        aws_access_key_id=assumed["access_key"],
        aws_secret_access_key=assumed["secret_key"],
        aws_session_token=assumed["session_token"],
    )


def test_user_describe_mapped_channel(kvs_device):
    """User with mapping can describe the mapped device's signaling channel."""
    device, group_id, user, user_group_api = kvs_device

    kvs_client = _user_kvs_client(user, group_id, device.node_thing_name)
    channel_name = f"rmng-v1-{device.node_thing_name}"
    response = kvs_client.describe_signaling_channel(ChannelName=channel_name)
    assert response["ChannelInfo"]["ChannelName"] == channel_name


def test_user_unmapped_device_denied(kvs_device, session_valid_device_rsa):
    """User cannot even assume role for an unmapped device — backend returns 403."""
    _, group_id, user, user_group_api = kvs_device
    device_b = session_valid_device_rsa

    data = json.dumps({"services": ["kvs"]})
    path = f"/v1/groups/{group_id}/nodes/{device_b.node_thing_name}/assumed-roles"
    response = user.make_api_request('POST', path, data=data)
    assert response.status_code == 403, (
        f"Expected 403 for unmapped node, got {response.status_code}: {response.text}"
    )
