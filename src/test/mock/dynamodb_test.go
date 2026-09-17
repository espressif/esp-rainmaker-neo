// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock_test

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
)

var timingFile *os.File
var _ = BeforeSuite(func() {
	// A filename such that it shows this context towards the end of the summary
	timingFile, _ = test_utils.CreateCommonSummaryFile("wwww.txt")
})

type TestValA struct {
	Id  string `dynamodbav:"id,omitempty"`
	Val int    `dynamodbav:"val,omitempty"`
}

type TestValB struct {
	Name  string `dynamodbav:"name,omitempty"`
	Count int    `dynamodbav:"count,omitempty"`
	Data  string `dynamodbav:"data,omitempty"`
}

// TestValBProjection is used to project only the count and data fields
type TestValBProjection struct {
	Count int    `dynamodbav:"count,omitempty"`
	Data  string `dynamodbav:"data,omitempty"`
}

func NewTestValAFromAV(av map[string]types.AttributeValue) (*TestValA, error) {
	var val TestValA
	err := attributevalue.UnmarshalMap(av, &val)
	if err != nil {
		return nil, err
	}
	return &val, nil
}

func NewTestValBFromAV(av map[string]types.AttributeValue) (*TestValB, error) {
	var val TestValB
	err := attributevalue.UnmarshalMap(av, &val)
	if err != nil {
		return nil, err
	}
	return &val, nil
}

func GetDataFromAV(av map[string]types.AttributeValue) (*TestValBProjection, error) {
	var val TestValBProjection
	err := attributevalue.UnmarshalMap(av, &val)
	if err != nil {
		return nil, err
	}
	return &val, nil
}

func ToUpdateItemInput(table string, item interface{}) (*dynamodb.UpdateItemInput, error) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return nil, err
	}
	return &dynamodb.UpdateItemInput{
		TableName: &table,
		Key:       av,
	}, nil
}

func ToPutItemInput(table string, item interface{}) (*dynamodb.PutItemInput, error) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return nil, err
	}
	return &dynamodb.PutItemInput{
		TableName: &table,
		Item:      av,
	}, nil
}

func ToGetItemInput(table string, key interface{}) (*dynamodb.GetItemInput, error) {
	av, err := attributevalue.MarshalMap(key)
	if err != nil {
		return nil, err
	}
	return &dynamodb.GetItemInput{
		TableName: &table,
		Key:       av,
	}, nil
}

