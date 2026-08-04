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
		if eventStr == "getAlexaEn" || eventStr == "getGVAEn" || eventStr == "getSchedVer" || eventStr == "getSchedDetails" || eventStr == "getTriggerDetails" || eventStr == "getTriggerVer" {
			needsNodeDetails = true
			break
		}
	}

	// Read node details once if needed
	var nodeDetails *node_details_db.NodeDetails
	if needsNodeDetails {
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngCtx)
		var err error
		nodeDetails, err = nodeDetailsDB.GetNodeDetails(event.ThingName)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msg("Failed to get node details")
		}
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

	err := responseData.Send(ctx)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
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
