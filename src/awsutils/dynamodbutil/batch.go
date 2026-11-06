// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
)

const (
	batchGetLimit    = 100 // DynamoDB's cap per BatchGetItem
	batchWriteLimit  = 25  // DynamoDB's cap per BatchWriteItem
	batchConcurrency = 4
)

// The SDK client's own backoff, so unprocessed items wait as long as a throttled call would. A var so tests can shorten it.
var backoffDelay = retry.NewExponentialJitterBackoff(awscommon.SDK_MAX_BACKOFF).BackoffDelay

// UnprocessedError reports batch items DynamoDB still refused after every retry.
type UnprocessedError struct {
	Table     string
	Remaining int
}

func (e *UnprocessedError) Error() string {
	return fmt.Sprintf("dynamodbutil: %d items in %s still unprocessed after %d attempts", e.Remaining, e.Table, awscommon.SDK_MAX_ATTEMPTS)
}

// BatchGet reads the items for keys; keys with no item are omitted and the order is not guaranteed.
func (t *Table[T]) BatchGet(ctx context.Context, keys []Key, opts ...ReadOpts) ([]T, error) {
	o := single("BatchGet", opts)
	var req types.KeysAndAttributes
	if o.Consistent {
		req.ConsistentRead = aws.Bool(true)
	}
	expr, err := exprParts{project: o.Project}.build()
	if err != nil {
		return nil, t.wrap("batch get", err)
	}
	req.ProjectionExpression = expr.Projection()
	req.ExpressionAttributeNames = expr.Names()

	keys, err = uniqueKeys(keys)
	if err != nil {
		return nil, t.wrap("batch get", err)
	}
	chunks := slices.Collect(slices.Chunk(keys, batchGetLimit))
	results, err := parallel.ProcessParallelErr(ctx, chunks, func(ctx context.Context, chunk []Key) ([]T, error) {
		rows, err := t.getChunk(ctx, req, chunk)
		if err != nil {
			return nil, err
		}
		items, err := decode[T](rows)
		return items, t.wrap("batch get", err)
	}, parallel.ParallelOptions{MaxRoutines: batchConcurrency})
	if err != nil {
		return nil, err
	}
	return slices.Concat(results...), nil
}

// BatchPut writes items whole; of two items with the same key the later one wins, as sequential puts would. Not atomic: on error, chunks already written stay written.
func (t *Table[T]) BatchPut(ctx context.Context, items []T, opts ...BatchWriteOpts) error {
	avs := make([]map[string]types.AttributeValue, 0, len(items))
	for _, item := range items {
		av, err := attributevalue.MarshalMap(item)
		if err != nil {
			return t.wrap("batch put", err)
		}
		avs = append(avs, av)
	}
	keyNames := t.baseKeyNames()
	avs, err := dedupeByKey(avs, func(av map[string]types.AttributeValue) (Key, error) { return pickKey(av, keyNames) })
	if err != nil {
		return t.wrap("batch put", err)
	}
	reqs := make([]types.WriteRequest, 0, len(avs))
	for _, av := range avs {
		reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: av}})
	}
	return t.writeAll(ctx, reqs, single("BatchPut", opts))
}

// BatchDelete deletes the items for keys; missing keys are not an error. Not atomic.
func (t *Table[T]) BatchDelete(ctx context.Context, keys []Key, opts ...BatchWriteOpts) error {
	keys, err := uniqueKeys(keys)
	if err != nil {
		return t.wrap("batch delete", err)
	}
	reqs := make([]types.WriteRequest, 0, len(keys))
	for _, key := range keys {
		reqs = append(reqs, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key}})
	}
	return t.writeAll(ctx, reqs, single("BatchDelete", opts))
}

func (t *Table[T]) writeAll(ctx context.Context, reqs []types.WriteRequest, o BatchWriteOpts) error {
	var limit uint = batchConcurrency
	if o.Sequential {
		limit = 1
	}
	chunks := slices.Collect(slices.Chunk(reqs, batchWriteLimit))
	_, err := parallel.ProcessParallelErr(ctx, chunks, func(ctx context.Context, chunk []types.WriteRequest) (struct{}, error) {
		return struct{}{}, t.writeChunk(ctx, chunk)
	}, parallel.ParallelOptions{MaxRoutines: limit})
	return err
}

// A throttled batch answers 200 with the refused part as unprocessed, so retrying is the normal path, not an error path.
func (t *Table[T]) writeChunk(ctx context.Context, pending []types.WriteRequest) error {
	for attempt := range awscommon.SDK_MAX_ATTEMPTS {
		if err := backoff(ctx, attempt); err != nil {
			return t.wrap("batch write", err)
		}
		out, err := t.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{t.schema.Name: pending},
		})
		if err != nil {
			return t.wrap("batch write", err)
		}
		pending = out.UnprocessedItems[t.schema.Name]
		if len(pending) == 0 {
			return nil
		}
	}
	return &UnprocessedError{Table: t.schema.Name, Remaining: len(pending)}
}

func (t *Table[T]) getChunk(ctx context.Context, req types.KeysAndAttributes, pending []Key) ([]map[string]types.AttributeValue, error) {
	var rows []map[string]types.AttributeValue
	for attempt := range awscommon.SDK_MAX_ATTEMPTS {
		if err := backoff(ctx, attempt); err != nil {
			return nil, t.wrap("batch get", err)
		}
		req.Keys = pending
		out, err := t.client.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{
			RequestItems: map[string]types.KeysAndAttributes{t.schema.Name: req},
		})
		if err != nil {
			return nil, t.wrap("batch get", err)
		}
		rows = append(rows, out.Responses[t.schema.Name]...)
		pending = out.UnprocessedKeys[t.schema.Name].Keys
		if len(pending) == 0 {
			return rows, nil
		}
	}
	return nil, &UnprocessedError{Table: t.schema.Name, Remaining: len(pending)}
}

// dedupeByKey keeps one element per key, as DynamoDB rejects a key repeated within one batch: the last one, in the position of the first. [a1, b, a2] -> [a2, b].
func dedupeByKey[E any](elems []E, keyOf func(E) (Key, error)) ([]E, error) {
	slot := make(map[string]int, len(elems))
	out := make([]E, 0, len(elems))
	for _, e := range elems {
		key, err := keyOf(e)
		if err != nil {
			return nil, err
		}
		id, err := EncodeToken(key)
		if err != nil {
			return nil, err
		}
		if i, seen := slot[id]; seen {
			out[i] = e
			continue
		}
		slot[id] = len(out)
		out = append(out, e)
	}
	return out, nil
}

func uniqueKeys(keys []Key) ([]Key, error) {
	return dedupeByKey(keys, func(k Key) (Key, error) { return k, nil })
}

// backoff waits before retry attempt n: nothing for n=0, then a random wait up to 2s, then up to 4s, capped at SDK_MAX_BACKOFF. Returns early when ctx ends.
func backoff(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}
	d, err := backoffDelay(attempt, nil)
	if err != nil {
		return err
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
