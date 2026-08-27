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

Two kinds of failure are asserted differently throughout. A *protocol* failure (unparseable body,
unknown method, unknown tool, missing or wrong-audience token) is a JSON-RPC error. A *tool*
failure (a node the caller cannot reach, a missing argument) is a successful JSON-RPC response
carrying isError, so the model can read the message and correct itself. Mixing the two up is the
bug these assertions exist to catch.
"""
import time
from urllib.parse import urlparse, parse_qs

import pytest
import requests

from py_sdk.test_group import Group
from py_sdk.test_mcp import Mcp, assert_matches_catalogue
from test.itest.conftest import MCP_API_URL, USER_API_GATEWAY_URL, pkce_pair, cognito_hosted_login
from test.itest.mcp_oauth import get_mcp_access_token

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
def mcp_client(super_admin_user):
    """Callable turning a pooled user into an authenticated MCP client, caching the token."""
    def _client(user):
        if not getattr(user, "_mcp_access_token", None):
            user._mcp_access_token = _mint_mcp_token(user, super_admin_user)
        return Mcp(MCP_API_URL, user._mcp_access_token)
    return _client


@pytest.fixture
def anon_mcp():
    """An MCP client with no credential, for the protocol-level rejection tests."""
    return Mcp(MCP_API_URL, None)


def _assert_protocol_error(response, code, status=200):
    """A protocol failure is a JSON-RPC error, never an isError tool result."""
    assert response.status_code == status, f"Expected {status}, got {response.status_code}: {response.text}"
    payload = response.json()
    assert payload.get("error") is not None, f"Expected a JSON-RPC error, got: {payload}"
    assert payload["error"]["code"] == code


def _shadow_name(device, group_id):
    """The named shadow the cloud uses for this device, which is group- and room-scoped."""
    name = f"params-{group_id}"
    for subgroup_id in sorted(getattr(device, "subgroup_ids", None) or []):
        name += f"-{subgroup_id}"
    return name


# --- Protocol level -----------------------------------------------------------------------

def test_mcp_get_returns_401():
    """GET /v1/mcp returns 401 indicating authentication is required."""
    response = requests.get(f"{MCP_API_URL}/v1/mcp")
    assert response.status_code == 401, f"Expected 401, got {response.status_code}. Response: {response.text}"


def test_mcp_unauthenticated_tools_list_returns_401(anon_mcp):
    """Unauthenticated tools/list returns 401."""
    _assert_protocol_error(anon_mcp.post("tools/list"), -32001, status=401)


def test_mcp_initialize_unauthenticated_returns_401(anon_mcp):
    """POST initialize without Bearer token returns 401 to trigger OAuth discovery."""
    _assert_protocol_error(anon_mcp.post("initialize"), -32001, status=401)


def test_mcp_tools_call_unauthenticated(anon_mcp):
    """POST tools/call without auth returns 401."""
    response = anon_mcp.post("tools/call", {"name": "list_groups", "arguments": {}})
    _assert_protocol_error(response, -32001, status=401)


def test_mcp_rejects_first_party_audience(test_user1, anon_mcp):
    """A first-party app token (aud=user-pool-client) must NOT authorize MCP calls — the MCP
    server is audience-confined to mcp-oauth-client (RFC 9700 audience restriction)."""
    response = anon_mcp.post("tools/list", token=test_user1.access_token)
    assert response.status_code == 401, \
        f"first-party token must be rejected at MCP, got {response.status_code}"


def test_mcp_initialize(test_user1, mcp_client):
    """POST initialize returns server info and capabilities with auth."""
    result = mcp_client(test_user1).rpc("initialize")
    assert result["protocolVersion"] == "2025-03-26"
    assert result["serverInfo"]["name"] == "rainmaker-mcp"
    assert result["serverInfo"]["version"] == "1.0.0"
    assert "tools" in result["capabilities"]


def test_mcp_notifications_initialized(test_user1, mcp_client):
    """POST notifications/initialized returns 200 with auth."""
    assert mcp_client(test_user1).post("notifications/initialized").status_code == 200


def test_mcp_invalid_json(anon_mcp):
    """POST with invalid JSON returns parse error (-32700)."""
    response = requests.post(f"{MCP_API_URL}/v1/mcp",
                             headers={"Content-Type": "application/json"},
                             data="not-valid-json")
    _assert_protocol_error(response, -32700)


def test_mcp_unknown_method(test_user1, mcp_client):
    """POST with unknown method returns method-not-found (-32601)."""
    _assert_protocol_error(mcp_client(test_user1).post("unknown/method"), -32601)


def test_mcp_unknown_tool(test_user1, mcp_client):
    """An unknown tool is a protocol error, not a tool error — the model asked for something
    that does not exist, which no argument change can fix."""
    response = mcp_client(test_user1).post("tools/call", {"name": "nonexistent_tool", "arguments": {}})
    _assert_protocol_error(response, -32602)


def test_mcp_invalid_jsonrpc_version(test_user1, mcp_client):
    """POST with wrong jsonrpc version returns invalid-request (-32600)."""
    response = mcp_client(test_user1).request({"jsonrpc": "1.0", "id": 1, "method": "initialize"})
    _assert_protocol_error(response, -32600)


def test_mcp_unsupported_http_method():
    """PUT /v1/mcp returns 405."""
    response = requests.put(f"{MCP_API_URL}/v1/mcp",
                            headers={"Content-Type": "application/json"},
                            data="{}")
    # HTTP API Gateway returns 404 for methods with no matching route,
    # REST API Gateway returns 403, and the Lambda itself returns 405
    assert response.status_code in [403, 404, 405], \
        f"Expected 403, 404, or 405, got {response.status_code}. Response: {response.text}"


# --- Tool catalogue -----------------------------------------------------------------------

def test_mcp_tools_list(test_user1, mcp_client):
    """tools/list advertises the whole surface, each tool described and schema'd."""
    tools = mcp_client(test_user1).list_tools()
    assert_matches_catalogue(tools)

    for name, tool in tools.items():
        assert tool.get("description"), f"{name} has no description"
        assert tool["inputSchema"]["type"] == "object", f"{name} has no object schema"


