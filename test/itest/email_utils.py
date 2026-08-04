# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Email reading utilities for tests using Mailosaur email testing service."""
import os
import re
import time
import uuid
import requests
from typing import Optional, Dict
import base64
import threading

from test.itest.config_sources import describe_sources, load_json_config


MAILOSAUR_API_URL = "https://mailosaur.com/api"

# Per-(worker, role) auto-incrementing counters for deterministic Mailosaur addresses. Deterministic
# addresses let a user be reused across runs (fast re-auth vs. re-creation); keying by role keeps the
# admin and regular-user sequences disjoint so a regular signup never inherits an admin's stale record.
_user_email_counters: Dict[str, int] = {}
_user_email_lock = threading.Lock()

# Mailosaur creds resolve from this repo's file, then the env blob, then the
# superproject's copy — see config_sources.
ITEST_CONFIG_REL_PATH = "itest/itest_config.json"
ITEST_CONFIG_ENV_VAR = "RMNG_ITEST_CONFIG_JSON"


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


def _mailosaur_headers(api_key: str) -> dict:
    """Basic-auth header for the Mailosaur API (api_key as the username)."""
    auth_b64 = base64.b64encode(f"{api_key}:".encode("ascii")).decode("ascii")
    return {"Authorization": f"Basic {auth_b64}"}


def _fetch_matching_messages(server_id, headers, recipient_email=None, since_timestamp=None):
    """Return hydrated (full) Mailosaur messages for a server, newest first,
    filtered by recipient and/or arrival time. One poll (no retry) — callers
    that wait for delivery loop over this. Returns [] on any API error."""
    from datetime import datetime
    try:
        resp = requests.get(f"{MAILOSAUR_API_URL}/messages", headers=headers,
                            params={"server": server_id}, timeout=15)
        if resp.status_code != 200:
            return []
        messages = resp.json().get("items", [])
    except requests.exceptions.RequestException as e:
        print(f"Error connecting to Mailosaur: {e}")
        return []

    if recipient_email:
        rl = recipient_email.lower()
        messages = [m for m in messages if any(
            (r.get("email", "") if isinstance(r, dict) else str(r)).lower() == rl
            for r in m.get("to", []))]
    if since_timestamp:
        kept = []
        for m in messages:
            rcv = m.get("received")
            try:
                if not rcv or datetime.fromisoformat(rcv.replace("Z", "+00:00")).timestamp() > since_timestamp:
                    kept.append(m)
            except Exception:
                kept.append(m)  # unparseable timestamp: keep to be safe
        messages = kept

    hydrated = []
    for msg in messages:
        mid = msg.get("id")
        if not mid:
            continue
        try:
            full = requests.get(f"{MAILOSAUR_API_URL}/messages/{mid}", headers=headers, timeout=15)
        except requests.exceptions.RequestException:
            continue
        if full.status_code == 200:
            hydrated.append(full.json())
    return hydrated


def _message_body_text(data: dict) -> str:
    """Best-effort plaintext body of a hydrated message (text, else de-tagged HTML)."""
    import re as _re
    text_data = data.get("text") or data.get("textBody")
    body = text_data.get("body", "") if text_data else ""
    if not body:
        html_data = data.get("html") or data.get("htmlBody")
        html_body = html_data.get("body", "") if html_data else ""
        if html_body:
            body = _re.sub(r"<[^>]+>", "", html_body)
    return body


def _extract_verification_code(body_text: str) -> Optional[str]:
    """Extract 6-digit verification code from email body text."""
    if not body_text:
        return None
    
    code_patterns = [
        r"verification code is\s+(\d{6})",
        r"code is\s+(\d{6})",
        r"code:\s*(\d{6})",
        r"verification code:\s*(\d{6})",
        r"(\d{6})",
    ]
    
    for pattern in code_patterns:
        match = re.search(pattern, body_text, re.IGNORECASE)
        if match:
            code = match.group(1)
            print(f"Found verification code: {code}")
            return code
    
    return None

