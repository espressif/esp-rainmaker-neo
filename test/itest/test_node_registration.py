# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from py_sdk.test_device import Device, generate_key_and_cert, validate_tags, split_combined_cert_pem
from test.itest.conftest import (
    connect_device_with_retry,
    request_from_cloud,
    REGION,
    CA_CERT,
    IOT_ENDPOINT,
    DEBUG,
)
import boto3
import csv
import datetime
import io
import time
import os
import pytest
import requests


def test_register_node_basic(super_admin_user, test_device_new):
    """Test basic node registration without tags or admin group."""
    # Register the node
    result = super_admin_user.register_node(test_device_new)
    assert result is not False, "Node registration failed"

    # Verify the node can connect
    assert test_device_new.connect(), "Registered device failed to connect"

def test_register_node_with_admin_group(super_admin_user, test_device_new):
    """Test node registration with an admin group."""

    admin_group_name = "test_admin_group"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        # Register the node with an admin group
        result = super_admin_user.register_node(test_device_new, admin_group_names=[admin_group_name])
        assert result is not False, "Node registration with admin group failed"

        # Verify the node is in the admin group using IoT Core APIs
        max_retries = 2
        retry_delay = 1  # seconds
        success = False

        for attempt in range(max_retries):
            try:
                response = iot_client.list_things_in_thing_group(thingGroupName=admin_group_name)
                if test_device_new.node_thing_name in response['things']:
                    success = True
                    break
                if attempt < max_retries - 1:
                    time.sleep(retry_delay * (2 ** attempt))
            except Exception as e:
                print(f"Error checking thing group membership (attempt {attempt + 1}): {str(e)}")
                if attempt < max_retries - 1:
                    time.sleep(retry_delay * (2 ** attempt))

        assert success, f"Device not found in IoT Core thing group after {max_retries} attempts"

        # Connect the device with retry
        assert connect_device_with_retry(test_device_new), "Registered device failed to connect after retries"

    finally:
        try:
            iot_client.remove_thing_from_thing_group(
                thingGroupName=admin_group_name,
                thingName=test_device_new.node_thing_name
            )
        except Exception as e:
                print(f"Error removing thing from group during cleanup: {str(e)}")

def test_register_node_with_parent_group(super_admin_user, test_device_new):
    """Test node registration with admin group under a parent group.
    Verifies that both parent and child groups are created and the node is placed in the child group.
    """
    parent_group = f"itest_parent_{os.urandom(4).hex()}"
    child_group = f"itest_child_{os.urandom(4).hex()}"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        result = super_admin_user.register_node(
            test_device_new,
            admin_group_names=[child_group],
            admin_parent_group_name=parent_group,
        )
        assert result is not False, "Node registration with parent group failed"

        # Verify parent group exists
        parent_desc = iot_client.describe_thing_group(thingGroupName=parent_group)
        assert parent_desc['thingGroupName'] == parent_group

        # Verify child group exists under the correct parent
        child_desc = iot_client.describe_thing_group(thingGroupName=child_group)
        assert child_desc['thingGroupName'] == child_group
        assert child_desc['thingGroupMetadata']['parentGroupName'] == parent_group

        # Verify node is in the child group (allow time for eventual consistency)
        time.sleep(2)
        things = iot_client.list_things_in_thing_group(thingGroupName=child_group)
        assert test_device_new.node_thing_name in things['things'], \
            f"Node not found in child group '{child_group}'"

    finally:
        # Cleanup: remove thing from group, delete child then parent
        for group in [child_group, parent_group]:
            try:
                iot_client.remove_thing_from_thing_group(
                    thingGroupName=group, thingName=test_device_new.node_thing_name
                )
            except Exception:
                pass
            try:
                iot_client.delete_thing_group(thingGroupName=group)
            except Exception:
                pass


def test_register_node_parent_group_mismatch(super_admin_user, test_device_new):
    """Test that registration fails when a group already exists under a different parent.
    Creates child under parentA, then attempts to register under parentB — should fail.
    """
    parent_a = f"itest_parentA_{os.urandom(4).hex()}"
    parent_b = f"itest_parentB_{os.urandom(4).hex()}"
    child_group = f"itest_mismatch_{os.urandom(4).hex()}"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        # Create parentA and child under it
        iot_client.create_thing_group(thingGroupName=parent_a)
        iot_client.create_thing_group(thingGroupName=child_group, parentGroupName=parent_a)

        # Attempt to register node with child under parentB — should fail
        result = super_admin_user.register_node(
            test_device_new,
            admin_group_names=[child_group],
            admin_parent_group_name=parent_b,
        )
        assert result is False, "Registration should have failed due to parent group mismatch"

    finally:
        for group in [child_group, parent_a, parent_b]:
            try:
                iot_client.delete_thing_group(thingGroupName=group)
            except Exception:
                pass