def test_mcp_tools_list_schemas(test_user1, mcp_client):
    """Every node-scoped tool demands both identifiers, and list_devices demands none."""
    tools = mcp_client(test_user1).list_tools()

    assert set(tools["list_schedules"]["inputSchema"]["required"]) == {"node_id", "group_id"}
    assert set(tools["set_params"]["inputSchema"]["required"]) == {"node_id", "group_id", "params"}
    assert set(tools["set_schedule"]["inputSchema"]["required"]) == {"node_id", "group_id", "operation"}
    assert tools["set_schedule"]["inputSchema"]["properties"]["operation"]["enum"] == \
        ["add", "edit", "remove", "enable", "disable"]

    # Discovery must never require an identifier: it is what the model calls when it has none.
    assert not tools["list_devices"]["inputSchema"].get("required")
    assert not tools["list_groups"]["inputSchema"].get("required")


def test_mcp_tool_descriptions_carry_their_guidance(test_user1, mcp_client):
    """The clauses below exist because models got these cases wrong: inventing parameters for
    unsupported actions, sending scheduled requests to set_params, and calling a tool for things
    this server cannot do at all. They are behaviour, so they are asserted like behaviour."""
    tools = mcp_client(test_user1).list_tools()

    set_params = tools["set_params"]["description"]
    assert "list_devices" in set_params
    assert "set_schedule" in set_params, "a timed request must be pointed at set_schedule"
    # Invented parameters are now refused rather than silently published, and the description has
    # to say so: a model told the write "does nothing" has no reason to read the error, while one
    # told it is rejected with the alternatives can fix the call in a single turn.
    assert "rejected" in set_params, "set_params must say an undeclared parameter is refused"
    assert "spec" in set_params, "set_params must point at spec, not params, for what it accepts"

    list_devices = tools["list_devices"]["description"]
    assert "spec" in list_devices, \
        "list_devices must distinguish what a device reports from what it will accept"

    for tool_name in ("list_devices", "list_groups"):
        description = tools[tool_name]["description"]
        assert "not available" in description, \
            f"{tool_name} must say what this server cannot do, so the model refuses instead of guessing"


@pytest.mark.xdist_group("env_mut")
def test_mcp_tools_list_with_access_token(test_user1, enable_test_cimd):
    """tools/list works with a Cognito access_token (AdminGetUser fallback)."""
    client = Mcp(MCP_API_URL, get_mcp_access_token(test_user1))
    assert_matches_catalogue(client.list_tools())


