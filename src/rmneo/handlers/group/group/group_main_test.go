// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var profiles = map[string]*mock.Profile{}

var _ = Describe("Group Main", func() {
	var (
		ctx            context.Context
		dbMock         *mock.DynamoDBMock
		userID         string
		otherUserID    string
		userEmail      string
		otherUserEmail string
		otherUserPhone string
		groupName      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		dbMock = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

		userID = "test-user-id"
		otherUserID = "other-user-id"
		userEmail = "test-user@example.com"
		otherUserEmail = "other-user@example.com"
		otherUserPhone = "+919876543210"
		groupName = "Test Group"

		// Set up users using helper functions. The invitee carries a phone number
		// too, so sharing can be exercised by either form of user name.
		test_utils.SetupTestUser(ctx, userID, userEmail)
		test_utils.SetupTestUserWithPhone(ctx, otherUserID, otherUserEmail, otherUserPhone)
	})

	Describe("handlePostGroup", func() {
		It("should successfully create a group", func() {
			groupID := CreateGroup(ctx, groupName, userID)

			// Verify that the group was stored in DynamoDB
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Key: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item["group_name"].(*types.AttributeValueMemberS).Value).To(Equal("Test Group"))

			// Verify that the user was associated with the group
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: userID},
				"group_id":       &types.AttributeValueMemberS{Value: groupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should successfully create a group with matter capability", func() {
			// Create group with matter capability
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups",
				Body:       `{"group_name": "Matter Group", "capabilities": ["matter"]}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}
			response, err := handlePostGroup(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			// Parse the response
			var rawResponse map[string]interface{}
			err = json.Unmarshal([]byte(response.Body), &rawResponse)
			Expect(err).To(BeNil())

			groupID, ok := rawResponse["group_id"].(string)
			Expect(ok).To(BeTrue())
			Expect(groupID).ToNot(BeEmpty())

			// Get matter data from top-level of response (not nested under capability_data)
			matterData, ok := rawResponse["matter"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "matter should be present at top level of response")

			// Verify Matter fields are present
			Expect(matterData["fabric_id"]).ToNot(BeEmpty())
			Expect(matterData["root_ca"]).ToNot(BeEmpty())
			Expect(matterData["ipk"]).ToNot(BeEmpty())
			Expect(matterData["group_cat_id_admin"]).ToNot(BeEmpty())
			Expect(matterData["group_cat_id_operate"]).ToNot(BeEmpty())

			// Verify fabric_id is derived from group_id (ASCII to hex encoding)
			fabricID := matterData["fabric_id"].(string)
			expectedFabricID := group.FabricIDFromGroupID(groupID)
			Expect(fabricID).To(Equal(expectedFabricID))

			// Verify root_ca is a valid PEM certificate
			rootCA := matterData["root_ca"].(string)
			Expect(rootCA).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(rootCA).To(ContainSubstring("-----END CERTIFICATE-----"))

			// Verify that DB entry matches HTTP response
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Key: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item["group_name"].(*types.AttributeValueMemberS).Value).To(Equal("Matter Group"))

			// Verify capabilities list is stored
			capList, ok := result.Item["capabilities"].(*types.AttributeValueMemberL)
			Expect(ok).To(BeTrue())
			Expect(capList.Value).To(HaveLen(1))
			Expect(capList.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("matter"))

			// Parse cap_matter JSON from DB and compare with HTTP response
			capMatterJSON, ok := result.Item["cap_matter"].(*types.AttributeValueMemberS)
			Expect(ok).To(BeTrue())
			var dbMatterData map[string]interface{}
			err = json.Unmarshal([]byte(capMatterJSON.Value), &dbMatterData)
			Expect(err).To(BeNil())

			// Compare HTTP response matter data with DB stored data
			Expect(matterData["fabric_id"]).To(Equal(dbMatterData["fabric_id"]))
			Expect(matterData["root_ca"]).To(Equal(dbMatterData["root_ca"]))
			Expect(matterData["ipk"]).To(Equal(dbMatterData["ipk"]))
			Expect(matterData["group_cat_id_admin"]).To(Equal(dbMatterData["group_cat_id_admin"]))
			Expect(matterData["group_cat_id_operate"]).To(Equal(dbMatterData["group_cat_id_operate"]))

			// Verify user-group mapping was created
			userGroupDB := user_group_db.NewUserGroupDB(rmngctx.NewRmngContext(user.NewUser(userID)))
			userGroup, err := userGroupDB.GetUserGroup(groupID)
			Expect(err).To(BeNil())
			Expect(userGroup.AccessType).To(Equal(utils.GroupPrimaryAccess))
		})

		It("should return an error for invalid request body", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups",
				Body:       "invalid json",
				Path:       "/v1/groups",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}
			response, err := handlePostGroup(ctx, request)

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should successfully create a subgroup", func() {
			// First, create a main group
			groupID := CreateGroup(ctx, groupName, userID)

			// Now, create a subgroup
			subgroupID := CreateSubgroup(ctx, groupID, "subgroupName", userID)

			// Verify that the subgroup was stored in DynamoDB
			test_utils.AssertRowInDB(group_db.GroupsTable, map[string]types.AttributeValue{
				"group_id":     &types.AttributeValueMemberS{Value: groupID},
				"sub_group_id": &types.AttributeValueMemberS{Value: subgroupID},
				"group_name":   &types.AttributeValueMemberS{Value: "subgroupName"},
			})
		})
	})

	Describe("handleListGroups", func() {
		It("should successfully list groups and subgroups for the user", func() {
			// Add Group 1
			groupID1 := CreateGroup(ctx, "Test Group 1", userID)

			// Add Group 2
			groupID2 := CreateGroup(ctx, "Test Group 2", userID)

			// List Groups
			listGroupsResponse := ListGroups(ctx, userID)
			Expect(listGroupsResponse.Groups).To(HaveLen(2))
			Expect(listGroupsResponse.Groups).To(ContainElements(GroupInfo{GroupID: groupID1, GroupName: "Test Group 1", AccessType: "primary"}, GroupInfo{GroupID: groupID2, GroupName: "Test Group 2", AccessType: "primary"}))

			// Verify the user_group_mapping table
			dbMock.ForEachRow(user_group_db.UserGroupMappingTable, func(item map[string]types.AttributeValue) error {
				Expect(item["user_id"].(*types.AttributeValueMemberS).Value).To(Equal(userID))
				Expect(item["group_id"].(*types.AttributeValueMemberS).Value).To(BeElementOf(groupID1, groupID2))
				return nil
			})
		})

		It("should surface module-provided capability_data verbatim in list groups response", func() {
			// Create a group
			groupID := CreateGroup(ctx, "Capability Data Group", userID)

			// Create a node and add to group
			nodeID := "test-extcap-node"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			detailValue := "de152ff2-f070-4d0e-94da-770828b1770f"

			// An optional module tags the node with its capability and writes the
			// per-capability detail into capability_data. Core stores and returns
			// capability_data verbatim — it does not interpret the capability name
			// or its detail shape.
			updateInput := &dynamodb.UpdateItemInput{
				TableName: aws.String(group_node_db.GroupDeviceMappingTable),
				Key: map[string]types.AttributeValue{
					"group_id": &types.AttributeValueMemberS{Value: groupID},
					"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				},
				UpdateExpression: aws.String("SET capabilities = :caps, capability_data = :cd"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":caps": &types.AttributeValueMemberL{
						Value: []types.AttributeValue{
							&types.AttributeValueMemberS{Value: "extcap"},
						},
					},
					":cd": &types.AttributeValueMemberM{
						Value: map[string]types.AttributeValue{
							"extcap": &types.AttributeValueMemberM{
								Value: map[string]types.AttributeValue{
									"external_id": &types.AttributeValueMemberS{Value: detailValue},
								},
							},
						},
					},
				},
			}
			_, err := dbMock.UpdateItem(ctx, updateInput)
			Expect(err).To(BeNil())

			// List groups
			listGroupsResponse := ListGroups(ctx, userID)
			Expect(listGroupsResponse.Groups).To(HaveLen(1))

			// Verify node_details carries the capability + its detail verbatim
			grp := listGroupsResponse.Groups[0]
			Expect(grp.NodeDetails).ToNot(BeNil())
			Expect(grp.NodeDetails).To(HaveKey(nodeID))

			nodeInfo := grp.NodeDetails[nodeID]
			Expect(nodeInfo.Capabilities).To(ContainElement("extcap"))

			capData := nodeInfo.CapabilityDetails["extcap"].(map[string]interface{})
			Expect(capData["external_id"]).To(Equal(detailValue))
		})

		It("should list rmng/matter node capabilities and derive matter_node_id", func() {
			groupID := CreateGroup(ctx, "Capabilities Group", userID)
			nodeID := "test-matter-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			// Tag the node with rmng + matter, as node association does.
			_, err := dbMock.UpdateItem(ctx, &dynamodb.UpdateItemInput{
				TableName: aws.String(group_node_db.GroupDeviceMappingTable),
				Key: map[string]types.AttributeValue{
					"group_id": &types.AttributeValueMemberS{Value: groupID},
					"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				},
				UpdateExpression: aws.String("SET capabilities = :caps"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":caps": &types.AttributeValueMemberL{Value: []types.AttributeValue{
						&types.AttributeValueMemberS{Value: "rmng"},
						&types.AttributeValueMemberS{Value: "matter"},
					}},
				},
			})
			Expect(err).To(BeNil())

			// matter_node_id is derived from the node_id, not stored. rmng carries no detail.
			grp := ListGroups(ctx, userID).Groups[0]
			test_utils.AssertNormalizedEqual(grp.NodeDetails, map[string]NodeCapabilityInfo{
				nodeID: {
					Capabilities: []string{"rmng", "matter"},
					CapabilityDetails: map[string]interface{}{
						"matter": map[string]interface{}{
							"matter_node_id": group.MatterNodeIDFromThingName(nodeID),
						},
					},
				},
			})
		})

		It("should successfully list groups, subgroups and nodes for the user", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)
			extraGroupID := CreateGroup(ctx, "Extra Group", userID)

			// Create a node
			nodeID1 := "test-node-id-1"
			nodeID2 := "test-node-id-2"

			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID1)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID2)

			// Create a subgroup
			subgroupID1 := CreateSubgroup(ctx, groupID, "Subgroup1", userID)
			subgroupID2 := CreateSubgroup(ctx, groupID, "Subgroup2", userID)

			// Add the node to the subgroup
			AddNodeToSubgroup(ctx, groupID, nodeID1, subgroupID1, userID)
			AddNodeToSubgroup(ctx, groupID, nodeID2, subgroupID2, userID)

			dbMock.ProfileReset()
			// List groups
			listGroupsResponse := ListGroups(ctx, userID)
			profile := dbMock.ProfileGet()
			readCount, writeCount := profile.TotalCounts()
			// +1 read vs. the Cognito path: OIDC callers resolve via a ResolveESPUserByID lookup on espuser-user-details, which lands inside the profiled window.
			Expect(readCount).To(Equal(6))
			Expect(writeCount).To(Equal(0))
			profiles["Group Main (List Groups/Subgroups/Nodes - 2 groups)"] = &profile

			test_utils.AssertNormalizedEqual(listGroupsResponse.Groups, []GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  "Main Group",
					AccessType: "primary",
					NodeIDs:    []string{nodeID1, nodeID2},
					NodeDetails: map[string]NodeCapabilityInfo{
						nodeID1: {Capabilities: []string{"rmng"}},
						nodeID2: {Capabilities: []string{"rmng"}},
					},
					Subgroups: []SubGroupInfo{
						{
							SubgroupID:   subgroupID1,
							SubgroupName: "Subgroup1",
							NodeIDs:      []string{nodeID1},
						},
						{
							SubgroupID:   subgroupID2,
							SubgroupName: "Subgroup2",
							NodeIDs:      []string{nodeID2},
						},
					},
				},
				{
					GroupID:    extraGroupID,
					GroupName:  "Extra Group",
					AccessType: "primary",
					NodeIDs:    nil,
					Subgroups:  nil,
				},
			})

			// Now remove the node from the subgroup
			response, err := RemoveNodeFromSubgroup(ctx, groupID, nodeID1, subgroupID1, userID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify List groups is accurate
			listGroupsResponse = ListGroups(ctx, userID)

			test_utils.AssertNormalizedEqual(listGroupsResponse.Groups, []GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  "Main Group",
					AccessType: "primary",
					NodeIDs:    []string{nodeID1, nodeID2},
					NodeDetails: map[string]NodeCapabilityInfo{
						nodeID1: {Capabilities: []string{"rmng"}},
						nodeID2: {Capabilities: []string{"rmng"}},
					},
					Subgroups: []SubGroupInfo{
						{
							SubgroupID:   subgroupID1,
							SubgroupName: "Subgroup1",
						},
						{
							SubgroupID:   subgroupID2,
							SubgroupName: "Subgroup2",
							NodeIDs:      []string{nodeID2},
						},
					},
				},
				{
					GroupID:    extraGroupID,
					GroupName:  "Extra Group",
					AccessType: "primary",
					NodeIDs:    nil,
					Subgroups:  nil,
				},
			})
		})

		It("should return an empty list when the user has no groups", func() {
			listGroupsResponse := ListGroups(ctx, userID)

			Expect(listGroupsResponse.Groups).To(BeEmpty())
		})

		It("should return an empty list when the group has no nodes", func() {
			// Create a group
			groupID := CreateGroup(ctx, groupName, userID)

			// List nodes for the group
			ListGroupsResponse := ListGroups(ctx, userID)
			Expect(ListGroupsResponse.Groups).To(Equal([]GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  groupName,
					AccessType: "primary",
					NodeIDs:    nil,
					Subgroups:  nil,
				},
			}))
		})

		It("should include matter data when listing groups with matter capability", func() {
			// Create a group with matter capability
			groupID := CreateGroupWithMatter(ctx, "Matter List Group", userID)

			// List groups
			listGroupsResponse := ListGroups(ctx, userID)
			Expect(listGroupsResponse.Groups).To(HaveLen(1))

			// Verify the group has matter data in the response
			grp := listGroupsResponse.Groups[0]
			Expect(grp.GroupID).To(Equal(groupID))
			Expect(grp.GroupName).To(Equal("Matter List Group"))
			Expect(grp.Matter).ToNot(BeNil())

			// Verify Matter fields are present
			Expect(grp.Matter["fabric_id"]).ToNot(BeEmpty())
			Expect(grp.Matter["root_ca"]).ToNot(BeEmpty())
			Expect(grp.Matter["ipk"]).ToNot(BeEmpty())
			Expect(grp.Matter["group_cat_id_admin"]).ToNot(BeEmpty())
			Expect(grp.Matter["group_cat_id_operate"]).ToNot(BeEmpty())

			// Verify fabric_id is derived from group_id
			fabricID := grp.Matter["fabric_id"].(string)
			expectedFabricID := group.FabricIDFromGroupID(groupID)
			Expect(fabricID).To(Equal(expectedFabricID))
		})

		It("should return correct access_type for shared groups", func() {
			// Create a group owned by userID
			groupID := CreateGroup(ctx, "Shared Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Add nodes to the group
			nodeID1 := "node-1"
			nodeID2 := "node-2"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID1)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID2)
			AddNodeToSubgroup(ctx, groupID, nodeID1, subgroupID, userID)

			// Share the group with secondary access
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `", "access_type": "secondary"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
			ApproveSharingRequest(ctx, otherUserID)

			// List groups for the shared user — should have secondary access_type
			listGroupsResponse := ListGroups(ctx, otherUserID)
			test_utils.AssertNormalizedEqual(listGroupsResponse.Groups, []GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  "Shared Group",
					AccessType: "secondary",
					NodeIDs:    []string{nodeID1, nodeID2},
					NodeDetails: map[string]NodeCapabilityInfo{
						nodeID1: {Capabilities: []string{"rmng"}},
						nodeID2: {Capabilities: []string{"rmng"}},
					},
					Subgroups: []SubGroupInfo{
						{
							SubgroupID:   subgroupID,
							SubgroupName: "Subgroup",
							NodeIDs:      []string{nodeID1},
						},
					},
				},
			})

			// Owner should still see primary
			ownerResponse := ListGroups(ctx, userID)
			test_utils.AssertNormalizedEqual(ownerResponse.Groups, []GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  "Shared Group",
					AccessType: "primary",
					NodeIDs:    []string{nodeID1, nodeID2},
					NodeDetails: map[string]NodeCapabilityInfo{
						nodeID1: {Capabilities: []string{"rmng"}},
						nodeID2: {Capabilities: []string{"rmng"}},
					},
					Subgroups: []SubGroupInfo{
						{
							SubgroupID:   subgroupID,
							SubgroupName: "Subgroup",
							NodeIDs:      []string{nodeID1},
						},
					},
				},
			})

			// Now share the subgroup with a third user
			thirdUserID := "third-user-id"
			thirdUserEmail := "third-user@example.com"
			test_utils.SetupTestUser(ctx, thirdUserID, thirdUserEmail)

			subgroupShareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:       `{"username": "` + thirdUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}
			response, err = handleSharingRequest(ctx, subgroupShareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
			ApproveSharingRequest(ctx, thirdUserID)

			// List groups for the subgroup user — should have subgroup access_type
			thirdUserGroups := ListGroups(ctx, thirdUserID)
			test_utils.AssertNormalizedEqual(thirdUserGroups.Groups, []GroupInfo{
				{
					GroupID:    groupID,
					GroupName:  "Shared Group",
					AccessType: "subgroup",
					NodeIDs:    []string{nodeID1},
					NodeDetails: map[string]NodeCapabilityInfo{
						nodeID1: {Capabilities: []string{"rmng"}},
					},
					Subgroups: []SubGroupInfo{
						{
							SubgroupID:   subgroupID,
							SubgroupName: "Subgroup",
							NodeIDs:      []string{nodeID1},
						},
					},
				},
			})
		})
	})

	Describe("handleAddNodeToSubgroup", func() {
		It("should successfully add a node to a subgroup", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a subgroup
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Create a node
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID, userID)
			test_utils.AssertRowInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				"subgrp1":  &types.AttributeValueMemberS{Value: subgroupID},
			})
		})

		It("should not add a node to a subgroup if it doesn't belong to the parent group", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a subgroup
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Create a node
			nodeID := "test-node-id"

			response, err := _AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID, userID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to add node to subgroup"))
			test_utils.AssertRowNotInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				"subgrp1":  &types.AttributeValueMemberS{Value: subgroupID},
			})
		})

		It("should successfully remove a node from a subgroup", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a subgroup
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Create a node
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			// First add node to subgroup
			AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID, userID)

			// Now remove node from subgroup
			response, err := RemoveNodeFromSubgroup(ctx, groupID, nodeID, subgroupID, userID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify node was removed from subgroup
			test_utils.AssertRowInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
			})

			// Verify subgroup field was removed
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
			dbMock.ForEachRow(group_node_db.GroupDeviceMappingTable, func(item map[string]types.AttributeValue) error {
				if item["group_id"].(*types.AttributeValueMemberS).Value == groupID &&
					item["node_id"].(*types.AttributeValueMemberS).Value == nodeID {
					Expect(item["subgrp1"]).To(BeNil())
				}
				return nil
			})
		})

		It("should return error when removing node from non-existent subgroup", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a node
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			// Try to remove node from non-existent subgroup
			response, err := RemoveNodeFromSubgroup(ctx, groupID, nodeID, "non-existent-subgroup", userID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to remove node from subgroup"))
		})

		It("should return error when removing node that is not in the subgroup", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a subgroup
			CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Create a node
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			// Try to remove node from subgroup it's not in
			response, err := RemoveNodeFromSubgroup(ctx, groupID, nodeID, "non-existent-subgroup", userID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to remove node from subgroup"))
		})

		It("should not allow unauthorized users to remove nodes from subgroups", func() {
			// Create a main group with first user
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create a subgroup
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)

			// Create a node
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
			AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID, userID)

			// Try to remove node using different user
			response, err := RemoveNodeFromSubgroup(ctx, groupID, nodeID, subgroupID, otherUserID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to remove node from subgroup"))

			// Verify node is still in subgroup
			test_utils.AssertRowInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				"subgrp1":  &types.AttributeValueMemberS{Value: subgroupID},
			})
		})
	})

	Describe("handleRemoveNodeFromGroup", func() {
		It("should successfully remove a node from a group with subgroups", func() {
			// Create a main group
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Create subgroups
			subgroupID1 := CreateSubgroup(ctx, groupID, "Subgroup 1", userID)
			subgroupID2 := CreateSubgroup(ctx, groupID, "Subgroup 2", userID)

			// Share group with secondary user
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Create a node and add it to the group and subgroups
			nodeID := "test-node-id"
			test_utils.RegisterIoTThing(nodeID)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
			AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID1, userID)
			AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID2, userID)

			// Remove node from entire group (primary user can still remove since secondary doesn't block it)
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/nodes/{nodeId}",
				Path:       "/v1/groups/" + groupID + "/nodes/" + nodeID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{
					"groupId": groupID,
					"nodeId":  nodeID,
				},
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify node was completely removed from group and all subgroups
			test_utils.AssertNodeNotInGroup(groupID, nodeID)
			test_utils.AssertNodeNotInSubgroup(groupID, nodeID, subgroupID1)
			test_utils.AssertNodeNotInSubgroup(groupID, nodeID, subgroupID2)

			// Verify group shadow is deleted, iparams user tags cleared, group_id attribute cleared
			oldGroups := group_node_db.NodesGroups{Group: groupID, SubGroups: []string{subgroupID1, subgroupID2}}
			test_utils.AssertShadowDeleted(nodeID, oldGroups)
			test_utils.AssertUserTagsCleared(nodeID)
			test_utils.AssertGroupIDAttributeCleared(nodeID)
		})

		It("should return error when removing node from non-existent group", func() {
			nonExistentGroupID := "non-existent-group"
			nodeID := "test-node-id"

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/nodes/{nodeId}",
				Path:       "/v1/groups/" + nonExistentGroupID + "/nodes/" + nodeID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{
					"groupId": nonExistentGroupID,
					"nodeId":  nodeID,
				},
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))
		})

		It("should return error when removing non-existent node from group", func() {
			// Create a group
			groupID := CreateGroup(ctx, "Main Group", userID)
			nonExistentNodeID := "non-existent-node"

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/nodes/{nodeId}",
				Path:       "/v1/groups/" + groupID + "/nodes/" + nonExistentNodeID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{
					"groupId": groupID,
					"nodeId":  nonExistentNodeID,
				},
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to remove node from group"))
		})

		It("should not allow users with secondary access to remove nodes from shared groups", func() {
			// Create a group with first user
			groupID := CreateGroup(ctx, "Main Group", userID)

			// Share group with secondary user
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			// Approve sharing request
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Add a node to the group
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			// Secondary user should NOT be able to remove nodes (only primary access can remove)
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/nodes/{nodeId}",
				Path:       "/v1/groups/" + groupID + "/nodes/" + nodeID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{
					"groupId": groupID,
					"nodeId":  nodeID,
				},
			}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to remove node from group"))

			// Verify node is still in the group
			test_utils.AssertNodeInGroup(groupID, nodeID)
		})
	})

	Describe("Security tests", func() {
		var groupID string

		BeforeEach(func() {
			// Create a group owned by userID
			groupID = CreateGroup(ctx, groupName, userID)
		})

		It("should not allow creating a subgroup in another user's group", func() {
			subgroupRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups",
				Path:       "/v1/groups/" + groupID + "/subgroups",
				Body:       `{"subgroup_name": "Unauthorized Subgroup"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handlePostGroup(ctx, subgroupRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("{\"message\":\"Failed to create subgroup\"}"))

			// The same request should work with the owner
			subgroupRequest.RequestContext.Identity.CognitoIdentityID = userID
			subgroupRequest.RequestContext.Identity.CognitoAuthenticationProvider = "https://issuer.example:" + userID
			response, err = handlePostGroup(ctx, subgroupRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
		})

		It("should not allow listing groups of another user", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Resource:   "/v1/groups",
				Path:       "/v1/groups",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
			}
			response, err := handleListGroups(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listGroupsResponse ListGroupsResponse
			err = json.Unmarshal([]byte(response.Body), &listGroupsResponse)
			Expect(err).To(BeNil())
			Expect(listGroupsResponse.Groups).To(BeEmpty())

			// The same request should work with the owner
			request.RequestContext.Identity.CognitoIdentityID = userID
			request.RequestContext.Identity.CognitoAuthenticationProvider = "https://issuer.example:" + userID
			response, err = handleListGroups(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should not allow adding nodes to a subgroup in another user's group", func() {
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup", userID)
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			addNodeRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PUT",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/nodes/" + nodeID,
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}",
				Body:       `{}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": subgroupID,
					"nodeId":     nodeID,
				},
			}

			response, err := handleRequest(ctx, addNodeRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("{\"message\":\"Failed to add node to subgroup\"}"))

			// Verify that the node was not added to the subgroup
			test_utils.AssertRowNotInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
				"group_id": &types.AttributeValueMemberS{Value: groupID},
				"node_id":  &types.AttributeValueMemberS{Value: nodeID},
				"subgrp1":  &types.AttributeValueMemberS{Value: subgroupID},
			})

			// The same request should work with the owner
			addNodeRequest.RequestContext.Identity.CognitoIdentityID = userID
			addNodeRequest.RequestContext.Identity.CognitoAuthenticationProvider = "https://issuer.example:" + userID
			response, err = handleRequest(ctx, addNodeRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should not allow removing nodes from another user's group", func() {
			nodeID := "test-node-id"
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			removeNodeRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/nodes/{nodeId}",
				Path:       "/v1/groups/" + groupID + "/nodes/" + nodeID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{
					"groupId": groupID,
					"nodeId":  nodeID,
				},
			}

			response, err := handleRequest(ctx, removeNodeRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))

			// Verify that the node was not removed from the group
			test_utils.AssertNodeInGroup(groupID, nodeID)

			// The same request should work with the owner
			removeNodeRequest.RequestContext.Identity.CognitoIdentityID = userID
			removeNodeRequest.RequestContext.Identity.CognitoAuthenticationProvider = "https://issuer.example:" + userID
			response, err = handleRequest(ctx, removeNodeRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify that the node was removed by the owner
			test_utils.AssertNodeNotInGroup(groupID, nodeID)
		})
	})

	Describe("handleDeleteGroup", func() {
		var rmng_context *rmngctx.RmngContext

		BeforeEach(func() {
			user := user.NewUser(userID)
			rmng_context = rmngctx.NewRmngContext(user)
		})

		It("rejects deletion with 409 while the group is non-empty, then succeeds once empty", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subGroupID := CreateSubgroup(ctx, groupID, "Subgroup 1", userID)

			nodeID := "node1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			test_utils.RegisterIoTThing(nodeID)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)

			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + groupID,
				PathParameters: map[string]string{
					"groupId": groupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			// Non-empty (node + subgroup) -> 409.
			response, err := handleDeleteGroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusConflict))
			Expect(response.Body).To(ContainSubstring("group not empty"))

			// Empty the group: remove the node and delete the subgroup.
			_, err = group.RemoveNode(rmng_context, groupID, nodeID)
			Expect(err).To(BeNil())
			Expect(group.DeleteSubGroup(rmng_context, groupID, subGroupID)).To(BeNil())

			// Now empty -> 200.
			response, err = handleDeleteGroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			groupDB := group_db.NewGroupDB(rmng_context)
			_, err = groupDB.GetGroupByID(groupID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("group not found"))

			userGroupDB := user_group_db.NewUserGroupDB(rmng_context)
			groups, err := userGroupDB.ListGroupsForUser("")
			Expect(err).To(BeNil())
			Expect(groups).To(BeEmpty())
		})

		It("should return an error when trying to delete a non-existent group", func() {
			nonExistentGroupID := "non-existent-group-id"

			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + nonExistentGroupID,
				PathParameters: map[string]string{
					"groupId": nonExistentGroupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			response, err := handleDeleteGroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))
		})

		It("should not allow deleting a group owned by another user", func() {
			// Create a group owned by userID
			groupID := CreateGroup(ctx, "Test Group", userID)

			// Attempt to delete the group as otherUserID
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + groupID,
				PathParameters: map[string]string{
					"groupId": groupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
			}

			response, err := handleDeleteGroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))

			// Verify that the group still exists
			groupDB := group_db.NewGroupDB(rmng_context)
			_, err = groupDB.GetGroupByID(groupID)
			Expect(err).To(BeNil())
		})
	})

	Describe("handleDeleteSubgroup", func() {
		var rmng_context *rmngctx.RmngContext

		BeforeEach(func() {
			u := user.NewUser(userID)
			rmng_context = rmngctx.NewRmngContext(u)
		})

		It("should successfully delete a subgroup and verify it is removed from DB", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup To Delete", userID)

			// Verify it exists before deletion
			rmng_context.SetAllow(utils.GroupListSubEntities, groupID)
			isSubGroup, err := group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeTrue())

			// Delete the subgroup
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": subgroupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("Subgroup deleted successfully"))

			// Verify subgroup no longer exists in DB
			isSubGroup, err = group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeFalse())
		})

		It("returns 409 when the subgroup still has a node", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Non-empty Subgroup", userID)

			nodeID := "node1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			test_utils.RegisterIoTThing(nodeID)
			test_utils.ManuallyAddNodeToGroup(ctx, groupID, nodeID)
			_, err := group.UpdateNodeAndSubgroup(rmng_context, groupID, nodeID, subgroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": subgroupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusConflict))
			Expect(response.Body).To(ContainSubstring("subgroup not empty"))

			rmng_context.SetAllow(utils.GroupListSubEntities, groupID)
			isSubGroup, err := group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeTrue())
		})

		It("should return 404 when trying to delete a non-existent subgroup", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)

			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/non-existent-subgroup",
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": "non-existent-subgroup",
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("Subgroup not found"))
		})

		It("should return an error when trying to delete a subgroup in a non-existent group", func() {
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/non-existent-group/subgroups/some-subgroup",
				PathParameters: map[string]string{
					"groupId":    "non-existent-group",
					"subGroupId": "some-subgroup",
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))
		})

		It("should not allow deleting a subgroup by an unauthorized user", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Protected Subgroup", userID)

			// Attempt to delete as otherUserID (who doesn't own the group)
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": subgroupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
			}

			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group not found or not accessible"))

			// Verify the subgroup still exists
			rmng_context.SetAllow(utils.GroupListSubEntities, groupID)
			isSubGroup, err := group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeTrue())
		})

		It("should work correctly via handleRequest router", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Routed Subgroup", userID)

			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{
					"groupId":    groupID,
					"subGroupId": subgroupID,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
			}

			// Use handleRequest to verify routing works
			response, err := handleRequest(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("Subgroup deleted successfully"))
		})

		It("should allow a secondary user to delete a subgroup", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Subgroup To Delete", userID)

			// Share the whole group with secondary access.
			shareReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				Resource:       "/v1/groups/{groupId}/sharing-requests",
				Path:           "/v1/groups/" + groupID + "/sharing-requests",
				Body:           `{"username": "` + otherUserEmail + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: userID, CognitoAuthenticationProvider: "https://issuer.example:" + userID}},
				PathParameters: map[string]string{"groupId": groupID},
			}
			_, err := handleSharingRequest(ctx, shareReq)
			Expect(err).To(BeNil())
			ApproveSharingRequest(ctx, otherUserID)

			// The secondary user deletes the subgroup.
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: otherUserID, CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID}},
			}
			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("Subgroup deleted successfully"))

			// Verify the subgroup is gone.
			rmng_context.SetAllow(utils.GroupListSubEntities, groupID)
			isSubGroup, err := group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeFalse())
		})

		It("should not allow a subentity user to delete their subgroup", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Shared Subgroup", userID)

			// Share only the subgroup (subentity access) with otherUserID.
			shareReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:           `{"username": "` + otherUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: userID, CognitoAuthenticationProvider: "https://issuer.example:" + userID}},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}
			_, err := handleSharingRequest(ctx, shareReq)
			Expect(err).To(BeNil())
			ApproveSharingRequest(ctx, otherUserID)

			// The subentity user lacks GroupDeleteSubGroup and must be denied.
			deleteRequest := events.APIGatewayProxyRequest{
				HTTPMethod:     "DELETE",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: otherUserID, CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID}},
			}
			response, err := handleDeleteSubgroup(ctx, deleteRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Insufficient permissions to delete subgroup"))

			// Verify the subgroup still exists.
			rmng_context.SetAllow(utils.GroupListSubEntities, groupID)
			isSubGroup, err := group.IsSubGroup(rmng_context, groupID, subgroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeTrue())
		})
	})

	Describe("Group sharing (POST sharing-requests, DELETE users)", func() {
		var groupID string

		BeforeEach(func() {
			// Create a group owned by userID
			groupID = CreateGroup(ctx, "Test Group", userID)
		})

		It("should successfully share a group with another user", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var shareResponse CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(response.Body), &shareResponse)).To(Succeed())
			Expect(shareResponse.Message).To(Equal(sharingRequestAcceptedMessage))
			Expect(shareResponse.RequestID).ToNot(BeEmpty())

			ApproveSharingRequest(ctx, otherUserID)

			// Verify that the group was shared
			expectedGroups := map[string]interface{}{
				"groups":    []string{groupID},
				"subgroups": map[string][]string{},
			}
			groups, err := group.ListUserAccessableGroups(rmngctx.NewRmngContext(user.NewUser(otherUserID)))
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(expectedGroups))
		})

		It("should return primary user info in received sharing requests", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `", "access_type": "secondary"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			// Fetch received sharing requests as the other user
			listRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Path:       "/v1/sharing-requests/received",
				Resource:   "/v1/sharing-requests/received",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
			}
			response, err = handleRequest(ctx, listRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp ListSharingRequestsResponse
			err = json.Unmarshal([]byte(response.Body), &listResp)
			Expect(err).To(BeNil())
			Expect(listResp.SharingRequests).To(HaveLen(1))

			req := listResp.SharingRequests[0]
			Expect(req.GroupID).To(Equal(groupID))
			Expect(req.AccessType).To(Equal("secondary"))
			Expect(req.PrimaryUserID).To(Equal(userID))
			Expect(req.PrimaryEmail).To(Equal("test-user@example.com"))

			// Clean up: approve and unshare
			ApproveSharingRequest(ctx, otherUserID)
			err = group.UnshareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID)
			Expect(err).To(BeNil())
		})

		It("should successfully unshare a group", func() {
			// First, share the group
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			sharingRequestID := sharingRequests[0].SharingRequestID
			err = group.ApproveSharingRequest(otherUserContext, sharingRequestID)
			Expect(err).To(BeNil())

			unshareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/users/{userId}",
				Path:       "/v1/groups/" + groupID + "/users/" + otherUserID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "userId": otherUserID},
			}

			response, err := handleDeleteGroupUser(ctx, unshareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify that the group was unshared
			expectedGroups := map[string]interface{}{
				"groups":    []string{},
				"subgroups": map[string][]string{},
			}

			groups, err := group.ListUserAccessableGroups(otherUserContext)
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(expectedGroups))
		})

		It("should successfully unshare a group by user_id", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			shareResp, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(shareResp.StatusCode).To(Equal(http.StatusCreated))
			ApproveSharingRequest(ctx, otherUserID)

			unshareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/users/{userId}",
				Path:       "/v1/groups/" + groupID + "/users/" + otherUserID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "userId": otherUserID},
			}

			response, err := handleDeleteGroupUser(ctx, unshareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			groups, err := group.ListUserAccessableGroups(rmngctx.NewRmngContext(user.NewUser(otherUserID)))
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(map[string]interface{}{
				"groups":    []string{},
				"subgroups": map[string][]string{},
			}))
		})

		// Removal addresses a member by user ID, never by user name. Emails and
		// phone numbers stay out of the URL: paths land in access logs, CDN logs
		// and browser history, and GET .../users already hands callers the IDs.
		DescribeTable("should not unshare a group when the path carries a user name",
			func(userName func() string) {
				_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
				Expect(err).To(BeNil())
				otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
				sharingRequests, err := group.GetMySharingRequests(otherUserContext)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))
				Expect(group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)).To(BeNil())

				segment := userName()
				unshareRequest := events.APIGatewayProxyRequest{
					HTTPMethod: "DELETE",
					Resource:   "/v1/groups/{groupId}/users/{userId}",
					Path:       "/v1/groups/" + groupID + "/users/" + url.PathEscape(segment),
					RequestContext: events.APIGatewayProxyRequestContext{
						Identity: events.APIGatewayRequestIdentity{
							CognitoIdentityID:             userID,
							CognitoAuthenticationProvider: "https://issuer.example:" + userID,
						},
					},
					PathParameters: map[string]string{"groupId": groupID, "userId": segment},
				}

				response, err := handleDeleteGroupUser(ctx, unshareRequest)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusNotFound))

				// The member keeps their access — the name was not resolved.
				groups, err := group.ListUserAccessableGroups(otherUserContext)
				Expect(err).To(BeNil())
				Expect(groups["groups"]).To(ConsistOf(groupID))
			},
			Entry("by email", func() string { return otherUserEmail }),
			Entry("by E.164 phone number", func() string { return otherUserPhone }),
		)

		It("should not allow sharing a group by a non-owner", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + userEmail + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to create sharing request"))
		})

		shareGroupByUserName := func(userName string) events.APIGatewayProxyResponse {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"username": "` + userName + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			return response
		}

		It("should share a group with a user named by E.164 phone number", func() {
			response := shareGroupByUserName(otherUserPhone)
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			ApproveSharingRequest(ctx, otherUserID)
			otherUserGroups := ListGroups(ctx, otherUserID)
			Expect(otherUserGroups.Groups).To(HaveLen(1))
			Expect(otherUserGroups.Groups[0].GroupID).To(Equal(groupID))
		})

		It("should respond generically when sharing a group with an unregistered user name", func() {
			response := shareGroupByUserName("nobody@example.com")
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
			var shareResponse CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(response.Body), &shareResponse)).To(Succeed())
			Expect(shareResponse.Message).To(Equal(sharingRequestAcceptedMessage))
			Expect(shareResponse.RequestID).ToNot(BeEmpty())
			Expect(response.Body).NotTo(ContainSubstring("nobody@example.com"))
		})

		It("should reject accepting a decoy request id exactly like a foreign one", func() {
			realResp := shareGroupByUserName(otherUserPhone)
			var real CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(realResp.Body), &real)).To(Succeed())

			decoyResp := shareGroupByUserName("nobody@example.com")
			var decoy CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(decoyResp.Body), &decoy)).To(Succeed())

			accept := func(requestID string) events.APIGatewayProxyResponse {
				req := events.APIGatewayProxyRequest{
					HTTPMethod:     "POST",
					Resource:       "/v1/sharing-requests/{requestId}/accept",
					Path:           "/v1/sharing-requests/" + requestID + "/accept",
					PathParameters: map[string]string{"requestId": requestID},
					RequestContext: events.APIGatewayProxyRequestContext{
						Identity: events.APIGatewayRequestIdentity{
							CognitoIdentityID:             userID,
							CognitoAuthenticationProvider: "https://issuer.example:" + userID,
						},
					},
				}
				response, err := handleAcceptSharingRequest(ctx, req)
				Expect(err).To(BeNil())
				return response
			}

			realAccept := accept(real.RequestID)
			decoyAccept := accept(decoy.RequestID)
			Expect(decoyAccept.StatusCode).To(Equal(realAccept.StatusCode))
			Expect(decoyAccept.Body).To(Equal(realAccept.Body))
		})

		It("should fail to share a group with a user name that is neither email nor phone", func() {
			response := shareGroupByUserName("INVALID")
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("username must be an email address or an E.164 phone number"))
		})

		It("should share a group with an email address whose local part starts with +", func() {
			// Classification must match signup's, which tests email before phone. A
			// leading-+ check first would send this address to the phone index, leaving
			// the account unshareable even though signup stored it as an email.
			plusEmail := "+plus-user@example.com"
			plusUserID := "plus-user-id"
			test_utils.SetupTestUser(ctx, plusUserID, plusEmail)

			response := shareGroupByUserName(plusEmail)
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
		})

		It("should fail to share a group when the user id of a real user is passed as the user name", func() {
			// Internal user IDs are deliberately not accepted on this endpoint.
			response := shareGroupByUserName(otherUserID)
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("username must be an email address or an E.164 phone number"))
		})

		It("should fail to share a group with a missing user name", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/sharing-requests",
				Body:       `{"access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			// A missing username is caught by the struct's `validate:"required"` tag inside
			// ExtractRequestStruct, so the body is the generic parse/validate failure.
			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("Subgroup sharing (POST sharing-requests, DELETE users)", func() {
		var groupID, subgroupID string

		BeforeEach(func() {
			// Create a group and subgroup owned by userID
			groupID = CreateGroup(ctx, "Test Group", userID)
			subgroupID = CreateSubgroup(ctx, groupID, "Test Subgroup", userID)
		})

		It("should successfully share a subgroup with another user", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			var shareResponse CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(response.Body), &shareResponse)).To(Succeed())
			Expect(shareResponse.Message).To(Equal(sharingRequestAcceptedMessage))
			Expect(shareResponse.RequestID).ToNot(BeEmpty())

			ApproveSharingRequest(ctx, otherUserID)

			// Verify that the subgroup was shared
			expectedGroups := map[string]interface{}{
				"groups": []string{},
				"subgroups": map[string][]string{
					groupID: {subgroupID},
				},
			}

			groups, err := group.ListUserAccessableGroups(rmngctx.NewRmngContext(user.NewUser(otherUserID)))
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(expectedGroups))
		})

		It("should successfully unshare a subgroup", func() {
			// First, share the subgroup
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:       `{"username": "` + otherUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusCreated))

			ApproveSharingRequest(ctx, otherUserID)

			// Verify that the subgroup was shared
			expectedGroups := map[string]interface{}{
				"groups": []string{},
				"subgroups": map[string][]string{
					groupID: {subgroupID},
				},
			}
			groups, err := group.ListUserAccessableGroups(rmngctx.NewRmngContext(user.NewUser(otherUserID)))
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(expectedGroups))

			unshareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/users/{userId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/users/" + otherUserID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID, "userId": otherUserID},
			}

			response, err = handleDeleteSubgroupUser(ctx, unshareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify that the subgroup was unshared
			expectedGroups = map[string]interface{}{
				"groups":    []string{},
				"subgroups": map[string][]string{},
			}

			groups, err = group.ListUserAccessableGroups(rmngctx.NewRmngContext(user.NewUser(otherUserID)))
			Expect(err).To(BeNil())
			Expect(groups).To(Equal(expectedGroups))
		})

		It("should not allow sharing a subgroup by a non-owner", func() {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:       `{"username": "` + userEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to create sharing request"))
		})

		shareSubgroupByUserName := func(userName string) events.APIGatewayProxyResponse {
			shareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:       `{"username": "` + userName + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}
			response, err := handleSharingRequest(ctx, shareRequest)
			Expect(err).To(BeNil())
			return response
		}

		It("should share a subgroup with a user named by E.164 phone number", func() {
			response := shareSubgroupByUserName(otherUserPhone)
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
		})

		It("should respond generically when sharing a subgroup with an unregistered user name", func() {
			response := shareSubgroupByUserName("nobody@example.com")
			Expect(response.StatusCode).To(Equal(http.StatusCreated))
			var shareResponse CreateSharingRequestResponse
			Expect(json.Unmarshal([]byte(response.Body), &shareResponse)).To(Succeed())
			Expect(shareResponse.Message).To(Equal(sharingRequestAcceptedMessage))
			Expect(shareResponse.RequestID).ToNot(BeEmpty())
		})

		It("should fail to share a subgroup with a user name that is neither email nor phone", func() {
			response := shareSubgroupByUserName("INVALID")
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("username must be an email address or an E.164 phone number"))
		})
	})

	Describe("handlePatchGroup", func() {
		It("should successfully update group name", func() {
			groupID := CreateGroup(ctx, "Original Name", userID)

			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PATCH",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + groupID,
				Body:       `{"group_name": "Updated Name"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handlePatchGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify the group name was updated in the database
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Key: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item["group_name"].(*types.AttributeValueMemberS).Value).To(Equal("Updated Name"))
		})

		It("should fail with missing group_name", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)

			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PATCH",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + groupID,
				Body:       `{}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handlePatchGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should fail with invalid request body", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)

			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PATCH",
				Resource:   "/v1/groups/{groupId}",
				Path:       "/v1/groups/" + groupID,
				Body:       "invalid-json",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handlePatchGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should allow shared user to rename group", func() {
			groupID := CreateGroup(ctx, "Original Group", userID)

			// Share the group with otherUserID
			var err error

			shareReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				Resource:       "/v1/groups/{groupId}/sharing-requests",
				Path:           "/v1/groups/" + groupID + "/sharing-requests",
				Body:           `{"username": "` + otherUserEmail + `", "access_type": "` + string(utils.GroupSecondaryAccess) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: userID, CognitoAuthenticationProvider: "https://issuer.example:" + userID}},
				PathParameters: map[string]string{"groupId": groupID},
			}
			_, err = handleSharingRequest(ctx, shareReq)
			Expect(err).To(BeNil())
			ApproveSharingRequest(ctx, otherUserID)

			// Shared user renames the group
			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod:     "PATCH",
				Resource:       "/v1/groups/{groupId}",
				Path:           "/v1/groups/" + groupID,
				Body:           `{"group_name": "Renamed by Shared User"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: otherUserID, CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID}},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handlePatchGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("handlePatchSubgroup", func() {
		It("should successfully update subgroup name", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Original Subgroup", userID)

			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PATCH",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				Body:       `{"subgroup_name": "Updated Subgroup"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handlePatchSubGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring("success"))

			// Verify the subgroup name was updated in the database
			getItemInput := &dynamodb.GetItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Key: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: subgroupID},
				},
			}
			result, err := dbMock.GetItem(ctx, getItemInput)
			Expect(err).To(BeNil())
			Expect(result.Item["group_name"].(*types.AttributeValueMemberS).Value).To(Equal("Updated Subgroup"))
		})

		It("should fail with missing subgroup_name", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Test Subgroup", userID)

			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "PATCH",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				Body:       `{}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handlePatchSubGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should allow shared user to rename subgroup", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Original Subgroup", userID)

			// Share the subgroup with otherUserID
			var err error

			shareReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/sharing-requests",
				Body:           `{"username": "` + otherUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: userID, CognitoAuthenticationProvider: "https://issuer.example:" + userID}},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}
			_, err = handleSharingRequest(ctx, shareReq)
			Expect(err).To(BeNil())
			ApproveSharingRequest(ctx, otherUserID)

			// Shared user renames the subgroup
			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod:     "PATCH",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + subgroupID,
				Body:           `{"subgroup_name": "Renamed by Shared User"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: otherUserID, CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID}},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": subgroupID},
			}

			response, err := handlePatchSubGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should not allow a subgroup member to rename a sibling subgroup", func() {
			groupID := CreateGroup(ctx, "Test Group", userID)
			sharedSubgroupID := CreateSubgroup(ctx, groupID, "Shared Subgroup", userID)
			siblingSubgroupID := CreateSubgroup(ctx, groupID, "Sibling Subgroup", userID)

			// Share only sharedSubgroupID with otherUserID (subentity access).
			shareReq := events.APIGatewayProxyRequest{
				HTTPMethod:     "POST",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}/sharing-requests",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + sharedSubgroupID + "/sharing-requests",
				Body:           `{"username": "` + otherUserEmail + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: userID, CognitoAuthenticationProvider: "https://issuer.example:" + userID}},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": sharedSubgroupID},
			}
			_, err := handleSharingRequest(ctx, shareReq)
			Expect(err).To(BeNil())
			ApproveSharingRequest(ctx, otherUserID)

			// The member must not be able to rename the sibling subgroup they
			// were never granted access to.
			patchRequest := events.APIGatewayProxyRequest{
				HTTPMethod:     "PATCH",
				Resource:       "/v1/groups/{groupId}/subgroups/{subGroupId}",
				Path:           "/v1/groups/" + groupID + "/subgroups/" + siblingSubgroupID,
				Body:           `{"subgroup_name": "Hijacked Sibling Name"}`,
				RequestContext: events.APIGatewayProxyRequestContext{Identity: events.APIGatewayRequestIdentity{CognitoIdentityID: otherUserID, CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID}},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": siblingSubgroupID},
			}

			response, err := handlePatchSubGroup(ctx, patchRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Subgroup not found or not accessible"))
		})
	})

	Describe("handleMatterNOC", func() {
		It("should generate NOC for user in Matter-enabled group", func() {
			// Create group with matter capability
			groupID := CreateGroupWithMatter(ctx, "Matter NOC Group", userID)

			// Generate a CSR for testing
			csrPEM := GenerateTestCSR()

			// Request NOC
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/group/" + groupID + "/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(csrPEM) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Parse response
			var nocResponse MatterNOCResponse
			err = json.Unmarshal([]byte(response.Body), &nocResponse)
			Expect(err).To(BeNil())

			// Verify NOC is a valid PEM certificate
			Expect(nocResponse.NOC).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
			Expect(nocResponse.NOC).To(ContainSubstring("-----END CERTIFICATE-----"))

			// matter_node_id is canonical
			Expect(nocResponse.MatterNodeID).ToNot(BeEmpty())
			Expect(len(nocResponse.MatterNodeID)).To(Equal(16)) // 16 hex chars = 8 bytes

			cert, err := group.ParseCertificatePEM(nocResponse.NOC)
			Expect(err).To(BeNil())
			var certificateNodeID string
			for _, attr := range cert.Subject.Names {
				if attr.Type.Equal(group.MatterNodeIDOID) {
					certificateNodeID, _ = attr.Value.(string)
				}
			}
			Expect(certificateNodeID).To(Equal(nocResponse.MatterNodeID))

			// Retrying with the same operational key is stable.
			retryResponse, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			var retryBody MatterNOCResponse
			Expect(json.Unmarshal([]byte(retryResponse.Body), &retryBody)).To(Succeed())
			Expect(retryBody.MatterNodeID).To(Equal(nocResponse.MatterNodeID))

			// A second phone's independent operational key gets a distinct identity.
			nocRequest.Body = `{"csr": "` + escapeJSON(GenerateTestCSR()) + `"}`
			secondPhoneResponse, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			var secondPhoneBody MatterNOCResponse
			Expect(json.Unmarshal([]byte(secondPhoneResponse.Body), &secondPhoneBody)).To(Succeed())
			Expect(secondPhoneBody.MatterNodeID).NotTo(Equal(nocResponse.MatterNodeID))
		})

		It("should fail for group without Matter capability", func() {
			// Create a regular group without matter capability
			groupID := CreateGroup(ctx, "Regular Group", userID)

			// Generate a CSR for testing
			csrPEM := GenerateTestCSR()

			// Request NOC
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/group/" + groupID + "/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(csrPEM) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Group does not have Matter capability"))
		})

		It("should fail with missing CSR", func() {
			// Create group with matter capability
			groupID := CreateGroupWithMatter(ctx, "Matter NOC Group", userID)

			// Request NOC without CSR
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/group/" + groupID + "/matter-nocs",
				Body:       `{}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing CSR"))
		})

		It("should fail with invalid CSR", func() {
			// Create group with matter capability
			groupID := CreateGroupWithMatter(ctx, "Matter NOC Group", userID)

			// Request NOC with invalid CSR
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/group/" + groupID + "/matter-nocs",
				Body:       `{"csr": "not a valid csr"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to generate NOC"))
		})

		It("should route correctly via handleRequest", func() {
			// Create group with matter capability
			groupID := CreateGroupWithMatter(ctx, "Matter NOC Group", userID)

			// Generate a CSR for testing
			csrPEM := GenerateTestCSR()

			// Request NOC via handleRequest
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Path:       "/v1/groups/" + groupID + "/matter-nocs",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(csrPEM) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleRequest(ctx, nocRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Parse response
			var nocResponse MatterNOCResponse
			err = json.Unmarshal([]byte(response.Body), &nocResponse)
			Expect(err).To(BeNil())
			Expect(nocResponse.NOC).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
		})

		It("should fail when the group is not owned by the current user", func() {
			// Create group with matter capability owned by userID
			groupID := CreateGroupWithMatter(ctx, "Matter NOC Group", userID)

			// Generate a CSR for testing
			csrPEM := GenerateTestCSR()

			// Request NOC as otherUserID (who doesn't have access to the group)
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/group/" + groupID + "/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(csrPEM) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}

			response, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("Group not found"))
		})
	})

	Describe("List Group Users (GET /v1/groups/{groupId}/users)", func() {
		var groupID string

		BeforeEach(func() {
			groupID = CreateGroup(ctx, "Test Group", userID)
		})

		It("should list only the owner for a newly created group", func() {
			response := ListGroupUsers(ctx, groupID, userID)
			Expect(response.Users).To(ConsistOf(GroupUserInfoResponse{
				UserID:     userID,
				Email:      "test-user@example.com",
				AccessType: string(utils.GroupPrimaryAccess),
			}))
		})

		It("should list both users after sharing", func() {
			// Share group with other user
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			response := ListGroupUsers(ctx, groupID, userID)
			Expect(response.Users).To(ConsistOf(
				GroupUserInfoResponse{
					UserID:     userID,
					Email:      "test-user@example.com",
					AccessType: string(utils.GroupPrimaryAccess),
				},
				GroupUserInfoResponse{
					UserID:     otherUserID,
					Email:      "other-user@example.com",
					Phone:      otherUserPhone,
					AccessType: string(utils.GroupSecondaryAccess),
				},
			))
		})

		It("should let a secondary user list group users but disclose only primary owners", func() {
			// Two secondary members: the caller and one other secondary.
			thirdUserID := "third-user-id"
			test_utils.SetupTestUser(ctx, thirdUserID, "third-user@example.com")
			for _, u := range []string{otherUserID, thirdUserID} {
				_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, u, utils.GroupSecondaryAccess, auth.UserInfo{})
				Expect(err).To(BeNil())
				uCtx := rmngctx.NewRmngContext(user.NewUser(u))
				sharingRequests, err := group.GetMySharingRequests(uCtx)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))
				Expect(group.ApproveSharingRequest(uCtx, sharingRequests[0].SharingRequestID)).To(Succeed())
			}

			// Secondary users lack GroupListUsers but have GroupListPrimaryUsers, so the
			// listing succeeds yet is narrowed to primary owners — neither the caller nor
			// the other secondary is disclosed.
			response := ListGroupUsers(ctx, groupID, otherUserID)
			Expect(response.Users).To(ConsistOf(GroupUserInfoResponse{
				UserID:     userID,
				Email:      "test-user@example.com",
				AccessType: string(utils.GroupPrimaryAccess),
			}))
		})

		It("should show sub_entity_ids for subgroup-only users", func() {
			// Create a subgroup and share it with other user
			subgroupID := CreateSubgroup(ctx, groupID, "Test Subgroup", userID)
			_, err := group.ShareSubGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, subgroupID, otherUserID, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			err = group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			response := ListGroupUsers(ctx, groupID, userID)
			Expect(response.Users).To(ConsistOf(
				GroupUserInfoResponse{
					UserID:     userID,
					Email:      "test-user@example.com",
					AccessType: string(utils.GroupPrimaryAccess),
				},
				GroupUserInfoResponse{
					UserID:     otherUserID,
					Email:      "other-user@example.com",
					Phone:      otherUserPhone,
					AccessType: "subgroup",
					Subgroups:  []string{subgroupID},
				},
			))

			// Validate that the 'other user' cannot list users for the group
			_ = ListGroupUsersWithCode(ctx, groupID, otherUserID, http.StatusInternalServerError)
		})

		It("should return error for a user without access", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Resource:   "/v1/groups/{groupId}/users",
				Path:       "/v1/groups/" + groupID + "/users",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			response, err := handleListGroupUsers(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("should return error for missing group ID", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Resource:   "/v1/groups/{groupId}/users",
				Path:       "/v1/groups//users",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": ""},
			}
			response, err := handleListGroupUsers(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should support the full unshare flow: list users then delete", func() {
			// Register users so they exist in the users table (required by handleDeleteGroupUser)
			ownerUser := user.NewUser(userID)
			err := ownerUser.RegisterClient(rmngctx.NewRmngContext(ownerUser), user_integration_db.UserIntegrationEntry{IntegrationID: "ios", EndpointID: "user-device-token", SNSEndpointARN: "user-device-token"})
			Expect(err).To(BeNil())
			otherUser := user.NewUser(otherUserID)
			err = otherUser.RegisterClient(rmngctx.NewRmngContext(otherUser), user_integration_db.UserIntegrationEntry{IntegrationID: "ios", EndpointID: "other-device-token", SNSEndpointARN: "other-device-token"})
			Expect(err).To(BeNil())

			// Share group
			_, err = group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			err = group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)
			Expect(err).To(BeNil())

			// Step 1: List users
			listResponse := ListGroupUsers(ctx, groupID, userID)
			Expect(listResponse.Users).To(HaveLen(2))

			// Step 2: Find the other user and unshare
			var targetUserID string
			for _, u := range listResponse.Users {
				if u.UserID != userID {
					targetUserID = u.UserID
				}
			}
			Expect(targetUserID).To(Equal(otherUserID))

			// Step 3: Delete (unshare)
			unshareRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				Resource:   "/v1/groups/{groupId}/users/{userId}",
				Path:       "/v1/groups/" + groupID + "/users/" + targetUserID,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "userId": targetUserID},
			}
			deleteResponse, err := handleDeleteGroupUser(ctx, unshareRequest)
			Expect(err).To(BeNil())
			Expect(deleteResponse.StatusCode).To(Equal(http.StatusOK))

			// Step 4: Verify only owner remains
			listResponse = ListGroupUsers(ctx, groupID, userID)
			Expect(listResponse.Users).To(HaveLen(1))
			Expect(listResponse.Users[0].UserID).To(Equal(userID))
		})
	})

	Describe("List Subgroup Users (GET /v1/groups/{groupId}/subgroups/{subGroupId}/users)", func() {
		var (
			groupID    string
			subgroupID string
		)

		BeforeEach(func() {
			groupID = CreateGroup(ctx, "Test Group", userID)
			subgroupID = CreateSubgroup(ctx, groupID, "Test Subgroup", userID)
		})

		It("should list only the owner when the subgroup has no other members", func() {
			response := ListSubgroupUsers(ctx, groupID, subgroupID, userID)
			Expect(response.Users).To(ConsistOf(GroupUserInfoResponse{
				UserID:     userID,
				Email:      "test-user@example.com",
				AccessType: string(utils.GroupPrimaryAccess),
			}))
		})

		It("should show the full member listing when the caller has primary access", func() {
			ShareSubgroupAndApprove(ctx, groupID, subgroupID, userID, otherUserID)

			response := ListSubgroupUsers(ctx, groupID, subgroupID, userID)
			Expect(response.Users).To(ConsistOf(
				GroupUserInfoResponse{
					UserID:     userID,
					Email:      "test-user@example.com",
					AccessType: string(utils.GroupPrimaryAccess),
				},
				GroupUserInfoResponse{
					UserID:     otherUserID,
					Email:      "other-user@example.com",
					Phone:      otherUserPhone,
					AccessType: "subgroup",
					Subgroups:  []string{subgroupID},
				},
			))
		})

		It("should show only primary owners when the caller has subgroup-only access", func() {
			ShareSubgroupAndApprove(ctx, groupID, subgroupID, userID, otherUserID)

			response := ListSubgroupUsers(ctx, groupID, subgroupID, otherUserID)
			Expect(response.Users).To(ConsistOf(GroupUserInfoResponse{
				UserID:     userID,
				Email:      "test-user@example.com",
				AccessType: string(utils.GroupPrimaryAccess),
			}))
		})

		It("should show only primary owners when the caller has group-level secondary access", func() {
			_, err := group.ShareGroup(rmngctx.NewRmngContext(user.NewUser(userID)), groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherUserContext := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherUserContext)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			Expect(group.ApproveSharingRequest(otherUserContext, sharingRequests[0].SharingRequestID)).To(Succeed())

			response := ListSubgroupUsers(ctx, groupID, subgroupID, otherUserID)
			Expect(response.Users).To(ConsistOf(GroupUserInfoResponse{
				UserID:     userID,
				Email:      "test-user@example.com",
				AccessType: string(utils.GroupPrimaryAccess),
			}))
		})

		It("should return error for missing group ID", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/users",
				Path:       "/v1/groups//subgroups/" + subgroupID + "/users",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": "", "subGroupId": subgroupID},
			}
			response, err := handleListSubgroupUsers(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return error for missing subgroup ID", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/users",
				Path:       "/v1/groups/" + groupID + "/subgroups//users",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: "https://issuer.example:" + userID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID, "subGroupId": ""},
			}
			response, err := handleListSubgroupUsers(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return error for a user without access to the group", func() {
			_ = ListSubgroupUsersWithCode(ctx, groupID, subgroupID, otherUserID, http.StatusInternalServerError)
		})

		It("should deny a subgroup-only member from listing users of a sibling subgroup", func() {
			// otherUser gets subentity access to subgroupID only.
			ShareSubgroupAndApprove(ctx, groupID, subgroupID, userID, otherUserID)

			// A sibling subgroup under the same parent group that otherUser has no access to.
			siblingSubgroupID := CreateSubgroup(ctx, groupID, "Sibling Subgroup", userID)

			// The caller has no access to the sibling subgroup at all.
			_ = ListSubgroupUsersWithCode(ctx, groupID, siblingSubgroupID, otherUserID, http.StatusInternalServerError)
		})
	})

	Describe("handleAddGroupCapabilities", func() {
		It("should convert a plain group into a Matter fabric", func() {
			groupID := CreateGroup(ctx, "Convertible Group", userID)

			response := EnableGroupCapabilities(ctx, groupID, userID, []string{"matter"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var body AddGroupCapabilitiesResponse
			Expect(json.Unmarshal([]byte(response.Body), &body)).To(Succeed())
			Expect(body.Matter["fabric_id"]).ToNot(BeEmpty())
			Expect(body.Matter["root_ca"]).ToNot(BeEmpty())
			Expect(body.Matter["ipk"]).ToNot(BeEmpty())
			Expect(body.Matter["group_cat_id_admin"]).ToNot(BeEmpty())
			Expect(body.Matter["group_cat_id_operate"]).ToNot(BeEmpty())

			// Verify the capability was persisted to the group row.
			result, err := dbMock.GetItem(ctx, &dynamodb.GetItemInput{
				TableName: aws.String(group_db.GroupsTable),
				Key: map[string]types.AttributeValue{
					"group_id":     &types.AttributeValueMemberS{Value: groupID},
					"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
				},
			})
			Expect(err).To(BeNil())
			capList, ok := result.Item["capabilities"].(*types.AttributeValueMemberL)
			Expect(ok).To(BeTrue(), "capabilities list should be present")
			Expect(capList.Value).To(HaveLen(1))
			Expect(capList.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("matter"))
			_, hasCapMatter := result.Item["cap_matter"].(*types.AttributeValueMemberS)
			Expect(hasCapMatter).To(BeTrue(), "cap_matter column should be present")
		})

		It("should convert a group that has a shared subgroup", func() {
			groupID := CreateGroup(ctx, "Group With Shared Subgroup", userID)
			subgroupID := CreateSubgroup(ctx, groupID, "Shared Subgroup", userID)

			ownerCtx := rmngctx.NewRmngContext(user.NewUser(userID))
			_, err := group.ShareSubGroup(ownerCtx, groupID, subgroupID, otherUserID, auth.UserInfo{})
			Expect(err).To(BeNil())

			otherCtx := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			Expect(group.ApproveSharingRequest(otherCtx, sharingRequests[0].SharingRequestID)).To(Succeed())

			response := EnableGroupCapabilities(ctx, groupID, userID, []string{"matter"})
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// The subgroup-shared (subentity) user has no fabric-wide access, so their NOC request fails.
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/v1/groups/" + groupID + "/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(GenerateTestCSR()) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			nocResponse, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(nocResponse.StatusCode).ToNot(Equal(http.StatusOK))
		})

		It("should reject enabling a capability the group already has", func() {
			groupID := CreateGroupWithMatter(ctx, "Already Fabric", userID)

			response := EnableGroupCapabilities(ctx, groupID, userID, []string{"matter"})
			Expect(response.StatusCode).To(Equal(http.StatusConflict))
			Expect(response.Body).To(ContainSubstring("already has the requested capability"))
		})

		It("should reject an unknown capability", func() {
			groupID := CreateGroup(ctx, "Bad Capability Group", userID)

			response := EnableGroupCapabilities(ctx, groupID, userID, []string{"not-a-real-capability"})
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to update group capabilities"))
		})

		It("should reject an empty capabilities array", func() {
			groupID := CreateGroup(ctx, "Empty Capabilities Group", userID)

			response := EnableGroupCapabilities(ctx, groupID, userID, []string{})
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing capabilities"))
		})

		It("should not let a non-owner convert the group", func() {
			groupID := CreateGroup(ctx, "Owned Group", userID)

			ownerCtx := rmngctx.NewRmngContext(user.NewUser(userID))
			_, err := group.ShareGroup(ownerCtx, groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())

			otherCtx := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherCtx)
			Expect(err).To(BeNil())
			Expect(sharingRequests).To(HaveLen(1))
			Expect(group.ApproveSharingRequest(otherCtx, sharingRequests[0].SharingRequestID)).To(Succeed())

			// A secondary user lacks group:updatecapabilities, so the DB authorization
			// check rejects the write and the handler surfaces it as a 500.
			response := EnableGroupCapabilities(ctx, groupID, otherUserID, []string{"matter"})
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("should let a pre-existing member fetch a NOC after conversion", func() {
			groupID := CreateGroup(ctx, "Group To Convert", userID)

			// otherUser joins as a secondary member before Matter is enabled.
			ownerCtx := rmngctx.NewRmngContext(user.NewUser(userID))
			_, err := group.ShareGroup(ownerCtx, groupID, otherUserID, utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(BeNil())
			otherCtx := rmngctx.NewRmngContext(user.NewUser(otherUserID))
			sharingRequests, err := group.GetMySharingRequests(otherCtx)
			Expect(err).To(BeNil())
			Expect(group.ApproveSharingRequest(otherCtx, sharingRequests[0].SharingRequestID)).To(Succeed())

			// Convert to fabric.
			Expect(EnableGroupCapabilities(ctx, groupID, userID, []string{"matter"}).StatusCode).To(Equal(http.StatusOK))

			// No per-user Matter state is needed for the pre-existing member to obtain a NOC.
			nocRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Resource:   "/v1/groups/{groupId}/matter-nocs",
				Path:       "/v1/groups/" + groupID + "/matter-nocs",
				Body:       `{"csr": "` + escapeJSON(GenerateTestCSR()) + `"}`,
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             otherUserID,
						CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
					},
				},
				PathParameters: map[string]string{"groupId": groupID},
			}
			nocResponse, err := handleMatterNOC(ctx, nocRequest, groupID)
			Expect(err).To(BeNil())
			Expect(nocResponse.StatusCode).To(Equal(http.StatusOK))

			var nocBody MatterNOCResponse
			Expect(json.Unmarshal([]byte(nocResponse.Body), &nocBody)).To(Succeed())
			Expect(nocBody.MatterNodeID).ToNot(BeEmpty())
			Expect(nocBody.NOC).To(ContainSubstring("-----BEGIN CERTIFICATE-----"))
		})

		It("should not overwrite other capabilities when updating one", func() {
			groupID := CreateGroupWithMatter(ctx, "Multi-cap Group", userID)
			ownerCtx := rmngctx.NewRmngContext(user.NewUser(userID))
			_, err := group.GetUserGroupAccess(ownerCtx, groupID) // populate owner permissions
			Expect(err).To(BeNil())
			groupDB := group_db.NewGroupDB(ownerCtx)

			// Enable a second capability alongside matter.
			Expect(groupDB.AddCapability(groupID, "other", map[string]interface{}{"k": "v1"})).To(Succeed())

			// Updating matter's data must leave the other capability and the list untouched.
			Expect(groupDB.UpdateCapabilityData(groupID, group.MatterCapabilityName, map[string]interface{}{"fabric_id": "ZZZ"})).To(Succeed())

			g, err := groupDB.GetGroupByID(groupID)
			Expect(err).To(BeNil())
			Expect(g.Capabilities).To(ConsistOf("matter", "other"))
			Expect(g.CapabilityData["other"]).To(HaveKeyWithValue("k", "v1"))
			Expect(g.CapabilityData["matter"]).To(HaveKeyWithValue("fabric_id", "ZZZ"))
		})

	})
})

