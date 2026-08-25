# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from test.itest.conftest import (
    API_GATEWAY_URL,
    END_USER_POOL_ID,
    IDENTITY_POOL_ID,
    IOT_ENDPOINT,
    MCP_API_URL,
    REGION,
    USER_API_GATEWAY_URL,
    cognito_hosted_login,
    esp_user_base_outputs,
    flow_id_from_cookie,
)
from test.itest.email_utils import generate_random_email, generate_test_password
from test.itest.mcp_oauth import (
    exchange_code_for_tokens,
    fetch_test_cimd_client_id,
    generate_pkce_pair,
    initiate_authorize,
)
from py_sdk.test_mcp import assert_matches_catalogue
from py_sdk.test_user import User
from urllib.parse import urlparse, parse_qs
import json
import pytest
import requests


# The OAuth proxy brokers to the ESP User OIDC issuer instead of the Cognito hosted UI.
# ESPUSER_ISSUER on the proxy Lambda is wired (src/rmng_core_stack.py) from the same SSM
# parameter that backs the EspUserDiscoveryIssuer output, so the host the proxy 302s to is
# exactly the host of this issuer. The proxy forwards MCP_OIDC_CLIENT_ID (seeded as the
# public PKCE client "mcp-oauth-client") as the redirect's client_id — NOT the incoming
# CIMD client_id (see handleAuthorize in mcp/proxy/handlers/mcp_oauth_proxy/mcp_oauth_proxy_main.go:
# serverClientID := os.Getenv("MCP_OIDC_CLIENT_ID"); params.Set("client_id", serverClientID)).
ESPUSER_ISSUER = (esp_user_base_outputs.get("EspUserDiscoveryIssuer") or "").rstrip("/")
MCP_OIDC_CLIENT_ID = esp_user_base_outputs.get("EspMcpClientId") or "mcp-oauth-client"


def test_protected_resource_metadata():
    """GET /.well-known/oauth-protected-resource returns correct RFC 9728 metadata."""
    response = requests.get(f"{MCP_API_URL}/.well-known/oauth-protected-resource")
    assert response.status_code == 200, f"Expected 200, got {response.status_code}. Response: {response.text}"

    data = response.json()
    assert "resource" in data, "Response should contain 'resource'"
    assert data["resource"].endswith("/v1/mcp"), \
        f"resource should end with /v1/mcp, got: {data['resource']}"
    assert "authorization_servers" in data, "Response should contain 'authorization_servers'"
    assert len(data["authorization_servers"]) >= 1, "Should have at least one authorization server"
    assert "bearer_methods_supported" in data, "Response should contain 'bearer_methods_supported'"
    assert "header" in data["bearer_methods_supported"]


def test_auth_server_metadata():
    """GET /.well-known/oauth-authorization-server returns correct RFC 8414 metadata."""
    response = requests.get(f"{MCP_API_URL}/.well-known/oauth-authorization-server")
    assert response.status_code == 200, f"Expected 200, got {response.status_code}. Response: {response.text}"

    data = response.json()
    assert "issuer" in data
    assert "authorization_endpoint" in data
    assert "token_endpoint" in data
    assert data["authorization_endpoint"].endswith("/oauth2/authorize"), \
        f"authorization_endpoint should end with /oauth2/authorize, got: {data['authorization_endpoint']}"
    assert data["token_endpoint"].endswith("/oauth2/token"), \
        f"token_endpoint should end with /oauth2/token, got: {data['token_endpoint']}"
    assert data.get("client_id_metadata_document_supported") is True, \
        "Should indicate CIMD support"
    assert "S256" in data.get("code_challenge_methods_supported", []), \
        "Should support S256 code challenge method"
    assert "authorization_code" in data.get("grant_types_supported", [])
    assert "refresh_token" in data.get("grant_types_supported", [])


def test_authorize_requires_pkce():
    """GET /oauth2/authorize without code_challenge returns error."""
    response = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
        "client_id": "https://example.com/cimd.json",
        "redirect_uri": "http://localhost:3000/callback",
        "response_type": "code",
        "state": "test-state",
    }, allow_redirects=False)
    assert response.status_code == 400, f"Expected 400, got {response.status_code}. Response: {response.text}"
    data = response.json()
    assert "code_challenge" in data.get("error", ""), \
        f"Error should mention code_challenge, got: {data}"


