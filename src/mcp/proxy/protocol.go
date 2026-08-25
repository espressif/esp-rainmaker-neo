// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// ToolHandler processes a tool call for an authenticated user.
type ToolHandler func(user UserContext, id json.RawMessage, args json.RawMessage) (events.APIGatewayV2HTTPResponse, error)

type registeredTool struct {
	tool    Tool
	handler ToolHandler
}

// Server is a reusable MCP protocol server.
type Server struct {
	Name    string
	Version string
	tools   []registeredTool
	auth    Authenticator
}

// NewServer creates a new MCP server with the given name, version, and authenticator.
func NewServer(name, version string, auth Authenticator) *Server {
	return &Server{
		Name:    name,
		Version: version,
		auth:    auth,
	}
}

// RegisterTool adds a tool to the server's tool registry.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.tools = append(s.tools, registeredTool{tool: tool, handler: handler})
}

// Tools returns the registered tool catalog. Exported so offline consumers — the eval
// framework's schema snapshot, docs generators — can read what the server actually serves
// without standing up a Lambda.
func (s *Server) Tools() []Tool {
	tools := make([]Tool, len(s.tools))
	for i, rt := range s.tools {
		tools[i] = rt.tool
	}
	return tools
}

// HandleRequest is the Lambda handler entry point for the MCP server.
func (s *Server) HandleRequest(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	switch request.RequestContext.HTTP.Method {
	case "GET":
		return JSONRPCUnauthorizedResponse(request, nil, "Unauthorized: authentication required"), nil
	case "POST":
		return s.handleMCPPost(ctx, request)
	default:
		body, _ := json.Marshal(map[string]string{"status": "Method not allowed"})
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       string(body),
		}, nil
	}
}

// hasBearerToken checks whether the request has an Authorization: Bearer header.
func hasBearerToken(request events.APIGatewayV2HTTPRequest) bool {
	auth := request.Headers["Authorization"]
	if auth == "" {
		auth = request.Headers["authorization"]
	}
	return len(auth) > len("Bearer ") && auth[:7] == "Bearer "
}

func (s *Server) handleMCPPost(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	body := request.Body
	if request.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return JSONRPCErrorResponse(nil, -32700, "Parse error", nil), nil
		}
		body = string(decoded)
	}

	var rpcReq JSONRPCRequest
	if err := json.Unmarshal([]byte(body), &rpcReq); err != nil {
		return JSONRPCErrorResponse(nil, -32700, "Parse error", nil), nil
	}

	if rpcReq.JSONRPC != "2.0" {
		return JSONRPCErrorResponse(rpcReq.ID, -32600, "Invalid Request: jsonrpc must be \"2.0\"", nil), nil
	}

	// All methods require a Bearer token at the HTTP level so that
	// clients discover the OAuth flow on the very first request.
	// Token *validation* only happens for methods that need a user context.
	if !hasBearerToken(request) {
		return JSONRPCUnauthorizedResponse(request, rpcReq.ID, "Unauthorized: missing Authorization header"), nil
	}

	switch rpcReq.Method {
	case "initialize":
		return s.handleInitialize(rpcReq.ID), nil

	case "notifications/initialized":
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "Content-Type": "application/json"},
			Body:       "{}",
		}, nil

	case "tools/list":
		user, err := s.auth(ctx, request)
		if err != nil {
			return JSONRPCUnauthorizedResponse(request, rpcReq.ID, "Unauthorized: "+err.Error()), nil
		}
		_ = user
		return s.handleToolsList(rpcReq.ID), nil

	case "tools/call":
		user, err := s.auth(ctx, request)
		if err != nil {
			return JSONRPCUnauthorizedResponse(request, rpcReq.ID, "Unauthorized: "+err.Error()), nil
		}
		return s.handleToolsCall(user, rpcReq.ID, rpcReq.Params)

	default:
		return JSONRPCErrorResponse(rpcReq.ID, -32601, "Method not found: "+rpcReq.Method, nil), nil
	}
}

func (s *Server) handleInitialize(id json.RawMessage) events.APIGatewayV2HTTPResponse {
	result := InitializeResult{
		ProtocolVersion: "2025-03-26",
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    s.Name,
			Version: s.Version,
		},
	}
	return JSONRPCSuccessResponse(id, result)
}

func (s *Server) handleToolsList(id json.RawMessage) events.APIGatewayV2HTTPResponse {
	return JSONRPCSuccessResponse(id, map[string]interface{}{"tools": s.Tools()})
}

func (s *Server) handleToolsCall(user UserContext, id json.RawMessage, params json.RawMessage) (events.APIGatewayV2HTTPResponse, error) {
	var toolCall ToolCallParams
	if err := json.Unmarshal(params, &toolCall); err != nil {
		return JSONRPCErrorResponse(id, -32602, "Invalid params", nil), nil
	}

	for _, rt := range s.tools {
		if rt.tool.Name == toolCall.Name {
			return rt.handler(user, id, toolCall.Arguments)
		}
	}
	return JSONRPCErrorResponse(id, -32602, "Unknown tool: "+toolCall.Name, nil), nil
}
