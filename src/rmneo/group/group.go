// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_group_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Handlers map ErrGroupNotEmpty/ErrSubGroupNotEmpty to 409. Child nodes
// (IDs containing "--", see IsChildNode) are excluded from the count — they're
// managed by their parent and swept after the parent node is removed.
// ErrGroupAccessDenied/ErrSubGroupAccessDenied is mapped to a 4xx by handlers.
// ErrSubGroupDeleteForbidden is mapped to 403: the caller can see the subgroup but
// lacks delete permission (e.g. subentity access), distinct from "not found/no access".
var (
	ErrGroupNotEmpty           = errors.New("group not empty")
	ErrSubGroupNotEmpty        = errors.New("subgroup not empty")
	ErrGroupAccessDenied       = errors.New("group does not exist or access denied")
	ErrSubGroupAccessDenied    = errors.New("subgroup does not exist or access denied")
	ErrSubGroupDeleteForbidden = errors.New("insufficient permissions to delete subgroup")
	ErrNotMatterCapable        = errors.New("group does not have Matter capability")
	ErrCapabilityAlreadyExists = errors.New("group already has capability")
)

// IsChildNode reports whether nodeID denotes a child (sub-)node under the
// RainMaker "--" naming convention: an ID of the form "<parent>--<suffix>"
// names a node lifecycle-managed by the parent named before the "--". Such
// nodes are added to and removed from groups by their parent's flow, not by end
// users — core enforces this generically (group emptiness ignores them, and the
// user-flow group-removal handler rejects direct removal).
func IsChildNode(nodeID string) bool {
	return strings.Contains(nodeID, "--")
}

// ContainsNonChildNode reports whether entries hold any non-child node. Used for
// group/subgroup emptiness, which ignores parent-managed child nodes.
func ContainsNonChildNode(entries map[string]*group_node_db.GroupNode) bool {
	for nodeID := range entries {
		if !IsChildNode(nodeID) {
			return true
		}
	}
	return false
}

type SubGroup struct {
	SubGroupID       string
	SubGroupName     string
	NodeGroupEntries map[string]*group_node_db.GroupNode // node_id -> group_node_item
}

type Group struct {
	GroupID          string
	GroupName        string
	AccessType       utils.GroupAccessType
	NodeGroupEntries map[string]*group_node_db.GroupNode // node_id -> group_node_item
	SubGroups        []SubGroup                          // subgroup_id -> node_id -> group_node_item
	Capabilities     []string
	CapabilityData   map[string]map[string]interface{}
}

type GroupInDB struct {
	GroupID    string `json:"group_id"`
	GroupName  string `json:"group_name"`
	SubGroupID string `json:"sub_group_id,omitempty"`
}

// CreateGroupOptions contains optional parameters for group creation
type CreateGroupOptions struct {
	Capabilities []string `json:"capabilities,omitempty"`
}

// CreateGroupForUser creates a new top-level group for the given user.
func CreateGroupForUser(ctx *rmngctx.RmngContext, groupName string) (*Group, error) {
	return CreateGroupForUserWithOptions(ctx, groupName, nil)
}

// CreateGroupForUserWithOptions creates a new top-level group with optional capabilities.
// If opts is nil, a basic group without capabilities is created.
func CreateGroupForUserWithOptions(ctx *rmngctx.RmngContext, groupName string, opts *CreateGroupOptions) (*Group, error) {
	groupDB := group_db.NewGroupDB(ctx)
	userGroupDB := user_group_db.NewUserGroupDB(ctx)

	var group *Group
	var err error
	// Map of capability name to its data (each capability's data stored in separate column)
	var capabilityData map[string]map[string]interface{}

	var capabilities []string
	if opts != nil {
		capabilities = opts.Capabilities
	}
	if err := ValidateCapabilities(capabilities); err != nil {
		return nil, rmerror.NewRMError(err, "invalid capabilities")
	}

	for i := 0; i < 3; i++ {
		groupID := ids.GenerateGroupID()
		group = &Group{
			GroupID:   groupID,
			GroupName: groupName,
		}

		// Generate capability data with the actual groupID (regenerated each retry).
		capabilityData, err = generateCapabilityData(ctx, group, capabilities)
		if err != nil {
			return nil, err
		}

		err = groupDB.CreateGroupWithCapabilities(groupID, groupName, capabilities, capabilityData)
		if err == nil {
			// Populate the group with capability data before returning
			group.Capabilities = capabilities
			group.CapabilityData = capabilityData
			break
		}
		if !db.IsConditionalCheckFailedException(err) {
			return nil, rmerror.NewRMError(err, "failed to create group in DynamoDB")
		}
		// If we reach here, it means we had a duplicate groupID. We'll retry in the next iteration.
	}

	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to create unique group after 3 attempts")
	}

	err = userGroupDB.CreateUserGroup(group.GroupID)
	if err != nil {
		// Delete the group we just created
		deleteErr := groupDB.DeleteGroup(group.GroupID)
		if deleteErr != nil {
			// Log the error, but don't return it as the primary error
			rlog.Error(ctx).Err(deleteErr).Send()
		}
		return nil, rmerror.NewRMError(err, "failed to associate user with group")
	}

	// Call OnUserJoinGroup for the creator for each capability
	if err := invokeCapabilityOnUserJoinGroup(ctx, group.GroupID, ctx.Accessor.GetID(), group.Capabilities); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to invoke OnUserJoinGroup for capabilities")
		// Don't fail the group creation, just log the error
	}

	return group, nil
}

