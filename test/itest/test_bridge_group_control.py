# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge group-control subscription tests.

Verifies that a bridge subscribed to the two group-control filters from
spec §3.3.1 / group-control-feature.md §3.1 actually receives both
group-level broadcasts and subgroup commands from the cloud.

The dispatch-to-children logic itself is exercised in unit form
inside bridge_sim.BridgeSim._dispatch_targets; here we only confirm
that the production stack delivers the right messages to the bridge.
"""

import json
import time
from queue import Empty, Queue

import boto3
import pytest
from awscrt import mqtt as awscrt_mqtt

RX_TIMEOUT_S = 5.0


@pytest.fixture
def gc_subs(bridge_in_group):
    """Adds the two group-control subscriptions on top of the four base
    subs the bridge_in_group fixture already established. Returns the
    bridge_in_group dict augmented with a `gc_messages` queue that
    captures everything received on either GC filter."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    parent = bridge.node_thing_name

    gc_q: Queue = Queue()

    def _cb(topic, payload, **_):
        try:
            msg = json.loads(payload.decode())
        except (json.JSONDecodeError, UnicodeDecodeError):
            msg = {"_raw": payload.decode(errors="replace")}
        gc_q.put({"topic": topic, "payload": msg})

    for f in (
        f"rainmaker/nodes/groups/{group_id}/control",
        f"rainmaker/nodes/groups/{group_id}/subgroups/+/control",
    ):
        future, _ = bridge.mqtt_connection.subscribe(
            topic=f, qos=awscrt_mqtt.QoS.AT_LEAST_ONCE, callback=_cb,
        )
        future.result(timeout=10)

    yield {**bridge_in_group, "gc_messages": gc_q, "parent": parent}


def _wait_for(q, predicate, timeout=RX_TIMEOUT_S):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            msg = q.get(timeout=max(0.05, deadline - time.time()))
        except Empty:
            return None
        if predicate(msg):
            return msg
    return None


@pytest.fixture
def iot_data():
    return boto3.client("iot-data")


def test_bridge_receives_group_broadcast(gc_subs, iot_data):
    """Cloud publish to `rainmaker/nodes/groups/<g>/control` is delivered
    to the bridge via subscription #3 from spec §3.3.1."""
    group_id = gc_subs["group_id"]
    topic = f"rainmaker/nodes/groups/{group_id}/control"

    sentinel = {"esp.device.light": {"params": {"esp.param.power": True}},
                "_probe": "gc-broadcast-itest"}
    iot_data.publish(topic=topic, qos=1, payload=json.dumps(sentinel))

    msg = _wait_for(
        gc_subs["gc_messages"],
        lambda m: m["topic"] == topic and m["payload"].get("_probe") == "gc-broadcast-itest",
    )
    assert msg is not None, f"no group broadcast within {RX_TIMEOUT_S}s on {topic}"
    assert msg["payload"]["esp.device.light"]["params"]["esp.param.power"] is True


def test_bridge_receives_subgroup_broadcast(gc_subs, iot_data):
    """Cloud publish to `rainmaker/nodes/groups/<g>/subgroups/<sg>/control`
    is delivered via the bridge's subgroup-wildcard subscription #4 even
    though no such subgroup actually exists in DDB. The bridge's policy
    permits subscription to any subgroup under its own group; the broker
    fans out by topic match, not by DDB membership."""
    group_id = gc_subs["group_id"]
    fake_sg = "sgFakeForBroadcastTest"
    topic = f"rainmaker/nodes/groups/{group_id}/subgroups/{fake_sg}/control"

    sentinel = {"esp.device.fan": {"params": {"esp.param.power": False}},
                "_probe": "gc-subgroup-itest"}
    iot_data.publish(topic=topic, qos=1, payload=json.dumps(sentinel))

    msg = _wait_for(
        gc_subs["gc_messages"],
        lambda m: m["topic"] == topic and m["payload"].get("_probe") == "gc-subgroup-itest",
    )
    assert msg is not None, f"no subgroup command within {RX_TIMEOUT_S}s on {topic}"
    assert msg["payload"]["esp.device.fan"]["params"]["esp.param.power"] is False


def test_dispatch_targets_pure():
    """Unit test for BridgeSim._dispatch_targets — the pure dispatch
    rule from spec §5.4 / group-control-feature.md §3.5. No AWS
    interaction; verifies the targeting logic directly."""
    # bridge_sim is a script, not an importable module (test/bridge_sim.py).
    # Load it by path so the test does not depend on package/sys.path layout.
    import importlib.util
    import os
    _sim_path = os.path.join(os.path.dirname(__file__), "..", "bridge_sim.py")
    _spec = importlib.util.spec_from_file_location("bridge_sim", _sim_path)
    _bridge_sim = importlib.util.module_from_spec(_spec)
    _spec.loader.exec_module(_bridge_sim)
    BridgeMQTT, BridgeSim, ChildState = (
        _bridge_sim.BridgeMQTT,
        _bridge_sim.BridgeSim,
        _bridge_sim.ChildState,
    )

    # Build a sim without actually connecting. The constructor only
    # touches mqtt_client.thing_name on init, so a minimal stand-in
    # works.
    class _StubMQTT:
        thing_name = "bridge-stub"

    sim = BridgeSim.__new__(BridgeSim)  # bypass __init__'s MQTT setup
    sim.mqtt = _StubMQTT()
    sim.thing = "bridge-stub"
    sim.group = "grpA"
    sim.subgroups = ["sgX"]
    sim.children = {
        "bridge-stub--child_alpha": ChildState(name="alpha", group="grpA", subgroups=["sgX", "sgY"]),
        "bridge-stub--child_beta":  ChildState(name="beta",  group="grpA", subgroups=["sgY"]),
        "bridge-stub--child_gamma": ChildState(name="gamma", group="grpA", subgroups=[]),
    }
    sim.local_id_to_child = {}
    sim._gc_subscribed_for_group = "grpA"

    # Group-level broadcast → everyone.
    self_t, kids = sim._dispatch_targets(None)
    assert self_t is True
    assert kids == [
        "bridge-stub--child_alpha",
        "bridge-stub--child_beta",
        "bridge-stub--child_gamma",
    ]

    # Subgroup sgX → bridge (in sgX) + child_alpha (in sgX).
    self_t, kids = sim._dispatch_targets("sgX")
    assert self_t is True
    assert kids == ["bridge-stub--child_alpha"]

    # Subgroup sgY → bridge NOT in sgY; child_alpha + child_beta.
    self_t, kids = sim._dispatch_targets("sgY")
    assert self_t is False
    assert kids == ["bridge-stub--child_alpha", "bridge-stub--child_beta"]

    # Subgroup with no members → empty.
    self_t, kids = sim._dispatch_targets("sgGhost")
    assert self_t is False
    assert kids == []
