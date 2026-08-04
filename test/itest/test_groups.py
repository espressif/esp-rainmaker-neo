# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import time
import uuid
from py_sdk.test_group import Group
from py_sdk.test_device import Device, generate_key_and_cert
from test.itest.conftest import (
    CA_CERT,
    IOT_ENDPOINT,
    REGION,
    DEBUG,
    connect_device_with_retry,
    accept_sharing_request_for,
)
from py_sdk.test_util import wait_until, describe_thing_attributes, seed_node_data, assert_node_data_deleted


def test_create_and_list_groups(test_user1):
    # Create a group
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Test Group")

    # List groups
    list_groups_data = user1_group_api.list_groups()
    assert "groups" in list_groups_data, "Response should contain groups"
    group = next((g for g in list_groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Created group {group_id} not found in the list of groups"
    assert group["access_type"] == "primary", f"Expected access_type 'primary', got '{group.get('access_type')}'"
    user1_group_api.delete_group(group_id)

def test_create_subgroup(test_user1):
    # Create a main group
    user1_group_api = Group(test_user1)
    main_group_id = user1_group_api.create_group("Main Group")

    # Create a subgroup
    subgroup_id = user1_group_api.create_subgroup(main_group_id, "Test Subgroup")

    # Verify the group structure
    expected_structure = {
        "group_id": main_group_id,
        "group_name": "Main Group",
        "access_type": "primary",
        "subgroups": [
            {
                "subgroup_id": subgroup_id,
                "subgroup_name": "Test Subgroup"
            }
        ]
    }
    user1_group_api.verify_group_structure(main_group_id, expected_structure)
    user1_group_api.empty_and_delete_group(main_group_id)

def test_update_group(test_user1):
    # Create a main group
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Original Group Name")

    # Verify the initial group name
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found"
    assert group["group_name"] == "Original Group Name", f"Expected 'Original Group Name', but got '{group['group_name']}'"
    assert group["access_type"] == "primary", f"Expected access_type 'primary', got '{group.get('access_type')}'"

    # Update the group name
    user1_group_api.update_group(group_id, "Updated Group Name")

    # Verify the updated group name
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found after update"
    assert group["group_name"] == "Updated Group Name", f"Expected 'Updated Group Name', but got '{group['group_name']}'"
    assert group["access_type"] == "primary", f"Expected access_type 'primary' after update, got '{group.get('access_type')}'"

    # Clean up
    user1_group_api.delete_group(group_id)

def test_update_subgroup(test_user1):
    # Create a main group and subgroup
    user1_group_api = Group(test_user1)
    main_group_id = user1_group_api.create_group("Main Group")
    subgroup_id = user1_group_api.create_subgroup(main_group_id, "Original Subgroup Name")

    # Verify the initial subgroup name
    expected_structure = {
        "group_id": main_group_id,
        "group_name": "Main Group",
        "access_type": "primary",
        "subgroups": [
            {
                "subgroup_id": subgroup_id,
                "subgroup_name": "Original Subgroup Name"
            }
        ]
    }
    user1_group_api.verify_group_structure(main_group_id, expected_structure)

    # Update the subgroup name
    user1_group_api.update_subgroup(main_group_id, subgroup_id, "Updated Subgroup Name")

    # Verify the updated subgroup name
    expected_structure_updated = {
        "group_id": main_group_id,
        "group_name": "Main Group",
        "access_type": "primary",
        "subgroups": [
            {
                "subgroup_id": subgroup_id,
                "subgroup_name": "Updated Subgroup Name"
            }
        ]
    }
    user1_group_api.verify_group_structure(main_group_id, expected_structure_updated)

    # Clean up
    user1_group_api.empty_and_delete_group(main_group_id)

def test_subgroup_member_cannot_rename_sibling_subgroup(test_user1, test_user2):
    """A user with subentity access to one subgroup must not be able to rename a
    different subgroup in the same group. Renaming the subgroup that WAS shared
    with them must still succeed. This guards the access check in UpdateSubGroup:
    it verifies the specific subGroupID, not just parent-group access."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)

    group_id = user1_group_api.create_group("Sibling Subgroup Auth Group")
    shared_subgroup_id = user1_group_api.create_subgroup(group_id, "Shared Subgroup")
    sibling_subgroup_id = user1_group_api.create_subgroup(group_id, "Sibling Subgroup")

    # Share only shared_subgroup_id with test_user2 (subentity access).
    user1_group_api.share_subgroup(group_id, shared_subgroup_id, test_user2.username)
    accept_sharing_request_for(test_user2, group_id, shared_subgroup_id)

    # Positive: test_user2 can rename the subgroup shared with them.
    user2_group_api.update_subgroup(group_id, shared_subgroup_id, "Renamed by Member")

    # Negative: test_user2 must NOT rename the sibling subgroup they don't have
    # access to. The handler maps the access failure to a 400.
    user2_group_api.update_subgroup(
        group_id, sibling_subgroup_id, "Hijacked Sibling Name", expected_status=400)

    # Verify the sibling subgroup name is unchanged for the owner.
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None
    sibling = next((s for s in group.get("subgroups", []) if s["subgroup_id"] == sibling_subgroup_id), None)
    assert sibling is not None, "Sibling subgroup should still exist"
    assert sibling["subgroup_name"] == "Sibling Subgroup", \
        f"Sibling subgroup name should be unchanged, but got '{sibling['subgroup_name']}'"

    # Clean up
    user1_group_api.empty_and_delete_group(group_id)

def test_list_groups_with_subgroups_and_nodes(test_user1, valid_device):
    # Create a main group
    user1_group_api = Group(test_user1)
    main_group_id = user1_group_api.create_group("Main Group with Subgroups and Nodes")

    # Create multiple subgroups
    subgroup_names = ["Subgroup 1", "Subgroup 2"]
    subgroup_ids = []
    for subgroup_name in subgroup_names:
        subgroup_ids.append(user1_group_api.create_subgroup(main_group_id, subgroup_name))

    # Associate the node with the main group
    result = test_user1.do_user_node_assoc(valid_device, main_group_id)
    assert result == None, f"Association failed with error: {result}"

    # Add the node to the first subgroup
    user1_group_api.add_node_to_subgroup(main_group_id, subgroup_ids[0], valid_device.node_thing_name)

    # Verify the group structure
    expected_structure = {
        "group_id": main_group_id,
        "group_name": "Main Group with Subgroups and Nodes",
        "access_type": "primary",
        "node_ids": [valid_device.node_thing_name],
        "subgroups": [
            {
                "subgroup_id": subgroup_ids[0],
                "subgroup_name": subgroup_names[0],
                "node_ids": [valid_device.node_thing_name]
            },
            {
                "subgroup_id": subgroup_ids[1],
                "subgroup_name": subgroup_names[1]
            }
        ]
    }
    user1_group_api.verify_group_structure(main_group_id, expected_structure)
    user1_group_api.empty_and_delete_group(main_group_id)

def test_list_groups_create_subgroups_and_add_nodes_from_different_user(test_user2, test_user1, valid_device):
    # Create a group with the other user
    user2_group_api = Group(test_user2)
    user1_group_api = Group(test_user1)
    other_group_id = user2_group_api.create_group("Other User's Group")

    # Create a subgroup in the other user's group
    other_subgroup_id = user2_group_api.create_subgroup(other_group_id, "Other User's Subgroup")

    # Add a node to the other user's group
    test_user2.do_user_node_assoc(valid_device, other_group_id)

    # Now, try to list groups with the main test user
    list_groups_data = user1_group_api.list_groups()

    # Verify that the other user's group is not in the list
    assert not any(g["group_id"] == other_group_id for g in list_groups_data["groups"]), f"Other user's group {other_group_id} should not be visible"

    # Try to create a subgroup in the other user's group
    try:
        user1_group_api.create_subgroup(other_group_id, "Unauthorized Subgroup")
        assert False, "Creating a subgroup in another user's group should fail"
    except Exception as e:
        assert "but got 500" in str(e), f"Unexpected error message: {str(e)}"

    # Try to add a node to the subgroup in the other user's group
    try:
        user1_group_api.add_node_to_subgroup(other_group_id, other_subgroup_id, "test-node-id")
        assert False, "Adding a node to a subgroup in another user's group should fail"
    except Exception as e:
        assert "but got 500" in str(e), f"Unexpected error message: {str(e)}"

    # Verify that the node was not added to the other user's group or subgroup
    other_list_groups_data = user2_group_api.list_groups()
    other_group = next((group for group in other_list_groups_data["groups"] if group["group_id"] == other_group_id), None)
    assert other_group is not None, f"Other user's group {other_group_id} not found in the list of groups"
    assert other_group["access_type"] == "primary", f"Expected access_type 'primary' for owner, got '{other_group.get('access_type')}'"
    assert "node_ids" in other_group and valid_device.node_thing_name in other_group["node_ids"], f"Original node {valid_device.node_thing_name} should still be in the other user's group"
    assert len(other_group["node_ids"]) == 1, f"No additional nodes should be in the other user's group"
    other_subgroup = next((subgroup for subgroup in other_group.get("subgroups", []) if subgroup["subgroup_id"] == other_subgroup_id), None)
    assert other_subgroup is not None, f"Other user's subgroup {other_subgroup_id} not found in the group"
    assert "node_ids" not in other_subgroup or len(other_subgroup["node_ids"]) == 0, f"No nodes should be in the other user's subgroup"
    user2_group_api.empty_and_delete_group(other_group_id)

def test_user_node_assoc_group_migration(test_user1, valid_device_rsa):
    """
    Test the group migration functionality, including group creation after MQTT connection has already been established.
    """

    # Associate the node with the first group
    user1_group_api = Group(test_user1)
    group_id_1 = user1_group_api.create_group("Test Group 1")
    result = test_user1.do_user_node_assoc(valid_device_rsa, group_id_1)
    assert result == None, f"Association failed with error: {result}"

    # Connect to MQTT
    test_user1.mqtt_connect()

    try:
        # Fake publish to test authorization
        assert test_user1.mqtt_publish_to_topic(valid_device_rsa.node_thing_name, f"params-{group_id_1}/params", {"test": "test"}), f"Failed to publish update for group {group_id_1}"
        
        # Now associate the same node with the second group
        group_id_2 = user1_group_api.create_group("Test Group 2")
        result = test_user1.do_user_node_assoc(valid_device_rsa, group_id_2)
        assert result == None, f"Association failed with error: {result}"

        # Refresh MQTT credentials
        assert test_user1.mqtt_refresh_credentials(), "Failed to refresh MQTT credentials"

        # Verify that the node is now in the second group
        list_groups_data = user1_group_api.list_groups()
        group_2 = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id_2), None)
        assert group_2 is not None, f"Created group {group_id_2} not found in the list of groups"
        assert valid_device_rsa.node_thing_name in group_2["node_ids"], f"Node {valid_device_rsa.node_thing_name} not found in the group's node_ids"

        # Verify that the node is no longer in the first group
        group_1 = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id_1), None)
        assert group_1 is not None, f"Created group {group_id_1} not found in the list of groups"
        assert "node_ids" not in group_1, f"node_ids should not be in the group"
        user1_group_api.empty_and_delete_group(group_id_1)
        user1_group_api.empty_and_delete_group(group_id_2)

        # Fake publish to test authorization
        assert test_user1.mqtt_publish_to_topic(valid_device_rsa.node_thing_name, f"params-{group_id_2}/params", {"test": "test"}), f"Failed to publish update for group {group_id_2}"
    except Exception as e:
        test_user1.mqtt_disconnect_and_wait()
        raise e
    test_user1.mqtt_disconnect_and_wait()


# ---------------------------------------------------------------------------
# Integration tests: group_id IoT Thing attribute lifecycle (Group Control Feature)
# ---------------------------------------------------------------------------

def test_group_id_attribute_set_on_node_association(associated_device):
    """After a node is associated with a group the IoT Thing's group_id attribute
    must equal the group ID so the device-level static IoT policy can authorise
    subscription to the group-control topic.
    """
    device, group_id, _user, _group_api = associated_device
    attrs = describe_thing_attributes(device.node_thing_name, REGION)
    assert attrs.get('group_id') == group_id, (
        f"Expected group_id={group_id!r} on thing {device.node_thing_name!r}, "
        f"got {attrs.get('group_id')!r}"
    )


def test_delete_group_full_cleanup(test_user1, valid_device):
    """Delete group with the empty-check contract.

    A populated group delete is rejected with 409; the group is removed only
    after it is emptied. Node-level cleanup (shadow params, thing attributes,
    schedules, triggers, automations) happens during the per-node remove that
    empty_and_delete_group performs, so those are verified afterwards.
    """
    node_id = valid_device.node_thing_name
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Delete Group Full Cleanup")

    # --- Setup: associate and seed data ---
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"

    # Seed schedule (API contract is snake_case "schedules")
    assert test_user1.set_node_schedule(group_id, "", node_id, {
        "schedules": [{"id": "s1", "name": "Morning", "enabled": True}],
    }), "Failed to set schedule"

    # Seed trigger
    assert test_user1.set_node_trigger(group_id, node_id,
        json.dumps({"triggers": [{"id": "t1", "name": "TempHigh"}]}),
    ), "Failed to set trigger"

    # Seed two automations that exercise both node-cleanup paths on removal:
    #  - condition automation: node is in the trigger condition (whole automation deleted)
    #  - action-only automation: node is only in the action target (target removed)
    # Condition trigger IDs are always "nodeID~automationID~triggerIndex", so the
    # only valid in-group node here is node_id.
    condition_auto = test_user1.create_automation(group_id, {
        "name": "Condition automation",
        "conditions": {"and": [f"{node_id}~placeholder~0"]},
        "actions": {"targets": [{"node": node_id, "path": "L.P", "value": True}]},
    })
    assert condition_auto is not None

    action_only_auto = test_user1.create_automation(group_id, {
        "name": "Action-only automation",
        "actions": {"targets": [{"node": node_id, "path": "L.P", "value": True}]},
    })
    assert action_only_auto is not None

    # Pre-condition: verify data exists
    attrs_before = describe_thing_attributes(node_id, REGION)
    assert attrs_before.get('group_id') == group_id, "Pre-condition: group_id attribute should be set"

    # --- Act: a populated group delete is rejected (no cascade) ---
    delete_resp = test_user1.make_api_request('DELETE', f'/v1/groups/{group_id}')
    assert delete_resp.status_code == 409, \
        f"Populated group delete should be rejected with 409, got {delete_resp.status_code}"

    # Emptying removes the node (and its data), then deletes the group.
    user1_group_api.empty_and_delete_group(group_id)

    # --- Assert: group is gone ---
    groups = user1_group_api.list_groups()
    assert all(g["group_id"] != group_id for g in groups.get("groups", [])), \
        "Group should not appear in list after deletion"

    # --- Assert: node-level cleanup done during the per-node remove ---

    # Shadow Params: deleted for node
    assert valid_device.get_shadow(f"params-{group_id}") is None, \
        "Group shadow should be deleted after the node is removed"

    # User Tags: iparams shadow still exists
    iparams = valid_device.get_shadow("iparams")
    assert iparams is not None, "iparams shadow should still exist"

    # Thing Attributes: group_id cleared
    attrs = describe_thing_attributes(node_id, REGION)
    assert attrs.get('group_id', '') == '', \
        f"group_id attribute should be cleared, got {attrs.get('group_id')!r}"

    # Schedules: deleted for node (async)
    wait_until(
        lambda: test_user1.get_node_schedule(group_id, "", node_id) is None,
        "Schedule should be deleted",
    )

    # Triggers: deleted for node (async)
    wait_until(
        lambda: test_user1.get_node_trigger(group_id, node_id) is None,
        "Trigger should be deleted",
    )

    # Automations for group deleted (async)
    wait_until(
        lambda: (test_user1.get_automations(group_id) or []) == [],
        "All automations should be deleted",
    )


def test_delete_group_rejected_when_not_empty(test_user1):
    """A group that still has a subgroup is not empty and cannot be deleted."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Reject Non-Empty Delete")

    # A subgroup alone makes the group non-empty.
    user1_group_api.create_subgroup(group_id, "Subgroup blocking delete")

    delete_resp = test_user1.make_api_request('DELETE', f'/v1/groups/{group_id}')
    assert delete_resp.status_code == 409, \
        f"Non-empty group delete should be rejected with 409, got {delete_resp.status_code}"

    user1_group_api.empty_and_delete_group(group_id)
    groups = user1_group_api.list_groups()
    assert all(g["group_id"] != group_id for g in groups.get("groups", [])), \
        "Group should be gone after empty_and_delete_group"


def test_delete_group_secondary_user_denied(test_user1, test_user2):
    """Test that a secondary user cannot delete a group."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Secondary Delete Test")

    # Share group with user2 as secondary
    user1_group_api.share_group(group_id, test_user2.username, "secondary")
    accept_sharing_request_for(test_user2, group_id, "")

    # User2 (secondary) tries to delete group — should fail
    delete_resp = test_user2.make_api_request('DELETE', f'/v1/groups/{group_id}')
    assert delete_resp.status_code != 200, \
        f"Secondary user should not be able to delete group, got {delete_resp.status_code}"

    # Verify group still exists
    groups_data = user1_group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is not None, "Group should still exist after secondary user's delete attempt"

    user1_group_api.delete_group(group_id)


def test_delete_group_subentity_user_denied(test_user1, test_user2, valid_device):
    """Test that a subentity user cannot delete a group."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Subentity Delete Test")

    # Create subgroup and add node so we can share the subgroup
    subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"
    user1_group_api.add_node_to_subgroup(group_id, subgroup_id, valid_device.node_thing_name)

    # Share subgroup with user2 (gives subentity access)
    user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
    accept_sharing_request_for(test_user2, group_id, subgroup_id)

    # User2 (subentity) tries to delete group — should fail
    delete_resp = test_user2.make_api_request('DELETE', f'/v1/groups/{group_id}')
    assert delete_resp.status_code != 200, \
        f"Subentity user should not be able to delete group, got {delete_resp.status_code}"

    # Verify group still exists
    groups_data = user1_group_api.list_groups()
    grp = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert grp is not None, "Group should still exist after subentity user's delete attempt"

    user1_group_api.empty_and_delete_group(group_id)


