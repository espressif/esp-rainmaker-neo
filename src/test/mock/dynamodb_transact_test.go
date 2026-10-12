// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DynamoDBMock TransactWriteItems", func() {
	var (
		ctx context.Context
		m   *mock.DynamoDBMock
	)
	s := func(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
	notExists := aws.String("attribute_not_exists(id)")

	BeforeEach(func() {
		ctx = context.Background()
		m = mock.NewDynamoDBMock()
		m.AddTable("t", "id", "")
	})

	It("writes nothing when any condition fails", func() {
		_, err := m.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("taken")}})
		Expect(err).NotTo(HaveOccurred())

		_, err = m.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("fresh")}, ConditionExpression: notExists}},
			{Put: &types.Put{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("taken")}, ConditionExpression: notExists}},
		}})
		var tce *types.TransactionCanceledException
		Expect(err).To(BeAssignableToTypeOf(tce))

		out, err := m.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"id": s("fresh")}})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Item).To(BeEmpty())
	})

	It("applies put, update and delete together when every condition holds", func() {
		_, err := m.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("u"), "v": s("1")}})
		Expect(err).NotTo(HaveOccurred())
		_, err = m.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("d")}})
		Expect(err).NotTo(HaveOccurred())

		_, err = m.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"id": s("p")}, ConditionExpression: notExists}},
			{Update: &types.Update{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"id": s("u")}, UpdateExpression: aws.String("SET v = :v"), ExpressionAttributeValues: map[string]types.AttributeValue{":v": s("2")}, ConditionExpression: aws.String("attribute_exists(id)")}},
			{Delete: &types.Delete{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"id": s("d")}}},
		}})
		Expect(err).NotTo(HaveOccurred())

		get := func(id string) map[string]types.AttributeValue {
			out, err := m.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"id": s(id)}})
			Expect(err).NotTo(HaveOccurred())
			return out.Item
		}
		Expect(get("p")).NotTo(BeEmpty())
		Expect(get("u")["v"]).To(Equal(s("2")))
		Expect(get("d")).To(BeEmpty())
	})

	It("rejects a batch write that names one key twice, as the service does", func() {
		put := func(id string) types.WriteRequest {
			return types.WriteRequest{PutRequest: &types.PutRequest{Item: map[string]types.AttributeValue{"id": s(id)}}}
		}
		_, err := m.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: map[string][]types.WriteRequest{"t": {put("a"), put("a")}}})
		Expect(err).To(MatchError(ContainSubstring("duplicates")))
	})

	It("rejects a batch get that names one key twice, as the service does", func() {
		key := map[string]types.AttributeValue{"id": s("a")}
		_, err := m.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: map[string]types.KeysAndAttributes{"t": {Keys: []map[string]types.AttributeValue{key, key}}}})
		Expect(err).To(MatchError(ContainSubstring("duplicates")))
	})
})
