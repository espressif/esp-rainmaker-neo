// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// scheduleService owns the stored-vs-firmware key translation and the version bump plus
// device push that every schedule write must trigger.
var scheduleService = schedule.NewScheduleService()

// maxScheduleNameLen is the firmware's bound on a schedule name.
const maxScheduleNameLen = 32

// ScheduleOperation is one edit to a node's schedule set.
type ScheduleOperation string

const (
	ScheduleAdd     ScheduleOperation = "add"
	ScheduleEdit    ScheduleOperation = "edit"
	ScheduleRemove  ScheduleOperation = "remove"
	ScheduleEnable  ScheduleOperation = "enable"
	ScheduleDisable ScheduleOperation = "disable"
)

// ScheduleInput carries the fields a write may set. Everything but the operation is optional;
// which of them are required is decided per operation.
type ScheduleInput struct {
	ScheduleID string
	Name       string
	Triggers   []map[string]interface{}
	Action     map[string]interface{}
	Enabled    *bool
	Info       string
}

// ListSchedules returns the schedules stored for a node, in the shape the device receives
// them. An empty list is a valid answer, not an error.
func ListSchedules(rmngCtx *rmngctx.RmngContext, groupID, nodeID string) ([]interface{}, error) {
	if err := authorizeNodeForUser(rmngCtx, groupID, nodeID); err != nil {
		return nil, err
	}
	return readSchedules(rmngCtx, nodeID)
}

// SetSchedule applies one operation to a node's schedule set and pushes the result to the
// device. rmng holds the authoritative schedule set in the cloud, so this is a
// read-modify-write of the whole array rather than an instruction handed to the firmware.
func SetSchedule(rmngCtx *rmngctx.RmngContext, groupID, nodeID string, operation ScheduleOperation, input ScheduleInput) (map[string]interface{}, error) {
	if err := authorizeNodeForUser(rmngCtx, groupID, nodeID); err != nil {
		return nil, err
	}

	// A schedule's action is a params payload, so it carries the same risk as set_params and is
	// checked the same way. Without this, a model refused by set_params would simply route the
	// invented parameter through a schedule instead, and the failure would surface at 7am.
	if len(input.Action) > 0 {
		if message := validateParamsForNode(rmngCtx, nodeID, input.Action); message != "" {
			return nil, fmt.Errorf("%s", message)
		}
	}

	existing, err := readSchedules(rmngCtx, nodeID)
	if err != nil {
		return nil, err
	}

	updated, affected, err := applyScheduleOperation(existing, operation, input)
	if err != nil {
		return nil, err
	}

	if err := scheduleService.Put(rmngCtx, nodeID, map[string]interface{}{schedule.APIScheduleKey: updated}); err != nil {
		return nil, err
	}
	return affected, nil
}

func readSchedules(rmngCtx *rmngctx.RmngContext, nodeID string) ([]interface{}, error) {
	stored, err := scheduleService.Get(rmngCtx, nodeID)
	if err != nil {
		return nil, err
	}

	asMap, ok := stored.(map[string]interface{})
	if !ok {
		return []interface{}{}, nil
	}
	schedules, ok := asMap[schedule.APIScheduleKey].([]interface{})
	if !ok {
		return []interface{}{}, nil
	}
	return schedules, nil
}

// applyScheduleOperation returns the new schedule set and the schedule the operation acted on.
// It is pure so the operation semantics can be tested without a database.
func applyScheduleOperation(existing []interface{}, operation ScheduleOperation, input ScheduleInput) ([]interface{}, map[string]interface{}, error) {
	switch operation {
	case ScheduleAdd:
		return addSchedule(existing, input)
	case ScheduleEdit:
		return editSchedule(existing, input)
	case ScheduleRemove:
		return removeSchedule(existing, input.ScheduleID)
	case ScheduleEnable:
		return setScheduleEnabled(existing, input.ScheduleID, true)
	case ScheduleDisable:
		return setScheduleEnabled(existing, input.ScheduleID, false)
	default:
		return nil, nil, fmt.Errorf("unknown operation %q — use add, edit, remove, enable or disable", operation)
	}
}

