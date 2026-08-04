// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group_test

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"reflect"
	"sort"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Group", func() {
	var (
		rmng_context *rmngctx.RmngContext
		dbMock       *mock.DynamoDBMock
		testGroup    *group.Group
		testUser     *user.User
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		dbMock = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

		testUser = user.NewUser("test-user-id")
		rmng_context = rmngctx.NewRmngContext(testUser)
		var err error
		testGroup, err = group.CreateGroupForUser(rmng_context, "Test Group")
		Expect(err).To(BeNil())
		Expect(testGroup).ToNot(BeNil())
	})

	Describe("AddNode", func() {
		It("should successfully add nodes to a group", func() {
			nodeIDs := []string{"node1", "node2", "node3"}
			for _, nodeID := range nodeIDs {
				rmng_context.SetAllow(utils.NodeAll, nodeID)
				_, err := group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
				Expect(err).To(BeNil())
			}

			// Verify that nodes were added to the group_device_mapping table
			test_utils.AssertNodeInGroup(testGroup.GroupID, nodeIDs[0])
			test_utils.AssertNodeInGroup(testGroup.GroupID, nodeIDs[1])
			test_utils.AssertNodeInGroup(testGroup.GroupID, nodeIDs[2])
		})

		It("should not add duplicate nodes", func() {
			nodeIDs := []string{"node1", "node2", "node3"}
			for _, nodeID := range nodeIDs {
				rmng_context.SetAllow(utils.NodeAll, nodeID)
				_, err := group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
				Expect(err).To(BeNil())
			}

			// Try to add the same nodes again
			for _, nodeID := range nodeIDs {
				_, err := group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
				Expect(err).To(BeNil())
			}

			// Verify that there are no duplicate entries
			queryInput := &dynamodb.QueryInput{
				TableName:              aws.String(group_node_db.GroupDeviceMappingTable),
				KeyConditionExpression: aws.String("group_id = :gid"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":gid": &types.AttributeValueMemberS{Value: testGroup.GroupID},
				},
			}
			queryOutput, err := dbMock.Query(rmng_context.Context, queryInput)
			Expect(err).To(BeNil())
			Expect(len(queryOutput.Items)).To(Equal(len(nodeIDs)))
		})

		It("should return an error when DynamoDB client fails", func() {
			nodeID := "node1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)

			// Simulate a DynamoDB error
			dbMock.PutItemErr = &types.InternalServerError{
				Message: aws.String("DynamoDB error"),
			}

			_, err := group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("failed to add node to group in DynamoDB"))

			// Reset the mock error
			dbMock.PutItemErr = nil
		})

		It("should remove a node from a previous group", func() {
			rmng_context.SetAllow(utils.NodeAll, "node1")
			_, err := group.AddNode(rmng_context, testGroup.GroupID, "node1", nil)
			Expect(err).To(BeNil())
			test_utils.AssertNodeInGroup(testGroup.GroupID, "node1")

			group2, err := group.CreateGroupForUser(rmng_context, "Test Group 2")
			Expect(err).To(BeNil())
			Expect(group2).ToNot(BeNil())

			_, err = group.AddNode(rmng_context, group2.GroupID, "node1", nil)
			Expect(err).To(BeNil())

			test_utils.AssertNodeNotInGroup(testGroup.GroupID, "node1")
			test_utils.AssertNodeInGroup(group2.GroupID, "node1")
		})
	})

	Describe("GenerateGroupID", func() {
		It("should generate a valid group ID", func() {
			groupID := ids.GenerateGroupID()
			Expect(groupID).To(HaveLen(6))
			Expect(string(groupID[0])).To(MatchRegexp("[a-z]"))
			Expect(string(groupID)).To(MatchRegexp("^[a-z][a-z0-9]{5}$"))
		})
	})

	Describe("Subgroups", func() {
		It("should return an error when the parent group does not exist", func() {
			_, err := group.CreateSubGroup(rmng_context, "non-existent-group", "Subgroup")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))
		})

		It("should create a subgroup and store it correctly in the database", func() {
			parentGroup, err := group.CreateGroupForUser(rmng_context, "Parent Group")
			Expect(err).To(BeNil())

			subGroupName := "Subgroup"
			subGroup, err := group.CreateSubGroup(rmng_context, parentGroup.GroupID, subGroupName)
			Expect(err).To(BeNil())
			Expect(subGroup.SubGroupID).To(HaveLen(3))
			Expect(subGroup.SubGroupID).To(MatchRegexp("^[a-z0-9]{3}$"))

			// Validate the subgroup entry in the mock database
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

			found := false
			shouldFind := make(map[string]types.AttributeValue)
			shouldFind["group_id"] = &types.AttributeValueMemberS{Value: parentGroup.GroupID}
			shouldFind["sub_group_id"] = &types.AttributeValueMemberS{Value: subGroup.SubGroupID}
			shouldFind["group_name"] = &types.AttributeValueMemberS{Value: subGroupName}

			dbMock.ForEachRow(group_db.GroupsTable, func(item map[string]types.AttributeValue) error {
				if reflect.DeepEqual(item, shouldFind) {
					found = true
					return nil
				}
				return nil
			})

			Expect(found).To(BeTrue(), "Subgroup should be found in the database")
		})

		It("should list subgroups", func() {
			parentGroup, err := group.CreateGroupForUser(rmng_context, "Parent Group")
			Expect(err).To(BeNil())

			subGroup1, err := group.CreateSubGroup(rmng_context, parentGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmng_context, parentGroup.GroupID, "Subgroup 2")
			Expect(err).To(BeNil())

			expectedParentResult := group.Group{
				GroupID:          parentGroup.GroupID,
				GroupName:        "Parent Group",
				AccessType:       utils.GroupPrimaryAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{},
				SubGroups:        []group.SubGroup{*subGroup1, *subGroup2},
			}

			groups, err := group.ListGroupsForUser(rmng_context, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(2))
			Expect(groups).To(ContainElement(expectedParentResult))
		})

		It("should not create subgroups with same subgroupIDs", func() {
			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())

			groupDB := group_db.NewGroupDB(rmng_context)
			err = groupDB.CreateSubGroup(testGroup.GroupID, "Subgroup 1", subGroup.SubGroupID)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListGroupsForUser", func() {
		It("should list all groups for a user including subgroups", func() {
			// Create multiple groups for the user
			group2, err := group.CreateGroupForUser(rmng_context, "Group 2")
			Expect(err).To(BeNil())

			// Create subgroups
			subGroup1, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 2")
			Expect(err).To(BeNil())
			subGroup3, err := group.CreateSubGroup(rmng_context, group2.GroupID, "Subgroup 3")
			Expect(err).To(BeNil())

			// List groups for the user
			groups, err := group.ListGroupsForUser(rmng_context, false)
			Expect(err).To(BeNil())

			// Verify the number of top-level groups
			Expect(groups).To(HaveLen(2))

			// Sort the groups by GroupID to ensure consistent order for assertions
			sort.Slice(groups, func(i, j int) bool {
				return groups[i].GroupID < groups[j].GroupID
			})

			expectedGroup1 := group.Group{
				GroupID:    testGroup.GroupID,
				GroupName:  "Test Group",
				AccessType: utils.GroupPrimaryAccess,
				SubGroups: []group.SubGroup{
					{
						SubGroupID:   subGroup1.SubGroupID,
						SubGroupName: "Subgroup 1",
					},
					{
						SubGroupID:   subGroup2.SubGroupID,
						SubGroupName: "Subgroup 2",
					},
				},
			}

			expectedGroup2 := group.Group{
				GroupID:    group2.GroupID,
				GroupName:  "Group 2",
				AccessType: utils.GroupPrimaryAccess,
				SubGroups: []group.SubGroup{
					{
						SubGroupID:   subGroup3.SubGroupID,
						SubGroupName: "Subgroup 3",
					},
				},
			}

			test_utils.SortGroup(&expectedGroup1)
			test_utils.SortGroup(&expectedGroup2)
			for _, group := range groups {
				test_utils.SortGroup(&group)
			}
			Expect(groups).To(ContainElement(expectedGroup1))
			Expect(groups).To(ContainElement(expectedGroup2))
		})

		It("should return an empty list for a user with no groups", func() {
			// Create a new user with no groups
			newUser := user.NewUser("new-user-id")
			rmng_new_context := rmngctx.NewRmngContext(newUser)

			// List groups for the new user
			groups, err := group.ListGroupsForUser(rmng_new_context, false)
			Expect(err).To(BeNil())
			Expect(groups).To(BeEmpty())
		})

		It("should handle a user with groups but no subgroups", func() {
			// Create groups for the user without subgroups
			group2, err := group.CreateGroupForUser(rmng_context, "Group B")
			Expect(err).To(BeNil())

			// List groups for the user
			groups, err := group.ListGroupsForUser(rmng_context, false)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(2))

			// Sort the groups by GroupID to ensure consistent order for assertions
			sort.Slice(groups, func(i, j int) bool {
				return groups[i].GroupID < groups[j].GroupID
			})

			expectedGroup1 := group.Group{
				GroupID:    testGroup.GroupID,
				GroupName:  "Test Group",
				AccessType: utils.GroupPrimaryAccess,
			}

			expectedGroup2 := group.Group{
				GroupID:    group2.GroupID,
				GroupName:  "Group B",
				AccessType: utils.GroupPrimaryAccess,
			}
			// Verify the groups
			Expect(groups).To(ContainElement(expectedGroup1))
			Expect(groups).To(ContainElement(expectedGroup2))
		})
	})

	Describe("AddNodeToSubgroup", func() {
		var (
			subGroup1 *group.SubGroup
			subGroup2 *group.SubGroup
			subGroup3 *group.SubGroup
			subGroup4 *group.SubGroup
			nodeID    string
		)
		BeforeEach(func() {
			var err error
			subGroup1, err = group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			subGroup2, err = group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 2")
			Expect(err).To(BeNil())
			subGroup3, err = group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 3")
			Expect(err).To(BeNil())
			subGroup4, err = group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 4")
			Expect(err).To(BeNil())

			nodeID = "test-node"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			_, err = group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
			Expect(err).To(BeNil())
		})
		It("should successfully add a node to a subgroup", func() {
			_, err := group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify that the node is in the subgroup
			item := getGroupDeviceMappingItem(testGroup.GroupID, nodeID)
			Expect(item["subgrp1"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup1.SubGroupID))
		})

		It("should add a node to multiple subgroups", func() {
			_, err := group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify that the node is in both subgroups
			item := getGroupDeviceMappingItem(testGroup.GroupID, nodeID)
			Expect(item["subgrp1"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup1.SubGroupID))
			Expect(item["subgrp2"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup2.SubGroupID))
		})

		It("should fail to add a node to more than three subgroups", func() {
			_, err := group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup3.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Attempt to add to a fourth subgroup
			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup4.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("node is already in 3 subgroups"))

			// Verify that only three subgroups are present
			item := getGroupDeviceMappingItem(testGroup.GroupID, nodeID)
			Expect(item["subgrp1"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup1.SubGroupID))
			Expect(item["subgrp2"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup2.SubGroupID))
			Expect(item["subgrp3"].(*types.AttributeValueMemberS).Value).To(Equal(subGroup3.SubGroupID))
			Expect(item["subgrp4"]).To(BeNil())

			// Verify using 'LoadGroup' and 'LoadNodes' APIs
			loadedGroup, err := group.LoadGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())
			loadedGroup.LoadNodes(rmng_context)
			Expect(loadedGroup.SubGroups).To(HaveLen(4))

			nodeEntries := map[string]*group_node_db.GroupNode{nodeID: {GroupID: testGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup1.SubGroupID, SubGrp2: subGroup2.SubGroupID, SubGrp3: subGroup3.SubGroupID}}
			expectedSubGroups := []group.SubGroup{
				{SubGroupID: subGroup1.SubGroupID, SubGroupName: subGroup1.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup3.SubGroupID, SubGroupName: subGroup3.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup4.SubGroupID, SubGroupName: subGroup4.SubGroupName},
			}
			Expect(loadedGroup.SubGroups).To(ConsistOf(expectedSubGroups))
		})

		It("should return an error when trying to add a node that's not in the group", func() {
			nonExistentNodeID := "non-existent-node"
			rmng_context.SetAllow(utils.NodeAll, nonExistentNodeID)
			_, err := group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nonExistentNodeID, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("node is not in the group"))
		})

		It("should return correct nodes when LoadNodes is called", func() {
			moreNodeIDs := []string{"node2", "node3", "node4"}
			for _, nid := range moreNodeIDs {
				rmng_context.SetAllow(utils.NodeAll, nid)
				_, err := group.AddNode(rmng_context, testGroup.GroupID, nid, nil)
				Expect(err).To(BeNil())
			}

			_, err := group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup1.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup3.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, "node2", subGroup4.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, "node3", subGroup4.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, "node4", subGroup4.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify using 'LoadGroup' and 'LoadNodes' APIs
			loadedGroup, err := group.LoadGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())
			loadedGroup.LoadNodes(rmng_context)
			Expect(loadedGroup.SubGroups).To(HaveLen(4))

			nodeEntries := map[string]*group_node_db.GroupNode{nodeID: {GroupID: testGroup.GroupID, NodeID: nodeID, SubGrp1: subGroup1.SubGroupID, SubGrp2: subGroup2.SubGroupID, SubGrp3: subGroup3.SubGroupID}}
			expectedSubGroups := []group.SubGroup{
				{SubGroupID: subGroup1.SubGroupID, SubGroupName: subGroup1.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup2.SubGroupID, SubGroupName: subGroup2.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup3.SubGroupID, SubGroupName: subGroup3.SubGroupName, NodeGroupEntries: nodeEntries},
				{SubGroupID: subGroup4.SubGroupID, SubGroupName: subGroup4.SubGroupName, NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: testGroup.GroupID, NodeID: "node2", SubGrp1: subGroup4.SubGroupID},
					"node3": {GroupID: testGroup.GroupID, NodeID: "node3", SubGrp1: subGroup4.SubGroupID},
					"node4": {GroupID: testGroup.GroupID, NodeID: "node4", SubGrp1: subGroup4.SubGroupID},
				}},
			}
			Expect(loadedGroup.SubGroups).To(ConsistOf(expectedSubGroups))
		})
	})

	Describe("DeleteGroup", func() {
		It("deletes an empty group and removes all references", func() {
			err := group.DeleteGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())

			// Verify that the parent group is deleted from the groups table
			groupDB := group_db.NewGroupDB(rmng_context)
			_, err = groupDB.GetGroupByID(testGroup.GroupID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("group not found"))

			// Verify that there are no references in the user_group_mapping table
			userGroupDB := user_group_db.NewUserGroupDB(rmng_context)
			groups, err := userGroupDB.ListGroupsForUser("")
			Expect(err).To(BeNil())
			Expect(groups).To(BeEmpty())

			// Additional verification using direct DynamoDB queries
			dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

			// Check groups table
			queryInput := &dynamodb.QueryInput{
				TableName:              aws.String(group_db.GroupsTable),
				KeyConditionExpression: aws.String("group_id = :gid"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":gid": &types.AttributeValueMemberS{Value: testGroup.GroupID},
				},
			}
			queryOutput, err := dbMock.Query(rmng_context.Context, queryInput)
			Expect(err).To(BeNil())
			Expect(queryOutput.Items).To(BeEmpty())

			// Check group_device_mapping table
			queryInput = &dynamodb.QueryInput{
				TableName:              aws.String(group_node_db.GroupDeviceMappingTable),
				KeyConditionExpression: aws.String("group_id = :gid"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":gid": &types.AttributeValueMemberS{Value: testGroup.GroupID},
				},
			}
			queryOutput, err = dbMock.Query(rmng_context.Context, queryInput)
			Expect(err).To(BeNil())
			Expect(queryOutput.Items).To(BeEmpty())

			// Check user_group_mapping table
			queryInput = &dynamodb.QueryInput{
				TableName:              aws.String(user_group_db.UserGroupMappingTable),
				IndexName:              aws.String(user_group_db.UserGroupMappingByGroupIDIndex),
				KeyConditionExpression: aws.String("group_id = :gid"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":gid": &types.AttributeValueMemberS{Value: testGroup.GroupID},
				},
			}
			queryOutput, err = dbMock.Query(rmng_context.Context, queryInput)
			Expect(err).To(BeNil())
			Expect(queryOutput.Items).To(BeEmpty())
		})

		It("ignores child nodes (\"--\") when checking emptiness", func() {
			nodeID := "node1--child1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			_, err := group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
			Expect(err).To(BeNil())

			err = group.DeleteGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())

			groupDB := group_db.NewGroupDB(rmng_context)
			_, err = groupDB.GetGroupByID(testGroup.GroupID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("group not found"))
		})

		It("deletes the group after its nodes and subgroups are removed (full flow)", func() {
			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())

			nodeID := "node1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			_, err = group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
			Expect(err).To(BeNil())
			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Non-empty: rejected while the node is still attached.
			err = group.DeleteGroup(rmng_context, testGroup.GroupID)
			Expect(errors.Is(err, group.ErrGroupNotEmpty)).To(BeTrue())

			// Empty the subgroup and delete it; the group still holds the node directly.
			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(BeNil())
			err = group.DeleteSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID)
			Expect(err).To(BeNil())

			// Still rejected: the node remains attached to the parent group.
			err = group.DeleteGroup(rmng_context, testGroup.GroupID)
			Expect(errors.Is(err, group.ErrGroupNotEmpty)).To(BeTrue())

			// Remove the last node, then the empty group deletes.
			_, err = group.RemoveNode(rmng_context, testGroup.GroupID, nodeID)
			Expect(err).To(BeNil())
			err = group.DeleteGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())

			groupDB := group_db.NewGroupDB(rmng_context)
			_, err = groupDB.GetGroupByID(testGroup.GroupID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("group not found"))
		})
	})

	Describe("DeleteSubGroup", func() {
		It("deletes the subgroup after its nodes are removed from it (full flow)", func() {
			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())

			nodeID := "node1"
			rmng_context.SetAllow(utils.NodeAll, nodeID)
			_, err = group.AddNode(rmng_context, testGroup.GroupID, nodeID, nil)
			Expect(err).To(BeNil())
			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Non-empty: delete is rejected.
			err = group.DeleteSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID)
			Expect(errors.Is(err, group.ErrSubGroupNotEmpty)).To(BeTrue())

			// Client removes the node from the subgroup, then delete succeeds.
			_, err = group.UpdateNodeAndSubgroup(rmng_context, testGroup.GroupID, nodeID, subGroup.SubGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(BeNil())

			err = group.DeleteSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID)
			Expect(err).To(BeNil())

			isSubGroup, err := group.IsSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID)
			Expect(err).To(BeNil())
			Expect(isSubGroup).To(BeFalse())

			// The node itself stays in the parent group.
			test_utils.AssertNodeInGroup(testGroup.GroupID, nodeID)
		})

		It("should remove the deleted subgroup from a subentity-shared user's sub_entity_ids", func() {
			rmngContext1 := rmngctx.NewRmngContext(user.NewUser("owner-user"))
			rmngContext2 := rmngctx.NewRmngContext(user.NewUser("shared-user"))

			parentGroup, err := group.CreateGroupForUser(rmngContext1, "Parent Group")
			Expect(err).To(BeNil())
			subGroup1, err := group.CreateSubGroup(rmngContext1, parentGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmngContext1, parentGroup.GroupID, "Subgroup 2")
			Expect(err).To(BeNil())

			// Share both subgroups with another user (subentity access)
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroup.GroupID, subGroup1.SubGroupID)
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroup.GroupID, subGroup2.SubGroupID)

			err = group.DeleteSubGroup(rmngContext1, parentGroup.GroupID, subGroup1.SubGroupID)
			Expect(err).To(BeNil())

			// The shared user keeps subGroup2 access; subGroup1 is gone from their mapping.
			// Assert the full mapping entry.
			userGroupDB := user_group_db.NewUserGroupDB(rmngContext2)
			item, err := userGroupDB.GetUserGroupItemByUserID(rmngContext2.Accessor.GetID(), parentGroup.GroupID)
			Expect(err).To(BeNil())
			Expect(item.Item).ToNot(BeNil())

			var entry user_group_db.UserGroupEntry
			Expect(attributevalue.UnmarshalMap(item.Item, &entry)).To(Succeed())
			test_utils.AssertNormalizedEqual(user_group_db.UserGroupEntry{
				UserID:       rmngContext2.Accessor.GetID(),
				GroupID:      parentGroup.GroupID,
				SubEntityIDs: []string{subGroup2.SubGroupID},
				AccessType:   utils.GroupSubEntityAccess,
			}, entry)
		})

		It("should delete the mapping row when the deleted subgroup was the user's only access", func() {
			rmngContext1 := rmngctx.NewRmngContext(user.NewUser("owner-user"))
			rmngContext2 := rmngctx.NewRmngContext(user.NewUser("shared-user"))

			parentGroup, err := group.CreateGroupForUser(rmngContext1, "Parent Group")
			Expect(err).To(BeNil())
			subGroup, err := group.CreateSubGroup(rmngContext1, parentGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())

			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroup.GroupID, subGroup.SubGroupID)

			err = group.DeleteSubGroup(rmngContext1, parentGroup.GroupID, subGroup.SubGroupID)
			Expect(err).To(BeNil())

			// With no remaining subgroup access, the shared user's mapping row is removed
			userGroupDB := user_group_db.NewUserGroupDB(rmngContext2)
			item, err := userGroupDB.GetUserGroupItemByUserID(rmngContext2.Accessor.GetID(), parentGroup.GroupID)
			Expect(err).To(BeNil())
			Expect(item.Item).To(BeNil())
		})

		It("should not affect the primary owner's mapping", func() {
			subGroup, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())

			err = group.DeleteSubGroup(rmng_context, testGroup.GroupID, subGroup.SubGroupID)
			Expect(err).To(BeNil())

			// Owner has empty sub_entity_ids; their mapping row must survive intact.
			// Assert the full mapping entry.
			userGroupDB := user_group_db.NewUserGroupDB(rmng_context)
			item, err := userGroupDB.GetUserGroupItemByUserID(rmng_context.Accessor.GetID(), testGroup.GroupID)
			Expect(err).To(BeNil())
			Expect(item.Item).ToNot(BeNil())

			var entry user_group_db.UserGroupEntry
			Expect(attributevalue.UnmarshalMap(item.Item, &entry)).To(Succeed())
			test_utils.AssertNormalizedEqual(user_group_db.UserGroupEntry{
				UserID:       rmng_context.Accessor.GetID(),
				GroupID:      testGroup.GroupID,
				SubEntityIDs: []string{},
				AccessType:   utils.GroupPrimaryAccess,
			}, entry)
		})

		It("should remove only the targeted subgroup and preserve siblings", func() {
			subGroup1, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			subGroup2, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 2")
			Expect(err).To(BeNil())
			subGroup3, err := group.CreateSubGroup(rmng_context, testGroup.GroupID, "Subgroup 3")
			Expect(err).To(BeNil())

			err = group.DeleteSubGroup(rmng_context, testGroup.GroupID, subGroup2.SubGroupID)
			Expect(err).To(BeNil())

			// Only subGroup2 is removed; assert the full surviving subgroup set.
			expectedGroup := group.Group{
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup1.SubGroupID, SubGroupName: subGroup1.SubGroupName},
					{SubGroupID: subGroup3.SubGroupID, SubGroupName: subGroup3.SubGroupName},
				},
			}
			updatedGroup, err := group.LoadGroup(rmng_context, testGroup.GroupID)
			Expect(err).To(BeNil())
			Expect(updatedGroup.LoadNodes(rmng_context)).To(BeNil())
			test_utils.AssertNormalizedEqual(expectedGroup.SubGroups, updatedGroup.SubGroups)
		})
	})
})

