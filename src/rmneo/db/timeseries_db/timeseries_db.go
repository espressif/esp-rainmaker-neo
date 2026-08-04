// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db provides database operations for the RainMaker timeseries data.

The raw_ts_data table stores timestamped values from devices:

Table Name: raw_ts_data
Primary Key: node_key_dt (Partition Key), ts (Sort Key)

Schema:
- node_key_dt (String): Partition key, format: "nodeID.key.dt"
- ts (Number): Sort key, epoch timestamp
- node_id (String): Device identifier
- topic_name (String): Topic name suffix (e.g., "ts-group456-sub1,sub2")
- key (String): Parameter name
- dt (String): Data type (int/float/bool/string)
- tz (String): Timezone identifier (IANA name, e.g. "UTC", "Asia/Kolkata") (optional)
- value (Mixed): The actual value
- cumulative (Boolean): Whether value is cumulative (optional)

Example Records:
1. Temperature sensor:
   {
     "node_key_dt": "device123.temperature.float",
     "ts": 1640995200,
     "node_id": "device123",
     "topic_name": "ts-group456",
     "key": "temperature",
     "dt": "float",
     "tz": "UTC",
     "value": 25.5,
     "cumulative": false
   }

2. Energy meter:
   {
     "node_key_dt": "device456.energy.float",
     "ts": 1640995260,
     "node_id": "device456",
     "topic_name": "ts-group456-sub1",
     "key": "energy",
     "dt": "float",
     "value": 1500.25,
     "cumulative": true
   }

Query Patterns:
1. Get timeseries data for a specific parameter:
   - Query by node_key_dt partition key
   - Use ts sort key for time range queries

2. Get data for a time range:
   - Query by node_key_dt with ts range conditions
   - Used for analytics and visualization

Access Control:
- Users can only access data for nodes they have permission to view
- Validated through node ownership or group membership
*/

package timeseries_db

import (
	"fmt"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	RawTSDataTable = "rmng-raw-ts-data"
)

type TimeseriesDB struct {
	dbcore.DB
}

func NewTimeseriesDB(ctx *rmngctx.RmngContext) *TimeseriesDB {
	return &TimeseriesDB{DB: *dbcore.NewDB(ctx)}
}

type TimeseriesEntry struct {
	NodeKeyDt  string      `dynamodbav:"node_key_dt"`
	Timestamp  int64       `dynamodbav:"ts"` // In milliseconds
	NodeID     string      `dynamodbav:"node_id"`
	TopicName  string      `dynamodbav:"topic_name"`
	DataKey    string      `dynamodbav:"key"`
	DataType   string      `dynamodbav:"dt"`
	Timezone   string      `dynamodbav:"tz,omitempty"`
	Value      interface{} `dynamodbav:"value"`
	Cumulative bool        `dynamodbav:"cumulative,omitempty"`
}

// TimeseriesQueryResult represents the result of a paginated timeseries query
type TimeseriesQueryResult struct {
	Entries   []*TimeseriesEntry
	NextToken string
}

