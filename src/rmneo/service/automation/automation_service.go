// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"math/rand"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// ErrActionTargetNotInGroup is returned when an automation's action target
// references a node that is not a member of the automation's group. The API
// handler maps it to HTTP 400 (client error), since it is a malformed request
// rather than a server fault.
var ErrActionTargetNotInGroup = errors.New("action target node is not a member of the automation group")

// ErrConditionNodeNotInGroup is the condition-trigger counterpart of ErrActionTargetNotInGroup.
var ErrConditionNodeNotInGroup = errors.New("condition trigger node is not a member of the automation group")

// AutomationService handles automation configurations for groups.
type AutomationService struct {
	service.BaseService
}

const (
	automationIDCharset         = "abcdefghijklmnopqrstuvwxyz0123456789"
	automationIDAlphabetCharset = "abcdefghijklmnopqrstuvwxyz"
	automationIDLength          = 3
)

func generateAutomationID() string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, automationIDLength)

	// Ensure the first character is an alphabet
	b[0] = automationIDAlphabetCharset[seededRand.Intn(len(automationIDAlphabetCharset))]

	// Generate the rest of the characters
	for i := 1; i < automationIDLength; i++ {
		b[i] = automationIDCharset[seededRand.Intn(len(automationIDCharset))]
	}

	return string(b)
}

// NewAutomationService creates a new instance of AutomationService.
func NewAutomationService() *AutomationService {
	return &AutomationService{
		BaseService: service.BaseService{
			Name:      "automations",
			Versioned: false,
		},
	}
}

// SupportsResourceID indicates that the automation service supports resource IDs
func (s *AutomationService) SupportsResourceID() bool {
	return true
}

// flattenAutomation builds the flat API representation of a stored automation:
// {id, name, description, conditions, actions, status}. The stored payload holds
// the user-supplied definition and is spread alongside the automation ID. The
// "status" field is derived from the item when the payload does not already
// carry it, so callers always see the automation's current state.
func flattenAutomation(item automation_db.AutomationItem) map[string]interface{} {
	flat := map[string]interface{}{
		"id": item.AutomationID,
	}
	if payload, ok := item.Payload.(map[string]interface{}); ok {
		for key, value := range payload {
			flat[key] = value
		}
	}
	if _, exists := flat["status"]; !exists {
		flat["status"] = automation_db.AutomationStatusFromItem(item)
	}
	return flat
}

// Get retrieves automation data for a group.
func (s *AutomationService) Get(rmngCtx *rmngctx.RmngContext, groupID string) (interface{}, error) {
	rlog.Info(rmngCtx).Msgf("AutomationService Get called for group %s", groupID)

	// Initialize the automation DB
	automationDB := automation_db.NewAutomationDB(rmngCtx)

	// Get all automations for the group
	automations, err := automationDB.ListGroupAutomations(groupID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list automations")
	}

	// Initialize an empty array for the response
	filteredAutomations := make([]map[string]interface{}, 0)

	// Flatten each automation into {id, ...payload, status} for the response.
	for _, automation := range automations {
		filteredAutomations = append(filteredAutomations, flattenAutomation(automation))
	}

	// Wrap in {"automations": [...]} for the API response. The array is
	// always present (empty when no automations are configured).
	return map[string]interface{}{
		"automations": filteredAutomations,
	}, nil
}

// GetWithResourceID retrieves a specific automation by ID
func (s *AutomationService) GetWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, automationID string) (interface{}, error) {
	rlog.Info(rmngCtx).Msgf("AutomationService GetWithResourceID called for group %s, automation %s",
		groupID, automationID)

	// Initialize the automation DB
	automationDB := automation_db.NewAutomationDB(rmngCtx)

	// Get specific automation
	automation, err := automationDB.GetAutomation(groupID, automationID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get automation")
	}

	return flattenAutomation(*automation), nil
}

