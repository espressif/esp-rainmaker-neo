// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db_test

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A DynamoDB Query returns at most 1 MB per call regardless of Limit, so a read path that
// ignores LastEvaluatedKey silently truncates once a partition outgrows one page. The mock's
// MaxPageItems stands in for that cap: every listing below is seeded past it, so any of these
// specs failing means that listing dropped rows it was asked for.
var _ = Describe("List reads spanning more than one DynamoDB page", func() {
	const (
		pageSize   = 3
		totalItems = 10
		groupID    = "grp001"
		userID     = "paginated-user"
	)

	var (
		mockDB *mock.DynamoDBMock
		ctx    *rmngctx.RmngContext
		sysCtx *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.MaxPageItems = pageSize
		mockDB.ProfileReset()

		u := user.NewUser(userID)
		ctx = rmngctx.NewRmngContext(u)
		sysCtx = rmngctx.NewRmngContext(utils.NewSystemActor())
	})

	AfterEach(func() {
		mockDB.MaxPageItems = 0
	})

	It("returns every node in a group, and grants access to all of them", func() {
		nodeDB := group_node_db.NewGroupNodeDB(sysCtx)
		want := make([]string, 0, totalItems)
		for i := range totalItems {
			nodeID := fmt.Sprintf("node-%02d", i)
			Expect(nodeDB.AddNode(groupID, nodeID, []string{"rmng"})).To(Succeed())
			want = append(want, nodeID)
		}

		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		nodes, _, err := group_node_db.NewGroupNodeDB(ctx).ListGroupNodesWithDBEntry(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(nodes))
		for nodeID := range nodes {
			got = append(got, nodeID)
		}
		Expect(got).To(ConsistOf(want))

		// The listing is also what grants node access for the rest of the request, so a node
		// missing from it is a node the caller can no longer touch.
		for _, nodeID := range want {
			Expect(ctx.IsAuthorized(utils.NodeGet, nodeID)).To(Succeed(), "no access granted for %s", nodeID)
		}
	})

	It("returns every group a user belongs to", func() {
		want := make([]string, 0, totalItems)
		for i := range totalItems {
			gid := fmt.Sprintf("grp%03d", i)
			Expect(user_group_db.NewUserGroupDB(ctx).CreateUserGroup(gid)).To(Succeed())
			want = append(want, gid)
		}

		entries, err := user_group_db.NewUserGroupDB(ctx).ListGroupsForUser("")
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.GroupID)
		}
		Expect(got).To(ConsistOf(want))
	})

	It("returns every member of a group", func() {
		want := make([]string, 0, totalItems)
		for i := range totalItems {
			uid := fmt.Sprintf("member-%02d", i)
			memberCtx := rmngctx.NewRmngContext(user.NewUser(uid))
			Expect(user_group_db.NewUserGroupDB(memberCtx).CreateUserGroup(groupID)).To(Succeed())
			want = append(want, uid)
		}

		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		entries, err := user_group_db.NewUserGroupDB(ctx).ListAllUsersForGroup(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.UserID)
		}
		Expect(got).To(ConsistOf(want))
	})

	It("returns every subgroup of a group alongside the parent row", func() {
		// Rows are put directly: CreateSubGroup's attribute_not_exists(group_id) guard is
		// evaluated per-partition by the mock, so it refuses a second row under one group.
		want := []string{"NONE"}
		for i := range totalItems {
			want = append(want, fmt.Sprintf("s%02d", i))
		}
		for _, sgid := range want {
			_, err := awscommon.GetDynamoDBClient().PutItem(ctx.Context, &dynamodb.PutItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Item: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: sgid},
					"group_name":   &types.AttributeValueMemberS{Value: "grp"},
				},
			})
			Expect(err).ToNot(HaveOccurred())
		}

		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		rows, err := group_db.NewGroupDB(ctx).ListRowsWithGroupID(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(rows))
		for _, r := range rows {
			got = append(got, r.SubGroupID)
		}
		Expect(got).To(ConsistOf(want))
	})

	It("returns every automation in a group", func() {
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		automationDB := automation_db.NewAutomationDB(ctx)
		want := make([]string, 0, totalItems)
		for i := range totalItems {
			aid := fmt.Sprintf("a%02d", i)
			Expect(automationDB.CreateAutomation(groupID, aid, map[string]interface{}{"name": aid})).To(Succeed())
			want = append(want, aid)
		}

		items, err := automationDB.ListGroupAutomations(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(items))
		for _, item := range items {
			got = append(got, item.AutomationID)
		}
		Expect(got).To(ConsistOf(want))
	})

	It("returns every sharing request addressed to a user", func() {
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		sharingDB := sharing_request_db.NewSharingRequestDB(ctx)
		for range totalItems {
			_, err := sharingDB.CreateSharingRequest(userID, groupID, "", string(utils.GroupSecondaryAccess), "primary@example.com", "")
			Expect(err).ToNot(HaveOccurred())
		}

		entries, err := sharingDB.GetMySharingRequests()
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(totalItems))
	})
})
