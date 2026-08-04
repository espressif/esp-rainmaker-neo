// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"

	mcpserver "mcp-server"

	mcptools "github.com/espressif/esp-rainmaker-neo/src/mcp/tools"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// rmngUserContext adapts rmngctx.RmngContext to the mcpserver.UserContext interface.
type rmngUserContext struct {
	rmngCtx *rmngctx.RmngContext
}

func (u *rmngUserContext) GetUserID() string {
	return u.rmngCtx.GetID()
}

func (u *rmngUserContext) GoContext() context.Context {
	return u.rmngCtx.Context
}

func resolveRmngUser(ctx context.Context, userID string) (mcpserver.UserContext, error) {
	newUser := user.NewUser(userID)
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, newUser)
	return &rmngUserContext{rmngCtx}, nil
}

func handleGetGroupsTool(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx
	groups, err := mcptools.GetGroups(rmngCtx)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return mcpserver.JSONRPCErrorResponse(id, -32603, "Failed to list groups", nil), nil
	}

	jsonData, err := json.Marshal(groups)
	if err != nil {
		return mcpserver.JSONRPCErrorResponse(id, -32603, "Failed to marshal groups", nil), nil
	}

	result := mcpserver.ToolCallResult{
		Content: []mcpserver.ToolContent{
			{Type: "text", Text: string(jsonData)},
		},
	}
	return mcpserver.JSONRPCSuccessResponse(id, result), nil
}

func handleGetParamsTool(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		GroupID string `json:"group_id"`
		NodeID  string `json:"node_id"`
	}
	if err := json.Unmarshal(args, &toolArgs); err != nil {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "Invalid arguments", nil), nil
	}
	if toolArgs.GroupID == "" {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "group_id is required", nil), nil
	}
	if toolArgs.NodeID == "" {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "node_id is required", nil), nil
	}

	params, err := mcptools.GetNodeParams(rmngCtx, toolArgs.GroupID, toolArgs.NodeID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return mcpserver.JSONRPCErrorResponse(id, -32603, "Failed to get params", nil), nil
	}

	jsonData, err := json.Marshal(params)
	if err != nil {
		return mcpserver.JSONRPCErrorResponse(id, -32603, "Failed to marshal params", nil), nil
	}

	result := mcpserver.ToolCallResult{
		Content: []mcpserver.ToolContent{
			{Type: "text", Text: string(jsonData)},
		},
	}
	return mcpserver.JSONRPCSuccessResponse(id, result), nil
}

func handleSetParamsTool(userCtx mcpserver.UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	rmngCtx := userCtx.(*rmngUserContext).rmngCtx

	var toolArgs struct {
		GroupID string                 `json:"group_id"`
		NodeID  string                 `json:"node_id"`
		Params  map[string]interface{} `json:"params"`
	}
	if err := json.Unmarshal(args, &toolArgs); err != nil {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "Invalid arguments", nil), nil
	}
	if toolArgs.GroupID == "" {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "group_id is required", nil), nil
	}
	if toolArgs.NodeID == "" {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "node_id is required", nil), nil
	}
	if toolArgs.Params == nil {
		return mcpserver.JSONRPCErrorResponse(id, -32602, "params is required", nil), nil
	}

	err := mcptools.SetNodeParams(rmngCtx, toolArgs.GroupID, toolArgs.NodeID, toolArgs.Params)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return mcpserver.JSONRPCErrorResponse(id, -32603, "Failed to set params", nil), nil
	}

	result := mcpserver.ToolCallResult{
		Content: []mcpserver.ToolContent{
			{Type: "text", Text: `{"status":"success"}`},
		},
	}
	return mcpserver.JSONRPCSuccessResponse(id, result), nil
}

// createServerAuth is a var so tests can override the authenticator.
var createServerAuth = func() mcpserver.Authenticator {
	return mcpserver.NewOIDCAuthenticator(
		os.Getenv("USER_ISSUER"),
		os.Getenv("MCP_CLIENT_ID"),
		os.Getenv("USER_JWKS_PARA_NAME"),
		resolveRmngUser,
	)
}

func createServer() *mcpserver.Server {
	server := mcpserver.NewServer("rainmaker-mcp", "1.0.0", createServerAuth())
	server.RegisterTool(
		mcpserver.Tool{
			Name:        "get_groups",
			Description: "List the authenticated user's groups with their nodes and subgroups",
			InputSchema: mcpserver.InputSchema{Type: "object", Properties: map[string]interface{}{}},
		},
		handleGetGroupsTool,
	)
	server.RegisterTool(
		mcpserver.Tool{
			Name:        "get_params",
			Description: "Get the current parameters for a node from its reported shadow",
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "The group ID the node belongs to",
					},
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "The node ID to get parameters for",
					},
				},
				Required: []string{"group_id", "node_id"},
			},
		},
		handleGetParamsTool,
	)
	server.RegisterTool(
		mcpserver.Tool{
			Name:        "set_params",
			Description: "Set parameters for a node by publishing to its desired shadow",
			InputSchema: mcpserver.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"group_id": map[string]interface{}{
						"type":        "string",
						"description": "The group ID the node belongs to",
					},
					"node_id": map[string]interface{}{
						"type":        "string",
						"description": "The node ID to set parameters for",
					},
					"params": map[string]interface{}{
						"type":        "object",
						"description": "The parameters to set, keyed by device name with parameter values",
					},
				},
				Required: []string{"group_id", "node_id", "params"},
			},
		},
		handleSetParamsTool,
	)
	return server
}

func main() {
	server := createServer()
	lambda.Start(server.HandleRequest)
}
