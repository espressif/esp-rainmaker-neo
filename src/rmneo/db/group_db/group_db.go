// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The groups table structure is designed to store information groups and their subgroups:

Table Name: groups
Primary Key: group_id (Partition Key), sub_group_id (Sort Key)

Schema:
- group_id (String): Partition key, uniquely identifies a group
- sub_group_id (String): Sort key, identifies subgroups within a group
- group_name (String): Name of the group or subgroup
- capabilities (List of Strings, optional): List of capability names enabled for this group (e.g., ["matter"]).
  Only present on main group entries (sub_group_id = "NONE").
- cap_<name> (String, optional): JSON-encoded capability-specific data. Each capability stores its data
  in a separate column named "cap_" followed by the capability name. Only present on main group entries.

Special Values:
- sub_group_id = "NONE": Represents the main group entry
- sub_group_id = "<id>": Represents a subgroup within the main group

Example Records:
1. Main Group (no capabilities):
   {
     "group_id": "abc123",
     "sub_group_id": "NONE",
     "group_name": "Living Room"
   }

2. Main Group with Matter capability:
   {
     "group_id": "abc123",
     "sub_group_id": "NONE",
     "group_name": "Smart Home",
     "capabilities": ["matter"],
     "cap_matter": "{\"fabric_id\":\"6162633132330000\",\"root_ca\":\"-----BEGIN CERTIFICATE-----...\",\"root_ca_priv_key\":\"-----BEGIN EC PRIVATE KEY-----...\",\"ipk\":\"0123456789ABCDEF0123456789ABCDEF\",\"group_cat_id_admin\":\"01000001\",\"group_cat_id_operate\":\"06000001\"}"
   }

3. Subgroup:
   {
     "group_id": "abc123",
     "sub_group_id": "xyz789",
     "group_name": "Corner Lights"
   }

Query Patterns:
1. Get all subgroups for a group:
   - Use KeyConditionExpression: group_id = :gid
   - Returns main group and all subgroups

2. Get specific group/subgroup:
   - Use both group_id and sub_group_id in the key
   - Exact match query

3. Check if subgroup exists:
   - Query with both group_id and sub_group_id
   - Check if any items are returned

Access Control:
- Group creation requires no special permissions
- Subgroup operations require appropriate permissions on the parent group
- Deletion cascades to all subgroups and related mappings

Capabilities:
- Groups can optionally have capabilities that extend their functionality
- Each capability is identified by a name (e.g., "matter")
- Capability data is stored as JSON in cap_<name> columns
- Capabilities are only stored on main group entries, not subgroups

Matter Capability (cap_matter):
- fabric_id: 16-char uppercase hex string, derived from group_id using ASCII-to-hex encoding
- root_ca: PEM-encoded Root CA certificate for the Matter fabric
- root_ca_priv_key: PEM-encoded private key for the Root CA
- ipk: 32-char uppercase hex string, Identity Protection Key
- group_cat_id_admin: 8-char hex string, admin CAT ID
- group_cat_id_operate: 8-char hex string, operate CAT ID
*/

package group_db

import (
	"encoding/json"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	GroupsTable = "rmng-groups"

	// Key column names
	groupsHashKey = "group_id"
	groupsSortKey = "sub_group_id"
)

type GroupDB struct {
	dbcore.DB
}

func NewGroupDB(ctx *rmngctx.RmngContext) *GroupDB {
	return &GroupDB{
		DB: *dbcore.NewDB(ctx),
	}
}

// CreateGroup creates a new group in DynamoDB
func (gdb *GroupDB) CreateGroup(groupID, groupName string) error {
	return gdb.CreateGroupWithCapabilities(groupID, groupName, nil, nil)
}

