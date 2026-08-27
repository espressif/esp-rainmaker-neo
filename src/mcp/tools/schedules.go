// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strconv"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/schedule"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// scheduleService owns the stored-vs-firmware key translation and the version bump plus
// device push that every schedule write must trigger.
var scheduleService = schedule.NewScheduleService()

// maxScheduleNameLen is the firmware's bound on a schedule name (esp_rmaker_schedule.c,
// MAX_NAME_LEN). A longer name is not truncated: the device's JSON parser refuses the
// over-length value, the name reads back empty, and the add is dropped.
const maxScheduleNameLen = 32

// maxScheduleIDLen is the firmware's bound on a schedule id (esp_rmaker_schedule.c,
// MAX_ID_LEN). The id is not cloud-side bookkeeping — the device copies it into the
// esp_schedule name and uses it verbatim as the NVS key, so an over-length one is refused the
// same way a name is, and the device drops the schedule while the cloud keeps it.
const maxScheduleIDLen = 8

// maxScheduleIDAttempts bounds the retries generateUnusedScheduleID makes before giving up.
const maxScheduleIDAttempts = 5

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
			return nil, guidancef("%s", message)
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

	if stored == nil {
		return []interface{}{}, nil
	}
	asMap, ok := stored.(map[string]interface{})
	if !ok {
		return nil, guidancef("stored schedules for %s are malformed", nodeID)
	}
	value, present := asMap[schedule.APIScheduleKey]
	if !present || value == nil {
		return []interface{}{}, nil
	}
	// Not a list means the stored value is something this code cannot merge into. Reporting it
	// keeps the read-modify-write in SetSchedule from Putting a one-element array over whatever
	// is really there.
	schedules, ok := value.([]interface{})
	if !ok {
		return nil, guidancef("stored schedules for %s are malformed", nodeID)
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
		return nil, nil, guidancef("unknown operation %q — use add, edit, remove, enable or disable", operation)
	}
}

func addSchedule(existing []interface{}, input ScheduleInput) ([]interface{}, map[string]interface{}, error) {
	if input.Name == "" {
		return nil, nil, guidancef("name is required to add a schedule")
	}
	if len(input.Name) > maxScheduleNameLen {
		return nil, nil, guidancef("schedule name must be %d characters or fewer, got %d", maxScheduleNameLen, len(input.Name))
	}
	if len(input.Triggers) == 0 {
		return nil, nil, guidancef("at least one trigger is required to add a schedule, for example {\"time\": \"07:00\", \"days\": \"weekdays\"}")
	}
	if len(input.Action) == 0 {
		return nil, nil, guidancef("action is required to add a schedule, for example {\"Light\": {\"Power\": true}}")
	}

	triggers, err := convertTriggers(input.Triggers)
	if err != nil {
		return nil, nil, err
	}

	scheduleID := input.ScheduleID
	if len(scheduleID) > maxScheduleIDLen {
		return nil, nil, guidancef("schedule_id must be %d characters or fewer, got %d — omit it and one will be generated", maxScheduleIDLen, len(scheduleID))
	}
	if scheduleID == "" {
		generated, err := generateUnusedScheduleID(existing)
		if err != nil {
			return nil, nil, err
		}
		scheduleID = generated
	} else if _, index := findSchedule(existing, scheduleID); index >= 0 {
		return nil, nil, guidancef("schedule %s already exists on this device — use operation edit to change it", scheduleID)
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
			return nil, nil, guidancef("schedule name must be %d characters or fewer, got %d", maxScheduleNameLen, len(input.Name))
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
		return nil, -1, guidancef("schedule_id is required to %s a schedule — call list_schedules to find it", operation)
	}
	target, index := findSchedule(existing, scheduleID)
	if index < 0 {
		return nil, -1, guidancef("this device has no schedule %s — call list_schedules to see the schedules it does have", scheduleID)
	}
	return target, index, nil
}

// findSchedule returns a copy of the matching schedule so an operation that fails part-way
// cannot leave the stored set half-edited.
// generateUnusedScheduleID mints an ID no schedule on this node already holds. The IDs are
// short enough to collide, and a duplicate is silent: findSchedule returns the first match, so
// a later edit or remove would act on the wrong schedule.
func generateUnusedScheduleID(existing []interface{}) (string, error) {
	for attempt := 0; attempt < maxScheduleIDAttempts; attempt++ {
		candidate := ids.GenerateScheduleID()
		if _, index := findSchedule(existing, candidate); index < 0 {
			return candidate, nil
		}
	}
	return "", guidancef("could not mint a free schedule id for this device — remove an unused schedule and try again")
}

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

	_, hasMinutes := trigger["m"]
	_, hasBitmask := trigger["d"]
	timeValue, hasTime := trigger["time"]
	days, hasDays := trigger["days"]

	// A model that lists a schedule and edits it can send the two forms mixed. Honouring both
	// halves separately is what keeps "the 07:00 one, but every weekday" from silently losing
	// its recurrence; the same field in both forms is a contradiction only the caller can settle.
	if hasMinutes && hasTime {
		return nil, guidancef("a trigger carries either m or time, not both — drop one")
	}
	if hasBitmask && hasDays {
		return nil, guidancef("a trigger carries either d or days, not both — drop one")
	}

	if hasDays {
		bitmask, err := parseDaysToBitmask(days)
		if err != nil {
			return nil, err
		}
		converted["d"] = bitmask
	}

	if hasMinutes {
		return converted, nil
	}

	if !hasTime {
		if _, hasRelative := trigger["rsec"]; hasRelative {
			return converted, nil
		}
		return nil, guidancef("a trigger needs a time, for example {\"time\": \"07:00\", \"days\": \"weekdays\"}")
	}

	timeStr, ok := timeValue.(string)
	if !ok {
		return nil, guidancef("trigger time must be a string in HH:MM form")
	}
	minutes, err := parseTimeToMinutes(timeStr)
	if err != nil {
		return nil, err
	}
	converted["m"] = minutes
	return converted, nil
}

func parseTimeToMinutes(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, guidancef("time must be in HH:MM form, got %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 || hours > 23 {
		return 0, guidancef("hours must be between 00 and 23, got %q", value)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, guidancef("minutes must be between 00 and 59, got %q", value)
	}
	return hours*60 + minutes, nil
}

func parseDaysToBitmask(days interface{}) (int, error) {
	switch value := days.(type) {
	case string:
		bitmask, ok := dayPresets[strings.ToLower(strings.TrimSpace(value))]
		if !ok {
			return 0, guidancef("unknown day preset %q — use daily, weekdays, weekends, or a list of day names", value)
		}
		return bitmask, nil

	case []interface{}:
		bitmask := 0
		for _, day := range value {
			name, ok := day.(string)
			if !ok {
				return 0, guidancef("day list must contain day names such as [\"mon\", \"tue\"]")
			}
			bit, ok := dayBits[strings.ToLower(strings.TrimSpace(name))]
			if !ok {
				return 0, guidancef("unknown day %q — use mon, tue, wed, thu, fri, sat or sun", name)
			}
			bitmask |= bit
		}
		return bitmask, nil

	case float64:
		return int(value), nil
	case int:
		return value, nil
	}
	return 0, guidancef("days must be a preset name, a list of day names, or a numeric bitmask")
}
