# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import time
from test.itest.conftest import run_shared_group_stages, run_shared_subgroup_stages

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