func addSchedule(existing []interface{}, input ScheduleInput) ([]interface{}, map[string]interface{}, error) {
	if input.Name == "" {
		return nil, nil, fmt.Errorf("name is required to add a schedule")
	}
	if len(input.Name) > maxScheduleNameLen {
		return nil, nil, fmt.Errorf("schedule name must be %d characters or fewer, got %d", maxScheduleNameLen, len(input.Name))
	}
	if len(input.Triggers) == 0 {
		return nil, nil, fmt.Errorf("at least one trigger is required to add a schedule, for example {\"time\": \"07:00\", \"days\": \"weekdays\"}")
	}
	if len(input.Action) == 0 {
		return nil, nil, fmt.Errorf("action is required to add a schedule, for example {\"Light\": {\"Power\": true}}")
	}

	triggers, err := convertTriggers(input.Triggers)
	if err != nil {
		return nil, nil, err
	}

	scheduleID := input.ScheduleID
	if scheduleID == "" {
		scheduleID = ids.GenerateScheduleID()
	} else if _, index := findSchedule(existing, scheduleID); index >= 0 {
		return nil, nil, fmt.Errorf("schedule %s already exists on this device — use operation edit to change it", scheduleID)
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	created := map[string]interface{}{
		"id":       scheduleID,
		"name":     input.Name,
		"enabled":  enabled,
		"triggers": triggers,
		"action":   input.Action,
	}
	if input.Info != "" {
		created["info"] = input.Info
	}

	return append(append([]interface{}{}, existing...), created), created, nil
}

func editSchedule(existing []interface{}, input ScheduleInput) ([]interface{}, map[string]interface{}, error) {
	target, index, err := requireSchedule(existing, input.ScheduleID, "edit")
	if err != nil {
		return nil, nil, err
	}

	if input.Name != "" {
		if len(input.Name) > maxScheduleNameLen {
			return nil, nil, fmt.Errorf("schedule name must be %d characters or fewer, got %d", maxScheduleNameLen, len(input.Name))
		}
		target["name"] = input.Name
	}
	if len(input.Triggers) > 0 {
		triggers, err := convertTriggers(input.Triggers)
		if err != nil {
			return nil, nil, err
		}
		target["triggers"] = triggers
	}
	if len(input.Action) > 0 {
		target["action"] = input.Action
	}
	if input.Enabled != nil {
		target["enabled"] = *input.Enabled
	}
	if input.Info != "" {
		target["info"] = input.Info
	}

	updated := replaceAt(existing, index, target)
	return updated, target, nil
}

func removeSchedule(existing []interface{}, scheduleID string) ([]interface{}, map[string]interface{}, error) {
	target, index, err := requireSchedule(existing, scheduleID, "remove")
	if err != nil {
		return nil, nil, err
	}
	updated := make([]interface{}, 0, len(existing)-1)
	updated = append(updated, existing[:index]...)
	updated = append(updated, existing[index+1:]...)
	return updated, target, nil
}

func setScheduleEnabled(existing []interface{}, scheduleID string, enabled bool) ([]interface{}, map[string]interface{}, error) {
	operation := "enable"
	if !enabled {
		operation = "disable"
	}
	target, index, err := requireSchedule(existing, scheduleID, operation)
	if err != nil {
		return nil, nil, err
	}
	target["enabled"] = enabled
	return replaceAt(existing, index, target), target, nil
}

func requireSchedule(existing []interface{}, scheduleID, operation string) (map[string]interface{}, int, error) {
	if scheduleID == "" {
		return nil, -1, fmt.Errorf("schedule_id is required to %s a schedule — call list_schedules to find it", operation)
	}
	target, index := findSchedule(existing, scheduleID)
	if index < 0 {
		return nil, -1, fmt.Errorf("this device has no schedule %s — call list_schedules to see the schedules it does have", scheduleID)
	}
	return target, index, nil
}

// findSchedule returns a copy of the matching schedule so an operation that fails part-way
// cannot leave the stored set half-edited.
func findSchedule(existing []interface{}, scheduleID string) (map[string]interface{}, int) {
	for index, item := range existing {
		asMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := asMap["id"].(string); id != scheduleID {
			continue
		}
		copied := make(map[string]interface{}, len(asMap))
		for key, value := range asMap {
			copied[key] = value
		}
		return copied, index
	}
	return nil, -1
}

func replaceAt(existing []interface{}, index int, value map[string]interface{}) []interface{} {
	updated := append([]interface{}{}, existing...)
	updated[index] = value
	return updated
}

// --- trigger conversion ---

// Weekday bitmask the firmware expects, Monday in the lowest bit.
var dayBits = map[string]int{
	"mon": 1, "monday": 1,
	"tue": 2, "tuesday": 2,
	"wed": 4, "wednesday": 4,
	"thu": 8, "thursday": 8,
	"fri": 16, "friday": 16,
	"sat": 32, "saturday": 32,
	"sun": 64, "sunday": 64,
}

var dayPresets = map[string]int{
	"daily":    127,
	"weekdays": 31,
	"weekends": 96,
}

func convertTriggers(triggers []map[string]interface{}) ([]interface{}, error) {
	converted := make([]interface{}, 0, len(triggers))
	for _, trigger := range triggers {
		deviceTrigger, err := convertTrigger(trigger)
		if err != nil {
			return nil, err
		}
		converted = append(converted, deviceTrigger)
	}
	return converted, nil
}

// convertTrigger turns a human-written trigger into the device's wire form: "m" is minutes
// past midnight and "d" is the weekday bitmask. A trigger already in that form, or a relative
// "rsec" trigger, passes through so a caller echoing back a listed schedule still works.
func convertTrigger(trigger map[string]interface{}) (map[string]interface{}, error) {
	converted := make(map[string]interface{}, len(trigger))
	for key, value := range trigger {
		if key != "time" && key != "days" {
			converted[key] = value
		}
	}

	if _, alreadyDeviceForm := trigger["m"]; alreadyDeviceForm {
		return converted, nil
	}

	timeValue, hasTime := trigger["time"]
	if !hasTime {
		if _, hasRelative := trigger["rsec"]; hasRelative {
			return converted, nil
		}
		return nil, fmt.Errorf("a trigger needs a time, for example {\"time\": \"07:00\", \"days\": \"weekdays\"}")
	}

	timeStr, ok := timeValue.(string)
	if !ok {
		return nil, fmt.Errorf("trigger time must be a string in HH:MM form")
	}
	minutes, err := parseTimeToMinutes(timeStr)
	if err != nil {
		return nil, err
	}
	converted["m"] = minutes

	if days, hasDays := trigger["days"]; hasDays {
		bitmask, err := parseDaysToBitmask(days)
		if err != nil {
			return nil, err
		}
		converted["d"] = bitmask
	}
	return converted, nil
}

func parseTimeToMinutes(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("time must be in HH:MM form, got %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 || hours > 23 {
		return 0, fmt.Errorf("hours must be between 00 and 23, got %q", value)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("minutes must be between 00 and 59, got %q", value)
	}
	return hours*60 + minutes, nil
}

func parseDaysToBitmask(days interface{}) (int, error) {
	switch value := days.(type) {
	case string:
		bitmask, ok := dayPresets[strings.ToLower(strings.TrimSpace(value))]
		if !ok {
			return 0, fmt.Errorf("unknown day preset %q — use daily, weekdays, weekends, or a list of day names", value)
		}
		return bitmask, nil

	case []interface{}:
		bitmask := 0
		for _, day := range value {
			name, ok := day.(string)
			if !ok {
				return 0, fmt.Errorf("day list must contain day names such as [\"mon\", \"tue\"]")
			}
			bit, ok := dayBits[strings.ToLower(strings.TrimSpace(name))]
			if !ok {
				return 0, fmt.Errorf("unknown day %q — use mon, tue, wed, thu, fri, sat or sun", name)
			}
			bitmask |= bit
		}
		return bitmask, nil

	case float64:
		return int(value), nil
	case int:
		return value, nil
	}
	return 0, fmt.Errorf("days must be a preset name, a list of day names, or a numeric bitmask")
}
