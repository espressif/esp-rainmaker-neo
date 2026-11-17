# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import json
import time

import boto3
import pytest

from test.infra.stacks.test_constants import ITEST_LWA_CLIENT_ID
from test.itest.conftest import ALEXA_REGION_ARNS, REGION, mark_itest_endpoints

AUTO_CONFIGURATION_PATH = '/v1/admin/integrations/alexa/auto-configuration'
# Well-formed but made up: real LWA refuses them, and the webhook mock accepts anything.
PLACEHOLDER_LWA = {
    'lwa_client_id': ITEST_LWA_CLIENT_ID,
    'lwa_client_secret': 'itest-placeholder',
    'lwa_refresh_token': 'Atzr|itest-placeholder',
}


def _post_auto_configuration(user, body):
    user.get_aws_credentials()
    return user.make_api_request('POST', AUTO_CONFIGURATION_PATH, data=json.dumps(body))


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
    # Skip only for an automatic setup that has not reached DONE; a deployment configured any other way must report everything below.
    setup_status = body['config_status']['status'] if body.get('config_status') else 'DONE'
    if setup_status != 'DONE':
        pytest.skip(f"Alexa automatic setup is {setup_status} on this deployment: {body['config_status']}")
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
    assert message1["getAlexaEn"]["enabled"] is True, "getAlexaEn enabled not set to true for device1"

    message2 = device2.wait_for_cloud_message(timeout=5)
    assert message2 is not None, "Timeout waiting for getAlexaEn message for device2"
    assert "event" in message2 and "getAlexaEn" in message2["event"], "getAlexaEn event not found in message for device2"
    assert message2["getAlexaEn"]["enabled"] is True, "getAlexaEn enabled not set to true for device2"


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


def test_alexa_rejects_a_token_for_another_api(test_user1, alexa_region_arn):
    """The skill accepts only tokens addressed to neo (RFC 9068 s4), so the same person's ESP User API token discovers nothing and the directive fails."""
    region, arn = alexa_region_arn
    signin = test_user1.signin()
    assert signin.status_code == 200, signin.text
    test_user1.alexa_set_lambda_arn(arn)

    accepted = test_user1.alexa_discover_devices(region=region)
    assert accepted.get("event", {}).get("header", {}).get("name") == "Discover.Response", \
        f"region {region}: the neo token must discover: {accepted}"

    refused = test_user1.alexa_discover_devices(region=region, token=test_user1.user_api_token())
    assert "event" not in refused, f"region {region}: a token addressed to the ESP User API must not discover: {refused}"
    assert refused.get("errorMessage") == "failed to get identity id", f"region {region}: {refused}"


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


# ---------------------------------------------------------------------------
# Rename. Alexa learns friendlyName from a discovery response only, exactly like
# SmartThings: a ChangeReport carries capability properties and no name. The
# adapter already emits AddOrUpdateReport when a node joins a group
# (marshalGroupMembership), but nothing fires it on a rename, so the name in the
# Alexa app goes stale. This test documents that gap; it is expected to fail
# until the rename path is wired up the way SmartThings' discoveryCallback is.
# ---------------------------------------------------------------------------
@pytest.mark.xfail(reason="Alexa has no rename path: no AddOrUpdateReport is emitted for esp.param.name",
                   strict=False)
def test_alexa_rename_emits_add_or_update_report(user_with_1_dev_each_in_2_groups, webhook_mock):
    """Renaming a device should reach Alexa as an AddOrUpdateReport."""
    import time

    import requests

    webhook_mock_base_url, webhook_mock_api_key = webhook_mock
    device1, _device2, group1_id, _group2_id, test_user1 = user_with_1_dev_each_in_2_groups

    # A fake Alexa link of the test's own, delivered to the mock.
    test_user1.register_client("alexa", json.dumps({
        "access_token": "test_access_token",
        "refresh_token": "test_refresh_token",
        "expires_at": int(time.time()) + 3600,
    }))
    mark_itest_endpoints(test_user1, "alexa")

    test_user1.get_aws_credentials()
    assert device1.connect(), "Failed to connect to MQTT"
    shadow_name = f"params-{group1_id}"
    assert device1.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device1.update_named_shadow(shadow_name, {
        "online": True,
        "Light1": {"Power": False, "Brightness": 0, "Name": "Light1"},
    })

    device1.update_named_shadow(shadow_name, {
        "Light1": {"Name": "Reading Lamp"},
        "notify": {"version": 12, "alexa": True},
    })

    def check():
        response = requests.get(
            f"{webhook_mock_base_url}/v1/alexa/validate",
            params={"uuid": test_user1.sub},
            headers={"x-api-key": webhook_mock_api_key})
        assert response.status_code == 200, \
            f"Failed to read Alexa notification: {response.text}"
        payload = response.json()
        assert payload is not None, "No Alexa notification captured"

        header = payload.get("event", {}).get("header", {})
        assert header.get("name") == "AddOrUpdateReport", \
            f"Expected AddOrUpdateReport after a rename, got {header.get('name')}"

        endpoints = payload["event"]["payload"].get("endpoints") or []
        names = [e.get("friendlyName") for e in endpoints]
        assert "Reading Lamp" in names, f"Renamed device not in AddOrUpdateReport: {names}"

    last_error = None
    for _ in range(3):
        try:
            check()
            return
        except AssertionError as e:
            last_error = e
            time.sleep(5)
    raise last_error


