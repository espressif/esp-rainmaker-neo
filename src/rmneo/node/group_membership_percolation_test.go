// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_test

import (
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// membershipInvokes returns the parsed group_membership_change events invoked on
// the notifications Lambda, in order.
func membershipInvokes(fnName string) []map[string]interface{} {
	lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
	var events []map[string]interface{}
	for _, call := range lambdaMock.InvokeCalls {
		if call.FunctionName == nil || *call.FunctionName != fnName {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(call.Payload, &payload); err != nil {
			continue
		}
		if payload["notification_type"] == "group_membership_change" {
			events = append(events, payload)
		}
	}
	return events
}

var _ = Describe("Group membership percolation", func() {
	const notificationsFn = "test-notifications"
	var (
		ctx    *rmngctx.RmngContext
		nodeID string
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		os.Setenv("NODE_DATA_RESET_FUNCTION_NAME", "test-node-data-reset")
		os.Setenv("NOTIFICATIONS_FUNCTION_NAME", notificationsFn)

		nodeID = "percolation-node"
		test_utils.RegisterIoTThing(nodeID)
		ctx = rmngctx.NewRmngContext(user.NewUser("percolation-user"))
		ctx.SetAllow(utils.NodeAll, nodeID)
	})

	AfterEach(func() {
		os.Unsetenv("NODE_DATA_RESET_FUNCTION_NAME")
		os.Unsetenv("NOTIFICATIONS_FUNCTION_NAME")
	})

	It("emits an 'added' event to the notifications Lambda when a node joins a group", func() {
		grp, err := group.CreateGroupForUser(ctx, "percolation-group")
		Expect(err).To(BeNil())

		lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
		lambdaMock.InvokeCalls = nil

		Expect(node.ShadowNodeAddToGroup(ctx, nodeID, grp.GroupID, nil)).To(BeNil())

		events := membershipInvokes(notificationsFn)
		Expect(events).To(HaveLen(1))
		Expect(events[0]["action"]).To(Equal(notification.GroupMembershipActionAdded))
		Expect(events[0]["node_id"]).To(Equal(nodeID))
		Expect(events[0]["group_id"]).To(Equal(grp.GroupID))
		// Both voice-assistant channels are targeted.
		Expect(events[0]["notify"]).To(HaveKey("alexa"))
		Expect(events[0]["notify"]).To(HaveKey("gva"))
	})

	It("emits a 'removed' event to the notifications Lambda when a node leaves a group", func() {
		grp, err := group.CreateGroupForUser(ctx, "percolation-group")
		Expect(err).To(BeNil())
		Expect(node.ShadowNodeAddToGroup(ctx, nodeID, grp.GroupID, nil)).To(BeNil())

		lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
		lambdaMock.InvokeCalls = nil

		Expect(node.ShadowNodeRemoveFromGroup(ctx, nodeID, grp.GroupID)).To(BeNil())

		events := membershipInvokes(notificationsFn)
		Expect(events).To(HaveLen(1))
		Expect(events[0]["action"]).To(Equal(notification.GroupMembershipActionRemoved))
		Expect(events[0]["node_id"]).To(Equal(nodeID))
		Expect(events[0]["group_id"]).To(Equal(grp.GroupID))
	})

	It("emits both removed(old) and added(new) when a node moves groups", func() {
		oldGrp, err := group.CreateGroupForUser(ctx, "old-group")
		Expect(err).To(BeNil())
		newGrp, err := group.CreateGroupForUser(ctx, "new-group")
		Expect(err).To(BeNil())
		Expect(node.ShadowNodeAddToGroup(ctx, nodeID, oldGrp.GroupID, nil)).To(BeNil())

		lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
		lambdaMock.InvokeCalls = nil

		Expect(node.ShadowNodeAddToGroup(ctx, nodeID, newGrp.GroupID, nil)).To(BeNil())

		events := membershipInvokes(notificationsFn)
		Expect(events).To(HaveLen(2))
		byGroup := map[string]string{}
		for _, e := range events {
			byGroup[e["group_id"].(string)] = e["action"].(string)
		}
		Expect(byGroup[newGrp.GroupID]).To(Equal(notification.GroupMembershipActionAdded))
		Expect(byGroup[oldGrp.GroupID]).To(Equal(notification.GroupMembershipActionRemoved))
	})
})
