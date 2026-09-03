# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""SmartThings integration tests.

Mirrors test_alexa.py / test_gva.py for parity. Two layers are covered:

1. Config API CRUD at /v1/admin/integrations/smartthings/configuration, a super-admin
   REST API driven through User.st_post_configuration and friends.

2. Schema App interactions. SmartThings invokes the st_action Lambda directly, not
   through API Gateway, so User.st_discover_devices / st_control_device / st_state_refresh
   build the Schema envelope and invoke that Lambda with boto3. The OAuth token
   SmartThings would send is the user's Cognito access token, which is what the Schema
   App validates (st_action.GetUserIDFromToken -> Cognito JWKS).

Run all: pytest test/itest/test_smartthings.py -v -s
"""
import os
import subprocess
import sys
import time

import pytest
import requests

from py_sdk.test_group import Group
from py_sdk.test_smartthings import cookie_for, st_external_device_id
from test.itest.conftest import REGION, accept_sharing_request_for, rmng_outputs


# ---------------------------------------------------------------------------
# SmartThings Schema App Lambda ARN lookup (mirrors conftest._get_alexa_region_arns).
#
# Outputs structure: rmng_outputs['rmng-st-core-<REGION>']['regions'][region]['STSchemaAppFunctionArn']
# ---------------------------------------------------------------------------
ST_STACK_REGIONS = ['us-east-1', 'eu-west-1', 'ap-northeast-1']


def _get_st_region_arns():
    """Return list of (region, arn) for the SmartThings stack from rmng_outputs."""
    st_key = f"rmng-st-core-{REGION}"
    st = rmng_outputs.get(st_key, {})
    regions = st.get("regions") or {}
    result = []
    for r in ST_STACK_REGIONS:
        if r not in regions:
            continue
        region_data = regions[r]
        arn = region_data.get("STSchemaAppFunctionArn") if isinstance(region_data, dict) else region_data
        if arn:
            result.append((r, arn))
    return result


ST_REGION_ARNS = _get_st_region_arns()


@pytest.fixture(
    params=ST_REGION_ARNS if ST_REGION_ARNS else [
        pytest.param((None, None), marks=pytest.mark.skip(reason="No rmng-st-core regions in rmng-outputs.json"))
    ],
    ids=[r for r, _ in ST_REGION_ARNS] if ST_REGION_ARNS else ["no-smartthings"],
)
def st_region_arn(request):
    """Parametrized fixture: yields (region, arn) for each SmartThings Schema App Lambda region."""
    return request.param


# ---------------------------------------------------------------------------
# 1. Config API CRUD (super-admin REST API)
# ---------------------------------------------------------------------------
@pytest.mark.xdist_group("smartthings_config")
def test_smartthings_configuration_round_trip(super_admin_user):
    """POST stores the credentials, GET returns client_id only, a second POST updates them,
    DELETE removes them."""
    admin = super_admin_user
    admin.get_aws_credentials()

    post_response = admin.st_post_configuration("test-st-client-id", "test-st-client-secret")
    assert post_response.status_code == 200, f"POST failed: {post_response.text}"

    get_response = admin.st_get_configuration()
    assert get_response.status_code == 200, f"GET failed: {get_response.text}"
    body = get_response.json()
    assert body['client_id'] == 'test-st-client-id'
    # Secret must never be returned by GET.
    assert 'client_secret' not in body, f"client_secret must be omitted from GET response: {body}"

    post_response = admin.st_post_configuration("updated-st-client-id", "updated-st-client-secret")
    assert post_response.status_code == 200, f"update POST failed: {post_response.text}"

    get_response = admin.st_get_configuration()
    assert get_response.status_code == 200, f"GET after update failed: {get_response.text}"
    assert get_response.json()['client_id'] == 'updated-st-client-id'

    delete_response = admin.st_delete_configuration()
    assert delete_response.status_code == 200, f"DELETE failed: {delete_response.text}"


def test_smartthings_config_non_admin_forbidden(test_user1):
    """A non-admin user must not be able to read/write the SmartThings config (403)."""
    test_user1.get_aws_credentials()

    post_response = test_user1.st_post_configuration("hacker-client-id", "hacker-client-secret")
    assert post_response.status_code == 403, f"expected 403 for non-admin POST, got {post_response.status_code}: {post_response.text}"

    get_response = test_user1.st_get_configuration()
    assert get_response.status_code == 403, f"expected 403 for non-admin GET, got {get_response.status_code}: {get_response.text}"


# ---------------------------------------------------------------------------
# 2. Schema App interactions (st_action Lambda, invoked directly by SmartThings)
# ---------------------------------------------------------------------------
def test_smartthings_discovery(user_with_1_dev_each_in_2_groups, st_region_arn):
    """discoveryRequest with a valid token lists the user's devices with the correct
    externalDeviceId and deviceHandlerType; both devices also receive getSTEn=true."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    # Connect and subscribe to from_cloud topic for both devices (to observe getSTEn).
    assert device1.connect(), "Failed to connect device1 to MQTT"
    from_cloud_topic = f"rainmaker/nodes/{device1.node_thing_name}/from_cloud"
    assert device1.subscribe(topic=from_cloud_topic), "Failed to subscribe device1 to from_cloud topic"

    assert device2.connect(), "Failed to connect device2 to MQTT"
    from_cloud_topic2 = f"rainmaker/nodes/{device2.node_thing_name}/from_cloud"
    assert device2.subscribe(topic=from_cloud_topic2), "Failed to subscribe device2 to from_cloud topic"

    test_user1.get_aws_credentials()

    # Ignore initial messages on the MQTT topic.
    time.sleep(2)
    device1.clear_queues()
    device2.clear_queues()

    response = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    print(f"SmartThings discovery response ({region}) is ", response)

    assert response['headers']['interactionType'] == 'discoveryResponse', f"region {region}: {response}"
    devices = response.get('devices') or []
    # Light1 (power+brightness) and Light2 (power) on node1, Switch1 (power) on node2.
    # Devices with only healthCheck (no controllable capability) are excluded by the handler.
    by_id = {d['externalDeviceId']: d for d in devices}

    light1_id = st_external_device_id(device1.node_thing_name, "Light1")
    light2_id = st_external_device_id(device1.node_thing_name, "Light2")
    switch1_id = st_external_device_id(device2.node_thing_name, "Switch1")

    assert light1_id in by_id, f"region {region}: Light1 missing from discovery {by_id.keys()}"
    assert light2_id in by_id, f"region {region}: Light2 missing from discovery {by_id.keys()}"
    assert switch1_id in by_id, f"region {region}: Switch1 missing from discovery {by_id.keys()}"

    # Light1 has power+brightness -> dimmer handler; Light2/Switch1 -> switch handler.
    assert by_id[light1_id]['deviceHandlerType'] == 'c2c-dimmer', f"region {region}: {by_id[light1_id]}"
    assert by_id[light2_id]['deviceHandlerType'] == 'c2c-switch', f"region {region}: {by_id[light2_id]}"
    assert by_id[switch1_id]['deviceHandlerType'] == 'c2c-switch', f"region {region}: {by_id[switch1_id]}"

    # Friendly names default to the device config id when no esp.param.name in shadow.
    assert by_id[light1_id]['friendlyName'] == 'Light1', f"region {region}: {by_id[light1_id]}"
    assert by_id[switch1_id]['friendlyName'] == 'Switch1', f"region {region}: {by_id[switch1_id]}"

    # Switch1's node sets fw_version -> manufacturerInfo populated.
    assert by_id[switch1_id].get('manufacturerInfo', {}).get('swVersion') == '1.1.0', f"region {region}: {by_id[switch1_id]}"

    # Both nodes should receive getSTEn enabled=true (mirrors Alexa getAlexaEn / GVA getGVAEn).
    message1 = device1.wait_for_cloud_message(timeout=5)
    assert message1 is not None, "Timeout waiting for getSTEn message for device1"
    assert "event" in message1 and "getSTEn" in message1["event"], "getSTEn event not found in message for device1"
    assert message1["getSTEn"]["enabled"] == True, "getSTEn enabled not set to true for device1"

    message2 = device2.wait_for_cloud_message(timeout=5)
    assert message2 is not None, "Timeout waiting for getSTEn message for device2"
    assert "event" in message2 and "getSTEn" in message2["event"], "getSTEn event not found in message for device2"
    assert message2["getSTEn"]["enabled"] == True, "getSTEn enabled not set to true for device2"

    # With esp.param.name set in the shadow, discovery uses it as the friendly name.
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device1.update_named_shadow(shadow_name, {
        "online": True,
        "Light1": {"Power": False, "Name": "Kitchen Light"},
        "Light2": {"Power": False, "Name": "Bedroom Light"},
    })
    time.sleep(2)

    by_id = {d['externalDeviceId']: d for d in (test_user1.st_discover_devices(lambda_arn=arn, region=region).get('devices') or [])}
    assert by_id[light1_id]['friendlyName'] == 'Kitchen Light', f"region {region}: {by_id[light1_id]}"
    assert by_id[light2_id]['friendlyName'] == 'Bedroom Light', f"region {region}: {by_id[light2_id]}"


