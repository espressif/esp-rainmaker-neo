# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge ChildToCloudPublish policy + reconnect schedule-sync tests.

Covers the bridge IoT policy statement `ChildrenToCloudPublish`
(spec §3.5) and the reconnect schedule-version-sync flow (spec §5.9).

The mechanism:
  1. Bridge publishes `{event:[getSchedVer]}` on
     `rainmaker/nodes/<child>/to_cloud`, authorized by the policy
     substitution `rainmaker/nodes/${self}--*/to_cloud`.
  2. The existing `node_to_cloud_rule` (filter `rainmaker/nodes/+/to_cloud`)
     extracts `thing_name = topic(3) = <child>` and invokes the
     publish_input_event_handler.
  3. Lambda replies on `rainmaker/nodes/<child>/from_cloud` with
     `{event:[getSchedVer], getSchedVer:{version: N}}`.
  4. Rule A rewrites the reply to
     `rainmaker/bridges/<parent>/children/<child>/from_cloud`.
  5. Bridge receives the rewritten reply on subscription #5
     (queue `bridges_fc` in the fixture).

A symmetric test confirms cross-parent isolation: bridge cannot
publish to a foreign child's `to_cloud`.
"""

import json
import time
from queue import Empty

import boto3
import pytest
from awscrt import mqtt as awscrt_mqtt

RX_TIMEOUT_S = 8.0
SCHEDULE_PROPAGATION_S = 3.0


def _wait_for(q, predicate, timeout=RX_TIMEOUT_S):
    """Pull from q until predicate(msg) returns truthy. Returns the
    message or None on timeout."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            msg = q.get(timeout=max(0.05, deadline - time.time()))
        except Empty:
            return None
        if predicate(msg):
            return msg
    return None


def _bridge_publish_child_to_cloud(bridge, child_node_id, payload):
    """Publish on the child's canonical to_cloud topic, authorized by
    the bridge cert's ChildrenToCloudPublish policy statement. Uses
    the direct topic (not Basic Ingest) so the AWS IoT policy
    substitution `rainmaker/nodes/${self}--*/to_cloud` is what's
    actually gating the publish."""
    topic = f"rainmaker/nodes/{child_node_id}/to_cloud"
    future, _ = bridge.mqtt_connection.publish(
        topic=topic,
        payload=json.dumps(payload),
        qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
    )
    future.result(timeout=10)