def test_group_id_attribute_unchanged_on_subgroup_operations(associated_device):
    """Adding / removing a node from a subgroup must NOT alter the group_id attribute —
    subgroup membership is separate from primary-group membership.
    """
    device, group_id, _user, user1_group_api = associated_device
    thing_name = device.node_thing_name

    initial_attrs = describe_thing_attributes(thing_name, REGION)
    assert initial_attrs.get('group_id') == group_id, (
        f"Pre-condition failed: expected group_id={group_id!r}, "
        f"got {initial_attrs.get('group_id')!r}"
    )

    subgroup_id = user1_group_api.create_subgroup(group_id, "attr-subgroup-test")
    user1_group_api.add_node_to_subgroup(group_id, subgroup_id, thing_name)

    attrs_after_add = describe_thing_attributes(thing_name, REGION)
    assert attrs_after_add.get('group_id') == group_id, (
        f"group_id changed after subgroup add! "
        f"Expected {group_id!r}, got {attrs_after_add.get('group_id')!r}"
    )

    user1_group_api.remove_node_from_subgroup(group_id, subgroup_id, thing_name)

    attrs_after_remove = describe_thing_attributes(thing_name, REGION)
    assert attrs_after_remove.get('group_id') == group_id, (
        f"group_id changed after subgroup remove! "
        f"Expected {group_id!r}, got {attrs_after_remove.get('group_id')!r}"
    )