var _ = Describe("Group Sharing", func() {
	var (
		testUser1     *user.User
		testUser2     *user.User
		testUser3     *user.User
		rmngContext1  *rmngctx.RmngContext
		rmngContext2  *rmngctx.RmngContext
		rmngContext3  *rmngctx.RmngContext
		sharedGroupID *group.Group
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testUser1 = user.NewUser("test-user-1")
		testUser2 = user.NewUser("test-user-2")
		testUser3 = user.NewUser("test-user-3")
		rmngContext1 = rmngctx.NewRmngContext(testUser1)
		rmngContext2 = rmngctx.NewRmngContext(testUser2)
		rmngContext3 = rmngctx.NewRmngContext(testUser3)

		// Create a group for testUser1
		var err error
		sharedGroupID, err = group.CreateGroupForUser(rmngContext1, "Shared Group")
		Expect(err).To(BeNil())
	})

	Describe("ShareGroup with Primary Access", func() {
		It("should successfully share a group with primary access", func() {
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupPrimaryAccess)

			// Verify that the group was shared with primary access
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should allow a user with primary access to share the group", func() {
			// First, share the group with testUser2 with primary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupPrimaryAccess)

			// This is necessary to ensure that the group is loaded into the user's context
			sharedGroups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(HaveLen(1))
			Expect(sharedGroups[0].GroupID).To(Equal(sharedGroupID.GroupID))

			// Now, testUser2 should be able to share the group with testUser3
			ShareAndApproveGroup(rmngContext2, rmngContext3, sharedGroupID.GroupID, utils.GroupSecondaryAccess)

			// Verify that testUser3 now has secondary access to the group
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser3.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
			})
		})
	})

	Describe("ShareGroup with Secondary Access", func() {
		It("should successfully share a group with secondary access", func() {
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupSecondaryAccess)

			// Verify that the group was shared with secondary access
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
			})
		})

		It("should not allow a user with secondary access to share the group", func() {
			// First, share the group with testUser2 with secondary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupSecondaryAccess)

			// Verify that the group was shared with secondary access
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
			})

			// Now, testUser2 should not be able to share the group with testUser3
			_, err := group.ShareGroup(rmngContext2, sharedGroupID.GroupID, testUser3.GetID(), utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("entity test-user-2 is not authorized to perform group:share"))
		})
	})

	Describe("UnshareGroup", func() {
		BeforeEach(func() {
			// Share the group with testUser2 and testUser3 before each test
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupPrimaryAccess)
			ShareAndApproveGroup(rmngContext1, rmngContext3, sharedGroupID.GroupID, utils.GroupSecondaryAccess)
		})

		It("should successfully unshare a group for a user with primary access", func() {
			err := group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Verify that the group was unshared for testUser2
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
			})).To(BeNil())

			// Verify that the group entry for testUser1 and testUser3 are still in the database
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser1.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser3.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
			})
		})

		It("should successfully unshare a group for a user with secondary access", func() {
			err := group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser3.GetID())
			Expect(err).To(BeNil())

			// Verify that the group was unshared for testUser3
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser3.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
			})).To(BeNil())

			// Verify that the group entries for testUser1 and testUser2 are still in the database
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser1.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should not allow a user with secondary access to unshare the group", func() {
			err := group.UnshareGroup(rmngContext3, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("entity test-user-3 is not authorized to perform group:share"))
		})

		It("should allow a secondary user to leave the group themselves (self-leave)", func() {
			// testUser3 is secondary (set up in BeforeEach). They should be able to leave via UnshareGroup
			// where targetUserID == caller ID, even without group:share permission.
			err := group.UnshareGroup(rmngContext3, sharedGroupID.GroupID, testUser3.GetID())
			Expect(err).To(BeNil())

			// testUser3's mapping should be gone.
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser3.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
			})).To(BeNil())
		})

		It("should allow a primary user to leave themselves when another primary remains", func() {
			// testUser2 is a primary; testUser1 is also primary. testUser2 leaves.
			err := group.UnshareGroup(rmngContext2, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
			})).To(BeNil())
		})

		It("should fail self-leave when caller has no access to the group", func() {
			outsider := user.NewUser("outsider-user")
			outsiderCtx := rmngctx.NewRmngContext(outsider)

			err := group.UnshareGroup(outsiderCtx, sharedGroupID.GroupID, outsider.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))
		})

		It("should fail kick when caller has no access to the group", func() {
			outsider := user.NewUser("outsider-user")
			outsiderCtx := rmngctx.NewRmngContext(outsider)

			// outsider tries to kick testUser2 (who is a legitimate primary)
			err := group.UnshareGroup(outsiderCtx, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))

			// testUser2's mapping must still be intact.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})
	})

	Describe("UnshareGroup last-primary guard on solo-primary group", func() {
		It("should not allow the last primary user to leave when co-primary is already removed", func() {
			// Start state: testUser1 (primary), testUser2 (primary), testUser3 (secondary).
			// Remove testUser2 so only one primary remains.
			err := group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// testUser1 is now the sole primary. Attempting to remove them must fail.
			err = group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser1.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("last primary user"))

			// The primary mapping should remain intact.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser1.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should not allow the sole primary user (owner) to leave their own group", func() {
			// sharedGroupID has only testUser1 as primary, no other members.
			err := group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser1.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("last primary user"))

			// Owner mapping must still exist.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser1.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: sharedGroupID.GroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should not allow the sole primary to leave even when a secondary user is present", func() {
			ShareAndApproveGroup(rmngContext1, rmngContext3, sharedGroupID.GroupID, utils.GroupSecondaryAccess)

			err := group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser1.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("last primary user"))
		})
	})

	Describe("ListSharedGroups", func() {
		var (
			expectedGroup group.Group
		)
		BeforeEach(func() {
			// Create a subgroup in Shared Group and add a few nodes to the group
			subGroup1, err := group.CreateSubGroup(rmngContext1, sharedGroupID.GroupID, "Subgroup 1")
			Expect(err).To(BeNil())
			nodeIDs := []string{"node1", "node2", "node3"}
			for _, nodeID := range nodeIDs {
				rmngContext1.SetAllow(utils.NodeAll, nodeID)
				_, err = group.AddNode(rmngContext1, sharedGroupID.GroupID, nodeID, nil)
				Expect(err).To(BeNil())
			}

			expectedGroup = group.Group{
				GroupID:   sharedGroupID.GroupID,
				GroupName: sharedGroupID.GroupName,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node1": {GroupID: sharedGroupID.GroupID, NodeID: "node1"},
					"node2": {GroupID: sharedGroupID.GroupID, NodeID: "node2"},
					"node3": {GroupID: sharedGroupID.GroupID, NodeID: "node3"},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroup1.SubGroupID, SubGroupName: subGroup1.SubGroupName},
				},
			}

			// Share the group with testUser2 (primary) and testUser3 (secondary) before each test
			ShareAndApproveGroup(rmngContext1, rmngContext2, sharedGroupID.GroupID, utils.GroupPrimaryAccess)
			ShareAndApproveGroup(rmngContext1, rmngContext3, sharedGroupID.GroupID, utils.GroupSecondaryAccess)
		})

		It("should list shared groups for a user with primary access until unshared", func() {
			expectedGroup.AccessType = utils.GroupPrimaryAccess
			sharedGroups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(HaveLen(1))
			Expect(sharedGroups[0]).To(Equal(expectedGroup))

			err = group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			sharedGroups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(BeEmpty())
		})

		It("should list shared groups for a user with secondary access until unshared", func() {
			expectedGroup.AccessType = utils.GroupSecondaryAccess
			sharedGroups, err := group.ListGroupsForUser(rmngContext3, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(HaveLen(1))
			Expect(sharedGroups[0]).To(Equal(expectedGroup))

			err = group.UnshareGroup(rmngContext1, sharedGroupID.GroupID, testUser3.GetID())
			Expect(err).To(BeNil())

			sharedGroups, err = group.ListGroupsForUser(rmngContext3, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(BeEmpty())
		})
	})

	Describe("Subgroup Sharing", func() {
		var (
			parentGroupID string
			subGroupID    string
			subGroup2ID   string
		)

		BeforeEach(func() {
			// Create a parent group
			parentGroup, err := group.CreateGroupForUser(rmngContext1, "Parent Group")
			Expect(err).To(BeNil())
			parentGroupID = parentGroup.GroupID

			// Create a subgroup
			subGroup, err := group.CreateSubGroup(rmngContext1, parentGroupID, "Test Subgroup")
			Expect(err).To(BeNil())
			subGroupID = subGroup.SubGroupID

			// Create another subgroup
			subGroup2, err := group.CreateSubGroup(rmngContext1, parentGroupID, "Test Subgroup 2")
			Expect(err).To(BeNil())
			subGroup2ID = subGroup2.SubGroupID
			moreNodeIDs := []string{"node2", "node3", "node4"}
			for _, nid := range moreNodeIDs {
				rmngContext1.SetAllow(utils.NodeAll, nid)
			}

			for _, nid := range moreNodeIDs {
				_, err = group.AddNode(rmngContext1, parentGroupID, nid, nil)
				Expect(err).To(BeNil())
			}

			_, err = group.UpdateNodeAndSubgroup(rmngContext1, parentGroupID, moreNodeIDs[0], subGroup.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())
			_, err = group.UpdateNodeAndSubgroup(rmngContext1, parentGroupID, moreNodeIDs[0], subGroup2.SubGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())
		})

		It("should successfully share a subgroup", func() {
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// Verify that the subgroup was shared
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			})

			expectedGroup := group.Group{
				GroupID:          parentGroupID,
				GroupName:        "Parent Group",
				AccessType:       utils.GroupSubEntityAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID}},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID}}},
				},
			}
			sharedGroups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(HaveLen(1))
			Expect(sharedGroups[0]).To(Equal(expectedGroup))

		})

		It("should not allow sharing a subgroup by a user without primary access to the parent group", func() {
			ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupSecondaryAccess)

			_, err := group.ShareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser3.GetID(), auth.UserInfo{})
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("entity test-user-2 is not authorized to perform group:share"))
		})

		It("should not allow share or edit access to a parent group if a sub-group is shared", func() {
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// Verify that the subgroup is shared
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			})

			_, err := group.ShareGroup(rmngContext2, parentGroupID, testUser3.GetID(), utils.GroupSecondaryAccess, auth.UserInfo{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))

			err = group.DeleteGroup(rmngContext2, parentGroupID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))
		})

		It("should successfully share/unshare multiple subgroups", func() {
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
			})).To(BeNil())

			// First, share the subgroups
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroup2ID)

			// Verify that the subgroups are shared
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
			})).To(Equal(map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}, &types.AttributeValueMemberS{Value: subGroup2ID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			}))

			// Now, unshare one of the subgroups
			err := group.UnshareSubGroup(rmngContext1, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Verify that the other subgroup is still shared
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
			})).To(Equal(map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroup2ID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			}))

			sharedGroups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(HaveLen(1))
			Expect(sharedGroups[0].SubGroups).To(HaveLen(1))
			Expect(sharedGroups[0].SubGroups[0].SubGroupID).To(Equal(subGroup2ID))

			// Now, unshare the other subgroup
			err = group.UnshareSubGroup(rmngContext1, parentGroupID, subGroup2ID, testUser2.GetID())
			Expect(err).To(BeNil())

			sharedGroups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(sharedGroups).To(BeEmpty())

			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
			})).To(BeNil())
		})

		It("should not allow a subentity user to kick another user from a subgroup", func() {
			// Share the subgroup with both testUser2 and testUser3.
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)
			ShareAndApproveSubGroup(rmngContext1, rmngContext3, parentGroupID, subGroupID)

			// testUser2 (subentity) tries to unshare testUser3 (a different user).
			// This is the "kick" path and must be rejected: subentity access doesn't
			// include group:share, so the kick authorization check fails.
			err := group.UnshareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser3.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not authorized to perform group:share"))
		})

		It("should allow a subentity user to leave a subgroup themselves (self-leave)", func() {
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// testUser2 leaves their own subgroup access.
			err := group.UnshareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Their mapping row should be gone (only subgroup was subGroupID).
			Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
			})).To(BeNil())
		})

		It("should allow a subentity user to leave one of multiple subgroups and keep the others", func() {
			// testUser2 is shared into both subGroupID and subGroup2ID.
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroup2ID)

			// testUser2 leaves subGroupID only.
			err := group.UnshareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Mapping should remain with only subGroup2ID in sub_entity_ids.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroup2ID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			})
		})

		It("should fail when a user tries to leave a subgroup they are not a member of", func() {
			// testUser2 has no membership in parentGroupID at all.
			err := group.UnshareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))
		})

		It("should fail self-leave of subgroup when caller has group-level primary access (not subentity)", func() {
			// testUser2 has group-level primary access, not subentity access. Trying
			// to "leave" a specific subgroup they don't have subentity membership in
			// is an invalid operation — they are not a subentity member of any subgroup.
			ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

			err := group.UnshareSubGroup(rmngContext2, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user is not part of the specified subgroup"))
		})

		It("should fail kick from subgroup when target has group-level primary access (not subentity)", func() {
			// testUser2 has group-level primary access. testUser1 (owner/primary) tries
			// to remove testUser2 from a specific subgroup — but testUser2 isn't a
			// subentity member of any subgroup, so this should not silently succeed.
			ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

			err := group.UnshareSubGroup(rmngContext1, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user is not part of the specified subgroup"))

			// testUser2's group-level primary access must be intact.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
			})
		})

		It("should fail self-leave of subgroup when caller has no access to the parent group", func() {
			outsider := user.NewUser("outsider-user")
			outsiderCtx := rmngctx.NewRmngContext(outsider)

			err := group.UnshareSubGroup(outsiderCtx, parentGroupID, subGroupID, outsider.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))
		})

		It("should fail kick from subgroup when caller has no access to the parent group", func() {
			outsider := user.NewUser("outsider-user")
			outsiderCtx := rmngctx.NewRmngContext(outsider)
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			err := group.UnshareSubGroup(outsiderCtx, parentGroupID, subGroupID, testUser2.GetID())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parent group does not exist"))

			// testUser2's subentity mapping must still exist.
			test_utils.AssertRowInDB(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
				"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
				"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
				"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
				"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
			})
		})

		It("should allow a user with access to a subgroup to list only that subgroup and its nodes dynamically", func() {
			// Share the subgroup with testUser2
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// List groups for testUser2
			expectedGroup := group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupSubEntityAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					}},
				},
			}
			groups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))

			// The original user should have full access to the parent group
			expectedOriginalGroup := group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupPrimaryAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					"node3": {GroupID: parentGroupID, NodeID: "node3"},
					"node4": {GroupID: parentGroupID, NodeID: "node4"},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					}},
					{SubGroupID: subGroup2ID, SubGroupName: "Test Subgroup 2", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					}},
				},
			}
			originalGroups, err := group.ListGroupsForUser(rmngContext1, true)
			Expect(err).To(BeNil())
			Expect(originalGroups).To(ContainElement(expectedOriginalGroup))

			// Add node3 to the shared subgroup and node 4 to the non-shared subgroup
			rmngContext1.SetAllow(utils.NodeAll, "node3")
			rmngContext1.SetAllow(utils.NodeAll, "node4")

			_, err = group.UpdateNodeAndSubgroup(rmngContext1, parentGroupID, "node3", subGroupID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())
			_, err = group.UpdateNodeAndSubgroup(rmngContext1, parentGroupID, "node4", subGroup2ID, group_node_db.SubGroupOperationTypeAdd)
			Expect(err).To(BeNil())

			// Verify user2 only has access to the shared subgroup and its nodes
			expectedGroup = group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupSubEntityAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					"node3": {GroupID: parentGroupID, NodeID: "node3", SubGrp1: subGroupID},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
						"node3": {GroupID: parentGroupID, NodeID: "node3", SubGrp1: subGroupID},
					}},
				},
			}
			groups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))

			// Verify that the original user has access to the parent group and all its nodes
			expectedOriginalGroup = group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupPrimaryAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					"node3": {GroupID: parentGroupID, NodeID: "node3", SubGrp1: subGroupID},
					"node4": {GroupID: parentGroupID, NodeID: "node4", SubGrp1: subGroup2ID},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
						"node3": {GroupID: parentGroupID, NodeID: "node3", SubGrp1: subGroupID},
					}},
					{SubGroupID: subGroup2ID, SubGroupName: "Test Subgroup 2", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
						"node4": {GroupID: parentGroupID, NodeID: "node4", SubGrp1: subGroup2ID},
					}},
				},
			}
			originalGroups, err = group.ListGroupsForUser(rmngContext1, true)
			Expect(err).To(BeNil())
			Expect(originalGroups).To(ContainElement(expectedOriginalGroup))

			// Delete node3 from the shared subgroup
			_, err = group.UpdateNodeAndSubgroup(rmngContext1, parentGroupID, "node3", subGroupID, group_node_db.SubGroupOperationTypeRemove)
			Expect(err).To(BeNil())

			// Verify that the user no longer has access to node3
			expectedGroup = group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupSubEntityAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					}},
				},
			}
			groups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))
		})

		It("should correctly list subgroups when multiple subgroups within the same parent group are shared", func() {
			// Share the subgroup with testUser2
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// List groups for testUser2
			expectedGroup := group.Group{
				GroupID:    parentGroupID,
				GroupName:  "Parent Group",
				AccessType: utils.GroupSubEntityAccess,
				NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
				},
				SubGroups: []group.SubGroup{
					{SubGroupID: subGroupID, SubGroupName: "Test Subgroup", NodeGroupEntries: map[string]*group_node_db.GroupNode{
						"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
					}},
				},
			}
			groups, err := group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))

			// Share the second subgroup with testUser2
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroup2ID)

			expectedGroup.SubGroups = append(expectedGroup.SubGroups, group.SubGroup{
				SubGroupID: subGroup2ID, SubGroupName: "Test Subgroup 2", NodeGroupEntries: map[string]*group_node_db.GroupNode{
					"node2": {GroupID: parentGroupID, NodeID: "node2", SubGrp1: subGroupID, SubGrp2: subGroup2ID},
				},
			})
			groups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))

			// Now share the parent group itself with secondary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupSecondaryAccess)

			// Verify that the parent group is now included in the list
			expectedGroup.AccessType = utils.GroupSecondaryAccess
			expectedGroup.NodeGroupEntries["node3"] = &group_node_db.GroupNode{GroupID: parentGroupID, NodeID: "node3"}
			expectedGroup.NodeGroupEntries["node4"] = &group_node_db.GroupNode{GroupID: parentGroupID, NodeID: "node4"}
			groups, err = group.ListGroupsForUser(rmngContext2, true)
			Expect(err).To(BeNil())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0]).To(Equal(expectedGroup))
		})
		It("should correctly return the appropriate groups and subgroups in ListUserAccessableGroups", func() {
			// Create a group for testUser2
			testUser2Group, err := group.CreateGroupForUser(rmngContext2, "Test User 2 Group")
			Expect(err).To(BeNil())
			// Share a subgroup with testUser2
			ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

			// User2's accessable groups
			group_access, err := group.ListUserAccessableGroups(rmngContext2)
			Expect(err).To(BeNil())
			Expect(group_access).To(Equal(map[string]interface{}{
				"groups": []string{testUser2Group.GroupID},
				"subgroups": map[string][]string{
					parentGroupID: {subGroupID},
				},
			}))

			// User1's accessable groups
			group_access, err = group.ListUserAccessableGroups(rmngContext1)
			Expect(err).To(BeNil())
			Expect(group_access).To(Equal(map[string]interface{}{
				"groups":    []string{sharedGroupID.GroupID, parentGroupID},
				"subgroups": map[string][]string{},
			}))
		})

		Context("Subgroup sharing with parent group access", func() {
			It("should fail to share subgroup when parent group is already shared", func() {
				// First share the parent group with primary access
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

				// Try to share the subgroup - should fail
				_, err := group.ShareSubGroup(rmngContext1, parentGroupID, subGroupID, testUser2.GetID(), auth.UserInfo{})
				Expect(err).To(BeNil())

				// Get the sharing request ID from the TobeSharedUser's context
				sharingRequests, err := group.GetMySharingRequests(rmngContext2)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))

				// Approve the sharing request from the TobeSharedUser's context
				err = group.ApproveSharingRequest(rmngContext2, sharingRequests[0].SharingRequestID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot add sub-group access to a group level access"))

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
				}))

				// Verify the same for secondary access
				ShareAndApproveGroup(rmngContext1, rmngContext3, parentGroupID, utils.GroupSecondaryAccess)
				_, err = group.ShareSubGroup(rmngContext1, parentGroupID, subGroupID, testUser3.GetID(), auth.UserInfo{})
				Expect(err).To(BeNil())
				// Get the sharing request ID from the TobeSharedUser's context
				sharingRequests, err = group.GetMySharingRequests(rmngContext3)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))
				// Approve the sharing request from the TobeSharedUser's context
				err = group.ApproveSharingRequest(rmngContext3, sharingRequests[0].SharingRequestID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cannot add sub-group access to a group level access"))

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser3.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser3.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
				}))
			})

			It("should allow sharing parent group after subgroup is shared", func() {
				// Share the subgroup first
				ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
				}))

				// Now share the parent group with secondary access - should succeed
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupSecondaryAccess)

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
				}))
			})
		})

		Context("Re-sharing with a user that already has access", func() {
			It("should fail to re-share the parent group when the user already has the same access type", func() {
				// First share the parent group with primary access
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

				// Re-share the same group with the same access type - approval should fail
				_, err := group.ShareGroup(rmngContext1, parentGroupID, testUser2.GetID(), utils.GroupPrimaryAccess, auth.UserInfo{})
				Expect(err).To(BeNil())

				sharingRequests, err := group.GetMySharingRequests(rmngContext2)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))

				err = group.ApproveSharingRequest(rmngContext2, sharingRequests[0].SharingRequestID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no change to user group mapping"))

				// Existing mapping is preserved
				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
				}))
			})

			It("should overwrite access type when re-sharing the parent group from secondary to primary", func() {
				// First share the parent group with secondary access
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupSecondaryAccess)

				// Re-share with primary access - should overwrite
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupPrimaryAccess)},
				}))
			})

			It("should overwrite access type when re-sharing the parent group from primary to secondary", func() {
				// First share the parent group with primary access
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupPrimaryAccess)

				// Re-share with secondary access - should overwrite
				ShareAndApproveGroup(rmngContext1, rmngContext2, parentGroupID, utils.GroupSecondaryAccess)

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSecondaryAccess)},
				}))
			})

			It("should fail to re-share the same subgroup to a user that already has it", func() {
				// First share the subgroup
				ShareAndApproveSubGroup(rmngContext1, rmngContext2, parentGroupID, subGroupID)

				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
				}))

				// Re-share the SAME subgroup - approval should fail, no duplicate entry
				_, err := group.ShareSubGroup(rmngContext1, parentGroupID, subGroupID, testUser2.GetID(), auth.UserInfo{})
				Expect(err).To(BeNil())

				sharingRequests, err := group.GetMySharingRequests(rmngContext2)
				Expect(err).To(BeNil())
				Expect(sharingRequests).To(HaveLen(1))

				err = group.ApproveSharingRequest(rmngContext2, sharingRequests[0].SharingRequestID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no change to user group mapping"))

				// Existing mapping is unchanged — subgroup appears exactly once
				Expect(test_utils.QuickGetItem(user_group_db.UserGroupMappingTable, map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id": &types.AttributeValueMemberS{Value: parentGroupID},
				})).To(BeEquivalentTo(map[string]types.AttributeValue{
					"user_id":        &types.AttributeValueMemberS{Value: testUser2.GetID()},
					"group_id":       &types.AttributeValueMemberS{Value: parentGroupID},
					"sub_entity_ids": &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: subGroupID}}},
					"access_type":    &types.AttributeValueMemberS{Value: string(utils.GroupSubEntityAccess)},
				}))
			})
		})
	})
})