// CreateGroupWithCapabilities creates a new group with optional capabilities in DynamoDB.
// Each capability's data is stored as a JSON string in a column named cap_<capability_name>.
func (gdb *GroupDB) CreateGroupWithCapabilities(groupID, groupName string, capabilities []string, capabilityData map[string]map[string]interface{}) error {
	if err := gdb.IsAuthorized(utils.GroupCreate, groupID); err != nil {
		return err
	}

	item := map[string]types.AttributeValue{
		"group_id":     &types.AttributeValueMemberS{Value: groupID},
		"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
		"group_name":   &types.AttributeValueMemberS{Value: groupName},
	}

	// Add capabilities list if provided
	if len(capabilities) > 0 {
		capList := make([]types.AttributeValue, len(capabilities))
		for i, cap := range capabilities {
			capList[i] = &types.AttributeValueMemberS{Value: cap}
		}
		item["capabilities"] = &types.AttributeValueMemberL{Value: capList}
	}

	// Add capability-specific data as JSON in cap_<name> columns
	for capName, capData := range capabilityData {
		jsonBytes, err := json.Marshal(capData)
		if err != nil {
			return rmerror.NewRMError(err, "failed to serialize capability data for "+capName)
		}
		item["cap_"+capName] = &types.AttributeValueMemberS{Value: string(jsonBytes)}
	}

	input := &dynamodb.PutItemInput{
		TableName:           aws.String(GroupsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(group_id)"),
	}
	_, err := gdb.PutItem(gdb.Ctx.Context, input)

	return err
}

// CreateSubGroup creates a new subgroup in DynamoDB
func (gdb *GroupDB) CreateSubGroup(parentGroupID, subGroupName, subGroupID string) error {
	if err := gdb.IsAuthorized(utils.GroupCreateSubGroup, parentGroupID); err != nil {
		return err
	}
	input := &dynamodb.PutItemInput{
		TableName: aws.String(GroupsTable),
		Item: map[string]types.AttributeValue{
			"group_id":     &types.AttributeValueMemberS{Value: parentGroupID},
			"sub_group_id": &types.AttributeValueMemberS{Value: subGroupID},
			"group_name":   &types.AttributeValueMemberS{Value: subGroupName},
		},
		ConditionExpression: aws.String("attribute_not_exists(group_id)"),
	}
	_, err := gdb.PutItem(gdb.Ctx.Context, input)
	return err
}

// ListRowsWithGroupID retrieves all rows for a given group ID, including capability data for the main group
func (gdb *GroupDB) ListRowsWithGroupID(parentGroupID string) ([]GroupInDB, error) {
	if err := gdb.IsAuthorized(utils.GroupListSubEntities, parentGroupID); err != nil {
		return nil, err
	}
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(GroupsTable),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: parentGroupID},
		},
	}

	queryOutput, err := gdb.Query(gdb.Ctx.Context, queryInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query groups in DynamoDB")
	}

	var groups []GroupInDB
	for _, item := range queryOutput.Items {
		addToGroups := false
		subGroupID, ok := dbcore.GetStringAttr(item, "sub_group_id")
		if !ok {
			// Sort key is mandatory; skip a malformed item rather than panic.
			continue
		}
		if subGroupID == "NONE" {
			// Add Parent group
			addToGroups = true
		} else {
			// Check if user has access to this sub-group, if so add subgroup
			match, err := gdb.Ctx.IsConditionMatch(utils.GroupListSubEntities, parentGroupID, subGroupID)
			if err != nil {
				return nil, rmerror.NewRMError(err, "failed to check condition match")
			}
			addToGroups = match
		}
		if addToGroups {
			groupName, _ := dbcore.GetStringAttr(item, "group_name")
			groupEntry := GroupInDB{
				GroupID:    parentGroupID,
				GroupName:  groupName,
				SubGroupID: subGroupID,
			}
			// Only parse capability data for the main group entry (not subgroups)
			if subGroupID == "NONE" {
				groupEntry.Capabilities, groupEntry.CapabilityData = parseCapabilitiesFromItem(item)
			}
			groups = append(groups, groupEntry)
		}
	}

	return groups, nil
}

// GetGroupByID retrieves a group by its ID, including capability data
func (gdb *GroupDB) GetGroupByID(groupID string) (*GroupInDB, error) {
	// Allow anybody to get a group by its ID
	input := &dynamodb.GetItemInput{
		TableName: aws.String(GroupsTable),
		Key: map[string]types.AttributeValue{
			"group_id":     &types.AttributeValueMemberS{Value: groupID},
			"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
		},
	}

	result, err := gdb.GetItem(gdb.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get group from DynamoDB")
	}

	if result.Item == nil {
		return nil, rmerror.NewRMError(nil, "group not found")
	}

	groupName, _ := dbcore.GetStringAttr(result.Item, "group_name")
	group := &GroupInDB{
		GroupID:   groupID,
		GroupName: groupName,
	}

	// Parse capabilities list and capability data
	group.Capabilities, group.CapabilityData = parseCapabilitiesFromItem(result.Item)

	return group, nil
}

// IsSubGroup checks if a subgroup exists for a given group
func (gdb *GroupDB) IsSubGroup(groupID, subGroupID string) (bool, error) {
	if err := gdb.IsAuthorized(utils.GroupListSubEntities, groupID); err != nil {
		return false, err
	}
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(GroupsTable),
		KeyConditionExpression: aws.String("group_id = :gid AND sub_group_id = :sgid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid":  &types.AttributeValueMemberS{Value: groupID},
			":sgid": &types.AttributeValueMemberS{Value: subGroupID},
		},
	}

	queryOutput, err := gdb.Query(gdb.Ctx.Context, queryInput)
	if err != nil {
		return false, rmerror.NewRMError(err, "failed to query groups in DynamoDB")
	}

	if len(queryOutput.Items) > 0 {
		return true, nil
	}
	return false, nil
}

