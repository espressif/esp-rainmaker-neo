# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the RainMaker Neo Controller node type.

For a controller scoped to a single group, these cover:
  - registration attaches rmng-controller-policy, and a plain node does not get it;
  - REST: the controller signs with credential-provider credentials (cert -> credential provider ->
    SigV4), is identified from its certificate, reads its group and a group node's config, and is
    refused a write and a group it is not in;
  - MQTT: it reads a node's shadow, controls one node, the whole group and a subgroup, receives
    group broadcasts, and every policy statement refuses another group's topics;
  - removal: a controller taken out of its group loses REST access at once and MQTT access once it
    reconnects;
  - a node that is both a bridge and a controller keeps both roles.

The calls are driven from Python by the Device helpers (controller_connect_api,
controller_api_request, publish_node_params, publish_group_control), mirroring what real controller
firmware does. See docs/en/specs/controller.md.
"""

import json
import time
import uuid
from queue import Empty, Queue

import boto3
import pytest
from awscrt import mqtt as awscrt_mqtt
from botocore.exceptions import ClientError

from py_sdk.test_group import Group
from test.itest.conftest import (
    API_GATEWAY_URL,
    CONTROLLER_ROLE_ALIAS,
    CREDENTIAL_PROVIDER_ENDPOINT,
    REGION,
    associate_and_reconnect,
    bridge_associate_and_ready,
    bridge_child_ids,
    cleanup_bridge_children,
    connect_device_with_retry,
    iot_data_client,
    publish_add_child,
    stored_node_type,
)

CONTROLLER_POLICY_NAME = "rmng-controller-policy"
BRIDGE_POLICY_NAME = "rmng-bridge-policy"
SHADOW_SETTLE_S = 1.5

# The controller role alias is an rmng-controller-base output; without it the optional controller
# stack group is not deployed and every assertion here would be about something that does not exist.
pytestmark = pytest.mark.skipif(
    not CONTROLLER_ROLE_ALIAS,
    reason="ControllerRoleAliases output not present; deploy the controller stack group",
)


def _attached_policy_names(thing_name):
    """Policy names attached to a thing's certificate(s)."""
    iot = boto3.client("iot", region_name=REGION)
    names = set()
    for principal in iot.list_thing_principals(thingName=thing_name).get("principals", []):
        for policy in iot.list_attached_policies(target=principal).get("policies", []):
            names.add(policy["policyName"])
    return names


def _connect_api(controller):
    """Get the controller's SigV4 credentials from the credential provider, as firmware does before any REST call."""
    controller.controller_connect_api(CREDENTIAL_PROVIDER_ENDPOINT, CONTROLLER_ROLE_ALIAS, REGION, API_GATEWAY_URL)


def _attempt_shadow_write(device, thing_name, shadow_name, marker):
    """Publish a reported-state write on another thing's shadow. A denied publish may close the session, so the outcome is read back from the shadow, not from the publish."""
    try:
        future, _ = device.mqtt_connection.publish(
            topic=f"$aws/things/{thing_name}/shadow/name/{shadow_name}/update",
            payload=json.dumps({"state": {"reported": {"_probe": marker}}}),
            qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
        )
        future.result(timeout=10)
    except Exception:
        pass
    time.sleep(SHADOW_SETTLE_S)


def _shadow_probe(thing_name, shadow_name):
    try:
        doc = json.loads(iot_data_client().get_thing_shadow(thingName=thing_name, shadowName=shadow_name)["payload"].read())
    except ClientError as e:
        if e.response["Error"]["Code"] == "ResourceNotFoundException":
            return None
        raise
    return doc.get("state", {}).get("reported", {}).get("_probe")


@pytest.fixture
def other_group(test_user2, bare_device):
    """A second group, owned by another user, holding one connected plain node: (group_id, node)."""
    group_api = Group(test_user2)
    group_id = group_api.create_group("Other Group (no controller)")
    node = bare_device()
    try:
        associate_and_reconnect(node, test_user2, group_id)
        yield group_id, node
    finally:
        group_api.delete_group(group_id, warn_error=True)


