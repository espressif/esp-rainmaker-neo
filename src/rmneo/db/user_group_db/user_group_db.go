// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The user_group_mapping table manages access control between users and groups/subgroups:

Table Name: user_group_mapping
Primary Key: user_id (Partition Key), group_id (Sort Key)

Schema:
- user_id (String): Partition key, identifies the user
- group_id (String): Sort key, identifies the group
- sub_entity_id (List of Strings): Identifies subentities if shared, empty list for main group access
- access_type (String): Type of access granted to the user
  - "primary": Full access, can share group/subgroup
  - "secondary": Limited access, cannot share further
  - "subentity": Access only to specific subentities
Secondary Indexes:
- user_group_mapping_group_id_index:
  - Partition Key: group_id
  - Projects user_id, sub_entity_ids, access_type
  - Used for finding all users with access to a group

Example Records:
1. User with primary access to group:
   {
     "user_id": "user123",
     "group_id": "group456",
     "sub_entity_ids": [],
     "access_type": "primary"
   }

2. User with access to specific subgroup:
   {
     "user_id": "user789",
     "group_id": "group456",
     "sub_entity_ids": ["subgrp123"],
     "access_type": "subentity"
   }

Query Patterns:
1. List user's groups:
   - Query by user_id to get all accessible groups
   - Returns both full group and subgroup access

2. Check specific access:
   - Query with both user_id and group_id
   - Check sub_entity_ids and access_type for permission level

3. List group's users:
   - Use group_id_index to find all users with access
   - Used during group deletion

Access Control Cascade:
- Primary access to group implies access to all subgroups
- Subgroup access does not grant access to parent group
- Deleting group removes all user-group mappings

Sharing Rules:
- Only primary access holders can share groups
- Subgroup access cannot be escalated to group access
- A user cannot share with themselves

Matter user Node IDs are derived during NOC issuance and are not stored on
user-group records.
*/

package user_group_db

import (
	"fmt"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"slices"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	UserGroupMappingTable = "rmng-user-group-assoc"

	// Key column names
	userGroupMappingHashKey = "user_id"
	userGroupMappingSortKey = "group_id"

	// Indexes
	UserGroupMappingByGroupIDIndex = "rmng-user-group-assoc-by-group-id"
	userGroupMappingHashKeyGSI     = "group_id"
	// No sort key for this GSI
)

type UserGroupDB struct {
	dbcore.DB
}

func NewUserGroupDB(ctx *rmngctx.RmngContext) *UserGroupDB {
	return &UserGroupDB{
		DB: *dbcore.NewDB(ctx),
	}
}

type UserGroupEntry struct {
	UserID       string                `dynamodbav:"user_id"`
	GroupID      string                `dynamodbav:"group_id"`
	SubEntityIDs []string              `dynamodbav:"sub_entity_ids,omitempty"`
	AccessType   utils.GroupAccessType `dynamodbav:"access_type"`
}

// UserGroupGSIEntry represents a projected item from the user_group_mapping_group_id_index GSI.
type UserGroupGSIEntry struct {
	UserID       string                `dynamodbav:"user_id"`
	GroupID      string                `dynamodbav:"group_id"`
	SubEntityIDs []string              `dynamodbav:"sub_entity_ids,omitempty"`
	AccessType   utils.GroupAccessType `dynamodbav:"access_type"`
}