@pytest.mark.xdist_group("env_mut")
def test_mcp_tools_call_with_access_token(test_user1, enable_test_cimd):
    """A tool call resolves the same user through the AdminGetUser fallback, not just
    tools/list — an access token carries no custom:user_id claim to read."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Access Token Test Group")

    try:
        client = Mcp(MCP_API_URL, get_mcp_access_token(test_user1))
        groups = client.list_groups().json()["groups"]
        assert group_id in {g["group_id"] for g in groups}
    finally:
        group_api.delete_group(group_id)


# --- list_groups --------------------------------------------------------------------------

def test_mcp_list_groups(test_user1, mcp_client):
    """list_groups returns the caller's groups."""
    group_api = Group(test_user1)
    group_id_1 = group_api.create_group("MCP Test Group 1")
    group_id_2 = group_api.create_group("MCP Test Group 2")

    try:
        groups = mcp_client(test_user1).list_groups().json()["groups"]
        returned = {g["group_id"] for g in groups}
        assert {group_id_1, group_id_2} <= returned

        for group in groups:
            assert "group_name" in group
            assert "device_count" in group
            # Structure only: device state belongs to list_devices.
            assert "params" not in group
            assert "connected" not in group
    finally:
        group_api.delete_group(group_id_1)
        group_api.delete_group(group_id_2)


def test_mcp_list_groups_always_reports_subgroups(test_user1, mcp_client):
    """A home with no rooms still carries subgroups: [] — an absent key reads as "rooms
    unknown" and sends the agent back for another look."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Roomless Home")

    try:
        groups = mcp_client(test_user1).list_groups(group_id=group_id).json()["groups"]
        assert len(groups) == 1
        assert "subgroups" in groups[0], "a home with no rooms must still say so"
        assert groups[0]["subgroups"] == []
    finally:
        group_api.delete_group(group_id)


def test_mcp_list_groups_filters_by_name(test_user1, mcp_client):
    """A group name resolves to exactly one group, ignoring case."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Named Group")

    try:
        groups = mcp_client(test_user1).list_groups(group_name="mcp named group").json()["groups"]
        assert [g["group_id"] for g in groups] == [group_id]
    finally:
        group_api.delete_group(group_id)


def test_mcp_list_groups_include_devices(associated_device, mcp_client):
    """include_devices adds node ids; without it the response carries counts only."""
    device, group_id, test_user1, _ = associated_device
    client = mcp_client(test_user1)

    def group_row(**kwargs):
        groups = client.list_groups(group_id=group_id, **kwargs).json()["groups"]
        assert len(groups) == 1
        return groups[0]

    without = group_row()
    assert without["device_count"] >= 1
    assert "node_ids" not in without

    with_devices = group_row(include_devices=True)
    assert device.node_thing_name in with_devices["node_ids"]


def test_mcp_list_groups_unknown_name_is_tool_error(test_user1, mcp_client):
    """An unknown group name is something the model can retry differently, so it is a tool
    error carrying the name it could not find — not a protocol error."""
    result = mcp_client(test_user1).list_groups(group_name="No Such Home At All")
    assert result.is_error
    assert "No Such Home At All" in result.text


# --- list_devices -------------------------------------------------------------------------

def test_mcp_list_devices(associated_device, mcp_client):
    """One list_devices row carries placement and live state together."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"
    # online is reported by the firmware on connect, not written by the cloud, so a simulated
    # device has to publish it the way test_device_status and test_alexa do. update_named_shadow
    # keeps online at the top level and moves the rest into params.
    device.update_named_shadow(shadow_name, {
        "online": True,
        "Light": {"Name": "Reading Lamp", "Power": True, "Brightness": 75},
        "Switch": {"Power": False},
    })
    time.sleep(2)

    devices = mcp_client(test_user1).list_devices(node_id=node_id, group_id=group_id).json()["devices"]
    assert len(devices) == 1
    row = devices[0]

    # The whole point of the tool: one call yields the ids every other tool needs, plus state.
    assert row["node_id"] == node_id
    assert row["group_id"] == group_id
    assert row["group_name"]
    # Assert the state this test wrote round-trips, not that params holds nothing else: the
    # device is pooled, so an earlier test may have left keys behind that this one never sets.
    assert row["params"]["Light"] == {"Name": "Reading Lamp", "Power": True, "Brightness": 75}
    assert row["params"]["Switch"] == {"Power": False}
    assert row["connected"] is True


def test_mcp_list_devices_by_name(associated_device, mcp_client):
    """A device is findable by the name its user sees, which lives in the Name parameter."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name])
    device.update_named_shadow(shadow_name, {"Light": {"Name": "Porch Lantern", "Power": True}})
    time.sleep(2)

    client = mcp_client(test_user1)
    found = client.list_devices(group_id=group_id, name="porch").json()["devices"]
    assert [d["node_id"] for d in found] == [node_id]

    missing = client.list_devices(group_id=group_id, name="no-such-device-anywhere").json()
    assert missing["devices"] == [], "an unmatched filter is an empty list, not an error"
    assert missing["count"] == 0