def generate_mailosaur_email(user_index: Optional[int] = None, is_admin: bool = False) -> Optional[str]:
    """
    Generate a deterministic Mailosaur email address, namespaced by role.

    Deterministic (per pytest-xdist worker + auto-incrementing index) so a user can be reused across
    runs. The 'user'/'admin' role segment keeps the two sequences disjoint, so a regular signup never
    reuses an address a prior admin test created (which would inherit a stale super_admin DB record).

    Args:
        user_index: Explicit index (1, 2, …). If None, auto-increments per (worker, role).
        is_admin: True for admin-pool signups, False for regular users. Selects the address namespace.

    Returns:
        Email address string, or None if the email service / credentials are unavailable.

    Email format: test-{worker_id}-{role}-{index}@{server_id}.mailosaur.net
    """
    if not is_email_service_available():
        print("Mailosaur email service is not available")
        return None

    credentials = _get_mailosaur_credentials()
    if not credentials:
        print(f"Mailosaur credentials not configured ({describe_sources(ITEST_CONFIG_REL_PATH, ITEST_CONFIG_ENV_VAR)})")
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

    server_email = f"test-{worker_id}-{role}-{user_index}@{server_id}.mailosaur.net"
    print(f"Using Mailosaur email: {server_email} (server: {server_id}, worker: {worker_id}, role: {role})")
    return server_email


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


_ses_verified_cache: set = set()


def ensure_ses_verified(email: str, region: Optional[str] = None) -> bool:
    """Make sure sandbox SES can deliver to `email`: fast-path when the identity is already
    verified, otherwise run the Mailosaur-driven verification (create identity, read the SES
    verification link from the inbox, follow it)."""
    if email in _ses_verified_cache:
        return True
    try:
        import boto3
    except ImportError:
        return False
    # SES identities are per region, so falling back to the profile's region would verify the address
    # somewhere useless.
    region = (region or os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION")
              or (boto3.DEFAULT_SESSION.region_name if boto3.DEFAULT_SESSION else None))
    if not region:
        return False
    ses = boto3.client("sesv2", region_name=region)
    try:
        if ses.get_email_identity(EmailIdentity=email).get("VerifiedForSendingStatus"):
            _ses_verified_cache.add(email)
            return True
    except Exception:
        pass  # Not verified yet; fall through.
    if verify_ses_identity_via_mailosaur(email, region):
        _ses_verified_cache.add(email)
        return True
    return False


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


def generate_mailosaur_email_specific(email_prefix: str) -> Optional[str]:
    """Build a fixed, caller-chosen Mailosaur address `<email_prefix>@<server_id>.mailosaur.net`.

    Unlike generate_mailosaur_email(), which auto-increments a fresh inbox, this
    returns a stable address for an explicit prefix — use it for deterministic
    inboxes (e.g. a per-account+region SES sender) that must be the same across
    runs. The prefix is lowercased and made DNS-safe. Returns None if Mailosaur
    creds are missing.
    """
    if not is_email_service_available():
        print("Mailosaur email service is not available")
        return None
    credentials = _get_mailosaur_credentials()
    if not credentials:
        return None
    server_id, _ = credentials
    safe_prefix = email_prefix.lower().replace("_", "-")
    return f"{safe_prefix}@{server_id}.mailosaur.net"


def get_link_from_server(
    link_pattern: str,
    recipient_email: Optional[str] = None,
    since_timestamp: Optional[float] = None,
    max_retries: int = 8,
    retry_delay: float = 3.0,
) -> Optional[str]:
    """Read the latest matching link out of a Mailosaur inbox.

    Used to click through email-confirmation links (e.g. AWS SES identity
    verification) without a real mailbox. Returns the first URL whose text
    matches link_pattern (regex, searched against both the parsed links and the
    raw HTML/text body), or None.
    """
    import re as _re
    credentials = _get_mailosaur_credentials()
    if not credentials:
        print("Mailosaur credentials not configured in test_config.json")
        return None
    server_id, api_key = credentials
    headers = _mailosaur_headers(api_key)
    pat = _re.compile(link_pattern)

    for _ in range(max_retries):
        for data in _fetch_matching_messages(server_id, headers, recipient_email, since_timestamp):
            # Prefer Mailosaur's parsed links, then fall back to scanning the body.
            for section in ("html", "text"):
                sec = data.get(section) or {}
                for link in sec.get("links", []) or []:
                    if pat.search(link.get("href", "")):
                        return link["href"]
                m = pat.search(sec.get("body", "") or "")
                if m:
                    return m.group(0)
        time.sleep(retry_delay)
    print(f"No link matching {link_pattern!r} found after {max_retries} attempts")
    return None


