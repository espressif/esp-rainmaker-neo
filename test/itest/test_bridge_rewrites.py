# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge IoT Rule A / Rule B rewrite + cross-Thing shadow write tests.

Covers the rewrite plane and the policy-substitution security boundary
from the production rmng-bridge stack. Each test runs against a fresh
bridge via the `bridge_in_group` fixture. See docs/en/specs/bridge.md §3.4
(Rules A and B) and §3.5 / §6.3 (policy substitution).
"""

import json
import time
import uuid
from queue import Empty

import boto3
import pytest
from awscrt import mqtt as awscrt_mqtt
from botocore.exceptions import ClientError

RX_TIMEOUT_S = 5.0
SHADOW_SETTLE_S = 1.5


def _wait_for_topic_match(q, expected_topic, timeout=RX_TIMEOUT_S, predicate=None):
    """Drain q until a message with exact topic match arrives that
    optionally satisfies a predicate. Returns the message or None."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            msg = q.get(timeout=max(0.05, deadline - time.time()))
        except Empty:
            return None
        if msg["topic"] != expected_topic:
            continue
        if predicate is None or predicate(msg["payload"]):
            return msg
    return None


@pytest.fixture
def iot_data():
    return boto3.client("iot-data")


def test_rule_a_rewrites_child_from_cloud(bridge_in_group, iot_data, seed_child, drain):
    """cloud publish to `rainmaker/nodes/<child>/from_cloud`
    arrives at the bridge on `rainmaker/bridges/<bridge>/children/<child>/from_cloud`."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    child = seed_child("rewrites_a", "0xRW_A")
    assert child is not None, "addChild failed during fixture setup"

    # Drain the proactive getGroupInfo the bridge handler pushes after
    # addChild, so the next received message is our sentinel.
    time.sleep(1.0)  # let proactive getGroupInfo land
    drain(bridge_in_group["bridges_fc"])

    sentinel = {"_probe": "rule-a-itest", "n": 1}
    iot_data.publish(
        topic=f"rainmaker/nodes/{child}/from_cloud",
        qos=1, payload=json.dumps(sentinel),
    )

    expected = f"rainmaker/bridges/{parent}/children/{child}/from_cloud"
    msg = _wait_for_topic_match(
        bridge_in_group["bridges_fc"], expected,
        predicate=lambda p: p.get("_probe") == "rule-a-itest",
    )
    assert msg is not None, f"no rewrite within {RX_TIMEOUT_S}s on {expected}"
    assert msg["payload"] == sentinel


def test_rule_b_rewrites_child_unicast(bridge_in_group, iot_data, seed_child, drain):
    """Rule B rewrite. topic(5) shadow-name
    segment carried through verbatim."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    group_id = bridge_in_group["group_id"]
    child = seed_child("rewrites_b", "0xRW_B")
    assert child is not None

    drain(bridge_in_group["bridges_uc"])

    shadow_segment = f"params-{group_id}"
    sentinel = {"Light": {"Power": True}, "_probe": "rule-b-itest"}
    iot_data.publish(
        topic=f"rainmaker/nodes/{child}/user/{shadow_segment}/params",
        qos=1, payload=json.dumps(sentinel),
    )

    expected = f"rainmaker/bridges/{parent}/children/{child}/user/{shadow_segment}/params"
    msg = _wait_for_topic_match(
        bridge_in_group["bridges_uc"], expected,
        predicate=lambda p: p.get("_probe") == "rule-b-itest",
    )
    assert msg is not None, f"no rewrite within {RX_TIMEOUT_S}s on {expected}"


def test_bridge_writes_child_shadow(bridge_in_group, iot_data, seed_child):
    """bridge cert writes a child's shadow via
    $aws/things/<self>--*/shadow/name/*/update — the load-bearing policy
    substitution from spec §3.5 / §6.3."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    child = seed_child("shadow_write", "0xSHADOW_W")
    assert child is not None

    shadow_name = f"params-{group_id}"
    # Start from a clean state.
    try:
        iot_data.delete_thing_shadow(thingName=child, shadowName=shadow_name)
    except ClientError as e:
        if e.response["Error"]["Code"] != "ResourceNotFoundException":
            raise

    probe_marker = f"itest-{uuid.uuid4()}"
    payload = {"state": {"reported": {"foo": 1, "_probe": probe_marker}}}
    future, _ = bridge.mqtt_connection.publish(
        topic=f"$aws/things/{child}/shadow/name/{shadow_name}/update",
        payload=json.dumps(payload),
        qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
    )
    future.result(timeout=10)
    time.sleep(SHADOW_SETTLE_S)

    doc_resp = iot_data.get_thing_shadow(thingName=child, shadowName=shadow_name)
    doc = json.loads(doc_resp["payload"].read())
    reported = doc.get("state", {}).get("reported", {})
    assert reported.get("foo") == 1
    assert reported.get("_probe") == probe_marker


def test_cross_parent_shadow_write_denied(bridge_in_group, iot_data):
    """bridge writing to a thing under a
    foreign parent prefix is denied at the policy layer (§3.5 / §6.3).
    AWS IoT may close the MQTT session as a side effect — assert by
    reading the target shadow afterwards."""
    bridge = bridge_in_group["bridge"]
    foreign = f"rmng-other-bridge-99--child_pwn"
    shadow_name = "params-anything"

    publish_raised = None
    try:
        future, _ = bridge.mqtt_connection.publish(
            topic=f"$aws/things/{foreign}/shadow/name/{shadow_name}/update",
            payload=json.dumps({"state": {"reported": {"intruder": True}}}),
            qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
        )
        future.result(timeout=10)
    except Exception as e:
        publish_raised = str(e)
    time.sleep(SHADOW_SETTLE_S)

    # Either the foreign Thing was never created (shadow doesn't exist)
    # or the publish was denied at the policy layer (shadow unchanged).
    try:
        doc_resp = iot_data.get_thing_shadow(
            thingName=foreign, shadowName=shadow_name,
        )
    except ClientError as e:
        if e.response["Error"]["Code"] == "ResourceNotFoundException":
            return  # ✓ shadow absent, policy denial held
        raise

    doc = json.loads(doc_resp["payload"].read())
    reported = doc.get("state", {}).get("reported", {})
    assert reported.get("intruder") is not True, \
        f"policy denial bypassed; foreign shadow shows {reported!r} " \
        f"(publish raised: {publish_raised})"
