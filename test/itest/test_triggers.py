# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import time

def test_trigger_get_empty(associated_device):
    """Test getting triggers for a node that has no triggers set."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Get triggers - should return empty array
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert "triggers" in response
    assert isinstance(response["triggers"], list)
    assert len(response["triggers"]) == 0

def test_trigger_set_and_get(associated_device):
    """Test setting triggers and retrieving them."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Set triggers
    trigger_data = {
        "triggers": [
            {"id": "trigger1"},
            {"id": "trigger2"}
        ]
    }

    # Use set_node_trigger method
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(trigger_data))

    # Use get_node_trigger method
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert "triggers" in response
    assert isinstance(response["triggers"], list)
    assert len(response["triggers"]) == 2
    assert {"id": "trigger1"} in response["triggers"]
    assert {"id": "trigger2"} in response["triggers"]

    # Test duplicate trigger ID validation
    trigger_data = {
        "triggers": [
            {"id": "trigger1"},
            {"id": "trigger1"}  # Duplicate ID
        ]
    }
    assert not test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(trigger_data))

def test_trigger_update(associated_device):
    """Test updating triggers."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Set initial triggers
    trigger_data = {
        "triggers": [
            {"id": "trigger1"},
            {"id": "trigger2"}
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(trigger_data))

    # Verify initial triggers
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert len(response["triggers"]) == 2
    assert {"id": "trigger1"} in response["triggers"]
    assert {"id": "trigger2"} in response["triggers"]

    # Update triggers
    updated_trigger_data = {
        "triggers": [
            {"id": "trigger2"},  # Keep one existing
            {"id": "trigger3"}   # Add one new
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(updated_trigger_data))

    # Verify updated triggers
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert len(response["triggers"]) == 2
    assert {"id": "trigger2"} in response["triggers"]
    assert {"id": "trigger3"} in response["triggers"]
    assert {"id": "trigger1"} not in response["triggers"]

def test_trigger_delete(associated_device):
    """Test deleting triggers."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Set initial triggers
    trigger_data = {
        "triggers": [
            {"id": "trigger1"},
            {"id": "trigger2"}
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(trigger_data))

    # Verify triggers were set
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert len(response["triggers"]) == 2

    # Delete triggers
    assert test_user1.delete_node_trigger(group_id, device.node_thing_name)

    # Verify triggers were deleted
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert "triggers" in response
    assert isinstance(response["triggers"], list)
    assert len(response["triggers"]) == 0

def test_trigger_validation_empty_triggers(associated_device):
    """Test validation with empty triggers array."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Empty triggers array should be valid
    valid_payload = json.dumps({
        "triggers": []
    })
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, valid_payload)

    # Verify empty triggers array is returned
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert "triggers" in response
    assert isinstance(response["triggers"], list)
    assert len(response["triggers"]) == 0

def test_trigger_validation_complex_payload(associated_device):
    """Test validation with complex trigger payload including additional fields."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Complex trigger payload with additional fields
    valid_payload = json.dumps({
        "triggers": [
            {
                "id": "trigger1",
                "name": "Test Trigger",
                "condition": {"temperature": ">30"},
                "action": {"switch": "on"},
                "enabled": True
            }
        ]
    })
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, valid_payload)

    # Verify the trigger was saved with all fields
    response = test_user1.get_node_trigger(group_id, device.node_thing_name)
    assert response is not None
    assert len(response["triggers"]) == 1
    trigger = response["triggers"][0]
    assert trigger["id"] == "trigger1"
    assert trigger["name"] == "Test Trigger"
    assert trigger["condition"] == {"temperature": ">30"}
    assert trigger["action"] == {"switch": "on"}
    assert trigger["enabled"] is True

