// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_group_db_test

// Regression suite for the subgroup-scope fail-open (finding M-4).
//
// IsConditionMatch is the only mechanism that narrows a subgroup-scoped user
// once they hold GroupListSubEntities on a whole PARENT group, and it allows
// when no condition is registered for that (action, resource). A grant that
// registered no conditions therefore reached every subgroup of the parent and,
// because ListGroupNodesWithDBEntry ends in SetAllow(NodeAll, nodeID), every
// device in them.
//
// Two things had to hold, and each has specs below: a scoped grant always
// registers its scope (SetScopedConditions), and a subentity row can never be written
// without a subgroup id.
//
// These specs drive the real bootstrap reads and the real DB functions against
// the DynamoDB mock. Nothing about the authorization context is hand-built.
//
// Topology: one group with two subgroups. The guest was shared ONLY
// "sub-guest-room"; "sub-master-bedroom" and the door lock in it must stay out
// of reach.
//
// Run: go test ./src/db/ -v -args -ginkgo.focus="subgroup scope"

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("subgroup scope narrowing (M-4 regression)", func() {
	const (
		guestID     = "scoped-guest-user"
		parentGroup = "grp-victim-home"

		sharedSub  = "sub-guest-room"     // legitimately shared with the attacker
		privateSub = "sub-master-bedroom" // NEVER shared — the target

		sharedNode  = "node-guest-lamp"
		privateNode = "node-front-door-lock" // the device that must stay unreachable
	)

	var (
		mockDB   *mock.DynamoDBMock
		attacker *user.User
		ctx      *rmngctx.RmngContext
	)

	putItem := func(table string, item map[string]types.AttributeValue) {
		_, err := mockDB.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: &table,
			Item:      item,
		})
		Expect(err).NotTo(HaveOccurred())
	}

	// seedVictimGroup writes the victim's group: the parent row, two subgroup
	// rows, and one node in each subgroup.
	seedVictimGroup := func() {
		for sub, name := range map[string]string{
			"NONE":     "Victim Home",
			sharedSub:  "Guest Room",
			privateSub: "Master Bedroom",
		} {
			putItem(group_db.GroupsTable, map[string]types.AttributeValue{
				"group_id":     &types.AttributeValueMemberS{Value: parentGroup},
				"sub_group_id": &types.AttributeValueMemberS{Value: sub},
				"group_name":   &types.AttributeValueMemberS{Value: name},
			})
		}
		for node, sub := range map[string]string{
			sharedNode:  sharedSub,
			privateNode: privateSub,
		} {
			putItem(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: parentGroup},
				"node_id":  &types.AttributeValueMemberS{Value: node},
				"subgrp1":  &types.AttributeValueMemberS{Value: sub},
			})
		}
	}

	// shareWithGuest writes the attacker's user_group_mapping row exactly as
	// createUserGroupEntry (src/db/user_group_db.go:151-166) would.
	shareWithGuest := func(accessType utils.GroupAccessType, subEntityIDs []string) {
		subs := make([]types.AttributeValue, 0, len(subEntityIDs))
		for _, s := range subEntityIDs {
			subs = append(subs, &types.AttributeValueMemberS{Value: s})
		}
		putItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
			"user_id":        &types.AttributeValueMemberS{Value: guestID},
			"group_id":       &types.AttributeValueMemberS{Value: parentGroup},
			"sub_entity_ids": &types.AttributeValueMemberL{Value: subs},
			"access_type":    &types.AttributeValueMemberS{Value: string(accessType)},
		})
	}

	// subGroupIDs flattens what ListRowsWithGroupID exposed to the caller.
	subGroupIDs := func(rows []group_db.GroupInDB) []string {
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			if r.SubGroupID != "NONE" {
				ids = append(ids, r.SubGroupID)
			}
		}
		return ids
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		attacker = user.NewUser(guestID)
		ctx = rmngctx.NewRmngContext(attacker)
		seedVictimGroup()
	})

	// -----------------------------------------------------------------------
	// CONTROL. Proves the narrowing mechanism genuinely works when the
	// conditions ARE registered, so the failures below are caused by the
	// missing conditions and not by the harness.
	// -----------------------------------------------------------------------
	Context("control: a correctly-formed subentity share (sub_entity_ids populated)", func() {
		It("confines the guest to the one subgroup they were shared", func() {
			shareWithGuest(utils.GroupSubEntityAccess, []string{sharedSub})

			// The real bootstrap read used by GET /v1/groups and every
			// LoadNodePermissions caller.
			_, err := user_group_db.NewUserGroupDB(ctx).ListGroupsForUser("")
			Expect(err).NotTo(HaveOccurred())
			Expect(ctx.ExtraConditions).NotTo(BeNil(), "conditions must be registered")

			rows, err := group_db.NewGroupDB(ctx).ListRowsWithGroupID(parentGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(subGroupIDs(rows)).To(ConsistOf(sharedSub))
			Expect(subGroupIDs(rows)).NotTo(ContainElement(privateSub))

			_, subgrpNodes, err := group_node_db.NewGroupNodeDB(ctx).ListGroupNodesWithDBEntry(parentGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(subgrpNodes).NotTo(HaveKey(privateSub))
			Expect(ctx.IsAuthorized(utils.NodeAll, privateNode)).To(HaveOccurred(),
				"the door lock must not be controllable")
		})
	})

	// -----------------------------------------------------------------------
	// The original exploit: a degenerate row — access_type "subentity" with an
	// EMPTY sub_entity_ids list. createUserGroupEntry now refuses to write one,
	// but rows created before that guard still exist in deployed tables, so the
	// read path has to deny on its own. SetScopedConditions in ListGroupsForUser is what
	// makes a zero-condition scope fail closed.
	// -----------------------------------------------------------------------
	Context("regression: access_type=subentity with an EMPTY sub_entity_ids list", func() {
		BeforeEach(func() {
			shareWithGuest(utils.GroupSubEntityAccess, []string{})

			_, err := user_group_db.NewUserGroupDB(ctx).ListGroupsForUser("")
			Expect(err).NotTo(HaveOccurred())
		})

		It("marks the parent scoped so an unshared subgroup is denied", func() {
			Expect(ctx.IsAuthorized(utils.GroupListSubEntities, parentGroup)).To(Succeed(),
				"permission is still granted on the parent group")
			Expect(ctx.ExtraConditions).NotTo(BeNil(),
				"the scope must be registered even with zero sub-entities")

			match, err := ctx.IsConditionMatch(utils.GroupListSubEntities, parentGroup, privateSub)
			Expect(err).NotTo(HaveOccurred())
			Expect(match).To(BeFalse(),
				"a zero-condition scope must deny, not fall through to the unrestricted case")
		})

		It("discloses no subgroups at all", func() {
			rows, err := group_db.NewGroupDB(ctx).ListRowsWithGroupID(parentGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(subGroupIDs(rows)).To(BeEmpty())
		})

		It("grants no node permissions", func() {
			grpNodes, subgrpNodes, err := group_node_db.NewGroupNodeDB(ctx).ListGroupNodesWithDBEntry(parentGroup)
			Expect(err).NotTo(HaveOccurred())

			Expect(subgrpNodes).NotTo(HaveKey(privateSub))
			Expect(grpNodes).To(BeEmpty())
			Expect(ctx.IsAuthorized(utils.NodeAll, privateNode)).To(HaveOccurred(),
				"the door lock must not be controllable")
		})
	})

	// The write-side guard: the degenerate row can no longer be created at all.
	Context("write guard: createUserGroupEntry", func() {
		It("refuses subentity access with no subgroup id", func() {
			err := user_group_db.NewUserGroupDB(ctx).ConfirmSharingRequest(&sharing_request_db.SharingRequestEntry{
				UserID:      guestID,
				GroupID:     parentGroup,
				SubEntityID: "",
				AccessType:  string(utils.GroupSubEntityAccess),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("requires a subgroup id"))
		})

		It("still allows group-level access with no subgroup id", func() {
			Expect(user_group_db.NewUserGroupDB(ctx).CreateUserGroup("grp-fresh")).To(Succeed())
		})
	})

	// GetUserSubGroup grants the parent-group-wide GroupListSubEntities, so it
	// must register the narrowing itself. Nothing chains this bootstrap into an
	// enumeration today, which is why the hole was latent rather than live —
	// these specs stop that from depending on the absence of a caller.
	Context("regression: bootstrap via GetUserSubGroup", func() {
		BeforeEach(func() {
			shareWithGuest(utils.GroupSubEntityAccess, []string{sharedSub})

			entry, err := user_group_db.NewUserGroupDB(ctx).GetUserSubGroup(parentGroup, sharedSub)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.SubEntityIDs).To(ConsistOf(sharedSub))
		})

		It("registers the narrowing alongside the parent-wide grant", func() {
			Expect(ctx.IsAuthorized(utils.GroupListSubEntities, parentGroup)).To(Succeed())

			match, err := ctx.IsConditionMatch(utils.GroupListSubEntities, parentGroup, privateSub)
			Expect(err).NotTo(HaveOccurred())
			Expect(match).To(BeFalse(), "an unshared subgroup must not match")

			match, err = ctx.IsConditionMatch(utils.GroupListSubEntities, parentGroup, sharedSub)
			Expect(err).NotTo(HaveOccurred())
			Expect(match).To(BeTrue(), "the shared subgroup must still match")
		})

		It("confines enumeration to the shared subgroup", func() {
			rows, err := group_db.NewGroupDB(ctx).ListRowsWithGroupID(parentGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(subGroupIDs(rows)).To(ConsistOf(sharedSub))

			_, subgrpNodes, err := group_node_db.NewGroupNodeDB(ctx).ListGroupNodesWithDBEntry(parentGroup)
			Expect(err).NotTo(HaveOccurred())
			Expect(subgrpNodes).NotTo(HaveKey(privateSub))
			Expect(ctx.IsAuthorized(utils.NodeAll, privateNode)).To(HaveOccurred())
		})
	})
})
