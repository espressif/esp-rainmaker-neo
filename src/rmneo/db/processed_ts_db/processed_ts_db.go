// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker platform.

The processed_ts_data table stores processed time series data for IoT nodes:

Table Name: processed_ts_data
Primary Key: node_key_dt (Partition Key)

Schema:
- node_key_dt (String): Partition key, format: nodeID.key.dt
- interval_key (String): Sort key, format: "current" or "hourly#2025-01-04T14"
- node_id (String): Node identifier
- key (String): Data point key
- dt (String): Data type
- tz (String): Timezone identifier (IANA name, e.g. "UTC", "Asia/Kolkata")
- hourly (Map): Hourly aggregates
- daily (Map): Daily aggregates
- weekly (Map): Weekly aggregates
- monthly (Map): Monthly aggregates
- is_cumulative (Boolean): Whether this is cumulative data
- last_update_time (Number): Unix timestamp in seconds of when the data was collected (device time, timezone-aware)
- updated_at (Number): Unix timestamp in seconds of when the processed entry was last updated (cloud time)

Example Records:
 1. Current Entry:
    {
    "node_key_dt": "node-123.temperature.current",
    "interval_key": "current",
    "node_id": "node-123",
    "key": "temperature",
    "dt": "current",
    "tz": "America/New_York",
    "hourly": {
    "count": 10,
    "sum": 100.0,
    "min": 20.0,
    "max": 30.0,
    "average": 30.0,
    "first_value": 25.0,
    "last_value": 30.0,
    "cumulative_value": 100.0,
    "window_start": 1714857600,
    "window_end": 1714944000,
    "last_data_timestamp": 1714943999
    },
    "daily": { ...
    },
    "weekly": { ...
    },
    "monthly": { ...
    },
    "is_cumulative": true,
    "last_update_time": 1714943999,
    "updated_at": 1714943999
    }

Query Patterns:
1. Get current aggregates:
  - Direct lookup by node_key_dt
  - Used to get current aggregates for a parameter

2. Get historical aggregates:
  - Query by node_key_dt and time range
  - Used to get historical aggregates for a parameter

3. Get all current aggregates:
  - Query by node_key_dt
  - Used to get all current aggregates for a parameter

4. Get window aggregates:
  - Query by node_key_dt and time range
  - Used to get window aggregates for a parameter

5. Get all window aggregates:
  - Query by node_key_dt
  - Used to get all window aggregates for a parameter

6. Get current aggregates for a specific window:
  - Query by node_key_dt and window type
  - Used to get current aggregates for a specific window

7. Get all current aggregates for a specific window:
  - Query by node_key_dt and window type
  - Used to get all current aggregates for a specific window

8. Get historical aggregates for a specific window:
  - Query by node_key_dt and time range
  - Used to get historical aggregates for a specific window

9. Get all historical aggregates:
  - Query by node_key_dt
  - Used to get all historical aggregates for a parameter

10. Get specific current aggregates for a specific window:
  - Query by node_key_dt and window type
  - Used to get specific current aggregates for a specific window

11. Get all current aggregates for a specific window:
  - Query by node_key_dt and window type
  - Used to get all current aggregates for a specific window
*/
package processed_ts_db

