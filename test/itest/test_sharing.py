# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import uuid
import time
import pytest
from test.itest.conftest import (
    accept_sharing_request_for,
    assert_subgroup_in_group,
    validate_user_group_dynamodb_entry,
    run_shared_group_stages,
    run_shared_subgroup_stages,
    CA_CERT,
    IOT_ENDPOINT,
    REGION,
    DEBUG,
    IDENTITY_POOL_ID,
    API_GATEWAY_URL,
)
from py_sdk.test_device import Device, generate_key_and_cert
from py_sdk.test_group import Group

def assert_sharing_request_has_primary_user_info(user, group_id, subgroup_id=""):
    """Assert that a sharing request for the given group contains primary user info."""
    sharing_requests = user.get_sharing_requests()
    assert sharing_requests is not None, "Failed to get sharing requests"
    for req in sharing_requests:
        req_subgroup = req.get('subgroup_id', '')
        if req['group_id'] == group_id and (req_subgroup == subgroup_id or (subgroup_id == "" and req_subgroup == '')):
            assert req.get('primary_user_id'), f"primary_user_id missing in sharing request {req['sharing_request_id']}"
            assert req.get('primary_email') or req.get('primary_phone_number'), \
                f"primary_email or primary_phone_number missing in sharing request {req['sharing_request_id']}"
            return
    assert False, f"Sharing request for group {group_id} subgroup '{subgroup_id}' not found"

def test_group_sharing_group_access(shared_group, subtests):
    def body(stage, data):
        if stage == "share_begin":
            groups = data["user2_group_api"].list_groups()
            assert not any(group['group_id'] == data['group_id'] for group in groups['groups']), "Unshared group still found in test_user2's groups"
        elif stage == "primary_share":
            subgroup_name = f"Test Subgroup {uuid.uuid4()}"
            subgroup_id = data["user2_group_api"].create_subgroup(data['group_id'], subgroup_name)
            assert subgroup_id is not None, "Failed to create subgroup with shared primary access"
            groups = data["user1_group_api"].list_groups()
            assert_subgroup_in_group(groups, data['group_id'], subgroup_id)
        elif stage == "primary_unshare":
            groups = data["user2_group_api"].list_groups()
            assert not any(group['group_id'] == data['group_id'] for group in groups['groups']), "Unshared group still found in test_user2's groups"

    run_shared_group_stages(shared_group, subtests, body)

def test_subgroup_sharing_access(shared_subgroup, subtests):
    def body(stage, data):
        user2_group_api = data["user2_group_api"]
        if stage in ("share_begin", "subgroup_unshare"):
            groups = user2_group_api.list_groups()
            shared_group = next((group for group in groups['groups'] if group['group_id'] == data['group_id']), None)
            assert shared_group is None or not any(subgroup['subgroup_id'] == data['subgroup_id'] for subgroup in shared_group.get('subgroups', [])), "Unshared subgroup still found in test_user2's groups"
        elif stage == "subgroup_share":
            groups = user2_group_api.list_groups()
            shared_group = next((group for group in groups['groups'] if group['group_id'] == data['group_id']), None)
            assert shared_group is not None, "Shared group not found in test_user2's groups"
            assert shared_group['access_type'] == 'subgroup', f"Expected 'subgroup' for subgroup-shared user, got '{shared_group.get('access_type')}'"
            assert any(subgroup['subgroup_id'] == data['subgroup_id'] for subgroup in shared_group.get('subgroups', [])), "Shared subgroup not found in test_user2's groups"

    run_shared_subgroup_stages(shared_subgroup, subtests, body)

