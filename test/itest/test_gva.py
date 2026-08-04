# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import time

import pytest
import requests

# presence_event_handler waits PRESENCE_OFFLINE_DELAY (10s) after a disconnect
# before writing reported.online=false, then re-checks. A connectivity assertion
# has to outwait that write plus the notification round trip to the mock.
OFFLINE_PROPAGATION_WAIT = 15


def _read_gva_notification(base_url, api_key, user_sub):
    """Return the last GVA Report State the in-cloud mock received for a user."""
    response = requests.get(
        f"{base_url}/v1/gva/validate",
        params={"uuid": user_sub},
        headers={"x-api-key": api_key})
    assert response.status_code == 200, \
        f"Failed to read GVA notification for user {user_sub}: {response.text}"
    notification_data = response.json()
    assert notification_data is not None, \
        f"No GVA notification data for user {user_sub}"
    assert notification_data.get("gva") is True, \
        f"Not a GVA notification: {notification_data}"
    return notification_data["payload"]["devices"]["states"]


def _assert_gva_reported_online(base_url, api_key, user_sub, device_ids, expected_online):
    """Assert the mock received a Report State carrying expected_online.

    Retries once, since the report travels device -> shadow -> shadow_notify_rule
    -> notifications lambda -> mock and the last hop is not synchronous.
    """
    def check():
        states = _read_gva_notification(base_url, api_key, user_sub)
        for device_id in device_ids:
            assert device_id in states, \
                f"Device {device_id} not in GVA report state: {states}"
            assert states[device_id].get("online") is expected_online, \
                (f"Expected online={expected_online} for {device_id}, "
                 f"got {states[device_id].get('online')}")

    try:
        check()
    except AssertionError as e:
        print(f"GVA connectivity validation failed on first attempt: {e}")
        time.sleep(5)
        check()


def _make_service_account(project_id="test-gva-project-123", client_email="test@test.iam.gserviceaccount.com"):
    """Helper to build a test service account JSON."""
    return {
        "type": "service_account",
        "project_id": project_id,
        "private_key_id": "test-private-key-id",
        "private_key": "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----\n",
        "client_email": client_email,
        "client_id": "123456789",
        "auth_uri": "https://accounts.google.com/o/oauth2/auth",
        "token_uri": "https://oauth2.googleapis.com/token",
        "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
        "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/test",
        "universe_domain": "googleapis.com"
    }

@pytest.mark.xdist_group("gva_config")
def test_gva_post_then_get_configuration(super_admin_user):
    """Test POST then GET /v1/admin/integrations/gva/configuration to verify round-trip."""
    admin = super_admin_user
    admin.get_aws_credentials()

    sa = _make_service_account(project_id="test-gva-project-123", client_email="test@test-gva-project-123.iam.gserviceaccount.com")

    # POST configuration
    post_response = admin.gva_post_configuration(sa)
    assert post_response.status_code == 200, f"POST failed: {post_response.text}"

    # GET configuration and verify
    get_response = admin.gva_get_configuration()
    assert get_response.status_code == 200, f"GET failed: {get_response.text}"

    body = get_response.json()
    # M-13: the private key is write-only and must never come back via GET.
    assert 'private_key' not in body
    sa_expected = dict(sa)
    del sa_expected['private_key']
    sa_expected['redirect_uris'] = ['https://oauth-redirect.googleusercontent.com/r/test-gva-project-123']
    assert body == sa_expected


