// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_test

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Suite")
}

var _ = Describe("User", func() {
	var tfo test_utils.TFOutput
	var nodeGroups group_node_db.NodesGroups
	BeforeEach(func() {
		test_utils.TestSetup()
		tf := test_utils.TestFormation{
			Users: map[string]test_utils.TFUser{
				"main-user-id": {
					Groups: map[string]test_utils.TFGroup{
						"main-group": {
							Nodes: []string{"main-node-id"},
							SubGroups: map[string]test_utils.TFSubGroup{
								"main-sub-group-id": {
									Nodes:  []string{"main-node-id"},
									Shared: []string{"user-subgroup-access-id"},
								},
							},
						},
					},
				},
				"foreign-user-id": {
					Groups: map[string]test_utils.TFGroup{
						"foreign-group": {
							Nodes: []string{"foreign-node-id"},
						},
					},
				},
				"user-no-access-id":       {},
				"user-subgroup-access-id": {},
			},
		}
		tfo = tf.Setup()
		nodeGroups = group_node_db.NodesGroups{
			Group:     tfo.Groups["main-group"].GroupID,
			SubGroups: []string{tfo.SubGroups["main-sub-group-id"].SubGroupID},
		}
		test_utils.SetupShadow("main-node-id", node.IoTNodeShadow{
			State: &node.ShadowState{
				Desired: &node.ReportedOrDesiredShadow{
					Params: map[string]interface{}{
						"brightness": 10,
					},
				},
			},
		}, nodeGroups)
	})

	Describe("Node Access Checks", func() {
		write_to_shadow := func(userCtx *rmngctx.RmngContext, should_succeed bool) {
			err := user.LoadNodePermissions(userCtx, tfo.Groups["main-group"].GroupID, "main-node-id")
			if should_succeed {
				Expect(err).To(BeNil())
			} else {
				Expect(err).To(HaveOccurred())
			}

			nodeObj := node.NewNode("main-node-id")
			err = nodeObj.WriteToDesiredShadow(userCtx, node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"brightness": 100,
				},
			})

			expectedShadow := node.IoTNodeShadow{
				State: &node.ShadowState{
					Desired: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"brightness": 100,
						},
					},
				},
			}

			if should_succeed {
				Expect(err).To(BeNil())
			} else {
				Expect(err).To(HaveOccurred())
				expectedShadow = node.IoTNodeShadow{
					State: &node.ShadowState{
						Desired: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{
								"brightness": 10,
							},
						},
					},
				}
			}

			shadow := test_utils.GetShadowForNodeGroup(nodeObj, nodeGroups)

			// Convert float values to int for proper comparison
			shadowStateConverted := test_utils.ConvertAllFloatToInt(shadow.State)
			expectedStateConverted := test_utils.ConvertAllFloatToInt(expectedShadow.State)

			Expect(shadowStateConverted).To(BeEquivalentTo(expectedStateConverted))
		}

		It("should allow access to the node if the user has access to the group", func() {
			write_to_shadow(tfo.UserCtx["main-user-id"], true)

		})

		It("should allow access to the node if the user has access to the sub group", func() {
			write_to_shadow(tfo.UserCtx["user-subgroup-access-id"], true)
		})

		It("should return an error if the user does not have access to the node", func() {
			write_to_shadow(tfo.UserCtx["user-no-access-id"], false)
		})

		It("should reject a node that is not a member of the given group, even if the user owns that group (security)", func() {
			// main-user-id legitimately owns "main-group", but "foreign-node-id"
			// belongs to a different group the user has no access to at all.
			// LoadNodePermissions must not authorize access just because the
			// caller supplied a groupID the user happens to own.
			err := user.LoadNodePermissions(tfo.UserCtx["main-user-id"], tfo.Groups["main-group"].GroupID, "foreign-node-id")
			Expect(err).To(HaveOccurred())
		})

		It("should not let a caller write to a foreign node by pairing it with an owned groupID (end-to-end, security)", func() {
			// This mirrors the real GVA/Alexa call pattern: LoadNodePermissions(groupID, nodeID)
			// followed by an authenticated write to the node's shadow.
			userCtx := tfo.UserCtx["main-user-id"]
			_ = user.LoadNodePermissions(userCtx, tfo.Groups["main-group"].GroupID, "foreign-node-id")

			foreignNode := node.NewNode("foreign-node-id")
			err := foreignNode.WriteToDesiredShadow(userCtx, node.ReportedOrDesiredShadow{
				Params: map[string]interface{}{
					"brightness": 100,
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})