var _ = Describe("Dynamodb", func() {
	Describe("DynamoDBMock", func() {
		var (
			dbMock  *mock.DynamoDBMock
			table1  string
			table2  string
			val1    TestValA
			val2    TestValA
			val3    TestValA
			putval1 *dynamodb.PutItemInput
			putval2 *dynamodb.PutItemInput
		)

		BeforeEach(func() {
			dbMock = mock.NewDynamoDBMock()
			table1 = "table1"
			table2 = "table2"
			val1 = TestValA{Id: "id1", Val: 1}
			val2 = TestValA{Id: "id2", Val: 2}
			val3 = TestValA{Id: "id3", Val: 3}
			dbMock.AddTable(table1, "id", "")
			putval1, _ = ToPutItemInput(table1, val1)
			putval2, _ = ToPutItemInput(table1, val2)

		})

		Context("GetItem", func() {
			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)
				dbMock.PutItem(context.TODO(), putval2)
			})
			Context("when the item1 exists", func() {
				It("should return the item1", func() {
					getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
					output, err := dbMock.GetItem(context.TODO(), getval1)
					Expect(err).To(BeNil())
					expect_val1, _ := NewTestValAFromAV(output.Item)
					Expect(*expect_val1).To(Equal(val1))
				})
			})

			Context("when the item2 exists", func() {
				It("should return the item2", func() {
					getval2, _ := ToGetItemInput(table1, map[string]string{"id": val2.Id})
					output, err := dbMock.GetItem(context.TODO(), getval2)
					Expect(err).To(BeNil())
					expect_val2, _ := NewTestValAFromAV(output.Item)
					Expect(*expect_val2).To(Equal(val2))
				})
			})

			Context("when the item does not exist", func() {
				It("should return empty item", func() {
					getval3, _ := ToGetItemInput(table1, map[string]string{"id": val3.Id})
					item, err := dbMock.GetItem(context.TODO(), getval3)
					Expect(item.Item).To(BeEmpty())
					Expect(err).To(BeNil())
				})
			})
		})

		Context("PutItem", func() {
			It("should store the item", func() {
				_, err := dbMock.PutItem(context.TODO(), putval1)
				Expect(err).To(BeNil())

				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				output, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())
				expect_val1, _ := NewTestValAFromAV(output.Item)
				Expect(*expect_val1).To(Equal(val1))
			})

			It("should respect attribute_not_exists condition", func() {
				// First put should succeed as item doesn't exist
				expr, err := expression.NewBuilder().
					WithCondition(expression.AttributeNotExists(expression.Name("id"))).
					Build()
				Expect(err).To(BeNil())

				putval1.ConditionExpression = expr.Condition()
				putval1.ExpressionAttributeNames = expr.Names()
				putval1.ExpressionAttributeValues = expr.Values()

				_, err = dbMock.PutItem(context.TODO(), putval1)
				Expect(err).To(BeNil())

				// Second put should fail as item now exists
				_, err = dbMock.PutItem(context.TODO(), putval1)
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))

				// Verify that the original item is still there
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				output, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())
				expect_val1, _ := NewTestValAFromAV(output.Item)
				Expect(*expect_val1).To(Equal(val1))
			})

			It("should respect multiple attribute_not_exists conditions", func() {
				// Create a table with both partition and sort key
				tableWithSort := "table_with_sort"
				dbMock.AddTable(tableWithSort, "pk", "sk")

				// Create test item with partition and sort keys
				testItem := map[string]types.AttributeValue{
					"pk":   &types.AttributeValueMemberS{Value: "partition1"},
					"sk":   &types.AttributeValueMemberS{Value: "sort1"},
					"data": &types.AttributeValueMemberS{Value: "test_data"},
				}

				// Build condition checking both keys don't exist
				expr, err := expression.NewBuilder().
					WithCondition(
						expression.And(
							expression.AttributeNotExists(expression.Name("pk")),
							expression.AttributeNotExists(expression.Name("sk")),
						),
					).
					Build()
				Expect(err).To(BeNil())

				// First put should succeed as neither key exists
				putInput := &dynamodb.PutItemInput{
					TableName:                 aws.String(tableWithSort),
					Item:                      testItem,
					ConditionExpression:       expr.Condition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				_, err = dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Second put should fail as both keys now exist
				_, err = dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(HaveOccurred())
				var ccfe *types.ConditionalCheckFailedException
				Expect(err).To(BeAssignableToTypeOf(ccfe))

				// Try with different sort key but same partition key - should succeed
				testItem["sk"] = &types.AttributeValueMemberS{Value: "sort2"}
				_, err = dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Try with different partition key but same sort key - should succeed
				testItem["pk"] = &types.AttributeValueMemberS{Value: "partition2"}
				testItem["sk"] = &types.AttributeValueMemberS{Value: "sort1"}
				_, err = dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Should succeed with both keys different
				testItem["pk"] = &types.AttributeValueMemberS{Value: "partition2"}
				testItem["sk"] = &types.AttributeValueMemberS{Value: "sort2"}
				_, err = dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())
			})
		})

		Context("DeleteItem", func() {
			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)
			})

			It("should delete the item", func() {
				_, err := dbMock.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
					TableName: &table1,
					Key: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: val1.Id},
					},
				})
				Expect(err).To(BeNil())

				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				item, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(item.Item).To(BeEmpty())
				Expect(err).To(BeNil())
			})
		})

		Context("Multiple Tables", func() {
			var (
				valb TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "")
				valb = TestValB{Name: "name1", Count: 101, Data: "data1"}
				putvalb, _ := ToPutItemInput(table2, valb)
				dbMock.PutItem(context.TODO(), putvalb)
			})

			It("should store the items in the correct tables", func() {
				// Get val1 from table1
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				output, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())
				expect_val1, _ := NewTestValAFromAV(output.Item)
				Expect(*expect_val1).To(Equal(val1))

				// Get valb from table2
				getvalb, _ := ToGetItemInput(table2, map[string]string{"name": valb.Name})
				output, err = dbMock.GetItem(context.TODO(), getvalb)
				Expect(err).To(BeNil())
				expect_valb, _ := NewTestValBFromAV(output.Item)
				Expect(*expect_valb).To(Equal(valb))
			})
		})
		Context("Sort Keys", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			Context("GetItem", func() {
				It("should should fail when sortkey is not used", func() {
					getvalb, _ := ToGetItemInput(table2, map[string]string{"name": valb2.Name})
					_, err := dbMock.GetItem(context.TODO(), getvalb)
					Expect(err).To(HaveOccurred())
				})
			})
			Context("GetItem", func() {
				It("should should succeed when primary+sort key is used", func() {
					getvalb2, _ := ToGetItemInput(table2, map[string]string{"name": valb2.Name, "count": "102"})
					av, err := dbMock.GetItem(context.TODO(), getvalb2)
					Expect(err).To(BeNil())
					Expect(NewTestValBFromAV(av.Item)).To(Equal(&valb2))
				})
				It("should should succeed when a different primary+sort key is used", func() {
					getvalb1, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name, "count": "101"})
					av, err := dbMock.GetItem(context.TODO(), getvalb1)
					Expect(err).To(BeNil())
					Expect(NewTestValBFromAV(av.Item)).To(Equal(&valb1))
				})
				It("should return empty response when an incorrect sort key is used", func() {
					getvalb1, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name, "count": "105"})
					item, err := dbMock.GetItem(context.TODO(), getvalb1)
					Expect(item.Item).To(BeEmpty())
					Expect(err).To(BeNil())
				})
			})
		})
		Context("Query", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			It("should return all items matching primary key", func() {
				keyCondition := expression.KeyEqual(expression.Key("name"), expression.Value("name1"))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(2))
				outb1, _ := NewTestValBFromAV(output.Items[0])
				outb2, _ := NewTestValBFromAV(output.Items[1])
				Expect(outb1).To(Or(Equal(&valb1), Equal(&valb2)))
				Expect(outb2).To(Or(Equal(&valb2), Equal(&valb1)))
				Expect(outb1).ToNot(Equal(outb2))
			})
			It("should return no items if primary key is not found", func() {
				keyCondition := expression.KeyEqual(expression.Key("name"), expression.Value("name234"))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(0))
			})
			It("should return items matching primary key and sort key", func() {
				keyCondition := expression.KeyAnd(expression.KeyEqual(expression.Key("name"), expression.Value("name1")),
					expression.KeyEqual(expression.Key("count"), expression.Value(101)))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(1))
				Expect(NewTestValBFromAV(output.Items[0])).To(Equal(&valb1))
			})
			It("should correctly process the filter expression", func() {
				keyCondition := expression.KeyEqual(expression.Key("name"), expression.Value("name1"))
				filter := expression.Equal(expression.Key("data"), expression.Value("data102"))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).
					WithFilter(filter).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2,
					KeyConditionExpression:    expr.KeyCondition(),
					FilterExpression:          expr.Filter(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(1))
				Expect(NewTestValBFromAV(output.Items[0])).To(Equal(&valb2))
			})
			It("should return all items matching primary key with correct projection", func() {
				expectb1 := TestValBProjection{Count: 101, Data: "data101"}
				expectb2 := TestValBProjection{Count: 102, Data: "data102"}
				keyCondition := expression.KeyEqual(expression.Key("name"), expression.Value("name1"))
				projection := expression.NamesList(expression.Name("count"), expression.Name("data"))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).WithProjection(projection).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ProjectionExpression:      expr.Projection(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(2))
				out1, _ := GetDataFromAV(output.Items[0])
				out2, _ := GetDataFromAV(output.Items[1])
				// Sometimes the order changes, hence these gymnastics
				Expect(out1).To(Or(Equal(&expectb1), Equal(&expectb2)))
				Expect(out2).To(Or(Equal(&expectb2), Equal(&expectb1)))
				Expect(out1).ToNot(Equal(out2))
			})
			It("should return all items matching secondary index with correct projection", func() {
				table2_index := "table2_index"
				dbMock.AddSecondaryIndex(table2_index, table2, "data", "count")
				expectb1 := TestValBProjection{Count: 101, Data: "data101"}
				keyCondition := expression.KeyEqual(expression.Key("data"), expression.Value("data101"))
				projection := expression.NamesList(expression.Name("count"), expression.Name("data"))
				expr, _ := expression.NewBuilder().
					WithKeyCondition(keyCondition).WithProjection(projection).Build()
				q := &dynamodb.QueryInput{
					TableName:                 &table2_index,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ProjectionExpression:      expr.Projection(),
				}
				output, err := dbMock.Query(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(1))
				out1, _ := GetDataFromAV(output.Items[0])
				Expect(out1).To(Equal(&expectb1))
			})
		})
		Context("UpdateItem", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			It("should update the item matching primary key and sort key", func() {
				updateExpression := expression.Set(expression.Name("data"), expression.Value("data101_updated"))
				expr, _ := expression.NewBuilder().
					WithUpdate(updateExpression).Build()

				key := TestValB{Name: valb1.Name, Count: 101}
				q, _ := ToUpdateItemInput(table2, key)
				q.UpdateExpression = expr.Update()
				q.ExpressionAttributeNames = expr.Names()
				q.ExpressionAttributeValues = expr.Values()
				_, err := dbMock.UpdateItem(context.TODO(), q)
				Expect(err).To(BeNil())

				getvalb1, _ := ToGetItemInput(table2, key)
				av, err := dbMock.GetItem(context.TODO(), getvalb1)
				Expect(err).To(BeNil())
				Expect(NewTestValBFromAV(av.Item)).To(Equal(&TestValB{Name: "name1", Count: 101, Data: "data101_updated"}))
			})

			It("should return updated value when ReturnValues is set to ALL_NEW", func() {
				updateExpression := expression.Set(expression.Name("data"), expression.Value("data101_updated"))
				expr, _ := expression.NewBuilder().
					WithUpdate(updateExpression).Build()

				key := TestValB{Name: valb1.Name, Count: 101}
				q, _ := ToUpdateItemInput(table2, key)
				q.UpdateExpression = expr.Update()
				q.ExpressionAttributeNames = expr.Names()
				q.ExpressionAttributeValues = expr.Values()
				q.ReturnValues = types.ReturnValueAllNew
				result, err := dbMock.UpdateItem(context.TODO(), q)
				Expect(err).To(BeNil())

				Expect(NewTestValBFromAV(result.Attributes)).To(Equal(&TestValB{Name: "name1", Count: 101, Data: "data101_updated"}))
			})

			It("should update the item matching primary key", func() {
				updateExpression := expression.Set(expression.Name("val"), expression.Value(10))
				expr, _ := expression.NewBuilder().
					WithUpdate(updateExpression).Build()

				key := TestValA{Id: val1.Id}

				// First ensure that the old value is present
				getval1, _ := ToGetItemInput(table1, key)
				av, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())
				Expect(NewTestValAFromAV(av.Item)).To(Equal(&TestValA{Id: "id1", Val: 1}))

				q, _ := ToUpdateItemInput(table1, key)
				q.UpdateExpression = expr.Update()
				q.ExpressionAttributeNames = expr.Names()
				q.ExpressionAttributeValues = expr.Values()
				_, err = dbMock.UpdateItem(context.TODO(), q)
				Expect(err).To(BeNil())

				// Now ensure that the new value is present
				getval1, _ = ToGetItemInput(table1, key)
				av, err = dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())
				Expect(NewTestValAFromAV(av.Item)).To(Equal(&TestValA{Id: "id1", Val: 10}))
			})
			It("should add a new item if primary key and sort key doesn't match", func() {
				updateExpression := expression.Set(expression.Name("data"), expression.Value("data104_updated"))
				expr, _ := expression.NewBuilder().
					WithUpdate(updateExpression).Build()

				key := TestValB{Name: valb1.Name, Count: 104}
				q, _ := ToUpdateItemInput(table2, key)
				q.UpdateExpression = expr.Update()
				q.ExpressionAttributeNames = expr.Names()
				q.ExpressionAttributeValues = expr.Values()
				_, err := dbMock.UpdateItem(context.TODO(), q)
				Expect(err).To(BeNil())

				getvalb1, _ := ToGetItemInput(table2, key)
				av, err := dbMock.GetItem(context.TODO(), getvalb1)
				Expect(err).To(BeNil())
				Expect(NewTestValBFromAV(av.Item)).To(Equal(&TestValB{Name: "name1", Count: 104, Data: "data104_updated"}))

				// Check one of the original item is unchanged
				key = TestValB{Name: valb1.Name, Count: 101}
				getvalb1, _ = ToGetItemInput(table2, key)
				av, err = dbMock.GetItem(context.TODO(), getvalb1)
				Expect(err).To(BeNil())
				Expect(NewTestValBFromAV(av.Item)).To(Equal(&TestValB{Name: "name1", Count: 101, Data: "data101"}))
			})

			It("should handle REMOVE operations in UpdateItem", func() {
				// First create an item with multiple fields
				initialItem := TestValB{
					Name:  "test-name",
					Count: 101,
					Data:  "test-data",
				}
				putInput, _ := ToPutItemInput(table2, initialItem)
				_, err := dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Verify initial state
				key := TestValB{Name: initialItem.Name, Count: initialItem.Count}
				getInput, _ := ToGetItemInput(table2, key)
				result, err := dbMock.GetItem(context.TODO(), getInput)
				Expect(err).To(BeNil())
				initialState, err := NewTestValBFromAV(result.Item)
				Expect(err).To(BeNil())
				Expect(initialState.Data).To(Equal("test-data"))

				// Create update expression to remove the 'data' field
				updateExpression := expression.Remove(expression.Name("data"))
				expr, err := expression.NewBuilder().
					WithUpdate(updateExpression).Build()
				Expect(err).To(BeNil())

				// Update the item to remove the field
				updateInput, _ := ToUpdateItemInput(table2, key)
				updateInput.UpdateExpression = expr.Update()
				updateInput.ExpressionAttributeNames = expr.Names()
				updateInput.ReturnValues = types.ReturnValueAllNew

				updateResult, err := dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify the field was removed
				updatedItem, err := NewTestValBFromAV(updateResult.Attributes)
				Expect(err).To(BeNil())
				Expect(updatedItem.Name).To(Equal(initialItem.Name))
				Expect(updatedItem.Count).To(Equal(initialItem.Count))
				Expect(updatedItem.Data).To(Equal("")) // Data field should be empty

				// Verify with a fresh get
				getResult, err := dbMock.GetItem(context.TODO(), getInput)
				Expect(err).To(BeNil())
				finalState, err := NewTestValBFromAV(getResult.Item)
				Expect(err).To(BeNil())
				Expect(finalState.Data).To(Equal("")) // Confirm data field is still empty
			})

			It("should handle multiple REMOVE operations in UpdateItem", func() {
				// Create an item with multiple fields
				initialItem := TestValB{
					Name:  "test-name",
					Count: 101,
					Data:  "test-data",
				}
				putInput, _ := ToPutItemInput(table2, initialItem)
				_, err := dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Create update expression to remove multiple fields
				updateExpression := expression.Remove(expression.Name("data")).
					Remove(expression.Name("count"))
				expr, err := expression.NewBuilder().
					WithUpdate(updateExpression).Build()
				Expect(err).To(BeNil())

				// Update the item to remove fields
				key := TestValB{Name: initialItem.Name, Count: initialItem.Count}
				updateInput, _ := ToUpdateItemInput(table2, key)
				updateInput.UpdateExpression = expr.Update()
				updateInput.ExpressionAttributeNames = expr.Names()
				updateInput.ReturnValues = types.ReturnValueAllNew

				result, err := dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify fields were removed
				updatedItem, err := NewTestValBFromAV(result.Attributes)
				Expect(err).To(BeNil())
				Expect(updatedItem.Name).To(Equal(initialItem.Name)) // Name is part of key, should remain
				Expect(updatedItem.Count).To(Equal(0))               // Count should be zero/empty
				Expect(updatedItem.Data).To(Equal(""))               // Data should be empty
			})

			It("Should handle REMOVE operations on list elements with ConditionExpression", func() {
				// Create initial item with a list
				initialItem := map[string]types.AttributeValue{
					"id": &types.AttributeValueMemberS{Value: "test1"},
					"mylist": &types.AttributeValueMemberL{
						Value: []types.AttributeValue{
							&types.AttributeValueMemberS{Value: "item1"},
							&types.AttributeValueMemberS{Value: "item2"},
							&types.AttributeValueMemberS{Value: "item3"},
						},
					},
				}
				putInput := &dynamodb.PutItemInput{
					TableName: &table1,
					Item:      initialItem,
				}
				_, err := dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Try to remove item with condition that matches
				updateInput := &dynamodb.UpdateItemInput{
					TableName: &table1,
					Key: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: "test1"},
					},
					UpdateExpression:    aws.String("REMOVE mylist[1]"),
					ConditionExpression: aws.String("mylist[1] = :val"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":val": &types.AttributeValueMemberS{Value: "item2"},
					},
					ReturnValues: types.ReturnValueAllNew,
				}

				result, err := dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify item was removed
				list := result.Attributes["mylist"].(*types.AttributeValueMemberL)
				Expect(len(list.Value)).To(Equal(2))
				Expect(list.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("item1"))
				Expect(list.Value[1].(*types.AttributeValueMemberS).Value).To(Equal("item3"))

				// Try to remove item with condition that doesn't match
				updateInput.ConditionExpression = aws.String("mylist[0] = :val")
				updateInput.ExpressionAttributeValues = map[string]types.AttributeValue{
					":val": &types.AttributeValueMemberS{Value: "wrong_value"},
				}
				_, err = dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(Not(BeNil()))

				// Verify list remains unchanged
				getInput := &dynamodb.GetItemInput{
					TableName: &table1,
					Key: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: "test1"},
					},
				}
				getResult, err := dbMock.GetItem(context.TODO(), getInput)
				Expect(err).To(BeNil())
				list = getResult.Item["mylist"].(*types.AttributeValueMemberL)
				Expect(len(list.Value)).To(Equal(2))
				Expect(list.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("item1"))
				Expect(list.Value[1].(*types.AttributeValueMemberS).Value).To(Equal("item3"))
			})

			It("should handle list operations in UpdateItem", func() {
				// Create initial item with a list
				initialItem := map[string]types.AttributeValue{
					"id": &types.AttributeValueMemberS{Value: "test1"},
					"mylist": &types.AttributeValueMemberL{
						Value: []types.AttributeValue{
							&types.AttributeValueMemberS{Value: "item1"},
							&types.AttributeValueMemberS{Value: "item2"},
						},
					},
				}
				putInput := &dynamodb.PutItemInput{
					TableName: &table1,
					Item:      initialItem,
				}
				_, err := dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Test list_append operation
				appendExpr := expression.Set(
					expression.Name("mylist"),
					expression.ListAppend(
						expression.Name("mylist"),
						expression.Value([]string{"item3", "item4"}),
					),
				)
				expr, err := expression.NewBuilder().WithUpdate(appendExpr).Build()
				Expect(err).To(BeNil())

				updateInput := &dynamodb.UpdateItemInput{
					TableName: &table1,
					Key: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: "test1"},
					},
					UpdateExpression:          expr.Update(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ReturnValues:              types.ReturnValueAllNew,
				}

				result, err := dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify list was appended
				list := result.Attributes["mylist"].(*types.AttributeValueMemberL)
				Expect(len(list.Value)).To(Equal(4))
				Expect(list.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("item1"))
				Expect(list.Value[1].(*types.AttributeValueMemberS).Value).To(Equal("item2"))
				Expect(list.Value[2].(*types.AttributeValueMemberS).Value).To(Equal("item3"))
				Expect(list.Value[3].(*types.AttributeValueMemberS).Value).To(Equal("item4"))

				// Test removing item from list
				removeExpr := expression.Remove(expression.Name("mylist[1]"))
				expr, err = expression.NewBuilder().WithUpdate(removeExpr).Build()
				Expect(err).To(BeNil())

				updateInput.UpdateExpression = expr.Update()
				updateInput.ExpressionAttributeNames = expr.Names()
				updateInput.ExpressionAttributeValues = expr.Values()

				result, err = dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify item was removed
				list = result.Attributes["mylist"].(*types.AttributeValueMemberL)
				Expect(len(list.Value)).To(Equal(3))
				Expect(list.Value[0].(*types.AttributeValueMemberS).Value).To(Equal("item1"))
				Expect(list.Value[1].(*types.AttributeValueMemberS).Value).To(Equal("item3"))
				Expect(list.Value[2].(*types.AttributeValueMemberS).Value).To(Equal("item4"))
			})

			It("should handle combined REMOVE and SET operations in UpdateItem", func() {
				// First create an item with multiple fields
				initialItem := TestValB{
					Name:  "test-name",
					Count: 101,
					Data:  "test-data",
				}
				putInput, _ := ToPutItemInput(table2, initialItem)
				_, err := dbMock.PutItem(context.TODO(), putInput)
				Expect(err).To(BeNil())

				// Verify initial state
				key := TestValB{Name: initialItem.Name, Count: initialItem.Count}
				getInput, _ := ToGetItemInput(table2, key)
				result, err := dbMock.GetItem(context.TODO(), getInput)
				Expect(err).To(BeNil())
				initialState, err := NewTestValBFromAV(result.Item)
				Expect(err).To(BeNil())
				Expect(initialState.Data).To(Equal("test-data"))

				// Create update expression to remove the 'data' field and set a new value
				updateExpression := expression.Remove(expression.Name("data")).
					Set(expression.Name("count"), expression.Value(102))
				expr, err := expression.NewBuilder().
					WithUpdate(updateExpression).Build()
				Expect(err).To(BeNil())

				// Update the item to remove the field and set new value
				updateInput, _ := ToUpdateItemInput(table2, key)
				updateInput.UpdateExpression = expr.Update()
				updateInput.ExpressionAttributeNames = expr.Names()
				updateInput.ExpressionAttributeValues = expr.Values()
				updateInput.ReturnValues = types.ReturnValueAllNew

				updateResult, err := dbMock.UpdateItem(context.TODO(), updateInput)
				Expect(err).To(BeNil())

				// Verify the field was removed and new value was set
				updatedItem, err := NewTestValBFromAV(updateResult.Attributes)
				Expect(err).To(BeNil())
				Expect(updatedItem.Name).To(Equal(initialItem.Name))
				Expect(updatedItem.Count).To(Equal(102))
				Expect(updatedItem.Data).To(Equal("")) // Data field should be empty

				// Verify with a fresh get
				getResult, err := dbMock.GetItem(context.TODO(), getInput)
				Expect(err).To(BeNil())
				finalState, err := NewTestValBFromAV(getResult.Item)
				Expect(err).To(BeNil())
				Expect(finalState.Count).To(Equal(102))
				Expect(finalState.Data).To(Equal("")) // Confirm data field is still empty
			})
		})
		Context("BatchGetItem", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			It("should return all items matching primary key", func() {
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				getvalb1, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name, "count": "101"})
				getvalb2, _ := ToGetItemInput(table2, map[string]string{"name": valb2.Name, "count": "102"})
				input := &dynamodb.BatchGetItemInput{
					RequestItems: map[string]types.KeysAndAttributes{
						table1: {
							Keys: []map[string]types.AttributeValue{
								getval1.Key,
							},
						},
						table2: {
							Keys: []map[string]types.AttributeValue{
								getvalb1.Key,
								getvalb2.Key,
							},
						},
					},
				}
				output, err := dbMock.BatchGetItem(context.TODO(), input)
				Expect(err).To(BeNil())
				Expect(len(output.Responses)).To(Equal(2))
				Expect(len(output.Responses[table1])).To(Equal(1))
				Expect(len(output.Responses[table2])).To(Equal(2))
				Expect(NewTestValAFromAV(output.Responses[table1][0])).To(Equal(&val1))
				Expect(NewTestValBFromAV(output.Responses[table2][0])).To(Equal(&valb1))
				Expect(NewTestValBFromAV(output.Responses[table2][1])).To(Equal(&valb2))
			})
		})
		Context("BatchWriteItem", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			It("PutRequests should store all items", func() {
				vala := TestValA{Id: "id1", Val: 5000}
				putvala, _ := ToPutItemInput(table1, vala)

				valb1 = TestValB{Name: "name1", Count: 1000, Data: "data1000"}
				putvalb1, _ := ToPutItemInput(table2, valb1)

				valb2 = TestValB{Name: "name1", Count: 2000, Data: "data2000"}
				putvalb2, _ := ToPutItemInput(table2, valb2)

				input := &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]types.WriteRequest{
						table1: {
							{
								PutRequest: &types.PutRequest{
									Item: putvala.Item,
								},
							},
						},
						table2: {
							{
								PutRequest: &types.PutRequest{
									Item: putvalb1.Item,
								},
							},
							{
								PutRequest: &types.PutRequest{
									Item: putvalb2.Item,
								},
							},
						},
					},
				}
				_, err := dbMock.BatchWriteItem(context.TODO(), input)
				Expect(err).To(BeNil())

				// Get val1 from table1
				getvala, _ := ToGetItemInput(table1, map[string]string{"id": vala.Id})
				output, err := dbMock.GetItem(context.TODO(), getvala)
				Expect(err).To(BeNil())
				expect_val1, _ := NewTestValAFromAV(output.Item)
				Expect(*expect_val1).To(Equal(vala))

				// Get valb from table2
				getvalb, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name, "count": "1000"})
				output, err = dbMock.GetItem(context.TODO(), getvalb)
				Expect(err).To(BeNil())
				expect_valb, _ := NewTestValBFromAV(output.Item)
				Expect(*expect_valb).To(Equal(valb1))

				// Get valb from table2
				getvalb, _ = ToGetItemInput(table2, map[string]string{"name": valb2.Name, "count": "2000"})
				output, err = dbMock.GetItem(context.TODO(), getvalb)
				Expect(err).To(BeNil())
				expect_valb, _ = NewTestValBFromAV(output.Item)
				Expect(*expect_valb).To(Equal(valb2))
			})
			It("DeleteRequests should delete all items", func() {
				// Get val1 from table1
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				_, err := dbMock.GetItem(context.TODO(), getval1)
				Expect(err).To(BeNil())

				// Get valb from table2
				getvalb1, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name, "count": "101"})
				_, err = dbMock.GetItem(context.TODO(), getvalb1)
				Expect(err).To(BeNil())

				// Get valb from table2
				getvalb2, _ := ToGetItemInput(table2, map[string]string{"name": valb2.Name, "count": "102"})
				_, err = dbMock.GetItem(context.TODO(), getvalb2)
				Expect(err).To(BeNil())
				input := &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]types.WriteRequest{
						table1: {
							{
								DeleteRequest: &types.DeleteRequest{
									Key: getval1.Key,
								},
							},
						},
						table2: {
							{
								DeleteRequest: &types.DeleteRequest{
									Key: getvalb1.Key,
								},
							},
							{
								DeleteRequest: &types.DeleteRequest{
									Key: getvalb2.Key,
								},
							},
						},
					},
				}
				_, err = dbMock.BatchWriteItem(context.TODO(), input)
				Expect(err).To(BeNil())

				// Get val1 from table1
				getvala, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				item, err := dbMock.GetItem(context.TODO(), getvala)
				Expect(item.Item).To(BeEmpty())
				Expect(err).To(BeNil())

				// Get valb from table2
				getvalb, _ := ToGetItemInput(table2, map[string]string{"name": valb1.Name})
				_, err = dbMock.GetItem(context.TODO(), getvalb)
				Expect(err).To(HaveOccurred())

				// Get valb from table2
				getvalb, _ = ToGetItemInput(table2, map[string]string{"name": valb2.Name})
				_, err = dbMock.GetItem(context.TODO(), getvalb)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("Scan", func() {
			var (
				valb1, valb2 TestValB
			)

			BeforeEach(func() {
				dbMock.PutItem(context.TODO(), putval1)

				dbMock.AddTable(table2, "name", "count")

				valb1 = TestValB{Name: "name1", Count: 101, Data: "data101"}
				putvalb, _ := ToPutItemInput(table2, valb1)
				dbMock.PutItem(context.TODO(), putvalb)

				valb2 = TestValB{Name: "name1", Count: 102, Data: "data102"}
				putvalb, _ = ToPutItemInput(table2, valb2)
				dbMock.PutItem(context.TODO(), putvalb)
			})
			It("should return all items", func() {
				s := &dynamodb.ScanInput{
					TableName: &table2,
				}
				output, err := dbMock.Scan(context.TODO(), s)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(2))
				outb1, _ := NewTestValBFromAV(output.Items[0])
				outb2, _ := NewTestValBFromAV(output.Items[1])
				Expect(outb1).To(Or(Equal(&valb1), Equal(&valb2)))
				Expect(outb2).To(Or(Equal(&valb2), Equal(&valb1)))
				Expect(outb1).ToNot(Equal(outb2))
			})
			It("should correctly process the filter expression", func() {
				filter := expression.Equal(expression.Key("data"), expression.Value("data101"))
				expr, _ := expression.NewBuilder().
					WithFilter(filter).Build()
				s := &dynamodb.ScanInput{
					TableName:                 &table2,
					FilterExpression:          expr.Filter(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}
				output, err := dbMock.Scan(context.TODO(), s)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(1))
				Expect(NewTestValBFromAV(output.Items[0])).To(Equal(&valb1))
			})
			It("should return all items with correct projection", func() {
				expectb1 := TestValBProjection{Count: 101, Data: "data101"}
				expectb2 := TestValBProjection{Count: 102, Data: "data102"}
				projection := expression.NamesList(expression.Name("count"), expression.Name("data"))
				expr, _ := expression.NewBuilder().WithProjection(projection).Build()
				q := &dynamodb.ScanInput{
					TableName:                 &table2,
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ProjectionExpression:      expr.Projection(),
				}
				output, err := dbMock.Scan(context.TODO(), q)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(2))
				out1, _ := GetDataFromAV(output.Items[0])
				out2, _ := GetDataFromAV(output.Items[1])
				// Sometimes the order changes, hence these gymnastics
				Expect(out1).To(Or(Equal(&expectb1), Equal(&expectb2)))
				Expect(out2).To(Or(Equal(&expectb2), Equal(&expectb1)))
				Expect(out1).ToNot(Equal(out2))
			})
			It("should return all items for secondary index", func() {
				table2_index := "table2_index"
				_ = dbMock.AddSecondaryIndex(table2_index, table2, "data", "count")

				s := &dynamodb.ScanInput{
					TableName: &table2_index,
				}
				output, err := dbMock.Scan(context.TODO(), s)
				Expect(err).To(BeNil())
				Expect(len(output.Items)).To(Equal(2))
				out1, _ := NewTestValBFromAV(output.Items[0])
				Expect(out1).To(Equal(&valb1))
				out2, _ := NewTestValBFromAV(output.Items[1])
				Expect(out2).To(Equal(&valb2))
			})
		})

		Context("Advanced Query Features", func() {
			var (
				dbMock                    *mock.DynamoDBMock
				table3                    string
				tsData1, tsData2, tsData3 map[string]types.AttributeValue
			)

			BeforeEach(func() {
				dbMock = mock.NewDynamoDBMock()
				table3 = "timeseries_table"

				// Create table with partition key and sort key (like raw_ts_data)
				dbMock.AddTable(table3, "node_key_dt", "timestamp")

				// Create test data with different timestamps
				tsData1 = map[string]types.AttributeValue{
					"node_key_dt": &types.AttributeValueMemberS{Value: "device1.temp.float"},
					"timestamp":   &types.AttributeValueMemberN{Value: "1640995200"},
					"value":       &types.AttributeValueMemberN{Value: "25.5"},
				}
				tsData2 = map[string]types.AttributeValue{
					"node_key_dt": &types.AttributeValueMemberS{Value: "device1.temp.float"},
					"timestamp":   &types.AttributeValueMemberN{Value: "1640995260"},
					"value":       &types.AttributeValueMemberN{Value: "26.0"},
				}
				tsData3 = map[string]types.AttributeValue{
					"node_key_dt": &types.AttributeValueMemberS{Value: "device1.temp.float"},
					"timestamp":   &types.AttributeValueMemberN{Value: "1640995320"},
					"value":       &types.AttributeValueMemberN{Value: "24.8"},
				}

				// Put test data
				dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
					TableName: &table3,
					Item:      tsData1,
				})
				dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
					TableName: &table3,
					Item:      tsData2,
				})
				dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
					TableName: &table3,
					Item:      tsData3,
				})
			})

			It("should respect ScanIndexForward=false (descending order)", func() {
				keyCondition := expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float"))
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ScanIndexForward:          aws.Bool(false), // Latest first
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(3))

				// Should be in descending order by timestamp (latest first)
				Expect(result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995320"))
				Expect(result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995260"))
				Expect(result.Items[2]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995200"))
			})

			It("should respect ScanIndexForward=true (ascending order)", func() {
				keyCondition := expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float"))
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ScanIndexForward:          aws.Bool(true), // Earliest first
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(3))

				// Should be in ascending order by timestamp (earliest first)
				Expect(result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995200"))
				Expect(result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995260"))
				Expect(result.Items[2]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995320"))
			})

			It("should respect Limit parameter", func() {
				keyCondition := expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float"))
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ScanIndexForward:          aws.Bool(false), // Latest first
					Limit:                     aws.Int32(2),    // Only return 2 items
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(2))

				// Should return the 2 latest items
				Expect(result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995320"))
				Expect(result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995260"))
			})

			It("should handle BETWEEN operations", func() {
				keyCondition := expression.KeyAnd(
					expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float")),
					expression.KeyBetween(expression.Key("timestamp"), expression.Value(1640995200), expression.Value(1640995260)),
				)
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(2))

				// Should return items within the range
				timestamps := []string{
					result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value,
					result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value,
				}
				Expect(timestamps).To(ContainElements("1640995200", "1640995260"))
			})

			It("should handle GreaterThanEqual operations", func() {
				keyCondition := expression.KeyAnd(
					expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float")),
					expression.KeyGreaterThanEqual(expression.Key("timestamp"), expression.Value(1640995260)),
				)
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(2))

				// Should return items >= 1640995260
				timestamps := []string{
					result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value,
					result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value,
				}
				Expect(timestamps).To(ContainElements("1640995260", "1640995320"))
			})

			It("should handle LessThanEqual operations", func() {
				keyCondition := expression.KeyAnd(
					expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float")),
					expression.KeyLessThanEqual(expression.Key("timestamp"), expression.Value(1640995260)),
				)
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(2))

				// Should return items <= 1640995260
				timestamps := []string{
					result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value,
					result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value,
				}
				Expect(timestamps).To(ContainElements("1640995200", "1640995260"))
			})

			It("should combine ScanIndexForward, Limit, and range conditions", func() {
				keyCondition := expression.KeyAnd(
					expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float")),
					expression.KeyGreaterThanEqual(expression.Key("timestamp"), expression.Value(1640995200)),
				)
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					ScanIndexForward:          aws.Bool(false), // Latest first
					Limit:                     aws.Int32(1),    // Only return 1 item
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(1))

				// Should return the latest item that matches the condition
				Expect(result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995320"))
			})

			It("should preserve insertion order when ScanIndexForward is not specified", func() {
				keyCondition := expression.KeyEqual(expression.Key("node_key_dt"), expression.Value("device1.temp.float"))
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table3,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
					// Note: ScanIndexForward is NOT specified
				}

				result, err := dbMock.Query(context.TODO(), query)
				Expect(err).To(BeNil())
				Expect(len(result.Items)).To(Equal(3))

				// Should return items in insertion order (not sorted by timestamp)
				// Since we inserted: 1640995200, 1640995260, 1640995320
				// We expect the same order back
				Expect(result.Items[0]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995200"))
				Expect(result.Items[1]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995260"))
				Expect(result.Items[2]["timestamp"].(*types.AttributeValueMemberN).Value).To(Equal("1640995320"))
			})
		})

		Context("Profile Operations", func() {
			var (
				dbMock *mock.DynamoDBMock
				table1 string
				table2 string
				val1   TestValA
				val2   TestValB
			)

			BeforeEach(func() {
				dbMock = mock.NewDynamoDBMock()
				table1 = "table1"
				table2 = "table2"
				val1 = TestValA{Id: "id1", Val: 1}
				val2 = TestValB{Name: "name1", Count: 101, Data: "data101"}

				dbMock.AddTable(table1, "id", "")
				dbMock.AddTable(table2, "name", "count")
			})

			It("should track read operations", func() {
				// Perform some read operations
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				dbMock.GetItem(context.TODO(), getval1)

				expectedGetItem := mock.ProfileOperation{Name: "GetItem", Details: "id1:", Size: 0}
				profile := dbMock.ProfileGet()
				Expect(profile.Accesses[table1].ReadCount).To(Equal(1))
				Expect(profile.Accesses[table1].WriteCount).To(Equal(0))
				Expect(profile.Accesses[table1].Operations).To(ContainElement(expectedGetItem))
			})

			It("should track write operations", func() {
				// Perform some write operations
				putval1, _ := ToPutItemInput(table1, val1)
				dbMock.PutItem(context.TODO(), putval1)

				expectedPutItem := mock.ProfileOperation{Name: "PutItem", Details: "id1:", Size: 0}
				profile := dbMock.ProfileGet()
				Expect(profile.Accesses[table1].ReadCount).To(Equal(0))
				Expect(profile.Accesses[table1].WriteCount).To(Equal(1))
				Expect(profile.Accesses[table1].Operations).To(ContainElement(expectedPutItem))
			})

			It("should track multiple operations on multiple tables", func() {
				// Perform operations on table1
				putval1, _ := ToPutItemInput(table1, val1)
				dbMock.PutItem(context.TODO(), putval1)
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				dbMock.GetItem(context.TODO(), getval1)

				// Perform operations on table2
				putval2, _ := ToPutItemInput(table2, val2)
				dbMock.PutItem(context.TODO(), putval2)

				profile := dbMock.ProfileGet()

				expectedPutItem1 := mock.ProfileOperation{Name: "PutItem", Details: "id1:", Size: 0}
				expectedGetItem := mock.ProfileOperation{Name: "GetItem", Details: "id1:", Size: 0}
				// Check table1 operations
				Expect(profile.Accesses[table1].ReadCount).To(Equal(1))
				Expect(profile.Accesses[table1].WriteCount).To(Equal(1))
				Expect(profile.Accesses[table1].Operations).To(ContainElements(expectedPutItem1, expectedGetItem))

				expectedPutItem2 := mock.ProfileOperation{Name: "PutItem", Details: "name1:101", Size: 0}
				// Check table2 operations
				Expect(profile.Accesses[table2].ReadCount).To(Equal(0))
				Expect(profile.Accesses[table2].WriteCount).To(Equal(1))
				Expect(profile.Accesses[table2].Operations).To(ContainElement(expectedPutItem2))
			})

			It("should reset profile statistics", func() {
				// Perform some operations
				putval1, _ := ToPutItemInput(table1, val1)
				dbMock.PutItem(context.TODO(), putval1)
				getval1, _ := ToGetItemInput(table1, map[string]string{"id": val1.Id})
				dbMock.GetItem(context.TODO(), getval1)

				// Verify operations were tracked
				profile := dbMock.ProfileGet()
				Expect(profile.Accesses[table1].ReadCount).To(Equal(1))
				Expect(profile.Accesses[table1].WriteCount).To(Equal(1))

				// Reset profile
				dbMock.ProfileReset()

				// Verify counts were reset
				profile = dbMock.ProfileGet()
				Expect(profile.Accesses).To(BeEmpty())
			})

			It("should track batch operations correctly", func() {
				// Prepare batch write
				putval1, _ := ToPutItemInput(table1, val1)
				putval2, _ := ToPutItemInput(table2, val2)

				batchWrite := &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]types.WriteRequest{
						table1: {{PutRequest: &types.PutRequest{Item: putval1.Item}}},
						table2: {{PutRequest: &types.PutRequest{Item: putval2.Item}}},
					},
				}

				// Perform batch write
				dbMock.BatchWriteItem(context.TODO(), batchWrite)

				profile := dbMock.ProfileGet()

				expectedPutItem1 := mock.ProfileOperation{Name: "PutItem", Details: "id1:", Size: 0}
				// Check table1 operations
				Expect(profile.Accesses[table1].WriteCount).To(Equal(1))
				Expect(profile.Accesses[table1].Operations).To(ContainElement(expectedPutItem1))

				expectedPutItem2 := mock.ProfileOperation{Name: "PutItem", Details: "name1:101", Size: 0}
				// Check table2 operations
				Expect(profile.Accesses[table2].WriteCount).To(Equal(1))
				Expect(profile.Accesses[table2].Operations).To(ContainElement(expectedPutItem2))
			})

			It("should track query operations", func() {
				// Prepare and execute a query
				keyCondition := expression.KeyEqual(expression.Key("id"), expression.Value(val1.Id))
				expr, _ := expression.NewBuilder().WithKeyCondition(keyCondition).Build()

				query := &dynamodb.QueryInput{
					TableName:                 &table1,
					KeyConditionExpression:    expr.KeyCondition(),
					ExpressionAttributeNames:  expr.Names(),
					ExpressionAttributeValues: expr.Values(),
				}

				dbMock.Query(context.TODO(), query)

				expectedQuery := mock.ProfileOperation{Name: "Query", Details: "KeyConditionExpression: #0 = :0 Key/Value: #0:id :0:id1 ", Size: 0}
				profile := dbMock.ProfileGet()
				Expect(profile.Accesses[table1].ReadCount).To(Equal(1))
				Expect(profile.Accesses[table1].Operations).To(ContainElement(expectedQuery))
			})
		})
	})
})

