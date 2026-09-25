# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from test.itest.conftest import alexa_region_arn
import json
import boto3
import pytest

def test_alexa_configuration_is_readable(admin_user, alexa_region_arn):
    """GET returns the configuration without the secret, and the skill Lambda's trigger
    matches the configured skill id.

    Read-only deliberately: a write here overwrites the deployment's real credentials and
    nothing can put them back, since the secret is never returned. alexa_cfg's unit specs
    cover the write path. The Lambda-policy check asserts deployed state rather than a POST
    side effect, which is the part only an itest can prove.
    """
    admin = admin_user
    admin.get_aws_credentials()

    response = admin.alexa_get_configuration()
    if response.status_code == 404:
        pytest.skip("Alexa is not configured on this deployment")
    assert response.status_code == 200, f"GET failed: {response.text}"

    body = response.json()
    assert body.get('client_id'), f"configured deployment must report a client_id: {body}"
    assert 'client_secret' not in body, f"client_secret must be omitted from GET: {body}"
    assert body.get('redirect_uris'), f"configured deployment must report redirect_uris: {body}"
    assert body.get('manufacturer_name'), f"manufacturer_name is never empty: {body}"

    skill_id = body.get('skill_id')
    assert skill_id, f"configured deployment must report a skill_id: {body}"

    # The config API grants the skill Lambda its invoke permission, scoping it to the
    # skill id. A deployment whose policy has drifted from the stored id links but never
    # receives a directive, which nothing else here would catch.
    region, alexa_skill_arn = alexa_region_arn
    lambda_client = boto3.client('lambda', region_name=region)
    policy = json.loads(lambda_client.get_policy(FunctionName=alexa_skill_arn)['Policy'])
    statement = next((s for s in policy['Statement'] if s.get('Sid') == 'AlexaSkillInvoke'), None)
    assert statement is not None, "AlexaSkillInvoke permission not found in Lambda policy"
    assert statement['Condition']['StringEquals']['lambda:EventSourceToken'] == skill_id, \
        "the skill Lambda's event source token does not match the configured skill_id"