var _ = Describe("ListUsersForGroupOrSubGroup", func() {
	var (
		rmng_context1 *rmngctx.RmngContext
		rmng_context2 *rmngctx.RmngContext
		rmng_context3 *rmngctx.RmngContext
		testUser1     *user.User
		testUser2     *user.User
		testUser3     *user.User
		testGroup     *group.Group
		testGroup2    *group.Group
		subGroup1     *group.SubGroup
		subGroup2     *group.SubGroup
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		// Create test users
		testUser1 = user.NewUser("test-user-1")
		testUser2 = user.NewUser("test-user-2")
		testUser3 = user.NewUser("test-user-3")
		rmng_context1 = rmngctx.NewRmngContext(testUser1)
		rmng_context2 = rmngctx.NewRmngContext(testUser2)
		rmng_context3 = rmngctx.NewRmngContext(testUser3)

		// Create a group for testUser1
		var err error
		testGroup, err = group.CreateGroupForUser(rmng_context1, "Test Group")
		Expect(err).To(BeNil())

		// Create a second group for testUser1
		testGroup2, err = group.CreateGroupForUser(rmng_context1, "Test Group 2")
		Expect(err).To(BeNil())

		// Create subgroups
		subGroup1, err = group.CreateSubGroup(rmng_context1, testGroup.GroupID, "Subgroup 1")
		Expect(err).To(BeNil())
		subGroup2, err = group.CreateSubGroup(rmng_context1, testGroup.GroupID, "Subgroup 2")
		Expect(err).To(BeNil())

		// Share the group with testUser2 (primary access)
		ShareAndApproveGroup(rmng_context1, rmng_context2, testGroup.GroupID, utils.GroupPrimaryAccess)

		// Share only subGroup1 with testUser3
		ShareAndApproveSubGroup(rmng_context1, rmng_context3, testGroup.GroupID, subGroup1.SubGroupID)
	})

	It("should list all users with access to only the group when no subgroups are specified", func() {
		// List users with access to the group
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup.GroupID, []string{})
		Expect(err).To(BeNil())

		// Should include testUser1 (owner), testUser2 (primary access)
		Expect(userIDs).To(HaveLen(2))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
	})

	It("should list only users with access to specific group and subgroups when subgroups are specified", func() {
		// List users with access to subGroup1
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup.GroupID, []string{subGroup1.SubGroupID})
		Expect(err).To(BeNil())

		// Should include testUser1 (owner), testUser2 (primary access) and testUser3 (subgroup access)
		Expect(userIDs).To(HaveLen(3))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser3.GetID()))
		Expect(userIDs[testUser3.GetID()]).To(Equal(utils.GroupSubEntityAccess))

		// List users with access to subGroup1 or subGroup2
		userIDs, err = group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup.GroupID, []string{subGroup1.SubGroupID, subGroup2.SubGroupID})
		Expect(err).To(BeNil())

		// Should include testUser1 (owner), testUser2 (primary access) and testUser3 (subgroup access)
		Expect(userIDs).To(HaveLen(3))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser3.GetID()))
		Expect(userIDs[testUser3.GetID()]).To(Equal(utils.GroupSubEntityAccess))
	})

	It("should return error for non-existent group", func() {
		_, err := group.ListUsersForGroupOrSubGroup(rmng_context1, "non-existent-group", []string{})
		Expect(err).ToNot(BeNil())
	})

	It("should return not return  irrelevant users for a group", func() {
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup2.GroupID, []string{})
		Expect(err).To(BeNil())
		Expect(userIDs).To(HaveLen(1))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
	})

	It("should return only relevant users for a group when the subgroup specified has not direct users", func() {
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup.GroupID, []string{subGroup2.SubGroupID})
		Expect(err).To(BeNil())
		Expect(userIDs).To(HaveLen(2))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
	})

	It("should not include list users multiple times if more than 1 subgroup is shared with the same user", func() {
		// First, share subGroup2 with testUser3
		ShareAndApproveSubGroup(rmng_context1, rmng_context3, testGroup.GroupID, subGroup2.SubGroupID)

		// List users with access to the group
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, testGroup.GroupID, []string{subGroup1.SubGroupID, subGroup2.SubGroupID})
		Expect(err).To(BeNil())

		// Should include all three users
		Expect(userIDs).To(HaveLen(3))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser3.GetID()))
		Expect(userIDs[testUser3.GetID()]).To(Equal(utils.GroupSubEntityAccess))
	})

	It("should correctly return both primary and secondary users with their access types", func() {
		// Create a new group for this test
		testUser4 := user.NewUser("test-user-4")
		rmng_context4 := rmngctx.NewRmngContext(testUser4)
		newGroup, err := group.CreateGroupForUser(rmng_context1, "Test Group 3")
		Expect(err).To(BeNil())

		// Share with testUser2 (primary access) and testUser4 (secondary access)
		ShareAndApproveGroup(rmng_context1, rmng_context2, newGroup.GroupID, utils.GroupPrimaryAccess)
		ShareAndApproveGroup(rmng_context1, rmng_context4, newGroup.GroupID, utils.GroupSecondaryAccess)

		// List users with access to the group
		userIDs, err := group.ListUsersForGroupOrSubGroup(rmng_context1, newGroup.GroupID, []string{})
		Expect(err).To(BeNil())

		// Should include testUser1 (owner/primary), testUser2 (primary), testUser4 (secondary)
		Expect(userIDs).To(HaveLen(3))
		Expect(userIDs).To(HaveKey(testUser1.GetID()))
		Expect(userIDs[testUser1.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser2.GetID()))
		Expect(userIDs[testUser2.GetID()]).To(Equal(utils.GroupPrimaryAccess))
		Expect(userIDs).To(HaveKey(testUser4.GetID()))
		Expect(userIDs[testUser4.GetID()]).To(Equal(utils.GroupSecondaryAccess))
	})
})

