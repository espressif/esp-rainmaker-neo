// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package main hosts the bridge cascade-delete Lambda. Triggered async
// when a bridge node is removed from its group, this Lambda deletes the
// bridge's children (cascade.DeleteChildren).
//
// See spec docs/en/specs/bridge.md §5.8.
package main

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/cascade"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func handleCascadeDelete(ctx context.Context, event nodelifecycle.NodeLeftGroupEvent) error {
	parent := event.NodeID
	if parent == "" {
		return nil
	}
	// Core fires the hook with no old group on a first placement, which §5.8 does not cascade; doing so would delete children added right after association.
	if event.OldGroupID == "" {
		return nil
	}

	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, node.NewNode(parent), parent)
	// The bridge has been removed from its old group before cascade runs, so the event is the only carrier of that group's identity.
	return cascade.DeleteChildren(rmngCtx, parent, event.OldGroupID)
}
