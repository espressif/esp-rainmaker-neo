# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

import pytest
import base64
import json
import time
import subprocess
import sys
import uuid
import os
import boto3
import requests
import tempfile
from py_sdk.test_device import Device, generate_key_and_cert
from py_sdk.test_group import Group
from test.itest.conftest import accept_sharing_request_for, REGION, rmng_outputs, connect_device_with_retry, CA_CERT, IOT_ENDPOINT, DEBUG, FILES_BUCKET_NAME

@pytest.mark.xdist_group("env_mut")
def test_webhook_notification(test_user2, test_user3, test_user4, associated_device, webhook_mock):
    # base_url is used in the validate URLs; api_key gates every /v1 method, so the
    # test sends it on its own validate GETs (the notifications Lambda gets it via env).
    webhook_mock_base_url, webhook_mock_api_key = webhook_mock

    # Set dummy Alexa client details in SSM if they don't exist
    ssm = boto3.client('ssm', region_name=REGION)

    # Helper function to set parameter if it doesn't exist
    def set_ssm_if_not_exists(name, value):
        try:
            ssm.get_parameter(Name=name, WithDecryption=True)
        except ssm.exceptions.ParameterNotFound:
            ssm.put_parameter(
                Name=name,
                Value=value,
                Type='String'
            )

    # Set client ID and secret only if they don't exist
    set_ssm_if_not_exists('/rmng/alexa/client_id', 'dummy_client_id')
    set_ssm_if_not_exists('/rmng/alexa/client_secret', 'dummy_client_secret')

    device, group_id, test_user1, user1_group_api = associated_device
    assert device.connect(), "Failed to connect the device"
    device.set_node_config({
        "devices": [{
            "id": "Light",
            "type": "esp.device.lightbulb",
            "params": [
                {"id": "Power", "type": "esp.param.power"},
                {"id": "Brightness", "type": "esp.param.brightness"}
            ]
        }]
    })

    # Register all users with webhook mock credentials
    webhook_creds = {
        "refresh_token": "test_refresh_token",
        "access_token": "test_access_token",
        "expires_at": int(time.time()) - (3600 * 24 * 30)  # 1 month before now, so force to refresh
    }

    test_users = [test_user1, test_user2, test_user3, test_user4]
    webhook_endpoint_ids = {}  # user.sub -> endpoint_id returned by register_client
    for user in test_users:
        webhook_endpoint_ids[user.sub] = user.register_client("webhook_mock", json.dumps(webhook_creds))
        # For now, share creds between webhook and alexa platforms
        user.register_client("alexa", json.dumps(webhook_creds))
        user.register_client("gva", json.dumps(webhook_creds))

    # Share the group with all users
    for user in test_users[1:]:  # Skip test_user1 as they already own the group
        user1_group_api.share_group(group_id, user.username, "secondary")
        accept_sharing_request_for(user, group_id, "")

    # Connect the device and set up shadow
    shadow_name = f"params-{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        for subgroup_id in sorted(device.subgroup_ids):
            shadow_name += f"-{subgroup_id}"

    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device.update_named_shadow(shadow_name, {"online": True})

    # Helper function to validate webhook notifications for all users
    def validate_webhook_notif_once(expected_state, expected_version=None):
        time.sleep(5)  # Give some time for the notification to be processed

        for user in test_users:
            # The mock keys each delivery by uuid = "<user.sub>#<endpoint_id>".
            # Pass uuid via params so the '#' is percent-encoded; a literal '#' in
            # the URL is treated as a fragment and dropped before the server sees it.
            uuid = f"{user.sub}#{webhook_endpoint_ids[user.sub]}"
            validate_url = f"{webhook_mock_base_url}/v1/validate"
            response = requests.get(validate_url, params={"uuid": uuid}, headers={"x-api-key": webhook_mock_api_key})
            assert response.status_code == 200, f"Failed to validate notification for user {user.sub}: {response.text}"

            # Parse the response
            notification_data = response.json()
            print(f"🔍 Notification data: {notification_data}")
            assert notification_data is not None, f"No notification data received for user {user.sub}"

            # Verify the notification content
            assert "data" in notification_data, f"Notification data missing 'data' field for user {user.sub}"
            assert notification_data["data"]["node_id"] == device.node_thing_name, f"Incorrect node_id in notification for user {user.sub}"
            assert notification_data["data"]["topic_name"] == shadow_name, f"Incorrect topic_name in notification for user {user.sub}"

            # Verify expected state
            for device_name, params in expected_state.items():
                for param_name, param_value in params.items():
                    assert notification_data["data"]["state"]["params"][device_name][param_name] == param_value, \
                        f"Incorrect {param_name} state in notification for user {user.sub}: " \
                        f"expected {param_value}, got {notification_data['data']['state']['params'][device_name][param_name]}"

            # If version is provided, verify it
            if expected_version is not None:
                assert notification_data["data"]["state"]["params"]["notify"]["version"] == expected_version, \
                    f"Incorrect notification version for user {user.sub}: " \
                    f"expected {expected_version}, got {notification_data['data']['state']['params']['notify']['version']}"

    # Helper function to validate alexa notifications
    def validate_alexa_notif_once(changed_properties=None, unchanged_properties=None):
        if changed_properties is None:
            changed_properties = {}
        if unchanged_properties is None:
            unchanged_properties = {}

        for user in test_users:
            validate_url = f"{webhook_mock_base_url}/v1/alexa/validate?uuid={user.sub}"
            response = requests.get(validate_url, headers={"x-api-key": webhook_mock_api_key})
            assert response.status_code == 200, f"Failed to validate notification for user {user.sub}: {response.text}"

            notification_data = response.json()
            assert notification_data is not None, f"No notification data received for user {user.sub}"

            # Overwrite variable fields for consistent comparison
            # Handle context properties
            for i in range(len(notification_data["context"]["properties"])):
                if "timeOfSample" in notification_data["context"]["properties"][i]:
                    notification_data["context"]["properties"][i]["timeOfSample"] = "test_overwritten_time"

            # Handle event properties
            for i in range(len(notification_data["event"]["payload"]["change"]["properties"])):
                if "timeOfSample" in notification_data["event"]["payload"]["change"]["properties"][i]:
                    notification_data["event"]["payload"]["change"]["properties"][i]["timeOfSample"] = "test_overwritten_time"

            notification_data["event"]["endpoint"]["scope"]["token"] = "test_access_token_overwritten"
            notification_data["event"]["header"]["messageId"] = "message_id_overwritten"

            # Build expected change properties
            change_properties = []
            for namespace, props in changed_properties.items():
                for prop_name, prop_value in props.items():
                    change_properties.append({
                        "namespace": namespace,
                        "name": prop_name,
                        "value": prop_value,
                        "timeOfSample": "test_overwritten_time",
                        "uncertaintyInMilliseconds": 0
                    })

            # Build expected context properties
            context_properties = []
            for namespace, props in unchanged_properties.items():
                for prop_name, prop_value in props.items():
                    context_properties.append({
                        "namespace": namespace,
                        "name": prop_name,
                        "value": prop_value,
                        "timeOfSample": "test_overwritten_time",
                        "uncertaintyInMilliseconds": 0
                    })

            # Always add connectivity to context properties
            context_properties.append({
                "namespace": "Alexa.EndpointHealth",
                "name": "connectivity",
                "value": {
                    "value": "OK"
                },
                "timeOfSample": "test_overwritten_time",
                "uncertaintyInMilliseconds": 0
            })

            # Build expected data structure
            expected_data = {
                "event": {
                    "header": {
                        "namespace": "Alexa",
                        "name": "ChangeReport",
                        "payloadVersion": "3",
                        "messageId": "message_id_overwritten"
                    },
                    "endpoint": {
                        "endpointId": device.node_thing_name + "#Light",
                        "scope": {
                            "type": "BearerToken",
                            "token": "test_access_token_overwritten"
                        }
                    },
                    "payload": {
                        "change": {
                            "cause": {
                                "type": "PHYSICAL_INTERACTION"
                            },
                            "properties": change_properties
                        }
                    }
                },
                "context": {
                    "properties": context_properties
                },
                "alexa": True
            }

            # Compare with informative error message if they don't match
            if notification_data != expected_data:
                import json
                import pytest
                pytest.fail(f"Incorrect alexa notification data for user {user.sub}:\n" +
                          f"ACTUAL:\n{json.dumps(notification_data, indent=2)}\n\n" +
                          f"EXPECTED:\n{json.dumps(expected_data, indent=2)}")


    # Helper function to validate GVA notifications
    def validate_gva_notif_once(expected_device_states):
        """Validate GVA report state notifications for all users.

        Args:
            expected_device_states: dict mapping device IDs
                (e.g. "nodeId.Light") to expected state dicts
                (e.g. {"on": True, "brightness": 80}).
        """
        for user in test_users:
            validate_url = (
                f"{webhook_mock_base_url}/v1/gva/validate?uuid={user.sub}")
            response = requests.get(validate_url, headers={"x-api-key": webhook_mock_api_key})
            assert response.status_code == 200, (
                f"Failed to validate GVA notification "
                f"for user {user.sub}: {response.text}")

            notification_data = response.json()
            assert notification_data is not None, (
                f"No GVA notification data for user "
                f"{user.sub}")

            # Verify it's GVA data
            assert notification_data.get("gva") is True, (
                f"Not a GVA notification: {notification_data}")

            # Verify report state structure
            assert "payload" in notification_data, (
                f"Missing payload in GVA notification")
            payload = notification_data["payload"]
            assert "devices" in payload, (
                f"Missing devices in GVA payload")
            states = payload["devices"]["states"]

            for dev_id, expected in expected_device_states.items():
                assert dev_id in states, (
                    f"Device {dev_id} not in GVA states "
                    f"for user {user.sub}")
                dev_state = states[dev_id]
                for key, value in expected.items():
                    assert dev_state.get(key) == value, (
                        f"GVA state mismatch for "
                        f"{dev_id}.{key}: "
                        f"expected {value}, "
                        f"got {dev_state.get(key)}")

    def validate_webhook_notifications(expected_state, expected_version=None):
        # Try validation with retry logic
        try:
            validate_webhook_notif_once(expected_state, expected_version)
        except (AssertionError, Exception) as e:
            print(f"Webhook validation failed on first attempt: {e}")
            print("Waiting 5 seconds before retry...")
            time.sleep(5)
            validate_webhook_notif_once(expected_state, expected_version)  # Retry once

    def validate_alexa_notifications(changed_properties=None, unchanged_properties=None):
        # Try validation with retry logic
        try:
            validate_alexa_notif_once(changed_properties, unchanged_properties)
        except (AssertionError, Exception) as e:
            print(f"Alexa validation failed on first attempt: {e}")
            print("Waiting 5 seconds before retry...")
            time.sleep(5)
            validate_alexa_notif_once(changed_properties, unchanged_properties)  # Retry once

    def validate_gva_notifications(expected_device_states):
        # Try validation with retry logic
        try:
            validate_gva_notif_once(expected_device_states)
        except (AssertionError, Exception) as e:
            print(f"GVA validation failed on first attempt: {e}")
            print("Waiting 5 seconds before retry...")
            time.sleep(5)
            validate_gva_notif_once(expected_device_states)


    # Put some initial data in the shadow
    device.update_named_shadow(shadow_name, {
        "Light": {
            "Power": False,
            "Brightness": 50,
        }
    })
    time.sleep(5)

    test_exception = None
    try:
        light_device_id = device.node_thing_name + ".Light"

        # NOTIFICATION TRIGGER 1: Power on with version 1
        print("Testing notification trigger 1: Power on with version 1")
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": True,
            },
            "notify": {
                "version": 1,
                "webhook_mock": True,
                "alexa": True,
                "gva": True,
            }
        })

        # Validate first notification
        validate_webhook_notifications(
            {"Light": {"Power": True}}, expected_version=1)
        validate_alexa_notifications(
            changed_properties={
                "Alexa.PowerController": {"powerState": "ON"}
            },
            unchanged_properties={
                "Alexa.BrightnessController": {"brightness": 50}
            }
        )
        # GVA reports full device state (not just delta)
        validate_gva_notifications({
            light_device_id: {
                "on": True,
                "brightness": 50,
                "online": True,
            }
        })


        # NOTIFICATION TRIGGER 2: Change brightness with version 2
        print("Testing notification trigger 2: Change brightness with version 2")
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Brightness": 75,
            },
            "notify": {
                "version": 2,
                "webhook_mock": True,
                "alexa": True,
                "gva": True,
            }
        })

        # Validate second notification
        validate_webhook_notifications(
            {"Light": {"Brightness": 75}}, expected_version=2)
        validate_alexa_notifications(
            changed_properties={
                "Alexa.BrightnessController": {"brightness": 75}
            },
            unchanged_properties={
                "Alexa.PowerController": {"powerState": "ON"}
            }
        )
        validate_gva_notifications({
            light_device_id: {
                "on": True,
                "brightness": 75,
                "online": True,
            }
        })

        # NOTIFICATION TRIGGER 3: Change Power to False without notify clause
        print("Testing notification trigger 3: Change Power to False without notify clause")
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": False,
            }
        })

        # Validate notification still contains second notification's data
        # So basically no NEW notification was generated
        validate_webhook_notifications(
            {"Light": {"Brightness": 75}}, expected_version=2)
        validate_alexa_notifications(
            changed_properties={
                "Alexa.BrightnessController": {"brightness": 75}
            },
            unchanged_properties={
                "Alexa.PowerController": {"powerState": "ON"}
            }
        )
        # GVA should still have the second notification's data
        validate_gva_notifications({
            light_device_id: {
                "on": True,
                "brightness": 75,
                "online": True,
            }
        })

    except Exception as e:
        print(f"Notification test failed: {e}")
        test_exception = e  # Store the exception to re-raise after cleanup

    finally:
        print("Webhook notification test completed, proceeding with cleanup...")

    # Clean up - unshare the group from all users
    for user in test_users[1:]:
        user1_group_api.unshare_group(group_id, user.user_id)

    device.disconnect()

    # Re-raise the exception after cleanup to mark the test as failed
    if test_exception is not None:
        raise test_exception