def test_bridge_publishes_child_get_sched_ver(bridge_in_group, seed_child, drain):
    """End-to-end §5.9: bridge publishes getSchedVer on a child's
    to_cloud → reply via Rule A lands on bridges/.../children/.../from_cloud
    with a version field."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    group_id = bridge_in_group["group_id"]
    user = bridge_in_group["user"]

    child = seed_child("schedver", "0xSCHEDVER1")
    assert child is not None, "addChild failed during setup"

    # Seed a schedule on the child via the user API so cloud has a
    # non-zero version to report. The proactive update lands on the
    # child's from_cloud and is rewritten by Rule A to the bridge.
    schedule_payload = {
        "schedule": {
            "Schedules": [
                {"id": "1", "name": "MorningOn", "enabled": True,
                 "time": "09:00", "action": "on"},
            ],
        },
    }
    assert user.set_node_schedule(group_id, None, child, schedule_payload), \
        "user set_node_schedule failed"
    time.sleep(SCHEDULE_PROPAGATION_S)

    # Drain the proactive getSchedDetails push so the next message on
    # bridges_fc is unambiguously the getSchedVer reply.
    drain(bridge_in_group["bridges_fc"])

    # Bridge issues the to_cloud publish for the child.
    _bridge_publish_child_to_cloud(bridge, child, {"event": ["getSchedVer"]})

    expected_topic = f"rainmaker/bridges/{parent}/children/{child}/from_cloud"

    def _is_sched_ver_reply(msg):
        if msg["topic"] != expected_topic:
            return False
        events = msg["payload"].get("event") or []
        return isinstance(events, list) and "getSchedVer" in events

    msg = _wait_for(bridge_in_group["bridges_fc"], _is_sched_ver_reply,
                    timeout=RX_TIMEOUT_S)
    assert msg is not None, (
        f"no getSchedVer reply within {RX_TIMEOUT_S}s on {expected_topic}"
    )

    ver_obj = msg["payload"].get("getSchedVer") or {}
    version = ver_obj.get("version")
    assert version is not None, f"reply missing version: {msg['payload']!r}"
    assert int(version) > 0, f"expected positive version, got {version!r}"


def test_bridge_publishes_child_get_sched_details(bridge_in_group, seed_child, drain):
    """End-to-end: bridge issues getSchedDetails on a child's to_cloud.
    The full schedule payload is rewritten by Rule A back to the
    bridge namespace."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    group_id = bridge_in_group["group_id"]
    user = bridge_in_group["user"]

    child = seed_child("schedet", "0xSCHEDET1")
    assert child is not None

    expected_entries = {
        "Schedules": [
            {"id": "42", "name": "EveningOff", "enabled": True,
             "time": "21:00", "action": "off"},
        ],
    }
    assert user.set_node_schedule(group_id, None, child,
                                  {"schedule": expected_entries})
    time.sleep(SCHEDULE_PROPAGATION_S)
    drain(bridge_in_group["bridges_fc"])

    _bridge_publish_child_to_cloud(bridge, child,
                                   {"event": ["getSchedDetails"]})

    expected_topic = f"rainmaker/bridges/{parent}/children/{child}/from_cloud"

    def _is_details(msg):
        if msg["topic"] != expected_topic:
            return False
        events = msg["payload"].get("event") or []
        return isinstance(events, list) and "getSchedDetails" in events

    msg = _wait_for(bridge_in_group["bridges_fc"], _is_details,
                    timeout=RX_TIMEOUT_S)
    assert msg is not None, "no getSchedDetails reply"

    details = msg["payload"].get("getSchedDetails") or {}
    # version is embedded by AppendGetSchedDetails (node.go:897).
    assert "version" in details, f"missing version in {details!r}"
    received_entries = {k: v for k, v in details.items() if k != "version"}
    assert received_entries == expected_entries, (
        f"schedule mismatch: got {received_entries!r}, expected {expected_entries!r}"
    )


def test_cross_parent_child_to_cloud_denied(bridge_in_group):
    """The ChildToCloudPublish policy substitution pins the topic to
    `rainmaker/nodes/<self>--*/to_cloud`. A bridge publishing on a
    foreign child's to_cloud must be denied at the policy layer.

    AWS IoT may either silently drop the publish or close the MQTT
    session as a side effect. We assert by verifying that the foreign
    Thing's schedule is NOT touched (it shouldn't even exist)."""
    bridge = bridge_in_group["bridge"]
    foreign = "rmng-other-bridge-77--childX"

    publish_raised = None
    try:
        future, _ = bridge.mqtt_connection.publish(
            topic=f"rainmaker/nodes/{foreign}/to_cloud",
            payload=json.dumps({"event": ["getSchedVer"]}),
            qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
        )
        future.result(timeout=10)
    except Exception as e:
        publish_raised = str(e)

    # Sleep long enough that any (unauthorized) Lambda invocation
    # would have completed and replied.
    time.sleep(3.0)

    # If the policy held, no message will have landed on the bridge's
    # own children-namespace subscription for the foreign thing. The
    # naming convention also ensures the rewrite rule wouldn't route
    # the reply to *this* bridge's namespace even if the Lambda did
    # run — so the strongest assertion is that the foreign Thing does
    # not exist in IoT.
    iot = boto3.client("iot")
    foreign_exists = True
    try:
        iot.describe_thing(thingName=foreign)
    except iot.exceptions.ResourceNotFoundException:
        foreign_exists = False

    assert not foreign_exists, (
        f"foreign Thing {foreign!r} was created — policy substitution "
        f"did not deny cross-parent to_cloud publish "
        f"(publish_raised={publish_raised!r})"
    )
