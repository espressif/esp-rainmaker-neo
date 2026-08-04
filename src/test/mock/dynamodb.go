// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	orderedmap "github.com/wk8/go-ordered-map"
)

type ProfileOperation struct {
	Name    string
	Details string
	Size    int
}

type Profile struct {
	mu sync.Mutex
	// For every table, the read/write counts and the operations performed on it
	Accesses map[string]struct {
		ReadCount  int
		WriteCount int
		Operations []ProfileOperation
	}
}

func NewProfile() *Profile {
	return &Profile{
		Accesses: make(map[string]struct {
			ReadCount  int
			WriteCount int
			Operations []ProfileOperation
		}),
	}
}

func (p *Profile) AddAction(table string, action string, operation string, details string, size int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Get or create entry
	entry := p.Accesses[table]

	// Modify the struct
	if action == "read" {
		entry.ReadCount++
	} else {
		entry.WriteCount++
	}
	if entry.Operations == nil {
		entry.Operations = make([]ProfileOperation, 0)
	}
	entry.Operations = append(entry.Operations, ProfileOperation{Name: operation, Details: details, Size: size})

	// Reassign back to map
	p.Accesses[table] = entry
}

func (p *Profile) AddRead(table string, operation string, details string, size int) {
	// XXX This is not yet 'size' aware
	p.AddAction(table, "read", operation, details, size)
}

func (p *Profile) AddWrite(table string, operation string, details string, size int) {
	// XXX This is not yet 'size' aware
	p.AddAction(table, "write", operation, details, size)
}

func (p *Profile) Print(w io.Writer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	details := os.Getenv("TEST_DYNAMODB_PROFILE_DETAILS")
	totalReadCount := 0
	totalWriteCount := 0
	for table, entry := range p.Accesses {
		fmt.Fprintf(w, "Table: %v: ", table)
		fmt.Fprintf(w, "  ReadCount: %v, ", entry.ReadCount)
		fmt.Fprintf(w, "  WriteCount: %v, ", entry.WriteCount)
		fmt.Fprintf(w, "  Operations: ")
		for _, op := range entry.Operations {
			if details == "true" {
				fmt.Fprintf(w, "\n    %v: %v, %v", op.Name, op.Details, op.Size)
			} else {
				fmt.Fprintf(w, " %v", op.Name)
			}
		}
		fmt.Fprintf(w, "\n")
		totalReadCount += entry.ReadCount
		totalWriteCount += entry.WriteCount
	}
	fmt.Fprintf(w, "Total: ReadCount: %v, WriteCount: %v\n", totalReadCount, totalWriteCount)
}

func (p *Profile) TotalCounts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	totalReadCount := 0
	totalWriteCount := 0
	for _, entry := range p.Accesses {
		totalReadCount += entry.ReadCount
		totalWriteCount += entry.WriteCount
	}
	return totalReadCount, totalWriteCount
}

func (p *Profile) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Accesses = make(map[string]struct {
		ReadCount  int
		WriteCount int
		Operations []ProfileOperation
	})
}

type TableDetails struct {
	PrimaryKey string
	SortKey    string
	Status     types.TableStatus
	GSIs       map[string]*types.GlobalSecondaryIndexDescription
}

type DBItem map[string]types.AttributeValue

// DynamoDBMock is a mock implementation of the DynamoDB interface
type DynamoDBMock struct {
	// table name to TableDetails
	tables map[string]TableDetails
	// secondary index just point to the original table in the tables array
	sec_index map[string]string
	// this is something like { "table1": { "id1": { "id": "id1", "val": 1 }, "id2": { "id": "id2", "val": 2 } } }
	// So the first key is the table name, the second key is the primary key, and the value is the item itself
	items_pkey map[string]*orderedmap.OrderedMap
	// If the table has a sort key, then the structure is like this:
	items_pskey map[string]*orderedmap.OrderedMap
	PutItemErr  error
	mx          sync.RWMutex
	// condMx makes a conditional write atomic with respect to other writes.
	//
	// getItem and putItem each take mx separately, so evaluating a
	// ConditionExpression and then writing is two critical sections: two
	// concurrent callers can both observe "item does not exist" and both
	// write. The real service evaluates the condition and applies the write
	// as one operation, so without this a mock-backed test cannot exercise
	// conditional-write races — and code that depends on exactly one writer
	// winning would appear correct here and fail in production.
	//
	// Ordering is always condMx -> mx; nothing takes them the other way.
	condMx  sync.Mutex
	profile *Profile

	// Returned once on the next matching call, then cleared.
	NextDescribeError error
	NextUpdateError   error
	// If > 0, the next BatchGetItem call returns the last N keys as UnprocessedKeys
	// instead of processing them, then resets to 0.
	NextBatchGetUnprocessedCount int
}

