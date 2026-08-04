// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

// JSON-RPC types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCP protocol types.
// External contract — all json tags below must remain camelCase to match the MCP / JSON-RPC spec.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct{}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolCallResult struct {
	Content []ToolContent `json:"content"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ResourceMetadataURL constructs the OAuth Protected Resource Metadata URL
// from the API Gateway request context (RFC 9728).
func ResourceMetadataURL(request events.APIGatewayV2HTTPRequest) string {
	domain := request.RequestContext.DomainName
	if domain == "" {
		return ""
	}
	return fmt.Sprintf("https://%s%s", domain, oidc.OAuthPRMetaPath)
}

func JSONRPCSuccessResponse(id json.RawMessage, result interface{}) events.APIGatewayV2HTTPResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	body, _ := json.Marshal(resp)
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Content-Type": "application/json"},
		Body:       string(body),
	}
}

func JSONRPCErrorResponse(id json.RawMessage, code int, message string, data interface{}) events.APIGatewayV2HTTPResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	body, _ := json.Marshal(resp)

	statusCode := http.StatusOK
	if code == -32001 {
		statusCode = http.StatusUnauthorized
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Content-Type": "application/json"},
		Body:       string(body),
	}
}

// JSONRPCUnauthorizedResponse returns a JSON-RPC error with HTTP 401 and a
// WWW-Authenticate header pointing to the OAuth Protected Resource Metadata.
func JSONRPCUnauthorizedResponse(request events.APIGatewayV2HTTPRequest, id json.RawMessage, message string) events.APIGatewayV2HTTPResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    -32001,
			Message: message,
		},
	}
	body, _ := json.Marshal(resp)

	headers := map[string]string{
		"Access-Control-Allow-Origin": "*",
		"X-Content-Type-Options":      "nosniff",
		"Content-Type":                "application/json",
	}
	if metaURL := ResourceMetadataURL(request); metaURL != "" {
		headers["WWW-Authenticate"] = fmt.Sprintf(`Bearer resource_metadata="%s"`, metaURL)
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusUnauthorized,
		Headers:    headers,
		Body:       string(body),
	}
}