def test_register_client_put_is_idempotent(test_user1):
    """PUT /v1/integrations/{integrationId}/endpoints is idempotent for matching delivery_credentials. Re-sending the same body returns the same endpoint_id (a no-op write). Sending a different token creates a new endpoint row with a new endpoint_id — multi-endpoint per (user_id, integration_id) is the supported model."""
    integration_id = "ios-dummy"

    # First PUT returns an endpoint_id.
    token1 = "idempotency-token-1"
    endpoint_id_1 = test_user1.register_client(integration_id, token1)
    assert endpoint_id_1, "first register_client did not return an endpoint_id"

    # Second PUT with the same body returns the same endpoint_id.
    endpoint_id_2 = test_user1.register_client(integration_id, token1)
    assert endpoint_id_2 == endpoint_id_1, f"endpoint_id changed across identical PUTs: {endpoint_id_1!r} -> {endpoint_id_2!r}"

    # Third PUT with a different token creates a new endpoint row (different endpoint_id).
    token2 = "idempotency-token-2"
    endpoint_id_3 = test_user1.register_client(integration_id, token2)
    assert endpoint_id_3 and endpoint_id_3 != endpoint_id_1, f"different tokens should yield different endpoint_ids; got {endpoint_id_1!r} and {endpoint_id_3!r}"

    # Only the push branch (lowercase gcm_*/apns_*) looks the row up, so ios-dummy alone would never
    # exercise the DELETE-side GetItem.
    absent_push_endpoint_id = base64.urlsafe_b64encode(
        f"arn:aws:sns:{REGION}:000000000000:endpoint/GCM/itest-absent/{uuid.uuid4()}".encode()
    ).decode().rstrip("=")

    for platform_type, endpoint_id, app_name in [
        (integration_id, endpoint_id_1, None),
        (integration_id, endpoint_id_3, None),
        ("gcm", absent_push_endpoint_id, "itest-absent"),
    ]:
        response = test_user1.unregister_client(platform_type, endpoint_id, app_name)
        assert response.status_code != 500, f"DELETE {platform_type} failed: {response.text}"