# ---------------------------------------------------------------------------
# Group/subgroup control tests (topic: …/[subgroups/<sgID>/]control, device-type-addressed)
# ---------------------------------------------------------------------------

def test_subgroup_control_reaches_subgroup_members(two_devices_same_group):
    """A publish on the device-type-addressed subgroup control topic is delivered
    to devices that subscribe to it.

    Topic: rainmaker/nodes/groups/<groupID>/subgroups/<subgroupID>/control

    Note: the IoT policy scopes subscribe permission by group_id only (no
    sub_group_ids attribute), so any device in the group can in principle
    subscribe to any subgroup's control topic. Cross-subgroup isolation is a
    firmware-layer concern — the firmware uses `getGroupInfo` to know which
    subgroups it belongs to and only subscribes to those. See §5.3 of
    docs/en/specs/group-control-feature.md.
    """
    device_in, _device_out, group_id, user, group_api = two_devices_same_group

    subgroup_id = group_api.create_subgroup(group_id, "DevType Control Subgroup")
    group_api.add_node_to_subgroup(group_id, subgroup_id, device_in.node_thing_name)

    subgroup_topic = f"rainmaker/nodes/groups/{group_id}/subgroups/{subgroup_id}/control"

    assert device_in.subscribe(topic=subgroup_topic), \
        "Subgroup-member device failed to subscribe to devtype-control subgroup topic"

    user.mqtt_connect()
    try:
        assert user.mqtt_refresh_credentials(), "Failed to refresh MQTT credentials"

        payload = {
            "esp.device.fan": {"params": {"esp.param.power": False}},
        }
        assert user.mqtt_publish_to_group_control(
            group_id, payload, subgroup_id=subgroup_id
        ), "User failed to publish devtype-control subgroup command"

        msg_in = device_in.wait_for_params_message(timeout=10)
        assert msg_in is not None, \
            "Subgroup-member device should receive devtype-control subgroup broadcast"
        assert msg_in.get("esp.device.fan", {}).get("params", {}).get("esp.param.power") is False, \
            f"Subgroup-member device unexpected payload: {msg_in}"
    finally:
        user.mqtt_disconnect_and_wait()


