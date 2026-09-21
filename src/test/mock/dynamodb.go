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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
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

// IndexDetails is the key schema of a secondary index. The sort key can differ from the base table's, so a query through the index has to order and paginate by these, not by the table's.
type IndexDetails struct {
	TableName  string
	PrimaryKey string
	SortKey    string
	// Projection defaults to ALL; SetIndexProjection narrows it to what the real index would carry.
	Projection       types.ProjectionType
	NonKeyAttributes []string
}

type DBItem map[string]types.AttributeValue

// DynamoDBMock is a mock implementation of the DynamoDB interface
type DynamoDBMock struct {
	// table name to TableDetails
	tables map[string]TableDetails
	// index name to its key schema and backing table
	sec_index map[string]*IndexDetails
	// this is something like { "table1": { "id1": { "id": "id1", "val": 1 }, "id2": { "id": "id2", "val": 2 } } }
	// So the first key is the table name, the second key is the primary key, and the value is the item itself
	items_pkey map[string]*orderedmap.OrderedMap
	// If the table has a sort key, then the structure is like this:
	items_pskey map[string]*orderedmap.OrderedMap
	PutItemErr  error
	// MaxPageItems caps how many items one Query returns, standing in for the service's 1 MB
	// page cap so tests can exercise a multi-page read. Zero (the default) means unlimited.
	MaxPageItems int
	mx           sync.RWMutex
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
	NextGetItemError  error
	// If > 0, the next BatchGetItem call returns the last N keys as UnprocessedKeys
	// instead of processing them, then resets to 0.
	NextBatchGetUnprocessedCount int
	// If > 0, the next BatchWriteItem call returns the last N requests as UnprocessedItems
	// instead of applying them, then decrements by one so a caller that retries eventually
	// drains. The real service answers 200 with UnprocessedItems when it throttles part of a
	// batch, so without this the mock cannot express a partially-applied batch write and a
	// caller that discards the response looks correct.
	NextBatchWriteUnprocessedCount int
}

func NewDynamoDBMock() *DynamoDBMock {
	return &DynamoDBMock{
		tables:      make(map[string]TableDetails),
		sec_index:   make(map[string]*IndexDetails),
		items_pkey:  make(map[string]*orderedmap.OrderedMap),
		items_pskey: make(map[string]*orderedmap.OrderedMap),
		profile:     NewProfile(),
	}
}

// DynamoDB serialises every item over the wire, so a caller can never reach the stored row through a value it read or wrote. Handing back the live map instead lets a test corrupt data it only meant to read, and hides code that mutates a shared item.
func copyAV(av types.AttributeValue) types.AttributeValue {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return &types.AttributeValueMemberS{Value: v.Value}
	case *types.AttributeValueMemberN:
		return &types.AttributeValueMemberN{Value: v.Value}
	case *types.AttributeValueMemberBOOL:
		return &types.AttributeValueMemberBOOL{Value: v.Value}
	case *types.AttributeValueMemberNULL:
		return &types.AttributeValueMemberNULL{Value: v.Value}
	case *types.AttributeValueMemberB:
		return &types.AttributeValueMemberB{Value: append([]byte(nil), v.Value...)}
	case *types.AttributeValueMemberSS:
		return &types.AttributeValueMemberSS{Value: append([]string(nil), v.Value...)}
	case *types.AttributeValueMemberNS:
		return &types.AttributeValueMemberNS{Value: append([]string(nil), v.Value...)}
	case *types.AttributeValueMemberBS:
		out := make([][]byte, len(v.Value))
		for i, b := range v.Value {
			out[i] = append([]byte(nil), b...)
		}
		return &types.AttributeValueMemberBS{Value: out}
	case *types.AttributeValueMemberL:
		out := make([]types.AttributeValue, len(v.Value))
		for i, e := range v.Value {
			out[i] = copyAV(e)
		}
		return &types.AttributeValueMemberL{Value: out}
	case *types.AttributeValueMemberM:
		return &types.AttributeValueMemberM{Value: copyItem(v.Value)}
	}
	return av
}

