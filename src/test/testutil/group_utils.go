// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"sort"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/gomega"
)

func ManuallyAddNodeToGroup(ctx context.Context, groupID, nodeID string) {
	ManuallyAddNodeToGroupWithCapabilities(ctx, groupID, nodeID)
}

// ManuallyAddNodeToGroupWithCapabilities adds a group-node row tagged with capabilities.
func ManuallyAddNodeToGroupWithCapabilities(ctx context.Context, groupID, nodeID string, capabilities ...string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	groupNodeItem := map[string]types.AttributeValue{
		"group_id": &types.AttributeValueMemberS{Value: groupID},
		"node_id":  &types.AttributeValueMemberS{Value: nodeID},
	}
	if len(capabilities) > 0 {
		capList := make([]types.AttributeValue, 0, len(capabilities))
		for _, capability := range capabilities {
			capList = append(capList, &types.AttributeValueMemberS{Value: capability})
		}
		groupNodeItem["capabilities"] = &types.AttributeValueMemberL{Value: capList}
	}
	dbMock.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(group_node_db.GroupDeviceMappingTable),
		Item:      groupNodeItem,
	})

	// Add to node_groups table
	dbMock.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String("node_groups"),
		Item: map[string]types.AttributeValue{
			"node_id": &types.AttributeValueMemberS{Value: nodeID},
			"group":   &types.AttributeValueMemberS{Value: groupID},
			"sub_groups": &types.AttributeValueMemberL{
				Value: []types.AttributeValue{},
			},
		},
	})
}

// AssertNodeInGroup asserts the full group-node row for nodeID in groupID. Pass the
// expected capability tags (e.g. "rmng", "matter"); pass none for a row stored without
// a capabilities attribute (e.g. via ManuallyAddNodeToGroup).
func AssertNodeInGroup(groupID, nodeID string, capabilities ...string) {
	expected := map[string]types.AttributeValue{
		"group_id": &types.AttributeValueMemberS{Value: groupID},
		"node_id":  &types.AttributeValueMemberS{Value: nodeID},
	}
	if len(capabilities) > 0 {
		capList := make([]types.AttributeValue, 0, len(capabilities))
		for _, capability := range capabilities {
			capList = append(capList, &types.AttributeValueMemberS{Value: capability})
		}
		expected["capabilities"] = &types.AttributeValueMemberL{Value: capList}
	}
	AssertRowInDB(group_node_db.GroupDeviceMappingTable, expected)
}

func AssertNodeNotInGroup(groupID, nodeID string) {
	AssertRowNotInDB(group_node_db.GroupDeviceMappingTable, map[string]types.AttributeValue{
		"group_id": &types.AttributeValueMemberS{Value: groupID},
		"node_id":  &types.AttributeValueMemberS{Value: nodeID},
	})
}

// AssertNodeNotInSubgroup checks if the given node_id is NOT part of the specified subgroup
func AssertNodeNotInSubgroup(groupID, nodeID, subGroupID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	// Check if node exists in group_device_mapping with the specific subgroup
	found := false
	dbMock.ForEachRow(group_node_db.GroupDeviceMappingTable, func(item map[string]types.AttributeValue) error {
		if groupIDVal, ok := item["group_id"].(*types.AttributeValueMemberS); ok && groupIDVal.Value == groupID {
			if nodeIDVal, ok := item["node_id"].(*types.AttributeValueMemberS); ok && nodeIDVal.Value == nodeID {
				// Check if node is in any of the subgroup fields
				if subgrp1, ok := item["subgrp1"].(*types.AttributeValueMemberS); ok && subgrp1.Value == subGroupID {
					found = true
					return nil
				}
				if subgrp2, ok := item["subgrp2"].(*types.AttributeValueMemberS); ok && subgrp2.Value == subGroupID {
					found = true
					return nil
				}
				if subgrp3, ok := item["subgrp3"].(*types.AttributeValueMemberS); ok && subgrp3.Value == subGroupID {
					found = true
					return nil
				}
			}
		}
		return nil
	})

	Expect(found).To(BeFalse(), fmt.Sprintf("Node %s should not be in subgroup %s of group %s", nodeID, subGroupID, groupID))
}

// AssertShadowDeleted checks if the shadow for the given node and groups is deleted
func AssertShadowDeleted(nodeID string, groups group_node_db.NodesGroups) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	shadowName := node.GetShadowNameForNodeGroups(groups)

	// Check if shadow exists in the mock
	if iotDataClient.Shadows[nodeID] != nil {
		_, exists := iotDataClient.Shadows[nodeID][shadowName]
		Expect(exists).To(BeFalse(), fmt.Sprintf("Shadow '%s' for node '%s' should be deleted but still exists", shadowName, nodeID))
	}
}

// RegisterIoTThing registers an IoT thing in the mock so attribute operations work.
func RegisterIoTThing(nodeID string) {
	iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
	iotClient.Things[nodeID] = mock.Things{
		Name:           nodeID,
		CertificateIds: []string{},
		Groups:         []string{},
		Attributes:     make(map[string]string),
	}
}