@pytest.mark.xdist_group("gva_config")
def test_gva_update_configuration(super_admin_user):
    """Test that POST with different values updates the configuration."""
    admin = super_admin_user
    admin.get_aws_credentials()

    # POST initial configuration
    sa1 = _make_service_account(project_id="first-gva-project", client_email="first@first.iam.gserviceaccount.com")
    post_response = admin.gva_post_configuration(sa1)
    assert post_response.status_code == 200, f"First POST failed: {post_response.text}"

    # POST updated configuration
    sa2 = _make_service_account(project_id="updated-gva-project", client_email="updated@updated.iam.gserviceaccount.com")
    post_response = admin.gva_post_configuration(sa2)
    assert post_response.status_code == 200, f"Second POST failed: {post_response.text}"

    # GET and verify values were updated
    get_response = admin.gva_get_configuration()
    assert get_response.status_code == 200, f"GET failed: {get_response.text}"

    body = get_response.json()
    assert 'private_key' not in body
    sa2_expected = dict(sa2)
    del sa2_expected['private_key']
    sa2_expected['redirect_uris'] = ['https://oauth-redirect.googleusercontent.com/r/updated-gva-project']
    assert body == sa2_expected

def test_gva_discovery(user_with_1_dev_each_in_2_groups):
    """Test GVA (Google Voice Assistant) device discovery."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    # Connect and subscribe to from_cloud topic for both devices
    assert device1.connect(), "Failed to connect device1 to MQTT"
    from_cloud_topic = f"rainmaker/nodes/{device1.node_thing_name}/from_cloud"
    assert device1.subscribe(topic=from_cloud_topic), "Failed to subscribe device1 to from_cloud topic"

    assert device2.connect(), "Failed to connect device2 to MQTT"
    from_cloud_topic2 = f"rainmaker/nodes/{device2.node_thing_name}/from_cloud"
    assert device2.subscribe(topic=from_cloud_topic2), "Failed to subscribe device2 to from_cloud topic"

    test_user1.get_aws_credentials()

    # Ignore initial messages arrived on MQTT topic
    message = device1.wait_for_cloud_message(timeout=2)
    message = device2.wait_for_cloud_message(timeout=2)

    discovery_response = test_user1.gva_discover_devices()
    print("GVA discovery_response is ", discovery_response)

    # Normalize dynamic fields
    discovery_response['requestId'] = 'mock_request_id'

    # Expected response (device order will be normalized automatically)
    expected = {
        'requestId': 'mock_request_id',
        'payload': {
            'agentUserId': test_user1.sub,
            'devices': [
                {
                    'id': device1.node_thing_name + '.Light1',
                    'type': 'action.devices.types.LIGHT',
                    'traits': ['action.devices.traits.OnOff', 'action.devices.traits.Brightness'],
                    'name': {'name': 'Light1'},
                    'willReportState': True,
                    'customData': {'groupID': group1_id, 'paramMap_Brightness': 'Brightness', 'paramMap_OnOff': 'Power'}
                },
                {
                    'id': device1.node_thing_name + '.Light2',
                    'type': 'action.devices.types.LIGHT',
                    'traits': ['action.devices.traits.OnOff'],
                    'name': {'name': 'Light2'},
                    'willReportState': True,
                    'customData': {'groupID': group1_id, 'paramMap_OnOff': 'Power'}
                },
                {
                    'id': device2.node_thing_name + '.Switch1',
                    'type': 'action.devices.types.SWITCH',
                    'traits': ['action.devices.traits.OnOff'],
                    'name': {'name': 'Switch1'},
                    'willReportState': True,
                    'deviceInfo': {'manufacturer': 'ESP32', 'model': 'RainMaker Device', 'swVersion': '1.1.0'},
                    'customData': {'groupID': group2_id, 'paramMap_OnOff': 'Power'}
                }
            ]
        }
    }

    # Normalize both objects for comparison (equivalent to test_utils.AssertNormalizedEqual)
    def normalize_for_comparison(obj):
        if isinstance(obj, dict):
            return {k: normalize_for_comparison(v) for k, v in obj.items()}
        elif isinstance(obj, list):
            return sorted([normalize_for_comparison(item) for item in obj], key=str)
        else:
            return obj

    assert normalize_for_comparison(discovery_response) == normalize_for_comparison(expected)

    # Verify both devices received the getGVAEn event
    message1 = device1.wait_for_cloud_message(timeout=5)
    assert message1 is not None, "Timeout waiting for getGVAEn message for device1"
    assert "event" in message1 and "getGVAEn" in message1["event"], "getGVAEn event not found in message for device1"
    assert message1["getGVAEn"]["enabled"] == True, "getGVAEn enabled not set to true for device1"

    message2 = device2.wait_for_cloud_message(timeout=5)
    assert message2 is not None, "Timeout waiting for getGVAEn message for device2"
    assert "event" in message2 and "getGVAEn" in message2["event"], "getGVAEn event not found in message for device2"
    assert message2["getGVAEn"]["enabled"] == True, "getGVAEn enabled not set to true for device2"

def test_gva_discovery_friendly_name(user_with_1_dev_each_in_2_groups):
    """Test that GVA sync uses esp.param.name from shadow as device name."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    # Connect and set up shadow with custom names
    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # The presence pipeline only writes offline, so the emulated device must publish online itself.
    device1.update_named_shadow(shadow_name, {"online": True})
    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": False, "Name": "Kitchen Light"},
        "Light2": {"Power": False, "Name": "Bedroom Light"}
    })

    test_user1.get_aws_credentials()

    discovery_response = test_user1.gva_discover_devices()
    discovery_response['requestId'] = 'mock_request_id'

    devices = discovery_response['payload']['devices']

    # Verify device names come from shadow esp.param.name values
    light1 = next(d for d in devices if d['id'] == device1.node_thing_name + '.Light1')
    assert light1['name']['name'] == 'Kitchen Light', f"expected 'Kitchen Light' but got '{light1['name']['name']}'"

    light2 = next(d for d in devices if d['id'] == device1.node_thing_name + '.Light2')
    assert light2['name']['name'] == 'Bedroom Light', f"expected 'Bedroom Light' but got '{light2['name']['name']}'"

