// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"testing"

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

func TestUserNodeTagsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Node Tags Main Suite")
}

var _ = Describe("User Node Tags Main", func() {
	var (
		ctx           context.Context
		testUser1     *user.User
		userContext   *rmngctx.RmngContext
		testUser2     *user.User
		iotDataClient *mock.IoTDataPlaneMock
		nodeID        string
		testGroup     *group.Group
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()

		testUser1, userContext = test_utils.SetupTestUser(ctx, "user-1-id", "user1@example.com")
		testUser2, _ = test_utils.SetupTestUser(ctx, "user-2-id", "user2@example.com")
		iotDataClient = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
		nodeID = "test-node-1"

		// Create a group owned by testUser1
		var err error
		testGroup, err = group.CreateGroupForUser(userContext, "Test Group")
		Expect(err).To(BeNil())
		Expect(testGroup).ToNot(BeNil())

		// nodeID must be an actual member of the group: the handler authorizes the
		// specific node against the caller's context, which is granted node
		// permissions only for nodes that belong to the group.
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, nodeID)
	})

	Describe("GET /v1/groups/{groupId}/nodes/{nodeId}/tags", func() {
		It("should return empty user tags when no shadow exists", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body UserTagsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.User).To(BeEmpty())
		})

		It("should return only user tags from the indexed shadow", func() {
			// Set up all tag types
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode := node.NewNode(nodeID)
			testNode.AddTags(systemContext, []string{"env:prod"}, node.TagTypeAdmin)
			testNode.AddTags(systemContext, []string{"model:esp32"}, node.TagTypeDevice)
			testNode.AddTags(systemContext, []string{"room:kitchen", "color:white"}, node.TagTypeUser)

			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body UserTagsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.User["room"]).To(Equal("kitchen"))
			Expect(body.User["color"]).To(Equal("white"))

			// Verify admin and device tags are NOT exposed
			bodyMap := make(map[string]interface{})
			json.Unmarshal([]byte(response.Body), &bodyMap)
			Expect(bodyMap).ToNot(HaveKey("admin"))
			Expect(bodyMap).ToNot(HaveKey("device"))
		})

		It("should return 403 for user without group access", func() {
			response := callHandler(ctx, testUser2.GetID(), testGroup.GroupID, nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("don't have access"))
		})

		It("should return 400 for missing groupId", func() {
			response := callHandler(ctx, testUser1.GetID(), "", nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("groupId is required"))
		})

		It("should return 400 for missing nodeId", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, "", "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("nodeId is required"))
		})
	})

	Describe("PUT /v1/groups/{groupId}/nodes/{nodeId}/tags", func() {
		It("should write user tags", func() {
			body := `{"user": {"room": "bedroom", "nickname": "main-light"}}`
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			Expect(iotDataClient.VerifyTags(nodeID,
				nil, nil,
				map[string]string{"room": "bedroom", "nickname": "main-light"},
			)).To(BeTrue())
		})

		It("should delete user tags by setting value to null", func() {
			// First add some user tags
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode := node.NewNode(nodeID)
			testNode.AddTags(systemContext, []string{"room:kitchen", "color:white"}, node.TagTypeUser)

			// Delete one tag
			body := `{"user": {"color": null}}`
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify remaining tag
			Expect(iotDataClient.VerifyTags(nodeID,
				nil, nil,
				map[string]string{"room": "kitchen"},
			)).To(BeTrue())
		})

		It("should preserve admin and device tags when writing user tags", func() {
			// Set up admin and device tags
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode := node.NewNode(nodeID)
			testNode.AddTags(systemContext, []string{"env:prod"}, node.TagTypeAdmin)
			testNode.AddTags(systemContext, []string{"model:esp32"}, node.TagTypeDevice)

			// Write user tags
			body := `{"user": {"room": "kitchen"}}`
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify all tags are present
			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"env": "prod"},
				map[string]string{"model": "esp32"},
				map[string]string{"room": "kitchen"},
			)).To(BeTrue())
		})

		It("should return 403 for user without group access", func() {
			body := `{"user": {"room": "bedroom"}}`
			response := callHandler(ctx, testUser2.GetID(), testGroup.GroupID, nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should return 400 for empty tags", func() {
			body := `{"user": {}}`
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("No tags provided"))
		})

		It("should return 400 for invalid JSON", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "PUT", "not json")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Cross-tenant node isolation (IDOR prevention)", func() {
		// Regression guard for the SystemActor-bypass IDOR: the caller has access to
		// the group, but the requested node is NOT a member of it. Both GET and PUT
		// must be rejected rather than read/write the foreign node's shadow.
		foreignNode := "foreign-node-not-in-group"

		It("should return 403 on GET when the node is not a member of the group", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, foreignNode, "GET", "")
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("don't have access to this node"))
		})

		It("should return 403 on PUT when the node is not a member of the group", func() {
			body := `{"user": {"room": "attacker"}}`
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, foreignNode, "PUT", body)
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("don't have access to this node"))
		})
	})

	Describe("Method not allowed", func() {
		It("should return 405 for POST", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "POST", "")
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("should return 405 for DELETE", func() {
			response := callHandler(ctx, testUser1.GetID(), testGroup.GroupID, nodeID, "DELETE", "")
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})
})

func callHandler(ctx context.Context, userID string, groupID string, nodeID string, method string, body string) events.APIGatewayProxyResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: method,
		Resource:   "/v1/groups/{groupId}/nodes/{nodeId}/tags",
		Path:       "/v1/groups/" + groupID + "/nodes/" + nodeID + "/tags",
		PathParameters: map[string]string{
			"groupId": groupID,
			"nodeId":  nodeID,
		},
		Body: body,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
	}

	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	return response
}
