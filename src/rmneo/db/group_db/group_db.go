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
	"errors"
	"fmt"
	"strings"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	GroupsTable = "rmng-groups"

	// Key column names
	groupsHashKey = "group_id"
	groupsSortKey = "sub_group_id"

	capColumnPrefix = "cap_"
)

// ErrSubGroupNotFound is returned when a subgroup operation targets a subgroup that does not exist; handlers map it to HTTP 404.
var ErrSubGroupNotFound = errors.New("subgroup not found")

type GroupDB struct {
	Ctx    *rmngctx.RmngContext
	groups *ddbu.Table[groupRow]
}

func NewGroupDB(ctx *rmngctx.RmngContext) *GroupDB {
	return &GroupDB{
		Ctx: ctx,
		groups: ddbu.NewTable[groupRow](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     GroupsTable,
			HashKey:  groupsHashKey,
			RangeKey: groupsSortKey,
		}),
	}
}

// groupRow is one rmng-groups item. Its cap_<name> columns are dynamic, so it (un)marshals itself; decoding is lenient so one malformed column does not fail a group read.
type groupRow struct {
	GroupID      string
	SubGroupID   string
	GroupName    string
	Capabilities []string
	// CapColumns holds each cap_<name> attribute as stored, keyed by the full column name.
	CapColumns map[string]types.AttributeValue
}

func (r groupRow) MarshalDynamoDBAttributeValue() (types.AttributeValue, error) {
	item := map[string]types.AttributeValue{
		groupsHashKey: &types.AttributeValueMemberS{Value: r.GroupID},
		groupsSortKey: &types.AttributeValueMemberS{Value: r.SubGroupID},
		"group_name":  &types.AttributeValueMemberS{Value: r.GroupName},
	}
	if len(r.Capabilities) > 0 {
		capList := make([]types.AttributeValue, len(r.Capabilities))
		for i, capability := range r.Capabilities {
			capList[i] = &types.AttributeValueMemberS{Value: capability}
		}
		item["capabilities"] = &types.AttributeValueMemberL{Value: capList}
	}
	for column, value := range r.CapColumns {
		item[column] = value
	}
	return &types.AttributeValueMemberM{Value: item}, nil
}

func (r *groupRow) UnmarshalDynamoDBAttributeValue(av types.AttributeValue) error {
	m, ok := av.(*types.AttributeValueMemberM)
	if !ok {
		return rmerror.NewRMError(nil, fmt.Sprintf("%s item is %T, not a map", GroupsTable, av))
	}
	*r = groupRow{
		GroupID:    stringAttr(m.Value, groupsHashKey),
		SubGroupID: stringAttr(m.Value, groupsSortKey),
		GroupName:  stringAttr(m.Value, "group_name"),
	}
	if list, ok := m.Value["capabilities"].(*types.AttributeValueMemberL); ok {
		for _, listItem := range list.Value {
			if s, ok := listItem.(*types.AttributeValueMemberS); ok {
				r.Capabilities = append(r.Capabilities, s.Value)
			}
		}
	}
	for column, value := range m.Value {
		if len(column) > len(capColumnPrefix) && strings.HasPrefix(column, capColumnPrefix) {
			if r.CapColumns == nil {
				r.CapColumns = make(map[string]types.AttributeValue)
			}
			r.CapColumns[column] = value
		}
	}
	return nil
}

// stringAttr returns an S attribute's value, or "" when it is absent or not a string.
func stringAttr(item map[string]types.AttributeValue, name string) string {
	if s, ok := item[name].(*types.AttributeValueMemberS); ok {
		return s.Value
	}
	return ""
}

// CreateGroup creates a new group in DynamoDB
func (gdb *GroupDB) CreateGroup(groupID, groupName string) error {
	return gdb.CreateGroupWithCapabilities(groupID, groupName, nil, nil)
}

// CreateGroupWithCapabilities creates a new group with optional capabilities in DynamoDB.
// Each capability's data is stored as a JSON string in a column named cap_<capability_name>.
func (gdb *GroupDB) CreateGroupWithCapabilities(groupID, groupName string, capabilities []string, capabilityData map[string]map[string]interface{}) error {
	if err := gdb.Ctx.IsAuthorized(utils.GroupCreate, groupID); err != nil {
		return err
	}

	row := groupRow{
		GroupID:      groupID,
		SubGroupID:   "NONE",
		GroupName:    groupName,
		Capabilities: capabilities,
		CapColumns:   make(map[string]types.AttributeValue, len(capabilityData)),
	}

	// Add capability-specific data as JSON in cap_<name> columns
	for capName, capData := range capabilityData {
		jsonBytes, err := json.Marshal(capData)
		if err != nil {
			return rmerror.NewRMError(err, "failed to serialize capability data for "+capName)
		}
		row.CapColumns[capColumnPrefix+capName] = &types.AttributeValueMemberS{Value: string(jsonBytes)}
	}

	return gdb.groups.Create(gdb.Ctx.Context, row)
}