// generateCapabilityData runs each capability's OnGroupCreate to produce the per-group
// data map (capability name -> DB fields). Shared by group creation and enabling
// capabilities on an existing group. Returns nil for an empty capability list.
func generateCapabilityData(ctx *rmngctx.RmngContext, group *Group, capabilities []string) (map[string]map[string]interface{}, error) {
	if len(capabilities) == 0 {
		return nil, nil
	}
	data := make(map[string]map[string]interface{})
	for _, capName := range capabilities {
		cap, ok := GetCapability(capName)
		if !ok {
			return nil, rmerror.NewRMError(nil, "capability is not registered: "+capName)
		}
		capData, err := cap.OnGroupCreate(ctx, group)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to create capability data for "+capName)
		}
		data[capName] = capData.DBFields
	}
	return data, nil
}

// invokeCapabilityOnUserJoinGroup calls OnUserJoinGroup for userID for each capability of a group
func invokeCapabilityOnUserJoinGroup(ctx *rmngctx.RmngContext, groupID string, userID string, capabilities []string) error {
	for _, capName := range capabilities {
		cap, ok := GetCapability(capName)
		if !ok {
			continue
		}
		if err := cap.OnUserJoinGroup(ctx, groupID, userID); err != nil {
			return rmerror.NewRMError(err, "failed to invoke OnUserJoinGroup for capability "+capName)
		}
	}
	return nil
}

// invokeCapabilityOnUserExitGroup calls OnUserExitGroup for all capabilities of a group
func invokeCapabilityOnUserExitGroup(ctx *rmngctx.RmngContext, groupID string, userID string, accessType string, capabilities []string) error {
	for _, capName := range capabilities {
		cap, ok := GetCapability(capName)
		if !ok {
			continue
		}
		if err := cap.OnUserExitGroup(ctx, groupID, userID, accessType); err != nil {
			return rmerror.NewRMError(err, "failed to invoke OnUserExitGroup for capability "+capName)
		}
	}
	return nil
}

// GetUserGroupAccess checks the caller's access to the given group ID and
// populates their permissions. Succeeds for primary and secondary access. It does not succeed for subentity access.
func GetUserGroupAccess(ctx *rmngctx.RmngContext, groupID string) (utils.GroupAccessType, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	userGroupEntry, err := userGroupDB.GetUserGroup(groupID)
	if err != nil {
		return utils.GroupAccessType(""), err
	}
	return userGroupEntry.AccessType, nil
}

// GetUserSubGroupAccess verifies the caller has access to the given subgroup — either
// group-level access to the parent (primary/secondary) or subentity access that
// includes this specific subGroupID. Populates caller permissions on success.
func GetUserSubGroupAccess(ctx *rmngctx.RmngContext, parentGroupID, subGroupID string) (utils.GroupAccessType, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	userGroupEntry, err := userGroupDB.GetUserSubGroup(parentGroupID, subGroupID)
	if err != nil {
		return utils.GroupAccessType(""), err
	}
	return userGroupEntry.AccessType, nil
}

