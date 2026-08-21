// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package timeseries

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/timeseries_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/timeutil"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"
)

// TimeseriesService provides timeseries data access functionality
type TimeseriesService struct {
	service.BaseService
}

// NewTimeseriesService creates a new timeseries service instance
func NewTimeseriesService() *TimeseriesService {
	return &TimeseriesService{
		BaseService: service.BaseService{
			Name:      "timeseries",
			Versioned: false,
		},
	}
}

// Register adds the timeseries service to the service registry
func Register() {
	service.Registry().RegisterNodeService(NewTimeseriesService())
}

// TimeseriesQueryRequest represents the request structure for querying timeseries data
type TimeseriesQueryRequest struct {
	DataKey   string `json:"key"`
	DataType  string `json:"data_type"`
	StartTime int64  `json:"start_time,omitempty"`
	EndTime   int64  `json:"end_time,omitempty"`
	Limit     int32  `json:"limit,omitempty"`
}

// TimeseriesQueryResponse represents the response structure for timeseries queries
type TimeseriesQueryResponse struct {
	Data []TimeseriesDataPoint `json:"data"`
}

// TimeseriesDataPoint represents a single timeseries data point
type TimeseriesDataPoint struct {
	Timestamp  int64       `json:"ts"`
	Value      interface{} `json:"value"`
	Timezone   string      `json:"tz,omitempty"`
	Cumulative bool        `json:"cumulative,omitempty"`
}

// Get retrieves timeseries data for a node
func (s *TimeseriesService) Get(rmngCtx *rmngctx.RmngContext, nodeID string) (interface{}, error) {
	// Check for timeseries sub-dataKey (raw, latest, aggregates) from context
	timeseriesType, _ := rmngCtx.Context.Value("timeseries_type").(string)

	// Check if query parameters are provided in the context
	queryParams, ok := rmngCtx.Context.Value("query_params").(map[string]string)
	if !ok || len(queryParams) == 0 {
		// No query parameters provided, return service information
		return s.getServiceInfo(nodeID), nil
	}

	// Extract query parameters
	dataKey := queryParams["key"]
	dataType := queryParams["data_type"]
	dataTypeParam := queryParams["type"]
	window := queryParams["window"]
	date := queryParams["date"]
	startDate := queryParams["start_date"]
	endDate := queryParams["end_date"]
	pageSize := int32(rmngrequest.ParsePageSize(queryParams))
	startKey := queryParams["start_key"]
	startTimeStr := queryParams["start_time"]
	endTimeStr := queryParams["end_time"]

	// DataKey and data type are required
	if dataKey == "" || dataType == "" {
		return nil, rmerror.NewRMError(nil, "key and data_type query parameters are required")
	}

	// Use sub-dataKey if present, otherwise use type query parameter, default to raw
	if timeseriesType != "" {
		dataTypeParam = timeseriesType
	} else if dataTypeParam == "" {
		dataTypeParam = "raw"
	}

	// Validate type parameter
	if dataTypeParam != "raw" && dataTypeParam != "latest" && dataTypeParam != "aggregates" {
		return nil, rmerror.NewRMError(nil, "invalid type parameter. Must be one of: raw, latest, aggregates")
	}

	// Validate permissions
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// Create database connections
	tsDB := timeseries_db.NewTimeseriesDB(rmngCtx)
	processedTsDB := processed_ts_db.NewProcessedTsDB(rmngCtx)

	// Handle aggregates type
	if dataTypeParam == "aggregates" {
		// If using /aggregates sub-dataKey, window is required
		if timeseriesType == "aggregates" && window == "" {
			return nil, rmerror.NewRMError(nil, "window query parameter is required for aggregates timeseries data")
		}
		return s.handleAggregatesRequest(processedTsDB, nodeID, dataKey, dataType, window, date, startDate, endDate, pageSize, startKey)
	}

	// Handle latest type
	if dataTypeParam == "latest" {
		dataPoint, err := s.GetLatest(rmngCtx, nodeID, dataKey, dataType)
		if err != nil {
			return nil, err
		}

		// Return as single object (not array) for latest endpoint
		return map[string]interface{}{
			"data": map[string]interface{}{
				"key":        dataKey,
				"dt":         dataType,
				"ts":         dataPoint.Timestamp,
				"value":      dataPoint.Value,
				"tz":         dataPoint.Timezone,
				"cumulative": dataPoint.Cumulative,
			},
		}, nil
	}

	// Handle raw type (default)
	// If using /raw sub-dataKey, start_time is required
	if timeseriesType == "raw" && startTimeStr == "" {
		return nil, rmerror.NewRMError(nil, "start_time query parameter is required for raw timeseries data")
	}
	return s.handleRawDataRequest(tsDB, nodeID, dataKey, dataType, startTimeStr, endTimeStr, pageSize, startKey)
}

