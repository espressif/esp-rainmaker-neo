// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	mcpserver "mcp-server"
)

// Every tool's name, description and schema lives here rather than beside its handler. The
// descriptions cross-reference each other — each one tells the model which sibling tool to
// reach for instead — so they only stay coherent if they are reviewed as a set.
//
// They are also the contract the eval framework measures, mirrored into
// docs/mcp/rainmaker-mcp.json and pinned by schema_snapshot_test.go. Reword one and the
// snapshot test fails until the catalog is regenerated with `make update-mcp-schema`; that
// is deliberate, because a description change is a behaviour change.
func registerTools(server *mcpserver.Server) {
	server.RegisterTool(
		mcpserver.Tool{
			Name: "list_devices",
			Description: `Find ESP RainMaker devices and read their current state. Every device belongs to exactly one group (the user's home) and may sit in subgroups within it (rooms); this tool returns that placement together with the device's live parameters, so one call answers both "which devices are in the kitchen" and "is the kitchen light on".

Call this first whenever the user names a device, a type of device, or a room instead of giving ids. It is the only tool that turns names into ids, and every row carries both the node_id and the group_id that the other tools require — so one call is always enough. Never follow it with a second lookup to find the group.

Each row carries two different things about parameters, and they are not interchangeable. params is what the device is reporting right now, which is what you read to answer a question about its state. spec is what the device will accept a write for — parameter name to type, range and meaning — and it is what set_params checks against, so it is the one to consult before changing anything. A device names its own parameters, so a colour light may call its hue "H"; spec is where you find that out.

Do not call it before set_params or set_schedule when the user has already given you a node_id and group_id, and do not call it to inspect schedules — list_schedules does that.

Filters combine: group_id or subgroup_id to narrow to a home or room, name or type for a kind of device. Node ids look like any other string, so when the user gives you an identifier you cannot classify, put it in name — that matches node ids as well as names, and never comes back empty just because the value turned out to be an id. Reading state fetches each device's shadow, so when you do not need the whole payload pass fields — for example "node_id,group_id,connected" or "params.Light.Power".

This server reads and controls devices that already exist, and shows only their state as of now. It cannot add, remove or rename a device, create or rename a home or room, move a device between rooms, or report history, trends or past usage. When the user asks for any of that, tell them it is not available here — there is no tool for it, and no combination of these tools does it.

In replies to the user prefer the human names in params.<Device>.Name; use ids only in tool calls.`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "One node id, or several comma-separated, when you know the values are node ids. Omit to search every device the user can reach. If you are not sure whether a string is a node id or a name, pass it as name instead — that matches ids too.",
					},
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "Restrict to one group (home). Get it from this tool or from list_groups.",
					},
					"subgroup_id": map[string]interface{}{
						"type":        "string",
						"description": "Restrict to one subgroup (room) within the group. Get it from list_groups.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Match whatever the user called the device, partially and ignoring case: the Name parameter they see in the app, the node's own name, or its node id. Safe to use for any identifier you were given but cannot classify.",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Match a device type, partially and ignoring case. Accepts a bare word (light, switch, sensor) or a full type such as esp.device.lightbulb.",
					},
					"fields": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated fields to return instead of the whole record. Top level: node_id, group_id, group_name, subgroup_ids, subgroup_names, name, type, model, fw_version, connected, params, spec, config. Dot paths reach inside: params.Light.Power. Keep spec whenever the next step is a write — it is what set_params validates against.",
					},
				},
			},
		},
		handleListDevices,
	)

	server.RegisterTool(
		mcpserver.Tool{
			Name: "list_groups",
			Description: `List the groups and subgroups that organise the user's devices — their homes and the rooms inside them — with the number of devices in each. Use it for questions about structure ("what rooms do I have?", "which homes can I see?") and to turn a group name into a group_id.

This tool describes placement only. It never returns parameters, connectivity or device configuration, and the node ids it can list carry no names or types. For anything about the devices themselves — including which devices are in a given room — call list_devices with a group_id or subgroup_id filter instead.

Set include_devices when you need the node ids in each group and subgroup but not their state. subgroups is always present: an empty array means this home genuinely has no rooms, so take it at face value rather than looking again.

This tool only reads. Nothing here creates, renames or deletes a home or a room, moves a device between them, or reports history — if the user asks for that, say it is not available rather than reaching for another tool.`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "Return only this group. Omit both group_id and group_name to list them all.",
					},
					"group_name": map[string]interface{}{
						"type":        "string",
						"description": "Return only the group with this name, ignoring case. Do not pass it together with group_id.",
					},
					"include_devices": map[string]interface{}{
						"type":        "boolean",
						"description": "Add the node ids belonging to each group and subgroup. Defaults to false, which returns counts only.",
					},
				},
			},
		},
		handleListGroups,
	)

	server.RegisterTool(
		mcpserver.Tool{
			Name: "list_schedules",
			Description: `List the schedules stored on one device: each schedule's id, name, enabled flag, triggers and the parameters it applies. ESP RainMaker keeps schedules per device, so ask about one node at a time.

Call this to turn a schedule the user described rather than named — "the morning alarm", "the one that shuts the porch light off" — into the schedule_id that set_schedule needs. Do not use list_devices for that; schedules are not part of a device's parameters.

Triggers come back in the device's own form: m is minutes past midnight and d is a weekday bitmask with Monday as the lowest bit. Translate them when you answer the user — m 420 with d 62 means 07:00 on weekdays.`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "The device whose schedules to list.",
					},
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "The group the device belongs to. list_devices returns it alongside the node_id.",
					},
				},
				Required: []string{"node_id", "group_id"},
			},
		},
		handleListSchedules,
	)

	server.RegisterTool(
		mcpserver.Tool{
			Name: "set_params",
			Description: `Change what a device is doing right now — switch it on or off, set brightness or temperature, or reboot or factory reset it where the device exposes a parameter for that. This is a direct action tool: call it as soon as the user asks for a change and you hold the ids.

If the user gave you node_id and group_id, act on them immediately. Only call list_devices first when the device is identified by name, type or room and you do not have the ids yet. To act on several devices in one go, pass their ids comma-separated in node_id; they must all belong to the group in group_id, and the same params are applied to every one of them, so only batch devices that share the same device names.

params is keyed by the device name inside the node and then by parameter: {"Light": {"Power": true, "Brightness": 80}}. Names are matched exactly and case-sensitively against the device's own configuration, and a write that names a device or parameter the device did not declare is rejected in full — nothing is sent. The rejection lists the parameters the device does have, so if you are unsure, send the call and read the error rather than asking the user. There is no {"OTA": {"Trigger": true}} that starts a firmware update and no key that adds a capability the device never reported; if nothing covers what the user asked for, tell them this device does not support it.

The spec field on each list_devices row is what a device accepts — parameter name to type, range and meaning, as in {"Colour Light": {"H": "int 0-360, hue", "V": "int 0-100, brightness"}}. Use it rather than the params field, which is only what the device currently reports: a light whose hue is named "H" will not accept "Hue". Values are checked too — send real booleans for on/off parameters, not "true" or "on", and keep numbers inside the stated range.

Do not use this to read state — list_devices does that — and do not use it for anything that happens later. If the request carries a time, a delay or a repetition ("at 7am", "every weekday", "in ten minutes", "every night"), it belongs to set_schedule, even when it also names a device and the state to put it in.

Delivery to the device is asynchronous: success means the change was accepted and published, not that the device has already applied it.`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "One node id, or several comma-separated to apply the same change to each.",
					},
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "The group every listed device belongs to. list_devices returns it alongside the node_id.",
					},
					"params": map[string]interface{}{
						"type":        "object",
						"description": "Device name to parameter name to value: {\"Light\": {\"Power\": true, \"Brightness\": 80}}. Case-sensitive. Both names must appear in the device's spec from list_devices; a write naming anything else, or a value of the wrong type or out of range, is rejected in full and the error names what the device does accept.",
					},
				},
				Required: []string{"node_id", "group_id", "params"},
			},
		},
		handleSetParams,
	)

	server.RegisterTool(
		mcpserver.Tool{
			Name: "set_schedule",
			Description: `Create, change, remove, enable or disable a schedule on one device. This is a direct action tool: call it as soon as the user asks for something to happen at a time or on a repeating basis, including loosely specified requests like "wake me up with the lights" or "turn the heater off overnight" — choose sensible values rather than asking the user for the wire format.

operation is one of add, edit, remove, enable, disable. add needs name, triggers and action; every other operation needs schedule_id. When the user refers to a schedule by description instead of by id, call list_schedules to resolve it — never list_devices.

Write triggers in human terms and this tool converts them: {"time": "07:00", "days": "weekdays"} or {"time": "20:30", "days": ["sat","sun"]}. days also accepts "daily" and "weekends". action takes the same shape as the params argument of set_params: {"Light": {"Power": true}}.

edit merges into the stored schedule, so send only the fields that change. The device is given its complete schedule set on every write, so avoid issuing concurrent edits to the same node.

Do not use this for an immediate change (set_params) or to read schedules back (list_schedules).`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "The device to schedule. One device per call.",
					},
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "The group the device belongs to. list_devices returns it alongside the node_id.",
					},
					"operation": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"add", "edit", "remove", "enable", "disable"},
						"description": "add needs name, triggers and action. edit, remove, enable and disable need schedule_id.",
					},
					"schedule_id": map[string]interface{}{
						"type":        "string",
						"description": "The schedule to act on, from list_schedules. Required for every operation except add, where supplying one pins the new schedule's id instead of generating it.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "What to call the schedule, up to 32 characters. Required when adding.",
					},
					"triggers": map[string]interface{}{
						"type":        "array",
						"description": "When it fires: [{\"time\": \"07:00\", \"days\": \"weekdays\"}]. days takes daily, weekdays, weekends, or a list like [\"mon\",\"tue\"]. Required when adding.",
						"items":       map[string]interface{}{"type": "object"},
					},
					"action": map[string]interface{}{
						"type":        "object",
						"description": "What it does, in the same shape as the params argument of set_params: {\"Light\": {\"Power\": true}}. Required when adding.",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the schedule is active. New schedules are enabled unless you set this to false.",
					},
					"info": map[string]interface{}{
						"type":        "string",
						"description": "Free-text note stored with the schedule.",
					},
				},
				Required: []string{"node_id", "group_id", "operation"},
			},
		},
		handleSetSchedule,
	)
}
