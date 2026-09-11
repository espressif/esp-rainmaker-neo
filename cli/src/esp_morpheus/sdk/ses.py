# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""The SES sender identity that delivers the OTP emails.

An identity is verified by clicking a link SES mails to the address, so the address owner normally
finishes the flow by hand. ``ensure_verified`` takes an optional ``confirm`` callable for a mailbox
nobody can open by hand — a test inbox — which keeps this module independent of how that mailbox is
read.
"""

import os
import time
from typing import Callable, Optional

import boto3

# AWS mails the verification link from the region's own verify endpoint.
VERIFICATION_LINK_PATTERN = r"https://[^\s\"'>]*amazonaws\.com[^\s\"'>]*[Vv]erif[^\s\"'>]*"

_verified: set = set()


def resolve_region(region: Optional[str] = None) -> Optional[str]:
    """The region to act in. Identities are per region, so a wrong one verifies nothing useful."""
    return (region or os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION")
            or (boto3.DEFAULT_SESSION.region_name if boto3.DEFAULT_SESSION else None))


def is_verified(email: str, region: str) -> bool:
    """True once SES reports the identity able to send."""
    try:
        return bool(boto3.client("sesv2", region_name=region)
                    .get_email_identity(EmailIdentity=email).get("VerifiedForSendingStatus"))
    except Exception:  # noqa: BLE001 - an absent identity is simply not verified
        return False


def request_verification(email: str, region: str) -> bool:
    """Create the identity so SES mails the verification link, or re-send for a pending one."""
    ses = boto3.client("sesv2", region_name=region)
    try:
        ses.create_email_identity(EmailIdentity=email)
        return True
    except ses.exceptions.AlreadyExistsException:
        try:
            boto3.client("ses", region_name=region).verify_email_identity(EmailAddress=email)
        except Exception:  # noqa: BLE001 - an already-verified identity re-sends nothing
            pass
        return True
    except Exception as e:  # noqa: BLE001
        print(f"SES create_email_identity failed: {e}")
        return False


def wait_verified(email: str, region: str, timeout: float = 30.0, poll: float = 3.0) -> bool:
    """Poll until SES reports the identity verified, or the timeout passes."""
    deadline = time.time() + timeout
    while True:
        if is_verified(email, region):
            return True
        if time.time() >= deadline:
            return False
        time.sleep(poll)


def ensure_verified(email: str, region: Optional[str] = None,
                    confirm: Optional[Callable[[float], bool]] = None,
                    timeout: float = 30.0) -> bool:
    """Make sure SES can send from `email`, requesting verification when it cannot.

    @note `confirm` takes the timestamp the link was requested and returns whether it followed it;
    without one the address owner must click the link before this reports success.
    """
    if email in _verified:
        return True
    region = resolve_region(region)
    if not region:
        return False
    if is_verified(email, region):
        _verified.add(email)
        return True

    since = time.time()
    if not request_verification(email, region):
        return False
    if confirm and not confirm(since):
        return False
    if not wait_verified(email, region, timeout=timeout):
        return False
    _verified.add(email)
    return True


def set_active_sender(email: str, region: str) -> None:
    """Record `email` as the deployment's outgoing sender.

    Written straight to espuser-admin-configs: there is no admin API for senders, and OTP dispatch
    reads the active sender from this row.
    """
    boto3.client("dynamodb", region_name=region).put_item(
        TableName="espuser-admin-configs",
        Item={
            "config_name": {"S": "email-sender"},
            "subtype": {"S": "global"},
            "value": {"S": email},
            "updated_at": {"N": str(int(time.time()))},
        },
    )
