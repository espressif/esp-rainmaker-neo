// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The automations table structure is designed to store automation configurations:

Table Name: automations
Primary Key: group_id (Partition Key), automation_id (Sort Key)

Schema:
- group_id (String): Partition key, identifies the group that owns the automation
- automation_id (String): Sort key, uniquely identifies an automation within a group
- payload (Map): Contains the actual automation configuration sent by the client
- state (String): Internal runtime state for trigger evaluation (not exposed via API)

Query Patterns:
1. Get all automations for a group:
   - Use KeyConditionExpression: group_id = :gid
   - Returns all automations for the group

2. Get specific automation:
   - Use both group_id and automation_id in the key
   - Exact match query

Access Control:
- Automation operations require appropriate permissions on the parent group
*/

package automation_db

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	AutomationsTable = "rmng-automations"

	AutomationStatusEnabled  = "enabled"
	AutomationStatusDisabled = "disabled"
)

func AutomationStatusFromItem(item AutomationItem) string {
	if payloadMap, ok := item.Payload.(map[string]interface{}); ok {
		if status, ok := payloadMap["status"].(string); ok && status != "" {
			return status
		}
	}
	return AutomationStatusEnabled
}

// AutomationDB provides database operations for automations
type AutomationDB struct {
	Ctx         *rmngctx.RmngContext
	automations *ddbu.Table[AutomationItem]
}

// AutomationItem represents an item in the automations table
type AutomationItem struct {
	GroupID      string      `dynamodbav:"group_id"`
	AutomationID string      `dynamodbav:"automation_id"`
	Payload      interface{} `dynamodbav:"payload"`
	State        string      `dynamodbav:"state"`
}

// NewAutomationDB creates a new instance of AutomationDB
func NewAutomationDB(ctx *rmngctx.RmngContext) *AutomationDB {
	return &AutomationDB{
		Ctx: ctx,
		automations: ddbu.NewTable[AutomationItem](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     AutomationsTable,
			HashKey:  "group_id",
			RangeKey: "automation_id",
		}),
	}
}

// CreateAutomation creates or replaces an automation in DynamoDB.
func (adb *AutomationDB) CreateAutomation(groupID, automationID string, payload interface{}) error {
	if err := adb.Ctx.IsAuthorized(utils.GroupEditAutomation, groupID); err != nil {
		return err
	}

	state := ""

	// Check if payload contains conditions and process them
	if payloadMap, ok := payload.(map[string]interface{}); ok {
		if conditions, exists := payloadMap["conditions"]; exists && conditions != nil {
			// Check if conditions is not empty
			if conditionsMap, isMap := conditions.(map[string]interface{}); isMap && len(conditionsMap) > 0 {
				// Extract trigger IDs from conditions
				triggerIDs := extractTriggerIDs(conditionsMap)

				// Create state object with conditions and trigger value mappings.
				stateData := map[string]interface{}{
					"conditions":     conditions,
					"trigger_values": createTriggerValueMap(triggerIDs),
				}

				// Marshal state to JSON string for storage
				if stateBytes, err := json.Marshal(stateData); err == nil {
					state = string(stateBytes)
				}
			}
		}
	}

	item := AutomationItem{
		GroupID:      groupID,
		AutomationID: automationID,
		Payload:      payload,
		State:        state,
	}

	if err := adb.automations.Put(adb.Ctx.Context, item); err != nil {
		return rmerror.NewRMError(err, "failed to create automation in DynamoDB")
	}

	return nil
}

// extractTriggerIDs extracts trigger IDs from the conditions object
// Conditions structure: {"and": ["trigger1", "trigger2"], "or": ["trigger3", "trigger4"]}
func extractTriggerIDs(conditions map[string]interface{}) []string {
	var triggerIDs []string
	seen := make(map[string]bool)

	// Process "and" conditions
	if andConditions, exists := conditions["and"]; exists {
		if andArray, ok := andConditions.([]interface{}); ok {
			for _, item := range andArray {
				if triggerID, ok := item.(string); ok && !seen[triggerID] {
					triggerIDs = append(triggerIDs, triggerID)
					seen[triggerID] = true
				}
			}
		}
	}

	// Process "or" conditions
	if orConditions, exists := conditions["or"]; exists {
		if orArray, ok := orConditions.([]interface{}); ok {
			for _, item := range orArray {
				if triggerID, ok := item.(string); ok && !seen[triggerID] {
					triggerIDs = append(triggerIDs, triggerID)
					seen[triggerID] = true
				}
			}
		}
	}

	return triggerIDs
}

