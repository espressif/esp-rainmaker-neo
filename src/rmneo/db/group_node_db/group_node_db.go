// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The group_device_mapping table manages the relationships between groups and nodes (devices):

Table Name: group_device_mapping
Primary Key: group_id (Partition Key), node_id (Sort Key)

Schema:
- group_id (String): Partition key, identifies the group
- node_id (String): Sort key, identifies the node/device
- subgrp1 (String): Optional, ID of first subgroup the node belongs to
- subgrp2 (String): Optional, ID of second subgroup the node belongs to
- subgrp3 (String): Optional, ID of third subgroup the node belongs to
- capabilities (List of Strings): Optional, list of capability names enabled for this node within the group.
  Note: This represents the capabilities of the node within the group, not the group itself. The group may have its own capabilities, and the node may have different ones. This data could equally be stored in the node_details table, but since a node can belong
  to only a single group, this row effectively serves as an extension of the node row in node_details. Also, pure matter nodes are not stored in the node_details table as they are effectively never registered (certs).
- alias (String): Optional, alias for the node (also used as GSI partition key)

Secondary Indexes:
- group_device_mapping_node_id_index:
  - Partition Key: node_id
  - Projects all attributes
  - Used to find which group a node belongs to
- group_device_mapping_alias_index:
  - Partition Key: alias
  - Projects node_id only
  - Used to resolve alias to node_id

Example Records:
1. Node in main group only:
   {
     "group_id": "abc123",
     "node_id": "device789",
   }

2. Node in main group and subgroups:
   {
     "group_id": "abc123",
     "node_id": "device789",
     "subgrp1": "x56",
     "subgrp2": "pqr"
   }

3. Node with capability:
   {
     "group_id": "abc123",
     "node_id": "device789",
     "capabilities": ["capability_name"],
     "alias": "f8c3de3d-1fea-4d7c-a8b0-29f63c4c3454"
   }

Query Patterns:
1. Get node's group:
   - Use node_id_index to find the group(s) a node belongs to
   - Single record expected as node can only be in one group

2. List group's nodes:
   - Query by group_id to get all nodes in a group
   - Also returns subgroup memberships

3. Get specific group-node mapping:
   - Use both group_id and node_id for exact match

Limitations:
- A node can belong to maximum 3 subgroups (subgrp1, subgrp2, subgrp3)
- A node can only be in one main group at a time

Access Control:
- Adding node to group requires permissions on both group and node
- Removing node from group only requires node permission - because ownership of the node is transferred to the new user
- Listing nodes requires group permission
*/

package group_node_db

import (
	"fmt"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	GroupDeviceMappingTable         = "rmng-group-node-assoc"
	GroupDeviceMappingByNodeIDIndex = "rmng-group-node-assoc-by-node-id"
	GroupDeviceMappingByAliasIndex  = "rmng-group-node-assoc-by-alias"
)

type GroupNodeDB struct {
	dbcore.DB
}

type GroupNode struct {
	GroupID      string   `dynamodbav:"group_id"`
	NodeID       string   `dynamodbav:"node_id"`
	SubGrp1      string   `dynamodbav:"subgrp1,omitempty"`
	SubGrp2      string   `dynamodbav:"subgrp2,omitempty"`
	SubGrp3      string   `dynamodbav:"subgrp3,omitempty"`
	Capabilities []string `dynamodbav:"capabilities,omitempty"`
	Alias        string   `dynamodbav:"alias,omitempty"`
	// CapabilityData holds per-capability detail (capability name -> detail map)
	// supplied by the optional module that owns the capability. Core stores and
	// returns it verbatim in group-info responses and does not interpret it.
	CapabilityData map[string]map[string]interface{} `dynamodbav:"capability_data,omitempty"`
}

// NodesGroups is the groups and sub-groups that a node is part of
type NodesGroups struct {
	Group     string
	SubGroups []string
}

// ToNodesGroups is the node's group and subgroups as carried by its own row. Exported because a
// caller that has already fetched the row can hand this to node.Node instead of letting it
// re-read the same row through the by-node-id index.
func (gn *GroupNode) ToNodesGroups() NodesGroups {
	ng := NodesGroups{Group: gn.GroupID}
	for _, sg := range []string{gn.SubGrp1, gn.SubGrp2, gn.SubGrp3} {
		if sg != "" {
			ng.SubGroups = append(ng.SubGroups, sg)
		}
	}
	return ng
}

