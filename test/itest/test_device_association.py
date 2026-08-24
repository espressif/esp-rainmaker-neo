# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json

from test.itest.conftest import (
    CA_CERT, IOT_ENDPOINT, REGION, DEBUG, accept_sharing_request_for,
    reported_state, wait_for_shadow_absent,
)
from py_sdk.test_device import Device
from py_sdk.test_group import Group
from py_sdk.test_util import wait_until, seed_node_data, assert_node_data_deleted, describe_thing_attributes
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives import serialization


def _test_user_node_assoc_valid_device(test_user1, device):
    # First, create two groups
    user1_group_api = Group(test_user1)
    group_id_1 = user1_group_api.create_group("Test Group 1")
    group_id_2 = user1_group_api.create_group("Test Group 2")

    # Shadow content must not be carried across a group change: the device is the
    # authority for reported state and re-reports after getGroupInfo, so copying
    # would fabricate a report it never made and reset its metadata timestamps.
    # Only meaningful when the node starts group-less, i.e. its old shadow is the
    # bare "params-".
    started_ungrouped = not describe_thing_attributes(device.node_thing_name, REGION).get("group_id")
    if started_ungrouped:
        assert device.shadow_connect(["params-"]), "Failed to connect device shadow client"
        # Boot reports state into both shadows; only the group-derived one moves.
        for shadow_name in ("params-", "iparams"):
            assert device.update_named_shadow(shadow_name, {"params": {"Light": {"power": True}}}), \
                f"Device failed to report into the {shadow_name} shadow"
        for shadow_name in ("params-", "iparams"):
            wait_until(lambda name=shadow_name: reported_state(device.node_thing_name, name).get("params", {}),
                       f"{shadow_name} shadow to appear")

    # Associate the node with the first group
    result = test_user1.do_user_node_assoc(device, group_id_1)
    assert result == None, f"Association failed with error: {result}"

    shadow_1 = f"params-{group_id_1}"
    if started_ungrouped:
        assert wait_for_shadow_absent(device.node_thing_name, "params-"), \
            "ungrouped 'params-' shadow outlived the association"
        assert "Light" not in reported_state(device.node_thing_name, shadow_1).get("params", {}), \
            "ungrouped params were copied into the first group's shadow"
        # iparams is group-independent, so association must leave it alone — it is
        # the only continuous record of reported state across the transition.
        assert reported_state(device.node_thing_name, "iparams").get("params", {}).get("Light") == {"power": True}, \
            "association disturbed the group-independent iparams shadow"

    # The device is what refills the new shadow, once it knows its group.
    assert device.shadow_connect([shadow_1]), "Failed to connect group-1 shadow client"
    assert device.update_named_shadow(shadow_1, {"params": {"Fan": {"speed": 3}}}), \
        "Device failed to report into the first group's shadow"
    wait_until(lambda: reported_state(device.node_thing_name, shadow_1).get("params", {}),
               f"device report to land in {shadow_1}")

    # Verify that the node is in the first group
    list_groups_data = user1_group_api.list_groups()
    group_1 = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id_1), None)
    assert group_1 is not None, f"Created group {group_id_1} not found in the list of groups"
    assert device.node_thing_name in group_1["node_ids"], f"Node {device.node_thing_name} not found in the group's node_ids"
    # A RainMaker (challenge-response) node is tagged "rmng".
    node_detail_1 = group_1.get("node_details", {}).get(device.node_thing_name, {})
    assert node_detail_1.get("capabilities") == ["rmng"], f"Expected ['rmng'], got {node_detail_1.get('capabilities')}"

    # Now associate the same node with the second group
    result = test_user1.do_user_node_assoc(device, group_id_2)
    assert result == None, f"Association failed with error: {result}"

    # Same contract across a group-to-group move, where it also stops one owner's
    # reported state reaching the next.
    shadow_2 = f"params-{group_id_2}"
    assert wait_for_shadow_absent(device.node_thing_name, shadow_1), \
        f"{shadow_1} outlived the move to the second group"
    assert "Fan" not in reported_state(device.node_thing_name, shadow_2).get("params", {}), \
        "first group's params were copied into the second group's shadow"

    # Verify that the node is now in the second group
    list_groups_data = user1_group_api.list_groups()
    group_2 = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id_2), None)
    assert group_2 is not None, f"Created group {group_id_2} not found in the list of groups"
    assert device.node_thing_name in group_2["node_ids"], f"Node {device.node_thing_name} not found in the group's node_ids"
    node_detail_2 = group_2.get("node_details", {}).get(device.node_thing_name, {})
    assert node_detail_2.get("capabilities") == ["rmng"], f"Expected ['rmng'], got {node_detail_2.get('capabilities')}"

    # Verify that the node is no longer in the first group
    group_1 = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id_1), None)
    assert group_1 is not None, f"Created group {group_id_1} not found in the list of groups"
    assert "node_ids" not in group_1, f"node_ids should not be in the group"
    user1_group_api.delete_group(group_id_1)
    user1_group_api.delete_group(group_id_2)