def test_smartthings_command(user_with_1_dev_each_in_2_groups, st_region_arn):
    """commandRequest (st.switch on/off) reflects the commanded state in the
    commandResponse and the device receives the command via MQTT params."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()

    # Connect device1 and set up shadow + params subscription so we can verify control.
    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"

    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    # Initial state + a discovery so the node is marked online/ST-enabled.
    device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": False, "Brightness": 0}})
    discovery = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)
    device1.clear_queues()

    light1_id = st_external_device_id(device1.node_thing_name, "Light1")

    # SmartThings stores the cookie from discovery and sends it back with every
    # command, so the test does the same: this is the path production takes.
    light1 = next(d for d in (discovery.get('devices') or []) if d['externalDeviceId'] == light1_id)
    cookie = light1.get('deviceCookie')
    assert cookie and cookie.get('esp.param.power') == 'Power', \
        f"region {region}: discovery did not return a usable deviceCookie: {light1}"

    def validate_switch_command(command, expected_power):
        commands = [{"component": "main", "capability": "st.switch", "command": command, "arguments": []}]
        response = test_user1.st_control_device(light1_id, commands, device_cookie=cookie,
                                                lambda_arn=arn, region=region)
        print(f"SmartThings command response ({region}, {command}) is ", response)

        assert response['headers']['interactionType'] == 'commandResponse', f"region {region}: {response}"
        device_states = response.get('deviceState') or []
        assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
        ds = device_states[0]
        assert ds['externalDeviceId'] == light1_id, f"region {region}: {ds}"
        # No deviceError on success.
        assert not ds.get('deviceError'), f"region {region}: command returned error: {ds}"
        switch_states = [s for s in ds['states'] if s['capability'] == 'st.switch' and s['attribute'] == 'switch']
        assert switch_states, f"region {region}: no st.switch state in response: {ds}"
        assert switch_states[0]['value'] == ('on' if expected_power else 'off'), f"region {region}: {switch_states[0]}"

        # Device should receive the corresponding params command over MQTT.
        message = device1.wait_for_params_message(timeout=5)
        assert message == {"Light1": {"Power": expected_power}}, f"region {region}: device params mismatch: {message}"

    validate_switch_command("on", True)
    validate_switch_command("off", False)


def test_smartthings_state_refresh(user_with_1_dev_each_in_2_groups, st_region_arn):
    """stateRefreshRequest returns a stateRefreshResponse including st.healthCheck."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()

    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": True, "Brightness": 50}})

    # Discovery first so the node is registered as online/ST-enabled.
    test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)

    light1_id = st_external_device_id(device1.node_thing_name, "Light1")
    response = test_user1.st_state_refresh([light1_id], lambda_arn=arn, region=region)
    print(f"SmartThings state refresh response ({region}) is ", response)

    assert response['headers']['interactionType'] == 'stateRefreshResponse', f"region {region}: {response}"
    device_states = response.get('deviceState') or []
    assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
    ds = device_states[0]
    assert ds['externalDeviceId'] == light1_id, f"region {region}: {ds}"

    capabilities = {s['capability'] for s in ds['states']}
    assert 'st.healthCheck' in capabilities, f"region {region}: st.healthCheck missing from states: {ds}"

    health = [s for s in ds['states'] if s['capability'] == 'st.healthCheck']
    assert health and health[0]['attribute'] == 'healthStatus', f"region {region}: {ds}"
    assert health[0]['value'] in ('online', 'offline'), f"region {region}: {health[0]}"