// GetNodeIDs returns the node IDs from a map of GroupNode entries
func GetNodeIDs(entries map[string]*GroupNode) []string {
	nodeIDs := make([]string, 0, len(entries))
	for nodeID := range entries {
		nodeIDs = append(nodeIDs, nodeID)
	}

	return nodeIDs
}

func NewGroupNodeDB(ctx *rmngctx.RmngContext) *GroupNodeDB {
	return &GroupNodeDB{
		DB: *dbcore.NewDB(ctx),
	}
}

// AddNode adds a node to a group. capabilities (e.g. ["rmng"], ["matter"]) are stored on
// the group-node row in the same write so the node is tagged atomically; pass nil for none.
func (g *GroupNodeDB) AddNode(groupID string, nodeID string, capabilities []string) error {
	if err := g.IsAuthorized(utils.NodeEditGroups, nodeID); err != nil {
		return err
	}
	if err := g.IsAuthorized(utils.GroupEditNodes, groupID); err != nil {
		return err
	}

	item := map[string]types.AttributeValue{
		"group_id": &types.AttributeValueMemberS{Value: groupID},
		"node_id":  &types.AttributeValueMemberS{Value: nodeID},
	}
	if len(capabilities) > 0 {
		capList := make([]types.AttributeValue, 0, len(capabilities))
		for _, capability := range capabilities {
			capList = append(capList, &types.AttributeValueMemberS{Value: capability})
		}
		item["capabilities"] = &types.AttributeValueMemberL{Value: capList}
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(GroupDeviceMappingTable),
		Item:      item,
	}
	_, err := g.PutItem(g.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to add node to group in DynamoDB")
	}
	return nil
}

func (g *GroupNodeDB) RemoveNode(groupID string, nodeID string) error {
	if err := g.IsAuthorized(utils.NodeEditGroups, nodeID); err != nil {
		return err
	}
	// We purposefully do not check for permissions on the group here, as we want to allow
	// the user to remove the node from the group even if they do not have permissions to
	// access the group. This is typically possible in scenarios where the device was reset-to-factory
	// and hence the ownership of the node is transferred to the new user. The new user may not
	// have permissions to access the old user's groups.

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(GroupDeviceMappingTable),
		Key: map[string]types.AttributeValue{
			"group_id": &types.AttributeValueMemberS{Value: groupID},
			"node_id":  &types.AttributeValueMemberS{Value: nodeID},
		},
	}
	_, err := g.DeleteItem(g.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to remove node from group in DynamoDB")
	}
	return nil
}

func (g *GroupNodeDB) GetNodesGroup(nodeID string) (NodesGroups, error) {
	groupNode, err := g.GetGroupNodeByNodeID(nodeID)
	if err != nil {
		return NodesGroups{}, err
	}
	if groupNode == nil {
		return NodesGroups{}, nil
	}
	return groupNode.ToNodesGroups(), nil
}

func (g *GroupNodeDB) GetGroupNodeByNodeID(nodeID string) (*GroupNode, error) {
	if err := g.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, err
	}
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(GroupDeviceMappingTable),
		IndexName:              aws.String(GroupDeviceMappingByNodeIDIndex),
		KeyConditionExpression: aws.String("node_id = :nid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":nid": &types.AttributeValueMemberS{Value: nodeID},
		},
	}

	queryOutput, err := g.Query(g.Ctx.Context, queryInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query group_device_mapping in DynamoDB")
	}

	if len(queryOutput.Items) == 0 {
		return nil, nil
	}

	var groupNode GroupNode
	err = attributevalue.UnmarshalMap(queryOutput.Items[0], &groupNode)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal group node")
	}

	return &groupNode, nil
}

