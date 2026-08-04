// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func VerifyGetGroupInfo(iotDataClient *mock.IoTDataPlaneMock, nodeID string, groupID string, subGroupIDs []string) {
	Expect(iotDataClient.PublishCalls).To(HaveLen(1))
	topic := "rainmaker/nodes/" + nodeID + "/from_cloud"
	Expect(iotDataClient.PublishCalls[0].Topic).To(Equal(&topic))

	expectedPayload := ""
	if len(subGroupIDs) > 0 {
		expectedPayload = fmt.Sprintf(`{"event":["getGroupInfo"],"getGroupInfo":{"pgrp":"%s","subgrps":["%s"]}}`, groupID, strings.Join(subGroupIDs, `","`))
	} else {
		expectedPayload = fmt.Sprintf(`{"event":["getGroupInfo"],"getGroupInfo":{"pgrp":"%s"}}`, groupID)
	}
	Expect(iotDataClient.PublishCalls[0].Payload).To(ContainSubstring(expectedPayload))
}

// Helper function to add tags of multiple types to a node
func AddTagsToNode(testNode *node.Node, testUserContext *rmngctx.RmngContext, adminTags, deviceTags, userTags []string) {
	if len(adminTags) > 0 {
		err := testNode.AddTags(testUserContext, adminTags, node.TagTypeAdmin)
		Expect(err).To(BeNil())
	}

	if len(deviceTags) > 0 {
		err := testNode.AddTags(testUserContext, deviceTags, node.TagTypeDevice)
		Expect(err).To(BeNil())
	}

	if len(userTags) > 0 {
		err := testNode.AddTags(testUserContext, userTags, node.TagTypeUser)
		Expect(err).To(BeNil())
	}
}

