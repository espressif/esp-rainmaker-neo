// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils_test

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTestFormation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test Formation Suite")
}

var _ = Describe("Test Formation", func() {
	BeforeEach(func() {
		test_utils.TestSetup()
	})
	It("should setup the test formation", func() {
		var tf = test_utils.TestFormation{
			Users: map[string]test_utils.TFUser{
				"test-user-id": {
					Groups: map[string]test_utils.TFGroup{
						"test-group-id": {
							Nodes: []string{"test-node-id", "test-node-id-2"},
							SubGroups: map[string]test_utils.TFSubGroup{
								"test-sub-group-id": {
									Nodes: []string{"test-node-id"},
								},
							},
							SharedPrimary: []string{"test-user-id-2"},
						},
						"test-group-id-2": {
							Nodes: []string{"test-node-id-3", "test-node-id-30"},
							SubGroups: map[string]test_utils.TFSubGroup{
								"test-sub-group-id-2": {
									Nodes:  []string{"test-node-id-3"},
									Shared: []string{"test-user-id-2"},
								},
							},
						},
					},
				},
				"test-user-id-2": {},
			},
		}

		tfo := tf.Setup()
		fmt.Println(tfo)
		Expect(tfo.UserCtx).To(HaveLen(2))
		Expect(tfo.Groups).To(HaveLen(2))
		Expect(tfo.SubGroups).To(HaveLen(2))

		g1ID := tfo.Groups["test-group-id"].GroupID
		g2ID := tfo.Groups["test-group-id-2"].GroupID
		sg1ID := tfo.SubGroups["test-sub-group-id"].SubGroupID
		sg2ID := tfo.SubGroups["test-sub-group-id-2"].SubGroupID

		// Verify groups for user2
		user2Ctx := tfo.UserCtx["test-user-id-2"]
		groups, err := group.ListGroupsForUser(user2Ctx, true)
		Expect(err).To(BeNil())
		Expect(groups).To(HaveLen(2))

		expectedGroup1 := group.Group{
			GroupID:    g1ID,
			GroupName:  "test-group-id",
			AccessType: utils.GroupPrimaryAccess,
			NodeGroupEntries: map[string]*group_node_db.GroupNode{
				"test-node-id":   {GroupID: g1ID, NodeID: "test-node-id", SubGrp1: sg1ID},
				"test-node-id-2": {GroupID: g1ID, NodeID: "test-node-id-2"},
			},
			SubGroups: []group.SubGroup{
				{SubGroupID: sg1ID, SubGroupName: "test-sub-group-id", NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"test-node-id": {GroupID: g1ID, NodeID: "test-node-id", SubGrp1: sg1ID},
				}},
			},
		}
		expectedGroup2ForUser2 := group.Group{
			GroupID:    g2ID,
			GroupName:  "test-group-id-2",
			AccessType: utils.GroupSubEntityAccess,
			NodeGroupEntries: map[string]*group_node_db.GroupNode{
				"test-node-id-3": {GroupID: g2ID, NodeID: "test-node-id-3", SubGrp1: sg2ID},
			},
			SubGroups: []group.SubGroup{
				{SubGroupID: sg2ID, SubGroupName: "test-sub-group-id-2", NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"test-node-id-3": {GroupID: g2ID, NodeID: "test-node-id-3", SubGrp1: sg2ID},
				}},
			},
		}
		Expect(groups).To(ContainElement(expectedGroup1))
		Expect(groups).To(ContainElement(expectedGroup2ForUser2))

		// Verify groups for user1 (same group1, group2 has extra node)
		user1Ctx := tfo.UserCtx["test-user-id"]
		groups, err = group.ListGroupsForUser(user1Ctx, true)
		Expect(err).To(BeNil())
		Expect(groups).To(HaveLen(2))

		expectedGroup2ForUser1 := group.Group{
			GroupID:    g2ID,
			GroupName:  "test-group-id-2",
			AccessType: utils.GroupPrimaryAccess,
			NodeGroupEntries: map[string]*group_node_db.GroupNode{
				"test-node-id-3":  {GroupID: g2ID, NodeID: "test-node-id-3", SubGrp1: sg2ID},
				"test-node-id-30": {GroupID: g2ID, NodeID: "test-node-id-30"},
			},
			SubGroups: []group.SubGroup{
				{SubGroupID: sg2ID, SubGroupName: "test-sub-group-id-2", NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"test-node-id-3": {GroupID: g2ID, NodeID: "test-node-id-3", SubGrp1: sg2ID},
				}},
			},
		}
		Expect(groups).To(ContainElement(expectedGroup1))
		Expect(groups).To(ContainElement(expectedGroup2ForUser1))
	})
})
