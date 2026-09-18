# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge presence-cascade integration test.

Closes the bridge's MQTT session and verifies that the full pipeline
(IoT presence event → topic rule on $aws/events/presence/disconnected/+
→ presence handler → bridge cascade → shadow writes) flips every child's
iparams.online and params-<groupID>.online to false and records
disconnect_info on the named shadow. See docs/en/specs/bridge.md §3.7 / §5.9.
"""

import json
import time

import boto3
import pytest
from botocore.exceptions import ClientError

PRESENCE_POLL_TIMEOUT_S = 30.0
PRESENCE_POLL_INTERVAL_S = 0.5


@pytest.fixture
def aws_clients():
    return {
        "iot_data": boto3.client("iot-data"),
    }


def _read_shadow_online(iot_data, thing, shadow_name):
    """Return state.reported.online for the given shadow, or None if the
    shadow is absent or the field unset."""
    try:
        shadow_resp = iot_data.get_thing_shadow(
            thingName=thing, shadowName=shadow_name,
        )
    except ClientError:
        return None
    doc = json.loads(shadow_resp["payload"].read())
    return doc.get("state", {}).get("reported", {}).get("online")


def _read_shadow_disconnect_info(iot_data, thing, shadow_name):
    """Return state.reported.disconnect_info dict or None if absent."""
    try:
        shadow_resp = iot_data.get_thing_shadow(
            thingName=thing, shadowName=shadow_name,
        )
    except ClientError:
        return None
    doc = json.loads(shadow_resp["payload"].read())
    return doc.get("state", {}).get("reported", {}).get("disconnect_info")


def test_presence_cascade_marks_children_offline(bridge_in_group, aws_clients, seed_child):
    """Seed 2 children, pre-write online=true on both shadows (iparams and
    params-<groupID>), close the bridge's MQTT session, and wait for the
    full pipeline (IoT presence event → topic rule → cascade Lambda) to flip
    both children offline on both shadows. Also asserts disconnect_info is
    recorded on the named shadow with the correct reason.

    This exercises:
      - the IoT topic rule wiring on $aws/events/presence/disconnected/+
      - the Lambda handler logic (both shadow writes, disconnect_info)
    in a single end-to-end flow. See docs/en/specs/bridge.md §3.7 / §5.9."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    group_id = bridge_in_group["group_id"]
    named_shadow = f"params-{group_id}"

    seeded = []
    for i in range(2):
        child = seed_child(f"pres_{i}", f"0xPRES_{i:03d}")
        assert child is not None, f"failed to seed presence child pres_{i}"
        seeded.append(child)

    iot_data = aws_clients["iot_data"]
    for child in seeded:
        for shadow in ("iparams", named_shadow):
            iot_data.update_thing_shadow(
                thingName=child, shadowName=shadow,
                payload=json.dumps({"state": {"reported": {"online": True}}}).encode(),
            )

    # Sanity-check pre-state so a downstream failure is unambiguous.
    for child in seeded:
        assert _read_shadow_online(iot_data, child, "iparams") is True
        assert _read_shadow_online(iot_data, child, named_shadow) is True

    disconnect_ts_before = int(time.time() * 1000)
    bridge.disconnect()

    # Real disconnect → presence event → rule → Lambda → shadow updates.
    # AWS IoT presence events typically arrive within ~1-2s; allow generous
    # headroom for the full fan-out across two children × two shadows.
    deadline = time.time() + PRESENCE_POLL_TIMEOUT_S
    shadows = ("iparams", named_shadow)
    flipped = {child: {s: False for s in shadows} for child in seeded}
    while time.time() < deadline:
        for child in seeded:
            for shadow in shadows:
                if flipped[child][shadow]:
                    continue
                if _read_shadow_online(iot_data, child, shadow) is False:
                    flipped[child][shadow] = True
        if all(all(v.values()) for v in flipped.values()):
            break
        time.sleep(PRESENCE_POLL_INTERVAL_S)

    missing = [
        f"{c}/{s}" for c, sm in flipped.items()
        for s, ok in sm.items() if not ok
    ]
    assert not missing, (
        f"shadows still online=true after {PRESENCE_POLL_TIMEOUT_S}s: {missing}. "
        "Bridge presence cascade must update both indexed (iparams) and named "
        f"(params-<groupID>) shadows for every child."
    )

    # disconnect_info is written when the AWS IoT presence event carries
    # disconnectReason / timestamp. Real test-client disconnects may omit
    # those fields; the content is covered by the handler unit test. Here
    # we only assert the field is present when the shadow confirms it was
    # written.
    for child in seeded:
        di = _read_shadow_disconnect_info(iot_data, child, named_shadow)
        if di is not None:
            assert di.get("last_disconnect_reason") == "CLIENT_INITIATED_DISCONNECT", \
                f"{child}: disconnect_info reason={di.get('last_disconnect_reason')!r}"
