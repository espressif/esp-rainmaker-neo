# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Integration tests for the superAdmin IoT event-mode API.

Endpoints under test:
  GET  /v1/admin/iot-event-mode
  PUT  /v1/admin/iot-event-mode  body: {"mode": "direct"|"sqs"}

The PUT endpoint flips the action on both node_disconnected_rule and
node_to_cloud_rule between Lambda-direct and SQS in one call. Both
infrastructure paths (lambda permissions, SQS queues, event-source mappings)
are pre-provisioned by the handler stacks, so the flip is a pure
iot:ReplaceTopicRule call with no redeploy.

Each PUT test restores the original mode in a finally block so the suite is
order-independent.
"""
import json

from py_sdk.test_user import user_log
from test.itest.conftest import REGION

import boto3
import pytest


# Every test in this module mutates the action attached to two global IoT
# topic rules (node_disconnected_rule, node_to_cloud_rule). They cannot run in
# parallel with each other: a flip from one test would race the
# read-modify-write cycle of another. xdist_group pins them to a single
# worker so they execute serially under --dist=loadgroup. Tests in other
# modules can still run in parallel with this group: both rule modes are
# functionally equivalent (the unified handler binary processes either
# payload shape) and the restore_iot_event_mode fixture returns the
# deployment to its starting mode after each mutating test.
pytestmark = pytest.mark.xdist_group("iot_event_mode")


PRESENCE_RULE = "node_disconnected_rule"
PUBLISH_INPUT_RULE = "node_to_cloud_rule"
ADMIN_CONFIG_TABLE = "rmng-admin-configs"
ADMIN_CONFIG_KEY = "iot_event_mode"
IOT_EVENT_MODE_LAMBDA = "rmng-iot-event-mode"
VALID_MODES = ("direct", "sqs")


def _action_mode(action):
    """Map an IoT topic-rule Action dict to "direct" | "sqs" | "other"."""
    if "sqs" in action:
        return "sqs"
    if "lambda" in action:
        return "direct"
    return "other"


def _read_rule_modes_via_aws():
    """Read both rules' current first-action mode directly from the IoT
    control plane (i.e. independent of the lambda under test)."""
    iot = boto3.client("iot", region_name=REGION)
    presence = iot.get_topic_rule(ruleName=PRESENCE_RULE)["rule"]
    publish_input = iot.get_topic_rule(ruleName=PUBLISH_INPUT_RULE)["rule"]
    return {
        "presence": _action_mode(presence["actions"][0]),
        "publish_input": _action_mode(publish_input["actions"][0]),
    }


@pytest.fixture
def restore_iot_event_mode(super_admin_user):
    """Snapshot the current IoT-rule mode before the test and restore it on
    teardown. Tests that flip mode should depend on this fixture so a failure
    can't leave the deployment in the wrong mode for subsequent tests."""
    initial = super_admin_user.admin_get_iot_event_mode()
    assert isinstance(initial, dict), \
        f"Failed to read initial iot-event-mode (super-admin): {initial}"
    yield initial
    # Best-effort restore: only flip if at least one rule diverged.
    try:
        current = super_admin_user.admin_get_iot_event_mode()
        if not isinstance(current, dict):
            user_log(f"Could not read post-test mode for restore: {current}")
            return
        if current != initial:
            # We only have a single mode dial, so flip both back together by
            # picking whichever value matches the pre-test state. If presence
            # and publish_input started in different modes (shouldn't happen
            # in normal deploys), prefer the presence value.
            target = initial.get("presence", "direct")
            user_log(f"Restoring iot-event-mode to {target!r} (was {current})")
            super_admin_user.admin_put_iot_event_mode(target)
    except Exception as e:
        user_log(f"Warning: failed to restore iot-event-mode: {e}")


def test_admin_get_iot_event_mode(super_admin_user):
    """GET as super-admin returns both rule modes."""
    user_log("Reading current iot-event-mode...")
    result = super_admin_user.admin_get_iot_event_mode()
    assert isinstance(result, dict), f"Expected dict, got: {result}"
    assert "presence" in result
    assert "publish_input" in result
    assert result["presence"] in VALID_MODES, f"Bad presence mode: {result['presence']}"
    assert result["publish_input"] in VALID_MODES, f"Bad publish_input mode: {result['publish_input']}"

    # Cross-check against the IoT control plane directly: the API's view
    # must match what AWS itself reports.
    aws_view = _read_rule_modes_via_aws()
    assert result == aws_view, (
        f"API mode disagrees with AWS IoT API. lambda={result}, aws={aws_view}"
    )