// AssertGroupIDAttribute checks that the IoT thing group_id attribute equals the expected value.
func AssertGroupIDAttribute(nodeID, expectedGroupID string) {
	iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
	attrVal, _ := iotClient.GetThingAttributeDirect(nodeID, "group_id")
	Expect(attrVal).To(Equal(expectedGroupID),
		fmt.Sprintf("Node '%s' group_id attribute should be '%s', got '%s'", nodeID, expectedGroupID, attrVal))
}

// AssertGroupIDAttributeCleared checks that the IoT thing group_id attribute is empty.
func AssertGroupIDAttributeCleared(nodeID string) {
	iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
	attrVal, _ := iotClient.GetThingAttributeDirect(nodeID, "group_id")
	Expect(attrVal).To(Equal(""),
		fmt.Sprintf("Node '%s' group_id attribute should be cleared after disassociation", nodeID))
}

// AssertUserTagsCleared checks that the iparams shadow exists but has no user tags.
func AssertUserTagsCleared(nodeID string) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)

	Expect(iotDataClient.Shadows[nodeID]).To(HaveKey("iparams"),
		fmt.Sprintf("Node '%s' should still have an iparams shadow after disassociation", nodeID))

	payload := iotDataClient.Shadows[nodeID]["iparams"]
	var shadow node.IoTNodeShadow
	err := json.Unmarshal(payload, &shadow)
	Expect(err).To(BeNil())

	if shadow.State != nil && shadow.State.Reported != nil &&
		shadow.State.Reported.Data != nil && shadow.State.Reported.Data.User != nil {
		Expect(shadow.State.Reported.Data.User.Tags).To(BeNil(),
			fmt.Sprintf("Node '%s' iparams user tags should be cleared", nodeID))
	}
}

func GetGroupIDFromName(groupName string) (string, error) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	var groupID string
	found := false

	dbMock.ForEachRow(group_db.GroupsTable, func(item map[string]types.AttributeValue) error {
		if name, ok := item["group_name"].(*types.AttributeValueMemberS); ok && name.Value == groupName {
			if id, ok := item["group_id"].(*types.AttributeValueMemberS); ok {
				groupID = id.Value
				found = true
				return nil // Stop iterating once we find the group
			}
		}
		return nil
	})

	if !found {
		return "", rmerror.NewRMError(nil, "group not found")
	}

	return groupID, nil
}

// AssertAutomationExists checks that an automation with the given ID exists for the group.
func AssertAutomationExists(groupID, automationID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	found := false
	dbMock.ForEachRow(automation_db.AutomationsTable, func(item map[string]types.AttributeValue) error {
		gid, gok := item["group_id"].(*types.AttributeValueMemberS)
		aid, aok := item["automation_id"].(*types.AttributeValueMemberS)
		if gok && aok && gid.Value == groupID && aid.Value == automationID {
			found = true
		}
		return nil
	})
	Expect(found).To(BeTrue(), fmt.Sprintf("Automation '%s' should exist for group '%s'", automationID, groupID))
}

// AssertAutomationNotExists checks that an automation with the given ID does NOT exist for the group.
func AssertAutomationNotExists(groupID, automationID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	found := false
	dbMock.ForEachRow(automation_db.AutomationsTable, func(item map[string]types.AttributeValue) error {
		gid, gok := item["group_id"].(*types.AttributeValueMemberS)
		aid, aok := item["automation_id"].(*types.AttributeValueMemberS)
		if gok && aok && gid.Value == groupID && aid.Value == automationID {
			found = true
		}
		return nil
	})
	Expect(found).To(BeFalse(), fmt.Sprintf("Automation '%s' should NOT exist for group '%s'", automationID, groupID))
}

// AssertNoAutomationsForGroup checks that no automations exist for the given group.
func AssertNoAutomationsForGroup(groupID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	count := 0
	dbMock.ForEachRow(automation_db.AutomationsTable, func(item map[string]types.AttributeValue) error {
		gid, ok := item["group_id"].(*types.AttributeValueMemberS)
		if ok && gid.Value == groupID {
			count++
		}
		return nil
	})
	Expect(count).To(Equal(0), fmt.Sprintf("No automations should exist for group '%s', but found %d", groupID, count))
}

