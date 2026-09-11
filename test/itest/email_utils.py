# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Mailosaur addresses and credentials for the integration tests.

The Mailosaur API client lives in :mod:`esp_morpheus.sdk.mailosaur` and the SES identity flow in
:mod:`esp_morpheus.sdk.ses`; both take credentials as arguments. This module supplies the itest
credentials and the pytest-xdist address scheme that neither of them knows about.
"""

import os
import threading
import uuid
from typing import Dict, Optional

from esp_morpheus.sdk import mailosaur, ses

from test.itest.config_sources import describe_sources, load_json_config

# Mailosaur creds resolve from this repo's file, then the env blob, then the
# superproject's copy — see config_sources.
ITEST_CONFIG_REL_PATH = "itest/itest_config.json"
ITEST_CONFIG_ENV_VAR = "RMNG_ITEST_CONFIG_JSON"

# Per-(worker, role) auto-incrementing counters for deterministic Mailosaur addresses. Deterministic
# addresses let a user be reused across runs (fast re-auth vs. re-creation); keying by role keeps the
# admin and regular-user sequences disjoint so a regular signup never inherits an admin's stale record.
_user_email_counters: Dict[str, int] = {}
_user_email_lock = threading.Lock()


def _load_itest_config() -> dict:
    return load_json_config(ITEST_CONFIG_REL_PATH, ITEST_CONFIG_ENV_VAR)


def _get_mailosaur_credentials() -> Optional[tuple[str, str]]:
    """Mailosaur server ID + API key; None when no source supplies both."""
    cfg = _load_itest_config()
    server_id = cfg.get("mailosaur_server_id", "")
    api_key = cfg.get("mailosaur_api_key", "")
    if not server_id or not api_key:
        print(f"Warning: Mailosaur creds not set ({describe_sources(ITEST_CONFIG_REL_PATH, ITEST_CONFIG_ENV_VAR)})")
        return None
    return (server_id, api_key)


def is_email_service_available() -> bool:
    """True when the creds are configured and the Mailosaur server answers."""
    credentials = _get_mailosaur_credentials()
    return bool(credentials) and mailosaur.server_reachable(*credentials)


def generate_mailosaur_email(user_index: Optional[int] = None, is_admin: bool = False) -> Optional[str]:
    """
    Generate a deterministic Mailosaur email address, namespaced by role.

    Deterministic (per pytest-xdist worker + auto-incrementing index) so a user can be reused across
    runs. The 'user'/'admin' role segment keeps the two sequences disjoint, so a regular signup never
    reuses an address a prior admin test created (which would inherit a stale admin DB record).

    Args:
        user_index: Explicit index (1, 2, …). If None, auto-increments per (worker, role).
        is_admin: True for admin-pool signups, False for regular users. Selects the address namespace.

    Returns:
        Email address string, or None if the email service / credentials are unavailable.

    Email format: test-{worker_id}-{role}-{index}@{server_id}.mailosaur.net
    """
    credentials = _get_mailosaur_credentials()
    if not credentials or not mailosaur.server_reachable(*credentials):
        print("Mailosaur email service is not available")
        return None

    server_id, _ = credentials
    # PYTEST_XDIST_WORKER is set by pytest-xdist (e.g., "gw0", "gw1", "master")
    worker_id = os.environ.get("PYTEST_XDIST_WORKER", "master")
    role = "admin" if is_admin else "user"

    if user_index is None:
        key = f"{worker_id}-{role}"
        with _user_email_lock:
            user_index = _user_email_counters.get(key, 0) + 1
            _user_email_counters[key] = user_index

    server_email = mailosaur.address(f"test-{worker_id}-{role}-{user_index}", server_id)
    print(f"Using Mailosaur email: {server_email} (server: {server_id}, worker: {worker_id}, role: {role})")
    return server_email


def generate_mailosaur_email_specific(email_prefix: str) -> Optional[str]:
    """Build a fixed, caller-chosen Mailosaur address `<email_prefix>@<server_id>.mailosaur.net`.

    Unlike generate_mailosaur_email(), which auto-increments a fresh inbox, this returns a stable
    address for an explicit prefix. Returns None if Mailosaur creds are missing.
    """
    credentials = _get_mailosaur_credentials()
    if not credentials or not mailosaur.server_reachable(*credentials):
        return None
    return mailosaur.address(email_prefix, credentials[0])


def generate_otp_recipient_email(user_index=None, is_admin=False):
    """A Mailosaur address that sandbox SES can DELIVER to — for flows that send via SES (the OTP
    login). Verifies the deterministic address as an SES identity on first use (one-time per
    address, persists across runs; a fast status check afterwards). Flows whose email goes out
    via Cognito (signup verification) use plain generate_mailosaur_email — no SES identity needed.
    """
    email = generate_mailosaur_email(user_index=user_index, is_admin=is_admin)
    if email:
        ensure_ses_verified(email)
    return email


def ensure_ses_verified(email: str, region: Optional[str] = None) -> bool:
    """Make sure sandbox SES can deliver to `email`, following the verification link out of the
    Mailosaur inbox so no one has to click it."""
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return False
    server_id, api_key = credentials
    return ses.ensure_verified(email, region, confirm=lambda since: mailosaur.follow_link(
        server_id, api_key, ses.VERIFICATION_LINK_PATTERN,
        recipient_email=email, since_timestamp=since))


def generate_test_password() -> str:
    """A fresh password per call, so no credential that can sign in to a deployed pool is ever a
    literal in the source tree. Nothing needs it to be stable: every fixture that provisions a user
    also sets the password, and the value is used only within that run.
    """
    return f"It-{uuid.uuid4().hex}-Aa1!"


def generate_random_email() -> str:
    """Generate a unique, collision-safe email address that needs NO Mailosaur credentials.

    Used by fixtures that only need a user to *exist* (the backend lambda force-creates
    and confirms the user in Cognito — it never reads an emailed verification code), so
    a mailbox that can be read is not required. Only signup-verification and
    forgot-password tests, which consume an emailed code, need generate_mailosaur_email().

    Format: test-{worker_id}-{uuid-hex}@example.com
        worker_id (from PYTEST_XDIST_WORKER, default "master") is included only for
        readability when debugging parallel runs; uniqueness comes from the random uuid4 token.
    """
    worker_id = os.environ.get("PYTEST_XDIST_WORKER", "master")
    token = uuid.uuid4().hex[:12]
    return f"test-{worker_id}-{token}@example.com"


def get_link_from_server(link_pattern: str, recipient_email: Optional[str] = None,
                         since_timestamp: Optional[float] = None, max_retries: int = 8,
                         retry_delay: float = 3.0) -> Optional[str]:
    """Read the latest link matching `link_pattern` out of the Mailosaur inbox."""
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return None
    return mailosaur.find_link(*credentials, link_pattern, recipient_email=recipient_email,
                               since_timestamp=since_timestamp, max_retries=max_retries,
                               retry_delay=retry_delay)


def get_verification_code_from_server(max_retries: int = 5, retry_delay: float = 3.0,
                                      timeout: float = 60.0,
                                      since_timestamp: Optional[float] = None,
                                      recipient_email: Optional[str] = None) -> Optional[str]:
    """Wait for the emailed numeric code and return it, or None on timeout.

    Args:
        max_retries: Maximum number of retry attempts
        retry_delay: Delay between retries in seconds
        timeout: Maximum time to wait for email in seconds
        since_timestamp: Only look for emails sent after this timestamp (Unix epoch)
        recipient_email: If set, only consider emails sent to this address (avoids cross-test pollution when parallel)
    """
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return None
    return mailosaur.read_verification_code(*credentials, max_retries=max_retries,
                                            retry_delay=retry_delay, timeout=timeout,
                                            since_timestamp=since_timestamp,
                                            recipient_email=recipient_email)


def delete_mailosaur_message(message_id: str) -> bool:
    """Delete one Mailosaur message by ID."""
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return False
    return mailosaur.delete_message(*credentials, message_id)


def delete_mailosaur_messages_for_email(email: str) -> int:
    """Delete every message sent to `email` and return how many went."""
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return 0
    return mailosaur.delete_messages_for_email(*credentials, email)