// Put creates a new automation for a group, generating its ID server-side.
func (s *AutomationService) Put(rmngCtx *rmngctx.RmngContext, groupID string, data interface{}) (interface{}, error) {
	rlog.Info(rmngCtx).Msgf("AutomationService Put called for group %s with data: %+v", groupID, data)

	// Generate a new automation ID for the new automation
	automationID := generateAutomationID()
	rlog.Info(rmngCtx).Msgf("Generated new automation ID: %s", automationID)

	// Persist through the shared write path. The create response echoes the
	// generated ID so the caller can address the automation afterwards.
	if _, err := s.PutWithResourceID(rmngCtx, groupID, automationID, data); err != nil {
		return nil, err
	}

	return map[string]string{
		"automation_id": automationID,
		"message":       "success",
	}, nil
}

// PutWithResourceID updates an automation with a specific ID
func (s *AutomationService) PutWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, automationID string, data interface{}) (interface{}, error) {
	// User-facing writes always validate action targets against the group.
	return s.putWithResourceID(rmngCtx, groupID, automationID, data, true)
}

// putWithResourceID is the shared write path. targetCheck gates the
// action-target group-membership validation: user-facing writes pass true so
// a foreign target is rejected up front, while internal cleanup
// (DeleteNodeFromAutomations) passes false because it only ever shrinks the
// target list and a sibling target may already have left the group. Stale
// targets that slip through are still blocked at execution time by
// executeActionTarget. RBAC (GroupEditAutomation) is enforced by the DB layer.
func (s *AutomationService) putWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, automationID string, data interface{}, targetCheck bool) (interface{}, error) {
	rlog.Info(rmngCtx).Msgf("AutomationService PutWithResourceID called for group %s, automation %s with data: %+v",
		groupID, automationID, data)

	payload, err := parseAutomationInput(data)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid automation request")
	}

	if targetCheck {
		if err := validateActionTargets(rmngCtx, groupID, payload); err != nil {
			return nil, err
		}
		if err := validateConditionNodes(rmngCtx, groupID, payload); err != nil {
			return nil, err
		}
	}

	automationDB := automation_db.NewAutomationDB(rmngCtx)
	if err := automationDB.CreateAutomation(groupID, automationID, payload); err != nil {
		return nil, rmerror.NewRMError(err, "failed to create/update automation")
	}

	rlog.Info(rmngCtx).Msgf("Successfully saved automation with ID %s for group %s", automationID, groupID)

	return map[string]string{
		"message": "success",
	}, nil
}

// validateActionTargets rejects automation payloads whose action targets
// reference nodes that are not members of the automation's group. Automations
// execute under a system actor whose authorization passes for any node, so an
// unvalidated foreign target becomes cross-tenant device control on trigger.
// executeActionTarget enforces the same rule at execution time as the
// backstop for nodes that leave the group after the automation is written;
// this gate surfaces the error to the caller at write time.
func validateActionTargets(rmngCtx *rmngctx.RmngContext, groupID string, payload interface{}) error {
	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return nil
	}
	rawActions, exists := payloadMap["actions"]
	if !exists || rawActions == nil {
		return nil
	}

	// Payloads that don't convert to the executable {"targets": [...]} shape
	// carry no resolvable node reference and are rejected by ExecuteActions at
	// trigger time, so they are not gated here.
	var actions ActionsFormat
	if err := utils.ConvertAnyToAny(rawActions, &actions); err != nil {
		return nil
	}

	groupNodeDB := group_node_db.NewGroupNodeDB(rmngCtx)
	checked := make(map[string]bool, len(actions.Targets))
	for _, target := range actions.Targets {
		if target.Node == "" || checked[target.Node] {
			continue
		}
		if _, err := groupNodeDB.GetGroupNode(groupID, target.Node); err != nil {
			// Wrap the sentinel (not the DB error) so the handler can map it to
			// HTTP 400 via errors.Is while the message keeps the node/group detail.
			return rmerror.NewRMError(ErrActionTargetNotInGroup, fmt.Sprintf("action target node %s is not a member of group %s", target.Node, groupID))
		}
		checked[target.Node] = true
	}
	return nil
}

