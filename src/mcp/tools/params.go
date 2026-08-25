// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"sort"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeResult is the outcome of a write against one device. A multi-device call reports each
// device separately so a single unreachable node does not hide the writes that did land.
type NodeResult struct {
	NodeID  string `json:"node_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SetParamsResult summarises a SetNodeParams call across one or more devices.
type SetParamsResult struct {
	Requested int          `json:"requested"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Results   []NodeResult `json:"results"`
}

// maxParamsFanout bounds the concurrent writes behind one "turn everything off" request.
const maxParamsFanout = 10

// SetNodeParams publishes params to the desired shadow of every named node in the group.
// Each node is authorized independently, so a caller cannot smuggle a foreign node through
// alongside their own.
func SetNodeParams(rmngCtx *rmngctx.RmngContext, groupID string, nodeIDs []string, params map[string]interface{}) (SetParamsResult, error) {
	if len(nodeIDs) == 0 {
		return SetParamsResult{}, fmt.Errorf("no node_id given")
	}

	// Concurrent because "turn the kitchen off" arrives as one call over several devices, and
	// nobody wants the last lamp to wait on the first. Safe because the permission set the
	// authorization below writes is mutex-guarded on the context.
	outcomes, _, err := parallel.ProcessParallel(rmngCtx.Context, nodeIDs,
		func(nodeID string) NodeResult { return publishParams(rmngCtx, groupID, nodeID, params) },
		parallel.ParallelOptions{MaxRoutines: maxParamsFanout, CollectResults: true})
	if err != nil {
		return SetParamsResult{}, err
	}

	// Report in the order the caller listed the devices, not the order the writes finished.
	sort.SliceStable(outcomes, func(i, j int) bool {
		return indexOf(nodeIDs, outcomes[i].NodeID) < indexOf(nodeIDs, outcomes[j].NodeID)
	})

	result := SetParamsResult{Requested: len(nodeIDs), Results: outcomes}
	for _, outcome := range outcomes {
		if outcome.Success {
			result.Succeeded++
			continue
		}
		result.Failed++
	}
	return result, nil
}

func publishParams(rmngCtx *rmngctx.RmngContext, groupID, nodeID string, params map[string]interface{}) NodeResult {
	err := authorizeNodeForUser(rmngCtx, groupID, nodeID)
	if err == nil {
		err = node.NewNode(nodeID).PublishToDeviceDesired(rmngCtx, params)
	}
	if err != nil {
		// Warned rather than returned: the failure becomes a row in the response, so this is
		// the only place the underlying cause is recorded.
		rlog.Warn(rmngCtx).Err(err).Str("node_id", nodeID).Msg("Failed to set params on node")
		return NodeResult{NodeID: nodeID, Error: setParamsFailure(nodeID, groupID)}
	}
	return NodeResult{NodeID: nodeID, Success: true}
}

func indexOf(values []string, wanted string) int {
	for index, value := range values {
		if value == wanted {
			return index
		}
	}
	return len(values)
}

// setParamsFailure is what the model sees. It never carries internal error detail — the
// underlying error is logged instead — but it does say what to try next.
func setParamsFailure(nodeID, groupID string) string {
	return fmt.Sprintf("could not set params on %s: it is not a device in group %s, or it is unreachable. Call list_devices to confirm the node_id and group_id.", nodeID, groupID)
}
