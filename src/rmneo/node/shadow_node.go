// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/convert"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/lambdautil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/nodelifecycle"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// NodeDataResetEvent is the payload for the async per-user data reset
// Lambda (triggers, schedules, timeseries, automations).
type NodeDataResetEvent struct {
	NodeIDs     []string `json:"node_ids"`
	OldGroupID  string   `json:"old_group_id"`
	GroupDelete bool     `json:"group_delete"`
}

// groupMembershipEvent mirrors the JSON contract of the notifications Lambda's
// NotificationEvent for the group_membership_change type. It is kept local to
// avoid an import cycle (the notification package imports node). The json tags
// and the constant values below MUST stay in sync with
// src/notification/notifications/notifications_main.go and src/notification/notification.go.
type groupMembershipEvent struct {
	NodeID           string                 `json:"node_id"`
	NotificationType string                 `json:"notification_type"`
	GroupID          string                 `json:"group_id"`
	SubGroupIDs      []string               `json:"sub_group_ids,omitempty"`
	Action           string                 `json:"action"`
	Notify           map[string]interface{} `json:"notify"`
}

const (
	notificationTypeGroupMembership = "group_membership_change"
	groupMembershipActionAdded      = "added"
	groupMembershipActionRemoved    = "removed"
)

// -----------------------------------------------------------------------------
// Internal building blocks
// -----------------------------------------------------------------------------

// CleanupNodeFromGroupAsync invokes the node_data_reset Lambda for the
// given nodes' prior group. No-op when the input is empty or there is
// no prior group (fresh association).
func CleanupNodeFromGroupAsync(ctx *rmngctx.RmngContext, nodeIDs []string, oldGroupID string, groupDelete bool) {
	if len(nodeIDs) == 0 || oldGroupID == "" {
		return
	}
	if err := lambdautil.InvokeAsync(ctx.Context, os.Getenv("NODE_DATA_RESET_FUNCTION_NAME"), NodeDataResetEvent{
		NodeIDs:     nodeIDs,
		OldGroupID:  oldGroupID,
		GroupDelete: groupDelete,
	}); err != nil {
		rlog.Error(ctx).Err(err).Send()
	}
}

// EmitGroupMembershipChangeAsync fires a group_membership_change event to the
// notifications Lambda so voice-assistant channels (Alexa, GVA) can re-discover
// (added) or drop (removed) the node. Best-effort: any failure is logged, never
// returned — the caller's primary association has already succeeded. No-op when
// the identifiers are missing or NOTIFICATIONS_FUNCTION_NAME is not configured.
func EmitGroupMembershipChangeAsync(ctx *rmngctx.RmngContext, nodeID, groupID string, subGroupIDs []string, action string) {
	if nodeID == "" || groupID == "" {
		return
	}
	fn := os.Getenv("NOTIFICATIONS_FUNCTION_NAME")
	if fn == "" {
		rlog.Warn(ctx).Msg("NOTIFICATIONS_FUNCTION_NAME not set; skipping group membership notification")
		return
	}
	event := groupMembershipEvent{
		NodeID:           nodeID,
		NotificationType: notificationTypeGroupMembership,
		GroupID:          groupID,
		SubGroupIDs:      subGroupIDs,
		Action:           action,
		// Voice-assistant channels that maintain their own device registry.
		Notify: map[string]interface{}{"alexa": struct{}{}, "gva": struct{}{}},
	}
	if err := lambdautil.InvokeAsync(ctx.Context, fn, event); err != nil {
		rlog.Error(ctx).Err(err).Str("nodeID", nodeID).Str("groupID", groupID).Str("action", action).
			Msg("failed to emit group membership change notification")
	}
}

// onGroupMembershipChanged percolates a node's group-membership transition to
// external consumers (Alexa/GVA) so they re-discover (added) or drop (removed)
// the node. It is a single seam for this feature, called from the system-flow
// entry points so every path (user API, node associate, factory reset, and
// downstream child operations that route through the Authorized primitives)
// notifies consistently.
//
// This is deliberately separate from the generic nodelifecycle.OnNodeLeftGroup
// hook (fired by the user-flow wrappers): that hook is user-flow-only and
// remove-only to avoid re-entrancy in its downstream handler, whereas the
// Alexa/GVA notification must fire on all flows and both directions. Notifying
// a child node (whose ID contains "--") is intentional — it is a device that
// external assistants may expose — and safe, since this path invokes the
// notifications Lambda, never a group-mutation.
func onGroupMembershipChanged(ctx *rmngctx.RmngContext, nodeID, groupID string, subGroupIDs []string, action string) {
	EmitGroupMembershipChangeAsync(ctx, nodeID, groupID, subGroupIDs, action)
}