import (
	"fmt"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

const (
	ProcessedTSDataTable = "rmng-processed-ts-data"
)

type ProcessedTsDB struct {
	dbcore.DB
}

func NewProcessedTsDB(ctx *rmngctx.RmngContext) *ProcessedTsDB {
	return &ProcessedTsDB{DB: *dbcore.NewDB(ctx)}
}

// WindowAggregates represents aggregate statistics for a time window (database storage format)
type WindowAggregates struct {
	Count             int64   `dynamodbav:"count,omitempty"`
	Sum               float64 `dynamodbav:"sum,omitempty"`
	Min               float64 `dynamodbav:"min,omitempty"`
	Max               float64 `dynamodbav:"max,omitempty"`
	Average           float64 `dynamodbav:"average,omitempty"`
	FirstValue        float64 `dynamodbav:"first_value,omitempty"`
	LastValue         float64 `dynamodbav:"last_value,omitempty"`
	CumulativeValue   float64 `dynamodbav:"cumulative_value,omitempty"`
	WindowStart       int64   `dynamodbav:"window_start,omitempty"`        // Unix timestamp in seconds
	WindowEnd         int64   `dynamodbav:"window_end,omitempty"`          // Unix timestamp in seconds
	LastDataTimestamp int64   `dynamodbav:"last_data_timestamp,omitempty"` // Unix timestamp in seconds of the last data point
}

// ToMap converts the aggregates to a map for API responses
func (agg *WindowAggregates) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"count":            agg.Count,
		"sum":              agg.Sum,
		"min":              agg.Min,
		"max":              agg.Max,
		"average":          agg.Average,
		"first_value":      agg.FirstValue,
		"last_value":       agg.LastValue,
		"cumulative_value": agg.CumulativeValue,
		"window_start":     time.Unix(agg.WindowStart, 0).Format(time.RFC3339),
		"window_end":       time.Unix(agg.WindowEnd, 0).Format(time.RFC3339),
		"last_update":      time.Now().UTC().Format(time.RFC3339),
	}
}

// ProcessedTsEntry represents an entry in the processed_ts_data table (database storage format)
type ProcessedTsEntry struct {
	NodeKeyDt   string `dynamodbav:"node_key_dt"`  // Partition key: nodeID.key.dt
	IntervalKey string `dynamodbav:"interval_key"` // Sort key: "current" or "hourly#2025-01-04T14"
	NodeID      string `dynamodbav:"node_id"`      // Node identifier
	DataKey     string `dynamodbav:"key"`          // Within-node data point key
	DataType    string `dynamodbav:"dt"`           // Data type
	Timezone    string `dynamodbav:"tz,omitempty"` // Timezone identifier (IANA name, e.g. "UTC", "Asia/Kolkata")

	// For current entries (when IntervalKey is "current") - All window types
	HourlyAggregates  WindowAggregates `dynamodbav:"hourly,omitempty"`
	DailyAggregates   WindowAggregates `dynamodbav:"daily,omitempty"`
	WeeklyAggregates  WindowAggregates `dynamodbav:"weekly,omitempty"`
	MonthlyAggregates WindowAggregates `dynamodbav:"monthly,omitempty"`

	// For historical entries (when IntervalKey is not "current")
	WindowType       string `dynamodbav:"window_type,omitempty"` // hourly, daily, weekly, monthly
	WindowAggregates        // Embedded for historical entries

	// Common fields
	IsCumulative   bool  `dynamodbav:"is_cumulative,omitempty"`    // Whether this is cumulative data
	LastUpdateTime int64 `dynamodbav:"last_update_time,omitempty"` // Unix timestamp in seconds of when the data was collected (device time, timezone-aware)
	UpdatedAt      int64 `dynamodbav:"updated_at,omitempty"`       // Unix timestamp in seconds of when the processed entry was last updated (cloud time)
}

// GetCurrentEntry retrieves the current scratchpad entry for a parameter
func (db *ProcessedTsDB) GetCurrentEntry(nodeID, path, dataType string) (*ProcessedTsEntry, error) {
	// Access permission check
	if err := db.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node processed timeseries data")
	}

	nodeKeyDt := fmt.Sprintf("%s.%s.%s", nodeID, path, dataType)

	// Build the key
	keyMap := map[string]interface{}{
		"node_key_dt":  nodeKeyDt,
		"interval_key": "current",
	}

	// Marshal the key
	key, err := attributevalue.MarshalMap(keyMap)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to marshal key")
	}

	// Build get item input
	input := &dynamodb.GetItemInput{
		TableName: aws.String(ProcessedTSDataTable),
		Key:       key,
	}

	// Get the item
	result, err := db.GetItem(db.Ctx.Context, input)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get current processed ts entry")
	}

	if result.Item == nil {
		return nil, nil // No current entry exists
	}

	// Unmarshal the result
	var entry ProcessedTsEntry
	err = attributevalue.UnmarshalMap(result.Item, &entry)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal processed ts entry")
	}

	return &entry, nil
}

