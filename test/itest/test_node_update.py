# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the bulk node-update flow (Change 4 phases 1 and 2).

These cover only paths that the in-tree Go unit tests can't validate against
real AWS:

- The end-to-end update-jobs flow (Lambda -> ECS RunTask -> container ->
  real IoT/DDB) for tag and admin-group updates.
- The cert-update flow's effect on real IoT cert state and on actual
  device connectivity.

Failed-nodes endpoint integration coverage lives in test_node_registration.py
since it's exercised on a registration job.
"""

import csv
import datetime
import io
import os
import time

import boto3
import pytest

from py_sdk.test_device import Device, generate_key_and_cert, split_combined_cert_pem
from test.itest.conftest import (
    connect_device_with_retry,
    REGION,
    CA_CERT,
    IOT_ENDPOINT,
    DEBUG,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _build_update_csv_bytes(rows, header):
    """Build a CSV payload as bytes from a list of dict rows.

    Each row dict should contain keys matching the header. Missing keys are
    written as empty cells. Returns the CSV body as bytes.
    """
    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(header)
    for row in rows:
        writer.writerow([row.get(col, "") for col in header])
    return buf.getvalue().encode("utf-8")


def _upload_csv_via_user(user, csv_bytes, name_hint):
    """Write a CSV to a temp file, upload it via the user's upload API,
    return the resulting s3:// path. The temp file is cleaned up on the way
    out; the S3 object is left for the test (or fixture) to clean up.
    """
    test_data_dir = os.path.join("build", "test")
    os.makedirs(test_data_dir, exist_ok=True)
    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
    csv_path = os.path.join(test_data_dir, f"{name_hint}_{timestamp}.csv")
    try:
        with open(csv_path, "wb") as f:
            f.write(csv_bytes)
        success, result = user.upload_file(csv_path, "node_cert")
        assert success, f"Upload failed: {result}"
        return result
    finally:
        try:
            if os.path.exists(csv_path):
                os.remove(csv_path)
        except Exception:
            pass


def _delete_s3_object(s3_path):
    """Best-effort delete of an s3:// object; swallow any errors."""
    try:
        bucket, key = s3_path.replace("s3://", "").split("/", 1)
        boto3.client("s3", region_name=REGION).delete_object(Bucket=bucket, Key=key)
    except Exception as e:
        print(f"Cleanup warning: failed to delete {s3_path}: {e}")


