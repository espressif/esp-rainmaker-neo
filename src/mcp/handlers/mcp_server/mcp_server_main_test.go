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

// callTool invokes a tool over the full JSON-RPC path and returns the tool result. A tool
// failure is a successful JSON-RPC response carrying isError, so this asserts only that the
// transport worked; whether the tool itself succeeded is the caller's assertion.
func callTool(server *mcpserver.Server, ctx context.Context, toolName string, args map[string]interface{}, token string) mcpserver.ToolCallResult {
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

	return toolResult
}

// callToolSuccess calls a tool and returns its text content, asserting the tool succeeded.
func callToolSuccess(server *mcpserver.Server, ctx context.Context, toolName string, args map[string]interface{}, token string) string {
	result := callTool(server, ctx, toolName, args, token)
	Expect(result.IsError).To(BeFalse(), "Unexpected tool error: %s", result.Content[0].Text)
	return result.Content[0].Text
}

// callToolError calls a tool and returns the message the model would see, asserting the tool
// reported a failure rather than a protocol error.
func callToolError(server *mcpserver.Server, ctx context.Context, toolName string, args map[string]interface{}, token string) string {
	result := callTool(server, ctx, toolName, args, token)
	Expect(result.IsError).To(BeTrue(), "Expected a tool error, got: %s", result.Content[0].Text)
	return result.Content[0].Text
}

// deviceRows unmarshals a list_devices response into its device rows.
func deviceRows(text string) []map[string]interface{} {
	var payload struct {
		Count   int                      `json:"count"`
		Devices []map[string]interface{} `json:"devices"`
	}
	Expect(json.Unmarshal([]byte(text), &payload)).To(Succeed())
	Expect(payload.Count).To(Equal(len(payload.Devices)))
	return payload.Devices
}

// groupRows unmarshals a list_groups response into its group rows.
func groupRows(text string) []mcptools.GroupInfo {
	var payload struct {
		Count  int                  `json:"count"`
		Groups []mcptools.GroupInfo `json:"groups"`
	}
	Expect(json.Unmarshal([]byte(text), &payload)).To(Succeed())
	Expect(payload.Count).To(Equal(len(payload.Groups)))
	return payload.Groups
}

