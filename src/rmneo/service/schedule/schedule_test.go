// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ScheduleService", func() {
	var (
		scheduleService *ScheduleService
		testUser        *user.User
		rmngCtx         *rmngctx.RmngContext
		testNodeID      string
		iotDataClient   *mock.IoTDataPlaneMock
	)

	BeforeEach(func() {
		// Initialize service registry
		service.Initialize()
		// Register schedule service
		Register()

		test_utils.TestSetup()
		scheduleService = NewScheduleService()
		testNodeID = "test-node-id"

		testUser = user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodePutConfig.String(), testNodeID)
		testUser.Permissions.SetAllow(utils.NodeDeleteConfig.String(), testNodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)

		// Initialize MQTT client
		iotDataClient = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	})

	It("should return empty object when node config doesn't exist", func() {
		data, err := scheduleService.Get(rmngCtx, testNodeID)
		Expect(err).To(BeNil())
		Expect(data).To(HaveLen(0))
	})

	It("should successfully store and retrieve schedule data", func() {
		// API contract uses snake_case "schedules".
		scheduleData := map[string]interface{}{
			"schedules": []interface{}{
				map[string]interface{}{
					"id":      "schedule-1",
					"name":    "Morning Schedule",
					"enabled": true,
				},
			},
		}

		// Put schedule data
		err := scheduleService.Put(rmngCtx, testNodeID, scheduleData)
		Expect(err).To(BeNil())

		// Verify MQTT message was published
		Expect(iotDataClient.PublishCalls).To(HaveLen(1))
		expectedTopic := fmt.Sprintf("rainmaker/nodes/%s/from_cloud", testNodeID)
		Expect(*iotDataClient.PublishCalls[0].Topic).To(Equal(expectedTopic))

		// Verify MQTT message content
		var publishedData map[string]interface{}
		err = json.Unmarshal(iotDataClient.PublishCalls[0].Payload, &publishedData)
		Expect(err).To(BeNil())
		Expect(publishedData["event"]).To(Equal([]interface{}{"getSchedDetails"}))

		// Verify getSchedDetails contains the schedule data and version.
		// MQTT keeps the firmware-shape key "Schedules" — the cloud only
		// renames on the REST API side.
		getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "getSchedDetails should be a map")
		Expect(getSchedDetails).To(HaveKey("version"), "getSchedDetails should contain version field")
		Expect(getSchedDetails["Schedules"]).To(Equal(scheduleData["schedules"]))

		// Get schedule data — API response uses the snake_case key.
		retrievedData, err := scheduleService.Get(rmngCtx, testNodeID)
		Expect(err).To(BeNil())

		retrievedMap, ok := retrievedData.(map[string]interface{})
		Expect(ok).To(BeTrue(), "Retrieved data should be a map")
		Expect(retrievedMap).To(HaveKey("schedules"))
	})

	It("should handle delete operation without error", func() {
		// API contract uses snake_case "schedules".
		initialData := map[string]interface{}{
			"schedules": []interface{}{
				map[string]interface{}{
					"id":      "schedule-1",
					"name":    "Test Schedule",
					"enabled": true,
				},
			},
		}

		// Put initial data
		err := scheduleService.Put(rmngCtx, testNodeID, initialData)
		Expect(err).To(BeNil())

		// Verify the Put operation MQTT message
		Expect(iotDataClient.PublishCalls).To(HaveLen(1))
		expectedTopic := fmt.Sprintf("rainmaker/nodes/%s/from_cloud", testNodeID)
		Expect(*iotDataClient.PublishCalls[0].Topic).To(Equal(expectedTopic))

		// Verify the Put operation MQTT message content
		var putPublishedData map[string]interface{}
		err = json.Unmarshal(iotDataClient.PublishCalls[0].Payload, &putPublishedData)
		fmt.Printf("Before deletion: putPublishedData: %+v\n", putPublishedData)
		Expect(err).To(BeNil())
		Expect(putPublishedData["event"]).To(Equal([]interface{}{"getSchedDetails"}))

		// Verify getSchedDetails contains the schedule data and version.
		// MQTT keeps "Schedules" (firmware shape).
		putGetSchedDetails, ok := putPublishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "getSchedDetails should be a map")
		Expect(putGetSchedDetails).To(HaveKey("version"), "getSchedDetails should contain version field")
		Expect(putGetSchedDetails["Schedules"]).To(Equal(initialData["schedules"]))

		// Clear the MQTT client calls for the delete operation
		iotDataClient.PublishCalls = nil

		// Delete schedule data
		err = scheduleService.Delete(rmngCtx, testNodeID)
		Expect(err).To(BeNil())

		// Verify MQTT message was published
		Expect(iotDataClient.PublishCalls).To(HaveLen(1))
		expectedTopic = fmt.Sprintf("rainmaker/nodes/%s/from_cloud", testNodeID)
		Expect(*iotDataClient.PublishCalls[0].Topic).To(Equal(expectedTopic))

		// Verify MQTT message content
		var publishedData map[string]interface{}
		err = json.Unmarshal(iotDataClient.PublishCalls[0].Payload, &publishedData)
		Expect(err).To(BeNil())

		// Print the actual published data for debugging
		fmt.Printf("Published data after deletion: %+v\n", publishedData)

		// Verify event field
		Expect(publishedData["event"]).To(Equal([]interface{}{"getSchedDetails"}))

		// Verify getSchedDetails contains only version field (empty schedule)
		getSchedDetails, ok := publishedData["getSchedDetails"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "getSchedDetails should be a map")
		Expect(getSchedDetails).To(HaveKey("version"), "getSchedDetails should contain version field")
		Expect(getSchedDetails).To(HaveLen(1), "getSchedDetails should only contain version field after deletion")
	})

	It("should fail when user doesn't have permission", func() {
		unauthorizedUser := user.NewUser("unauthorized-user")
		unauthorizedCtx := rmngctx.NewRmngContext(unauthorizedUser)

		// Attempt to get schedule data
		_, err := scheduleService.Get(unauthorizedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to node schedule"))

		// Attempt to put schedule data
		err = scheduleService.Put(unauthorizedCtx, testNodeID, map[string]interface{}{"key": "value"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to update node schedule"))

		// Attempt to delete schedule data
		err = scheduleService.Delete(unauthorizedCtx, testNodeID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unauthorized access to delete node schedule"))
	})

	Describe("Sharing access", func() {
		var (
			ownerUser *user.User
			ownerCtx  *rmngctx.RmngContext
			groupID   string
		)

		scheduleData := func() map[string]interface{} {
			return map[string]interface{}{
				"schedules": []interface{}{
					map[string]interface{}{
						"id":      "s1",
						"name":    "Morning",
						"enabled": true,
					},
				},
			}
		}

		BeforeEach(func() {
			ownerUser = user.NewUser("owner-user")
			ownerUser.Permissions.SetAllow(utils.GroupCreate.String(), "*")
			ownerCtx = rmngctx.NewRmngContext(ownerUser)

			createdGroup, err := group.CreateGroupForUser(ownerCtx, "Test Group")
			Expect(err).To(BeNil())
			Expect(createdGroup).ToNot(BeNil())
			groupID = createdGroup.GroupID

			test_utils.ManuallyAddNodeToGroup(context.Background(), groupID, testNodeID)
			ownerUser.Permissions.SetAllow(utils.GroupShare.String(), groupID)

			err = user.LoadNodePermissions(ownerCtx, groupID, testNodeID)
			Expect(err).To(BeNil())
		})

		It("primary access: owner can get/put/delete schedule via group ownership", func() {
			err := scheduleService.Put(ownerCtx, testNodeID, scheduleData())
			Expect(err).To(BeNil())

			retrieved, err := scheduleService.Get(ownerCtx, testNodeID)
			Expect(err).To(BeNil())
			retrievedMap, ok := retrieved.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(retrievedMap).To(HaveKey("schedules"))

			err = scheduleService.Delete(ownerCtx, testNodeID)
			Expect(err).To(BeNil())
		})

		It("secondary access: full group share grants get/put/delete then unshare revokes", func() {
			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err := scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node schedule"))

			_, err = group.ShareGroup(ownerCtx, groupID, "shared-user", utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
			Expect(err).To(BeNil())

			err = scheduleService.Put(sharedCtx, testNodeID, scheduleData())
			Expect(err).To(BeNil())

			retrieved, err := scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(BeNil())
			retrievedMap, ok := retrieved.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(retrievedMap).To(HaveKey("schedules"))

			err = scheduleService.Delete(sharedCtx, testNodeID)
			Expect(err).To(BeNil())

			err = group.UnshareGroup(ownerCtx, groupID, "shared-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node schedule"))

			err = scheduleService.Put(sharedCtx, testNodeID, scheduleData())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to update node schedule"))

			err = scheduleService.Delete(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to delete node schedule"))
		})

		It("subgroup access: subgroup share grants get/put/delete then unshare revokes", func() {
			createdSubgroup, err := group.CreateSubGroup(ownerCtx, groupID, "Test Subgroup")
			Expect(err).To(BeNil())
			Expect(createdSubgroup).ToNot(BeNil())
			subgroupID := createdSubgroup.SubGroupID

			_, err = group.UpdateNodeAndSubgroup(ownerCtx, groupID, testNodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			sharedUser := user.NewUser("shared-user")
			sharedCtx := rmngctx.NewRmngContext(sharedUser)

			_, err = scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node schedule"))

			_, err = group.ShareSubGroup(ownerCtx, groupID, subgroupID, "shared-user", auth.UserInfo{})
			Expect(err).To(BeNil())

			sharingRequests, err := group.GetMySharingRequests(sharedCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(sharedCtx, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			err = user.LoadNodePermissions(sharedCtx, groupID, testNodeID)
			Expect(err).To(BeNil())

			err = scheduleService.Put(sharedCtx, testNodeID, scheduleData())
			Expect(err).To(BeNil())

			retrieved, err := scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(BeNil())
			retrievedMap, ok := retrieved.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(retrievedMap).To(HaveKey("schedules"))

			err = scheduleService.Delete(sharedCtx, testNodeID)
			Expect(err).To(BeNil())

			err = group.UnshareSubGroup(ownerCtx, groupID, subgroupID, "shared-user")
			Expect(err).To(BeNil())

			sharedUser = user.NewUser("shared-user")
			sharedCtx = rmngctx.NewRmngContext(sharedUser)

			_, err = scheduleService.Get(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to node schedule"))

			err = scheduleService.Put(sharedCtx, testNodeID, scheduleData())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to update node schedule"))

			err = scheduleService.Delete(sharedCtx, testNodeID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unauthorized access to delete node schedule"))
		})
	})
})