func copyItem(item map[string]types.AttributeValue) map[string]types.AttributeValue {
	if item == nil {
		return nil
	}
	out := make(map[string]types.AttributeValue, len(item))
	for k, v := range item {
		out[k] = copyAV(v)
	}
	return out
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
	m.mx.Lock()
	defer m.mx.Unlock()
	if _, ok := m.tables[tableName]; !ok {
		return &types.ResourceNotFoundException{Message: aws.String("Table not found")}
	}
	m.sec_index[indexName] = &IndexDetails{TableName: tableName, PrimaryKey: primaryKey, SortKey: sortKey, Projection: types.ProjectionTypeAll}
	return nil
}

// SetIndexProjection narrows what a query through the index returns. Indexes default to ALL, so a test only calls this when the real index is KEYS_ONLY or INCLUDE and the code under test must not see the unprojected attributes.
func (m *DynamoDBMock) SetIndexProjection(indexName string, projection types.ProjectionType, nonKeyAttributes ...string) error {
	m.mx.Lock()
	defer m.mx.Unlock()
	idx, ok := m.sec_index[indexName]
	if !ok {
		return &types.ResourceNotFoundException{Message: aws.String("Index not found")}
	}
	idx.Projection = projection
	idx.NonKeyAttributes = nonKeyAttributes
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
	pkeyAttr, ok := key[primaryKey].(*types.AttributeValueMemberS)
	if !ok {
		return "", "", &smithy.GenericAPIError{
			Code:    "ValidationException",
			Message: "The provided key element does not match the schema",
		}
	}
	pkey := pkeyAttr.Value
	skey := ""
	if sortKey != "" {
		skey_name, ok := key[sortKey]
		if !ok {
			return "", "", &types.ResourceNotFoundException{Message: aws.String("Sort Key not found")}
		}
		skeyValue := SorN(skey_name)
		if skeyValue == nil {
			return "", "", &smithy.GenericAPIError{
				Code:    "ValidationException",
				Message: "The provided key element does not match the schema",
			}
		}
		skey = *skeyValue
	}
	return pkey, skey, nil
}

// validateKeyOnly rejects a Key carrying non-key attributes, as the service does.
// Tolerating them let requests DynamoDB always refuses pass the unit suite.
func validateKeyOnly(key map[string]types.AttributeValue, table TableDetails) error {
	for name := range key {
		if name == table.PrimaryKey || (table.SortKey != "" && name == table.SortKey) {
			continue
		}
		return &smithy.GenericAPIError{
			Code:    "ValidationException",
			Message: "The provided key element does not match the schema",
		}
	}
	return nil
}