def test_register_node_parent_exists_child_created(super_admin_user, test_device_new):
    """Test that when a parent group already exists, the child is created under it."""
    parent_group = f"itest_pexist_{os.urandom(4).hex()}"
    child_group = f"itest_cnew_{os.urandom(4).hex()}"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        # Pre-create the parent group
        iot_client.create_thing_group(thingGroupName=parent_group)

        result = super_admin_user.register_node(
            test_device_new,
            admin_group_names=[child_group],
            admin_parent_group_name=parent_group,
        )
        assert result is not False, "Node registration failed when parent already exists"

        # Verify child was created under the existing parent
        child_desc = iot_client.describe_thing_group(thingGroupName=child_group)
        assert child_desc['thingGroupMetadata']['parentGroupName'] == parent_group

        # Verify node is in the child group (with retry for IoT propagation)
        for attempt in range(3):
            things = iot_client.list_things_in_thing_group(thingGroupName=child_group)
            if test_device_new.node_thing_name in things['things']:
                break
            time.sleep(2 ** attempt)
        assert test_device_new.node_thing_name in things['things'], \
            f"Node not found in child group after retries"

    finally:
        for group in [child_group, parent_group]:
            try:
                iot_client.remove_thing_from_thing_group(
                    thingGroupName=group, thingName=test_device_new.node_thing_name
                )
            except Exception:
                pass
            try:
                iot_client.delete_thing_group(thingGroupName=group)
            except Exception:
                pass


def test_register_node_parent_child_both_exist(super_admin_user, test_device_new):
    """Test that when both parent and child already exist in the correct hierarchy, registration succeeds."""
    parent_group = f"itest_pboth_{os.urandom(4).hex()}"
    child_group = f"itest_cboth_{os.urandom(4).hex()}"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        # Pre-create parent and child in correct hierarchy
        iot_client.create_thing_group(thingGroupName=parent_group)
        iot_client.create_thing_group(thingGroupName=child_group, parentGroupName=parent_group)

        result = super_admin_user.register_node(
            test_device_new,
            admin_group_names=[child_group],
            admin_parent_group_name=parent_group,
        )
        assert result is not False, "Node registration failed when parent+child already exist"

        # Verify node is in the child group (with retry for IoT propagation)
        for attempt in range(3):
            things = iot_client.list_things_in_thing_group(thingGroupName=child_group)
            if test_device_new.node_thing_name in things['things']:
                break
            time.sleep(2 ** attempt)
        assert test_device_new.node_thing_name in things['things'], \
            f"Node not found in child group after retries"

    finally:
        for group in [child_group, parent_group]:
            try:
                iot_client.remove_thing_from_thing_group(
                    thingGroupName=group, thingName=test_device_new.node_thing_name
                )
            except Exception:
                pass
            try:
                iot_client.delete_thing_group(thingGroupName=group)
            except Exception:
                pass


def test_register_node_standalone_child_fails_with_parent(super_admin_user, test_device_new):
    """Test that when a child exists as a standalone (no parent) group,
    requesting it under a parent fails with a mismatch error."""
    parent_group = f"itest_pnew_{os.urandom(4).hex()}"
    child_group = f"itest_calone_{os.urandom(4).hex()}"
    iot_client = boto3.client('iot', region_name=REGION)

    try:
        # Pre-create child as a standalone group (no parent)
        iot_client.create_thing_group(thingGroupName=child_group)

        # Attempt to register with child under a parent — should fail (parent mismatch)
        result = super_admin_user.register_node(
            test_device_new,
            admin_group_names=[child_group],
            admin_parent_group_name=parent_group,
        )
        assert result is False, "Registration should have failed: standalone child cannot be moved under a parent"

    finally:
        for group in [child_group, parent_group]:
            try:
                iot_client.delete_thing_group(thingGroupName=group)
            except Exception:
                pass