// getServiceInfo returns service information and usage examples when no query parameters are provided
func (s *TimeseriesService) getServiceInfo(nodeID string) map[string]interface{} {
	return map[string]interface{}{
		"service": "timeseries",
		"message": "Use query parameters to query timeseries data",
		"parameters": map[string]string{
			"key":       "Data point key (required)",
			"data_type": "Data type (required)",
			"type":      "Data type: raw (default), latest, aggregates",
		},
		"raw_data": map[string]string{
			"start_time": "Start timestamp in milliseconds (optional)",
			"end_time":   "End timestamp in milliseconds (optional)",
			"page_size":  "Maximum records per page (optional)",
			"start_key":  "Pagination token from a previous response's next_key (optional)",
		},
		"aggregates": map[string]string{
			"window":     "Specific window type (optional: hourly, daily, weekly, monthly)",
			"date":       "Specific date for historical aggregates (YYYY-MM-DD format, or YYYY-MM-DDTHH for hourly)",
			"start_date": "Start date for historical aggregates range (YYYY-MM-DD format, or YYYY-MM-DDTHH for hourly)",
			"end_date":   "End date for historical aggregates range (YYYY-MM-DD format, or YYYY-MM-DDTHH for hourly)",
			"page_size":  "Maximum number of historical aggregates per page (optional)",
			"start_key":  "Pagination token from a previous response's next_key (optional)",
		},
		"examples": map[string]string{
			"raw_data":              fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/raw?key=temperature&data_type=float", nodeID),
			"raw_with_time_range":   fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/raw?key=temperature&data_type=float&start_time=1704067200000&end_time=1704153600000", nodeID),
			"latest_data":           fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/latest?key=temperature&data_type=float", nodeID),
			"current_all":           fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float", nodeID),
			"current_daily":         fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=daily", nodeID),
			"historical_daily":      fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=daily&date=2025-01-15", nodeID),
			"historical_hour":       fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=hourly&date=2025-01-15T14", nodeID),
			"range_daily":           fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=daily&start_date=2025-01-01&end_date=2025-01-31", nodeID),
			"range_hourly":          fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=hourly&start_date=2025-01-15T10&end_date=2025-01-15T15", nodeID),
			"range_hourly_mixed":    fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=hourly&start_date=2025-01-15&end_date=2025-01-15T23", nodeID),
			"range_monthly_year":    fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=monthly&start_date=2025-01-01&end_date=2025-12-31", nodeID),
			"range_daily_paginated": fmt.Sprintf("/v1/groups/{groupId}/nodes/%s/timeseries/aggregates?key=temperature&data_type=float&window=daily&start_date=2025-01-01&end_date=2025-01-31&page_size=10", nodeID),
		},
	}
}

func (s *TimeseriesService) handleAggregatesRequest(processedTsDB *processed_ts_db.ProcessedTsDB, nodeID, dataKey, dataType, window, date, startDate, endDate string, pageSize int32, startKey string) (interface{}, error) {
	// If date range is specified, get historical aggregates range
	if startDate != "" || endDate != "" {
		return s.handleHistoricalAggregatesRange(processedTsDB, nodeID, dataKey, dataType, window, startDate, endDate, pageSize, startKey)
	}

	// If single date is specified, get historical aggregates for that date
	if date != "" {
		return s.handleHistoricalAggregates(processedTsDB, nodeID, dataKey, dataType, window, date)
	}

	// Get current aggregates
	if window != "" {
		// Get current aggregates for specific window
		var windowType timewindow.TimeWindow
		switch window {
		case "hourly":
			windowType = timewindow.WindowHourly
		case "daily":
			windowType = timewindow.WindowDaily
		case "weekly":
			windowType = timewindow.WindowWeekly
		case "monthly":
			windowType = timewindow.WindowMonthly
		default:
			return nil, rmerror.NewRMError(nil, "invalid window type. Must be one of: hourly, daily, weekly, monthly")
		}

		aggregates, err := processedTsDB.GetCurrentAggregatesForWindow(nodeID, dataKey, dataType, windowType)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to get current aggregates for window")
		}

		// Return as array for consistency with other APIs
		return map[string]interface{}{
			"aggregates": []map[string]interface{}{aggregates},
		}, nil
	} else {
		// Get current aggregates for all windows
		aggregates, err := processedTsDB.GetAllCurrentAggregates(nodeID, dataKey, dataType)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to get all current aggregates")
		}

		// Extract is_cumulative from any window that has data
		isCumulative := false
		for _, windowData := range aggregates {
			if windowMap, ok := windowData.(map[string]interface{}); ok {
				if cumulative, exists := windowMap["is_cumulative"]; exists {
					if cumulativeBool, ok := cumulative.(bool); ok {
						isCumulative = cumulativeBool
						break
					}
				}
			}
		}

		// Return as array for consistency with other APIs
		return map[string]interface{}{
			"aggregates": []map[string]interface{}{
				{
					"parameter":     fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
					"is_cumulative": isCumulative,
					"windows":       aggregates,
				},
			},
		}, nil
	}
}