def test_alexa_discovery(user_with_1_dev_each_in_2_groups, alexa_region_arn):
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = alexa_region_arn

    # Connect and subscribe to from_cloud topic for both devices
    assert device1.connect(), "Failed to connect device1 to MQTT"
    from_cloud_topic = f"rainmaker/nodes/{device1.node_thing_name}/from_cloud"
    assert device1.subscribe(topic=from_cloud_topic), "Failed to subscribe device1 to from_cloud topic"

    assert device2.connect(), "Failed to connect device2 to MQTT"
    from_cloud_topic2 = f"rainmaker/nodes/{device2.node_thing_name}/from_cloud"
    assert device2.subscribe(topic=from_cloud_topic2), "Failed to subscribe device2 to from_cloud topic"

    test_user1.get_aws_credentials()
    test_user1.alexa_set_lambda_arn(arn)

    # Ignore initial messages arrived on MQTT topic
    import time
    time.sleep(2)
    device1.clear_queues()
    device2.clear_queues()

    discovery_response = test_user1.alexa_discover_devices(region=region)
    print(f"discovery_response ({region}) is ", discovery_response)
    assert discovery_response['event']['header']['namespace'] == 'Alexa.Discovery', f"region {region}"
    assert discovery_response['event']['header']['name'] == 'Discover.Response', f"region {region}"

    endpoints = discovery_response['event']['payload']['endpoints']
    assert len(endpoints) == 3, f"region {region}: expected 3 endpoints"

    # Verify Light1 device capabilities
    light_endpoint = next(ep for ep in endpoints if ep['friendlyName'] == 'Light1')
    assert light_endpoint['endpointId'] == device1.node_thing_name + "#Light1", f"region {region}"
    assert light_endpoint['displayCategories'] == ['LIGHT'], f"region {region}"
    capabilities = {cap['interface'] for cap in light_endpoint['capabilities']}
    for capability in ['Alexa.PowerController', 'Alexa.BrightnessController', 'Alexa.EndpointHealth', 'Alexa']:
        assert capability in capabilities, f"region {region}"

    # Verify Light2 device capabilities
    light2_endpoint = next(ep for ep in endpoints if ep['friendlyName'] == 'Light2')
    assert light2_endpoint['endpointId'] == device1.node_thing_name + "#Light2", f"region {region}"
    assert light2_endpoint['displayCategories'] == ['LIGHT'], f"region {region}"
    capabilities = {cap['interface'] for cap in light2_endpoint['capabilities']}
    for capability in ['Alexa.PowerController', 'Alexa.EndpointHealth', 'Alexa']:
        assert capability in capabilities, f"region {region}"

    # Verify Switch device capabilities
    switch_endpoint = next(ep for ep in endpoints if ep['friendlyName'] == 'Switch1')
    assert switch_endpoint['endpointId'] == device2.node_thing_name + "#Switch1", f"region {region}"
    assert switch_endpoint['displayCategories'] == ['SWITCH'], f"region {region}"
    capabilities = {cap['interface'] for cap in switch_endpoint['capabilities']}
    for capability in ['Alexa.PowerController', 'Alexa.EndpointHealth', 'Alexa']:
        assert capability in capabilities, f"region {region}"
    assert switch_endpoint['additionalAttributes']['firmwareVersion'] == "1.1.0", f"region {region}"

    # WWA review rejects a placeholder manufacturer, so every endpoint must advertise a real
    # brand in both places. The brand itself is deployment configuration that the config tests
    # may be mid-flight on another xdist worker, so assert its shape rather than a fixed value.
    for endpoint in endpoints:
        manufacturer = endpoint['manufacturerName']
        assert manufacturer, f"region {region}: empty manufacturerName on {endpoint['endpointId']}"
        assert manufacturer not in ('TEST', 'Something'), \
            f"region {region}: placeholder manufacturerName {manufacturer!r}"
        assert endpoint['additionalAttributes']['manufacturer'] == manufacturer, \
            f"region {region}: additionalAttributes.manufacturer disagrees with manufacturerName"
        assert endpoint['description'], f"region {region}: empty description"

    # Verify both devices received the getAlexaEn event
    message1 = device1.wait_for_cloud_message(timeout=5)
    assert message1 is not None, "Timeout waiting for getAlexaEn message for device1"
    assert "event" in message1 and "getAlexaEn" in message1["event"], "getAlexaEn event not found in message for device1"
    assert message1["getAlexaEn"]["enabled"] == True, "getAlexaEn enabled not set to true for device1"

    message2 = device2.wait_for_cloud_message(timeout=5)
    assert message2 is not None, "Timeout waiting for getAlexaEn message for device2"
    assert "event" in message2 and "getAlexaEn" in message2["event"], "getAlexaEn event not found in message for device2"
    assert message2["getAlexaEn"]["enabled"] == True, "getAlexaEn enabled not set to true for device2"


