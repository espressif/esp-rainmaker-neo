// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// Page is one page of a Query or Scan; NextToken is "" when nothing is left.
type Page[T any] struct {
	Items     []T
	NextToken string
}

// Query returns up to Limit items matching keyCond, on the table or on one of its declared indexes; Limit 0 returns every match.
func (t *Table[T]) Query(ctx context.Context, keyCond expression.KeyConditionBuilder, opts ...QueryOpts) (Page[T], error) {
	p := single("Query", opts).params()
	if p.limit < 0 {
		return Page[T]{}, t.wrap("query", negativeLimit(p.limit))
	}
	keyNames, err := t.keyNames(p.index)
	if err != nil {
		return Page[T]{}, err
	}
	in, err := t.queryInput(keyCond, p, keyNames)
	if err != nil {
		return Page[T]{}, t.wrap("query", err)
	}
	return t.readPage(ctx, "query", p.limit, p.filter.IsSet(), keyNames, func(ctx context.Context, limit *int32, start Key) ([]map[string]types.AttributeValue, Key, error) {
		in.Limit = limit
		if start != nil {
			in.ExclusiveStartKey = start
		}
		out, err := t.client.Query(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return out.Items, out.LastEvaluatedKey, nil
	})
}

// Scan returns up to Limit items of the base table that pass Filter, the same way Query pages; Limit 0 returns every match.
func (t *Table[T]) Scan(ctx context.Context, opts ...ScanOpts) (Page[T], error) {
	p := single("Scan", opts).params()
	if p.limit < 0 {
		return Page[T]{}, t.wrap("scan", negativeLimit(p.limit))
	}
	keyNames := t.baseKeyNames()
	in, err := t.scanInput(p, keyNames)
	if err != nil {
		return Page[T]{}, t.wrap("scan", err)
	}
	return t.readPage(ctx, "scan", p.limit, p.filter.IsSet(), keyNames, func(ctx context.Context, limit *int32, start Key) ([]map[string]types.AttributeValue, Key, error) {
		in.Limit = limit
		if start != nil {
			in.ExclusiveStartKey = start
		}
		out, err := t.client.Scan(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		return out.Items, out.LastEvaluatedKey, nil
	})
}

// fetchPage makes one service call: limit is nil when the call must not be capped, start is nil on the first call.
type fetchPage func(ctx context.Context, limit *int32, start Key) (rows []map[string]types.AttributeValue, next Key, err error)

// readPage gathers up to limit items over as many service calls as it takes. DynamoDB applies Limit before the filter, so with a filter it reads full pages and trims the overshoot, resuming the next page right after the last item returned.
func (t *Table[T]) readPage(ctx context.Context, op string, limit int, filtered bool, keyNames []string, fetch fetchPage) (Page[T], error) {
	var page Page[T]
	var start Key
	for {
		remaining := limit - len(page.Items)
		var callLimit *int32
		if limit > 0 && !filtered {
			callLimit = aws.Int32(int32(min(remaining, math.MaxInt32))) //nolint:gosec // G115: min clamps it to the int32 range
		}
		rows, next, err := fetch(ctx, callLimit, start)
		if err != nil {
			return Page[T]{}, t.wrap(op, err)
		}
		if limit > 0 && len(rows) > remaining {
			rows = rows[:remaining]
			if next, err = pickKey(rows[remaining-1], keyNames); err != nil {
				return Page[T]{}, t.wrap(op, err)
			}
		}
		items, err := decode[T](rows)
		if err != nil {
			return Page[T]{}, t.wrap(op, err)
		}
		page.Items = append(page.Items, items...)

		if len(next) == 0 {
			return page, nil
		}
		if limit > 0 && len(page.Items) == limit {
			page.NextToken, err = EncodeToken(next)
			return page, t.wrap(op, err)
		}
		start = next
	}
}

// QueryEach calls fn on every item matching keyCond, one page in memory at a time. An error from fn stops the walk and is returned as is.
func (t *Table[T]) QueryEach(ctx context.Context, keyCond expression.KeyConditionBuilder, fn func(T) error, opts ...QueryEachOpts) error {
	return t.eachRow(ctx, "query", keyCond, single("QueryEach", opts).params(), func(row map[string]types.AttributeValue) error {
		var item T
		if err := attributevalue.UnmarshalMap(row, &item); err != nil {
			return t.wrap("query", err)
		}
		return fn(item)
	})
}

// QueryCount returns how many items match keyCond across every page.
func (t *Table[T]) QueryCount(ctx context.Context, keyCond expression.KeyConditionBuilder, opts ...QueryCountOpts) (int, error) {
	p := single("QueryCount", opts).params()
	keyNames, err := t.keyNames(p.index)
	if err != nil {
		return 0, err
	}
	in, err := t.queryInput(keyCond, p, keyNames)
	if err != nil {
		return 0, t.wrap("query count", err)
	}
	in.Select = types.SelectCount
	total := 0
	pages := dynamodb.NewQueryPaginator(t.client, in)
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			return 0, t.wrap("query count", err)
		}
		total += int(out.Count)
	}
	return total, nil
}

