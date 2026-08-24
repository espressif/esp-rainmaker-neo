# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""
Integration tests that exercise the SQS-batch path of presence_event_handler
and publish_input_event_handler end-to-end.

The handler stacks pre-provision the SQS queue, the SqsEventSource mapping,
and the lambda's sqs:ReceiveMessage IAM, regardless of whether the IoT
topic rule is currently set to Lambda-direct or SQS mode (see
rmneo/handlers/node/{node_conn,node_to_cloud}/stack.py). These tests
write directly to the queue, bypassing the IoT rule, so they pass
regardless of the deployment's current iot-event-mode and validate that:
  - the queue exists and the test caller can SendMessage to it
  - the SqsEventSource invokes the lambda with a Records[] batch
  - the unified handler's sniffer routes the SQS shape into handleSQSBatch
  - the lambda's downstream IAM (IoT data plane, DynamoDB) still works

These tests don't mutate global state, so they're safe to run in parallel
with each other and with test_iot_event_mode.py (no xdist_group needed).
"""
from queue import Empty
import json
import time

import boto3
import pytest

from test.itest.conftest import REGION, wait_for_node_session, wait_for_reported_online


NODE_CONN_QUEUE_NAME = "node-conn-queue"
NODE_TO_CLOUD_QUEUE_NAME = "node-to-cloud-queue"


def _get_queue_url(queue_name):
    sqs = boto3.client("sqs", region_name=REGION)
    return sqs.get_queue_url(QueueName=queue_name)["QueueUrl"]


def test_presence_lambda_processes_sqs_record(session_valid_device_rsa):
    """The presence_event_handler must process a disconnect event delivered
    via SQS — exercises the queue, the event-source mapping, the unified
    handler's sniffer/dispatcher, and the lambda's iot:UpdateThingShadow
    permission.

    We connect the device so node_connected_rule populates rmng-nodes-online with
    a broker-assigned session, then write a fake disconnect event for that
    same session directly to the queue. The handler matches the session,
    flips the iparams indexed shadow to online=false, and we read it back.
    """
    device = session_valid_device_rsa
    assert device.connect(), f"Failed to connect device {device.node_thing_name}"

    session_id, version_number = wait_for_node_session(device.node_thing_name)
    assert session_id, (
        f"node_connected_rule did not populate rmng-nodes-online for "
        f"{device.node_thing_name} within timeout"
    )

    sqs = boto3.client("sqs", region_name=REGION)
    queue_url = _get_queue_url(NODE_CONN_QUEUE_NAME)

    body = json.dumps({
        "clientId": device.node_thing_name,
        "eventType": "disconnected",
        "sessionIdentifier": session_id,
        "timestamp": int(time.time() * 1000),
        "principalIdentifier": "sqs-pipeline-test",
        "ipAddress": "127.0.0.1",
        "versionNumber": version_number,
        "disconnectReason": "CLIENT_INITIATED_DISCONNECT",
    })
    sqs.send_message(QueueUrl=queue_url, MessageBody=body)

    online = wait_for_reported_online(device.node_thing_name, "iparams", expected=False)
    assert online is False, (
        f"Timed out waiting for SQS-driven disconnect to flip iparams.online "
        f"to False for {device.node_thing_name}; last saw online={online!r}"
    )


def test_publish_input_lambda_processes_sqs_record(associated_device):
    """The publish_input_event_handler must process a to_cloud event
    delivered via SQS — exercises the queue, the event-source mapping, the
    unified handler's sniffer/dispatcher, and the lambda's iot:Publish
    permission on rainmaker/nodes/+/from_cloud.

    We bypass the IoT topic rule and put a getGroupInfo request directly on
    the queue. The handler resolves the device's group and publishes the
    response on rainmaker/nodes/<thing>/from_cloud, which the device
    subscribed to at connect time (see Device.connect() in test_device.py).
    """
    device, group_id, _, _ = associated_device

    # Drain stale getGroupInfo responses left over from association/setup.
    while not device.from_cloud_queue.empty():
        try:
            device.from_cloud_queue.get_nowait()
        except Empty:
            break

    sqs = boto3.client("sqs", region_name=REGION)
    queue_url = _get_queue_url(NODE_TO_CLOUD_QUEUE_NAME)

    body = json.dumps({
        "thing_name": device.node_thing_name,
        "data": {"event": ["getGroupInfo"]},
    })
    sqs.send_message(QueueUrl=queue_url, MessageBody=body)

    deadline = time.time() + 30
    while time.time() < deadline:
        try:
            message = device.from_cloud_queue.get(timeout=2)
        except Empty:
            continue
        if "getGroupInfo" in message:
            assert message["getGroupInfo"].get("pgrp") == group_id, (
                f"Expected pgrp={group_id}, got {message}"
            )
            return

    pytest.fail(
        f"Timed out waiting for SQS-driven getGroupInfo response on "
        f"rainmaker/nodes/{device.node_thing_name}/from_cloud"
    )


def test_publish_input_lambda_time_sync(associated_device):
    """A getTimeSync request on to_cloud must be answered with the current
    server time (epoch ms) on from_cloud, so devices can set their wall
    clock without waiting for SNTP.

    Delivered via the SQS queue like the test above, so it passes regardless
    of the deployment's current iot-event-mode. The tolerance is generous
    (5 minutes) to absorb queue latency and test-host clock skew — the goal
    is to prove the plumbing returns a sane wall-clock time, not accuracy.
    """
    device, _, _, _ = associated_device

    # Drain stale from_cloud responses left over from association/setup.
    while not device.from_cloud_queue.empty():
        try:
            device.from_cloud_queue.get_nowait()
        except Empty:
            break

    sqs = boto3.client("sqs", region_name=REGION)
    queue_url = _get_queue_url(NODE_TO_CLOUD_QUEUE_NAME)

    body = json.dumps({
        "thing_name": device.node_thing_name,
        "data": {"event": ["getTimeSync"]},
    })
    sqs.send_message(QueueUrl=queue_url, MessageBody=body)

    deadline = time.time() + 30
    while time.time() < deadline:
        try:
            message = device.from_cloud_queue.get(timeout=2)
        except Empty:
            continue
        if "getTimeSync" in message:
            server_time_ms = message["getTimeSync"].get("time")
            assert isinstance(server_time_ms, int), (
                f"Expected integer epoch-ms time, got {message}"
            )
            assert abs(server_time_ms - time.time() * 1000) < 5 * 60 * 1000, (
                f"Server time {server_time_ms} deviates more than 5 minutes "
                f"from test host time"
            )
            return

    pytest.fail(
        f"Timed out waiting for SQS-driven getTimeSync response on "
        f"rainmaker/nodes/{device.node_thing_name}/from_cloud"
    )