func NewDynamoDBMock() *DynamoDBMock {
	return &DynamoDBMock{
		tables:      make(map[string]TableDetails),
		sec_index:   make(map[string]string),
		items_pkey:  make(map[string]*orderedmap.OrderedMap),
		items_pskey: make(map[string]*orderedmap.OrderedMap),
		profile:     NewProfile(),
	}
}

func (m *DynamoDBMock) ProfileReset() {
	m.profile.Reset()
}

func deepCopy(src, dst interface{}) error {
	bytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dst)
}

func (m *DynamoDBMock) ProfileGet() Profile {
	var profile Profile
	deepCopy(m.profile, &profile)
	return profile
}

// ForEachRow iterates over all the rows in the DynamoDBMock and executes the closure fn
func (m *DynamoDBMock) ForEachRow(table string, fn func(map[string]types.AttributeValue) error) error {
	var err error
	m.mx.RLock()
	defer m.mx.RUnlock()
	if m.tables[table].SortKey != "" {
		for v1pair := m.items_pskey[table].Oldest(); v1pair != nil; v1pair = v1pair.Next() {
			for v2pair := v1pair.Value.(*orderedmap.OrderedMap).Oldest(); v2pair != nil; v2pair = v2pair.Next() {
				err = fn(v2pair.Value.(map[string]types.AttributeValue))
				if err != nil {
					return err
				}
			}
		}
	} else {
		for v1pair := m.items_pkey[table].Oldest(); v1pair != nil; v1pair = v1pair.Next() {
			err = fn(v1pair.Value.(map[string]types.AttributeValue))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func PrintAttributeValueMap(av map[string]types.AttributeValue) {
	result := map[string]interface{}{}
	err := attributevalue.UnmarshalMap(av, &result)
	if err != nil {
		fmt.Println("Error unmarshalling:", err)
		return
	}
	PrintMap(result, "")
}

// Recursive function to print a map[string]interface{}
func PrintMap(data map[string]interface{}, indent string) {
	var keys []string
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := data[key]
		switch v := value.(type) {
		case map[string]interface{}:
			fmt.Printf("%s%s:\n", indent, key)
			// Recursively print the nested map with increased indentation
			PrintMap(v, indent+"  ")
		case []interface{}:
			fmt.Printf("%s%s: [\n", indent, key)
			// Loop through the slice and handle each item
			for i, item := range v {
				fmt.Printf("%s  [%d]: ", indent, i)
				// Handle nested maps or primitive types inside the slice
				switch item := item.(type) {
				case map[string]interface{}:
					fmt.Println()
					PrintMap(item, indent+"    ")
				default:
					fmt.Printf("%v\n", item)
				}
			}
			fmt.Printf("%s]\n", indent)
		default:
			// Print primitive types directly
			fmt.Printf("%s%s: %v\n", indent, key, v)
		}
	}
}

func printValue(value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		PrintMap(v, "")
	case map[string]types.AttributeValue:
		// Unmarshal into a Go map (map[string]interface{})
		var result map[string]interface{}
		err := attributevalue.UnmarshalMap(value.(map[string]types.AttributeValue), &result)
		if err != nil {
			fmt.Println("Error unmarshalling:", err)
			return
		}
		PrintMap(result, "       ")
	default:
		fmt.Printf("%v\n", v)
	}
}

// Print the contents of the DynamoDBMock
func (m *DynamoDBMock) Print() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	fmt.Printf("tables: %v\n", m.tables)
	for k, v := range m.items_pkey {
		fmt.Printf("table: %v\n", k)
		for v1pair := v.Oldest(); v1pair != nil; v1pair = v1pair.Next() {
			fmt.Printf("  pkey: %v\n", v1pair.Key)
			printValue(v1pair.Value)
		}
	}
	for k, v := range m.items_pskey {
		fmt.Printf("table: %v\n", k)
		for v1pair := v.Oldest(); v1pair != nil; v1pair = v1pair.Next() {
			fmt.Printf("  pkey: %v\n", v1pair.Key)
			for v2pair := v1pair.Value.(*orderedmap.OrderedMap).Oldest(); v2pair != nil; v2pair = v2pair.Next() {
				fmt.Printf("    skey: %v\n", v2pair.Key)
				printValue(v2pair.Value)
			}
		}
	}
}