func (db *UserGroupDB) createUserGroupEntry(groupID string, userId string, subEntityID string, accessType utils.GroupAccessType) error {
	// Scope lives in sub_entity_ids, and an empty list is how full-group access is stored — so a subentity row naming no subgroup would read as access to the whole group everywhere it is consulted.
	if accessType == utils.GroupSubEntityAccess && subEntityID == "" {
		return rmerror.NewRMError(nil, "subgroup access requires a subgroup id")
	}

	// First check if entry already exists
	existingEntry, err := db.GetUserGroupItemByUserID(userId, groupID)
	if err != nil {
		return err
	}

	var subEntityValue []types.AttributeValue
	if subEntityID != "" {
		subEntityValue = []types.AttributeValue{
			&types.AttributeValueMemberS{Value: subEntityID},
		}
	} else {
		subEntityValue = []types.AttributeValue{}
	}

	var existing UserGroupEntry
	if existingEntry.Item != nil {
		if err := attributevalue.UnmarshalMap(existingEntry.Item, &existing); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal existing user group mapping")
		}
	}

	if existingEntry.Item != nil &&
		existing.AccessType != utils.GroupSubEntityAccess &&
		accessType == utils.GroupSubEntityAccess {
		// A group level access is already provided, so we can't add sub-group access to it
		return rmerror.NewRMError(nil, "cannot add sub-group access to a group level access")
	}

	var input *dynamodb.UpdateItemInput
	if existingEntry.Item == nil || existing.AccessType != accessType {
		// Create/Overwrite with a new entry if the entry doesn't exist or the access type is different (switching between primary/secondary).
		// When a user with subentity access receives a group share (primary/secondary), this branch wipes sub_entity_ids to [] and promotes the user to full group access — group access supersedes any subgroup access.
		input = &dynamodb.UpdateItemInput{
			TableName: aws.String(UserGroupMappingTable),
			Key: map[string]types.AttributeValue{
				userGroupMappingHashKey: &types.AttributeValueMemberS{Value: userId},
				userGroupMappingSortKey: &types.AttributeValueMemberS{Value: groupID},
			},
			UpdateExpression: aws.String("SET sub_entity_ids = :sids, access_type = :at"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":sids": &types.AttributeValueMemberL{
					Value: subEntityValue,
				},
				":at": &types.AttributeValueMemberS{Value: string(accessType)},
			},
		}
	} else if len(existing.SubEntityIDs) > 0 {
		// Entry exists with sub_entity_ids. Reject if the subgroup is already present to avoid duplicates.
		for _, existingID := range existing.SubEntityIDs {
			if existingID == subEntityID {
				return rmerror.NewRMError(nil, "no change to user group mapping")
			}
		}

		// Append the new subgroup using list_append
		updateExpr := expression.Set(
			expression.Name("sub_entity_ids"),
			expression.ListAppend(
				expression.Name("sub_entity_ids"),
				expression.Value([]string{subEntityID}),
			),
		)
		expr, err := expression.NewBuilder().WithUpdate(updateExpr).Build()
		if err != nil {
			return rmerror.NewRMError(err, "failed to build update expression")
		}

		input = &dynamodb.UpdateItemInput{
			TableName: aws.String(UserGroupMappingTable),
			Key: map[string]types.AttributeValue{
				userGroupMappingHashKey: &types.AttributeValueMemberS{Value: userId},
				userGroupMappingSortKey: &types.AttributeValueMemberS{Value: groupID},
			},
			UpdateExpression:          expr.Update(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
		}
	} else {
		// Nothing to do
		return rmerror.NewRMError(nil, "no change to user group mapping")
	}

	_, err = db.UpdateItem(db.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to associate user with group in DynamoDB")
	}

	// Allow access based on the permissions for the access type
	db.Ctx.SetAllowMultiple(utils.GetGroupPermissions(accessType), groupID)
	return nil
}

// CreateUserGroup creates a new user-group mapping for the given group ID.
func (db *UserGroupDB) CreateUserGroup(groupID string) error {
	// No checks, since any user can create a group
	err := db.createUserGroupEntry(groupID, db.Ctx.Accessor.GetID(), "", utils.GroupPrimaryAccess)
	if err != nil {
		return err
	}
	return nil
}

// GetUserGroupItemByUserID gets the user-group mapping for the given user ID and group ID.
// This should sparingly be used, you almost always want to use GetUserGroup or GetUserGroupItem instead
func (db *UserGroupDB) GetUserGroupItemByUserID(userID string, groupID string) (*dynamodb.GetItemOutput, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(UserGroupMappingTable),
		Key: map[string]types.AttributeValue{
			userGroupMappingHashKey: &types.AttributeValueMemberS{Value: userID},
			userGroupMappingSortKey: &types.AttributeValueMemberS{Value: groupID},
		},
	}

	result, err := db.GetItem(db.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user group from DynamoDB")
	}

	return result, nil
}

