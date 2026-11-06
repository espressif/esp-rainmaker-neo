// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"slices"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

// exprParts lists a request's optional expression parts; unset parts are skipped.
type exprParts struct {
	keyCond *expression.KeyConditionBuilder
	cond    expression.ConditionBuilder
	filter  expression.ConditionBuilder
	update  *expression.UpdateBuilder
	project []string
}

// build returns an empty Expression when no part is set, where Builder.Build would fail with "unset parameter"; an empty Expression answers nil from every accessor, so it drops straight into any input.
func (p exprParts) build() (expression.Expression, error) {
	var b expression.Builder
	set := false
	if p.keyCond != nil {
		b, set = b.WithKeyCondition(*p.keyCond), true
	}
	if p.cond.IsSet() {
		b, set = b.WithCondition(p.cond), true
	}
	if p.filter.IsSet() {
		b, set = b.WithFilter(p.filter), true
	}
	if p.update != nil {
		b, set = b.WithUpdate(*p.update), true
	}
	if len(p.project) > 0 {
		b, set = b.WithProjection(projection(p.project)), true
	}
	if !set {
		return expression.Expression{}, nil
	}
	return b.Build()
}

// and joins the conditions that are set, skipping zero values.
func and(conds ...expression.ConditionBuilder) expression.ConditionBuilder {
	var out expression.ConditionBuilder
	for _, c := range conds {
		switch {
		case !c.IsSet():
		case !out.IsSet():
			out = c
		default:
			out = out.And(c)
		}
	}
	return out
}

func projection(attrs []string) expression.ProjectionBuilder {
	var p expression.ProjectionBuilder
	for _, a := range attrs {
		p = p.AddNames(expression.Name(a))
	}
	return p
}

// withKeys returns attrs plus any non-empty key name it lacks, so a projected row can still yield its key.
func withKeys(attrs, keys []string) []string {
	out := slices.Clone(attrs)
	for _, k := range keys {
		if k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func (t *Table[T]) mustExist() expression.ConditionBuilder {
	return expression.AttributeExists(expression.Name(t.schema.HashKey))
}

func (t *Table[T]) mustNotExist() expression.ConditionBuilder {
	return expression.AttributeNotExists(expression.Name(t.schema.HashKey))
}

func (t *Table[T]) createCondition(cond expression.ConditionBuilder) expression.ConditionBuilder {
	return and(t.mustNotExist(), cond)
}

func (t *Table[T]) existingCondition(cond expression.ConditionBuilder) expression.ConditionBuilder {
	return and(t.mustExist(), cond)
}
