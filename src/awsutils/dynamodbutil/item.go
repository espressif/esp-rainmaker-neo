// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Get reads one item; found is false when no item has key. With Project, an item holding none of the projected attributes also reads as not found, as DynamoDB returns it empty.
func (t *Table[T]) Get(ctx context.Context, key Key, opts ...ReadOpts) (item T, found bool, err error) {
	o := single("Get", opts)
	in := &dynamodb.GetItemInput{TableName: aws.String(t.schema.Name), Key: key}
	if o.Consistent {
		in.ConsistentRead = aws.Bool(true)
	}
	expr, err := exprParts{project: o.Project}.build()
	if err != nil {
		return item, false, t.wrap("get", err)
	}
	in.ProjectionExpression = expr.Projection()
	in.ExpressionAttributeNames = expr.Names()
	out, err := t.client.GetItem(ctx, in)
	if err != nil {
		return item, false, t.wrap("get", err)
	}
	if len(out.Item) == 0 {
		return item, false, nil
	}
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return item, false, t.wrap("get", err)
	}
	return item, true, nil
}

// Create writes item only if no item with its key exists.
func (t *Table[T]) Create(ctx context.Context, item T, opts ...WriteOpts) error {
	o := single("Create", opts)
	return t.put(ctx, "create", item, t.createCondition(o.If), o)
}

// Put writes item whole, replacing any existing item; only an If condition guards it.
func (t *Table[T]) Put(ctx context.Context, item T, opts ...WriteOpts) error {
	o := single("Put", opts)
	return t.put(ctx, "put", item, o.If, o)
}

func (t *Table[T]) put(ctx context.Context, op string, item T, cond expression.ConditionBuilder, o WriteOpts) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return t.wrap(op, err)
	}
	expr, err := exprParts{cond: cond}.build()
	if err != nil {
		return t.wrap(op, err)
	}
	in := &dynamodb.PutItemInput{
		TableName:                 aws.String(t.schema.Name),
		Item:                      av,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	if o.ReturnOld {
		in.ReturnValuesOnConditionCheckFailure = types.ReturnValuesOnConditionCheckFailureAllOld
	}
	_, err = t.client.PutItem(ctx, in)
	return t.wrap(op, err)
}

// Update applies update to an existing item and returns the item as it is afterwards. A missing item fails the condition.
func (t *Table[T]) Update(ctx context.Context, key Key, update expression.UpdateBuilder, opts ...WriteOpts) (T, error) {
	o := single("Update", opts)
	return t.update(ctx, "update", key, update, t.existingCondition(o.If), o)
}

// Upsert is Update that creates the item when it is missing.
func (t *Table[T]) Upsert(ctx context.Context, key Key, update expression.UpdateBuilder, opts ...WriteOpts) (T, error) {
	o := single("Upsert", opts)
	return t.update(ctx, "upsert", key, update, o.If, o)
}

func (t *Table[T]) update(ctx context.Context, op string, key Key, update expression.UpdateBuilder, cond expression.ConditionBuilder, o WriteOpts) (T, error) {
	var item T
	expr, err := exprParts{cond: cond, update: &update}.build()
	if err != nil {
		return item, t.wrap(op, err)
	}
	in := &dynamodb.UpdateItemInput{
		TableName:                 aws.String(t.schema.Name),
		Key:                       key,
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	}
	if o.ReturnOld {
		in.ReturnValuesOnConditionCheckFailure = types.ReturnValuesOnConditionCheckFailureAllOld
	}
	out, err := t.client.UpdateItem(ctx, in)
	if err != nil {
		return item, t.wrap(op, err)
	}
	if err := attributevalue.UnmarshalMap(out.Attributes, &item); err != nil {
		return item, t.wrap(op, err)
	}
	return item, nil
}

// UpdateFrom SETs every non-key attribute item marshals to, leaving attributes it omits untouched: {id:"a", name:"x"} with omitempty on an empty age updates name only.
func (t *Table[T]) UpdateFrom(ctx context.Context, item T, opts ...WriteOpts) (T, error) {
	var zero T
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return zero, t.wrap("update", err)
	}
	keyNames := t.baseKeyNames()
	key, err := pickKey(av, keyNames)
	if err != nil {
		return zero, t.wrap("update", err)
	}
	var update expression.UpdateBuilder
	fields := 0
	// Sorted so the generated expression, and so any test asserting on it, is stable.
	for _, name := range slices.Sorted(maps.Keys(av)) {
		if slices.Contains(keyNames, name) {
			continue
		}
		update = update.Set(expression.Name(name), expression.Value(av[name]))
		fields++
	}
	if fields == 0 {
		return zero, t.wrap("update", errors.New("item has no attributes besides its key"))
	}
	return t.Update(ctx, key, update, single("UpdateFrom", opts))
}

// Delete removes an existing item. A missing item fails the condition.
func (t *Table[T]) Delete(ctx context.Context, key Key, opts ...WriteOpts) error {
	o := single("Delete", opts)
	return t.delete(ctx, "delete", key, t.existingCondition(o.If), o)
}

// DeleteAllowMissing is Delete that succeeds when the item is already gone.
func (t *Table[T]) DeleteAllowMissing(ctx context.Context, key Key, opts ...WriteOpts) error {
	o := single("DeleteAllowMissing", opts)
	return t.delete(ctx, "delete allow missing", key, o.If, o)
}

func (t *Table[T]) delete(ctx context.Context, op string, key Key, cond expression.ConditionBuilder, o WriteOpts) error {
	expr, err := exprParts{cond: cond}.build()
	if err != nil {
		return t.wrap(op, err)
	}
	in := &dynamodb.DeleteItemInput{
		TableName:                 aws.String(t.schema.Name),
		Key:                       key,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}
	if o.ReturnOld {
		in.ReturnValuesOnConditionCheckFailure = types.ReturnValuesOnConditionCheckFailureAllOld
	}
	_, err = t.client.DeleteItem(ctx, in)
	return t.wrap(op, err)
}