// GetTimeseriesDataWithPagination retrieves timeseries data for a specific parameter within a time range with pagination support
// This is the main function that handles all timeseries queries
func (db *TimeseriesDB) GetTimeseriesDataWithPagination(nodeID string, dataKey string, dataType string, startTime int64, endTime int64, limit int32, nextToken string) (*TimeseriesQueryResult, error) {
	// Access permission check
	if err := db.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// Build the partition key
	nodeKeyDt := fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType)

	// Handle pagination token first to extract timestamp if needed
	var paginationTimestamp int64 = 0
	if nextToken != "" {
		// Decode the next token (it's base64 encoded JSON)
		decodedToken, err := dbcore.DecodePaginationToken(nextToken)
		if err != nil {
			return nil, rmerror.NewRMError(err, "invalid pagination token")
		}

		// Extract timestamp from the decoded token for range-based pagination
		if timestampAttr, ok := decodedToken["ts"]; ok {
			if timestampN, ok := timestampAttr.(*types.AttributeValueMemberN); ok {
				if ts, err := strconv.ParseInt(timestampN.Value, 10, 64); err == nil {
					paginationTimestamp = ts
				}
			}
		}
	}

	// Build key condition expression using expression builder
	var keyCondition expression.KeyConditionBuilder
	keyCondition = expression.Key("node_key_dt").Equal(expression.Value(nodeKeyDt))

	// Determine effective time bounds
	var effectiveEndTime int64
	hasEndTime := false

	if endTime > 0 || paginationTimestamp > 0 {
		hasEndTime = true
		if endTime > 0 && paginationTimestamp > 0 {
			effectiveEndTime = min(endTime, paginationTimestamp-1)
		} else if endTime > 0 {
			effectiveEndTime = endTime
		} else {
			effectiveEndTime = paginationTimestamp - 1
		}
	}

	// Apply time range filters
	if startTime > 0 && hasEndTime {
		keyCondition = keyCondition.And(expression.Key("ts").Between(expression.Value(startTime), expression.Value(effectiveEndTime)))
	} else if startTime > 0 {
		keyCondition = keyCondition.And(expression.Key("ts").GreaterThanEqual(expression.Value(startTime)))
	} else if hasEndTime {
		keyCondition = keyCondition.And(expression.Key("ts").LessThanEqual(expression.Value(effectiveEndTime)))
	}

	// Build expression
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build query expression")
	}

	// Build the query input
	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(RawTSDataTable),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ScanIndexForward:          aws.Bool(false), // Return latest data first
	}

	// Add limit if specified
	if limit > 0 {
		queryInput.Limit = aws.Int32(limit)
	}

	// Note: We're using timestamp-based pagination instead of ExclusiveStartKey
	// to avoid potential issues with DynamoDB pagination in the real AWS environment

	// Execute the query
	result, err := db.Query(db.Ctx.Context, queryInput)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query timeseries data")
	}

	// Convert results to TimeseriesEntry structs
	var entries []*TimeseriesEntry
	err = attributevalue.UnmarshalListOfMaps(result.Items, &entries)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal timeseries entries")
	}

	// Prepare pagination result
	queryResult := &TimeseriesQueryResult{
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

// GetTimeseriesData retrieves timeseries data for a specific parameter within a time range
// This function calls GetTimeseriesDataWithPagination without pagination to maintain backward compatibility
func (db *TimeseriesDB) GetTimeseriesData(nodeID string, dataKey string, dataType string, startTime int64, endTime int64, limit int32) ([]*TimeseriesEntry, error) {
	queryResult, err := db.GetTimeseriesDataWithPagination(nodeID, dataKey, dataType, startTime, endTime, limit, "")
	if err != nil {
		return nil, err
	}
	return queryResult.Entries, nil
}

// GetLatestTimeseriesData retrieves the most recent timeseries data for a specific parameter
func (db *TimeseriesDB) GetLatestTimeseriesData(nodeID string, dataKey string, dataType string) (*TimeseriesEntry, error) {
	entries, err := db.GetTimeseriesData(nodeID, dataKey, dataType, 0, 0, 1)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, rmerror.NewRMError(nil, "no timeseries data found")
	}

	return entries[0], nil
}

// GetTimeseriesDataByTimeRange retrieves timeseries data for a specific parameter within a time range
func (db *TimeseriesDB) GetTimeseriesDataByTimeRange(nodeID string, dataKey string, dataType string, startTime time.Time, endTime time.Time) ([]*TimeseriesEntry, error) {
	return db.GetTimeseriesData(nodeID, dataKey, dataType, startTime.Unix(), endTime.Unix(), 0)
}

