// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
)

// DynamoDB treats ExclusiveStartKey as a position in the sorted result, not as an item that must still exist, so a page resumes correctly even when the row the cursor names was deleted between calls. A mock that looks the cursor item up by identity silently restarts from the top and re-serves rows the caller already saw.
var _ = Describe("Query pagination", func() {
	const (
		table     = "pagination-base"
		partition = "p1"
	)

	var dbMock *mock.DynamoDBMock

	// sortKeys returns the sk attribute of each returned item, which is what every spec below asserts on.
	sortKeys := func(items []map[string]types.AttributeValue) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item["sk"].(*types.AttributeValueMemberS).Value)
		}
		return out
	}

	put := func(tbl string, item map[string]types.AttributeValue) {
		_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{TableName: aws.String(tbl), Item: item})
		Expect(err).ToNot(HaveOccurred())
	}

	queryPartition := func(in *dynamodb.QueryInput) *dynamodb.QueryOutput {
		expr, err := expression.NewBuilder().
			WithKeyCondition(expression.Key("pk").Equal(expression.Value(partition))).Build()
		Expect(err).ToNot(HaveOccurred())
		in.KeyConditionExpression = expr.KeyCondition()
		in.ExpressionAttributeNames = expr.Names()
		in.ExpressionAttributeValues = expr.Values()
		out, err := dbMock.Query(context.TODO(), in)
		Expect(err).ToNot(HaveOccurred())
		return out
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "pk", "sk")
		for i := 1; i <= 6; i++ {
			put(table, map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: partition},
				"sk": &types.AttributeValueMemberS{Value: fmt.Sprintf("s%02d", i)},
			})
		}
	})

	It("resumes after a cursor whose item was deleted", func() {
		page1 := queryPartition(&dynamodb.QueryInput{TableName: aws.String(table), Limit: aws.Int32(2)})
		Expect(sortKeys(page1.Items)).To(Equal([]string{"s01", "s02"}))
		Expect(page1.LastEvaluatedKey).ToNot(BeEmpty())

		_, err := dbMock.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
			TableName: aws.String(table),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: partition},
				"sk": &types.AttributeValueMemberS{Value: "s02"},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		page2 := queryPartition(&dynamodb.QueryInput{
			TableName: aws.String(table), Limit: aws.Int32(2), ExclusiveStartKey: page1.LastEvaluatedKey,
		})
		Expect(sortKeys(page2.Items)).To(Equal([]string{"s03", "s04"}))
	})

	It("returns sort-key order even when ScanIndexForward is unset", func() {
		for _, sk := range []string{"s00", "s09", "s07"} {
			put(table, map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: partition},
				"sk": &types.AttributeValueMemberS{Value: sk},
			})
		}

		out := queryPartition(&dynamodb.QueryInput{TableName: aws.String(table)})
		Expect(sortKeys(out.Items)).To(Equal([]string{"s00", "s01", "s02", "s03", "s04", "s05", "s06", "s07", "s09"}))
	})

	It("reverses order and still advances the cursor when ScanIndexForward is false", func() {
		page1 := queryPartition(&dynamodb.QueryInput{
			TableName: aws.String(table), Limit: aws.Int32(2), ScanIndexForward: aws.Bool(false),
		})
		Expect(sortKeys(page1.Items)).To(Equal([]string{"s06", "s05"}))

		_, err := dbMock.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
			TableName: aws.String(table),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: partition},
				"sk": &types.AttributeValueMemberS{Value: "s05"},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		page2 := queryPartition(&dynamodb.QueryInput{
			TableName: aws.String(table), Limit: aws.Int32(2),
			ScanIndexForward: aws.Bool(false), ExclusiveStartKey: page1.LastEvaluatedKey,
		})
		Expect(sortKeys(page2.Items)).To(Equal([]string{"s04", "s03"}))
	})
})