def test_authorize_redirect():
    """GET /oauth2/authorize with an un-hosted CIMD client_id fails CIMD validation (400)."""
    response = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
        "client_id": "https://example.com/cimd.json",
        "redirect_uri": "http://localhost:3000/callback",
        "response_type": "code",
        "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
        "code_challenge_method": "S256",
        "state": "test-state",
        "scope": "openid email",
    }, allow_redirects=False)
    # example.com doesn't host a CIMD doc, so CIMD validation must fail with 400.
    assert response.status_code == 400, \
        f"Expected 400 (CIMD validation failure), got {response.status_code}. Response: {response.text}"


def test_authorize_cimd_ssrf_private_ip_rejected():
    """H7 / SSRF: the proxy fetches the client_id URL server-side (CIMD). A client_id pointing at
    a private/link-local host (e.g. the cloud metadata IP) must be refused before any fetch, so the
    proxy can't be turned into an SSRF gateway to internal services."""
    for evil in (
        "https://169.254.169.254/latest/meta-data/",  # cloud metadata
        "https://127.0.0.1/cimd.json",                  # loopback
        "https://10.0.0.1/cimd.json",                   # RFC1918 private
    ):
        response = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
            "client_id": evil,
            "redirect_uri": "http://localhost:3000/callback",
            "response_type": "code",
            "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
            "code_challenge_method": "S256",
            "state": "ssrf-test",
            "scope": "openid email",
        }, allow_redirects=False)
        assert response.status_code >= 400, \
            f"SSRF client_id {evil} must be refused, got {response.status_code}: {response.text}"
        # And must NOT redirect anywhere (no brokering to the internal host).
        assert "Location" not in response.headers or response.status_code >= 400, \
            f"SSRF client_id {evil} must not be brokered: {response.headers.get('Location')}"


def test_token_endpoint_rejects_invalid_code():
    """POST /oauth2/token with a bad code returns an error brokered from the OIDC issuer."""
    response = requests.post(f"{MCP_API_URL}/oauth2/token", data={
        "grant_type": "authorization_code",
        "code": "invalid-code",
        "redirect_uri": "http://localhost:3000/callback",
        "client_id": "https://example.com/cimd.json",
        "code_verifier": "test-verifier",
    })
    # Either the OIDC token endpoint rejects the code (400) or the proxy cannot reach it (502).
    assert response.status_code in [400, 502], \
        f"Expected 400 or 502, got {response.status_code}. Response: {response.text}"


# ============================================================================
# CIMD-based OAuth flow integration tests
# ============================================================================


# ---------------------------------------------------------------------------
# OAuth flow helper functions
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.mark.xdist_group("env_mut")
def test_cimd_endpoint_returns_valid_document(enable_test_cimd):
    """GET /.well-known/test-cimd.json returns a valid CIMD document."""
    response = requests.get(f"{MCP_API_URL}/.well-known/test-cimd.json")
    assert response.status_code == 200, \
        f"Expected 200, got {response.status_code}. Response: {response.text}"

    doc = response.json()
    assert "client_id" in doc, "CIMD should contain client_id"
    assert doc["client_id"].endswith("/.well-known/test-cimd.json"), \
        f"client_id should end with /.well-known/test-cimd.json, got: {doc['client_id']}"
    assert "redirect_uris" in doc, "CIMD should contain redirect_uris"
    assert "http://localhost:3000/callback" in doc["redirect_uris"]
    assert doc.get("client_name") == "Integration Test Client"


