// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// TransactWriteItems evaluates every condition before applying any write, so a failed condition leaves all items untouched, as in the service.
func (m *DynamoDBMock) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	m.condMx.Lock()
	defer m.condMx.Unlock()

	reasons := make([]types.CancellationReason, len(params.TransactItems))
	cancelled := false
	for i, op := range params.TransactItems {
		holds, err := m.transactConditionHolds(op)
		if err != nil {
			return nil, err
		}
		reasons[i] = types.CancellationReason{Code: aws.String("None")}
		if !holds {
			reasons[i].Code = aws.String("ConditionalCheckFailed")
			cancelled = true
		}
	}
	if cancelled {
		return nil, &types.TransactionCanceledException{
			Message:             aws.String("Transaction cancelled, please refer cancellation reasons for specific reasons"),
			CancellationReasons: reasons,
		}
	}

	// Conditions are stripped: they were checked above, and PutItem would otherwise re-take condMx.
	for _, op := range params.TransactItems {
		var err error
		switch {
		case op.Put != nil:
			_, err = m.PutItem(ctx, &dynamodb.PutItemInput{TableName: op.Put.TableName, Item: op.Put.Item})
		case op.Update != nil:
			_, err = m.UpdateItem(ctx, &dynamodb.UpdateItemInput{
				TableName:                 op.Update.TableName,
				Key:                       op.Update.Key,
				UpdateExpression:          op.Update.UpdateExpression,
				ExpressionAttributeNames:  op.Update.ExpressionAttributeNames,
				ExpressionAttributeValues: op.Update.ExpressionAttributeValues,
			})
		case op.Delete != nil:
			_, err = m.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: op.Delete.TableName, Key: op.Delete.Key})
		}
		if err != nil {
			return nil, err
		}
	}
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (m *DynamoDBMock) transactConditionHolds(op types.TransactWriteItem) (bool, error) {
	var (
		table  *string
		key    map[string]types.AttributeValue
		cond   *string
		names  map[string]string
		values map[string]types.AttributeValue
	)
	switch {
	case op.Put != nil:
		table, key, cond, names, values = op.Put.TableName, op.Put.Item, op.Put.ConditionExpression, op.Put.ExpressionAttributeNames, op.Put.ExpressionAttributeValues
	case op.Update != nil:
		table, key, cond, names, values = op.Update.TableName, op.Update.Key, op.Update.ConditionExpression, op.Update.ExpressionAttributeNames, op.Update.ExpressionAttributeValues
	case op.Delete != nil:
		table, key, cond, names, values = op.Delete.TableName, op.Delete.Key, op.Delete.ConditionExpression, op.Delete.ExpressionAttributeNames, op.Delete.ExpressionAttributeValues
	case op.ConditionCheck != nil:
		table, key, cond, names, values = op.ConditionCheck.TableName, op.ConditionCheck.Key, op.ConditionCheck.ConditionExpression, op.ConditionCheck.ExpressionAttributeNames, op.ConditionCheck.ExpressionAttributeValues
	default:
		return false, fmt.Errorf("ValidationException: TransactWriteItem has no operation")
	}
	if cond == nil {
		return true, nil
	}
	details := m.tables[aws.ToString(table)]
	pkey, skey, err := ExtractKeys(key, details.PrimaryKey, details.SortKey)
	if err != nil {
		return false, err
	}
	item, _ := m.getItem(aws.ToString(table), pkey, skey)
	if item == nil {
		item = map[string]types.AttributeValue{}
	}
	return NewMexpression(cond, names, values).Evaluate(item)
}
