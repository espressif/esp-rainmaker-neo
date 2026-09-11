# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Common utility functions for testing.
"""

import boto3
import json
import time

def shadow_to_unstructured(shadow):
    """
    Restructures shadow state by moving params content one level up.
    Assumes shadow structure defined in shadow_node.go.
    """
    if isinstance(shadow, dict) and "state" in shadow:
        state = shadow["state"]
        if "reported" in state and state["reported"] and "params" in state["reported"]:
            # Move params content one level up while preserving other fields
            params = state["reported"].pop("params")
            state["reported"].update(params)
        if "desired" in state and state["desired"] and "params" in state["desired"]:
            # Do the same for desired state
            params = state["desired"].pop("params")
            state["desired"].update(params)
    return shadow


def describe_thing_attributes(thing_name: str, region: str) -> dict:
    """Return the attribute dict of an IoT Thing via boto3."""
    iot = boto3.client('iot', region_name=region)
    return iot.describe_thing(thingName=thing_name).get('attributes', {})


def wait_until(predicate, description, retries=10, interval=2):
    """Poll *predicate* up to *retries* times, sleeping *interval* seconds between attempts.
    Asserts with *description* if *predicate* never returns True.
    """
    for _ in range(retries):
        if predicate():
            return
        time.sleep(interval)
    assert False, f"Timed out waiting for: {description}"


def seed_node_data(user, group_id, node_id):
    """Seed schedule, trigger, and two automations (trigger-ref and action-ref) for a node in a group.

    Both a condition trigger node and an action target node MUST be members of
    the automation's group: creation rejects either outside the group (the
    cross-tenant device-control guard). Both seeded automations therefore
    reference `node_id` only. On removal of `node_id` both are cleaned up:
    - trigger-ref: `node_id` is in its trigger conditions -> whole automation deleted.
    - action-ref: `node_id` is its sole action target (and it carries no
      conditions, so it does not also match via the trigger path) -> deleted
      once its last target is pruned.
    The "prune-but-keep" path (node removed from an automation that has another
    remaining member target) needs a second member node and is covered by the
    Go unit test in automation_test.go.

    Returns (trigger_auto_id, action_auto_id).
    """
    schedule_data = {"schedules": [{"id": "s1", "name": "Morning", "enabled": True}]}
    assert user.set_node_schedule(group_id, "", node_id, schedule_data), "Failed to set schedule"

    trigger_data = json.dumps({"triggers": [{"id": "t1", "name": "TempHigh"}]})
    assert user.set_node_trigger(group_id, node_id, trigger_data), "Failed to set trigger"

    # node_id appears in the trigger conditions -> whole automation deleted on removal.
    trigger_auto = user.create_automation(group_id, {
        "name": "Trigger auto",
        "conditions": {"and": [f"{node_id}~placeholder~0"]},
        "actions": {"targets": [{"node": node_id, "path": "L.P", "value": True}]},
    })
    assert trigger_auto is not None, "Failed to create trigger automation"

    # node_id is the sole action target and appears in no trigger conditions
    # -> deleted on removal via the action-ref (pruned-to-empty) path. Conditions
    # are omitted: any trigger node would itself have to be a group member, and
    # referencing node_id there would instead exercise the trigger-ref path above.
    action_auto = user.create_automation(group_id, {
        "name": "Action auto",
        "actions": {"targets": [
            {"node": node_id, "path": "Fan.Speed", "value": 2},
        ]},
    })
    assert action_auto is not None, "Failed to create action automation"

    return trigger_auto["automation_id"], action_auto["automation_id"]


def assert_node_data_deleted(user, group_id, node_id, trigger_auto_id, action_auto_id):
    """Assert that seeded node data (schedule, trigger, automations) was cleaned up asynchronously.

    Expects:
      - Schedule deleted
      - Trigger deleted
      - Automation with node in trigger: deleted
      - Automation with node as sole action target: deleted
    """
    wait_until(
        lambda: user.get_node_schedule(group_id, "", node_id) is None,
        "Schedule should be deleted",
    )

    wait_until(
        lambda: user.get_node_trigger(group_id, node_id) is None,
        "Trigger should be deleted",
    )

    wait_until(
        lambda: all(
            a.get("id") != trigger_auto_id
            for a in (user.get_automations(group_id) or [])
        ),
        f"Automation '{trigger_auto_id}' (trigger ref) should be deleted",
    )

    wait_until(
        lambda: all(
            a.get("id") != action_auto_id
            for a in (user.get_automations(group_id) or [])
        ),
        f"Automation '{action_auto_id}' (sole action target) should be deleted",
    )