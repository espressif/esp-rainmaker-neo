// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"

	mcpserver "mcp-server"

	mcptools "github.com/espressif/esp-rainmaker-neo/src/mcp/tools"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
)

// toolFailure logs the real error and hands the model a message it can act on. The two are
// deliberately different: internal detail stays in the log, guidance goes to the caller.
func toolFailure(rmngCtx *rmngctx.RmngContext, id json.RawMessage, err error, message string) events.APIGatewayV2HTTPResponse {
	rlog.Error(rmngCtx).Err(err).Send()
	return mcpserver.ToolErrorResponse(id, message)
}

// parseArgs unmarshals a tool's arguments. Absent arguments are valid for tools whose fields
// are all optional, so an empty payload decodes to the zero value rather than failing.
func parseArgs(args json.RawMessage, target interface{}) error {
	if len(args) == 0 {
		return nil
	}
	return json.Unmarshal(args, target)
}

func handleListDevices(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		NodeID     string `json:"node_id"`
		GroupID    string `json:"group_id"`
		SubgroupID string `json:"subgroup_id"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		Fields     string `json:"fields"`
	}
	if err := parseArgs(args, &toolArgs); err != nil {
		return mcpserver.ToolErrorResponse(id, "could not read the arguments: "+err.Error()), nil
	}

	devices, err := mcptools.ListDevices(rmngCtx, mcptools.DeviceFilter{
		NodeIDs:    mcptools.SplitIDs(toolArgs.NodeID),
		GroupID:    toolArgs.GroupID,
		SubgroupID: toolArgs.SubgroupID,
		Name:       toolArgs.Name,
		Type:       toolArgs.Type,
		Fields:     toolArgs.Fields,
	})
	if err != nil {
		return toolFailure(rmngCtx, id, err, err.Error()), nil
	}

	return mcpserver.ToolTextResponse(id, map[string]interface{}{
		"count":   len(devices),
		"devices": devices,
	}), nil
}

func handleListGroups(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		GroupID        string `json:"group_id"`
		GroupName      string `json:"group_name"`
		IncludeDevices bool   `json:"include_devices"`
	}
	if err := parseArgs(args, &toolArgs); err != nil {
		return mcpserver.ToolErrorResponse(id, "could not read the arguments: "+err.Error()), nil
	}

	groups, err := mcptools.ListGroups(rmngCtx, mcptools.GroupFilter{
		GroupID:        toolArgs.GroupID,
		GroupName:      toolArgs.GroupName,
		IncludeDevices: toolArgs.IncludeDevices,
	})
	if err != nil {
		return toolFailure(rmngCtx, id, err, err.Error()), nil
	}

	return mcpserver.ToolTextResponse(id, map[string]interface{}{
		"count":  len(groups),
		"groups": groups,
	}), nil
}

func handleListSchedules(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		NodeID  string `json:"node_id"`
		GroupID string `json:"group_id"`
	}
	if err := parseArgs(args, &toolArgs); err != nil {
		return mcpserver.ToolErrorResponse(id, "could not read the arguments: "+err.Error()), nil
	}
	if missing := missingIDs(toolArgs.NodeID, toolArgs.GroupID); missing != "" {
		return mcpserver.ToolErrorResponse(id, missing), nil
	}

	schedules, err := mcptools.ListSchedules(rmngCtx, toolArgs.GroupID, toolArgs.NodeID)
	if err != nil {
		return toolFailure(rmngCtx, id, err, "could not read the schedules for "+toolArgs.NodeID+": it is not a device in group "+toolArgs.GroupID+", or it is unreachable. Call list_devices to confirm the node_id and group_id."), nil
	}

	return mcpserver.ToolTextResponse(id, map[string]interface{}{
		"node_id":   toolArgs.NodeID,
		"count":     len(schedules),
		"schedules": schedules,
	}), nil
}

func handleSetParams(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		NodeID  string                 `json:"node_id"`
		GroupID string                 `json:"group_id"`
		Params  map[string]interface{} `json:"params"`
	}
	if err := parseArgs(args, &toolArgs); err != nil {
		return mcpserver.ToolErrorResponse(id, "could not read the arguments: "+err.Error()), nil
	}
	if missing := missingIDs(toolArgs.NodeID, toolArgs.GroupID); missing != "" {
		return mcpserver.ToolErrorResponse(id, missing), nil
	}
	if len(toolArgs.Params) == 0 {
		return mcpserver.ToolErrorResponse(id, `params is required and must name at least one device, for example {"Light": {"Power": true}}`), nil
	}

	nodeIDs := mcptools.SplitIDs(toolArgs.NodeID)
	result, err := mcptools.SetNodeParams(rmngCtx, toolArgs.GroupID, nodeIDs, toolArgs.Params)
	if err != nil {
		return toolFailure(rmngCtx, id, err, err.Error()), nil
	}
	// Every device failing is a failed call, not a partial success — say so plainly so the
	// model does not report a change that never happened.
	if result.Succeeded == 0 {
		return mcpserver.ToolErrorResponse(id, firstError(result)), nil
	}

	return mcpserver.ToolTextResponse(id, result), nil
}

func handleSetSchedule(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		NodeID     string                   `json:"node_id"`
		GroupID    string                   `json:"group_id"`
		Operation  string                   `json:"operation"`
		ScheduleID string                   `json:"schedule_id"`
		Name       string                   `json:"name"`
		Triggers   []map[string]interface{} `json:"triggers"`
		Action     map[string]interface{}   `json:"action"`
		Enabled    *bool                    `json:"enabled"`
		Info       string                   `json:"info"`
	}
	if err := parseArgs(args, &toolArgs); err != nil {
		return mcpserver.ToolErrorResponse(id, "could not read the arguments: "+err.Error()), nil
	}
	if missing := missingIDs(toolArgs.NodeID, toolArgs.GroupID); missing != "" {
		return mcpserver.ToolErrorResponse(id, missing), nil
	}
	if toolArgs.Operation == "" {
		return mcpserver.ToolErrorResponse(id, "operation is required — use add, edit, remove, enable or disable"), nil
	}

	schedule, err := mcptools.SetSchedule(rmngCtx, toolArgs.GroupID, toolArgs.NodeID,
		mcptools.ScheduleOperation(toolArgs.Operation),
		mcptools.ScheduleInput{
			ScheduleID: toolArgs.ScheduleID,
			Name:       toolArgs.Name,
			Triggers:   toolArgs.Triggers,
			Action:     toolArgs.Action,
			Enabled:    toolArgs.Enabled,
			Info:       toolArgs.Info,
		})
	if err != nil {
		return toolFailure(rmngCtx, id, err, err.Error()), nil
	}

	return mcpserver.ToolTextResponse(id, map[string]interface{}{
		"node_id":   toolArgs.NodeID,
		"operation": toolArgs.Operation,
		"schedule":  schedule,
	}), nil
}

// missingIDs returns the guidance for whichever identifier the caller left out. Both come
// from the same list_devices row, so the fix is one call either way.
func missingIDs(nodeID, groupID string) string {
	switch {
	case nodeID == "" && groupID == "":
		return "node_id and group_id are required — call list_devices to find them, it returns both on every device"
	case nodeID == "":
		return "node_id is required — call list_devices to find it"
	case groupID == "":
		return "group_id is required — call list_devices, it returns the group_id alongside every node_id"
	}
	return ""
}

func firstError(result mcptools.SetParamsResult) string {
	for _, nodeResult := range result.Results {
		if nodeResult.Error != "" {
			return nodeResult.Error
		}
	}
	return "the parameters could not be set on any of the devices given"
}
