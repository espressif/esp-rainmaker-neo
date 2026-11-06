// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_group_db_test

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserGroupDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UserGroupDB Suite")
}

var _ = Describe("RemoveUserFromGroup", func() {
	const groupID = "grp-remove"

	var owner *user_group_db.UserGroupDB

	join := func(userID string) {
		Expect(user_group_db.NewUserGroupDB(rmngctx.NewRmngContext(user.NewUser(userID))).CreateUserGroup(groupID)).To(Succeed())
	}
	mappingOf := func(userID string) *user_group_db.UserGroupEntry {
		entry, err := owner.GetUserGroupItemByUserID(userID, groupID)
		Expect(err).NotTo(HaveOccurred())
		return entry
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		join("owner")
		join("member-a")
		join("member-b")
		ownerCtx := rmngctx.NewRmngContext(user.NewUser("owner"))
		Expect(ownerCtx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)).To(Succeed())
		owner = user_group_db.NewUserGroupDB(ownerCtx)
	})

	It("deletes only that user's mapping row", func() {
		Expect(owner.RemoveUserFromGroup(groupID, "member-a")).To(Succeed())
		Expect(mappingOf("member-a")).To(BeNil())
		Expect(mappingOf("member-b")).NotTo(BeNil())
		Expect(mappingOf("owner")).NotTo(BeNil())
	})

	It("succeeds for a user who has no mapping row", func() {
		Expect(owner.RemoveUserFromGroup(groupID, "never-joined")).To(Succeed())
		Expect(mappingOf("member-a")).NotTo(BeNil())
	})
})
