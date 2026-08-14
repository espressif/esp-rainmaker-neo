// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// nodeDetailsEvents are the events answered from the node_details row. A nil row with no error
// legitimately means "this node has no config yet", and version 0 is the right answer. A nil row
// because the READ FAILED means we do not know, and these events must not be answered: the
// version doubles as the device's staleness marker, so replying "version 0, empty" makes a device
// holding real schedules and triggers discard them.
var nodeDetailsEvents = map[string]bool{
	"getAlexaEn":        true,
	"getGVAEn":          true,
	"getSchedVer":       true,
	"getSchedDetails":   true,
	"getTriggerVer":     true,
	"getTriggerDetails": true,
}

// handlePublishInputEvent processes a single publish input event from a device
// This is the core business logic shared between direct invocation and SQS modes
func handlePublishInputEvent(ctx context.Context, event node.PublishInputEvent) error {
	n := node.NewNode(event.ThingName)
	rmngCtx := rmngctx.NewRmngContextWithNode(ctx, n, event.ThingName)
	responseData := node.NewDataToDevice(n)

	// Extract events from input data
	events, ok := event.Data["event"].([]interface{})
	if !ok {
		return fmt.Errorf("'event' field is missing or invalid in the input")
	}

	// Check if any events need node details
	needsNodeDetails := false
	for _, eventInterface := range events {
		eventStr, ok := eventInterface.(string)
		if !ok {
			continue
		}
		if nodeDetailsEvents[eventStr] {
			needsNodeDetails = true
			break
		}
	}

	// Read node details once if needed
	var nodeDetails *node_details_db.NodeDetails
	var nodeDetailsErr error
	if needsNodeDetails {
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		nodeDetails, nodeDetailsErr = nodeDetailsDB.GetNodeDetails(event.ThingName)
	}

	// Process each event
	for _, eventInterface := range events {
		eventStr, ok := eventInterface.(string)
		if !ok {
			rlog.Error(rmngCtx).Err(fmt.Errorf("skipping non-string event: %v", eventInterface)).Send()
			continue
		}

		// Add debug logging
		rlog.Debug(rmngCtx).Msgf("Processing event: %s", eventStr)

		// Leave a details-dependent event UNANSWERED when the read failed. The device re-asks on
		// its next batch, whereas a "version 0, empty" reply would make it drop real state.
		if nodeDetailsErr != nil && nodeDetailsEvents[eventStr] {
			rlog.Warn(rmngCtx).Err(nodeDetailsErr).Msgf("Not answering %s: node details unreadable, replying would look like an empty config", eventStr)
			continue
		}

		switch eventStr {
		case "getGroupInfo":
			err := n.HandleGetGroupInfo(ctx, responseData)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to get group info")
				continue
			}
		case "hello":
			helloData, ok := event.Data["hello"].(map[string]interface{})
			if !ok {
				rlog.Error(rmngCtx).Msg("'hello' field is missing or invalid in the input")
				continue
			}
			responseData.Event = append(responseData.Event, "hello")
			responseData.Data["hello"] = helloData
		case "setNodeConfig":
			configData, ok := event.Data["setNodeConfig"].(map[string]interface{})
			if !ok {
				rlog.Error(rmngCtx).Err(fmt.Errorf("'setNodeConfig' field is missing or invalid in the input")).Send()
			} else {
				err := n.HandleSetNodeConfig(ctx, configData, responseData)
				if err != nil {
					rlog.Error(rmngCtx).Err(err).Send()
				}
			}
		case "getAlexaEn":
			err := node.HandleGetAlexaEnWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getAlexaEn")
			}
		case "getGVAEn":
			err := node.HandleGetGVAEnWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getGVAEn")
			}
		case "getSchedVer":
			err := node.HandleGetSchedVerWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getSchedVer")
			}
		case "getSchedDetails":
			err := node.HandleGetSchedDetailsWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getSchedDetails")
			}
		case "getTriggerDetails":
			err := node.HandleGetTriggerDetailsWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getTriggerDetails")
			}
		case "getTimeSync":
			node.AppendGetTimeSync(responseData)
		case "getServerConfig":
			err := node.HandleGetServerConfig(ctx, n, responseData)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getServerConfig")
				continue
			}
		case "getTriggerVer":
			err := node.HandleGetTriggerVerWithNodeDetails(ctx, n, responseData, nodeDetails)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msg("Failed to handle getTriggerVer")
			}
		default:
			// Defensive catch for unknown event names on
			// rainmaker/nodes/+/to_cloud — most likely a firmware typo
			// or an event not yet implemented in this Lambda.
			rlog.Warn(rmngCtx).Msgf("Event %q has no handler in this Lambda", eventStr)
		}
	}

	// Answer whatever did not depend on node details before surfacing the failure, so a device
	// asking for group info or a time sync in the same batch is not held hostage by it.
	if err := responseData.Send(ctx); err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
	}

	// Propagate the read failure so the SQS path retries the message and the direct path's rule
	// error action sees it. Returning nil here is what made the outage invisible.
	if nodeDetailsErr != nil {
		return rmerror.NewRMError(nodeDetailsErr, fmt.Sprintf("could not read node details for %s; details-dependent events left unanswered", event.ThingName))
	}
	return nil
}

// handleHello is a helper function to handle hello events
func handleHello(data map[string]interface{}, responseData *node.DataToDevice) error {
	id, ok := data["id"]
	if !ok {
		return rmerror.NewRMError(fmt.Errorf("id is missing in hello data"), "")
	}
	response := map[string]interface{}{
		"id": id,
	}

	responseData.Event = append(responseData.Event, "hello")
	responseData.Data["hello"] = response
	return nil
}