// createTriggerValueMap creates a map of trigger IDs to their initial values (false)
func createTriggerValueMap(triggerIDs []string) map[string]bool {
	triggerValues := make(map[string]bool)
	for _, triggerID := range triggerIDs {
		triggerValues[triggerID] = false
	}
	return triggerValues
}

// UpdateAutomationState updates only the state of an existing automation in DynamoDB
func (adb *AutomationDB) UpdateAutomationState(groupID, automationID, state string) error {
	// Check authorization for updating automations in this group
	if err := adb.Ctx.IsAuthorized(utils.GroupEditAutomation, groupID); err != nil {
		return err
	}

	update := expression.Set(expression.Name("state"), expression.Value(state))
	if _, err := adb.automations.Update(adb.Ctx.Context, adb.automations.Key(groupID, automationID), update); err != nil {
		if ddbu.IsConditionFailed(err) {
			return rmerror.NewRMError(err, "automation does not exist")
		}
		return rmerror.NewRMError(err, "failed to update automation state in DynamoDB")
	}

	return nil
}

// GetAutomation retrieves a specific automation from DynamoDB
func (adb *AutomationDB) GetAutomation(groupID, automationID string) (*AutomationItem, error) {
	// Check authorization for accessing automations in this group.
	// Uses the dedicated GroupGetAutomation permission so subgroup-shared users
	// (who have GroupListSubEntities but not this) cannot read automations.
	if err := adb.Ctx.IsAuthorized(utils.GroupGetAutomation, groupID); err != nil {
		return nil, err
	}

	automation, found, err := adb.automations.Get(adb.Ctx.Context, adb.automations.Key(groupID, automationID))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get automation from DynamoDB")
	}
	if !found {
		return nil, rmerror.NewRMError(nil, "automation not found")
	}

	return &automation, nil
}

// ListGroupAutomations retrieves all automations for a group from DynamoDB
func (adb *AutomationDB) ListGroupAutomations(groupID string) ([]AutomationItem, error) {
	// Check authorization for listing automations in this group.
	// Uses the dedicated GroupGetAutomation permission so subgroup-shared users
	// (who have GroupListSubEntities but not this) cannot list automations.
	if err := adb.Ctx.IsAuthorized(utils.GroupGetAutomation, groupID); err != nil {
		return nil, err
	}

	// Every page, not one: DeleteNodeFromAutomations walks this list to strip a departing node.
	page, err := adb.automations.Query(adb.Ctx.Context, expression.Key("group_id").Equal(expression.Value(groupID)))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query automations in DynamoDB")
	}

	return page.Items, nil
}

// DeleteAutomation deletes an automation from DynamoDB
func (adb *AutomationDB) DeleteAutomation(groupID, automationID string) error {
	// Check authorization for deleting automations in this group
	if err := adb.Ctx.IsAuthorized(utils.GroupDeleteAutomation, groupID); err != nil {
		return err
	}

	// First check if the automation exists
	_, err := adb.GetAutomation(groupID, automationID)
	if err != nil {
		return err // Forward the error, which might be "automation not found"
	}

	err = adb.automations.DeleteAllowMissing(adb.Ctx.Context, adb.automations.Key(groupID, automationID))
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete automation from DynamoDB")
	}

	return nil
}

// DeleteAllGroupAutomations deletes all automations for a group.
func (adb *AutomationDB) DeleteAllGroupAutomations(groupID string) error {
	// Check authorization for deleting automations in this group
	if err := adb.Ctx.IsAuthorized(utils.GroupDeleteAutomation, groupID); err != nil {
		return err
	}

	// Returned unwrapped: an RMError would hide the "unprocessed" detail of a throttled batch behind its own message.
	err := adb.automations.QueryDelete(adb.Ctx.Context, expression.Key("group_id").Equal(expression.Value(groupID)))
	return err
}