// scheduleRows unmarshals a list_schedules response into its schedule entries.
func scheduleRows(text string) []map[string]interface{} {
	var payload struct {
		Count     int                      `json:"count"`
		Schedules []map[string]interface{} `json:"schedules"`
	}
	Expect(json.Unmarshal([]byte(text), &payload)).To(Succeed())
	Expect(payload.Count).To(Equal(len(payload.Schedules)))
	return payload.Schedules
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
			It("returns the whole tool surface when authenticated", func() {
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
				toolNames := make([]string, len(toolsList.Tools))
				for i, t := range toolsList.Tools {
					toolNames[i] = t.Name
					// A tool the model cannot tell apart from its siblings is worse than no tool,
					// and an empty schema type breaks strict MCP clients.
					Expect(t.Description).NotTo(BeEmpty(), "tool %s has no description", t.Name)
					Expect(t.InputSchema.Type).To(Equal("object"))
				}
				Expect(toolNames).To(ConsistOf("list_devices", "list_groups", "list_schedules", "set_params", "set_schedule"))
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

		Describe("tools/call list_groups", func() {
			It("returns the caller's groups with device counts", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				testUser := user.NewUser(userID)
				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)

				group1, err := group.CreateGroupForUser(rmngCtx, "Test Group 1")
				Expect(err).To(BeNil())
				group2, err := group.CreateGroupForUser(rmngCtx, "Test Group 2")
				Expect(err).To(BeNil())

				groups := groupRows(callToolSuccess(server, ctx, "list_groups", map[string]interface{}{}, "test-token"))
				Expect(groups).To(HaveLen(2))
				Expect([]string{groups[0].GroupID, groups[1].GroupID}).To(ConsistOf(group1.GroupID, group2.GroupID))
			})

			It("counts devices and lists node ids only when asked", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Counting Group")
				Expect(err).To(BeNil())
				nodeID := "counted-node"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				groups := groupRows(callToolSuccess(server, ctx, "list_groups", map[string]interface{}{}, "test-token"))
				Expect(groups).To(HaveLen(1))
				Expect(groups[0].DeviceCount).To(Equal(1))
				Expect(groups[0].NodeIDs).To(BeEmpty(), "node ids must stay opt-in")

				withDevices := groupRows(callToolSuccess(server, ctx, "list_groups",
					map[string]interface{}{"include_devices": true}, "test-token"))
				Expect(withDevices[0].NodeIDs).To(ConsistOf(nodeID))
			})

			It("filters by group name, ignoring case", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				wanted, err := group.CreateGroupForUser(rmngCtx, "Beach House")
				Expect(err).To(BeNil())
				_, err = group.CreateGroupForUser(rmngCtx, "City Flat")
				Expect(err).To(BeNil())

				groups := groupRows(callToolSuccess(server, ctx, "list_groups",
					map[string]interface{}{"group_name": "beach house"}, "test-token"))
				Expect(groups).To(HaveLen(1))
				Expect(groups[0].GroupID).To(Equal(wanted.GroupID))
			})

			It("reports an unknown group name as a tool error", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				message := callToolError(server, ctx, "list_groups",
					map[string]interface{}{"group_name": "No Such Home"}, "test-token")
				Expect(message).To(ContainSubstring("No Such Home"))
			})

			It("refuses group_id and group_name together", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				message := callToolError(server, ctx, "list_groups",
					map[string]interface{}{"group_id": "g1", "group_name": "Home"}, "test-token")
				Expect(message).To(ContainSubstring("not both"))
			})
		})

		Describe("tools/call list_devices", func() {
			It("returns placement and live state in one row", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Devices Group")
				Expect(err).To(BeNil())

				nodeID := "test-node-devices"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				online := true
				test_utils.SetupShadow(nodeID, node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: &online,
							Params: map[string]interface{}{
								"Light":  map[string]interface{}{"Name": "Reading Lamp", "Power": true, "Brightness": 75},
								"Switch": map[string]interface{}{"Power": false},
							},
						},
					},
				}, group_node_db.NodesGroups{Group: grp.GroupID, SubGroups: []string{}})

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices", map[string]interface{}{}, "test-token"))
				Expect(devices).To(HaveLen(1))
				Expect(devices[0]["node_id"]).To(Equal(nodeID))
				Expect(devices[0]["group_id"]).To(Equal(grp.GroupID))
				Expect(devices[0]["group_name"]).To(Equal("Devices Group"))
				Expect(devices[0]["connected"]).To(BeTrue())
				Expect(devices[0]["params"]).To(HaveKey("Light"))
			})

			It("lists a node that has never reported, rather than failing the call", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Silent Node Group")
				Expect(err).To(BeNil())
				nodeID := "test-node-no-shadow"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices", map[string]interface{}{}, "test-token"))
				Expect(devices).To(HaveLen(1))
				Expect(devices[0]["node_id"]).To(Equal(nodeID))
				Expect(devices[0]["connected"]).To(BeFalse())
			})

			It("matches a device by the Name parameter its user sees", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Named Devices Group")
				Expect(err).To(BeNil())

				for nodeID, deviceName := range map[string]string{"node-lamp": "Reading Lamp", "node-fan": "Ceiling Fan"} {
					Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
					_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
					Expect(err).To(BeNil())
					test_utils.SetupShadow(nodeID, node.IoTNodeShadow{
						State: &node.ShadowState{Reported: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{"Light": map[string]interface{}{"Name": deviceName}},
						}},
					}, group_node_db.NodesGroups{Group: grp.GroupID, SubGroups: []string{}})
				}

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices",
					map[string]interface{}{"name": "reading"}, "test-token"))
				Expect(devices).To(HaveLen(1))
				Expect(devices[0]["node_id"]).To(Equal("node-lamp"))
			})

			It("returns an empty list, not an error, when nothing matches", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Empty Match Group")
				Expect(err).To(BeNil())
				nodeID := "unmatched-node"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices",
					map[string]interface{}{"name": "nothing-like-this"}, "test-token"))
				Expect(devices).To(BeEmpty())
			})

			It("returns only the requested fields", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Projection Group")
				Expect(err).To(BeNil())
				nodeID := "projected-node"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())
				test_utils.SetupShadow(nodeID, node.IoTNodeShadow{
					State: &node.ShadowState{Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{"Light": map[string]interface{}{"Power": true, "Brightness": 40}},
					}},
				}, group_node_db.NodesGroups{Group: grp.GroupID, SubGroups: []string{}})

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices",
					map[string]interface{}{"fields": "node_id,params.Light.Power"}, "test-token"))
				Expect(devices).To(HaveLen(1))
				Expect(devices[0]).To(HaveLen(2))
				Expect(devices[0]["node_id"]).To(Equal(nodeID))
				Expect(devices[0]["params.Light.Power"]).To(BeTrue())
			})

			It("does not reveal another user's device", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				otherCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser("other-user-id"))
				test_utils.SetupTestUser(ctx, "other-user-id", "other-user@example.com")
				otherGroup, err := group.CreateGroupForUser(otherCtx, "Someone Else's Home")
				Expect(err).To(BeNil())
				Expect(otherCtx.SetAllow(utils.NodeAll, "foreign-node")).To(Succeed())
				_, err = group.AddNode(otherCtx, otherGroup.GroupID, "foreign-node", nil)
				Expect(err).To(BeNil())

				devices := deviceRows(callToolSuccess(server, ctx, "list_devices", map[string]interface{}{}, "test-token"))
				Expect(devices).To(BeEmpty())

				message := callToolError(server, ctx, "list_devices",
					map[string]interface{}{"node_id": "foreign-node"}, "test-token")
				Expect(message).To(ContainSubstring("foreign-node"))
			})

			It("reports an inaccessible group as a tool error", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				message := callToolError(server, ctx, "list_devices",
					map[string]interface{}{"group_id": "nonexistent-group"}, "test-token")
				Expect(message).To(ContainSubstring("nonexistent-group"))
			})
		})

		Describe("tools/call set_params", func() {
			It("publishes params to the node's desired shadow", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Set Params Group")
				Expect(err).To(BeNil())

				nodeID := "test-node-set-params"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
				Expect(err).To(BeNil())

				setParams := map[string]interface{}{
					"Light": map[string]interface{}{"Power": true, "Brightness": 100},
				}
				text := callToolSuccess(server, ctx, "set_params", map[string]interface{}{
					"group_id": grp.GroupID,
					"node_id":  nodeID,
					"params":   setParams,
				}, "test-token")

				var result mcptools.SetParamsResult
				Expect(json.Unmarshal([]byte(text), &result)).To(Succeed())
				Expect(result).To(Equal(mcptools.SetParamsResult{
					Requested: 1, Succeeded: 1, Failed: 0,
					Results: []mcptools.NodeResult{{NodeID: nodeID, Success: true}},
				}))

				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotDataClient.PublishCalls).To(HaveLen(1))
				Expect(*iotDataClient.PublishCalls[0].Topic).To(ContainSubstring(nodeID))
				Expect(iotDataClient.PublishCalls[0].Payload).To(MatchJSON(`{
					"Light": {"Power": true, "Brightness": 100}
				}`))
			})

			It("applies the same params to every node in a comma-separated list", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Multi Node Group")
				Expect(err).To(BeNil())
				for _, nodeID := range []string{"multi-node-a", "multi-node-b"} {
					Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
					_, err = group.AddNode(rmngCtx, grp.GroupID, nodeID, nil)
					Expect(err).To(BeNil())
				}

				text := callToolSuccess(server, ctx, "set_params", map[string]interface{}{
					"group_id": grp.GroupID,
					"node_id":  "multi-node-a, multi-node-b",
					"params":   map[string]interface{}{"Light": map[string]interface{}{"Power": false}},
				}, "test-token")

				var result mcptools.SetParamsResult
				Expect(json.Unmarshal([]byte(text), &result)).To(Succeed())
				Expect(result.Requested).To(Equal(2))
				Expect(result.Succeeded).To(Equal(2))
				Expect(result.Failed).To(BeZero())
				// The writes run concurrently, so the report has to be re-ordered back to the
				// caller's list or the model cannot line results up with what it asked for.
				Expect(result.Results).To(Equal([]mcptools.NodeResult{
					{NodeID: "multi-node-a", Success: true},
					{NodeID: "multi-node-b", Success: true},
				}))

				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotDataClient.PublishCalls).To(HaveLen(2))
			})

			It("reports the foreign node and still writes the caller's own", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Partial Failure Group")
				Expect(err).To(BeNil())
				ownNode := "own-node"
				Expect(rmngCtx.SetAllow(utils.NodeAll, ownNode)).To(Succeed())
				_, err = group.AddNode(rmngCtx, grp.GroupID, ownNode, nil)
				Expect(err).To(BeNil())

				text := callToolSuccess(server, ctx, "set_params", map[string]interface{}{
					"group_id": grp.GroupID,
					"node_id":  ownNode + ",someone-elses-node",
					"params":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")

				var result mcptools.SetParamsResult
				Expect(json.Unmarshal([]byte(text), &result)).To(Succeed())
				Expect(result.Succeeded).To(Equal(1))
				Expect(result.Failed).To(Equal(1))

				// The foreign node must not be published to, whatever the summary says.
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotDataClient.PublishCalls).To(HaveLen(1))
				Expect(*iotDataClient.PublishCalls[0].Topic).To(ContainSubstring(ownNode))
			})

			It("tells the caller which identifier is missing", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				withoutGroup := callToolError(server, ctx, "set_params", map[string]interface{}{
					"node_id": "some-node",
					"params":  map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")
				Expect(withoutGroup).To(ContainSubstring("group_id is required"))

				withoutParams := callToolError(server, ctx, "set_params", map[string]interface{}{
					"group_id": "some-group",
					"node_id":  "some-node",
				}, "test-token")
				Expect(withoutParams).To(ContainSubstring("params is required"))
			})

			It("fails the whole call when no node could be written", func() {
				restore := mockAuthSuccess(userID)
				defer restore()
				server = createServer()

				message := callToolError(server, ctx, "set_params", map[string]interface{}{
					"group_id": "nonexistent-group",
					"node_id":  "unknown-node",
					"params":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")
				Expect(message).To(ContainSubstring("unknown-node"))

				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				Expect(iotDataClient.PublishCalls).To(BeEmpty())
			})
		})

		Describe("tools/call list_schedules and set_schedule", func() {
			var (
				rmngCtx *rmngctx.RmngContext
				groupID string
				nodeID  string
			)

			BeforeEach(func() {
				restore := mockAuthSuccess(userID)
				DeferCleanup(restore)
				server = createServer()

				rmngCtx = rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))
				grp, err := group.CreateGroupForUser(rmngCtx, "Schedule Group")
				Expect(err).To(BeNil())
				groupID = grp.GroupID

				nodeID = "test-node-schedules"
				Expect(rmngCtx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
				_, err = group.AddNode(rmngCtx, groupID, nodeID, nil)
				Expect(err).To(BeNil())
			})

			addMorningSchedule := func() map[string]interface{} {
				text := callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id":  groupID,
					"node_id":   nodeID,
					"operation": "add",
					"name":      "Morning Lights",
					"triggers":  []interface{}{map[string]interface{}{"time": "07:00", "days": "weekdays"}},
					"action":    map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")

				var payload struct {
					Schedule map[string]interface{} `json:"schedule"`
				}
				Expect(json.Unmarshal([]byte(text), &payload)).To(Succeed())
				return payload.Schedule
			}

			It("adds a schedule, converting the trigger to the device's form", func() {
				created := addMorningSchedule()
				Expect(created["name"]).To(Equal("Morning Lights"))
				Expect(created["enabled"]).To(BeTrue())
				Expect(created["id"]).NotTo(BeEmpty())

				triggers, ok := created["triggers"].([]interface{})
				Expect(ok).To(BeTrue())
				Expect(triggers).To(HaveLen(1))
				// 07:00 is 420 minutes past midnight; weekdays is Mon-Fri = 1+2+4+8+16.
				Expect(triggers[0]).To(Equal(map[string]interface{}{"m": float64(420), "d": float64(31)}))
			})

			It("lists the schedules stored on the node", func() {
				created := addMorningSchedule()

				schedules := scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules).To(HaveLen(1))
				Expect(schedules[0]["id"]).To(Equal(created["id"]))
				Expect(schedules[0]["name"]).To(Equal("Morning Lights"))
			})

			It("returns an empty list for a node with no schedules", func() {
				schedules := scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules).To(BeEmpty())
			})

			It("edits only the fields it is given", func() {
				created := addMorningSchedule()

				callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id":    groupID,
					"node_id":     nodeID,
					"operation":   "edit",
					"schedule_id": created["id"],
					"triggers":    []interface{}{map[string]interface{}{"time": "07:30", "days": "daily"}},
				}, "test-token")

				schedules := scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules).To(HaveLen(1))
				Expect(schedules[0]["name"]).To(Equal("Morning Lights"), "an edit must not clear untouched fields")
				Expect(schedules[0]["action"]).To(Equal(map[string]interface{}{"Light": map[string]interface{}{"Power": true}}))
				Expect(schedules[0]["triggers"]).To(Equal([]interface{}{
					map[string]interface{}{"m": float64(450), "d": float64(127)},
				}))
			})

			It("disables and re-enables a schedule", func() {
				created := addMorningSchedule()

				callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID,
					"operation": "disable", "schedule_id": created["id"],
				}, "test-token")
				schedules := scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules[0]["enabled"]).To(BeFalse())

				callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID,
					"operation": "enable", "schedule_id": created["id"],
				}, "test-token")
				schedules = scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules[0]["enabled"]).To(BeTrue())
			})

			It("removes a schedule without disturbing the others", func() {
				first := addMorningSchedule()
				callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID, "operation": "add",
					"name":     "Evening Lights",
					"triggers": []interface{}{map[string]interface{}{"time": "20:00", "days": "daily"}},
					"action":   map[string]interface{}{"Light": map[string]interface{}{"Power": false}},
				}, "test-token")

				callToolSuccess(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID,
					"operation": "remove", "schedule_id": first["id"],
				}, "test-token")

				schedules := scheduleRows(callToolSuccess(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": nodeID}, "test-token"))
				Expect(schedules).To(HaveLen(1))
				Expect(schedules[0]["name"]).To(Equal("Evening Lights"))
			})

			It("rejects an edit to a schedule the node does not have", func() {
				message := callToolError(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID,
					"operation": "edit", "schedule_id": "nope",
				}, "test-token")
				Expect(message).To(ContainSubstring("nope"))
				Expect(message).To(ContainSubstring("list_schedules"))
			})

			It("names the field an add is missing", func() {
				withoutName := callToolError(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID, "operation": "add",
					"triggers": []interface{}{map[string]interface{}{"time": "07:00", "days": "daily"}},
					"action":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")
				Expect(withoutName).To(ContainSubstring("name is required"))

				withoutTriggers := callToolError(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID, "operation": "add",
					"name":   "No Triggers",
					"action": map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")
				Expect(withoutTriggers).To(ContainSubstring("trigger"))
			})

			It("rejects an unparseable trigger time", func() {
				message := callToolError(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID, "operation": "add",
					"name":     "Bad Time",
					"triggers": []interface{}{map[string]interface{}{"time": "25:00", "days": "daily"}},
					"action":   map[string]interface{}{"Light": map[string]interface{}{"Power": true}},
				}, "test-token")
				Expect(message).To(ContainSubstring("hours"))
			})

			It("rejects an unknown operation", func() {
				message := callToolError(server, ctx, "set_schedule", map[string]interface{}{
					"group_id": groupID, "node_id": nodeID, "operation": "reschedule",
				}, "test-token")
				Expect(message).To(ContainSubstring("reschedule"))
			})

			It("refuses a node outside the caller's group", func() {
				message := callToolError(server, ctx, "list_schedules",
					map[string]interface{}{"group_id": groupID, "node_id": "not-my-node"}, "test-token")
				Expect(message).To(ContainSubstring("not-my-node"))
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

			groups := groupRows(callToolSuccess(server, ctx, "list_groups", map[string]interface{}{}, token))
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

			devices := deviceRows(callToolSuccess(server, ctx, "list_devices",
				map[string]interface{}{"node_id": nodeID, "group_id": grp.GroupID}, token))
			Expect(devices).To(HaveLen(1))
			Expect(devices[0]["params"]).To(Equal(map[string]interface{}{
				"Fan": map[string]interface{}{"Speed": float64(3)},
			}))
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