# ---------------------------------------------------------------------------
# 4. Cross-user authorization
#
# The Schema App acts on the externalDeviceId carried in the request, so a second
# linked account must not be able to reach the first account's devices by supplying
# their id. Discovery is safe by construction (it only ever enumerates the caller's
# own groups); command and state refresh have to check explicitly.
# ---------------------------------------------------------------------------


@pytest.fixture
def victim_device_and_attacker(user_with_1_dev_each_in_2_groups, test_user2, st_region_arn):
    """user1 owns device1; user2 is a legitimate but unrelated account.

    Returns (region, arn, device1, group1_id, victim_device_id, attacker).
    """
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()
    test_user2.get_aws_credentials()

    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    # Known starting state, and a discovery so the node is ST-enabled/online.
    device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": False, "Brightness": 0}})
    test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)
    device1.clear_queues()

    victim_device_id = st_external_device_id(device1.node_thing_name, "Light1")
    return region, arn, device1, group1_id, victim_device_id, test_user2


def test_smartthings_command_rejects_foreign_device(victim_device_and_attacker):
    """user2's token must not actuate user1's device."""
    region, arn, device1, _group1_id, victim_device_id, attacker = victim_device_and_attacker

    commands = [{"component": "main", "capability": "st.switch", "command": "on", "arguments": []}]
    # Carrying a cookie must not help: it only names params, authorization still
    # comes from the caller's own groups.
    response = attacker.st_control_device(victim_device_id, commands,
                                          device_cookie={"esp.param.power": "Power"},
                                          lambda_arn=arn, region=region)
    print(f"cross-user commandRequest response ({region}) is ", response)

    # Collect the MQTT evidence BEFORE asserting on the response: whether the device
    # actually actuated is the fact that matters, and asserting the response first
    # would abort the test before we ever look.
    message = device1.wait_for_params_message(timeout=5)
    print(f"cross-user command: device params message ({region}) is ", message)

    device_states = response.get("deviceState") or []
    reported_error = bool(device_states and device_states[0].get("deviceError"))

    assert message is None, (
        f"region {region}: user1's device ACTUATED by user2's token, received over MQTT: {message}"
    )
    assert reported_error or not device_states, (
        f"region {region}: user2 commanded user1's device without error: {device_states}"
    )


