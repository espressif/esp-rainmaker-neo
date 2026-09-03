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
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
	dbcore.DB
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
		DB: *dbcore.NewDB(ctx),
	}
}

// CreateAutomation creates or replaces an automation in DynamoDB.
func (adb *AutomationDB) CreateAutomation(groupID, automationID string, payload interface{}) error {
	if err := adb.IsAuthorized(utils.GroupEditAutomation, groupID); err != nil {
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

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal automation item")
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(AutomationsTable),
		Item:      av,
	}

	_, err = adb.PutItem(adb.Ctx.Context, input)
	if err != nil {
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
	if err := adb.IsAuthorized(utils.GroupEditAutomation, groupID); err != nil {
		return err
	}

	// Build update and condition expressions using expression builder
	updateExpr := expression.Set(expression.Name("state"), expression.Value(state))
	conditionExpr := expression.And(
		expression.AttributeExists(expression.Name("group_id")),
		expression.AttributeExists(expression.Name("automation_id")),
	)

	expr, err := expression.NewBuilder().
		WithUpdate(updateExpr).
		WithCondition(conditionExpr).
		Build()
	if err != nil {
		return rmerror.NewRMError(err, "failed to build expressions")
	}

	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(AutomationsTable),
		Key: map[string]types.AttributeValue{
			"group_id":      &types.AttributeValueMemberS{Value: groupID},
			"automation_id": &types.AttributeValueMemberS{Value: automationID},
		},
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}

	_, err = adb.UpdateItem(adb.Ctx.Context, input)
	if err != nil {
		if dbcore.IsConditionalCheckFailedException(err) {
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
	if err := adb.IsAuthorized(utils.GroupGetAutomation, groupID); err != nil {
		return nil, err
	}

	input := &dynamodb.GetItemInput{
		TableName: aws.String(AutomationsTable),
		Key: map[string]types.AttributeValue{
			"group_id":      &types.AttributeValueMemberS{Value: groupID},
			"automation_id": &types.AttributeValueMemberS{Value: automationID},
		},
	}

	result, err := adb.GetItem(adb.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get automation from DynamoDB")
	}

	if result.Item == nil {
		return nil, rmerror.NewRMError(nil, "automation not found")
	}

	var automation AutomationItem
	err = attributevalue.UnmarshalMap(result.Item, &automation)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal automation item")
	}

	return &automation, nil
}

// ListGroupAutomations retrieves all automations for a group from DynamoDB
func (adb *AutomationDB) ListGroupAutomations(groupID string) ([]AutomationItem, error) {
	// Check authorization for listing automations in this group.
	// Uses the dedicated GroupGetAutomation permission so subgroup-shared users
	// (who have GroupListSubEntities but not this) cannot list automations.
	if err := adb.IsAuthorized(utils.GroupGetAutomation, groupID); err != nil {
		return nil, err
	}

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(AutomationsTable),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
	}

	// Paginated: DeleteNodeFromAutomations walks this list to strip a departing node, so a
	// truncated page would leave the tail of a group's automations pointing at that node.
	var automations []AutomationItem
	err := adb.QueryPaginated(adb.Ctx.Context, queryInput, func(item map[string]types.AttributeValue) error {
		var automation AutomationItem
		if err := attributevalue.UnmarshalMap(item, &automation); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal automation items")
		}
		automations = append(automations, automation)
		return nil
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query automations in DynamoDB")
	}

	return automations, nil
}

// DeleteAutomation deletes an automation from DynamoDB
func (adb *AutomationDB) DeleteAutomation(groupID, automationID string) error {
	// Check authorization for deleting automations in this group
	if err := adb.IsAuthorized(utils.GroupDeleteAutomation, groupID); err != nil {
		return err
	}

	// First check if the automation exists
	_, err := adb.GetAutomation(groupID, automationID)
	if err != nil {
		return err // Forward the error, which might be "automation not found"
	}

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(AutomationsTable),
		Key: map[string]types.AttributeValue{
			"group_id":      &types.AttributeValueMemberS{Value: groupID},
			"automation_id": &types.AttributeValueMemberS{Value: automationID},
		},
	}

	_, err = adb.DeleteItem(adb.Ctx.Context, input)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete automation from DynamoDB")
	}

	return nil
}

// DeleteAllGroupAutomations deletes all automations for a group using QueryAndBatchDelete
func (adb *AutomationDB) DeleteAllGroupAutomations(groupID string) error {
	// Check authorization for deleting automations in this group
	if err := adb.IsAuthorized(utils.GroupDeleteAutomation, groupID); err != nil {
		return err
	}

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(AutomationsTable),
		KeyConditionExpression: aws.String("group_id = :gid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":gid": &types.AttributeValueMemberS{Value: groupID},
		},
	}

	return adb.QueryAndBatchDelete(adb.Ctx.Context, queryInput, AutomationsTable, []string{"group_id", "automation_id"})
}