def test_mcp_list_devices_by_node_id_passed_as_name(associated_device, mcp_client):
    """A node id passed as name resolves to the device.

    Node ids carry no distinguishing shape, so a model handed one cannot tell it from a name and
    puts it in name — the argument the tool text points at for "whatever the user called it".
    When that came back empty the model reported the device as non-existent, so the filter has to
    match ids too."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    found = mcp_client(test_user1).list_devices(name=node_id).json()["devices"]
    assert [d["node_id"] for d in found] == [node_id]


def test_mcp_list_devices_fields(associated_device, mcp_client):
    """fields narrows the response to exactly what was asked for, dot paths included."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name])
    device.update_named_shadow(shadow_name, {"Light": {"Power": True}})
    time.sleep(2)

    devices = mcp_client(test_user1).list_devices(
        node_id=node_id, group_id=group_id, fields="node_id,params.Light.Power").json()["devices"]
    assert devices == [{"node_id": node_id, "params.Light.Power": True}]


def test_mcp_list_devices_hides_other_users_devices(associated_device, test_user2, mcp_client):
    """user2 can neither list nor address user1's device."""
    device, group_id, _, _ = associated_device
    client = mcp_client(test_user2)

    listed = client.list_devices().json()["devices"]
    assert device.node_thing_name not in [d["node_id"] for d in listed]

    result = client.list_devices(node_id=device.node_thing_name)
    assert result.is_error
    assert device.node_thing_name in result.text


def test_mcp_list_devices_unauthorized_group(test_user1, test_user2, mcp_client):
    """A group the caller cannot reach is a tool error naming the group."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Unauthorized Devices Group")

    try:
        result = mcp_client(test_user2).list_devices(group_id=group_id)
        assert result.is_error
        assert group_id in result.text
    finally:
        group_api.delete_group(group_id)


# --- set_params ---------------------------------------------------------------------------

def test_mcp_set_params(associated_device, mcp_client):
    """set_params reaches the device over MQTT."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name]), "Failed to connect to shadow"

    params_topic = f"rainmaker/nodes/{node_id}/user/{shadow_name}/params"
    assert device.subscribe(topic=params_topic), "Failed to subscribe to params topic"

    set_data = {"Light": {"Power": True, "Brightness": 100}}
    result = mcp_client(test_user1).set_params(group_id, node_id, set_data).json()
    assert result["requested"] == 1
    assert result["succeeded"] == 1
    assert result["failed"] == 0
    assert result["results"] == [{"node_id": node_id, "success": True}]

    received = device.wait_for_params_message(timeout=10)
    assert received is not None, "Device did not receive params message"
    assert received == set_data


