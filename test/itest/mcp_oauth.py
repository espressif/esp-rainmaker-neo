# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""Shared helpers for driving the MCP OAuth (CIMD + PKCE) flow in integration tests.

The MCP server binds token validation to its dedicated ``mcp-oauth-client``
(see ``mcp/proxy/auth.go`` and ``mcp_oauth_construct.py``). A first-party app
token is minted for a *different* client of the same user pool and is now
rejected with 401 ("token was issued for a different client"). To call
``/v1/mcp``, tests must present a token minted through the OAuth proxy's
authorization-code flow.

These helpers run that flow programmatically (replacing the browser step with a
scripted Cognito hosted-UI login) and cache the resulting tokens per user, since
the flow is expensive. The token exchange yields both an ID token (carries
``custom:user_id`` → direct authorizer path) and an access token (no
``custom:user_id`` → AdminGetUser fallback path), so both authorizer paths stay
covered.
"""
from test.itest.conftest import MCP_API_URL
from urllib.parse import urlparse, parse_qs, urljoin
import threading
import time
import hashlib
import secrets
import base64
import requests


# ---------------------------------------------------------------------------
# OAuth flow steps
# ---------------------------------------------------------------------------

def generate_pkce_pair():
    """Generate a PKCE code_verifier and code_challenge (S256)."""
    raw = secrets.token_bytes(32)
    code_verifier = base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")
    digest = hashlib.sha256(code_verifier.encode("ascii")).digest()
    code_challenge = base64.urlsafe_b64encode(digest).rstrip(b"=").decode("ascii")
    return code_verifier, code_challenge


def fetch_test_cimd_client_id():
    """Fetch the test CIMD document and return its client_id."""
    resp = requests.get(f"{MCP_API_URL}/.well-known/test-cimd.json")
    assert resp.status_code == 200, f"Failed to fetch test CIMD: {resp.text}"
    return resp.json()["client_id"]


def initiate_authorize(client_id, code_challenge):
    """Call /oauth2/authorize and return the ESP User OIDC authorize URL from the 302 Location."""
    resp = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
        "client_id": client_id,
        "redirect_uri": "http://localhost:3000/callback",
        "response_type": "code",
        "code_challenge": code_challenge,
        "code_challenge_method": "S256",
        "state": "integration-test",
        "scope": "openid email",
    }, allow_redirects=False)
    assert resp.status_code == 302, f"Expected 302, got {resp.status_code}: {resp.text}"
    return resp.headers["Location"]


def complete_cognito_login(cognito_url, username, password):
    """Complete the Cognito hosted UI login in a headless browser and return the final
    localhost callback URL carrying the authorization code.

    A browser rather than a form POST: the hosted UI's sign-in submit requires the
    device-fingerprint fields its own JavaScript computes, and rejects a bare POST with 403.
    """
    playwright_sync = __import__("playwright.sync_api", fromlist=["sync_playwright"])

    with playwright_sync.sync_playwright() as pw:
        browser = pw.chromium.launch(headless=True)
        try:
            context = browser.new_context()
            page = context.new_page()
            page.goto(cognito_url, wait_until="domcontentloaded")
            if "amazoncognito.com" not in page.url:
                base = page.url.split("/oauth2/", 1)[0]
                page.goto(f"{base}/oauth2/federation/start?provider=cognito",
                          wait_until="domcontentloaded")
            assert "amazoncognito.com" in page.url, f"expected the hosted UI, on {page.url}"

            page.locator("#signInFormUsername").locator("visible=true").first.fill(username)
            page.locator("#signInFormPassword").locator("visible=true").first.fill(password)

            callback_holder = {}

            def _capture(request):
                if request.url.startswith("http://localhost:3000/callback"):
                    callback_holder["url"] = request.url

            page.on("request", _capture)
            page.locator("input[name=signInSubmitButton]").locator("visible=true").first.click()
            for _ in range(300):
                if "url" in callback_holder:
                    break
                page.wait_for_timeout(100)

            assert "url" in callback_holder, \
                f"did not reach the localhost callback after sign-in; on {page.url}"
            return callback_holder["url"]
        finally:
            browser.close()


def extract_auth_code(callback_url):
    """Extract the authorization code from a callback redirect URL."""
    params = parse_qs(urlparse(callback_url).query)
    codes = params.get("code", [])
    assert codes, f"No 'code' parameter in callback URL: {callback_url}"
    return codes[0]


def exchange_code_for_tokens(auth_code, code_verifier, client_id):
    """Exchange an authorization code for tokens via the OAuth proxy token endpoint."""
    resp = requests.post(f"{MCP_API_URL}/oauth2/token", data={
        "grant_type": "authorization_code",
        "code": auth_code,
        "redirect_uri": "http://localhost:3000/callback",
        "client_id": client_id,
        "code_verifier": code_verifier,
    })
    assert resp.status_code == 200, f"Token exchange failed ({resp.status_code}): {resp.text}"
    return resp.json()


def fetch_mcp_tokens(user):
    """Run the full CIMD + PKCE OAuth flow for ``user`` and return the token bundle.

    The bundle contains ``access_token`` and ``id_token`` (both minted for
    ``mcp-oauth-client``, so both pass the MCP authorizer's client binding).
    """
    code_verifier, code_challenge = generate_pkce_pair()
    client_id = fetch_test_cimd_client_id()
    cognito_url = initiate_authorize(client_id, code_challenge)
    callback_url = complete_cognito_login(cognito_url, user.username, user.password)
    auth_code = extract_auth_code(callback_url)
    return exchange_code_for_tokens(auth_code, code_verifier, client_id)


# ---------------------------------------------------------------------------
# Per-user token cache
# ---------------------------------------------------------------------------
# The flow (Cognito login + token exchange) is slow, and pooled users are reused
# across tests, so we cache the mcp-oauth-client tokens keyed by username and
# refresh only when near expiry. xdist workers are separate processes, so this
# cache is per-worker.
_TOKEN_CACHE = {}
_TOKEN_CACHE_LOCK = threading.Lock()
_EXPIRY_MARGIN_SECS = 300


def _get_cached_tokens(user):
    with _TOKEN_CACHE_LOCK:
        entry = _TOKEN_CACHE.get(user.username)
        if entry and time.time() < entry["expires_at"]:
            return entry

    # Run the flow outside the lock (slow network I/O); last writer wins, which
    # is harmless since every token for a user is equivalent.
    tokens = fetch_mcp_tokens(user)
    entry = {
        "access_token": tokens["access_token"],
        "id_token": tokens["id_token"],
        "expires_at": time.time() + tokens.get("expires_in", 3600) - _EXPIRY_MARGIN_SECS,
    }
    with _TOKEN_CACHE_LOCK:
        _TOKEN_CACHE[user.username] = entry
    return entry


def get_mcp_id_token(user):
    """mcp-oauth-client ID token (carries custom:user_id → direct authorizer path)."""
    return _get_cached_tokens(user)["id_token"]


def get_mcp_access_token(user):
    """mcp-oauth-client access token (no custom:user_id → AdminGetUser fallback path)."""
    return _get_cached_tokens(user)["access_token"]
