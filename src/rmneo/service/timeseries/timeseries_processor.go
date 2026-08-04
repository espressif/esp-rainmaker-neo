// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package timeseries provides timeseries processing functionality that sits between the stream processor
and the database layer.

This processor handles:
- Business logic for aggregation processing
- Value type conversion and validation
- Window boundary management
- Meter reset detection for cumulative data (when value decreases, treat as absolute consumption)
*/

package timeseries

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// WindowAggregatesProcessor handles aggregation calculations (business logic)
type WindowAggregatesProcessor struct {
	Aggregates processed_ts_db.WindowAggregates
}

// UpdateStats updates the aggregate statistics with a new value (business logic)
func (agg *WindowAggregatesProcessor) UpdateStats(value float64, isCumulative bool, windowStart, windowEnd time.Time, dataTimestamp time.Time) {
	if agg.Aggregates.Count == 0 && (agg.Aggregates.FirstValue == 0 || !isCumulative) {
		// First data point - for cumulative data, we can't calculate consumption yet
		agg.Aggregates.FirstValue = value
		agg.Aggregates.LastValue = value
		agg.Aggregates.WindowStart = windowStart.Unix()
		agg.Aggregates.WindowEnd = windowEnd.Unix()
		agg.Aggregates.LastDataTimestamp = dataTimestamp.Unix()

		if isCumulative {
			// For cumulative data, we need at least 2 readings to calculate consumption
			// So we don't increment count or update aggregates yet
			agg.Aggregates.CumulativeValue = value // Store the baseline cumulative reading
			agg.Aggregates.Count = 0               // Wait for next reading
			agg.Aggregates.Sum = 0
			agg.Aggregates.Min = 0
			agg.Aggregates.Max = 0
			agg.Aggregates.Average = 0
		} else {
			// For non-cumulative data, use the value directly
			agg.Aggregates.Count = 1
			agg.Aggregates.Sum = value
			agg.Aggregates.Min = value
			agg.Aggregates.Max = value
			agg.Aggregates.Average = value
		}
	} else {
		// Additional data points
		previousValue := agg.Aggregates.LastValue // Save the previous value BEFORE updating
		agg.Aggregates.LastValue = value
		agg.Aggregates.WindowEnd = windowEnd.Unix()
		agg.Aggregates.LastDataTimestamp = dataTimestamp.Unix()

		if isCumulative {
			// Calculate actual consumption since last reading
			// Handle meter reset: if current value < previous value, treat as absolute consumption
			var consumption float64
			if value < previousValue {
				// Meter reset detected - treat current value as absolute consumption
				consumption = value
			} else {
				// Normal case - calculate consumption difference
				consumption = value - previousValue
			}

			// Handle the case where this is the second reading (first consumption calculation)
			if agg.Aggregates.Count == 0 {
				// This is our first consumption reading
				agg.Aggregates.Count = 1
				agg.Aggregates.Sum = consumption
				agg.Aggregates.Min = consumption
				agg.Aggregates.Max = consumption
				agg.Aggregates.Average = consumption
			} else {
				// Additional consumption readings
				agg.Aggregates.Count++
				agg.Aggregates.Sum += consumption
				if consumption < agg.Aggregates.Min {
					agg.Aggregates.Min = consumption
				}
				if consumption > agg.Aggregates.Max {
					agg.Aggregates.Max = consumption
				}
				agg.Aggregates.Average = agg.Aggregates.Sum / float64(agg.Aggregates.Count)
			}

			// Store the latest cumulative reading
			agg.Aggregates.CumulativeValue = value
		} else {
			// Non-cumulative data - use value directly
			agg.Aggregates.Count++
			agg.Aggregates.Sum += value
			if value < agg.Aggregates.Min {
				agg.Aggregates.Min = value
			}
			if value > agg.Aggregates.Max {
				agg.Aggregates.Max = value
			}
			agg.Aggregates.Average = agg.Aggregates.Sum / float64(agg.Aggregates.Count)
		}
	}
}