func (m *DynamoDBMock) AddTable(name, primaryKey string, sortKey string) {
	m.mx.Lock()
	defer m.mx.Unlock()
	m.tables[name] = TableDetails{PrimaryKey: primaryKey, SortKey: sortKey}
	if sortKey != "" {
		m.items_pskey[name] = orderedmap.New()
	} else {
		m.items_pkey[name] = orderedmap.New()
	}
}

func (m *DynamoDBMock) AddSecondaryIndex(indexName, tableName, primaryKey, sortKey string) error {
	// We don't do anything with the primary and sort key right now
	if _, ok := m.tables[tableName]; !ok {
		return &types.ResourceNotFoundException{Message: aws.String("Table not found")}
	}
	m.sec_index[indexName] = tableName
	return nil
}

func SorN(av types.AttributeValue) *string {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return &v.Value
	case *types.AttributeValueMemberN:
		return &v.Value
	default:
		return nil
	}
}

func ExtractKeys(key map[string]types.AttributeValue, primaryKey, sortKey string) (string, string, error) {
	pkey := key[primaryKey].(*types.AttributeValueMemberS).Value
	skey := ""
	if sortKey != "" {
		skey_name, ok := key[sortKey]
		if !ok {
			return "", "", &types.ResourceNotFoundException{Message: aws.String("Sort Key not found")}
		}
		skey = *SorN(skey_name)
	}
	return pkey, skey, nil
}

func (m *DynamoDBMock) GetKeys(item interface{}) (string, string, string, error) {
	switch item := item.(type) {
	case *dynamodb.GetItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		pkey, skey, err := ExtractKeys(item.Key, table.PrimaryKey, table.SortKey)
		return *item.TableName, pkey, skey, err
	case *dynamodb.PutItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		pkey, skey, err := ExtractKeys(item.Item, table.PrimaryKey, table.SortKey)
		return *item.TableName, pkey, skey, err
	case *dynamodb.DeleteItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		pkey, skey, err := ExtractKeys(item.Key, table.PrimaryKey, table.SortKey)
		return *item.TableName, pkey, skey, err
	case *dynamodb.UpdateItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		pkey, skey, err := ExtractKeys(item.Key, table.PrimaryKey, table.SortKey)
		return *item.TableName, pkey, skey, err
	default:
		return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
	}
}

func (m *DynamoDBMock) BatchGetItem(ctx context.Context, params *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	output := &dynamodb.BatchGetItemOutput{}
	output.Responses = make(map[string][]map[string]types.AttributeValue)

	unprocessedCount := m.NextBatchGetUnprocessedCount
	m.NextBatchGetUnprocessedCount = 0

	for table, v := range params.RequestItems {
		keys := v.Keys
		outitems := make([]map[string]types.AttributeValue, 0)

		processUntil := len(keys)
		if unprocessedCount > 0 && unprocessedCount < len(keys) {
			processUntil = len(keys) - unprocessedCount
		}

		for _, key := range keys[:processUntil] {
			pkey, skey, err := ExtractKeys(key, m.tables[table].PrimaryKey, m.tables[table].SortKey)
			if err != nil {
				return nil, err
			}
			if item, err := m.getItem(table, pkey, skey); err == nil {
				outitems = append(outitems, item)
			}
		}
		output.Responses[table] = outitems

		if unprocessedCount > 0 && unprocessedCount < len(keys) {
			if output.UnprocessedKeys == nil {
				output.UnprocessedKeys = make(map[string]types.KeysAndAttributes)
			}
			output.UnprocessedKeys[table] = types.KeysAndAttributes{
				Keys:                     keys[processUntil:],
				ProjectionExpression:     v.ProjectionExpression,
				ExpressionAttributeNames: v.ExpressionAttributeNames,
			}
		}
	}
	return output, nil
}

func (m *DynamoDBMock) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	// Extract the table name and item ID from the input
	tableName, pkey, skey, err := m.GetKeys(params)
	if err != nil {
		return nil, err
	}

	m.profile.AddRead(*params.TableName, "GetItem", pkey+":"+skey, 0)
	if v, err := m.getItem(tableName, pkey, skey); err == nil {
		// Create a GetItemOutput with the item
		output := &dynamodb.GetItemOutput{
			Item: v,
		}
		return output, nil
	} else {
		return nil, err
	}
}

// GetDirect returns the item from the DynamoDBMock without going through the GetItem API
func (m *DynamoDBMock) GetDirect(tableName, pkey, skey string, out interface{}) error {
	if v, err := m.getItem(tableName, pkey, skey); err == nil {
		// Create a GetItemOutput with the item
		return attributevalue.UnmarshalMap(v, out)
	} else {
		return err
	}
}

