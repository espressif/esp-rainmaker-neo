# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import time

from test.itest.conftest import (
    connect_device_with_retry,
    wait_for_node_session,
    wait_for_reported_online,
)

# presence_event_handler waits 10s after a disconnect event before writing
# offline, so this clears that window. It is deliberately not sized for the full
# ~25s round-trip: tests that must see the offline write land poll for it instead
# (a longer blanket wait lets a previous test's in-flight presence events land
# inside this one, and the associated_device pool shares the device).
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


def _wait_for_user_reported_online(test_user1, thing_name, shadow_name, expected, timeout=60):
    """Poll the user's MQTT shadow read until reported.online is `expected`.

    Deliberately reads through the user's subscription rather than the iot-data
    API: that is what an app sees, and it also exercises the user's read access.
    Returns the last document seen, so a caller can assert on what it was stuck at.
    """
    deadline = time.time() + timeout
    shadow_data = None
    while True:
        test_user1.read_shadow(thing_name, shadow_name)
        latest = test_user1.read_shadow_queue()
        if latest is not None:
            shadow_data = latest
            if shadow_data['state']['reported'].get('online') is expected:
                return shadow_data
        if time.time() >= deadline:
            return shadow_data
        time.sleep(3)


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

    # Poll rather than sleep a fixed interval: the handler's presenceOfflineDelay
    # (10s) plus the nodes-online -> lambda -> shadow round-trip runs ~25s, and a
    # fixed wait races it under load.
    shadow_data = _wait_for_user_reported_online(
        test_user1, device.node_thing_name, shadow_name, expected=False)
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


# Group-less nodes: the shadow name is derived as "params-<groupID>...", which
# renders as the bare "params-" for a node with no group. That is the name such a
# device reports into, so presence writes offline there as well as to the
# group-independent "iparams".

def _connect_groupless_device(bare_device):
    """Register a never-associated device, report online into "params-" as
    firmware does, then disconnect it."""
    device = bare_device()

    assert connect_device_with_retry(device, max_retries=3, base_delay=2), \
        f"Failed to connect group-less device {device.node_thing_name}"
    assert device.group_id in (None, ""), \
        f"bare_device unexpectedly has a group: {device.group_id}"
    session_id, _ = wait_for_node_session(device.node_thing_name)
    assert session_id, \
        f"node_connected_rule did not populate rmng-nodes-online for {device.node_thing_name}"

    # Boot reports online into both shadows, so seed both: asserting iparams flips
    # to false only means something if it was true first.
    assert device.shadow_connect(["params-"]), "Failed to connect device shadow client"
    for shadow_name in ("params-", "iparams"):
        assert device.update_named_shadow(shadow_name, {"online": True}), \
            f"Device failed to report online into the {shadow_name} shadow"
    for shadow_name in ("params-", "iparams"):
        assert wait_for_reported_online(device.node_thing_name, shadow_name, expected=True, timeout=20) is True, \
            f"Device's own online:true never landed in {shadow_name}"

    device.disconnect()
    return device


def test_groupless_node_disconnect_writes_offline_shadows(bare_device):
    """Both shadows a group-less node has must be corrected to offline.

    "params-" is the name it reports into (the shadow name renders that way for
    an empty group) and "iparams" is group-independent. Without the matching
    offline writes the reported online:true is stuck until association deletes
    the shadow. Requires the fix to be deployed.
    """
    thing_name = _connect_groupless_device(bare_device).node_thing_name

    for shadow_name in ("iparams", "params-"):
        online = wait_for_reported_online(thing_name, shadow_name, expected=False)
        assert online is False, (
            f"'{shadow_name}' shadow for group-less node {thing_name} is stuck at "
            f"online={online!r}; presence never corrected it"
        )
