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

	if len(event.NodeIDs) == 0 || event.OldGroupID == "" {
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

	// Delete node services (triggers, schedules, timeseries) and automations for each node in parallel.
	_, _, err := parallel.ProcessParallel(rmngCtx, event.NodeIDs, func(nodeID string) error {
		for name, svc := range service.Registry().GetAllNodeServices() {
			if name == "config" {
				continue
			}
			if err := svc.Delete(rmngCtx, nodeID); err != nil {
				rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Str("service", name).Msg("failed to delete node service data")
			}
		}

		// Clean up automations referencing this node (single-node removal)
		if !event.GroupDelete {
			if err := automationSvc.DeleteNodeFromAutomations(rmngCtx, event.OldGroupID, nodeID); err != nil {
				rlog.Error(rmngCtx).Err(err).Str("nodeID", nodeID).Str("oldGroupID", event.OldGroupID).Msg("failed to clean up automations")
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Group deletion — wipe all automations for the group in one shot (outside the per-node loop)
	if event.GroupDelete {
		if err := automationSvc.Delete(rmngCtx, event.OldGroupID); err != nil {
			return rmerror.NewRMError(err, "failed to delete all automations for group")
		}
	}

	rlog.Info(rmngCtx).Strs("nodeIDs", event.NodeIDs).Msg("node data reset completed")
	return nil
}
