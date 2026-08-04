# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Integration tests for S3 device file storage.

Tests device file operations (upload, download, list, delete) using IoT Credential Provider
credentials, and user file operations (list, download, delete) using assume-role credentials.
"""
import json
import pytest
import uuid

from botocore.exceptions import ClientError

from py_sdk.test_device import Device, generate_key_and_cert
from py_sdk.test_group import Group
from test.itest.conftest import (
    node_registrar_identity,
    CREDENTIAL_PROVIDER_ENDPOINT,
    DEVICE_FILE_ROLE_ALIAS,
    FILES_BUCKET_NAME,
    REGION,
    CA_CERT,
    IOT_ENDPOINT,
    DEBUG,
    connect_device_with_retry,
)


NODE_DATA_PREFIX = "node-data"


def _s3_key(thing_name, file_name):
    """Build the full S3 key for a device file."""
    return f"{NODE_DATA_PREFIX}/{thing_name}/{file_name}"


def _cleanup_s3_objects(s3_client, keys):
    """Best-effort cleanup of S3 objects."""
    for key in keys:
        try:
            s3_client.delete_object(Bucket=FILES_BUCKET_NAME, Key=key)
        except Exception:
            pass


# ---------------------------------------------------------------------------
# Device flow tests
# ---------------------------------------------------------------------------

def test_device_upload_file(associated_device):
    """Upload a file using device credentials, verify it exists via ListObjectsV2."""
    device, group_id, user, user_group_api = associated_device
    s3 = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"test-upload-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)

    try:
        s3.put_object(
            Bucket=FILES_BUCKET_NAME, Key=key, Body=b"hello world",
            Metadata={"name": "test-file", "description": "integration test upload"},
        )

        resp = s3.list_objects_v2(
            Bucket=FILES_BUCKET_NAME,
            Prefix=f"{NODE_DATA_PREFIX}/{device.node_thing_name}/",
        )
        listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
        assert key in listed_keys, f"Uploaded key {key} not found in listing"

        # Verify user metadata round-trips via HeadObject
        head = s3.head_object(Bucket=FILES_BUCKET_NAME, Key=key)
        assert head["Metadata"]["name"] == "test-file"
        assert head["Metadata"]["description"] == "integration test upload"
    finally:
        _cleanup_s3_objects(s3, [key])


def test_device_download_file(associated_device):
    """Upload then download a file, verify content matches (round-trip)."""
    device, group_id, user, user_group_api = associated_device
    s3 = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"test-download-{uuid.uuid4().hex[:8]}.bin"
    key = _s3_key(device.node_thing_name, file_name)
    content = f"round-trip-{uuid.uuid4()}".encode()

    try:
        s3.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=content)

        resp = s3.get_object(Bucket=FILES_BUCKET_NAME, Key=key)
        downloaded = resp["Body"].read()
        assert downloaded == content, "Downloaded content does not match uploaded content"
    finally:
        _cleanup_s3_objects(s3, [key])


def test_device_list_files(associated_device):
    """Upload multiple files, list them, verify all appear."""
    device, group_id, user, user_group_api = associated_device
    s3 = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_names = [f"test-list-{i}-{uuid.uuid4().hex[:8]}.txt" for i in range(3)]
    keys = [_s3_key(device.node_thing_name, fn) for fn in file_names]

    try:
        for key in keys:
            s3.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"data")

        resp = s3.list_objects_v2(
            Bucket=FILES_BUCKET_NAME,
            Prefix=f"{NODE_DATA_PREFIX}/{device.node_thing_name}/",
        )
        listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
        for key in keys:
            assert key in listed_keys, f"Key {key} not found in listing"
    finally:
        _cleanup_s3_objects(s3, keys)


def test_device_delete_file(associated_device):
    """Upload then delete a file, verify it no longer appears in listing."""
    device, group_id, user, user_group_api = associated_device
    s3 = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"test-delete-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)

    try:
        s3.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"to-delete")

        s3.delete_object(Bucket=FILES_BUCKET_NAME, Key=key)

        resp = s3.list_objects_v2(
            Bucket=FILES_BUCKET_NAME,
            Prefix=f"{NODE_DATA_PREFIX}/{device.node_thing_name}/",
        )
        listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
        assert key not in listed_keys, f"Deleted key {key} still found in listing"
    finally:
        _cleanup_s3_objects(s3, [key])


def test_device_nested_keys(associated_device):
    """Upload with nested path keys (e.g., logs/2024/data.txt)."""
    device, group_id, user, user_group_api = associated_device
    s3 = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    nested_name = f"logs/2024/data-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, nested_name)

    try:
        s3.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"nested content")

        resp = s3.get_object(Bucket=FILES_BUCKET_NAME, Key=key)
        assert resp["Body"].read() == b"nested content"
    finally:
        _cleanup_s3_objects(s3, [key])


def test_device_cross_device_isolation(associated_device, session_valid_device_rsa):
    """Device A tries to access device B's prefix, expects AccessDenied."""
    device_a, group_id, user, user_group_api = associated_device
    device_b = session_valid_device_rsa

    s3_a = device_a.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    s3_b = device_b.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)

    file_name = f"isolation-{uuid.uuid4().hex[:8]}.txt"
    key_b = _s3_key(device_b.node_thing_name, file_name)

    try:
        # Device B uploads a file to its own prefix
        s3_b.put_object(Bucket=FILES_BUCKET_NAME, Key=key_b, Body=b"device-b-data")

        # Device A tries to read device B's file — should be denied
        with pytest.raises(ClientError) as exc_info:
            s3_a.get_object(Bucket=FILES_BUCKET_NAME, Key=key_b)
        assert exc_info.value.response["Error"]["Code"] == "AccessDenied"

        # Device A tries to delete device B's file — should be denied
        with pytest.raises(ClientError) as exc_info:
            s3_a.delete_object(Bucket=FILES_BUCKET_NAME, Key=key_b)
        assert exc_info.value.response["Error"]["Code"] == "AccessDenied"

        # Device A tries to list device B's prefix — should return empty or be denied
        try:
            resp = s3_a.list_objects_v2(
                Bucket=FILES_BUCKET_NAME,
                Prefix=f"{NODE_DATA_PREFIX}/{device_b.node_thing_name}/",
            )
            listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
            assert key_b not in listed_keys, "Device A should not see device B's files"
        except ClientError as e:
            assert e.response["Error"]["Code"] == "AccessDenied"
    finally:
        _cleanup_s3_objects(s3_b, [key_b])