def test_smartthings_state_refresh_rejects_foreign_device(victim_device_and_attacker):
    """user2's token must not read user1's device state."""
    region, arn, _device1, _group1_id, victim_device_id, attacker = victim_device_and_attacker

    response = attacker.st_state_refresh([victim_device_id], lambda_arn=arn, region=region)
    print(f"cross-user stateRefreshRequest response ({region}) is ", response)

    device_states = response.get("deviceState") or []
    if not device_states:
        return  # nothing returned is an acceptable rejection

    ds = device_states[0]
    if ds.get("deviceError"):
        return  # explicit rejection is what we want

    leaked = [s for s in (ds.get("states") or []) if s.get("capability") != "st.healthCheck"]
    assert not leaked, (
        f"region {region}: user1's device state leaked to user2: {leaked}"
    )


# ---------------------------------------------------------------------------
# 5. Shared-group access
#
# The authorization added for command/state refresh must reject strangers without
# also rejecting users the group was legitimately shared with. Sharing writes a
# user_group_mapping row for the invitee, so ListGroupForUser returns the group and
# the node resolves through the same path an owner takes.
# ---------------------------------------------------------------------------


def test_smartthings_command_allowed_for_shared_user(user_with_1_dev_each_in_2_groups, test_user2, st_region_arn):
    """A user the group was shared with can command its devices."""
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()
    test_user2.get_aws_credentials()

    user1_group_api = Group(test_user1)
    user1_group_api.share_group(group1_id, test_user2.username, "secondary")
    accept_sharing_request_for(test_user2, group1_id, "")

    try:
        assert device1.connect(), "Failed to connect device1 to MQTT"
        shadow_name = f"params-{group1_id}"
        assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
        params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
        assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

        device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": False, "Brightness": 0}})

        # The shared user's own discovery is what marks the node ST-enabled for them.
        discovery = test_user2.st_discover_devices(lambda_arn=arn, region=region)
        light1_id = st_external_device_id(device1.node_thing_name, "Light1")
        discovered = {d["externalDeviceId"] for d in (discovery.get("devices") or [])}
        assert light1_id in discovered, (
            f"region {region}: shared device missing from shared user's discovery: {discovered}"
        )

        time.sleep(2)
        device1.clear_queues()

        commands = [{"component": "main", "capability": "st.switch", "command": "on", "arguments": []}]
        response = test_user2.st_control_device(light1_id, commands,
                                               device_cookie=cookie_for(discovery, light1_id),
                                               lambda_arn=arn, region=region)
        print(f"shared-user commandRequest response ({region}) is ", response)

        message = device1.wait_for_params_message(timeout=5)
        print(f"shared-user command: device params message ({region}) is ", message)

        device_states = response.get("deviceState") or []
        assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
        assert not device_states[0].get("deviceError"), (
            f"region {region}: shared user was refused: {device_states[0]}"
        )
        assert message == {"Light1": {"Power": True}}, (
            f"region {region}: shared user's command did not reach the device: {message}"
        )
    finally:
        user1_group_api.unshare_group(group1_id, test_user2.user_id)