// parseAggregateBound parses an aggregates date bound and returns the half-open period it names.
// YYYY-MM-DDTHH is accepted only for the hourly window; every other bound is a whole day, so an
// hourly bound written without an hour still spans that day rather than collapsing to hour 00.
// The two formats must not be tried in sequence with a shared err: a format that was never
// attempted leaves err nil, which previously let an unparsed bound through as the zero time.
func parseAggregateBound(value string, window string) (start time.Time, end time.Time, err error) {
	if window == "hourly" && len(value) >= 13 {
		start, err = time.Parse("2006-01-02T15", value)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		return start, start.Add(time.Hour), nil
	}

	start, err = time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, start.AddDate(0, 0, 1), nil
}

func invalidBoundMessage(param string, window string) string {
	if window == "hourly" {
		return fmt.Sprintf("invalid %s format. Use YYYY-MM-DD or YYYY-MM-DDTHH format for hourly aggregates", param)
	}
	return fmt.Sprintf("invalid %s format. Use YYYY-MM-DD format", param)
}

func (s *TimeseriesService) handleHistoricalAggregates(processedTsDB *processed_ts_db.ProcessedTsDB, nodeID, dataKey, dataType, window, date string) (interface{}, error) {
	if window == "" {
		return nil, rmerror.NewRMError(nil, "window parameter is required for historical aggregates")
	}

	parsedDate, _, err := parseAggregateBound(date, window)
	if err != nil {
		return nil, rmerror.NewRMError(err, invalidBoundMessage("date", window))
	}

	// Validate window type
	var windowType timewindow.TimeWindow
	switch window {
	case "hourly":
		windowType = timewindow.WindowHourly
	case "daily":
		windowType = timewindow.WindowDaily
	case "weekly":
		windowType = timewindow.WindowWeekly
	case "monthly":
		windowType = timewindow.WindowMonthly
	default:
		return nil, rmerror.NewRMError(nil, "invalid window type. Must be one of: hourly, daily, weekly, monthly")
	}

	// For historical aggregates, we need to query the window entries
	// Calculate start and end times for the specific date and window
	var startTime, endTime time.Time
	switch windowType {
	case timewindow.WindowDaily:
		startTime = time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, parsedDate.Location())
		endTime = startTime.AddDate(0, 0, 1)
	case timewindow.WindowWeekly:
		// Use configured week start
		startTime, endTime = timewindow.GetWindowBoundariesWithWeekStart(parsedDate, timewindow.WindowWeekly, GetWeekStartWeekday())
	case timewindow.WindowMonthly:
		startTime = time.Date(parsedDate.Year(), parsedDate.Month(), 1, 0, 0, 0, 0, parsedDate.Location())
		endTime = startTime.AddDate(0, 1, 0)
	case timewindow.WindowHourly:
		startTime = time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), parsedDate.Hour(), 0, 0, 0, parsedDate.Location())
		endTime = startTime.Add(time.Hour)
	}

	// Get window entries for the specified time range
	entries, err := processedTsDB.GetWindowEntries(nodeID, dataKey, dataType, windowType, startTime, endTime)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get historical aggregates")
	}

	if len(entries) == 0 {
		return map[string]interface{}{
			"aggregates": []map[string]interface{}{
				{
					"parameter":   fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
					"window_type": window,
					"date":        date,
					"message":     "No historical data available for this window",
				},
			},
		}, nil
	}

	// Return the first (should be only) entry for the specified window as array
	entry := entries[0]
	return map[string]interface{}{
		"aggregates": []map[string]interface{}{
			{
				"parameter":        fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
				"window_type":      window,
				"date":             date,
				"is_cumulative":    entry.IsCumulative,
				"count":            entry.WindowAggregates.Count,
				"sum":              entry.WindowAggregates.Sum,
				"min":              entry.WindowAggregates.Min,
				"max":              entry.WindowAggregates.Max,
				"average":          entry.WindowAggregates.Average,
				"first_value":      entry.WindowAggregates.FirstValue,
				"last_value":       entry.WindowAggregates.LastValue,
				"cumulative_value": entry.WindowAggregates.CumulativeValue,
				"window_start":     time.Unix(entry.WindowAggregates.WindowStart, 0).Format(time.RFC3339),
				"window_end":       time.Unix(entry.WindowAggregates.WindowEnd, 0).Format(time.RFC3339),
				"status":           "completed",
			},
		},
	}, nil
}