// CreateSubGroup creates a new subgroup within the given parent group.
func CreateSubGroup(ctx *rmngctx.RmngContext, parentGroupID, subGroupName string) (*SubGroup, error) {
	groupDB := group_db.NewGroupDB(ctx)

	var subGroup *SubGroup
	var err error

	// Check that we have access to the parent group
	_, err = GetUserGroupAccess(ctx, parentGroupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "parent group does not exist")
	}

	for i := 0; i < 5; i++ {
		subGroupID := ids.GenerateSubGroupID()
		err = groupDB.CreateSubGroup(parentGroupID, subGroupName, subGroupID)
		if err == nil {
			subGroup = &SubGroup{
				SubGroupID:   subGroupID,
				SubGroupName: subGroupName,
			}
			break
		}
		if !db.IsConditionalCheckFailedException(err) {
			return nil, rmerror.NewRMError(err, "failed to create subgroup in DynamoDB")
		}
		// If we reach here, it means we had a duplicate subGroupID. We'll retry in the next iteration.
	}

	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to create unique subgroup after 3 attempts")
	}

	return subGroup, nil
}

// ListUserAccessableGroups returns a map of groups and subgroups that the user has access to
// For example, if user has access to group1 and subgroup2a and subgroup2b, the function will return:
//
//	{
//		"groups": ["group1"],
//		"subgroups": {"group2":["subgroup2a", "subgroup2b"]}
//	}
func ListUserAccessableGroups(ctx *rmngctx.RmngContext) (map[string]interface{}, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	userGroupEntry, err := userGroupDB.ListGroupsForUser("")
	if err != nil {
		return nil, err
	}

	group_access := make(map[string]interface{})
	group_access["groups"] = []string{}
	group_access["subgroups"] = make(map[string][]string)
	for _, group := range userGroupEntry {
		if len(group.SubEntityIDs) == 0 {
			group_access["groups"] = append(group_access["groups"].([]string), group.GroupID)
		} else {
			if _, ok := group_access["subgroups"].(map[string][]string)[group.GroupID]; !ok {
				group_access["subgroups"].(map[string][]string)[group.GroupID] = []string{}
			}
			group_access["subgroups"].(map[string][]string)[group.GroupID] = group.SubEntityIDs
		}
	}

	return group_access, nil
}

// ListGroupsForUser retrieves all groups associated with the given user including their subgroups.
// It returns a slice of Group objects and any error that occurred.
func ListGroupsForUser(ctx *rmngctx.RmngContext, loadNodes bool) ([]Group, error) {
	return _listGroupsForUser(ctx, "", loadNodes)
}

// ListGroupForUser retrieves the group with the given groupId along with its subgroups and nodes
func ListGroupForUser(ctx *rmngctx.RmngContext, groupId string, loadNodes bool) ([]Group, error) {
	return _listGroupsForUser(ctx, groupId, loadNodes)
}

// ListUsersForGroupOrSubGroup lists all users that have access to the specified group or sub-group.
// Returns a map of userID -> accessType.
func ListUsersForGroupOrSubGroup(ctx *rmngctx.RmngContext, groupId string, subGroupIDs []string) (map[string]utils.GroupAccessType, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	return userGroupDB.ListUsersForGroupOrSubGroup(groupId, subGroupIDs)
}

// _listGroupsForUser retrieves all groups associated with the given user including their subgroups.
// It returns a slice of Group objects and any error that occurred.
// If groupId is provided, it will only return that specific group, if accessible to the user
func _listGroupsForUser(ctx *rmngctx.RmngContext, groupId string, loadNodes bool) ([]Group, error) {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)

	userGroupEntry, err := userGroupDB.ListGroupsForUser(groupId)
	if err != nil {
		return nil, err
	}

	var groups []Group
	for _, entry := range userGroupEntry {
		group, err := LoadGroup(ctx, entry.GroupID)
		if err != nil {
			return nil, err
		}
		group.AccessType = entry.AccessType
		if loadNodes {
			err := group.LoadNodes(ctx)
			if err != nil {
				return nil, err
			}
		}
		groups = append(groups, *group)
	}

	return groups, nil
}

