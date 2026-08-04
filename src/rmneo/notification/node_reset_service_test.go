// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node Reset Service", func() {
	var (
		service  *NodeResetService
		ownerCtx *rmngctx.RmngContext
		groupID  string
		nodeID   string
		ownerID  string
	)

	// buildNotification mirrors the direct_notification the IoT rule produces:
	// nodeID lives in DirectNotificationData, group info is parsed from the
	// notify topic name.
	buildNotification := func(nodeID, topicName string) *Notification {
		notif, err := NewDirectNotification(nodeID, topicName, map[string]interface{}{"node_reset": true})
		Expect(err).To(BeNil())
		return notif
	}

	resetLambdaCalls := func() int {
		lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
		count := 0
		for _, call := range lambdaMock.InvokeCalls {
			if call.FunctionName != nil && *call.FunctionName == "test-node-data-reset" {
				count++
			}
		}
		return count
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")

		service = NewNodeResetService()
		nodeID = "reset-node-id"
		ownerID = "owner-user-id"

		// Create a group owned by ownerID and add the node, so the disassoc has
		// a real association to remove.
		test_utils.RegisterIoTThing(nodeID)
		ownerCtx = rmngctx.NewRmngContext(user.NewUser(ownerID))
		ownerCtx.SetAllow(utils.NodeAll, nodeID)

		grp, err := group.CreateGroupForUser(ownerCtx, "reset-group")
		Expect(err).To(BeNil())
		groupID = grp.GroupID

		err = node.ShadowNodeAddToGroup(ownerCtx, nodeID, groupID, nil)
		Expect(err).To(BeNil())
		test_utils.AssertNodeInGroup(groupID, nodeID)
	})

	AfterEach(func() {
		os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
	})

	Describe("Service Properties", func() {
		It("should have name node_reset", func() {
			Expect(service.GetName()).To(Equal("node_reset"))
		})

		It("should be a generic (not user-specific) service type", func() {
			Expect(service.GetType()).To(Equal(NotificationServiceTypeGeneric))
		})
	})

	Describe("Marshal", func() {
		It("should pass the notification through unchanged", func() {
			notif := buildNotification(nodeID, groupID)
			result, err := service.Marshal(notif)
			Expect(err).To(BeNil())
			Expect(result).To(BeIdenticalTo(notif))
		})
	})

	Describe("Send", func() {
		It("should disassociate the node and trigger data cleanup exactly once", func() {
			notif := buildNotification(nodeID, groupID)
			// Regression guard: ShadowUpdateData is nil for direct notifications;
			// the service must read the node ID from DirectNotificationData.
			Expect(notif.ShadowUpdateData).To(BeNil())

			err := service.Send(notif)
			Expect(err).To(BeNil())

			test_utils.AssertNodeNotInGroup(groupID, nodeID)
			Expect(resetLambdaCalls()).To(Equal(1))
		})

		It("should run even when the group has no members (no member resolution gate)", func() {
			// No push devices / members are set up at all — a generic service
			// must still act. This is the empty-group case that the user-specific
			// variant would have skipped.
			err := service.Send(buildNotification(nodeID, groupID))
			Expect(err).To(BeNil())
			test_utils.AssertNodeNotInGroup(groupID, nodeID)
		})

		It("should return an error when the node is not in the named group", func() {
			err := service.Send(buildNotification(nodeID, "non-existent-group"))
			Expect(err).ToNot(BeNil())
			// Original association is untouched on failure.
			test_utils.AssertNodeInGroup(groupID, nodeID)
		})

		It("should ignore a shadow_update (non-direct) notification", func() {
			shadowNotif, err := NewShadowUpdateNotification(
				nodeID,
				"params-"+groupID,
				node.ReportedOrDesiredShadow{Params: map[string]interface{}{"power": false}},
				node.ReportedOrDesiredShadow{Params: map[string]interface{}{"power": true}},
			)
			Expect(err).To(BeNil())

			err = service.Send(shadowNotif)
			Expect(err).To(BeNil())
			// No action taken: node stays in the group, no cleanup fired.
			test_utils.AssertNodeInGroup(groupID, nodeID)
			Expect(resetLambdaCalls()).To(Equal(0))
		})

		It("should ignore a payload that is not *Notification", func() {
			err := service.Send("not-a-notification")
			Expect(err).To(BeNil())
			test_utils.AssertNodeInGroup(groupID, nodeID)
		})
	})
})