// validateConditionNodes rejects automation payloads whose condition triggers
// reference nodes that are not members of the automation's group. Trigger IDs use
// the format "nodeID~automationID~triggerIndex"; only the node segment is checked,
// since the automation ID is server-assigned and callers send a placeholder.
// TODO: This code is quite similar to validateActionTargets but with some minor differences. Can we refactor this to avoid code duplication?
func validateConditionNodes(rmngCtx *rmngctx.RmngContext, groupID string, payload interface{}) error {
	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return nil
	}
	rawConditions, exists := payloadMap["conditions"]
	if !exists || rawConditions == nil {
		return nil
	}

	var conditions AutomationConditions
	if err := utils.ConvertAnyToAny(rawConditions, &conditions); err != nil {
		return nil
	}

	groupNodeDB := group_node_db.NewGroupNodeDB(rmngCtx)
	checked := make(map[string]bool, len(conditions.And)+len(conditions.Or))
	for _, triggerID := range append(append([]string{}, conditions.And...), conditions.Or...) {
		nodeID, _, found := strings.Cut(triggerID, "~")
		if !found || nodeID == "" || checked[nodeID] {
			continue
		}
		if _, err := groupNodeDB.GetGroupNode(groupID, nodeID); err != nil {
			return rmerror.NewRMError(ErrConditionNodeNotInGroup, fmt.Sprintf("condition trigger node %s is not a member of group %s", nodeID, groupID))
		}
		checked[nodeID] = true
	}
	return nil
}

// Delete removes automation data for a group.
func (s *AutomationService) Delete(rmngCtx *rmngctx.RmngContext, groupID string) error {
	rlog.Info(rmngCtx).Msgf("AutomationService Delete called for group %s", groupID)

	// Initialize the automation DB
	automationDB := automation_db.NewAutomationDB(rmngCtx)

	// Delete all automations for the group
	err := automationDB.DeleteAllGroupAutomations(groupID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete all automations for group")
	}

	return nil
}

// DeleteWithResourceID removes a specific automation by ID
func (s *AutomationService) DeleteWithResourceID(rmngCtx *rmngctx.RmngContext, groupID string, automationID string) error {
	rlog.Info(rmngCtx).Msgf("AutomationService DeleteWithResourceID called for group %s, automation %s",
		groupID, automationID)

	// Initialize the automation DB
	automationDB := automation_db.NewAutomationDB(rmngCtx)

	// Delete specific automation
	err := automationDB.DeleteAutomation(groupID, automationID)
	if err != nil {
		rlog.Error(rmngCtx).Err(err).Msgf("Failed to delete automation %s for group %s: %v",
			automationID, groupID, err)
		return rmerror.NewRMError(err, "failed to delete automation")
	}

	rlog.Info(rmngCtx).Msgf("Successfully deleted automation %s for group %s", automationID, groupID)
	return nil
}

// AutomationEntry is the fully typed representation of an automation in the
// flat API shape: the automation ID alongside the inlined payload fields.
type AutomationEntry struct {
	ID string `json:"id"`
	AutomationPayload
}

// AutomationPayload is the typed automation payload stored in DynamoDB.
type AutomationPayload struct {
	Status      string               `json:"status,omitempty"`
	Name        string               `json:"name,omitempty"`
	Description string               `json:"description,omitempty"`
	Conditions  AutomationConditions `json:"conditions,omitempty"`
	Actions     AutomationActions    `json:"actions,omitempty"`
}

func parseAutomationInput(data interface{}) (interface{}, error) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		dataMap = map[string]interface{}{}
		if err := utils.ConvertAnyToAny(data, &dataMap); err != nil {
			return data, nil
		}
	}
	if status, exists := dataMap["status"]; exists {
		statusStr, ok := status.(string)
		if !ok || (statusStr != "" && statusStr != automation_db.AutomationStatusEnabled && statusStr != automation_db.AutomationStatusDisabled) {
			return nil, rmerror.NewRMError(nil, "invalid automation status")
		}
	}
	return dataMap, nil
}

