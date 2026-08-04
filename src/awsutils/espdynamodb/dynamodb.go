// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package espdynamodb

import (
	"errors"
	"github.com/espressif/esp-cloud-common/go/rbac/constants"
	"github.com/espressif/esp-cloud-common/go/rbac/pkg/logger"
	"github.com/espressif/esp-rainmaker-neo/src/utils/convert"
	"strconv"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/luraim/fun"
)

func GetItemExistsCondition(query DBItem) expression.ConditionBuilder {
	hashKey := query.GetHKey()
	condition := expression.Name(hashKey).AttributeExists()
	if rangeKey := query.GetRKey(); rangeKey != "" {
		condition = condition.And(expression.Name(rangeKey).AttributeExists())
	}
	return condition
}

func GetItemNotExistsCondition(query DBItem) expression.ConditionBuilder {
	hashKey := query.GetHKey()
	condition := expression.Name(hashKey).AttributeNotExists()
	if rangeKey := query.GetRKey(); rangeKey != "" {
		condition = condition.And(expression.Name(rangeKey).AttributeNotExists())
	}
	return condition
}

// DbCreateItem creates an item in the database using condition builder
func (d *EspDB) DbCreateItem(tableName string, v DBItem) error {
	av, err := attributevalue.MarshalMap(v)
	if err != nil {
		return err
	}

	// Build condition expression using expression builder
	expr, err := expression.NewBuilder().WithCondition(GetItemNotExistsCondition(v)).Build()
	if err != nil {
		return err
	}

	_, err = d.DB.PutItem(d.Ctx.Context, &dynamodb.PutItemInput{
		TableName:                 aws.String(tableName),
		Item:                      av,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

// DbGetItem gets a single item from the database
func (d *EspDB) DbGetItem(tableName string, query DBItem, out interface{}) error {
	av, err := attributevalue.MarshalMap(query)
	if err != nil {
		return err
	}

	result, err := d.DB.GetItem(d.Ctx.Context, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key:       av,
	})
	if err != nil {
		return err
	}
	return attributevalue.UnmarshalMap(result.Item, out)
}

// dbQueryCount returns the count of the rows returned by the database
func (d *EspDB) dbQueryCount(input DbQueryCountInput) (int32, map[string]types.AttributeValue, error) {
	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(input.TableName),
		KeyConditionExpression:    input.Expr.KeyCondition(),
		ExpressionAttributeNames:  input.Expr.Names(),
		ExpressionAttributeValues: input.Expr.Values(),
		FilterExpression:          input.Expr.Filter(),
		Select:                    types.SelectCount,
	}

	if input.StartID != nil {
		queryInput.ExclusiveStartKey = input.StartID
	}

	if input.IndexName != constants.EMPTY_STRING {
		queryInput.IndexName = &input.IndexName
	}

	if input.ConsistentRead {
		queryInput.ConsistentRead = aws.Bool(true)
	}

	result, err := d.DB.Query(d.Ctx.Context, queryInput)
	if err != nil {
		return 0, nil, err
	}
	return result.Count, result.LastEvaluatedKey, nil
}

func (d *EspDB) DbQueryCountLoop(input DbQueryCountInput) (int32, error) {
	var finalCnt int32
	for {
		cnt, nextKey, err := d.dbQueryCount(input)
		if err != nil {
			return 0, err
		}
		finalCnt += cnt
		if nextKey != nil {
			input.StartID = nextKey
		} else {
			break
		}
	}
	return finalCnt, nil
}

type DbUpdateItemInput struct {
	TableName string
	Update    expression.UpdateBuilder
	Query     DBItem
	Condition expression.ConditionBuilder
}

// DbUpdateItem updates the items in the database
func (d *EspDB) DbUpdateItem(input DbUpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
	av, err := attributevalue.MarshalMap(input.Query)
	if err != nil {
		return nil, err
	}

	keys := make(map[string]types.AttributeValue)
	keys[input.Query.GetHKey()] = av[input.Query.GetHKey()]
	if rangeKey := input.Query.GetRKey(); rangeKey != "" {
		keys[rangeKey] = av[rangeKey]
	}

	var conditionUpdate = GetItemExistsCondition(input.Query)
	if !utils.IsEmpty(input.Condition) {
		conditionUpdate = input.Condition.And(GetItemExistsCondition(input.Query))
	}

	expr, err := expression.NewBuilder().
		WithCondition(conditionUpdate).
		WithUpdate(input.Update).
		Build()
	if err != nil {
		return nil, err
	}

	return d.DB.UpdateItem(d.Ctx.Context, &dynamodb.UpdateItemInput{
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Key:                       keys,
		TableName:                 aws.String(input.TableName),
		UpdateExpression:          expr.Update(),
		ReturnValues:              types.ReturnValueUpdatedNew,
		ConditionExpression:       expr.Condition(),
	})
}

// DbDeleteItem deletes the items in the database
func (d *EspDB) DbDeleteItem(tableName string, query DBItem) error {
	av, err := attributevalue.MarshalMap(query)
	if err != nil {
		return err
	}

	expr, err := expression.NewBuilder().WithCondition(GetItemExistsCondition(query)).Build()
	if err != nil {
		return err
	}
	_, err = d.DB.DeleteItem(d.Ctx.Context, &dynamodb.DeleteItemInput{
		TableName:                 aws.String(tableName),
		Key:                       av,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

// dbBatchGetItemByExp gets the items from the requested tables as per the conditions for each of them.
func (d *EspDB) dbBatchGetItemByExp(input dbBatchGetItemByExpInput) error {
	result, err := d.DB.BatchGetItem(d.Ctx.Context, &dynamodb.BatchGetItemInput{
		RequestItems: map[string]types.KeysAndAttributes{
			input.TableName: {
				Keys:                     input.Keys,
				ProjectionExpression:     input.Expr.Projection(),
				ExpressionAttributeNames: input.Expr.Names(),
			},
		},
	})
	if err != nil {
		return err
	}

	// process left items to batch get
	for {
		if len(result.UnprocessedKeys) > 0 {
			logger.LogDebug("Getting unprocessed keys " + strconv.Itoa(len(result.UnprocessedKeys[input.TableName].Keys)))

			unprocessedInput := &dynamodb.BatchGetItemInput{
				RequestItems: result.UnprocessedKeys,
			}
			unprocessedResult, errUnprocess := d.DB.BatchGetItem(d.Ctx.Context, unprocessedInput)
			if errUnprocess != nil {
				return errUnprocess
			}
			// Appending unprocessed results with results
			result.Responses[input.TableName] = append(result.Responses[input.TableName], unprocessedResult.Responses[input.TableName]...)
			result.UnprocessedKeys = unprocessedResult.UnprocessedKeys
		}

		if len(result.UnprocessedKeys) == 0 {
			break
		}
	}
	return attributevalue.UnmarshalListOfMaps(result.Responses[input.TableName], input.Out)
}

func DbBatchGetItemLoop[T any](input DbBatchGetItemLoopInput) ([]T, error) {
	var stIndex, edIndex int
	var ret bool
	var out []T
	keysLen := len(input.Keys)
	for {
		stIndex = edIndex
		edIndex += DYNAMODB_BATCH_GET_LIMIT
		if edIndex >= keysLen {
			edIndex = keysLen
			ret = true
		}
		var tempOut []T
		err := input.DBConn.dbBatchGetItemByExp(dbBatchGetItemByExpInput{
			TableName: input.TableName,
			Expr:      input.Expr,
			Keys:      input.Keys[stIndex:edIndex],
			Out:       &tempOut,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, tempOut...)
		if ret {
			return out, nil
		}
	}
}

func DbBatchPutItem[T any](d *EspDB, tableName string, v []T) error {
	batches := fun.Chunked(v, DYNAMODB_BATCH_WRITE_LIMIT)

	for _, batchItems := range batches {
		writeRequests := make([]types.WriteRequest, 0, DYNAMODB_BATCH_WRITE_LIMIT)
		for _, item := range batchItems {
			av, errMarshal := attributevalue.MarshalMap(item)
			if errMarshal != nil {
				return errMarshal
			}
			batchPutRequest := types.PutRequest{Item: av}
			writeRequests = append(writeRequests, types.WriteRequest{PutRequest: &batchPutRequest})
		}
		// Writing batches to the DB table
		result, errBatchWrite := d.DB.BatchWriteItem(d.Ctx.Context, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{tableName: writeRequests},
		})
		if errBatchWrite != nil {
			return errBatchWrite
		}
		// process left items to batch write
		unprocessedItems := result.UnprocessedItems[tableName]
		for {
			if len(unprocessedItems) > 0 {
				logger.LogDebug("Getting unprocessed items " + strconv.Itoa(len(unprocessedItems)))
				unprocessedResult, errUnProcess := d.DB.BatchWriteItem(d.Ctx.Context, &dynamodb.BatchWriteItemInput{
					RequestItems: map[string][]types.WriteRequest{tableName: unprocessedItems},
				})
				if errUnProcess != nil {
					return errUnProcess
				}
				unprocessedItems = unprocessedResult.UnprocessedItems[tableName]
			}
			if len(unprocessedItems) == 0 {
				break
			}
		}
	}

	return nil
}

// DbBatchDeleteItem deletes maximum 25 items in DB table
// It takes list of items with maximum length 25 as an interface v
func (d *EspDB) DbBatchDeleteItem(tableName string, v interface{}) error {
	// Converting interface to slice of go structs
	vSlice, errInterfaceToSlice := convert.InterfaceToSlice(v)
	if errInterfaceToSlice != nil {
		return errInterfaceToSlice
	}
	// Creating batch write input
	var writeRequests []types.WriteRequest
	batchCount := 0
	writeRequests = make([]types.WriteRequest, len(vSlice))
	for _, item := range vSlice {
		av, errMarshal := attributevalue.MarshalMap(item)
		if errMarshal != nil {
			return errMarshal
		}
		batchDeleteRequest := types.DeleteRequest{
			Key: av,
		}
		writeRequestInput := types.WriteRequest{DeleteRequest: &batchDeleteRequest}
		writeRequests[batchCount] = writeRequestInput
		batchCount++
		if batchCount == 25 {
			break
		}
	}
	// Writing batches to the DB table
	result, errBatchWrite := d.DB.BatchWriteItem(d.Ctx.Context, &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			tableName: writeRequests,
		},
	})
	if errBatchWrite != nil {
		return errBatchWrite
	}

	// process left items to batch write
	unprocessedItmes := result.UnprocessedItems[tableName]
	for {
		if len(unprocessedItmes) > 0 {
			logger.LogDebug("Getting unprocessed items " + strconv.Itoa(len(unprocessedItmes)))
			unprocessedResult, errUnprocess := d.DB.BatchWriteItem(d.Ctx.Context, &dynamodb.BatchWriteItemInput{
				RequestItems: map[string][]types.WriteRequest{
					tableName: unprocessedItmes,
				},
			})
			if errUnprocess != nil {
				return errUnprocess
			}
			unprocessedItmes = unprocessedResult.UnprocessedItems[tableName]
		}
		if len(unprocessedItmes) == 0 {
			break
		}
	}

	return nil
}

/*
DbQueryWithLoop ...
Query based on some conditions on the composite keys.

This function is a generalized function to query a table until required.
AppendAndReInitialize Function controls the flow of the query.
Examples:
 1. To get all the elements for the matching condition
    appendAndReInitialize := func(out, fOut interface{}, nxtID map[string]*dynamodb.AttributeValue, count int64) (map[string]*dynamodb.AttributeValue, bool) {
    if out != nil && fOut != nil {
    temp := out.((*[]models.NodeInfo))
    fTemp := fOut.((*[]models.NodeInfo))
    *fTemp = append(*fTemp, *temp...)
    temp = new([]models.NodeInfo)
    }
    return nxtID, true
    }
 2. When filter condition is present and you want to query for certain number of records
    appendAndReInitialize := func(out, fOut interface{}, nxtID map[string]*dynamodb.AttributeValue, count int64) (map[string]*dynamodb.AttributeValue, bool) {
    if out != nil && fOut != nil {
    tempResult := out.(*[]models.FileDetails)
    finalResult := fOut.(*[]models.FileDetails)
    fTempLen := len(*finalResult)
    tempLen := len(*tempResult)
    if tempLen+fTempLen > int(count) {
    if count != 0 {
    *finalResult = append(*finalResult, (*tempResult)[0:count-int64(fTempLen)]...)
    } else {
    *finalResult = append(*finalResult, *tempResult...) // If count is 0(i.e ∞), add all of temp to fTemp, instead of slicing.
    }
    nxtID[constants.USER_ID] = &dynamodb.AttributeValue{S: aws.String((*finalResult)[len(*finalResult)-1].UserId)}
    nxtID["file_id"] = &dynamodb.AttributeValue{S: aws.String((*finalResult)[len(*finalResult)-1].FileId)}
    return nxtID, false // If we get more records than required, take as many as required, return the lastEvaluatedKey and don't paginate further.
    } else {
    *finalResult = append(*finalResult, *tempResult...)
    fTempLen += tempLen
    if fTempLen == int(count) || nxtID == nil {
    return nxtID, false // If desired no. of records are retrieved OR there are no more records at DB, return lastEvaluatedKey and don't paginate further.
    }
    *tempResult = []models.FileDetails{}
    return nxtID, true // If there still are more records to retrieve, return lastEvaluatedKey and paginate further.
    }
    }
    return nxtID, true
    }
*/
func DbQueryWithLoop[T any](input QueryWithLoopInput[T]) ([]T, map[string]types.AttributeValue, error) {
	var out, finalOut []T

	queryInput := &dynamodb.QueryInput{
		TableName:                 aws.String(input.TableName),
		KeyConditionExpression:    input.Expr.KeyCondition(),
		ProjectionExpression:      input.Expr.Projection(),
		ExpressionAttributeNames:  input.Expr.Names(),
		ExpressionAttributeValues: input.Expr.Values(),
		FilterExpression:          input.Expr.Filter(),
	}

	if input.IndexName != constants.EMPTY_STRING {
		queryInput.IndexName = &input.IndexName
	}

	if input.Limit != 0 {
		queryInput.Limit = utils.Ptr(int32(input.Limit))
	}

	if input.StartKey != nil {
		queryInput.ExclusiveStartKey = input.StartKey
	}

	if input.SortOrder != nil {
		queryInput.ScanIndexForward = input.SortOrder
	}

	if input.AppendAndReInitialize == nil && input.Limit == 0 {
		input.AppendAndReInitialize = func(out, fOut *[]T, nxtID map[string]types.AttributeValue, count int64) (map[string]types.AttributeValue, bool) {
			*fOut = append(*fOut, *out...)
			*out = make([]T, 0)
			return nxtID, true
		}
	}

	if input.AppendAndReInitialize == nil && input.Limit != 0 {
		if input.GetKey == nil {
			return nil, nil, errors.New("GetKey function is required when limit is passed and AppendAndReInitialize is to be skipped")
		}
		input.AppendAndReInitialize = func(out, fOut *[]T, nxtID map[string]types.AttributeValue, count int64) (map[string]types.AttributeValue, bool) {
			if out != nil && fOut != nil {
				tempResult := out
				finalResult := fOut
				fTempLen := len(*finalResult)
				tempLen := len(*tempResult)
				if tempLen+fTempLen > int(count) {
					*finalResult = append(*finalResult, (*tempResult)[0:count-int64(fTempLen)]...)
					nxtID = input.GetKey((*finalResult)[len(*finalResult)-1], input.IndexName)
					return nxtID, false // If we get more records than required, take as many as required, return the lastEvaluatedKey and don't paginate further.
				} else {
					*finalResult = append(*finalResult, *tempResult...)
					fTempLen += tempLen
					if fTempLen == int(count) || nxtID == nil {
						return nxtID, false // If desired no. of records are retrieved OR there are no more records at DB, return lastEvaluatedKey and don't paginate further.
					}
					*tempResult = []T{}
					return nxtID, true // If there still are more records to retrieve, return lastEvaluatedKey and paginate further.
				}
			}
			return nxtID, true
		}
	}

	nextPage := true
	lastEvalKey := map[string]types.AttributeValue{}

	// Continue until we get the required records
	for lastEvalKey != nil && nextPage {
		result, err := input.DBHandle.DB.Query(input.DBHandle.Ctx.Context, queryInput)
		if err != nil {
			return nil, nil, err
		}
		err = attributevalue.UnmarshalListOfMaps(result.Items, &out)
		if err != nil {
			return nil, nil, err
		}
		lastEvalKey = result.LastEvaluatedKey
		lastEvalKey, nextPage = input.AppendAndReInitialize(&out, &finalOut, lastEvalKey, input.Limit)
		queryInput.ExclusiveStartKey = lastEvalKey
	}
	return finalOut, lastEvalKey, nil
}

type DbUpdateItemStructSetInput struct {
	TableName string
	Item      DBItem
	Condition expression.ConditionBuilder
}

// DbUpdateItemStructSet updates all non-key fields of a struct in DynamoDB
// item should implement the DBItem interface and contain the updated values
func (d *EspDB) DbUpdateItemStructSet(input DbUpdateItemStructSetInput) (*dynamodb.UpdateItemOutput, error) {
	// Marshal the item to get all fields
	av, err := attributevalue.MarshalMap(input.Item)
	if err != nil {
		return nil, err
	}

	// Create update builder
	update := expression.UpdateBuilder{}

	// Get key names to exclude them from update
	hashKey := input.Item.GetHKey()
	rangeKey := input.Item.GetRKey()

	// Add each field to update expression except the key fields
	for k, v := range av {
		if k != hashKey && k != rangeKey {
			update = update.Set(expression.Name(k), expression.Value(v))
		}
	}

	// Use existing DbUpdateItem to perform the update
	return d.DbUpdateItem(DbUpdateItemInput{
		TableName: input.TableName,
		Update:    update,
		Query:     input.Item,
		Condition: input.Condition,
	})
}