// A secondary index can be keyed on attributes the base table is not, so ordering and cursors have to follow the index's schema. Falling back to the base table's silently reorders results and, when the base sort key is constant across the queried partition, collapses the cursor comparison so pagination stalls.
var _ = Describe("Query pagination through a secondary index", func() {
	const (
		table = "events"
		index = "events-by-kind"
		kind  = "alert"
	)

	var dbMock *mock.DynamoDBMock

	deviceIDs := func(items []map[string]types.AttributeValue) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item["device_id"].(*types.AttributeValueMemberS).Value)
		}
		return out
	}

	queryIndex := func(in *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
		expr, err := expression.NewBuilder().
			WithKeyCondition(expression.Key("kind").Equal(expression.Value(kind))).Build()
		Expect(err).ToNot(HaveOccurred())
		in.KeyConditionExpression = expr.KeyCondition()
		in.ExpressionAttributeNames = expr.Names()
		in.ExpressionAttributeValues = expr.Values()
		return dbMock.Query(context.TODO(), in)
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "device_id", "ts")
		Expect(dbMock.AddSecondaryIndex(index, table, "kind", "priority")).To(Succeed())

		// Inserted in device_id order with priorities that disagree, so base-table order and index order cannot both be right.
		for i, priority := range []int{30, 10, 50, 20, 40} {
			_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(table),
				Item: map[string]types.AttributeValue{
					"device_id": &types.AttributeValueMemberS{Value: fmt.Sprintf("dev-%02d", i)},
					"ts":        &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", i)},
					"kind":      &types.AttributeValueMemberS{Value: kind},
					"priority":  &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", priority)},
				},
			})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("orders by the index sort key, not the base table's", func() {
		out, err := queryIndex(&dynamodb.QueryInput{TableName: aws.String(table), IndexName: aws.String(index)})
		Expect(err).ToNot(HaveOccurred())
		Expect(deviceIDs(out.Items)).To(Equal([]string{"dev-01", "dev-03", "dev-00", "dev-04", "dev-02"}))
	})

	It("resumes after a cursor whose item was deleted", func() {
		page1, err := queryIndex(&dynamodb.QueryInput{
			TableName: aws.String(table), IndexName: aws.String(index), Limit: aws.Int32(2),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(deviceIDs(page1.Items)).To(Equal([]string{"dev-01", "dev-03"}))

		_, err = dbMock.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
			TableName: aws.String(table),
			Key: map[string]types.AttributeValue{
				"device_id": &types.AttributeValueMemberS{Value: "dev-03"},
				"ts":        &types.AttributeValueMemberN{Value: "3"},
			},
		})
		Expect(err).ToNot(HaveOccurred())

		page2, err := queryIndex(&dynamodb.QueryInput{
			TableName: aws.String(table), IndexName: aws.String(index), Limit: aws.Int32(2),
			ExclusiveStartKey: page1.LastEvaluatedKey,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(deviceIDs(page2.Items)).To(Equal([]string{"dev-00", "dev-04"}))
	})

	It("returns the index key and the table key in LastEvaluatedKey", func() {
		out, err := queryIndex(&dynamodb.QueryInput{
			TableName: aws.String(table), IndexName: aws.String(index), Limit: aws.Int32(2),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.LastEvaluatedKey).To(HaveKey("kind"))
		Expect(out.LastEvaluatedKey).To(HaveKey("priority"))
		Expect(out.LastEvaluatedKey).To(HaveKey("device_id"))
		Expect(out.LastEvaluatedKey).To(HaveKey("ts"))
	})

	It("rejects a query naming an index that was never registered", func() {
		_, err := queryIndex(&dynamodb.QueryInput{
			TableName: aws.String(table), IndexName: aws.String("events-by-nothing"),
		})
		Expect(err).To(BeAssignableToTypeOf(&types.ResourceNotFoundException{}))
	})
})

// The shape that defeated the previous fix: a hash-only index whose partition key is the base table's sort key, so every row the query matches shares it. Ordering by that attribute alone leaves all rows comparing equal and the cursor cannot advance.
var _ = Describe("Query pagination through a hash-only secondary index", func() {
	const (
		table    = "user-group"
		index    = "user-group-by-group"
		groupID  = "grp-1"
		pageSize = 3
		total    = 10
	)

	It("returns every row exactly once across pages", func() {
		dbMock := mock.NewDynamoDBMock()
		dbMock.AddTable(table, "user_id", "group_id")
		Expect(dbMock.AddSecondaryIndex(index, table, "group_id", "")).To(Succeed())
		dbMock.MaxPageItems = pageSize

		want := make([]string, 0, total)
		for i := range total {
			userID := fmt.Sprintf("user-%02d", i)
			_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(table),
				Item: map[string]types.AttributeValue{
					"user_id":  &types.AttributeValueMemberS{Value: userID},
					"group_id": &types.AttributeValueMemberS{Value: groupID},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			want = append(want, userID)
		}

		expr, err := expression.NewBuilder().
			WithKeyCondition(expression.Key("group_id").Equal(expression.Value(groupID))).Build()
		Expect(err).ToNot(HaveOccurred())

		var got []string
		var startKey map[string]types.AttributeValue
		for {
			out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName:                 aws.String(table),
				IndexName:                 aws.String(index),
				KeyConditionExpression:    expr.KeyCondition(),
				ExpressionAttributeNames:  expr.Names(),
				ExpressionAttributeValues: expr.Values(),
				ExclusiveStartKey:         startKey,
			})
			Expect(err).ToNot(HaveOccurred())
			for _, item := range out.Items {
				got = append(got, item["user_id"].(*types.AttributeValueMemberS).Value)
			}
			if len(out.LastEvaluatedKey) == 0 {
				break
			}
			startKey = out.LastEvaluatedKey
			Expect(len(got)).To(BeNumerically("<=", total), "cursor is not advancing")
		}
		Expect(got).To(Equal(want))
	})
})
