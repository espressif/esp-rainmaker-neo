// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package nodelifecycle hosts generic node-lifecycle hooks (node-left-group, node-type-changed, node-offline). Each async-invokes a downstream Lambda named by an env var if set, else no-ops; core links no downstream code.
package nodelifecycle

import (
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/lambdautil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeLeftGroupEvent is the payload the node-left-group hook Lambda receives.
type NodeLeftGroupEvent struct {
	NodeID     string `json:"node_id"`
	OldGroupID string `json:"old_group_id"` // empty if the node wasn't in a group
}

// NodeTypeChangedEvent is the payload the node-type-changed hook Lambda receives.
type NodeTypeChangedEvent struct {
	NodeID string `json:"node_id"`
}

// OnNodeLeftGroup async-invokes the node-left-group hook Lambda named by NODE_LEFT_GROUP_HOOK_FUNCTION_NAME; a no-op (best-effort) when unset.
func OnNodeLeftGroup(ctx *rmngctx.RmngContext, nodeID, oldGroupID string) {
	invokeHook(ctx, "NODE_LEFT_GROUP_HOOK_FUNCTION_NAME", nodeID, NodeLeftGroupEvent{
		NodeID:     nodeID,
		OldGroupID: oldGroupID,
	})
}

// OnNodeTypeChanged async-invokes the node-type-changed hook Lambda named by NODE_TYPE_CHANGED_HOOK_FUNCTION_NAME, for a node whose non-empty node_type was changed or cleared; a no-op (best-effort) when unset.
func OnNodeTypeChanged(ctx *rmngctx.RmngContext, nodeID string) {
	invokeHook(ctx, "NODE_TYPE_CHANGED_HOOK_FUNCTION_NAME", nodeID, NodeTypeChangedEvent{NodeID: nodeID})
}

func invokeHook(ctx *rmngctx.RmngContext, fnNameEnv, nodeID string, payload any) {
	fnName := os.Getenv(fnNameEnv)
	if fnName == "" {
		return
	}
	if err := lambdautil.InvokeAsync(ctx.Context, fnName, payload); err != nil {
		rlog.Error(ctx.Context).Err(err).Str("nodeID", nodeID).Str("hook", fnName).
			Msg("failed to invoke node lifecycle hook Lambda")
	}
}
