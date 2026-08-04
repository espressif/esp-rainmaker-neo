// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodb_types "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// putNodeInGroup adds a group_device_mapping row directly via the dynamo mock, bypassing the user-level AddNode authz that would block users who don't yet own the node.
func putNodeInGroup(ctx context.Context, groupID, nodeID string, subgroups ...string) {
	item := map[string]dynamodb_types.AttributeValue{
		"group_id": &dynamodb_types.AttributeValueMemberS{Value: groupID},
		"node_id":  &dynamodb_types.AttributeValueMemberS{Value: nodeID},
	}
	for i, sg := range subgroups {
		if sg == "" {
			continue
		}
		item[fmt.Sprintf("subgrp%d", i+1)] = &dynamodb_types.AttributeValueMemberS{Value: sg}
	}
	_, err := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock).PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(group_node_db.GroupDeviceMappingTable),
		Item:      item,
	})
	Expect(err).To(BeNil())
}

var profile *mock.Profile
var timingFile *os.File
var _ = BeforeSuite(func() {
	timingFile, _ = test_utils.CreateCommonSummaryFile("assume_role_main.txt")
})

type ActionResourcePair struct {
	Action   string
	Resource []string
}

func TestAssumeRoleMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Assume Role Main Suite")
}

var _ = Describe("Assume Role Main", func() {
	var (
		ctx           context.Context
		stsMock       *mock.STSMock
		testUser      *user.User
		testUser2     *user.User
		rmng_context  *rmngctx.RmngContext
		rmng_context2 *rmngctx.RmngContext
		testGroup     *group.Group
		topicPrefix   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		stsMock = awscommon.GetSTSClient().(*mock.STSMock)

		GinkgoT().Setenv("IOT_USER_ROLE_ARN", "arn:aws:iam::123456789012:role/IoTUserRole")
		GinkgoT().Setenv("AWS_REGION", "us-east-1")

		testUser, rmng_context = test_utils.SetupTestUser(ctx, "test-user-id", "test-user@example.com")
		var err error
		testGroup, err = group.CreateGroupForUser(rmng_context, "Test Group")
		Expect(err).To(BeNil())
		Expect(testGroup).ToNot(BeNil())
		topicPrefix = "arn:aws:iot:us-east-1:00112233445566778899:"

		testUser2, rmng_context2 = test_utils.SetupTestUser(ctx, "test-user-id-2", "test-user-id-2@example.com")
	})

	AfterEach(func() {
		os.Unsetenv("IOT_USER_ROLE_ARN")
	})

	Describe("MQTT mode (no services)", func() {
		It("should set correct policy of the group the user is part of", func() {
			CallAssumeRoleHandler(ctx, testUser.GetID())

			policy := *stsMock.GetLastAssumeRoleInput().Policy

			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + testGroup.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/subgroups/*/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
					},
				},
				{
					Action: "iot:Connect",
					Resource: []string{
						topicPrefix + "client/user:test-user@example.com:*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(resources).To(Equal(pair.Resource), "For action: "+pair.Action)
			}
		})

		It("should set correct policy if user is part of multiple groups", func() {
			group2, err := group.CreateGroupForUser(rmng_context, "Test Group 2")
			Expect(err).To(BeNil())
			Expect(group2).ToNot(BeNil())

			CallAssumeRoleHandler(ctx, testUser.GetID())
			policy := *stsMock.GetLastAssumeRoleInput().Policy
			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + testGroup.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/subgroups/*/control",
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + group2.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group2.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group2.GroupID + "/subgroups/*/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group2.GroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group2.GroupID + "*/*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(resources).To(Equal(pair.Resource), "For action: "+pair.Action)
			}
		})

		It("should set correct policy for a user with owned groups and those shared with them", func() {
			group3, err := group.CreateGroupForUser(rmng_context2, "Test Group 3")
			Expect(err).To(BeNil())
			Expect(group3).ToNot(BeNil())

			_, err = group.ShareGroup(rmng_context, testGroup.GroupID, testUser2.GetID(), utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			sharingRequests, err := group.GetMySharingRequests(rmng_context2)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			sharingRequestID := sharingRequests[0].SharingRequestID
			err = group.ApproveSharingRequest(rmng_context2, sharingRequestID)
			Expect(err).To(BeNil())

			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			CallAssumeRoleHandler(ctx, testUser2.GetID())

			p := dbMock.ProfileGet()
			profile = &p
			readCount, writeCount := profile.TotalCounts()
			// 1 for ListGroupsForUser + 1 for the OIDC caller ResolveESPUserByID lookup.
			Expect(readCount).To(Equal(2))
			Expect(writeCount).To(Equal(0))

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + group3.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group3.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group3.GroupID + "/subgroups/*/control",
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + testGroup.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/subgroups/*/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group3.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group3.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*/*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(resources).To(Equal(pair.Resource), "For action: "+pair.Action)
			}
		})

		It("should set correct policy for a user with multiple subgroups", func() {
			group3, err := group.CreateGroupForUser(rmng_context2, "Test Group 3")
			Expect(err).To(BeNil())
			Expect(group3).ToNot(BeNil())

			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Test Sub Group")
			Expect(err).To(BeNil())
			Expect(subGroup).ToNot(BeNil())

			_, err = group.ShareSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID, testUser2.GetID(), auth.UserInfo{})
			Expect(err).To(BeNil())
			sharingRequests, err := group.GetMySharingRequests(rmng_context2)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			sharingRequestID := sharingRequests[0].SharingRequestID
			err = group.ApproveSharingRequest(rmng_context2, sharingRequestID)
			Expect(err).To(BeNil())

			CallAssumeRoleHandler(ctx, testUser2.GetID())
			policy := *stsMock.GetLastAssumeRoleInput().Policy
			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + group3.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group3.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + group3.GroupID + "/subgroups/*/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + testGroup.GroupID + "/subgroups/" + subGroup.SubGroupID + "/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group3.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*" + subGroup.SubGroupID + "*/*",
					},
				},
				{
					Action: "iot:Receive",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group3.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*" + subGroup.SubGroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + group3.GroupID + "*/*",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + testGroup.GroupID + "*" + subGroup.SubGroupID + "*/*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(pair.Resource).To(Equal(resources), "For action: "+pair.Action)
			}
		})

		It("should return an error for invalid request body", func() {
			request := events.APIGatewayProxyRequest{
				Body: "invalid json",
			}

			response, err := handleRequest(ctx, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should return 500 when IOT_USER_ROLE_ARN is not set", func() {
			os.Unsetenv("IOT_USER_ROLE_ARN")
			response := callRaw(ctx, testUser.GetID(), Request{})
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Internal server error"))
		})

		It("should forward request tags to STS AssumeRole", func() {
			response := callRaw(ctx, testUser.GetID(), Request{
				Tags: map[string]string{"clientType": "mobile", "appVersion": "1.2.3"},
			})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			input := stsMock.GetLastAssumeRoleInput()
			tagMap := make(map[string]string, len(input.Tags))
			for _, t := range input.Tags {
				tagMap[*t.Key] = *t.Value
			}
			Expect(tagMap).To(HaveKeyWithValue("clientType", "mobile"))
			Expect(tagMap).To(HaveKeyWithValue("appVersion", "1.2.3"))
		})
	})

	Describe("Services mode (per-node, path-routed)", func() {
		const nodeID = "node-abc"

		BeforeEach(func() {
			putNodeInGroup(ctx, testGroup.GroupID, nodeID)
			GinkgoT().Setenv("FILES_BUCKET_NAME", "esp-rm-files-123456789012-us-east-1")
		})

		It("should emit only S3 statements scoped to node when services=[s3]", func() {
			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), testGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy

			Expect(GetResourceForAction(policy, "s3:ListBucket")).To(ConsistOf("arn:aws:s3:::esp-rm-files-123456789012-us-east-1"))
			Expect(GetResourceForAction(policy, "s3:GetObject")).To(ConsistOf("arn:aws:s3:::esp-rm-files-123456789012-us-east-1/node-data/" + nodeID + "/*"))
			Expect(GetResourceForAction(policy, "s3:DeleteObject")).To(ConsistOf("arn:aws:s3:::esp-rm-files-123456789012-us-east-1/node-data/" + nodeID + "/*"))

			// No IoT/Cognito statements
			Expect(GetResourceForAction(policy, "iot:Publish")).To(BeEmpty())
			Expect(GetResourceForAction(policy, "iot:Subscribe")).To(BeEmpty())
			Expect(GetResourceForAction(policy, "iot:Connect")).To(BeEmpty())
			Expect(GetResourceForAction(policy, "cognito-identity:GetCredentialsForIdentity")).To(BeEmpty())
		})

		It("should emit only KVS statements scoped to node when services=[kvs]", func() {
			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), testGroup.GroupID, nodeID, []string{"kvs"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			expectedArn := "arn:aws:kinesisvideo:us-east-1:00112233445566778899:channel/rmng-v1-" + nodeID + "/*"

			Expect(GetResourceForAction(policy, "kinesisvideo:ConnectAsViewer")).To(ConsistOf(expectedArn))
			Expect(GetResourceForAction(policy, "kinesisvideo:GetSignalingChannelEndpoint")).To(ConsistOf(expectedArn))
			Expect(GetResourceForAction(policy, "kinesisvideo:DescribeSignalingChannel")).To(ConsistOf(expectedArn))
			Expect(GetResourceForAction(policy, "kinesisvideo:GetIceServerConfig")).To(ConsistOf(expectedArn))

			Expect(GetResourceForAction(policy, "s3:GetObject")).To(BeEmpty())
			Expect(GetResourceForAction(policy, "iot:Publish")).To(BeEmpty())
		})

		It("should emit both S3 and KVS statements when services=[s3,kvs]", func() {
			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), testGroup.GroupID, nodeID, []string{"s3", "kvs"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			Expect(GetResourceForAction(policy, "s3:GetObject")).To(ConsistOf("arn:aws:s3:::esp-rm-files-123456789012-us-east-1/node-data/" + nodeID + "/*"))
			Expect(GetResourceForAction(policy, "kinesisvideo:ConnectAsViewer")).To(ConsistOf("arn:aws:kinesisvideo:us-east-1:00112233445566778899:channel/rmng-v1-" + nodeID + "/*"))
		})

		It("should use O(1) DB access (point lookups only)", func() {
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ProfileReset()

			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), testGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			p := dbMock.ProfileGet()
			readCount, writeCount := p.TotalCounts()
			// 1 GetItem on group_device_mapping + 1 Query on user_group_mapping (point key)
			// + 1 for the OIDC caller ResolveESPUserByID lookup.
			Expect(readCount).To(Equal(3))
			Expect(writeCount).To(Equal(0))
		})

		It("should return 400 when body has services but path params are absent", func() {
			response := callRaw(ctx, testUser.GetID(), Request{Services: []string{"s3"}})
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("services requires"))
		})

		It("should return 400 when services is empty on the per-node route", func() {
			response := callRawWithPath(ctx, testUser.GetID(), Request{}, map[string]string{
				"groupId": testGroup.GroupID,
				"nodeId":  nodeID,
			})
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("services is required"))
		})

		It("should return 400 when services contains an unsupported value", func() {
			response := callRawWithPath(ctx, testUser.GetID(), Request{Services: []string{"mqtt"}}, map[string]string{
				"groupId": testGroup.GroupID,
				"nodeId":  nodeID,
			})
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("unsupported service"))
		})

		It("should return 403 when user has no access to the group", func() {
			response := CallAssumeRoleHandlerWithServices(ctx, testUser2.GetID(), testGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("does not have access"))
		})

		It("should return 403 when node does not belong to the supplied group", func() {
			otherGroup, err := group.CreateGroupForUser(rmng_context, "Other")
			Expect(err).To(BeNil())
			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), otherGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should return 403 when user has subgroup access but node belongs to a sibling subgroup", func() {
			subA, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "SubA")
			Expect(err).To(BeNil())
			subB, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "SubB")
			Expect(err).To(BeNil())

			putNodeInGroup(ctx, testGroup.GroupID, nodeID, subB.SubGroupID)

			_, err = group.ShareSubGroup(rmng_context, testGroup.GroupID, subA.SubGroupID, testUser2.GetID(), auth.UserInfo{})
			Expect(err).To(BeNil())
			sharingRequests, err := group.GetMySharingRequests(rmng_context2)
			Expect(err).To(BeNil())
			err = group.ApproveSharingRequest(rmng_context2, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			response := CallAssumeRoleHandlerWithServices(ctx, testUser2.GetID(), testGroup.GroupID, nodeID, []string{"kvs"})
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("should allow user access via a shared subgroup that the node belongs to", func() {
			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Sub")
			Expect(err).To(BeNil())

			putNodeInGroup(ctx, testGroup.GroupID, nodeID, subGroup.SubGroupID)

			_, err = group.ShareSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID, testUser2.GetID(), auth.UserInfo{})
			Expect(err).To(BeNil())
			sharingRequests, err := group.GetMySharingRequests(rmng_context2)
			Expect(err).To(BeNil())
			err = group.ApproveSharingRequest(rmng_context2, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			response := CallAssumeRoleHandlerWithServices(ctx, testUser2.GetID(), testGroup.GroupID, nodeID, []string{"kvs"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should omit S3 statements when FILES_BUCKET_NAME is unset", func() {
			os.Unsetenv("FILES_BUCKET_NAME")
			response := CallAssumeRoleHandlerWithServices(ctx, testUser.GetID(), testGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy
			Expect(GetResourceForAction(policy, "s3:ListBucket")).To(BeEmpty())
			Expect(GetResourceForAction(policy, "s3:GetObject")).To(BeEmpty())
		})
	})

	Describe("Admin handleRequest", func() {
		var (
			adminUser     *user.User
			adminContext  *rmngctx.RmngContext
			adminGroup    *group.Group
			adminSubgroup *group.SubGroup
		)

		BeforeEach(func() {
			adminUser, adminContext = test_utils.SetupTestAdminUser(ctx, "admin-user-id", "admin-user@example.com")

			var err error
			adminGroup, err = group.CreateGroupForUser(adminContext, "Admin Test Group")
			Expect(err).To(BeNil())
			Expect(adminGroup).ToNot(BeNil())

			adminSubgroup, err = group.CreateSubGroup(adminContext, adminGroup.GroupID, "Admin Test SubGroup")
			Expect(err).To(BeNil())
			Expect(adminSubgroup).ToNot(BeNil())
		})

		It("should set correct policy when admin requests specific group access", func() {
			response := CallAssumeRoleHandlerWithGroupAsAdmin(ctx, adminUser.GetID(), adminGroup.GroupID, "")
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy

			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/*/user/params-" + adminGroup.GroupID + "*/*",
						topicPrefix + "topic/rainmaker/nodes/groups/" + adminGroup.GroupID + "/control",
						topicPrefix + "topic/rainmaker/nodes/groups/" + adminGroup.GroupID + "/subgroups/*/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + adminGroup.GroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + adminGroup.GroupID + "*/*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(resources).To(Equal(pair.Resource), "For action: "+pair.Action)
			}
		})

		It("should set correct policy when admin requests specific subgroup access", func() {
			response := CallAssumeRoleHandlerWithGroupAsAdmin(ctx, adminUser.GetID(), adminGroup.GroupID, adminSubgroup.SubGroupID)
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			policy := *stsMock.GetLastAssumeRoleInput().Policy

			expectedPairs := []ActionResourcePair{
				{
					Action: "iot:Publish",
					Resource: []string{
						topicPrefix + "topic/rainmaker/nodes/groups/" + adminGroup.GroupID + "/subgroups/" + adminSubgroup.SubGroupID + "/control",
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + adminGroup.GroupID + "*" + adminSubgroup.SubGroupID + "*/*",
					},
				},
				{
					Action: "iot:Subscribe",
					Resource: []string{
						topicPrefix + "*/$aws/things/*/shadow/name/params-" + adminGroup.GroupID + "*" + adminSubgroup.SubGroupID + "*/*",
					},
				},
			}

			for _, pair := range expectedPairs {
				resources := GetResourceForAction(policy, pair.Action)
				Expect(resources).To(Equal(pair.Resource), "For action: "+pair.Action)
			}
		})

		It("should let super-admin assume role for any node", func() {
			nodeID := "admin-target-node"
			putNodeInGroup(ctx, testGroup.GroupID, nodeID)

			GinkgoT().Setenv("FILES_BUCKET_NAME", "esp-rm-files-123456789012-us-east-1")

			response := CallAssumeRoleHandlerWithServicesAsAdmin(ctx, adminUser.GetID(), testGroup.GroupID, nodeID, []string{"s3"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should return forbidden when non-admin user requests specific group access", func() {
			response := CallAssumeRoleHandlerWithGroup(ctx, testUser.GetID(), adminGroup.GroupID, "")

			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Admin privileges required"))
		})

		It("should return bad request when admin requests non-existent group", func() {
			response := CallAssumeRoleHandlerWithGroupAsAdmin(ctx, adminUser.GetID(), "nonexistent-group", "")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found"))
		})

		It("should return bad request when admin requests non-existent subgroup", func() {
			response := CallAssumeRoleHandlerWithGroupAsAdmin(ctx, adminUser.GetID(), adminGroup.GroupID, "nonexistent-subgroup")

			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Subgroup not found"))
		})
	})

	// The session policy grows per accessible group. STS caps the AssumeRole
	// Policy parameter at 2048 chars and its API model does not declare that max,
	// so an overrun is only caught by STS at request time.
	Describe("Session policy size budget", func() {
		// groupsForUser gives the user full access to n groups total (testGroup
		// already counts as one) and returns the policy the handler would send.
		policyForGroupCount := func(n int) string {
			for i := 1; i < n; i++ {
				extra, err := group.CreateGroupForUser(rmng_context, fmt.Sprintf("Budget Group %d", i))
				Expect(err).To(BeNil())
				Expect(extra).ToNot(BeNil())
			}
			CallAssumeRoleHandler(ctx, testUser.GetID())
			return *stsMock.GetLastAssumeRoleInput().Policy
		}

		It("should stay within the STS limit at the supported group count", func() {
			policy := policyForGroupCount(supportedGroupsPerSession)

			Expect(len(policy)).To(BeNumerically("<=", maxSessionPolicyChars),
				fmt.Sprintf("policy for %d groups is %d chars; STS rejects anything over %d",
					supportedGroupsPerSession, len(policy), maxSessionPolicyChars))
		})

		It("should reject with a diagnosable error instead of letting STS fail opaquely", func() {
			for i := 1; i <= supportedGroupsPerSession; i++ {
				extra, err := group.CreateGroupForUser(rmng_context, fmt.Sprintf("Overflow Group %d", i))
				Expect(err).To(BeNil())
				Expect(extra).ToNot(BeNil())
			}

			response := callRaw(ctx, testUser.GetID(), Request{})

			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("too many accessible groups"))
		})
	})
})

