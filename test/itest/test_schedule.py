# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import os
import time

import boto3
import pytest

from test.itest.conftest import (
    connect_device_with_retry,
    run_shared_group_stages,
    run_shared_subgroup_stages,
)

def test_schedule_functionality(associated_device):
    """Test schedule functionality including setting and getting schedule."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected
    assert device.connect(), "Failed to connect device"

    # API contract uses "schedules": [...] (snake_case). The cloud
    # translates to the firmware key "Schedules" before forwarding over MQTT,
    # so the device sees the array under its expected key.
    schedule_data = [
        {"id": "1", "name": "Morning", "enabled": True, "time": "09:00", "action": "on"},
        {"id": "2", "name": "Evening", "enabled": True, "time": "17:00", "action": "off"},
    ]
    assert test_user1.set_node_schedule(group_id, None, device.node_thing_name, {"schedules": schedule_data})

    # Wait a bit longer for the schedule to be processed
    time.sleep(3)

    # Test getting schedule version
    version = device.get_schedule_version()
    assert version is not None
    assert version > 0

    # Test getting schedule details. The MQTT payload to the device uses
    # "Schedules" (firmware shape) plus a version field.
    schedule = device.get_schedule_details()
    assert schedule is not None
    assert schedule.get("Schedules") == schedule_data, (
        f"Schedules payload did not match: {schedule!r}"
    )

    # Test updating schedule
    updated_schedule = [
        {"id": "1", "name": "Morning", "enabled": True, "time": "10:00", "action": "on"},
        {"id": "2", "name": "Evening", "enabled": True, "time": "18:00", "action": "off"},
    ]
    assert test_user1.set_node_schedule(group_id, None, device.node_thing_name, {"schedules": updated_schedule})

    # Wait a bit longer for the schedule to be processed
    time.sleep(3)

    # Verify schedule version increased
    new_version = device.get_schedule_version()
    assert new_version is not None
    assert new_version > version

    # Verify schedule details updated. MQTT shape still uses "Schedules".
    new_schedule = device.get_schedule_details()
    assert new_schedule.get("Schedules") == updated_schedule, (
        f"Updated schedule payload did not match: {new_schedule!r}"
    )

def test_schedule_proactive_update(associated_device):
    """Test that device receives schedule updates proactively when connected."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected
    assert device.connect(), "Failed to connect device"

    # Clear any existing messages
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Set initial schedule. API uses "schedules" (snake_case); the cloud
    # translates to "Schedules" (firmware shape) for the MQTT publish.
    schedule_data = {
        "schedules": [
            {
                "id": "1",
                "name": "Morning Schedule",
                "enabled": True,
                "time": "09:00",
                "action": "on",
            }
        ]
    }
    assert test_user1.set_node_schedule(group_id, None, device.node_thing_name, schedule_data)

    # Wait for proactive schedule update
    received_schedule = device.wait_for_schedule_update()
    assert received_schedule is not None, "Device should receive schedule update proactively"

    # Verify version field is present
    assert "version" in received_schedule, "Received schedule should contain version field"
    received_version = received_schedule["version"]
    assert received_version is not None, "Version should not be None"

    # MQTT payload uses "Schedules" (firmware shape).
    assert received_schedule.get("Schedules") == schedule_data["schedules"], (
        f"Received schedule did not match sent schedule: {received_schedule!r}"
    )

    # Wait to ensure the version (unix timestamp) increments
    import time
    time.sleep(1)

    # Clear any remaining messages before the second update to avoid race conditions
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Update schedule while device is still connected
    updated_schedule = {
        "schedules": [
            {
                "id": "1",
                "name": "Morning Schedule",
                "enabled": True,
                "time": "10:00",
                "action": "on",
            }
        ]
    }
    assert test_user1.set_node_schedule(group_id, None, device.node_thing_name, updated_schedule)

    # Wait for proactive schedule update
    received_schedule = device.wait_for_schedule_update()
    assert received_schedule is not None, "Device should receive schedule update proactively"

    # Verify version field is present and updated
    assert "version" in received_schedule, "Received schedule should contain version field"
    updated_version = received_schedule["version"]
    assert updated_version is not None, "Version should not be None"
    assert updated_version > received_version, "Version should increase after update"

    # MQTT payload still uses "Schedules".
    assert received_schedule.get("Schedules") == updated_schedule["schedules"], (
        f"Received schedule did not match updated schedule: {received_schedule!r}"
    )

