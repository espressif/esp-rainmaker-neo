// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// ConditionEvaluator defines the interface for evaluating automation conditions
type ConditionEvaluator interface {
	// EvaluateConditions evaluates the conditions against the current trigger values
	// Returns true if conditions are satisfied, false otherwise
	EvaluateConditions(conditions map[string]interface{}, triggerValues map[string]bool) (bool, error)
}

// DefaultConditionEvaluator is the default implementation of ConditionEvaluator
type DefaultConditionEvaluator struct{}

// NewConditionEvaluator creates a new instance of DefaultConditionEvaluator
func NewConditionEvaluator() ConditionEvaluator {
	return &DefaultConditionEvaluator{}
}

// EvaluateConditions evaluates automation conditions based on current trigger values
// Conditions structure: {"and": ["trigger1", "trigger2"], "or": ["trigger3", "trigger4"]}
// Mixed conditions: {"and": ["trigger1", "trigger2"], "or": ["trigger3", "trigger4"]}
// For mixed conditions: (AND conditions) OR (OR conditions)
func (e *DefaultConditionEvaluator) EvaluateConditions(conditions map[string]interface{}, triggerValues map[string]bool) (bool, error) {
	if len(conditions) == 0 {
		rlog.Debug(context.TODO()).Msg("No conditions to evaluate, returning false")
		return false, nil
	}

	if triggerValues == nil {
		rlog.Debug(context.TODO()).Msg("No trigger values provided, returning false")
		return false, nil
	}

	var andResult *bool
	var orResult *bool

	// Evaluate AND conditions
	if andConditions, exists := conditions["and"]; exists {
		result, err := e.evaluateAndConditions(andConditions, triggerValues)
		if err != nil {
			return false, err
		}
		andResult = &result
		rlog.Debug(context.TODO()).Msgf("AND conditions evaluated to: %v", result)
	}

	// Evaluate OR conditions
	if orConditions, exists := conditions["or"]; exists {
		result, err := e.evaluateOrConditions(orConditions, triggerValues)
		if err != nil {
			return false, err
		}
		orResult = &result
		rlog.Debug(context.TODO()).Msgf("OR conditions evaluated to: %v", result)
	}

	// Combine results: (AND conditions) OR (OR conditions)
	// If only AND conditions exist, return AND result
	// If only OR conditions exist, return OR result
	// If both exist, return (AND result) OR (OR result)
	var finalResult bool
	if andResult != nil && orResult != nil {
		finalResult = *andResult || *orResult
	} else if andResult != nil {
		finalResult = *andResult
	} else if orResult != nil {
		finalResult = *orResult
	} else {
		// No valid conditions found
		rlog.Debug(context.TODO()).Msg("No valid and/or conditions found")
		return false, nil
	}

	rlog.Info(context.TODO()).Msgf("Final condition evaluation result: %v", finalResult)
	return finalResult, nil
}

// evaluateAndConditions evaluates AND conditions - all triggers must be true
func (e *DefaultConditionEvaluator) evaluateAndConditions(andConditions interface{}, triggerValues map[string]bool) (bool, error) {
	andArray, ok := andConditions.([]interface{})
	if !ok {
		return false, rmerror.NewRMError(nil, "invalid format for AND conditions, expected array")
	}

	if len(andArray) == 0 {
		rlog.Debug(context.TODO()).Msg("Empty AND conditions array, returning true")
		return true, nil
	}

	for _, item := range andArray {
		triggerID, ok := item.(string)
		if !ok {
			rlog.Error(context.TODO()).Msgf("Invalid trigger ID in AND conditions: %v", item)
			continue
		}

		triggerValue, exists := triggerValues[triggerID]
		if !exists {
			rlog.Debug(context.TODO()).Msgf("Trigger %s not found in trigger values, treating as false", triggerID)
			return false, nil
		}

		if !triggerValue {
			rlog.Debug(context.TODO()).Msgf("Trigger %s is false, AND condition fails", triggerID)
			return false, nil
		}
	}

	rlog.Debug(context.TODO()).Msg("All AND conditions are true")
	return true, nil
}

// evaluateOrConditions evaluates OR conditions - at least one trigger must be true
func (e *DefaultConditionEvaluator) evaluateOrConditions(orConditions interface{}, triggerValues map[string]bool) (bool, error) {
	orArray, ok := orConditions.([]interface{})
	if !ok {
		return false, rmerror.NewRMError(nil, "invalid format for OR conditions, expected array")
	}

	if len(orArray) == 0 {
		rlog.Debug(context.TODO()).Msg("Empty OR conditions array, returning false")
		return false, nil
	}

	for _, item := range orArray {
		triggerID, ok := item.(string)
		if !ok {
			rlog.Error(context.TODO()).Msgf("Invalid trigger ID in OR conditions: %v", item)
			continue
		}

		triggerValue, exists := triggerValues[triggerID]
		if !exists {
			rlog.Debug(context.TODO()).Msgf("Trigger %s not found in trigger values, treating as false", triggerID)
			continue
		}

		if triggerValue {
			rlog.Debug(context.TODO()).Msgf("Trigger %s is true, OR condition succeeds", triggerID)
			return true, nil
		}
	}

	rlog.Debug(context.TODO()).Msg("No OR conditions are true")
	return false, nil
}