@pytest.mark.xdist_group("env_mut")
def test_authorize_with_real_cimd(enable_test_cimd):
    """CIMD fetch -> validation -> 302 broker to the ESP User OIDC authorize endpoint."""
    if not ESPUSER_ISSUER:
        pytest.skip("EspUserDiscoveryIssuer output not present; redeploy espuser-base")

    # Step 1: Fetch the test CIMD to discover the dynamic client_id
    cimd_response = requests.get(f"{MCP_API_URL}/.well-known/test-cimd.json")
    assert cimd_response.status_code == 200, \
        f"Failed to fetch test CIMD: {cimd_response.text}"
    cimd_doc = cimd_response.json()
    client_id = cimd_doc["client_id"]

    # Step 2: Call /oauth2/authorize with the real CIMD client_id
    response = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
        "client_id": client_id,
        "redirect_uri": "http://localhost:3000/callback",
        "response_type": "code",
        "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
        "code_challenge_method": "S256",
        "state": "test-state-cimd",
        "scope": "openid email",
    }, allow_redirects=False)

    assert response.status_code == 302, \
        f"Expected 302 redirect, got {response.status_code}. Response: {response.text}"

    # Step 3: Parse and verify the Location header brokers to the ESP User OIDC
    # authorization_endpoint. The proxy resolves that endpoint from the issuer's
    # discovery document — it is the ESP User API Gateway host, NOT the S3 issuer host.
    disco = requests.get(f"{ESPUSER_ISSUER}/.well-known/openid-configuration").json()
    expected_authorize = urlparse(disco["authorization_endpoint"])
    location = response.headers.get("Location", "")
    parsed = urlparse(location)
    params = parse_qs(parsed.query)

    assert parsed.hostname == expected_authorize.hostname, \
        f"Redirect should point to the discovered authorize host " \
        f"{expected_authorize.hostname}, got: {parsed.hostname}"
    assert parsed.path == expected_authorize.path, \
        f"Redirect path should be {expected_authorize.path}, got: {parsed.path}"
    # The proxy substitutes MCP_OIDC_CLIENT_ID (mcp-oauth-client) for its own server-side
    # registry client — it does NOT forward the incoming CIMD client_id (handleAuthorize:
    # params.Set("client_id", serverClientID)).
    assert params.get("client_id") == [MCP_OIDC_CLIENT_ID], \
        f"Redirect client_id should be the server OIDC client {MCP_OIDC_CLIENT_ID}, " \
        f"got: {params.get('client_id')}"
    assert params.get("client_id") != [client_id], \
        "Proxy must not forward the incoming CIMD client_id to the OIDC issuer"
    assert params.get("response_type") == ["code"], \
        f"response_type should be 'code', got: {params.get('response_type')}"
    assert params.get("code_challenge") == ["E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"], \
        f"code_challenge mismatch: {params.get('code_challenge')}"
    assert params.get("code_challenge_method") == ["S256"], \
        f"code_challenge_method should be S256, got: {params.get('code_challenge_method')}"
    assert "state" in params, "Redirect should include state"
    assert "scope" in params, "Redirect should include scope"
    redirect_uri_values = params.get("redirect_uri", [""])
    assert redirect_uri_values[0].endswith("/oauth2/callback"), \
        f"redirect_uri should end with /oauth2/callback, got: {redirect_uri_values}"


@pytest.mark.xdist_group("env_mut")
def test_authorize_with_real_cimd_wrong_redirect_uri(enable_test_cimd):
    """Valid CIMD client_id but redirect_uri not in CIMD's allowed list returns 400."""
    # Fetch the test CIMD to get the real client_id
    cimd_response = requests.get(f"{MCP_API_URL}/.well-known/test-cimd.json")
    assert cimd_response.status_code == 200
    client_id = cimd_response.json()["client_id"]

    # Use a redirect_uri NOT in the CIMD's allowed list
    response = requests.get(f"{MCP_API_URL}/oauth2/authorize", params={
        "client_id": client_id,
        "redirect_uri": "https://evil.example.com/steal-tokens",
        "response_type": "code",
        "code_challenge": "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
        "code_challenge_method": "S256",
        "state": "test-state",
    }, allow_redirects=False)

    assert response.status_code == 400, \
        f"Expected 400, got {response.status_code}. Response: {response.text}"
    data = response.json()
    assert "redirect_uri not allowed" in data.get("error", ""), \
        f"Error should mention redirect_uri not allowed, got: {data}"