// QueryDelete deletes every item keyCond matches, one page at a time, so it never holds more than a page of keys. Not atomic: on error, items already deleted stay deleted.
func (t *Table[T]) QueryDelete(ctx context.Context, keyCond expression.KeyConditionBuilder, opts ...QueryDeleteOpts) error {
	p := single("QueryDelete", opts).params()
	baseKeys := t.baseKeyNames()
	p.project = baseKeys
	return t.eachPage(ctx, "query delete", keyCond, p, func(rows []map[string]types.AttributeValue) error {
		keys := make([]Key, 0, len(rows))
		for _, row := range rows {
			key, err := pickKey(row, baseKeys)
			if err != nil {
				return t.wrap("query delete", err)
			}
			keys = append(keys, key)
		}
		return t.BatchDelete(ctx, keys)
	})
}

// ScanEach calls fn on every item of the base table that passes Filter, one page in memory at a time. An error from fn stops the walk and is returned as is.
func (t *Table[T]) ScanEach(ctx context.Context, fn func(T) error, opts ...ScanEachOpts) error {
	in, err := t.scanInput(single("ScanEach", opts).params(), t.baseKeyNames())
	if err != nil {
		return t.wrap("scan", err)
	}
	pages := dynamodb.NewScanPaginator(t.client, in)
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			return t.wrap("scan", err)
		}
		for _, row := range out.Items {
			var item T
			if err := attributevalue.UnmarshalMap(row, &item); err != nil {
				return t.wrap("scan", err)
			}
			if err := fn(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Table[T]) eachRow(ctx context.Context, op string, keyCond expression.KeyConditionBuilder, p readParams, fn func(map[string]types.AttributeValue) error) error {
	return t.eachPage(ctx, op, keyCond, p, func(rows []map[string]types.AttributeValue) error {
		for _, row := range rows {
			if err := fn(row); err != nil {
				return err
			}
		}
		return nil
	})
}

// DynamoDB resumes a query after its last returned key even when that item is gone, so fn may delete the rows it is given.
func (t *Table[T]) eachPage(ctx context.Context, op string, keyCond expression.KeyConditionBuilder, p readParams, fn func([]map[string]types.AttributeValue) error) error {
	keyNames, err := t.keyNames(p.index)
	if err != nil {
		return err
	}
	in, err := t.queryInput(keyCond, p, keyNames)
	if err != nil {
		return t.wrap(op, err)
	}
	pages := dynamodb.NewQueryPaginator(t.client, in)
	for pages.HasMorePages() {
		out, err := pages.NextPage(ctx)
		if err != nil {
			return t.wrap(op, err)
		}
		if err := fn(out.Items); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table[T]) queryInput(keyCond expression.KeyConditionBuilder, p readParams, keyNames []string) (*dynamodb.QueryInput, error) {
	var project []string
	if len(p.project) > 0 {
		project = withKeys(p.project, keyNames)
	}
	expr, err := exprParts{keyCond: &keyCond, filter: p.filter, project: project}.build()
	if err != nil {
		return nil, err
	}
	in := &dynamodb.QueryInput{
		TableName:                 aws.String(t.schema.Name),
		KeyConditionExpression:    expr.KeyCondition(),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	if p.index != "" {
		in.IndexName = aws.String(p.index)
	}
	if p.descending {
		in.ScanIndexForward = aws.Bool(false)
	}
	if p.consistent {
		in.ConsistentRead = aws.Bool(true)
	}
	if in.ExclusiveStartKey, err = startKey(p.startToken, keyNames); err != nil {
		return nil, err
	}
	return in, nil
}

func (t *Table[T]) scanInput(p readParams, keyNames []string) (*dynamodb.ScanInput, error) {
	var project []string
	if len(p.project) > 0 {
		project = withKeys(p.project, keyNames)
	}
	expr, err := exprParts{filter: p.filter, project: project}.build()
	if err != nil {
		return nil, err
	}
	in := &dynamodb.ScanInput{
		TableName:                 aws.String(t.schema.Name),
		FilterExpression:          expr.Filter(),
		ProjectionExpression:      expr.Projection(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	if p.consistent {
		in.ConsistentRead = aws.Bool(true)
	}
	if in.ExclusiveStartKey, err = startKey(p.startToken, keyNames); err != nil {
		return nil, err
	}
	return in, nil
}

// startKey decodes a caller's StartToken; an empty token means start from the beginning. The token must carry exactly the key attributes this read pages on, so one minted on another table or index is rejected as invalid instead of reaching DynamoDB as a validation error.
func startKey(token string, keyNames []string) (Key, error) {
	if token == "" {
		return nil, nil
	}
	key, err := DecodeToken(token)
	if err != nil {
		return nil, rmerror.NewRMError(ErrInvalidToken, fmt.Sprintf("invalid pagination token: %v", err))
	}
	if len(key) != len(keyNames) || slices.ContainsFunc(keyNames, func(n string) bool { _, ok := key[n]; return !ok }) {
		return nil, rmerror.NewRMError(ErrInvalidToken, fmt.Sprintf("invalid pagination token: it carries %d key attributes, this read pages on %v", len(key), keyNames))
	}
	return key, nil
}

func negativeLimit(limit int) error {
	return rmerror.NewRMError(nil, fmt.Sprintf("negative limit %d", limit))
}