// UpsertCurrentEntry updates or inserts a current entry
func (db *ProcessedTsDB) UpsertCurrentEntry(entry *ProcessedTsEntry) error {
	// Access permission check
	if err := db.IsAuthorized(utils.NodePublishToDevice, entry.NodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to node processed timeseries data")
	}

	// Ensure it's marked as current
	entry.IntervalKey = "current"

	// Note: UpdatedAt should be set by the caller (processor) to maintain proper timestamp semantics
	// The processor knows whether this is a new entry or an update and sets timestamps appropriately

	// Marshal the entry
	av, err := attributevalue.MarshalMap(entry)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal processed ts entry")
	}

	// Put the item
	_, err = db.PutItem(db.Ctx.Context, &dynamodb.PutItemInput{
		TableName: aws.String(ProcessedTSDataTable),
		Item:      av,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to put processed ts entry")
	}

	return nil
}

// CreateWindowEntry creates a new window entry when a window boundary is crossed
func (db *ProcessedTsDB) CreateWindowEntry(entry *ProcessedTsEntry, windowKey string) error {
	// Access permission check
	if err := db.IsAuthorized(utils.NodePublishToDevice, entry.NodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to node processed timeseries data")
	}

	// Create a copy for the window entry
	windowEntry := *entry
	windowEntry.IntervalKey = windowKey
	windowEntry.UpdatedAt = time.Now().Unix()

	// Marshal the entry
	av, err := attributevalue.MarshalMap(&windowEntry)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal window entry")
	}

	// Put the item
	_, err = db.PutItem(db.Ctx.Context, &dynamodb.PutItemInput{
		TableName: aws.String(ProcessedTSDataTable),
		Item:      av,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to put window entry")
	}

	return nil
}

// GetWindowEntries retrieves window entries for a given node parameter
func (db *ProcessedTsDB) GetWindowEntries(nodeID, dataKey, dataType string, windowType timewindow.TimeWindow, startTime, endTime time.Time) ([]*ProcessedTsEntry, error) {
	// Call GetWindowEntriesWithPagination without pagination parameters
	queryResult, err := db.GetWindowEntriesWithPagination(nodeID, dataKey, dataType, windowType, startTime, endTime, 0, "")
	if err != nil {
		return nil, err
	}
	return queryResult.Entries, nil
}

// HistoricalAggregatesQueryResult represents the result of a paginated historical aggregates query
type HistoricalAggregatesQueryResult struct {
	Entries   []*ProcessedTsEntry
	NextToken string
}

// lastWindowKeyBefore returns the interval_key of the last window that starts before endTime.
// endTime is an exclusive bound, but Between is inclusive on both ends, so formatting endTime
// itself would also match the window that begins exactly at it.
func lastWindowKeyBefore(endTime time.Time, windowType timewindow.TimeWindow) string {
	return timewindow.FormatWindowKey(endTime.Add(-time.Nanosecond), windowType)
}