def test_gva_query_and_control(user_with_1_dev_each_in_2_groups):
    """Test GVA device state query and control."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    custom_data = {
        "groupID": group1_id,
        "paramMap_OnOff": "Power",
        "paramMap_Brightness": "Brightness"
    }

    test_user1.get_aws_credentials()

    # First connect to MQTT and initialize shadow client
    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # The presence pipeline only writes offline, so the emulated device must publish online itself.
    device1.update_named_shadow(shadow_name, {"online": True})

    # GVA QUERY reports the device's real connectivity from reported.online.
    # Firmware publishes online=true on connect; the test device must do the same.
    device1.update_named_shadow(shadow_name, {"online": True})

    # Subscribe to the params topic
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe to params topic"



    # Set initial state through shadow
    device1.update_named_shadow(shadow_name, {"Light1": {"Power": False, "Brightness": 0}})

    def validate_gva_control(device_name, command, params, expected_state):
        """Validate GVA control command."""
        device_id = device1.node_thing_name + "." + device_name
        control_response = test_user1.gva_control_device(device_id, custom_data, command, params)

        # Overwrite dynamic field with mock value
        control_response['requestId'] = 'mock_request_id'
        # Build expected states based on command type
        if 'OnOff' in command:
            expected_states = {'on': params['on'], 'online': True}
        elif 'Brightness' in command:
            expected_states = {'brightness': params['brightness'], 'online': True}
        else:
            expected_states = {'online': True}

        expected = {
            'requestId': 'mock_request_id',
            'payload': {'commands': [{'ids': [device_id], 'status': 'SUCCESS', 'states': expected_states}]}
        }
        assert control_response == expected

        # Verify device state change
        message = device1.wait_for_params_message(timeout=5)
        assert message == {device_name: expected_state}

    def validate_gva_query(device_name, expected_state):
        """Validate GVA query command."""
        device_id = device1.node_thing_name + "." + device_name
        query_response = test_user1.gva_query_device(device_id, custom_data)

        # Overwrite dynamic field with mock value
        query_response['requestId'] = 'mock_request_id'
        expected = {
            'requestId': 'mock_request_id',
            'payload': {'devices': {device_id: {'online': True, 'status': 'SUCCESS', **expected_state}}}
        }
        assert query_response == expected

    # Test OnOff control
    validate_gva_control("Light1", "action.devices.commands.OnOff", {"on": True}, {"Power": True})
    validate_gva_control("Light1", "action.devices.commands.OnOff", {"on": False}, {"Power": False})

    # Test Brightness control
    validate_gva_control("Light1", "action.devices.commands.BrightnessAbsolute", {"brightness": 75}, {"Brightness": 75})

    # Test state queries
    device1.update_named_shadow(shadow_name, {"Light1": {"Power": True, "Brightness": 50}})
    validate_gva_query("Light1", {"on": True, "brightness": 50})

    device1.update_named_shadow(shadow_name, {"Light1": {"Power": False, "Brightness": 25}})
    validate_gva_query("Light1", {"on": False, "brightness": 25})

@pytest.mark.xdist_group("env_mut")
def test_gva_report_state(user_with_1_dev_each_in_2_groups, webhook_mock):
    """Test GVA report state (QUERY) after shadow updates.

    Similar to Alexa validate_report_state_directive.

    The proactive connectivity Report State at the end is dispatched by the
    notifications lambda; the webhook_mock fixture points that lambda at the
    in-cloud test mock (with its API key) so the report is captured there. The
    env_mut group serializes this against test_webhook_notification, which
    toggles the same lambda config.
    """
    webhook_mock_base_url, webhook_mock_api_key = webhook_mock
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    custom_data_light = {
        "groupID": group1_id,
        "paramMap_OnOff": "Power",
        "paramMap_Brightness": "Brightness"
    }
    custom_data_light2 = {
        "groupID": group1_id,
        "paramMap_OnOff": "Power",
    }

    test_user1.get_aws_credentials()

    # Connect to MQTT and setup shadow
    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # The presence pipeline only writes offline, so the emulated device must publish online itself.
    device1.update_named_shadow(shadow_name, {"online": True})

    # GVA QUERY reports the device's real connectivity from reported.online.
    # Firmware publishes online=true on connect; the test device must do the same.
    device1.update_named_shadow(shadow_name, {"online": True})

    # Set initial state
    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": False, "Brightness": 0},
        "Light2": {"Power": False},
    })

    def validate_report_state(device_name, custom_data, expected_state):
        """Validate GVA report state by querying device and checking expected properties."""
        device_id = device1.node_thing_name + "." + device_name
        query_response = test_user1.gva_query_device(device_id, custom_data)
        print(f"GVA ReportState response for {device_name}:", query_response)

        states = query_response['payload']['devices']
        device_state = states[device_id]
        assert device_state.get('status') == 'SUCCESS', \
            f"Query status not SUCCESS: {device_state}"
        assert device_state.get('online') is True, \
            f"Device not online: {device_state}"

        for key, value in expected_state.items():
            actual = device_state.get(key)
            assert actual == value, \
                f"Expected {key}={value}, got {actual}"

    # Turn on Light1 via control, then verify report state
    device_id = device1.node_thing_name + ".Light1"
    on_cmd = "action.devices.commands.OnOff"
    brightness_cmd = "action.devices.commands.BrightnessAbsolute"

    test_user1.gva_control_device(
        device_id, custom_data_light, on_cmd, {"on": True})
    device1.wait_for_params_message(timeout=5)

    device1.update_named_shadow(
        shadow_name, {"Light1": {"Power": True}})
    validate_report_state(
        "Light1", custom_data_light, {"on": True, "brightness": 0})
    validate_report_state(
        "Light2", custom_data_light2, {"on": False})

    # Turn off Light1
    test_user1.gva_control_device(
        device_id, custom_data_light, on_cmd, {"on": False})
    device1.wait_for_params_message(timeout=5)

    device1.update_named_shadow(
        shadow_name, {"Light1": {"Power": False}})
    validate_report_state(
        "Light1", custom_data_light, {"on": False})
    validate_report_state(
        "Light2", custom_data_light2, {"on": False})

    # Turn on Light2
    device_id2 = device1.node_thing_name + ".Light2"
    test_user1.gva_control_device(
        device_id2, custom_data_light2, on_cmd, {"on": True})
    device1.wait_for_params_message(timeout=5)

    device1.update_named_shadow(
        shadow_name, {"Light2": {"Power": True}})
    validate_report_state(
        "Light1", custom_data_light, {"on": False})
    validate_report_state(
        "Light2", custom_data_light2, {"on": True})

    # Set brightness on Light1
    test_user1.gva_control_device(
        device_id, custom_data_light, brightness_cmd,
        {"brightness": 50})
    device1.wait_for_params_message(timeout=5)

    device1.update_named_shadow(shadow_name, {"Light1": {"Brightness": 50}})
    validate_report_state("Light1", custom_data_light, {"brightness": 50})

    # Connectivity change must reach Google as a proactive Report State, not just
    # as a QUERY answer. A disconnect writes reported.online without moving
    # notify.version, so this covers the shadow_notify_rule online branch and the
    # connectivity-only dispatch guard: GVA opts in and must still be reported.
    device_ids = [device1.node_thing_name + ".Light1",
                  device1.node_thing_name + ".Light2"]

    # SYNC records the account link. Report State is only sent to linked users, so
    # without it the dispatch stops at "no GVA-linked users among the recipients".
    test_user1.gva_discover_devices()

    # Seed the notify map firmware maintains once an assistant is enabled. It has
    # to be in the shadow before the connectivity change, because the rule reads
    # notify out of reported params and a disconnect writes only online. This is
    # the lingering map the dispatch guard keys on: its version does not move, so
    # the event is connectivity-only and only opted-in services are dispatched.
    device1.update_named_shadow(shadow_name, {
        "notify": {"version": 1, "alexa": True, "gva": True},
    })
    time.sleep(2)

    device1.disconnect()
    time.sleep(OFFLINE_PROPAGATION_WAIT)
    _assert_gva_reported_online(webhook_mock_base_url, webhook_mock_api_key, test_user1.sub, device_ids, False)

    assert device1.connect(), "Device failed to reconnect"
    assert device1.shadow_connect([shadow_name]), "Failed to reconnect device shadow client"
    device1.update_named_shadow(shadow_name, {"online": True})
    time.sleep(5)
    _assert_gva_reported_online(webhook_mock_base_url, webhook_mock_api_key, test_user1.sub, device_ids, True)


def test_gva_disconnect(user_with_1_dev_each_in_2_groups):
    """Test GVA disconnect functionality."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    test_user1.get_aws_credentials()

    # Test disconnect
    disconnect_response = test_user1.gva_disconnect()

    # Overwrite dynamic field with mock value
    disconnect_response['requestId'] = 'mock_request_id'
    expected = {'requestId': 'mock_request_id', 'payload': {}}
    assert disconnect_response == expected