def test_alexa_discovery_friendly_name(user_with_1_dev_each_in_2_groups, alexa_region_arn):
    """Test that Alexa discovery uses esp.param.name from shadow as friendly name."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = alexa_region_arn

    # Connect and set up shadow with custom names
    assert device1.connect(), "Failed to connect device1 to MQTT"
    from_cloud_topic = f"rainmaker/nodes/{device1.node_thing_name}/from_cloud"
    assert device1.subscribe(topic=from_cloud_topic), "Failed to subscribe device1 to from_cloud topic"

    assert device2.connect(), "Failed to connect device2 to MQTT"
    from_cloud_topic2 = f"rainmaker/nodes/{device2.node_thing_name}/from_cloud"
    assert device2.subscribe(topic=from_cloud_topic2), "Failed to subscribe device2 to from_cloud topic"

    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # The presence pipeline only writes offline, so the emulated device must publish online itself.
    device1.update_named_shadow(shadow_name, {"online": True})
    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": False, "Name": "Kitchen Light"},
        "Light2": {"Power": False, "Name": "Bedroom Light"}
    })

    test_user1.get_aws_credentials()
    test_user1.alexa_set_lambda_arn(arn)

    import time
    time.sleep(2)
    device1.clear_queues()
    device2.clear_queues()

    discovery_response = test_user1.alexa_discover_devices(region=region)
    endpoints = discovery_response['event']['payload']['endpoints']

    # Verify friendly names come from shadow esp.param.name values
    light1 = next(ep for ep in endpoints if ep['endpointId'] == device1.node_thing_name + "#Light1")
    assert light1['friendlyName'] == 'Kitchen Light', f"region {region}: expected 'Kitchen Light' but got '{light1['friendlyName']}'"

    light2 = next(ep for ep in endpoints if ep['endpointId'] == device1.node_thing_name + "#Light2")
    assert light2['friendlyName'] == 'Bedroom Light', f"region {region}: expected 'Bedroom Light' but got '{light2['friendlyName']}'"


def test_alexa_control(user_with_1_dev_each_in_2_groups, alexa_region_arn):
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups
    region, arn = alexa_region_arn

    cookie = {
        "groupID": group1_id,
        "paramMap_PowerController": "Power",
        "paramMap_BrightnessController": "Brightness"
    }

    test_user1.get_aws_credentials()
    test_user1.alexa_set_lambda_arn(arn)

    # First connect to MQTT and initialize shadow client
    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # The presence pipeline only writes offline, so the emulated device must publish online itself.
    device1.update_named_shadow(shadow_name, {"online": True})

    # Subscribe to the params topic
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    # Set initial state through shadow
    device1.update_named_shadow(shadow_name, {"Light1": {"Power": False, "Brightness": 0}, "Light2": {"Power": False}})

    def validate_control_directive(device_name, capability, action, payload=None, expected=None):
        expected = {device_name: expected}
        test_user1.alexa_control_device(device1.node_thing_name + "#" + device_name, capability, action, cookie, payload, region=region)
        message = device1.wait_for_params_message(timeout=5)
        print(f"Message after control directive ({region}):", message)
        assert message is not None, f"region {region}: timeout waiting for params message after {action}"
        assert message == expected, f"region {region}: message should match expected state: {expected}"

    def validate_report_state_directive(device_name, expected_properties=None):
        response = test_user1.alexa_control_device(device1.node_thing_name + "#" + device_name, "Alexa", "ReportState", cookie, region=region)
        print(f"ReportState response ({region}):", response)
        for expected_prop in expected_properties:
            found = False
            for prop in response['context']['properties']:
                if all(str(prop.get(key)) == str(expected_prop[key]) for key in expected_prop):
                    found = True
                    break
            assert found, f"region {region}: expected property not found: {expected_prop}"

    # TurnOn on a light dimmed to 0 also restores brightness, so Alexa turning it "on" is visible.
    validate_control_directive("Light1", "Alexa.PowerController", "TurnOn", expected={"Power": True, "Brightness": 100})
    validate_control_directive("Light1", "Alexa.PowerController", "TurnOff", expected={"Power": False})
    validate_control_directive("Light2", "Alexa.PowerController", "TurnOn", expected={"Power": True})
    # Brightness is coupled with power: any positive brightness implies on, 0 implies off.
    validate_control_directive("Light1", "Alexa.BrightnessController", "SetBrightness", payload={"brightness": 50}, expected={"Brightness": 50, "Power": True})

    device1.update_named_shadow(shadow_name, {"Light1": {"Power": True}})
    device1.update_named_shadow(shadow_name, {"Light2": {"Power": False}})
    # State reads reflect the reported shadow; the simulated device never reported Brightness
    # back, so it still reads the seeded 0 regardless of the SetBrightness command above.
    validate_report_state_directive("Light1", [{"name": "powerState", "value": "ON"}, {"name": "brightness", "value": 0}])
    validate_report_state_directive("Light2", [{"name": "powerState", "value": "OFF"}])

    device1.update_named_shadow(shadow_name, {"Light1": {"Power": False}})
    validate_report_state_directive("Light1", [{"name": "powerState", "value": "OFF"}])
    validate_report_state_directive("Light2", [{"name": "powerState", "value": "OFF"}])

    device1.update_named_shadow(shadow_name, {"Light2": {"Power": True}})
    validate_report_state_directive("Light1", [{"name": "powerState", "value": "OFF"}])
    validate_report_state_directive("Light2", [{"name": "powerState", "value": "ON"}])

    device1.update_named_shadow(shadow_name, {"Light1": {"Brightness": 50}})
    validate_report_state_directive("Light1", [{"name": "brightness", "value": 50}])