def _schedule_sharing_body(stage, data):
    """
    Test schedule functionality with different sharing mechanisms, group or sub-group.
    Used as a body callback for run_shared_group_stages / run_shared_subgroup_stages.
    """
    device = data["device"]
    group_id = data["group_id"]
    test_user1 = data["test_user1"]
    test_user2 = data["test_user2"]
    subgroup_id = data.get('subgroup_id')

    def user_can_access_schedule(user, group_id, device_thing_name):
        # Ensure device is connected
        device.connect()

        # API contract uses "schedules"; MQTT shape uses "Schedules".
        schedule_data = [
            {"id": "1", "name": "Morning", "enabled": True, "time": "09:00", "action": "on"},
        ]
        assert user.set_node_schedule(group_id, subgroup_id, device_thing_name, {"schedules": schedule_data}), "User should be able to set schedule"

        # Wait for schedule to be processed
        time.sleep(2)

        # Get schedule and verify (MQTT payload uses "Schedules")
        schedule = device.get_schedule_details()
        assert schedule is not None, "User should be able to get schedule"
        assert schedule.get("Schedules") == schedule_data, (
            f"Schedule data should match: {schedule!r}"
        )

    def user_cannot_access_schedule(user, group_id, device_thing_name):
        # Try to set schedule
        schedule_data = [
            {"id": "1", "name": "Morning", "enabled": True, "time": "10:00", "action": "off"},
        ]
        assert not user.set_node_schedule(group_id, subgroup_id, device_thing_name, {"schedules": schedule_data}), "User should not be able to set schedule"

    if stage in ("share_begin",):
        # Test user1 (owner) should be able to set/get schedule
        user_can_access_schedule(test_user1, group_id, device.node_thing_name)
        # Test user2 (no access) should not be able to set schedule
        user_cannot_access_schedule(test_user2, group_id, device.node_thing_name)
    elif stage in ("primary_share", "subgroup_share"):
        # Test user1 (owner) should still be able to set/get schedule
        user_can_access_schedule(test_user1, group_id, device.node_thing_name)
        # Now test_user2 should be able to set/get schedule
        user_can_access_schedule(test_user2, group_id, device.node_thing_name)
    elif stage in ("primary_unshare", "subgroup_unshare"):
        # Ensure device is connected again
        device.connect()

        # Test user1 (owner) should still be able to set/get schedule
        user_can_access_schedule(test_user1, group_id, device.node_thing_name)
        # Test user2 should no longer be able to set schedule
        user_cannot_access_schedule(test_user2, group_id, device.node_thing_name)

def test_schedule_functionality_with_group_sharing(shared_group, subtests):
    run_shared_group_stages(shared_group, subtests, _schedule_sharing_body)

def test_schedule_functionality_with_subgroup_sharing(shared_subgroup, subtests):
    run_shared_subgroup_stages(shared_subgroup, subtests, _schedule_sharing_body)


def test_node_schedule_cross_tenant_write_denied(two_tenants):
    """A cannot write a schedule onto B's node (would drive B's device)."""
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    sched = {"schedules": [{"id": "sc1", "name": "evil", "triggers": [{"m": 0, "h": 0}],
                            "action": {"Light": {"Power": True}}}]}

    assert user_a.set_node_schedule(tenant_a["group_id"], "", tenant_b["node_id"], sched) is False, \
        "Wrote schedule to foreign node via own-group path + foreign nodeId"
    assert user_a.set_node_schedule(tenant_b["group_id"], "", tenant_b["node_id"], sched) is False, \
        "Wrote schedule to foreign node via foreign group path"


# A failed node_details read must not be answered as "you have no schedules".
#
# The getSchedVer reply carries the version the firmware compares against its own copy, so it is a
# staleness marker. Answering version 0 after a read failure makes a device holding real schedules
# conclude the cloud has none, ask for details, receive an empty set, and discard the user's data.
#
# Reproducing this live needs the read to actually fail, so the test injects an explicit IAM Deny on
# just the node_to_cloud Lambda's GetItem against the node_details table, then removes it. The
# injection is a separate inline policy so removal is a delete of something this test created — it
# cannot corrupt the role's real policies. Marked `unsafe` because it mutates a shared role for a
# few seconds: it is excluded from the default `-m "not unsafe"` suite run.
FAULT_POLICY_NAME = "ZZZ-itest-faultinjection-deny-node-details-read"
# TABLE_NAMES["NODE_DETAILS"] is "rmng-nodes" — the logical name and the physical name
# differ, and denying the wrong ARN silently denies nothing.
NODE_DETAILS_TABLE = "rmng-nodes"