def test_bulk_register_common_admin_group_only(super_admin_user, node_csv_uploader):
    """
    Test that bulk registration applies common admin_group_names from the API body
    when the CSV has no per-node admin_groups column.
    This was previously broken: common groups were stored in DB but never passed
    to RegisterNodeInRmng.
    """
    admin_group_name = "BulkCommonGroupTest"
    iot_client = boto3.client('iot', region_name=REGION)
    registered_devices = []

    try:
        nodes = [
            {
                "node_id": f"node_{os.urandom(8).hex()}",
                "city": "Munich",
                "type": "Light",
                "model": "Led",
                "subtype": "RGB",
                "key_type": "ec"
            },
        ]

        # Upload CSV without admin_groups column (default CSV has no admin_groups)
        s3_path, generated_certs = node_csv_uploader(super_admin_user, nodes, return_certs=True)
        assert s3_path.startswith("s3://"), f"S3 path not returned: {s3_path}"

        # Bulk register with only common admin_group_names (no per-node groups in CSV)
        response = super_admin_user.bulk_register_nodes(s3_path, admin_group_names=[admin_group_name])
        assert response is not None, "Bulk register API did not return a response"
        request_id = response.get("request_id")
        assert request_id, "No request_id returned from bulk register"

        # Poll for completion
        max_retries = 10
        status = None
        for attempt in range(max_retries):
            time.sleep(10)
            status = super_admin_user.get_bulk_register_status(request_id)
            if status is None:
                pytest.fail(f"Got None response on attempt {attempt + 1}")
            if status.get("request_id") and status.get("status") in ("completed", "failed"):
                print(f"Bulk register reached terminal state on attempt {attempt + 1}: {status}")
                break
            print(f"Attempt {attempt + 1}: status={status.get('status')}")
        else:
            pytest.fail(f"Bulk register did not reach terminal state after {max_retries} attempts. Last: {status}")

        assert status.get("status") == "completed", f"Bulk register did not complete successfully: {status}"
        assert status.get("success_count") == len(nodes), \
            f"Expected {len(nodes)} successes, got {status.get('success_count')}"

        # Create Device objects for cleanup
        for node in nodes:
            device = Device(
                node['node_id'],
                generated_certs[node['node_id']]['private_key'],
                generated_certs[node['node_id']]['cert'],
                CA_CERT,
                IOT_ENDPOINT,
                REGION,
                DEBUG
            )
            registered_devices.append(device)

        # Verify each node is in the common admin group
        for device in registered_devices:
            max_group_retries = 3
            found = False
            for attempt in range(max_group_retries):
                try:
                    response = iot_client.list_things_in_thing_group(thingGroupName=admin_group_name)
                    if device.node_thing_name in response['things']:
                        found = True
                        break
                except Exception as e:
                    print(f"Error checking thing group (attempt {attempt + 1}): {e}")
                if attempt < max_group_retries - 1:
                    time.sleep(2 ** attempt)

            assert found, \
                f"Node {device.node_thing_name} not found in admin group '{admin_group_name}' " \
                f"after {max_group_retries} attempts"
            print(f"Verified {device.node_thing_name} is in group '{admin_group_name}'")

    finally:
        for device in registered_devices:
            try:
                device.destroy_test_node()
                print(f"Cleaned up node: {device.node_thing_name}")
            except Exception as e:
                print(f"Warning: Failed to clean up {device.node_thing_name}: {e}")


def test_generate_and_upload_node_csv(super_admin_user, node_csv_uploader):
    """
    Generate node registration CSV with real certificates and upload it to the API.
    Combines CSV generation with file upload functionality.
    """
    nodes = [
        {
            "node_id": f"node_{os.urandom(8).hex()}",
            "city": "Amsterdam",
            "type": "Light",
            "model": "Led",
            "subtype": "RGB",
            "key_type": "ec"
        },
        {
            "node_id": f"node_{os.urandom(8).hex()}",
            "city": "Barcelona",
            "type": "Switch",
            "model": "basic",
            "subtype": "",
            "key_type": "rsa"
        }
    ]
    s3_path = node_csv_uploader(super_admin_user, nodes)
    assert s3_path.startswith("s3://"), f"S3 path not returned: {s3_path}"
    print(f"Test completed successfully! S3 location: {s3_path}")