def test_mcp_set_params_reports_each_node(associated_device, mcp_client):
    """A foreign node in the list is reported as failed while the caller's own still lands."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name])
    params_topic = f"rainmaker/nodes/{node_id}/user/{shadow_name}/params"
    assert device.subscribe(topic=params_topic)

    set_data = {"Light": {"Power": False}}
    result = mcp_client(test_user1).set_params(
        group_id, f"{node_id},not-a-real-node", set_data).json()

    assert result["requested"] == 2
    assert result["succeeded"] == 1
    assert result["failed"] == 1
    outcomes = {r["node_id"]: r for r in result["results"]}
    assert outcomes[node_id]["success"] is True
    assert outcomes["not-a-real-node"]["success"] is False
    assert "not-a-real-node" in outcomes["not-a-real-node"]["error"]

    assert device.wait_for_params_message(timeout=10) == set_data


def test_mcp_set_params_rejects_a_parameter_the_device_never_declared(associated_device, mcp_client):
    """An invented parameter is refused, names the real ones, and reaches no device.

    set_params is a generic write, so a model can name anything. Before the cloud checked the
    node's config it published whatever it was handed: firmware ignored the unknown key, nothing
    acknowledged it either way, and the tool still reported succeeded=1. The user was told a
    change happened that never did — which is why this is asserted at the wire, not just in the
    response.
    """
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    # A config that positively declares its devices: with no `devices` key there is nothing to
    # contradict, and validation correctly stands aside (see test_mcp_set_params).
    assert device.set_node_config({
        "devices": [{
            "id": "Light",
            "type": "esp.device.lightbulb",
            "primary": "Power",
            "params": [
                {"id": "Power", "type": "esp.param.power", "data_type": "bool",
                 "properties": ["read", "write"]},
                {"id": "Brightness", "type": "esp.param.brightness", "data_type": "int",
                 "properties": ["read", "write"], "bounds": {"min": 0, "max": 100}},
            ],
        }],
        "info": {"fw_version": "1.0"},
    })

    shadow_name = _shadow_name(device, group_id)
    assert device.shadow_connect([shadow_name])
    params_topic = f"rainmaker/nodes/{node_id}/user/{shadow_name}/params"
    assert device.subscribe(topic=params_topic)

    client = mcp_client(test_user1)

    unknown_device = client.set_params(group_id, node_id, {"OTA": {"Trigger": True}})
    assert unknown_device.is_error
    assert "Light" in unknown_device.text, "the error must name the devices the node does have"

    unknown_param = client.set_params(group_id, node_id, {"Light": {"Hue": 120}})
    assert unknown_param.is_error
    assert "Brightness" in unknown_param.text, "the error must name the writable parameters"

    wrong_type = client.set_params(group_id, node_id, {"Light": {"Power": "on"}})
    assert wrong_type.is_error
    assert "boolean" in wrong_type.text

    out_of_bounds = client.set_params(group_id, node_id, {"Light": {"Brightness": 150}})
    assert out_of_bounds.is_error
    assert "0-100" in out_of_bounds.text

    # The point of rejecting before publishing: none of the four reached the device.
    assert device.wait_for_params_message(timeout=5) is None, \
        "a rejected write must not be published"

    # And a declared parameter still lands, so validation is not simply refusing everything.
    accepted = {"Light": {"Power": True, "Brightness": 80}}
    result = client.set_params(group_id, node_id, accepted).json()
    assert result["succeeded"] == 1
    assert device.wait_for_params_message(timeout=10) == accepted


def test_mcp_list_devices_reports_what_a_write_will_accept(associated_device, mcp_client):
    """spec tells the model what set_params validates against, so it can get it right first time.

    params is the reported shadow — current values — and a device may report a parameter its
    config never declared. Only spec is built from the config the write is checked against, so
    those two must not be confused: a light whose hue is named "H" will refuse "Hue".
    """
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name

    device.connect()
    device.get_group_info()
    assert device.set_node_config({
        "devices": [{
            "id": "Colour Light",
            "type": "esp.device.lightbulb",
            "primary": "Power",
            "params": [
                {"id": "Power", "type": "esp.param.power", "data_type": "bool",
                 "properties": ["read", "write"]},
                {"id": "H", "type": "esp.param.hue", "data_type": "int",
                 "properties": ["read", "write"], "bounds": {"min": 0, "max": 360}},
                {"id": "Temperature", "type": "esp.param.temperature", "data_type": "float",
                 "properties": ["read"]},
            ],
        }],
        "info": {"fw_version": "1.0"},
    })

    client = mcp_client(test_user1)
    devices = client.call_tool("list_devices", node_id=node_id).json()["devices"]
    spec = next(d for d in devices if d["node_id"] == node_id)["spec"]

    assert spec["Colour Light"]["H"] == "int 0-360, hue", \
        "the model needs the range and the meaning, since the name alone is unguessable"
    assert "Temperature" not in spec["Colour Light"], "a read-only parameter is not writable"

    # The name the spec gives is accepted; the one a model would guess is not.
    assert client.set_params(group_id, node_id, {"Colour Light": {"Hue": 120}}).is_error
    assert client.set_params(group_id, node_id, {"Colour Light": {"H": 120}}).json()["succeeded"] == 1


def test_mcp_set_params_unauthorized(test_user1, test_user2, mcp_client):
    """A device the caller cannot reach fails the call and says how to recover."""
    group_api = Group(test_user1)
    group_id = group_api.create_group("MCP Unauthorized Params Group")

    try:
        result = mcp_client(test_user2).set_params(group_id, "some-node", {"Light": {"Power": True}})
        assert result.is_error
        assert "list_devices" in result.text
    finally:
        group_api.delete_group(group_id)


def test_mcp_set_params_missing_args(test_user1, mcp_client):
    """A missing argument names itself, so the model can fix the call without guessing."""
    client = mcp_client(test_user1)

    missing_both = client.call_tool("set_params", params={"Light": {"Power": True}})
    assert missing_both.is_error
    assert "node_id" in missing_both.text and "group_id" in missing_both.text

    missing_params = client.call_tool("set_params", node_id="some-node", group_id="some-group")
    assert missing_params.is_error
    assert "params is required" in missing_params.text


# --- schedules ----------------------------------------------------------------------------

def test_mcp_schedule_lifecycle(associated_device, mcp_client):
    """add, list, edit, disable and remove, each reaching the device."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name
    client = mcp_client(test_user1)

    assert device.connect(), "Failed to connect device"

    try:
        created = client.set_schedule(
            group_id, node_id, "add",
            name="MCP Morning",
            triggers=[{"time": "07:00", "days": "weekdays"}],
            action={"Light": {"Power": True}},
        ).json()["schedule"]
        schedule_id = created["id"]

        # 07:00 is 420 minutes past midnight; weekdays is Mon-Fri = 1+2+4+8+16.
        assert created["triggers"] == [{"m": 420, "d": 31}]
        assert created["enabled"] is True

        listed = client.list_schedules(group_id, node_id).json()["schedules"]
        assert [s["id"] for s in listed] == [schedule_id]
        assert listed[0]["name"] == "MCP Morning"

        time.sleep(3)
        version = device.get_schedule_version()
        assert version is not None and version > 0
        pushed = device.get_schedule_details()
        assert pushed is not None
        assert [s["id"] for s in pushed.get("Schedules", [])] == [schedule_id]

        # An edit changes what it is given and leaves the rest of the schedule alone.
        client.set_schedule(group_id, node_id, "edit", schedule_id=schedule_id,
                            triggers=[{"time": "07:30", "days": "daily"}])
        listed = client.list_schedules(group_id, node_id).json()["schedules"]
        assert listed[0]["triggers"] == [{"m": 450, "d": 127}]
        assert listed[0]["name"] == "MCP Morning"
        assert listed[0]["action"] == {"Light": {"Power": True}}

        time.sleep(3)
        assert device.get_schedule_version() > version

        client.set_schedule(group_id, node_id, "disable", schedule_id=schedule_id)
        assert client.list_schedules(group_id, node_id).json()["schedules"][0]["enabled"] is False

        client.set_schedule(group_id, node_id, "enable", schedule_id=schedule_id)
        assert client.list_schedules(group_id, node_id).json()["schedules"][0]["enabled"] is True

        client.set_schedule(group_id, node_id, "remove", schedule_id=schedule_id)
        emptied = client.list_schedules(group_id, node_id).json()
        assert emptied["schedules"] == []
        assert emptied["count"] == 0
    finally:
        # Leave the pooled device without schedules whatever failed above.
        for row in client.list_schedules(group_id, node_id).json()["schedules"]:
            client.set_schedule(group_id, node_id, "remove", schedule_id=row["id"])


