// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-cloud-common/go/rbac/rbac"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestDemoteCascade(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bridge DemoteCascade Suite")
}

var _ = Describe("handleDemoteCascade", func() {
	const (
		bridgeName = "rmng-bridge-001"
		groupID    = "grp-A"
	)

	var (
		ctx     context.Context
		bcDB    *bridge_children_db.BridgeChildrenDB
		mockDB  *mock.DynamoDBMock
		iotMock *mock.IoTClientMock
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testutil.RegisterTestTables()

		ctx = context.Background()
		seedBridge := node.NewNode(bridgeName)
		perms := make(rbac.EntityPermissions)
		perms.SetAllow(utils.NodeAll.String(), bridgeName)
		perms.SetAllow(utils.GroupAll.String(), groupID)
		seedBridge.Permissions = perms

		bcDB = bridge_children_db.NewBridgeChildrenDB(rmngctx.NewRmngContextWithCtx(ctx, seedBridge))
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		iotMock = awscommon.GetIoTClient().(*mock.IoTClientMock)
	})

	seedGroupAssoc := func(nodeID string) {
		_, err := mockDB.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(group_node_db.GroupDeviceMappingTable),
			Item: map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
			},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	groupAssocExists := func(nodeID string) bool {
		out, err := mockDB.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(group_node_db.GroupDeviceMappingTable),
			Key: map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		return len(out.Item) > 0
	}

	// seedChild creates a child the way addChild does: bridge_children row, IoT Thing and, when inGroup, the group association.
	seedChild := func(suffix string, inGroup bool) string {
		child := bridgeName + "--" + suffix
		Expect(bcDB.AddChild(bridgeName, child, "loc-"+suffix)).To(Succeed())
		iotMock.Things[child] = mock.Things{Name: child, CertificateIds: []string{}, Groups: []string{}}
		if inGroup {
			seedGroupAssoc(child)
		}
		return child
	}

	demote := func() error {
		return handleDemoteCascade(ctx, nodelifecycle.NodeTypeChangedEvent{NodeID: bridgeName})
	}

	It("deletes a demoted bridge's children from its group, with their Things and bridge_children rows", func() {
		seedGroupAssoc(bridgeName)
		children := []string{seedChild("c1", true), seedChild("c2", true)}

		Expect(demote()).To(Succeed())

		entries, err := bcDB.GetChildrenByParent(bridgeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
		for _, child := range children {
			Expect(iotMock.VerifyThingExists(child)).To(BeFalse())
			Expect(groupAssocExists(child)).To(BeFalse())
		}
		Expect(groupAssocExists(bridgeName)).To(BeTrue(), "the demoted node itself stays in its group")
	})

	It("deletes the Things and bridge_children rows of a demoted bridge that is in no group", func() {
		child := seedChild("orphan", false)

		Expect(demote()).To(Succeed())

		entries, err := bcDB.GetChildrenByParent(bridgeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
		Expect(iotMock.VerifyThingExists(child)).To(BeFalse())
	})
})
