# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Client for the Mailosaur email testing service.

Every entry point takes the server ID and the API key, so this module holds no opinion about where
credentials live: the CLI reads its own config, the integration tests read theirs.
"""

import base64
import re
import time
from datetime import datetime
from typing import Optional

import requests

API_URL = "https://mailosaur.com/api"

_reachable_servers: set = set()


def headers(api_key: str) -> dict:
    """Basic-auth header for the Mailosaur API (the API key is the username)."""
    auth_b64 = base64.b64encode(f"{api_key}:".encode("ascii")).decode("ascii")
    return {"Authorization": f"Basic {auth_b64}"}


def address(prefix: str, server_id: str) -> str:
    """The address ``<prefix>@<server_id>.mailosaur.net``, with the prefix lowercased and DNS-safe."""
    return f"{prefix.lower().replace('_', '-')}@{server_id}.mailosaur.net"


def server_reachable(server_id: str, api_key: str) -> bool:
    """Probe the Mailosaur server once.

    Only success is cached. A timeout or a throttle when many pytest-xdist workers probe at once
    must not poison the worker, so the next call simply re-probes.
    """
    if server_id in _reachable_servers:
        return True
    url = f"{API_URL}/servers/{server_id}"
    try:
        response = requests.get(url, headers=headers(api_key), timeout=15)
        if response.status_code == 200:
            _reachable_servers.add(server_id)
            return True
        print(f"Mailosaur probe failed ({url}): HTTP {response.status_code}: {response.text[:300]}")
    except requests.exceptions.RequestException as e:
        print(f"Mailosaur probe failed ({url}): {type(e).__name__}: {e}")
    return False


def fetch_messages(server_id: str, api_key: str, recipient_email: Optional[str] = None,
                   since_timestamp: Optional[float] = None) -> list:
    """Return hydrated (full) messages for a server, newest first, filtered by recipient and arrival
    time. One poll, no retry — callers that wait for delivery loop over this. Returns [] on any API
    error.
    """
    hdrs = headers(api_key)
    try:
        resp = requests.get(f"{API_URL}/messages", headers=hdrs,
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
            full = requests.get(f"{API_URL}/messages/{mid}", headers=hdrs, timeout=15)
        except requests.exceptions.RequestException:
            continue
        if full.status_code == 200:
            hydrated.append(full.json())
    return hydrated


def message_body_text(data: dict) -> str:
    """Best-effort plaintext body of a hydrated message (text, else de-tagged HTML)."""
    text_data = data.get("text") or data.get("textBody")
    body = text_data.get("body", "") if text_data else ""
    if not body:
        html_data = data.get("html") or data.get("htmlBody")
        html_body = html_data.get("body", "") if html_data else ""
        if html_body:
            body = re.sub(r"<[^>]+>", "", html_body)
    return body


def extract_verification_code(body_text: str) -> Optional[str]:
    """Extract a numeric verification code from email body text.

    Codes are not all six digits. This repo's own sign-up template sends six, but
    Cognito's managed template for passwordless EMAIL_OTP sign-in ("Your authentication
    code is 48405273") sends eight, and a `\\d{6}` pattern silently returns the first
    six of those — a code that looks perfectly well-formed and that Cognito rejects as
    a CodeMismatchException, which reads like a broken OTP flow rather than a broken
    parser. Every pattern is therefore bounded by `\\b` on both sides, so a run of
    digits is matched whole or not at all.
    """
    if not body_text:
        return None

    code_patterns = [
        r"verification code is\s+\b(\d{4,8})\b",
        # Cognito's own managed template for passwordless EMAIL_OTP sign-in, configured
        # nowhere in this stack, so it needs its own pattern rather than falling
        # through to the generic ones below.
        r"authentication code is\s+\b(\d{4,8})\b",
        r"one-time password is\s+\b(\d{4,8})\b",
        r"code is\s+\b(\d{4,8})\b",
        r"code:\s*\b(\d{4,8})\b",
        r"verification code:\s*\b(\d{4,8})\b",
        r"\b(\d{4,8})\b",
    ]

    for pattern in code_patterns:
        match = re.search(pattern, body_text, re.IGNORECASE)
        if match:
            code = match.group(1)
            print(f"Found verification code: {code}")
            return code

    return None


def find_link(server_id: str, api_key: str, link_pattern: str,
              recipient_email: Optional[str] = None, since_timestamp: Optional[float] = None,
              max_retries: int = 8, retry_delay: float = 3.0) -> Optional[str]:
    """Read the latest matching link out of an inbox.

    Returns the first URL whose text matches link_pattern (a regex, searched against both the parsed
    links and the raw HTML/text body), or None.
    """
    pat = re.compile(link_pattern)
    for _ in range(max_retries):
        for data in fetch_messages(server_id, api_key, recipient_email, since_timestamp):
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


def follow_link(server_id: str, api_key: str, link_pattern: str,
                recipient_email: Optional[str] = None,
                since_timestamp: Optional[float] = None) -> bool:
    """Find a link in an inbox and GET it, the way a recipient would click it."""
    link = find_link(server_id, api_key, link_pattern,
                     recipient_email=recipient_email, since_timestamp=since_timestamp)
    if not link:
        return False
    try:
        requests.get(link, timeout=20)
    except requests.exceptions.RequestException as e:
        print(f"Failed to follow the link: {e}")
        return False
    return True


def read_verification_code(server_id: str, api_key: str, max_retries: int = 5,
                           retry_delay: float = 3.0, timeout: float = 60.0,
                           since_timestamp: Optional[float] = None,
                           recipient_email: Optional[str] = None) -> Optional[str]:
    """Wait for an email carrying a numeric code and return the code, or None on timeout."""
    start_time = time.time()
    for attempt in range(max_retries):
        if time.time() - start_time > timeout:
            print(f"Timeout waiting for email in server {server_id} after {time.time() - start_time:.1f}s")
            return None
        for data in fetch_messages(server_id, api_key, recipient_email, since_timestamp):
            code = extract_verification_code(message_body_text(data))
            if code:
                return code
        if attempt < max_retries - 1:
            time.sleep(retry_delay)
    print(f"Could not find verification code in server {server_id} after {max_retries} attempts")
    return None


def delete_message(server_id: str, api_key: str, message_id: str) -> bool:
    """Delete one message by ID."""
    try:
        response = requests.delete(f"{API_URL}/messages/{message_id}",
                                   headers=headers(api_key), timeout=15)
    except requests.exceptions.RequestException as e:
        print(f"Error deleting Mailosaur message {message_id}: {e}")
        return False
    if response.status_code in (200, 204):
        return True
    print(f"Failed to delete Mailosaur message {message_id}: {response.status_code}")
    return False


def delete_messages_for_email(server_id: str, api_key: str, email: str) -> int:
    """Delete every message sent to one address and return how many went.

    @note An address is a Mailosaur pattern, not a deletable entity; only its messages go.
    """
    deleted = 0
    for message in fetch_messages(server_id, api_key, recipient_email=email):
        message_id = message.get("id")
        if message_id and delete_message(server_id, api_key, message_id):
            deleted += 1
    return deleted