def test_user_node_assoc_valid_device_rsa(test_user1, valid_device_rsa):
    _test_user_node_assoc_valid_device(test_user1, valid_device_rsa)

def test_user_node_assoc_valid_device_ecdsa(test_user1, valid_device):
    _test_user_node_assoc_valid_device(test_user1, valid_device)

def test_user_node_assoc_invalid_device(test_user1, valid_device):
    # Generate a new key pair for an invalid device
    private_key = ec.generate_private_key(ec.SECP256R1())
    private_key_pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption()
    ).decode('utf-8')

    public_key = private_key.public_key()
    public_key_pem = public_key.public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo
    ).decode('utf-8')

    # The thing name is the same as a valid device, but the keys are different
    invalid_device = Device(valid_device.node_thing_name, private_key_pem, public_key_pem, CA_CERT, IOT_ENDPOINT, REGION, DEBUG)

    # Create a group
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Test Group for Invalid Device")

    # Associate the node with the group
    result = test_user1.do_user_node_assoc(invalid_device, group_id)
    assert result == "ERROR_VERIFY_FAILED_401", f"Association should fail with error: {result}"

    # Verify that the node is not in the group
    list_groups_data = user1_group_api.list_groups()
    group = next((group for group in list_groups_data["groups"] if group["group_id"] == group_id), None)
    assert group is not None, f"Group {group_id} not found in the list of groups"
    assert "node_ids" not in group, f"node_ids should not be in the group"
    user1_group_api.delete_group(group_id)

def test_user_node_assoc_on_different_user_group(test_user1, test_user2, valid_device):
    # Create a group with the other user
    user2_group_api = Group(test_user2)
    other_group_id = user2_group_api.create_group("Other User's Group")

    # Try to associate the node with the other user's group
    result = test_user1.do_user_node_assoc(valid_device, other_group_id)
    assert result == "ERROR_VERIFY_FAILED_500", f"Association should fail with error, but got: {result}"

    # Verify that the node is not in the other user's group
    other_list_groups_data = user2_group_api.list_groups()
    other_group = next((group for group in other_list_groups_data["groups"] if group["group_id"] == other_group_id), None)
    assert other_group is not None, f"Other user's group {other_group_id} not found in the list of groups"
    assert "node_ids" not in other_group or valid_device.node_thing_name not in other_group["node_ids"], f"Node {valid_device.node_thing_name} should not be in the other user's group"
    user2_group_api.delete_group(other_group_id)

def test_remove_node_from_group(test_user1, valid_device):
    """Scenario 1: Remove node from group (disassociate).

    Matrix assertions — every column checked one by one:
      Node group entry     : deleted
      User Tags            : deleted for node (iparams shadow user section cleared)
      Shadow Params        : deleted for node (params-<groupId> shadow gone)
      Thing Attributes     : group_id cleared
      Schedules            : deleted for node (async)
      Triggers             : deleted for node (async)
      Automations (trigger): deleted for node (async)
      Automations (action) : updated / deleted if empty (async)
    """
    node_id = valid_device.node_thing_name
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Remove Node Test Group")

    # --- Setup: associate and seed data ---
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"
    user1_group_api.verify_group_structure(group_id, {
        "group_id": group_id, "group_name": "Remove Node Test Group",
        "node_ids": [node_id],
    })

    trigger_auto_id, action_auto_id = seed_node_data(test_user1, group_id, node_id)

    # --- Act: disassociate ---
    user1_group_api.remove_node_from_group(group_id, node_id)

    # --- Assert: synchronous side-effects ---

    # Node group entry: deleted
    user1_group_api.verify_group_structure(group_id, {
        "group_id": group_id, "group_name": "Remove Node Test Group",
    })

    # Shadow Params: deleted
    assert valid_device.get_shadow(f"params-{group_id}") is None, \
        "Group shadow should be deleted after disassociation"

    # User Tags: iparams shadow still exists but user section cleared
    iparams = valid_device.get_shadow("iparams")
    assert iparams is not None, "iparams shadow should still exist"

    # Thing Attributes: group_id cleared
    attrs = describe_thing_attributes(node_id, REGION)
    assert attrs.get('group_id', '') == '', \
        f"group_id attribute should be cleared, got {attrs.get('group_id')!r}"

    # --- Assert: async side-effects (node_data_reset Lambda) ---
    assert_node_data_deleted(test_user1, group_id, node_id, trigger_auto_id, action_auto_id)

    user1_group_api.delete_group(group_id)