// LoadGroup populates the group with its name, subgroups, and capability data
func LoadGroup(ctx *rmngctx.RmngContext, groupID string) (*Group, error) {
	groupDB := group_db.NewGroupDB(ctx)

	dbGroups, err := groupDB.ListRowsWithGroupID(groupID)
	if err != nil {
		return nil, err
	}

	group := &Group{
		GroupID: groupID,
	}
	var subGroups []SubGroup
	for _, dbGroup := range dbGroups {
		if dbGroup.SubGroupID == "NONE" {
			group.GroupID = dbGroup.GroupID
			group.GroupName = dbGroup.GroupName
			group.Capabilities = dbGroup.Capabilities
			group.CapabilityData = dbGroup.CapabilityData
		} else {
			subGroups = append(subGroups, SubGroup{
				SubGroupID:   dbGroup.SubGroupID,
				SubGroupName: dbGroup.GroupName,
			})
		}
	}
	group.SubGroups = subGroups
	return group, nil
}

// GetGroupByID retrieves a group by its ID, including capability data
func GetGroupByID(ctx *rmngctx.RmngContext, groupID string) (*Group, error) {
	groupDB := group_db.NewGroupDB(ctx)

	dbGroup, err := groupDB.GetGroupByID(groupID)
	if err != nil {
		return nil, err
	}

	return &Group{
		GroupID:        dbGroup.GroupID,
		GroupName:      dbGroup.GroupName,
		Capabilities:   dbGroup.Capabilities,
		CapabilityData: dbGroup.CapabilityData,
	}, nil
}

// AddCapabilitiesToGroup enables one or more capabilities on an existing group.
// Owner-only access is enforced by AddCapability's IsAuthorized check at the DB layer.
// Each capability's per-group data is generated via its OnGroupCreate hook, and every
// existing member is provisioned via its OnUserJoinGroup hook — so capability-specific
// behaviour (e.g. Matter) stays behind the capability abstraction.
func AddCapabilitiesToGroup(ctx *rmngctx.RmngContext, groupID string, capabilities []string) (*Group, error) {
	if len(capabilities) == 0 {
		return nil, rmerror.NewRMError(nil, "no capabilities provided")
	}

	if err := ValidateCapabilities(capabilities); err != nil {
		return nil, rmerror.NewRMError(err, "invalid capabilities")
	}

	// Populates the caller's permissions for the DB authorization checks below.
	// Wrap the ErrGroupAccessDenied sentinel (not the raw err) so callers can
	// errors.Is it to a 4xx, matching UpdateGroup/DeleteSubGroup.
	if _, err := GetUserGroupAccess(ctx, groupID); err != nil {
		return nil, rmerror.NewRMError(ErrGroupAccessDenied, "group does not exist or access denied")
	}

	groupDB := group_db.NewGroupDB(ctx)
	grp := &Group{
		GroupID:        groupID,
		CapabilityData: make(map[string]map[string]interface{}),
	}

	capabilityData, err := generateCapabilityData(ctx, grp, capabilities)
	if err != nil {
		return nil, err
	}

	for _, capName := range capabilities {
		// AddCapability rejects an already-enabled capability via a conditional write.
		if err := groupDB.AddCapability(groupID, capName, capabilityData[capName]); err != nil {
			if db.IsConditionalCheckFailedException(err) {
				return nil, rmerror.NewRMError(ErrCapabilityAlreadyExists, "group already has capability: "+capName)
			}
			return nil, rmerror.NewRMError(err, "failed to enable capability "+capName+" on group")
		}
		grp.Capabilities = append(grp.Capabilities, capName)
		grp.CapabilityData[capName] = capabilityData[capName]
	}

	// Provision per-user capability state for every existing member, as if each joined
	// the newly-enabled capabilities. Hooks are idempotent per capability.
	members, err := user_group_db.NewUserGroupDB(ctx).ListAllUsersForGroup(groupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list group members")
	}
	for _, member := range members {
		if err := invokeCapabilityOnUserJoinGroup(ctx, groupID, member.UserID, capabilities); err != nil {
			rlog.Error(ctx).Err(err).Str("user_id", member.UserID).Msg("Failed to provision capability state for member")
		}
	}

	return grp, nil
}

func IsSubGroup(ctx *rmngctx.RmngContext, groupID, subGroupID string) (bool, error) {
	groupDB := group_db.NewGroupDB(ctx)

	return groupDB.IsSubGroup(groupID, subGroupID)
}

// UpdateGroup updates the information of a group
func UpdateGroup(ctx *rmngctx.RmngContext, groupID string, groupName string) error {
	// Check that we have access to the parent group
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(errors.Join(ErrGroupAccessDenied, err), "group does not exist or access denied")
	}

	groupDB := group_db.NewGroupDB(ctx)
	err = groupDB.UpdateGroup(groupID, groupName)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update group information")
	}

	return nil
}