// Reset resets the aggregates for a new window while preserving cumulative continuity (business logic)
func (agg *WindowAggregatesProcessor) Reset(isCumulative bool, windowStart, windowEnd time.Time) {
	lastValue := agg.Aggregates.LastValue
	agg.Aggregates = processed_ts_db.WindowAggregates{
		WindowStart: windowStart.Unix(),
		WindowEnd:   windowEnd.Unix(),
	}
	if isCumulative {
		// For cumulative data, the last value becomes the first value of the new window
		// This ensures continuity for consumption calculations
		agg.Aggregates.FirstValue = lastValue
		agg.Aggregates.LastValue = lastValue
		agg.Aggregates.CumulativeValue = lastValue // Preserve the latest cumulative reading
		// Reset aggregates - they'll be recalculated as consumption differences come in
		agg.Aggregates.Count = 0
		agg.Aggregates.Sum = 0
		agg.Aggregates.Min = 0
		agg.Aggregates.Max = 0
		agg.Aggregates.Average = 0
	}
}

// ToMap converts the aggregates to a map for API responses
func (agg *WindowAggregatesProcessor) ToMap() map[string]interface{} {
	return agg.Aggregates.ToMap()
}

// ToWindowAggregates converts to database storage format
func (agg *WindowAggregatesProcessor) ToWindowAggregates() processed_ts_db.WindowAggregates {
	return agg.Aggregates
}

// FromWindowAggregates converts from database storage format
func (agg *WindowAggregatesProcessor) FromWindowAggregates(dbAgg processed_ts_db.WindowAggregates) {
	agg.Aggregates = dbAgg
}

// getWindowAggregates returns a pointer to the appropriate WindowAggregates for the given window type
func getWindowAggregates(entry *processed_ts_db.ProcessedTsEntry, window timewindow.TimeWindow) *processed_ts_db.WindowAggregates {
	switch window {
	case timewindow.WindowHourly:
		return &entry.HourlyAggregates
	case timewindow.WindowDaily:
		return &entry.DailyAggregates
	case timewindow.WindowWeekly:
		return &entry.WeeklyAggregates
	case timewindow.WindowMonthly:
		return &entry.MonthlyAggregates
	default:
		return nil
	}
}

// TimeseriesProcessor handles the overall timeseries processing workflow
type TimeseriesProcessor struct {
	timeseriesDB  *timeseries_db.TimeseriesDB
	processedTsDB *processed_ts_db.ProcessedTsDB
	rmngCtx       *rmngctx.RmngContext
}

// NewTimeseriesProcessor creates a new TimeseriesProcessor instance
func NewTimeseriesProcessor(ctx *rmngctx.RmngContext) *TimeseriesProcessor {
	// Note: This processor is typically used with a system actor context
	// from the stream processor, which bypasses normal user access controls
	// since it's processing IoT data streams that are already validated at ingestion
	return &TimeseriesProcessor{
		timeseriesDB:  timeseries_db.NewTimeseriesDB(ctx),
		processedTsDB: processed_ts_db.NewProcessedTsDB(ctx),
		rmngCtx:       ctx,
	}
}