def test_list_registration_jobs(super_admin_user, node_csv_uploader):
    """Test the list registration jobs endpoint."""
    # First verify listing works (may already have jobs from previous runs)
    initial_response = super_admin_user.list_registration_jobs()
    assert initial_response is not None, "List registration jobs returned None"
    assert "jobs" in initial_response, "Response missing 'jobs' key"
    initial_count = len(initial_response["jobs"])

    # Create a bulk registration to ensure at least one job exists
    nodes = [
        {
            "node_id": f"node_{os.urandom(8).hex()}",
            "key_type": "ec",
            "city": "TestCity",
            "type": "Light",
            "model": "Basic",
            "subtype": "",
        },
    ]
    s3_path, generated_certs = node_csv_uploader(super_admin_user, nodes, return_certs=True)
    response = super_admin_user.bulk_register_nodes(s3_path, admin_group_names=["ListTestGroup"], tags=["test:list"])
    assert response is not None, "Bulk register failed"
    request_id = response.get("request_id")
    assert request_id, "No request_id returned"

    # Wait for the job to be created in DB
    time.sleep(5)

    # Search all pages for our job (env may already have >20 jobs)
    our_job = None
    next_key = None
    total_seen = 0
    while True:
        list_response = super_admin_user.list_registration_jobs(page_size=20, start_key=next_key)
        assert list_response is not None, "List registration jobs returned None"
        jobs = list_response["jobs"]
        total_seen += len(jobs)
        our_job = next((j for j in jobs if j.get("request_id") == request_id), None)
        if our_job or not list_response.get("next_key"):
            break
        next_key = list_response["next_key"]

    assert our_job is not None, f"Job {request_id} not found after searching {total_seen} jobs"
    assert our_job.get("admin_group_names") == ["ListTestGroup"], f"admin_group_names mismatch: {our_job}"
    assert our_job.get("tags") == ["test:list"], f"tags mismatch: {our_job}"

    # Test page_size parameter — verify next_key is returned when more results exist
    limit_response = super_admin_user.list_registration_jobs(page_size=1)
    assert limit_response is not None
    assert len(limit_response["jobs"]) == 1, f"Expected 1 job with page_size=1, got {len(limit_response['jobs'])}"
    if total_seen > 1:
        assert limit_response.get("next_key"), "Expected next_key when there are more jobs"

    # Clean up
    for node in nodes:
        try:
            device = Device(
                node['node_id'],
                generated_certs[node['node_id']]['private_key'],
                generated_certs[node['node_id']]['cert'],
                CA_CERT, IOT_ENDPOINT, REGION, DEBUG
            )
            device.destroy_test_node()
        except Exception as e:
            print(f"Warning: Failed to cleanup node {node['node_id']}: {e}")