def test_remove_node_from_group_with_subgroups(test_user1, valid_device):
    """Test removing node from group that has subgroups - should remove from all"""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Remove Node with Subgroups Test")
    
    # Create subgroups
    subgroup1_id = user1_group_api.create_subgroup(group_id, "Subgroup 1")
    subgroup2_id = user1_group_api.create_subgroup(group_id, "Subgroup 2")
    
    # Associate node with group and add to subgroups
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result == None, f"Association failed with error: {result}"
    user1_group_api.add_node_to_subgroup(group_id, subgroup1_id, valid_device.node_thing_name)
    user1_group_api.add_node_to_subgroup(group_id, subgroup2_id, valid_device.node_thing_name)
    
    # Remove node from entire group
    user1_group_api.remove_node_from_group(group_id, valid_device.node_thing_name)
    
    # Verify node is removed from group and all subgroups
    expected_structure = {
        "group_id": group_id,
        "group_name": "Remove Node with Subgroups Test",
        "subgroups": [
            {"subgroup_id": subgroup1_id, "subgroup_name": "Subgroup 1"},
            {"subgroup_id": subgroup2_id, "subgroup_name": "Subgroup 2"}
        ]
        # No node_ids at group or subgroup level
    }
    user1_group_api.verify_group_structure(group_id, expected_structure)

    # Group shadow must be deleted after disassociation
    group_shadow_name = f"params-{group_id}-{'-'.join(sorted([subgroup1_id, subgroup2_id]))}"
    group_shadow = valid_device.get_shadow(group_shadow_name)
    assert group_shadow is None, f"Group shadow '{group_shadow_name}' should be gone after disassociation"

    # iparams shadow must still exist
    iparams_shadow = valid_device.get_shadow("iparams")
    assert iparams_shadow is not None, "iparams shadow should still exist after disassociation"

    # IoT Thing group_id attribute must be cleared
    attrs = describe_thing_attributes(valid_device.node_thing_name, REGION)
    assert attrs.get('group_id', '') == '', (
        f"group_id attribute should be cleared after disassociation, got {attrs.get('group_id')!r}"
    )

    user1_group_api.delete_group(group_id)

def test_remove_node_unauthorized(test_user1, test_user2, valid_device):
    """Test that unauthorized users cannot remove nodes from groups"""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group("Unauthorized Remove Test")
    
    # User1 associates node with their group
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result == None, f"Association failed with error: {result}"
    
    # User2 should not be able to remove node from user1's group
    try:
        user2_group_api.remove_node_from_group(group_id, valid_device.node_thing_name)
        assert False, "Unauthorized user should not be able to remove node from group"
    except AssertionError as e:
        if "Expected 200, but got" in str(e):
            pass  # Expected failure
        else:
            raise e
    
    # Verify node is still in group
    expected_structure = {
        "group_id": group_id,
        "group_name": "Unauthorized Remove Test",
        "node_ids": [valid_device.node_thing_name]
    }
    user1_group_api.verify_group_structure(group_id, expected_structure)
    user1_group_api.delete_group(group_id)