def test_registering_a_controller_attaches_its_policy(bare_device):
    """capabilities=["controller"] attaches rmng-controller-policy and the node still connects."""
    controller = bare_device(capabilities=["controller"])

    assert connect_device_with_retry(controller), "registered controller failed to connect"

    attached = _attached_policy_names(controller.node_thing_name)
    assert CONTROLLER_POLICY_NAME in attached, \
        f"{CONTROLLER_POLICY_NAME} not attached; attached={sorted(attached)}"


def test_plain_node_does_not_get_the_controller_policy(bare_device):
    """A node registered without the capability must not receive the controller policy."""
    node = bare_device()

    attached = _attached_policy_names(node.node_thing_name)
    assert CONTROLLER_POLICY_NAME not in attached, \
        f"{CONTROLLER_POLICY_NAME} wrongly attached to a plain node; attached={sorted(attached)}"


def test_controller_reads_its_group_and_cannot_write(controller_group):
    """Signing with its own credentials, the controller reads its group and a node's config; a write is refused at the edge."""
    controller, node_a, node_b, group_id, _user = controller_group
    assert node_a.set_node_config({"device_type": "light_bulb", "firmware_version": "1.0"}), \
        "node_a's config report was not acknowledged"
    _connect_api(controller)

    response = controller.controller_api_request("GET", f"v1/groups/{group_id}")
    assert response.status_code == 200, f"controller group read failed: {response.status_code} {response.text}"
    body = response.json()
    assert body["access_type"] == "controller"
    assert {node_a.node_thing_name, node_b.node_thing_name} <= set(body.get("node_ids", []))

    config_path = f"v1/groups/{group_id}/nodes/{node_a.node_thing_name}/config"
    response = controller.controller_api_request("GET", config_path)
    assert response.status_code == 200, \
        f"controller could not read a node config in its own group: {response.status_code} {response.text}"
    assert response.json().get("device_type") == "light_bulb", response.text

    # The role's execute-api grant is GET-only, so API Gateway refuses the write before any Lambda runs.
    response = controller.controller_api_request("PUT", config_path, {"device_type": "hijacked"})
    assert response.status_code == 403, f"controller write was not refused: {response.status_code} {response.text}"


def test_controller_reads_a_node_shadow(controller_group):
    """The controller gets a node's params-<group> shadow over MQTT (spec §3.6 "Get params")."""
    controller, node_a, _b, group_id, _user = controller_group
    shadow_name = f"params-{group_id}"
    marker = f"itest-{uuid.uuid4()}"
    iot_data_client().update_thing_shadow(
        thingName=node_a.node_thing_name, shadowName=shadow_name,
        payload=json.dumps({"state": {"reported": {"_probe": marker}}}))

    accepted = Queue()
    get_topic = f"$aws/things/{node_a.node_thing_name}/shadow/name/{shadow_name}/get"
    controller.mqtt_connection.subscribe(
        topic=f"{get_topic}/accepted", qos=awscrt_mqtt.QoS.AT_LEAST_ONCE,
        callback=lambda topic, payload, **_: accepted.put(json.loads(payload)),
    )[0].result(timeout=10)
    controller.mqtt_connection.publish(topic=get_topic, payload="{}", qos=awscrt_mqtt.QoS.AT_LEAST_ONCE)[0].result(timeout=10)

    try:
        doc = accepted.get(timeout=10)
    except Empty:
        pytest.fail("controller received no shadow get/accepted")
    assert doc.get("state", {}).get("reported", {}).get("_probe") == marker