def test_admin_get_iot_event_mode_denied_for_non_admin(test_user1):
    """GET as a non-admin user returns 403 (helper returns the response)."""
    user_log("Verifying non-admin user cannot read iot-event-mode...")
    result = test_user1.admin_get_iot_event_mode()
    # Helper returns response object on non-200.
    assert hasattr(result, "status_code"), \
        f"Expected raw response on denied access, got: {result}"
    assert result.status_code == 403, \
        f"Expected 403 for non-admin, got {result.status_code}: {result.text}"


def test_admin_put_iot_event_mode_invalid(super_admin_user, restore_iot_event_mode):
    """PUT with an unknown mode returns 400 and does not mutate either rule."""
    user_log("Sending PUT with invalid mode...")
    before = _read_rule_modes_via_aws()

    result = super_admin_user.admin_put_iot_event_mode("bogus")
    assert hasattr(result, "status_code"), \
        f"Expected raw response on validation failure, got: {result}"
    assert result.status_code == 400, \
        f"Expected 400 for invalid mode, got {result.status_code}: {result.text}"

    after = _read_rule_modes_via_aws()
    assert before == after, \
        f"Invalid PUT mutated rule state. before={before}, after={after}"


def test_admin_put_iot_event_mode_denied_for_non_admin(test_user1, restore_iot_event_mode):
    """PUT as a non-admin user returns 403 and does not mutate either rule."""
    user_log("Verifying non-admin user cannot flip iot-event-mode...")
    before = _read_rule_modes_via_aws()

    result = test_user1.admin_put_iot_event_mode("sqs")
    assert hasattr(result, "status_code"), \
        f"Expected raw response on denied access, got: {result}"
    assert result.status_code == 403, \
        f"Expected 403 for non-admin, got {result.status_code}: {result.text}"

    after = _read_rule_modes_via_aws()
    assert before == after, \
        f"Forbidden PUT mutated rule state. before={before}, after={after}"


@pytest.mark.parametrize("target_mode", ["sqs", "direct"])
def test_admin_put_iot_event_mode_flip(super_admin_user, restore_iot_event_mode, target_mode):
    """PUT flips both rules to the requested mode and the change is visible
    both in the API response and in the IoT control plane.

    The fixture handles restoration so this test runs cleanly in either
    direction regardless of the deployment's starting state.
    """
    user_log(f"Flipping iot-event-mode to {target_mode!r}...")

    result = super_admin_user.admin_put_iot_event_mode(target_mode)
    assert isinstance(result, dict), f"Expected dict on success, got: {result}"
    assert result == {"presence": target_mode, "publish_input": target_mode}, \
        f"Unexpected PUT response: {result}"

    # Verify against the live IoT control plane.
    aws_view = _read_rule_modes_via_aws()
    assert aws_view == {"presence": target_mode, "publish_input": target_mode}, \
        f"AWS-side rule actions don't match requested mode. aws={aws_view}"

    # GET should now agree.
    get_result = super_admin_user.admin_get_iot_event_mode()
    assert isinstance(get_result, dict)
    assert get_result == {"presence": target_mode, "publish_input": target_mode}, \
        f"GET after PUT disagrees: {get_result}"


