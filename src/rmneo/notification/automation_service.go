// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/automation"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// AutomationService implements the NotificationService interface for automation notifications
type AutomationService struct {
	conditionEvaluator automation.ConditionEvaluator
	actionExecutor     automation.ActionExecutor
}

// NewAutomationService creates a new AutomationService
func NewAutomationService() *AutomationService {
	return &AutomationService{
		conditionEvaluator: automation.NewConditionEvaluator(),
		actionExecutor:     automation.NewActionExecutor(),
	}
}

// GetName returns the name of the notification service
func (s *AutomationService) GetName() string {
	return "automation"
}

// GetType returns the type of the notification service
func (s *AutomationService) GetType() NotificationServiceType {
	return NotificationServiceTypeGeneric
}

// TriggerInfo represents parsed trigger information from trigger ID
type TriggerInfo struct {
	NodeID       string
	AutomationID string
	TriggerIndex string
}

// AutomationState represents the state structure of an automation
type AutomationState struct {
	TriggerValues map[string]bool `json:"trigger_values"`
	Conditions    interface{}     `json:"conditions,omitempty"`
}

// AutomationPayload represents the payload structure of an automation
type AutomationPayload struct {
	Conditions interface{} `json:"conditions,omitempty"`
	Actions    interface{} `json:"actions,omitempty"`
}

// parseTriggerID parses a trigger ID in format "nodeID~automationID~triggerIndex"
func (s *AutomationService) parseTriggerID(triggerID string) (*TriggerInfo, error) {
	parts := strings.Split(triggerID, "~")
	if len(parts) != 3 {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("invalid trigger ID format: %s (expected nodeID~automationID~triggerIndex)", triggerID))
	}

	nodeID := parts[0]
	automationID := parts[1]
	triggerIndex := parts[2]

	if nodeID == "" || automationID == "" || triggerIndex == "" {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("invalid trigger ID format: %s (empty component)", triggerID))
	}

	return &TriggerInfo{
		NodeID:       nodeID,
		AutomationID: automationID,
		TriggerIndex: triggerIndex,
	}, nil
}

// updateAutomationState updates the state of an automation with new trigger values
func (s *AutomationService) updateAutomationState(rmngCtx *rmngctx.RmngContext, groupID, automationID string, triggerUpdates map[string]bool) error {
	automationDB := automation_db.NewAutomationDB(rmngCtx)

	// Get the current automation
	automation, err := automationDB.GetAutomation(groupID, automationID)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to get automation %s in group %s", automationID, groupID))
	}

	// Parse the current state using the utility function
	var currentState AutomationState
	if automation.State != "" {
		var stateData interface{}
		if err := json.Unmarshal([]byte(automation.State), &stateData); err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to parse automation state for %s, will recreate", automationID)
		} else {
			if err := utils.ConvertAnyToAny(stateData, &currentState); err != nil {
				rlog.Error(rmngCtx).Err(err).Msgf("Failed to convert automation state for %s, will recreate", automationID)
			}
		}
	}

	// Initialize trigger values if nil
	if currentState.TriggerValues == nil {
		currentState.TriggerValues = make(map[string]bool)
	}

	// Update trigger values with new values
	for triggerID, value := range triggerUpdates {
		currentState.TriggerValues[triggerID] = value
		rlog.Info(rmngCtx).Msgf("Updated trigger %s to %v for automation %s", triggerID, value, automationID)
	}

	// Preserve conditions from the automation payload if not already present
	if currentState.Conditions == nil {
		var payloadData AutomationPayload
		if err := utils.ConvertAnyToAny(automation.Payload, &payloadData); err == nil {
			if payloadData.Conditions != nil {
				currentState.Conditions = payloadData.Conditions
			}
		}
	}

	// Marshal the updated state back to JSON
	updatedStateBytes, err := json.Marshal(currentState)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal updated automation state")
	}

	// Update the automation state in the database
	err = automationDB.UpdateAutomationState(groupID, automationID, string(updatedStateBytes))
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to update automation %s state in database", automationID))
	}

	rlog.Info(rmngCtx).Msgf("Successfully updated automation %s state with trigger values: %v", automationID, triggerUpdates)

	// After updating state, evaluate conditions and execute actions if conditions are met
	err = s.evaluateAndExecuteActions(rmngCtx, groupID, automationID, automation, &currentState)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msgf("Failed to evaluate conditions and execute actions for automation %s", automationID)
		// Don't return error as the trigger update was successful
	}

	return nil
}