def test_gva_comprehensive_discovery(user_with_1_dev_each_in_2_groups):
    """
    Comprehensive GVA discovery test that mirrors Alexa discovery test structure.
    Tests device mapping, traits, attributes, and proper event handling.
    """
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    # Connect devices and subscribe to cloud messages
    assert device1.connect(), "Failed to connect device1 to MQTT"
    from_cloud_topic = f"rainmaker/nodes/{device1.node_thing_name}/from_cloud"
    assert device1.subscribe(topic=from_cloud_topic), "Failed to subscribe device1 to from_cloud topic"

    assert device2.connect(), "Failed to connect device2 to MQTT"
    from_cloud_topic2 = f"rainmaker/nodes/{device2.node_thing_name}/from_cloud"
    assert device2.subscribe(topic=from_cloud_topic2), "Failed to subscribe device2 to from_cloud topic"

    test_user1.get_aws_credentials()

    # Clear any initial messages
    message = device1.wait_for_cloud_message(timeout=2)
    message = device2.wait_for_cloud_message(timeout=2)

    # Perform discovery
    discovery_response = test_user1.gva_discover_devices()

    # Normalize dynamic fields
    discovery_response['requestId'] = 'mock_request_id'

    # Expected response (device order will be normalized automatically)
    expected = {
        'requestId': 'mock_request_id',
        'payload': {
            'agentUserId': test_user1.sub,
            'devices': [
                {
                    'id': device1.node_thing_name + '.Light1',
                    'type': 'action.devices.types.LIGHT',
                    'traits': ['action.devices.traits.OnOff', 'action.devices.traits.Brightness'],
                    'name': {'name': 'Light1'},
                    'willReportState': True,
                    'customData': {'groupID': group1_id, 'paramMap_Brightness': 'Brightness', 'paramMap_OnOff': 'Power'}
                },
                {
                    'id': device1.node_thing_name + '.Light2',
                    'type': 'action.devices.types.LIGHT',
                    'traits': ['action.devices.traits.OnOff'],
                    'name': {'name': 'Light2'},
                    'willReportState': True,
                    'customData': {'groupID': group1_id, 'paramMap_OnOff': 'Power'}
                },
                {
                    'id': device2.node_thing_name + '.Switch1',
                    'type': 'action.devices.types.SWITCH',
                    'traits': ['action.devices.traits.OnOff'],
                    'name': {'name': 'Switch1'},
                    'willReportState': True,
                    'deviceInfo': {'manufacturer': 'ESP32', 'model': 'RainMaker Device', 'swVersion': '1.1.0'},
                    'customData': {'groupID': group2_id, 'paramMap_OnOff': 'Power'}
                }
            ]
        }
    }

    # Normalize both objects for comparison (equivalent to test_utils.AssertNormalizedEqual)
    def normalize_for_comparison(obj):
        if isinstance(obj, dict):
            return {k: normalize_for_comparison(v) for k, v in obj.items()}
        elif isinstance(obj, list):
            return sorted([normalize_for_comparison(item) for item in obj], key=str)
        else:
            return obj

    assert normalize_for_comparison(discovery_response) == normalize_for_comparison(expected)

    message2 = device2.wait_for_cloud_message(timeout=5)
    assert message2 is not None and "getGVAEn" in message2.get("event", []) and message2["getGVAEn"]["enabled"] == True