// EnableGroupCapabilities enables the given capabilities on a group via handleRequest
// (POST /v1/groups/{groupId}/capabilities) and returns the raw response to assert on.
func EnableGroupCapabilities(ctx context.Context, groupID, userID string, capabilities []string) events.APIGatewayProxyResponse {
	body, err := json.Marshal(AddGroupCapabilitiesRequest{Capabilities: capabilities})
	Expect(err).To(BeNil())
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Resource:   "/v1/groups/{groupId}/capabilities",
		Path:       "/v1/groups/" + groupID + "/capabilities",
		Body:       string(body),
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{"groupId": groupID},
	}
	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	return response
}

// CreateGroup creates a group using handlePostGroup and returns the group ID
func CreateGroup(ctx context.Context, groupName, userID string) string {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Resource:   "/v1/groups",
		Body:       `{"group_name": "` + groupName + `"}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
	}
	createResponse, err := handlePostGroup(ctx, request)
	Expect(err).To(BeNil())
	Expect(createResponse.StatusCode).To(Equal(http.StatusCreated))

	var createResponseBody CreateGroupResponse
	err = json.Unmarshal([]byte(createResponse.Body), &createResponseBody)
	Expect(err).To(BeNil())
	groupID := createResponseBody.GroupID
	return groupID
}

func CreateSubgroup(ctx context.Context, groupID, subgroupName, userID string) string {
	subgroupRequest := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Resource:   "/v1/groups/{groupId}/subgroups",
		Path:       "/v1/groups/" + groupID + "/subgroups",
		Body:       `{"subgroup_name": "` + subgroupName + `"}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{"groupId": groupID},
	}

	subgroupResponse, err := handlePostGroup(ctx, subgroupRequest)
	Expect(err).To(BeNil())
	Expect(subgroupResponse.StatusCode).To(Equal(http.StatusCreated))

	var subgroupResponseBody CreateSubgroupResponse
	err = json.Unmarshal([]byte(subgroupResponse.Body), &subgroupResponseBody)
	Expect(err).To(BeNil())
	Expect(subgroupResponseBody.SubgroupID).ToNot(BeEmpty())
	return subgroupResponseBody.SubgroupID
}