// GetUserGroup retrieves the user-group mapping for the given group ID.
// Caller's permissions for the parent group are populated.
func (db *UserGroupDB) GetUserGroup(groupID string) (*UserGroupEntry, error) {
	//No authorisation check, as this is the bootstrap read

	result, err := db.GetUserGroupItemByUserID(db.Ctx.Accessor.GetID(), groupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user group from DynamoDB")
	}
	if result.Item == nil {
		return nil, rmerror.NewRMError(nil, "user group not found")
	}

	entry := &UserGroupEntry{}
	if err = attributevalue.UnmarshalMap(result.Item, entry); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal user group")
	}

	// If sub_entity_ids is present, the user has access to sub groups but not the main group
	if len(entry.SubEntityIDs) > 0 {
		return nil, rmerror.NewRMError(nil, "user group not found")
	}

	accessType := utils.GroupAccessType(entry.AccessType)
	db.Ctx.SetAllowMultiple(utils.GetGroupPermissions(accessType), groupID)
	return entry, nil
}

// GetUserSubGroup retrieves the caller's user-group mapping for a specific
// subgroup. the full entry is returned, including all sub_entity_ids or returned entry is filtered to just that subgroup
// Caller's permissions for the parent group are populated.
func (db *UserGroupDB) GetUserSubGroup(parentGroupID, subGroupID string) (*UserGroupEntry, error) {
	// Fetch + populate permissions first (this is the bootstrap read).
	result, err := db.GetUserGroupItemByUserID(db.Ctx.Accessor.GetID(), parentGroupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get user group from DynamoDB")
	}
	if result.Item == nil {
		return nil, rmerror.NewRMError(nil, "user group not found")
	}

	entry := &UserGroupEntry{}
	if err := attributevalue.UnmarshalMap(result.Item, entry); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal user group")
	}

	// Group-level access (empty sub_entity_ids) covers any subgroup and returns
	// the full entry. Subentity access must include the requested subGroupID.
	if len(entry.SubEntityIDs) > 0 {
		hasSubGroup := slices.Contains(entry.SubEntityIDs, subGroupID)
		if !hasSubGroup {
			return nil, rmerror.NewRMError(nil, "user group not found")
		}
		entry.SubEntityIDs = []string{subGroupID}
	}

	db.Ctx.SetAllowMultiple(utils.GetGroupPermissions(entry.AccessType), parentGroupID)

	// The grant above is parent-group-wide, so a subgroup-scoped caller needs the narrowing registered here too — otherwise any enumeration later in the same request sees every sub-entity.
	if entry.AccessType == utils.GroupSubEntityAccess {
		db.Ctx.SetScopedConditions(utils.GroupListSubEntities, parentGroupID, entry.SubEntityIDs)
	}
	return entry, nil
}

// ListGroupsForUser retrieves all group IDs associated with the given user.
// If groupId is provided, it will only return that specific group, if accessible to the user
func (db *UserGroupDB) ListGroupsForUser(groupId string) ([]UserGroupEntry, error) {
	// Callers context is used, so no need of authorization check

	var keyConditionExpression string
	expressionAttributeValues := map[string]types.AttributeValue{
		":uid": &types.AttributeValueMemberS{Value: db.Ctx.Accessor.GetID()},
	}

	// Build key condition expression based on whether groupId is provided
	if groupId != "" {
		keyConditionExpression = "user_id = :uid AND group_id = :gid"
		expressionAttributeValues[":gid"] = &types.AttributeValueMemberS{Value: groupId}
	} else {
		keyConditionExpression = "user_id = :uid"
	}

	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(UserGroupMappingTable),
		KeyConditionExpression:    aws.String(keyConditionExpression),
		ExpressionAttributeValues: expressionAttributeValues,
		ProjectionExpression:      aws.String("group_id, sub_entity_ids, access_type"),
	}

	var groups []UserGroupEntry
	// Paginated: each row also grants this request's permissions on its group, so a truncated
	// page would hide the group and deny access to it.
	err := db.QueryPaginated(db.Ctx.Context, queryInput, func(item map[string]types.AttributeValue) error {
		var entry UserGroupEntry
		if err := attributevalue.UnmarshalMap(item, &entry); err != nil || entry.GroupID == "" {
			// Skip an item missing the mandatory sort key (or otherwise malformed) rather than panic on the auth path.
			return nil
		}

		db.Ctx.SetAllowMultiple(utils.GetGroupPermissions(entry.AccessType), entry.GroupID)
		if entry.AccessType == utils.GroupSubEntityAccess {
			db.Ctx.SetScopedConditions(utils.GroupListSubEntities, entry.GroupID, entry.SubEntityIDs)
		}

		groups = append(groups, UserGroupEntry{
			UserID:       db.Ctx.Accessor.GetID(),
			GroupID:      entry.GroupID,
			SubEntityIDs: append([]string{}, entry.SubEntityIDs...),
			AccessType:   entry.AccessType,
		})
		return nil
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user_group_mapping in DynamoDB")
	}

	return groups, nil
}

