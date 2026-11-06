// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGroupDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GroupDB Suite")
}

var _ = Describe("GroupDB", func() {
	const (
		pageSize   = 3
		totalItems = 10
		groupID    = "grp001"
	)

	var (
		mockDB *mock.DynamoDBMock
		ctx    *rmngctx.RmngContext
	)

	putGroupRow := func(subGroupID string) {
		_, err := mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(group_db.GroupsTable),
			Item: map[string]types.AttributeValue{
				"group_id":     &types.AttributeValueMemberS{Value: groupID},
				"sub_group_id": &types.AttributeValueMemberS{Value: subGroupID},
				"group_name":   &types.AttributeValueMemberS{Value: "grp"},
			},
		})
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.ProfileReset()

		ctx = rmngctx.NewRmngContext(user.NewUser("paginated-user"))
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
	})

	AfterEach(func() {
		mockDB.MaxPageItems = 0
	})

	// The mock's MaxPageItems stands in for DynamoDB's 1 MB page cap, so a failure means the listing dropped rows.
	It("returns every subgroup of a group alongside the parent row across pages", func() {
		mockDB.MaxPageItems = pageSize

		want := []string{"NONE"}
		for i := range totalItems {
			want = append(want, fmt.Sprintf("s%02d", i))
		}
		for _, sgid := range want {
			putGroupRow(sgid)
		}

		rows, err := group_db.NewGroupDB(ctx).ListRowsWithGroupID(groupID)
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(rows))
		for _, r := range rows {
			got = append(got, r.SubGroupID)
		}
		Expect(got).To(ConsistOf(want))
	})

	It("reports whether a subgroup row exists", func() {
		putGroupRow("NONE")
		putGroupRow("s01")

		groupDB := group_db.NewGroupDB(ctx)
		Expect(groupDB.IsSubGroup(groupID, "s01")).To(BeTrue())
		Expect(groupDB.IsSubGroup(groupID, "s02")).To(BeFalse())
	})
})