def test_admin_put_iot_event_mode_preserves_sql_and_error_action(super_admin_user, restore_iot_event_mode):
    """A flip must rewrite only the rule's first action — SQL, SQL version,
    description, disabled flag, and error_action all carry over unchanged."""
    iot = boto3.client("iot", region_name=REGION)

    def _snapshot(rule_name):
        rule = iot.get_topic_rule(ruleName=rule_name)["rule"]
        return {
            "sql": rule.get("sql"),
            "awsIotSqlVersion": rule.get("awsIotSqlVersion"),
            "description": rule.get("description"),
            "ruleDisabled": rule.get("ruleDisabled"),
            "errorAction": rule.get("errorAction"),
        }

    initial_state = super_admin_user.admin_get_iot_event_mode()
    assert isinstance(initial_state, dict)
    other_mode = "sqs" if initial_state["presence"] == "direct" else "direct"

    presence_before = _snapshot(PRESENCE_RULE)
    publish_input_before = _snapshot(PUBLISH_INPUT_RULE)

    user_log(f"Flipping to {other_mode!r} to verify non-action fields are preserved...")
    flip_result = super_admin_user.admin_put_iot_event_mode(other_mode)
    assert isinstance(flip_result, dict), f"Flip failed: {flip_result}"

    presence_after = _snapshot(PRESENCE_RULE)
    publish_input_after = _snapshot(PUBLISH_INPUT_RULE)

    assert presence_after == presence_before, (
        f"presence rule non-action fields changed across flip.\n"
        f"before={presence_before}\nafter={presence_after}"
    )
    assert publish_input_after == publish_input_before, (
        f"publish_input rule non-action fields changed across flip.\n"
        f"before={publish_input_before}\nafter={publish_input_after}"
    )


def test_admin_put_persists_to_rmng_admin_config(super_admin_user, restore_iot_event_mode):
    """The PUT path must persist the mode to rmng_admin_config so the
    drift-correction custom resource can restore it after a redeploy.
    Verifies the API → DB write that unit tests can only mock."""
    user_log("Verifying PUT writes the mode to rmng_admin_config...")

    # Pick a target mode that differs from the current state so the write
    # is observable even if the deployment happens to be in the target mode
    # already.
    initial = super_admin_user.admin_get_iot_event_mode()
    assert isinstance(initial, dict)
    target_mode = "sqs" if initial["presence"] == "direct" else "direct"

    flip = super_admin_user.admin_put_iot_event_mode(target_mode)
    assert isinstance(flip, dict), f"Flip failed: {flip}"

    ddb = boto3.client("dynamodb", region_name=REGION)
    item = ddb.get_item(
        TableName=ADMIN_CONFIG_TABLE,
        Key={"config_key": {"S": ADMIN_CONFIG_KEY}},
        ConsistentRead=True,
    ).get("Item")

    assert item is not None, (
        f"PUT to mode={target_mode!r} succeeded but no row written to "
        f"{ADMIN_CONFIG_TABLE} under config_key={ADMIN_CONFIG_KEY!r}"
    )
    assert item["presence"]["S"] == target_mode, item
    assert item["publish_input"]["S"] == target_mode, item
    assert "updated_at" in item and item["updated_at"]["N"], (
        f"updated_at missing or empty: {item}"
    )
    assert "updated_by" in item and item["updated_by"]["S"], (
        f"updated_by missing or empty: {item}"
    )


