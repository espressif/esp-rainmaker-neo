// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package timewindow provides time window management functionality for timeseries data processing.
This package is used by both the database and service layers to handle time-based aggregations
and window boundary calculations.
*/
package timewindow

import (
	"time"
)

// TimeWindow represents different aggregation windows
type TimeWindow string

const (
	WindowHourly  TimeWindow = "hourly"
	WindowDaily   TimeWindow = "daily"
	WindowWeekly  TimeWindow = "weekly"
	WindowMonthly TimeWindow = "monthly"
)

// GetSupportedWindows returns all supported time windows
func GetSupportedWindows() []TimeWindow {
	return []TimeWindow{
		WindowHourly,
		WindowDaily,
		WindowWeekly,
		WindowMonthly,
	}
}

// ConvertToTimezone converts an epoch timestamp to a timezone-aware time.
// tz is an IANA timezone identifier (e.g. "UTC", "Asia/Kolkata"). Falls back to UTC on parse failure.
func ConvertToTimezone(epochSeconds int64, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return time.Unix(epochSeconds, 0).In(loc), nil
}

// GetWindowBoundariesWithWeekStart returns the start and end times for a given window with configurable week start
func GetWindowBoundariesWithWeekStart(t time.Time, window TimeWindow, weekStartDay time.Weekday) (time.Time, time.Time) {
	switch window {
	case WindowHourly:
		start := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
		end := start.Add(time.Hour)
		return start, end
	case WindowDaily:
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		end := start.AddDate(0, 0, 1)
		return start, end
	case WindowWeekly:
		// Use provided week start day
		currentDay := t.Weekday()

		var daysToSubtract int
		if weekStartDay == time.Sunday {
			// If week starts on Sunday
			daysToSubtract = int(currentDay)
		} else {
			// If week starts on Monday
			days := int(currentDay)
			if days == 0 { // Sunday
				days = 7
			}
			daysToSubtract = days - 1
		}

		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, -daysToSubtract)
		end := start.AddDate(0, 0, 7)
		return start, end
	case WindowMonthly:
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
		end := start.AddDate(0, 1, 0)
		return start, end
	default:
		return t, t
	}
}

// GetWindowBoundaries returns the start and end times for a given window with default Monday week start
func GetWindowBoundaries(t time.Time, window TimeWindow) (time.Time, time.Time) {
	return GetWindowBoundariesWithWeekStart(t, window, time.Monday)
}

// FormatWindowKey formats a time into a window key like "hourly#2025-01-04T14"
func FormatWindowKey(t time.Time, window TimeWindow) string {
	switch window {
	case WindowHourly:
		return string(window) + "#" + t.Format("2006-01-02T15")
	case WindowDaily:
		return string(window) + "#" + t.Format("2006-01-02")
	case WindowWeekly:
		// Use start of week
		_, weekEnd := GetWindowBoundaries(t, window)
		return string(window) + "#" + weekEnd.AddDate(0, 0, -7).Format("2006-01-02")
	case WindowMonthly:
		return string(window) + "#" + t.Format("2006-01")
	default:
		return string(window) + "#" + t.Format("2006-01-02T15")
	}
}

// FormatWindowKeyWithWeekStart formats a time into a window key with configurable week start
func FormatWindowKeyWithWeekStart(t time.Time, window TimeWindow, weekStartDay time.Weekday) string {
	switch window {
	case WindowHourly:
		return string(window) + "#" + t.Format("2006-01-02T15")
	case WindowDaily:
		return string(window) + "#" + t.Format("2006-01-02")
	case WindowWeekly:
		// Use start of week
		_, weekEnd := GetWindowBoundariesWithWeekStart(t, window, weekStartDay)
		return string(window) + "#" + weekEnd.AddDate(0, 0, -7).Format("2006-01-02")
	case WindowMonthly:
		return string(window) + "#" + t.Format("2006-01")
	default:
		return string(window) + "#" + t.Format("2006-01-02T15")
	}
}

// HasWindowCrossed checks if a timestamp crosses a window boundary compared to a reference time
func HasWindowCrossed(refTime, newTime time.Time, window TimeWindow) bool {
	refStart, _ := GetWindowBoundaries(refTime, window)
	newStart, _ := GetWindowBoundaries(newTime, window)
	return !refStart.Equal(newStart)
}

// HasWindowCrossedWithWeekStart checks if a timestamp crosses a window boundary with configurable week start
func HasWindowCrossedWithWeekStart(refTime, newTime time.Time, window TimeWindow, weekStartDay time.Weekday) bool {
	refStart, _ := GetWindowBoundariesWithWeekStart(refTime, window, weekStartDay)
	newStart, _ := GetWindowBoundariesWithWeekStart(newTime, window, weekStartDay)
	return !refStart.Equal(newStart)
}
