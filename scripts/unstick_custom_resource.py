#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Unstick a CloudFormation stack hung on a custom resource that never sent a response.

A custom-resource lambda that raises before calling cfnresponse.send leaves CloudFormation waiting
on that request (up to the 1-hour custom-resource timeout), stalling the stack in *_IN_PROGRESS.
CloudFormation does NOT re-invoke the lambda on its own once Lambda's async retries are exhausted —
it just polls the pre-signed ResponseURL from the original event. So the only way to clear it in place
is to PUT SUCCESS to that ResponseURL directly.

This script recovers the ResponseURL from the handler's CloudWatch logs — our inline handlers log the
raw event as `CR_EVENT {...}` at entry (see aws-rules.mdc) — and PUTs a SUCCESS response for the stuck
request. Deploy the real fix (stable PhysicalResourceId) afterwards so the resource stops replacing on
every update.

Usage:
    AWS_REGION=... python3 scripts/unstick_custom_resource.py --function <lambda-name>
    # --request-id <id>  target a specific stuck request (else the newest CR_EVENT is used)
Then watch the stack leave *_IN_PROGRESS in the console; deploy the real fix afterwards.
"""
import argparse
import json
import sys
import urllib.request

import boto3


def _find_stuck_event(logs, log_group, request_id):
    """Return the raw CR event dict from the most recent (or matching) CR_EVENT log line."""
    streams = logs.describe_log_streams(
        logGroupName=log_group, orderBy="LastEventTime", descending=True, limit=10
    )["logStreams"]
    candidates = []
    for s in streams:
        events = logs.get_log_events(
            logGroupName=log_group, logStreamName=s["logStreamName"], limit=200, startFromHead=False
        )["events"]
        for e in events:
            msg = e["message"]
            idx = msg.find("CR_EVENT ")
            if idx == -1:
                continue
            try:
                ev = json.loads(msg[idx + len("CR_EVENT "):].strip())
            except json.JSONDecodeError:
                continue
            if request_id and ev.get("RequestId") != request_id:
                continue
            candidates.append((e["timestamp"], ev))
    if not candidates:
        return None
    candidates.sort(key=lambda c: c[0])
    return candidates[-1][1]


def _send_success(event):
    body = json.dumps({
        "Status": "SUCCESS",
        "Reason": "unstuck by unstick_custom_resource.py",
        "PhysicalResourceId": event.get("PhysicalResourceId") or event["LogicalResourceId"],
        "StackId": event["StackId"],
        "RequestId": event["RequestId"],
        "LogicalResourceId": event["LogicalResourceId"],
        "Data": {},
    }).encode()
    req = urllib.request.Request(
        event["ResponseURL"], data=body, method="PUT",
        headers={"content-type": "", "content-length": str(len(body))},
    )
    with urllib.request.urlopen(req) as resp:
        return resp.status


def main() -> int:
    ap = argparse.ArgumentParser(description="Send SUCCESS to a stuck CloudFormation custom resource.")
    ap.add_argument("--function", required=True, help="Physical name of the stuck custom-resource lambda")
    ap.add_argument("--request-id", help="Target a specific stuck RequestId (default: newest CR_EVENT)")
    args = ap.parse_args()

    logs = boto3.client("logs")
    log_group = f"/aws/lambda/{args.function}"

    event = _find_stuck_event(logs, log_group, args.request_id)
    if not event:
        print(f"No CR_EVENT log line found in {log_group}.")
        print("The handler must log the raw event as 'CR_EVENT {...}' at entry (see aws-rules.mdc);")
        print("without it the pre-signed ResponseURL cannot be recovered.")
        return 1
    if "ResponseURL" not in event:
        print("Found a CR_EVENT but it has no ResponseURL — cannot respond.")
        return 1

    print(f"Recovered request {event['RequestId']} ({event.get('RequestType')}) for {event['LogicalResourceId']}.")
    status = _send_success(event)
    print(f"PUT SUCCESS to ResponseURL -> HTTP {status}")
    print("Watch the stack leave *_IN_PROGRESS, then redeploy the real fix.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