// UpdateSubGroup updates the information of a subgroup (or the group if sub_group_id=="NONE")
func (gdb *GroupDB) UpdateSubGroup(groupID, subGroupID, groupOrSubgroupName string) error {
	action := utils.GroupUpdateSubGroup

	if subGroupID == "NONE" {
		action = utils.GroupUpdate
	}

	if err := gdb.IsAuthorized(action, groupID); err != nil {
		return err
	}

	update := expression.Set(expression.Name("group_name"), expression.Value(groupOrSubgroupName))

	cond := expression.AttributeExists(expression.Name("group_id"))

	if subGroupID != "NONE" {
		cond = expression.And(cond,
			expression.NotEqual(expression.Name("sub_group_id"), expression.Value("NONE")))
	}

	expr, err := expression.NewBuilder().
		WithUpdate(update).
		WithCondition(cond).
		Build()

	if err != nil {
		return rmerror.NewRMError(err, "failed to build update expression")
	}

	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(GroupsTable),
		Key: map[string]types.AttributeValue{
			"group_id":     &types.AttributeValueMemberS{Value: groupID},
			"sub_group_id": &types.AttributeValueMemberS{Value: subGroupID},
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ConditionExpression:       expr.Condition(),
	}

	if _, err = gdb.UpdateItem(gdb.Ctx.Context, input); err != nil {
		return rmerror.NewRMError(err, "failed to update group/subgroup name in DynamoDB")
	}

	return nil
}

// UpdateGroup updates the information of a group in DynamoDB
func (gdb *GroupDB) UpdateGroup(groupID, groupName string) error {
	return gdb.UpdateSubGroup(groupID, "NONE", groupName)
}

// DeleteGroup deletes a group and all its subgroups from DynamoDB using QueryAndBatchDelete
func (gdb *GroupDB) DeleteGroup(groupID string) error {
	if err := gdb.IsAuthorized(utils.GroupDelete, groupID); err != nil {
		return err
	}

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(GroupsTable),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
	}

	return gdb.QueryAndBatchDelete(gdb.Ctx.Context, queryInput, GroupsTable, []string{"group_id", "sub_group_id"})
}

// DeleteSubGroup deletes a specific subgroup from DynamoDB.
func (gdb *GroupDB) DeleteSubGroup(groupID, subGroupID string) error {

	// Safety Check: Prevent deletion of the Main Group via this method
	// To delete the main group (and cascade to all subgroups), use DeleteGroup() instead.
	if subGroupID == "NONE" {
		return rmerror.NewRMError(nil, "cannot delete main group via DeleteSubGroup; use DeleteGroup instead")
	}

	if err := gdb.IsAuthorized(utils.GroupDeleteSubGroup, groupID); err != nil {
		return err
	}

	// Verify it exists before deleting to return 404 if needed
	cond := expression.And(
		expression.AttributeExists(expression.Name(groupsHashKey)),
		expression.AttributeExists(expression.Name(groupsSortKey)),
	)
	expr, err := expression.NewBuilder().WithCondition(cond).Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build condition expression")
	}

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(GroupsTable),
		Key: map[string]types.AttributeValue{
			groupsHashKey: &types.AttributeValueMemberS{Value: groupID},
			groupsSortKey: &types.AttributeValueMemberS{Value: subGroupID},
		},
		ConditionExpression:      expr.Condition(),
		ExpressionAttributeNames: expr.Names(),
	}

	_, err = gdb.DeleteItem(gdb.Ctx.Context, input)
	if err != nil {
		if dbcore.IsConditionalCheckFailedException(err) {
			return rmerror.NewRMError(dbcore.ErrSubGroupNotFound, "subgroup not found")
		}
		return rmerror.NewRMError(err, "failed to delete subgroup from DynamoDB")
	}

	return nil
}

type GroupInDB struct {
	GroupID        string
	GroupName      string
	SubGroupID     string
	Capabilities   []string
	CapabilityData map[string]map[string]interface{} // cap_name -> parsed JSON data
}

