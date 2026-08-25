// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// addNode puts a node in a group, granting the caller the access the group flow would.
func addNode(ctx *rmngctx.RmngContext, groupID, nodeID string) {
	GinkgoHelper()
	Expect(ctx.SetAllow(utils.NodeAll, nodeID)).To(Succeed())
	_, err := group.AddNode(ctx, groupID, nodeID, nil)
	Expect(err).NotTo(HaveOccurred())
}

// addToSubGroup puts an already-grouped node into one of that group's subgroups.
func addToSubGroup(ctx *rmngctx.RmngContext, groupID, subGroupID, nodeID string) {
	GinkgoHelper()
	_, err := group.UpdateNodeAndSubgroup(ctx, groupID, nodeID, subGroupID, group_node_db.SubGroupOperationTypeAdd)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Node index", func() {
	var ctx *rmngctx.RmngContext

	BeforeEach(func() {
		test_utils.TestSetup()
		ctx = rmngctx.NewRmngContext(user.NewUser("index-user"))
	})

	It("places every node in the group that holds it", func() {
		grp, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		addNode(ctx, grp.GroupID, "node-a")
		addNode(ctx, grp.GroupID, "node-b")

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(HaveLen(2))
		Expect(index["node-a"].GroupID).To(Equal(grp.GroupID))
		Expect(index["node-a"].GroupName).To(Equal("Home"))
		Expect(index["node-a"].SubgroupIDs).To(BeEmpty())
	})

	It("records the rooms a node belongs to", func() {
		grp, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		kitchen, err := group.CreateSubGroup(ctx, grp.GroupID, "Kitchen")
		Expect(err).NotTo(HaveOccurred())

		addNode(ctx, grp.GroupID, "node-a")
		addToSubGroup(ctx, grp.GroupID, kitchen.SubGroupID, "node-a")

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index["node-a"].SubgroupIDs).To(ConsistOf(kitchen.SubGroupID))
		Expect(index["node-a"].SubgroupNames).To(ConsistOf("Kitchen"))
		Expect(index["node-a"].GroupID).To(Equal(grp.GroupID), "a room does not replace the home")
	})

	It("records every room a node is in at once", func() {
		grp, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		kitchen, err := group.CreateSubGroup(ctx, grp.GroupID, "Kitchen")
		Expect(err).NotTo(HaveOccurred())
		hallway, err := group.CreateSubGroup(ctx, grp.GroupID, "Hallway")
		Expect(err).NotTo(HaveOccurred())

		addNode(ctx, grp.GroupID, "node-a")
		addToSubGroup(ctx, grp.GroupID, kitchen.SubGroupID, "node-a")
		addToSubGroup(ctx, grp.GroupID, hallway.SubGroupID, "node-a")

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index["node-a"].SubgroupIDs).To(ConsistOf(kitchen.SubGroupID, hallway.SubGroupID))
		Expect(index["node-a"].SubgroupNames).To(ConsistOf("Kitchen", "Hallway"))
	})

	It("spans every group the user can reach", func() {
		first, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		second, err := group.CreateGroupForUser(ctx, "Cabin")
		Expect(err).NotTo(HaveOccurred())
		addNode(ctx, first.GroupID, "node-a")
		addNode(ctx, second.GroupID, "node-b")

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index["node-a"].GroupID).To(Equal(first.GroupID))
		Expect(index["node-b"].GroupID).To(Equal(second.GroupID))
	})

	It("narrows to one group when asked", func() {
		first, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		second, err := group.CreateGroupForUser(ctx, "Cabin")
		Expect(err).NotTo(HaveOccurred())
		addNode(ctx, first.GroupID, "node-a")
		addNode(ctx, second.GroupID, "node-b")

		index, err := buildNodeIndex(ctx, first.GroupID)
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(HaveKey("node-a"))
		Expect(index).NotTo(HaveKey("node-b"))
	})

	It("is empty for a user with no groups", func() {
		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(BeEmpty())
	})

	It("refuses a group the user is not a member of", func() {
		otherCtx := rmngctx.NewRmngContext(user.NewUser("other-user"))
		otherGroup, err := group.CreateGroupForUser(otherCtx, "Someone Else's Home")
		Expect(err).NotTo(HaveOccurred())
		addNode(otherCtx, otherGroup.GroupID, "foreign-node")

		_, err = buildNodeIndex(ctx, otherGroup.GroupID)
		Expect(err).To(MatchError(ContainSubstring(otherGroup.GroupID)))
	})

	It("never indexes another user's node", func() {
		grp, err := group.CreateGroupForUser(ctx, "Home")
		Expect(err).NotTo(HaveOccurred())
		addNode(ctx, grp.GroupID, "node-a")

		otherCtx := rmngctx.NewRmngContext(user.NewUser("other-user"))
		otherGroup, err := group.CreateGroupForUser(otherCtx, "Someone Else's Home")
		Expect(err).NotTo(HaveOccurred())
		addNode(otherCtx, otherGroup.GroupID, "foreign-node")

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(HaveLen(1))
		Expect(index).NotTo(HaveKey("foreign-node"))
	})

	// A DynamoDB Query returns at most one page whatever the Limit, and the group-node listing
	// both fills this index and grants node access. A truncating read would therefore hide
	// devices and deny access to them in the same breath, so seed past the mock's page cap.
	It("indexes every node in a group larger than one DynamoDB page", func() {
		const pageSize, totalNodes = 3, 10

		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.MaxPageItems = pageSize
		DeferCleanup(func() { mockDB.MaxPageItems = 0 })

		grp, err := group.CreateGroupForUser(ctx, "Big Home")
		Expect(err).NotTo(HaveOccurred())
		want := make([]string, 0, totalNodes)
		for i := range totalNodes {
			nodeID := fmt.Sprintf("node-%02d", i)
			addNode(ctx, grp.GroupID, nodeID)
			want = append(want, nodeID)
		}

		index, err := buildNodeIndex(ctx, "")
		Expect(err).NotTo(HaveOccurred())

		indexed := make([]string, 0, len(index))
		for nodeID := range index {
			indexed = append(indexed, nodeID)
		}
		Expect(indexed).To(ConsistOf(want))
	})
})