def test_mcp_set_schedule_rejects_unknown_schedule(associated_device, mcp_client):
    """Editing a schedule the node does not have points the model at list_schedules."""
    device, group_id, test_user1, _ = associated_device

    result = mcp_client(test_user1).set_schedule(
        group_id, device.node_thing_name, "edit", schedule_id="no-such-schedule")
    assert result.is_error
    assert "no-such-schedule" in result.text
    assert "list_schedules" in result.text


def test_mcp_set_schedule_validates_add(associated_device, mcp_client):
    """An incomplete or unreadable add says which field is at fault."""
    device, group_id, test_user1, _ = associated_device
    node_id = device.node_thing_name
    client = mcp_client(test_user1)

    without_name = client.set_schedule(
        group_id, node_id, "add",
        triggers=[{"time": "07:00", "days": "daily"}],
        action={"Light": {"Power": True}})
    assert without_name.is_error
    assert "name is required" in without_name.text

    bad_time = client.set_schedule(
        group_id, node_id, "add", name="Impossible",
        triggers=[{"time": "25:00", "days": "daily"}],
        action={"Light": {"Power": True}})
    assert bad_time.is_error
    assert "hours" in bad_time.text


def test_mcp_list_schedules_unauthorized(associated_device, test_user2, mcp_client):
    """user2 cannot read the schedules on user1's device."""
    device, group_id, _, _ = associated_device

    result = mcp_client(test_user2).list_schedules(group_id, device.node_thing_name)
    assert result.is_error


def test_mcp_list_schedules_missing_args(test_user1, mcp_client):
    """list_schedules names the identifier it is missing."""
    result = mcp_client(test_user1).call_tool("list_schedules", node_id="some-node")
    assert result.is_error
    assert "group_id is required" in result.text
