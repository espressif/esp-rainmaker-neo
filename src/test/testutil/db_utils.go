// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"reflect"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/gomega"
)

func CheckRowInDB(tableName string, expectedItem map[string]types.AttributeValue) bool {
	dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)

	found := false
	dbMock.ForEachRow(tableName, func(item map[string]types.AttributeValue) error {
		if reflect.DeepEqual(item, expectedItem) {
			found = true
			return nil
		}
		return nil
	})
	return found
}

func AssertRowInDB(tableName string, expectedItem map[string]types.AttributeValue) {
	Expect(CheckRowInDB(tableName, expectedItem)).To(BeTrue(), "Row should be in the database")
}

func AssertRowNotInDB(tableName string, expectedItem map[string]types.AttributeValue) {
	Expect(CheckRowInDB(tableName, expectedItem)).To(BeFalse(), "Row should not be in the database")
}

func QuickGetItem(tableName string, key map[string]types.AttributeValue) map[string]types.AttributeValue {
	db := awscommon.GetDynamoDBClient()
	input := &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key:       key,
	}
	item, err := db.GetItem(context.Background(), input)
	Expect(err).To(BeNil())
	return item.Item
}

func QuickGetItemByIndex(tableName string, indexName string, keyName string, keyValue string) map[string]types.AttributeValue {
	db := awscommon.GetDynamoDBClient()
	keyCondition := expression.KeyEqual(expression.Key(keyName), expression.Value(keyValue))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
	Expect(err).To(BeNil())

	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(tableName),
		IndexName:                 aws.String(indexName),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	queryOutput, err := db.Query(context.Background(), queryInput)
	Expect(err).To(BeNil())
	Expect(len(queryOutput.Items)).To(BeNumerically(">", 0), "Item should exist in index")
	return queryOutput.Items[0]
}