def _complete_federation_at_authorize(session, authorize_url, email, password):
    """Drive the CORE login leg of the brokered authorize: follow the proxy's redirect to
    /oauth2/authorize (which sets the esp_flow_id cookie), take the Cognito federation leg
    (hosted-UI password login), and return the /oauth2/callback URL the federation callback
    redirects the browser back to (carrying the authorization code + proxy state). Uses
    federation — not OTP — so this end-to-end proxy test runs on OSS deployments too."""
    authz = session.get(authorize_url, allow_redirects=False)
    assert authz.status_code == 302, f"espuser authorize should redirect to login: {authz.status_code} {authz.text}"
    flow_id = flow_id_from_cookie(authz)
    assert flow_id, "espuser authorize must set the esp_flow_id cookie"

    fed = session.get(f"{USER_API_GATEWAY_URL}/oauth2/federation/start",
                      params={"provider": "cognito"}, allow_redirects=False)
    assert fed.status_code == 302, f"federation start failed: {fed.status_code} {fed.text[:200]}"

    our_callback = cognito_hosted_login(session, fed.headers["Location"], email, password)
    cb = session.get(our_callback, allow_redirects=False)
    assert cb.status_code == 302, f"federation callback failed: {cb.status_code} {cb.text[:200]}"
    callback_url = cb.headers["Location"]
    assert "/oauth2/callback" in callback_url, \
        f"federation callback should redirect to the proxy callback, got: {callback_url}"
    return callback_url


@pytest.mark.xdist_group("env_mut")
def test_full_oauth_flow_with_real_cimd(enable_test_cimd, request):
    """End-to-end CIMD OAuth: /oauth2/authorize brokers to the ESP User OIDC authorize
    endpoint (server PKCE client + params intact), a passwordless email OTP login yields
    an authorization code, the proxy token endpoint exchanges it, and the resulting access
    token is accepted by the MCP server for an authenticated tools/list."""
    if not ESPUSER_ISSUER:
        pytest.skip("EspUserDiscoveryIssuer output not present; redeploy espuser-base")
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    code_verifier, code_challenge = generate_pkce_pair()
    print(f"\n[1/5] Generated PKCE pair: verifier={code_verifier[:16]}... challenge={code_challenge[:16]}...")

    # Discover client_id from the test CIMD
    client_id = fetch_test_cimd_client_id()
    print(f"[2/5] Fetched test CIMD client_id: {client_id}")

    # Initiate OAuth flow -> the proxy brokers to the ESP User OIDC authorize endpoint.
    oidc_authorize_url = initiate_authorize(client_id, code_challenge)
    print(f"[3/5] /oauth2/authorize returned 302 -> {oidc_authorize_url}")

    parsed = urlparse(oidc_authorize_url)
    params = parse_qs(parsed.query)
    _expected_authorize = urlparse(
        requests.get(f"{ESPUSER_ISSUER}/.well-known/openid-configuration").json()["authorization_endpoint"]
    )
    assert parsed.hostname == _expected_authorize.hostname, \
        f"Redirect should broker to the discovered authorize host " \
        f"{_expected_authorize.hostname}, got: {parsed.hostname}"
    assert parsed.path == _expected_authorize.path, \
        f"Redirect path should be {_expected_authorize.path}, got: {parsed.path}"
    # The proxy uses its own server registry client (mcp-oauth-client), not the CIMD id.
    assert params.get("client_id") == [MCP_OIDC_CLIENT_ID], \
        f"Redirect client_id should be {MCP_OIDC_CLIENT_ID}, got: {params.get('client_id')}"
    assert params.get("code_challenge") == [code_challenge], \
        f"PKCE challenge must pass through unchanged: {params.get('code_challenge')}"
    assert params.get("code_challenge_method") == ["S256"]
    assert params.get("response_type") == ["code"]
    assert params.get("redirect_uri", [""])[0].endswith("/oauth2/callback"), \
        f"redirect_uri should be the proxy callback, got: {params.get('redirect_uri')}"

    email = generate_random_email()
    password = generate_test_password()
    fed_user = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL,
                    USER_API_GATEWAY_URL, IOT_ENDPOINT, end_user_pool_id=END_USER_POOL_ID)
    fed_user.create_user_via_cognito(email=email, password=password)
    # Registered before the flow runs, so the account is removed even if an assertion below fails.
    request.addfinalizer(lambda: fed_user.delete_user_by_email(email))
    session = requests.Session()
    proxy_callback_url = _complete_federation_at_authorize(session, oidc_authorize_url, email, password)
    print(f"[4/5] federation login complete; proxy callback -> {proxy_callback_url}")

    # The proxy callback exchanges the OIDC code and 302s back to the CIMD redirect_uri
    # with the client-facing authorization code + the client's original state.
    callback = session.get(proxy_callback_url, allow_redirects=False)
    assert callback.status_code == 302, f"proxy callback should redirect to the client: {callback.text}"
    client_redirect = callback.headers.get("Location", "")
    auth_code = parse_qs(urlparse(client_redirect).query).get("code", [None])[0]
    assert auth_code, f"proxy callback must hand the client a code, got: {client_redirect}"

    # Exchange the code (with the PKCE verifier) at the proxy token endpoint.
    tokens = exchange_code_for_tokens(auth_code, code_verifier, client_id)
    access_token = tokens.get("access_token")
    assert access_token, f"token exchange must yield an access token: {tokens}"

    # The MCP server accepts the ESP User access token (aud == MCP_CLIENT_ID) for tools/list.
    resp = requests.post(
        f"{MCP_API_URL}/v1/mcp",
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {access_token}"},
        data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list"}),
    )
    assert resp.status_code == 200, f"authenticated tools/list failed ({resp.status_code}): {resp.text}"
    rpc = resp.json()
    assert rpc.get("error") is None, f"unexpected JSON-RPC error: {rpc.get('error')}"
    tool_names = [t["name"] for t in rpc["result"]["tools"]]
    # This test is about the token, not any one tool, so it checks the whole surface against the
    # committed catalogue — a tool rename then lands in one place instead of breaking it here.
    assert_matches_catalogue(tool_names)
    print(f"[5/5] MCP tools/list succeeded with the OTP-minted token; tools: {tool_names}")