var _ = Describe("ShadowNode", func() {
	var (
		iotDataClient   *mock.IoTDataPlaneMock
		nodeID          string
		shadowState     node.IoTNodeShadow
		testUserContext *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		nodeID = "test-node"
		shadowState = node.IoTNodeShadow{
			State: &node.ShadowState{
				Reported: &node.ReportedOrDesiredShadow{
					Online: utils.Ptr(true),
					Params: map[string]interface{}{
						"key1": "value1",
						"key2": 42,
					},
				},
			},
		}

		iotDataClient = awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
		testUserContext = rmngctx.NewRmngContext(user.NewUser("test-user-id"))
		testUserContext.SetAllow(utils.NodeWriteShadow, "*")
	})

	Describe("AddTags", func() {
		It("should add tags to correct structure based on their type", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Add all types of tags using the helper function
			adminTags := []string{"env:prod", "region:us-west"}
			deviceTags := []string{"model:esp32", "mac:00:11:22:33:44:55"}
			userTags := []string{"room:kitchen", "color:white"}

			AddTagsToNode(testNode, testUserContext, adminTags, deviceTags, userTags)

			// Verify all tags were added correctly using the helper function
			Expect(iotDataClient.VerifyTags(nodeID,
				map[string]string{"env": "prod", "region": "us-west"},
				map[string]string{"model": "esp32", "mac": "00:11:22:33:44:55"},
				map[string]string{"room": "kitchen", "color": "white"},
			)).To(BeTrue())
		})

		It("should add admin tags to the node's shadow", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Create tags
			adminTags := []string{"environment:production", "owner:admin", "region:us-west"}

			// Add admin tags
			err := testNode.AddTags(testUserContext, adminTags, node.TagTypeAdmin)
			Expect(err).To(BeNil())

			// Verify tags were added correctly
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "production",
				"owner":       "admin",
				"region":      "us-west",
			}, nil, nil)).To(BeTrue())
		})

		It("should add device tags to the node's shadow", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Create tags
			deviceTags := []string{"model:esp32", "firmware:1.2.3", "mac:00:11:22:33:44:55"}

			// Add device tags
			err := testNode.AddTags(testUserContext, deviceTags, node.TagTypeDevice)
			Expect(err).To(BeNil())

			// Verify tags were added correctly
			Expect(iotDataClient.VerifyTags(nodeID, nil, map[string]string{
				"model":    "esp32",
				"firmware": "1.2.3",
				"mac":      "00:11:22:33:44:55",
			}, nil)).To(BeTrue())
		})

		It("should add user tags to the node's shadow", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Create tags
			userTags := []string{"location:kitchen", "name:light", "color:white"}

			// Add user tags
			err := testNode.AddTags(testUserContext, userTags, node.TagTypeUser)
			Expect(err).To(BeNil())

			// Verify tags were added correctly
			Expect(iotDataClient.VerifyTags(nodeID, nil, nil, map[string]string{
				"location": "kitchen",
				"name":     "light",
				"color":    "white",
			})).To(BeTrue())
		})

		It("should handle empty tags array", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Add empty tags array
			err := testNode.AddTags(testUserContext, []string{}, node.TagTypeAdmin)
			Expect(err).To(BeNil())
		})

		It("should update existing tags", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)

			// Add initial tags
			initialTags := []string{"environment:dev", "version:1.0"}
			err := testNode.AddTags(testUserContext, initialTags, node.TagTypeAdmin)
			Expect(err).To(BeNil())

			// Verify initial tags
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "dev",
				"version":     "1.0",
			}, nil, nil)).To(BeTrue())

			// Update tags
			updatedTags := []string{"environment:prod", "owner:admin"}
			err = testNode.AddTags(testUserContext, updatedTags, node.TagTypeAdmin)
			Expect(err).To(BeNil())

			// Verify updated tags (should contain both old and new tags)
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "prod",
				"owner":       "admin",
				"version":     "1.0",
			}, nil, nil)).To(BeTrue())
		})

		It("should preserve tags of different types when adding new tags", func() {
			// Create a new node
			testNode := node.NewNode(nodeID)
			rmngContext := testUserContext

			// Add admin tags first
			adminTags := []string{"environment:prod", "owner:admin"}
			err := testNode.AddTags(rmngContext, adminTags, node.TagTypeAdmin)
			Expect(err).To(BeNil())

			// Verify admin tags
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "prod",
				"owner":       "admin",
			}, nil, nil)).To(BeTrue())

			// Now add device tags
			deviceTags := []string{"model:esp32", "firmware:1.2.3"}
			err = testNode.AddTags(rmngContext, deviceTags, node.TagTypeDevice)
			Expect(err).To(BeNil())

			// Verify device tags were added
			Expect(iotDataClient.VerifyTags(nodeID, nil, map[string]string{
				"model":    "esp32",
				"firmware": "1.2.3",
			}, nil)).To(BeTrue())

			// Verify admin tags are still present
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "prod",
				"owner":       "admin",
			}, nil, nil)).To(BeTrue())

			// Now add user tags
			userTags := []string{"location:kitchen", "name:light"}
			err = testNode.AddTags(rmngContext, userTags, node.TagTypeUser)
			Expect(err).To(BeNil())

			// Verify all three types of tags are present
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "prod",
				"owner":       "admin",
			}, nil, nil)).To(BeTrue())

			Expect(iotDataClient.VerifyTags(nodeID, nil, map[string]string{
				"model":    "esp32",
				"firmware": "1.2.3",
			}, nil)).To(BeTrue())

			Expect(iotDataClient.VerifyTags(nodeID, nil, nil, map[string]string{
				"location": "kitchen",
				"name":     "light",
			})).To(BeTrue())

			// Update just the device tags
			updatedDeviceTags := []string{"model:esp8266", "wifi:enabled"}
			err = testNode.AddTags(rmngContext, updatedDeviceTags, node.TagTypeDevice)
			Expect(err).To(BeNil())

			// Verify device tags were updated
			Expect(iotDataClient.VerifyTags(nodeID, nil, map[string]string{
				"model":    "esp8266",
				"wifi":     "enabled",
				"firmware": "1.2.3", // This should still be present
			}, nil)).To(BeTrue())

			// Verify admin and user tags are unchanged
			Expect(iotDataClient.VerifyTags(nodeID, map[string]string{
				"environment": "prod",
				"owner":       "admin",
			}, nil, nil)).To(BeTrue())

			Expect(iotDataClient.VerifyTags(nodeID, nil, nil, map[string]string{
				"location": "kitchen",
				"name":     "light",
			})).To(BeTrue())
		})
	})

	Describe("ShadowNodeAddToGroup", func() {

		It("should be a no-op when adding a node to the group it is already in — no data deleted", func() {
			// Same-group re-association must not trigger any cleanup.
			// All existing data (shadow, attributes, schedules, triggers, automations)
			// must be preserved and the node_data_reset Lambda must NOT be invoked.

			os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
			defer os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaMock.InvokeCalls = nil

			test_utils.RegisterIoTThing(nodeID)

			newUser := user.NewUser("same-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			// Create group and add node
			grp, err := group.CreateGroupForUser(rmng_context, "my-group")
			Expect(err).To(BeNil())
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, grp.GroupID, nil)
			Expect(err).To(BeNil())
			test_utils.AssertNodeInGroup(grp.GroupID, nodeID)

			// Verify getGroupInfo was sent on initial add
			VerifyGetGroupInfo(iotDataClient, nodeID, grp.GroupID, []string{})

			// Set up shadow data that must survive
			nodeGroups := group_node_db.NodesGroups{Group: grp.GroupID}
			test_utils.SetupShadow(nodeID, shadowState, nodeGroups)

			// Set up user tags that must survive
			testNode := node.NewNode(nodeID)
			err = testNode.AddTags(rmng_context, []string{"room:kitchen", "name:light"}, node.TagTypeUser)
			Expect(err).To(BeNil())

			// Verify thing attribute set before the no-op
			test_utils.AssertGroupIDAttribute(nodeID, grp.GroupID)

			// Reset tracking before the no-op
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}
			lambdaMock.InvokeCalls = nil

			// Add again to the same group — should succeed (no-op)
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, grp.GroupID, nil)
			Expect(err).To(BeNil())

			// Node still in group
			test_utils.AssertNodeInGroup(grp.GroupID, nodeID)

			// Thing attribute preserved
			test_utils.AssertGroupIDAttribute(nodeID, grp.GroupID)

			// Shadow data preserved (not deleted)
			iotDataMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := node.GetShadowNameForNodeGroups(nodeGroups)
			Expect(iotDataMock.Shadows[nodeID]).To(HaveKey(shadowName),
				"Group shadow should still exist after same-group re-association")

			// User tags preserved (not cleared)
			Expect(iotDataMock.VerifyTags(nodeID, nil, nil, map[string]string{
				"room": "kitchen",
				"name": "light",
			})).To(BeTrue(), "User tags should be preserved after same-group re-association")

			// No MQTT messages sent (no-op should not notify device)
			Expect(iotDataClient.PublishCalls).To(BeEmpty(),
				"No MQTT notifications should be sent for same-group re-association")

			// node_data_reset Lambda must NOT have been invoked
			Expect(lambdaMock.InvokeCalls).To(BeEmpty(),
				"node_data_reset Lambda should NOT be invoked for same-group re-association")
		})

		It("should re-associate node to a different user, delete old shadow (incl. subgroups), and invoke node_data_reset", func() {
			// Matrix Scenario 2: Associate node to different user
			//   Node group entry  : old removed, new created
			//   Shadow Params     : deleted for node (old group, including subgroup shadow)
			//   Thing Attributes  : replaced with new group id
			//   Notify-to-node    : sent for new group
			//   node_data_reset   : Lambda invoked for old group

			// Enable node_data_reset Lambda
			os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
			defer os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaMock.InvokeCalls = nil

			test_utils.RegisterIoTThing(nodeID)

			// Old user creates a group, adds node, and adds node to a subgroup
			oldUser := user.NewUser("old-user")
			oldCtx := rmngctx.NewRmngContext(oldUser)
			oldCtx.SetAllow(utils.NodeAll, nodeID)

			oldGroup, err := group.CreateGroupForUser(oldCtx, "old-user-group")
			Expect(err).To(BeNil())
			err = node.ShadowNodeAddToGroup(oldCtx, nodeID, oldGroup.GroupID, nil)
			Expect(err).To(BeNil())
			test_utils.AssertNodeInGroup(oldGroup.GroupID, nodeID)
			VerifyGetGroupInfo(iotDataClient, nodeID, oldGroup.GroupID, []string{})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Add node to a subgroup in the old group
			subGroup, err := group.CreateSubGroup(oldCtx, oldGroup.GroupID, "old-subgroup")
			Expect(err).To(BeNil())
			err = node.ShadowNodeUpdateSubGroup(oldCtx, nodeID, oldGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())
			VerifyGetGroupInfo(iotDataClient, nodeID, oldGroup.GroupID, []string{subGroup.SubGroupID})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Set up the named shadow for old group+subgroup so we can verify deletion
			oldNodeGroups := group_node_db.NodesGroups{Group: oldGroup.GroupID, SubGroups: []string{subGroup.SubGroupID}}
			test_utils.SetupShadow(nodeID, shadowState, oldNodeGroups)

			// Set up user tags on the node that should be cleared on re-association
			testNode := node.NewNode(nodeID)
			err = testNode.AddTags(oldCtx, []string{"room:kitchen", "name:light"}, node.TagTypeUser)
			Expect(err).To(BeNil())
			Expect(iotDataClient.VerifyTags(nodeID, nil, nil, map[string]string{
				"room": "kitchen", "name": "light",
			})).To(BeTrue(), "Pre-condition: user tags should be set")

			// Verify group_id attribute set to old group
			test_utils.AssertGroupIDAttribute(nodeID, oldGroup.GroupID)

			// Reset tracking before re-association
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}
			lambdaMock.InvokeCalls = nil

			// New user creates their own group and re-associates the node
			newUser := user.NewUser("new-user")
			newCtx := rmngctx.NewRmngContext(newUser)
			newCtx.SetAllow(utils.NodeAll, nodeID)

			newGroup, err := group.CreateGroupForUser(newCtx, "new-user-group")
			Expect(err).To(BeNil())

			err = node.ShadowNodeAddToGroup(newCtx, nodeID, newGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Node group entry: new mapping created
			test_utils.AssertNodeInGroup(newGroup.GroupID, nodeID)

			// Node group entry: old mapping removed
			test_utils.AssertNodeNotInGroup(oldGroup.GroupID, nodeID)

			// Shadow Params: old group+subgroup shadow deleted
			test_utils.AssertShadowDeleted(nodeID, oldNodeGroups)

			// Thing Attributes: replaced with new group id
			test_utils.AssertGroupIDAttribute(nodeID, newGroup.GroupID)

			// Notify-to-node: new group info sent (last MQTT message)
			Expect(len(iotDataClient.PublishCalls)).To(BeNumerically(">=", 1))
			lastCall := iotDataClient.PublishCalls[len(iotDataClient.PublishCalls)-1]
			expectedPayload := fmt.Sprintf(`{"event":["getGroupInfo"],"getGroupInfo":{"pgrp":"%s"}}`, newGroup.GroupID)
			Expect(string(lastCall.Payload)).To(ContainSubstring(expectedPayload))

			test_utils.AssertUserTagsCleared(nodeID)

			// node_data_reset Lambda: invoked for old group
			test_utils.AssertNodeDataResetInvoked("test-node-data-reset", nodeID, oldGroup.GroupID)
		})
	})

	Describe("ShadowNodeAddToSubGroup", func() {
		It("should add node to subgroup and update shadow", func() {
			groupName := "test-group"
			subGroupName := "test-subgroup"
			subGroupName2 := "test-subgroup2"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Create a subgroup
			subGroup, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, subGroupName)
			Expect(err).To(BeNil())

			// Create another subgroup
			subGroup2, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, subGroupName2)
			Expect(err).To(BeNil())

			// Add node to main group first
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Create some data in named shadow of the thing
			oldNodeGroups := group_node_db.NodesGroups{Group: mainGroup.GroupID}
			test_utils.SetupShadow(nodeID, shadowState, oldNodeGroups)

			// Verify GetGroupInfo for main group
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Now add node to one subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify node was added to subgroup
			expectedGroup := group.Group{
				GroupID: mainGroup.GroupID,
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup.SubGroupID, SubGroupName: subGroup.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
						nodeID: {GroupID: mainGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup.SubGroupID},
					}},
					{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName},
				},
			}
			updatedGroup, err := group.LoadGroup(rmng_context, mainGroup.GroupID)
			Expect(err).To(BeNil())
			updatedGroup.LoadNodes(rmng_context)
			test_utils.SortGroup(updatedGroup)
			test_utils.SortGroup(&expectedGroup)
			Expect(updatedGroup.SubGroups).To(Equal(expectedGroup.SubGroups))

			// Verify GetGroupInfo for main group + subgroup
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{subGroup.SubGroupID})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Verify shadow state is the same
			newNodeGroups := oldNodeGroups
			newNodeGroups.SubGroups = []string{subGroup.SubGroupID}
			ValidateShadowMigration(node.NewNode(nodeID), oldNodeGroups, newNodeGroups, shadowState)

			// Now add node to another subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify node was added to both subgroups
			expectedGroup = group.Group{
				GroupID: mainGroup.GroupID,
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup.SubGroupID, SubGroupName: subGroup.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
						nodeID: {GroupID: mainGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup.SubGroupID, SubGrp2: subGroup2.SubGroupID},
					}},
					{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
						nodeID: {GroupID: mainGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup.SubGroupID, SubGrp2: subGroup2.SubGroupID},
					}},
				},
			}
			updatedGroup, err = group.LoadGroup(rmng_context, mainGroup.GroupID)
			Expect(err).To(BeNil())
			updatedGroup.LoadNodes(rmng_context)
			test_utils.SortGroup(updatedGroup)
			test_utils.SortGroup(&expectedGroup)
			Expect(updatedGroup.SubGroups).To(Equal(expectedGroup.SubGroups))

			// Verify GetGroupInfo for main group + 2 subgroups
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{subGroup.SubGroupID, subGroup2.SubGroupID})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Verify shadow state is the same
			oldNodeGroups = newNodeGroups
			newNodeGroups.SubGroups = append(newNodeGroups.SubGroups, subGroup2.SubGroupID)
			ValidateShadowMigration(node.NewNode(nodeID), oldNodeGroups, newNodeGroups, shadowState)

		})

		It("should remove node from subgroup and update shadow", func() {
			groupName := "test-group"
			subGroupName := "test-subgroup"
			subGroupName2 := "test-subgroup2"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Create two subgroups
			subGroup, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, subGroupName)
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, subGroupName2)
			Expect(err).To(BeNil())

			// Add node to main group
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Add node to both subgroups
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Create some data in named shadow
			oldNodeGroups := group_node_db.NodesGroups{
				Group:     mainGroup.GroupID,
				SubGroups: []string{subGroup.SubGroupID, subGroup2.SubGroupID},
			}
			test_utils.SetupShadow(nodeID, shadowState, oldNodeGroups)

			// Verify initial state
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{subGroup.SubGroupID, subGroup2.SubGroupID})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Remove node from first subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(BeNil())

			// Verify node was removed from subgroup
			expectedGroup := group.Group{
				GroupID: mainGroup.GroupID,
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup.SubGroupID, SubGroupName: subGroup.SubGroupName},
					{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
						nodeID: {GroupID: mainGroup.GroupID, NodeID: nodeID, SubGrp2: subGroup2.SubGroupID},
					}},
				},
			}
			updatedGroup, err := group.LoadGroup(rmng_context, mainGroup.GroupID)
			Expect(err).To(BeNil())
			updatedGroup.LoadNodes(rmng_context)
			test_utils.SortGroup(updatedGroup)
			test_utils.SortGroup(&expectedGroup)
			Expect(updatedGroup.SubGroups).To(Equal(expectedGroup.SubGroups))

			// Verify GetGroupInfo shows only remaining subgroup
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{subGroup2.SubGroupID})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Verify shadow state is preserved but moved to new shadow name
			newNodeGroups := group_node_db.NodesGroups{
				Group:     mainGroup.GroupID,
				SubGroups: []string{subGroup2.SubGroupID},
			}
			ValidateShadowMigration(node.NewNode(nodeID), oldNodeGroups, newNodeGroups, shadowState)

			// Remove from second subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(BeNil())

			// Verify node is now only in main group
			expectedGroup.SubGroups = []group.SubGroup{
				{SubGroupID: subGroup.SubGroupID, SubGroupName: subGroup.SubGroupName},
				{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName},
			}
			updatedGroup, err = group.LoadGroup(rmng_context, mainGroup.GroupID)
			Expect(err).To(BeNil())
			updatedGroup.LoadNodes(rmng_context)
			test_utils.SortGroup(updatedGroup)
			test_utils.SortGroup(&expectedGroup)
			Expect(updatedGroup.SubGroups).To(Equal(expectedGroup.SubGroups))

			// Verify GetGroupInfo shows no subgroups
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{})
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Verify shadow state is preserved but moved to main group shadow
			oldNodeGroups = newNodeGroups
			newNodeGroups = group_node_db.NodesGroups{Group: mainGroup.GroupID}
			ValidateShadowMigration(node.NewNode(nodeID), oldNodeGroups, newNodeGroups, shadowState)
		})

		It("should return error when removing node from non-existent subgroup", func() {
			groupName := "test-group"
			nonExistentSubGroupID := "non-existent-subgroup"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Add node to main group
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Try to remove node from non-existent subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, nonExistentSubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("subgroup does not exist"))
		})

		It("should return error when removing node from a subgroup that it is not a part of", func() {
			groupName := "test-group"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Add node to main group
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Create two subgroups
			subGroup, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, "subGroup 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, "subGroup 2")
			Expect(err).To(BeNil())

			// Add node to one subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Try to remove node from another subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("node is not in the specified subgroup"))

			// Verify the node is still in the first subgroup
			expectedGroup := group.Group{
				GroupID: mainGroup.GroupID,
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup.SubGroupID, SubGroupName: subGroup.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
						nodeID: {GroupID: mainGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup.SubGroupID},
					}},
					{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName},
				},
			}
			updatedGroup, err := group.LoadGroup(rmng_context, mainGroup.GroupID)
			Expect(err).To(BeNil())
			updatedGroup.LoadNodes(rmng_context)
			test_utils.SortGroup(updatedGroup)
			test_utils.SortGroup(&expectedGroup)
			Expect(updatedGroup.SubGroups).To(Equal(expectedGroup.SubGroups))
		})

		It("should return an error when node is not in the main group", func() {
			nodeID := "test-node"
			groupName := "test-group"
			subGroupName := "test-subgroup"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			// Create a new group
			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Create a subgroup
			subGroup, err := group.CreateSubGroup(rmng_context, mainGroup.GroupID, subGroupName)
			Expect(err).To(BeNil())

			// Now add node to subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("node is not in the group"))
			Expect(iotDataClient.PublishCalls).To(HaveLen(0))
		})

		It("should return an error when subgroup doesn't exist", func() {
			nodeID := "test-node"
			groupName := "test-group"
			nonExistentSubGroupID := "non-existent-subgroup"

			// Create a new user and group
			newUser := user.NewUser("new-user")
			rmng_context := rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			mainGroup, err := group.CreateGroupForUser(rmng_context, groupName)
			Expect(err).To(BeNil())

			// Add node to main group
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Try to add node to non-existent subgroup
			err = node.ShadowNodeUpdateSubGroup(rmng_context, nodeID, mainGroup.GroupID, nonExistentSubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("subgroup does not exist"))
		})
	})

	Describe("ShadowNodeGroupIDAttribute", func() {
		var (
			iotClient    *mock.IoTClientMock
			rmng_context *rmngctx.RmngContext
		)

		BeforeEach(func() {
			iotClient = awscommon.GetIoTClient().(*mock.IoTClientMock)
			newUser := user.NewUser("attr-test-user")
			rmng_context = rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			// Pre-register the IoT thing so UpdateThing succeeds
			iotClient.Things[nodeID] = mock.Things{
				Name:           nodeID,
				CertificateIds: []string{},
				Groups:         []string{},
				Attributes:     make(map[string]string),
			}
		})

		It("should succeed even if IoT thing is not registered (best-effort attribute update)", func() {
			delete(iotClient.Things, nodeID)
			mainGroup, err := group.CreateGroupForUser(rmng_context, "attr-fail-group")
			Expect(err).To(BeNil())

			// ShadowNodeAddToGroup should succeed even if UpdateThingAttribute fails
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Group membership notification should still be sent
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{})
		})
	})

	Describe("ShadowNodeRemoveFromGroup", func() {
		var (
			iotClient    *mock.IoTClientMock
			rmng_context *rmngctx.RmngContext
		)

		BeforeEach(func() {
			iotClient = awscommon.GetIoTClient().(*mock.IoTClientMock)
			newUser := user.NewUser("remove-test-user")
			rmng_context = rmngctx.NewRmngContext(newUser)
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			// Pre-register the IoT thing
			iotClient.Things[nodeID] = mock.Things{
				Name:           nodeID,
				CertificateIds: []string{},
				Groups:         []string{},
				Attributes:     make(map[string]string),
			}
		})

		It("should send empty notification even when IoT thing is not registered (best-effort attribute clear)", func() {
			mainGroup, err := group.CreateGroupForUser(rmng_context, "remove-test-group-2")
			Expect(err).To(BeNil())

			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())

			// Unregister IoT thing
			delete(iotClient.Things, nodeID)
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Should not panic and should not return error (side-effects are best-effort)
			err = node.ShadowNodeRemoveFromGroup(rmng_context, nodeID, mainGroup.GroupID)
			Expect(err).To(BeNil())

			// Should still send empty notification
			Expect(iotDataClient.PublishCalls).To(HaveLen(1))
		})

		It("should clean up all matrix columns on disassociation and invoke async Lambda", func() {
			// Matrix Scenario 1: Remove node from group (disassociate)
			//   Node group entry  : deleted
			//   User Tags         : deleted (iparams user section cleared)
			//   Shadow Params     : deleted for node
			//   Thing Attributes  : group_id cleared
			//   Notify-to-node    : empty getGroupInfo sent
			//   node_data_reset   : Lambda invoked (deletes schedules, triggers, timeseries, automations)

			// Enable node_data_reset Lambda invocation in tests
			os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
			defer os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaMock.InvokeCalls = nil

			mainGroup, err := group.CreateGroupForUser(rmng_context, "full-disassoc-test")
			Expect(err).To(BeNil())

			// Add node to group
			err = node.ShadowNodeAddToGroup(rmng_context, nodeID, mainGroup.GroupID, nil)
			Expect(err).To(BeNil())
			test_utils.AssertNodeInGroup(mainGroup.GroupID, nodeID)

			// Verify getGroupInfo was sent on initial add
			VerifyGetGroupInfo(iotDataClient, nodeID, mainGroup.GroupID, []string{})

			// Set up group shadow so we can verify deletion
			oldNodeGroups := group_node_db.NodesGroups{Group: mainGroup.GroupID}
			test_utils.SetupShadow(nodeID, node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{"key": "value"},
					},
				},
			}, oldNodeGroups)

			// Set up user tags so we can verify clearing
			testNode := node.NewNode(nodeID)
			err = testNode.AddTags(rmng_context, []string{"room:kitchen", "name:light"}, node.TagTypeUser)
			Expect(err).To(BeNil())
			Expect(iotDataClient.VerifyTags(nodeID, nil, nil, map[string]string{
				"room": "kitchen", "name": "light",
			})).To(BeTrue(), "Pre-condition: user tags should be set")

			// Verify thing attribute is set before disassociation
			test_utils.AssertGroupIDAttribute(nodeID, mainGroup.GroupID)

			// Reset publish calls before disassociation
			iotDataClient.PublishCalls = []iotdataplane.PublishInput{}

			// Perform disassociation
			err = node.ShadowNodeRemoveFromGroup(rmng_context, nodeID, mainGroup.GroupID)
			Expect(err).To(BeNil())

			// Node group entry: deleted
			test_utils.AssertNodeNotInGroup(mainGroup.GroupID, nodeID)

			// Shadow Params: deleted for node
			test_utils.AssertShadowDeleted(nodeID, oldNodeGroups)

			test_utils.AssertUserTagsCleared(nodeID)

			// Thing Attributes: group_id cleared
			test_utils.AssertGroupIDAttributeCleared(nodeID)

			// Notify-to-node: empty getGroupInfo sent
			test_utils.AssertEmptyGetGroupInfoNotification(nodeID)

			// node_data_reset Lambda: invoked (async deletion of schedules, triggers, timeseries, automations)
			test_utils.AssertNodeDataResetInvoked("test-node-data-reset", nodeID, mainGroup.GroupID)
		})
	})

	Describe("ShadowOnline", func() {
		It("should report online when the reported Online field is true", func() {
			shadow := node.ReportedOrDesiredShadow{Online: utils.Ptr(true)}
			Expect(node.ShadowOnline(shadow)).To(BeTrue())
		})

		It("should report offline when the reported Online field is explicitly false", func() {
			shadow := node.ReportedOrDesiredShadow{Online: utils.Ptr(false)}
			Expect(node.ShadowOnline(shadow)).To(BeFalse())
		})

		It("should default to offline when the Online field was never reported", func() {
			shadow := node.ReportedOrDesiredShadow{}
			Expect(node.ShadowOnline(shadow)).To(BeFalse())
		})
	})

	Describe("DeviceParamsFromShadow", func() {
		It("should return the device's params map when present", func() {
			shadow := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"Switch": map[string]interface{}{"power": true},
				},
			}
			Expect(node.DeviceParamsFromShadow(shadow, "Switch")).To(Equal(map[string]interface{}{"power": true}))
		})

		It("should return an empty map when Params is nil", func() {
			shadow := node.ReportedOrDesiredShadow{}
			Expect(node.DeviceParamsFromShadow(shadow, "Switch")).To(Equal(map[string]interface{}{}))
		})

		It("should return an empty map when the device is not present in Params", func() {
			shadow := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{"OtherDevice": map[string]interface{}{"power": true}},
			}
			Expect(node.DeviceParamsFromShadow(shadow, "Switch")).To(Equal(map[string]interface{}{}))
		})

		It("should return an empty map when the device entry is not a map", func() {
			shadow := node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{"Switch": "not-a-map"},
			}
			Expect(node.DeviceParamsFromShadow(shadow, "Switch")).To(Equal(map[string]interface{}{}))
		})
	})

})