func (g *GroupNodeDB) getGroupNodeEntry(groupID string, nodeID string) (map[string]types.AttributeValue, error) {
	// Why is this check not for GroupListSubEntities?
	// Because, this function doesn't bother about sub-groups. If such an action needs to be triggered
	// for a node that is accessible via a sub-group, reimplement the logic to check for sub-group
	// permissions
	if err := g.IsAuthorized(utils.GroupGet, groupID); err != nil {
		return nil, err
	}
	// No Need to check for node permissions, as group permissions automatically grant access to the node
	// if the node is in the group
	input := &dynamodb.GetItemInput{
		TableName: aws.String(GroupDeviceMappingTable),
		Key: map[string]types.AttributeValue{
			"group_id": &types.AttributeValueMemberS{Value: groupID},
			"node_id":  &types.AttributeValueMemberS{Value: nodeID},
		},
	}

	result, err := g.GetItem(g.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get item from group_device_mapping in DynamoDB")
	}
	if result.Item != nil {
		g.Ctx.SetAllow(utils.NodeAll, nodeID)
	}

	return result.Item, nil
}

func (g *GroupNodeDB) GetGroupNode(groupID string, nodeID string) (*GroupNode, error) {
	if err := g.IsAuthorized(utils.GroupListSubEntities, groupID); err != nil {
		return nil, err
	}
	item, err := g.getGroupNodeEntry(groupID, nodeID)
	if err != nil {
		return nil, err
	}

	if item == nil {
		return nil, rmerror.NewRMError(nil, "node not found in group")
	}

	var groupNode GroupNode
	err = attributevalue.UnmarshalMap(item, &groupNode)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal group node")
	}

	return &groupNode, nil
}

func (g *GroupNodeDB) GetGroupNodes(groupID string, nodeID string) (NodesGroups, error) {
	groupNode, err := g.GetGroupNode(groupID, nodeID)
	if err != nil {
		return NodesGroups{}, err
	}
	if groupNode == nil {
		return NodesGroups{}, nil
	}
	return groupNode.ToNodesGroups(), nil
}

// ListGroupNodesWithDBEntry returns nodes, subgroups, and the entire group_device_mapping Item for all nodes in the group.
// The return values are:
// 1. map[string]*GroupNode: the entire Item for each node in the group (keyed by node_id)
// 2. map[string]map[string]*GroupNode: the entire Item for each node in each sub-group (keyed by subgroup_id, then node_id)
// 3. error: the error that occurred
func (g *GroupNodeDB) ListGroupNodesWithDBEntry(groupID string) (map[string]*GroupNode, map[string]map[string]*GroupNode, error) {
	if err := g.IsAuthorized(utils.GroupListSubEntities, groupID); err != nil {
		return nil, nil, err
	}
	input := &dynamodb.QueryInput{
		TableName:              aws.String(GroupDeviceMappingTable),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
	}

	subgrpNodes := make(map[string]map[string]*GroupNode)
	grpNodes := make(map[string]*GroupNode)

	// Paginated: this listing also decides which nodes get a SetAllow below, so a truncated
	// page would drop nodes from the response and deny access to them in the same breath.
	err := g.QueryPaginated(g.Ctx.Context, input, func(item map[string]types.AttributeValue) error {
		var groupNode GroupNode
		if err := attributevalue.UnmarshalMap(item, &groupNode); err != nil || groupNode.NodeID == "" {
			// node_id is the mapping's key; skip a malformed item rather than panic on the auth path.
			return nil
		}
		grpNodes[groupNode.NodeID] = &groupNode

		for _, subgrpID := range []string{groupNode.SubGrp1, groupNode.SubGrp2, groupNode.SubGrp3} {
			if subgrpID == "" {
				continue
			}
			if subgrpNodes[subgrpID] == nil {
				subgrpNodes[subgrpID] = make(map[string]*GroupNode)
			}
			subgrpNodes[subgrpID][groupNode.NodeID] = grpNodes[groupNode.NodeID]
		}
		return nil
	})
	if err != nil {
		return nil, nil, rmerror.NewRMError(err, "failed to query group_device_mapping in DynamoDB")
	}

	isLimitedAccess := false
	if err := g.IsAuthorized(utils.GroupGet, groupID); err != nil {
		// This implies the user does not have full access to the parent group, as we could list subentities, but not do a GroupGet on the parent group
		isLimitedAccess = true
	}

	// Filter out the sub-groups that the user does not have access to
	for subgrpID := range subgrpNodes {
		match, err := g.Ctx.IsConditionMatch(utils.GroupListSubEntities, groupID, subgrpID)
		if err != nil {
			return nil, nil, err
		}
		if !match {
			delete(subgrpNodes, subgrpID)
		}
	}

	if isLimitedAccess {
		// Only include nodes that are in accessible sub-groups
		filteredEntries := make(map[string]*GroupNode)
		for _, nodes := range subgrpNodes {
			for nodeID, entry := range nodes {
				filteredEntries[nodeID] = entry
			}
		}
		grpNodes = filteredEntries
	}

	// Set allow for the nodes that are accessible to the user
	for nodeID := range grpNodes {
		g.Ctx.SetAllow(utils.NodeAll, nodeID)
	}
	return grpNodes, subgrpNodes, nil
}

