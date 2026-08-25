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

Do not call it before set_params or set_schedule when the user has already given you a node_id and group_id, and do not call it to inspect schedules — list_schedules does that.

Filters combine: group_id or subgroup_id to narrow to a home or room, name or type for a kind of device. Reading state fetches each device's shadow, so when you do not need the whole payload pass fields — for example "node_id,group_id,connected" or "params.Light.Power".

In replies to the user prefer the human names in params.<Device>.Name; use ids only in tool calls.`,
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "One node id, or several comma-separated. Omit to search every device the user can reach.",
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
						"description": "Match a device name, partially and ignoring case. Matches both the node's own name and the Name parameter of the devices inside it.",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Match a device type, partially and ignoring case. Accepts a bare word (light, switch, sensor) or a full type such as esp.device.lightbulb.",
					},
					"fields": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated fields to return instead of the whole record. Top level: node_id, group_id, group_name, subgroup_ids, subgroup_names, name, type, model, fw_version, connected, params, config. Dot paths reach inside: params.Light.Power.",
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

Set include_devices when you need the node ids in each group and subgroup but not their state.`,
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
			Description: `Change what a device is doing right now — switch it on or off, set brightness or temperature, trigger a reboot or a factory reset. This is a direct action tool: call it as soon as the user asks for a change and you hold the ids.

If the user gave you node_id and group_id, act on them immediately. Only call list_devices first when the device is identified by name, type or room and you do not have the ids yet. To act on several devices in one go, pass their ids comma-separated in node_id; they must all belong to the group in group_id, and the same params are applied to every one of them, so only batch devices that share the same device names.

params is keyed by the device name inside the node and then by parameter: {"Light": {"Power": true, "Brightness": 80}}. The names are case-sensitive and must match what list_devices returned. Send real booleans for on/off parameters, not "true" or "on".

Do not use this to read state (list_devices) or to make something happen later (set_schedule). Delivery to the device is asynchronous: success means the change was accepted and published, not that the device has already applied it.`,
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
						"description": "Device name to parameter name to value: {\"Light\": {\"Power\": true, \"Brightness\": 80}}. Case-sensitive.",
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