// ProcessTimeseriesEntry processes a single timeseries entry for all window aggregations
func (p *TimeseriesProcessor) ProcessTimeseriesEntry(rawEntry *timeseries_db.TimeseriesEntry) error {
	// Convert epoch timestamp to timezone-aware time
	// Note: rawEntry.Timestamp is in milliseconds, but ConvertToTimezone expects seconds
	tsTime, err := timewindow.ConvertToTimezone(rawEntry.Timestamp/1000, rawEntry.Timezone)
	if err != nil {
		return rmerror.NewRMError(err, "failed to convert timestamp to timezone")
	}

	// Get current processed entry for this parameter from database
	dbEntry, err := p.processedTsDB.GetCurrentEntry(rawEntry.NodeID, rawEntry.DataKey, rawEntry.DataType)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get current processed entry")
	}

	// Create new entry if none exists
	var currentEntry *ProcessedTsEntryProcessor
	if dbEntry == nil {
		currentEntry = &ProcessedTsEntryProcessor{Entry: p.createNewProcessedEntry(rawEntry, tsTime)}
	} else {
		currentEntry = &ProcessedTsEntryProcessor{Entry: dbEntry}
	}

	// Convert value to float64 for processing
	value, err := p.ConvertValueToFloat64(rawEntry.Value)
	if err != nil {
		return rmerror.NewRMError(err, "failed to convert value to float64")
	}

	// Process all window types
	for _, window := range timewindow.GetSupportedWindows() {
		// Check for window boundary crossings and create historical entries
		if err := p.checkAndCreateWindowEntry(currentEntry, window, tsTime); err != nil {
			return err
		}

		// Update aggregates for this window
		windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(tsTime, window, time.Monday)
		currentEntry.UpdateAggregatesForWindow(window, value, rawEntry.Cumulative, windowStart, windowEnd, tsTime)
	}

	// Update timestamps: UpdatedAt to current cloud time, LastUpdateTime to device timestamp
	currentEntry.Entry.UpdatedAt = time.Now().Unix()  // Cloud timestamp when record was last updated
	currentEntry.Entry.LastUpdateTime = tsTime.Unix() // Device timestamp (timezone-aware) of the data point

	// Save the updated entry
	if err := p.processedTsDB.UpsertCurrentEntry(currentEntry.Entry); err != nil {
		return rmerror.NewRMError(err, "failed to upsert current processed entry")
	}

	return nil
}

// checkAndCreateWindowEntry checks for window boundary crossings and creates historical entries
func (p *TimeseriesProcessor) checkAndCreateWindowEntry(currentEntry *ProcessedTsEntryProcessor, window timewindow.TimeWindow, currentTime time.Time) error {
	// Get the current window aggregates
	currentAggregates := currentEntry.GetWindowAggregatesProcessor(window)
	if currentAggregates == nil {
		return fmt.Errorf("invalid window type: %v", window)
	}

	// Get window boundaries for current time
	windowStart, windowEnd := timewindow.GetWindowBoundariesWithWeekStart(currentTime, window, time.Monday)

	// If this is the first reading for this window (Count == 0), initialize the window boundaries
	if currentAggregates.Aggregates.Count == 0 {
		currentAggregates.Aggregates.WindowStart = windowStart.Unix()
		currentAggregates.Aggregates.WindowEnd = windowEnd.Unix()
		return nil // No historical entry to create
	}

	// If the current aggregates are for a different window, create a historical entry
	if currentAggregates.Aggregates.WindowStart != windowStart.Unix() {
		// Create historical entry with the completed window data.
		// Use the OLD window's start (from the aggregates being archived) for the IntervalKey,
		// interpreted in the node's timezone so the date string is correct.
		oldWindowStart, _ := timewindow.ConvertToTimezone(currentAggregates.Aggregates.WindowStart, currentEntry.Entry.Timezone)
		now := time.Now().Unix()
		historicalEntry := &processed_ts_db.ProcessedTsEntry{
			NodeKeyDt:        currentEntry.Entry.NodeKeyDt,
			IntervalKey:      timewindow.FormatWindowKeyWithWeekStart(oldWindowStart, window, time.Monday),
			NodeID:           currentEntry.Entry.NodeID,
			DataKey:          currentEntry.Entry.DataKey,
			DataType:         currentEntry.Entry.DataType,
			Timezone:         currentEntry.Entry.Timezone,
			WindowType:       string(window),
			WindowAggregates: currentAggregates.Aggregates,
			IsCumulative:     currentEntry.Entry.IsCumulative,
			LastUpdateTime:   time.Unix(currentAggregates.Aggregates.LastDataTimestamp, 0).Unix(), // Use last data timestamp from aggregates
			UpdatedAt:        now,                                                                 // Cloud timestamp when historical record was created
		}

		// Save the historical entry
		err := p.processedTsDB.CreateWindowEntry(historicalEntry, historicalEntry.IntervalKey)
		if err != nil {
			return fmt.Errorf("failed to create historical window entry: %w", err)
		}

		// Reset the current window aggregates for the new window
		currentEntry.ResetWindowAggregates(window, currentEntry.Entry.IsCumulative, windowStart, windowEnd)
	}

	return nil
}