func getGroupDeviceMappingItem(groupID, nodeID string) map[string]types.AttributeValue {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(group_node_db.GroupDeviceMappingTable),
		KeyConditionExpression: aws.String("group_id = :gid AND node_id = :nid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
			":nid": &types.AttributeValueMemberS{Value: nodeID},
		},
	}

	queryOutput, err := dbMock.Query(context.Background(), queryInput)
	Expect(err).To(BeNil())
	Expect(queryOutput.Items).To(HaveLen(1))

	return queryOutput.Items[0]
}

// ShareAndApproveGroup shares a group from the sharing user's context and approves the sharing request from the TobeSharedUser's context
var _ = Describe("RemoveNode (Disassociation)", func() {
	var (
		rmng_context1 *rmngctx.RmngContext
		rmng_context2 *rmngctx.RmngContext
		testUser1     *user.User
		testUser2     *user.User
		testGroup     *group.Group
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		testUser1 = user.NewUser("user-1")
		testUser2 = user.NewUser("user-2")
		rmng_context1 = rmngctx.NewRmngContext(testUser1)
		rmng_context2 = rmngctx.NewRmngContext(testUser2)

		var err error
		testGroup, err = group.CreateGroupForUser(rmng_context1, "Test Group")
		Expect(err).To(BeNil())

		// Add a node to the group
		rmng_context1.SetAllow(utils.NodeAll, "node1")
		_, err = group.AddNode(rmng_context1, testGroup.GroupID, "node1", nil)
		Expect(err).To(BeNil())
		test_utils.AssertNodeInGroup(testGroup.GroupID, "node1")
	})

	It("should fail when user is not primary", func() {
		// Share with secondary access
		ShareAndApproveGroup(rmng_context1, rmng_context2, testGroup.GroupID, utils.GroupSecondaryAccess)

		_, err := group.RemoveNode(rmng_context2, testGroup.GroupID, "node1")
		Expect(err).To(HaveOccurred())
	})

	It("should not fail when other primary users exist", func() {
		// Share with primary access
		ShareAndApproveGroup(rmng_context1, rmng_context2, testGroup.GroupID, utils.GroupPrimaryAccess)

		_, err := group.RemoveNode(rmng_context1, testGroup.GroupID, "node1")
		Expect(err).To(BeNil())
	})

	It("should fail when node is not in the specified group", func() {
		_, err := group.RemoveNode(rmng_context1, testGroup.GroupID, "nonexistent-node")
		Expect(err).To(HaveOccurred())
	})

})