# IAM policy changes are not read-your-writes for an already-warm Lambda execution role. The
# initial wait is a floor, not a guarantee — the test polls for the deny to actually bite, so a
# slow propagation costs time instead of failing the run.
IAM_PROPAGATION_S = 45
FAULT_EFFECTIVE_TIMEOUT_S = 180


def _region():
    region = os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION")
    assert region, "AWS_REGION (or AWS_DEFAULT_REGION) must be set to inject the fault"
    return region


def _role_name():
    return f"rmng-node-to-cloud-role-{_region()}"


def _deny_policy(node_id):
    """Deny the node_details read for ONE node, named by the table's partition key.

    The policy attaches to a role every device's node_to_cloud invocation shares, so an
    unconditioned Deny would stop schedule and trigger sync for every device in the region
    while this test runs — including tests on other xdist workers. LeadingKeys narrows it to
    the node under test: a GetItem for any other node_id fails the condition, so the Deny does
    not apply to it.
    """
    account_id = boto3.client("sts").get_caller_identity()["Account"]
    return {
        "Version": "2012-10-17",
        "Statement": [{
            "Effect": "Deny",
            "Action": "dynamodb:GetItem",
            "Resource": f"arn:aws:dynamodb:{_region()}:{account_id}:table/{NODE_DETAILS_TABLE}",
            "Condition": {"ForAllValues:StringEquals": {"dynamodb:LeadingKeys": [node_id]}},
        }],
    }


@pytest.fixture
def deny_node_details_read():
    """Yield a callable that denies the node_details read for one node, and always removes it.

    A callable rather than a plain fixture because the node id is only known inside the test,
    and because the schedule has to be written BEFORE reads start failing.
    """
    iam = boto3.client("iam")
    applied = False

    def apply(node_id):
        nonlocal applied
        iam.put_role_policy(
            RoleName=_role_name(),
            PolicyName=FAULT_POLICY_NAME,
            PolicyDocument=json.dumps(_deny_policy(node_id)),
        )
        applied = True
        time.sleep(IAM_PROPAGATION_S)

    try:
        yield apply
    finally:
        # Always remove it, including on assertion failure — leaving this in place would break
        # that device's schedule and trigger sync for good.
        if applied:
            iam.delete_role_policy(RoleName=_role_name(), PolicyName=FAULT_POLICY_NAME)
            time.sleep(IAM_PROPAGATION_S)


@pytest.mark.unsafe
def test_failed_node_details_read_is_not_answered_as_empty(associated_device, deny_node_details_read):
    """With the read denied, the cloud must not claim version 0 — it must not answer at all."""
    device, group_id, test_user1, _ = associated_device
    assert connect_device_with_retry(device), "failed to connect device"
    device.get_group_info()

    # Give the node a real schedule, so "answered as empty" is distinguishable from "empty".
    schedule_data = [{"id": "1", "name": "Morning", "enabled": True, "time": "09:00", "action": "on"}]
    assert test_user1.set_node_schedule(
        group_id, None, device.node_thing_name, {"schedules": schedule_data}
    ), "failed to set a schedule before the fault injection"

    # Only now start failing the read, and only for this node.
    deny_node_details_read(device.node_thing_name)

    # A real version means the Deny has not propagated yet, so keep asking. Version 0 is the
    # defect itself and fails immediately — waiting longer could only hide it.
    deadline = time.time() + FAULT_EFFECTIVE_TIMEOUT_S
    version = device.get_schedule_version(timeout=15)
    while version is not None and version != 0 and time.time() < deadline:
        time.sleep(10)
        version = device.get_schedule_version(timeout=15)

    assert version != 0, (
        "the cloud answered getSchedVer with version 0 while the node_details read was failing — "
        "a device holding real schedules would treat that as 'cloud has none' and discard them"
    )
    assert version is None, (
        f"the read was never actually denied: still answering version {version} after "
        f"{FAULT_EFFECTIVE_TIMEOUT_S}s, so this run proves nothing about the fix"
    )