// UpdateSubGroup updates the information of a subgroup
func UpdateSubGroup(ctx *rmngctx.RmngContext, groupID string, subGroupID string, subGroupName string) error {
	// Check that we have access to the parent group OR the specific subgroup
	_, err := GetUserSubGroupAccess(ctx, groupID, subGroupID)

	if err != nil {
		return rmerror.NewRMError(errors.Join(ErrSubGroupAccessDenied, err), "subgroup does not exist or access denied")
	}

	// Update the subgroup
	groupDB := group_db.NewGroupDB(ctx)
	err = groupDB.UpdateSubGroup(groupID, subGroupID, subGroupName)
	if err != nil {
		return rmerror.NewRMError(err, "failed to update subgroup information")
	}

	return nil
}

// DeleteGroup deletes an empty group. Returns ErrGroupNotEmpty if any
// user node or subgroup remains.
func DeleteGroup(ctx *rmngctx.RmngContext, groupID string) error {
	userGroupDB := user_group_db.NewUserGroupDB(ctx)

	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(err, "parent group does not exist")
	}

	// Reject if any subgroup exists (LoadGroup surfaces empty ones too,
	// which GetGroupNodes would miss) or any user node is attached.
	loaded, err := LoadGroup(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to load group")
	}
	if len(loaded.SubGroups) > 0 {
		return ErrGroupNotEmpty
	}
	grpNodes, _, err := GetGroupNodes(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to enumerate group nodes")
	}
	if ContainsNonChildNode(grpNodes) {
		return ErrGroupNotEmpty
	}

	groupDB := group_db.NewGroupDB(ctx)
	err = groupDB.DeleteGroup(groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete group")
	}

	err = userGroupDB.DeleteUserGroupByGroupID(groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete user-group mappings")
	}

	return nil
}

// DeleteSubGroup deletes an empty subgroup. Returns ErrSubGroupNotEmpty
// if any user node still carries the subgroup tag. On success it also
// scrubs the subgroup from every user's sub_entity_ids (unconditional,
// so a shared-but-nodeless subgroup is cleaned up too). Nodes stay in
// the parent group.
func DeleteSubGroup(ctx *rmngctx.RmngContext, groupID string, subGroupID string) error {
	_, err := GetUserSubGroupAccess(ctx, groupID, subGroupID)
	if err != nil {
		return rmerror.NewRMError(errors.Join(ErrSubGroupAccessDenied, err), "subgroup does not exist or access denied")
	}

	// Early exit
	if err := ctx.IsAuthorized(utils.GroupDeleteSubGroup, groupID); err != nil {
		return rmerror.NewRMError(errors.Join(ErrSubGroupDeleteForbidden, err), "insufficient permissions to delete subgroup")
	}

	_, subgrpNodes, err := GetGroupNodes(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to enumerate group nodes")
	}
	if ContainsNonChildNode(subgrpNodes[subGroupID]) {
		return ErrSubGroupNotEmpty
	}

	groupDB := group_db.NewGroupDB(ctx)
	err = groupDB.DeleteSubGroup(groupID, subGroupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete subgroup")
	}

	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	err = userGroupDB.DeleteUserSubgroupBySubgroupID(groupID, subGroupID)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to clean up user-subgroup mappings")
		// Don't fail the deletion — the subgroup is already removed
	}

	return nil
}

// ShareGroup shares a group with a user.
func ShareGroup(ctx *rmngctx.RmngContext, groupID string, targetUserID string, accessType utils.GroupAccessType, primaryUserInfo auth.UserInfo) (string, error) {
	// check if we have permission to access the group
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return "", rmerror.NewRMError(err, "parent group does not exist")
	}

	if targetUserID == ctx.Accessor.GetID() {
		return "", rmerror.NewRMError(nil, "Cannot share with self")
	}

	sharingRequestDB := sharing_request_db.NewSharingRequestDB(ctx)
	requestID, err := sharingRequestDB.CreateSharingRequest(targetUserID, groupID, "", string(accessType), primaryUserInfo.Email, primaryUserInfo.PhoneNumber)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to create sharing request")
	}
	return requestID, nil
}