func (m *DynamoDBMock) getItem(tableName, pkey, skey string) (map[string]types.AttributeValue, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	var item DBItem

	_, exists := m.tables[tableName]
	if !exists {
		return nil, &types.ResourceNotFoundException{Message: aws.String("Table not found")}
	}

	// Process the Sort Key now
	if m.tables[tableName].SortKey != "" {
		p_item, _ := m.items_pskey[tableName].Get(pkey)
		if p_item != nil {
			item_interface, _ := p_item.(*orderedmap.OrderedMap).Get(skey)
			item, _ = item_interface.(map[string]types.AttributeValue)
		}
	} else {
		item_interface, _ := m.items_pkey[tableName].Get(pkey)
		item, _ = item_interface.(map[string]types.AttributeValue)
	}
	return item, nil
}

func (m *DynamoDBMock) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	// Extract the table name and item ID from the input
	tableName, pkey, skey, err := m.GetKeys(params)
	if err != nil {
		return nil, err
	}

	if m.PutItemErr != nil {
		return nil, m.PutItemErr
	}

	if params.ConditionExpression != nil {
		// Held across both the evaluation and the write so the pair is atomic,
		// as it is in the real service.
		m.condMx.Lock()
		defer m.condMx.Unlock()

		condition := NewMexpression(params.ConditionExpression, params.ExpressionAttributeNames, params.ExpressionAttributeValues)
		item, _ := m.getItem(tableName, pkey, skey)
		if valid, _ := condition.Evaluate(item); !valid {
			return nil, &types.ConditionalCheckFailedException{Message: aws.String("Condition not met")}
		}
	}
	m.profile.AddWrite(tableName, "PutItem", pkey+":"+skey, 0)
	return &dynamodb.PutItemOutput{}, m.putItem(tableName, pkey, skey, params.Item)
}

// PutDirect puts the item into the DynamoDBMock without going through the PutItem API
func (m *DynamoDBMock) PutDirect(tableName, pkey, skey string, item interface{}) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	return m.putItem(tableName, pkey, skey, av)
}

func (m *DynamoDBMock) putItem(tableName, pkey, skey string, item map[string]types.AttributeValue) error {
	m.mx.Lock()
	defer m.mx.Unlock()
	if m.tables[tableName].SortKey != "" {
		if skey == "" {
			return &types.ResourceNotFoundException{Message: aws.String("Index not found")}
		} else {
			pkey_item, _ := m.items_pskey[tableName].Get(pkey)
			if pkey_item == nil {
				pkey_item = orderedmap.New()
				m.items_pskey[tableName].Set(pkey, pkey_item)
			}
			pkey_item.(*orderedmap.OrderedMap).Set(skey, item)
		}
	} else {
		// Store the item in the hashmap
		m.items_pkey[tableName].Set(pkey, item)
	}

	return nil
}

func (m *DynamoDBMock) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	m.mx.Lock()
	defer m.mx.Unlock()
	// Extract the table name and item ID from the input
	tableName, pkey, skey, err := m.GetKeys(params)
	if err != nil {
		return nil, err
	}

	m.profile.AddWrite(tableName, "DeleteItem", pkey+":"+skey, 0)

	// Check if item exists first
	var item map[string]types.AttributeValue
	if m.tables[tableName].SortKey != "" {
		if p_item, exists := m.items_pskey[tableName].Get(pkey); exists && p_item != nil {
			if item_interface, exists := p_item.(*orderedmap.OrderedMap).Get(skey); exists && item_interface != nil {
				item = item_interface.(map[string]types.AttributeValue)
			}
		}
	} else {
		if item_interface, exists := m.items_pkey[tableName].Get(pkey); exists && item_interface != nil {
			item = item_interface.(map[string]types.AttributeValue)
		}
	}

	// If we have a condition expression, evaluate it
	if params.ConditionExpression != nil {
		if item == nil {
			return nil, &types.ConditionalCheckFailedException{Message: aws.String("The conditional request failed")}
		}
		condition := NewMexpression(params.ConditionExpression, params.ExpressionAttributeNames, params.ExpressionAttributeValues)
		if valid, _ := condition.Evaluate(item); !valid {
			return nil, &types.ConditionalCheckFailedException{Message: aws.String("The conditional request failed")}
		}
	}

	// Process the Sort Key now
	if m.tables[tableName].SortKey != "" {
		if skey_item, exists := m.items_pskey[tableName].Get(pkey); exists && skey_item != nil {
			skey_item.(*orderedmap.OrderedMap).Delete(skey)
			if skey_item.(*orderedmap.OrderedMap).Len() == 0 {
				m.items_pskey[tableName].Delete(pkey)
			}
		}
	} else {
		// Delete the item from the hashmap
		m.items_pkey[tableName].Delete(pkey)
	}

	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *DynamoDBMock) BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	var out dynamodb.BatchWriteItemOutput
	for table, requests := range params.RequestItems {
		for _, request := range requests {
			if request.PutRequest != nil {
				if _, err := m.PutItem(ctx, &dynamodb.PutItemInput{TableName: &table, Item: request.PutRequest.Item}); err != nil {
					return nil, err
				}
			}
			if request.DeleteRequest != nil {
				if _, err := m.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &table, Key: request.DeleteRequest.Key}); err != nil {
					return nil, err
				}
			}
		}
	}
	return &out, nil
}

