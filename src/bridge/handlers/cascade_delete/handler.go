// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package main hosts the bridge cascade-delete Lambda. Triggered async
// when a bridge node is removed from its group, this Lambda fans out
// over the bridge's children: for each, runs the standard system-flow
// disassoc + sync notification (via
// node.ShadowNodeRemoveFromGroupAuthorizedBulk), and on top of that
// drops the IoT Thing and the bridge_children row. The bulk helper
// also issues the single batched node_data_reset for all
// successfully-removed children.
//
// All per-child operations are best-effort: failures are logged and the
// fan-out continues. The cascade is the outcome of a user-initiated
// disassoc — partial cleanup leaves leaked rows/Things rather than
// blocking the user-facing operation.
//
// See spec docs/en/specs/bridge.md §5.8.
package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func handleCascadeDelete(ctx context.Context, event nodelifecycle.NodeLeftGroupEvent) error {
	parent := event.NodeID
	oldGroup := event.OldGroupID

	if parent == "" {
		return nil
	}

	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, node.NewNode(parent), parent)
	bcDB := bridge_children_db.NewBridgeChildrenDB(rmngCtx)

	children, err := bcDB.GetChildrenByParent(parent)
	if err != nil {
		return rmerror.NewRMError(err, "cascade: failed to query bridge_children")
	}
	if len(children) == 0 {
		rlog.Trace(rmngCtx).Str("parent", parent).Msg("cascade: no children to remove")
		return nil
	}

	// The bridge has been removed from oldGroup before cascade runs, so
	// no DB read on the bridge's current state can recover that grant —
	// the trigger event is the only carrier of the prior group identity.
	// Grant only what the remove chain actually checks: GroupGet (via
	// getGroupNodeEntry), GroupListSubEntities (via GetGroupNode), and
	// GroupEditNodes (RemoveNodeAuthorized explicit). Wildcard GroupAll
	// would escalate a secondary user past their primary's restrictions.
	// (An empty oldGroup is tolerated by the bulk helper below; only the
	// bridge-specific teardown runs in that case.)
	if oldGroup != "" {
		rmngCtx.SetAllowMultiple(
			[]string{
				utils.GroupGet.String(),
				utils.GroupListSubEntities.String(),
				utils.GroupEditNodes.String(),
			},
			oldGroup,
		)
	}

	childNames := make([]string, len(children))
	for i, c := range children {
		childNames[i] = c.ChildNodeID
	}

	node.ShadowNodeRemoveFromGroupAuthorizedBulk(rmngCtx, childNames, oldGroup, func(child string) error {
		// Bridge-specific teardown on top of the standard removal: drop
		// the IoT Thing (DeleteThing is idempotent on
		// ResourceNotFoundException, so a half-deleted prior run
		// doesn't trip us) and the bridge_children row.
		if err := iotutil.DeleteThing(rmngCtx.Context, child); err != nil {
			rlog.Error(rmngCtx).Err(err).Str("parent", parent).Str("child", child).
				Msg("cascade: DeleteThing failed; continuing")
			return nil
		}
		if err := bcDB.DeleteChild(parent, child); err != nil {
			rlog.Error(rmngCtx).Err(err).Str("parent", parent).Str("child", child).
				Msg("cascade: DeleteChild failed; continuing")
		}
		return nil
	})

	rlog.Info(rmngCtx).
		Str("parent", parent).
		Str("oldGroup", oldGroup).
		Int("children", len(children)).
		Msg("bridge cascade-delete complete")
	return nil
}