# ---------------------------------------------------------------------------
# User flow tests
# ---------------------------------------------------------------------------

def test_user_list_device_files(associated_device):
    """User with mapping lists files in device's prefix."""
    device, group_id, user, user_group_api = associated_device
    s3_device = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"user-list-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)

    try:
        s3_device.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"user-visible")

        s3_user = user.get_s3_client(group_id=group_id, node_id=device.node_thing_name)
        resp = s3_user.list_objects_v2(
            Bucket=FILES_BUCKET_NAME,
            Prefix=f"{NODE_DATA_PREFIX}/{device.node_thing_name}/",
        )
        listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
        assert key in listed_keys, f"User could not see device file {key}"
    finally:
        _cleanup_s3_objects(s3_device, [key])


def test_user_download_device_file(associated_device):
    """User with mapping downloads a file from device's prefix."""
    device, group_id, user, user_group_api = associated_device
    s3_device = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"user-download-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)
    content = f"user-download-content-{uuid.uuid4()}".encode()

    try:
        s3_device.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=content)

        s3_user = user.get_s3_client(group_id=group_id, node_id=device.node_thing_name)
        resp = s3_user.get_object(Bucket=FILES_BUCKET_NAME, Key=key)
        downloaded = resp["Body"].read()
        assert downloaded == content, "User downloaded content does not match"
    finally:
        _cleanup_s3_objects(s3_device, [key])


def test_user_delete_device_file(associated_device):
    """User with mapping deletes a file from device's prefix."""
    device, group_id, user, user_group_api = associated_device
    s3_device = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
    file_name = f"user-delete-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)

    try:
        s3_device.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"to-be-deleted")

        s3_user = user.get_s3_client(group_id=group_id, node_id=device.node_thing_name)
        s3_user.delete_object(Bucket=FILES_BUCKET_NAME, Key=key)

        resp = s3_device.list_objects_v2(
            Bucket=FILES_BUCKET_NAME,
            Prefix=f"{NODE_DATA_PREFIX}/{device.node_thing_name}/",
        )
        listed_keys = [obj["Key"] for obj in resp.get("Contents", [])]
        assert key not in listed_keys, f"User-deleted key {key} still found in listing"
    finally:
        _cleanup_s3_objects(s3_device, [key])


def test_user_put_object_denied(associated_device):
    """User cannot upload files — only devices can write to node-data/."""
    device, group_id, user, user_group_api = associated_device
    s3_user = user.get_s3_client(group_id=group_id, node_id=device.node_thing_name)
    file_name = f"user-put-denied-{uuid.uuid4().hex[:8]}.txt"
    key = _s3_key(device.node_thing_name, file_name)

    with pytest.raises(ClientError) as exc_info:
        s3_user.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=b"should-fail")
    assert exc_info.value.response["Error"]["Code"] == "AccessDenied"


def test_user_no_mapping_denied(associated_device, session_valid_device_rsa):
    """User without mapping to a specific device cannot even assume role for it.

    The user has a group (from associated_device) but session_valid_device_rsa
    is NOT in that group, so the backend rejects assume_role for that node.
    """
    _, group_id, user, user_group_api = associated_device
    device = session_valid_device_rsa

    data = json.dumps({"services": ["s3"]})
    path = f"/v1/groups/{group_id}/nodes/{device.node_thing_name}/assumed-roles"
    response = user.make_api_request('POST', path, data=data)
    assert response.status_code == 403, (
        f"Expected 403 for unmapped node, got {response.status_code}: {response.text}"
    )