// ListUsersForGroupOrSubGroup lists the users that have access to the specified group or
// sub-group, scoped to the caller's capability:
//   - GroupListUsers        -> full membership (primary/secondary/subentity).
//   - GroupListPrimaryUsers -> primary owners only, so lower-privilege members can still
//     discover the owners they need to contact.
//   - neither               -> denied.
//
// Returns a map of userID -> accessType.
func (db *UserGroupDB) ListUsersForGroupOrSubGroup(groupID string, subGroupIDs []string) (map[string]utils.GroupAccessType, error) {
	// Prefer the full listing; fall back to primary-only for lower-privilege members.
	primaryOnly := false
	if err := db.IsAuthorized(utils.GroupListUsers, groupID); err != nil {
		if err := db.IsAuthorized(utils.GroupListPrimaryUsers, groupID); err != nil {
			return nil, err
		}
		primaryOnly = true
	}

	entries, err := db.listAllUsersForGroupAuthorized(groupID)
	if err != nil {
		return nil, err
	}

	users := make(map[string]utils.GroupAccessType)
	for _, entry := range entries {
		// Lower-privilege callers may only ever see primary owners.
		if primaryOnly && entry.AccessType != utils.GroupPrimaryAccess {
			continue
		}
		if len(entry.SubEntityIDs) > 0 {
			// Subentity user: include only if one of their subgroups is in the requested list.
			for _, subEntityID := range entry.SubEntityIDs {
				if slices.Contains(subGroupIDs, subEntityID) {
					users[entry.UserID] = entry.AccessType
					break
				}
			}
		} else {
			// Group-level access (primary/secondary): always included.
			users[entry.UserID] = entry.AccessType
		}
	}

	return users, nil
}

// ListAllUsersForGroup returns all user-group entries for a given group.
func (db *UserGroupDB) ListAllUsersForGroup(groupID string) ([]UserGroupGSIEntry, error) {
	primaryOnly := false
	if err := db.IsAuthorized(utils.GroupListUsers, groupID); err != nil {
		if err := db.IsAuthorized(utils.GroupListPrimaryUsers, groupID); err != nil {
			return nil, err
		}
		primaryOnly = true
	}

	entries, err := db.listAllUsersForGroupAuthorized(groupID)
	if err != nil {
		return nil, err
	}

	users := make([]UserGroupGSIEntry, 0, len(entries))

	for _, entry := range entries {
		if primaryOnly && entry.AccessType != utils.GroupPrimaryAccess {
			continue
		}
		users = append(users, entry)
	}

	return users, nil
}

// listAllUsersForGroupAuthorized runs the GSI query for a group's members
// without any authorization check. Per the "Authorized" suffix convention,
// callers MUST enforce authorization themselves before invoking it (e.g. a
// per-access-type check).
func (db *UserGroupDB) listAllUsersForGroupAuthorized(groupID string) ([]UserGroupGSIEntry, error) {
	// Use expression builder for DynamoDB query
	expr, err := expression.NewBuilder().
		WithKeyCondition(
			expression.Key("group_id").Equal(expression.Value(groupID)),
		).
		Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build DynamoDB expression")
	}

	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(UserGroupMappingTable),
		IndexName:                 aws.String(UserGroupMappingByGroupIDIndex),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}

	var entries []UserGroupGSIEntry
	err = db.QueryPaginated(db.Ctx.Context, queryInput, func(item map[string]types.AttributeValue) error {
		var entry UserGroupGSIEntry
		if err := attributevalue.UnmarshalMap(item, &entry); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal user group entries")
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query user_group_mapping in DynamoDB")
	}

	return entries, nil
}