var _ = AfterSuite(func() {
	fmt.Fprintf(timingFile, "\n---Set the environment variable TEST_DYNAMODB_PROFILE_DETAILS=true to see the DB Operation details ---\n")
	fmt.Fprintf(timingFile, "\n---Set the environment variable RLOG to {\"level\":\"info\"} to configure logging level (trace < debug < info < warn) ---\n")
	fmt.Fprintf(timingFile, "-----------------------------\n\n")
	timingFile.Close()
})

var _ = Describe("Result fidelity", func() {
	const (
		table     = "fidelity"
		index     = "fidelity-by-kind"
		partition = "p1"
	)

	var dbMock *mock.DynamoDBMock

	put := func(sk, keep string) {
		_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
			TableName: aws.String(table),
			Item: map[string]types.AttributeValue{
				"pk": avS(partition), "sk": avS(sk), "keep": avS(keep),
				"kind": avS("alert"), "bulk": avS("payload"),
			},
		})
		Expect(err).ToNot(HaveOccurred())
	}

	partitionQuery := func(in *dynamodb.QueryInput) *dynamodb.QueryOutput {
		b := expression.NewBuilder().WithKeyCondition(expression.Key("pk").Equal(expression.Value(partition)))
		if in.FilterExpression != nil {
			b = b.WithFilter(expression.Equal(expression.Name("keep"), expression.Value("yes")))
		}
		expr, err := b.Build()
		Expect(err).ToNot(HaveOccurred())
		in.KeyConditionExpression = expr.KeyCondition()
		in.FilterExpression = expr.Filter()
		in.ExpressionAttributeNames = expr.Names()
		in.ExpressionAttributeValues = expr.Values()
		out, err := dbMock.Query(context.TODO(), in)
		Expect(err).ToNot(HaveOccurred())
		return out
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "pk", "sk")
		Expect(dbMock.AddSecondaryIndex(index, table, "kind", "sk")).To(Succeed())
		for _, sk := range []string{"s01", "s02", "s03", "s04"} {
			put(sk, "no")
		}
		put("s05", "yes")
	})

	// A returned item is a copy in the real service. Handing back the stored map lets a caller's edit reach into the table, so a test can mutate data it only meant to read and code that corrupts a shared map still looks correct.
	Describe("returned items are detached from the store", func() {
		It("does not let a mutated GetItem result reach the store", func() {
			key := map[string]types.AttributeValue{"pk": avS(partition), "sk": avS("s01")}
			got, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{TableName: aws.String(table), Key: key})
			Expect(err).ToNot(HaveOccurred())
			got.Item["injected"] = avS("x")
			got.Item["keep"] = avS("tampered")

			again, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{TableName: aws.String(table), Key: key})
			Expect(err).ToNot(HaveOccurred())
			Expect(again.Item).ToNot(HaveKey("injected"))
			Expect(again.Item["keep"]).To(Equal(avS("no")))
		})

		It("does not let a mutated Query result reach the store", func() {
			out := partitionQuery(&dynamodb.QueryInput{TableName: aws.String(table)})
			out.Items[0]["injected"] = avS("x")

			again := partitionQuery(&dynamodb.QueryInput{TableName: aws.String(table)})
			Expect(again.Items[0]).ToNot(HaveKey("injected"))
		})

		It("does not let the item passed to PutItem be changed afterwards", func() {
			item := map[string]types.AttributeValue{"pk": avS(partition), "sk": avS("s99"), "keep": avS("no")}
			_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{TableName: aws.String(table), Item: item})
			Expect(err).ToNot(HaveOccurred())
			item["keep"] = avS("tampered")

			got, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{
				TableName: aws.String(table),
				Key:       map[string]types.AttributeValue{"pk": avS(partition), "sk": avS("s99")},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Item["keep"]).To(Equal(avS("no")))
		})
	})

	// Limit caps the rows DynamoDB evaluates, not the rows that survive the filter, so a filtered page legitimately comes back short (even empty) while still carrying a cursor. A mock that filters first always returns a full page and hides the loop the caller needs.
	Describe("Limit and ScannedCount", func() {
		It("returns a short page and a cursor when the filter rejects the evaluated rows", func() {
			out := partitionQuery(&dynamodb.QueryInput{
				TableName: aws.String(table), Limit: aws.Int32(2), FilterExpression: aws.String("placeholder"),
			})
			Expect(out.Items).To(BeEmpty())
			Expect(out.Count).To(BeEquivalentTo(0))
			Expect(out.ScannedCount).To(BeEquivalentTo(2))
			Expect(out.LastEvaluatedKey).ToNot(BeEmpty())
		})

		It("reaches the matching row by following the cursor", func() {
			var seen int32
			var startKey map[string]types.AttributeValue
			var matched []map[string]types.AttributeValue
			for {
				out := partitionQuery(&dynamodb.QueryInput{
					TableName: aws.String(table), Limit: aws.Int32(2),
					FilterExpression: aws.String("placeholder"), ExclusiveStartKey: startKey,
				})
				seen += out.ScannedCount
				matched = append(matched, out.Items...)
				if len(out.LastEvaluatedKey) == 0 {
					break
				}
				startKey = out.LastEvaluatedKey
			}
			Expect(seen).To(BeEquivalentTo(5))
			Expect(matched).To(HaveLen(1))
			Expect(matched[0]["sk"]).To(Equal(avS("s05")))
		})

		It("sets ScannedCount to the row count on an unfiltered query", func() {
			out := partitionQuery(&dynamodb.QueryInput{TableName: aws.String(table)})
			Expect(out.ScannedCount).To(BeEquivalentTo(5))
			Expect(out.Count).To(BeEquivalentTo(5))
		})
	})

	// An index only carries the attributes it projects. Returning the whole base-table row lets a test read an attribute that would come back empty in production.
	Describe("index projections", func() {
		It("returns only key attributes from a KEYS_ONLY index", func() {
			Expect(dbMock.SetIndexProjection(index, types.ProjectionTypeKeysOnly)).To(Succeed())
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("kind").Equal(expression.Value("alert"))).Build()
			Expect(err).ToNot(HaveOccurred())

			out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), IndexName: aws.String(index),
				KeyConditionExpression:   expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Items).ToNot(BeEmpty())
			Expect(out.Items[0]).To(HaveKey("pk"))
			Expect(out.Items[0]).To(HaveKey("sk"))
			Expect(out.Items[0]).To(HaveKey("kind"))
			Expect(out.Items[0]).ToNot(HaveKey("bulk"))
			Expect(out.Items[0]).ToNot(HaveKey("keep"))
		})

		It("returns key attributes plus the included ones from an INCLUDE index", func() {
			Expect(dbMock.SetIndexProjection(index, types.ProjectionTypeInclude, "keep")).To(Succeed())
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("kind").Equal(expression.Value("alert"))).Build()
			Expect(err).ToNot(HaveOccurred())

			out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), IndexName: aws.String(index),
				KeyConditionExpression:   expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Items[0]).To(HaveKey("keep"))
			Expect(out.Items[0]).ToNot(HaveKey("bulk"))
		})
	})

	Describe("projection expressions on point reads", func() {
		It("returns only the projected attributes from GetItem", func() {
			out, err := dbMock.GetItem(context.TODO(), &dynamodb.GetItemInput{
				TableName:            aws.String(table),
				Key:                  map[string]types.AttributeValue{"pk": avS(partition), "sk": avS("s01")},
				ProjectionExpression: aws.String("sk, keep"),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Item).To(HaveKey("sk"))
			Expect(out.Item).To(HaveKey("keep"))
			Expect(out.Item).ToNot(HaveKey("bulk"))
		})

		It("returns only the projected attributes from BatchGetItem", func() {
			out, err := dbMock.BatchGetItem(context.TODO(), &dynamodb.BatchGetItemInput{
				RequestItems: map[string]types.KeysAndAttributes{
					table: {
						Keys:                 []map[string]types.AttributeValue{{"pk": avS(partition), "sk": avS("s01")}},
						ProjectionExpression: aws.String("sk, keep"),
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Responses[table]).To(HaveLen(1))
			Expect(out.Responses[table][0]).To(HaveKey("keep"))
			Expect(out.Responses[table][0]).ToNot(HaveKey("bulk"))
		})

		It("omits a key that does not exist rather than returning a nil entry", func() {
			out, err := dbMock.BatchGetItem(context.TODO(), &dynamodb.BatchGetItemInput{
				RequestItems: map[string]types.KeysAndAttributes{
					table: {Keys: []map[string]types.AttributeValue{{"pk": avS(partition), "sk": avS("nope")}}},
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Responses[table]).To(BeEmpty())
		})
	})

	Describe("request validation", func() {
		It("rejects a consistent read on a secondary index", func() {
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("kind").Equal(expression.Value("alert"))).Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), IndexName: aws.String(index), ConsistentRead: aws.Bool(true),
				KeyConditionExpression:   expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		})

		It("allows a consistent read on the base table", func() {
			out := partitionQuery(&dynamodb.QueryInput{TableName: aws.String(table), ConsistentRead: aws.Bool(true)})
			Expect(out.Items).To(HaveLen(5))
		})

		It("rejects a query whose key condition does not pin the partition key", func() {
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("sk").BeginsWith("s0")).Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), KeyConditionExpression: expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		})

		It("rejects a query with no key condition at all", func() {
			_, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{TableName: aws.String(table)})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		})

		// Only "=" fixes a partition. A range operator on it would span partitions, which is a Scan.
		It("rejects a partition key compared with anything but equals", func() {
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("pk").GreaterThan(expression.Value("p0"))).Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), KeyConditionExpression: expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		})

		// Either branch of an OR leaves the partition unfixed, so pinning it in one is not pinning it.
		It("rejects a partition key pinned only inside an OR", func() {
			_, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName:              aws.String(table),
				KeyConditionExpression: aws.String("pk = :a OR pk = :b"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":a": avS(partition), ":b": avS("p2"),
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ValidationException"))
		})

		// The partition that must be pinned is the index's, not the table's. Validating against the base table's key would let this through and quietly read the whole index.
		It("rejects an index query that pins the base table's partition key", func() {
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("pk").Equal(expression.Value(partition))).Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), IndexName: aws.String(index),
				KeyConditionExpression:   expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kind"))
		})

		It("accepts an index query that pins the index partition key", func() {
			expr, err := expression.NewBuilder().
				WithKeyCondition(expression.Key("kind").Equal(expression.Value("alert"))).Build()
			Expect(err).ToNot(HaveOccurred())

			out, err := dbMock.Query(context.TODO(), &dynamodb.QueryInput{
				TableName: aws.String(table), IndexName: aws.String(index),
				KeyConditionExpression:   expr.KeyCondition(),
				ExpressionAttributeNames: expr.Names(), ExpressionAttributeValues: expr.Values(),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Items).To(HaveLen(5))
		})
	})

	It("returns a count without items for Select COUNT on a scan", func() {
		out, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{
			TableName: aws.String(table), Select: types.SelectCount,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.Count).To(BeEquivalentTo(5))
		Expect(out.Items).To(BeEmpty())
	})
})

