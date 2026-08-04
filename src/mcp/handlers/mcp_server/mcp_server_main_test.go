// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	mcpserver "mcp-server"

	mcptools "github.com/espressif/esp-rainmaker-neo/src/mcp/tools"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	timingFile *os.File
)

var _ = BeforeSuite(func() {
	var err error
	timingFile, err = test_utils.CreateCommonSummaryFile("mcp_tests.txt")
	Expect(err).NotTo(HaveOccurred(), "Failed to create timing summary file")
})

// httpReq builds an APIGatewayV2HTTPRequest with the given method and path.
func httpReq(method, path string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		RawPath: path,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: method,
			},
		},
	}
}

func makeJSONRPCRequest(method string, params interface{}) string {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		paramsBytes, _ := json.Marshal(params)
		req["params"] = json.RawMessage(paramsBytes)
	}
	body, _ := json.Marshal(req)
	return string(body)
}

func mockAuthSuccess(userID string) func() {
	original := createServerAuth
	createServerAuth = func() mcpserver.Authenticator {
		return func(ctx context.Context, request events.APIGatewayV2HTTPRequest) (mcpserver.UserContext, error) {
			newUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, newUser)
			return &rmngUserContext{rmngCtx}, nil
		}
	}
	return func() { createServerAuth = original }
}

// callToolSuccess calls a tool via the MCP server and returns the text content
// of the first result entry. It asserts HTTP 200 and no JSON-RPC error.
func callToolSuccess(server *mcpserver.Server, ctx context.Context, toolName string, args map[string]interface{}, token string) string {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	}
	request := httpReq("POST", "/v1/mcp")
	request.Body = makeJSONRPCRequest("tools/call", params)
	request.Headers = map[string]string{"Authorization": "Bearer " + token}

	response, err := server.HandleRequest(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	var rpcResp mcpserver.JSONRPCResponse
	err = json.Unmarshal([]byte(response.Body), &rpcResp)
	Expect(err).To(BeNil())
	Expect(rpcResp.Error).To(BeNil(), "Unexpected JSON-RPC error: %v", rpcResp.Error)

	resultBytes, _ := json.Marshal(rpcResp.Result)
	var toolResult mcpserver.ToolCallResult
	err = json.Unmarshal(resultBytes, &toolResult)
	Expect(err).To(BeNil())
	Expect(toolResult.Content).To(HaveLen(1))

	return toolResult.Content[0].Text
}

func mockAuthFailure(errMsg string) func() {
	original := createServerAuth
	createServerAuth = func() mcpserver.Authenticator {
		return func(ctx context.Context, request events.APIGatewayV2HTTPRequest) (mcpserver.UserContext, error) {
			return nil, fmt.Errorf("%s", errMsg)
		}
	}
	return func() { createServerAuth = original }
}

