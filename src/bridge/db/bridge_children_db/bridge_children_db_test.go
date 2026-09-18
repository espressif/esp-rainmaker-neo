// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package bridge_children_db_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/testutil"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestBridgeChildrenDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BridgeChildrenDB Suite")
}

var _ = Describe("BridgeChildrenDB", func() {
	const (
		parentA = "rmng-bridge-001"
		parentB = "rmng-bridge-002"
		childA1 = "rmng-bridge-001--child_A"
		childA2 = "rmng-bridge-001--child_B"
		childB1 = "rmng-bridge-002--child_X"
		localA1 = "0x00158D00000A0001"
		localA2 = "0x00158D00000A0002"
		localB1 = "0x00158D00000B0001"
	)

	var db *bridge_children_db.BridgeChildrenDB

	BeforeEach(func() {
		test_utils.TestSetup()
		testutil.RegisterTestTables()
		// System actor has `*:*` so all IsAuthorized checks pass — these
		// tests are about DB storage isolation across parents, not the
		// per-method authz gating (which is exercised in handler tests).
		ctx := rmngctx.NewRmngContext(utils.NewSystemActor())
		db = bridge_children_db.NewBridgeChildrenDB(ctx)
	})

	Describe("AddChild", func() {
		It("inserts a new row with a created_at timestamp", func() {
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())

			entry, err := db.GetChild(parentA, childA1)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).NotTo(BeNil())
			Expect(entry.ParentNodeID).To(Equal(parentA))
			Expect(entry.ChildNodeID).To(Equal(childA1))
			Expect(entry.ChildLocalID).To(Equal(localA1))
			Expect(entry.CreatedAt).NotTo(BeZero())
		})

		It("rejects a duplicate (parent, child) row", func() {
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())
			err := db.AddChild(parentA, childA1, localA1)
			Expect(err).To(HaveOccurred())
		})

		It("isolates rows across parents that reuse the same child_local_id", func() {
			// Two different bridges with the same child_local_id must
			// produce independent rows. Looking up parentA by composite
			// key must not be affected by parentB's row.
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())
			Expect(db.AddChild(parentB, childB1, localA1)).To(Succeed())

			entry, err := db.GetChild(parentA, childA1)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).NotTo(BeNil())
			Expect(entry.ChildNodeID).To(Equal(childA1))
			Expect(entry.ChildLocalID).To(Equal(localA1))
		})
	})

	Describe("GetChildrenByParent", func() {
		It("returns every child for the given parent, and only that parent", func() {
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())
			Expect(db.AddChild(parentA, childA2, localA2)).To(Succeed())
			Expect(db.AddChild(parentB, childB1, localB1)).To(Succeed())

			entries, err := db.GetChildrenByParent(parentA)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(2))

			names := []string{entries[0].ChildNodeID, entries[1].ChildNodeID}
			Expect(names).To(ConsistOf(childA1, childA2))
		})

		It("returns an empty slice for an unknown parent", func() {
			entries, err := db.GetChildrenByParent("unknown-bridge")
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})
	})

	Describe("GetMyGroup", func() {
		const groupA = "grp-A"

		It("rejects a non-node context", func() {
			// db uses a system actor context (set up in BeforeEach)
			_, err := db.GetMyGroup()
			Expect(err).To(HaveOccurred())
		})

		It("returns empty NodesGroups when the node is not in any group", func() {
			nodeCtx := rmngctx.NewRmngContext(node.NewNode(childA1))
			nodeDB := bridge_children_db.NewBridgeChildrenDB(nodeCtx)

			ng, err := nodeDB.GetMyGroup()
			Expect(err).NotTo(HaveOccurred())
			Expect(ng).To(Equal(group_node_db.NodesGroups{}))
		})

		It("returns the group and grants GroupGet, GroupListSubEntities, GroupEditNodes", func() {
			sysCtx := rmngctx.NewRmngContext(utils.NewSystemActor())
			Expect(group_node_db.NewGroupNodeDB(sysCtx).AddNode(groupA, childA1, nil)).To(Succeed())

			nodeCtx := rmngctx.NewRmngContext(node.NewNode(childA1))
			nodeDB := bridge_children_db.NewBridgeChildrenDB(nodeCtx)

			ng, err := nodeDB.GetMyGroup()
			Expect(err).NotTo(HaveOccurred())
			Expect(ng).To(Equal(group_node_db.NodesGroups{Group: groupA}))

			Expect(nodeCtx.IsAuthorized(utils.GroupGet, groupA)).To(Succeed())
			Expect(nodeCtx.IsAuthorized(utils.GroupListSubEntities, groupA)).To(Succeed())
			Expect(nodeCtx.IsAuthorized(utils.GroupEditNodes, groupA)).To(Succeed())
		})
	})

	Describe("DeleteChild", func() {
		It("removes the row and is idempotent on a second call", func() {
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())

			Expect(db.DeleteChild(parentA, childA1)).To(Succeed())
			entry, err := db.GetChild(parentA, childA1)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry).To(BeNil())

			Expect(db.DeleteChild(parentA, childA1)).To(Succeed())
		})

		It("does not affect siblings", func() {
			Expect(db.AddChild(parentA, childA1, localA1)).To(Succeed())
			Expect(db.AddChild(parentA, childA2, localA2)).To(Succeed())

			Expect(db.DeleteChild(parentA, childA1)).To(Succeed())

			entries, err := db.GetChildrenByParent(parentA)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].ChildNodeID).To(Equal(childA2))
		})
	})
})