def test_alexa_auto_configuration_is_admin_only(test_user2):
    response = _post_auto_configuration(test_user2, {'skill_name': 'ESP RainMaker'})
    assert response.status_code == 403, f"expected 403, got {response.status_code}: {response.text}"
    # Disconnecting is the destructive one: a non-admin must not be able to drop the deployment's Amazon account.
    response = test_user2.alexa_delete_lwa_connection()
    assert response.status_code == 403, f"expected 403, got {response.status_code}: {response.text}"


def _wait_for_auto_configuration(admin, skill_id, timeout_s=960):
    """Poll config_status until the run for skill_id ends; 960s outlasts the worker's 900s Lambda cap."""
    deadline = time.monotonic() + timeout_s
    status = {}
    while time.monotonic() < deadline:
        response = admin.alexa_get_configuration()
        if response.status_code == 200 and response.json().get('config_status'):
            status = response.json()['config_status']
            if status.get('ref_id') == skill_id and status.get('status') in ('DONE', 'FAILED'):
                return status
        time.sleep(5)
    pytest.fail(f"auto-configuration of {skill_id} did not finish in {timeout_s}s; last status: {status}")


ALEXA_SSM_CREDENTIALS = ('/rmng/alexa/client_id', '/rmng/alexa/client_secret', '/rmng/alexa/skill_id')
ALEXA_SSM_STATUS = '/rmng/alexa/auto-configuration-status'
ALEXA_SSM_LWA_CONNECTION = '/rmng/alexa/lwa-connection'
# The mock answers SMAPI's vendor lookup with this id, so every redirect URI a mock run registers ends with it.
MOCK_VENDOR_LINK_SUFFIX = '/link/MOCKVENDOR'


def _snapshot_alexa_config(admin):
    """The deployment's live manual-setup inputs, or None when Alexa is not configured. The secret is never returned by the API, so it is read from SSM."""
    response = admin.alexa_get_configuration()
    body = response.json() if response.status_code == 200 else {}
    if not body.get('client_id'):
        return None
    secret = boto3.client('ssm', region_name=REGION).get_parameter(
        Name='/rmng/alexa/client_secret', WithDecryption=True)['Parameter']['Value']
    return {'redirect_uris': body.get('redirect_uris') or [], 'client_id': body['client_id'],
            'client_secret': secret, 'skill_id': body['skill_id']}


def _restore_alexa_config(admin, snapshot):
    """Put back what the run replaced. A manual POST re-grants and re-stores the old skill and clears the status; with nothing to restore, remove what the run added."""
    if snapshot:
        response = admin.alexa_post_configuration(**snapshot)
        assert response.status_code == 200, f"restore failed: {response.status_code} {response.text}"
        return
    for region, arn in ALEXA_REGION_ARNS:
        lambda_client = boto3.client('lambda', region_name=region)
        try:
            lambda_client.remove_permission(FunctionName=arn, StatementId='AlexaSkillInvoke')
        except lambda_client.exceptions.ResourceNotFoundException:
            pass
    boto3.client('ssm', region_name=REGION).delete_parameters(Names=[*ALEXA_SSM_CREDENTIALS, ALEXA_SSM_STATUS])


def _snapshot_lwa_connection():
    ssm = boto3.client('ssm', region_name=REGION)
    try:
        return ssm.get_parameter(Name=ALEXA_SSM_LWA_CONNECTION, WithDecryption=True)['Parameter']['Value']
    except ssm.exceptions.ParameterNotFound:
        return None