// createNewProcessedEntry creates a new ProcessedTsEntry from a raw timeseries entry
func (p *TimeseriesProcessor) createNewProcessedEntry(rawEntry *timeseries_db.TimeseriesEntry, tsTime time.Time) *processed_ts_db.ProcessedTsEntry {
	now := time.Now().Unix()
	return &processed_ts_db.ProcessedTsEntry{
		NodeKeyDt:      rawEntry.NodeKeyDt,
		IntervalKey:    "current",
		NodeID:         rawEntry.NodeID,
		DataKey:        rawEntry.DataKey,
		DataType:       rawEntry.DataType,
		Timezone:       rawEntry.Timezone,
		IsCumulative:   rawEntry.Cumulative,
		UpdatedAt:      now,           // Cloud timestamp when record was last updated
		LastUpdateTime: tsTime.Unix(), // Device timestamp (timezone-aware) of the data point
	}
}

// ConvertValueToFloat64 converts various value types to float64 for processing
func (p *TimeseriesProcessor) ConvertValueToFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, nil
		}
		return 0, fmt.Errorf("cannot convert string '%s' to float64", v)
	case bool:
		if v {
			return 1.0, nil
		}
		return 0.0, nil
	default:
		return 0, fmt.Errorf("unsupported value type: %T", value)
	}
}

// ValidateTimeseriesEntry validates a timeseries entry for completeness
func (p *TimeseriesProcessor) ValidateTimeseriesEntry(entry *timeseries_db.TimeseriesEntry) error {
	if entry.NodeID == "" {
		return fmt.Errorf("missing required field: node_id")
	}
	if entry.Timestamp == 0 {
		return fmt.Errorf("missing required field: timestamp")
	}
	if entry.DataKey == "" {
		return fmt.Errorf("missing required field: name")
	}
	if entry.Value == nil {
		return fmt.Errorf("missing required field: value")
	}
	if entry.DataType == "" {
		return fmt.Errorf("missing required field: data_type")
	}
	return nil
}

// ProcessedTsEntryProcessor handles business logic for processed timeseries entries
type ProcessedTsEntryProcessor struct {
	Entry *processed_ts_db.ProcessedTsEntry
}

// GetWindowAggregatesProcessor returns a pointer to the appropriate WindowAggregatesProcessor for the given window type
func (entry *ProcessedTsEntryProcessor) GetWindowAggregatesProcessor(window timewindow.TimeWindow) *WindowAggregatesProcessor {
	dbAgg := getWindowAggregates(entry.Entry, window)
	if dbAgg == nil {
		return nil
	}
	return &WindowAggregatesProcessor{Aggregates: *dbAgg}
}

// UpdateAggregatesForWindow updates aggregates for a specific window type (business logic)
func (entry *ProcessedTsEntryProcessor) UpdateAggregatesForWindow(window timewindow.TimeWindow, value float64, isCumulative bool, windowStart, windowEnd time.Time, dataTimestamp time.Time) {
	if agg := entry.GetWindowAggregatesProcessor(window); agg != nil {
		agg.UpdateStats(value, isCumulative, windowStart, windowEnd, dataTimestamp)
		// Update the DB entry with the processed aggregates
		dbAgg := getWindowAggregates(entry.Entry, window)
		if dbAgg != nil {
			*dbAgg = agg.Aggregates
		}
	}
	entry.Entry.LastUpdateTime = dataTimestamp.Unix() // Device timestamp of the data point
}

// ResetWindowAggregates resets the aggregates for a specific window when crossing boundaries (business logic)
func (entry *ProcessedTsEntryProcessor) ResetWindowAggregates(window timewindow.TimeWindow, isCumulative bool, windowStart, windowEnd time.Time) {
	if agg := entry.GetWindowAggregatesProcessor(window); agg != nil {
		agg.Reset(isCumulative, windowStart, windowEnd)
		// Update the DB entry with the reset aggregates
		dbAgg := getWindowAggregates(entry.Entry, window)
		if dbAgg != nil {
			*dbAgg = agg.Aggregates
		}
	}
}
