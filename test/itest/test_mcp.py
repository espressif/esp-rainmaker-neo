# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""MCP server itests.

The MCP server is an audience-confined resource server: it accepts only OUR access tokens whose
aud is the MCP OAuth client (mcp-oauth-client) — the audience third-party AI clients receive via
the MCP OAuth proxy. A first-party app token (aud user-pool-client) must NOT authorize MCP calls
(RFC 9700 audience restriction; see test_mcp_rejects_first_party_audience).

Authenticated tests therefore mint an MCP-audience token for the SAME subject as the pooled test
user, by driving the real flows end to end: our /oauth2/authorize as mcp-oauth-client → the
Cognito federation leg (hosted-UI password login with the pooled user's credentials) → our
authorization code → confidential code exchange at /oauth2/token. Identity resolution is by
verified email, so the resulting sub matches the pooled user's — groups they create via the SigV4
APIs are visible through MCP.
"""
from py_sdk.test_group import Group
from test.itest.conftest import MCP_API_URL, USER_API_GATEWAY_URL, pkce_pair, cognito_hosted_login
from test.itest.mcp_oauth import get_mcp_access_token
import json
import pytest
import requests
from urllib.parse import urlparse, parse_qs

MCP_CLIENT_ID = "mcp-oauth-client"

_mcp_client_secret = None


def _get_mcp_client_secret(super_admin):
    global _mcp_client_secret
    if _mcp_client_secret:
        return _mcp_client_secret
    resp = super_admin.list_oauth_clients(get_secret=True)
    assert resp.status_code == 200, f"listing clients failed: {resp.status_code} {resp.text}"
    row = next(c for c in resp.json()["clients"] if c["client_id"] == MCP_CLIENT_ID)
    _mcp_client_secret = row["client_secret"]
    return _mcp_client_secret




def _mint_mcp_token(user, super_admin):
    """Mint OUR access token with aud=mcp-oauth-client for `user`'s subject via the real flows
    (authorize → federation → hosted-UI login → code exchange). See module docstring."""
    verifier, challenge = pkce_pair()
    proxy_callback = f"{MCP_API_URL}/oauth2/callback"
    base = USER_API_GATEWAY_URL.rstrip("/")
    session = requests.Session()

    authz = session.get(f"{base}/oauth2/authorize", params={
        "client_id": MCP_CLIENT_ID, "redirect_uri": proxy_callback, "response_type": "code",
        "scope": "openid email", "state": "mcp-itest",
        "code_challenge": challenge, "code_challenge_method": "S256",
    }, allow_redirects=False)
    assert authz.status_code == 302, f"authorize failed: {authz.status_code} {authz.text[:300]}"

    # Deep-link the federation leg, so this works whether or not the deployment shows a chooser.
    fed = session.get(f"{base}/oauth2/federation/start", params={"provider": "cognito"},
                      allow_redirects=False)
    assert fed.status_code == 302, f"federation start failed: {fed.status_code} {fed.text[:300]}"

    # The callback's final redirect must not be followed: its Location carries our code.
    our_callback = cognito_hosted_login(session, fed.headers["Location"], user.username, user.password)
    cb = session.get(our_callback, allow_redirects=False)
    assert cb.status_code == 302, f"federation callback failed: {cb.status_code} {cb.text[:300]}"
    loc = cb.headers["Location"]
    assert loc.startswith(proxy_callback), f"unexpected post-login redirect: {loc}"
    code = parse_qs(urlparse(loc).query)["code"][0]

    tok = requests.post(f"{base}/oauth2/token",
                        auth=(MCP_CLIENT_ID, _get_mcp_client_secret(super_admin)),
                        data={"grant_type": "authorization_code", "code": code,
                              "redirect_uri": proxy_callback, "client_id": MCP_CLIENT_ID,
                              "code_verifier": verifier})
    assert tok.status_code == 200, f"code exchange failed: {tok.status_code} {tok.text[:300]}"
    return tok.json()["access_token"]


@pytest.fixture
def mcp_tokenize(super_admin_user):
    """Callable that stamps (and caches) an MCP-audience access token on a pooled user."""
    def _tokenize(user):
        if not getattr(user, "_mcp_access_token", None):
            user._mcp_access_token = _mint_mcp_token(user, super_admin_user)
        return user
    return _tokenize


def _mcp_request(token, body):
    """POST a JSON-RPC body to the MCP endpoint with the given Bearer token."""
    return requests.post(
        f"{MCP_API_URL}/v1/mcp",
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"},
        data=json.dumps(body),
    )


def _mcp_post(user, method, params=None):
    """Send a JSON-RPC request with the user's MCP-audience token.

    The MCP server accepts only tokens whose aud is mcp-oauth-client, so the user must have been
    passed through mcp_tokenize first — a first-party app token is rejected by design.
    """
    token = getattr(user, "_mcp_access_token", None)
    assert token, "user has no MCP token — pass the user through mcp_tokenize first"
    body = {"jsonrpc": "2.0", "id": 1, "method": method}
    if params is not None:
        body["params"] = params
    return _mcp_request(token, body)


def test_mcp_get_returns_401():
    """GET /v1/mcp returns 401 indicating authentication is required."""
    response = requests.get(f"{MCP_API_URL}/v1/mcp")
    assert response.status_code == 401, f"Expected 401, got {response.status_code}. Response: {response.text}"


def test_mcp_unauthenticated_tools_list_returns_401():
    """Unauthenticated tools/list returns 401."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data=body)
    assert response.status_code == 401

    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32001


def test_mcp_initialize_unauthenticated_returns_401():
    """POST initialize without Bearer token returns 401 to trigger OAuth discovery."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize"})
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data=body)
    assert response.status_code == 401

    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32001


def test_mcp_rejects_first_party_audience(test_user1):
    """A first-party app token (aud=user-pool-client) must NOT authorize MCP calls — the MCP
    server is audience-confined to mcp-oauth-client (RFC 9700 audience restriction)."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json",
                                      "Authorization": f"Bearer {test_user1.access_token}"},
                             data=body)
    assert response.status_code == 401, \
        f"first-party token must be rejected at MCP, got {response.status_code}"