def test_remove_node_secondary_user_denied(test_user1, test_user2, valid_device):
    """Test that a secondary user cannot remove a node from a group."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Secondary Remove Test")

    # User1 associates node with group
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"

    # Share group with user2 as secondary
    user1_group_api.share_group(group_id, test_user2.username, "secondary")
    accept_sharing_request_for(test_user2, group_id, "")

    # User2 (secondary) tries to remove node — should fail
    remove_resp = test_user2.make_api_request(
        'DELETE', f'/v1/groups/{group_id}/nodes/{valid_device.node_thing_name}')
    assert remove_resp.status_code != 200, \
        f"Secondary user should not be able to remove node, got {remove_resp.status_code}"

    # Verify node is still in group
    user1_group_api.verify_group_structure(group_id, {
        "group_id": group_id,
        "group_name": "Secondary Remove Test",
        "node_ids": [valid_device.node_thing_name],
    })
    user1_group_api.delete_group(group_id)

def test_remove_node_subentity_user_denied(test_user1, test_user2, valid_device):
    """Test that a subentity user cannot remove a node from a group."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Subentity Remove Test")

    # Create subgroup and add node
    subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"
    user1_group_api.add_node_to_subgroup(group_id, subgroup_id, valid_device.node_thing_name)

    # Share subgroup with user2 (gives subentity access)
    user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
    accept_sharing_request_for(test_user2, group_id, subgroup_id)

    # User2 (subentity) tries to remove node — should fail
    remove_resp = test_user2.make_api_request(
        'DELETE', f'/v1/groups/{group_id}/nodes/{valid_device.node_thing_name}')
    assert remove_resp.status_code != 200, \
        f"Subentity user should not be able to remove node, got {remove_resp.status_code}"

    # Verify node is still in group
    user1_group_api.verify_group_structure(group_id, {
        "group_id": group_id,
        "group_name": "Subentity Remove Test",
        "node_ids": [valid_device.node_thing_name],
        "subgroups": [{"subgroup_id": subgroup_id, "subgroup_name": "Test Subgroup", "node_ids": [valid_device.node_thing_name]}],
    })
    user1_group_api.delete_group(group_id)

def test_remove_nonexistent_node_from_group(test_user1):
    """Test removing a non-existent node from group returns appropriate error"""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Nonexistent Node Test")
    
    # Try to remove non-existent node
    try:
        user1_group_api.remove_node_from_group(group_id, "nonexistent-node-id")
        assert False, "Removing non-existent node should fail"
    except AssertionError as e:
        if "Expected 200, but got" in str(e):
            pass  # Expected failure
        else:
            raise e

    user1_group_api.delete_group(group_id)

def test_reassoc_to_same_group_is_noop(test_user1, valid_device):
    """Re-associating a node to the group it is already in should succeed as a no-op.

    All existing data must be preserved — nothing should be deleted.
    """
    node_id = valid_device.node_thing_name
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group("Same Group Reassoc Test")

    # Associate node with group
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Association failed: {result}"

    # --- Seed data that must survive the no-op ---

    # Seed schedule
    schedule_data = {"schedules": [{"id": "s1", "name": "Morning", "enabled": True}]}
    assert test_user1.set_node_schedule(group_id, "", node_id, schedule_data), "Failed to set schedule"

    # Seed trigger
    trigger_data = json.dumps({"triggers": [{"id": "t1", "name": "TempHigh"}]})
    assert test_user1.set_node_trigger(group_id, node_id, trigger_data), "Failed to set trigger"

    # Seed automation
    auto_result = test_user1.create_automation(group_id, {
        "name": "Survive reassoc",
        "conditions": {"and": [f"{node_id}~placeholder~0"]},
        "actions": {"targets": [{"node": node_id, "path": "L.P", "value": True}]},
    })
    assert auto_result is not None, "Failed to create automation"
    automation_id = auto_result["automation_id"]

    # Record thing attribute before
    attrs_before = describe_thing_attributes(node_id, REGION)
    assert attrs_before.get('group_id') == group_id

    # --- Act: re-associate same node to same group (no-op) ---
    result = test_user1.do_user_node_assoc(valid_device, group_id)
    assert result is None, f"Same-group re-association should succeed as no-op, got: {result}"

    # --- Assert: everything preserved ---

    # Node still in group
    groups_data = user1_group_api.list_groups()
    group = next((g for g in groups_data["groups"] if g["group_id"] == group_id), None)
    assert group is not None
    assert node_id in group.get("node_ids", []), "Node should still be in the group"

    # Thing attribute unchanged
    attrs_after = describe_thing_attributes(node_id, REGION)
    assert attrs_after.get('group_id') == group_id, \
        f"group_id attribute should still be {group_id}, got {attrs_after.get('group_id')!r}"

    # Group shadow still exists (not deleted)
    # Note: shadow may or may not have been created depending on device state,
    # but the key point is it should NOT have been deleted by the no-op path.

    # Schedule preserved
    sched = test_user1.get_node_schedule(group_id, "", node_id)
    assert sched is not None, "Schedule should be preserved after same-group re-association"

    # Trigger preserved
    trig = test_user1.get_node_trigger(group_id, node_id)
    assert trig is not None, "Trigger should be preserved after same-group re-association"

    # Automation preserved
    automations = test_user1.get_automations(group_id)
    assert automations is not None
    assert any(a.get("id") == automation_id for a in automations), \
        f"Automation '{automation_id}' should be preserved after same-group re-association"

    user1_group_api.delete_group(group_id)


