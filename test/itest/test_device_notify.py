# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import time

from py_sdk.test_group import Group
from py_sdk.test_util import seed_node_data, assert_node_data_deleted
from test.itest.conftest import connect_device_with_retry


def _node_in_group(group_api, group_id, node_id):
    """True if node_id is currently a member of group_id (per the list-groups API)."""
    group = group_api._get_group(group_id)
    return node_id in group_api._user_node_ids(group)


def test_node_reset_disassociates_node(test_user1, valid_device):
    """A node that reports a self factory-reset (notify: {node_reset: true})
    must be disassociated from its group by the backend.

    Exercises the device → node_notify_rule → notifications Lambda path
    (NodeResetService), distinct from the user-API removal in
    test_remove_node_from_group which lands on the same backend cleanup.
    """
    group_api = Group(test_user1)
    group_id = group_api.create_group("Node Reset Test Group")
    try:
        node_id = valid_device.node_thing_name

        # Associate the device, then reconnect so the IoT policy re-resolves the
        # newly-set group_id thing attribute (policy variables resolve at
        # connection time). wait_for_group_info populates device.group_id, which
        # send_direct_notification uses to build the notify topic.
        assert connect_device_with_retry(valid_device, max_retries=3, base_delay=2), \
            "Failed to connect device"
        assert test_user1.do_user_node_assoc(valid_device, group_id) is None, \
            "Association failed"
        assert valid_device.wait_for_group_info(), "Device did not receive group info"
        valid_device.disconnect()
        valid_device.clear_queues()
        assert valid_device.connect(), "Failed to reconnect device after association"

        assert _node_in_group(group_api, group_id, node_id), \
            f"Node {node_id} should be in group {group_id} before reset"

        # Seed schedule, trigger, and automations so we can assert the async
        # node_data_reset cleanup actually fires (not just the disassociation).
        trigger_auto_id, action_auto_id = seed_node_data(test_user1, group_id, node_id)

        assert test_user1.put_node_tags(group_id, node_id, {"env": "test"}) is not None, \
            "Failed to seed user tag before reset"

        # Firmware reports it factory-reset itself.
        assert valid_device.send_direct_notification({"node_reset": True}), \
            "Failed to publish node_reset notification"

        # Backend disassociates the node. Poll, since the IoT-rule → Lambda
        # path is asynchronous.
        removed = False
        for _ in range(10):
            time.sleep(2)
            if not _node_in_group(group_api, group_id, node_id):
                removed = True
                break
        assert removed, f"Node {node_id} was not disassociated from group {group_id} after node_reset"

        # Disassociation also fans out node_data_reset (triggers, schedules,
        # automations). Assert that ran — same backend cleanup exercised by
        # test_remove_node_from_group.
        assert_node_data_deleted(test_user1, group_id, node_id, trigger_auto_id, action_auto_id)

        iparams = valid_device.get_shadow("iparams")
        user_tags = (
            (iparams or {}).get("state", {}).get("reported", {})
            .get("data", {}).get("user", {}).get("t")
        )
        assert not user_tags, \
            f"iparams user tags should be cleared after node_reset, got {user_tags!r}"

    finally:
        try:
            valid_device.disconnect()
        except Exception as e:
            print(f"Error disconnecting device: {e}")
        # delete_group empties any remaining members first, so this is safe
        # whether or not the disassoc succeeded.
        group_api.delete_group(group_id, warn_error=True)