def test_bulk_register_nodes_and_status(super_admin_user, node_csv_uploader):
    """
    Test the bulk_register_nodes and get_bulk_register_status methods.
    """
    registered_devices = []

    try:
        # Prepare nodes for CSV
        nodes = [
            {
                "node_id": f"node_{os.urandom(8).hex()}",
                "city": "Berlin",
                "type": "Light",
                "model": "Led",
                "subtype": "RGB",
                "key_type": "ec"
            },
            {
                "node_id": f"node_{os.urandom(8).hex()}",
                "city": "Paris",
                "type": "Switch",
                "model": "basic",
                "subtype": "",
                "key_type": "rsa"
            }
        ]

        # Generate and upload CSV, get S3 path and certificates
        s3_path, generated_certs = node_csv_uploader(super_admin_user, nodes, return_certs=True)
        assert s3_path.startswith("s3://"), f"S3 path not returned: {s3_path}"

        # Call bulk_register_nodes
        response = super_admin_user.bulk_register_nodes(s3_path, admin_group_names=["BulkTestGroup"], tags=["env:test"])
        assert response is not None, "Bulk register API did not return a response"
        request_id = response.get("request_id")
        assert request_id, "No request_id returned from bulk register"

        # Poll for status with a maximum retry limit to prevent infinite loops
        import time
        max_initial_retries = 10  # Maximum retries to get a valid response
        status = None

        for attempt in range(max_initial_retries):
            time.sleep(10)
            status = super_admin_user.get_bulk_register_status(request_id)

            # If status is None, fail immediately
            if status is None:
                pytest.fail(f"Got None response on attempt {attempt + 1}")

            # Check if we got a valid non-empty response with real data
            if status.get("request_id") and status.get("request_id") != "" and status.get("status") != "":
                print(f"Got valid status response on attempt {attempt + 1}: {status}")
                break

            print(f"Attempt {attempt + 1}: Got empty status response: {status}")
        else:
            # If we exhausted all retries without getting a valid response
            pytest.fail(f"Failed to get valid status response after {max_initial_retries} attempts. Last response: {status}")

        # Now do additional retries to check for terminal state
        max_terminal_retries = 2
        for attempt in range(max_terminal_retries):
            assert status.get("request_id") == request_id, f"Status response does not match request_id: {status}"
            print(f"Bulk register status (attempt {attempt+1}): {status}")

            if status.get("status") in ("completed"):  # Acceptable terminal states
                # Check if all nodes were successfully registered
                total_nodes = status.get("total_nodes", 0)
                success_count = status.get("success_count", 0)
                assert success_count == total_nodes, f"Not all nodes were registered successfully. Total: {total_nodes}, Success: {success_count}"
                break

            time.sleep(2)
            status = super_admin_user.get_bulk_register_status(request_id)
        else:
            print(f"Warning: Bulk register did not reach terminal state after {max_terminal_retries} attempts. Last status: {status}")

        # Create Device objects for connectivity testing and cleanup
        print("Creating device objects for connectivity testing...")
        for node in nodes:
            device = Device(
                node['node_id'],
                generated_certs[node['node_id']]['private_key'],
                generated_certs[node['node_id']]['cert'],
                CA_CERT,
                IOT_ENDPOINT,
                REGION,
                DEBUG
            )
            registered_devices.append(device)

        # Verify node connectivity and shadow tags
        print("Verifying node connectivity and shadow tags...")
        from py_sdk.test_device import validate_tags

        for device in registered_devices:
            # Try to connect with retry
            connected = connect_device_with_retry(device)
            if connected:
                print(f"Node {device.node_thing_name} is connected")

                # Verify shadow tags
                # Get the indexed shadow to check tags
                ishadow_name = "iparams"
                device.shadow_connect([ishadow_name])
                ishadow_data = device.get_shadow(ishadow_name)

                assert ishadow_data is not None, f"Failed to get indexed shadow for {device.node_thing_name}"

                # Create expected tags dictionary
                node_id = device.node_thing_name
                matching_node = next((n for n in nodes if n["node_id"] == node_id), None)
                expected_tags = {"env": "test"}

                if matching_node:
                    expected_tags["city"] = matching_node["city"]
                    expected_tags["type"] = matching_node["type"]
                    expected_tags["model"] = matching_node["model"]
                    if matching_node["subtype"]:
                        expected_tags["subtype"] = matching_node["subtype"]

                # Use validate_tags to verify shadow tags
                assert validate_tags(ishadow_data, expected_tags), f"Shadow tags validation failed for {node_id}"
                print(f"Successfully validated tags for {node_id}")

                # Disconnect after verification
                device.disconnect()
            else:
                print(f"Warning: Node {device.node_thing_name} did not connect after retries")

    finally:
        # Clean up resources
        print("Cleaning up resources...")

        # Delete nodes using destroy_test_node
        for device in registered_devices:
            try:
                device.destroy_test_node()
                print(f"Deleted node: {device.node_thing_name}")
            except Exception as e:
                print(f"Warning: Failed to delete node {device.node_thing_name}: {e}")


