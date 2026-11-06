// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

const transactLimit = 100 // DynamoDB's cap per TransactWriteItems

// TxOp is one write in a TransactWrite. Build it with a table's CreateOp, PutOp, UpdateOp, DeleteOp or CheckOp; a build error surfaces from TransactWrite.
type TxOp struct {
	item types.TransactWriteItem
	err  error
}

// TransactWrite applies every op or none. When a condition fails, IsConditionFailed(err) is true and *types.TransactionCanceledException says which op failed.
func TransactWrite(ctx context.Context, client awscommon.DynamoDBClientInterface, ops ...TxOp) error {
	if len(ops) == 0 {
		return nil
	}
	if len(ops) > transactLimit {
		return rmerror.NewRMError(nil, fmt.Sprintf("dynamodbutil: transaction has %d ops; DynamoDB allows %d", len(ops), transactLimit))
	}
	items := make([]types.TransactWriteItem, 0, len(ops))
	for _, op := range ops {
		if op.err != nil {
			return op.err
		}
		items = append(items, op.item)
	}
	if _, err := client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items}); err != nil {
		return rmerror.NewRMError(err, "dynamodbutil: transact write")
	}
	return nil
}

// CreateOp is Create inside a transaction.
func (t *Table[T]) CreateOp(item T, opts ...TxOpts) TxOp {
	return t.putOp(item, t.createCondition(single("CreateOp", opts).If))
}

// PutOp is Put inside a transaction.
func (t *Table[T]) PutOp(item T, opts ...TxOpts) TxOp {
	return t.putOp(item, single("PutOp", opts).If)
}

func (t *Table[T]) putOp(item T, cond expression.ConditionBuilder) TxOp {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return TxOp{err: t.wrap("transact put", err)}
	}
	expr, err := exprParts{cond: cond}.build()
	if err != nil {
		return TxOp{err: t.wrap("transact put", err)}
	}
	return TxOp{item: types.TransactWriteItem{Put: &types.Put{
		TableName:                 aws.String(t.schema.Name),
		Item:                      av,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}}
}

// UpdateOp is Update inside a transaction.
func (t *Table[T]) UpdateOp(key Key, update expression.UpdateBuilder, opts ...TxOpts) TxOp {
	expr, err := exprParts{cond: t.existingCondition(single("UpdateOp", opts).If), update: &update}.build()
	if err != nil {
		return TxOp{err: t.wrap("transact update", err)}
	}
	return TxOp{item: types.TransactWriteItem{Update: &types.Update{
		TableName:                 aws.String(t.schema.Name),
		Key:                       key,
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}}
}

// DeleteOp is Delete inside a transaction.
func (t *Table[T]) DeleteOp(key Key, opts ...TxOpts) TxOp {
	expr, err := exprParts{cond: t.existingCondition(single("DeleteOp", opts).If)}.build()
	if err != nil {
		return TxOp{err: t.wrap("transact delete", err)}
	}
	return TxOp{item: types.TransactWriteItem{Delete: &types.Delete{
		TableName:                 aws.String(t.schema.Name),
		Key:                       key,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}}
}

// CheckOp makes the transaction depend on cond holding for the item at key, without writing it.
func (t *Table[T]) CheckOp(key Key, cond expression.ConditionBuilder) TxOp {
	if !cond.IsSet() {
		return TxOp{err: t.wrap("transact check", errors.New("CheckOp needs a condition"))}
	}
	expr, err := exprParts{cond: cond}.build()
	if err != nil {
		return TxOp{err: t.wrap("transact check", err)}
	}
	return TxOp{item: types.TransactWriteItem{ConditionCheck: &types.ConditionCheck{
		TableName:                 aws.String(t.schema.Name),
		Key:                       key,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}}
}