// A Scan returns at most 1 MB per call, so a caller that ignores LastEvaluatedKey silently reads a prefix of the table. Returning everything in one page hides that, and the loops written to handle it never run.
var _ = Describe("Scan pagination", func() {
	const (
		table     = "scan-paged"
		partition = "p1"
		total     = 5
	)

	var dbMock *mock.DynamoDBMock

	sortKeys := func(items []map[string]types.AttributeValue) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item["sk"].(*types.AttributeValueMemberS).Value)
		}
		return out
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "pk", "sk")
		for i := 1; i <= total; i++ {
			_, err := dbMock.PutItem(context.TODO(), &dynamodb.PutItemInput{
				TableName: aws.String(table),
				Item: map[string]types.AttributeValue{
					"pk": avS(partition), "sk": avS(fmt.Sprintf("s%02d", i)),
					"keep": avS(map[bool]string{true: "yes", false: "no"}[i == total]),
				},
			})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("pages through the table without repeating or dropping a row", func() {
		dbMock.MaxPageItems = 2
		var seen []string
		var startKey map[string]types.AttributeValue
		for {
			out, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{
				TableName: aws.String(table), ExclusiveStartKey: startKey,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(out.Items)).To(BeNumerically("<=", 2))
			seen = append(seen, sortKeys(out.Items)...)
			if len(out.LastEvaluatedKey) == 0 {
				break
			}
			startKey = out.LastEvaluatedKey
			Expect(len(seen)).To(BeNumerically("<=", total), "cursor is not advancing")
		}
		Expect(seen).To(Equal([]string{"s01", "s02", "s03", "s04", "s05"}))
	})

	It("resumes after a cursor whose row was deleted", func() {
		page1, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{
			TableName: aws.String(table), Limit: aws.Int32(2),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(sortKeys(page1.Items)).To(Equal([]string{"s01", "s02"}))

		_, err = dbMock.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
			TableName: aws.String(table),
			Key:       map[string]types.AttributeValue{"pk": avS(partition), "sk": avS("s02")},
		})
		Expect(err).ToNot(HaveOccurred())

		page2, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{
			TableName: aws.String(table), Limit: aws.Int32(2), ExclusiveStartKey: page1.LastEvaluatedKey,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(sortKeys(page2.Items)).To(Equal([]string{"s03", "s04"}))
	})

	It("counts rows examined rather than rows returned when a filter rejects the page", func() {
		out, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{
			TableName: aws.String(table), Limit: aws.Int32(2),
			FilterExpression:          aws.String("keep = :k"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":k": avS("yes")},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.Items).To(BeEmpty())
		Expect(out.ScannedCount).To(BeEquivalentTo(2))
		Expect(out.LastEvaluatedKey).ToNot(BeEmpty())
	})
})

