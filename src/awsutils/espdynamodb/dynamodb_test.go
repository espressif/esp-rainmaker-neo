// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package espdynamodb_test

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Mock implementation of DBItem for testing
type mockDBItem struct {
	HashKey  string `dynamodbav:"hash_key"`
	RangeKey string `dynamodbav:"range_key"`
	Data     string `dynamodbav:"data"`
}

func (m mockDBItem) GetHKey() string {
	return "hash_key"
}

func (m mockDBItem) GetRKey() string {
	return "range_key"
}

func TestDynamoDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DynamoDB Suite")
}

var _ = Describe("DynamoDB", func() {
	var (
		db     espdynamodb.EspDB
		dbMock *mock.DynamoDBMock
		ctx    *rmngctx.RmngContext
		item   mockDBItem
		table  string
	)

	BeforeEach(func() {
		ctx = &rmngctx.RmngContext{
			Context: context.Background(),
		}
		dbMock = mock.NewDynamoDBMock()
		awscommon.SetDynamoDBClient(dbMock) // Set the mock client globally

		db = espdynamodb.NewEspDB(ctx)

		item = mockDBItem{
			HashKey:  "test-hash",
			RangeKey: "test-range",
			Data:     "test-data",
		}
		table = "test-table"
		dbMock.AddTable(table, "hash_key", "range_key")
	})

	Describe("DbCreateItem", func() {
		Context("when creating a new item", func() {
			It("should successfully create item when it doesn't exist", func() {
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Verify item was created
				var result mockDBItem
				err = db.DbGetItem(table, item, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(item))
			})

			It("should return error when item already exists", func() {
				// First create the item
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Try to create it again
				err = db.DbCreateItem(table, item)
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))
			})
		})
	})

	Describe("DbGetItem", func() {
		Context("when getting an item", func() {
			It("should successfully get existing item", func() {
				// First create the item
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Now get it
				var result mockDBItem
				err = db.DbGetItem(table, item, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(item))
			})
		})
	})

	Describe("DbUpdateItem", func() {
		Context("when updating an item", func() {
			It("should successfully update existing item", func() {
				// First create the item
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Now update it
				update := expression.Set(expression.Name("data"), expression.Value("updated-data"))
				_, err = db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
					TableName: table,
					Update:    update,
					Query:     item,
				})
				Expect(err).NotTo(HaveOccurred())

				// Verify update
				var result mockDBItem
				err = db.DbGetItem(table, item, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Data).To(Equal("updated-data"))
			})

			It("should return error when item doesn't exist", func() {
				update := expression.Set(expression.Name("data"), expression.Value("updated-data"))
				nonExistentItem := mockDBItem{
					HashKey:  "non-existent",
					RangeKey: "non-existent",
				}
				_, err := db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
					TableName: table,
					Update:    update,
					Query:     nonExistentItem,
				})
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))
			})
		})
	})

	Describe("DbDeleteItem", func() {
		Context("when deleting an item", func() {
			It("should successfully delete existing item", func() {
				// First create the item
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Now delete it
				err = db.DbDeleteItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Verify deletion
				var result mockDBItem
				_ = db.DbGetItem(table, item, &result)
				Expect(result.Data).To(BeEmpty())
			})

			It("should return error when item doesn't exist", func() {
				nonExistentItem := mockDBItem{
					HashKey:  "non-existent",
					RangeKey: "non-existent",
				}
				err := db.DbDeleteItem(table, nonExistentItem)
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))
			})
		})
	})

	Describe("DbQueryCountLoop", func() {
		Context("when querying count with pagination", func() {
			BeforeEach(func() {
				// Create multiple items
				for i := 0; i < 15; i++ {
					testItem := mockDBItem{
						HashKey:  "test-hash",
						RangeKey: fmt.Sprintf("range-%d", i),
						Data:     fmt.Sprintf("data-%d", i),
					}
					err := db.DbCreateItem(table, testItem)
					Expect(err).NotTo(HaveOccurred())
				}
			})

			It("should return total count across all pages", func() {
				keyCondition := expression.KeyEqual(expression.Key("hash_key"), expression.Value("test-hash"))
				expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
				Expect(err).NotTo(HaveOccurred())

				input := espdynamodb.DbQueryCountInput{
					TableName: table,
					Expr:      expr,
				}

				count, err := db.DbQueryCountLoop(input)
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(int32(15)))
			})
		})
	})

	Describe("Hash Key Only Operations", func() {
		var (
			hashOnlyTable string
			hashOnlyItem  mockDBItem
		)

		BeforeEach(func() {
			hashOnlyTable = "hash-only-table"
			hashOnlyItem = mockDBItem{
				HashKey:  "test-hash",
				RangeKey: "", // Empty range key for hash-only table
				Data:     "test-data",
			}
			dbMock.AddTable(hashOnlyTable, "hash_key", "") // Empty range key
		})

		Context("when working with hash-key-only items", func() {
			It("should successfully create and get item", func() {
				// Create item
				err := db.DbCreateItem(hashOnlyTable, hashOnlyItem)
				Expect(err).NotTo(HaveOccurred())

				// Get item
				var result mockDBItem
				err = db.DbGetItem(hashOnlyTable, hashOnlyItem, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(hashOnlyItem))
			})

			It("should successfully update item", func() {
				// First create the item
				err := db.DbCreateItem(hashOnlyTable, hashOnlyItem)
				Expect(err).NotTo(HaveOccurred())

				// Update it
				update := expression.Set(expression.Name("data"), expression.Value("updated-data"))
				_, err = db.DbUpdateItem(espdynamodb.DbUpdateItemInput{
					TableName: hashOnlyTable,
					Update:    update,
					Query:     hashOnlyItem,
				})
				Expect(err).NotTo(HaveOccurred())

				// Verify update
				var result mockDBItem
				err = db.DbGetItem(hashOnlyTable, hashOnlyItem, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Data).To(Equal("updated-data"))
			})

			It("should successfully delete item", func() {
				// First create the item
				err := db.DbCreateItem(hashOnlyTable, hashOnlyItem)
				Expect(err).NotTo(HaveOccurred())

				// Delete it
				err = db.DbDeleteItem(hashOnlyTable, hashOnlyItem)
				Expect(err).NotTo(HaveOccurred())

				// Verify deletion
				var result mockDBItem
				_ = db.DbGetItem(hashOnlyTable, hashOnlyItem, &result)
				Expect(result.Data).To(BeEmpty())
			})

			It("should return error when deleting non-existent item", func() {
				nonExistentItem := mockDBItem{
					HashKey:  "non-existent",
					RangeKey: "", // Empty range key for hash-only table
					Data:     "test",
				}
				err := db.DbDeleteItem(hashOnlyTable, nonExistentItem)
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))
			})

			It("should handle batch operations", func() {
				items := []mockDBItem{
					{HashKey: "hash-1", RangeKey: "", Data: "data1"},
					{HashKey: "hash-2", RangeKey: "", Data: "data2"},
				}

				// Batch put
				err := espdynamodb.DbBatchPutItem(&db, hashOnlyTable, items)
				Expect(err).NotTo(HaveOccurred())

				// Verify items were created
				for _, item := range items {
					var result mockDBItem
					err = db.DbGetItem(hashOnlyTable, item, &result)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(item))
				}
			})

			It("should handle query operations", func() {
				// Create multiple items with unique hash keys
				for i := 0; i < 5; i++ {
					testItem := mockDBItem{
						HashKey:  fmt.Sprintf("test-hash-%d", i),
						RangeKey: "",
						Data:     fmt.Sprintf("data-%d", i),
					}
					err := db.DbCreateItem(hashOnlyTable, testItem)
					Expect(err).NotTo(HaveOccurred())
				}

				// Query by hash key prefix
				keyCondition := expression.KeyBeginsWith(expression.Key("hash_key"), "test-hash")
				expr, err := expression.NewBuilder().WithKeyCondition(keyCondition).Build()
				Expect(err).NotTo(HaveOccurred())

				input := espdynamodb.DbQueryCountInput{
					TableName: hashOnlyTable,
					Expr:      expr,
				}

				count, err := db.DbQueryCountLoop(input)
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(int32(5)))
			})

			It("should handle batch get operations", func() {
				// Create test items first
				items := []mockDBItem{
					{HashKey: "hash-1", RangeKey: "", Data: "data1"},
					{HashKey: "hash-2", RangeKey: "", Data: "data2"},
					{HashKey: "hash-3", RangeKey: "", Data: "data3"},
				}
				err := espdynamodb.DbBatchPutItem(&db, hashOnlyTable, items)
				Expect(err).NotTo(HaveOccurred())

				// Prepare batch get input
				keys := []map[string]types.AttributeValue{
					{
						"hash_key": &types.AttributeValueMemberS{Value: "hash-1"},
					},
					{
						"hash_key": &types.AttributeValueMemberS{Value: "hash-2"},
					},
				}

				// Create a projection expression to get all fields
				projection := expression.NamesList(
					expression.Name("hash_key"),
					expression.Name("data"),
				)
				expr, err := expression.NewBuilder().WithProjection(projection).Build()
				Expect(err).NotTo(HaveOccurred())

				input := espdynamodb.DbBatchGetItemLoopInput{
					DBConn:    &db,
					TableName: hashOnlyTable,
					Expr:      expr,
					Keys:      keys,
				}

				var results []mockDBItem
				results, err = espdynamodb.DbBatchGetItemLoop[mockDBItem](input)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(results)).To(Equal(2))
			})

			It("should handle batch delete operations", func() {
				// Create test items first
				items := []mockDBItem{
					{HashKey: "hash-1", RangeKey: "", Data: "data1"},
					{HashKey: "hash-2", RangeKey: "", Data: "data2"},
				}
				err := espdynamodb.DbBatchPutItem(&db, hashOnlyTable, items)
				Expect(err).NotTo(HaveOccurred())

				// Delete items in batch
				err = db.DbBatchDeleteItem(hashOnlyTable, items)
				Expect(err).NotTo(HaveOccurred())

				// Verify items were deleted
				for _, item := range items {
					var result mockDBItem
					_ = db.DbGetItem(hashOnlyTable, item, &result)
					Expect(result.Data).To(BeEmpty())
				}
			})

			It("should handle struct set operations", func() {
				// First create the item
				err := db.DbCreateItem(hashOnlyTable, hashOnlyItem)
				Expect(err).NotTo(HaveOccurred())

				// Update using struct set
				updatedItem := mockDBItem{
					HashKey:  hashOnlyItem.HashKey,
					RangeKey: "",
					Data:     "updated-via-struct",
				}
				_, err = db.DbUpdateItemStructSet(espdynamodb.DbUpdateItemStructSetInput{
					TableName: hashOnlyTable,
					Item:      updatedItem,
				})
				Expect(err).NotTo(HaveOccurred())

				// Verify update
				var result mockDBItem
				err = db.DbGetItem(hashOnlyTable, hashOnlyItem, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Data).To(Equal("updated-via-struct"))
			})
		})
	})

	Describe("DbBatchGetItemLoop", func() {
		Context("when batch getting items", func() {
			BeforeEach(func() {
				// Create multiple items
				for i := 0; i < 2; i++ {
					testItem := mockDBItem{
						HashKey:  fmt.Sprintf("hash-%d", i),
						RangeKey: "range-1",
						Data:     fmt.Sprintf("data-%d", i),
					}
					err := db.DbCreateItem(table, testItem)
					Expect(err).NotTo(HaveOccurred())
				}
			})

			It("should successfully get items in batches", func() {
				keys := []map[string]types.AttributeValue{
					{
						"hash_key":  &types.AttributeValueMemberS{Value: "hash-0"},
						"range_key": &types.AttributeValueMemberS{Value: "range-1"},
					},
					{
						"hash_key":  &types.AttributeValueMemberS{Value: "hash-1"},
						"range_key": &types.AttributeValueMemberS{Value: "range-1"},
					},
				}

				// Create a projection expression to get all fields
				projection := expression.NamesList(
					expression.Name("hash_key"),
					expression.Name("range_key"),
					expression.Name("data"),
				)
				expr, err := expression.NewBuilder().WithProjection(projection).Build()
				Expect(err).NotTo(HaveOccurred())

				input := espdynamodb.DbBatchGetItemLoopInput{
					DBConn:    &db,
					TableName: table,
					Expr:      expr,
					Keys:      keys,
				}

				var results []mockDBItem
				results, err = espdynamodb.DbBatchGetItemLoop[mockDBItem](input)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(results)).To(Equal(2))
			})

			It("should retry and return all items when some keys are unprocessed", func() {
				for i := 0; i < 3; i++ {
					err := db.DbCreateItem(table, mockDBItem{
						HashKey:  fmt.Sprintf("unproc-hash-%d", i),
						RangeKey: "range-1",
						Data:     fmt.Sprintf("data-%d", i),
					})
					Expect(err).NotTo(HaveOccurred())
				}

				keys := []map[string]types.AttributeValue{
					{"hash_key": &types.AttributeValueMemberS{Value: "unproc-hash-0"}, "range_key": &types.AttributeValueMemberS{Value: "range-1"}},
					{"hash_key": &types.AttributeValueMemberS{Value: "unproc-hash-1"}, "range_key": &types.AttributeValueMemberS{Value: "range-1"}},
					{"hash_key": &types.AttributeValueMemberS{Value: "unproc-hash-2"}, "range_key": &types.AttributeValueMemberS{Value: "range-1"}},
				}

				// Return the last 2 keys as unprocessed on the first call; the retry fetches them.
				dbMock.NextBatchGetUnprocessedCount = 2

				projection := expression.NamesList(
					expression.Name("hash_key"),
					expression.Name("range_key"),
					expression.Name("data"),
				)
				expr, err := expression.NewBuilder().WithProjection(projection).Build()
				Expect(err).NotTo(HaveOccurred())

				results, err := espdynamodb.DbBatchGetItemLoop[mockDBItem](espdynamodb.DbBatchGetItemLoopInput{
					DBConn:    &db,
					TableName: table,
					Expr:      expr,
					Keys:      keys,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(len(results)).To(Equal(3))
			})

			It("should chunk keys at DYNAMODB_BATCH_GET_LIMIT and return all items", func() {
				const itemCount = 105
				for i := 0; i < itemCount; i++ {
					err := db.DbCreateItem(table, mockDBItem{
						HashKey:  fmt.Sprintf("chunk-hash-%d", i),
						RangeKey: "range-1",
						Data:     fmt.Sprintf("data-%d", i),
					})
					Expect(err).NotTo(HaveOccurred())
				}

				keys := make([]map[string]types.AttributeValue, itemCount)
				for i := 0; i < itemCount; i++ {
					keys[i] = map[string]types.AttributeValue{
						"hash_key":  &types.AttributeValueMemberS{Value: fmt.Sprintf("chunk-hash-%d", i)},
						"range_key": &types.AttributeValueMemberS{Value: "range-1"},
					}
				}

				projection := expression.NamesList(
					expression.Name("hash_key"),
					expression.Name("range_key"),
					expression.Name("data"),
				)
				expr, err := expression.NewBuilder().WithProjection(projection).Build()
				Expect(err).NotTo(HaveOccurred())

				results, err := espdynamodb.DbBatchGetItemLoop[mockDBItem](espdynamodb.DbBatchGetItemLoopInput{
					DBConn:    &db,
					TableName: table,
					Expr:      expr,
					Keys:      keys,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(len(results)).To(Equal(itemCount))
			})
		})
	})

	Describe("DbBatchPutItem", func() {
		Context("when batch putting items", func() {
			It("should successfully put items in batches", func() {
				items := []mockDBItem{
					{HashKey: "hash-1", RangeKey: "range-1", Data: "data1"},
					{HashKey: "hash-2", RangeKey: "range-1", Data: "data2"},
				}

				err := espdynamodb.DbBatchPutItem(&db, table, items)
				Expect(err).NotTo(HaveOccurred())

				// Verify items were created
				for _, item := range items {
					var result mockDBItem
					err = db.DbGetItem(table, item, &result)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(item))
				}
			})
		})
	})

	Describe("DbUpdateItemStructSet", func() {
		Context("when updating item struct", func() {
			It("should successfully update non-key fields", func() {
				// First create the item
				err := db.DbCreateItem(table, item)
				Expect(err).NotTo(HaveOccurred())

				// Update the data field
				item.Data = "updated-data"
				_, err = db.DbUpdateItemStructSet(espdynamodb.DbUpdateItemStructSetInput{
					TableName: table,
					Item:      item,
				})
				Expect(err).NotTo(HaveOccurred())

				// Verify update
				var result mockDBItem
				err = db.DbGetItem(table, item, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Data).To(Equal("updated-data"))
			})
		})
	})
})