# ---------------------------------------------------------------------------
# 6. Capability coverage
#
# Only st.switch was exercised above. These drive the remaining command handlers
# (handle_command.go) and assert both the state SmartThings is told and the
# RainMaker params the device actually receives.
# ---------------------------------------------------------------------------


@pytest.fixture
def capability_device(user_with_multi_capability_device, st_region_arn):
    """Connect the multi-capability device, run discovery, and return the pieces
    every capability test needs: (region, arn, device, group_id, user, discovery)."""
    device, group_id, test_user1 = user_with_multi_capability_device
    region, arn = st_region_arn

    test_user1.get_aws_credentials()

    assert device.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group_id}"
    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"
    params_topic = f"rainmaker/nodes/{device.node_thing_name}/user/params-{group_id}/params"
    assert device.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    device.update_named_shadow(shadow_name, {
        "online": True,
        "RGBLight": {"Power": False, "Brightness": 0, "Hue": 0, "Saturation": 0},
        "CCTLight": {"Power": False, "Brightness": 0, "CCT": 2700},
        "Fan": {"Power": False, "Speed": 0},
        "Thermostat": {"Setpoint": 20},
    })

    discovery = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)
    device.clear_queues()

    return region, arn, device, group_id, test_user1, discovery


def test_smartthings_discovery_handler_types(capability_device):
    """Each device profile maps to the handler type SmartThings needs to render controls."""
    region, _arn, device, _group_id, _user, discovery = capability_device

    by_id = {d["externalDeviceId"]: d for d in (discovery.get("devices") or [])}
    expected = {
        "RGBLight": "c2c-rgb-color-bulb",
        "CCTLight": "c2c-color-temperature-bulb",
        "Fan": "c2c-fan",
        "Thermostat": "c2c-thermostat",
    }
    for device_name, handler in expected.items():
        eid = st_external_device_id(device.node_thing_name, device_name)
        assert eid in by_id, f"region {region}: {device_name} missing from discovery: {list(by_id)}"
        assert by_id[eid]["deviceHandlerType"] == handler, (
            f"region {region}: {device_name} handler {by_id[eid]['deviceHandlerType']} != {handler}"
        )


def _run_capability_command(capability_device, device_name, commands, expected_params):
    """Send commands to one device and assert the params the device receives."""
    region, arn, device, _group_id, user, _discovery = capability_device

    eid = st_external_device_id(device.node_thing_name, device_name)
    response = user.st_control_device(eid, commands, device_cookie=cookie_for(_discovery, eid),
                                      lambda_arn=arn, region=region)
    print(f"capability commandRequest ({region}, {device_name}) is ", response)

    message = device.wait_for_params_message(timeout=5)
    print(f"capability command params ({region}, {device_name}) is ", message)

    device_states = response.get("deviceState") or []
    assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
    assert not device_states[0].get("deviceError"), (
        f"region {region}: {device_name} command failed: {device_states[0]}"
    )
    assert message == {device_name: expected_params}, (
        f"region {region}: {device_name} params mismatch: {message} != {{{device_name}: {expected_params}}}"
    )
    return device_states[0]


def test_smartthings_command_switch_level(capability_device):
    """st.switchLevel setLevel maps to the brightness param."""
    ds = _run_capability_command(
        capability_device, "RGBLight",
        [{"component": "main", "capability": "st.switchLevel", "command": "setLevel", "arguments": [60]}],
        {"Brightness": 60},
    )
    levels = [s for s in ds["states"] if s["capability"] == "st.switchLevel"]
    assert levels and levels[0]["value"] == 60, f"unexpected level state: {ds}"


def test_smartthings_command_color_control(capability_device):
    """st.colorControl setColor maps hue and saturation together."""
    ds = _run_capability_command(
        capability_device, "RGBLight",
        [{"component": "main", "capability": "st.colorControl", "command": "setColor",
          "arguments": [{"hue": 120, "saturation": 80}]}],
        {"Hue": 120, "Saturation": 80},
    )
    attrs = {s["attribute"] for s in ds["states"] if s["capability"] == "st.colorControl"}
    assert {"hue", "saturation"} <= attrs, f"missing colour attributes: {ds}"