func ListGroups(ctx context.Context, userID string) ListGroupsResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Resource:   "/v1/groups",
		Path:       "/v1/groups",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
	}
	response, err := handleListGroups(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	var listGroupsResponse ListGroupsResponse
	err = json.Unmarshal([]byte(response.Body), &listGroupsResponse)
	Expect(err).To(BeNil())
	return listGroupsResponse
}

func ListGroupUsers(ctx context.Context, groupID, userID string) ListGroupUsersResponse {
	return ListGroupUsersWithCode(ctx, groupID, userID, http.StatusOK)
}

func ListGroupUsersWithCode(ctx context.Context, groupID, userID string, expectedStatusCode int) ListGroupUsersResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Resource:   "/v1/groups/{groupId}/users",
		Path:       "/v1/groups/" + groupID + "/users",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{"groupId": groupID},
	}
	response, err := handleListGroupUsers(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(expectedStatusCode))

	if expectedStatusCode != http.StatusOK {
		return ListGroupUsersResponse{}
	}

	var listGroupUsersResponse ListGroupUsersResponse
	err = json.Unmarshal([]byte(response.Body), &listGroupUsersResponse)
	Expect(err).To(BeNil())
	return listGroupUsersResponse
}

func ListSubgroupUsers(ctx context.Context, groupID, subGroupID, userID string) ListGroupUsersResponse {
	return ListSubgroupUsersWithCode(ctx, groupID, subGroupID, userID, http.StatusOK)
}