def _restore_lwa_connection(value):
    ssm = boto3.client('ssm', region_name=REGION)
    if value is None:
        try:
            ssm.delete_parameter(Name=ALEXA_SSM_LWA_CONNECTION)
        except ssm.exceptions.ParameterNotFound:
            pass
        return
    ssm.put_parameter(Name=ALEXA_SSM_LWA_CONNECTION, Value=value, Type='SecureString', Overwrite=True)


def _remove_mock_redirect_uris(admin):
    """Drop the mock vendor's account-linking URIs from every OAuth client. The configuration API only ever adds redirect URIs to a client, so a restore cannot take back what a mock run added."""
    listed = admin.list_oauth_clients()
    assert listed.status_code == 200, listed.text
    for client in listed.json()['clients']:
        uris = client.get('redirect_uris') or []
        kept = [uri for uri in uris if not uri.endswith(MOCK_VENDOR_LINK_SUFFIX)]
        if kept == uris:
            continue
        # PUT replaces every mutable field, so the rest of the client is sent back as read.
        body = {k: client.get(k) for k in ('client_name', 'post_logout_redirect_uris', 'grant_types', 'scopes',
                                           'allowed_resources', 'allowed_providers', 'require_pkce', 'first_party')}
        response = admin.put_oauth_client(client['client_id'], {**body, 'redirect_uris': kept})
        assert response.status_code == 200, f"{client['client_id']}: {response.status_code} {response.text}"


@pytest.mark.xdist_group("env_mut")
def test_alexa_auto_configuration_end_to_end(admin_user, webhook_mock):
    """Creates and configures a skill through the deployed endpoint and worker, with LWA and SMAPI answered by the itest webhook mock, so nothing at Amazon is created or changed. The deployment's previous Alexa configuration is restored afterwards."""
    if not ALEXA_REGION_ARNS:
        pytest.skip("No rmng-alexa-core regions in rmng-outputs.json")
    snapshot = _snapshot_alexa_config(admin_user)
    lwa_snapshot = _snapshot_lwa_connection()
    try:
        # Connect the way the dashboard does: the code consent returned, redeemed by the backend (here, by the mock).
        response = admin_user.alexa_put_lwa_connection(
            PLACEHOLDER_LWA['lwa_client_id'], PLACEHOLDER_LWA['lwa_client_secret'],
            lwa_authorization_code='itest-code', lwa_redirect_uri='https://dashboard.example.com/alexa-lwa-callback')
        assert response.status_code == 200, f"connect failed: {response.status_code} {response.text}"
        assert admin_user.alexa_get_configuration().json().get('lwa_client_id') == PLACEHOLDER_LWA['lwa_client_id']

        response = _post_auto_configuration(admin_user, {'skill_name': 'ESP RainMaker itest'})
        assert response.status_code == 200, f"POST failed: {response.status_code} {response.text}"
        skill_id = response.json()['skill_id']
        assert skill_id.startswith('amzn1.ask.skill.'), response.text
        assert response.json()['status'] == 'IN_PROGRESS'

        status = _wait_for_auto_configuration(admin_user, skill_id)
        assert status['status'] == 'DONE', f"auto-configuration failed: {status}"
        body = admin_user.alexa_get_configuration().json()
        assert body.get('skill_id') == skill_id
        assert body.get('client_id') == 'amzn1.application-oa2-client.itest-mock'

        # Activation must leave exactly the live grant for this skill, and no setup-only grant behind.
        for region, arn in ALEXA_REGION_ARNS:
            policy = json.loads(boto3.client('lambda', region_name=region).get_policy(FunctionName=arn)['Policy'])
            live = next((st for st in policy['Statement'] if st.get('Sid') == 'AlexaSkillInvoke'), None)
            assert live is not None, f"{region}: AlexaSkillInvoke missing"
            assert live['Condition']['StringEquals']['lambda:EventSourceToken'] == skill_id, f"{region}: live grant is not {skill_id}"
            leftovers = [st['Sid'] for st in policy['Statement'] if st.get('Sid', '').startswith('AlexaSkillSetup-')]
            assert not leftovers, f"{region}: setup grants left behind: {leftovers}"

        response = admin_user.alexa_delete_lwa_connection()
        assert response.status_code == 200, f"disconnect failed: {response.status_code} {response.text}"
        assert 'lwa_client_id' not in admin_user.alexa_get_configuration().json()
    finally:
        _restore_alexa_config(admin_user, snapshot)
        _restore_lwa_connection(lwa_snapshot)
        _remove_mock_redirect_uris(admin_user)
