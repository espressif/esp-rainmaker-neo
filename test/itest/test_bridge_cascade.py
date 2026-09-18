# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Bridge cascade-delete integration test.

Removes the bridge from its group via the user API (DELETE
/v1/groups/{group_id}/nodes/{node_id}), which exercises the full
production path: ShadowNodeRemoveFromGroup → MaybeInvokeForBridge →
cascade Lambda → child teardown (DDB row, IoT Thing, group_node_assoc).
See docs/en/specs/bridge.md §5.8.
"""

import time

import boto3
import pytest
from botocore.exceptions import ClientError

BRIDGE_CHILDREN_TABLE = "rmng-bridge-children"
GROUP_NODE_ASSOC_TABLE = "rmng-group-node-assoc"
CASCADE_POLL_TIMEOUT_S = 15.0
CASCADE_POLL_INTERVAL_S = 0.5


def _count_children(ddb, parent):
    try:
        resp = ddb.query(
            TableName=BRIDGE_CHILDREN_TABLE,
            KeyConditionExpression="parent_node_id = :p",
            ExpressionAttributeValues={":p": {"S": parent}},
            Select="COUNT",
        )
    except ClientError:
        return -1
    return resp.get("Count", 0)


@pytest.fixture
def aws_clients():
    return {
        "ddb": boto3.client("dynamodb"),
        "iot": boto3.client("iot"),
    }


def test_cascade_delete_fans_out_child_cleanup(bridge_in_group, aws_clients, seed_child):
    """Seed 3 children, remove the bridge from its group via the user API,
    assert all children removed (DDB, IoT Thing, group_node_assoc) within
    the polling window.

    Exercises the full production path: DELETE /v1/groups/{gid}/nodes/{nid}
    → ShadowNodeRemoveFromGroup → MaybeInvokeForBridge → cascade Lambda."""
    bridge = bridge_in_group["bridge"]
    parent = bridge.node_thing_name
    group_id = bridge_in_group["group_id"]
    group_api = bridge_in_group["group_api"]

    seeded = []
    for i in range(3):
        child = seed_child(f"casc_{i}", f"0xCASC_{i:03d}")
        assert child is not None, f"addChild failed to seed cascade_{i}"
        seeded.append(child)

    # Sanity: bridge_children rows present before the trigger fires.
    pre_count = _count_children(aws_clients["ddb"], parent)
    assert pre_count >= 3, f"expected ≥3 rows before cascade, saw {pre_count}"

    # Remove the bridge from its group — this is the production trigger.
    # ShadowNodeRemoveFromGroup calls MaybeInvokeForBridge which fires the
    # cascade Lambda asynchronously.
    group_api.remove_node_from_group(group_id, parent)

    # Poll until all 3 rows are gone.
    deadline = time.time() + CASCADE_POLL_TIMEOUT_S
    while time.time() < deadline:
        if _count_children(aws_clients["ddb"], parent) == 0:
            break
        time.sleep(CASCADE_POLL_INTERVAL_S)
    final_count = _count_children(aws_clients["ddb"], parent)
    assert final_count == 0, \
        f"after {CASCADE_POLL_TIMEOUT_S}s, bridge_children still has {final_count} rows"

    # IoT Things must be gone.
    for child in seeded:
        try:
            aws_clients["iot"].describe_thing(thingName=child)
            pytest.fail(f"IoT Thing {child!r} still exists after cascade")
        except ClientError as e:
            assert e.response["Error"]["Code"] == "ResourceNotFoundException"

    # group_node_assoc rows must be gone.
    for child in seeded:
        out = aws_clients["ddb"].get_item(
            TableName=GROUP_NODE_ASSOC_TABLE,
            Key={"group_id": {"S": group_id}, "node_id": {"S": child}},
        )
        assert not out.get("Item"), \
            f"group_assoc row for ({group_id}, {child}) still present"