def test_group_control_reaches_all_devices_with_payload_intact(two_devices_same_group):
    """A multi-device-type publish on the group control topic is delivered to
    every device in the group with the payload intact (cloud fans out unchanged;
    per-device-type filtering is the firmware's responsibility).

    Topic: rainmaker/nodes/groups/<groupID>/control
    Payload is keyed by device type with a nested params envelope.
    """
    device1, device2, group_id, user, _group_api = two_devices_same_group

    devtype_topic = f"rainmaker/nodes/groups/{group_id}/control"
    assert device1.subscribe(topic=devtype_topic), \
        "device1 failed to subscribe to devtype-control group topic"
    assert device2.subscribe(topic=devtype_topic), \
        "device2 failed to subscribe to devtype-control group topic"

    user.mqtt_connect()
    try:
        assert user.mqtt_refresh_credentials(), "Failed to refresh MQTT credentials"

        payload = {
            "esp.device.light": {"params": {"esp.param.power": True, "esp.param.brightness": 75}},
            "esp.device.fan": {"params": {"esp.param.power": False}},
        }
        assert user.mqtt_publish_to_group_control(group_id, payload), \
            "User failed to publish multi-device-type devtype-control command"

        msg1 = device1.wait_for_params_message(timeout=10)
        msg2 = device2.wait_for_params_message(timeout=10)
        assert msg1 is not None, "device1 did not receive devtype-control group broadcast"
        assert msg2 is not None, "device2 did not receive devtype-control group broadcast"
        assert msg1 == payload, f"device1 payload altered in transit: got {msg1}, expected {payload}"
        assert msg2 == payload, f"device2 payload altered in transit: got {msg2}, expected {payload}"
    finally:
        user.mqtt_disconnect_and_wait()


