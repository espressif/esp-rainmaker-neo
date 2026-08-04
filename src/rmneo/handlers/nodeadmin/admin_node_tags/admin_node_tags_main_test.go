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

func TestAdminNodeTagsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admin Node Tags Main Suite")
}

var _ = Describe("Admin Node Tags Main", func() {
	var (
		ctx           context.Context
		adminUser     *user.User
		adminContext  *rmngctx.RmngContext
		testUser      *user.User
		iotDataClient *mock.IoTDataPlaneMock
		nodeID        string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()

		adminUser, adminContext = test_utils.SetupTestAdminUser(ctx, "admin-user-id", "admin@example.com")
		testUser, _ = test_utils.SetupTestUser(ctx, "test-user-id", "test@example.com")
		iotDataClient = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
		nodeID = "test-node-1"
	})

	Describe("GET /v1/admin/nodes/{nodeId}/tags", func() {
		It("should return empty tags when no shadow exists", func() {
			response := callHandler(ctx, adminUser.GetID(), nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body TagsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.Admin).To(BeEmpty())
			Expect(body.Device).To(BeEmpty())
			Expect(body.User).To(BeEmpty())
		})

		It("should return all tag types from the indexed shadow", func() {
			// Set up tags on the node
			testNode := node.NewNode(nodeID)
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode.AddTags(systemContext, []string{"env:prod", "region:us-west"}, node.TagTypeAdmin)
			testNode.AddTags(systemContext, []string{"model:esp32"}, node.TagTypeDevice)
			testNode.AddTags(systemContext, []string{"room:kitchen"}, node.TagTypeUser)

			response := callHandler(ctx, adminUser.GetID(), nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body TagsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.Admin["env"]).To(Equal("prod"))
			Expect(body.Admin["region"]).To(Equal("us-west"))
			Expect(body.Device["model"]).To(Equal("esp32"))
			Expect(body.User["room"]).To(Equal("kitchen"))
		})

		It("should return 403 for non-admin user", func() {
			response := callHandlerAsEndUser(ctx, testUser.GetID(), nodeID, "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})

		It("should return 400 for missing nodeId", func() {
			response := callHandler(ctx, adminUser.GetID(), "", "GET", "")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("nodeId is required"))
		})
	})

	Describe("PUT /v1/admin/nodes/{nodeId}/tags", func() {
		It("should write admin tags", func() {
			body := `{"admin": {"env": "staging", "team": "backend"}}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify tags were written
			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"env": "staging", "team": "backend"},
				nil, nil,
			)).To(BeTrue())
		})

		It("should write user tags as admin", func() {
			body := `{"user": {"room": "bedroom", "nickname": "main-light"}}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify user tags were written
			Expect(iotDataClient.VerifyTags(nodeID,
				nil, nil,
				map[string]string{"room": "bedroom", "nickname": "main-light"},
			)).To(BeTrue())
		})

		It("should write both admin and user tags in one request", func() {
			body := `{"admin": {"env": "prod"}, "user": {"room": "kitchen"}}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"env": "prod"},
				nil,
				map[string]string{"room": "kitchen"},
			)).To(BeTrue())
		})

		It("should delete tags by setting value to null", func() {
			// First add some tags
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode := node.NewNode(nodeID)
			testNode.AddTags(systemContext, []string{"env:prod", "region:us-west"}, node.TagTypeAdmin)

			// Now delete one tag by setting it to null
			body := `{"admin": {"env": null}}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify the remaining tag is still present
			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"region": "us-west"},
				nil, nil,
			)).To(BeTrue())
		})

		It("should return 403 for non-admin user", func() {
			body := `{"admin": {"env": "prod"}}`
			response := callHandlerAsEndUser(ctx, testUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should return 400 for empty tags", func() {
			body := `{}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("No tags provided"))
		})

		It("should return 400 for invalid JSON", func() {
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", "not json")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should preserve device tags when writing admin tags", func() {
			// Set up device tags
			systemContext := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
			testNode := node.NewNode(nodeID)
			testNode.AddTags(systemContext, []string{"model:esp32"}, node.TagTypeDevice)

			// Write admin tags
			body := `{"admin": {"env": "prod"}}`
			response := callHandler(ctx, adminUser.GetID(), nodeID, "PUT", body)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify device tags are still present
			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"env": "prod"},
				map[string]string{"model": "esp32"},
				nil,
			)).To(BeTrue())
		})
	})

	Describe("Method not allowed", func() {
		It("should return 405 for POST", func() {
			response := callHandler(ctx, adminUser.GetID(), nodeID, "POST", "")
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("should return 405 for DELETE", func() {
			response := callHandler(ctx, adminUser.GetID(), nodeID, "DELETE", "")
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	// Suppress unused variable warnings
	_ = adminContext
})

func callHandler(ctx context.Context, userID string, nodeID string, method string, body string) events.APIGatewayProxyResponse {
	return callHandlerWithProvider(ctx, userID, ":CognitoSignIn:"+userID, nodeID, method, body)
}

// callHandlerAsEndUser drives the handler as a passwordless OIDC end user: the provider string is "<issuer>:<sub>" so extractCallerIdentity resolves the caller via ResolveESPUserByID.
func callHandlerAsEndUser(ctx context.Context, userID string, nodeID string, method string, body string) events.APIGatewayProxyResponse {
	return callHandlerWithProvider(ctx, userID, "https://issuer.example:"+userID, nodeID, method, body)
}

func callHandlerWithProvider(ctx context.Context, userID, provider, nodeID, method, body string) events.APIGatewayProxyResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: method,
		Resource:   "/v1/admin/nodes/{nodeId}/tags",
		Path:       "/v1/admin/nodes/" + nodeID + "/tags",
		PathParameters: map[string]string{
			"nodeId": nodeID,
		},
		Body: body,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: provider,
			},
		},
	}

	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	return response
}