// AutomationConditions holds the trigger IDs.
// Trigger IDs use format "nodeID~automationID~triggerIndex".
type AutomationConditions struct {
	And []string `json:"and,omitempty"`
	Or  []string `json:"or,omitempty"`
}

// AutomationActions holds the action targets.
type AutomationActions struct {
	Targets []ActionTarget `json:"targets,omitempty"`
}

// ContainsNode checks whether any trigger ID starts with the given prefix (nodeID~).
func (c *AutomationConditions) ContainsNode(triggerPrefix string) bool {
	for _, id := range c.And {
		if strings.HasPrefix(id, triggerPrefix) {
			return true
		}
	}
	for _, id := range c.Or {
		if strings.HasPrefix(id, triggerPrefix) {
			return true
		}
	}
	return false
}

// ContainsNode checks whether any action target references the given nodeID.
func (a *AutomationActions) ContainsNode(nodeID string) bool {
	for _, t := range a.Targets {
		if t.Node == nodeID {
			return true
		}
	}
	return false
}

// RemoveNode returns a copy of actions with targets referencing nodeID removed.
func (a *AutomationActions) RemoveNode(nodeID string) AutomationActions {
	var remaining []ActionTarget
	for _, t := range a.Targets {
		if t.Node != nodeID {
			remaining = append(remaining, t)
		}
	}
	return AutomationActions{Targets: remaining}
}

// DeleteNodeFromAutomations cleans up automations in the group that reference the given nodeID.
// - If the node is in trigger conditions → delete the entire automation
// - If the node is only in action targets → remove those targets; if no targets remain, delete the automation
func (s *AutomationService) DeleteNodeFromAutomations(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) error {
	data, err := s.Get(rmngCtx, groupID)
	if err != nil {
		return err
	}
	// Get returns the API-wrapped shape `{"automations": [...]}`. Unwrap
	// before structural conversion.
	wrapper, ok := data.(map[string]interface{})
	if !ok {
		return rmerror.NewRMError(nil, "unexpected automations response shape")
	}
	var entries []AutomationEntry
	if err := utils.ConvertAnyToAny(wrapper["automations"], &entries); err != nil {
		return rmerror.NewRMError(err, "failed to convert automations to typed structs")
	}

	triggerPrefix := nodeID + "~"
	for _, entry := range entries {
		if entry.Conditions.ContainsNode(triggerPrefix) {
			// Node in triggers → delete entire automation
			if err := s.DeleteWithResourceID(rmngCtx, groupID, entry.ID); err != nil {
				rlog.Error(rmngCtx).Err(err).Str("automationID", entry.ID).Msg("failed to delete automation with node in triggers")
			}
			continue
		}

		if !entry.Actions.ContainsNode(nodeID) {
			continue
		}

		// Node only in actions → remove those targets
		cleaned := entry.AutomationPayload
		cleaned.Actions = cleaned.Actions.RemoveNode(nodeID)
		if len(cleaned.Actions.Targets) == 0 {
			// No actions left → delete the automation
			if err := s.DeleteWithResourceID(rmngCtx, groupID, entry.ID); err != nil {
				rlog.Error(rmngCtx).Err(err).Str("automationID", entry.ID).Msg("failed to delete automation with no remaining actions")
			}
		} else {
			// Update with cleaned payload, skipping the action-target
			// membership gate (targetCheck=false): cleanup only shrinks the
			// target list, and when several nodes leave the group at once a
			// sibling target may already be gone from the group, which would
			// wedge the cleanup. Stale targets are still blocked at execution
			// time.
			if _, err := s.putWithResourceID(rmngCtx, groupID, entry.ID, cleaned, false); err != nil {
				rlog.Error(rmngCtx).Err(err).Str("automationID", entry.ID).Msg("failed to update automation after removing node from actions")
			}
		}
	}
	return nil
}