// CreateSubGroup creates a new subgroup in DynamoDB
func (gdb *GroupDB) CreateSubGroup(parentGroupID, subGroupName, subGroupID string) error {
	if err := gdb.Ctx.IsAuthorized(utils.GroupCreateSubGroup, parentGroupID); err != nil {
		return err
	}
	return gdb.groups.Create(gdb.Ctx.Context, groupRow{
		GroupID:    parentGroupID,
		SubGroupID: subGroupID,
		GroupName:  subGroupName,
	})
}

// ListRowsWithGroupID retrieves all rows for a given group ID, including capability data for the main group
func (gdb *GroupDB) ListRowsWithGroupID(parentGroupID string) ([]GroupInDB, error) {
	if err := gdb.Ctx.IsAuthorized(utils.GroupListSubEntities, parentGroupID); err != nil {
		return nil, err
	}

	// Paginated: the parent row sorts under "NONE", so a truncated page can drop the group
	// itself as easily as its subgroups.

	// A caller that can list sub-entities but cannot GroupGet the parent is subgroup-scoped. It still receives the parent row, so the cap_* secrets have to be withheld from it explicitly.
	limitedAccess := gdb.Ctx.IsAuthorized(utils.GroupGet, parentGroupID) != nil

	var groups []GroupInDB
	err := gdb.groups.QueryEach(gdb.Ctx.Context, expression.Key(groupsHashKey).Equal(expression.Value(parentGroupID)), func(row groupRow) error {
		var addToGroups bool
		subGroupID := row.SubGroupID
		if subGroupID == "" {
			// Sort key is mandatory; skip a malformed item rather than panic.
			return nil
		}
		if subGroupID == "NONE" {
			// Add Parent group
			addToGroups = true
		} else {
			// Check if user has access to this sub-group, if so add subgroup
			match, err := gdb.Ctx.IsConditionMatch(utils.GroupListSubEntities, parentGroupID, subGroupID)
			if err != nil {
				return rmerror.NewRMError(err, "failed to check condition match")
			}
			addToGroups = match
		}
		if addToGroups {
			groupEntry := GroupInDB{
				GroupID:    parentGroupID,
				GroupName:  row.GroupName,
				SubGroupID: subGroupID,
			}
			// Only parse capability data for the main group entry (not subgroups)
			if subGroupID == "NONE" {
				caps, capData := row.parseCapabilities()
				groupEntry.Capabilities = caps
				// The cap_* columns hold a capability's secrets — for Matter that is the fabric identity-protection key, the root CA and both CAT ids. A subgroup-scoped caller may know the group is Matter-enabled, but must not receive the fabric material: get-groups feeds this straight into the response via the capability's GetResponseData.
				if !limitedAccess {
					groupEntry.CapabilityData = capData
				}
			}
			groups = append(groups, groupEntry)
		}
		return nil
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query groups in DynamoDB")
	}

	return groups, nil
}

// GetGroupByID retrieves a group by its ID, including capability data
func (gdb *GroupDB) GetGroupByID(groupID string) (*GroupInDB, error) {
	// Allow anybody to get a group by its ID
	row, found, err := gdb.groups.Get(gdb.Ctx.Context, gdb.groups.Key(groupID, "NONE"))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get group from DynamoDB")
	}

	if !found {
		return nil, rmerror.NewRMError(nil, "group not found")
	}

	group := &GroupInDB{
		GroupID:   groupID,
		GroupName: row.GroupName,
	}

	// Parse capabilities list and capability data
	group.Capabilities, group.CapabilityData = row.parseCapabilities()

	return group, nil
}

// IsSubGroup checks if a subgroup exists for a given group
func (gdb *GroupDB) IsSubGroup(groupID, subGroupID string) (bool, error) {
	if err := gdb.Ctx.IsAuthorized(utils.GroupListSubEntities, groupID); err != nil {
		return false, err
	}

	_, found, err := gdb.groups.Get(gdb.Ctx.Context, gdb.groups.Key(groupID, subGroupID))
	if err != nil {
		return false, rmerror.NewRMError(err, "failed to query groups in DynamoDB")
	}
	return found, nil
}

// UpdateSubGroup updates the information of a subgroup (or the group if sub_group_id=="NONE")
func (gdb *GroupDB) UpdateSubGroup(groupID, subGroupID, groupOrSubgroupName string) error {
	action := utils.GroupUpdateSubGroup

	if subGroupID == "NONE" {
		action = utils.GroupUpdate
	}

	if err := gdb.Ctx.IsAuthorized(action, groupID); err != nil {
		return err
	}

	update := expression.Set(expression.Name("group_name"), expression.Value(groupOrSubgroupName))

	var opts ddbu.WriteOpts
	if subGroupID != "NONE" {
		opts.If = expression.NotEqual(expression.Name("sub_group_id"), expression.Value("NONE"))
	}

	if _, err := gdb.groups.Update(gdb.Ctx.Context, gdb.groups.Key(groupID, subGroupID), update, opts); err != nil {
		return rmerror.NewRMError(err, "failed to update group/subgroup name in DynamoDB")
	}

	return nil
}