def test_smartthings_command_color_temperature(capability_device):
    """st.colorTemperature setColorTemperature maps to the cct param."""
    ds = _run_capability_command(
        capability_device, "CCTLight",
        [{"component": "main", "capability": "st.colorTemperature",
          "command": "setColorTemperature", "arguments": [4000]}],
        {"CCT": 4000},
    )
    cct = [s for s in ds["states"] if s["capability"] == "st.colorTemperature"]
    assert cct and cct[0]["value"] == 4000, f"unexpected cct state: {ds}"


def test_smartthings_command_fan_speed(capability_device):
    """st.fanSpeed setFanSpeed maps to the speed param."""
    ds = _run_capability_command(
        capability_device, "Fan",
        [{"component": "main", "capability": "st.fanSpeed", "command": "setFanSpeed", "arguments": [3]}],
        {"Speed": 3},
    )
    speed = [s for s in ds["states"] if s["capability"] == "st.fanSpeed"]
    assert speed and speed[0]["value"] == 3, f"unexpected fan speed state: {ds}"


def test_smartthings_command_thermostat_setpoint(capability_device):
    """st.thermostatHeatingSetpoint maps to the setpoint-temperature param."""
    ds = _run_capability_command(
        capability_device, "Thermostat",
        [{"component": "main", "capability": "st.thermostatHeatingSetpoint",
          "command": "setHeatingSetpoint", "arguments": [23]}],
        {"Setpoint": 23},
    )
    sp = [s for s in ds["states"] if s["capability"] == "st.thermostatHeatingSetpoint"]
    assert sp, f"missing heating setpoint state: {ds}"


# ---------------------------------------------------------------------------
# 7. Offline, multi-device and multi-command handling
#
# The loops over request.Devices and device.Commands, and the isNodeOnline branch
# in handle_command.go, have only ever seen the single online device case.
# ---------------------------------------------------------------------------


def test_smartthings_command_offline_device(user_with_1_dev_each_in_2_groups, st_region_arn):
    """A command for a disconnected node reports OFFLINE rather than pretending to succeed."""
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()
    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"

    # Discovery while still online, so the node is known and ST-enabled.
    discovery = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    light1_id = st_external_device_id(device1.node_thing_name, "Light1")

    # Now take it offline and wait for the presence handler to write reported.online=false.
    # Poll st.healthCheck rather than sleeping a fixed time: the disconnect event is
    # asynchronous, and this also fails loudly if presence never updates at all.
    device1.disconnect()
    deadline = time.time() + 90
    health = None
    while time.time() < deadline:
        refresh = test_user1.st_state_refresh([light1_id], lambda_arn=arn, region=region)
        states = (refresh.get("deviceState") or [{}])[0].get("states") or []
        health = next((s["value"] for s in states if s["capability"] == "st.healthCheck"), None)
        if health == "offline":
            break
        time.sleep(5)
    assert health == "offline", (
        f"region {region}: node never reported offline within 90s of disconnect (last: {health})"
    )

    commands = [{"component": "main", "capability": "st.switch", "command": "on", "arguments": []}]
    response = test_user1.st_control_device(light1_id, commands,
                                            device_cookie=cookie_for(discovery, light1_id),
                                            lambda_arn=arn, region=region)
    print(f"offline commandRequest response ({region}) is ", response)

    device_states = response.get("deviceState") or []
    assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
    errors = device_states[0].get("deviceError") or []
    assert errors, f"region {region}: offline device reported no error: {device_states[0]}"
    assert errors[0]["errorEnum"] == "DEVICE-ERROR", f"region {region}: {errors[0]}"
    assert errors[0]["detail"] == "OFFLINE", f"region {region}: {errors[0]}"


def test_smartthings_command_multiple_devices_are_independent(user_with_1_dev_each_in_2_groups, st_region_arn):
    """One bad device in a request must not stop the others being actuated."""
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()
    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": False, "Brightness": 0}})
    discovery = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)
    device1.clear_queues()

    good_id = st_external_device_id(device1.node_thing_name, "Light1")
    unknown_id = st_external_device_id("test-rsa-device-does-not-exist", "Light1")
    switch_on = [{"component": "main", "capability": "st.switch", "command": "on", "arguments": []}]

    response = test_user1.st_control_devices([
        {"externalDeviceId": unknown_id, "commands": switch_on},
        {"externalDeviceId": good_id, "commands": switch_on,
         "deviceCookie": cookie_for(discovery, good_id)},
    ], lambda_arn=arn, region=region)
    print(f"multi-device commandRequest response ({region}) is ", response)

    message = device1.wait_for_params_message(timeout=5)
    print(f"multi-device command params ({region}) is ", message)

    by_id = {ds["externalDeviceId"]: ds for ds in (response.get("deviceState") or [])}
    assert len(by_id) == 2, f"region {region}: expected a state per device, got {response}"
    assert by_id[unknown_id].get("deviceError"), f"region {region}: unknown device not rejected: {by_id[unknown_id]}"
    assert not by_id[good_id].get("deviceError"), f"region {region}: valid device refused: {by_id[good_id]}"
    assert message == {"Light1": {"Power": True}}, (
        f"region {region}: valid device did not actuate alongside a bad one: {message}"
    )


