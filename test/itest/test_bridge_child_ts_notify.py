# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge ChildrenPublish policy — ts + notify paths for bridged children.

Covers the ts (time-series) and notify (per-device notification) topic
shapes added to the bridge IoT policy `ChildrenPublish` statement
(spec §3.5). The schedule-sync / to_cloud path is covered in
test_bridge_child_to_cloud.py.

The mechanism (same as to_cloud):
  1. Bridge publishes on `rainmaker/nodes/<child>/ts/<group>` (or
     `…/notify/<group>`), authorized by the policy substitution
     `rainmaker/nodes/${self}--*/ts/*` / `…/notify/*`.
  2. The existing `node_ts_rule` (filter `rainmaker/nodes/+/ts/+`)
     extracts `topic(3) = <child>` and writes to the
     `rmng-raw-ts-data` table.
  3. Batched reports take the `/batch` segment and `node_ts_batch_rule`
     (filter `rainmaker/nodes/+/ts/+/batch`) instead, which hands the
     points to timeseries_ingest_handler. The policy needs no new
     statement: `*` spans `/` in an IoT policy resource, so the
     existing `ts/*` grant already covers `ts/<group>/batch`.
  4. Symmetric for `node_notify_rule` invoking the notifications
     Lambda.

The naming-constraint argument from §3.5 holds: a bridge cannot
publish on any other parent's `/ts/` or `/notify/` because the policy
substitution pins the prefix segment.
"""

import json
import time
import uuid
from queue import Empty

import boto3
import pytest
from awscrt import mqtt as awscrt_mqtt

RAW_TS_DATA_TABLE = "rmng-raw-ts-data"
TS_INGEST_SETTLE_S = 4.0


def _bridge_publish(bridge, topic, payload):
    """Publish on the bridge's MQTT session. Uses direct (non-basic-ingest)
    topics so the AWS IoT policy substitution is the actual authorization
    gate under test."""
    future, _ = bridge.mqtt_connection.publish(
        topic=topic,
        payload=json.dumps(payload),
        qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
    )
    future.result(timeout=10)


@pytest.fixture
def ddb():
    return boto3.client("dynamodb")


def _ts_row_exists(ddb, node_key_dt, ts_ms):
    """Direct GetItem against raw_ts_data using the deterministic PK/SK."""
    out = ddb.get_item(
        TableName=RAW_TS_DATA_TABLE,
        Key={
            "node_key_dt": {"S": node_key_dt},
            "ts": {"N": str(ts_ms)},
        },
    )
    return "Item" in out


def test_bridge_publishes_child_ts(bridge_in_group, ddb, seed_child):
    """Happy path: bridge publishes a ts row on behalf of a child →
    node_ts_rule ingests into raw_ts_data table, keyed by
    `<child>.<k>.<dt>`."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    child = seed_child("ts_child", "0xTS_CHILD1")
    assert child is not None, "addChild failed during setup"

    # Use a unique key+timestamp to avoid collisions with anything else
    # the test environment may have ingested.
    probe = f"itest_{uuid.uuid4().hex[:8]}"
    ts_ms = int(time.time() * 1000)
    payload = {
        "k": probe,
        "dt": "int",
        "tz": "UTC",
        "t": ts_ms,
        "v": 42,
        "cumulative": False,
    }
    topic = f"rainmaker/nodes/{child}/ts/{group_id}"
    _bridge_publish(bridge, topic, payload)

    # Ingestion is eventually consistent; poll the table briefly.
    expected_node_key_dt_prefix = f"{child}.{probe}."
    deadline = time.time() + TS_INGEST_SETTLE_S * 2
    found = False
    while time.time() < deadline:
        if _ts_row_exists(ddb, f"{child}.{probe}.int", ts_ms):
            found = True
            break
        time.sleep(0.5)

    assert found, (
        f"ts row not found in {RAW_TS_DATA_TABLE} for node_key_dt="
        f"{expected_node_key_dt_prefix}int ts={ts_ms}"
    )


