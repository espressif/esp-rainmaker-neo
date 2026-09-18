// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/testutil"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestCascadeDelete(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bridge CascadeDelete Suite")
}

// seedAdminPerms builds a context that's permissive enough to set up
// bridge_children rows + group_device_mapping rows in the test
// BeforeEach. The Lambda under test grants its own in-memory perms,
// so this is purely for fixture setup.
func seedAdminPerms(bridgeID, groupID string) *rbac.EntityPermissions {
	perms := make(rbac.EntityPermissions)
	perms.SetAllow(utils.NodeAll.String(), bridgeID)
	perms.SetAllow(utils.GroupAll.String(), groupID)
	return &perms
}

var _ = Describe("handleCascadeDelete", func() {
	const (
		bridgeName = "rmng-bridge-001"
		groupID    = "grp-A"
	)

	var (
		ctx     context.Context
		bcDB    *bridge_children_db.BridgeChildrenDB
		seedCtx *rmngctx.RmngContext
		mockDB  *mock.DynamoDBMock
		iotMock *mock.IoTClientMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testutil.RegisterTestTables()

		ctx = context.Background()
		seedBridge := node.NewNode(bridgeName)
		seedBridge.Permissions = *seedAdminPerms(bridgeName, groupID)
		seedCtx = rmngctx.NewRmngContextWithCtx(ctx, seedBridge)

		bcDB = bridge_children_db.NewBridgeChildrenDB(seedCtx)
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		iotMock = awscommon.GetIoTClient().(*mock.IoTClientMock)
	})

	// Direct fixture helpers — much cleaner than wrestling with the mock.
	createIotThing := func(child string) {
		// Use the awsiot wrapper so attributes get set the same way
		// production sets them. The mock honours AttributePayload now.
		// We pass parent attribute purely so the cascade can be
		// observed by inspection if needed.
		Expect(iotMock.Things).NotTo(BeNil())
		iotMock.Things[child] = mock.Things{
			Name:           child,
			CertificateIds: []string{},
			Groups:         []string{},
			Attributes: map[string]string{
				"node_id": bridgeName,
			},
		}
	}

	// seedGroupAssoc writes directly to the mock DDB so we don't have
	// to thread auth perms through groupNodeDB for every fixture child.
	seedGroupAssoc := func(child string) {
		_, err := mockDB.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(group_node_db.GroupDeviceMappingTable),
			Item: map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: child},
			},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	expectGroupAssocAbsent := func(child string) {
		out, err := mockDB.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(group_node_db.GroupDeviceMappingTable),
			Key: map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: child},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Item).To(BeEmpty())
	}

	Describe("happy path", func() {
		It("removes all children from group, deletes Things, and deletes bridge_children rows", func() {
			children := []string{
				bridgeName + "--c1",
				bridgeName + "--c2",
				bridgeName + "--c3",
			}
			for i, child := range children {
				Expect(bcDB.AddChild(bridgeName, child, "loc-"+child)).To(Succeed())
				createIotThing(child)
				seedGroupAssoc(child)
				_ = i
			}

			Expect(handleCascadeDelete(ctx, nodelifecycle.NodeLeftGroupEvent{
				NodeID:     bridgeName,
				OldGroupID: groupID,
			})).To(Succeed())

			// bridge_children rows all gone
			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())

			// IoT Things all gone
			for _, child := range children {
				Expect(iotMock.VerifyThingExists(child)).To(BeFalse())
				expectGroupAssocAbsent(child)
			}
		})
	})

	Describe("no children", func() {
		It("returns success without invoking anything", func() {
			Expect(handleCascadeDelete(ctx, nodelifecycle.NodeLeftGroupEvent{
				NodeID:     bridgeName,
				OldGroupID: groupID,
			})).To(Succeed())
		})
	})

	Describe("empty parent_node_id", func() {
		It("returns nil and is a no-op", func() {
			Expect(handleCascadeDelete(ctx, nodelifecycle.NodeLeftGroupEvent{
				NodeID:     "",
				OldGroupID: groupID,
			})).To(Succeed())
		})
	})

	Describe("partial failures", func() {
		It("continues past a missing IoT Thing and still clears DDB rows", func() {
			child1 := bridgeName + "--c1"
			child2 := bridgeName + "--c2"

			// Seed child1 with a Thing; child2 without (DeleteThing is
			// idempotent on ResourceNotFoundException, so the cascade
			// should not abort).
			Expect(bcDB.AddChild(bridgeName, child1, "loc1")).To(Succeed())
			Expect(bcDB.AddChild(bridgeName, child2, "loc2")).To(Succeed())
			createIotThing(child1)
			seedGroupAssoc(child1)
			seedGroupAssoc(child2)

			Expect(handleCascadeDelete(ctx, nodelifecycle.NodeLeftGroupEvent{
				NodeID:     bridgeName,
				OldGroupID: groupID,
			})).To(Succeed())

			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
			expectGroupAssocAbsent(child1)
			expectGroupAssocAbsent(child2)
		})
	})

	Describe("empty oldGroupID", func() {
		It("still deletes Things and bridge_children rows, skips group removal", func() {
			child := bridgeName + "--orphan"
			Expect(bcDB.AddChild(bridgeName, child, "loc-orphan")).To(Succeed())
			createIotThing(child)

			Expect(handleCascadeDelete(ctx, nodelifecycle.NodeLeftGroupEvent{
				NodeID:     bridgeName,
				OldGroupID: "",
			})).To(Succeed())

			entries, err := bcDB.GetChildrenByParent(bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
			Expect(iotMock.VerifyThingExists(child)).To(BeFalse())
		})
	})
})
