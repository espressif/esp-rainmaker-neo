# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

"""MCP JSON-RPC client for tests.

The MCP server speaks JSON-RPC 2.0 over a single POST endpoint, and it draws a line the tests
have to respect: a *protocol* failure (bad JSON, unknown method, unknown tool, bad token) comes
back as a JSON-RPC error, while a *tool* failure comes back as a successful response whose
result carries isError. ToolResult below keeps that distinction visible so a test cannot
accidentally assert one and get the other.

Minting the access token is deployment-specific and stays in the itest fixtures; this class
only needs the token itself.
"""

import json

import requests


class ToolResult:
    """The outcome of one tools/call."""

    def __init__(self, result: dict):
        self.raw = result
        self.is_error = bool(result.get("isError"))
        content = result.get("content") or []
        self.text = content[0]["text"] if content else ""

    def json(self):
        """Parse the text content, which every successful tool returns as a JSON document."""
        assert not self.is_error, f"tool reported an error: {self.text}"
        return json.loads(self.text)


class Mcp:
    """A JSON-RPC client for one authenticated MCP session."""

    def __init__(self, base_url: str, token: str):
        self.url = f"{base_url.rstrip('/')}/v1/mcp"
        self.token = token

    def request(self, body: dict, token=None):
        """POST a raw JSON-RPC body. Pass token to override or omit (None) the credential."""
        headers = {"Content-Type": "application/json"}
        bearer = self.token if token is None else token
        if bearer:
            headers["Authorization"] = f"Bearer {bearer}"
        return requests.post(self.url, headers=headers, data=json.dumps(body))

    def post(self, method: str, params=None, token=None):
        """Send one JSON-RPC request and return the raw HTTP response."""
        body = {"jsonrpc": "2.0", "id": 1, "method": method}
        if params is not None:
            body["params"] = params
        return self.request(body, token=token)

    def rpc(self, method: str, params=None):
        """Send a request that must succeed at the protocol level, and return its result."""
        response = self.post(method, params)
        assert response.status_code == 200, f"{method} failed: {response.status_code} {response.text}"
        payload = response.json()
        assert payload.get("error") is None, f"{method} returned a JSON-RPC error: {payload['error']}"
        return payload["result"]

    def list_tools(self):
        """Return the advertised tools, keyed by name."""
        return {tool["name"]: tool for tool in self.rpc("tools/list")["tools"]}

    def call_tool(self, tool_name: str, /, **arguments) -> ToolResult:
        """Invoke a tool. A tool-level failure is returned, not raised — assert on is_error.

        tool_name is positional-only: several tools take an argument called `name`, which would
        otherwise collide with this parameter.
        """
        return ToolResult(self.rpc("tools/call", {"name": tool_name, "arguments": arguments}))

    # --- one method per tool, so a rename lands in one place ---

    def list_devices(self, **filters):
        return self.call_tool("list_devices", **filters)

    def list_groups(self, **filters):
        return self.call_tool("list_groups", **filters)

    def list_schedules(self, group_id: str, node_id: str):
        return self.call_tool("list_schedules", group_id=group_id, node_id=node_id)

    def set_params(self, group_id: str, node_id: str, params: dict):
        return self.call_tool("set_params", group_id=group_id, node_id=node_id, params=params)

    def set_schedule(self, group_id: str, node_id: str, operation: str, **fields):
        return self.call_tool("set_schedule", group_id=group_id, node_id=node_id,
                              operation=operation, **fields)