def test_bridge_publishes_child_notify(bridge_in_group, seed_child, drain):
    """Bridge publishes on a child's notify topic. We can't easily
    observe the webhook dispatch from an itest (would need a public
    HTTP listener), so this test focuses on the MQTT-layer
    authorization: the publish must complete without the broker
    closing the session, and a follow-up control-plane round-trip must
    still succeed (proves the session is alive — a policy violation
    typically tears it down).

    Combined with the symmetric coverage of /ts/ in the test above
    (which proves the policy enumeration shape works end-to-end), this
    gives reasonable confidence that /notify/ is also wired
    correctly."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    parent = bridge.node_thing_name
    child = seed_child("notify_child", "0xNOTIFY1")
    assert child is not None

    topic = f"rainmaker/nodes/{child}/notify/{group_id}"
    _bridge_publish(bridge, topic, {"notify": {"event": "test_probe"}})

    # Liveness probe: round-trip getGroupInfo on the bridge's own
    # to_cloud and ensure the reply lands on subscription #1. If the
    # notify publish triggered a session teardown, the publish below
    # would either fail outright or never reach the broker.
    drain(bridge_in_group["from_cloud"])
    bridge.publish_to_cloud({"event": ["getGroupInfo"]}, basic_ingest=False)

    deadline = time.time() + 5.0
    got_reply = False
    while time.time() < deadline:
        try:
            msg = bridge_in_group["from_cloud"].get(
                timeout=max(0.05, deadline - time.time()),
            )
        except Empty:
            continue
        events = msg["payload"].get("event") or []
        if isinstance(events, list) and "getGroupInfo" in events:
            got_reply = True
            break

    assert got_reply, (
        f"no getGroupInfo reply after notify publish — session likely "
        f"torn down by broker (policy denial?). parent={parent} child={child}"
    )


def test_cross_parent_ts_publish_denied(bridge_in_group, ddb):
    """The ChildrenPublish policy substitution pins ts publishes to
    `rainmaker/nodes/<self>--*/ts/*`. A bridge publishing on a foreign
    child's ts topic must be denied at the policy layer.

    Verified by checking that no raw_ts_data row was ingested for the
    foreign thing — if the policy held, the publish never reached the
    broker and node_ts_rule never ran."""
    bridge = bridge_in_group["bridge"]
    foreign = "rmng-other-bridge-88--ts_intruder"
    probe = f"itest_attack_{uuid.uuid4().hex[:8]}"
    ts_ms = int(time.time() * 1000)
    payload = {
        "k": probe, "dt": "int", "tz": "UTC", "t": ts_ms,
        "v": 9999, "cumulative": False,
    }
    topic = f"rainmaker/nodes/{foreign}/ts/anything"

    publish_raised = None
    try:
        _bridge_publish(bridge, topic, payload)
    except Exception as e:
        publish_raised = str(e)

    time.sleep(TS_INGEST_SETTLE_S)

    foreign_node_key_dt = f"{foreign}.{probe}.int"
    assert not _ts_row_exists(ddb, foreign_node_key_dt, ts_ms), (
        f"foreign ts row was ingested ({foreign_node_key_dt} ts={ts_ms}) — "
        f"policy substitution did not deny cross-parent /ts/ publish "
        f"(publish_raised={publish_raised!r})"
    )


def test_bridge_publishes_child_ts_batch(bridge_in_group, ddb, seed_child):
    """Batched counterpart of test_bridge_publishes_child_ts: the bridge
    publishes several points for a child in one message on the `/batch`
    topic, and timeseries_ingest_handler writes one raw_ts_data row per
    point, keyed the same way as the single-point path.

    This covers the `ts/*` grant stretching over the extra `/batch`
    segment — the reason the batch topic was given a suffix rather than a
    separate topic shape."""
    bridge = bridge_in_group["bridge"]
    group_id = bridge_in_group["group_id"]
    child = seed_child("ts_batch_child", "0xTS_BATCH1")
    assert child is not None, "addChild failed during setup"

    probe = f"itest_{uuid.uuid4().hex[:8]}"
    ts_ms = int(time.time() * 1000)
    # Two keys of different dt so each lands on its own node_key_dt, proving
    # the batch fans out into one row per point rather than one per message.
    points = [
        {"k": f"{probe}_a", "dt": "int", "tz": "UTC", "t": ts_ms, "v": 7, "cumulative": False},
        {"k": f"{probe}_b", "dt": "float", "tz": "UTC", "t": ts_ms + 1, "v": 1.5, "cumulative": False},
    ]
    topic = f"rainmaker/nodes/{child}/ts/{group_id}/batch"
    _bridge_publish(bridge, topic, {"data": points})

    expected = [
        (f"{child}.{probe}_a.int", ts_ms),
        (f"{child}.{probe}_b.float", ts_ms + 1),
    ]
    # Longer than the single-point budget: this path adds a Lambda hop, and
    # in SQS mode an event-source-mapping batching window on top.
    deadline = time.time() + TS_INGEST_SETTLE_S * 3
    pending = list(expected)
    while pending and time.time() < deadline:
        pending = [(k, t) for k, t in pending if not _ts_row_exists(ddb, k, t)]
        if pending:
            time.sleep(0.5)

    assert not pending, (
        f"batch ts rows not found in {RAW_TS_DATA_TABLE} for child={child}: "
        f"{pending}"
    )


def test_cross_parent_ts_batch_publish_denied(bridge_in_group, ddb):
    """The `/batch` suffix must not open a hole in the parent pinning that
    test_cross_parent_ts_publish_denied covers for single points."""
    bridge = bridge_in_group["bridge"]
    foreign = "rmng-other-bridge-88--ts_batch_intruder"
    probe = f"itest_attack_{uuid.uuid4().hex[:8]}"
    ts_ms = int(time.time() * 1000)
    points = [
        {"k": probe, "dt": "int", "tz": "UTC", "t": ts_ms, "v": 9999, "cumulative": False},
    ]
    topic = f"rainmaker/nodes/{foreign}/ts/anything/batch"

    publish_raised = None
    try:
        _bridge_publish(bridge, topic, {"data": points})
    except Exception as e:
        publish_raised = str(e)

    time.sleep(TS_INGEST_SETTLE_S)

    foreign_node_key_dt = f"{foreign}.{probe}.int"
    assert not _ts_row_exists(ddb, foreign_node_key_dt, ts_ms), (
        f"foreign batch ts row was ingested ({foreign_node_key_dt} ts={ts_ms}) — "
        f"policy substitution did not deny cross-parent /ts/ batch publish "
        f"(publish_raised={publish_raised!r})"
    )
