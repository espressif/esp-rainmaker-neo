// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package espdynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type QueryWithLoopInput[T any] struct {
	DBHandle              *EspDB
	TableName             string
	IndexName             string
	Limit                 int64
	StartKey              map[string]types.AttributeValue
	Expr                  expression.Expression
	SortOrder             *bool
	AppendAndReInitialize func(*[]T, *[]T, map[string]types.AttributeValue, int64) (map[string]types.AttributeValue, bool)
	GetKey                func(T, ...string) map[string]types.AttributeValue // Required when limit is passed and AppendAndReInitialize is to be skipped
}

type DbQueryCountInput struct {
	TableName string
	IndexName string
	Expr      expression.Expression
	StartID   map[string]types.AttributeValue
	// ConsistentRead forces a strongly consistent count. Only valid on a
	// base-table query (IndexName empty); DynamoDB rejects it on a GSI.
	ConsistentRead bool
}

type dbBatchGetItemByExpInput struct {
	TableName string
	Expr      expression.Expression
	Keys      []map[string]types.AttributeValue
	Out       interface{}
}

type DbBatchGetItemLoopInput struct {
	TableName string
	Expr      expression.Expression
	Keys      []map[string]types.AttributeValue
	DBConn    *EspDB
}