def test_mcp_initialize(test_user1, mcp_tokenize):
    """POST initialize returns server info and capabilities with auth."""
    response = _mcp_post(mcp_tokenize(test_user1), "initialize")
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp.get("jsonrpc") == "2.0"
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    result = rpc_resp["result"]
    assert result["protocolVersion"] == "2025-03-26"
    assert result["serverInfo"]["name"] == "rainmaker-mcp"
    assert result["serverInfo"]["version"] == "1.0.0"
    assert "tools" in result["capabilities"]


def test_mcp_notifications_initialized(test_user1, mcp_tokenize):
    """POST notifications/initialized returns 200 with auth."""
    response = _mcp_post(mcp_tokenize(test_user1), "notifications/initialized")
    assert response.status_code == 200


def test_mcp_tools_list(test_user1, mcp_tokenize):
    """POST tools/list returns the get_groups tool when authenticated."""
    response = _mcp_post(mcp_tokenize(test_user1), "tools/list")
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    tools = rpc_resp["result"]["tools"]
    assert len(tools) >= 1, "Should have at least one tool"
    tool_names = [t["name"] for t in tools]
    assert "get_groups" in tool_names, f"get_groups tool not found in: {tool_names}"

    get_groups_tool = next(t for t in tools if t["name"] == "get_groups")
    assert "description" in get_groups_tool
    assert "inputSchema" in get_groups_tool


def test_mcp_tools_call_get_groups(test_user1, mcp_tokenize):
    """POST tools/call get_groups returns the user's groups."""
    group_api = Group(test_user1)
    group_id_1 = group_api.create_group("MCP Test Group 1")
    group_id_2 = group_api.create_group("MCP Test Group 2")

    try:
        # Call get_groups via MCP
        response = _mcp_post(mcp_tokenize(test_user1), "tools/call", {
            "name": "get_groups",
            "arguments": {}
        })
        assert response.status_code == 200

        rpc_resp = response.json()
        assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

        result = rpc_resp["result"]
        assert "content" in result
        assert len(result["content"]) == 1
        assert result["content"][0]["type"] == "text"

        groups = json.loads(result["content"][0]["text"])
        returned_group_ids = [g["group_id"] for g in groups]
        assert group_id_1 in returned_group_ids, f"Group {group_id_1} not found in MCP response"
        assert group_id_2 in returned_group_ids, f"Group {group_id_2} not found in MCP response"

        # Verify group structure
        for g in groups:
            assert "group_id" in g
            assert "group_name" in g
    finally:
        group_api.delete_group(group_id_1)
        group_api.delete_group(group_id_2)


def test_mcp_tools_list_unauthenticated():
    """POST tools/list without auth returns 401."""
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data=body)
    assert response.status_code == 401

    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32001


def test_mcp_tools_call_unauthenticated():
    """POST tools/call without auth returns 401."""
    body = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": "get_groups", "arguments": {}}
    })
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data=body)
    assert response.status_code == 401

    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32001