func (s *TimeseriesService) handleHistoricalAggregatesRange(processedTsDB *processed_ts_db.ProcessedTsDB, nodeID, dataKey, dataType, window, startDate, endDate string, pageSize int32, startKey string) (interface{}, error) {
	if window == "" {
		return nil, rmerror.NewRMError(nil, "window parameter is required for historical aggregates")
	}

	// start_date is inclusive; end_date contributes the exclusive end of the period it names,
	// so a date-only bound on an hourly window covers that whole day.
	var startTime time.Time
	if startDate != "" {
		var err error
		startTime, _, err = parseAggregateBound(startDate, window)
		if err != nil {
			return nil, rmerror.NewRMError(err, invalidBoundMessage("start_date", window))
		}
	}

	var endTime time.Time
	if endDate != "" {
		var err error
		_, endTime, err = parseAggregateBound(endDate, window)
		if err != nil {
			return nil, rmerror.NewRMError(err, invalidBoundMessage("end_date", window))
		}
	}

	// Validate date range
	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return nil, rmerror.NewRMError(nil, "start_date must be before or equal to end_date")
	}

	// Validate window type
	var windowType timewindow.TimeWindow
	switch window {
	case "hourly":
		windowType = timewindow.WindowHourly
	case "daily":
		windowType = timewindow.WindowDaily
	case "weekly":
		windowType = timewindow.WindowWeekly
	case "monthly":
		windowType = timewindow.WindowMonthly
	default:
		return nil, rmerror.NewRMError(nil, "invalid window type. Must be one of: hourly, daily, weekly, monthly")
	}

	// Get window entries for the specified time range with pagination
	queryResult, err := processedTsDB.GetWindowEntriesWithPagination(nodeID, dataKey, dataType, windowType, startTime, endTime, pageSize, startKey)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get historical aggregates range")
	}

	if len(queryResult.Entries) == 0 {
		response := map[string]interface{}{
			"aggregates": []map[string]interface{}{},
			"page_total": 0,
			"query_info": map[string]interface{}{
				"parameter":   fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
				"window_type": window,
				"start_date":  startDate,
				"end_date":    endDate,
				"message":     "No historical data available for the specified date range",
			},
		}
		return response, nil
	}

	// Convert entries to API response format
	aggregates := make([]map[string]interface{}, len(queryResult.Entries))
	for i, entry := range queryResult.Entries {
		// Extract date from the IntervalKey (e.g. "daily#2026-02-22", "hourly#2026-02-22T14")
		// which already stores the correct local timezone date.
		parts := strings.SplitN(entry.IntervalKey, "#", 2)
		dateStr := parts[1]

		aggregates[i] = map[string]interface{}{
			"date":             dateStr,
			"window_start":     time.Unix(entry.WindowAggregates.WindowStart, 0).Format(time.RFC3339),
			"window_end":       time.Unix(entry.WindowAggregates.WindowEnd, 0).Format(time.RFC3339),
			"is_cumulative":    entry.IsCumulative,
			"count":            entry.WindowAggregates.Count,
			"sum":              entry.WindowAggregates.Sum,
			"min":              entry.WindowAggregates.Min,
			"max":              entry.WindowAggregates.Max,
			"average":          entry.WindowAggregates.Average,
			"first_value":      entry.WindowAggregates.FirstValue,
			"last_value":       entry.WindowAggregates.LastValue,
			"cumulative_value": entry.WindowAggregates.CumulativeValue,
			"status":           "completed",
		}
	}

	response := map[string]interface{}{
		"aggregates": aggregates,
		"page_total": len(aggregates),
		"query_info": map[string]interface{}{
			"parameter":   fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
			"window_type": window,
			"start_date":  startDate,
			"end_date":    endDate,
		},
	}

	if queryResult.NextToken != "" {
		response["next_key"] = queryResult.NextToken
	}

	return response, nil
}

