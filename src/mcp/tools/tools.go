// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
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

// authorizeGroupForUser proves the caller can reach the group, which is also what grants the
// group permissions every check below tests against.
//
// Split from the node check so a call spanning several nodes resolves the group once rather than
// once per node — the group is the same for all of them, and this is two DynamoDB reads.
func authorizeGroupForUser(rmngCtx *rmngctx.RmngContext, groupID string) error {
	// Without the nodes: this only has to establish access. Loading them reads the group's whole
	// node listing and discards it, and the node the caller actually named is checked below by
	// key, which is both cheaper and stricter.
	groups, err := group.ListGroupForUser(rmngCtx, groupID, false)
	if err != nil {
		return fmt.Errorf("failed to resolve access to group %s: %w", groupID, err)
	}
	// A non-member yields an empty list, not an error, so absence of error is not access.
	if len(groups) == 0 {
		return fmt.Errorf("user does not have access to group %s", groupID)
	}
	return nil
}

// authorizeNodeInGroup asserts the node belongs to the group, and returns the placement recorded
// on its row. Group access does not imply access to a node, so this is a separate check.
//
// The placement is returned rather than discarded because the row it comes from is the same one
// node.Node would otherwise fetch again through the by-node-id index to build a shadow name.
func authorizeNodeInGroup(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) (group_node_db.NodesGroups, error) {
	groupNode, err := group_node_db.NewGroupNodeDB(rmngCtx).GetGroupNode(groupID, nodeID)
	if err != nil {
		return group_node_db.NodesGroups{}, fmt.Errorf("node %s is not a member of group %s: %w", nodeID, groupID, err)
	}
	return groupNode.ToNodesGroups(), nil
}

// authorizeNodeForUser verifies the user has access to the given group, grants the
// necessary node permissions on the context, and asserts the node belongs to that group.
func authorizeNodeForUser(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) error {
	if err := authorizeGroupForUser(rmngCtx, groupID); err != nil {
		return err
	}
	_, err := authorizeNodeInGroup(rmngCtx, groupID, nodeID)
	return err
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

// validateParamsForNode checks a params payload against what the node declared in its config,
// returning the message to hand back to the model, or "" when the write may proceed.
//
// The whole point is that set_params is a generic write: a model can name a device or parameter
// the node never had, and the cloud used to publish it, get ignored by firmware, and report
// success. Checking here is the only place it can be caught — there is no acknowledgement from
// the device to check afterwards.
//
// Every branch that cannot judge the write lets it through, and says why in the log. Config is
// firmware-reported and its ingest is never schema-checked, so sparse and malformed configs are
// normal; refusing on missing metadata would make working devices uncontrollable, which is worse
// than the silent no-op being fixed. Only a config that positively contradicts the write rejects.
func validateParamsForNode(rmngCtx *rmngctx.RmngContext, nodeID string, params map[string]interface{}) string {
	skip := func(reason string) string {
		rlog.Debug(rmngCtx).Str("node_id", nodeID).Str("validation_skipped", reason).
			Msg("Publishing params without validating them against node config")
		return ""
	}

	nodeDetails, err := node_details_db.NewNodeDetailsDB(rmngCtx).GetNodeDetails(nodeID)
	if err != nil || nodeDetails == nil {
		// A DynamoDB blip must not make a lamp uncontrollable: availability of the write path
		// must not become worse than it was before this check existed.
		return skip("config_unreadable")
	}
	cfgData, err := nodeDetails.GetServiceData(configService.GetName())
	if err != nil || cfgData == nil {
		// A node registered but never connected has no config and is still worth writing to.
		return skip("config_absent")
	}
	nodeCfg, err := config.ToNodeCfg(cfgData)
	if err != nil {
		return skip("config_undecodable")
	}
	if nodeCfg.SkipValidation() {
		return skip("config_not_judgeable")
	}

	violations := nodeCfg.ValidateParams(params)
	if len(violations) == 0 {
		return ""
	}
	for _, violation := range violations {
		rlog.Info(rmngCtx).Str("node_id", nodeID).Str("validation_rejected", string(violation.Kind)).
			Str("device", violation.Device).Str("param", violation.Param).
			Msg("Refused a params write the node's config contradicts")
	}
	return config.ViolationsMessage(nodeID, violations)
}
