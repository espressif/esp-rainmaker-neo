# MCP Server

## 1. Overview

The MCP server exposes a RainMaker Neo deployment to AI assistants over the
[Model Context Protocol](https://modelcontextprotocol.io). An assistant that speaks MCP —
Claude, or any other client — connects to one endpoint, authenticates the end user through
an OAuth flow, and can then find that user's devices, read their state, change it, and
manage their schedules, entirely within the permissions that user already has.

Two Lambdas sit behind a single HTTP API:

- **`mcp-server`** answers JSON-RPC 2.0 on `POST /v1/mcp`. It implements `initialize`,
  `notifications/initialized`, `tools/list` and `tools/call`, and nothing else.
- **`mcp-oauth-proxy`** serves the discovery and OAuth endpoints (`/.well-known/*`,
  `/oauth2/authorize`, `/oauth2/callback`, `/oauth2/token`), brokering end-user
  authentication to the ESP User OIDC issuer as the `mcp-oauth-client` registry client.

Both are deployed by the reusable `McpOAuthConstruct`
([src/mcp/handlers/core.py](../../../src/mcp/handlers/core.py)), wired up in
`rmng_core_stack.py`.

The server calls rmng's own Go packages in-process — the same `group`, `node` and service
code the REST API uses. It is not a client of the REST API, and it has no data of its own.

## 2. Why these tools

An MCP tool surface is a prompt, not just an API. The model chooses which tool to call from
the descriptions alone, so the surface is designed around the requests a user actually
makes rather than around the resources the backend happens to have.

In rmng every node belongs to **exactly one group** (the user's home) and optionally to
subgroups within it (rooms). Nothing exists outside a group. So a real request nearly always
starts from a human name — "the bedroom light" — and never from an id. Two consequences shape
the whole surface:

**Resolution must cost exactly one call.** Node-scoped tools need `group_id` as well as
`node_id`. A discovery tool that returned only `node_id` would force a second lookup for the
group, and a model that has learned to chain lookups will chain them everywhere. So
`list_devices` returns `group_id` on every row, and every description says so.

**Placement and state belong in the same answer.** "Which devices are in the kitchen" and "is
the kitchen light on" are one question asked twice. `list_devices` answers both, because a
node's group membership and its reported shadow are both reachable from its id.

Each description therefore names the sibling tool to use for every adjacent intent, and says
plainly when *not* to call the tool it describes. Those clauses are load-bearing: without
them models insert redundant lookup calls before every action.

Three further clauses were added after observed failures, and are covered by tests because they
are behaviour, not prose:

- **`set_params` bounds what a parameter is.** Given a request the device cannot serve, a model
  would otherwise invent a plausible key — `{"OTA": {"Trigger": true}}` for "run a firmware
  update" — which the cloud accepts and publishes to a device that ignores it, so the user is
  told something happened when nothing did. The description states that every device and
  parameter name must be one `list_devices` reported, and that a made-up key does nothing.
- **`set_params` hands timed requests to `set_schedule`.** "Every weekday at 7am" names a device
  and a state, which is enough to look like an immediate write. The description routes anything
  carrying a time, a delay or a repetition to `set_schedule`.
- **`list_devices` accepts a node id in `name`.** See the section below — an argument a model
  cannot fill correctly is a defect in the argument, not in the model.
- **`list_devices` and `list_groups` say what the server cannot do.** With no tool describing
  the boundary, "create a room" or "show me last month's energy" draws a speculative tool call
  instead of a straight answer. Both discovery tools now state that this server does not create,
  rename or move devices, homes or rooms, and does not read history.

A tool surface has no way to express "we do not do that", so the refusal has to live in the
description of the tool a model would otherwise reach for.

## 3. Tools

| Tool | Scope | Purpose |
|---|---|---|
| `list_devices` | user | Find devices by name, type, home or room; return placement and live state |
| `list_groups` | user | Homes and rooms — structure and device counts only |
| `list_schedules` | node | The schedules stored on one device |
| `set_params` | node(s) | Change device state now, one or many devices at a time |
| `set_schedule` | node | Add, edit, remove, enable or disable a schedule |

### list_devices

Reads the user's groups once — which also grants node permissions on the request context —
then fans out per device with [`parallel.ProcessParallel`](../../../src/utils/parallel/parallel.go):
one `node_details` read for the config and one shadow read for params and connectivity.
Filters that need no per-device I/O (`node_id`, `group_id`, `subgroup_id`) are applied first.

A device's user-visible name is **not** in its config: rmng's `NodeCfgDevice` carries an id
and a type, and the name lives in the `Name` parameter. The `name` filter therefore matches
against params as well as `config.info.name`, which is why shadows are read before filtering
rather than after.

`name` also matches the **node id**. That is not a semantic slip, it is the only thing that
works: a rmng node id is an arbitrary string with no distinguishing shape, so a model handed
`node_switch` cannot tell an id from a name, and the tool text — "call this first whenever the
user names a device" — points it at `name`. Documenting that `name` excludes ids does not teach
the model a distinction it has no way to draw; it only turns a resolvable request into an empty
result, which the model then reports to the user as "that device does not exist". Matching ids
costs one substring compare and removes the failure, so both argument descriptions say plainly
that an unclassifiable identifier belongs in `name`.

A device whose config or shadow cannot be read is still returned, with an `error` field. One
unreachable device must not blind the assistant to the rest of the home.

`fields` projects the response, accepting both top-level keys and dot paths
(`params.Light.Power`), and returns each value under the path it was asked for.

### list_groups

Structure only — ids, names, device counts, and node ids when `include_devices` is set. It
deliberately returns no parameters, connectivity or config, so there is exactly one tool that
answers questions about devices.

`subgroups` is always serialised, empty array included. An omitted key reads to an agent as
"rooms unknown" and earns a second call; `[]` says plainly that the home has no rooms. This is
why `subgroups` carries no `omitempty` while `node_ids` — which is genuinely opt-in behind
`include_devices` — does.

### set_params

Authorizes and publishes per node, and reports each node separately: a caller cannot smuggle
a foreign node through alongside their own, and one unreachable device does not hide the
writes that did land. When no node could be written the call is reported as a tool failure
rather than a partial success.

The write goes to the desired shadow, so success means published, not applied.

### list_schedules / set_schedule

rmng holds the authoritative schedule set in the cloud (`node_details`, service `schedule`),
unlike classic RainMaker where the firmware merges operations it is sent. `set_schedule` is
therefore a **read-modify-write**: it reads the current array, applies the operation in Go,
and writes the whole array back through `ScheduleService.Put`, which bumps the version and
pushes the full set to the device. `enable`/`disable` set the stored `enabled` flag; there is
no separate firmware operation. Concurrent edits to one node's schedules will clobber each
other, and the tool description says so.

Triggers are written in human terms and converted to the device's wire form: `time` in
`HH:MM` becomes `m`, minutes past midnight; `days` becomes `d`, a weekday bitmask with Monday
in the lowest bit (`daily` 127, `weekdays` 31, `weekends` 96). A trigger already in device
form, or a relative `rsec` trigger, passes through untouched so a listed schedule can be
echoed straight back.

## 4. Errors

The server distinguishes two kinds of failure, and clients must too.

- **Protocol failures** — unparseable body, wrong JSON-RPC version, unknown method, unknown
  tool, missing or wrong-audience token — are JSON-RPC errors (`-32700`, `-32600`, `-32601`,
  `-32602`, `-32001`). No argument change fixes these.
- **Tool failures** — a device the caller cannot reach, a missing argument, an unparseable
  trigger — are successful JSON-RPC responses whose result carries `isError: true` and a
  message written for the model to act on ("call list_schedules to find it").

That split is what lets an assistant recover in one turn instead of reporting a dead end.
Internal error detail never reaches the client; it is logged instead.

## 5. Authentication

End-user auth is OIDC only. Every request must carry `Authorization: Bearer <token>`, even
`initialize`, so a client discovers the OAuth flow on its very first call — an unauthenticated
request returns `401` with a `WWW-Authenticate: Bearer resource_metadata="..."` header
(RFC 9728). `initialize` and `notifications/initialized` do not *validate* the token;
`tools/list` and `tools/call` do.

Validation requires `aud == mcp-oauth-client`: a first-party app token must not authorize MCP
calls (RFC 9700 audience restriction). ID tokens are rejected; only access tokens are
accepted. The JWKS is read from SSM and cached in-process.

The authenticated subject becomes an ordinary `rmngctx.RmngContext`, so every RBAC check in
the DB layer applies exactly as it does for the REST API. Authorization is not re-implemented
here: `authorizeNodeForUser` is the single chokepoint that checks both group access and node
membership, and every node-scoped tool goes through it.

## 6. Tool catalogue

[docs/mcp/rainmaker-mcp.json](../../../docs/mcp/rainmaker-mcp.json) is a machine-readable
snapshot of the live tool registry: names, descriptions and input schemas. Offline consumers —
the eval framework, docs generators — read it as the answer to "what does this server expose",
so it must never drift from the code.

`TestToolCatalogMatchesSnapshot` regenerates the catalog from `createServer()` and
byte-compares it, failing the build on any divergence. Regenerate with:

```sh
make update-mcp-schema
```

The resulting diff is the review artifact for a tool change. Because the descriptions decide
how models behave, rewording one is a behaviour change and should be evaluated as such.

## 7. Related

- [Schedules](schedules.md) — the schedule service the MCP tools read and write
- [Device management](device_management.md) — node config and shadow structure
- [Group](group.md) — groups, subgroups and the access model MCP inherits