// getProjection returns only the projection of the item based on the projection expression
func getProjection(item map[string]types.AttributeValue, projection *Mexpression) map[string]types.AttributeValue {
	if projection.Expr == nil {
		return item
	}
	output := make(map[string]types.AttributeValue)
	for _, name := range projection.GetNamesList() {
		output[name] = item[name]
	}
	return output
}

func (m *DynamoDBMock) Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	int_input := params
	attr := ""
	for k, v := range int_input.ExpressionAttributeNames {
		attr += k + ":" + v + " "
	}
	for k, v := range int_input.ExpressionAttributeValues {
		attr += k + ":" + *SorN(v) + " "
	}
	m.profile.AddRead(*params.TableName, "Query", "KeyConditionExpression: "+*int_input.KeyConditionExpression+" Key/Value: "+attr, 0)
	// The query could be on a secondary index
	if _, ok := m.tables[*params.TableName]; !ok {
		if _, ok := m.sec_index[*params.TableName]; !ok {
			return nil, &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		s := m.sec_index[*params.TableName]
		int_input.TableName = &s
	}
	return m.QueryInternal(int_input)
}

func (m *DynamoDBMock) QueryInternal(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
	output := &dynamodb.QueryOutput{}
	keyCond := NewMexpression(input.KeyConditionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	filterCond := NewMexpression(input.FilterExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	projection := NewMexpression(input.ProjectionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)

	var matchingItems []map[string]types.AttributeValue

	m.ForEachRow(*input.TableName, func(item map[string]types.AttributeValue) error {
		if valid, _ := keyCond.Evaluate(item); valid {
			if valid, _ := filterCond.Evaluate(item); valid {
				matchingItems = append(matchingItems, item)
			}
		}
		return nil
	})

	// Sort items by sort key if table has one AND ScanIndexForward is explicitly set
	tableDetails := m.tables[*input.TableName]
	if tableDetails.SortKey != "" && input.ScanIndexForward != nil {
		m.sortItemsBySortKey(matchingItems, tableDetails.SortKey, input.ScanIndexForward)
	}

	// Handle ExclusiveStartKey for pagination
	if input.ExclusiveStartKey != nil {
		// Find the starting position based on ExclusiveStartKey
		startIndex := -1
		for i, item := range matchingItems {
			if m.matchesKey(item, input.ExclusiveStartKey, tableDetails) {
				startIndex = i + 1 // Start from the next item
				break
			}
		}
		if startIndex > 0 && startIndex < len(matchingItems) {
			matchingItems = matchingItems[startIndex:]
		} else if startIndex >= len(matchingItems) {
			matchingItems = []map[string]types.AttributeValue{}
		}
	}

	// Apply limit and set LastEvaluatedKey if there are more results
	if input.Limit != nil && *input.Limit > 0 {
		limit := int(*input.Limit)
		if len(matchingItems) > limit {
			// Set LastEvaluatedKey to the last item we're returning
			lastItem := matchingItems[limit-1]
			output.LastEvaluatedKey = make(map[string]types.AttributeValue)
			output.LastEvaluatedKey[tableDetails.PrimaryKey] = lastItem[tableDetails.PrimaryKey]
			if tableDetails.SortKey != "" {
				output.LastEvaluatedKey[tableDetails.SortKey] = lastItem[tableDetails.SortKey]
			}
			matchingItems = matchingItems[:limit]
		}
	}

	// Handle Count select or return items with projection
	if input.Select == types.SelectCount {
		output.Count = int32(len(matchingItems))
	} else {
		// Apply projection to final results
		for _, item := range matchingItems {
			projectedItem := getProjection(item, projection)
			output.Items = append(output.Items, projectedItem)
		}
		output.Count = int32(len(output.Items))
	}
	return output, nil
}

// matchesKey checks if an item's key matches the provided key map
func (m *DynamoDBMock) matchesKey(item map[string]types.AttributeValue, key map[string]types.AttributeValue, tableDetails TableDetails) bool {
	// Compare primary key
	if !IsEqual(item[tableDetails.PrimaryKey], key[tableDetails.PrimaryKey]) {
		return false
	}

	// Compare sort key if table has one
	if tableDetails.SortKey != "" {
		if !IsEqual(item[tableDetails.SortKey], key[tableDetails.SortKey]) {
			return false
		}
	}

	return true
}

// sortItemsBySortKey sorts items by their sort key value
// scanIndexForward: true = ascending, false = descending, nil = ascending (default)
func (m *DynamoDBMock) sortItemsBySortKey(items []map[string]types.AttributeValue, sortKeyName string, scanIndexForward *bool) {
	// Default to ascending if not specified
	ascending := true
	if scanIndexForward != nil {
		ascending = *scanIndexForward
	}

	sort.Slice(items, func(i, j int) bool {
		// Get sort key values
		val1 := items[i][sortKeyName]
		val2 := items[j][sortKeyName]

		// Compare using the common comparison function
		compare := CompareAttributeValues(val1, val2)

		if ascending {
			return compare < 0
		} else {
			return compare > 0
		}
	})
}

func (m *DynamoDBMock) ScanInternal(input *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
	output := &dynamodb.ScanOutput{}
	filterCond := NewMexpression(input.FilterExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	projection := NewMexpression(input.ProjectionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	_ = m.ForEachRow(*input.TableName, func(item map[string]types.AttributeValue) error {
		//              fmt.Printf("evaluating item %v\n", item)
		if valid, _ := filterCond.Evaluate(item); valid {
			//                              fmt.Printf("adding item  %v\n", item)
			item = getProjection(item, projection)
			output.Items = append(output.Items, item)
		}
		return nil
	})
	// AWS sets Count to the number of items that matched, regardless of the Select mode (e.g. SELECT_COUNT returns Count without Items). Mirror that here so callers using Select=COUNT read the right value.
	output.Count = int32(len(output.Items))
	return output, nil
}

func (m *DynamoDBMock) Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	m.profile.AddRead(*params.TableName, "Scan", "", 0)
	int_input := params
	// The scan could be on a secondary index
	if _, ok := m.tables[*int_input.TableName]; !ok {
		if _, ok := m.sec_index[*int_input.TableName]; !ok {
			return nil, &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		s := m.sec_index[*int_input.TableName]
		int_input.TableName = &s
	}
	return m.ScanInternal(int_input)
}

func (m *DynamoDBMock) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	output := &dynamodb.UpdateItemOutput{}
	// Extract the table name and item ID from the input
	tableName, pkey, skey, err := m.GetKeys(input)
	if err != nil {
		return nil, err
	}

	m.profile.AddWrite(tableName, "UpdateItem", pkey+":"+skey, 0)
	// We ignore the error, since if the item doesn't exist, we create it
	item, _ := m.getItem(tableName, pkey, skey)

	// If the item doesn't exist and we have a condition expression that checks for attribute_exists,
	// we should return a ConditionalCheckFailedException
	if item == nil && input.ConditionExpression != nil {
		condition := NewMexpression(input.ConditionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
		if valid, _ := condition.Evaluate(make(map[string]types.AttributeValue)); !valid {
			return nil, &types.ConditionalCheckFailedException{Message: aws.String("Condition not met")}
		}
	}

	if item == nil {
		// If the item doesn't exist, we need to create it
		item = make(map[string]types.AttributeValue)
		item[m.tables[tableName].PrimaryKey] = input.Key[m.tables[tableName].PrimaryKey]
		if m.tables[tableName].SortKey != "" {
			item[m.tables[tableName].SortKey] = input.Key[m.tables[tableName].SortKey]
		}
	}

	condition := NewMexpression(input.ConditionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	if valid, _ := condition.Evaluate(item); valid {
		// Split the update expression into REMOVE and SET parts
		updateExpr := *input.UpdateExpression
		parts := strings.Split(updateExpr, " SET ")
		if len(parts) > 1 {
			// Handle combined REMOVE and SET operations
			removePart := strings.TrimPrefix(parts[0], "REMOVE ")
			setPart := "SET " + parts[1]

			// Process REMOVE operation
			removeExpr := NewMexpression(aws.String("REMOVE "+removePart), input.ExpressionAttributeNames, input.ExpressionAttributeValues)
			err := removeExpr.ProcessUpdate(func(op UpdateOp, name string, value types.AttributeValue) {
				if op == Delete {
					delete(item, name)
				}
			})
			if err != nil {
				return nil, err
			}

			// Process SET operation
			setExpr := NewMexpression(aws.String(setPart), input.ExpressionAttributeNames, input.ExpressionAttributeValues)
			err = setExpr.ProcessUpdate(func(op UpdateOp, name string, value types.AttributeValue) {
				if op == Set {
					item[name] = value
				} else if op == SetDone {
					// Put the updated item back in the hashmap
					m.putItem(tableName, pkey, skey, item)
				}
			})
			if err != nil {
				return nil, err
			}
		} else {
			// Handle single operation (either REMOVE or SET)
			update := NewMexpression(input.UpdateExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
			err := update.ProcessUpdate(func(op UpdateOp, name string, value types.AttributeValue) {
				switch op {
				case Set:
					// Overwrite specific values and maintain it in item
					item[name] = value
				case SetDone:
					// item is now updated, so put it back in the hashmap
					m.putItem(tableName, pkey, skey, item)
				case Delete:
					delete(item, name)
				case Add:
					switch v := value.(type) {
					case *types.AttributeValueMemberN:
						if v.Value != "" {
							existValInt := 0
							if item[name] != nil && item[name].(*types.AttributeValueMemberN).Value != "" {
								existValInt, _ = strconv.Atoi(item[name].(*types.AttributeValueMemberN).Value)
							}
							addValInt, _ := strconv.Atoi(v.Value)
							item[name] = &types.AttributeValueMemberN{Value: strconv.Itoa(existValInt + addValInt)}
						}
					case *types.AttributeValueMemberSS:
						// ADD onto a string set is set-union: keep existing members, append new ones absent.
						seen := map[string]bool{}
						var merged []string
						if existing, ok := item[name].(*types.AttributeValueMemberSS); ok {
							for _, s := range existing.Value {
								seen[s] = true
								merged = append(merged, s)
							}
						}
						for _, s := range v.Value {
							if !seen[s] {
								seen[s] = true
								merged = append(merged, s)
							}
						}
						item[name] = &types.AttributeValueMemberSS{Value: merged}
					}
				case ListAppend:
					// Handle list append
					if existingList, ok := item[name].(*types.AttributeValueMemberL); ok {
						// Append new value to existing list
						newValue := value.(*types.AttributeValueMemberL)
						existingList.Value = append(existingList.Value, newValue.Value...)
					} else {
						// Create new list if it doesn't exist
						item[name] = &types.AttributeValueMemberL{
							Value: value.(*types.AttributeValueMemberL).Value,
						}
					}
				case ListRemove:
					// Handle list item removal
					if existingList, ok := item[name].(*types.AttributeValueMemberL); ok {
						index, _ := strconv.Atoi(value.(*types.AttributeValueMemberN).Value)
						if index >= 0 && index < len(existingList.Value) {
							// Remove item at index
							existingList.Value = append(existingList.Value[:index], existingList.Value[index+1:]...)
						}
					}
				}
			})
			if err != nil {
				return nil, err
			}
		}

	} else {
		return nil, &types.ConditionalCheckFailedException{Message: aws.String("Condition not met")}
	}

	if input.ReturnValues == types.ReturnValueAllNew || input.ReturnValues == types.ReturnValueUpdatedNew {
		output.Attributes, _ = m.getItem(tableName, pkey, skey)
	}
	return output, nil
}

// Control-plane simulator (DescribeTable / UpdateTable).
// Builders are fluent: NewDynamoDBMock().WithTable(...).WithGSI(...).

func (m *DynamoDBMock) WithTable(name string, status types.TableStatus) *DynamoDBMock {
	m.mx.Lock()
	defer m.mx.Unlock()
	t := m.tables[name]
	t.Status = status
	if t.GSIs == nil {
		t.GSIs = map[string]*types.GlobalSecondaryIndexDescription{}
	}
	m.tables[name] = t
	return m
}

// WithGSI: sortKey == "" for hash-only; projection is ALL. Use WithGSIDescription for non-default projection / NonKeyAttributes.
func (m *DynamoDBMock) WithGSI(table, indexName, hashKey, sortKey string, status types.IndexStatus) *DynamoDBMock {
	keys := []types.KeySchemaElement{{AttributeName: aws.String(hashKey), KeyType: types.KeyTypeHash}}
	if sortKey != "" {
		keys = append(keys, types.KeySchemaElement{AttributeName: aws.String(sortKey), KeyType: types.KeyTypeRange})
	}
	return m.WithGSIDescription(table, types.GlobalSecondaryIndexDescription{
		IndexName:   aws.String(indexName),
		IndexStatus: status,
		KeySchema:   keys,
		Projection:  &types.Projection{ProjectionType: types.ProjectionTypeAll},
	})
}

func (m *DynamoDBMock) WithGSIDescription(table string, gsi types.GlobalSecondaryIndexDescription) *DynamoDBMock {
	m.mx.Lock()
	defer m.mx.Unlock()
	t, ok := m.tables[table]
	if !ok {
		panic(fmt.Sprintf("DynamoDBMock: table %q not registered; call WithTable first", table))
	}
	if t.GSIs == nil {
		t.GSIs = map[string]*types.GlobalSecondaryIndexDescription{}
	}
	t.GSIs[aws.ToString(gsi.IndexName)] = &gsi
	m.tables[table] = t
	return m
}

func (m *DynamoDBMock) HasGSI(table, indexName string) bool {
	m.mx.RLock()
	defer m.mx.RUnlock()
	t, ok := m.tables[table]
	if !ok {
		return false
	}
	_, ok = t.GSIs[indexName]
	return ok
}

func (m *DynamoDBMock) GSIStatus(table, indexName string) types.IndexStatus {
	m.mx.RLock()
	defer m.mx.RUnlock()
	t, ok := m.tables[table]
	if !ok {
		return ""
	}
	gsi, ok := t.GSIs[indexName]
	if !ok {
		return ""
	}
	return gsi.IndexStatus
}

func (m *DynamoDBMock) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	m.mx.Lock()
	defer m.mx.Unlock()
	if m.NextDescribeError != nil {
		err := m.NextDescribeError
		m.NextDescribeError = nil
		return nil, err
	}
	t, ok := m.tables[aws.ToString(in.TableName)]
	if !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String("table not found")}
	}

	// Empty Status defaults to ACTIVE — keeps existing data-plane tests (which never set Status) from looking stuck.
	status := t.Status
	if status == "" {
		status = types.TableStatusActive
	}

	gsis := make([]types.GlobalSecondaryIndexDescription, 0, len(t.GSIs))
	for _, g := range t.GSIs {
		gsis = append(gsis, *g)
	}

	pkName := t.PrimaryKey
	if pkName == "" {
		pkName = "pk"
	}
	attrDefs := []types.AttributeDefinition{
		{AttributeName: aws.String(pkName), AttributeType: types.ScalarAttributeTypeS},
	}

	return &dynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName:              in.TableName,
			TableStatus:            status,
			AttributeDefinitions:   attrDefs,
			GlobalSecondaryIndexes: gsis,
		},
	}, nil
}

func (m *DynamoDBMock) UpdateTable(_ context.Context, in *dynamodb.UpdateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateTableOutput, error) {
	m.mx.Lock()
	defer m.mx.Unlock()
	if m.NextUpdateError != nil {
		err := m.NextUpdateError
		m.NextUpdateError = nil
		return nil, err
	}

	t, ok := m.tables[aws.ToString(in.TableName)]
	if !ok {
		return nil, &types.ResourceNotFoundException{Message: aws.String("table not found")}
	}
	if t.GSIs == nil {
		t.GSIs = map[string]*types.GlobalSecondaryIndexDescription{}
	}

	for _, u := range in.GlobalSecondaryIndexUpdates {
		switch {
		case u.Create != nil:
			name := aws.ToString(u.Create.IndexName)
			if _, exists := t.GSIs[name]; exists {
				return nil, fmt.Errorf("ValidationException: Index %s already exists", name)
			}
			t.GSIs[name] = &types.GlobalSecondaryIndexDescription{
				IndexName:   u.Create.IndexName,
				IndexStatus: types.IndexStatusActive,
				KeySchema:   u.Create.KeySchema,
				Projection:  u.Create.Projection,
			}
		case u.Delete != nil:
			name := aws.ToString(u.Delete.IndexName)
			if _, exists := t.GSIs[name]; !exists {
				return nil, fmt.Errorf("ValidationException: Index %s does not exist", name)
			}
			delete(t.GSIs, name)
		}
	}

	m.tables[aws.ToString(in.TableName)] = t
	return &dynamodb.UpdateTableOutput{}, nil
}