def test_share_group_with_secondary_access(test_user1, test_user2, test_user3):
    # Create a group for test_user1
    group_name = f"Test Group {uuid.uuid4()}"
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    user3_group_api = Group(test_user3)
    group_id = user1_group_api.create_group(group_name)
    assert group_id is not None, "Failed to create group"

    # Share the group with test_user2 with secondary access
    user1_group_api.share_group(group_id, test_user2.username, "secondary")

    assert_sharing_request_has_primary_user_info(test_user2, group_id)
    accept_sharing_request_for(test_user2, group_id, "")

    # Attempt to share the group from test_user2 to test_user3 (should fail)
    with pytest.raises(Exception):  # Adjust the exception type as needed
        user2_group_api.share_group(group_id, test_user3.username, "secondary")

    # Share the group with test_user2 with primary access
    user1_group_api.share_group(group_id, test_user2.username, "primary")
    assert_sharing_request_has_primary_user_info(test_user2, group_id)
    accept_sharing_request_for(test_user2, group_id, "")

    # Now test_user2 should be able to share the group with test_user3
    user2_group_api.share_group(group_id, test_user3.username, "secondary")

    assert_sharing_request_has_primary_user_info(test_user3, group_id)
    accept_sharing_request_for(test_user3, group_id, "")

    # Verify that test_user3 can access the group with secondary access_type
    groups = user3_group_api.list_groups()
    shared_group = next((g for g in groups['groups'] if g['group_id'] == group_id), None)
    assert shared_group is not None, "Shared group not found in test_user3's groups"
    assert shared_group['access_type'] == 'secondary', f"Expected 'secondary', got '{shared_group.get('access_type')}'"

    # Verify user2 now has primary access_type after upgrade
    groups = user2_group_api.list_groups()
    user2_group = next((g for g in groups['groups'] if g['group_id'] == group_id), None)
    assert user2_group is not None, "Shared group not found in test_user2's groups"
    assert user2_group['access_type'] == 'primary', f"Expected 'primary' after upgrade, got '{user2_group.get('access_type')}'"
    user1_group_api.delete_group(group_id)

def _test_shadow_access_after_sharing(sharing_details):
    device = sharing_details["device"]
    group_id = sharing_details["group_id"]
    user2_group_api = sharing_details["user2_group_api"]
    test_user2 = sharing_details["test_user2"]

    shadow_name = f"params-{group_id}"
    if sharing_details.get('subgroup_id'):
        # If this test case is for subgroup sharing, then the subgroup_id is present
        shadow_name += f"-{sharing_details['subgroup_id']}"

    if sharing_details['state'] == "begin":
        # Connect the device and set its online status
        assert device.shadow_connect([shadow_name]), "Failed to connect the device"
        online_status = {"online": True}
        device.update_named_shadow(shadow_name, online_status)

        # Create a dummy group for user2 - assume role requires all users to have a group
        user2_group_id = user2_group_api.create_group("Test Group")

        # First verify that user2 cannot access the shadow
        # mqtt connect is going to assume_role everytime
        test_user2.mqtt_connect()
        test_user2.disable_reconnect = True
        try:
            test_user2.subscribe_to_named_shadows(device.node_thing_name, [shadow_name])
        except Exception as e:
            assert "AWS_ERROR_MQTT_CONNECTION_DESTROYED" in str(e), f"Unexpected error message: {str(e)}"

        # Wait for connection interrupted event, incorrect subscription causes connection to be destroyed
        connection_status = test_user2.read_connection_queue()
        assert connection_status == "interrupted", "User2 should get disconnected when trying to subscribe to unauthorized shadow"
    elif sharing_details['state'] == "shared":
        # Try reading the shadow again after sharing
        # mqtt connect is going to assume_role again, thus getting the new policy
        test_user2.mqtt_connect()
        test_user2.subscribe_to_named_shadows(device.node_thing_name, [shadow_name])
        test_user2.read_shadow(device.node_thing_name, shadow_name)

        # Now user2 should be able to get the shadow update
        shadow_data = test_user2.read_shadow_queue()
        assert shadow_data is not None, "User2 should be able to read shadow after sharing"
        assert shadow_data['state']['reported']['online'] is True, "Should receive correct online status"
        test_user2.mqtt_disconnect_and_wait()
    elif sharing_details['state'] == "unshared":
        # Verify that user2 cannot access the shadow
        test_user2.mqtt_connect()
        test_user2.disable_reconnect = True
        try:
            test_user2.subscribe_to_named_shadows(device.node_thing_name, [shadow_name])
        except Exception as e:
            assert "AWS_ERROR_MQTT_CONNECTION_DESTROYED" in str(e), f"Unexpected error message: {str(e)}"

        # Wait for connection interrupted event, incorrect subscription causes connection to be destroyed
        connection_status = test_user2.read_connection_queue()
        assert connection_status == "interrupted", "User2 should get disconnected when trying to subscribe to unauthorized shadow"
        test_user2.mqtt_disconnect_and_wait()
        device.disconnect()

