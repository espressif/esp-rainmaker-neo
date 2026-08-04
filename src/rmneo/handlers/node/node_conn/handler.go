// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/nodes_online_db"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func handlePresenceEvent(ctx context.Context, event node.PresenceEvent) error {
	if event.EventType == "disconnected" {
		s := utils.NewSystemActor()
		rmngContext := rmngctx.NewRmngContextWithNode(ctx, s, event.ClientID)

		nodesOnlineDB := nodes_online_db.NewNodesOnlineDB(rmngContext)
		currentInfo, err := nodesOnlineDB.GetNodeSessionInfo(event.ClientID)
		if errors.Is(err, nodes_online_db.ErrNodeNotFound) {
			rlog.Debug(rmngContext).Msgf("no nodes_online entry for '%s'; dropping disconnect", event.ClientID)
			return nil
		}
		if err != nil {
			rlog.Error(rmngContext).Err(fmt.Errorf("error getting current session for node: %v", err)).Send()
			return nil
		}

		if !node.SessionMatches(currentInfo, event) {
			rlog.Info(rmngContext).Msgf("session mismatch for node '%s'; dropping stale disconnect. Current: (%s, v%d), Event: (%s, v%d)",
				event.ClientID, currentInfo.SessionID, currentInfo.VersionNumber, event.SessionID, event.VersionNumber)
			return nil
		}

		shadowData := node.BuildDisconnectShadow(event)
		n := node.NewNode(event.ClientID)
		if err := n.WriteToReportedShadow(rmngContext, shadowData); err != nil {
			rlog.Error(rmngContext).Err(err).Send()
		}
		if err := n.WriteToIndexedReportedShadow(rmngContext, shadowData); err != nil {
			rlog.Error(rmngContext).Err(err).Send()
		}
	} else if event.EventType == "connected" {
		// This event will never come from the aws topic: $aws/events/presence/connected/+
		// Just triggered if the lamdba is invoked directly

		s := utils.NewSystemActor()
		rmngContext := rmngctx.NewRmngContextWithNode(ctx, s, event.ClientID)

		nodesOnlineDB := nodes_online_db.NewNodesOnlineDB(rmngContext)
		err := nodesOnlineDB.AddNodeSession(nodes_online_db.NodesOnlineEntry{
			ClientID:      event.ClientID,
			SessionID:     event.SessionID,
			IPAddress:     event.IPAddress,
			PrincipalID:   event.PrincipalID,
			Timestamp:     event.Timestamp,
			EventType:     event.EventType,
			VersionNumber: event.VersionNumber,
		})
		if err != nil {
			rlog.Error(rmngContext).Err(err).Send()
		}
	}
	return nil
}
