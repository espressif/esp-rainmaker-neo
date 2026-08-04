// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// GroupInfo represents a user's group with its nodes and subgroups.
type GroupInfo struct {
	GroupID   string                 `json:"group_id"`
	GroupName string                 `json:"group_name"`
	NodeIDs   []string               `json:"node_ids,omitempty"`
	Subgroups []SubGroupInfo         `json:"subgroups,omitempty"`
	Matter    map[string]interface{} `json:"matter,omitempty"`
}

// SubGroupInfo represents a subgroup within a group.
type SubGroupInfo struct {
	SubgroupID   string   `json:"subgroup_id"`
	SubgroupName string   `json:"subgroup_name"`
	NodeIDs      []string `json:"node_ids,omitempty"`
}

// GetGroups lists all groups for the authenticated user, including nodes,
// subgroups, and matter capability data.
func GetGroups(rmngCtx *rmngctx.RmngContext) ([]GroupInfo, error) {
	groups, err := group.ListGroupsForUser(rmngCtx, true)
	if err != nil {
		return nil, err
	}

	groupInfos := make([]GroupInfo, 0, len(groups))
	for _, grp := range groups {
		subgroupInfos := make([]SubGroupInfo, 0, len(grp.SubGroups))
		for _, subgroup := range grp.SubGroups {
			subgroupInfos = append(subgroupInfos, SubGroupInfo{
				SubgroupID:   subgroup.SubGroupID,
				SubgroupName: subgroup.SubGroupName,
				NodeIDs:      group_node_db.GetNodeIDs(subgroup.NodeGroupEntries),
			})
		}

		groupInfo := GroupInfo{
			GroupID:   grp.GroupID,
			GroupName: grp.GroupName,
			NodeIDs:   group_node_db.GetNodeIDs(grp.NodeGroupEntries),
			Subgroups: subgroupInfos,
		}

		// If matter capability is present, populate Matter data
		if _, hasMatter := grp.CapabilityData[group.MatterCapabilityName]; hasMatter {
			cap, _ := group.GetCapability(group.MatterCapabilityName)
			matterData, err := cap.GetResponseData(rmngCtx, &grp)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to get matter capability data for group")
			} else {
				groupInfo.Matter = matterData
			}
		}

		groupInfos = append(groupInfos, groupInfo)
	}

	return groupInfos, nil
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

// GetNodeParams retrieves all parameters for a node from its reported shadow.
func GetNodeParams(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) (map[string]interface{}, error) {
	if err := authorizeNodeForUser(rmngCtx, groupID, nodeID); err != nil {
		return nil, err
	}

	n := node.NewNode(nodeID)
	shadowData, err := n.ReadFromReportedShadow(rmngCtx)
	if err != nil {
		return nil, err
	}

	if shadowData.Params == nil {
		return map[string]interface{}{}, nil
	}

	return shadowData.Params, nil
}

// SetNodeParams sets parameters for a node by publishing to its desired shadow.
func SetNodeParams(rmngCtx *rmngctx.RmngContext, groupID, nodeID string, params map[string]interface{}) error {
	if err := authorizeNodeForUser(rmngCtx, groupID, nodeID); err != nil {
		return err
	}

	n := node.NewNode(nodeID)
	return n.PublishToDeviceDesired(rmngCtx, params)
}