def test_smartthings_command_multiple_commands_one_device(user_with_1_dev_each_in_2_groups, st_region_arn):
    """Several commands for one device all apply and all come back as states."""
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = st_region_arn

    test_user1.get_aws_credentials()
    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    device1.update_named_shadow(shadow_name, {"online": True, "Light1": {"Power": False, "Brightness": 0}})
    discovery = test_user1.st_discover_devices(lambda_arn=arn, region=region)
    time.sleep(2)
    device1.clear_queues()

    light1_id = st_external_device_id(device1.node_thing_name, "Light1")
    response = test_user1.st_control_device(light1_id, [
        {"component": "main", "capability": "st.switch", "command": "on", "arguments": []},
        {"component": "main", "capability": "st.switchLevel", "command": "setLevel", "arguments": [50]},
    ], device_cookie=cookie_for(discovery, light1_id), lambda_arn=arn, region=region)
    print(f"multi-command commandRequest response ({region}) is ", response)

    # Each command publishes separately, so collect both messages.
    received = {}
    for _ in range(2):
        message = device1.wait_for_params_message(timeout=5)
        if message is None:
            break
        received.update(message.get("Light1", {}))
    print(f"multi-command params ({region}) is ", received)

    device_states = response.get("deviceState") or []
    assert len(device_states) == 1, f"region {region}: expected 1 deviceState, got {response}"
    assert not device_states[0].get("deviceError"), f"region {region}: {device_states[0]}"

    capabilities = {s["capability"] for s in device_states[0]["states"]}
    assert {"st.switch", "st.switchLevel"} <= capabilities, (
        f"region {region}: both commands should return states, got {capabilities}"
    )
    assert received == {"Power": True, "Brightness": 50}, (
        f"region {region}: device did not receive both commands: {received}"
    )


# ---------------------------------------------------------------------------
# 6. Proactive state callback (device -> SmartThings)
#
# The other tests here all drive SmartThings -> connector. This one covers the
# reverse direction: a device param change reaching SmartThings as a
# stateCallback, dispatched by the notifications Lambda.
#
# Unlike Alexa/GVA, the SmartThings adapter does not implement
# NotifyOnConnectivityChange, so it is only dispatched when notify.version moves
# — a param change, not a bare online/offline flip. The test therefore bumps
# notify.version rather than disconnecting the device.
# ---------------------------------------------------------------------------
def _read_st_notification(base_url, api_key, callback_token):
    """Return the last SmartThings stateCallback the in-cloud mock captured.

    Keyed by the callback access token, since a stateCallback envelope names no
    user: the callback URL plus bearer token are what identify the recipient.
    """
    response = requests.get(
        f"{base_url}/v1/smartthings/validate",
        params={"uuid": callback_token},
        headers={"x-api-key": api_key})
    assert response.status_code == 200, \
        f"Failed to read SmartThings notification for token {callback_token}: {response.text}"
    payload = response.json()
    assert payload is not None, f"No SmartThings notification for token {callback_token}"
    assert payload.get("smartthings") is True, f"Not a SmartThings notification: {payload}"
    return payload


def _assert_st_reported(base_url, api_key, callback_token, device_id, expected_states):
    """Assert the mock captured a stateCallback carrying expected_states for device_id.

    Retries, since the callback travels device -> shadow -> shadow_notify_rule ->
    notifications lambda -> mock and the last hop is not synchronous.
    """
    def check():
        payload = _read_st_notification(base_url, api_key, callback_token)
        assert payload["headers"]["interactionType"] == "stateCallback", \
            f"Not a stateCallback: {payload}"

        device_states = payload.get("deviceState") or []
        match = [ds for ds in device_states if ds.get("externalDeviceId") == device_id]
        assert match, f"Device {device_id} not in stateCallback: {payload}"

        states = {(s["capability"], s["attribute"]): s["value"] for s in match[0]["states"]}
        for key, value in expected_states.items():
            assert states.get(key) == value, \
                f"Expected {key}={value}, got {states.get(key)} in {states}"

    last_error = None
    for _ in range(3):
        try:
            check()
            return
        except AssertionError as e:
            last_error = e
            print(f"SmartThings stateCallback validation retrying: {e}")
            time.sleep(5)
    raise last_error



