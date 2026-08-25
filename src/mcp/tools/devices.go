// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// maxDeviceFanout bounds the concurrent per-node reads. Each device costs one DynamoDB read
// plus one shadow read, so the cap keeps a large home from saturating the Lambda's
// connection pool while still finishing well inside the function timeout.
const maxDeviceFanout = 10

// configService names the node_details service column holding node config; going through the
// service keeps the column name from drifting away from the REST API's.
var configService = config.NewConfigService()

// DeviceInfo is one device in a ListDevices response: where it sits, what it is, and what it
// is currently doing.
type DeviceInfo struct {
	NodeID        string                 `json:"node_id"`
	GroupID       string                 `json:"group_id"`
	GroupName     string                 `json:"group_name,omitempty"`
	SubgroupIDs   []string               `json:"subgroup_ids,omitempty"`
	SubgroupNames []string               `json:"subgroup_names,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Type          string                 `json:"type,omitempty"`
	Model         string                 `json:"model,omitempty"`
	FWVersion     string                 `json:"fw_version,omitempty"`
	Connected     bool                   `json:"connected"`
	Params        map[string]interface{} `json:"params,omitempty"`
	Config        map[string]interface{} `json:"config,omitempty"`
	// Error records a per-device read failure. One unreachable device must not blind the
	// caller to the rest of the home, so the row is returned with whatever was resolved.
	Error string `json:"error,omitempty"`
}

// DeviceFilter narrows a ListDevices call. Every field is optional and all of them combine.
type DeviceFilter struct {
	NodeIDs    []string
	GroupID    string
	SubgroupID string
	Name       string
	Type       string
	Fields     string
}

// ListDevices resolves the caller's devices, reads their live state, and applies the filters.
// Results are ordered by node ID so repeated calls are stable.
func ListDevices(rmngCtx *rmngctx.RmngContext, filter DeviceFilter) ([]map[string]interface{}, error) {
	index, err := buildNodeIndex(rmngCtx, filter.GroupID)
	if err != nil {
		return nil, err
	}

	candidates, err := selectCandidates(index, filter)
	if err != nil {
		return nil, err
	}

	devices, _, err := parallel.ProcessParallel(rmngCtx.Context, candidates,
		func(nodeID string) DeviceInfo { return readDevice(rmngCtx, nodeID, index[nodeID]) },
		parallel.ParallelOptions{MaxRoutines: maxDeviceFanout, CollectResults: true})
	if err != nil {
		return nil, err
	}

	// Order before projecting: a `fields` selection may drop node_id, and the caller still
	// wants a stable list across repeated calls.
	sort.Slice(devices, func(i, j int) bool { return devices[i].NodeID < devices[j].NodeID })

	rows := make([]map[string]interface{}, 0, len(devices))
	for _, device := range devices {
		if !deviceMatches(device, filter) {
			continue
		}
		row, err := deviceToMap(device)
		if err != nil {
			return nil, err
		}
		rows = append(rows, projectFields(row, filter.Fields))
	}
	return rows, nil
}

// deviceNotFoundError explains an unreachable node in terms the model can act on: the node is
// either not the caller's or not in the group they named.
func deviceNotFoundError(nodeID, groupID string) error {
	if groupID != "" {
		return fmt.Errorf("device %s is not in group %s — call list_devices without filters to see the devices you can reach", nodeID, groupID)
	}
	return fmt.Errorf("device %s does not exist or you do not have access to it", nodeID)
}

// selectCandidates narrows the caller's reachable nodes to those the filter can exclude
// without any per-node I/O.
func selectCandidates(index nodeIndex, filter DeviceFilter) ([]string, error) {
	if len(filter.NodeIDs) > 0 {
		for _, nodeID := range filter.NodeIDs {
			if _, ok := index[nodeID]; !ok {
				return nil, deviceNotFoundError(nodeID, filter.GroupID)
			}
		}
		return withinSubgroup(filter.NodeIDs, index, filter.SubgroupID), nil
	}

	all := make([]string, 0, len(index))
	for nodeID := range index {
		all = append(all, nodeID)
	}
	sort.Strings(all)
	return withinSubgroup(all, index, filter.SubgroupID), nil
}

func withinSubgroup(nodeIDs []string, index nodeIndex, subgroupID string) []string {
	if subgroupID == "" {
		return nodeIDs
	}
	kept := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if slicesContain(index[nodeID].SubgroupIDs, subgroupID) {
			kept = append(kept, nodeID)
		}
	}
	return kept
}

// readDevice fetches one device's config and reported shadow. Both reads are best effort:
// a node registered but never connected has neither, and must still be listed.
func readDevice(rmngCtx *rmngctx.RmngContext, nodeID string, placement NodePlacement) DeviceInfo {
	device := DeviceInfo{
		NodeID:        nodeID,
		GroupID:       placement.GroupID,
		GroupName:     placement.GroupName,
		SubgroupIDs:   placement.SubgroupIDs,
		SubgroupNames: placement.SubgroupNames,
	}

	nodeDetails, err := node_details_db.NewNodeDetailsDB(rmngCtx).GetNodeDetails(nodeID)
	if err != nil {
		device.Error = "failed to read device configuration"
	} else if nodeDetails != nil {
		if cfgData, err := nodeDetails.GetServiceData(configService.GetName()); err == nil && cfgData != nil {
			applyConfig(&device, cfgData)
		}
	}

	shadow, err := node.NewNode(nodeID).ReadFromReportedShadow(rmngCtx)
	if err != nil {
		if device.Error == "" {
			device.Error = "failed to read device state"
		}
		return device
	}
	device.Connected = node.ShadowOnline(shadow)
	device.Params = shadow.Params
	return device
}

func applyConfig(device *DeviceInfo, cfgData interface{}) {
	var asMap map[string]interface{}
	if err := utils.ConvertAnyToAny(cfgData, &asMap); err == nil {
		device.Config = asMap
	}
	nodeCfg, err := config.ToNodeCfg(cfgData)
	if err != nil {
		return
	}
	device.Name = nodeCfg.Info.Name
	device.Type = nodeCfg.Info.Type
	device.Model = nodeCfg.Info.Model
	device.FWVersion = nodeCfg.Info.FWVersion
}

// --- filtering ---

func deviceMatches(device DeviceInfo, filter DeviceFilter) bool {
	if filter.Name != "" && !matchesDeviceName(device, filter.Name) {
		return false
	}
	if filter.Type != "" && !matchesDeviceType(device, filter.Type) {
		return false
	}
	return true
}

// matchesDeviceName looks at both names a device can carry: the node-level name in its config
// and the per-device Name parameter, which is what the user actually sees in the app.
func matchesDeviceName(device DeviceInfo, wanted string) bool {
	if containsFold(device.Name, wanted) {
		return true
	}
	for deviceName, deviceParams := range device.Params {
		if containsFold(deviceName, wanted) {
			return true
		}
		params, ok := deviceParams.(map[string]interface{})
		if !ok {
			continue
		}
		if name, ok := params["Name"].(string); ok && containsFold(name, wanted) {
			return true
		}
	}
	return false
}

func matchesDeviceType(device DeviceInfo, wanted string) bool {
	if containsFold(device.Type, wanted) {
		return true
	}
	nodeCfg, err := config.ToNodeCfg(device.Config)
	if err != nil {
		return false
	}
	for _, cfgDevice := range nodeCfg.Devices {
		if containsFold(cfgDevice.Type, wanted) {
			return true
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	return haystack != "" && strings.Contains(strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(needle)))
}

// --- field projection ---

func deviceToMap(device DeviceInfo) (map[string]interface{}, error) {
	var row map[string]interface{}
	if err := utils.ConvertAnyToAny(device, &row); err != nil {
		return nil, err
	}
	return row, nil
}

// projectFields keeps only the requested keys. A field with dots is a path into the row
// (params.Light.Power) and is returned under the path it was asked for, so the caller can
// match request to response without re-walking the structure.
func projectFields(row map[string]interface{}, fields string) map[string]interface{} {
	requested := SplitIDs(fields)
	if len(requested) == 0 {
		return row
	}

	projected := make(map[string]interface{}, len(requested))
	for _, field := range requested {
		if value := lookupPath(row, field); value != nil {
			projected[field] = value
		}
	}
	return projected
}

func lookupPath(row map[string]interface{}, path string) interface{} {
	var current interface{} = row
	for _, segment := range strings.Split(path, ".") {
		asMap, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		if current, ok = asMap[segment]; !ok {
			return nil
		}
	}
	return current
}

// --- helpers ---

func slicesContain(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