def test_group_control_not_received_by_non_member(associated_device, bare_device):
    """A device that is not a member of a group cannot receive its group control broadcasts.

    The device-level static IoT policy uses ${iot:Connection.Thing.Attributes[group_id]}.
    A device with no group_id attribute (not associated with any group) will have its
    connection terminated by AWS IoT Core when it attempts to subscribe to another
    group's control topic. As a result it must not receive any payload.

    Topic: rainmaker/nodes/groups/<groupID>/control
    """
    device_in_group, group_id, user, _group_api = associated_device

    # Create a fresh device that has never been associated with any group.
    # Its group_id IoT Thing attribute will be absent/empty.
    device_outside = bare_device()

    try:
        assert connect_device_with_retry(device_outside, max_retries=3, base_delay=2), \
            "Failed to connect non-member device"

        # Reconnect the in-group device so the IoT policy re-evaluates with the
        # group_id thing attribute that was set during association. Policy
        # variables like ${iot:Connection.Thing.Attributes[group_id]} are
        # resolved at connection time, not subscribe time.
        device_in_group.disconnect()
        device_in_group.clear_queues()
        assert device_in_group.connect(), \
            "Failed to reconnect in-group device"

        devtype_topic = f"rainmaker/nodes/groups/{group_id}/control"

        # In-group device subscribes successfully.
        assert device_in_group.subscribe(topic=devtype_topic), \
            "In-group device failed to subscribe to group control topic"

        # Non-member device's subscribe attempt will cause AWS IoT to terminate
        # the connection (policy variable resolves to empty string → no match).
        device_outside.subscribe(topic=devtype_topic)  # expected to fail; no assertion

        user.mqtt_connect()
        try:
            assert user.mqtt_refresh_credentials(), "Failed to refresh MQTT credentials"

            payload = {"esp.device.light": {"params": {"esp.param.power": True}}}
            assert user.mqtt_publish_to_group_control(group_id, payload), \
                "User failed to publish group control command"

            # In-group device receives the command.
            msg_in = device_in_group.wait_for_params_message(timeout=10)
            assert msg_in is not None, \
                "In-group device should receive the group control broadcast"
            assert msg_in.get("esp.device.light", {}).get("params", {}).get("esp.param.power") is True, \
                f"In-group device unexpected payload: {msg_in}"

            # Non-member device must not receive anything.
            time.sleep(2)  # extra wait to confirm no delayed delivery
            msg_out = device_outside.wait_for_params_message(timeout=2)
            assert msg_out is None, \
                "Non-member device must not receive group control broadcasts"
        finally:
            user.mqtt_disconnect_and_wait()
    finally:
        device_outside.disconnect()
        device_outside.destroy_test_node()