func ListSubgroupUsersWithCode(ctx context.Context, groupID, subGroupID, userID string, expectedStatusCode int) ListGroupUsersResponse {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/users",
		Path:       "/v1/groups/" + groupID + "/subgroups/" + subGroupID + "/users",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{"groupId": groupID, "subGroupId": subGroupID},
	}

	response, err := handleListSubgroupUsers(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(expectedStatusCode))

	if expectedStatusCode != http.StatusOK {
		return ListGroupUsersResponse{}
	}

	var listResponse ListGroupUsersResponse
	err = json.Unmarshal([]byte(response.Body), &listResponse)
	Expect(err).To(BeNil())
	return listResponse
}

// ShareSubgroupAndApprove shares subgroupID (under groupID, owned by ownerUserID) with
// targetUserID and approves the resulting sharing request on targetUserID's behalf.
func ShareSubgroupAndApprove(ctx context.Context, groupID, subgroupID, ownerUserID, targetUserID string) {
	_, err := group.ShareSubGroup(rmngctx.NewRmngContext(user.NewUser(ownerUserID)), groupID, subgroupID, targetUserID, auth.UserInfo{})
	Expect(err).To(BeNil())
	targetCtx := rmngctx.NewRmngContext(user.NewUser(targetUserID))
	sharingRequests, err := group.GetMySharingRequests(targetCtx)
	Expect(err).To(BeNil())
	Expect(sharingRequests).ToNot(BeEmpty())
	Expect(group.ApproveSharingRequest(targetCtx, sharingRequests[0].SharingRequestID)).To(Succeed())
}

