# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import pytest
from test.itest.conftest import run_device_with_2_subgroups_stages

def test_device_get_group_info(associated_device):
    device, expected_group_id, test_user1, user1_group_api = associated_device

    # The topic notification is already validated in the associated_device fixture
    # Validate by explicitly triggering the get_group_info method
    device.group_id = None
    device.subgroup_ids = None
    device.get_group_info()
    group_id = device.group_id

    # Validate that it returns the correct group id
    assert group_id == expected_group_id, f"Expected group ID {expected_group_id}, but got {group_id}"
    assert device.subgroup_ids is None, "Expected subgroup_ids to be None"

def test_group_info_received_for_update_subgroup(device_with_2_subgroups, subtests):
    def body(stage, data):
        if stage != "subgroup_add_begin":
            device = data['device']
            group_id = data['group_id']
            assert device.wait_for_group_info(), "Device failed to receive group info"
            assert device.group_id == group_id, f"Expected group ID {group_id}, but got {device.group_id}"
            for subgroup_id in data.get('subgroups', []):
                assert subgroup_id in device.subgroup_ids, f"Expected subgroup ID {subgroup_id} in device.subgroup_ids, but got {device.subgroup_ids}"

    run_device_with_2_subgroups_stages(device_with_2_subgroups, subtests, body)

def test_explicit_get_group_info_received_for_update_subgroup(device_with_2_subgroups, subtests):
    def body(stage, data):
        if stage != "subgroup_add_begin":
            device = data['device']
            group_id = data['group_id']
            device.group_id = None
            device.subgroup_ids = None
            device.get_group_info()
            assert device.group_id == group_id, f"Expected group ID {group_id}, but got {device.group_id}"
            for subgroup_id in data.get('subgroups', []):
                assert subgroup_id in device.subgroup_ids, f"Expected subgroup ID {subgroup_id} in device.subgroup_ids, but got {device.subgroup_ids}"

    run_device_with_2_subgroups_stages(device_with_2_subgroups, subtests, body)

def test_shadow_migration_for_update_subgroup(device_with_2_subgroups, subtests):
    def body(stage, data):
        device = data['device']
        group_id = data['group_id']
        shadow_name = f"params-{group_id}"
        if data.get('subgroups'):
            for subgroup_id in sorted(data['subgroups']):
                shadow_name += f"-{subgroup_id}"

        if stage == "subgroup_add_begin":
            device.shadow_connect([shadow_name])
            device.update_named_shadow(shadow_name, {"door_open": True})
        else:
            shadow_data = device.get_shadow(shadow_name)
            assert shadow_data is not None, "No shadow update received"
            assert shadow_data['state']['reported']['door_open'] is True, "Should receive correct door_open status"

        if stage == "subgroup_remove_2":
            device.disconnect()

    run_device_with_2_subgroups_stages(device_with_2_subgroups, subtests, body)

def test_old_shadow_deletion_for_update_subgroup(device_with_2_subgroups, subtests):
    def body(stage, data):
        device = data['device']
        if stage == "subgroup_add_1":
            device.shadow_connect([f"params-{data['group_id']}"])
            shadow_data = device.get_shadow(f"params-{data['group_id']}")
            assert shadow_data is None, "Old shadow should be deleted"
        elif stage == "subgroup_add_2":
            device.shadow_connect([f"params-{data['group_id']}-{data['subgroups'][0]}"])
            shadow_data = device.get_shadow(f"params-{data['group_id']}-{data['subgroups'][0]}")
            assert shadow_data is None, "Old shadow should be deleted"

            device.shadow_connect([f"params-{data['group_id']}-{data['subgroups'][1]}"])
            shadow_data = device.get_shadow(f"params-{data['group_id']}-{data['subgroups'][1]}")
            assert shadow_data is None, "Old shadow should be deleted"
        elif stage == "subgroup_remove_1":
            shadow_name = f"params-{data['group_id']}"
            if data.get('subgroups'):
                for subgroup_id in sorted(data['subgroups'] + [data['removed_subgroup']]):
                    shadow_name += f"-{subgroup_id}"

            device.shadow_connect([shadow_name])
            shadow_data = device.get_shadow(shadow_name)
            assert shadow_data is None, "Old shadow should be deleted"
        elif stage == "subgroup_remove_2":
            device.shadow_connect([f"params-{data['group_id']}-{data['removed_subgroup']}"])
            shadow_data = device.get_shadow(f"params-{data['group_id']}-{data['removed_subgroup']}")
            assert shadow_data is None, "Old shadow should be deleted"

            device.disconnect()

    run_device_with_2_subgroups_stages(device_with_2_subgroups, subtests, body)
