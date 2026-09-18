// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package main hosts the bridge to_cloud control-plane handler.
// Today it dispatches addChild / removeChild only; future bridge-only
// events ride the same Lambda by adding cases to the switch in
// handleBridgeEvent.
//
// Wiring: bridge_to_cloud_rule (Rule D, spec §3.4) selects
//
//	SELECT topic(3) as thing_name, * as data FROM 'rainmaker/bridges/+/to_cloud'
//	WHERE clientid() = topic(3)
//
// and invokes this Lambda. The WHERE clause pins the publishing bridge's
// clientid to the parent segment of the topic, so event.ThingName arrives
// already authenticated — no clientid re-check needed here.
package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/iotutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// validSuffixRe enforces spec §4.2 step 4: alphanumerics + underscore
// only, 1–32 chars. Single hyphens are banned to prevent any '--'
// substring from appearing in the suffix, which would break the
// pure-SQL parent-extraction in Rules A/B (spec §3.4).
var validSuffixRe = regexp.MustCompile(`^[a-zA-Z0-9_]{1,32}$`)

// handleBridgeEvent is invoked by the IoT rule. A single payload carries
// at most one bridge event in practice; if more are present we honour
// only the first to avoid the bridgeAck-key collision (the response
// envelope's `bridgeAck` is a single object, not an array).
func handleBridgeEvent(ctx context.Context, event node.PublishInputEvent) error {
	bridgeNode := node.NewNode(event.ThingName)
	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, bridgeNode, event.ThingName)
	response := node.NewDataToDevice(bridgeNode)

	events, ok := event.Data["event"].([]interface{})
	if !ok {
		return fmt.Errorf("'event' field missing or not an array")
	}

	handled := false
	for _, raw := range events {
		eventName, ok := raw.(string)
		if !ok {
			rlog.Warn(rmngCtx).Msgf("skipping non-string event entry: %v", raw)
			continue
		}
		if handled && (eventName == "addChild" || eventName == "removeChild") {
			rlog.Trace(rmngCtx).Str("event", eventName).
				Msg("multiple bridge events in one payload; bridgeAck only carries one — ignoring this one")
			continue
		}
		switch eventName {
		case "addChild":
			handleAddChild(rmngCtx, event, response)
			handled = true
		case "removeChild":
			handleRemoveChild(rmngCtx, event, response)
			handled = true
		default:
			rlog.Debug(rmngCtx).Msgf("event %q not handled by bridge Lambda", eventName)
		}
	}

	if err := response.Send(ctx); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("failed to publish bridgeAck")
		return err
	}
	return nil
}

func bridgeAckSuccess(response *node.DataToDevice, requestID string, extra map[string]interface{}) {
	ack := map[string]interface{}{
		"request_id": requestID,
		"status":     "success",
	}
	for k, v := range extra {
		ack[k] = v
	}
	response.Event = append(response.Event, "bridgeAck")
	response.Data["bridgeAck"] = ack
}

func bridgeAckError(response *node.DataToDevice, requestID, errCode string) {
	ack := map[string]interface{}{
		"request_id": requestID,
		"status":     "error",
		"error":      errCode,
	}
	response.Event = append(response.Event, "bridgeAck")
	response.Data["bridgeAck"] = ack
}

func handleAddChild(rmngCtx *rmngctx.RmngContext, event node.PublishInputEvent, response *node.DataToDevice) {
	parent := event.ThingName

	payload, ok := event.Data["addChild"].(map[string]interface{})
	if !ok {
		bridgeAckError(response, "", "invalid_payload")
		return
	}
	requestID, _ := payload["request_id"].(string)
	childSuffix, _ := payload["child_suffix"].(string)
	childLocalID, _ := payload["child_local_id"].(string)

	if !validSuffixRe.MatchString(childSuffix) {
		rlog.Warn(rmngCtx).Str("suffix", childSuffix).Msg("addChild rejected: invalid child_suffix")
		bridgeAckError(response, requestID, "invalid_suffix")
		return
	}
	if childLocalID == "" {
		bridgeAckError(response, requestID, "missing_child_local_id")
		return
	}

	childNodeID := parent + "--" + childSuffix
	bcDB := bridge_children_db.NewBridgeChildrenDB(rmngCtx)

	// One Query serves both the idempotency check (spec §5.7: same
	// child_local_id ⇒ return the existing child) and the
	// suffix-collision check (§4.2 step 3: same suffix, different
	// local_id ⇒ reject).
	siblings, err := bcDB.GetChildrenByParent(parent)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("addChild: GetChildrenByParent failed")
		bridgeAckError(response, requestID, "internal_error")
		return
	}
	for _, sibling := range siblings {
		if sibling.ChildLocalID == childLocalID {
			bridgeAckSuccess(response, requestID, map[string]interface{}{
				"child_node_id": sibling.ChildNodeID,
				"idempotent":    true,
			})
			return
		}
		if sibling.ChildNodeID == childNodeID {
			bridgeAckError(response, requestID, "child_suffix_in_use")
			return
		}
	}

	// Bridge must already be associated to a group for the
	// ADD NODE TO GROUP FLOW to have a target. Spec §4.2 step 5.
	// GetMyGroup also grants the minimum group perms on the result.
	bridgeGroup, err := bcDB.GetMyGroup()
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("addChild: GetMyGroup failed")
		bridgeAckError(response, requestID, "internal_error")
		return
	}
	if bridgeGroup.Group == "" {
		bridgeAckError(response, requestID, "bridge_not_associated")
		return
	}

	// Create the child Thing (no cert principal). Attributes per spec §3.2.
	attrs := map[string]string{
		"parent_node_id": parent,
		"child_local_id": childLocalID,
	}
	if err := iotutil.CreateThingWithAttributes(rmngCtx.Context, childNodeID, attrs); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("addChild: CreateThingWithAttributes failed")
		bridgeAckError(response, requestID, "thing_create_failed")
		return
	}

	// Persist the bridge_children row.
	if err := bcDB.AddChild(parent, childNodeID, childLocalID); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("addChild: bridge_children insert failed; rolling back Thing")
		_ = iotutil.DeleteThing(rmngCtx.Context, childNodeID)
		bridgeAckError(response, requestID, "db_write_failed")
		return
	}

	// GetMyGroup (above) and AddChild (just now) granted
	// the in-memory authority that ShadowNodeAddToGroupAuthorized requires.
	if _, err := node.ShadowNodeAddToGroupAuthorized(rmngCtx, childNodeID, bridgeGroup.Group, nil); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("addChild: ShadowNodeAddToGroupAuthorized failed; rolling back DDB + Thing")
		_ = bcDB.DeleteChild(parent, childNodeID)
		_ = iotutil.DeleteThing(rmngCtx.Context, childNodeID)
		bridgeAckError(response, requestID, "group_assoc_failed")
		return
	}

	rlog.Trace(rmngCtx).
		Str("parent", parent).
		Str("child", childNodeID).
		Str("group", bridgeGroup.Group).
		Msg("addChild succeeded")

	bridgeAckSuccess(response, requestID, map[string]interface{}{
		"child_node_id": childNodeID,
	})
}