var _ = Describe("DeleteGroup", func() {
	var (
		rmng_context1 *rmngctx.RmngContext
		rmng_context2 *rmngctx.RmngContext
		testUser1     *user.User
		testUser2     *user.User
		testGroup     *group.Group
	)

	BeforeEach(func() {
		test_utils.TestSetup()

		testUser1 = user.NewUser("user-1")
		testUser2 = user.NewUser("user-2")
		rmng_context1 = rmngctx.NewRmngContext(testUser1)
		rmng_context2 = rmngctx.NewRmngContext(testUser2)

		var err error
		testGroup, err = group.CreateGroupForUser(rmng_context1, "Delete Test Group")
		Expect(err).To(BeNil())
	})

	It("should not fail when other primary users exist", func() {
		// Share with primary access
		ShareAndApproveGroup(rmng_context1, rmng_context2, testGroup.GroupID, utils.GroupPrimaryAccess)

		err := group.DeleteGroup(rmng_context1, testGroup.GroupID)
		Expect(err).To(BeNil())
	})

	It("should fail when user is not primary", func() {
		// Share with secondary access
		ShareAndApproveGroup(rmng_context1, rmng_context2, testGroup.GroupID, utils.GroupSecondaryAccess)

		err := group.DeleteGroup(rmng_context2, testGroup.GroupID)
		Expect(err).To(HaveOccurred())
	})
})

