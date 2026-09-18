# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge addChild / removeChild lifecycle tests.

Covers the production rmng-bridge stack's addChild + removeChild
flows. Each test runs against a fresh bridge (registered via
capabilities=['bridge']) in a fresh group via the `bridge_in_group`
fixture in conftest.py. See docs/en/specs/bridge.md §4.2 / §4.3 for the
addChild/removeChild protocol.
"""

import json
import time
import uuid
from queue import Empty

import boto3
import pytest
from botocore.exceptions import ClientError

BRIDGE_CHILDREN_TABLE = "rmng-bridge-children"
GROUP_NODE_ASSOC_TABLE = "rmng-group-node-assoc"
ACK_TIMEOUT_S = 8.0


# ---------------------------------------------------------------------------
# Small helpers — published as bridge cert via the fixture's MQTT connection.
# Not in conftest because they're specific to the bridge MQTT envelope.
# ---------------------------------------------------------------------------


def _drain(q):
    while True:
        try:
            q.get_nowait()
        except Empty:
            return


def _wait_for_ack(q, request_id, timeout=ACK_TIMEOUT_S):
    """Drain `q` until a bridgeAck with the given request_id arrives,
    or time out and return None."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            msg = q.get(timeout=max(0.05, deadline - time.time()))
        except Empty:
            return None
        events = msg["payload"].get("event") or []
        if isinstance(events, list) and "bridgeAck" in events:
            ack = msg["payload"].get("bridgeAck", {})
            if ack.get("request_id") == request_id:
                return ack
    return None


def _publish_bridge_to_cloud(bridge, payload, *, basic_ingest=True):
    """Publish to Rule D's source. Both paths must work:

      basic_ingest=True (default) — production path; the rule name is
        baked into the topic and the broker is bypassed entirely.
      basic_ingest=False         — direct topic; broker fans out to
        every matching rule (currently only Rule D).
    """
    direct = f"rainmaker/bridges/{bridge.node_thing_name}/to_cloud"
    topic = f"$aws/rules/bridge_to_cloud_rule/{direct}" if basic_ingest else direct
    bridge._publish_to_topic(topic, payload, "bridge_to_cloud")


def _send_add_child(bridge, from_cloud_q, *, suffix, local_id, basic_ingest=True):
    req_id = str(uuid.uuid4())[:8]
    _drain(from_cloud_q)
    _publish_bridge_to_cloud(bridge, {
        "event": ["addChild"],
        "addChild": {
            "request_id": req_id,
            "child_suffix": suffix,
            "child_local_id": local_id,
        },
    }, basic_ingest=basic_ingest)
    return _wait_for_ack(from_cloud_q, req_id)


def _send_remove_child(bridge, from_cloud_q, *, child_node_id, basic_ingest=True):
    req_id = str(uuid.uuid4())[:8]
    _drain(from_cloud_q)
    _publish_bridge_to_cloud(bridge, {
        "event": ["removeChild"],
        "removeChild": {
            "request_id": req_id,
            "child_node_id": child_node_id,
        },
    }, basic_ingest=basic_ingest)
    return _wait_for_ack(from_cloud_q, req_id)


def _ddb_get_bridge_child(ddb, parent, child):
    out = ddb.get_item(
        TableName=BRIDGE_CHILDREN_TABLE,
        Key={
            "parent_node_id": {"S": parent},
            "child_node_id": {"S": child},
        },
    )
    return out.get("Item")


def _ddb_get_group_assoc(ddb, group_id, node_id):
    out = ddb.get_item(
        TableName=GROUP_NODE_ASSOC_TABLE,
        Key={"group_id": {"S": group_id}, "node_id": {"S": node_id}},
    )
    return out.get("Item")


def _iot_thing_exists(iot, thing):
    try:
        iot.describe_thing(thingName=thing)
        return True
    except ClientError as e:
        if e.response["Error"]["Code"] == "ResourceNotFoundException":
            return False
        raise