// notifyNodeRemovedFromGroup runs the sync side-effects for a node that
// has just left a group (shadow delete, iparams clear, group_id Thing
// attribute clear, getGroupInfo notify). Errors are logged, not
// returned — the caller's primary operation has already succeeded.
func notifyNodeRemovedFromGroup(ctx *rmngctx.RmngContext, nodeID string, oldGroup group_node_db.NodesGroups) {
	if err := NewNode(nodeID).NotifyCleaupNodeFromGroupSync(ctx, oldGroup); err != nil {
		rlog.Error(ctx).Err(err).Str("nodeID", nodeID).Msg("failed to run group removal side-effects")
	}
}

// removeNodeFromGroupAuthorizedNoReset is the disassoc + sync notify
// primitive shared by the single-node and bulk authorized removers. The
// "NoReset" suffix means it skips the per-user async data reset so the
// bulk variant can issue one batched reset instead of N individual ones.
// Returns the node's prior group/subgroups so callers can fan out the
// membership-change notification to the right subgroup users.
func removeNodeFromGroupAuthorizedNoReset(ctx *rmngctx.RmngContext, nodeID, groupID string) (group_node_db.NodesGroups, error) {
	oldGroup, err := group.RemoveNodeAuthorized(ctx, groupID, nodeID)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "Failed to remove node from group")
	}
	notifyNodeRemovedFromGroup(ctx, nodeID, oldGroup)
	return oldGroup, nil
}

// -----------------------------------------------------------------------------
// System-flow entry points
//
// Called by system Lambdas (identity pinned by an IoT rule). They skip
// the user-group membership check and do NOT fire the node-left-group
// hook — re-firing from within a hook handler would recurse. They DO run
// the Alexa/GVA membership notification (onGroupMembershipChanged), which
// targets only the notifications Lambda and so cannot recurse.
// -----------------------------------------------------------------------------

// ShadowNodeAddToGroupAuthorized runs the full add-to-group side
// effects: disassoc from any old group, sync notify (group_id Thing
// attribute, shadow migration, iparams clear, getGroupInfo notify),
// async per-user data reset, and the Alexa/GVA membership notification.
// Returns the prior group/subgroups.
func ShadowNodeAddToGroupAuthorized(ctx *rmngctx.RmngContext, nodeID, groupID string, capabilities []string) (group_node_db.NodesGroups, error) {
	oldGroup, err := group.AddNode(ctx, groupID, nodeID, capabilities)
	if err != nil {
		return group_node_db.NodesGroups{}, rmerror.NewRMError(err, "failed to add node to group")
	}
	// Same-group re-association — no side-effects (relevant on WiFi
	// reconfiguration where the node re-announces itself).
	if oldGroup.Group == groupID {
		return oldGroup, nil
	}

	if err := NewNode(nodeID).NotifyCleanupGroupAddSync(ctx, oldGroup, groupID); err != nil {
		rlog.Error(ctx).Err(err).Send()
	}
	CleanupNodeFromGroupAsync(ctx, []string{nodeID}, oldGroup.Group, false)

	// Notify Alexa/GVA: the node is now discoverable in the new group. A fresh
	// association has no subgroups yet (those are assigned via UpdateSubGroup).
	// If the node moved from another group, also tell that group's users to drop
	// the now-stale endpoint. (The child-node cascade off the old group is handled
	// separately by nodelifecycle.OnNodeLeftGroup in the user-flow wrappers.)
	onGroupMembershipChanged(ctx, nodeID, groupID, nil, groupMembershipActionAdded)
	if oldGroup.Group != "" {
		onGroupMembershipChanged(ctx, nodeID, oldGroup.Group, oldGroup.SubGroups, groupMembershipActionRemoved)
	}
	return oldGroup, nil
}

// ShadowNodeRemoveFromGroupAuthorized runs disassoc + sync notify +
// async per-user data reset for a single node.
func ShadowNodeRemoveFromGroupAuthorized(ctx *rmngctx.RmngContext, nodeID, groupID string) error {
	oldGroup, err := removeNodeFromGroupAuthorizedNoReset(ctx, nodeID, groupID)
	if err != nil {
		return err
	}
	CleanupNodeFromGroupAsync(ctx, []string{nodeID}, groupID, false)
	// Notify Alexa/GVA to drop the node from their device registries.
	onGroupMembershipChanged(ctx, nodeID, groupID, oldGroup.SubGroups, groupMembershipActionRemoved)
	return nil
}