@pytest.mark.xdist_group("env_mut")
def test_mobile_push_notification(test_user2, associated_device, super_admin_user):
    """Test mobile push notifications using SQS queue for validation."""
    import subprocess
    import uuid
    import os

    # Create SQS queue for capturing push notifications
    sqs = boto3.client('sqs', region_name=REGION)
    queue_name = f"test-push-notifications-{uuid.uuid4()}"

    # Create queue
    response = sqs.create_queue(QueueName=queue_name)
    queue_url = response['QueueUrl']

    print(f"Created SQS queue: {queue_name}")
    print(f"Queue URL: {queue_url}")

    try:
        # Redirect push to SQS, and reload push text config per invocation so config changes apply without a cold start.
        subprocess.run([
            sys.executable, "tools/drx.py", "update-env",
            "rmng-notifications",
            f"push_mock_sqs_url={queue_url}",
            "test_push_text_config_no_cache=1"
        ], check=True)

        device, group_id, test_user1, user1_group_api = associated_device
        assert device.connect(), "Failed to connect the device"
        device.set_node_config({
            "devices": [{
                "id": "Light",
                "type": "esp.device.lightbulb",
                "params": [
                    {"id": "Power", "type": "esp.param.power"},
                    {"id": "Brightness", "type": "esp.param.brightness"}
                ]
            }]
        })

        test_users = [test_user1, test_user2]
        endpoint_ids = {}  # username -> {"ios": endpoint_id, "android": endpoint_id}

        # Each user registers with a distinct locale so TEST 3 can assert per-user
        # localized rendering. TEST 1/2 use configs with no locale block, so every
        # message falls back to the default text regardless of the stored locale.
        user_locales = {test_user1.sub: "es_ES", test_user2.sub: "fr_FR"}

        # Register each user for both iOS and Android platforms
        for user in test_users:
            locale = user_locales[user.sub]
            # Register iOS device
            ios_token = f"arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/app/ios-device-token-{user.sub}"
            ios_endpoint_id = user.register_client("MOCK_APNS_cafebabe", ios_token, locale=locale)

            # Register Android device
            android_token = f"arn:aws:sns:us-east-1:123456789012:endpoint/GCM/app/android-device-token-{user.sub}"
            android_endpoint_id = user.register_client("MOCK_GCM_cafebabe", android_token, locale=locale)

            endpoint_ids[user.username] = {"ios": ios_endpoint_id, "android": android_endpoint_id}
            print(f"Registered user {user.username} (locale={locale}) with both iOS and Android platforms")

        # Share the group with test_user2
        user1_group_api.share_group(group_id, test_user2.username, "secondary")
        accept_sharing_request_for(test_user2, group_id, "")

        # Connect the device and set up shadow
        shadow_name = f"params-{group_id}"
        if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
            for subgroup_id in sorted(device.subgroup_ids):
                shadow_name += f"-{subgroup_id}"

        assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"

        # Clear any existing messages from the queue
        while True:
            response = sqs.receive_message(QueueUrl=queue_url, MaxNumberOfMessages=10, WaitTimeSeconds=1)
            if 'Messages' not in response:
                break
            for message in response['Messages']:
                sqs.delete_message(QueueUrl=queue_url, ReceiptHandle=message['ReceiptHandle'])

        # Put some initial data in the shadow
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": False,
                "Brightness": 50,
            }
        })
        time.sleep(2)

        def read_and_validate_push_messages(expected_alert_body, body_by_sub=None):
            """Helper function to read SQS messages and validate against expected structure.

            expected_alert_body is the body every user's message should carry. To assert per-user
            localized bodies (different users rendered in their own locale from the same push), pass
            body_by_sub as a {user.sub: expected_body} map; it overrides expected_alert_body per user.
            """
            # The ARNs this test owns: the 2 MOCK endpoints (iOS + Android) per test user.
            # The fanout also hits any other endpoints the user happens to have (e.g. real
            # platform endpoints left over from manual testing), so scope validation to our
            # own ARNs rather than asserting a global message count.
            expected_arns = set()
            for user in test_users:
                expected_arns.add(f"arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/app/ios-device-token-{user.sub}")
                expected_arns.add(f"arn:aws:sns:us-east-1:123456789012:endpoint/GCM/app/android-device-token-{user.sub}")

            # Read messages from SQS queue. SNS→SQS is at-least-once, so the same
            # TargetArn can be delivered more than once; dedup by TargetArn and poll
            # until all expected targets arrive (or timeout) rather than counting raw
            # messages — a duplicate would otherwise satisfy the count before the last
            # distinct message lands.
            messages_by_arn = {}
            max_polls = 8
            poll_count = 0

            while poll_count < max_polls and not expected_arns.issubset(messages_by_arn.keys()):
                response = sqs.receive_message(QueueUrl=queue_url, MaxNumberOfMessages=10, WaitTimeSeconds=2)
                print("response", response)
                if 'Messages' in response:
                    for message in response['Messages']:
                        try:
                            # Parse SQS message body (should be SNS message format)
                            message_body = json.loads(message['Body'])
                            messages_by_arn[message_body['TargetArn']] = message_body
                            sqs.delete_message(QueueUrl=queue_url, ReceiptHandle=message['ReceiptHandle'])
                        except json.JSONDecodeError as e:
                            print(f"Failed to parse message: {e}")
                poll_count += 1

            # Keep only the ARNs this test owns; ignore any unrelated endpoints on the user.
            messages_by_arn = {arn: m for arn, m in messages_by_arn.items() if arn in expected_arns}
            messages = list(messages_by_arn.values())
            print(f"Received {len(messages)} distinct push notification messages")

            # Validate that we received all of this test's expected messages (2 users × 2 platforms).
            assert expected_arns.issubset(messages_by_arn.keys()), f"Missing expected push messages. Expected {sorted(expected_arns)} but got {sorted(messages_by_arn)}"

            # Define expected message structures
            expected_messages = []
            for user in test_users:
                # Per-user body: localized override if provided, else the shared body.
                user_body = (body_by_sub or {}).get(user.sub, expected_alert_body)
                # iOS message for this user
                expected_messages.append({
                    "TargetArn": f"arn:aws:sns:us-east-1:123456789012:endpoint/APNS_SANDBOX/app/ios-device-token-{user.sub}",
                    "MessageStructure": "json",
                    "Message": {
                        "default": f"Node Alert: {user_body}",
                        "APNS": {
                            "aps": {
                                "alert": {
                                    "title": "Node Alert",
                                    "body": user_body
                                },
                                "sound": "default",
                                "category": "node_alert",
                                "mutable-content": 1,
                                "thread-id": device.node_thing_name + ".node.alert"
                            },
                            "event_data": {
                                "data": {
                                    "nodeID": device.node_thing_name,
                                },
                                "ts": 0,
                                "type": "node_alert",
                            },
                        }
                    }
                })

                # Android message for this user
                expected_messages.append({
                    "TargetArn": f"arn:aws:sns:us-east-1:123456789012:endpoint/GCM/app/android-device-token-{user.sub}",
                    "MessageStructure": "json",
                    "Message": {
                        "default": f"Node Alert: {user_body}",
                        "GCM": {
                            "data": {
                                "title": "Node Alert",
                                "event_data": {
                                    "data": {
                                        "nodeID": device.node_thing_name,
                                    },
                                    "ts": 0,
                                    "type": "node_alert",
                                    "notif_grp_id": device.node_thing_name + ".node.alert"
                                },
                                "body": user_body
                            },
                            "android": {
                                "priority": "high"
                            }
                        }
                    }
                })

            # Create a map of actual messages by target ARN for easier comparison
            actual_messages_by_arn = {}
            for message in messages:
                target_arn = message['TargetArn']

                # Parse nested JSON in Message field
                parsed_message = json.loads(message['Message'])

                # Parse platform-specific payloads
                for platform in ['APNS', 'GCM']:
                    if platform in parsed_message:
                        parsed_message[platform] = json.loads(parsed_message[platform])

                        # Normalize nondeterministic fields for comparison: ts
                        # (timestamp) and uuid (mock_random correlation token the
                        # send path injects into event_data when mock_random is set).
                        if platform == 'APNS' and 'event_data' in parsed_message[platform]:
                            parsed_message[platform]['event_data']['ts'] = 0
                            parsed_message[platform]['event_data'].pop('uuid', None)
                        elif platform == 'GCM' and 'data' in parsed_message[platform] and 'event_data' in parsed_message[platform]['data']:
                            parsed_message[platform]['data']['event_data']['ts'] = 0
                            parsed_message[platform]['data']['event_data'].pop('uuid', None)

                actual_messages_by_arn[target_arn] = {
                    "TargetArn": target_arn,
                    "MessageStructure": message['MessageStructure'],
                    "Message": parsed_message
                }

            # Validate each expected message
            for expected in expected_messages:
                target_arn = expected['TargetArn']

                assert target_arn in actual_messages_by_arn, f"Expected message for ARN {target_arn} not found"

                actual = actual_messages_by_arn[target_arn]

                # Compare the complete message structure
                assert actual == expected, f"Message mismatch for ARN {target_arn}:\nExpected: {json.dumps(expected, indent=2)}\nActual: {json.dumps(actual, indent=2)}"

        def write_custom_push_text_config_via_api(custom_config):
            """Upload custom push text configuration using the file API instead of direct S3 upload."""
            # Create a temporary file with the custom configuration
            config_body = json.dumps(custom_config, indent=2)
            print(f"Uploading custom push text configuration via API:")
            print(config_body)

            # Create a temporary file
            with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as temp_file:
                temp_file.write(config_body)
                temp_file_path = temp_file.name

            # The file API uploads with If-None-Match (create-only), so a second upload in
            # the same run hits 412 PreconditionFailed. Delete any existing config first so
            # this helper can be called more than once.
            try:
                boto3.client('s3', region_name=REGION).delete_object(
                    Bucket=FILES_BUCKET_NAME, Key='system/push_text_config.json')
            except Exception as e:
                print(f"Note: could not pre-delete existing push_text_config.json: {e}")

            try:
                # Upload the file using the file API
                success, s3_path = super_admin_user.upload_file(temp_file_path, 'push_text_config')

                if not success:
                    raise Exception(f"File upload failed: {s3_path}")

                print(f"✅ Upload successful! S3 Path: {s3_path}")

                # Restart the notifications lambda to reload the configuration
                # This simulates the lambda cold start that would pick up the new config
                subprocess.run([
                    sys.executable, "tools/drx.py", "update-env",
                    "rmng-notifications",
                    f"FORCE_RESTART={int(time.time())}"
                ], check=True)

            finally:
                # Clean up temporary file
                try:
                    os.unlink(temp_file_path)
                except Exception as e:
                    print(f"Warning: Could not clean up temporary file {temp_file_path}: {e}")

        # TEST 1: Default text. Upload a default-only config (no locale block) to pin a known state.
        print("Testing mobile push notification: Default alert text")
        write_custom_push_text_config_via_api({
            "default": {
                "event": {
                    "node_alert": {
                        "text": "Node {nodeID} has an alert!"
                    }
                }
            }
        })
        time.sleep(5)  # wait for the S3 config write to be visible
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": True,
            },
            "notify": {
                "version": 1,
                "push": True,
            }
        })

        # Validate with default alert text
        default_alert_body = f"Node {device.node_thing_name} has an alert!"
        read_and_validate_push_messages(default_alert_body)

        # TEST 1b: same push via a direct notification (nil ShadowUpdateData); regression guard for the Marshal nil-deref.
        print("Testing mobile push notification: direct notification path")
        device.group_id = group_id  # send_direct_notification builds the topic from this
        assert device.send_direct_notification({"push": True}), \
            "Failed to publish direct push notification"
        read_and_validate_push_messages(default_alert_body)

        # TEST 2: Upload custom push text configuration via API and test override
        print("Testing mobile push notification: Custom alert text via API")
        # Create custom push text configuration
        custom_config = {
            "default": {
                "event": {
                    "node_alert": {
                        "text": "CUSTOM ALERT: Node {nodeID} requires attention!"
                    }
                }
            }
        }
        write_custom_push_text_config_via_api(custom_config)

        # Wait for some time, otherwise the S3 object is not seen by the lambda
        time.sleep(5)
        # Trigger another push notification
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": False,
            },
            "notify": {
                "version": 2,
                "push": True,
            }
        })

        # Validate with custom alert text
        custom_alert_body = f"CUSTOM ALERT: Node {device.node_thing_name} requires attention!"
        read_and_validate_push_messages(custom_alert_body)

        # TEST 3: Per-user locale. The same push must render in each user's stored
        # locale (set at registration above): test_user1 (es_ES) gets Spanish,
        # test_user2 (fr_FR) gets French.
        print("Testing mobile push notification: Per-user locale override")

        # Upload a config with locale-specific node_alert bodies (title stays default).
        locale_config = {
            "default": {
                "event": {
                    "node_alert": {
                        "text": "Node {nodeID} has an alert!"
                    }
                }
            },
            "locale": {
                "es_ES": {
                    "event": {
                        "node_alert": {
                            "text": "¡El nodo {nodeID} tiene una alerta!"
                        }
                    }
                },
                "fr_FR": {
                    "event": {
                        "node_alert": {
                            "text": "Le nœud {nodeID} a une alerte!"
                        }
                    }
                }
            }
        }
        write_custom_push_text_config_via_api(locale_config)

        time.sleep(5)  # wait for the S3 config write to be visible
        device.update_named_shadow(shadow_name, {
            "Light": {
                "Power": True,
            },
            "notify": {
                "version": 3,
                "push": True,
            }
        })

        body_by_sub = {
            test_user1.sub: f"¡El nodo {device.node_thing_name} tiene una alerta!",
            test_user2.sub: f"Le nœud {device.node_thing_name} a une alerte!",
        }
        read_and_validate_push_messages(expected_alert_body=None, body_by_sub=body_by_sub)

        print("Mobile push notification test completed successfully!")

    finally:
        # Clean up custom push text configuration using S3 (since we don't have a delete API)
        try:
            s3 = boto3.client('s3', region_name=REGION)
            s3.delete_object(Bucket=FILES_BUCKET_NAME, Key='system/push_text_config.json')
            print("Deleted custom push text configuration from S3")
        except Exception as e:
            print(f"Error deleting S3 configuration: {e}")

        # Clean up SQS queue
        try:
            sqs.delete_queue(QueueUrl=queue_url)
            print(f"Deleted SQS queue: {queue_name}")
        except Exception as e:
            print(f"Error deleting SQS queue: {e}")

        # Clean up environment variable
        try:
            subprocess.run([
                sys.executable, "tools/drx.py", "update-env",
                "rmng-notifications",
                "push_mock_sqs_url=",
                "test_push_text_config_no_cache="
            ], check=True)
            print("Cleared push_mock_sqs_url and test_push_text_config_no_cache environment variables")
        except Exception as e:
            print(f"Error clearing environment variable: {e}")

        for user in test_users:
            ids = endpoint_ids.get(user.username, {})
            if ids.get("ios"):
                user.unregister_client("MOCK_APNS_cafebabe", ids["ios"])
            if ids.get("android"):
                user.unregister_client("MOCK_GCM_cafebabe", ids["android"])

        # Clean up - unshare the group from test_user2
        try:
            user1_group_api.unshare_group(group_id, test_user2.user_id)
        except Exception as e:
            print(f"Error unsharing group for user: {e}")

        device.disconnect()