def test_controller_unicast_reaches_only_the_addressed_node(controller_group):
    """Unicast control is delivered to the target node alone."""
    controller, node_a, node_b, group_id, _user = controller_group

    topic_a = f"rainmaker/nodes/{node_a.node_thing_name}/user/params-{group_id}/params"
    topic_b = f"rainmaker/nodes/{node_b.node_thing_name}/user/params-{group_id}/params"
    assert node_a.subscribe(topic=topic_a), "node_a failed to subscribe to its unicast topic"
    assert node_b.subscribe(topic=topic_b), "node_b failed to subscribe to its unicast topic"
    node_a.clear_queues()
    node_b.clear_queues()

    payload = {"Switch": {"Power": True}}
    assert controller.publish_node_params(node_a.node_thing_name, group_id, payload), \
        "controller failed to publish unicast params to a node in its group"

    assert node_a.wait_for_params_message(timeout=10) == payload, \
        "target node did not receive the unicast control"
    assert node_b.wait_for_params_message(timeout=5) is None, \
        "a non-target node received the unicast control"


def test_controller_group_and_subgroup_control(controller_group):
    """Group control reaches every node; subgroup control reaches the subgroup topic only; the controller hears broadcasts sent by others."""
    controller, node_a, node_b, group_id, user = controller_group
    group_topic = f"rainmaker/nodes/groups/{group_id}/control"
    assert node_a.subscribe(topic=group_topic), "node_a failed to subscribe to the group control topic"
    assert node_b.subscribe(topic=group_topic), "node_b failed to subscribe to the group control topic"
    node_a.clear_queues()
    node_b.clear_queues()

    payload = {
        "esp.device.switch": {"params": {"Power": True}},
        "esp.device.light": {"params": {"Brightness": 75}},
    }
    assert controller.publish_group_control(group_id, payload), \
        "controller failed to publish to its group control topic"
    assert node_a.wait_for_params_message(timeout=10) == payload, "node_a did not receive the group control intact"
    assert node_b.wait_for_params_message(timeout=10) == payload, "node_b did not receive the group control intact"

    subgroup_id = Group(user).create_subgroup(group_id, "Controller Subgroup")
    Group(user).add_node_to_subgroup(group_id, subgroup_id, node_a.node_thing_name)
    assert node_a.subscribe(topic=f"{group_topic.rsplit('/', 1)[0]}/subgroups/{subgroup_id}/control"), \
        "node_a failed to subscribe to its subgroup control topic"
    node_a.clear_queues()
    node_b.clear_queues()
    sub_payload = {"esp.device.light": {"params": {"Power": False}}}
    assert controller.publish_group_control(group_id, sub_payload, subgroup_id=subgroup_id), \
        "controller failed to publish to a subgroup control topic"
    assert node_a.wait_for_params_message(timeout=10) == sub_payload, "subgroup node did not receive the subgroup control"
    assert node_b.wait_for_params_message(timeout=5) is None, "subgroup control leaked onto the group topic"

    assert controller.subscribe(topic=group_topic), "controller failed to subscribe to its group control topic"
    controller.clear_queues()
    broadcast = {"esp.device.switch": {"params": {"Power": False}}}
    iot_data_client().publish(topic=group_topic, qos=1, payload=json.dumps(broadcast))
    assert controller.wait_for_params_message(timeout=10) == broadcast, "controller did not receive a group broadcast"


def test_controller_cannot_reach_another_group(controller_group, other_group):
    """REST and every MQTT policy statement refuse another group: the RBAC checkpoint for reads, the broker for unicast, group control and shadow writes."""
    controller, _a, _b, _group_id, _user = controller_group
    other_group_id, other_node = other_group

    _connect_api(controller)
    response = controller.controller_api_request("GET", f"v1/groups/{other_group_id}")
    assert response.status_code == 404, \
        f"controller must not read a group it is not in; got {response.status_code} {response.text}"

    unicast = f"rainmaker/nodes/{other_node.node_thing_name}/user/params-{other_group_id}/params"
    control = f"rainmaker/nodes/groups/{other_group_id}/control"
    assert other_node.subscribe(topic=unicast), "other_node failed to subscribe to its unicast topic"
    assert other_node.subscribe(topic=control), "other_node failed to subscribe to its group control topic"
    other_node.clear_queues()

    # A denied publish can close the controller's session, so it reconnects before each attempt.
    controller.publish_node_params(other_node.node_thing_name, other_group_id, {"Switch": {"Power": True}})
    assert other_node.wait_for_params_message(timeout=8) is None, "controller reached a node outside its group"

    assert connect_device_with_retry(controller)
    controller.publish_group_control(other_group_id, {"esp.device.switch": {"params": {"Power": True}}})
    assert other_node.wait_for_params_message(timeout=8) is None, "controller reached another group's control topic"

    assert connect_device_with_retry(controller)
    marker = f"itest-{uuid.uuid4()}"
    _attempt_shadow_write(controller, other_node.node_thing_name, f"params-{other_group_id}", marker)
    assert _shadow_probe(other_node.node_thing_name, f"params-{other_group_id}") != marker, \
        "controller wrote another group's shadow"