def test_gva_comprehensive_control(user_with_1_dev_each_in_2_groups):
    """
    Comprehensive GVA control test that mirrors Alexa control test structure.
    Tests device control, state queries, and shadow synchronization.
    """
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    # Setup custom data for device control
    custom_data = {
        "groupID": group1_id,
        "paramMap_OnOff": "Power",
        "paramMap_Brightness": "Brightness"
    }

    test_user1.get_aws_credentials()

    # Connect to MQTT and setup shadow
    assert device1.connect(), "Failed to connect device1 to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect device1 to shadow"
    device1.update_named_shadow(shadow_name, {"online": True})

    # GVA QUERY reports the device's real connectivity from reported.online.
    # Firmware publishes online=true on connect; the test device must do the same.
    device1.update_named_shadow(shadow_name, {"online": True})

    # Subscribe to params topic for device control verification
    params_topic = f"rainmaker/nodes/{device1.node_thing_name}/user/params-{group1_id}/params"
    assert device1.subscribe(topic=params_topic), "Failed to subscribe device1 to params topic"

    # Set initial device state
    device1.update_named_shadow(shadow_name, {
        "Light1": {"Power": False, "Brightness": 0},
        "Light2": {"Power": False}
    })

    def execute_gva_control_command(device_name, command, params, expected_device_state):
        """Execute a GVA control command and verify the response and device state."""
        device_id = device1.node_thing_name + "." + device_name
        control_response = test_user1.gva_control_device(device_id, custom_data, command, params)

        # Overwrite dynamic field with mock value
        control_response['requestId'] = 'mock_request_id'
        # Build expected states based on command type
        if 'OnOff' in command:
            expected_states = {'on': params['on'], 'online': True}
        elif 'Brightness' in command:
            expected_states = {'brightness': params['brightness'], 'online': True}
        else:
            expected_states = {'online': True}

        expected = {
            'requestId': 'mock_request_id',
            'payload': {'commands': [{'ids': [device_id], 'status': 'SUCCESS', 'states': expected_states}]}
        }
        assert control_response == expected

        # Verify device state change
        params_message = device1.wait_for_params_message(timeout=5)
        assert params_message == {device_name: expected_device_state}

    def execute_gva_query_command(device_name, expected_state):
        """Execute a GVA query command and verify the response."""
        device_id = device1.node_thing_name + "." + device_name
        query_response = test_user1.gva_query_device(device_id, custom_data)

        # Overwrite dynamic field with mock value
        query_response['requestId'] = 'mock_request_id'
        expected = {
            'requestId': 'mock_request_id',
            'payload': {'devices': {device_id: {'online': True, 'status': 'SUCCESS', **expected_state}}}
        }
        assert query_response == expected

    # Clear any residual messages from previous tests
    try:
        device1.wait_for_params_message(timeout=0.5)
    except:
        pass  # No message to clear

    # Test OnOff control commands
    print("Testing GVA OnOff control commands...")
    execute_gva_control_command("Light1", "action.devices.commands.OnOff", {"on": True}, {"Power": True})
    execute_gva_control_command("Light1", "action.devices.commands.OnOff", {"on": False}, {"Power": False})
    execute_gva_control_command("Light2", "action.devices.commands.OnOff", {"on": True}, {"Power": True})

    # Test Brightness control commands
    print("Testing GVA Brightness control commands...")
    execute_gva_control_command("Light1", "action.devices.commands.BrightnessAbsolute", {"brightness": 75}, {"Brightness": 75})
    execute_gva_control_command("Light1", "action.devices.commands.BrightnessAbsolute", {"brightness": 25}, {"Brightness": 25})

    # Test state query commands with different shadow states
    print("Testing GVA state query commands...")

    # Set specific state and query
    device1.update_named_shadow(shadow_name, {"Light1": {"Power": True, "Brightness": 100}})
    execute_gva_query_command("Light1", {"on": True, "brightness": 100})

    # Change state and query again
    device1.update_named_shadow(shadow_name, {"Light1": {"Power": False, "Brightness": 50}})
    execute_gva_query_command("Light1", {"on": False, "brightness": 50})

    # Test Light2 state query
    device1.update_named_shadow(shadow_name, {"Light2": {"Power": True}})
    execute_gva_query_command("Light2", {"on": True})

    device1.update_named_shadow(shadow_name, {"Light2": {"Power": False}})
    execute_gva_query_command("Light2", {"on": False})

    print("GVA comprehensive control test completed successfully!")

