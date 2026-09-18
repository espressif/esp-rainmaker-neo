// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iotdataplane"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/testutil"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestAddRemoveChildHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bridge AddRemoveChild Handler Suite")
}

// buildBridgePerms grants the bridge full self-permissions plus
// GroupAll on its group, mirroring what the addChild/removeChild flow
// expects from a bridge cert that's been associated to a group.
func buildBridgePerms(bridgeID, groupID string) *rbac.EntityPermissions {
	perms := make(rbac.EntityPermissions)
	perms.SetAllow(utils.NodeAll.String(), bridgeID)
	perms.SetAllow(utils.GroupAll.String(), groupID)
	return &perms
}

var _ = Describe("handleBridgeEvent", func() {
	const (
		bridgeName = "rmng-bridge-001"
		groupID    = "grp-A"
	)

	var (
		ctx     context.Context
		rmngCtx *rmngctx.RmngContext
		bcDB    *bridge_children_db.BridgeChildrenDB
		mockDB  *mock.DynamoDBMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testutil.RegisterTestTables()

		ctx = context.Background()
		bridgeNode := node.NewNode(bridgeName)
		bridgeNode.Permissions = *buildBridgePerms(bridgeName, groupID)
		rmngCtx = rmngctx.NewRmngContextWithCtx(ctx, bridgeNode)

		// Place the bridge in a group so addChild has a target.
		groupNodeDB := group_node_db.NewGroupNodeDB(rmngCtx)
		Expect(groupNodeDB.AddNode(groupID, bridgeName, nil)).To(Succeed())

		bcDB = bridge_children_db.NewBridgeChildrenDB(rmngCtx)
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	})

	addChildEvent := func(suffix, localID, reqID string) node.PublishInputEvent {
		return node.PublishInputEvent{
			ThingName: bridgeName,
			Data: map[string]interface{}{
				"event": []interface{}{"addChild"},
				"addChild": map[string]interface{}{
					"request_id":     reqID,
					"child_suffix":   suffix,
					"child_local_id": localID,
				},
			},
		}
	}

	removeChildEvent := func(childName, reqID string) node.PublishInputEvent {
		return node.PublishInputEvent{
			ThingName: bridgeName,
			Data: map[string]interface{}{
				"event": []interface{}{"removeChild"},
				"removeChild": map[string]interface{}{
					"request_id":    reqID,
					"child_node_id": childName,
				},
			},
		}
	}

	Describe("addChild", func() {
		It("creates the Thing, bridge_children row, and group_device_mapping entry", func() {
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-1"))).To(Succeed())

			// Bridge_children row
			entry, err := bcDB.GetChild(bridgeName, bridgeName+"--child_A")
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).NotTo(BeNil())
			Expect(entry.ChildNodeID).To(Equal(bridgeName + "--child_A"))
			Expect(entry.ChildLocalID).To(Equal("local-A"))

			// IoT Thing exists with attributes
			iotMock := awscommon.GetIoTClient().(*mock.IoTClientMock)
			thing, ok := iotMock.GetThingDirect(bridgeName + "--child_A")
			Expect(ok).To(BeTrue())
			Expect(thing.Attributes).To(HaveKeyWithValue("parent_node_id", bridgeName))
			Expect(thing.Attributes).To(HaveKeyWithValue("child_local_id", "local-A"))

			// group_device_mapping row exists for the child in the bridge's group
			child := bridgeName + "--child_A"
			out, err := mockDB.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(group_node_db.GroupDeviceMappingTable),
				Key: map[string]types.AttributeValue{
					"group_id": &types.AttributeValueMemberS{Value: groupID},
					"node_id":  &types.AttributeValueMemberS{Value: child},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Item).NotTo(BeEmpty())

			// Spec §5.1 step 5: proactive getGroupInfo published to
			// the child's from_cloud. Rule A rewrites this to the
			// bridge namespace so the bridge learns the (group,
			// subgroups) state without polling.
			iotData := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			expectedTopic := "rainmaker/nodes/" + child + "/from_cloud"
			var matched *iotdataplane.PublishInput
			for i := range iotData.PublishCalls {
				p := &iotData.PublishCalls[i]
				if p.Topic != nil && *p.Topic == expectedTopic {
					matched = p
					break
				}
			}
			Expect(matched).NotTo(BeNil(),
				"expected a Publish to %s for the proactive getGroupInfo", expectedTopic)
			var payload map[string]interface{}
			Expect(json.Unmarshal(matched.Payload, &payload)).To(Succeed())
			Expect(payload).To(HaveKeyWithValue("event",
				ConsistOf("getGroupInfo")))
			gi, ok := payload["getGroupInfo"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(gi).To(HaveKeyWithValue("pgrp", groupID))
		})

		It("is idempotent on the child_local_id — second call returns the same child name and creates no duplicate", func() {
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-1"))).To(Succeed())
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-2"))).To(Succeed())

			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
		})

		It("rejects a suffix already in use by a different child_local_id", func() {
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-1"))).To(Succeed())
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-B", "req-2"))).To(Succeed())

			// Only the first row should exist.
			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].ChildLocalID).To(Equal("local-A"))
		})

		It("rejects an invalid suffix (contains hyphen)", func() {
			Expect(handleBridgeEvent(ctx, addChildEvent("child-A", "local-A", "req-1"))).To(Succeed())
			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})

		It("rejects when the bridge is not in a group", func() {
			// Remove the bridge from its group.
			groupNodeDB := group_node_db.NewGroupNodeDB(rmngCtx)
			Expect(groupNodeDB.RemoveNode(groupID, bridgeName)).To(Succeed())

			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-1"))).To(Succeed())
			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})
	})

	Describe("removeChild", func() {
		BeforeEach(func() {
			Expect(handleBridgeEvent(ctx, addChildEvent("child_A", "local-A", "req-add"))).To(Succeed())
		})

		It("deletes Thing, bridge_children row, and group_device_mapping entry", func() {
			child := bridgeName + "--child_A"
			Expect(handleBridgeEvent(ctx, removeChildEvent(child, "req-rm"))).To(Succeed())

			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())

			iotMock := awscommon.GetIoTClient().(*mock.IoTClientMock)
			Expect(iotMock.VerifyThingExists(child)).To(BeFalse())

			out, err := mockDB.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(group_node_db.GroupDeviceMappingTable),
				Key: map[string]types.AttributeValue{
					"group_id": &types.AttributeValueMemberS{Value: groupID},
					"node_id":  &types.AttributeValueMemberS{Value: child},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Item).To(BeEmpty())
		})

		It("is idempotent — second remove for the same child succeeds with no state change", func() {
			child := bridgeName + "--child_A"
			Expect(handleBridgeEvent(ctx, removeChildEvent(child, "req-rm1"))).To(Succeed())
			Expect(handleBridgeEvent(ctx, removeChildEvent(child, "req-rm2"))).To(Succeed())
		})

		It("rejects an attempt to remove a child that isn't owned by this bridge", func() {
			// Forged child name with a different bridge prefix
			foreign := "rmng-bridge-999--child_A"
			Expect(handleBridgeEvent(ctx, removeChildEvent(foreign, "req-rm"))).To(Succeed())

			// The legitimate child is still there.
			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
		})
	})
})
