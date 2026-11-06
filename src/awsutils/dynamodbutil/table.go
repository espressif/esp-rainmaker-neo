// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// Key is a primary key or pagination cursor. An alias, so SDK maps pass straight through.
type Key = map[string]types.AttributeValue

// IndexKeys names the key attributes of a secondary index.
type IndexKeys struct {
	HashKey  string
	RangeKey string
}

// Schema describes a table's keys. Every index a caller queries must be listed in Indexes.
type Schema struct {
	Name     string
	HashKey  string
	RangeKey string
	Indexes  map[string]IndexKeys
}

// Table is a typed handle on one DynamoDB table; T is the struct its items unmarshal into.
type Table[T any] struct {
	client   awscommon.DynamoDBClientInterface
	schema   Schema
	keyTypes map[string]string // key attribute -> "S", "N" or "B", as T stores it
}

func NewTable[T any](client awscommon.DynamoDBClientInterface, schema Schema) *Table[T] {
	return &Table[T]{client: client, schema: schema, keyTypes: keyTypes[T](schema)}
}

// Key builds a primary key from Go values: on a table keyed (node_id, ts), Key("n1", 5) -> {node_id: S "n1", ts: N "5"}. A range value is required exactly when the schema has a RangeKey, and each value must marshal to the type T stores that key as; a mismatch panics because it is a programming error.
func (t *Table[T]) Key(hash any, rangeVal ...any) Key {
	names := t.baseKeyNames()
	if len(rangeVal) != len(names)-1 {
		panic(fmt.Sprintf("dynamodbutil: %s.Key got %d range values for range key %q", t.schema.Name, len(rangeVal), t.schema.RangeKey))
	}
	values := append([]any{hash}, rangeVal...)
	key := make(Key, len(names))
	for i, n := range names {
		key[n] = t.keyValue(n, values[i])
	}
	return key
}

func (t *Table[T]) keyValue(name string, v any) types.AttributeValue {
	av := mustMarshal(v)
	if want, got := t.keyTypes[name], attrType(av); want != "" && got != want {
		panic(fmt.Sprintf("dynamodbutil: %s.Key got %s for %q, which %T stores as %s; pass a matching value, or use KeyOf(item) when a struct tag converts the field", t.schema.Name, got, name, *new(T), want))
	}
	return av
}

// keyTypes learns each key attribute's type from T's struct tags by marshalling a zero T. A key the zero value leaves out, such as one tagged omitempty, goes unchecked.
func keyTypes[T any](schema Schema) map[string]string {
	var zero T
	av, err := attributevalue.MarshalMap(zero)
	if err != nil {
		return nil
	}
	out := make(map[string]string, 2)
	for _, name := range []string{schema.HashKey, schema.RangeKey} {
		if t := attrType(av[name]); name != "" && t != "" {
			out[name] = t
		}
	}
	return out
}

func attrType(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberB:
		return "B"
	}
	return ""
}

// KeyOf extracts item's primary key using the schema.
func (t *Table[T]) KeyOf(item T) (Key, error) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return nil, t.wrap("key", err)
	}
	key, err := pickKey(av, t.baseKeyNames())
	return key, t.wrap("key", err)
}

func (t *Table[T]) baseKeyNames() []string {
	if t.schema.RangeKey == "" {
		return []string{t.schema.HashKey}
	}
	return []string{t.schema.HashKey, t.schema.RangeKey}
}

// keyNames lists the attributes that identify a row in index ("" = base table): the base key plus the index key, as a LastEvaluatedKey would carry them.
func (t *Table[T]) keyNames(index string) ([]string, error) {
	if index == "" {
		return t.baseKeyNames(), nil
	}
	idx, ok := t.schema.Indexes[index]
	if !ok {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("dynamodbutil: index %q is not declared in the schema of %s", index, t.schema.Name))
	}
	return withKeys(t.baseKeyNames(), []string{idx.HashKey, idx.RangeKey}), nil
}

func pickKey(item map[string]types.AttributeValue, names []string) (Key, error) {
	key := make(Key, len(names))
	for _, n := range names {
		v, ok := item[n]
		if !ok {
			return nil, rmerror.NewRMError(nil, fmt.Sprintf("item has no key attribute %q", n))
		}
		key[n] = v
	}
	return key, nil
}

// Only unsupported Go kinds (func, chan) fail to marshal, which no key value is.
func mustMarshal(v any) types.AttributeValue {
	av, err := attributevalue.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("dynamodbutil: cannot marshal key value %v: %v", v, err))
	}
	return av
}

// The RMError location points at this helper, so op names the caller instead.
func (t *Table[T]) wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return rmerror.NewRMError(err, fmt.Sprintf("dynamodbutil: %s %s", op, t.schema.Name))
}

func decode[T any](rows []map[string]types.AttributeValue) ([]T, error) {
	var items []T
	if len(rows) == 0 {
		return items, nil
	}
	if err := attributevalue.UnmarshalListOfMaps(rows, &items); err != nil {
		return nil, err
	}
	return items, nil
}