// End users federate through the OIDC provider ("<issuer>:<sub>"); Cognito admins keep the "CognitoSignIn:<sub>" marker.
// extractCallerIdentity reads the shape to pick the resolver.
func oidcProvider(userID string) string    { return "https://issuer.example:" + userID }
func cognitoProvider(userID string) string { return ":CognitoSignIn:" + userID }

// callRaw invokes the handler against the default /v1/assumed-roles route (no path parameters) with the given body, as an OIDC end user.
func callRaw(ctx context.Context, userID string, body Request) events.APIGatewayProxyResponse {
	return callRawWithPath(ctx, userID, body, nil)
}

// callRawWithPath invokes the handler with explicit PathParameters as an OIDC end user. Used to simulate the per-node /v1/groups/{group_id}/nodes/{node_id}/assumed-roles route in tests.
func callRawWithPath(ctx context.Context, userID string, body Request, pathParams map[string]string) events.APIGatewayProxyResponse {
	return callRawWithProvider(ctx, userID, oidcProvider(userID), body, pathParams)
}

func callRawWithProvider(ctx context.Context, userID, provider string, body Request, pathParams map[string]string) events.APIGatewayProxyResponse {
	requestJSON, _ := json.Marshal(body)
	request := events.APIGatewayProxyRequest{
		Body:           string(requestJSON),
		PathParameters: pathParams,
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

func CallAssumeRoleHandler(ctx context.Context, userID string) events.APIGatewayProxyResponse {
	stsMock := awscommon.GetSTSClient().(*mock.STSMock)
	response := callRaw(ctx, userID, Request{})
	Expect(response.StatusCode).To(Equal(200))

	var responseBody Response
	json.Unmarshal([]byte(response.Body), &responseBody)
	Expect(responseBody.AccessKey).To(Equal("new-access-key"))
	Expect(responseBody.SecretKey).To(Equal("new-secret-key"))
	Expect(responseBody.SessionToken).To(Equal("new-session-token"))
	Expect(responseBody.Expiration).ToNot(BeNil())
	Expect(*responseBody.Expiration).To(BeNumerically(">", 0))
	Expect(*stsMock.GetLastAssumeRoleInput().RoleArn).To(Equal("arn:aws:iam::123456789012:role/IoTUserRole"))
	return response
}

func CallAssumeRoleHandlerWithServices(ctx context.Context, userID, groupID, nodeID string, services []string) events.APIGatewayProxyResponse {
	return callRawWithProvider(ctx, userID, oidcProvider(userID), Request{Services: services}, servicesPathParams(groupID, nodeID))
}

func CallAssumeRoleHandlerWithServicesAsAdmin(ctx context.Context, userID, groupID, nodeID string, services []string) events.APIGatewayProxyResponse {
	return callRawWithProvider(ctx, userID, cognitoProvider(userID), Request{Services: services}, servicesPathParams(groupID, nodeID))
}

func servicesPathParams(groupID, nodeID string) map[string]string {
	return map[string]string{"groupId": groupID, "nodeId": nodeID}
}

func CallAssumeRoleHandlerWithGroup(ctx context.Context, userID, groupID, subgroupID string) events.APIGatewayProxyResponse {
	return callRawWithProvider(ctx, userID, oidcProvider(userID), Request{}, groupPathParams(groupID, subgroupID))
}

func CallAssumeRoleHandlerWithGroupAsAdmin(ctx context.Context, userID, groupID, subgroupID string) events.APIGatewayProxyResponse {
	return callRawWithProvider(ctx, userID, cognitoProvider(userID), Request{}, groupPathParams(groupID, subgroupID))
}

func groupPathParams(groupID, subgroupID string) map[string]string {
	pathParams := map[string]string{"groupId": groupID}
	if subgroupID != "" {
		pathParams["subGroupId"] = subgroupID
	}
	return pathParams
}

func GetResourceForAction(policy string, action string) []string {
	var policyDoc map[string]interface{}
	err := json.Unmarshal([]byte(policy), &policyDoc)
	if err != nil {
		return nil
	}

	statements, ok := policyDoc["Statement"].([]interface{})
	if !ok {
		return nil
	}

	var resources []string
	for _, stmt := range statements {
		statement, ok := stmt.(map[string]interface{})
		if !ok {
			continue
		}

		actions, ok := statement["Action"].([]interface{})
		if !ok {
			actionStr, ok := statement["Action"].(string)
			if !ok {
				continue
			}
			actions = []interface{}{actionStr}
		}

		for _, act := range actions {
			if act.(string) == action {
				resource, ok := statement["Resource"].([]interface{})
				if !ok {
					resourceStr, ok := statement["Resource"].(string)
					if !ok {
						continue
					}
					resources = append(resources, resourceStr)
				} else {
					for _, res := range resource {
						resources = append(resources, res.(string))
					}
				}
				break
			}
		}
	}

	return resources
}

var _ = AfterSuite(func() {
	if profile != nil {
		fmt.Fprintf(timingFile, "\n--- Assume Role ---\n")
		profile.Print(timingFile)
		fmt.Fprintf(timingFile, "-----------------------------\n\n")
	}
	timingFile.Close()
})
