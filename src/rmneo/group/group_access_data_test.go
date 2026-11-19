// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_test

import (
	"context"
	"errors"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetUserGroupSubgroupAccessWithData", func() {
	var (
		ownerCtx  *rmngctx.RmngContext
		sharedCtx *rmngctx.RmngContext
		parentID  string
		sharedSub string
		otherSub  string
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		ownerCtx = rmngctx.NewRmngContext(user.NewUser("access-data-owner"))
		sharedCtx = rmngctx.NewRmngContext(user.NewUser("access-data-subentity"))

		parentGroup, err := group.CreateGroupForUser(ownerCtx, "Access Data Parent")
		Expect(err).To(BeNil())
		parentID = parentGroup.GroupID

		sharedSubGroup, err := group.CreateSubGroup(ownerCtx, parentID, "Shared")
		Expect(err).To(BeNil())
		sharedSub = sharedSubGroup.SubGroupID

		otherSubGroup, err := group.CreateSubGroup(ownerCtx, parentID, "Other")
		Expect(err).To(BeNil())
		otherSub = otherSubGroup.SubGroupID

		ShareAndApproveSubGroup(ownerCtx, sharedCtx, parentID, sharedSub)
	})

	It("returns a whole-group grant for the owner", func() {
		g, err := group.GetUserGroupSubgroupAccessWithData(ownerCtx, parentID)
		Expect(err).To(BeNil())
		Expect(g).To(Equal(group.Group{GroupID: parentID, AccessType: utils.GroupPrimaryAccess}))
		Expect(g.HasWholeGroupAccess()).To(BeTrue())
	})

	It("returns the shared subgroups for a subentity member instead of failing", func() {
		g, err := group.GetUserGroupSubgroupAccessWithData(sharedCtx, parentID)
		Expect(err).To(BeNil())
		Expect(g).To(Equal(group.Group{GroupID: parentID, AccessType: utils.GroupSubEntityAccess, SubGroups: []group.SubGroup{{SubGroupID: sharedSub}}}))
		Expect(g.HasWholeGroupAccess()).To(BeFalse())
	})

	It("restricts a subentity member's context to its shared subgroups", func() {
		_, err := group.GetUserGroupSubgroupAccessWithData(sharedCtx, parentID)
		Expect(err).To(BeNil())

		match, err := sharedCtx.IsConditionMatch(utils.GroupListSubEntities, parentID, otherSub)
		Expect(err).To(BeNil())
		Expect(match).To(BeFalse())
	})

	It("treats a subentity grant with no shared subgroups as not the whole group", func() {
		Expect(group.Group{GroupID: parentID, AccessType: utils.GroupSubEntityAccess}.HasWholeGroupAccess()).To(BeFalse())
	})

	It("does not treat a Group without an access type as whole-group access", func() {
		Expect(group.Group{GroupID: parentID}.HasWholeGroupAccess()).To(BeFalse())
	})

	It("returns ErrGroupAccessDenied with a 403 for a group the caller has no grant on", func() {
		strangerCtx := rmngctx.NewRmngContext(user.NewUser("access-data-stranger"))
		_, err := group.GetUserGroupSubgroupAccessWithData(strangerCtx, parentID)
		Expect(err).To(MatchError(group.ErrGroupAccessDenied))
		Expect(clientErrorOf(err)).To(Equal(clientErr{http.StatusForbidden, "User does not have access to this group"}))
	})

	It("returns a 403 for a subentity grant whose shared subgroups are all gone", func() {
		// The share API never writes an empty subentity row, so put one directly.
		_, err := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock).PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(user_group_db.UserGroupMappingTable),
			Item: map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: "access-data-empty"},
				"group_id":       &types.AttributeValueMemberS{Value: parentID},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		_, err = group.GetUserGroupSubgroupAccessWithData(rmngctx.NewRmngContext(user.NewUser("access-data-empty")), parentID)
		Expect(err).To(MatchError(group.ErrGroupAccessDenied))
		Expect(clientErrorOf(err)).To(Equal(clientErr{http.StatusForbidden, "User does not have access to this group"}))
	})

	Describe("NarrowToSubGroup", func() {
		It("returns the group unchanged when no subgroup is named", func() {
			g, err := group.GetUserGroupSubgroupAccessWithData(ownerCtx, parentID)
			Expect(err).To(BeNil())
			narrowed, err := g.NarrowToSubGroup(ownerCtx, "")
			Expect(err).To(BeNil())
			Expect(narrowed).To(Equal(g))
		})

		It("narrows a subentity member to a subgroup shared with it", func() {
			g, err := group.GetUserGroupSubgroupAccessWithData(sharedCtx, parentID)
			Expect(err).To(BeNil())
			narrowed, err := g.NarrowToSubGroup(sharedCtx, sharedSub)
			Expect(err).To(BeNil())
			Expect(narrowed).To(Equal(group.Group{GroupID: parentID, AccessType: utils.GroupSubEntityAccess, SubGroups: []group.SubGroup{{SubGroupID: sharedSub}}}))
		})

		It("refuses a subentity member a sibling subgroup with a 403", func() {
			g, err := group.GetUserGroupSubgroupAccessWithData(sharedCtx, parentID)
			Expect(err).To(BeNil())
			_, err = g.NarrowToSubGroup(sharedCtx, otherSub)
			Expect(err).To(MatchError(group.ErrSubGroupAccessDenied))
			Expect(clientErrorOf(err)).To(Equal(clientErr{http.StatusForbidden, "User does not have access to this subgroup"}))
		})

		It("narrows a whole-group owner to any subgroup that exists", func() {
			g, err := group.GetUserGroupSubgroupAccessWithData(ownerCtx, parentID)
			Expect(err).To(BeNil())
			narrowed, err := g.NarrowToSubGroup(ownerCtx, otherSub)
			Expect(err).To(BeNil())
			Expect(narrowed).To(Equal(group.Group{GroupID: parentID, AccessType: utils.GroupSubEntityAccess, SubGroups: []group.SubGroup{{SubGroupID: otherSub}}}))
		})

		It("returns a 400 to a whole-group owner naming a subgroup that does not exist", func() {
			g, err := group.GetUserGroupSubgroupAccessWithData(ownerCtx, parentID)
			Expect(err).To(BeNil())
			_, err = g.NarrowToSubGroup(ownerCtx, "missing-sub")
			Expect(err).To(MatchError(group.ErrSubGroupAccessDenied))
			Expect(clientErrorOf(err)).To(Equal(clientErr{http.StatusBadRequest, "Subgroup not found"}))
		})
	})
})

type clientErr struct {
	status  int
	message string
}

func clientErrorOf(err error) clientErr {
	var rmErr *rmerror.RMError
	Expect(errors.As(err, &rmErr)).To(BeTrue(), "expected an RMError in the chain")
	status, message := rmErr.ClientError()
	return clientErr{status, message}
}