@pytest.fixture
def aws_clients():
    """Local AWS clients for state assertions (DDB + IoT control plane)."""
    return {
        "ddb": boto3.client("dynamodb"),
        "iot": boto3.client("iot"),
    }


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "basic_ingest,label",
    [(True, "basic_ingest"), (False, "direct_topic")],
)
def test_addchild_creates_thing_ddb_and_group_assoc(
    bridge_in_group, aws_clients, basic_ingest, label,
):
    """addChild creates IoT Thing, bridge_children
    row, and group_device_mapping entry for the new child. Runs over
    both Rule D publish paths — production basic-ingest and the direct
    broker-fanout topic — to lock in that either path reaches the
    Lambda."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    parent = bridge.node_thing_name
    suffix = f"lifecycle_{label}"

    ack = _send_add_child(bridge, bridge_in_group["from_cloud"],
                          suffix=suffix, local_id=f"0xLIFE_{label}",
                          basic_ingest=basic_ingest)
    assert ack is not None, f"no bridgeAck within timeout (path={label})"
    assert ack.get("status") == "success", f"ack: {ack}"
    child = ack.get("child_node_id")
    assert child == f"{parent}--{suffix}"

    ddb = aws_clients["ddb"]
    iot = aws_clients["iot"]
    assert _ddb_get_bridge_child(ddb, parent, child) is not None
    assert _iot_thing_exists(iot, child)
    assert _ddb_get_group_assoc(ddb, group_id, child) is not None


def test_addchild_idempotent_on_child_local_id(bridge_in_group):
    """same child_local_id → same child name,
    no duplicate row."""
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]

    ack1 = _send_add_child(bridge, q, suffix="idem_one", local_id="0xIDEM_001")
    assert ack1 is not None and ack1.get("status") == "success"
    first_child = ack1["child_node_id"]

    ack2 = _send_add_child(bridge, q, suffix="idem_one", local_id="0xIDEM_001")
    assert ack2 is not None and ack2.get("status") == "success"
    assert ack2.get("idempotent") is True, f"second addChild did not set idempotent=true: {ack2}"
    assert ack2.get("child_node_id") == first_child


def test_addchild_rejects_suffix_reuse(bridge_in_group):
    """same suffix + different local_id is rejected."""
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]

    ack1 = _send_add_child(bridge, q, suffix="colli_one", local_id="0xCOLLI_001")
    assert ack1 is not None and ack1.get("status") == "success"

    ack2 = _send_add_child(bridge, q, suffix="colli_one", local_id="0xCOLLI_DIFFERENT")
    assert ack2 is not None
    assert ack2.get("status") == "error"
    assert ack2.get("error") == "child_suffix_in_use", f"unexpected error: {ack2.get('error')}"


@pytest.mark.parametrize(
    "basic_ingest,label",
    [(True, "basic_ingest"), (False, "direct_topic")],
)
def test_removechild_clears_thing_ddb_and_group_assoc(
    bridge_in_group, aws_clients, basic_ingest, label,
):
    """removeChild deletes Thing, bridge_children
    row, and group_device_mapping entry. Parametrized over both
    Rule D publish paths."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    parent = bridge.node_thing_name
    q = bridge_in_group["from_cloud"]
    suffix = f"rm_{label}"

    add_ack = _send_add_child(bridge, q, suffix=suffix,
                              local_id=f"0xRM_{label}",
                              basic_ingest=basic_ingest)
    assert add_ack is not None and add_ack.get("status") == "success"
    child = add_ack["child_node_id"]

    rm_ack = _send_remove_child(bridge, q, child_node_id=child,
                                basic_ingest=basic_ingest)
    assert rm_ack is not None and rm_ack.get("status") == "success"

    ddb = aws_clients["ddb"]
    iot = aws_clients["iot"]
    assert _ddb_get_bridge_child(ddb, parent, child) is None
    assert not _iot_thing_exists(iot, child)
    assert _ddb_get_group_assoc(ddb, group_id, child) is None


@pytest.mark.parametrize(
    "bad_suffix,label",
    [
        ("with-hyphen", "hyphen"),
        ("", "empty"),
        ("a" * 33, "too-long"),
        ("has space", "space"),
        ("dotted.suffix", "dot"),
    ],
)
def test_addchild_rejects_invalid_suffix(bridge_in_group, bad_suffix, label):
    """addChild regex enforcement.
    Each invalid suffix shape must come back as `invalid_suffix`."""
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]
    ack = _send_add_child(bridge, q, suffix=bad_suffix, local_id=f"0xBAD_{label}")
    assert ack is not None, f"no ack for {label}"
    assert ack.get("status") == "error", f"case {label} accepted: {ack}"
    assert ack.get("error") == "invalid_suffix", \
        f"case {label}: got error={ack.get('error')}, want invalid_suffix"


def test_removechild_rejects_forged_cross_bridge_child(bridge_in_group):
    """removeChild with a child name
    that doesn't start with `<bridge>--` is rejected with child_not_owned,
    even when the publisher's clientid is correctly pinned."""
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]
    foreign = "rmng-some-other-bridge-99--child_pwn"
    ack = _send_remove_child(bridge, q, child_node_id=foreign)
    assert ack is not None
    assert ack.get("status") == "error"
    assert ack.get("error") == "child_not_owned"


def test_addchild_via_node_to_cloud_basic_ingest_is_a_noop(bridge_in_group):
    """Regression: publishing addChild via node_to_cloud_rule's
    basic-ingest prefix must NOT reach the bridge Lambda. Basic ingest
    invokes only the named rule, and node_to_cloud_rule's handler
    doesn't know about addChild — so the message is silently dropped.
    This test pins the failure mode: no bridgeAck arrives within
    ACK_TIMEOUT_S.

    If this test starts producing an ack, someone has re-introduced
    the additive-rule pattern (rule on rainmaker/nodes/+/to_cloud) and
    the bridge has lost its dedicated control-plane topic. See
    docs/en/specs/bridge.md §3.4 + §3.5."""
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]
    req_id = str(uuid.uuid4())[:8]
    _drain(q)

    wrong_topic = (
        f"$aws/rules/node_to_cloud_rule/"
        f"rainmaker/nodes/{bridge.node_thing_name}/to_cloud"
    )
    bridge._publish_to_topic(wrong_topic, {
        "event": ["addChild"],
        "addChild": {
            "request_id": req_id,
            "child_suffix": "wrong_rule_one",
            "child_local_id": "0xWRONGRULE_001",
        },
    }, "wrong_rule_addchild")

    ack = _wait_for_ack(q, req_id, timeout=3.0)
    assert ack is None, (
        f"unexpected bridgeAck on the wrong-rule basic-ingest path: {ack}. "
        "addChild must only be reachable via bridge_to_cloud_rule."
    )


def test_removechild_idempotent_for_unknown_child(bridge_in_group):
    """removeChild for an absent but
    correctly-prefixed child returns idempotent success (spec §4.3 step 2).
    """
    bridge = bridge_in_group["bridge"]
    q = bridge_in_group["from_cloud"]
    parent = bridge.node_thing_name
    ghost = f"{parent}--ghost_never_existed"
    ack = _send_remove_child(bridge, q, child_node_id=ghost)
    assert ack is not None
    assert ack.get("status") == "success"
    assert ack.get("idempotent") is True