func handleRemoveChild(rmngCtx *rmngctx.RmngContext, event node.PublishInputEvent, response *node.DataToDevice) {
	parent := event.ThingName

	payload, ok := event.Data["removeChild"].(map[string]interface{})
	if !ok {
		bridgeAckError(response, "", "invalid_payload")
		return
	}
	requestID, _ := payload["request_id"].(string)
	childNodeID, _ := payload["child_node_id"].(string)

	// Defence-in-depth: ensure the child belongs to the requesting
	// bridge. The IoT rule's clientid pinning already gives us parent
	// identity; this prefix check makes a payload-forgery attempt by a
	// bridge against another bridge's child impossible too.
	expectedPrefix := parent + "--"
	if !strings.HasPrefix(childNodeID, expectedPrefix) || childNodeID == expectedPrefix {
		bridgeAckError(response, requestID, "child_not_owned")
		return
	}

	bcDB := bridge_children_db.NewBridgeChildrenDB(rmngCtx)

	// Confirm the row exists; absent means the removal is a no-op
	// success (spec §4.3 step 2). GetChild is a single GetItem on the
	// composite key — O(1) regardless of the bridge's child count.
	existing, err := bcDB.GetChild(parent, childNodeID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("removeChild: GetChild failed")
		bridgeAckError(response, requestID, "internal_error")
		return
	}
	if existing == nil {
		bridgeAckSuccess(response, requestID, map[string]interface{}{
			"child_node_id": childNodeID,
			"idempotent":    true,
		})
		return
	}

	// Fetch the bridge's own group and grant the minimum group perms needed
	// for the removal flow. GetMyGroup also grants GroupGet +
	// GroupListSubEntities + GroupEditNodes on the result.
	bridgeGroup, err := bcDB.GetMyGroup()
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("removeChild: GetMyGroup failed")
		bridgeAckError(response, requestID, "internal_error")
		return
	}
	if bridgeGroup.Group != "" {
		if err := node.ShadowNodeRemoveFromGroupAuthorized(rmngCtx, childNodeID, bridgeGroup.Group); err != nil {
			rlog.Error(rmngCtx).Err(err).Msg("removeChild: ShadowNodeRemoveFromGroupAuthorized failed")
			bridgeAckError(response, requestID, "group_disassoc_failed")
			return
		}
	}

	// Bridge-specific teardown on top of the standard group-removal
	// flow above: drop the IoT Thing and the bridge_children row.
	// (node_data_reset for the child was already queued inside
	// ShadowNodeRemoveFromGroupAuthorized above.)
	if err := iotutil.DeleteThing(rmngCtx.Context, childNodeID); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("removeChild: DeleteThing failed")
		bridgeAckError(response, requestID, "teardown_failed")
		return
	}
	if err := bcDB.DeleteChild(parent, childNodeID); err != nil {
		rlog.Error(rmngCtx).Err(err).Msg("removeChild: DeleteChild failed")
		bridgeAckError(response, requestID, "teardown_failed")
		return
	}

	rlog.Trace(rmngCtx).
		Str("parent", parent).
		Str("child", childNodeID).
		Msg("removeChild succeeded")

	bridgeAckSuccess(response, requestID, map[string]interface{}{
		"child_node_id": childNodeID,
	})
}