// UpdateGroup updates the information of a group in DynamoDB
func (gdb *GroupDB) UpdateGroup(groupID, groupName string) error {
	return gdb.UpdateSubGroup(groupID, "NONE", groupName)
}

// DeleteGroup deletes a group and all its subgroups from DynamoDB
func (gdb *GroupDB) DeleteGroup(groupID string) error {
	if err := gdb.Ctx.IsAuthorized(utils.GroupDelete, groupID); err != nil {
		return err
	}

	err := gdb.groups.QueryDelete(gdb.Ctx.Context, expression.Key(groupsHashKey).Equal(expression.Value(groupID)))
	return err
}

// DeleteSubGroup deletes a specific subgroup from DynamoDB.
func (gdb *GroupDB) DeleteSubGroup(groupID, subGroupID string) error {

	// Safety Check: Prevent deletion of the Main Group via this method
	// To delete the main group (and cascade to all subgroups), use DeleteGroup() instead.
	if subGroupID == "NONE" {
		return rmerror.NewRMError(nil, "cannot delete main group via DeleteSubGroup; use DeleteGroup instead")
	}

	if err := gdb.Ctx.IsAuthorized(utils.GroupDeleteSubGroup, groupID); err != nil {
		return err
	}

	// Delete's must-exist condition is what turns a missing subgroup into a 404.
	if err := gdb.groups.Delete(gdb.Ctx.Context, gdb.groups.Key(groupID, subGroupID)); err != nil {
		if ddbu.IsConditionFailed(err) {
			return rmerror.NewRMError(ErrSubGroupNotFound, "subgroup not found")
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

// parseCapabilities returns the capabilities list and the JSON in each cap_* column that holds a string. capabilityData is nil when no column parses.
func (r groupRow) parseCapabilities() ([]string, map[string]map[string]interface{}) {
	var capabilityData map[string]map[string]interface{}

	for column, val := range r.CapColumns {
		s, ok := val.(*types.AttributeValueMemberS)
		if !ok {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(s.Value), &data); err != nil {
			continue
		}
		if capabilityData == nil {
			capabilityData = make(map[string]map[string]interface{})
		}
		capabilityData[strings.TrimPrefix(column, capColumnPrefix)] = data
	}

	return r.Capabilities, capabilityData
}

// GetCapabilityData retrieves capability-specific data for a group from the cap_<name> column.
// The data is stored as JSON and returned as a map.
func (gdb *GroupDB) GetCapabilityData(groupID string, capabilityName string) (map[string]interface{}, error) {
	// Allow anybody to get a group by its ID
	columnName := capColumnPrefix + capabilityName

	// Whole row, no projection: a group without this column would project to an empty item and read as not found.
	row, found, err := gdb.groups.Get(gdb.Ctx.Context, gdb.groups.Key(groupID, "NONE"))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get capability data from DynamoDB")
	}

	if !found {
		return nil, rmerror.NewRMError(nil, "group not found")
	}

	val, ok := row.CapColumns[columnName]
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

// setCapabilityDataAuthorized writes a group's capability data into the cap_<name> column. When createCapability is true it also appends capabilityName to the "capabilities" list and requires the capability to be absent (enable-once); when false it overwrites the data of an already-enabled capability. The caller handles authorization.
func (gdb *GroupDB) setCapabilityDataAuthorized(groupID, capabilityName string, data map[string]interface{}, createCapability bool) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return rmerror.NewRMError(err, "failed to serialize capability data")
	}

	columnName := capColumnPrefix + capabilityName

	update := expression.Set(expression.Name(columnName), expression.Value(string(jsonBytes)))
	var opts ddbu.WriteOpts

	if createCapability {
		// The cap_<name> column exists iff the capability is enabled, so its absence is the duplicate guard.
		update = update.Set(
			expression.Name("capabilities"),
			expression.ListAppend(
				expression.IfNotExists(expression.Name("capabilities"), expression.Value([]string{})),
				expression.Value([]string{capabilityName}),
			),
		)
		opts.If = expression.AttributeNotExists(expression.Name(columnName))
	}

	_, err = gdb.groups.Update(gdb.Ctx.Context, gdb.groups.Key(groupID, "NONE"), update, opts)
	return err
}

// UpdateCapabilityData overwrites the cap_<name> data of an already-enabled capability.
func (gdb *GroupDB) UpdateCapabilityData(groupID string, capabilityName string, data map[string]interface{}) error {
	if err := gdb.Ctx.IsAuthorized(utils.GroupUpdate, groupID); err != nil {
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
	if err := gdb.Ctx.IsAuthorized(utils.GroupUpdateCapabilities, groupID); err != nil {
		return err
	}
	if err := gdb.setCapabilityDataAuthorized(groupID, capabilityName, data, true); err != nil {
		return rmerror.NewRMError(err, "failed to add capability to group in DynamoDB")
	}
	return nil
}