def test_delete_subgroup(associated_device):
    """
    Test subgroup deletion mechanisms:
    1. Verify a specific subgroup can be deleted.
    2. Verify deletion does not affect sibling subgroups.
    3. Verify parent group remains intact.

    Uses the pooled associated_device fixture so the parent group's lifecycle
    (and cleanup) is owned by the fixture.
    """
    _device, group_id, _test_user1, user1_group_api = associated_device

    sub1_id = user1_group_api.create_subgroup(group_id, "Sibling 1")
    target_sub_id = user1_group_api.create_subgroup(group_id, "Target To Delete")
    sub3_id = user1_group_api.create_subgroup(group_id, "Sibling 3")

    # 1. Pre-check: Verify all 3 exist
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    initial_ids = [s["subgroup_id"] for s in group.get("subgroups", [])]
    assert all(sid in initial_ids for sid in [sub1_id, target_sub_id, sub3_id]), "Setup failed: All subgroups should exist before deletion"

    # 2. Action: Delete the middle subgroup (delete_subgroup asserts a 200 status)
    user1_group_api.delete_subgroup(group_id, target_sub_id)

    # 3. Verification: Check resulting state
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)

    assert group is not None, "Parent group should still exist"

    current_subgroup_ids = [s["subgroup_id"] for s in group.get("subgroups", [])]

    # Assert target is gone
    assert target_sub_id not in current_subgroup_ids, f"Target subgroup {target_sub_id} should be deleted"

    # Assert siblings remain
    assert sub1_id in current_subgroup_ids, "Sibling 1 should still exist"
    assert sub3_id in current_subgroup_ids, "Sibling 3 should still exist"

def test_delete_subgroup_rejected_when_has_node(associated_device):
    """A subgroup that still holds a user node cannot be deleted (409); once the
    node is removed the subgroup deletes (200) without removing the node itself.
    """
    device, group_id, _test_user1, user1_group_api = associated_device

    sub_id = user1_group_api.create_subgroup(group_id, "Subgroup With Node")
    user1_group_api.add_node_to_subgroup(group_id, sub_id, device.node_thing_name)

    # Subgroup still has a node -> rejected.
    user1_group_api.delete_subgroup(group_id, sub_id, expected_status=409)

    # Remove the node, then the subgroup deletes successfully.
    user1_group_api.remove_node_from_subgroup(group_id, sub_id, device.node_thing_name)
    user1_group_api.delete_subgroup(group_id, sub_id)

    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None, "Parent group should still exist"
    current_subgroup_ids = [s["subgroup_id"] for s in group.get("subgroups", [])]
    assert sub_id not in current_subgroup_ids, f"Subgroup {sub_id} should be deleted"


def test_delete_subgroup_unauthorized_user(test_user1, test_user2):
    """An unauthorized user should not be able to delete another user's subgroup."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group("Auth Test Group")
    subgroup_id = user1_group_api.create_subgroup(group_id, "Protected Subgroup")

    # Attempt to delete as user2 (group is not accessible to them -> 400)
    user2_group_api.delete_subgroup(group_id, subgroup_id, expected_status=400)

    # Verify subgroup still exists for user1
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None
    remaining_ids = [s["subgroup_id"] for s in group.get("subgroups", [])]
    assert subgroup_id in remaining_ids, "Subgroup should still exist after unauthorized delete attempt"

    # Clean up
    user1_group_api.empty_and_delete_group(group_id)

def test_delete_nonexistent_subgroup(test_user1):
    """Deleting a non-existent subgroup should return 404."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("404 Test Group")

    user1_group_api.delete_subgroup(group_id, "non-existent-subgroup-id", expected_status=404)

    # Clean up
    user1_group_api.delete_group(group_id)