// GetParameterList retrieves all parameters for a given node that have timeseries data
func (db *TimeseriesDB) GetParameterList(nodeID string) ([]string, error) {
	// Access permission check
	if err := db.IsAuthorized(utils.NodeGet, nodeID); err != nil {
		return nil, rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// This would require a GSI on node_id to be efficient
	// For now, we'll return an error indicating this operation is not supported
	return nil, rmerror.NewRMError(nil, "parameter list query not supported without GSI on node_id")
}

// PutTimeseriesData stores a single timeseries data point
// Note: This is typically done via IoT rules, but provided for completeness
func (db *TimeseriesDB) PutTimeseriesData(entry *TimeseriesEntry) error {
	// Access permission check
	if err := db.IsAuthorized(utils.NodePutConfig, entry.NodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	// Build the partition key
	entry.NodeKeyDt = fmt.Sprintf("%s.%s.%s", entry.NodeID, entry.DataKey, entry.DataType)

	// Marshal the entry to DynamoDB format
	av, err := attributevalue.MarshalMap(entry)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal timeseries entry")
	}

	// Put the item in DynamoDB
	_, err = db.PutItem(db.Ctx.Context, &dynamodb.PutItemInput{
		TableName: aws.String(RawTSDataTable),
		Item:      av,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to put timeseries data")
	}

	return nil
}

// UnMarshalToTimeseriesEntry unmarshals a DynamoDB stream record to a TimeseriesEntry
func (db *TimeseriesDB) UnMarshalToTimeseriesEntry(image map[string]events.DynamoDBAttributeValue) (*TimeseriesEntry, error) {
	// Convert DynamoDB attribute values to standard types
	standardMap := make(map[string]types.AttributeValue)

	for k, v := range image {
		dataType := v.DataType()
		switch dataType {
		case events.DataTypeString:
			if strVal := v.String(); strVal != "" {
				standardMap[k] = &types.AttributeValueMemberS{Value: strVal}
			}
		case events.DataTypeNumber:
			if numVal := v.Number(); numVal != "" {
				standardMap[k] = &types.AttributeValueMemberN{Value: numVal}
			}
		case events.DataTypeBoolean:
			standardMap[k] = &types.AttributeValueMemberBOOL{Value: v.Boolean()}
		case events.DataTypeNull:
			standardMap[k] = &types.AttributeValueMemberNULL{Value: true}
		case events.DataTypeBinary:
			if binVal := v.Binary(); len(binVal) > 0 {
				standardMap[k] = &types.AttributeValueMemberB{Value: binVal}
			}
		}
	}

	// Unmarshal to TimeseriesEntry
	var entry TimeseriesEntry
	err := attributevalue.UnmarshalMap(standardMap, &entry)
	if err != nil {
		return nil, err
	}

	// Validate required fields
	if entry.NodeID == "" {
		return nil, fmt.Errorf("missing required field: node_id")
	}
	if entry.Timestamp == 0 {
		return nil, fmt.Errorf("missing required field: timestamp")
	}
	if entry.DataKey == "" {
		return nil, fmt.Errorf("missing required field: key")
	}
	if entry.Value == nil {
		return nil, fmt.Errorf("missing required field: value")
	}

	return &entry, nil
}

// DeleteTimeseriesForParam deletes all timeseries data for a specific node parameter from the given table.
// tableName should be "raw_ts_data" or "processed_ts_data".
func (db *TimeseriesDB) DeleteTimeseriesForParam(tableName, nodeID, dataKey, dataType string) error {
	// Access permission check
	if err := db.IsAuthorized(utils.NodeDeleteConfig, nodeID); err != nil {
		return rmerror.NewRMError(err, "unauthorized access to node timeseries data")
	}

	nodeKeyDt := fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType)

	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("node_key_dt = :npd"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":npd": &types.AttributeValueMemberS{Value: nodeKeyDt},
		},
	}

	return db.QueryAndBatchDelete(db.Ctx.Context, queryInput, tableName, []string{"node_key_dt", "ts"})
}

// DeleteAllTimeseriesForNode deletes all timeseries data (raw and processed) for a node
// given its parameter list (each param has Name and DataType).
func (db *TimeseriesDB) DeleteAllTimeseriesForNode(nodeID string, params []ParamKey) error {
	for _, dataKey := range params {
		if err := db.DeleteTimeseriesForParam(RawTSDataTable, nodeID, dataKey.Name, dataKey.DataType); err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to delete raw timeseries for %s.%s", dataKey.Name, dataKey.DataType))
		}
		if err := db.DeleteTimeseriesForParam(processed_ts_db.ProcessedTSDataTable, nodeID, dataKey.Name, dataKey.DataType); err != nil {
			return rmerror.NewRMError(err, fmt.Sprintf("failed to delete processed timeseries for %s.%s", dataKey.Name, dataKey.DataType))
		}
	}
	return nil
}

// ParamKey identifies a node parameter by name and data type.
type ParamKey struct {
	Name     string
	DataType string
}
