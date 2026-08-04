// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/automation"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGroupService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Group Service Suite")
}

// MockGroupService implements service.GroupService for testing
type MockGroupService struct {
	supportsResourceID bool
	getData            interface{}
	getError           error
	putResult          interface{}
	putError           error
	deleteError        error
	name               string
}

func (m *MockGroupService) Get(rmngCtx *rmngctx.RmngContext, groupID string) (interface{}, error) {
	return m.getData, m.getError
}

func (m *MockGroupService) GetWithResourceID(rmngCtx *rmngctx.RmngContext, groupID, resourceID string) (interface{}, error) {
	return m.getData, m.getError
}

func (m *MockGroupService) Put(rmngCtx *rmngctx.RmngContext, groupID string, data interface{}) (interface{}, error) {
	return m.putResult, m.putError
}

func (m *MockGroupService) PutWithResourceID(rmngCtx *rmngctx.RmngContext, groupID, resourceID string, data interface{}) (interface{}, error) {
	return m.putResult, m.putError
}

func (m *MockGroupService) Delete(rmngCtx *rmngctx.RmngContext, groupID string) error {
	return m.deleteError
}

func (m *MockGroupService) DeleteWithResourceID(rmngCtx *rmngctx.RmngContext, groupID, resourceID string) error {
	return m.deleteError
}

func (m *MockGroupService) SupportsResourceID() bool {
	return m.supportsResourceID
}

func (m *MockGroupService) GetName() string {
	return m.name
}

func (m *MockGroupService) HasVersion() bool {
	return false
}

var _ = Describe("GroupService", func() {
	var (
		testUser    *user.User
		testGroupID string
		testCtx     context.Context
		request     events.APIGatewayProxyRequest
	)

	BeforeEach(func() {
		// Initialize service registry and register services
		service.Initialize()
		service.Registry().RegisterGroupService(automation.NewAutomationService())

		// Setup test environment
		test_utils.TestSetup()

		testGroupID = "test-group-id"
		testCtx = context.Background()

		// Set up user using helper function
		testUser, _ = test_utils.SetupTestUser(testCtx, "test-user-id", "test-user@example.com")

		// Setup API Gateway request
		request = events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			PathParameters: map[string]string{
				"groupId":     testGroupID,
				"serviceName": "automations",
			},
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             testUser.GetID(),
					CognitoAuthenticationProvider: "https://issuer.example:" + testUser.GetID(),
				},
			},
		}

		// Setup group access
		groupData := &group.GroupInDB{
			GroupID:    testGroupID,
			GroupName:  "Test Group",
			SubGroupID: "NONE",
		}
		groupDB := group_db.NewGroupDB(rmngctx.NewRmngContext(testUser))
		err := groupDB.CreateGroup(testGroupID, groupData.GroupName)
		Expect(err).To(BeNil())

		// Map user to group
		userGroupDB := user_group_db.NewUserGroupDB(rmngctx.NewRmngContext(testUser))
		err = userGroupDB.CreateUserGroup(testGroupID)
		Expect(err).To(BeNil())
	})

	It("should successfully get automation service data", func() {
		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())

		// We expect the request to be handled, even if data isn't present
		statusOK := response.StatusCode == http.StatusOK
		statusNotFound := response.StatusCode == http.StatusNotFound
		Expect(statusOK || statusNotFound).To(BeTrue())
	})

	It("should fail when user doesn't have access to the group", func() {
		// Setup request with unauthorized group
		request.PathParameters["groupId"] = "unauthorized-group-id"

		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusForbidden))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("You don't have access to this group"))
	})

	It("should successfully create service data with POST", func() {
		// Setup test data for the request
		testData := map[string]interface{}{
			"name": "Created Automation",
		}

		// Convert data to JSON
		dataJSON, err := json.Marshal(testData)
		Expect(err).To(BeNil())

		// Setup POST request (create — the service assigns the ID)
		postRequest := request
		postRequest.HTTPMethod = "POST"
		postRequest.Body = string(dataJSON)

		// Make request
		response, err := HandleRequest(testCtx, postRequest)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		// Verify response: create echoes the generated automation_id
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("automation_id"))
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("success"))
	})

	It("should successfully update service data with PUT", func() {
		// Setup test data for the request
		testData := map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"id":     "rule1",
					"name":   "Updated Rule",
					"active": true,
				},
			},
		}

		// Convert data to JSON
		dataJSON, err := json.Marshal(testData)
		Expect(err).To(BeNil())

		// Setup PUT request addressing a specific automation by ID (update)
		putRequest := request
		putRequest.HTTPMethod = "PUT"
		putRequest.PathParameters["resourceId"] = "abc"
		putRequest.Body = string(dataJSON)

		// Make request
		response, err := HandleRequest(testCtx, putRequest)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		// Verify response: update returns only a success message
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("success"))
	})

	It("should fail PUT with invalid JSON payload", func() {
		// Setup PUT request with invalid JSON
		putRequest := request
		putRequest.HTTPMethod = "PUT"
		putRequest.Body = "invalid json"

		// Make request
		response, err := HandleRequest(testCtx, putRequest)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusBadRequest))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("Invalid request payload"))
	})

	It("should successfully delete service data", func() {
		// Setup DELETE request
		deleteRequest := request
		deleteRequest.HTTPMethod = "DELETE"

		// Make request
		response, err := HandleRequest(testCtx, deleteRequest)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("success"))
	})

	It("should return 404 for non-existent service", func() {
		// Setup request with non-existent service
		request.PathParameters["serviceName"] = "non-existent-service"

		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusNotFound))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
	})

	It("should return 404 for service that's not a GroupService", func() {
		// Since we can't directly register a non-GroupService, we'll simulate this
		// by using a service name that doesn't exist
		request.PathParameters["serviceName"] = "not-a-group-service"

		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())
		// It will be not found since the service doesn't exist
		Expect(response.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("should return 405 for unsupported HTTP method", func() {
		// Setup request with unsupported method (PATCH is not handled)
		request.HTTPMethod = "PATCH"

		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("Method not allowed"))
	})
})
