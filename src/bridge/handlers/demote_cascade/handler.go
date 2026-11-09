// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package main hosts the bridge demote-cascade Lambda. Core invokes it when a node loses its node_type. For a bridge re-claimed as a plain node, nothing relays for its children any more and the presence cascade no longer marks them offline (it filters on node_type=bridge), so this Lambda deletes them (cascade.DeleteChildren). Only bridges have children, so for any other node it finds none and does nothing.
//
// See spec docs/en/specs/bridge.md §5.8.
package main

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/cascade"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func handleDemoteCascade(ctx context.Context, event nodelifecycle.NodeTypeChangedEvent) error {
	parent := event.NodeID
	if parent == "" {
		return nil
	}

	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, node.NewNode(parent), parent)
	// A re-claim leaves the node in its group, and children are tethered to their bridge's group.
	ng, err := bridge_children_db.NewBridgeChildrenDB(rmngCtx).GetMyGroup()
	if err != nil {
		return rmerror.NewRMError(err, "demote cascade: failed to look up the node's group")
	}
	return cascade.DeleteChildren(rmngCtx, parent, ng.Group)
}