// GetWindowEntriesWithPagination retrieves window entries with pagination.
// startTime is inclusive and endTime is exclusive, matching Go's own half-open time ranges.
func (db *ProcessedTsDB) GetWindowEntriesWithPagination(nodeID, dataKey, dataType string, windowType timewindow.TimeWindow, startTime, endTime time.Time, limit int32, nextToken string) (*HistoricalAggregatesQueryResult, error) {
	// Access permission check
	if err := db.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node processed timeseries data")
	}

	nodeKeyDt := fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType)

	// Build key condition using interval_key (sort key) for efficient querying
	// instead of filter expressions on window_type/window_start/window_end.
	// This ensures DynamoDB only reads matching items rather than scanning all
	// items for the partition and filtering afterwards.
	keyCondition := expression.Key("node_key_dt").Equal(expression.Value(nodeKeyDt))
	keyPrefix := fmt.Sprintf("%s#", windowType)

	if !startTime.IsZero() && !endTime.IsZero() {
		startKey := timewindow.FormatWindowKey(startTime, windowType)
		endKey := lastWindowKeyBefore(endTime, windowType)
		keyCondition = keyCondition.And(expression.Key("interval_key").Between(expression.Value(startKey), expression.Value(endKey)))
	} else if !startTime.IsZero() {
		startKey := timewindow.FormatWindowKey(startTime, windowType)
		keyCondition = keyCondition.And(expression.Key("interval_key").Between(expression.Value(startKey), expression.Value(keyPrefix+"~")))
	} else if !endTime.IsZero() {
		endKey := lastWindowKeyBefore(endTime, windowType)
		keyCondition = keyCondition.And(expression.Key("interval_key").Between(expression.Value(keyPrefix), expression.Value(endKey)))
	} else {
		keyCondition = keyCondition.And(expression.Key("interval_key").BeginsWith(keyPrefix))
	}

	// Build the expression
	expr, err := expression.NewBuilder().
		WithKeyCondition(keyCondition).
		Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build query expression")
	}

	// Build the query input
	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(ProcessedTSDataTable),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ScanIndexForward:          aws.Bool(false), // Latest first
	}

	// Add pagination support
	if limit > 0 {
		queryInput.Limit = aws.Int32(limit)
	}

	if nextToken != "" {
		// Decode the next token (it's base64 encoded JSON)
		decodedToken, err := dbcore.DecodePaginationToken(nextToken)
		if err != nil {
			return nil, rmerror.NewRMError(err, "invalid pagination token")
		}
		queryInput.ExclusiveStartKey = decodedToken
	}

	// Execute the query
	result, err := db.Query(db.Ctx.Context, queryInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query processed ts data")
	}

	// Convert results
	entries := make([]*ProcessedTsEntry, len(result.Items))
	for i, item := range result.Items {
		var entry ProcessedTsEntry
		err = attributevalue.UnmarshalMap(item, &entry)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to unmarshal processed ts entry")
		}
		entries[i] = &entry
	}

	// Prepare pagination result
	queryResult := &HistoricalAggregatesQueryResult{
		Entries: entries,
	}

	// Generate next token if there are more results
	if result.LastEvaluatedKey != nil {
		nextToken, err := dbcore.EncodePaginationToken(result.LastEvaluatedKey)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to encode pagination token")
		}
		queryResult.NextToken = nextToken
	}

	return queryResult, nil
}

// GetCurrentAggregatesForWindow retrieves current aggregates for a specific window
func (db *ProcessedTsDB) GetCurrentAggregatesForWindow(nodeID, dataKey, dataType string, window timewindow.TimeWindow) (map[string]interface{}, error) {
	// Access permission check is handled by GetCurrentEntry, so no need to duplicate it here
	entry, err := db.GetCurrentEntry(nodeID, dataKey, dataType)
	if err != nil {
		return nil, err
	}

	if entry == nil {
		return map[string]interface{}{
			"window_type": string(window),
			"message":     "No current data available for this window",
		}, nil
	}

	agg := entry.GetWindowAggregates(window)
	if agg == nil {
		return map[string]interface{}{
			"window_type": string(window),
			"error":       "unsupported window type",
		}, nil
	}

	result := agg.ToMap()
	result["window_type"] = string(window)
	result["is_cumulative"] = entry.IsCumulative

	return result, nil
}

// GetAllCurrentAggregates returns current aggregates for all window types in a user-friendly format
func (db *ProcessedTsDB) GetAllCurrentAggregates(nodeID, dataKey, dataType string) (map[string]interface{}, error) {
	// Access permission check is handled by GetCurrentEntry, so no need to duplicate it here
	entry, err := db.GetCurrentEntry(nodeID, dataKey, dataType)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})

	if entry == nil {
		for _, window := range timewindow.GetSupportedWindows() {
			result[string(window)] = map[string]interface{}{
				"message": "No current data available for this window",
			}
		}
		return result, nil
	}

	for _, window := range timewindow.GetSupportedWindows() {
		windowKey := string(window)
		agg := entry.GetWindowAggregates(window)
		if agg != nil {
			windowData := agg.ToMap()
			windowData["is_cumulative"] = entry.IsCumulative
			result[windowKey] = windowData
		} else {
			result[windowKey] = map[string]interface{}{
				"error": "unsupported window type",
			}
		}
	}

	return result, nil
}

// GetWindowAggregates returns a pointer to the appropriate WindowAggregates for the given window type
func (entry *ProcessedTsEntry) GetWindowAggregates(window timewindow.TimeWindow) *WindowAggregates {
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