@pytest.fixture
def st_action_test_mode(webhook_mock):
    """Point the regional Schema App Lambda at the in-cloud mock for one test.

    grantCallbackAccess runs in rmng-st-action, not in rmng-notifications, so the
    webhook_mock fixture's env patch does not reach it. Without this the handler
    reads /rmng/smartthings/{client_id,client_secret} from SSM and fails outright on
    an account where SmartThings was never configured. drx.py takes its region from
    AWS_REGION, hence the per-region env for the subprocess.
    """
    base_url, api_key = webhook_mock
    if not ST_REGION_ARNS:
        pytest.skip("No rmng-st-core regions in rmng-outputs.json")
    region, arn = ST_REGION_ARNS[0]
    fn = arn.rsplit(":", 1)[-1]

    def _set(url_value, key_value):
        # Both vars matter: the base URL is what makes the handler skip the SSM
        # credential read, and the key is what MakeHTTPPostRequest attaches as
        # x-api-key -- every /v1 method on the test-infra API is api_key_required,
        # so the token POST is answered with 403 without it.
        env = {**os.environ, "AWS_REGION": region}
        subprocess.run(
            [sys.executable, "tools/drx.py", "update-env", fn,
             f"webhook_mock_base_url={url_value}",
             f"webhook_mock_api_key={key_value}"],
            check=True, env=env)

    _set(base_url, api_key)
    try:
        yield base_url
    finally:
        _set("", "")


@pytest.mark.xdist_group("env_mut")
def test_smartthings_state_callback(user_with_1_dev_each_in_2_groups, webhook_mock, st_action_test_mode):
    """A device param change is reported to SmartThings as a proactive stateCallback.

    The webhook_mock fixture points rmng-notifications at the in-cloud mock, which
    is where the adapter's test mode routes state callbacks. The env_mut group
    serialises this against the other tests that toggle the same Lambda config.

    Only the first ST region is exercised: the dispatch path under test lives in
    the notifications Lambda, which is regional-independent, and the fixture
    mutates shared Lambda env.
    """
    webhook_mock_base_url, webhook_mock_api_key = webhook_mock
    if not ST_REGION_ARNS:
        pytest.skip("No rmng-st-core regions in rmng-outputs.json")
    region, arn = ST_REGION_ARNS[0]

    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    test_user1.get_aws_credentials()

    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device1.update_named_shadow(shadow_name, {
        "online": True,
        "Light1": {"Power": False, "Brightness": 0},
    })

    # Discovery registers the node as ST-enabled, the same precondition the
    # command and state-refresh tests rely on.
    test_user1.st_discover_devices(lambda_arn=arn, region=region)

    # Link callback tokens: without a stored row SendTo skips the user entirely.
    # The mock echoes the code back as the access token, so the token the state
    # callback will carry — and thus the capture key — is predictable.
    callback_token = f"st-cb-{test_user1.sub}"
    test_user1.st_grant_callback(
        callback_token,
        f"{webhook_mock_base_url}/v1/smartthings/token",
        f"{webhook_mock_base_url}/v1/smartthings/data",
        lambda_arn=arn, region=region)

    # notify names the services to dispatch; a bumped version marks the event as
    # a param change rather than connectivity-only, which is the only kind the ST
    # adapter is dispatched for.
    light1_id = st_external_device_id(device1.node_thing_name, "Light1")

    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": True, "Brightness": 75},
        "notify": {"version": 1, "smartthings": True},
    })
    _assert_st_reported(
        webhook_mock_base_url, webhook_mock_api_key, callback_token, light1_id,
        {("st.switch", "switch"): "on", ("st.switchLevel", "level"): 75})

    # A second change must be reported too, not just the first: the version has
    # to move again for the event to count as a param change.
    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": False},
        "notify": {"version": 2, "smartthings": True},
    })
    _assert_st_reported(
        webhook_mock_base_url, webhook_mock_api_key, callback_token, light1_id,
        {("st.switch", "switch"): "off"})