def test_failed_nodes_presigned_csv_retry(super_admin_user):
    """Submit a bulk registration with one valid cert and one malformed PEM,
    then verify the failure-visibility surface and the eager failed-rows CSV.

    Validates the parts of the failed-nodes flow that the unit tests can't:
      - real DDB query of the node_reg_failed_nodes table behind GET .../failed-nodes
      - the container's eager failed-rows CSV write to S3 (one PutObject per job)
      - the status endpoint mints a working presigned GET URL for that CSV
      - the downloaded CSV round-trips: re-uploaded as-is (with the PEM fixed)
        it registers cleanly via per-step idempotency
    """
    good_node_id = f"goodnode_{os.urandom(6).hex()}"
    bad_node_id = f"badnode_{os.urandom(6).hex()}"

    # Build a CSV with one valid row + one bad PEM. We construct it inline
    # rather than going through generate_and_upload_node_csv because that
    # helper only emits valid certs; the whole point of this test is to
    # exercise the failed-nodes path on a real partial-failure run.
    good_key, good_cert_combined = generate_key_and_cert(thing_name=good_node_id, key_type="ec")
    good_cert_only, _ = split_combined_cert_pem(good_cert_combined)

    original_header = ["node_id", "certs", "city", "type", "model", "subtype"]
    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(original_header)
    writer.writerow([good_node_id, good_cert_only, "Madrid", "Light", "Led", "RGB"])
    writer.writerow([bad_node_id, "this-is-not-a-valid-pem", "Madrid", "Light", "Led", "RGB"])
    csv_body = buf.getvalue().encode("utf-8")

    test_data_dir = os.path.join("build", "test")
    os.makedirs(test_data_dir, exist_ok=True)
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    csv_path = os.path.join(test_data_dir, f"failed_nodes_itest_{timestamp}.csv")

    s3_path = None
    request_id = None
    good_device = None
    bad_device = None
    fixed_csv_path = None
    fixed_s3_path = None
    try:
        with open(csv_path, "wb") as f:
            f.write(csv_body)
        success, s3_path = super_admin_user.upload_file(csv_path, "node_cert")
        assert success, f"Failed to upload CSV: {s3_path}"

        # Trigger the bulk registration. Expect one success and one failure.
        resp = super_admin_user.bulk_register_nodes(s3_path)
        assert resp is not None, "Bulk register API returned None"
        request_id = resp["request_id"]

        # Poll until completed.
        max_attempts = 20
        status = None
        for attempt in range(max_attempts):
            time.sleep(5)
            status = super_admin_user.get_bulk_register_status(request_id)
            if status and status.get("status") == "completed":
                break
        else:
            pytest.fail(f"Bulk job {request_id} did not complete after polling: {status}")

        assert status.get("total_nodes") == 2
        assert status.get("success_count") == 1
        assert status.get("failed_count") == 1

        # Failed-nodes JSON list returns the bad row with a real reason.
        failed_resp = super_admin_user.list_failed_nodes(request_id, job_type="register")
        assert failed_resp is not None, "list_failed_nodes returned None"
        failed_entries = failed_resp.get("failed_nodes", [])
        assert len(failed_entries) == 1, f"Expected 1 failure, got: {failed_entries}"
        assert failed_entries[0]["node_id"] == bad_node_id
        assert failed_entries[0]["reason"], "Failure reason should be non-empty"

        # The status response carries the presigned GET URL for the eager
        # failed-rows CSV the container wrote to S3 at end-of-job. There is no
        # on-demand export endpoint; the client fetches the bytes straight from
        # S3 via this short-lived URL.
        download_url = status.get("failed_file_download_url")
        assert download_url, f"status missing failed_file_download_url: {status}"
        assert "X-Amz-Signature" in download_url, \
            f"failed_file_download_url is not a presigned S3 URL: {download_url}"

        csv_resp = requests.get(download_url, timeout=30)
        assert csv_resp.status_code == 200, \
            f"presigned CSV download failed: {csv_resp.status_code} {csv_resp.text}"
        retry_rows = list(csv.reader(io.StringIO(csv_resp.text)))

        # Header matches the original input verbatim (original column order),
        # and only the failed row is present -- certs and all columns intact.
        assert retry_rows[0] == original_header, \
            f"failed-rows CSV header should match original input shape: {retry_rows[0]}"
        data_rows = retry_rows[1:]
        assert len(data_rows) == 1, f"Expected 1 failed row, got {len(data_rows)}: {data_rows}"
        assert data_rows[0][0] == bad_node_id
        certs_idx = original_header.index("certs")
        assert data_rows[0][certs_idx] == "this-is-not-a-valid-pem", \
            "failed-rows CSV should preserve the original (bad) cert column verbatim"

        # Capture the good node's device for cleanup at the end.
        good_device = Device(good_node_id, good_key, good_cert_combined,
                             CA_CERT, IOT_ENDPOINT, REGION, DEBUG)

        # Close the retry loop: operator generates a valid cert for the
        # previously-failed node, swaps it into the downloaded failed-rows CSV
        # (preserving every other column -- the whole point of the cert-bearing
        # CSV shape), re-uploads, and registers. The second job should succeed
        # cleanly with no failures.
        bad_key, bad_cert_combined = generate_key_and_cert(thing_name=bad_node_id, key_type="ec")
        bad_cert_only, _ = split_combined_cert_pem(bad_cert_combined)

        fixed_row = list(data_rows[0])
        fixed_row[certs_idx] = bad_cert_only

        fixed_buf = io.StringIO()
        fixed_writer = csv.writer(fixed_buf)
        fixed_writer.writerow(retry_rows[0])  # original header
        fixed_writer.writerow(fixed_row)
        fixed_body = fixed_buf.getvalue().encode("utf-8")

        fixed_csv_path = os.path.join(test_data_dir, f"failed_nodes_itest_retry_{timestamp}.csv")
        with open(fixed_csv_path, "wb") as f:
            f.write(fixed_body)
        fixed_success, fixed_s3_path = super_admin_user.upload_file(fixed_csv_path, "node_cert")
        assert fixed_success, f"Failed to upload corrected retry CSV: {fixed_s3_path}"

        retry_resp = super_admin_user.bulk_register_nodes(fixed_s3_path)
        assert retry_resp is not None, "Retry bulk_register API returned None"
        retry_request_id = retry_resp["request_id"]

        retry_status = None
        for attempt in range(max_attempts):
            time.sleep(5)
            retry_status = super_admin_user.get_bulk_register_status(retry_request_id)
            if retry_status and retry_status.get("status") == "completed":
                break
        else:
            pytest.fail(f"Retry job {retry_request_id} did not complete after polling: {retry_status}")

        assert retry_status.get("total_nodes") == 1, retry_status
        assert retry_status.get("success_count") == 1, retry_status
        assert retry_status.get("failed_count") == 0, retry_status

        # Now-registered bad node needs Thing/cert cleanup in the finally
        # block.
        bad_device = Device(bad_node_id, bad_key, bad_cert_combined,
                            CA_CERT, IOT_ENDPOINT, REGION, DEBUG)

    finally:
        for path in (csv_path, fixed_csv_path):
            if path and os.path.exists(path):
                try:
                    os.remove(path)
                except Exception:
                    pass
        # Clean up the IoT-side artefacts. good_device was registered by
        # the first job; bad_device by the retry job once we swapped in a
        # valid PEM. Either may be None if the test failed before they
        # were captured.
        for device, label in ((good_device, good_node_id), (bad_device, bad_node_id)):
            if device is not None:
                try:
                    device.destroy_test_node()
                except Exception as e:
                    print(f"Warning: failed to clean up {label}: {e}")
        for path in (s3_path, fixed_s3_path):
            if path:
                try:
                    bucket, key = path.replace("s3://", "").split("/", 1)
                    boto3.client("s3", region_name=REGION).delete_object(Bucket=bucket, Key=key)
                except Exception as e:
                    print(f"Warning: failed to delete S3 object {path}: {e}")


