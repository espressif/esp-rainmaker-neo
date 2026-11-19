// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// FailingIndexQuery throttles every Query on IndexName, or every Query when IndexName is empty, as a throttled table or a missing grant would.
type FailingIndexQuery struct {
	*DynamoDBMock
	IndexName string
}

func (f *FailingIndexQuery) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if f.IndexName == "" || aws.ToString(params.IndexName) == f.IndexName {
		return nil, throttled()
	}
	return f.DynamoDBMock.Query(ctx, params, optFns...)
}

// FailingGetItem throttles every GetItem on TableName and passes every other call through.
type FailingGetItem struct {
	*DynamoDBMock
	TableName string
}

func (f *FailingGetItem) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if aws.ToString(params.TableName) == f.TableName {
		return nil, throttled()
	}
	return f.DynamoDBMock.GetItem(ctx, params, optFns...)
}

// FailingDeleteItem throttles every DeleteItem on TableName and passes every other call through.
type FailingDeleteItem struct {
	*DynamoDBMock
	TableName string
}

func (f *FailingDeleteItem) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if aws.ToString(params.TableName) == f.TableName {
		return nil, throttled()
	}
	return f.DynamoDBMock.DeleteItem(ctx, params, optFns...)
}

func throttled() error {
	return &types.ProvisionedThroughputExceededException{Message: aws.String("throttled")}
}
