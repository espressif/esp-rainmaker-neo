// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package hooks is bridge's in-process half of the node lifecycle: the work
// core used to reach by invoking a bridge Lambda by convention name.
//
// OnNodeRegister installs itself into nodelifecycle from init(), so a binary
// that registers nodes picks it up by importing this package and everything
// else carries none of it. OnBridgeDisconnect is called directly by the core
// presence handler, which already receives the event this needs.
package hooks

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func init() {
	nodelifecycle.RegisterNodeRegisterHook(OnNodeRegister)
}

// presenceFanoutConcurrency bounds in-flight UpdateThingShadow calls
// per bridge disconnect.
const presenceFanoutConcurrency uint = 10

var bridgePolicy = nodelifecycle.CapabilityPolicy{
	Capability:        "bridge",
	PolicyEnvVar:      "BRIDGE_POLICY_NAME",
	DefaultPolicyName: "rmng-bridge-policy",
}

// OnNodeRegister attaches the bridge IoT policy to a node registering with the
// "bridge" capability and classifies it as node_type=bridge; any other node is a
// no-op. Returning an error fails registration, which is what guarantees the
// policy is on the cert before the bridge device connects.
func OnNodeRegister(rmngCtx *rmngctx.RmngContext, nodeID string, capabilities []string, certArn string) (string, error) {
	attached, err := bridgePolicy.Attach(rmngCtx, capabilities, certArn)
	if err != nil || !attached {
		return "", err
	}
	// node_type=bridge on the node_details row is what the presence cascade
	// filters on. Core stores the value without interpreting it.
	return "bridge", nil
}

// OnBridgeDisconnect marks a bridge's children offline. Callers must already
// have established that the event is the current session — core's presence
// handler does that for every disconnect before it gets here, so re-reading
// nodes_online would be a second read of a row the caller just validated.
func OnBridgeDisconnect(ctx context.Context, event node.PresenceEvent) error {
	parent := event.ClientID
	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, utils.NewSystemActor(), parent)

	// Core's handler runs for every disconnect and knows nothing about node
	// types, so the bridge filter is ours to apply.
	nodeType, err := node_details_db.NewNodeDetailsDB(rmngCtx).GetNodeType(parent)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("parent", parent).
			Msg("presence_cascade: GetNodeType failed; skipping")
		return nil
	}
	if nodeType != "bridge" {
		return nil
	}

	bcDB := bridge_children_db.NewBridgeChildrenDB(rmngCtx)
	children, err := bcDB.GetChildrenByParent(parent)
	if err != nil {
		rlog.Error(rmngCtx).Err(rmerror.NewRMError(err, "presence_cascade: GetChildrenByParent failed")).Send()
		return nil
	}
	if len(children) == 0 {
		rlog.Trace(rmngCtx).Str("parent", parent).
			Msg("presence_cascade: bridge has no children")
		return nil
	}

	// DisconnectInfo carries the bridge's disconnect timing — semantically
	// correct for each child since they are offline because their bridge dropped.
	shadowData := node.BuildDisconnectShadow(event)

	results, _, _ := parallel.ProcessParallel(ctx, children, func(c bridge_children_db.ChildEntry) int {
		childName := c.ChildNodeID
		childNode := node.NewNode(childName)
		childCtx := rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())

		failed := 0
		if err := childNode.WriteToReportedShadow(childCtx, shadowData); err != nil {
			rlog.Error(childCtx).Err(err).Str("parent", parent).Str("child", childName).
				Msg("presence_cascade: reported shadow update failed; continuing")
			failed++
		}
		if err := childNode.WriteToIndexedReportedShadow(childCtx, shadowData); err != nil {
			rlog.Error(childCtx).Err(err).Str("parent", parent).Str("child", childName).
				Msg("presence_cascade: indexed shadow update failed; continuing")
			failed++
		}
		return failed
	}, parallel.ParallelOptions{MaxRoutines: presenceFanoutConcurrency, CollectResults: true})

	totalFailed := 0
	for _, n := range results {
		totalFailed += n
	}

	rlog.Info(rmngCtx).
		Str("parent", parent).
		Int("nChildren", len(children)).
		Int("failed", totalFailed).
		Msg("presence_cascade: children marked offline")
	return nil
}
