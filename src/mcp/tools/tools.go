// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodePlacement is where a node sits in the user's hierarchy: exactly one group (the home)
// and any subgroups within it (the rooms).
type NodePlacement struct {
	GroupID       string   `json:"group_id"`
	GroupName     string   `json:"group_name"`
	SubgroupIDs   []string `json:"subgroup_ids,omitempty"`
	SubgroupNames []string `json:"subgroup_names,omitempty"`
}

// nodeIndex maps every node the caller can reach to its placement. Building it walks the
// user's groups with nodes loaded, which is also what grants node permissions on the
// context — so a node absent from the index is one the caller has no access to, and every
// node present is already authorized for the reads that follow.
type nodeIndex map[string]NodePlacement

// buildNodeIndex lists the caller's groups and indexes their nodes by ID. Pass a groupID to
// restrict the walk to one group.
func buildNodeIndex(rmngCtx *rmngctx.RmngContext, groupID string) (nodeIndex, error) {
	groups, err := listGroups(rmngCtx, groupID)
	if err != nil {
		return nil, err
	}

	index := make(nodeIndex)
	for _, grp := range groups {
		for nodeID := range grp.NodeGroupEntries {
			index[nodeID] = NodePlacement{GroupID: grp.GroupID, GroupName: grp.GroupName}
		}
		// Subgroup membership is layered on afterwards: a node listed in a subgroup is always
		// also listed in the parent group, so the placement above already exists.
		for _, sub := range grp.SubGroups {
			for nodeID := range sub.NodeGroupEntries {
				placement, ok := index[nodeID]
				if !ok {
					continue
				}
				placement.SubgroupIDs = append(placement.SubgroupIDs, sub.SubGroupID)
				placement.SubgroupNames = append(placement.SubgroupNames, sub.SubGroupName)
				index[nodeID] = placement
			}
		}
	}
	return index, nil
}

// listGroups returns the caller's groups with nodes loaded. An empty groupID lists all.
func listGroups(rmngCtx *rmngctx.RmngContext, groupID string) ([]group.Group, error) {
	if groupID == "" {
		return group.ListGroupsForUser(rmngCtx, true)
	}
	groups, err := group.ListGroupForUser(rmngCtx, groupID, true)
	if err != nil {
		return nil, err
	}
	// A non-member yields an empty list, not an error, so absence of error is not access.
	if len(groups) == 0 {
		return nil, fmt.Errorf("group %s does not exist or you do not have access to it", groupID)
	}
	return groups, nil
}

// authorizeNodeForUser verifies the user has access to the given group, grants the
// necessary node permissions on the context, and asserts the node belongs to that group.
func authorizeNodeForUser(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) error {
	groups, err := group.ListGroupForUser(rmngCtx, groupID, true)
	if err != nil {
		return fmt.Errorf("failed to resolve access to group %s: %w", groupID, err)
	}
	// A non-member yields an empty list, not an error, so absence of error is not access.
	if len(groups) == 0 {
		return fmt.Errorf("user does not have access to group %s", groupID)
	}
	// Group access does not imply access to this node.
	if _, err := group_node_db.NewGroupNodeDB(rmngCtx).GetGroupNode(groupID, nodeID); err != nil {
		return fmt.Errorf("node %s is not a member of group %s: %w", nodeID, groupID, err)
	}
	return nil
}

// SplitIDs parses a comma-separated ID list, dropping blanks. Tools accept the comma form so
// a model can act on several devices in one call.
func SplitIDs(value string) []string {
	var ids []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}
