// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_group_db_test

import (
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The mock's MaxPageItems stands in for DynamoDB's 1 MB page cap; every listing is seeded past it, so a failure means the listing dropped rows.
var _ = Describe("user-group listings spanning more than one DynamoDB page", func() {
	const (
		pageSize   = 3
		totalItems = 10
		groupID    = "grp001"
		userID     = "paginated-user"
	)

	var (
		mockDB *mock.DynamoDBMock
		ctx    *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.MaxPageItems = pageSize
		mockDB.ProfileReset()

		ctx = rmngctx.NewRmngContext(user.NewUser(userID))
	})

	AfterEach(func() {
		mockDB.MaxPageItems = 0
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
})