def test_delete_subgroup_cleans_up_shared_user_mappings(shared_subgroup):
    """
    When a subgroup is deleted, shared users should lose access to it, while
    access to a sibling shared subgroup is preserved.

    Reuses the shared_subgroup fixture for the share+accept flow; the pooled
    group's lifecycle (and cleanup) is owned by the fixture.
    """
    from test.itest.conftest import accept_sharing_request_for

    data = shared_subgroup.share_subgroup()
    group_id = data["group_id"]
    target_sub_id = data["subgroup_id"]
    user1_group_api = data["user1_group_api"]
    user2_group_api = data["user2_group_api"]
    test_user2 = data["test_user2"]

    # Share a sibling subgroup with the same user, to assert it survives the delete.
    sibling_sub_id = user1_group_api.create_subgroup(group_id, "Sibling Shared Subgroup")
    user1_group_api.share_subgroup(group_id, sibling_sub_id, test_user2.username)
    accept_sharing_request_for(test_user2, group_id, sibling_sub_id)

    # Verify user2 sees both before deletion
    shared_group = next((g for g in user2_group_api.list_groups()["groups"] if g["group_id"] == group_id), None)
    assert shared_group is not None, "Shared group should appear for user2"
    shared_sub_ids = [s["subgroup_id"] for s in shared_group.get("subgroups", [])]
    assert target_sub_id in shared_sub_ids, "Target shared subgroup should appear for user2"
    assert sibling_sub_id in shared_sub_ids, "Sibling shared subgroup should appear for user2"

    # Owner deletes only the target subgroup
    user1_group_api.delete_subgroup(group_id, target_sub_id)

    # Verify user2 lost the deleted subgroup but kept the sibling
    shared_group = next((g for g in user2_group_api.list_groups()["groups"] if g["group_id"] == group_id), None)
    assert shared_group is not None, "User2 should still see the group (has access to the sibling subgroup)"
    remaining_ids = [s["subgroup_id"] for s in shared_group.get("subgroups", [])]
    assert target_sub_id not in remaining_ids, "Deleted subgroup should no longer appear for shared user"
    assert sibling_sub_id in remaining_ids, "Sibling shared subgroup should still be visible to user2"

def test_delete_subgroup_removes_access_for_group_shared_user(shared_group):
    """
    A user with full (primary) group access should lose visibility of a subgroup
    once it is deleted, while retaining access to the group itself.
    """
    data = shared_group.share_primary()
    group_id = data["group_id"]
    user1_group_api = data["user1_group_api"]
    user2_group_api = data["user2_group_api"]

    # Owner creates a subgroup; the group-shared user sees it via their group access.
    subgroup_id = user1_group_api.create_subgroup(group_id, "Subgroup In Shared Group")

    shared = next((g for g in user2_group_api.list_groups()["groups"] if g["group_id"] == group_id), None)
    assert shared is not None, "Group-shared user should see the group"
    assert any(s["subgroup_id"] == subgroup_id for s in shared.get("subgroups", [])), \
        "Group-shared user should see the new subgroup"

    # Owner deletes the subgroup
    user1_group_api.delete_subgroup(group_id, subgroup_id)

    # The subgroup is gone for the shared user, but the group remains accessible.
    shared = next((g for g in user2_group_api.list_groups()["groups"] if g["group_id"] == group_id), None)
    assert shared is not None, "Group-shared user should still see the group after subgroup deletion"
    remaining_ids = [s["subgroup_id"] for s in shared.get("subgroups", [])]
    assert subgroup_id not in remaining_ids, "Deleted subgroup should no longer appear for the group-shared user"


# Cross-tenant tests; see test_automations.py for the origin of this class.

def test_add_foreign_node_to_own_subgroup_denied(two_tenants):
    """A cannot pull B's node into A's own subgroup.

    Adding a node to a subgroup should require that the node is already a member
    of the parent group. If A can add B's node to A's subgroup, A gains control
    of B's device.
    """
    tenant_a, tenant_b = two_tenants
    group_a = tenant_a["group_id"]

    subgroup_id = tenant_a["group_api"].create_subgroup(group_a, "A subgroup")
    assert subgroup_id is not None

    # add_node_to_subgroup asserts a 200 internally, so a denial raises. Treat
    # both an exception and a non-membership as denied.
    escalated = False
    try:
        tenant_a["group_api"].add_node_to_subgroup(group_a, subgroup_id, tenant_b["node_id"])
        groups = tenant_a["group_api"].list_groups()
        g = next((x for x in groups["groups"] if x["group_id"] == group_a), None)
        node_ids = set()
        if g:
            node_ids.update(g.get("node_ids", []) or [])
            for sg in g.get("subgroups", []) or []:
                node_ids.update(sg.get("node_ids", []) or [])
        escalated = tenant_b["node_id"] in node_ids
    except Exception:
        escalated = False

    assert not escalated, "Foreign node was added to caller's subgroup (cross-tenant capture)"


