// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_test

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A subentity user resolving one shared subgroup must come away with the
// GroupListSubEntities restriction recorded on their context. IsConditionMatch treats an
// absent condition as unrestricted, so a missing restriction would let the subgroup filter
// in ListNodesForGroup / ListRowsWithGroupID admit every sibling subgroup.
var _ = Describe("Subgroup access conditions", func() {
	var (
		ownerCtx  *rmngctx.RmngContext
		sharedCtx *rmngctx.RmngContext
		parentID  string
		sharedSub string
		otherSub  string
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		ownerCtx = rmngctx.NewRmngContext(user.NewUser("owner-user"))
		sharedCtx = rmngctx.NewRmngContext(user.NewUser("subentity-user"))

		parentGroup, err := group.CreateGroupForUser(ownerCtx, "Parent Group")
		Expect(err).To(BeNil())
		parentID = parentGroup.GroupID

		sharedSubGroup, err := group.CreateSubGroup(ownerCtx, parentID, "Shared Subgroup")
		Expect(err).To(BeNil())
		sharedSub = sharedSubGroup.SubGroupID

		otherSubGroup, err := group.CreateSubGroup(ownerCtx, parentID, "Other Subgroup")
		Expect(err).To(BeNil())
		otherSub = otherSubGroup.SubGroupID

		// Only the first subgroup is shared; the second belongs to the owner alone.
		ShareAndApproveSubGroup(ownerCtx, sharedCtx, parentID, sharedSub)
	})

	It("restricts a subentity user to the subgroup they were granted", func() {
		accessType, err := group.GetUserSubGroupAccess(sharedCtx, parentID, sharedSub)
		Expect(err).To(BeNil())
		Expect(accessType).To(Equal(utils.GroupSubEntityAccess))

		match, err := sharedCtx.IsConditionMatch(utils.GroupListSubEntities, parentID, sharedSub)
		Expect(err).To(BeNil())
		Expect(match).To(BeTrue(), "the granted subgroup must remain visible")

		match, err = sharedCtx.IsConditionMatch(utils.GroupListSubEntities, parentID, otherSub)
		Expect(err).To(BeNil())
		Expect(match).To(BeFalse(), "a sibling subgroup must not be visible to a subentity user")
	})

	It("leaves full-group access unrestricted", func() {
		// The owner sets no conditions, so every subgroup of their own group stays visible.
		_, err := group.GetUserGroupAccess(ownerCtx, parentID)
		Expect(err).To(BeNil())

		for _, subGroupID := range []string{sharedSub, otherSub} {
			match, err := ownerCtx.IsConditionMatch(utils.GroupListSubEntities, parentID, subGroupID)
			Expect(err).To(BeNil())
			Expect(match).To(BeTrue(), "full-group access must not be narrowed")
		}
	})
})