def _poll_until_completed(user, request_id, status_fn, max_attempts=20, sleep_seconds=5):
    """Poll the given status_fn(user, request_id) until status == 'completed'.

    Returns the terminal status dict on success; fails the test on timeout.
    """
    last_status = None
    for attempt in range(max_attempts):
        time.sleep(sleep_seconds)
        last_status = status_fn(request_id)
        if last_status is None:
            continue
        if last_status.get("status") == "completed":
            return last_status
    pytest.fail(f"Job {request_id} did not reach 'completed' after "
                f"{max_attempts * sleep_seconds}s; last status: {last_status}")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_update_node_metadata(super_admin_user, node_csv_uploader):
    """Pre-register a node via the bulk-register flow, then run an update job
    that adds new tags and a new admin group; verify both land on the node.

    Validates the parts of the update path that the unit tests can't:
      - the new admin_nodes_update Lambda is wired up correctly under
        /v1/admin/nodes/update-jobs
      - it kicks off the shared Fargate task with JOB_TYPE=update and the
        container actually runs the update path
      - real IoT AddThingToGroup and shadow-tag writes succeed
    """
    node_id = f"updnode_{os.urandom(6).hex()}"
    register_nodes = [{
        "node_id": node_id,
        "city": "London",
        "type": "Light",
        "model": "Led",
        "subtype": "Tunable",
        "key_type": "ec",
    }]
    register_s3_path, certs = node_csv_uploader(super_admin_user, register_nodes, return_certs=True)

    # Pre-register the node so node_details exists and the update path's
    # existence check passes.
    reg_resp = super_admin_user.bulk_register_nodes(register_s3_path, tags=["env:initial"])
    assert reg_resp is not None, "Pre-registration failed"
    _poll_until_completed(super_admin_user, reg_resp["request_id"],
                          super_admin_user.get_bulk_register_status)

    # Build an update CSV with no certs column -- metadata-only update.
    # admin_groups is supplied in the request body (not the per-row CSV column)
    # so the Lambda's CreateAdminGroupIfNotExists creates the group before the
    # container runs; per-row admin_groups would hit AddThingToGroup against a
    # group nothing has created.
    update_group = f"itest_update_grp_{os.urandom(4).hex()}"
    update_csv = _build_update_csv_bytes(
        rows=[{"node_id": node_id, "env": "updated", "ring": "phase1"}],
        header=["node_id", "env", "ring"],
    )
    update_s3_path = _upload_csv_via_user(super_admin_user, update_csv, "node_update")

    update_request_id = None
    iot_client = boto3.client("iot", region_name=REGION)
    try:
        update_resp = super_admin_user.bulk_update_nodes(
            update_s3_path,
            admin_group_names=[update_group],
            tags=["batch:itest"],
        )
        assert update_resp is not None, "Bulk update API returned None"
        update_request_id = update_resp.get("request_id")
        assert update_request_id, "No request_id in bulk_update_nodes response"

        terminal = _poll_until_completed(super_admin_user, update_request_id,
                                          super_admin_user.get_bulk_update_status)
        assert terminal.get("job_type") == "update", f"job_type wrong: {terminal}"
        assert terminal.get("total_nodes") == 1
        assert terminal.get("success_count") == 1
        assert terminal.get("failed_count") == 0

        # Verify the new admin group exists and contains the node.
        # Allow brief eventual-consistency window.
        time.sleep(2)
        things = iot_client.list_things_in_thing_group(thingGroupName=update_group)
        assert node_id in things["things"], \
            f"Node {node_id} not in update group {update_group}"

        # Verify the new tags landed on the indexed shadow by connecting and
        # reading. Reuse the cert generated for registration since we did not
        # rotate it.
        device = Device(node_id, certs[node_id]["private_key"], certs[node_id]["cert"],
                        CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
        try:
            assert connect_device_with_retry(device), \
                f"Device {node_id} could not connect after update"
            ishadow_name = "iparams"
            device.shadow_connect([ishadow_name])
            ishadow = device.get_shadow(ishadow_name)
            assert ishadow is not None, "iparams shadow missing after update"
            from py_sdk.test_device import validate_tags
            assert validate_tags(ishadow, {"env": "updated", "ring": "phase1", "batch": "itest"}), \
                f"Updated tags not visible in shadow: {ishadow}"
        finally:
            try:
                device.disconnect()
            except Exception:
                pass
            try:
                device.destroy_test_node()
            except Exception:
                pass

    finally:
        # Clean up the update group and the update CSV.
        if update_group:
            try:
                iot_client.remove_thing_from_thing_group(
                    thingGroupName=update_group, thingName=node_id)
            except Exception:
                pass
            try:
                iot_client.delete_thing_group(thingGroupName=update_group)
            except Exception:
                pass
        _delete_s3_object(update_s3_path)


def test_update_node_cert(super_admin_user):
    """Pre-register a node with cert A, run an update job with cert B for the
    same node, then verify:
      - cert B is ACTIVE and attached to the Thing
      - cert A is INACTIVE (deactivated) and detached from the Thing
      - a Device built with cert B can actually connect to MQTT

    This is the highest-leverage integration test for phase 2: the cert
    state transitions were entirely mocked at the Go-unit level, and the
    only way to confirm IoT actually behaves as the unit tests assumed is
    to drive the real APIs.
    """
    node_id = f"certupd_{os.urandom(6).hex()}"
    iot_client = boto3.client("iot", region_name=REGION)

    # Cert A -- used for the initial registration.
    key_a_pem, cert_a_pem = generate_key_and_cert(thing_name=node_id, key_type="ec")
    cert_a_only, _ = split_combined_cert_pem(cert_a_pem)

    # Cert B -- the replacement that the update job will install.
    key_b_pem, cert_b_pem = generate_key_and_cert(thing_name=node_id, key_type="ec")
    cert_b_only, _ = split_combined_cert_pem(cert_b_pem)

    # Pre-register via the single-node Lambda path so we can keep cert A's
    # private key around for connectivity assertions.
    device_a = Device(node_id, key_a_pem, cert_a_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
    assert super_admin_user.register_node(device_a), \
        f"Pre-registration of {node_id} with cert A failed"
    assert connect_device_with_retry(device_a), \
        f"Pre-registered device {node_id} could not connect with cert A"
    try:
        device_a.disconnect()
    except Exception:
        pass

    # Capture cert A's IoT cert ID up front so we can verify it ends INACTIVE.
    # ListThingPrincipals is eventually consistent against AttachThingPrincipal —
    # the data plane (MQTT auth, which we just exercised in connect()) sees the
    # attachment immediately, but the control-plane listing can lag by tens of
    # seconds. Poll instead of a fixed sleep so the test isn't tuned to a guess.
    cert_a_id = None
    deadline = time.time() + 30
    while time.time() < deadline:
        principals_before = iot_client.list_thing_principals(thingName=node_id)["principals"]
        for p in principals_before:
            if ":cert/" in p:
                cert_a_id = p.rsplit("/", 1)[-1]
                break
        if cert_a_id:
            break
        time.sleep(2)
    assert cert_a_id, f"No cert principal found on Thing {node_id} before update"

    update_csv = _build_update_csv_bytes(
        rows=[{"node_id": node_id, "certs": cert_b_only}],
        header=["node_id", "certs"],
    )
    update_s3_path = _upload_csv_via_user(super_admin_user, update_csv, "node_cert_update")
    update_request_id = None
    try:
        update_resp = super_admin_user.bulk_update_nodes(update_s3_path)
        assert update_resp is not None, "Bulk update API returned None"
        update_request_id = update_resp.get("request_id")
        assert update_request_id, "No request_id in bulk_update_nodes response"

        terminal = _poll_until_completed(super_admin_user, update_request_id,
                                          super_admin_user.get_bulk_update_status)
        assert terminal.get("success_count") == 1
        assert terminal.get("failed_count") == 0

        # Verify in real IoT: cert B is attached + ACTIVE, cert A is detached
        # and INACTIVE.
        time.sleep(2)
        principals_after = iot_client.list_thing_principals(thingName=node_id)["principals"]
        cert_a_arn = next((p for p in principals_after if p.endswith(cert_a_id)), None)
        assert cert_a_arn is None, \
            f"Cert A still attached after cert update: {principals_after}"
        cert_b_principal = next((p for p in principals_after if ":cert/" in p), None)
        assert cert_b_principal is not None, \
            f"No cert attached to Thing {node_id} after cert update"
        cert_b_id = cert_b_principal.rsplit("/", 1)[-1]

        cert_a_status = iot_client.describe_certificate(certificateId=cert_a_id) \
            ["certificateDescription"]["status"]
        assert cert_a_status == "INACTIVE", \
            f"Cert A status after update should be INACTIVE, got {cert_a_status}"
        cert_b_status = iot_client.describe_certificate(certificateId=cert_b_id) \
            ["certificateDescription"]["status"]
        assert cert_b_status == "ACTIVE", \
            f"Cert B status after update should be ACTIVE, got {cert_b_status}"

        # The device-side proof: a Device built with cert B + key B can actually
        # connect to MQTT. (We do NOT positively assert that cert A fails to
        # connect -- IoT MQTT auth-failure timing is flaky in tests, and the
        # describe_certificate INACTIVE check above already proves the cloud-
        # side state.)
        device_b = Device(node_id, key_b_pem, cert_b_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)
        try:
            assert connect_device_with_retry(device_b), \
                f"Device {node_id} could not connect with new cert B after update"
        finally:
            try:
                device_b.disconnect()
            except Exception:
                pass
            try:
                device_b.destroy_test_node()
            except Exception:
                pass

    finally:
        _delete_s3_object(update_s3_path)