// expectUnauthorized asserts a tools/list call with the given bearer token is rejected as 401.
func expectUnauthorized(server *mcpserver.Server, ctx context.Context, token string) {
	request := httpReq("POST", "/v1/mcp")
	request.Body = makeJSONRPCRequest("tools/list", nil)
	request.Headers = map[string]string{"Authorization": "Bearer " + token}

	response, err := server.HandleRequest(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

	var rpcResp mcpserver.JSONRPCResponse
	err = json.Unmarshal([]byte(response.Body), &rpcResp)
	Expect(err).To(BeNil())
	Expect(rpcResp.Error).ToNot(BeNil())
	Expect(rpcResp.Error.Code).To(Equal(-32001))
}

var _ = Describe("MCP Main", func() {
	var (
		ctx    context.Context
		userID string
		server *mcpserver.Server
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		userID = "test-user-id"
		ctx = context.Background()
		test_utils.SetupTestUser(ctx, userID, "test-user@example.com")

		server = createServer()
	})

	Describe("GET /v1/mcp - Authentication Required", func() {
		It("returns 401", func() {
			request := httpReq("GET", "/v1/mcp")

			response, err := server.HandleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		})
	})

	Describe("POST /v1/mcp - MCP JSON-RPC", func() {
		Describe("initialize", func() {
			It("returns 401 without Bearer token to trigger OAuth discovery", func() {
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("initialize", nil)

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32001))
			})

			It("returns server info and capabilities with Bearer token", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("initialize", nil)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).To(BeNil())
				Expect(rpcResp.JSONRPC).To(Equal("2.0"))

				resultBytes, _ := json.Marshal(rpcResp.Result)
				var initResult mcpserver.InitializeResult
				err = json.Unmarshal(resultBytes, &initResult)
				Expect(err).To(BeNil())
				Expect(initResult.ProtocolVersion).To(Equal("2025-03-26"))
				Expect(initResult.ServerInfo.Name).To(Equal("rainmaker-mcp"))
				Expect(initResult.ServerInfo.Version).To(Equal("1.0.0"))
				Expect(initResult.Capabilities.Tools).ToNot(BeNil())
			})
		})

		Describe("notifications/initialized", func() {
			It("returns 200 with Bearer token", func() {
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("notifications/initialized", nil)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))
			})
		})

		Describe("tools/list", func() {
			It("returns get_groups tool when authenticated", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/list", nil)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).To(BeNil())

				resultBytes, _ := json.Marshal(rpcResp.Result)
				var toolsList struct {
					Tools []mcpserver.Tool `json:"tools"`
				}
				err = json.Unmarshal(resultBytes, &toolsList)
				Expect(err).To(BeNil())
				Expect(toolsList.Tools).To(HaveLen(3))
				toolNames := make([]string, len(toolsList.Tools))
				for i, t := range toolsList.Tools {
					toolNames[i] = t.Name
				}
				Expect(toolNames).To(ContainElement("get_groups"))
				Expect(toolNames).To(ContainElement("get_params"))
				Expect(toolNames).To(ContainElement("set_params"))
			})

			It("returns 401 when unauthenticated", func() {
				restore := mockAuthFailure("missing Authorization header")
				defer restore()
				server = createServer()

				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/list", nil)

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32001))
			})
		})

		Describe("tools/call get_groups", func() {
			It("returns groups for authenticated user", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				// Create test user context and groups
				testUser := user.NewUser(userID)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

				group1, err := group.CreateGroupForUser(rmngCtx, "Test Group 1")
				Expect(err).To(BeNil())
				group2, err := group.CreateGroupForUser(rmngCtx, "Test Group 2")
				Expect(err).To(BeNil())

				text := callToolSuccess(server, ctx, "get_groups", map[string]interface{}{}, "test-token")

				var groups []mcptools.GroupInfo
				err = json.Unmarshal([]byte(text), &groups)
				Expect(err).To(BeNil())
				Expect(groups).To(HaveLen(2))

				groupIDs := []string{groups[0].GroupID, groups[1].GroupID}
				Expect(groupIDs).To(ContainElement(group1.GroupID))
				Expect(groupIDs).To(ContainElement(group2.GroupID))
			})
		})

		Describe("tools/call get_params", func() {
			It("returns params for a node in the user's group", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				testUser := user.NewUser(userID)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

				// Create a group and add a node
				grp, err := group.CreateGroupForUser(rmngCtx, "Params Test Group")
				Expect(err).To(BeNil())

				nodeID := "test-node-params"
				rmngCtx.SetAllow(utils.NodeAll, nodeID)
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				// Set up shadow with params data
				shadowState := node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{
								"Light": map[string]interface{}{
									"Power":      true,
									"Brightness": 75,
								},
								"Switch": map[string]interface{}{
									"Power": false,
								},
							},
						},
					},
				}
				nodeGroups := group_node_db.NodesGroups{Group: grp.GroupID, SubGroups: []string{}}
				test_utils.SetupShadow(nodeID, shadowState, nodeGroups)

				text := callToolSuccess(server, ctx, "get_params",
					map[string]interface{}{"group_id": grp.GroupID, "node_id": nodeID}, "test-token")

				Expect(text).To(MatchJSON(`{
					"Light": {"Power": true, "Brightness": 75},
					"Switch": {"Power": false}
				}`))
			})

			It("returns empty params when node has no shadow", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				testUser := user.NewUser(userID)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

				grp, err := group.CreateGroupForUser(rmngCtx, "Empty Shadow Group")
				Expect(err).To(BeNil())

				nodeID := "test-node-no-shadow"
				rmngCtx.SetAllow(utils.NodeAll, nodeID)
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				text := callToolSuccess(server, ctx, "get_params",
					map[string]interface{}{"group_id": grp.GroupID, "node_id": nodeID}, "test-token")

				Expect(text).To(MatchJSON(`{}`))
			})

			It("returns error when group_id is missing", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name":      "get_params",
					"arguments": map[string]interface{}{"node_id": "some-node"},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32602))
			})

			It("returns error when user does not have access to group", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name":      "get_params",
					"arguments": map[string]interface{}{"group_id": "nonexistent-group", "node_id": "unknown-node"},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32603))
				Expect(rpcResp.Error.Message).To(ContainSubstring("Failed to get params"))
			})
		})

		Describe("tools/call set_params", func() {
			It("publishes params to the node's desired shadow", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				testUser := user.NewUser(userID)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

				grp, err := group.CreateGroupForUser(rmngCtx, "Set Params Group")
				Expect(err).To(BeNil())

				nodeID := "test-node-set-params"
				rmngCtx.SetAllow(utils.NodeAll, nodeID)
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				setParams := map[string]interface{}{
					"Light": map[string]interface{}{
						"Power":      true,
						"Brightness": 100,
					},
				}
				text := callToolSuccess(server, ctx, "set_params", map[string]interface{}{
					"group_id": grp.GroupID,
					"node_id":  nodeID,
					"params":   setParams,
				}, "test-token")

				Expect(text).To(MatchJSON(`{"status":"success"}`))

				// Verify the publish was called via IoT mock
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotDataClient.PublishCalls).To(HaveLen(1))
				Expect(*iotDataClient.PublishCalls[0].Topic).To(ContainSubstring(nodeID))
				Expect(iotDataClient.PublishCalls[0].Payload).To(MatchJSON(`{
					"Light": {"Power": true, "Brightness": 100}
				}`))
			})

			It("returns error when group_id is missing", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name": "set_params",
					"arguments": map[string]interface{}{
						"node_id": "some-node",
						"params":  map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
					},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32602))
			})

			It("returns error when params is missing", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name": "set_params",
					"arguments": map[string]interface{}{
						"group_id": "some-group",
						"node_id":  "some-node",
					},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32602))
			})

			It("returns error when user does not have access to group", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name": "set_params",
					"arguments": map[string]interface{}{
						"group_id": "nonexistent-group",
						"node_id":  "unknown-node",
						"params":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
					},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32603))
				Expect(rpcResp.Error.Message).To(ContainSubstring("Failed to set params"))
			})
		})

		Describe("Error cases", func() {
			It("returns parse error for invalid JSON body", func() {
				request := httpReq("POST", "/v1/mcp")
				request.Body = "not-json"

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32700))
			})

			It("returns method-not-found for unknown method", func() {
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("unknown/method", nil)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32601))
			})

			It("returns invalid-params for unknown tool", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				params := map[string]interface{}{
					"name":      "nonexistent_tool",
					"arguments": map[string]interface{}{},
				}
				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/call", params)
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32602))
			})

			It("returns invalid-request for wrong jsonrpc version", func() {
				request := httpReq("POST", "/v1/mcp")
				request.Body = `{"jsonrpc":"1.0","id":1,"method":"initialize"}`
				request.Headers = map[string]string{"Authorization": "Bearer test-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32600))
			})

			It("returns 401 for invalid bearer token", func() {
				restore := mockAuthFailure("invalid token")
				defer restore()
				server = createServer()

				request := httpReq("POST", "/v1/mcp")
				request.Body = makeJSONRPCRequest("tools/list", nil)
				request.Headers = map[string]string{"Authorization": "Bearer invalid-token"}

				response, err := server.HandleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

				var rpcResp mcpserver.JSONRPCResponse
				err = json.Unmarshal([]byte(response.Body), &rpcResp)
				Expect(err).To(BeNil())
				Expect(rpcResp.Error).ToNot(BeNil())
				Expect(rpcResp.Error.Code).To(Equal(-32001))
			})
		})
	})

	Describe("Authentication with real token validation", func() {
		var (
			jwksServer *httptest.Server
			issuer     string
		)

		BeforeEach(func() {
			jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/jwks.json" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(test_utils.TestJWKUtil.GetTestJWKS())
			}))
			issuer = jwksServer.URL
			os.Setenv("USER_ISSUER", issuer)
			// Verification reads the JWKS from SSM; seed the same test JWKS there.
			jwksParam := "/espuser/base/user-jwks-test"
			jwksVal := string(test_utils.TestJWKUtil.GetTestJWKS())
			_, err := awscommon.GetSSMClient().PutParameter(context.Background(), &ssm.PutParameterInput{
				Name: &jwksParam, Value: &jwksVal, Type: ssmtypes.ParameterTypeString,
				Overwrite: aws.Bool(true),
			})
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("USER_JWKS_PARA_NAME", jwksParam)
			os.Unsetenv("SSM_" + strings.ToUpper(jwksParam))
			server = createServer()
		})

		AfterEach(func() {
			jwksServer.Close()
			os.Unsetenv("USER_ISSUER")
			os.Unsetenv("USER_JWKS_PARA_NAME")
		})

		It("resolves the user by sub for a valid ESP User token", func() {
			token := test_utils.TestJWKUtil.GetIdentityToken(userID, issuer, "access", false, false, "test@example.com", "")

			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
			grp, err := group.CreateGroupForUser(rmngCtx, "OIDC Auth Group")
			Expect(err).To(BeNil())

			text := callToolSuccess(server, ctx, "get_groups", map[string]interface{}{}, token)

			var groups []mcptools.GroupInfo
			err = json.Unmarshal([]byte(text), &groups)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].GroupID).To(Equal(grp.GroupID))
		})

		It("authenticates and returns node params with a valid ESP User token", func() {
			token := test_utils.TestJWKUtil.GetIdentityToken(userID, issuer, "access", false, false, "test@example.com", "")

			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

			grp, err := group.CreateGroupForUser(rmngCtx, "OIDC Params Group")
			Expect(err).To(BeNil())

			nodeID := "auth-test-node"
			rmngCtx.SetAllow(utils.NodeAll, nodeID)
			_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
			Expect(err).To(BeNil())

			shadowState := node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"Fan": map[string]interface{}{
								"Speed": 3,
							},
						},
					},
				},
			}
			nodeGroups := group_node_db.NodesGroups{Group: grp.GroupID, SubGroups: []string{}}
			test_utils.SetupShadow(nodeID, shadowState, nodeGroups)

			text := callToolSuccess(server, ctx, "get_params",
				map[string]interface{}{"group_id": grp.GroupID, "node_id": nodeID}, token)

			Expect(text).To(MatchJSON(`{"Fan": {"Speed": 3}}`))
		})

		It("rejects an expired ESP User token", func() {
			token := test_utils.TestJWKUtil.GetIdentityToken(userID, issuer, "id", true, false, "test@example.com", "")
			expectUnauthorized(server, ctx, token)
		})

		It("rejects a token with the wrong issuer", func() {
			token := test_utils.TestJWKUtil.GetIdentityToken(userID, "https://wrong-issuer.example.com", "id", false, false, "", "")
			expectUnauthorized(server, ctx, token)
		})

		It("rejects request with no Authorization header", func() {
			request := httpReq("POST", "/v1/mcp")
			request.Body = makeJSONRPCRequest("tools/list", nil)

			response, err := server.HandleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

			var rpcResp mcpserver.JSONRPCResponse
			err = json.Unmarshal([]byte(response.Body), &rpcResp)
			Expect(err).To(BeNil())
			Expect(rpcResp.Error).ToNot(BeNil())
			Expect(rpcResp.Error.Code).To(Equal(-32001))
			Expect(rpcResp.Error.Message).To(ContainSubstring("Unauthorized"))
		})

		It("rejects request with malformed Bearer token", func() {
			request := httpReq("POST", "/v1/mcp")
			request.Body = makeJSONRPCRequest("tools/list", nil)
			request.Headers = map[string]string{"Authorization": "Bearer not-a-valid-jwt"}

			response, err := server.HandleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))

			var rpcResp mcpserver.JSONRPCResponse
			err = json.Unmarshal([]byte(response.Body), &rpcResp)
			Expect(err).To(BeNil())
			Expect(rpcResp.Error).ToNot(BeNil())
			Expect(rpcResp.Error.Code).To(Equal(-32001))
		})
	})

	Describe("Unsupported HTTP method", func() {
		It("returns 405 for unsupported methods", func() {
			request := httpReq("PUT", "/v1/mcp")

			response, err := server.HandleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})

})

func TestMCP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCP Suite")
}