def test_shadow_access_after_sharing_with_group(shared_group, subtests):
    def body(stage, data):
        _test_shadow_access_after_sharing(data)

    run_shared_group_stages(shared_group, subtests, body)

def test_shadow_access_after_sharing_with_subgroup(shared_subgroup, subtests):
    def body(stage, data):
        _test_shadow_access_after_sharing(data)

    run_shared_subgroup_stages(shared_subgroup, subtests, body)

def _test_get_node_config(sharing_details):
    """
    Test the get_node_config method with different sharing mechanisms, group or sub-group
    """
    device = sharing_details["device"]
    group_id = sharing_details["group_id"]
    user1_group_api = sharing_details["user1_group_api"]
    test_user1 = sharing_details["test_user1"]
    test_user2 = sharing_details["test_user2"]
    if sharing_details.get('subgroup_id'):
        subgroup_id = sharing_details['subgroup_id']
    else:
        subgroup_id = "node"

    def user_can_access_node_config(user, group_id, device_thing_name):
        node_config = user.get_node_config(group_id, subgroup_id, device_thing_name)
        assert node_config is not None, "User should be able to get node config"
        assert node_config["device_type"] == "light_bulb"
        assert node_config["firmware_version"] == "1.0"

    def user_cannot_access_node_config(user, group_id, device_thing_name):
        node_config = user.get_node_config(group_id, subgroup_id, device_thing_name)
        assert node_config is None, "User should not be able to get node config"

    if sharing_details['state'] == "begin":
        # Test user1 (owner) should be able to get node config
        user_can_access_node_config(test_user1, group_id, device.node_thing_name)

        # Test user2 (no access) should not be able to get node config
        user_cannot_access_node_config(test_user2, group_id, device.node_thing_name)
    elif sharing_details['state'] == "shared":
        # Now test_user2 should be able to get node config
        user_can_access_node_config(test_user2, group_id, device.node_thing_name)

        # Test user1 (owner) should still be able to get node config
        user_can_access_node_config(test_user1, group_id, device.node_thing_name)
    elif sharing_details['state'] == "unshared":
        # Test user2 should no longer be able to get node config
        user_cannot_access_node_config(test_user2, group_id, device.node_thing_name)

def test_get_node_config_with_group_sharing(shared_group, subtests):
    def body(stage, data):
        _test_get_node_config(data)

    run_shared_group_stages(shared_group, subtests, body)

def test_get_node_config_with_subgroup_sharing(shared_subgroup, subtests):
    def body(stage, data):
        _test_get_node_config(data)

    run_shared_subgroup_stages(shared_subgroup, subtests, body)