def test_drift_correction_reapply_restores_runtime_mode(super_admin_user, restore_iot_event_mode):
    """Simulates what happens after a CloudFormation stack update rewrites
    the IoT rule: the row in rmng_admin_config still says SQS, but the live
    rule has been overwritten to Lambda-direct. Invoking the iot_event_mode
    lambda directly with `{"action":"reapply"}` (the same payload the
    AwsCustomResource sends on every deploy) must restore the runtime-set
    mode.

    This is the headline test for the drift-correction feature; without it,
    the only way to exercise the reapply path is to run a real `cdk deploy`.
    """
    iot = boto3.client("iot", region_name=REGION)

    # Step 1: ensure both rules are in direct mode (so we can capture their
    # Lambda actions to replay later as the "CFN rewrite").
    user_log("Drift-test setup: forcing both rules to direct mode...")
    setup = super_admin_user.admin_put_iot_event_mode("direct")
    assert isinstance(setup, dict) and setup == {"presence": "direct", "publish_input": "direct"}, setup

    presence_rule = iot.get_topic_rule(ruleName=PRESENCE_RULE)["rule"]
    publish_input_rule = iot.get_topic_rule(ruleName=PUBLISH_INPUT_RULE)["rule"]
    presence_lambda_action = presence_rule["actions"][0]
    publish_input_lambda_action = publish_input_rule["actions"][0]
    assert "lambda" in presence_lambda_action, presence_lambda_action
    assert "lambda" in publish_input_lambda_action, publish_input_lambda_action

    # Step 2: flip to sqs via the API (writes both the live rule and the
    # rmng_admin_config row).
    user_log("Drift-test: flipping to sqs via API...")
    flip = super_admin_user.admin_put_iot_event_mode("sqs")
    assert isinstance(flip, dict) and flip == {"presence": "sqs", "publish_input": "sqs"}, flip
    assert _read_rule_modes_via_aws() == {"presence": "sqs", "publish_input": "sqs"}

    # Step 3: simulate a CFN rewrite by directly replacing both rules with
    # their original Lambda-direct payloads (preserving SQL, error_action,
    # etc., as CFN would). The row still says sqs.
    user_log("Drift-test: simulating CFN rewrite by replacing both rules with Lambda-direct action...")

    def _payload_with_action(rule, action):
        payload = {"actions": [action]}
        if rule.get("sql") is not None:
            payload["sql"] = rule["sql"]
        if rule.get("description") is not None:
            payload["description"] = rule["description"]
        if rule.get("awsIotSqlVersion") is not None:
            payload["awsIotSqlVersion"] = rule["awsIotSqlVersion"]
        if rule.get("ruleDisabled") is not None:
            payload["ruleDisabled"] = rule["ruleDisabled"]
        if rule.get("errorAction") is not None:
            payload["errorAction"] = rule["errorAction"]
        return payload

    iot.replace_topic_rule(
        ruleName=PRESENCE_RULE,
        topicRulePayload=_payload_with_action(presence_rule, presence_lambda_action),
    )
    iot.replace_topic_rule(
        ruleName=PUBLISH_INPUT_RULE,
        topicRulePayload=_payload_with_action(publish_input_rule, publish_input_lambda_action),
    )
    assert _read_rule_modes_via_aws() == {"presence": "direct", "publish_input": "direct"}, (
        "Manual rewrite did not land both rules in direct mode"
    )

    # Step 4: invoke the iot_event_mode lambda directly with the same
    # payload the AwsCustomResource sends on every deploy.
    user_log("Drift-test: invoking iot_event_mode lambda with reapply payload...")
    lambda_client = boto3.client("lambda", region_name=REGION)
    invoke_response = lambda_client.invoke(
        FunctionName=IOT_EVENT_MODE_LAMBDA,
        InvocationType="RequestResponse",
        Payload=json.dumps({"action": "reapply"}).encode("utf-8"),
    )
    assert invoke_response.get("FunctionError") is None, (
        f"reapply invoke failed: {invoke_response.get('FunctionError')} / "
        f"{invoke_response['Payload'].read().decode()}"
    )
    body = json.loads(invoke_response["Payload"].read())
    assert body.get("status") == "applied", f"Expected applied, got: {body}"
    assert body.get("presence") == "sqs", body
    assert body.get("publish_input") == "sqs", body

    # Step 5: confirm the live rules are back to sqs.
    after = _read_rule_modes_via_aws()
    assert after == {"presence": "sqs", "publish_input": "sqs"}, (
        f"Reapply did not restore runtime mode. live={after}"
    )
    user_log("Drift-test: reapply restored both rules to sqs as expected")


@pytest.mark.unsafe
def test_admin_put_iot_event_mode_round_trip_does_not_disrupt_presence(
    super_admin_user, restore_iot_event_mode, associated_device,
):
    """Flipping to SQS and back to direct should not break the presence
    pipeline: a device that goes offline still gets its shadow updated.

    Marked unsafe because it briefly mutates global infrastructure state
    used by all devices in the deployment.
    """
    device, group_id, _, _ = associated_device

    user_log("Flipping iot-event-mode to sqs and back, with a presence event in between...")
    flip_to_sqs = super_admin_user.admin_put_iot_event_mode("sqs")
    assert isinstance(flip_to_sqs, dict), f"Flip to sqs failed: {flip_to_sqs}"
    assert flip_to_sqs["presence"] == "sqs"

    # Trigger a disconnect; the presence_event_handler lambda must process
    # it via the SQS path. We do not assert the shadow here — that is
    # already covered by test_device_status; we just confirm the flip
    # completes and a flip back also succeeds without error.
    try:
        device.disconnect()
    except Exception:
        pass

    flip_to_direct = super_admin_user.admin_put_iot_event_mode("direct")
    assert isinstance(flip_to_direct, dict), f"Flip to direct failed: {flip_to_direct}"
    assert flip_to_direct["presence"] == "direct"
    assert flip_to_direct["publish_input"] == "direct"
