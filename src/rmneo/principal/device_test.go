// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package principal_test

import (
	"context"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/principal"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Device principal", func() {
	const (
		nodeID     = "principal-device-1"
		groupID    = "principal-grp-own"
		otherGroup = "principal-grp-other"
	)

	var ctx context.Context

	requestFrom := func(identity events.APIGatewayRequestIdentity) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{RequestContext: events.APIGatewayProxyRequestContext{Identity: identity}}
	}

	isDevice := func(rmngCtx *rmngctx.RmngContext) bool {
		if rmngCtx == nil {
			return false
		}
		_, ok := rmngCtx.Accessor.(*principal.DeviceActor)
		return ok
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
	})

	Describe("FromAPIRequest for a device in a group", func() {
		var rmngCtx *rmngctx.RmngContext

		BeforeEach(func() {
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
			rmngCtx = principal.FromAPIRequest(ctx, requestFrom(test_utils.DeviceIdentity(nodeID)))
			Expect(rmngCtx).ToNot(BeNil())
		})

		It("builds a device principal identified by the node its certificate is attached to", func() {
			Expect(isDevice(rmngCtx)).To(BeTrue())
			Expect(rmngCtx.Accessor.GetID()).To(Equal(nodeID))
			scoped := rmngCtx.Accessor.(rmngctx.NodeScopedAccessor)
			Expect(scoped.ScopedNodeID()).To(Equal(nodeID))
			Expect(scoped.ScopedGroupID()).To(Equal(groupID))
		})

		It("grants group read on the node's group", func() {
			Expect(rmngCtx.IsAuthorized(utils.GroupGet, groupID)).To(BeNil())
			Expect(rmngCtx.IsAuthorized(utils.GroupListSubEntities, groupID)).To(BeNil())
		})

		It("grants no group write, share or delete", func() {
			for _, action := range []utils.GroupActions{
				utils.GroupUpdate, utils.GroupDelete, utils.GroupShare, utils.GroupEditNodes,
				utils.GroupCreateSubGroup, utils.GroupUpdateCapabilities, utils.GroupListUsers,
				utils.GroupEditAutomation,
			} {
				Expect(rmngCtx.IsAuthorized(action, groupID)).To(HaveOccurred(), "unexpectedly authorized for %s", action)
			}
		})

		It("grants nothing on any other group", func() {
			Expect(rmngCtx.IsAuthorized(utils.GroupGet, otherGroup)).To(HaveOccurred())
			Expect(rmngCtx.IsAuthorized(utils.GroupListSubEntities, otherGroup)).To(HaveOccurred())
		})

		It("grants read on its own node and nothing on others up front", func() {
			// Access to the group's other nodes is granted per node by the group-membership flow when the group's nodes are listed.
			Expect(rmngCtx.IsAuthorized(utils.NodeGet, nodeID)).To(BeNil())
			Expect(rmngCtx.IsAuthorized(utils.NodeUpdate, nodeID)).To(HaveOccurred())
			Expect(rmngCtx.IsAuthorized(utils.NodeGet, "some-node")).To(HaveOccurred())
		})

		It("lists exactly its own group through the group package", func() {
			groups, err := group.ListGroupsForUser(rmngCtx, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].GroupID).To(Equal(groupID))
			Expect(groups[0].AccessType).To(Equal(utils.GroupDeviceAccess))
		})
	})

	Describe("group scope follows live membership", func() {
		It("refuses a node that belongs to no group", func() {
			Expect(principal.FromAPIRequest(ctx, requestFrom(test_utils.DeviceIdentity(nodeID)))).To(BeNil())
		})

		It("reads the new group on the next request after a move, with the same credentials", func() {
			identity := test_utils.DeviceIdentity(nodeID)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
			before := principal.FromAPIRequest(ctx, requestFrom(identity))
			Expect(before.IsAuthorized(utils.GroupGet, groupID)).To(BeNil())

			// AddNode moves a node out of its old group first, as the user API does.
			adminCtx := rmngctx.NewRmngContext(user.NewUser("mover"))
			adminCtx.SetAllow(utils.NodeEditGroups, nodeID)
			adminCtx.SetAllow(utils.GroupEditNodes, otherGroup)
			adminCtx.SetAllow(utils.NodeGet, nodeID)
			_, err := group.AddNode(adminCtx, otherGroup, nodeID, nil)
			Expect(err).ToNot(HaveOccurred())

			after := principal.FromAPIRequest(ctx, requestFrom(identity))
			Expect(after).ToNot(BeNil())
			Expect(after.IsAuthorized(utils.GroupGet, otherGroup)).To(BeNil())
			Expect(after.IsAuthorized(utils.GroupGet, groupID)).To(HaveOccurred())
		})
	})

	Describe("rejected callers", func() {
		BeforeEach(func() {
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
		})

		DescribeTable("refuses a non-Cognito signer that is not a credential-provider session, rather than falling through to the user path",
			func(userArn string) {
				Expect(principal.FromAPIRequest(ctx, requestFrom(events.APIGatewayRequestIdentity{UserArn: userArn}))).To(BeNil())
			},
			Entry("session not named after a certificate", "arn:aws:sts::123456789012:assumed-role/rmng-controller-role-us-east-1/device-session"),
			Entry("IAM user", "arn:aws:iam::123456789012:user/operator"),
			Entry("malformed ARN", "not-an-arn"),
		)

		It("refuses a certificate that is attached to no thing", func() {
			unattached := test_utils.CredentialProviderIdentity(strings.Repeat("ab", 32))
			Expect(principal.FromAPIRequest(ctx, requestFrom(unattached))).To(BeNil())
		})

		It("resolves an app user's Identity Pool signature as a user, never as a device", func() {
			identity := test_utils.DeviceIdentity(nodeID)
			identity.CognitoIdentityID = "eu-west-1:identity-id"
			identity.CognitoAuthenticationProvider = "https://issuer.example:some-user"
			Expect(isDevice(principal.FromAPIRequest(ctx, requestFrom(identity)))).To(BeFalse())
		})

		It("refuses a request with no IAM signature", func() {
			Expect(principal.FromAPIRequest(ctx, requestFrom(events.APIGatewayRequestIdentity{}))).To(BeNil())
		})
	})
})