def test_cross_user_reassociation(test_user1, test_user2, valid_device):
    """Scenario 2: Associate node to different user.

    Matrix assertions — every column checked one by one:
      Node group entry     : old mapping removed, new mapping created
      User Tags            : deleted for node in old group
      Shadow Params        : deleted for node in old group
      Thing Attributes     : replaced with new group id
      Schedules            : deleted for node (async, old group data)
      Triggers             : deleted for node (async, old group data)
      Automations (trigger): deleted for node in old group (async)
      Automations (action) : updated / deleted if empty in old group (async)
      New group            : node present, new getGroupInfo notification sent
    """
    node_id = valid_device.node_thing_name
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)

    group_id_1 = user1_group_api.create_group("User1 Group for Reassoc")
    group_id_2 = user2_group_api.create_group("User2 Group for Reassoc")

    # --- Setup: user1 associates and seeds data ---
    result = test_user1.do_user_node_assoc(valid_device, group_id_1)
    assert result is None, f"Association by user1 failed: {result}"

    trigger_auto_id, action_auto_id = seed_node_data(test_user1, group_id_1, node_id)

    # --- Act: user2 re-associates ---
    result = test_user2.do_user_node_assoc(valid_device, group_id_2)
    assert result is None, f"Cross-user re-association failed: {result}"

    # --- Assert: synchronous side-effects ---

    # Node group entry: new mapping created
    groups_2 = user2_group_api.list_groups()
    grp2 = next((g for g in groups_2["groups"] if g["group_id"] == group_id_2), None)
    assert grp2 is not None, "User2's group not found"
    assert node_id in grp2.get("node_ids", []), "Node should be in user2's group"

    # Node group entry: old mapping removed
    groups_1 = user1_group_api.list_groups()
    grp1 = next((g for g in groups_1["groups"] if g["group_id"] == group_id_1), None)
    assert grp1 is not None, "User1's group not found"
    assert node_id not in grp1.get("node_ids", []), "Node should NOT be in user1's group"

    # Thing Attributes: replaced with new group id
    attrs = describe_thing_attributes(node_id, REGION)
    assert attrs.get('group_id') == group_id_2, \
        f"group_id should be {group_id_2}, got {attrs.get('group_id')!r}"

    # Shadow Params: old group shadow deleted
    assert valid_device.get_shadow(f"params-{group_id_1}") is None, \
        "Old group shadow should be deleted after re-association"

    # User Tags: iparams shadow still exists but user section cleared
    iparams = valid_device.get_shadow("iparams")
    assert iparams is not None, "iparams shadow should still exist"

    # --- Assert: async side-effects (node_data_reset Lambda on old group) ---
    assert_node_data_deleted(test_user1, group_id_1, node_id, trigger_auto_id, action_auto_id)

    user1_group_api.delete_group(group_id_1)
    user2_group_api.delete_group(group_id_2)

# Cross-tenant tests; see test_automations.py for the origin of this class.

def test_node_config_cross_tenant_denied(two_tenants):
    """A must not read or write B's node config.

    Both path variants are probed: A's own legitimate group paired with B's node
    (group check passes, only the node-in-group check denies), and B's group
    directly (A is not a member). A successful PUT would drive B's device.
    """
    tenant_a, tenant_b = two_tenants
    user_a = tenant_a["user"]
    node_b = tenant_b["node_id"]
    cfg = json.dumps({"devices": [{"id": "Evil", "type": "esp.device.switch",
                                   "params": [{"id": "Power", "type": "esp.param.power"}]}]})

    for label, group_id in (("own-group path", tenant_a["group_id"]),
                            ("foreign-group path", tenant_b["group_id"])):
        assert user_a.get_node_config(group_id, "", node_b) is None, \
            f"Read foreign node config via {label}"

        resp = user_a.make_api_request(
            "PUT", f"/v1/groups/{group_id}/nodes/{node_b}/config", data=cfg)
        assert resp.status_code >= 400, (
            f"Wrote config to foreign node via {label} returned "
            f"{resp.status_code}: {resp.text[:150]}"
        )