def test_groupless_node_bootsequence(bare_device, super_admin_user):
    """A registered but unassociated node must complete its boot sequence.

    Before its first association a node belongs to no group, and no step here
    depends on one: it learns it has no group, reaches its unicast params topic,
    syncs its clock, uploads its config, and carries admin tags in the
    group-independent iparams shadow. A group-less node blocked at any of these
    would never finish coming online.
    """
    device = bare_device()
    assert connect_device_with_retry(device, max_retries=3, base_delay=2), \
        "Group-less device failed to connect"
    node_id = device.node_thing_name

    # getGroupInfo must be answered, and the answer must carry no group.
    group_info = request_from_cloud(device, "getGroupInfo")
    assert group_info is not None, "Cloud never answered getGroupInfo for a group-less node"
    assert "pgrp" not in group_info, \
        f"Expected no group for an unassociated node, got {group_info}"

    unicast_topic = f"rainmaker/nodes/{node_id}/user/params-/params"
    assert device.subscribe(topic=unicast_topic), \
        f"Group-less node could not subscribe to {unicast_topic}"

    time_sync = request_from_cloud(device, "getTimeSync")
    assert time_sync is not None, \
        "No getTimeSync answer — the session did not survive the unicast subscribe"
    assert abs(time_sync["time"] - time.time() * 1000) < 5 * 60 * 1000, \
        f"Server time {time_sync['time']} deviates more than 5 minutes from test host time"

    # Config is stored per node, not per group. set_node_config consumes its own
    # from_cloud ack and returns True only on status == "success".
    assert device.set_node_config({"device_type": "light_bulb", "firmware_version": "1.0"}), \
        "Cloud did not accept setNodeConfig for a group-less node"

    # Admin tags live in the iparams shadow, whose name carries no group.
    result = super_admin_user.admin_put_node_tags(node_id, admin_tags={"env": "staging"})
    assert result is not False, "Admin tag write failed for a group-less node"
    tags = super_admin_user.admin_get_node_tags(node_id)
    assert tags["admin"]["env"] == "staging", f"Admin tags not readable back: {tags}"