// DeleteUserGroupByGroupID deletes all user-group mappings for a given group ID
func (db *UserGroupDB) DeleteUserGroupByGroupID(groupID string) error {
	if err := db.IsAuthorized(utils.GroupDelete, groupID); err != nil {
		return err
	}

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(UserGroupMappingTable),
		IndexName:              aws.String(UserGroupMappingByGroupIDIndex),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
	}

	return db.QueryAndBatchDelete(db.Ctx.Context, queryInput, UserGroupMappingTable, []string{userGroupMappingHashKey, userGroupMappingSortKey})
}

// DeleteUserSubgroupBySubgroupID removes a subgroup from all users' sub_entity_ids.
// This is used when deleting a subgroup to clean up user-group mappings.
// It only affects users with "subentity" access who have the target subgroup in their list.
// Users with primary/secondary access (empty sub_entity_ids) are not affected.
func (db *UserGroupDB) DeleteUserSubgroupBySubgroupID(groupID string, subGroupID string) error {
	if err := db.IsAuthorized(utils.GroupDeleteSubGroup, groupID); err != nil {
		return err
	}

	// Query all user-group mappings for this group via GSI
	entries, err := db.listAllUsersForGroupAuthorized(groupID)
	if err != nil {
		return err
	}

	var errs []error

	for _, entry := range entries {
		// Primary/secondary access users have empty sub_entity_ids and are not affected.
		if len(entry.SubEntityIDs) == 0 {
			continue
		}

		// Check if this user has the target subgroup in their sub_entity_ids
		hasSubGroup := slices.Contains(entry.SubEntityIDs, subGroupID)
		if !hasSubGroup {
			continue
		}

		// Delegate to shared helper — handles filtering, empty-list deletion, and the
		// race-safe SET write. Authorization was already enforced above.
		if err := db.removeSubGroupFromUser(entry.UserID, groupID, subGroupID, entry.SubEntityIDs); err != nil {
			errs = append(errs, fmt.Errorf("user %s: %w", entry.UserID, err))
		}
	}

	if len(errs) > 0 {
		return rmerror.NewRMError(fmt.Errorf("cleanup completed with %d errors: %v", len(errs), errs), "partial failure in subgroup cleanup")
	}

	return nil
}

// RemoveUserFromGroup removes a user from a group.
func (db *UserGroupDB) RemoveUserFromGroup(groupID string, userID string) error {

	//For self unshare or leave from group, bypass the authorization check
	if userID != db.Ctx.Accessor.GetID() {
		if err := db.IsAuthorized(utils.GroupShare, groupID); err != nil {
			return err
		}
	}

	return db.removeUserFromGroupAuthorized(groupID, userID)
}

// removeUserFromGroupAuthorized deletes a user's mapping row without any authorization check. Per the "Authorized" suffix convention the caller must have enforced its own check first — used by the subgroup-cleanup path, which is gated on GroupDeleteSubGroup rather than GroupShare.
func (db *UserGroupDB) removeUserFromGroupAuthorized(groupID string, userID string) error {
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(UserGroupMappingTable),
		IndexName:              aws.String(UserGroupMappingByGroupIDIndex),
		KeyConditionExpression: aws.String("group_id = :gid"),
		FilterExpression:       aws.String("user_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
	}

	return db.QueryAndBatchDelete(db.Ctx.Context, queryInput, UserGroupMappingTable, []string{userGroupMappingHashKey, userGroupMappingSortKey})
}

// RemoveUserFromSubGroup removes a user from a subgroup by removing the subgroup ID from the list
func (db *UserGroupDB) RemoveUserFromSubGroup(groupID string, subGroupID string, userID string) error {
	//For self unshare or leave from subgroup, bypass the authorization check
	if userID != db.Ctx.Accessor.GetID() {
		if err := db.IsAuthorized(utils.GroupShare, groupID); err != nil {
			return err
		}
	}

	// Get current entry to find the subgroup to remove
	item, err := db.GetUserGroupItemByUserID(userID, groupID)
	if err != nil {
		return err
	}

	if item.Item == nil {
		return rmerror.NewRMError(nil, "user has no subgroup access to remove")
	}

	var entry UserGroupEntry
	if err := attributevalue.UnmarshalMap(item.Item, &entry); err != nil {
		return rmerror.NewRMError(err, "invalid data format for user group mapping")
	}

	// Reject if the user is not part of the specified subgroup. This also covers
	// group-level (primary/secondary) users, whose sub_entity_ids is empty.
	found := false
	for _, id := range entry.SubEntityIDs {
		if id == subGroupID {
			found = true
			break
		}
	}
	if !found {
		return rmerror.NewRMError(nil, "user is not part of the specified subgroup")
	}

	// Delegate to shared helper — handles filtering, empty-list deletion, and the dbcore.DB write
	if err := db.removeSubGroupFromUser(userID, groupID, subGroupID, entry.SubEntityIDs); err != nil {
		return rmerror.NewRMError(err, "failed to remove user from subgroup")
	}

	return nil
}

