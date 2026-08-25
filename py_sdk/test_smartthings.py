# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""SmartThings Schema helper functions for testing.

SmartThings invokes the st_action Lambda directly, not through API Gateway, so tests build
the Schema request envelope here and invoke that Lambda with boto3. The request builders take
the caller's OAuth token, which is the user's Cognito access token; User.st_* wraps them.
"""

import json
import uuid

import boto3

ST_SCHEMA = 'st-schema'
ST_SCHEMA_VERSION = '1.0'


def st_external_device_id(node_id, device_name):
    """Build the SmartThings externalDeviceId, which is <nodeID>#<deviceName>."""
    return f"{node_id}#{device_name}"


def st_headers(interaction_type):
    """Build the Schema envelope headers for one interaction."""
    return {
        'schema': ST_SCHEMA,
        'version': ST_SCHEMA_VERSION,
        'interactionType': interaction_type,
        'requestId': str(uuid.uuid4()),
    }


def discovery_request(token):
    """Build a discoveryRequest envelope."""
    return {
        'headers': st_headers('discoveryRequest'),
        'authentication': {'tokenType': 'Bearer', 'token': token},
    }


def command_request(token, devices):
    """Build a commandRequest envelope.

    devices: list of {"externalDeviceId": str, "commands": [...], "deviceCookie": {...}}
    """
    return {
        'headers': st_headers('commandRequest'),
        'authentication': {'tokenType': 'Bearer', 'token': token},
        'devices': devices,
    }


def state_refresh_request(token, external_device_ids):
    """Build a stateRefreshRequest envelope for the given device ids."""
    return {
        'headers': st_headers('stateRefreshRequest'),
        'authentication': {'tokenType': 'Bearer', 'token': token},
        'devices': [{'externalDeviceId': eid} for eid in external_device_ids],
    }


def command_device(external_device_id, commands, device_cookie=None):
    """Build one commandRequest device entry.

    commands: list of {"component", "capability", "command", "arguments"}
    device_cookie: the cookie discovery returned for this device, which SmartThings echoes
        back on every command. Omit it to exercise the node-config fallback.
    """
    device = {'externalDeviceId': external_device_id, 'commands': commands}
    if device_cookie is not None:
        device['deviceCookie'] = device_cookie
    return device


def invoke_schema_app(request_body, lambda_arn, region):
    """Invoke the Schema App Lambda and return the parsed response."""
    lambda_client = boto3.client('lambda', region_name=region)
    response = lambda_client.invoke(
        FunctionName=lambda_arn,
        InvocationType='RequestResponse',
        Payload=json.dumps(request_body)
    )
    return json.loads(response['Payload'].read())