// UnshareGroup unshares a group with a user.
func UnshareGroup(ctx *rmngctx.RmngContext, groupID string, targetUserID string) error {
	// check if we have permission to access the group
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return rmerror.NewRMError(err, "parent group does not exist")
	}

	userGroupDB := user_group_db.NewUserGroupDB(ctx)

	// Get target user's access type BEFORE removal (needed for capability callbacks)
	var accessType string
	targetUserGroupItem, err := userGroupDB.GetUserGroupItemByUserID(targetUserID, groupID)
	if err != nil {
		// Log but continue - the user may not be in the group
		rlog.Error(ctx).Err(err).Msg("Failed to get target user's group access")
	} else if targetUserGroupItem.Item != nil {
		// Extract access_type from the item
		if attrVal, ok := targetUserGroupItem.Item["access_type"]; ok {
			if s, ok := attrVal.(*types.AttributeValueMemberS); ok {
				accessType = s.Value
			}
		}
	}

	// Get group to find its capabilities
	group, err := GetGroupByID(ctx, groupID)
	if err != nil {
		// Log but continue - the unshare should still proceed
		rlog.Error(ctx).Err(err).Msg("Failed to get group for capability callbacks")
	} else if accessType != "" && len(group.Capabilities) > 0 {
		// Call OnUserExitGroup for each capability
		if err := invokeCapabilityOnUserExitGroup(ctx, groupID, targetUserID, accessType, group.Capabilities); err != nil {
			rlog.Error(ctx).Err(err).Msg("Failed to invoke OnUserExitGroup for capabilities")
			// Don't fail the unshare, just log the error
		}
	}

	// Then remove the user
	return userGroupDB.UnshareUserGroup(groupID, targetUserID)
}

// ShareSubGroup shares a subgroup with a user.
// A subgroup is only shared with secondary access, so the user can only access the nodes in the sub-group
// They can never share this sub-group further
func ShareSubGroup(ctx *rmngctx.RmngContext, parentGroupID string, subGroupID string, targetUserID string, primaryUserInfo auth.UserInfo) (string, error) {
	// check if we have permission to access the group
	_, err := GetUserGroupAccess(ctx, parentGroupID)
	if err != nil {
		return "", rmerror.NewRMError(err, "parent group does not exist")
	}

	if targetUserID == ctx.Accessor.GetID() {
		return "", rmerror.NewRMError(nil, "Cannot share with self")
	}

	sharingRequestDB := sharing_request_db.NewSharingRequestDB(ctx)
	requestID, err := sharingRequestDB.CreateSharingRequest(targetUserID, parentGroupID, subGroupID, string(utils.GroupSubEntityAccess), primaryUserInfo.Email, primaryUserInfo.PhoneNumber)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to create sharing request")
	}
	return requestID, nil
}

// UnshareSubGroup removes a user's access to a subgroup.
func UnshareSubGroup(ctx *rmngctx.RmngContext, parentGroupID string, subGroupID string, targetUserID string) error {
	if _, err := GetUserSubGroupAccess(ctx, parentGroupID, subGroupID); err != nil {
		return rmerror.NewRMError(err, "parent group does not exist")
	}

	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	return userGroupDB.UnshareUserSubGroup(parentGroupID, subGroupID, targetUserID)
}

// ApproveSharingRequest confirms a sharing request for a user with the group or sub-group
func ApproveSharingRequest(ctx *rmngctx.RmngContext, sharingRequestID string) error {
	sharingRequestDB := sharing_request_db.NewSharingRequestDB(ctx)
	sharingRequest, err := sharingRequestDB.GetSharingRequestbyID(sharingRequestID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get sharing request")
	}
	defer sharingRequestDB.DeleteSharingRequest(sharingRequestID)

	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	err = userGroupDB.ConfirmSharingRequest(sharingRequest)
	if err != nil {
		return err
	}

	// Get the group to find its capabilities
	group, err := GetGroupByID(ctx, sharingRequest.GroupID)
	if err != nil {
		// Log but don't fail - the sharing was already confirmed
		rlog.Error(ctx).Err(err).Msg("Failed to get group for capability callbacks")
		return nil
	}

	// Call OnUserJoinGroup for the joining user for each capability
	if err := invokeCapabilityOnUserJoinGroup(ctx, group.GroupID, ctx.Accessor.GetID(), group.Capabilities); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to invoke OnUserJoinGroup for capabilities")
		// Don't fail the sharing approval, just log the error
	}

	return nil
}

