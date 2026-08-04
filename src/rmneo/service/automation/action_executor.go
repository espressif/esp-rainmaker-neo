// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jsonutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// ActionExecutor defines the interface for executing automation actions
type ActionExecutor interface {
	// ExecuteActions executes the actions defined in the automation
	ExecuteActions(ctx *rmngctx.RmngContext, groupID, automationID string, actions interface{}) error
}

// DefaultActionExecutor is the default implementation of ActionExecutor
type DefaultActionExecutor struct{}

// NewActionExecutor creates a new instance of DefaultActionExecutor
func NewActionExecutor() ActionExecutor {
	return &DefaultActionExecutor{}
}

// ActionTarget represents a single action target.
// Path is the within-node data point path:
//   - Default data model: "<deviceId>.<paramId>", e.g. "Light.Power"
//   - Matter data model:  the full dotted key chain into the desired shadow, e.g.
//     "0x1.c.s.0x6.a.0x0" (attribute write) or "0x1.c.s.0x6.c.0x1" (command invoke)
type ActionTarget struct {
	Node  string      `json:"node"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

// ActionsFormat represents the complete actions structure
type ActionsFormat struct {
	Targets []ActionTarget `json:"targets"`
}

// ExecuteActions executes automation actions
// Expected actions structure: {"targets": [{"node": "nodeId", "path": "Light.Power", "value": true}]}
func (e *DefaultActionExecutor) ExecuteActions(ctx *rmngctx.RmngContext, groupID, automationID string, actions interface{}) error {
	if actions == nil {
		rlog.Debug(ctx).Msgf("No actions to execute for automation %s", automationID)
		return nil
	}

	// Convert actions to structured format using strict conversion
	var actionsFormat ActionsFormat
	err := utils.ConvertAnyToAny(actions, &actionsFormat)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("invalid actions format for automation %s", automationID))
	}

	if len(actionsFormat.Targets) == 0 {
		rlog.Debug(ctx).Msgf("No targets found in actions for automation %s", automationID)
		return nil
	}

	rlog.Info(ctx).Msgf("Executing %d actions for automation %s", len(actionsFormat.Targets), automationID)

	// Execute each action target
	for i, target := range actionsFormat.Targets {
		err := e.validateActionTarget(&target)
		if err != nil {
			rlog.Error(ctx).Err(err).Msgf("Invalid action target %d for automation %s", i, automationID)
			continue
		}

		err = e.executeActionTarget(ctx, groupID, automationID, &target)
		if err != nil {
			rlog.Error(ctx).Err(err).Msgf("Failed to execute action target %d for automation %s", i, automationID)
			continue
		}

		rlog.Info(ctx).Msgf("Successfully executed action: node=%s, path=%s, value=%v",
			target.Node, target.Path, target.Value)
	}

	return nil
}

// validateActionTarget validates that an action target has all required fields
func (e *DefaultActionExecutor) validateActionTarget(target *ActionTarget) error {
	if target.Node == "" {
		return rmerror.NewRMError(nil, "missing 'node' field in action target")
	}
	if target.Path == "" {
		return rmerror.NewRMError(nil, "missing 'path' field in action target")
	}
	// Note: Value can be any type including nil, so we don't need to validate its existence
	return nil
}

func buildDesiredPayload(target *ActionTarget) (map[string]interface{}, error) {
	return jsonutil.ToJson(target.Path, target.Value)
}

// executeActionTarget executes a single action target by updating the node's parameter
func (e *DefaultActionExecutor) executeActionTarget(ctx *rmngctx.RmngContext, groupID, automationID string, target *ActionTarget) error {
	// Security: actions run under a system actor, whose IsAuthorized passes for
	// any node (see node.PublishToDevice). The automation is scoped to groupID,
	// so a target node MUST be a member of that group. Without this check an
	// automation created under a group the caller owns can push arbitrary params
	// to a node in another tenant's group (cross-tenant device control).
	if _, err := group_node_db.NewGroupNodeDB(ctx).GetGroupNode(groupID, target.Node); err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("action target node %s is not a member of automation group %s", target.Node, groupID))
	}

	paramUpdate, err := buildDesiredPayload(target)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("invalid path %q on node %s", target.Path, target.Node))
	}

	nodeInstance := node.NewNode(target.Node)

	rlog.Info(ctx).Msgf("Updating node %s with parameter: %s = %v (automation %s)",
		target.Node, target.Path, target.Value, automationID)

	err = nodeInstance.PublishToDeviceDesired(ctx, paramUpdate)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to update path %s on node %s",
			target.Path, target.Node))
	}

	return nil
}
