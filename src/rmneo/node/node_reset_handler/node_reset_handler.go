// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_reset_handler

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/automation"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/trigger"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// HandleNodeDataReset processes a node data reset event.
func HandleNodeDataReset(ctx context.Context, event node.NodeDataResetEvent) error {

	if event.OldGroupID == "" || (len(event.NodeIDs) == 0 && !event.GroupDelete) {
		return rmerror.NewRMError(nil, "node_ids is required") // Don't retry on bad input
	}

	rlog.Debug(ctx).Strs("nodeIDs", event.NodeIDs).Str("oldGroupID", event.OldGroupID).Bool("groupDelete", event.GroupDelete).Msg("starting node data reset")

	systemActor := utils.NewSystemActor()
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, systemActor)

	// Register services so the service registry is populated
	service.Initialize()
	trigger.Register()
	schedule.Register()
	timeseries.Register()

	automationSvc := automation.NewAutomationService()

	if event.GroupDelete {
		if err := automationSvc.Delete(rmngCtx, event.OldGroupID); err != nil {
			return rmerror.NewRMError(err, "failed to delete all automations for group")
		}
		rlog.Info(rmngCtx).Str("oldGroupID", event.OldGroupID).Msg("group automation wipe completed")
		return nil
	}

	// Delete node services (triggers, schedules, timeseries) for each node in parallel.
	_, _, err := parallel.ProcessParallel(rmngCtx, event.NodeIDs, func(nodeID string) error {
		for name, svc := range service.Registry().GetAllNodeServices() {
			if name == "config" {
				continue
			}
			if err := svc.Delete(rmngCtx, nodeID); err != nil {
				rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Str("service", name).Msg("failed to delete node service data")
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Automations are group-scoped, so the whole batch is cleaned in one pass rather than inside the fan-out above — concurrent per-node writes to one shared automation would overwrite each other's target removal. Group deletion is not handled here either: group.DeleteGroup wipes the group's automations synchronously, since a deletable group may have no node removals to ride along with.
	if err := automationSvc.DeleteNodesFromAutomations(rmngCtx, event.OldGroupID, event.NodeIDs); err != nil {
		rlog.Error(rmngCtx).Err(err).Strs("nodeIDs", event.NodeIDs).Str("oldGroupID", event.OldGroupID).Msg("failed to clean up automations")
	}

	rlog.Info(rmngCtx).Strs("nodeIDs", event.NodeIDs).Msg("node data reset completed")
	return nil
}