// ShadowNodeRemoveFromGroupAuthorizedBulk removes N nodes from the same
// group in parallel with bounded fan-out. For each node it runs the
// authorized disassoc + sync notify, then the optional perNode callback
// for caller-specific extras (e.g. downstream Thing/DDB teardown). After
// the fan-out completes, issues a single batched node_data_reset for
// successfully-removed nodes.
//
// An empty groupID is permitted — the per-node disassoc + reset phases
// are skipped, but the parallel fan-out and perNode callback still run.
// Supports cleanup flows where the upstream group association is
// already gone (e.g. a downstream cascade after a parent node has left
// its group).
//
// Best-effort per node: any error from the disassoc OR the perNode
// callback is logged and the rest of the fan-out continues. A node
// whose disassoc failed is omitted from the batched reset.
func ShadowNodeRemoveFromGroupAuthorizedBulk(
	ctx *rmngctx.RmngContext,
	nodeIDs []string,
	groupID string,
	perNode func(nodeID string) error,
) {
	if len(nodeIDs) == 0 {
		return
	}

	// Per node: returns the nodeID on a successful disassoc (so the results slice doubles as the removed list), or "" when the disassoc failed or was skipped.
	// Best-effort: any disassoc or perNode error is logged and the rest of the fan-out continues.
	removed, _, _ := parallel.ProcessParallel(ctx, nodeIDs, func(nodeID string) string {
		removedID := ""
		if groupID != "" {
			oldGroup, err := removeNodeFromGroupAuthorizedNoReset(ctx, nodeID, groupID)
			if err != nil {
				rlog.Error(ctx).Err(err).Str("nodeID", nodeID).Str("groupID", groupID).
					Msg("bulk remove: disassoc failed; continuing")
			} else {
				removedID = nodeID
				// Notify Alexa/GVA of the removal. Safe to run inside a
				// downstream fan-out: it targets only the notifications Lambda,
				// never a group-mutation, so it cannot re-enter this path.
				onGroupMembershipChanged(ctx, nodeID, groupID, oldGroup.SubGroups, groupMembershipActionRemoved)
			}
		}
		if perNode != nil {
			if err := perNode(nodeID); err != nil {
				rlog.Error(ctx).Err(err).Str("nodeID", nodeID).
					Msg("bulk remove: perNode callback failed; continuing")
			}
		}
		return removedID
	})

	if groupID == "" {
		return
	}
	// Compact: omit empty slots (nodes whose disassoc failed) before batching the reset.
	cleaned := removed[:0]
	for _, n := range removed {
		if n != "" {
			cleaned = append(cleaned, n)
		}
	}
	CleanupNodeFromGroupAsync(ctx, cleaned, groupID, false)
}

// -----------------------------------------------------------------------------
// User-flow entry points
//
// Called by API handlers. Layered on top of the system-flow blocks:
// add the user-group membership check up front, and fire the
// node-left-group lifecycle hook after the disassoc.
// -----------------------------------------------------------------------------

// ShadowNodeAddToGroup is the user-flow add. Verifies the caller owns
// the target group, runs the system-flow add, then fires the
// node-left-group lifecycle hook if this is a real group change (a
// downstream handler, if deployed, reacts to it).
func ShadowNodeAddToGroup(ctx *rmngctx.RmngContext, nodeID, groupID string, capabilities []string) error {
	if _, err := group.GetUserGroupAccess(ctx, groupID); err != nil {
		return rmerror.NewRMError(err, "parent group does not exist")
	}
	oldGroup, err := ShadowNodeAddToGroupAuthorized(ctx, nodeID, groupID, capabilities)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to add node to new group")
	}
	if oldGroup.Group != groupID {
		nodelifecycle.OnNodeLeftGroup(ctx, nodeID, oldGroup.Group)
	}
	return nil
}

// ShadowNodeRemoveFromGroup is the user-flow remove. Verifies the
// caller owns the source group, runs the system-flow remove, then
// fires the node-left-group lifecycle hook.
func ShadowNodeRemoveFromGroup(ctx *rmngctx.RmngContext, nodeID, groupID string) error {
	if _, err := group.GetUserGroupAccess(ctx, groupID); err != nil {
		return rmerror.NewRMError(group.ErrGroupAccessDenied, "group does not exist or access denied")
	}
	if err := ShadowNodeRemoveFromGroupAuthorized(ctx, nodeID, groupID); err != nil {
		return err
	}
	nodelifecycle.OnNodeLeftGroup(ctx, nodeID, groupID)
	return nil
}

