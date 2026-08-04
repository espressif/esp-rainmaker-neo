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

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNodeService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Service Suite")
}

var _ = Describe("NodeService", func() {
	var (
		testUser   *user.User
		testNodeID string
		testCtx    context.Context
		request    events.APIGatewayProxyRequest
		groupID    string
	)

	BeforeEach(func() {
		// Initialize service registry and register services
		service.Initialize()
		schedule.Register()
		config.Register()

		// Setup test environment
		test_utils.TestSetup()

		testNodeID = "test-node-id"
		groupID = "test-group-id"
		testCtx = context.Background()

		// Set up user using helper function
		testUser, _ = test_utils.SetupTestUser(testCtx, "test-user-id", "test-user@example.com")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodePutConfig.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodeDeleteConfig.String(), testNodeID)

		// Setup API Gateway request
		request = events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			Resource:   "/v1/groups/{groupId}/nodes/{nodeId}/{serviceName}",
			PathParameters: map[string]string{
				"groupId":     groupID,
				"nodeId":      testNodeID,
				"serviceName": "schedule",
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
			GroupID:    groupID,
			GroupName:  "Test Group",
			SubGroupID: "NONE",
		}
		groupDB := group_db.NewGroupDB(rmngctx.NewRmngContext(testUser))
		err := groupDB.CreateGroup(groupID, groupData.GroupName)
		Expect(err).To(BeNil())

		// Map user to group
		userGroupDB := user_group_db.NewUserGroupDB(rmngctx.NewRmngContext(testUser))
		err = userGroupDB.CreateUserGroup(groupID)
		Expect(err).To(BeNil())

		// Map node to group
		test_utils.ManuallyAddNodeToGroup(testCtx, groupID, testNodeID)
	})

	It("should successfully get schedule data", func() {
		// Setup test data
		scheduleData := map[string]interface{}{
			"schedule": map[string]interface{}{
				"Schedules": []interface{}{
					map[string]interface{}{
						"id":      "schedule-1",
						"name":    "Morning Schedule",
						"enabled": true,
					},
				},
			},
		}

		// Store test data
		rmngCtx := rmngctx.NewRmngContext(testUser)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		err := nodeDetailsDB.UpdateServiceDataWithVersion(testNodeID, "schedule", scheduleData)
		Expect(err).To(BeNil())

		// Make request
		response, err := HandleRequest(testCtx, request)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("schedules"))
	})

	It("should fail when user doesn't have access to the group", func() {
		// Setup test data with a different group
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

	It("should successfully update schedule data", func() {
		// API contract uses snake_case "schedules". The schedule service translates this to the firmware key "Schedules" before storage.
		scheduleData := map[string]interface{}{
			"schedules": []interface{}{
				map[string]interface{}{
					"id":      "schedule-1",
					"name":    "Morning Schedule",
					"enabled": true,
				},
			},
		}

		// Convert schedule data to JSON
		scheduleJSON, err := json.Marshal(scheduleData)
		Expect(err).To(BeNil())

		// Setup PUT request
		putRequest := request
		putRequest.HTTPMethod = "PUT"
		putRequest.Body = string(scheduleJSON)

		// Make request
		response, err := HandleRequest(testCtx, putRequest)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))

		// Verify response
		var responseData map[string]interface{}
		err = json.Unmarshal([]byte(response.Body), &responseData)
		Expect(err).To(BeNil())
		Expect(responseData).To(HaveKey("message"))
		Expect(responseData["message"]).To(Equal("success"))

		// Verify data was stored
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngctx.NewRmngContext(testUser))
		nd, err := nodeDetailsDB.GetNodeDetails(testNodeID)
		Expect(err).To(BeNil())
		Expect(nd).ToNot(BeNil())
		// Storage uses the firmware-shape key "Schedules"; the schedule service translates from the API "schedules" key on the way in.
		storedScheduleData, err := nd.GetServiceData("schedule")
		Expect(err).To(BeNil())
		Expect(storedScheduleData).To(Equal(map[string]interface{}{
			"Schedules": scheduleData["schedules"],
		}))
	})

	It("should fail to update schedule data with invalid JSON", func() {
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

	It("should successfully delete schedule data", func() {
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

		// Verify data was deleted
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngctx.NewRmngContext(testUser))
		nd, err := nodeDetailsDB.GetNodeDetails(testNodeID)
		Expect(err).To(BeNil())
		Expect(nd).ToNot(BeNil())
		deletedScheduleData, err := nd.GetServiceData("schedule")
		Expect(err).To(BeNil())
		Expect(deletedScheduleData).To(BeNil())
	})

	Context("config service PUT", func() {
		var putConfigRequest events.APIGatewayProxyRequest

		validMatterConfigJSON := func() string {
			body, err := json.Marshal(map[string]interface{}{
				"data_model": "matter",
				"info": map[string]interface{}{
					"name":       "Living Room Light",
					"type":       "matter",
					"fw_version": "1.0",
					"model":      "0x010D",
				},
				"endpoints": map[string]interface{}{
					"0x0": map[string]interface{}{
						"dt": "0x0016",
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x1d": map[string]interface{}{},
								"0x28": map[string]interface{}{},
								"0x1f": map[string]interface{}{},
								"0x3e": map[string]interface{}{},
							},
						},
					},
					"0x1": map[string]interface{}{
						"dt": "0x010D",
						"c": map[string]interface{}{
							"s": map[string]interface{}{
								"0x3":   map[string]interface{}{},
								"0x4":   map[string]interface{}{},
								"0x6":   map[string]interface{}{"a": []interface{}{"0x0"}},
								"0x8":   map[string]interface{}{"a": []interface{}{"0x0"}},
								"0x300": map[string]interface{}{"a": []interface{}{"0x7", "0x8", "0x0f"}},
								"0x1d":  map[string]interface{}{},
							},
						},
					},
				},
			})
			Expect(err).To(BeNil())
			return string(body)
		}

		BeforeEach(func() {
			putConfigRequest = request
			putConfigRequest.HTTPMethod = "PUT"
			putConfigRequest.PathParameters["serviceName"] = "config"
			putConfigRequest.Body = validMatterConfigJSON()
		})

		It("returns 200 when writing config for a pure Matter node", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(testCtx, groupID, testNodeID, group.MatterCapabilityName)

			response, err := HandleRequest(testCtx, putConfigRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("returns 403 for a RainMaker (non-pure-matter) node", func() {
			test_utils.ManuallyAddNodeToGroupWithCapabilities(testCtx, groupID, testNodeID, group.NodeCapabilityRMNG)

			response, err := HandleRequest(testCtx, putConfigRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})
	})
})