func (s *TimeseriesService) handleRawDataRequest(tsDB *timeseries_db.TimeseriesDB, nodeID, dataKey, dataType, startTimeStr, endTimeStr string, pageSize int32, startKey string) (interface{}, error) {
	startTime, err := timeutil.NormalizeTimestampMs(startTimeStr, "start_time")
	if err != nil {
		return nil, err
	}

	endTime, err := timeutil.NormalizeTimestampMs(endTimeStr, "end_time")
	if err != nil {
		return nil, err
	}

	// Handle general data request with pagination including time filters
	queryResult, err := tsDB.GetTimeseriesDataWithPagination(nodeID, dataKey, dataType, startTime, endTime, pageSize, startKey)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get timeseries data")
	}

	// Convert to response format
	dataPoints := make([]map[string]interface{}, len(queryResult.Entries))
	for i, entry := range queryResult.Entries {
		dataPoints[i] = map[string]interface{}{
			"key":        dataKey,
			"dt":         dataType,
			"ts":         entry.Timestamp,
			"value":      entry.Value,
			"tz":         entry.Timezone,
			"cumulative": entry.Cumulative,
		}
	}

	response := map[string]interface{}{
		"data":       dataPoints,
		"page_total": len(dataPoints),
	}

	if queryResult.NextToken != "" {
		response["next_key"] = queryResult.NextToken
	}

	return response, nil
}

// Put is not supported for timeseries data (data is ingested via IoT rules)
func (s *TimeseriesService) Put(rmngCtx *rmngctx.RmngContext, nodeID string, data interface{}) error {
	return rmerror.NewRMError(nil, "PUT operation not supported for timeseries data - use IoT topic for data ingestion")
}

// Delete removes all timeseries data for a node. It reads the node's config
// to discover parameter names and data types, then deletes all raw and processed data.
func (s *TimeseriesService) Delete(rmngCtx *rmngctx.RmngContext, nodeID string) error {
	// The config names the parameters to purge, so failing to read it means we cannot know what to
	// delete. That must surface: the error used to be constructed and dropped, leaving params empty
	// and the purge reporting success while deleting nothing.
	configService := config.NewConfigService()
	configData, err := configService.Get(rmngCtx, nodeID)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get node config, cannot determine which timeseries data to delete")
	}
	if configData == nil {
		return rmerror.NewRMError(nil, "node has no config, cannot determine which timeseries data to delete")
	}

	// The config names the parameters to purge, so a config we cannot decode must stop the purge
	// rather than narrow it. Dropping this error left params empty and the purge reported success
	// having deleted nothing — the same outcome as having no timeseries parameters at all.
	nodeCfg, err := config.ToNodeCfg(configData)
	if err != nil {
		return rmerror.NewRMError(err, "cannot determine which timeseries data to delete")
	}
	params := nodeCfg.GetTimeSeriesParams()
	if len(params) == 0 {
		return nil
	}

	tsDB := timeseries_db.NewTimeseriesDB(rmngCtx)
	return tsDB.DeleteAllTimeseriesForNode(nodeID, params)
}

// GetLatest retrieves the latest timeseries data point for a parameter
func (s *TimeseriesService) GetLatest(rmngCtx *rmngctx.RmngContext, nodeID string, dataKey string, dataType string) (*TimeseriesDataPoint, error) {
	// Validate permissions
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// Create database connection
	tsDB := timeseries_db.NewTimeseriesDB(rmngCtx)

	// Query latest data
	entry, err := tsDB.GetLatestTimeseriesData(nodeID, dataKey, dataType)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get latest timeseries data")
	}

	return &TimeseriesDataPoint{
		Timestamp:  entry.Timestamp,
		Value:      entry.Value,
		Timezone:   entry.Timezone,
		Cumulative: entry.Cumulative,
	}, nil
}

// GetTimeRange retrieves timeseries data for a specific time range
func (s *TimeseriesService) GetTimeRange(rmngCtx *rmngctx.RmngContext, nodeID string, dataKey string, dataType string, startTime time.Time, endTime time.Time) (*TimeseriesQueryResponse, error) {
	// Validate permissions
	if err := rmngCtx.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// Create database connection
	tsDB := timeseries_db.NewTimeseriesDB(rmngCtx)

	// Query data by time range
	entries, err := tsDB.GetTimeseriesDataByTimeRange(nodeID, dataKey, dataType, startTime, endTime)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get timeseries data by time range")
	}

	// Convert to response format
	dataPoints := make([]TimeseriesDataPoint, len(entries))
	for i, entry := range entries {
		dataPoints[i] = TimeseriesDataPoint{
			Timestamp:  entry.Timestamp,
			Value:      entry.Value,
			Timezone:   entry.Timezone,
			Cumulative: entry.Cumulative,
		}
	}

	return &TimeseriesQueryResponse{
		Data: dataPoints,
	}, nil
}
