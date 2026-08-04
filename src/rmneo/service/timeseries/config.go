// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeseries

import (
	"context"
	"encoding/json"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/file"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// WeekStart represents the start of the week
type WeekStart string

const (
	WeekStartMonday WeekStart = "monday"
	WeekStartSunday WeekStart = "sunday"
)

// TimeseriesConfig holds timeseries processing configuration
type TimeseriesConfig struct {
	// WeekStart defines when a week starts (monday or sunday)
	WeekStart WeekStart `json:"week_start"`
}

// Global timeseries configuration
// Exported for testing purposes
var GTimeseriesConfig TimeseriesConfig

const TimeseriesConfigKey = "timeseries_config.json"

// LoadTimeseriesConfigFromDefaults sets default values for timeseries configuration
// Exported for testing purposes
func LoadTimeseriesConfigFromDefaults(c *TimeseriesConfig) {
	*c = TimeseriesConfig{
		WeekStart: WeekStartMonday, // Default to Monday as start of week
	}
}

func init() {
	DoInitTimeseriesConfig()
}

// DoInitTimeseriesConfig initializes the timeseries configuration
// Exported for testing purposes
func DoInitTimeseriesConfig() {
	// Initialize the configuration with defaults
	LoadTimeseriesConfigFromDefaults(&GTimeseriesConfig)

	// Load any customizations from S3 on top of the defaults
	if err := LoadTimeseriesConfigFromS3(&GTimeseriesConfig); err != nil {
		rlog.Error(context.TODO()).Err(err).Msg("Failed to load timeseries configuration from S3, using default configuration")
	}
}

// LoadTimeseriesConfigFromS3 fetches the timeseries configuration from S3 and updates GTimeseriesConfig
// Exported for testing purposes
func LoadTimeseriesConfigFromS3(c *TimeseriesConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	f, err := file.NewSystemFile(TimeseriesConfigKey)
	if err != nil {
		return err
	}
	content, err := f.ReadContent(ctx)
	if err != nil {
		return err
	}

	var tmp TimeseriesConfig
	if err := json.Unmarshal(content, &tmp); err != nil {
		return err
	}

	// Only overwrite if the value from S3 is valid
	if IsValidWeekStart(tmp.WeekStart) {
		c.WeekStart = tmp.WeekStart
	}

	rlog.Info(ctx).Str("week_start", string(c.WeekStart)).Msg("Successfully loaded timeseries configuration from S3")
	return nil
}

// GetTimeseriesConfig returns a copy of the current timeseries configuration
// Exported for testing purposes
func GetTimeseriesConfig() TimeseriesConfig {
	return GTimeseriesConfig
}

// GetWeekStart returns the configured week start
func GetWeekStart() WeekStart {
	return GTimeseriesConfig.WeekStart
}

// GetWeekStartWeekday returns the weekday for the configured week start
func GetWeekStartWeekday() time.Weekday {
	switch GTimeseriesConfig.WeekStart {
	case WeekStartSunday:
		return time.Sunday
	case WeekStartMonday:
		return time.Monday
	default:
		return time.Monday // Default fallback
	}
}

// IsValidWeekStart checks if a week start value is valid
func IsValidWeekStart(weekStart WeekStart) bool {
	return weekStart == WeekStartMonday || weekStart == WeekStartSunday
}