// evaluateAndExecuteActions evaluates automation conditions and executes actions if conditions are satisfied
func (s *AutomationService) evaluateAndExecuteActions(rmngCtx *rmngctx.RmngContext, groupID, automationID string, automation *automation_db.AutomationItem, state *AutomationState) error {
	if automation_db.AutomationStatusFromItem(*automation) == automation_db.AutomationStatusDisabled {
		rlog.Debug(rmngCtx).Msgf("Automation %s is disabled, skipping action execution", automationID)
		return nil
	}

	// Check if conditions exist
	if state.Conditions == nil {
		rlog.Debug(rmngCtx).Msgf("No conditions found for automation %s, skipping evaluation", automationID)
		return nil
	}

	conditions, ok := state.Conditions.(map[string]interface{})
	if !ok {
		rlog.Error(rmngCtx).Msgf("Invalid conditions format for automation %s", automationID)
		return rmerror.NewRMError(nil, fmt.Sprintf("invalid conditions format for automation %s", automationID))
	}

	// Check if trigger values exist
	if state.TriggerValues == nil {
		rlog.Debug(rmngCtx).Msgf("No trigger values found for automation %s, skipping evaluation", automationID)
		return nil
	}

	rlog.Info(rmngCtx).Msgf("Evaluating conditions for automation %s with trigger values: %v", automationID, state.TriggerValues)

	// Evaluate conditions
	conditionsMet, err := s.conditionEvaluator.EvaluateConditions(conditions, state.TriggerValues)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to evaluate conditions for automation %s", automationID))
	}

	rlog.Info(rmngCtx).Msgf("Conditions evaluation result for automation %s: %v", automationID, conditionsMet)

	if !conditionsMet {
		rlog.Debug(rmngCtx).Msgf("Conditions not met for automation %s, skipping action execution", automationID)
		return nil
	}

	// Conditions are met, extract and execute actions from payload
	var payloadData AutomationPayload
	if err := utils.ConvertAnyToAny(automation.Payload, &payloadData); err != nil {
		rlog.Error(rmngCtx).Err(err).Msgf("Invalid payload format for automation %s", automationID)
		return rmerror.NewRMError(err, fmt.Sprintf("invalid payload format for automation %s", automationID))
	}

	if payloadData.Actions == nil {
		rlog.Debug(rmngCtx).Msgf("No actions found for automation %s, nothing to execute", automationID)
		return nil
	}

	rlog.Info(rmngCtx).Msgf("Conditions met for automation %s, executing actions", automationID)

	// Execute actions
	err = s.actionExecutor.ExecuteActions(rmngCtx, groupID, automationID, payloadData.Actions)
	if err != nil {
		return rmerror.NewRMError(err, fmt.Sprintf("failed to execute actions for automation %s", automationID))
	}

	rlog.Info(rmngCtx).Msgf("Successfully executed actions for automation %s", automationID)
	return nil
}

// Send sends an automation notification
func (s *AutomationService) Send(notification interface{}) error {
	notificationData, ok := notification.(*Notification)
	if !ok {
		rlog.Error(context.TODO()).Msg("Failed to cast notification to Notification")
		return nil
	}

	// Create system context for database operations (needed for all logging below)
	s_actor := utils.NewSystemActor()
	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), s_actor)

	// Automation service only handles direct notifications
	if notificationData.NotificationType != NotificationTypeDirect {
		rlog.Debug(rmngCtx).Msgf("Automation service ignoring notification type: %s", notificationData.NotificationType)
		return nil
	}

	if notificationData.DirectNotificationData == nil {
		rlog.Error(rmngCtx).Msg("Direct notification data is nil")
		return nil
	}

	nodeID := notificationData.DirectNotificationData.NodeID
	notifyData := notificationData.DirectNotificationData.NotifyData
	topicName := notificationData.TopicName

	// Log notification details
	rlog.Info(rmngCtx).Msgf("Processing automation notification for node: %s, topic: %s", nodeID, topicName)

	// Check if this is an automation trigger notification
	automationData, ok := notifyData["automation"].(map[string]interface{})
	if !ok {
		rlog.Debug(rmngCtx).Msg("No automation data found in notification")
		return nil
	}

	// Look for trigger data
	triggerData, ok := automationData["trigger"]
	if !ok {
		rlog.Debug(rmngCtx).Msg("No trigger data found in automation notification")
		return nil
	}

	// Parse trigger array
	triggers, ok := triggerData.([]interface{})
	if !ok {
		rlog.Error(rmngCtx).Msg("Trigger data is not an array")
		return nil
	}

	// Get group information from the notification (validation already done in main handler)
	groupID, _, _, err := notificationData.GetGroupInfo()
	if err != nil {
		return err
	}

	// Process each trigger
	automationUpdates := make(map[string]map[string]bool) // automationID -> triggerID -> value

	for _, trigger := range triggers {
		triggerMap, ok := trigger.(map[string]interface{})
		if !ok {
			rlog.Error(rmngCtx).Msg("Invalid trigger format in notification")
			continue
		}

		triggerID, ok := triggerMap["id"].(string)
		if !ok {
			rlog.Error(rmngCtx).Msg("Missing trigger ID in notification")
			continue
		}

		triggerValue, ok := triggerMap["value"].(bool)
		if !ok {
			rlog.Error(rmngCtx).Msg("Missing or invalid trigger value in notification")
			continue
		}

		// Parse trigger ID
		triggerInfo, err := s.parseTriggerID(triggerID)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to parse trigger ID: %s", triggerID)
			continue
		}

		// Additional validation: ensure the node in trigger ID matches the notification sender
		if triggerInfo.NodeID != nodeID {
			rlog.Error(rmngCtx).Msgf("Node mismatch: trigger ID indicates node %s but notification from node %s", triggerInfo.NodeID, nodeID)
			continue
		}

		// Group updates by automation ID
		if automationUpdates[triggerInfo.AutomationID] == nil {
			automationUpdates[triggerInfo.AutomationID] = make(map[string]bool)
		}
		automationUpdates[triggerInfo.AutomationID][triggerID] = triggerValue

		rlog.Info(rmngCtx).Msgf("Processed trigger %s: %v for automation %s in group %s", triggerID, triggerValue, triggerInfo.AutomationID, groupID)
	}

	// Update automation states
	for automationID, triggerUpdates := range automationUpdates {
		err := s.updateAutomationState(rmngCtx, groupID, automationID, triggerUpdates)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to update automation %s state", automationID)
			continue
		}
	}

	return nil
}

// SendTo sends an automation notification to specific users
func (s *AutomationService) SendTo(notificationData interface{}, userIDs []string) error {
	// Automation notifications are not user-specific, so we just call Send
	return s.Send(notificationData)
}

// Marshal marshals the notification
func (s *AutomationService) Marshal(notification *Notification) (interface{}, error) {
	return notification, nil
}