type SubGroupOperationType int

const (
	SubGroupOperationTypeAdd SubGroupOperationType = iota
	SubGroupOperationTypeRemove
)

// UpdateSubGroup updates the subgroup of a node in the group_device_mapping table
func (g *GroupNodeDB) UpdateSubGroup(groupID, nodeID, subGroupID string, operationType SubGroupOperationType) (NodesGroups, error) {
	if err := g.IsAuthorized(utils.GroupEditNodes, groupID); err != nil {
		return NodesGroups{}, err
	}

	groupNodeEntry, err := g.getGroupNodeEntry(groupID, nodeID)
	if err != nil {
		return NodesGroups{}, err
	}
	if groupNodeEntry == nil {
		return NodesGroups{}, rmerror.NewRMError(nil, "node is not in the group")
	}

	// Build update expression based on operation type
	var updateExpression string
	var expressionAttributeValues map[string]types.AttributeValue
	var expressionAttributeNames map[string]string

	if operationType == SubGroupOperationTypeRemove {
		updateExpression, expressionAttributeNames = getRemoveSubGroupExpression(groupNodeEntry, subGroupID)
		if updateExpression == "" {
			return NodesGroups{}, rmerror.NewRMError(nil, "node is not in the specified subgroup")
		}
	} else {
		var alreadyPresent bool
		updateExpression, expressionAttributeNames, expressionAttributeValues, alreadyPresent = getAddSubGroupExpression(groupNodeEntry, subGroupID)
		if alreadyPresent {
			// Idempotent: the node is already in this subgroup, so report the current state rather than writing a duplicate tag.
			var current GroupNode
			if err := attributevalue.UnmarshalMap(groupNodeEntry, &current); err != nil {
				return NodesGroups{}, rmerror.NewRMError(err, "failed to unmarshal group node")
			}
			return current.ToNodesGroups(), nil
		}
		if updateExpression == "" {
			return NodesGroups{}, rmerror.NewRMError(nil, "node is already in 3 subgroups")
		}
	}

	updateItemInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(GroupDeviceMappingTable),
		Key: map[string]types.AttributeValue{
			"group_id": &types.AttributeValueMemberS{Value: groupID},
			"node_id":  &types.AttributeValueMemberS{Value: nodeID},
		},
		UpdateExpression:          &updateExpression,
		ExpressionAttributeValues: expressionAttributeValues,
		ExpressionAttributeNames:  expressionAttributeNames,
		ReturnValues:              types.ReturnValueAllNew,
	}

	updatedResult, err := g.UpdateItem(g.Ctx.Context, updateItemInput)
	if err != nil {
		return NodesGroups{}, rmerror.NewRMError(err, "failed to update item in group_device_mapping")
	}
	var groupNode GroupNode
	if err := attributevalue.UnmarshalMap(updatedResult.Attributes, &groupNode); err != nil {
		return NodesGroups{}, rmerror.NewRMError(err, "failed to unmarshal updated group node")
	}
	return groupNode.ToNodesGroups(), nil
}

func getRemoveSubGroupExpression(item map[string]types.AttributeValue, subGroupID string) (string, map[string]string) {
	// Find and remove the specified subgroup
	subgroups := []string{"subgrp1", "subgrp2", "subgrp3"}
	for _, subgroup := range subgroups {
		if s, ok := item[subgroup].(*types.AttributeValueMemberS); ok && s.Value == subGroupID {
			return fmt.Sprintf("REMOVE #%s", subgroup), map[string]string{
				fmt.Sprintf("#%s", subgroup): subgroup,
			}
		}
	}
	return "", nil
}