def test_remove_foreign_node_from_group_denied(two_tenants):
    """A cannot remove B's node from B's group (denial of service on B)."""
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]

    resp = user_a.make_api_request(
        "DELETE", f"/v1/groups/{tenant_b['group_id']}/nodes/{tenant_b['node_id']}")
    # Deployed handler answers 500 instead of a clean 403 — denied, but sloppy
    # error mapping, so this asserts >=400 and checks the real property below.
    assert resp.status_code >= 400, (
        f"Removing a foreign node from a foreign group returned {resp.status_code}; "
        f"expected an error. Body: {resp.text}"
    )
    groups_b = tenant_b["group_api"].list_groups()
    g = next((x for x in groups_b["groups"] if x["group_id"] == tenant_b["group_id"]), None)
    assert g is not None and tenant_b["node_id"] in (g.get("node_ids", []) or []), \
        "Foreign node removed from its group by a non-member (DoS)"

def test_delete_foreign_group_denied(two_tenants):
    """A cannot delete B's group."""
    tenant_a, tenant_b = two_tenants
    resp = tenant_a["user"].make_api_request(
        "DELETE", f"/v1/groups/{tenant_b['group_id']}")
    assert resp.status_code >= 400, (
        f"Deleting a foreign group returned {resp.status_code}, expected an error."
    )
    groups_b = tenant_b["group_api"].list_groups()
    assert any(x["group_id"] == tenant_b["group_id"] for x in groups_b["groups"]), \
        "Foreign group deleted by a non-member"


def test_add_capabilities_to_foreign_group_denied(two_tenants):
    """A must not enable capabilities (e.g. Matter fabric) on B's group."""
    tenant_a, tenant_b = two_tenants
    resp = tenant_a["group_api"].add_group_capabilities(tenant_b["group_id"], ["matter"])
    assert resp.status_code >= 400, (
        f"Added capabilities to a foreign group returned {resp.status_code}: {resp.text[:150]}"
    )


def test_remove_user_from_foreign_group_denied(two_tenants):
    """A must not remove B (the owner) or any user from B's group."""
    tenant_a, tenant_b = two_tenants
    user_a, user_b = tenant_a["user"], tenant_b["user"]

    resp = user_a.make_api_request(
        "DELETE", f"/v1/groups/{tenant_b['group_id']}/users/{user_b.sub}")
    assert resp.status_code >= 400, (
        f"Removing the owner from a foreign group returned {resp.status_code}: {resp.text[:150]}"
    )
    b_groups = user_b.make_api_request("GET", "/v1/groups").json()
    assert any(g["group_id"] == tenant_b["group_id"] for g in b_groups["groups"]), \
        "Owner lost access to their own group via a non-member's removal call"


def test_subgroup_crud_on_foreign_group_denied(two_tenants):
    """A must not create/rename/delete subgroups in B's group."""
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    group_b = tenant_b["group_id"]

    r_create = user_a.make_api_request(
        "POST", f"/v1/groups/{group_b}/subgroups",
        data=json.dumps({"group_name": "evil-sub"}))
    assert r_create.status_code >= 400, (
        f"Created a subgroup in a foreign group returned {r_create.status_code}: {r_create.text[:150]}"
    )

    sg_id = tenant_b["group_api"].create_subgroup(group_b, "B private sub")
    assert sg_id is not None
    r_rename = user_a.make_api_request(
        "PATCH", f"/v1/groups/{group_b}/subgroups/{sg_id}",
        data=json.dumps({"group_name": "hijacked-sub"}))
    assert r_rename.status_code >= 400, (
        f"Renamed a foreign subgroup returned {r_rename.status_code}: {r_rename.text[:150]}"
    )
    r_delete = user_a.make_api_request(
        "DELETE", f"/v1/groups/{group_b}/subgroups/{sg_id}")
    assert r_delete.status_code >= 400, (
        f"Deleted a foreign subgroup returned {r_delete.status_code}: {r_delete.text[:150]}"
    )
    b_groups = tenant_b["group_api"].list_groups()
    g = next((x for x in b_groups["groups"] if x["group_id"] == group_b), None)
    sub = next((s for s in (g.get("subgroups", []) or []) if s["subgroup_id"] == sg_id), None)
    assert sub is not None and sub.get("group_name") != "hijacked-sub", \
        "Foreign subgroup was renamed or deleted by a non-member"


def test_remove_foreign_node_from_foreign_subgroup_denied(two_tenants):
    """A must not remove B's node from B's subgroup."""
    tenant_a, tenant_b = two_tenants
    group_b = tenant_b["group_id"]

    sg_id = tenant_b["group_api"].create_subgroup(group_b, "B sub with node")
    tenant_b["group_api"].add_node_to_subgroup(group_b, sg_id, tenant_b["node_id"])

    resp = tenant_a["user"].make_api_request(
        "DELETE", f"/v1/groups/{group_b}/subgroups/{sg_id}/nodes/{tenant_b['node_id']}")
    assert resp.status_code >= 400, (
        f"Removed a foreign node from a foreign subgroup returned {resp.status_code}: {resp.text[:150]}"
    )