func ShareAndApproveGroup(SharingUser *rmngctx.RmngContext, TobeSharedUser *rmngctx.RmngContext, groupID string, accessType utils.GroupAccessType) {
	// Share the group from the sharing user's context
	_, err := group.ShareGroup(SharingUser, groupID, TobeSharedUser.GetID(), accessType, auth.UserInfo{})
	Expect(err).To(BeNil())

	// Get the sharing request ID from the TobeSharedUser's context
	sharingRequests, err := group.GetMySharingRequests(TobeSharedUser)
	Expect(err).To(BeNil())
	Expect(sharingRequests).To(HaveLen(1))

	// Approve the sharing request from the TobeSharedUser's context
	err = group.ApproveSharingRequest(TobeSharedUser, sharingRequests[0].SharingRequestID)
	Expect(err).To(BeNil())

}

// ShareAndApproveSubGroup shares a sub-group from the sharing user's context and approves the sharing request from the TobeSharedUser's context
func ShareAndApproveSubGroup(SharingUser *rmngctx.RmngContext, TobeSharedUser *rmngctx.RmngContext, parentGroupID, subGroupID string) {
	// Share the sub-group from the sharing user's context
	_, err := group.ShareSubGroup(SharingUser, parentGroupID, subGroupID, TobeSharedUser.GetID(), auth.UserInfo{})
	Expect(err).To(BeNil())

	// Get the sharing request ID from the TobeSharedUser's context
	sharingRequests, err := group.GetMySharingRequests(TobeSharedUser)
	Expect(err).To(BeNil())
	Expect(sharingRequests).To(HaveLen(1))

	// Approve the sharing request from the TobeSharedUser's context
	err = group.ApproveSharingRequest(TobeSharedUser, sharingRequests[0].SharingRequestID)
	Expect(err).To(BeNil())
}