def test_update_and_list_mobile_platforms(
    super_admin_user, test_user1, apns_credentials, firebase_service_account
):
    """
    Test updating mobile platform credentials for iOS and Android, and that the
    non-admin GET /v1/integrations surfaces those integrations to a plain user.

    Shares one setup (admin registers iOS + Android) across two concerns:
      - admin PUT credential update, and
      - the non-admin public list (id+type only, no credentials, must not 403).
    """
    ios_config = apns_credentials
    android_config = firebase_service_account

    sns_client = boto3.client('sns', region_name=REGION)
    created_platform_arns = []

    try:
        # Test iOS platform update
        ios_result = super_admin_user.register_ios_platform(
            authentication_key=ios_config["key"],
            key_id=ios_config["key_id"],
            team_id=ios_config["team_id"],
            bundle_id=ios_config["bundle_id"],
            sandbox=False
        )
        assert ios_result is not None, "Failed to create iOS platform"
        created_platform_arns.append(("apns", ios_config["bundle_id"]))

        update_result = super_admin_user.update_mobile_platform(
            platform="APNS",
            authentication_key=ios_config["key"],
            key_id=ios_config["key_id"],
            team_id=ios_config["team_id"],
            bundle_id=ios_config["bundle_id"],
            apns_sandbox=False
        )
        assert update_result is not None, "Failed to update iOS platform"
        print("iOS platform updated successfully")

        # Test Android platform update
        android_result = super_admin_user.register_android_platform(
            json_content=json.dumps(android_config)
        )
        assert android_result is not None, "Failed to create Android platform"
        created_platform_arns.append(("GCM", android_config["project_id"]))

        update_result = super_admin_user.update_mobile_platform(
            platform="GCM",
            api_key=json.dumps(android_config)
        )
        assert update_result is not None, "Failed to update Android platform"
        print("Android platform updated successfully")

        expected_apns = f"apns_{ios_config['bundle_id']}"
        expected_gcm = f"gcm_{android_config['project_id']}"

        # --- Admin GET /v1/admin/integrations/{id} on the same setup ----------
        # The admin GET-one returns per-type detail the public list omits.
        status, apns_detail = super_admin_user.get_mobile_platform(expected_apns)
        assert status == 200, f"admin GET-one apns failed: {status} {apns_detail}"
        assert apns_detail["integration_id"] == expected_apns
        assert apns_detail["integration_type"] == "apns"
        assert apns_detail["bundle_id"] == ios_config["bundle_id"]

        status, gcm_detail = super_admin_user.get_mobile_platform(expected_gcm)
        assert status == 200, f"admin GET-one gcm failed: {status} {gcm_detail}"
        assert gcm_detail["integration_id"] == expected_gcm
        assert gcm_detail["integration_type"] == "gcm"
        assert gcm_detail["project_id"] == android_config["project_id"]

        # A non-admin user must NOT reach the admin tree.
        status, _ = test_user1.get_mobile_platform(expected_apns)
        assert status == 403, f"expected 403 for non-admin admin GET-one, got {status}"

        # --- Non-admin GET /v1/integrations on the same setup -----------------

        # A plain user may call the public list (must not 403).
        public = test_user1.list_public_integrations()
        assert public is not None, "non-admin GET /v1/integrations failed"
        assert "integrations" in public, f"unexpected response shape: {public}"

        items = public["integrations"]
        ids = [it["integration_id"] for it in items]
        assert expected_apns in ids, f"{expected_apns} not in public list: {ids}"
        assert expected_gcm in ids, f"{expected_gcm} not in public list: {ids}"

        # Each entry exposes only the two public identifiers — no credentials.
        for it in items:
            assert set(it.keys()) == {"integration_id", "integration_type"}, \
                f"public entry leaks extra fields: {it}"
        body = json.dumps(public)
        for secret in ("private_key", "authentication_key", "team_id", "key_id"):
            assert secret not in body, f"public list leaked '{secret}'"

        # Public ids agree with the admin list (admin returns the same set plus
        # per-type detail the public view strips out).
        admin = super_admin_user.list_mobile_platforms()
        admin_ids = {p["integration_id"] for p in admin["integrations"]}
        assert set(ids) == admin_ids, \
            f"public ids {set(ids)} disagree with admin ids {admin_ids}"

        # The integration_type query filter narrows the list.
        filtered = test_user1.list_public_integrations(integration_type="gcm")
        assert filtered is not None
        assert all(it["integration_type"] == "gcm" for it in filtered["integrations"]), \
            f"filter returned off-type entries: {filtered['integrations']}"
        assert expected_gcm in [it["integration_id"] for it in filtered["integrations"]]
        print("non-admin public integrations list validated")

    finally:
        for platform, app_name in created_platform_arns:
            try:
                super_admin_user.delete_mobile_platform(platform, app_name)
            except Exception as e:
                print(f"Warning: Failed to cleanup integration {platform}_{app_name}: {e}")