def test_mcp_invalid_json():
    """POST with invalid JSON returns parse error (-32700)."""
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data="not-valid-json")
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp["error"]["code"] == -32700


def test_mcp_unknown_method(test_user1, mcp_tokenize):
    """POST with unknown method returns method-not-found (-32601)."""
    response = _mcp_post(mcp_tokenize(test_user1), "unknown/method")
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp["error"]["code"] == -32601


def test_mcp_unknown_tool(test_user1, mcp_tokenize):
    """POST tools/call with unknown tool returns invalid-params (-32602)."""
    response = _mcp_post(mcp_tokenize(test_user1), "tools/call", {
        "name": "nonexistent_tool",
        "arguments": {}
    })
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp["error"]["code"] == -32602


def test_mcp_invalid_jsonrpc_version(test_user1, mcp_tokenize):
    """POST with wrong jsonrpc version returns invalid-request (-32600)."""
    user = mcp_tokenize(test_user1)
    response = _mcp_request(user._mcp_access_token,
                            {"jsonrpc": "1.0", "id": 1, "method": "initialize"})
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp["error"]["code"] == -32600


def test_mcp_unsupported_http_method():
    """PUT /v1/mcp returns 405."""
    response = requests.put(f"{MCP_API_URL}/v1/mcp",
                            headers={"Content-Type": "application/json"},
                            data="{}")
    # HTTP API Gateway returns 404 for methods with no matching route,
    # REST API Gateway returns 403, and the Lambda itself returns 405
    assert response.status_code in [403, 404, 405], \
        f"Expected 403, 404, or 405, got {response.status_code}. Response: {response.text}"


def _mcp_post_with_access_token(user, method, params=None):
    """Helper to send a JSON-RPC request using the mcp-oauth-client access_token (not id_token).

    This exercises the AdminGetUser fallback path since Cognito access tokens
    don't contain the custom:user_id claim.
    """
    body = {"jsonrpc": "2.0", "id": 1, "method": method}
    if params is not None:
        body["params"] = params
    return _mcp_request(get_mcp_access_token(user), body)


@pytest.mark.xdist_group("env_mut")
def test_mcp_tools_list_with_access_token(test_user1, enable_test_cimd):
    """POST tools/list works with a Cognito access_token (AdminGetUser fallback)."""
    response = _mcp_post_with_access_token(test_user1, "tools/list")
    assert response.status_code == 200, \
        f"Expected 200, got {response.status_code}. Response: {response.text}"

    rpc_resp = response.json()
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    tools = rpc_resp["result"]["tools"]
    tool_names = [t["name"] for t in tools]
    assert "get_groups" in tool_names


@pytest.mark.xdist_group("env_mut")
def test_mcp_tools_call_get_groups_with_access_token(test_user1, enable_test_cimd):
    """POST tools/call get_groups works with a Cognito access_token (AdminGetUser fallback)."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Access Token Test Group")

    try:
        response = _mcp_post_with_access_token(test_user1, "tools/call", {
            "name": "get_groups",
            "arguments": {}
        })
        assert response.status_code == 200

        rpc_resp = response.json()
        assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

        result = rpc_resp["result"]
        groups = json.loads(result["content"][0]["text"])
        returned_group_ids = [g["group_id"] for g in groups]
        assert group_id in returned_group_ids, \
            f"Group {group_id} not found in MCP access_token response"
    finally:
        group_api.delete_group(group_id)


def test_mcp_tools_list_includes_param_tools(test_user1, mcp_tokenize):
    """POST tools/list returns get_params and set_params tools."""
    response = _mcp_post(mcp_tokenize(test_user1), "tools/list")
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    tools = rpc_resp["result"]["tools"]
    tool_names = [t["name"] for t in tools]
    assert "get_params" in tool_names, f"get_params not found in: {tool_names}"
    assert "set_params" in tool_names, f"set_params not found in: {tool_names}"

    # Verify get_params schema has required fields
    get_params_tool = next(t for t in tools if t["name"] == "get_params")
    assert "group_id" in get_params_tool["inputSchema"]["properties"]
    assert "node_id" in get_params_tool["inputSchema"]["properties"]
    assert "group_id" in get_params_tool["inputSchema"]["required"]
    assert "node_id" in get_params_tool["inputSchema"]["required"]

    # Verify set_params schema has required fields
    set_params_tool = next(t for t in tools if t["name"] == "set_params")
    assert "group_id" in set_params_tool["inputSchema"]["properties"]
    assert "node_id" in set_params_tool["inputSchema"]["properties"]
    assert "params" in set_params_tool["inputSchema"]["properties"]
    assert "params" in set_params_tool["inputSchema"]["required"]


def test_mcp_get_params(associated_device, mcp_tokenize):
    """POST tools/call get_params returns the device's reported shadow params."""
    device, group_id, test_user1, user1_group_api = associated_device
    node_id = device.node_thing_name

    # Connect device and set up shadow with param data
    device.connect()
    device.get_group_info()

    shadow_name = f"params-{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        for subgroup_id in sorted(device.subgroup_ids):
            shadow_name += f"-{subgroup_id}"

    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"
    device.update_named_shadow(shadow_name, {
        "Light": {"Power": True, "Brightness": 75},
        "Switch": {"Power": False},
    })

    import time
    time.sleep(2)

    # Call get_params via MCP
    response = _mcp_post(mcp_tokenize(test_user1), "tools/call", {
        "name": "get_params",
        "arguments": {"group_id": group_id, "node_id": node_id}
    })
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    result = rpc_resp["result"]
    assert "content" in result
    assert len(result["content"]) == 1
    assert result["content"][0]["type"] == "text"

    params = json.loads(result["content"][0]["text"])
    expected = {
        "Light": {"Power": True, "Brightness": 75},
        "Switch": {"Power": False},
    }
    assert params == expected, f"Expected {expected}, got {params}"