func (m *DynamoDBMock) GetKeys(item interface{}) (string, string, string, error) {
	switch item := item.(type) {
	case *dynamodb.GetItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		if err := validateKeyOnly(item.Key, table); err != nil {
			return "", "", "", err
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
		if err := validateKeyOnly(item.Key, table); err != nil {
			return "", "", "", err
		}
		pkey, skey, err := ExtractKeys(item.Key, table.PrimaryKey, table.SortKey)
		return *item.TableName, pkey, skey, err
	case *dynamodb.UpdateItemInput:
		table, ok := m.tables[*item.TableName]
		if !ok {
			return "", "", "", &types.ResourceNotFoundException{Message: aws.String("Table not found")}
		}
		if err := validateKeyOnly(item.Key, table); err != nil {
			return "", "", "", err
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

		projection := NewMexpression(v.ProjectionExpression, v.ExpressionAttributeNames, nil)
		for _, key := range keys[:processUntil] {
			if err := validateKeyOnly(key, m.tables[table]); err != nil {
				return nil, err
			}
			pkey, skey, err := ExtractKeys(key, m.tables[table].PrimaryKey, m.tables[table].SortKey)
			if err != nil {
				return nil, err
			}
			// A key with no row is omitted, as the service does; appending the nil map instead unmarshals into a zero-valued struct the caller cannot tell from real data.
			if item, err := m.getItem(table, pkey, skey); err == nil && item != nil {
				outitems = append(outitems, getProjection(item, projection))
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
	// A read that FAILS is a different thing from a read that finds nothing, and callers routinely
	// conflate them. Without this the mock can only ever produce the second, so code that treats a
	// throttle or timeout as "no such row" looks correct in tests.
	if m.NextGetItemError != nil {
		err := m.NextGetItemError
		m.NextGetItemError = nil
		return nil, err
	}

	// Extract the table name and item ID from the input
	tableName, pkey, skey, err := m.GetKeys(params)
	if err != nil {
		return nil, err
	}

	m.profile.AddRead(*params.TableName, "GetItem", pkey+":"+skey, 0)
	v, err := m.getItem(tableName, pkey, skey)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return &dynamodb.GetItemOutput{}, nil
	}
	projection := NewMexpression(params.ProjectionExpression, params.ExpressionAttributeNames, nil)
	return &dynamodb.GetItemOutput{Item: getProjection(v, projection)}, nil
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
	return copyItem(item), nil
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
		valid, err := condition.Evaluate(item)
		if err != nil {
			return nil, err
		}
		if !valid {
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
	item = copyItem(item)
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
		valid, err := condition.Evaluate(item)
		if err != nil {
			return nil, err
		}
		if !valid {
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

	// Hold back the tail of each batch as unprocessed, mimicking a partial throttle. Decrement so
	// a caller that retries makes progress and eventually drains, which is what lets a test tell
	// "retries until done" apart from "gave up" or "never noticed".
	if m.NextBatchWriteUnprocessedCount > 0 {
		holdBack := m.NextBatchWriteUnprocessedCount
		m.NextBatchWriteUnprocessedCount--
		out.UnprocessedItems = make(map[string][]types.WriteRequest)
		applied := make(map[string][]types.WriteRequest)
		for table, requests := range params.RequestItems {
			if holdBack >= len(requests) {
				out.UnprocessedItems[table] = requests
				continue
			}
			split := len(requests) - holdBack
			applied[table] = requests[:split]
			out.UnprocessedItems[table] = requests[split:]
		}
		params = &dynamodb.BatchWriteItemInput{RequestItems: applied}
	}

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
	attr := ""
	for k, v := range params.ExpressionAttributeNames {
		attr += k + ":" + v + " "
	}
	for k, v := range params.ExpressionAttributeValues {
		attr += k + ":" + *SorN(v) + " "
	}
	// A Query with no key condition is a ValidationException, not a crash, so profiling must not dereference it before QueryInternal validates.
	m.profile.AddRead(*params.TableName, "Query", "KeyConditionExpression: "+aws.ToString(params.KeyConditionExpression)+" Key/Value: "+attr, 0)
	return m.QueryInternal(params)
}

func (m *DynamoDBMock) QueryInternal(input *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
	tableName, schema, idx, err := m.resolveTarget(*input.TableName, input.IndexName)
	if err != nil {
		return nil, err
	}
	if aws.ToBool(input.ConsistentRead) && idx != nil {
		return nil, fmt.Errorf("ValidationException: Consistent reads are not supported on global secondary indexes")
	}

	output := &dynamodb.QueryOutput{}
	keyCond := NewMexpression(input.KeyConditionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	filterCond := NewMexpression(input.FilterExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	projection := NewMexpression(input.ProjectionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)

	if !keyCond.PinsPartitionKey(schema[0]) {
		return nil, fmt.Errorf("ValidationException: Query key condition must fix the partition key %q with '='", schema[0])
	}

	// Rows matching the key condition. The filter is applied later because Limit caps what DynamoDB evaluates, not what survives filtering.
	var evaluated []map[string]types.AttributeValue

	if err := m.ForEachRow(tableName, func(item map[string]types.AttributeValue) error {
		matched, err := keyCond.Evaluate(item)
		if err != nil || !matched {
			return err
		}
		evaluated = append(evaluated, item)
		return nil
	}); err != nil {
		return nil, err
	}

	sortBySchema(evaluated, schema, input.ScanIndexForward)

	if len(input.ExclusiveStartKey) > 0 {
		evaluated = itemsAfterCursor(evaluated, input.ExclusiveStartKey, schema, input.ScanIndexForward)
	}

	if limit := m.pageLimit(input.Limit); limit > 0 && len(evaluated) > limit {
		output.LastEvaluatedKey = keyFromItem(evaluated[limit-1], schema)
		evaluated = evaluated[:limit]
	}
	output.ScannedCount = int32(len(evaluated))

	var matchingItems []map[string]types.AttributeValue
	for _, item := range evaluated {
		kept, err := filterCond.Evaluate(item)
		if err != nil {
			return nil, err
		}
		if kept {
			matchingItems = append(matchingItems, item)
		}
	}

	if input.Select == types.SelectCount {
		output.Count = int32(len(matchingItems))
		return output, nil
	}
	for _, item := range matchingItems {
		output.Items = append(output.Items, getProjection(applyIndexProjection(copyItem(item), idx, schema), projection))
	}
	output.Count = int32(len(output.Items))
	return output, nil
}

// An index entry is keyed by the index's own key followed by the base table's. That tail is what makes a hash-only index paginable: it breaks the ties the index key alone leaves.
func indexKeySchema(idx *IndexDetails, base TableDetails) []string {
	schema := []string{idx.PrimaryKey}
	if idx.SortKey != "" {
		schema = append(schema, idx.SortKey)
	}
	for _, attr := range baseKeySchema(base) {
		if !slices.Contains(schema, attr) {
			schema = append(schema, attr)
		}
	}
	return schema
}

// A read names its index either in IndexName, as production code does, or in place of the table name, as the mock's own callers do. Both resolve to the backing table and the index's key schema.
func (m *DynamoDBMock) resolveTarget(tableName string, indexName *string) (string, []string, *IndexDetails, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	if indexName != nil && *indexName != "" {
		idx, ok := m.sec_index[*indexName]
		if !ok {
			return "", nil, nil, &types.ResourceNotFoundException{Message: aws.String("Index not found")}
		}
		return idx.TableName, indexKeySchema(idx, m.tables[idx.TableName]), idx, nil
	}
	if table, ok := m.tables[tableName]; ok {
		return tableName, baseKeySchema(table), nil, nil
	}
	if idx, ok := m.sec_index[tableName]; ok {
		return idx.TableName, indexKeySchema(idx, m.tables[idx.TableName]), idx, nil
	}
	return "", nil, nil, &types.ResourceNotFoundException{Message: aws.String("Table not found")}
}

// An index only stores what it projects, so a query through one cannot see the rest of the base-table row even though the mock keeps it all in one place.
func applyIndexProjection(item map[string]types.AttributeValue, idx *IndexDetails, schema []string) map[string]types.AttributeValue {
	if idx == nil || idx.Projection == "" || idx.Projection == types.ProjectionTypeAll {
		return item
	}
	allowed := make(map[string]bool, len(schema)+len(idx.NonKeyAttributes))
	for _, attr := range schema {
		allowed[attr] = true
	}
	if idx.Projection == types.ProjectionTypeInclude {
		for _, attr := range idx.NonKeyAttributes {
			allowed[attr] = true
		}
	}
	for k := range item {
		if !allowed[k] {
			delete(item, k)
		}
	}
	return item
}

// A query's key schema is the ordered attribute list it is sorted and paginated by. It has to be unique per item: a cursor is a position in that order, so any two items comparing equal would make the resume point ambiguous and silently drop rows.
func baseKeySchema(t TableDetails) []string {
	if t.SortKey == "" {
		return []string{t.PrimaryKey}
	}
	return []string{t.PrimaryKey, t.SortKey}
}

func ascending(scanIndexForward *bool) bool {
	return scanIndexForward == nil || *scanIndexForward
}

func compareBySchema(a, b map[string]types.AttributeValue, schema []string) int {
	for _, attr := range schema {
		if c := CompareAttributeValues(a[attr], b[attr]); c != 0 {
			return c
		}
	}
	return 0
}

// DynamoDB always returns a Query in key order, whatever ScanIndexForward says; only the direction is the caller's to choose.
func sortBySchema(items []map[string]types.AttributeValue, schema []string, scanIndexForward *bool) {
	asc := ascending(scanIndexForward)
	sort.SliceStable(items, func(i, j int) bool {
		c := compareBySchema(items[i], items[j], schema)
		if asc {
			return c < 0
		}
		return c > 0
	})
}

// Attributes the cursor does not carry are skipped rather than compared against nil, so a cursor built by hand from an item still resolves: every row a single-partition query can return shares the value anyway.
func compareToCursor(item, cursor map[string]types.AttributeValue, schema []string) int {
	for _, attr := range schema {
		want, ok := cursor[attr]
		if !ok {
			continue
		}
		if c := CompareAttributeValues(item[attr], want); c != 0 {
			return c
		}
	}
	return 0
}

// pageLimit is the smaller of the caller's Limit and MaxPageItems, which stands in for the service's 1 MB page cap. Zero means unlimited.
func (m *DynamoDBMock) pageLimit(limit *int32) int {
	n := 0
	if limit != nil && *limit > 0 {
		n = int(*limit)
	}
	if m.MaxPageItems > 0 && (n == 0 || m.MaxPageItems < n) {
		n = m.MaxPageItems
	}
	return n
}

func keyFromItem(item map[string]types.AttributeValue, schema []string) map[string]types.AttributeValue {
	key := make(map[string]types.AttributeValue, len(schema))
	for _, attr := range schema {
		key[attr] = item[attr]
	}
	return key
}

// ExclusiveStartKey is a position, not an item: the row it names may have been deleted since the previous page, and DynamoDB still resumes at the next one. Items are already in query order here, so the predicate is monotonic and the first match is the resume point.
func itemsAfterCursor(items []map[string]types.AttributeValue, cursor map[string]types.AttributeValue, schema []string, scanIndexForward *bool) []map[string]types.AttributeValue {
	asc := ascending(scanIndexForward)
	return items[sort.Search(len(items), func(i int) bool {
		c := compareToCursor(items[i], cursor, schema)
		if asc {
			return c > 0
		}
		return c < 0
	}):]
}

// The real service leaves Scan order unspecified, but a cursor needs a stable position to resume from, so the mock scans in key order. Treat that order as an implementation detail: assert on the set a Scan returns, never on its sequence.
func (m *DynamoDBMock) ScanInternal(input *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
	tableName, schema, idx, err := m.resolveTarget(*input.TableName, input.IndexName)
	if err != nil {
		return nil, err
	}

	output := &dynamodb.ScanOutput{}
	filterCond := NewMexpression(input.FilterExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)
	projection := NewMexpression(input.ProjectionExpression, input.ExpressionAttributeNames, input.ExpressionAttributeValues)

	var evaluated []map[string]types.AttributeValue
	if err := m.ForEachRow(tableName, func(item map[string]types.AttributeValue) error {
		evaluated = append(evaluated, item)
		return nil
	}); err != nil {
		return nil, err
	}

	sortBySchema(evaluated, schema, nil)
	if len(input.ExclusiveStartKey) > 0 {
		evaluated = itemsAfterCursor(evaluated, input.ExclusiveStartKey, schema, nil)
	}
	if limit := m.pageLimit(input.Limit); limit > 0 && len(evaluated) > limit {
		output.LastEvaluatedKey = keyFromItem(evaluated[limit-1], schema)
		evaluated = evaluated[:limit]
	}
	output.ScannedCount = int32(len(evaluated))

	matched := 0
	for _, item := range evaluated {
		kept, err := filterCond.Evaluate(item)
		if err != nil {
			return nil, err
		}
		if !kept {
			continue
		}
		matched++
		if input.Select != types.SelectCount {
			output.Items = append(output.Items, getProjection(applyIndexProjection(copyItem(item), idx, schema), projection))
		}
	}
	// Count is what matched whatever the Select mode; SELECT_COUNT returns it without any Items.
	output.Count = int32(matched)
	return output, nil
}

func (m *DynamoDBMock) Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	m.profile.AddRead(*params.TableName, "Scan", "", 0)
	return m.ScanInternal(params)
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
		valid, err := condition.Evaluate(make(map[string]types.AttributeValue))
		if err != nil {
			return nil, err
		}
		if !valid {
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
	valid, err := condition.Evaluate(item)
	if err != nil {
		return nil, err
	}
	if valid {
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

	// The item read above is a copy, so it has to be written back explicitly; a REMOVE- or ADD-only expression would otherwise be silently lost.
	if err := m.putItem(tableName, pkey, skey, item); err != nil {
		return nil, err
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