def test_multi_subgroup_sharing_in_grp_dynamodb_validation(test_user2, associated_device):
    device, group_id, test_user1, user1_group_api = associated_device

    expected_dynamodb_item = {
        'user_id': test_user2.sub,
        'group_id': group_id,
        'access_type': 'subentity',
        'sub_entity_ids': []
    }
    expected_user2_group = {
        'group_id': group_id,
        'group_name': "Test Associated Group",
        'access_type': 'subgroup',
        'subgroups': [],
        'node_ids' : [device.node_thing_name],
        'node_details': {device.node_thing_name: {'capabilities': ['rmng']}},
    }

    subgroup_ids = []
    # Create 2 subgroups and share them with test_user2
    for i in range(2):
        # Subgroup1
        subgroup_id = user1_group_api.create_subgroup(group_id, f"Test Subgroup {i}")
        user1_group_api.add_node_to_subgroup(group_id, subgroup_id, device.node_thing_name)
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        assert_sharing_request_has_primary_user_info(test_user2, group_id, subgroup_id)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        subgroup_ids.append(subgroup_id)
        expected_dynamodb_item['sub_entity_ids'] = sorted(expected_dynamodb_item['sub_entity_ids'] + [subgroup_id])
        expected_user2_group['subgroups'] = sorted(expected_user2_group['subgroups'] + [{'subgroup_id': subgroup_id, "subgroup_name": f"Test Subgroup {i}", "node_ids": [device.node_thing_name]}], key=lambda x: x['subgroup_id'])

        # Verify the user group mapping entry
        validate_user_group_dynamodb_entry(test_user2.sub, group_id, expected_dynamodb_item)

        # Verify test_user2 can see the subgroup in their groups list
        user2_group_api = Group(test_user2)
        groups = user2_group_api.list_groups()
        for grp in groups['groups']:
            if grp['group_id'] == group_id:
                grp['subgroups'] = sorted(grp['subgroups'], key=lambda x: x['subgroup_id'])
                grp['node_ids'] = sorted(grp['node_ids'])
        assert expected_user2_group in groups['groups']

    # Now test the case where the user has secondary access to the parent group, so the subgroup mappings should go away
    user1_group_api.share_group(group_id, test_user2.username, "secondary")
    assert_sharing_request_has_primary_user_info(test_user2, group_id)
    accept_sharing_request_for(test_user2, group_id, "")

    expected_dynamodb_item = {
        'user_id': test_user2.sub,
        'group_id': group_id,
        'access_type': 'secondary',
        'sub_entity_ids': []
    }
    validate_user_group_dynamodb_entry(test_user2.sub, group_id, expected_dynamodb_item)

    # Cleanup
    for subgroup_id in subgroup_ids:
        user1_group_api.remove_node_from_subgroup(group_id, subgroup_id, device.node_thing_name)
    user1_group_api.unshare_group(group_id, test_user2.user_id)

def test_multi_subgroup_remove_in_grp_dynamodb_validation(test_user2, associated_device):
    device, group_id, test_user1, user1_group_api = associated_device

    expected_dynamodb_item = {
        'user_id': test_user2.sub,
        'group_id': group_id,
        'access_type': 'subentity',
        'sub_entity_ids': []
    }

    subgroup_ids = []
    # Create 2 subgroups and share them with test_user2
    for i in range(2):
        # Subgroup1
        subgroup_id = user1_group_api.create_subgroup(group_id, f"Test Subgroup {i}")
        user1_group_api.add_node_to_subgroup(group_id, subgroup_id, device.node_thing_name)
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        assert_sharing_request_has_primary_user_info(test_user2, group_id, subgroup_id)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        expected_dynamodb_item['sub_entity_ids'] = sorted(expected_dynamodb_item['sub_entity_ids'] + [subgroup_id])
        subgroup_ids.append(subgroup_id)

        validate_user_group_dynamodb_entry(test_user2.sub, group_id, expected_dynamodb_item)

    # Now remove the subgroup sharing
    for subgroup_id in subgroup_ids:
        user1_group_api.unshare_subgroup(group_id, subgroup_id, test_user2.user_id)
        expected_dynamodb_item['sub_entity_ids'] = sorted(list(set(expected_dynamodb_item['sub_entity_ids']) - set([subgroup_id])))
        if len(expected_dynamodb_item['sub_entity_ids']) > 0:
            # Verify the entry is still present with a single subgroup
            validate_user_group_dynamodb_entry(test_user2.sub, group_id, expected_dynamodb_item)
        else:
            with pytest.raises(Exception):
                # The entry should be deleted and hence an exception should be thrown
                validate_user_group_dynamodb_entry(test_user2.sub, group_id, expected_dynamodb_item)

    # Cleanup
    for subgroup_id in subgroup_ids:
        user1_group_api.remove_node_from_subgroup(group_id, subgroup_id, device.node_thing_name)
    user1_group_api.unshare_group(group_id, test_user2.user_id)