// getAddSubGroupExpression puts subGroupID in the first free slot. alreadyPresent is true when the node is already tagged with it, in which case there is nothing to write — a second tag for the same subgroup would survive a removal, which clears only the slot it finds first.
func getAddSubGroupExpression(item map[string]types.AttributeValue, subGroupID string) (expr string, names map[string]string, values map[string]types.AttributeValue, alreadyPresent bool) {
	subgroups := []string{"subgrp1", "subgrp2", "subgrp3"}
	for _, subgroup := range subgroups {
		if s, ok := item[subgroup].(*types.AttributeValueMemberS); ok && s.Value == subGroupID {
			return "", nil, nil, true
		}
	}

	// Add the subgroup to first available slot
	for _, subgroup := range subgroups {
		if _, exists := item[subgroup]; !exists {
			updateExpression := fmt.Sprintf("SET #%s = :%s", subgroup, subgroup)
			expressionAttributeValues := map[string]types.AttributeValue{
				fmt.Sprintf(":%s", subgroup): &types.AttributeValueMemberS{Value: subGroupID},
			}
			expressionAttributeNames := map[string]string{
				fmt.Sprintf("#%s", subgroup): subgroup,
			}
			return updateExpression, expressionAttributeNames, expressionAttributeValues, false
		}
	}
	return "", nil, nil, false
}

// GetNodeIDByAlias resolves an alias to a node_id by querying the group_device_mapping table.
// Uses the alias GSI for efficient lookups.
func (g *GroupNodeDB) GetNodeIDByAlias(alias string) (string, error) {
	keyCondition := expression.Key("alias").Equal(expression.Value(alias))

	expr, err := expression.NewBuilder().
		WithKeyCondition(keyCondition).
		Build()
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to build expression")
	}

	input := &dynamodb.QueryInput{
		TableName:                 aws.String(GroupDeviceMappingTable),
		IndexName:                 aws.String(GroupDeviceMappingByAliasIndex),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}

	result, err := g.Query(g.Ctx.Context, input)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to query group_device_mapping by alias")
	}

	if len(result.Items) == 0 {
		return "", nil
	}

	nodeID, ok := dbcore.GetStringAttr(result.Items[0], "node_id")
	if !ok {
		return "", rmerror.NewRMError(nil, "node_id missing from group_device_mapping item")
	}
	return nodeID, nil
}

// UpdateNodeCapability updates capability data for a node in the group_device_mapping table.
// It appends capabilityName to the capabilities list and sets additional attributes from attributeUpdates.
// attributeUpdates holds attribute names to SET; use JSON-compatible values (e.g. map from json.Unmarshal).
// Keys group_id, node_id, and capabilities are ignored in attributeUpdates.
func (g *GroupNodeDB) UpdateNodeCapability(groupID, nodeID, capabilityName string, attributeUpdates map[string]interface{}) error {
	if err := g.IsAuthorized(utils.NodeEditGroups, nodeID); err != nil {
		return err
	}
	if err := g.IsAuthorized(utils.GroupEditNodes, groupID); err != nil {
		return err
	}

	emptyList := &types.AttributeValueMemberL{Value: []types.AttributeValue{}}
	capNameList := &types.AttributeValueMemberL{Value: []types.AttributeValue{&types.AttributeValueMemberS{Value: capabilityName}}}
	updateExpr := expression.Set(
		expression.Name("capabilities"),
		expression.ListAppend(
			expression.IfNotExists(expression.Name("capabilities"), expression.Value(emptyList)),
			expression.Value(capNameList),
		),
	)

	for attrName, val := range attributeUpdates {
		av, err := attributevalue.Marshal(val)
		if err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to marshal attribute %q", attrName))
		}
		updateExpr = updateExpr.Set(expression.Name(attrName), expression.Value(av))
	}

	conditionExpr := expression.And(
		expression.AttributeExists(expression.Name("group_id")),
		expression.AttributeExists(expression.Name("node_id")),
	)

	expr, err := expression.NewBuilder().
		WithUpdate(updateExpr).
		WithCondition(conditionExpr).
		Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build expressions")
	}

	updateItemInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(GroupDeviceMappingTable),
		Key: map[string]types.AttributeValue{
			"group_id": &types.AttributeValueMemberS{Value: groupID},
			"node_id":  &types.AttributeValueMemberS{Value: nodeID},
		},
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}

	_, err = g.UpdateItem(g.Ctx.Context, updateItemInput)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update node capability in DynamoDB")
	}

	return nil
}