func _AddNodeToSubgroup(ctx context.Context, groupID, nodeID, subgroupID, userID string) (events.APIGatewayProxyResponse, error) {
	// Add node to subgroup
	addNodeRequest := events.APIGatewayProxyRequest{
		HTTPMethod: "PUT",
		Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/nodes/" + nodeID,
		Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}",
		Body:       `{}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{
			"groupId":    groupID,
			"subGroupId": subgroupID,
			"nodeId":     nodeID,
		},
	}

	return handleRequest(ctx, addNodeRequest)
}

func AddNodeToSubgroup(ctx context.Context, groupID, nodeID, subgroupID, userID string) {
	response, err := _AddNodeToSubgroup(ctx, groupID, nodeID, subgroupID, userID)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusOK))
}

func RemoveNodeFromSubgroup(ctx context.Context, groupID, nodeID, subgroupID, userID string) (events.APIGatewayProxyResponse, error) {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "DELETE",
		Path:       fmt.Sprintf("/v1/groups/%s/subgroups/%s/nodes/%s", groupID, subgroupID, nodeID),
		Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
		PathParameters: map[string]string{
			"groupId":    groupID,
			"subGroupId": subgroupID,
			"nodeId":     nodeID,
		},
	}

	return handleRequest(ctx, request)
}

// ApproveSharingRequest fetches the sharing request for the user using GET /v1/sharing-requests/received
// and then approves it using POST /v1/sharing-requests/{requestId}/accept
func ApproveSharingRequest(ctx context.Context, otherUserID string) {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Path:       "/v1/sharing-requests/received",
		Resource:   "/v1/sharing-requests/received",
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             otherUserID,
				CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
			},
		},
	}

	response, err := handleRequest(ctx, request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusOK))

	var listSharingRequestsResponse ListSharingRequestsResponse
	err = json.Unmarshal([]byte(response.Body), &listSharingRequestsResponse)
	Expect(err).To(BeNil())
	Expect(listSharingRequestsResponse.SharingRequests).To(HaveLen(1))
	sharingRequest := listSharingRequestsResponse.SharingRequests[0]
	Expect(sharingRequest.PrimaryUserID).ToNot(BeEmpty())
	Expect(sharingRequest.PrimaryEmail).ToNot(BeEmpty())
	sharingRequestID := sharingRequest.SharingRequestID

	approveRequest := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       "/v1/sharing-requests/" + sharingRequestID + "/accept",
		Resource:   "/v1/sharing-requests/{requestId}/accept",
		PathParameters: map[string]string{
			"requestId": sharingRequestID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             otherUserID,
				CognitoAuthenticationProvider: "https://issuer.example:" + otherUserID,
			},
		},
	}
	response, err = handleRequest(ctx, approveRequest)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(http.StatusOK))
}

// CreateGroupWithMatter creates a group with matter capability and returns the group ID
func CreateGroupWithMatter(ctx context.Context, groupName, userID string) string {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Resource:   "/v1/groups",
		Body:       `{"group_name": "` + groupName + `", "capabilities": ["matter"]}`,
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID:             userID,
				CognitoAuthenticationProvider: "https://issuer.example:" + userID,
			},
		},
	}
	createResponse, err := handlePostGroup(ctx, request)
	Expect(err).To(BeNil())
	Expect(createResponse.StatusCode).To(Equal(http.StatusCreated))

	var rawResponse map[string]interface{}
	err = json.Unmarshal([]byte(createResponse.Body), &rawResponse)
	Expect(err).To(BeNil())
	groupID := rawResponse["group_id"].(string)
	return groupID
}

// GenerateTestCSR generates a valid CSR for testing purposes
func GenerateTestCSR() string {
	// Use the group package's key generation
	privateKey, err := group.CreateECKeyPair()
	Expect(err).To(BeNil())

	// Create CSR template
	csrTemplate := &x509.CertificateRequest{
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	// Create CSR
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privateKey)
	Expect(err).To(BeNil())

	// Encode to PEM
	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrBytes,
	})

	return string(csrPEM)
}

// escapeJSON escapes a string for use in JSON
func escapeJSON(s string) string {
	// Replace backslashes first, then newlines
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func DoHTTPGetNodeConfig(ctx context.Context, groupID, subgroupID, nodeID string, callerUserID string) (events.APIGatewayProxyResponse, error) {
	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET",
		Path:       "/v1/groups/" + groupID + "/subgroups/" + subgroupID + "/nodes/" + nodeID,
		Resource:   "/v1/groups/{groupId}/subgroups/{subGroupId}/nodes/{nodeId}",
		PathParameters: map[string]string{
			"groupId":    groupID,
			"subGroupId": subgroupID,
			"nodeId":     nodeID,
		},
		RequestContext: events.APIGatewayProxyRequestContext{
			Identity: events.APIGatewayRequestIdentity{
				CognitoIdentityID: callerUserID,
			},
		},
	}
	return handleRequest(ctx, request)
}

var _ = AfterSuite(func() {
	fmt.Printf("profiles: %v\n", profiles)
	for key, profile := range profiles {
		if profile != nil {
			var timingFile *os.File
			timingFile, _ = test_utils.CreateCommonSummaryFile("group_main_" + uuid.New().String() + ".txt")
			fmt.Fprintf(timingFile, "\n--- %s ---\n", key)
			profile.Print(timingFile)
			fmt.Fprintf(timingFile, "-----------------------------\n\n")
			timingFile.Close()
		}
	}
})

func TestGroup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Group API Suite")
}