@pytest.mark.xdist_group("env_mut")
def test_delete_mobile_platform(super_admin_user, apns_credentials, firebase_service_account):
    """
    Test deleting mobile platform for iOS and Android.
    """
    time.sleep(5) # allow aws to propagate previous test changes

    ios_config = apns_credentials
    android_config = firebase_service_account

    sns_client = boto3.client('sns', region_name=REGION)

    # Test iOS platform deletion
    ios_result = super_admin_user.register_ios_platform(
        authentication_key=ios_config["key"],
        key_id=ios_config["key_id"],
        team_id=ios_config["team_id"],
        bundle_id=ios_config["bundle_id"],
        sandbox=False
    )
    assert ios_result is not None, "Failed to create iOS platform"

    delete_result = super_admin_user.delete_mobile_platform(
        platform="APNS",
        platform_app_name=ios_config["bundle_id"],
    )
    assert delete_result is not None, "Failed to delete iOS platform"
    print("iOS platform deleted successfully")

    # Test Android platform deletion
    android_result = super_admin_user.register_android_platform(
        json_content=json.dumps(android_config)
    )
    assert android_result is not None, "Failed to create Android platform"

    delete_result = super_admin_user.delete_mobile_platform(
        platform="GCM",
        platform_app_name=android_config["project_id"],
    )
    assert delete_result is not None, "Failed to delete Android platform"
    print("Android platform deleted successfully")


def test_unregister_foreign_push_endpoint_denied(two_tenants):
    """A must not unregister B's push endpoint (notification DoS).

    Endpoint rows are keyed (user_id, integration_endpoint); the delete is
    scoped to the caller. This locks that in: A deleting B's endpoint id must
    not remove B's ability to receive notifications.
    """
    tenant_a, tenant_b = two_tenants
    user_a, user_b = tenant_a["user"], tenant_b["user"]

    endpoint_id = user_b.register_client("ios-dummy", "victim-device-token")
    if not endpoint_id:
        pytest.skip("could not obtain B's endpoint_id to attempt cross-tenant delete")

    # The delete is keyed (caller_user_id, integration, endpoint), so it must not
    # touch B's row regardless of what it returns to A.
    a_deleted = user_a.unregister_client("ios-dummy", endpoint_id).status_code

    # register_client (PUT) is idempotent, so B re-registering returns the same
    # endpoint_id iff the row was never removed.
    reg2 = user_b.register_client("ios-dummy", "victim-device-token")
    assert reg2 == endpoint_id, (
        f"B's push endpoint was altered by A's cross-tenant delete "
        f"(A's delete returned HTTP {a_deleted}; before={endpoint_id}, after={reg2})"
    )