// removeSubGroupFromUser removes a single sub-entity from a user-group mapping,
// operating on an already-fetched sub_entity_ids list (no extra read). It:
//  1. Filters the list in memory to exclude the target subGroupID.
//  2. If the filtered list is empty, deletes the entire mapping row.
//  3. Otherwise, overwrites the list with SET — constructing the new list in memory
//     avoids the index-shift race inherent in REMOVE sub_entity_ids[index].
//
// Authorization is the caller's responsibility; this is an internal helper.
func (db *UserGroupDB) removeSubGroupFromUser(userID, groupID, subGroupID string, currentSubEntities []string) error {
	// Filter the list in memory
	newSubEntities := make([]string, 0, len(currentSubEntities))
	for _, id := range currentSubEntities {
		if id != subGroupID {
			newSubEntities = append(newSubEntities, id)
		}
	}

	if len(newSubEntities) == 0 {
		// This was the user's only subgroup — remove the entire mapping row.
		// Use the auth-free variant; the caller already enforced authorization.
		// (RemoveUserFromGroup would re-check GroupShare, which the subgroup-delete caller does not hold — that mismatch used to abort the cleanup and leave the member with a mapping row pointing at a deleted subgroup.)
		return db.removeUserFromGroupAuthorized(groupID, userID)
	}

	expr, err := expression.NewBuilder().
		WithUpdate(expression.Set(expression.Name("sub_entity_ids"), expression.Value(newSubEntities))).
		Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build update expression for user group mapping")
	}

	updateInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(UserGroupMappingTable),
		Key: map[string]types.AttributeValue{
			userGroupMappingHashKey: &types.AttributeValueMemberS{Value: userID},
			userGroupMappingSortKey: &types.AttributeValueMemberS{Value: groupID},
		},
		UpdateExpression:          expr.Update(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	if _, err := db.UpdateItem(db.Ctx.Context, updateInput); err != nil {
		return rmerror.NewRMError(err, "failed to update user group mapping in DynamoDB")
	}

	return nil
}

// ConfirmSharingRequest confirms a sharing request for a user with the group or sub-group
func (db *UserGroupDB) ConfirmSharingRequest(sharingRequest *sharing_request_db.SharingRequestEntry) error {
	return db.createUserGroupEntry(sharingRequest.GroupID, sharingRequest.UserID, sharingRequest.SubEntityID, utils.GroupAccessType(sharingRequest.AccessType))
}

// UnshareUserGroup removes a user from a group.
func (db *UserGroupDB) UnshareUserGroup(groupID string, targetUserID string) error {
	// Enforces that a group must always retain at least one primary user: removing
	// the last primary (whether via kick or self leave) is rejected.
	entries, err := db.ListAllUsersForGroup(groupID)
	if err == nil {
		targetIsPrimary := false
		primaryCount := 0
		for _, e := range entries {
			if e.AccessType == utils.GroupPrimaryAccess {
				primaryCount++
				if e.UserID == targetUserID {
					targetIsPrimary = true
				}
			}
		}
		if targetIsPrimary && primaryCount <= 1 {
			return rmerror.NewRMError(dbcore.ErrLastPrimaryUser, "cannot remove the last primary user from the group")
		}
	}

	return db.RemoveUserFromGroup(groupID, targetUserID)
}

// UnshareUserSubGroup removes a user's access to a subgroup.
func (db *UserGroupDB) UnshareUserSubGroup(parentGroupID string, subGroupID string, targetUserID string) error {
	return db.RemoveUserFromSubGroup(parentGroupID, subGroupID, targetUserID)
}