// parseCapabilitiesFromItem extracts capabilities list and capability data from a DynamoDB item.
// It looks for the "capabilities" list attribute and "cap_*" JSON string attributes.
// Returns nil for capabilityData if no capability data is found.
func parseCapabilitiesFromItem(item map[string]types.AttributeValue) ([]string, map[string]map[string]interface{}) {
	var capabilities []string
	var capabilityData map[string]map[string]interface{}

	// Parse capabilities list
	if val, ok := item["capabilities"]; ok {
		if list, ok := val.(*types.AttributeValueMemberL); ok {
			for _, listItem := range list.Value {
				if s, ok := listItem.(*types.AttributeValueMemberS); ok {
					capabilities = append(capabilities, s.Value)
				}
			}
		}
	}

	// Parse capability data from cap_* columns
	for key, val := range item {
		if len(key) > 4 && key[:4] == "cap_" {
			capName := key[4:]
			if s, ok := val.(*types.AttributeValueMemberS); ok {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(s.Value), &data); err == nil {
					if capabilityData == nil {
						capabilityData = make(map[string]map[string]interface{})
					}
					capabilityData[capName] = data
				}
			}
		}
	}

	return capabilities, capabilityData
}

// GetCapabilityData retrieves capability-specific data for a group from the cap_<name> column.
// The data is stored as JSON and returned as a map.
func (gdb *GroupDB) GetCapabilityData(groupID string, capabilityName string) (map[string]interface{}, error) {
	// Allow anybody to get a group by its ID
	columnName := "cap_" + capabilityName
	input := &dynamodb.GetItemInput{
		TableName: aws.String(GroupsTable),
		Key: map[string]types.AttributeValue{
			"group_id":     &types.AttributeValueMemberS{Value: groupID},
			"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
		},
		ProjectionExpression: aws.String(columnName),
	}

	result, err := gdb.GetItem(gdb.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get capability data from DynamoDB")
	}

	if result.Item == nil {
		return nil, rmerror.NewRMError(nil, "group not found")
	}

	val, ok := result.Item[columnName]
	if !ok {
		return nil, nil // No capability data
	}

	s, ok := val.(*types.AttributeValueMemberS)
	if !ok {
		return nil, rmerror.NewRMError(nil, "invalid capability data format")
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(s.Value), &data); err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse capability data JSON")
	}

	return data, nil
}

// setCapabilityData writes a group's capability data into the cap_<name> column using the
// expression builder. When enable is true it also appends capabilityName to the
// "capabilities" list and requires the capability to be absent (enable-once); when false
// it overwrites the data of an already-enabled capability. The caller handles authorization.
func (gdb *GroupDB) setCapabilityDataAuthorized(groupID, capabilityName string, data map[string]interface{}, createCapability bool) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return rmerror.NewRMError(err, "failed to serialize capability data")
	}

	columnName := "cap_" + capabilityName

	update := expression.Set(expression.Name(columnName), expression.Value(string(jsonBytes)))
	condition := expression.AttributeExists(expression.Name("group_id"))

	if createCapability {
		// The cap_<name> column exists iff the capability is enabled, so its absence is the duplicate guard.
		update = update.Set(
			expression.Name("capabilities"),
			expression.ListAppend(
				expression.IfNotExists(expression.Name("capabilities"), expression.Value([]string{})),
				expression.Value([]string{capabilityName}),
			),
		)
		condition = condition.And(expression.AttributeNotExists(expression.Name(columnName)))
	}

	expr, err := expression.NewBuilder().WithUpdate(update).WithCondition(condition).Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build capability update expression")
	}

	_, err = gdb.UpdateItem(gdb.Ctx.Context, &dynamodb.UpdateItemInput{
		TableName: aws.String(GroupsTable),
		Key: map[string]types.AttributeValue{
			"group_id":     &types.AttributeValueMemberS{Value: groupID},
			"sub_group_id": &types.AttributeValueMemberS{Value: "NONE"},
		},
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

// UpdateCapabilityData overwrites the cap_<name> data of an already-enabled capability.
func (gdb *GroupDB) UpdateCapabilityData(groupID string, capabilityName string, data map[string]interface{}) error {
	if err := gdb.IsAuthorized(utils.GroupUpdate, groupID); err != nil {
		return err
	}
	if err := gdb.setCapabilityDataAuthorized(groupID, capabilityName, data, false); err != nil {
		return rmerror.NewRMError(err, "failed to update capability data in DynamoDB")
	}
	return nil
}

// AddCapability enables a capability on a group: it appends the name to the "capabilities"
// list and writes its data, rejecting the write if the capability is already present.
func (gdb *GroupDB) AddCapability(groupID string, capabilityName string, data map[string]interface{}) error {
	if err := gdb.IsAuthorized(utils.GroupUpdateCapabilities, groupID); err != nil {
		return err
	}
	if err := gdb.setCapabilityDataAuthorized(groupID, capabilityName, data, true); err != nil {
		return rmerror.NewRMError(err, "failed to add capability to group in DynamoDB")
	}
	return nil
}
