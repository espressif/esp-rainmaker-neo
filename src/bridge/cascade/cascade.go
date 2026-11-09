// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package cascade tears down a bridge's children. Shared by the cascade-delete Lambda (the bridge left its group) and the demote-cascade Lambda (the bridge was re-claimed as a plain node). See docs/en/specs/bridge.md §5.8.
package cascade

import (
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// DeleteChildren removes each of parent's children from groupID (the standard disassoc, sync notification and batched node_data_reset), then deletes its IoT Thing and its bridge_children row. An empty groupID skips the group removal.
//
// Per-child failures are logged and the fan-out continues: the cascade follows a user action that has already happened, so a partial cleanup leaks rows and Things rather than failing it.
func DeleteChildren(rmngCtx *rmngctx.RmngContext, parent, groupID string) error {
	bcDB := bridge_children_db.NewBridgeChildrenDB(rmngCtx)

	children, err := bcDB.GetChildrenByParent(parent)
	if err != nil {
		return rmerror.NewRMError(err, "cascade: failed to query bridge_children")
	}
	if len(children) == 0 {
		rlog.Trace(rmngCtx).Str("parent", parent).Msg("cascade: no children to remove")
		return nil
	}

	// Grant only what the remove chain actually checks: GroupGet (via
	// getGroupNodeEntry), GroupListSubEntities (via GetGroupNode), and
	// GroupEditNodes (RemoveNodeAuthorized explicit). Wildcard GroupAll
	// would escalate a secondary user past their primary's restrictions.
	if groupID != "" {
		if err := rmngCtx.SetAllowMultiple(
			[]string{
				utils.GroupGet.String(),
				utils.GroupListSubEntities.String(),
				utils.GroupEditNodes.String(),
			},
			groupID,
		); err != nil {
			return rmerror.NewRMError(err, "failed to grant permissions")
		}
	}

	childNames := make([]string, len(children))
	for i, c := range children {
		childNames[i] = c.ChildNodeID
	}

	node.ShadowNodeRemoveFromGroupAuthorizedBulk(rmngCtx, childNames, groupID, func(child string) error {
		// DeleteThing is idempotent on ResourceNotFoundException, so a half-deleted prior run doesn't trip us.
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
		Str("group", groupID).
		Int("children", len(children)).
		Msg("bridge cascade-delete complete")
	return nil
}
