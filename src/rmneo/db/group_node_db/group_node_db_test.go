// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_node_db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGroupNodeDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GroupNodeDB Suite")
}

// The mock's MaxPageItems stands in for DynamoDB's 1 MB page cap; the group is seeded past it, so a failure means the listing dropped rows.
var _ = Describe("group node listing spanning more than one DynamoDB page", func() {
	const (
		pageSize   = 3
		totalItems = 10
		groupID    = "grp001"
	)

	var mockDB *mock.DynamoDBMock

	BeforeEach(func() {
		test_utils.TestSetup()

		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.MaxPageItems = pageSize
		mockDB.ProfileReset()
	})

	AfterEach(func() {
		mockDB.MaxPageItems = 0
	})

	It("returns every node in a group, and grants access to all of them", func() {
		nodeDB := group_node_db.NewGroupNodeDB(rmngctx.NewRmngContext(utils.NewSystemActor()))
		want := make([]string, 0, totalItems)
		for i := range totalItems {
			nodeID := fmt.Sprintf("node-%02d", i)
			Expect(nodeDB.AddNode(groupID, nodeID, []string{"rmng"})).To(Succeed())
			want = append(want, nodeID)
		}

		ctx := rmngctx.NewRmngContext(user.NewUser("paginated-user"))
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		nodes, _, err := group_node_db.NewGroupNodeDB(ctx).ListGroupNodesWithDBEntry(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(nodes))
		for nodeID := range nodes {
			got = append(got, nodeID)
		}
		Expect(got).To(ConsistOf(want))

		// The listing is also what grants node access for the rest of the request, so a node missing from it is a node the caller can no longer touch.
		for _, nodeID := range want {
			Expect(ctx.IsAuthorized(utils.NodeGet, nodeID)).To(Succeed(), "no access granted for %s", nodeID)
		}
	})
})

// deletesBeforeUpdate removes the target row just before an UpdateItem reaches DynamoDB, standing in for a node moving out of the group between a read and the write that follows it.
type deletesBeforeUpdate struct {
	*mock.DynamoDBMock
}

func (d deletesBeforeUpdate) UpdateItem(ctx context.Context, in *dynamodb.UpdateItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if _, err := d.DynamoDBMock.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: in.TableName, Key: in.Key}); err != nil {
		return nil, err
	}
	return d.DynamoDBMock.UpdateItem(ctx, in, opts...)
}

var _ = Describe("UpdateSubGroup when the node leaves the group mid-update", func() {
	It("fails instead of writing the group's mapping row back", func() {
		test_utils.TestSetup()
		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		ctx := rmngctx.NewRmngContext(utils.NewSystemActor())
		Expect(group_node_db.NewGroupNodeDB(ctx).AddNode("grp001", "node-01", []string{"rmng"})).To(Succeed())

		awscommon.SetDynamoDBClient(deletesBeforeUpdate{mockDB})
		DeferCleanup(awscommon.SetDynamoDBClient, mockDB)
		_, err := group_node_db.NewGroupNodeDB(ctx).UpdateSubGroup("grp001", "node-01", "sub1", group_node_db.SubGroupOperationTypeAdd)
		Expect(err).To(MatchError("node is not in the group"))

		awscommon.SetDynamoDBClient(mockDB)
		entry, err := group_node_db.NewGroupNodeDB(ctx).GetGroupNodeByNodeID("node-01")
		Expect(err).ToNot(HaveOccurred())
		Expect(entry).To(BeNil(), "the removed mapping row was recreated")
	})
})
