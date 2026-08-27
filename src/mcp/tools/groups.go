// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// GroupInfo is one group (a home) in a list_groups response. It carries structure only —
// never parameters or connectivity, which belong to the device and are served by ListDevices.
type GroupInfo struct {
	GroupID     string   `json:"group_id"`
	GroupName   string   `json:"group_name"`
	AccessType  string   `json:"access_type,omitempty"`
	DeviceCount int      `json:"device_count"`
	NodeIDs     []string `json:"node_ids,omitempty"`
	// Subgroups is always serialised, empty included: an absent key reads as "rooms unknown"
	// and sends agents back for another look, whereas [] plainly says this home has no rooms.
	// node_ids stays omitempty by contrast — it is opt-in behind include_devices.
	Subgroups []SubGroupInfo         `json:"subgroups"`
	Matter    map[string]interface{} `json:"matter,omitempty"`
}

// SubGroupInfo is one subgroup (a room) within a group.
type SubGroupInfo struct {
	SubgroupID   string   `json:"subgroup_id"`
	SubgroupName string   `json:"subgroup_name"`
	DeviceCount  int      `json:"device_count"`
	NodeIDs      []string `json:"node_ids,omitempty"`
}

// GroupFilter narrows a ListGroups call. GroupID and GroupName are mutually exclusive.
type GroupFilter struct {
	GroupID        string
	GroupName      string
	IncludeDevices bool
}

// ListGroups returns the caller's groups and subgroups with device counts, optionally
// listing the node IDs in each.
func ListGroups(rmngCtx *rmngctx.RmngContext, filter GroupFilter) ([]GroupInfo, error) {
	if filter.GroupID != "" && filter.GroupName != "" {
		return nil, guidancef("pass either group_id or group_name, not both")
	}

	groups, err := listGroups(rmngCtx, filter.GroupID)
	if err != nil {
		return nil, err
	}

	groupInfos := make([]GroupInfo, 0, len(groups))
	for _, grp := range groups {
		if !matchesGroupName(grp.GroupName, filter.GroupName) {
			continue
		}
		groupInfos = append(groupInfos, buildGroupInfo(rmngCtx, grp, filter.IncludeDevices))
	}

	if filter.GroupName != "" && len(groupInfos) == 0 {
		return nil, guidancef("no group named %q — call list_groups without filters to see the available groups", filter.GroupName)
	}
	return groupInfos, nil
}

func matchesGroupName(groupName, wanted string) bool {
	if wanted == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(groupName), strings.TrimSpace(wanted))
}

func buildGroupInfo(rmngCtx *rmngctx.RmngContext, grp group.Group, includeDevices bool) GroupInfo {
	subgroupInfos := make([]SubGroupInfo, 0, len(grp.SubGroups))
	for _, subgroup := range grp.SubGroups {
		info := SubGroupInfo{
			SubgroupID:   subgroup.SubGroupID,
			SubgroupName: subgroup.SubGroupName,
			DeviceCount:  len(subgroup.NodeGroupEntries),
		}
		if includeDevices {
			info.NodeIDs = group_node_db.GetNodeIDs(subgroup.NodeGroupEntries)
		}
		subgroupInfos = append(subgroupInfos, info)
	}

	groupInfo := GroupInfo{
		GroupID:     grp.GroupID,
		GroupName:   grp.GroupName,
		AccessType:  string(grp.AccessType),
		DeviceCount: len(grp.NodeGroupEntries),
		Subgroups:   subgroupInfos,
	}
	if includeDevices {
		groupInfo.NodeIDs = group_node_db.GetNodeIDs(grp.NodeGroupEntries)
	}

	if _, hasMatter := grp.CapabilityData[group.MatterCapabilityName]; hasMatter {
		cap, _ := group.GetCapability(group.MatterCapabilityName)
		matterData, err := cap.GetResponseData(rmngCtx, &grp)
		if err != nil {
			// Matter data is supplementary; losing it must not cost the caller the whole listing.
			rlog.Error(rmngCtx).Err(err).Msg("Failed to get matter capability data for group")
		} else {
			groupInfo.Matter = matterData
		}
	}

	return groupInfo
}