# You could skip this test since running it multiple times will cause AWS to block your device for some time
@pytest.mark.unsafe
def test_multi_group_subgroup_shadow_access(test_user1, test_user2, bare_device):
    # User2 requires some group for assume_role to work
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    user2_dummy_group_id = user2_group_api.create_group("Group Dummy")

    # Create two groups A and B
    group_a_id = user1_group_api.create_group("Group A")
    group_b_id = user1_group_api.create_group("Group B")

    # Create two subgroups in group A
    subgroup_aa_id = user1_group_api.create_subgroup(group_a_id, "Subgroup AA")
    subgroup_ab_id = user1_group_api.create_subgroup(group_a_id, "Subgroup AB")

    # Create three nodes with different keys and certificates
    # Registration handled by bare_device (guaranteed teardown on pass/fail/error).
    nodeaa = bare_device(thing_name=f"nodeaa-{uuid.uuid4()}")
    nodeab = bare_device(thing_name=f"nodeab-{uuid.uuid4()}")
    nodeb = bare_device(thing_name=f"nodeb-{uuid.uuid4()}")

    # Connect all nodes
    for node in [nodeaa, nodeab, nodeb]:
        assert node.connect(), f"Failed to connect {node.node_thing_name}"

    # Add nodes to their respective groups and subgroups
    test_user1.do_user_node_assoc(nodeaa, group_a_id)
    test_user1.do_user_node_assoc(nodeab, group_a_id)
    test_user1.do_user_node_assoc(nodeb, group_b_id)

    user1_group_api.add_node_to_subgroup(group_a_id, subgroup_aa_id, nodeaa.node_thing_name)
    user1_group_api.add_node_to_subgroup(group_a_id, subgroup_ab_id, nodeab.node_thing_name)

    # Connect all nodes and set their shadow state
    for node, group_id, subgroup_id in [
        (nodeaa, group_a_id, subgroup_aa_id),
        (nodeab, group_a_id, subgroup_ab_id),
        (nodeb, group_b_id, None)
    ]:
        shadow_name = f"params-{group_id}"
        if subgroup_id:
            shadow_name += f"-{subgroup_id}"

        assert node.shadow_connect([shadow_name]), f"Failed to connect {node.node_thing_name} to shadow"
        node.update_named_shadow(shadow_name, {"status": node.node_thing_name})


    def verify_shadow_access(node, group_id, subgroup_id=None, should_succeed=True):
        # Connect test_user2 to MQTT
        try:
            test_user2.mqtt_connect()
        except Exception as e:
            # Retry connection after waiting for 3 seconds
            # It is observed that sometimes the MQTT Connection itself is rejected by	AWS, probably
            # because of frequent connects? Hoping this gets us past that problem, let's see
            print(f"Failed to connect to MQTT: {e}, retrying in 3 seconds...")
            time.sleep(3)
            test_user2.mqtt_connect()
        test_user2.disable_reconnect = True

        shadow_name = f"params-{group_id}"
        if subgroup_id:
            shadow_name += f"-{subgroup_id}"

        if not should_succeed:
            with pytest.raises(Exception):
                test_user2.subscribe_to_named_shadows(node.node_thing_name, [shadow_name])

            connection_status = test_user2.read_connection_queue()
            assert connection_status == "interrupted", f"User2 should get disconnected when trying to subscribe to unauthorized shadow"
        else:
            test_user2.subscribe_to_named_shadows(node.node_thing_name, [shadow_name])
            test_user2.read_shadow(node.node_thing_name, shadow_name)
            shadow_data = test_user2.read_shadow_queue()

            assert shadow_data is not None, f"Should be able to read shadow for {node.node_thing_name}"
            assert shadow_data['state']['reported']['status'] == node.node_thing_name

        test_user2.mqtt_disconnect_and_wait()

    # Initially verify no access to any shadows
    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, False)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, False)
    verify_shadow_access(nodeb, group_b_id, None, False)

    # Share subgroup AA and verify access
    user1_group_api.share_subgroup(group_a_id, subgroup_aa_id, test_user2.username)
    assert_sharing_request_has_primary_user_info(test_user2, group_a_id, subgroup_aa_id)
    accept_sharing_request_for(test_user2, group_a_id, subgroup_aa_id)

    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, True)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, False)
    verify_shadow_access(nodeb, group_b_id, None, False)

    # Share subgroup AB and verify access
    user1_group_api.share_subgroup(group_a_id, subgroup_ab_id, test_user2.username)
    assert_sharing_request_has_primary_user_info(test_user2, group_a_id, subgroup_ab_id)
    accept_sharing_request_for(test_user2, group_a_id, subgroup_ab_id)

    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, True)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, True)
    verify_shadow_access(nodeb, group_b_id, None, False)

    # Share group B and verify access
    user1_group_api.share_group(group_b_id, test_user2.username, "secondary")
    assert_sharing_request_has_primary_user_info(test_user2, group_b_id)
    accept_sharing_request_for(test_user2, group_b_id, "")

    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, True)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, True)
    verify_shadow_access(nodeb, group_b_id, None, True)

    # Unshare subgroup AB and verify access
    user1_group_api.unshare_subgroup(group_a_id, subgroup_ab_id, test_user2.user_id)
    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, True)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, False)
    verify_shadow_access(nodeb, group_b_id, None, True)

    # Unshare subgroup AA and verify access
    user1_group_api.unshare_subgroup(group_a_id, subgroup_aa_id, test_user2.user_id)
    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, False)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, False)
    verify_shadow_access(nodeb, group_b_id, None, True)

    # Unshare group B and verify access
    user1_group_api.unshare_group(group_b_id, test_user2.user_id)
    verify_shadow_access(nodeaa, group_a_id, subgroup_aa_id, False)
    verify_shadow_access(nodeab, group_a_id, subgroup_ab_id, False)
    verify_shadow_access(nodeb, group_b_id, None, False)

    # Cleanup
    test_user2.mqtt_disconnect_and_wait()
    for node in [nodeaa, nodeab, nodeb]:
        node.disconnect()
        node.destroy_test_node()
    user1_group_api.delete_group(group_a_id)
    user1_group_api.delete_group(group_b_id)
    user2_group_api.delete_group(user2_dummy_group_id)


