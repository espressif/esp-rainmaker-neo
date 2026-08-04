// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAdminNodeGroupsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admin Node Groups Main Suite")
}

var _ = Describe("Admin Node Groups Main", func() {
	var (
		ctx          context.Context
		adminUser    *user.User
		adminContext *rmngctx.RmngContext
		testUser     *user.User
		testGroup    *group.Group
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()

		adminUser, adminContext = test_utils.SetupTestAdminUser(ctx, "admin-user-id", "admin-user@example.com")
		testUser, _ = test_utils.SetupTestUser(ctx, "test-user-id", "test-user@example.com")

		var err error
		testGroup, err = group.CreateGroupForUser(adminContext, "Test Group")
		Expect(err).To(BeNil())
		Expect(testGroup).ToNot(BeNil())
	})

	Describe("handleRequest", func() {
		It("should return group info for a node in a group", func() {
			nodeID := "test-node-1"
			test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, nodeID)

			response := CallNodeGroupsHandler(ctx, adminUser.GetID(), nodeID)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body NodeGroupsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.Group).To(Equal(testGroup.GroupID))
			Expect(body.SubGroups).To(BeEmpty())
		})

		It("should return group info with subgroups for a node in subgroups", func() {
			nodeID := "test-node-2"
			subGroup1, err := group.CreateSubGroup(adminContext, testGroup.GroupID, "Sub Group 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(adminContext, testGroup.GroupID, "Sub Group 2")
			Expect(err).To(BeNil())

			// Manually add node to group with subgroups via direct DB mock
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.PutItem(ctx, &dynamodb.PutItemInput{
				TableName: aws.String(group_node_db.GroupDeviceMappingTable),
				Item: map[string]types.AttributeValue{
					"group_id": &types.AttributeValueMemberS{Value: testGroup.GroupID},
					"node_id":  &types.AttributeValueMemberS{Value: nodeID},
					"subgrp1":  &types.AttributeValueMemberS{Value: subGroup1.SubGroupID},
					"subgrp2":  &types.AttributeValueMemberS{Value: subGroup2.SubGroupID},
				},
			})

			response := CallNodeGroupsHandler(ctx, adminUser.GetID(), nodeID)

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body NodeGroupsResponse
			err = json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.Group).To(Equal(testGroup.GroupID))
			Expect(body.SubGroups).To(HaveLen(2))
			Expect(body.SubGroups).To(ContainElement(subGroup1.SubGroupID))
			Expect(body.SubGroups).To(ContainElement(subGroup2.SubGroupID))
		})

		It("should return empty result for a node not in any group", func() {
			response := CallNodeGroupsHandler(ctx, adminUser.GetID(), "non-existent-node")

			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body NodeGroupsResponse
			err := json.Unmarshal([]byte(response.Body), &body)
			Expect(err).To(BeNil())
			Expect(body.Group).To(BeEmpty())
			Expect(body.SubGroups).To(BeEmpty())
		})

		It("should return 403 Forbidden for non-admin user", func() {
			nodeID := "test-node-3"
			test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, nodeID)

			response := CallNodeGroupsHandlerAsEndUser(ctx, testUser.GetID(), nodeID)

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})

		It("should return 400 for missing nodeId", func() {
			response := CallNodeGroupsHandler(ctx, adminUser.GetID(), "")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("nodeId is required"))
		})
	})
})

func CallNodeGroupsHandler(ctx context.Context, userID string, nodeID string) events.APIGatewayProxyResponse {
	return callNodeGroupsHandlerWithProvider(ctx, userID, ":CognitoSignIn:"+userID, nodeID)
}

// CallNodeGroupsHandlerAsEndUser drives the handler as a passwordless OIDC end user: the provider string is "<issuer>:<sub>" so extractCallerIdentity resolves via ResolveESPUserByID.
func CallNodeGroupsHandlerAsEndUser(ctx context.Context, userID string, nodeID string) events.APIGatewayProxyResponse {
	return callNodeGroupsHandlerWithProvider(ctx, userID, "https://issuer.example:"+userID, nodeID)
}

func callNodeGroupsHandlerWithProvider(ctx context.Context, userID, provider, nodeID string) events.APIGatewayProxyResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Resource:   "/v1/admin/nodes/{nodeId}/groups",
		Path:       "/v1/admin/nodes/" + nodeID + "/groups",
		PathParameters: map[string]string{
			"nodeId": nodeID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: provider,
			},
		},
	}

	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	return response
}