// DynamoDB sheds part of a batch under load and hands back UnprocessedItems for the caller to resubmit. A mock that always applies everything never runs the retry loop callers write for it.
var _ = Describe("BatchWriteItem unprocessed items", func() {
	const table = "batch-table"

	var dbMock *mock.DynamoDBMock

	writeRequests := func(count int) []types.WriteRequest {
		out := make([]types.WriteRequest, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, types.WriteRequest{PutRequest: &types.PutRequest{
				Item: map[string]types.AttributeValue{"pk": avS(fmt.Sprintf("k%02d", i))},
			}})
		}
		return out
	}

	BeforeEach(func() {
		dbMock = mock.NewDynamoDBMock()
		dbMock.AddTable(table, "pk", "")
	})

	It("returns the tail of the batch as unprocessed and applies the rest", func() {
		dbMock.NextBatchWriteUnprocessedCount = 2

		out, err := dbMock.BatchWriteItem(context.TODO(), &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{table: writeRequests(3)},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.UnprocessedItems[table]).To(HaveLen(2))

		stored, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{TableName: aws.String(table)})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.Items).To(HaveLen(1))
	})

	// The hook decrements rather than clearing, so each retry sheds less than the last and a caller that keeps resubmitting drains. That is what separates "retries until done" from "gave up after one go".
	It("drains once the caller keeps resubmitting what came back", func() {
		dbMock.NextBatchWriteUnprocessedCount = 2

		pending := map[string][]types.WriteRequest{table: writeRequests(3)}
		rounds := 0
		for len(pending) > 0 {
			out, err := dbMock.BatchWriteItem(context.TODO(), &dynamodb.BatchWriteItemInput{RequestItems: pending})
			Expect(err).ToNot(HaveOccurred())
			pending = out.UnprocessedItems
			rounds++
			Expect(rounds).To(BeNumerically("<=", 4), "hook is not draining")
		}

		stored, err := dbMock.Scan(context.TODO(), &dynamodb.ScanInput{TableName: aws.String(table)})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.Items).To(HaveLen(3))
		Expect(rounds).To(BeNumerically(">", 1), "the first call should have held some back")
	})
})