var _ = Describe("Matter Capability OnUserExitGroup", func() {
	var (
		testUser1    *user.User
		testUser2    *user.User
		rmngContext1 *rmngctx.RmngContext
		rmngContext2 *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		testUser1 = user.NewUser("test-user-1")
		testUser2 = user.NewUser("test-user-2")
		rmngContext1 = rmngctx.NewRmngContext(testUser1)
		rmngContext2 = rmngctx.NewRmngContext(testUser2)
	})

	Describe("Unsharing users from Matter groups", func() {
		It("should increment operate CAT ID version when secondary access user is unshared", func() {
			// Create Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmngContext1, "Matter Test Group", opts)
			Expect(err).To(BeNil())
			Expect(matterGroup).NotTo(BeNil())

			// Get initial CAT ID versions
			groupDB := group_db.NewGroupDB(rmngContext1)
			initialCapData, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			initialOperateCATID := initialCapData["group_cat_id_operate"].(string)
			initialAdminCATID := initialCapData["group_cat_id_admin"].(string)

			// Share with secondary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, matterGroup.GroupID, utils.GroupSecondaryAccess)

			// Unshare the user
			err = group.UnshareGroup(rmngContext1, matterGroup.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Get updated CAT ID versions
			updatedCapData, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			updatedOperateCATID := updatedCapData["group_cat_id_operate"].(string)
			updatedAdminCATID := updatedCapData["group_cat_id_admin"].(string)

			// Operate CAT ID should be incremented
			expectedOperateCATID, err := group.IncrementCATIDVersion(initialOperateCATID)
			Expect(err).To(BeNil())
			Expect(updatedOperateCATID).To(Equal(expectedOperateCATID))

			// Admin CAT ID should remain unchanged
			Expect(updatedAdminCATID).To(Equal(initialAdminCATID))
		})

		It("should increment admin CAT ID version when primary access user is unshared", func() {
			// Create Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmngContext1, "Matter Test Group", opts)
			Expect(err).To(BeNil())
			Expect(matterGroup).NotTo(BeNil())

			// Get initial CAT ID versions
			groupDB := group_db.NewGroupDB(rmngContext1)
			initialCapData, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			initialOperateCATID := initialCapData["group_cat_id_operate"].(string)
			initialAdminCATID := initialCapData["group_cat_id_admin"].(string)

			// Share with primary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, matterGroup.GroupID, utils.GroupPrimaryAccess)

			// Unshare the user
			err = group.UnshareGroup(rmngContext1, matterGroup.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Get updated CAT ID versions
			updatedCapData, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			updatedOperateCATID := updatedCapData["group_cat_id_operate"].(string)
			updatedAdminCATID := updatedCapData["group_cat_id_admin"].(string)

			// Admin CAT ID should be incremented
			expectedAdminCATID, err := group.IncrementCATIDVersion(initialAdminCATID)
			Expect(err).To(BeNil())
			Expect(updatedAdminCATID).To(Equal(expectedAdminCATID))

			// Operate CAT ID should remain unchanged
			Expect(updatedOperateCATID).To(Equal(initialOperateCATID))
		})

		It("should not increment CAT ID for non-matter groups", func() {
			// Create regular group (no Matter capability)
			regularGroup, err := group.CreateGroupForUser(rmngContext1, "Regular Test Group")
			Expect(err).To(BeNil())
			Expect(regularGroup).NotTo(BeNil())

			// Share with secondary access
			ShareAndApproveGroup(rmngContext1, rmngContext2, regularGroup.GroupID, utils.GroupSecondaryAccess)

			// Unshare the user - should not cause any errors
			err = group.UnshareGroup(rmngContext1, regularGroup.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())
		})

		It("should increment CAT ID multiple times for multiple unshares", func() {
			testUser3 := user.NewUser("test-user-3")
			rmngContext3 := rmngctx.NewRmngContext(testUser3)

			// Create Matter group
			opts := &group.CreateGroupOptions{
				Capabilities: []string{"matter"},
			}
			matterGroup, err := group.CreateGroupForUserWithOptions(rmngContext1, "Matter Test Group", opts)
			Expect(err).To(BeNil())

			// Get initial CAT ID
			groupDB := group_db.NewGroupDB(rmngContext1)
			initialCapData, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			initialOperateCATID := initialCapData["group_cat_id_operate"].(string)

			// Share with two users (secondary access)
			ShareAndApproveGroup(rmngContext1, rmngContext2, matterGroup.GroupID, utils.GroupSecondaryAccess)
			ShareAndApproveGroup(rmngContext1, rmngContext3, matterGroup.GroupID, utils.GroupSecondaryAccess)

			// Unshare first user
			err = group.UnshareGroup(rmngContext1, matterGroup.GroupID, testUser2.GetID())
			Expect(err).To(BeNil())

			// Verify first increment
			capData1, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			expectedCATID1, _ := group.IncrementCATIDVersion(initialOperateCATID)
			Expect(capData1["group_cat_id_operate"]).To(Equal(expectedCATID1))

			// Unshare second user
			err = group.UnshareGroup(rmngContext1, matterGroup.GroupID, testUser3.GetID())
			Expect(err).To(BeNil())

			// Verify second increment
			capData2, err := groupDB.GetCapabilityData(matterGroup.GroupID, "matter")
			Expect(err).To(BeNil())
			expectedCATID2, _ := group.IncrementCATIDVersion(expectedCATID1)
			Expect(capData2["group_cat_id_operate"]).To(Equal(expectedCATID2))
		})
	})
})