@pytest.mark.unsafe
def test_user_multiple_devices(test_user1):
    """User with access to multiple devices can access files from each."""
    user = test_user1
    group_api = Group(user)

    # Create two devices and associate them with separate groups
    devices = []
    group_ids = []
    keys_to_clean = []

    try:
        for i in range(2):
            thing_name = f"test-multi-dev-{i}-{uuid.uuid4().hex[:8]}"
            private_key_pem, cert_pem = generate_key_and_cert(thing_name, 'rsa')
            device = Device(thing_name, private_key_pem, cert_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
            device.register_test_node(caller_identity=node_registrar_identity())
            assert connect_device_with_retry(device, max_retries=3, base_delay=2), \
                f"Failed to connect device {thing_name}"

            group_id = group_api.create_group(f"Multi Device Test Group {i}")
            result = user.do_user_node_assoc(device, group_id)
            assert result is None, f"Association failed for {thing_name}: {result}"
            assert device.wait_for_group_info(), f"Device {thing_name} did not receive group info"

            devices.append(device)
            group_ids.append(group_id)

        # Upload a file from each device
        s3_clients = []
        for device in devices:
            s3_dev = device.get_s3_client(CREDENTIAL_PROVIDER_ENDPOINT, DEVICE_FILE_ROLE_ALIAS, REGION)
            s3_clients.append(s3_dev)
            file_name = f"multi-{uuid.uuid4().hex[:8]}.txt"
            key = _s3_key(device.node_thing_name, file_name)
            keys_to_clean.append((s3_dev, key))
            s3_dev.put_object(Bucket=FILES_BUCKET_NAME, Key=key, Body=f"data-{device.node_thing_name}".encode())

        # User assumes a fresh role for each node (services mode is single-node).
        for device, gid, (s3_dev, key) in zip(devices, group_ids, keys_to_clean):
            s3_user = user.get_s3_client(group_id=gid, node_id=device.node_thing_name)
            resp = s3_user.get_object(Bucket=FILES_BUCKET_NAME, Key=key)
            assert resp["Body"].read() is not None, f"User could not download {key}"

    finally:
        # Cleanup files
        for s3_dev, key in keys_to_clean:
            try:
                s3_dev.delete_object(Bucket=FILES_BUCKET_NAME, Key=key)
            except Exception:
                pass
        # Cleanup devices and groups
        for device in devices:
            try:
                device.disconnect()
            except Exception:
                pass
            try:
                device.destroy_test_node()
            except Exception:
                pass
        for gid in group_ids:
            try:
                group_api.delete_group(gid, warn_error=True)
            except Exception:
                pass


# ---------------------------------------------------------------------------
# Negative tests: device without S3 capability
# ---------------------------------------------------------------------------

def test_device_without_s3_capability_denied(bare_device):
    """Device registered without capabilities=["s3"] cannot get S3 credentials.

    The IoT Credential Provider should return 400 because the rmng-node-file-policy
    is not attached to the device's certificate.
    """
    import requests
    import tempfile
    import os

    # Register a device WITHOUT capabilities (no rmng-node-file-policy attached)
    device = bare_device(thing_name=f"test-no-s3-cap-{uuid.uuid4().hex[:8]}", capabilities=None)  # No S3 capability

    try:
        # Try to get S3 credentials from the credential provider
        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as cert_file:
            cert_file.write(device.node_cert)
            cert_path = cert_file.name

        with tempfile.NamedTemporaryFile(mode='w', suffix='.pem', delete=False) as key_file:
            key_file.write(device.node_key)
            key_path = key_file.name

        try:
            url = f"https://{CREDENTIAL_PROVIDER_ENDPOINT}/role-aliases/{DEVICE_FILE_ROLE_ALIAS}/credentials"
            headers = {"x-amzn-iot-thingname": device.node_thing_name}

            response = requests.get(
                url,
                headers=headers,
                cert=(cert_path, key_path),
                verify=True,
            )

            # Should fail — device doesn't have rmng-node-file-policy attached
            assert response.status_code != 200, (
                f"Expected credential provider to deny access for device without S3 capability, "
                f"but got status {response.status_code}"
            )
        finally:
            os.unlink(cert_path)
            os.unlink(key_path)
    finally:
        device.destroy_test_node()


def test_assume_role_foreign_group_path_denied(two_tenants):
    """A must not mint S3 creds for B's node via B's OWN group in the path.

    The own-group + unowned-node path is covered by test_user_no_mapping_denied;
    this covers the variant where both group and node belong to the other tenant,
    so the node∈group check passes and only the caller's group membership denies.
    """
    tenant_a, tenant_b = two_tenants

    data = json.dumps({"services": ["s3"]})
    path = f"/v1/groups/{tenant_b['group_id']}/nodes/{tenant_b['node_id']}/assumed-roles"
    resp = tenant_a["user"].make_api_request("POST", path, data=data)
    assert resp.status_code == 403, (
        f"assume-role for a foreign node via foreign-group path returned "
        f"{resp.status_code}, expected 403. Body: {resp.text}"
    )
    assert "secret_access_key" not in (resp.text or ""), "S3 creds minted for a foreign node"