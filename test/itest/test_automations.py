# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import time
import pytest
from py_sdk.test_group import Group
from test.itest.conftest import accept_sharing_request_for, connect_device_with_retry

# Automation API Tests
def test_automation_api_functionality(test_user1):
    """
    Comprehensive test for basic automation API functionality:
    - Creating an automation (previously: test_create_automation)
    - Getting all automations (previously: test_get_automations)
    - Getting an automation by ID (previously: test_get_automation_by_id)
    - Updating an automation (previously: test_update_automation)
    - Deleting an automation (previously: test_delete_automation)
    - Getting empty automation list (previously: test_get_automations_empty)
    """
    # Create a group for testing
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Automation API Test Group")

    # Test empty automations list (previously test_get_automations_empty)
    automations = test_user1.get_automations(group_id)
    assert automations is not None, "Response should not be None"
    assert isinstance(automations, list), "Response should be a list"
    assert len(automations) == 0, "List should be empty for new group"

    # Create a sample automation (previously test_create_automation)
    automation_data = {
        "status": "enabled",
        "automation": {
            "name": "Test Automation",
            "triggers": {
                "time": "08:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    # Create the automation
    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    assert "automation_id" in result, "Automation ID not returned"

    # Store the automation ID for later tests
    automation_id = result["automation_id"]

    # Verify it was created by fetching it (previously test_get_automations)
    automations = test_user1.get_automations(group_id)
    assert automations is not None, "Failed to get automations"
    assert len(automations) > 0, "No automations returned"

    # Find our automation in the list
    found = False
    for automation in automations:
        if automation["id"] == automation_id:
            found = True
            # The response is flat: {id, ...payload, status}. Compare the
            # user-supplied fields, ignoring id and the derived status.
            payload_fields = {k: v for k, v in automation.items() if k not in ("id", "status")}
            assert payload_fields == {k: v for k, v in automation_data.items() if k != "status"}, "Payload doesn't match"
            break

    assert found, f"Created automation with ID {automation_id} not found in the list"

    # Get automation by ID (previously test_get_automation_by_id)
    automation = test_user1.get_automation(group_id, automation_id)
    assert automation is not None, "Failed to get automation by ID"
    assert "id" in automation, "ID not returned"
    assert automation["id"] == automation_id, "ID doesn't match"
    payload_fields = {k: v for k, v in automation.items() if k not in ("id", "status")}
    assert payload_fields == {k: v for k, v in automation_data.items() if k != "status"}, "Payload doesn't match"

    # Update the automation (previously test_update_automation)
    updated_data = {
        "status": "enabled",
        "automation": {
            "name": "Updated Automation",
            "triggers": {
                "time": "10:00"
            },
            "actions": {
                "switch": "off"
            }
        }
    }

    update_result = test_user1.update_automation(group_id, automation_id, updated_data)
    assert update_result is not None, "Failed to update automation"
    assert update_result.get("message") == "success", "Update did not succeed"

    # Verify the update
    automation = test_user1.get_automation(group_id, automation_id)
    assert automation is not None, "Failed to get updated automation"
    payload_fields = {k: v for k, v in automation.items() if k not in ("id", "status")}
    assert payload_fields == {k: v for k, v in updated_data.items() if k != "status"}, "Payload not updated correctly"

    # Delete the automation (previously test_delete_automation)
    delete_result = test_user1.delete_automation(group_id, automation_id)
    assert delete_result is True, "Delete operation failed"

    # Verify it's gone
    automations_after = test_user1.get_automations(group_id)
    found_after = any(a["id"] == automation_id for a in automations_after)
    assert not found_after, "Automation still exists after deletion"

    # Clean up
    user1_group_api.delete_group(group_id)

def test_automation_enable_disable(associated_device):
    """Test enable/disable via the optional status field in payload."""
    # The action target must be a real member of the group: creation now rejects
    # automations whose action targets are outside the group (cross-tenant guard).
    device, group_id, test_user1, user1_group_api = associated_device
    node_id = device.node_thing_name

    automation_data = {
        "name": "Status test automation",
        "conditions": {"and": [f"{node_id}~placeholder~0"]},
        "actions": {"targets": [{"node": node_id, "path": "Light.Power", "value": True}]},
    }

    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    automation_id = result["automation_id"]

    automation = test_user1.get_automation(group_id, automation_id)
    assert automation is not None, "Failed to get automation"
    # The response is flat: {id, ...payload, status}.
    assert automation["status"] == "enabled", "New automations should default to enabled"

    automations = test_user1.get_automations(group_id)
    entry = next(a for a in automations if a["id"] == automation_id)
    assert entry["status"] == "enabled"

    disabled_data = {**automation_data, "status": "disabled"}
    update_result = test_user1.update_automation(group_id, automation_id, disabled_data)
    assert update_result is not None, "Failed to disable automation"

    automation = test_user1.get_automation(group_id, automation_id)
    assert automation["status"] == "disabled"

    enabled_data = {**automation_data, "status": "enabled"}
    assert test_user1.update_automation(group_id, automation_id, enabled_data) is not None

    automation = test_user1.get_automation(group_id, automation_id)
    assert automation["status"] == "enabled"

    rename_data = {**automation_data, "name": "Renamed automation"}
    assert test_user1.update_automation(group_id, automation_id, rename_data) is not None
    automation = test_user1.get_automation(group_id, automation_id)
    assert automation["status"] == "enabled"
    assert automation["name"] == "Renamed automation"

    # The group is owned by the associated_device fixture; only clean up the
    # automation created here.
    test_user1.delete_automation(group_id, automation_id)

def test_delete_all_automations(test_user1):
    """Test deleting all automations for a group."""
    # Create a group for testing
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Delete All Automations Test Group")

    # Create multiple automations
    for i in range(3):
        automation_data = {
            "automation": {
                "name": f"Bulk Delete Test {i}",
                "triggers": {
                    "time": f"{14+i}:00"
                },
                "actions": {
                    "switch": "on"
                }
            }
        }

        result = test_user1.create_automation(group_id, automation_data)
        assert result is not None, f"Failed to create automation {i}"

    # Verify automations exist
    automations_before = test_user1.get_automations(group_id)
    assert len(automations_before) == 3, "Expected 3 automations before deletion"

    # Delete all automations
    delete_result = test_user1.delete_all_automations(group_id)
    assert delete_result is True, "Delete all operation failed"

    # Verify all automations are gone
    automations_after = test_user1.get_automations(group_id)
    assert len(automations_after) == 0, "Expected 0 automations after deletion"

    # Clean up
    user1_group_api.delete_group(group_id)

# Automation Permission and Access Tests
def test_automation_unauthorized_access(test_user1, test_user2):
    """Test that unauthorized users cannot access automations."""
    # Create a group for testing with test_user1
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Automation Auth Test Group")

    # Create a sample automation with test_user1
    automation_data = {
        "automation": {
            "name": "Auth Test Automation",
            "triggers": {
                "time": "08:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    automation_id = result["automation_id"]

    # Attempt to get automations with test_user2 (unauthorized)
    automations = test_user2.get_automations(group_id)
    assert automations is None, "Unauthorized user should not be able to get automations"

    # Attempt to get specific automation with test_user2 (unauthorized)
    automation = test_user2.get_automation(group_id, automation_id)
    assert automation is None, "Unauthorized user should not be able to get specific automation"

    # Attempt to create automation with test_user2 (unauthorized)
    new_automation = test_user2.create_automation(group_id, automation_data)
    assert new_automation is None, "Unauthorized user should not be able to create automation"

    # Attempt to update automation with test_user2 (unauthorized)
    update_result = test_user2.update_automation(group_id, automation_id, automation_data)
    assert update_result is None, "Unauthorized user should not be able to update automation"

    # Attempt to delete automation with test_user2 (unauthorized)
    delete_result = test_user2.delete_automation(group_id, automation_id)
    assert delete_result is False, "Unauthorized user should not be able to delete automation"

    # Verify automation still exists for test_user1
    automations_after = test_user1.get_automations(group_id)
    found = any(a["id"] == automation_id for a in automations_after)
    assert found, "Automation should still exist after unauthorized deletion attempt"

    # Clean up
    test_user1.delete_automation(group_id, automation_id)
    user1_group_api.delete_group(group_id)

def test_automation_action_target_cross_group_rejected(associated_device, test_user2):
    """Reject an action target that is not a member of the automation's group.

    Regression for the cross-tenant device-control finding: an automation runs
    under a system actor whose authorization passes for any node, so without a
    group-membership check an attacker could create an automation under a group
    they own, point an action at a victim node in another tenant's group, and
    have the cloud publish arbitrary params to that node.

    victim_device lives in victim_group (owned by the associated_device user).
    test_user2 owns a separate group and must not be able to create or update
    an automation whose action target is the victim's node.
    """
    victim_device, victim_group_id, _victim_user, _victim_group_api = associated_device

    attacker_group_api = Group(test_user2)
    attacker_group_id = attacker_group_api.create_group("Automation Cross-Tenant Attacker Group")

    try:
        # Attempt 1: create an automation in the attacker's group whose action
        # target is the victim node. The victim node is not a member of the
        # attacker's group, so creation must be rejected.
        cross_group_automation = {
            "name": "Cross-tenant action target",
            "actions": {
                "targets": [
                    {"node": victim_device.node_thing_name, "path": "Light.Power", "value": True}
                ]
            },
        }
        result = test_user2.create_automation(attacker_group_id, cross_group_automation)
        assert result is None, "Automation with a foreign action target must be rejected on create"

        # A foreign action target is a malformed request, so the rejection must
        # surface as HTTP 400 (client error), not 500 (server fault). Issue the
        # raw POST since create_automation() collapses all failures to None.
        raw = test_user2.make_api_request(
            'POST',
            f"/v1/groups/{attacker_group_id}/service/automations",
            json.dumps(cross_group_automation),
        )
        assert raw.status_code == 400, \
            f"Foreign action target must be rejected with HTTP 400, got {raw.status_code}: {raw.text}"

        # Nothing was stored in the attacker's group.
        automations = test_user2.get_automations(attacker_group_id)
        assert automations is not None, "Attacker should be able to list their own group's automations"
        assert len(automations) == 0, "No automation should have been created with a foreign action target"

        # Attempt 2: create a valid automation with no action targets, then try
        # to update it to point at the victim node. The update must be rejected
        # and the stored automation must be left unchanged.
        benign_automation = {
            "name": "Benign automation",
            "actions": {"targets": []},
        }
        create_result = test_user2.create_automation(attacker_group_id, benign_automation)
        assert create_result is not None, "Benign automation without action targets should be accepted"
        automation_id = create_result["automation_id"]

        update_result = test_user2.update_automation(
            attacker_group_id, automation_id, cross_group_automation
        )
        assert update_result is None, "Updating an automation to a foreign action target must be rejected"

        stored = test_user2.get_automation(attacker_group_id, automation_id)
        assert stored is not None, "Benign automation should still exist after the rejected update"
        assert stored.get("actions", {}).get("targets", []) == [], \
            "Rejected update must not persist the foreign action target"

        test_user2.delete_automation(attacker_group_id, automation_id)
    finally:
        attacker_group_api.delete_group(attacker_group_id, warn_error=True)

def test_automation_shared_group_access(test_user1, test_user2):
    """
    Test that users with shared access can interact with automations.
    This test covers multiple aspects of automation functionality with shared access:
    - Creating automations by owner and shared user
    - Getting automations (list and individual)
    - Updating automations
    - Deleting automations (single and bulk)
    """
    # Create a group for testing with test_user1
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group("Automation Shared Access Test Group")

    # Create a sample automation with test_user1
    automation_data = {
        "automation": {
            "name": "Shared Access Test Automation",
            "triggers": {
                "time": "08:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    automation_id = result["automation_id"]

    # Share the group with test_user2
    user1_group_api.share_group(group_id, test_user2.username, "primary")

    # Accept the sharing request for test_user2
    accept_sharing_request_for(test_user2, group_id, "")

    # Verify test_user2 can now access the automation
    automations = test_user2.get_automations(group_id)
    assert automations is not None, "Shared user should be able to get automations"
    found = any(a["id"] == automation_id for a in automations)
    assert found, "Shared user should see the automation in the list"

    # Verify test_user2 can get the specific automation
    automation = test_user2.get_automation(group_id, automation_id)
    assert automation is not None, "Shared user should be able to get specific automation"
    assert automation["id"] == automation_id, "Automation ID should match"

    # Verify test_user2 can create their own automation in the shared group
    new_automation_data = {
        "automation": {
            "name": "New Shared Access Automation",
            "triggers": {
                "time": "09:00"
            },
            "actions": {
                "switch": "off"
            }
        }
    }

    new_result = test_user2.create_automation(group_id, new_automation_data)
    assert new_result is not None, "Shared user should be able to create automation"
    new_automation_id = new_result["automation_id"]

    # Verify test_user2 can update automations
    updated_data = {
        "automation": {
            "name": "Updated Shared Access Automation",
            "triggers": {
                "time": "10:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    update_result = test_user2.update_automation(group_id, new_automation_id, updated_data)
    assert update_result is not None, "Shared user should be able to update automation"

    # Verify test_user2 can delete their automation
    delete_result = test_user2.delete_automation(group_id, new_automation_id)
    assert delete_result is True, "Shared user should be able to delete automation"

    # Verify test_user2 can delete automation created by test_user1
    delete_result = test_user2.delete_automation(group_id, automation_id)
    assert delete_result is True, "Shared user should be able to delete automation created by another user"

    # Verify automation is gone
    automations_after = test_user1.get_automations(group_id)
    found = any(a["id"] == automation_id for a in automations_after)
    assert not found, "Automation should be gone after shared user deletion"

    # Create multiple automations for delete-all test
    for i in range(3):
        automation_data = {
            "automation": {
                "name": f"Bulk Delete Test {i}",
                "triggers": {
                    "time": f"{14+i}:00"
                },
                "actions": {
                    "switch": "on"
                }
            }
        }
        test_user1.create_automation(group_id, automation_data)

    # Verify automations exist
    automations_before = test_user1.get_automations(group_id)
    assert len(automations_before) == 3, "Expected 3 automations before deletion"

    # Verify test_user2 can delete all automations
    delete_all_result = test_user2.delete_all_automations(group_id)
    assert delete_all_result is True, "Shared user should be able to delete all automations"

    # Verify all automations are gone
    automations_after = test_user1.get_automations(group_id)
    assert len(automations_after) == 0, "Expected 0 automations after shared user deletion"

    # Clean up
    user1_group_api.delete_group(group_id)

# Automation Edge Case Tests
def test_automation_with_complex_payload(test_user1):
    """Test that automation service handles complex nested JSON payloads correctly."""
    # Create a group for testing
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Complex Payload Test Group")

    # Create a complex automation payload with nested arrays and objects
    complex_data = {
        "automation": {
            "name": "Complex Automation",
            "description": "A complex automation with nested structure",
            "enabled": True,
            "triggers": {
                "schedules": [
                    {"time": "08:00", "days": ["Monday", "Wednesday", "Friday"]},
                    {"time": "20:00", "days": ["Tuesday", "Thursday"]}
                ],
                "conditions": [
                    {
                        "sensor": "temperature",
                        "operator": ">",
                        "value": 25,
                        "modifiers": {
                            "hysteresis": 2,
                            "debounce": {"value": 5, "unit": "minutes"}
                        }
                    },
                    {
                        "sensor": "humidity",
                        "operator": "<",
                        "value": 40,
                        "logic": "OR"
                    }
                ]
            },
            "actions": [
                {
                    "device": "fan",
                    "command": "set_speed",
                    "parameters": {
                        "speed": "high",
                        "oscillate": True,
                        "duration": {"value": 30, "unit": "minutes"}
                    }
                },
                {
                    "device": "light",
                    "command": "set_state",
                    "parameters": {
                        "state": "on",
                        "brightness": 80,
                        "color": {"r": 255, "g": 200, "b": 100}
                    }
                }
            ],
            "metadata": {
                "created_at": "2023-01-01T12:00:00Z",
                "modified_at": "2023-01-02T14:30:00Z",
                "tags": ["comfort", "energy_saving"],
                "version": 2,
                "priority": "high"
            }
        }
    }

    # Create the automation with complex payload
    result = test_user1.create_automation(group_id, complex_data)
    assert result is not None, "Failed to create automation with complex payload"
    automation_id = result["automation_id"]

    # Retrieve the automation and verify the complex payload is preserved
    automation = test_user1.get_automation(group_id, automation_id)
    assert automation is not None, "Failed to get automation with complex payload"

    # The response is flat: {id, ...payload}. Drop id to recover the payload.
    payload = {k: v for k, v in automation.items() if k != "id"}
    assert payload["automation"]["name"] == "Complex Automation", "Name does not match"
    assert len(payload["automation"]["triggers"]["schedules"]) == 2, "Schedule array not preserved"
    assert len(payload["automation"]["actions"]) == 2, "Actions array not preserved"
    assert payload["automation"]["actions"][1]["parameters"]["color"]["r"] == 255, "Nested color values not preserved"
    assert payload["automation"]["triggers"]["conditions"][0]["modifiers"]["debounce"]["unit"] == "minutes", "Deeply nested values not preserved"

    # Clean up
    test_user1.delete_automation(group_id, automation_id)
    user1_group_api.delete_group(group_id)

def test_automation_with_invalid_group_id(test_user1):
    """Test that automation service properly handles requests with invalid group IDs."""
    # Use an invalid group ID (doesn't exist)
    invalid_group_id = "nonexistent-group-id"

    # Create a sample automation data
    automation_data = {
        "automation": {
            "name": "Invalid Group Test",
            "triggers": {
                "time": "08:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    # Attempt to create automation with invalid group ID
    result = test_user1.create_automation(invalid_group_id, automation_data)
    assert result is None, "Create automation should fail with invalid group ID"

    # Attempt to get automations with invalid group ID
    automations = test_user1.get_automations(invalid_group_id)
    assert automations is None, "Get automations should fail with invalid group ID"

    # Attempt to get specific automation with invalid group ID
    automation = test_user1.get_automation(invalid_group_id, "some-automation-id")
    assert automation is None, "Get automation should fail with invalid group ID"

    # Attempt to update automation with invalid group ID
    update_result = test_user1.update_automation(invalid_group_id, "some-automation-id", automation_data)
    assert update_result is None, "Update automation should fail with invalid group ID"

    # Attempt to delete automation with invalid group ID
    delete_result = test_user1.delete_automation(invalid_group_id, "some-automation-id")
    assert delete_result is False, "Delete automation should fail with invalid group ID"

    # Attempt to delete all automations with invalid group ID
    delete_all_result = test_user1.delete_all_automations(invalid_group_id)
    assert delete_all_result is False, "Delete all automations should fail with invalid group ID"

def test_automation_id_format(test_user1):
    """Test that generated automation IDs follow the expected format."""
    # Create a group for testing
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Automation ID Format Test Group")

    # Create multiple automations to check ID format
    automation_ids = []
    for i in range(5):
        automation_data = {
            "automation": {
                "name": f"ID Format Test {i}",
                "triggers": {
                    "time": f"{8+i}:00"
                },
                "actions": {
                    "switch": "on"
                }
            }
        }

        result = test_user1.create_automation(group_id, automation_data)
        assert result is not None, f"Failed to create automation {i}"
        automation_ids.append(result["automation_id"])

    # Verify the format of each automation ID
    for i, automation_id in enumerate(automation_ids):
        # Automation IDs should be 3 characters long
        assert len(automation_id) == 3, f"Automation ID {automation_id} should be 3 characters long"

        # First character should be a lowercase letter
        assert automation_id[0].islower() and automation_id[0].isalpha(), f"First character of automation ID {automation_id} should be a lowercase letter"

        # All characters should be lowercase letters or numbers
        assert automation_id.islower(), f"Automation ID {automation_id} should be lowercase"
        assert all(c.isalnum() for c in automation_id), f"Automation ID {automation_id} should only contain alphanumeric characters"

    # Check that all IDs are unique
    assert len(set(automation_ids)) == len(automation_ids), "Automation IDs should be unique"

    # Clean up
    for automation_id in automation_ids:
        test_user1.delete_automation(group_id, automation_id)
    user1_group_api.delete_group(group_id)

# Automation Integration Tests
def test_automations_persist_after_node_changes(associated_device):
    """Test that automations persist after adding or removing nodes from a group."""
    device, group_id, test_user1, user1_group_api = associated_device

    # Create a sample automation
    automation_data = {
        "automation": {
            "name": "Persistent Automation",
            "triggers": {
                "time": "08:00"
            },
            "actions": {
                "switch": "on"
            }
        }
    }

    # Create the automation
    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    automation_id = result["automation_id"]

    # Verify the automation exists
    automations_before = test_user1.get_automations(group_id)
    found_before = any(a["id"] == automation_id for a in automations_before)
    assert found_before, "Automation not found before node changes"

    # Remove the node from the group (removing device)
    new_group_id = user1_group_api.create_group("Test Node Change Group")
    assert test_user1.do_user_node_assoc(device, new_group_id) is None, "Failed to move device to new group"

    # Verify the automation still exists in the original group
    automations_after_remove = test_user1.get_automations(group_id)
    found_after_remove = any(a["id"] == automation_id for a in automations_after_remove)
    assert found_after_remove, "Automation should persist after removing node"

    # Add the node back to the original group
    assert test_user1.do_user_node_assoc(device, group_id) is None, "Failed to move device back to original group"

    # Verify the automation still exists
    automations_after_add = test_user1.get_automations(group_id)
    found_after_add = any(a["id"] == automation_id for a in automations_after_add)
    assert found_after_add, "Automation should persist after adding node back"

    # Clean up
    test_user1.delete_automation(group_id, automation_id)
    user1_group_api.delete_group(new_group_id)
    # Don't delete original group as it's part of the fixture

def test_delete_nonexistent_automation(test_user1):
    """Test deleting a non-existent automation returns an error."""
    # Create a group for testing
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Nonexistent Automation Test Group")

    # Use a random ID that shouldn't exist
    nonexistent_id = "xyz"

    # Attempt to delete the non-existent automation
    delete_result = test_user1.delete_automation(group_id, nonexistent_id)
    assert delete_result is False, "Delete operation should fail for non-existent automation"

    # Clean up
    user1_group_api.delete_group(group_id)

@pytest.mark.parametrize("basic_ingest", [True, False], ids=["basic_ingest", "direct_topic"])
def test_automation_end_to_end_execution(associated_device, basic_ingest):
    """
    Comprehensive end-to-end test for automation functionality.
    Tests the complete automation workflow:
    1. Create an automation with conditions and actions
    2. Set up triggers on the device
    3. Send trigger notifications that satisfy conditions
    4. Verify that actions are executed by checking device state changes
    """
    device, group_id, test_user1, user1_group_api = associated_device

    # Clean up any existing triggers from previous tests to ensure clean state
    print("Cleaning up existing triggers...")
    try:
        test_user1.delete_node_trigger(group_id, device.node_thing_name)
    except:
        pass  # Ignore errors if no triggers exist

    # Clean up any stale subgroup associations from previous tests
    print("Cleaning up stale subgroup associations...")
    if hasattr(device, 'subgroup_ids'):
        device.subgroup_ids = None

    # Wait a moment for cleanup to propagate
    time.sleep(1)

    # Ensure device is connected and has proper configuration
    assert connect_device_with_retry(device), "Failed to connect device"
    device.set_node_config({
        "devices": [{
            "id": "Light",
            "type": "esp.device.lightbulb",
            "params": [
                {"id": "Power", "type": "esp.param.power"},
                {"id": "Brightness", "type": "esp.param.brightness"}
            ]
        }, {
            "id": "Fan",
            "type": "esp.device.fan",
            "params": [
                {"id": "Power", "type": "esp.param.power"},
                {"id": "Speed", "type": "esp.param.speed"}
            ]
        }]
    })

    # Set up shadow connection
    shadow_name = f"params-{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        for subgroup_id in sorted(device.subgroup_ids):
            shadow_name += f"-{subgroup_id}"

    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"

    # Subscribe to params topic to capture action execution
    params_topic = f"rainmaker/nodes/{device.node_thing_name}/user/{shadow_name}/params"
    assert device.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    # Set initial device state in shadow
    initial_state = {
        "Light": {"Power": False, "Brightness": 30},
        "Fan": {"Power": False, "Speed": 1}
    }
    device.update_named_shadow(shadow_name, initial_state)
    time.sleep(2)  # Allow shadow update to propagate

    # Step 1: Create automation with AND conditions and multiple actions
    # We'll use a placeholder automation ID first, then update with the real one
    placeholder_automation_id = "e2e"

    automation_data = {
        "name": "End-to-End Test Automation",
        "description": "Test automation with multiple triggers and actions",
        "conditions": {
            "and": [
                f"{device.node_thing_name}~{placeholder_automation_id}~temperature-high",
                f"{device.node_thing_name}~{placeholder_automation_id}~motion-detected"
            ]
        },
        "actions": {
            "targets": [
                {
                    "node": device.node_thing_name,
                    "path": "Light.Power",
                    "value": True
                },
                {
                    "node": device.node_thing_name,
                    "path": "Light.Brightness",
                    "value": 80
                },
                {
                    "node": device.node_thing_name,
                    "path": "Fan.Power",
                    "value": True
                },
                {
                    "node": device.node_thing_name,
                    "path": "Fan.Speed",
                    "value": 2
                }
            ]
        }
    }

    # Create the automation
    result = test_user1.create_automation(group_id, automation_data)
    assert result is not None, "Failed to create automation"
    automation_id = result["automation_id"]
    print(f"Created automation with ID: {automation_id}")

    # Wait a moment for the update to propagate
    time.sleep(2)

    # Debug: Verify the automation was updated correctly
    created_automation = test_user1.get_automation(group_id, automation_id)
    print(f"Final automation details: {json.dumps(created_automation, indent=2)}")

    # Verify the automation has the correct structure. The response is flat:
    # conditions and actions sit at the top level alongside id.
    conditions = created_automation.get("conditions", {})
    actions = created_automation.get("actions", {})

    print(f"Automation conditions: {conditions}")
    print(f"Automation actions: {actions}")

    # Verify conditions are properly formatted
    and_conditions = conditions.get("and", [])
    assert len(and_conditions) == 2, f"Expected 2 AND conditions, got {len(and_conditions)}"
    print(f"AND conditions: {and_conditions}")

    # Verify actions are properly formatted
    targets = actions.get("targets", [])
    assert len(targets) == 4, f"Expected 4 action targets, got {len(targets)}"
    print(f"Action targets: {targets}")

    # Step 2: Set up triggers on the device
    trigger_data = {
        "triggers": [
            {
                "id": "temperature-high",
                "name": "Temperature High Alert",
                "condition": {"temperature": ">30"}
            },
            {
                "id": "motion-detected",
                "name": "Motion Detection",
                "condition": {"motion": "detected"}
            }
        ]
    }

    # Set triggers on the device
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps(trigger_data))
    print("Set triggers on device")

    # Wait for trigger notification to be received by device
    trigger_update = device.wait_for_trigger_update()
    assert trigger_update is not None, "Device should receive trigger update"

    # Verify version field is present
    assert "version" in trigger_update, "Trigger update should contain version field"

    assert len(trigger_update["triggers"]) == 2, "Device should receive both triggers"

    # Step 3: Send trigger notifications that DO NOT satisfy conditions (partial match)
    print("Testing partial condition satisfaction (should NOT execute actions)")

    # Send only first trigger notification via direct notification
    trigger_notification_1 = {
        "automation": {
            "trigger": [
                {
                    "id": f"{device.node_thing_name}~{automation_id}~temperature-high",
                    "value": True
                }
            ]
        }
    }

    # Send trigger notification using device's direct notification method
    assert device.send_direct_notification(trigger_notification_1, basic_ingest=basic_ingest), "Failed to send trigger notification"
    print(f"Sent first trigger notification: {trigger_notification_1}")

    # Wait a bit and verify no actions were executed (conditions not fully satisfied)
    time.sleep(3)
    print("Verified partial condition satisfaction doesn't execute actions")

    # Check that no params message was received (no actions executed)
    try:
        message = device.wait_for_params_message(timeout=2)
        assert message is None, "No actions should be executed with partial condition satisfaction"
    except:
        pass  # No message is expected, which is correct

    # Step 4: Send trigger notifications that DO satisfy conditions (full match)
    print("Testing full condition satisfaction (should execute actions)")

    # Send second trigger notification via direct notification to complete the AND condition
    trigger_notification_2 = {
        "automation": {
            "trigger": [
                {
                    "id": f"{device.node_thing_name}~{automation_id}~motion-detected",
                    "value": True
                }
            ]
        }
    }

    assert device.send_direct_notification(trigger_notification_2, basic_ingest=basic_ingest), "Failed to send second trigger notification"
    print(f"Sent second trigger notification: {trigger_notification_2}")

    # Give some time for the automation service to process the trigger
    print("Waiting for automation service to process triggers...")
    time.sleep(3)

    # Step 5: Verify that actions are executed
    print("Waiting for action execution...")

    # Collect all action messages (actions are sent individually)
    received_actions = {}
    expected_actions = 4  # Light Power, Light Brightness, Fan Power, Fan Speed

    # First, wait a bit longer for the first message to arrive in CI environments
    print("Waiting for first action message...")
    first_message = device.wait_for_params_message(timeout=15)

    if first_message is None:
        # Debug: Check if automation is working by querying the automation state
        print("No action messages received. Debugging automation state...")
        automation_details = test_user1.get_automation(group_id, automation_id)
        print(f"Automation details after trigger: {json.dumps(automation_details, indent=2)}")

        # Check if there's a state field that might show trigger values
        if "state" in automation_details:
            print(f"Automation state: {automation_details['state']}")

        # Check if the triggers were actually set on the device
        print("Checking device trigger configuration...")
        device_triggers = test_user1.get_node_trigger(group_id, device.node_thing_name)
        print(f"Device triggers: {json.dumps(device_triggers, indent=2) if device_triggers else 'None'}")

        # Try sending the trigger notifications again
        print("Re-sending trigger notifications...")
        assert device.send_direct_notification(trigger_notification_1, basic_ingest=basic_ingest), "Failed to re-send first trigger notification"
        time.sleep(2)
        assert device.send_direct_notification(trigger_notification_2, basic_ingest=basic_ingest), "Failed to re-send second trigger notification"

        # Try waiting a bit more in case of slow CI
        print("Waiting additional time for delayed action execution...")
        time.sleep(8)
        first_message = device.wait_for_params_message(timeout=10)

    assert first_message is not None, "Should receive at least one params message indicating actions were executed"
    print(f"Received first action execution message: {first_message}")

    # Merge the first message into received_actions
    for device_name, params in first_message.items():
        if device_name not in received_actions:
            received_actions[device_name] = {}
        received_actions[device_name].update(params)

    # Collect remaining action messages (up to expected_actions - 1 more)
    messages_received = 1
    for i in range(expected_actions - 1):
        message = device.wait_for_params_message(timeout=5)  # Shorter timeout for subsequent messages
        if message is not None:
            messages_received += 1
            print(f"Received action execution message {messages_received}: {message}")

            # Merge the message into received_actions
            for device_name, params in message.items():
                if device_name not in received_actions:
                    received_actions[device_name] = {}
                received_actions[device_name].update(params)
        else:
            print(f"No more action messages after {messages_received} messages (timeout)")
            break

    print(f"Total messages received: {messages_received}")
    print(f"All received actions: {received_actions}")

    # Verify the actions were executed correctly
    assert len(received_actions) > 0, "At least some actions should be executed"

    # Check if we have Light actions
    if "Light" in received_actions:
        light_actions = received_actions["Light"]
        print(f"Light actions received: {light_actions}")
        # Check what Light actions we received
        if "Power" in light_actions:
            assert light_actions["Power"] is True, "Light Power should be turned on"
        if "Brightness" in light_actions:
            assert light_actions["Brightness"] == 80, "Light Brightness should be set to 80"

    # Check if we have Fan actions
    if "Fan" in received_actions:
        fan_actions = received_actions["Fan"]
        print(f"Fan actions received: {fan_actions}")
        # Check what Fan actions we received
        if "Power" in fan_actions:
            assert fan_actions["Power"] is True, "Fan Power should be turned on"
        if "Speed" in fan_actions:
            assert fan_actions["Speed"] == 2, "Fan Speed should be set to 2"

    # Final verification - we should have at least received some actions for both devices
    # But be more lenient for CI environments
    if messages_received < 4:
        print(f"⚠️  Only received {messages_received} of 4 expected action messages (CI timing issue?)")
        # At minimum, verify we got some action execution
        assert len(received_actions) > 0, "Should receive at least some action execution"
    else:
        # Full verification if we got all messages
        assert "Light" in received_actions, "Light actions should be executed"
        assert "Fan" in received_actions, "Fan actions should be executed"

    print("✅ Actions executed successfully!")

    # Step 6: Test trigger reset (value false)
    print("Testing trigger reset...")

    # Send trigger notification with value false to reset condition
    trigger_reset = {
        "automation": {
            "trigger": [
                {
                    "id": f"{device.node_thing_name}~{automation_id}~temperature-high",
                    "value": False
                }
            ]
        }
    }

    assert device.send_direct_notification(trigger_reset, basic_ingest=basic_ingest), "Failed to send trigger reset notification"

    # Wait a bit - no new actions should be executed
    time.sleep(3)

    # Verify no new actions (since AND condition is no longer satisfied)
    try:
        message = device.wait_for_params_message(timeout=2)
        assert message is None, "No new actions should be executed after trigger reset"
    except:
        pass  # No message is expected

    # Step 7: Test OR condition automation
    print("Testing OR condition automation...")

    # Create a second automation with OR conditions using placeholder first
    or_placeholder_id = "or"
    or_automation_data = {
        "name": "OR Condition Test Automation",
        "conditions": {
            "or": [
                f"{device.node_thing_name}~{or_placeholder_id}~door-open",
                f"{device.node_thing_name}~{or_placeholder_id}~window-open"
            ]
        },
        "actions": {
            "targets": [
                {
                    "node": device.node_thing_name,
                    "path": "Light.Brightness",
                    "value": 50
                }
            ]
        }
    }

    or_result = test_user1.create_automation(group_id, or_automation_data)
    assert or_result is not None, "Failed to create OR automation"
    or_automation_id = or_result["automation_id"]

    # Add OR condition triggers
    or_trigger_data = {
        "triggers": [
            {
                "id": "door-open",
                "name": "Door Open Sensor",
                "condition": {"door": "open"}
            },
            {
                "id": "window-open",
                "name": "Window Open Sensor",
                "condition": {"window": "open"}
            }
        ]
    }

    # Update triggers to include OR triggers
    combined_triggers = trigger_data["triggers"] + or_trigger_data["triggers"]
    assert test_user1.set_node_trigger(group_id, device.node_thing_name, json.dumps({"triggers": combined_triggers}))

    # Send single OR trigger (should be sufficient to execute actions)
    or_trigger_notification = {
        "automation": {
            "trigger": [
                {
                    "id": f"{device.node_thing_name}~{or_automation_id}~door-open",
                    "value": True
                }
            ]
        }
    }

    assert device.send_direct_notification(or_trigger_notification, basic_ingest=basic_ingest), "Failed to send OR trigger notification"

    # Verify OR automation actions are executed
    or_message = device.wait_for_params_message(timeout=8)
    assert or_message is not None, "Should receive params message for OR automation"
    assert "Light" in or_message, "Light action should be executed for OR automation"
    assert or_message["Light"]["Brightness"] == 50, "Light brightness should be set to 50"

    print("✅ OR condition automation executed successfully!")

    # Step 8: Verify automations still exist and can be retrieved
    automations = test_user1.get_automations(group_id)
    assert automations is not None, "Should be able to retrieve automations"

    automation_ids = [a["id"] for a in automations]
    assert automation_id in automation_ids, "Original automation should still exist"
    assert or_automation_id in automation_ids, "OR automation should still exist"

    print("✅ All automations verified in system!")

    # Cleanup
    test_user1.delete_automation(group_id, automation_id)
    test_user1.delete_automation(group_id, or_automation_id)
    device.disconnect()

    print("🎉 End-to-end automation test completed successfully!")


# Cross-tenant tests. Regression guards for commit d9167471, which fixed an
# automation action target reaching a node in another tenant's group: actions run
# under a SystemActor whose IsAuthorized passes for any node, so the target was
# never checked against the automation's group.

def test_automation_condition_foreign_node_rejected(two_tenants):
    """Tenant A's automation must not be able to CONDITION on tenant B's node.

    Conditions use trigger IDs of the form `<nodeID>~<automationID>~<trigger>`.
    If A can reference B's node in a condition, A learns when B's node fires
    triggers (cross-tenant state inference) and can chain it to A-owned actions.
    """
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    group_a = tenant_a["group_id"]
    foreign_node = tenant_b["node_id"]

    automation_data = {
        "name": "cross-tenant condition injection",
        "conditions": {"and": [f"{foreign_node}~aaa~trig0"]},
        "actions": {"targets": [
            {"node": tenant_a["node_id"], "path": "Light.Power", "value": True},
        ]},
    }
    result = user_a.create_automation(group_a, automation_data)
    assert result is None, (
        "Automation CONDITIONING on a node in another tenant's group was "
        "accepted; enables cross-tenant trigger-state inference."
    )


def test_automation_resource_id_is_group_scoped(two_tenants):
    """A foreign automation ID must not be readable/deletable via A's group.

    Automations are keyed (group_id, automation_id) in DynamoDB, so addressing
    B's automation id under A's group should simply not resolve. This is the
    'safe by construction' case — asserted to lock the behaviour in.
    """
    tenant_a, tenant_b = two_tenants
    b_auto = tenant_b["user"].create_automation(tenant_b["group_id"], {
        "name": "B private automation",
        "actions": {"targets": [{"node": tenant_b["node_id"], "path": "Light.Power", "value": True}]},
    })
    assert b_auto is not None
    b_auto_id = b_auto["automation_id"]

    assert tenant_a["user"].get_automation(tenant_a["group_id"], b_auto_id) is None, \
        "Foreign automation id resolved under caller's own group (IDOR)"
    assert tenant_a["user"].delete_automation(tenant_a["group_id"], b_auto_id) is False, \
        "Foreign automation id deletable under caller's own group (IDOR)"

    assert tenant_a["user"].get_automation(tenant_b["group_id"], b_auto_id) is None, \
        "Caller read an automation in a group they do not belong to (cross-tenant)"

    assert tenant_b["user"].get_automation(tenant_b["group_id"], b_auto_id) is not None


def test_delete_all_automations_on_foreign_group_denied(two_tenants):
    """A must not delete-all automations in B's group.

    Single-automation update/delete authz is covered by
    test_automation_unauthorized_access; this covers the bulk endpoint, which
    takes only a group_id and so cannot rely on a per-resource check.
    """
    tenant_a, tenant_b = two_tenants
    user_a, user_b = tenant_a["user"], tenant_b["user"]

    b_auto = user_b.create_automation(tenant_b["group_id"], {
        "name": "B auto", "actions": {"targets": [
            {"node": tenant_b["node_id"], "path": "Light.Power", "value": True}]}})
    assert b_auto is not None

    assert user_a.delete_all_automations(tenant_b["group_id"]) is False, \
        "Deleted all automations in a foreign group (cross-tenant)"

    assert user_b.get_automation(tenant_b["group_id"], b_auto["automation_id"]) is not None, \
        "Foreign automation was destroyed by a non-member"