// AssertNodeDataResetInvoked checks that the node_data_reset Lambda was invoked
// with a payload whose node_ids list contains nodeID and whose old_group_id matches.
func AssertNodeDataResetInvoked(functionName, nodeID, oldGroupID string) {
	lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
	found := false
	for _, call := range lambdaMock.InvokeCalls {
		if call.FunctionName != nil && *call.FunctionName == functionName {
			var p node.NodeDataResetEvent
			if err := json.Unmarshal(call.Payload, &p); err == nil {
				if p.OldGroupID == oldGroupID && sliceContains(p.NodeIDs, nodeID) {
					found = true
					break
				}
			}
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf(
		"node_data_reset Lambda should have been invoked for node '%s' with old_group_id '%s'", nodeID, oldGroupID))
}

// AssertNodeDataResetInvokedWithGroupDelete is like AssertNodeDataResetInvoked but also
// verifies the group_delete flag is true.
func AssertNodeDataResetInvokedWithGroupDelete(functionName, oldGroupID string, expectedNodeIDs []string) {
	lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
	found := false
	for _, call := range lambdaMock.InvokeCalls {
		if call.FunctionName != nil && *call.FunctionName == functionName {
			var p node.NodeDataResetEvent
			if err := json.Unmarshal(call.Payload, &p); err == nil {
				if p.OldGroupID == oldGroupID && p.GroupDelete {
					// Check all expected node IDs are present
					allFound := true
					for _, nid := range expectedNodeIDs {
						if !sliceContains(p.NodeIDs, nid) {
							allFound = false
							break
						}
					}
					if allFound {
						found = true
						break
					}
				}
			}
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf(
		"node_data_reset Lambda should have been invoked with group_delete=true for group '%s' with nodes %v", oldGroupID, expectedNodeIDs))
}

func sliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// AssertNoUserGroupMappingsForGroup checks that no user_group_mapping entries exist for the group.
func AssertNoUserGroupMappingsForGroup(groupID string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	count := 0
	dbMock.ForEachRow(user_group_db.UserGroupMappingTable, func(item map[string]types.AttributeValue) error {
		gid, ok := item["group_id"].(*types.AttributeValueMemberS)
		if ok && gid.Value == groupID {
			count++
		}
		return nil
	})
	Expect(count).To(Equal(0), fmt.Sprintf("No user_group_mapping entries should exist for group '%s', but found %d", groupID, count))
}

// AssertEmptyGetGroupInfoNotification checks that an empty getGroupInfo notification was sent to the node.
func AssertEmptyGetGroupInfoNotification(nodeID string) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	topic := "rainmaker/nodes/" + nodeID + "/from_cloud"
	found := false
	for _, call := range iotDataClient.PublishCalls {
		if call.Topic != nil && *call.Topic == topic {
			payloadStr := string(call.Payload)
			if strings.Contains(payloadStr, `"getGroupInfo":{}`) ||
				(strings.Contains(payloadStr, `"pgrp":""`) && strings.Contains(payloadStr, `"subgrps":[]`)) {
				found = true
				break
			}
		}
	}
	Expect(found).To(BeTrue(), fmt.Sprintf(
		"Empty getGroupInfo notification should have been sent to node '%s'", nodeID))
}

// SetupNodeServiceData writes schedule or trigger data into the node_details table
// so that deletion can be verified later.
func SetupNodeServiceData(nodeID, serviceName string, data interface{}) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	// Check if node_details row already exists
	getResult, _ := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(node_details_db.NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			"node_id": &types.AttributeValueMemberS{Value: nodeID},
		},
	})

	item := map[string]types.AttributeValue{
		"node_id": &types.AttributeValueMemberS{Value: nodeID},
	}
	if getResult != nil && getResult.Item != nil {
		item = getResult.Item
	}

	dataJSON, _ := json.Marshal(data)
	var attrVal types.AttributeValue
	json.Unmarshal(dataJSON, &attrVal)
	// Store as a map attribute
	item[serviceName] = &types.AttributeValueMemberS{Value: string(dataJSON)}

	dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(node_details_db.NodeDetailsTable),
		Item:      item,
	})
}

// AssertNodeServiceDataDeleted checks that a node service field (e.g. "schedule", "trigger")
// has been removed from the node_details table.
func AssertNodeServiceDataDeleted(nodeID, serviceName string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	result, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(node_details_db.NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			"node_id": &types.AttributeValueMemberS{Value: nodeID},
		},
	})
	Expect(err).To(BeNil())

	if result.Item != nil {
		_, exists := result.Item[serviceName]
		Expect(exists).To(BeFalse(), fmt.Sprintf(
			"Service '%s' data should have been deleted from node_details for node '%s'", serviceName, nodeID))
	}
}

// AssertNodeServiceDataExists checks that a node service field (e.g. "schedule", "trigger")
// still exists in the node_details table.
func AssertNodeServiceDataExists(nodeID, serviceName string) {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	result, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(node_details_db.NodeDetailsTable),
		Key: map[string]types.AttributeValue{
			"node_id": &types.AttributeValueMemberS{Value: nodeID},
		},
	})
	Expect(err).To(BeNil())
	Expect(result.Item).NotTo(BeNil(), fmt.Sprintf(
		"node_details row should exist for node '%s'", nodeID))
	_, exists := result.Item[serviceName]
	Expect(exists).To(BeTrue(), fmt.Sprintf(
		"Service '%s' data should still exist in node_details for node '%s'", serviceName, nodeID))
}

func SortGroup(group *group.Group) {
	sort.Slice(group.SubGroups, func(i, j int) bool {
		return group.SubGroups[i].SubGroupID < group.SubGroups[j].SubGroupID
	})
}
