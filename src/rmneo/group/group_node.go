// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package group

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// GetNodesGroup returns the group and subgroups the node is currently in.
func GetNodesGroup(ctx *rmngctx.RmngContext, nodeID string) (group_node_db.NodesGroups, error) {
	return group_node_db.NewGroupNodeDB(ctx).GetNodesGroup(nodeID)
}

// AddNode adds a node to a group. If the node is already in another group,
// it is removed from the old group first.
// Returns the old group and subgroups the node was in.
func AddNode(ctx *rmngctx.RmngContext, groupID string, nodeID string, capabilities []string) (group_node_db.NodesGroups, error) {
	groupNodeDB := group_node_db.NewGroupNodeDB(ctx)
	oldGroup, err := groupNodeDB.GetNodesGroup(nodeID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to get old groups")
	}

	if oldGroup.Group == groupID {
		// Already in the target group — no-op: Relevant for WiFi reconfiguration
		return oldGroup, nil
	}

	if oldGroup.Group != "" {
		// Remove from old group
		err = groupNodeDB.RemoveNode(oldGroup.Group, nodeID)
		if err != nil {
			return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to remove node from old group")
		}
	}

	err = groupNodeDB.AddNode(groupID, nodeID, capabilities)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to add node to new group")
	}

	return oldGroup, nil
}

// GetGroupNodes retrieves the nodes in the group and their sub-groups
// The return values are:
// 1. map[string]*group_node_db.GroupNode: the entire Item for each node in the group (keyed by node_id)
// 2. map[string]map[string]*group_node_db.GroupNode: the entire Item for each node in each sub-group (keyed by subgroup_id, then node_id)
// 3. error: the error that occurred
func GetGroupNodes(ctx *rmngctx.RmngContext, groupID string) (map[string]*group_node_db.GroupNode, map[string]map[string]*group_node_db.GroupNode, error) {
	groupNodeDB := group_node_db.NewGroupNodeDB(ctx)
	grpNodes, subgrpNodes, err := groupNodeDB.ListGroupNodesWithDBEntry(groupID)
	if err != nil {
		return nil, nil, err
	}
	return grpNodes, subgrpNodes, nil
}

// LoadNodes retrieves all node IDs and capability data associated with the given group.
func (g *Group) LoadNodes(ctx *rmngctx.RmngContext) error {
	groupNodeDB := group_node_db.NewGroupNodeDB(ctx)
	nodeEntries, subgrpNodes, err := groupNodeDB.ListGroupNodesWithDBEntry(g.GroupID)
	if err != nil {
		return err
	}

	g.NodeGroupEntries = nodeEntries
	for i, subgrp := range g.SubGroups {
		g.SubGroups[i].NodeGroupEntries = subgrpNodes[subgrp.SubGroupID]
	}

	return nil
}

// UpdateNodeAndSubgroup updates a node's subgroup membership - add or remove
// Returns the old groups and subgroups the node was in
func UpdateNodeAndSubgroup(ctx *rmngctx.RmngContext, groupID, nodeID, subGroupID string, operationType group_node_db.SubGroupOperationType) (group_node_db.NodesGroups, error) {
	// check if we have permission to access the group
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "parent group does not exist")
	}

	// Validate that the subGroupID is a subgroup of the current group
	subGroupExists, err := IsSubGroup(ctx, groupID, subGroupID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to check if subgroup exists")
	}
	if !subGroupExists {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(nil, "subgroup does not exist")
	}

	groupNodeDB := group_node_db.NewGroupNodeDB(ctx)
	updatedResult, err := groupNodeDB.UpdateSubGroup(groupID, nodeID, subGroupID, operationType)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to update subgroup")
	}

	// updatedResult is the new state of the node
	// we need to return the OLD state of the node
	oldResult := updatedResult
	if operationType == group_node_db.SubGroupOperationTypeAdd {
		// Remove subGroupID from oldResult.SubGroups - that we had added right now
		oldSubGroups := oldResult.SubGroups
		for i, subGroup := range oldSubGroups {
			if subGroup == subGroupID {
				oldResult.SubGroups = append(oldSubGroups[:i], oldSubGroups[i+1:]...)
				break
			}
		}
	} else if operationType == group_node_db.SubGroupOperationTypeRemove {
		// Add subGroupID to oldResult.SubGroups - that we had removed right now
		oldResult.SubGroups = append(oldResult.SubGroups, subGroupID)
	}

	return oldResult, nil
}

// RemoveNode is the user-flow entry point for disassociating a node from
// a group. Validates that the caller (a user) belongs to the group via
// user_group_mapping, then delegates to RemoveNodeAuthorized.
func RemoveNode(ctx *rmngctx.RmngContext, groupID string, nodeID string) (group_node_db.NodesGroups, error) {
	_, err := GetUserGroupAccess(ctx, groupID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "parent group does not exist")
	}
	return RemoveNodeAuthorized(ctx, groupID, nodeID)
}

// RemoveNodeAuthorized is the system-flow entry point: assumes the caller
// is trusted (e.g. a system Lambda whose identity has already been pinned
// by an IoT rule). Runs the GroupEditNodes RBAC check and the actual
// disassoc; skips the user-group membership check that RemoveNode does.
//
// (RBAC check is here, not at the DB layer, because the same DB function
// is reached on the AddNode old-group-cleanup path where the caller may
// legitimately lack access to the old group.)
func RemoveNodeAuthorized(ctx *rmngctx.RmngContext, groupID string, nodeID string) (group_node_db.NodesGroups, error) {
	groupNodeDB := group_node_db.NewGroupNodeDB(ctx)

	groupNode, err := groupNodeDB.GetGroupNodes(groupID, nodeID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "node does not exist in any group")
	}
	// Normally we have these RBAC checks at DB level, but since RemoveNode is used for
	// AddNode too and the user might not have access to the older group, we define outside
	if err := ctx.IsAuthorized(utils.GroupEditNodes, groupID); err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "insufficient permissions to remove node from group")
	}

	if err := groupNodeDB.RemoveNode(groupID, nodeID); err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to remove node from group")
	}

	return groupNode, nil
}