def test_controller_removed_from_its_group_loses_access(controller_group):
    """REST access ends on the next request; MQTT access lasts on the open connection and ends once the controller reconnects, when the policy re-reads the cleared group_id attribute (spec §3.2)."""
    controller, node_a, _b, group_id, user = controller_group
    group_topic = f"rainmaker/nodes/groups/{group_id}/control"
    assert node_a.subscribe(topic=group_topic), "node_a failed to subscribe to the group control topic"
    Group(user).remove_node_from_group(group_id, controller.node_thing_name)

    _connect_api(controller)
    response = controller.controller_api_request("GET", f"v1/groups/{group_id}")
    assert response.status_code == 401, \
        f"a controller in no group must not read its old group; got {response.status_code} {response.text}"

    node_a.clear_queues()
    still_open = {"esp.device.switch": {"params": {"Power": False}}}
    assert controller.publish_group_control(group_id, still_open), "publish on the pre-removal connection failed"
    assert node_a.wait_for_params_message(timeout=10) == still_open, \
        "expected the pre-removal connection to keep its old group scope until reconnect"

    controller.disconnect()
    assert connect_device_with_retry(controller), "controller failed to reconnect after removal"
    node_a.clear_queues()
    controller.publish_group_control(group_id, {"esp.device.switch": {"params": {"Power": True}}})
    assert node_a.wait_for_params_message(timeout=8) is None, "a removed controller still controls its old group"

    # With group_id cleared, params-${group_id}* must not widen to every group's shadow.
    assert connect_device_with_retry(controller)
    marker = f"itest-{uuid.uuid4()}"
    _attempt_shadow_write(controller, node_a.node_thing_name, f"params-{group_id}", marker)
    assert _shadow_probe(node_a.node_thing_name, f"params-{group_id}") != marker, \
        "a controller in no group wrote a group node's shadow"


def test_node_that_is_both_bridge_and_controller(_bridge_stack_deployed, test_user1, bare_device):
    """Both register hooks run: the node carries both policies, is classified a bridge, adds a child and reads its group as a controller."""
    node = bare_device(capabilities=["bridge", "controller"])
    attached = _attached_policy_names(node.node_thing_name)
    assert {BRIDGE_POLICY_NAME, CONTROLLER_POLICY_NAME} <= attached, f"attached={sorted(attached)}"
    assert stored_node_type(node.node_thing_name) == "bridge"

    group_api = Group(test_user1)
    group_id = bridge_associate_and_ready(node, test_user1, group_api)
    try:
        child_id = f"{node.node_thing_name}--dual1"
        publish_add_child(node, "dual1", "0xDUAL001")
        deadline = time.time() + 15
        while time.time() < deadline and bridge_child_ids(node.node_thing_name) != [child_id]:
            time.sleep(0.5)
        assert bridge_child_ids(node.node_thing_name) == [child_id], "addChild did not create the child"

        _connect_api(node)
        response = node.controller_api_request("GET", f"v1/groups/{group_id}")
        assert response.status_code == 200, f"{response.status_code} {response.text}"
        assert response.json()["access_type"] == "controller"
    finally:
        cleanup_bridge_children(node.node_thing_name)
        group_api.delete_group(group_id, warn_error=True)