// RejectSharingRequest rejects a sharing request for a user with the group or sub-group
func RejectSharingRequest(ctx *rmngctx.RmngContext, sharingRequestID string) error {
	sharingRequestDB := sharing_request_db.NewSharingRequestDB(ctx)
	return sharingRequestDB.DeleteSharingRequest(sharingRequestID)
}

// GetMySharingRequests returns all the sharing requests for the current user
// Ideally, we might want to return the entire sharing request entry, since the user can take
// the 'approve' action based on it
func GetMySharingRequests(ctx *rmngctx.RmngContext) ([]*sharing_request_db.SharingRequestEntry, error) {
	sharingRequestDB := sharing_request_db.NewSharingRequestDB(ctx)
	sharingRequests, err := sharingRequestDB.GetMySharingRequests()
	if err != nil {
		return nil, err
	}
	return sharingRequests, nil
}

// GroupUserInfo represents a user's access to a group, enriched with user details.
type GroupUserInfo struct {
	UserID       string
	Email        string
	Phone        string
	AccessType   utils.GroupAccessType
	SubEntityIDs []string
}

// enrichGroupUsers batch-fetches user details for the given users and fills in
// each one's Email/Phone in place. Users without a details row are left as-is.
// A fetch failure is logged, not returned — callers still get the base data.
// Callers build the base GroupUserInfo (UserID/AccessType/SubEntityIDs); this
// only adds the contact fields.
func enrichGroupUsers(ctx *rmngctx.RmngContext, users []GroupUserInfo) []GroupUserInfo {
	if len(users) == 0 {
		return users
	}

	userIDs := make([]string, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.UserID)
	}

	userDetails, err := user_details_db.NewUserDetailsDB(ctx).BatchGetUserDetailsByIDs(userIDs)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("failed to fetch user details, returning without email/phone")
		return users
	}

	detailsMap := make(map[string]*user_details_db.UserDetailsEntry, len(userDetails))
	for i := range userDetails {
		detailsMap[userDetails[i].UserID] = &userDetails[i]
	}

	for i := range users {
		if details, ok := detailsMap[users[i].UserID]; ok {
			users[i].Email = details.Email
			users[i].Phone = details.PhoneNumber
		}
	}
	return users
}

// ListUsersForGroup returns all users who have access to the specified group,
// enriched with their email/phone from the user_details table.
func ListUsersForGroup(ctx *rmngctx.RmngContext, groupID string) ([]GroupUserInfo, error) {
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return nil, err
	}

	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	entries, err := userGroupDB.ListAllUsersForGroup(groupID)
	if err != nil {
		return nil, err
	}

	result := make([]GroupUserInfo, 0, len(entries))
	for _, e := range entries {
		result = append(result, GroupUserInfo{
			UserID:       e.UserID,
			AccessType:   e.AccessType,
			SubEntityIDs: e.SubEntityIDs,
		})
	}

	return enrichGroupUsers(ctx, result), nil
}

// ListUsersForSubGroup returns all users who have access to the specified subgroup,
// enriched with their email/phone from the user_details table. When accessType is
// empty, the listing scope defaults to the caller's capability: primary users get
// the full member listing ("all"), while lower-privilege members can only discover
// the group's primary owners ("primary").
func ListUsersForSubGroup(ctx *rmngctx.RmngContext, groupID, subGroupID string) ([]GroupUserInfo, error) {
	// Populates the caller's permissions and gates access to the subgroup.
	if _, err := GetUserSubGroupAccess(ctx, groupID, subGroupID); err != nil {
		return nil, err
	}

	// The DB layer scopes the listing to the caller's capability (full vs primary-only).
	userGroupDB := user_group_db.NewUserGroupDB(ctx)
	entries, err := userGroupDB.ListUsersForGroupOrSubGroup(groupID, []string{subGroupID})
	if err != nil {
		return nil, err
	}

	result := make([]GroupUserInfo, 0, len(entries))
	for userID, at := range entries {
		// Scope the reported subgroups to the one being listed — this endpoint must
		// not leak which other subgroups a user belongs to.
		result = append(result, GroupUserInfo{
			UserID:       userID,
			AccessType:   at,
			SubEntityIDs: []string{subGroupID},
		})
	}

	return enrichGroupUsers(ctx, result), nil
}