def test_secondary_user_leaves_group(test_user1, test_user2):
    """A secondary user can leave a group they were shared into via DELETE /users/me."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        # Sanity: user2 sees the group before leaving.
        groups = user2_group_api.list_groups()
        assert any(g['group_id'] == group_id for g in groups['groups']), "Shared group not visible to user2"

        # user2 leaves via "me" alias.
        user2_group_api.leave_group(group_id)

        # user2 no longer sees the group.
        groups = user2_group_api.list_groups()
        assert not any(g['group_id'] == group_id for g in groups['groups']), "Group still visible after leaving"

        # user1 still sees it and is still the sole primary.
        data = user1_group_api.list_group_users(group_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_subgroup_user_leaves_subgroup(test_user1, test_user2):
    """A subentity user can leave a subgroup they were shared into via DELETE /subgroups/{id}/users/me."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        subgroup_id = user1_group_api.create_subgroup(group_id, "Test Subgroup")
        user1_group_api.share_subgroup(group_id, subgroup_id, test_user2.username)
        accept_sharing_request_for(test_user2, group_id, subgroup_id)

        # user2 sees the subgroup before leaving.
        groups = user2_group_api.list_groups()
        grp = next((g for g in groups['groups'] if g['group_id'] == group_id), None)
        assert grp is not None and any(sg['subgroup_id'] == subgroup_id for sg in grp.get('subgroups', [])), \
            "Subgroup not visible to user2 before leave"

        # user2 leaves the subgroup.
        user2_group_api.leave_subgroup(group_id, subgroup_id)

        # user2 no longer sees any mapping for this group.
        groups = user2_group_api.list_groups()
        assert not any(g['group_id'] == group_id for g in groups['groups']), \
            "Group still visible after leaving only subgroup access"
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_primary_user_leaves_after_upgrade(test_user1, test_user2):
    """A user upgraded to primary can leave via 'me' while original primary retains access."""
    user1_group_api = Group(test_user1)
    user2_group_api = Group(test_user2)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        # Upgrade user2 to primary.
        user1_group_api.share_group(group_id, test_user2.username, "primary")
        accept_sharing_request_for(test_user2, group_id, "")

        # user2 (primary) leaves themselves; another primary (user1) remains.
        user2_group_api.leave_group(group_id)

        # user1 is still a primary and sole remaining user.
        data = user1_group_api.list_group_users(group_id)
        assert data["users"] == [
            {"user_id": test_user1.sub, "email": test_user1.username, "access_type": "primary"},
        ]
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_last_primary_cannot_leave_group(test_user1):
    """The last remaining primary cannot leave; API must return 409."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        # user1 is the sole primary; leaving must be rejected.
        resp = test_user1.make_api_request('DELETE', f'/v1/groups/{group_id}/users/me')
        assert resp.status_code == 409, \
            f"Expected 409 Conflict for last-primary leave, got {resp.status_code}: {resp.text}"

        # user1 should still have the group.
        groups = user1_group_api.list_groups()
        assert any(g['group_id'] == group_id for g in groups['groups']), \
            "Group missing after rejected last-primary leave"
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


def test_last_primary_cannot_leave_when_secondary_present(test_user1, test_user2):
    """Even with secondary users in the group, the last primary cannot leave."""
    user1_group_api = Group(test_user1)
    group_id = user1_group_api.create_group(f"Test Group {uuid.uuid4()}")

    try:
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        resp = test_user1.make_api_request('DELETE', f'/v1/groups/{group_id}/users/me')
        assert resp.status_code == 409, \
            f"Expected 409 Conflict when last primary tries to leave, got {resp.status_code}: {resp.text}"
    finally:
        user1_group_api.delete_group(group_id, warn_error=True)


# Cross-tenant tests; see test_automations.py for the origin of this class.

def test_cannot_accept_foreign_sharing_request(two_tenants, test_user3):
    """A must not be able to accept a sharing request addressed to someone else.

    B shares its group with C. A (who is neither) enumerates a request id and
    tries to accept it. The accept must be scoped to the recipient identity.
    """
    tenant_a, tenant_b = two_tenants
    user_a, user_c = tenant_a["user"], test_user3

    tenant_b["group_api"].share_group(tenant_b["group_id"], user_c.username, "secondary")

    reqs = user_c.get_sharing_requests()
    assert reqs, "expected a pending sharing request for C"
    target = next((r for r in reqs if r["group_id"] == tenant_b["group_id"]), None)
    assert target is not None
    req_id = target["sharing_request_id"]

    resp = user_a.make_api_request("POST", f"/v1/sharing-requests/{req_id}/accept",
                                   data=json.dumps({}))
    # Deployed handler answers 500 rather than a clean 403 — denied, but sloppy
    # status, so this asserts >=400 and checks the real property below.
    assert resp.status_code >= 400, (
        f"Accepting a foreign user's sharing request returned {resp.status_code}, "
        f"expected an error. Body: {resp.text}"
    )
    a_groups = Group(user_a).list_groups()
    assert not any(g["group_id"] == tenant_b["group_id"] for g in a_groups["groups"]), \
        "Attacker gained access to a group via another user's sharing request"

    try:
        accept_sharing_request_for(user_c, tenant_b["group_id"], "")
    except Exception:
        pass

def test_create_sharing_request_on_foreign_group_denied(two_tenants, test_user3):
    """A must not create a sharing request for B's group (grant others access)."""
    tenant_a, tenant_b = two_tenants
    user_a, user_c = tenant_a["user"], test_user3

    resp = user_a.make_api_request(
        "POST", f"/v1/groups/{tenant_b['group_id']}/sharing-requests",
        data=json.dumps({"username": user_c.username, "access_type": "primary"}))
    assert resp.status_code >= 400, (
        f"Created a sharing request for a foreign group returned {resp.status_code}: {resp.text[:150]}"
    )
    reqs = user_c.get_sharing_requests() or []
    assert not any(r["group_id"] == tenant_b["group_id"] for r in reqs), \
        "A non-member created a valid sharing request for a foreign group"