# ============================================================================
# Third-party client interoperability test (FastMCP)
# ============================================================================

@pytest.mark.xdist_group("env_mut")
def test_fastmcp_client_oauth_interop(enable_test_cimd, request):
    """
    Interoperability test: FastMCP (third-party MCP client library) completes CIMD-based
    OAuth against our proxy end to end and performs an authenticated tools/list.

    The FastMCP client's transport is authenticated with the official MCP SDK's
    OAuthClientProvider (an httpx auth handler), so the whole spec-compliant client path
    runs against the proxy:
      1. POST /v1/mcp -> 401 triggers OAuth
      2. Discover PRM at /.well-known/oauth-protected-resource (RFC 9728)
      3. Discover AS metadata at /.well-known/oauth-authorization-server (RFC 8414)
      4. Detect client_id_metadata_document_supported=true, use CIMD (no DCR)
      5. /oauth2/authorize with CIMD client_id + SDK-generated PKCE (S256), which the
         proxy 302s to the ESP User OIDC issuer's authorize endpoint
      6. Code + state handed back to the SDK, token exchange at /oauth2/token, and an
         authenticated tools/list through the FastMCP client.

    ESP User login is passwordless email OTP, which FastMCP's default browser-open ->
    localhost-callback handshake cannot drive. The SDK's redirect_handler/callback_handler
    hooks exist for exactly this: the redirect handler stands in for the browser and drives
    the same Cognito federation leg as test_full_oauth_flow_with_real_cimd
    (_complete_federation_at_authorize), and the callback handler feeds the resulting
    code + state back to the SDK, which then exchanges tokens like any real client.
    """
    import asyncio
    from fastmcp import Client
    from mcp.client.auth import OAuthClientProvider
    from mcp.shared.auth import OAuthClientMetadata

    if not ESPUSER_ISSUER:
        pytest.skip("EspUserDiscoveryIssuer output not present; redeploy espuser-base")
    if not USER_API_GATEWAY_URL:
        pytest.skip("USER_API_GATEWAY_URL not configured")

    cimd_url = f"{MCP_API_URL}/.well-known/test-cimd.json"
    client_id = fetch_test_cimd_client_id()
    print(f"\n  [interop] test CIMD client_id: {client_id}")

    email = generate_random_email()
    password = generate_test_password()
    fed_user = User(email, password, REGION, IDENTITY_POOL_ID, API_GATEWAY_URL,
                    USER_API_GATEWAY_URL, IOT_ENDPOINT, end_user_pool_id=END_USER_POOL_ID)
    fed_user.create_user_via_cognito(email=email, password=password)
    # Registered before the flow runs, so the account is removed even if an assertion fails.
    request.addfinalizer(lambda: fed_user.delete_user_by_email(email))

    session = requests.Session()
    captured = {}

    async def redirect_handler(authorization_url):
        """Browser stand-in: the SDK hands us the authorize URL it would open; drive the
        proxy broker + federation login with requests and capture the client callback."""
        assert authorization_url.startswith(f"{MCP_API_URL}/oauth2/authorize"), \
            f"SDK should authorize at the proxy, got: {authorization_url}"
        sent = parse_qs(urlparse(authorization_url).query)
        assert sent.get("client_id") == [client_id], \
            f"SDK should use the CIMD URL as client_id (no DCR), got: {sent.get('client_id')}"

        authz = session.get(authorization_url, allow_redirects=False)
        assert authz.status_code == 302, \
            f"proxy authorize should broker to the OIDC issuer: {authz.status_code} {authz.text}"
        oidc_authorize_url = authz.headers["Location"]

        parsed = urlparse(oidc_authorize_url)
        brokered = parse_qs(parsed.query)
        _expected_authorize = urlparse(
            requests.get(f"{ESPUSER_ISSUER}/.well-known/openid-configuration").json()["authorization_endpoint"]
        )
        assert parsed.hostname == _expected_authorize.hostname, \
            f"CIMD authorize should broker to the discovered authorize host " \
            f"{_expected_authorize.hostname}, got: {parsed.hostname}"
        assert parsed.path == _expected_authorize.path
        assert brokered.get("client_id") == [MCP_OIDC_CLIENT_ID], \
            f"Redirect client_id should be {MCP_OIDC_CLIENT_ID}, got: {brokered.get('client_id')}"
        assert brokered.get("code_challenge") == sent.get("code_challenge"), \
            "SDK-generated PKCE challenge must pass through unchanged"
        assert brokered.get("code_challenge_method") == ["S256"]

        proxy_callback_url = _complete_federation_at_authorize(session, oidc_authorize_url, email, password)
        cb = session.get(proxy_callback_url, allow_redirects=False)
        assert cb.status_code == 302, f"proxy callback should redirect to the client: {cb.text}"
        client_redirect = cb.headers.get("Location", "")
        q = parse_qs(urlparse(client_redirect).query)
        captured["code"] = q.get("code", [None])[0]
        captured["state"] = q.get("state", [None])[0]
        assert captured["code"], f"proxy callback must hand the client a code, got: {client_redirect}"

    async def callback_handler():
        return captured["code"], captured["state"]

    class _MemoryTokenStorage:
        """Ephemeral TokenStorage: the interop test never persists tokens across runs."""
        _tokens = None
        _client_info = None

        async def get_tokens(self):
            return self._tokens

        async def set_tokens(self, tokens):
            self._tokens = tokens

        async def get_client_info(self):
            return self._client_info

        async def set_client_info(self, client_info):
            self._client_info = client_info

    provider = OAuthClientProvider(
        server_url=f"{MCP_API_URL}/v1/mcp",
        client_metadata=OAuthClientMetadata(
            client_name="Integration Test Client",
            redirect_uris=["http://localhost:3000/callback"],
            grant_types=["authorization_code", "refresh_token"],
            response_types=["code"],
            token_endpoint_auth_method="none",
            scope="openid email",
        ),
        storage=_MemoryTokenStorage(),
        redirect_handler=redirect_handler,
        callback_handler=callback_handler,
        client_metadata_url=cimd_url,
    )

    async def _list_tools():
        async with Client(f"{MCP_API_URL}/v1/mcp", auth=provider) as mcp_client:
            return [tool.name for tool in await mcp_client.list_tools()]

    tool_names = asyncio.run(_list_tools())
    assert_matches_catalogue(tool_names)
    print(f"  [interop] FastMCP authenticated tools/list succeeded; tools: {tool_names}")
