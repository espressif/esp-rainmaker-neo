// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Group Membership Notification", func() {
	const (
		nodeID  = "node123"
		groupID = "grpABC"
	)

	It("populates group info directly without parsing a topic name", func() {
		subGroups := []string{"sub1", "sub2"}
		notif, err := NewGroupMembershipNotification(nodeID, groupID, subGroups, GroupMembershipActionAdded)

		Expect(err).To(BeNil())
		Expect(notif).To(Equal(&Notification{
			NotificationType: NotificationTypeGroupMembership,
			GroupMembershipData: &GroupMembershipNotification{
				NodeID: nodeID,
				Action: GroupMembershipActionAdded,
			},
			GroupID:     groupID,
			SubGroupIDs: subGroups,
		}))
		// TopicName stays empty: the group is known, not embedded in a topic.
		Expect(notif.TopicName).To(BeEmpty())
	})

	It("defaults nil subgroups to an empty slice for a removed action", func() {
		notif, err := NewGroupMembershipNotification(nodeID, groupID, nil, GroupMembershipActionRemoved)

		Expect(err).To(BeNil())
		Expect(notif.SubGroupIDs).To(Equal([]string{}))
		Expect(notif.GroupMembershipData.Action).To(Equal(GroupMembershipActionRemoved))
	})

	DescribeTable("rejects invalid input",
		func(node, group, action string) {
			notif, err := NewGroupMembershipNotification(node, group, nil, action)
			Expect(err).ToNot(BeNil())
			Expect(notif).To(BeNil())
		},
		Entry("empty node ID", "", groupID, GroupMembershipActionAdded),
		Entry("empty group ID", nodeID, "", GroupMembershipActionAdded),
		Entry("unknown action", nodeID, groupID, "moved"),
		Entry("empty action", nodeID, groupID, ""),
	)
})
