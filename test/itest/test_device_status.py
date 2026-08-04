# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import time

# presence_event_handler waits this long after a disconnect event before writing
# offline. Tests that need the offline write to land must wait longer than this;
# tests that want to defeat the offline write must reconnect within this window.
PRESENCE_OFFLINE_DELAY = 10
OFFLINE_PROPAGATION_WAIT = 15


def _resolve_shadow_name(device, group_id):
    shadow_name = f"params-{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        for subgroup_id in sorted(device.subgroup_ids):
            shadow_name += f"-{subgroup_id}"
    return shadow_name


def _publish_online_true(device, shadow_name, ishadow_name):
    assert device.shadow_connect([shadow_name]), "Failed to connect device shadow client"
    online_status = {"online": True}
    device.update_named_shadow(shadow_name, online_status)
    device.update_named_shadow(ishadow_name, online_status)


def test_device_online_offline_status(associated_device):
    device, group_id, test_user1, user1_group_api = associated_device

    # Connect the device to the cloud
    device.connect()
    device.get_group_info()

    shadow_name = _resolve_shadow_name(device, group_id)
    ishadow_name = "iparams"

    _publish_online_true(device, shadow_name, ishadow_name)

    test_user1.mqtt_connect()
    test_user1.subscribe_to_named_shadows(device.node_thing_name, [shadow_name])
    # Read the shadow to verify the device is online
    test_user1.read_shadow(device.node_thing_name, shadow_name)
    # As user will not be able to read the indexed shadow, we need to get it from the device
    ishadow_data = device.get_shadow(ishadow_name)

    # Get the shadow update from the queue
    shadow_data = test_user1.read_shadow_queue()
    assert shadow_data is not None, "No shadow update received"
    assert shadow_data['state']['reported']['online'] is True, "Device should be online"
    assert ishadow_data is not None, "No indexed shadow update received"
    assert ishadow_data['state']['reported']['online'] is True, "Device should be online"

    # Disconnect the device from the cloud
    device.disconnect()

    # Wait for the offline status to propagate through the presence event handler.
    # The handler enforces a presenceOfflineDelay (10s) + post-delay re-check before writing,
    # so we must wait longer than that before asserting offline.
    time.sleep(OFFLINE_PROPAGATION_WAIT)

    # Read the shadow again to verify the device is offline
    test_user1.read_shadow(device.node_thing_name, shadow_name)

    # Get the shadow update from the queue
    shadow_data = test_user1.read_shadow_queue()
    assert shadow_data is not None, "No shadow update received"
    assert shadow_data['state']['reported']['online'] is False, "Device should be offline"
    # As user will not be able to read the indexed shadow, we need to get it from the device
    ishadow_data = device.get_shadow(ishadow_name)
    assert ishadow_data is not None, "No indexed shadow update received"
    assert ishadow_data['state']['reported']['online'] is False, "Device should be offline"

    # Reconnect after the lambda has written offline; device republishes online=true.
    # Verifies the offline → online transition pair on the long-outage path.
    assert device.connect(), "Device failed to reconnect after long outage"
    _publish_online_true(device, shadow_name, ishadow_name)

    # Shadow update is direct from the device — no lambda wait on the connect path.
    time.sleep(2)

    ishadow_data = device.get_shadow(ishadow_name)
    assert ishadow_data is not None, "No indexed shadow update received after reconnect"
    assert ishadow_data['state']['reported']['online'] is True, \
        "Device should be online again after reconnect"


def test_device_fast_reconnect_preserves_online(associated_device):
    """Wi-Fi flicker: disconnect and reconnect within presenceOfflineDelay.

    The lambda's session-mismatch check must drop the stale disconnect so the
    shadow stays online. This is the regression path guarded by the
    `presenceOfflineDelay` + `(SessionID, VersionNumber)` match in
    presence_event_handler.
    """
    device, group_id, test_user1, user1_group_api = associated_device

    device.connect()
    device.get_group_info()

    shadow_name = _resolve_shadow_name(device, group_id)
    ishadow_name = "iparams"

    _publish_online_true(device, shadow_name, ishadow_name)

    # Sanity: device starts online.
    ishadow_data = device.get_shadow(ishadow_name)
    assert ishadow_data is not None and ishadow_data['state']['reported']['online'] is True, \
        "Device should be online before flicker"

    # Flicker: disconnect, then reconnect inside the lambda's wait window so the
    # `connected` IoT-rule's PutItem updates nodes_online before the lambda reads it.
    device.disconnect()
    time.sleep(2)  # well under PRESENCE_OFFLINE_DELAY (10s)
    assert device.connect(), "Device failed to reconnect after flicker"
    _publish_online_true(device, shadow_name, ishadow_name)

    # Wait past the lambda's wait window so any stale-disconnect processing finishes.
    time.sleep(OFFLINE_PROPAGATION_WAIT)

    ishadow_data = device.get_shadow(ishadow_name)
    assert ishadow_data is not None, "No indexed shadow update received"
    assert ishadow_data['state']['reported']['online'] is True, \
        "Shadow should remain online — stale disconnect must be dropped on session mismatch"
