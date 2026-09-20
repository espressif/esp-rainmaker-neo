// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"fmt"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// QueryPaginated queries items in paginated manner and processes each item
func (db *DBUtil) QueryPaginated(ctx context.Context, queryInput *dynamodb.QueryInput, processItem func(item map[string]types.AttributeValue) error) error {
	// Query and collect items
	paginator := dynamodb.NewQueryPaginator(db, queryInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return rmerror.NewRMError(err, "failed to query items in DynamoDB")
		}

		for _, item := range page.Items {
			if err := processItem(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractKeyFromItem extracts the key attributes from an item based on the provided key attribute names
func extractKeyFromItem(item map[string]types.AttributeValue, keyAttributeNames []string) (map[string]types.AttributeValue, error) {
	key := make(map[string]types.AttributeValue)
	for _, attrName := range keyAttributeNames {
		if val, exists := item[attrName]; exists {
			key[attrName] = val
		} else {
			return nil, fmt.Errorf("key attribute %s not found in item", attrName)
		}
	}
	return key, nil
}

// QueryAndBatchDelete queries for items based on the provided input and deletes all matching entries
func (db *DBUtil) QueryAndBatchDelete(ctx context.Context, queryInput *dynamodb.QueryInput, tableName string, keyAttributeNames []string) error {
	var deleteRequests []types.WriteRequest
	err := db.QueryPaginated(ctx, queryInput, func(item map[string]types.AttributeValue) error {
		key, err := extractKeyFromItem(item, keyAttributeNames)
		if err != nil {
			return err
		}
		deleteRequest := types.WriteRequest{
			DeleteRequest: &types.DeleteRequest{Key: key},
		}
		deleteRequests = append(deleteRequests, deleteRequest)
		return nil
	})
	if err != nil {
		return err
	}
	return db.batchDeleteItems(ctx, deleteRequests, tableName)
}

// BatchPutItems writes items in batches of up to 25 and retries any
// unprocessed requests returned by DynamoDB.
func (db *DBUtil) BatchPutItems(ctx context.Context, items []map[string]types.AttributeValue, tableName string) error {
	writeRequests := make([]types.WriteRequest, 0, len(items))
	for _, item := range items {
		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{Item: item},
		})
	}

	for i := 0; i < len(writeRequests); i += 25 {
		end := i + 25
		if end > len(writeRequests) {
			end = len(writeRequests)
		}

		unprocessedItems := writeRequests[i:end]
		for len(unprocessedItems) > 0 {
			result, err := db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
				RequestItems: map[string][]types.WriteRequest{
					tableName: unprocessedItems,
				},
			})
			if err != nil {
				return rmerror.NewRMError(err, "failed to batch put items into DynamoDB")
			}
			unprocessedItems = result.UnprocessedItems[tableName]
		}
	}
	return nil
}

const (
	// DynamoDB's hard cap on requests per BatchWriteItem call.
	batchWriteChunkSize = 25
	// A throttled batch comes back as UnprocessedItems rather than an error, so retries are the
	// normal path, not an exception. Bounded so a persistently throttled table surfaces a failure
	// instead of spinning until the Lambda times out.
	batchWriteMaxAttempts = 5
	batchWriteBaseBackoff = 50 * time.Millisecond
)

// batchDeleteItems deletes items in batches of up to 25 items
func (db *DBUtil) batchDeleteItems(ctx context.Context, deleteRequests []types.WriteRequest, tableName string) error {
	for i := 0; i < len(deleteRequests); i += batchWriteChunkSize {
		end := i + batchWriteChunkSize
		if end > len(deleteRequests) {
			end = len(deleteRequests)
		}
		if err := db.batchDeleteChunk(ctx, deleteRequests[i:end], tableName); err != nil {
			return err
		}
	}
	return nil
}

// batchDeleteChunk drains one chunk, re-submitting whatever DynamoDB hands back as unprocessed.
// BatchWriteItem answers 200 with an UnprocessedItems list when it throttles part of a batch, so
// discarding the response reports a successful delete while leaving rows in place.
func (db *DBUtil) batchDeleteChunk(ctx context.Context, requests []types.WriteRequest, tableName string) error {
	pending := requests
	for attempt := 0; attempt < batchWriteMaxAttempts; attempt++ {
		if attempt > 0 {
			// Items come back unprocessed because the table is being throttled, so wait before
			// asking again rather than adding to the pressure.
			select {
			case <-ctx.Done():
				return rmerror.NewRMError(ctx.Err(), "cancelled while re-submitting unprocessed deletes")
			case <-time.After(batchWriteBaseBackoff << (attempt - 1)):
			}
		}

		out, err := db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{tableName: pending},
		})
		if err != nil {
			return rmerror.NewRMError(err, "failed to delete items from DynamoDB")
		}

		pending = out.UnprocessedItems[tableName]
		if len(pending) == 0 {
			return nil
		}
	}

	return rmerror.NewRMError(nil, fmt.Sprintf("%d of %d items in %s were not deleted: DynamoDB kept returning them as unprocessed across %d attempts", len(pending), len(requests), tableName, batchWriteMaxAttempts))
}
