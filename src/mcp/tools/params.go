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
	// Summary is set only when some devices succeeded and others did not, because that case
	// reaches the model as an ordinary success and is otherwise easy to read as "it worked".
	Summary string `json:"summary,omitempty"`
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

	// Resolved once for the whole call: every node named must be in this one group, so doing it
	// per node re-read the same two rows for each of them.
	if err := authorizeGroupForUser(rmngCtx, groupID); err != nil {
		// Reported as a per-node failure rather than a returned error, because that is what each
		// node would have produced on its own — the caller sees the same rows, and the same
		// guidance, as before the check was hoisted.
		rlog.Warn(rmngCtx).Err(err).Str("group_id", groupID).Msg("Failed to set params: group not accessible")
		outcomes := make([]NodeResult, 0, len(nodeIDs))
		for _, nodeID := range nodeIDs {
			outcomes = append(outcomes, NodeResult{NodeID: nodeID, Error: setParamsFailure(nodeID, groupID)})
		}
		return SetParamsResult{Requested: len(nodeIDs), Failed: len(nodeIDs), Results: outcomes}, nil
	}

	// Concurrent because "turn the kitchen off" arrives as one call over several devices, and
	// nobody wants the last lamp to wait on the first. Safe because the group permissions are
	// now granted above, before the fan-out: what runs concurrently only reads them.
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
	// The caller's access to the group is established by SetNodeParams before the fan-out; what
	// remains per node is that this node is in it.
	placement, err := authorizeNodeInGroup(rmngCtx, groupID, nodeID)
	if err != nil {
		return paramsFailed(rmngCtx, err, nodeID, groupID)
	}

	// Checked before publishing, never after: MQTT has no acknowledgement, so a device that
	// cannot act on these params says nothing and the caller would be told it worked. Rejecting
	// the node's whole write keeps it from being left half-set on a call the model got wrong.
	// Authorization comes first so a stranger never learns whether a node has a config.
	if message := validateParamsForNode(rmngCtx, nodeID, params); message != "" {
		return NodeResult{NodeID: nodeID, Error: message}
	}

	// The shadow name is derived from the node's group and subgroups, which the authorization
	// above already read off the node's own row. Handing them over spares a second read of that
	// row through the by-node-id index: ensureGroups treats a populated GroupID as loaded. It is
	// also the more precise answer — that index returns whichever row comes first, while this is
	// the group the caller named and access was just checked against.
	target := node.NewNode(nodeID)
	target.GroupID = placement.Group
	target.SubGroupIDs = placement.SubGroups

	if err := target.PublishToDeviceDesired(rmngCtx, params); err != nil {
		return paramsFailed(rmngCtx, err, nodeID, groupID)
	}
	return NodeResult{NodeID: nodeID, Success: true}
}

// paramsFailed records the real cause and returns the row the model sees. The two are
// deliberately different: the response carries only guidance, so this is the one place the
// underlying error is written down.
func paramsFailed(rmngCtx *rmngctx.RmngContext, err error, nodeID, groupID string) NodeResult {
	rlog.Warn(rmngCtx).Err(err).Str("node_id", nodeID).Msg("Failed to set params on node")
	return NodeResult{NodeID: nodeID, Error: setParamsFailure(nodeID, groupID)}
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