// ShadowNodeUpdateSubGroup updates a node's subgroup membership and
// runs the shadow-side notify. Subgroup changes don't reorganize
// child nodes, so no lifecycle hook is fired.
func ShadowNodeUpdateSubGroup(ctx *rmngctx.RmngContext, nodeID, groupID, subGroupID string, operationType group_node_db.SubGroupOperationType) error {
	oldGroups, err := group.UpdateNodeAndSubgroup(ctx, groupID, nodeID, subGroupID, operationType)
	if err != nil {
		return rmerror.NewRMError(err, "Failed to add node to sub group")
	}
	if err := NewNode(nodeID).NotifySubGroupUpdate(ctx, oldGroups, subGroupID, operationType); err != nil {
		// Don't fail the operation just because shadow migration failed.
		rlog.Error(ctx).Err(err).Send()
	}
	return nil
}

// -----------------------------------------------------------------------------
// Shadow document types and utilities
// -----------------------------------------------------------------------------

type IoTNodeShadow struct {
	State     *ShadowState `json:"state,omitempty"`
	Metadata  *Metadata    `json:"metadata,omitempty"`
	Version   *int         `json:"version,omitempty"`
	Timestamp *int         `json:"timestamp,omitempty"`
}

type ShadowState struct {
	Reported *ReportedOrDesiredShadow `json:"reported,omitempty"`
	Desired  *ReportedOrDesiredShadow `json:"desired,omitempty"`
}

type Metadata struct {
	Reported map[string]interface{} `json:"reported,omitempty"`
	Desired  map[string]interface{} `json:"desired,omitempty"`
}

type DisconnectInfo struct {
	LastDisconnectReason    string `json:"last_disconnect_reason,omitempty"`
	LastDisconnectTimestamp int64  `json:"last_disconnect_ts,omitempty"`
}

type ReportedOrDesiredShadow struct {
	Data           *Data                  `json:"data,omitempty"`
	Online         *bool                  `json:"online,omitempty"`
	DisconnectInfo *DisconnectInfo        `json:"disconnect_info,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
}

type Data struct {
	Admin  *TagsObj `json:"admin,omitempty"`
	Device *TagsObj `json:"device,omitempty"`
	User   *TagsObj `json:"user,omitempty"`
}

type TagType string

const (
	TagTypeAdmin  TagType = "admin"
	TagTypeDevice TagType = "device"
	TagTypeUser   TagType = "user"
)

type TagsObj struct {
	Tags map[string]interface{} `json:"t,omitempty"`
}

// ShadowOnline reports whether the node is reachable per its reported shadow.
// A nil Online field (never reported — the node has never completed a connect
// handshake) defaults to offline; online is reported only once firmware has
// explicitly published `online=true` on connect.
func ShadowOnline(shadow ReportedOrDesiredShadow) bool {
	return shadow.Online != nil && *shadow.Online
}

// DeviceParamsFromShadow extracts a single device's params map from a shadow's
// Params, returning an empty map when absent or malformed so callers can report
// whatever state they have rather than failing outright.
func DeviceParamsFromShadow(shadow ReportedOrDesiredShadow, deviceName string) map[string]interface{} {
	if shadow.Params == nil {
		return map[string]interface{}{}
	}
	deviceData, ok := shadow.Params[deviceName].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return deviceData
}

// Can be improved
func ComputeJSONDeltaMap(old, new ReportedOrDesiredShadow) (ReportedOrDesiredShadow, error) {
	var oldMap map[string]interface{}
	var newMap map[string]interface{}
	err := utils.ConvertAnyToAny(old, &oldMap)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "Failed to convert old shadow state to map")
	}

	err = utils.ConvertAnyToAny(new, &newMap)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "Failed to convert new shadow state to map")
	}
	delta := convert.ComputeJSONDelta(oldMap, newMap)
	if delta == nil {
		return ReportedOrDesiredShadow{}, nil
	}

	deltaMap, ok := delta.(map[string]interface{})
	if !ok {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(fmt.Errorf("delta is not a map"), "")
	}
	var deltaShadowState ReportedOrDesiredShadow
	err = utils.ConvertAnyToAny(deltaMap, &deltaShadowState)
	if err != nil {
		return ReportedOrDesiredShadow{}, rmerror.NewRMError(err, "Failed to convert delta map to shadow state")
	}
	return deltaShadowState, nil
}