def test_mcp_set_params(associated_device, mcp_tokenize):
    """POST tools/call set_params publishes params to the device."""
    device, group_id, test_user1, user1_group_api = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()

    shadow_name = f"params-{group_id}"
    if hasattr(device, 'subgroup_ids') and device.subgroup_ids:
        for subgroup_id in sorted(device.subgroup_ids):
            shadow_name += f"-{subgroup_id}"

    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"

    # Subscribe to the params topic so we can verify delivery
    params_topic = f"rainmaker/nodes/{node_id}/user/{shadow_name}/params"
    assert device.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    # Call set_params via MCP
    set_data = {
        "Light": {"Power": True, "Brightness": 100},
    }
    response = _mcp_post(mcp_tokenize(test_user1), "tools/call", {
        "name": "set_params",
        "arguments": {
            "group_id": group_id,
            "node_id": node_id,
            "params": set_data,
        }
    })
    assert response.status_code == 200

    rpc_resp = response.json()
    assert rpc_resp.get("error") is None, f"Unexpected error: {rpc_resp.get('error')}"

    result = rpc_resp["result"]
    content_text = result["content"][0]["text"]
    assert "success" in content_text, f"Expected success in response: {content_text}"

    # Verify the device received the params on the subscribed topic
    received = device.wait_for_params_message(timeout=10)
    assert received is not None, "Device did not receive params message"
    assert received == set_data, f"Expected {set_data}, got {received}"


def test_mcp_get_params_unauthorized_group(test_user1, test_user2, mcp_tokenize):
    """get_params returns error when user doesn't have access to the group."""
    # user1 creates a group
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Unauthorized Params Group")

    try:
        # user2 tries to read params from user1's group
        response = _mcp_post(mcp_tokenize(test_user2), "tools/call", {
            "name": "get_params",
            "arguments": {"group_id": group_id, "node_id": "some-node"}
        })
        assert response.status_code == 200

        rpc_resp = response.json()
        assert rpc_resp.get("error") is not None, "Expected error for unauthorized group access"
        assert rpc_resp["error"]["code"] == -32603
        assert "Failed" in rpc_resp["error"]["message"]
    finally:
        group_api.delete_group(group_id)


def test_mcp_get_params_missing_args(test_user1, mcp_tokenize):
    """get_params returns error when required arguments are missing."""
    user = mcp_tokenize(test_user1)
    # Missing both group_id and node_id
    response = _mcp_post(user, "tools/call", {
        "name": "get_params",
        "arguments": {}
    })
    assert response.status_code == 200
    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32602

    # Missing node_id
    response = _mcp_post(user, "tools/call", {
        "name": "get_params",
        "arguments": {"group_id": "some-group"}
    })
    assert response.status_code == 200
    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32602


def test_mcp_set_params_missing_args(test_user1, mcp_tokenize):
    """set_params returns error when required arguments are missing."""
    # Missing params
    response = _mcp_post(mcp_tokenize(test_user1), "tools/call", {
        "name": "set_params",
        "arguments": {"group_id": "some-group", "node_id": "some-node"}
    })
    assert response.status_code == 200
    rpc_resp = response.json()
    assert rpc_resp.get("error") is not None
    assert rpc_resp["error"]["code"] == -32602