def test_gva_error_handling(user_with_1_dev_each_in_2_groups):
    """Test GVA error handling for invalid commands and devices."""
    device1, device2, group1_id, group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    custom_data = {
        "groupID": group1_id,
        "paramMap_OnOff": "Power",
        "paramMap_Brightness": "Brightness"
    }

    test_user1.get_aws_credentials()

    # Test control command with invalid device ID
    invalid_device_id = "nonexistent.device"
    control_response = test_user1.gva_control_device(invalid_device_id, custom_data, "action.devices.commands.OnOff", {"on": True})

    # Overwrite dynamic field with mock value
    control_response['requestId'] = 'mock_request_id'
    expected = {
        'requestId': 'mock_request_id',
        'payload': {'commands': [{'ids': [invalid_device_id], 'status': 'ERROR', 'errorCode': 'unknownError'}]}
    }
    assert control_response == expected

    # Test query command with invalid device ID
    query_response = test_user1.gva_query_device(invalid_device_id, custom_data)

    # Overwrite dynamic field with mock value
    query_response['requestId'] = 'mock_request_id'
    expected = {
        'requestId': 'mock_request_id',
        'payload': {'devices': {invalid_device_id: {'status': 'ERROR', 'errorCode': 'deviceNotFound'}}}
    }
    assert query_response == expected

    print("GVA error handling test completed successfully!")