def verify_ses_identity_via_mailosaur(email: str, region: str) -> bool:
    """Register a Mailosaur address as an SES email identity and confirm it by
    reading the SES verification link out of the Mailosaur inbox and following it.

    Lets sandbox SES deliver to the Mailosaur test inbox without a real mailbox or
    a manual click. Returns True once SES reports the identity verified.
    """
    import time as _time
    try:
        import boto3
    except ImportError:
        print("boto3 not available; cannot verify SES identity")
        return False

    ses = boto3.client("sesv2", region_name=region)
    since = _time.time()
    try:
        ses.create_email_identity(EmailIdentity=email)
        print(f"Requested SES verification for {email}")
    except ses.exceptions.AlreadyExistsException:
        # Re-send the verification email for an existing, still-pending identity.
        try:
            boto3.client("ses", region_name=region).verify_email_identity(EmailAddress=email)
        except Exception:
            pass
    except Exception as e:
        print(f"SES create_email_identity failed: {e}")
        return False

    # AWS SES verification links point at the region's amazonaws.com verify endpoint.
    link = get_link_from_server(
        link_pattern=r"https://[^\s\"'>]*amazonaws\.com[^\s\"'>]*[Vv]erif[^\s\"'>]*",
        recipient_email=email,
        since_timestamp=since,
    )
    if not link:
        print("No SES verification link arrived in the Mailosaur inbox")
        return False
    try:
        requests.get(link, timeout=20)
        print("Followed SES verification link")
    except requests.exceptions.RequestException as e:
        print(f"Failed to follow verification link: {e}")
        return False

    # Poll SES for the verified status.
    for _ in range(10):
        try:
            status = ses.get_email_identity(EmailIdentity=email).get("VerifiedForSendingStatus")
            if status:
                print(f"SES identity {email} verified")
                return True
        except Exception:
            pass
        _time.sleep(3)
    print(f"SES identity {email} did not reach verified status")
    return False

def get_verification_code_from_server(
    max_retries: int = 5,
    retry_delay: float = 3.0,
    timeout: float = 60.0,
    since_timestamp: Optional[float] = None,
    recipient_email: Optional[str] = None
) -> Optional[str]:
    """
    Retrieve verification code from the latest email in a Mailosaur server.
    
    Args:
        max_retries: Maximum number of retry attempts
        retry_delay: Delay between retries in seconds
        timeout: Maximum time to wait for email in seconds
        since_timestamp: Only look for emails sent after this timestamp (Unix epoch)
        recipient_email: If set, only consider emails sent to this address (avoids cross-test pollution when parallel)
        
    Returns:
        Verification code as string, or None if not found
    """
    credentials = _get_mailosaur_credentials()
    if not credentials:
        print(f"Mailosaur credentials not configured ({describe_sources(ITEST_CONFIG_REL_PATH, ITEST_CONFIG_ENV_VAR)})")
        return None

    server_id, api_key = credentials
    headers = _mailosaur_headers(api_key)
    start_time = time.time()

    for attempt in range(max_retries):
        if time.time() - start_time > timeout:
            print(f"Timeout waiting for email in server {server_id} after {time.time() - start_time:.1f}s")
            return None
        for data in _fetch_matching_messages(server_id, headers, recipient_email, since_timestamp):
            code = _extract_verification_code(_message_body_text(data))
            if code:
                print(f"Successfully extracted verification code: {code}")
                return code
        if attempt < max_retries - 1:
            time.sleep(retry_delay)

    print(f"Could not find verification code in server {server_id} after {max_retries} attempts")
    return None