def test_trigger_node_notification_on_set(associated_device):
    """Test that node receives notification when triggers are set."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected and queue is clear
    assert device.connect()
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Get initial trigger version
    initial_version = device.get_trigger_version()
    assert initial_version is not None

    # Set triggers
    triggers_data = {
        "triggers": [
            {
                "id": "trigger1",
                "name": "Temperature Alert",
                "condition": {"temperature": ">30"},
                "action": {"switch": "on"}
            }
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(triggers_data))

    # Wait for and verify trigger notification
    trigger_update = device.wait_for_trigger_update()
    assert trigger_update is not None
    assert isinstance(trigger_update, dict)

    # Verify version field is present in the notification
    assert "version" in trigger_update, "Trigger update should contain version field"
    notif_version = trigger_update["version"]
    assert notif_version is not None, "Version should not be None"
    assert notif_version > initial_version, "Version in notification should be greater than initial version"

    # Verify trigger data
    assert "triggers" in trigger_update
    assert len(trigger_update["triggers"]) == 1
    assert trigger_update["triggers"][0]["id"] == "trigger1"
    assert trigger_update["triggers"][0]["name"] == "Temperature Alert"
    assert trigger_update["triggers"][0]["condition"] == {"temperature": ">30"}
    assert trigger_update["triggers"][0]["action"] == {"switch": "on"}

    # Verify trigger version was incremented
    new_version = device.get_trigger_version()
    assert new_version is not None
    assert new_version > initial_version
    assert new_version == notif_version, "Version from getTriggerVer should match version in notification"

    # Clean up triggers to avoid interfering with subsequent tests
    print("Cleaning up triggers in test_trigger_node_notification_on_set...")
    assert test_user1.delete_node_trigger(group_id, device.node_thing_name)

def test_trigger_node_notification_on_update(associated_device):
    """Test that node receives notification when triggers are updated."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected
    assert device.connect()

    # Get initial trigger version
    initial_version = device.get_trigger_version()
    assert initial_version is not None

    # Set initial triggers
    initial_triggers = {
        "triggers": [
            {
                "id": "trigger1",
                "name": "Initial Trigger",
                "condition": {"temperature": ">25"},
                "action": {"switch": "off"}
            }
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(initial_triggers))

    # Wait for initial trigger notification
    initial_update = device.wait_for_trigger_update()
    assert initial_update is not None
    assert isinstance(initial_update, dict)

    # Verify version field is present
    assert "version" in initial_update, "Initial trigger update should contain version field"
    initial_notif_version = initial_update["version"]
    assert initial_notif_version is not None, "Version should not be None"

    assert "triggers" in initial_update
    assert len(initial_update["triggers"]) == 1

    # Clear any pending notifications
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Wait to ensure the next update gets a different version timestamp (version is Unix seconds)
    time.sleep(1)

    # Update triggers
    updated_triggers = {
        "triggers": [
            {
                "id": "trigger1",
                "name": "Updated Trigger",
                "condition": {"temperature": ">30"},
                "action": {"switch": "on"}
            }
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(updated_triggers))

    # Wait for and verify trigger notification
    trigger_update = device.wait_for_trigger_update()
    assert trigger_update is not None

    # Verify version field is present and updated
    assert "version" in trigger_update, "Trigger update should contain version field"
    updated_notif_version = trigger_update["version"]
    assert updated_notif_version is not None, "Version should not be None"
    assert updated_notif_version > initial_notif_version, "Version should increase after update"

    # Verify trigger data
    assert "triggers" in trigger_update
    assert len(trigger_update["triggers"]) == 1
    assert trigger_update["triggers"][0]["id"] == "trigger1"
    assert trigger_update["triggers"][0]["name"] == "Updated Trigger"
    assert trigger_update["triggers"][0]["condition"] == {"temperature": ">30"}
    assert trigger_update["triggers"][0]["action"] == {"switch": "on"}

    # Verify trigger version was incremented
    new_version = device.get_trigger_version()
    assert new_version is not None
    assert new_version > initial_version
    assert new_version == updated_notif_version, "Version from getTriggerVer should match version in notification"

    # Clean up triggers to avoid interfering with subsequent tests
    print("Cleaning up triggers in test_trigger_node_notification_on_update...")
    assert test_user1.delete_node_trigger(group_id, device.node_thing_name)

def test_trigger_node_notification_on_delete(associated_device):
    """Test that node receives notification when triggers are deleted."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected
    assert device.connect()

    # Get initial trigger version
    initial_version = device.get_trigger_version()
    assert initial_version is not None

    # Set initial triggers
    initial_triggers = {
        "triggers": [
            {
                "id": "trigger1",
                "name": "Test Trigger",
                "condition": {"temperature": ">30"},
                "action": {"switch": "on"}
            }
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(initial_triggers))

    # Wait for initial trigger notification
    initial_update = device.wait_for_trigger_update()
    assert initial_update is not None

    # Verify version field is present
    assert "version" in initial_update, "Initial trigger update should contain version field"
    initial_notif_version = initial_update["version"]

    assert "triggers" in initial_update
    assert len(initial_update["triggers"]) == 1

    # Clear any pending notifications
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Wait so the timestamp-based version increments (second-level granularity)
    time.sleep(1)

    # Delete triggers by setting empty array
    empty_triggers = {"triggers": []}
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(empty_triggers))

    # Wait for and verify trigger notification
    trigger_update = device.wait_for_trigger_update()
    assert trigger_update is not None

    # Verify version field is present and updated
    assert "version" in trigger_update, "Trigger update should contain version field"
    delete_notif_version = trigger_update["version"]
    assert delete_notif_version is not None, "Version should not be None"
    assert delete_notif_version > initial_notif_version, "Version should increase after delete"

    assert "triggers" in trigger_update
    assert len(trigger_update["triggers"]) == 0

    # Verify trigger version was incremented
    new_version = device.get_trigger_version()
    assert new_version is not None
    assert new_version > initial_version
    assert new_version == delete_notif_version, "Version from getTriggerVer should match version in notification"

def test_trigger_node_get_details(associated_device):
    """Test that node can request trigger details."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Ensure device is connected
    assert device.connect()

    # Set triggers
    triggers_data = {
        "triggers": [
            {
                "id": "trigger1",
                "name": "Temperature Alert",
                "condition": {"temperature": ">30"},
                "action": {"switch": "on"}
            },
            {
                "id": "trigger2",
                "name": "Humidity Alert",
                "condition": {"humidity": "<40"},
                "action": {"fan": "on"}
            }
        ]
    }
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(triggers_data))

    # Wait for initial trigger notification
    initial_update = device.wait_for_trigger_update()
    assert initial_update is not None

    # Verify version field is present
    assert "version" in initial_update, "Initial trigger update should contain version field"

    assert "triggers" in initial_update
    assert len(initial_update["triggers"]) == 2

    # Clear any pending notifications
    while not device.from_cloud_queue.empty():
        device.from_cloud_queue.get_nowait()

    # Get version after setting triggers
    version_after_set = device.get_trigger_version()
    assert version_after_set is not None
    assert version_after_set > 0  # Version should be non-zero after setting triggers

    # Request trigger details from device
    trigger_details = device.get_trigger_details()
    assert trigger_details is not None
    assert "triggers" in trigger_details
    assert len(trigger_details["triggers"]) == 2

    # Verify first trigger
    assert trigger_details["triggers"][0]["id"] == "trigger1"
    assert trigger_details["triggers"][0]["name"] == "Temperature Alert"
    assert trigger_details["triggers"][0]["condition"] == {"temperature": ">30"}
    assert trigger_details["triggers"][0]["action"] == {"switch": "on"}

    # Verify second trigger
    assert trigger_details["triggers"][1]["id"] == "trigger2"
    assert trigger_details["triggers"][1]["name"] == "Humidity Alert"
    assert trigger_details["triggers"][1]["condition"] == {"humidity": "<40"}
    assert trigger_details["triggers"][1]["action"] == {"fan": "on"}

    # Verify trigger version hasn't changed just by getting details
    current_version = device.get_trigger_version()
    assert current_version is not None
    assert current_version == version_after_set  # Version should not change when just getting details

    # Clean up triggers to avoid interfering with subsequent tests
    print("Cleaning up triggers in test_trigger_node_get_details...")
    assert test_user1.delete_node_trigger(group_id, device.node_thing_name)


def test_node_trigger_cross_tenant_write_denied(two_tenants):
    """A cannot set or read triggers on B's node."""
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    trig = json.dumps({"triggers": [{"id": "t1", "name": "evil", "condition": {"x": ">1"}}]})

    assert user_a.set_node_trigger(tenant_a["group_id"], tenant_b["node_id"], trig) is False, \
        "Set triggers on foreign node via own-group path + foreign nodeId"
    assert user_a.get_node_trigger(tenant_a["group_id"], tenant_b["node_id"]) is None, \
        "Read triggers on foreign node via own-group path + foreign nodeId"