def delete_mailosaur_message(message_id: str) -> bool:
    """
    Delete a Mailosaur message by ID.
    
    Args:
        message_id: Mailosaur message ID to delete
        
    Returns:
        True if deletion was successful, False otherwise
    """
    credentials = _get_mailosaur_credentials()
    if not credentials:
        print("Mailosaur credentials not configured")
        return False
    
    server_id, api_key = credentials
    
    # Create Basic Auth header
    auth_string = f"{api_key}:"
    auth_bytes = auth_string.encode('ascii')
    auth_b64 = base64.b64encode(auth_bytes).decode('ascii')
    headers = {"Authorization": f"Basic {auth_b64}"}
    
    try:
        delete_url = f"{MAILOSAUR_API_URL}/messages/{message_id}"
        response = requests.delete(delete_url, headers=headers, timeout=15)
        
        if response.status_code in [204, 200]:
            print(f"Deleted Mailosaur message: {message_id}")
            return True
        else:
            print(f"Failed to delete Mailosaur message {message_id}: {response.status_code}")
            return False
    except requests.exceptions.RequestException as e:
        print(f"Error deleting Mailosaur message {message_id}: {e}")
        return False


def delete_mailosaur_messages_for_email(email: str) -> int:
    """
    Delete all messages sent to a specific email address in a Mailosaur server.
    
    Note: Email addresses in Mailosaur are patterns, not deletable entities.
    This function deletes messages sent to the email address, not the address itself.
    
    Args:
        email: Email address to delete messages for
        
    Returns:
        Number of messages deleted
    """
    credentials = _get_mailosaur_credentials()
    if not credentials:
        print("Mailosaur credentials not configured")
        return 0
    
    server_id, api_key = credentials
    
    # Create Basic Auth header
    auth_string = f"{api_key}:"
    auth_bytes = auth_string.encode('ascii')
    auth_b64 = base64.b64encode(auth_bytes).decode('ascii')
    headers = {"Authorization": f"Basic {auth_b64}"}
    
    try:
        # Get all messages from the server and filter by recipient
        messages_url = f"{MAILOSAUR_API_URL}/messages"
        params = {"server": server_id}
        messages_response = requests.get(messages_url, headers=headers, params=params, timeout=15)
        
        if messages_response.status_code != 200:
            print(f"Failed to get messages for server {server_id}: {messages_response.status_code}")
            return 0
        
        messages_data = messages_response.json()
        all_messages = messages_data.get("items", [])
        
        # Filter messages sent to this specific email address
        matching_messages = []
        for msg in all_messages:
            # Check recipients - Mailosaur returns recipients as a list
            recipients = msg.get("to", [])
            for recipient in recipients:
                recipient_email = recipient.get("email", "") if isinstance(recipient, dict) else str(recipient)
                if recipient_email.lower() == email.lower():
                    matching_messages.append(msg)
                    break
        
        if not matching_messages:
            return 0
        
        # Delete each matching message
        deleted_count = 0
        for message in matching_messages:
            message_id = message.get("id")
            if message_id:
                if delete_mailosaur_message(message_id):
                    deleted_count += 1
        
        if deleted_count > 0:
            print(f"Deleted {deleted_count} message(s) for email {email}")
        
        return deleted_count
        
    except requests.exceptions.RequestException as e:
        print(f"Error deleting messages for email {email}: {e}")
        return 0

_email_service_available_cache = None

def is_email_service_available() -> bool:
    """Check if Mailosaur service is available.

    Only a successful (200) probe is cached. Transient failures — timeouts or
    throttling when many pytest-xdist workers probe at once under `-n` — are
    NOT cached, so one hiccup at worker startup can't poison the whole worker;
    the next call simply retries.
    """
    global _email_service_available_cache

    # Trust only a confirmed-available result from cache.
    if _email_service_available_cache is True:
        return True

    credentials = _get_mailosaur_credentials()
    if not credentials:
        return False

    server_id, api_key = credentials

    # Basic auth: API key as username, empty password (Mailosaur scheme).
    auth_b64 = base64.b64encode(f"{api_key}:".encode('ascii')).decode('ascii')
    headers = {"Authorization": f"Basic {auth_b64}"}
    url = f"{MAILOSAUR_API_URL}/servers/{server_id}"

    # Single probe (no retry). Cache only success so a transient miss doesn't
    # poison the worker; the next call simply re-probes.
    try:
        response = requests.get(url, headers=headers, timeout=15)
        if response.status_code == 200:
            _email_service_available_cache = True
            return True
        print(f"Mailosaur probe failed ({url}): HTTP {response.status_code}: {response.text[:300]}")
    except requests.exceptions.RequestException as e:
        print(f"Mailosaur probe failed ({url}): {type(e).__name__}: {e}")

    return False
