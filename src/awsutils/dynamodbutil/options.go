// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

// ReadOpts tunes Get and BatchGet. The zero value reads eventually consistent and returns every attribute.
type ReadOpts struct {
	Consistent bool     // default false: eventually consistent
	Project    []string // default empty: every attribute
}

// WriteOpts tunes Create, Put, Update, Upsert, UpdateFrom, Delete and DeleteAllowMissing. The zero value applies only the operation's own condition.
type WriteOpts struct {
	If        expression.ConditionBuilder // default unset: only the operation's own condition; set: ANDed with it
	ReturnOld bool                        // default false: a condition failure carries no item; true: read it with OldItem
}

// TxOpts tunes CreateOp, PutOp, UpdateOp and DeleteOp. It has no ReturnOld, as OldItem cannot read one back from a cancelled transaction.
type TxOpts struct {
	If expression.ConditionBuilder // default unset: only the operation's own condition; set: ANDed with it
}

// QueryOpts tunes Query, which returns one page.
type QueryOpts struct {
	Index      string                      // default "": the base table
	Filter     expression.ConditionBuilder // default unset: every item the key condition matches
	Descending bool                        // default false: ascending sort-key order
	Consistent bool                        // default false: eventually consistent; true is not allowed on a GSI
	Project    []string                    // default empty: every attribute; the key attributes are always added
	Limit      int                         // default 0: every match; counted after the filter
	StartToken string                      // default "": the first page; pass the previous page's NextToken
}

// QueryEachOpts tunes QueryEach, which walks every match, so it has no Limit or StartToken.
type QueryEachOpts struct {
	Index      string                      // default "": the base table
	Filter     expression.ConditionBuilder // default unset: every item the key condition matches
	Descending bool                        // default false: ascending sort-key order
	Consistent bool                        // default false: eventually consistent; true is not allowed on a GSI
	Project    []string                    // default empty: every attribute
}

// QueryDeleteOpts tunes QueryDelete, which deletes every match. No Project or Descending: it reads only the keys, and order does not change what is deleted.
type QueryDeleteOpts struct {
	Index      string                      // default "": the base table
	Filter     expression.ConditionBuilder // default unset: delete every item the key condition matches
	Consistent bool                        // default false: may miss a row written just before the call; true is not allowed on a GSI
}

// QueryCountOpts tunes QueryCount. No Project, as DynamoDB cannot project a count.
type QueryCountOpts struct {
	Index      string                      // default "": the base table
	Filter     expression.ConditionBuilder // default unset: count every item the key condition matches
	Consistent bool                        // default false: eventually consistent; true is not allowed on a GSI
}

// ScanOpts tunes Scan, which returns one page of the base table. No Descending, as DynamoDB scans in its own order.
type ScanOpts struct {
	Filter     expression.ConditionBuilder // default unset: every item
	Consistent bool                        // default false: eventually consistent
	Project    []string                    // default empty: every attribute; the key attributes are always added
	Limit      int                         // default 0: every item; counted after the filter
	StartToken string                      // default "": the first page; pass the previous page's NextToken
}

// ScanEachOpts tunes ScanEach, which walks every item, so it has no Limit or StartToken.
type ScanEachOpts struct {
	Filter     expression.ConditionBuilder // default unset: every item
	Consistent bool                        // default false: eventually consistent
	Project    []string                    // default empty: every attribute
}

// BatchWriteOpts tunes BatchPut and BatchDelete.
type BatchWriteOpts struct {
	Sequential bool // default false: chunks in parallel; true: one chunk at a time, stopping at the first failure
}

// readParams is what every query and scan options type reduces to.
type readParams struct {
	index      string
	filter     expression.ConditionBuilder
	descending bool
	consistent bool
	project    []string
	limit      int
	startToken string
}

func (o QueryOpts) params() readParams {
	return readParams{index: o.Index, filter: o.Filter, descending: o.Descending, consistent: o.Consistent, project: o.Project, limit: o.Limit, startToken: o.StartToken}
}

func (o QueryEachOpts) params() readParams {
	return readParams{index: o.Index, filter: o.Filter, descending: o.Descending, consistent: o.Consistent, project: o.Project}
}

func (o QueryDeleteOpts) params() readParams {
	return readParams{index: o.Index, filter: o.Filter, consistent: o.Consistent}
}

func (o QueryCountOpts) params() readParams {
	return readParams{index: o.Index, filter: o.Filter, consistent: o.Consistent}
}

func (o ScanOpts) params() readParams {
	return readParams{filter: o.Filter, consistent: o.Consistent, project: o.Project, limit: o.Limit, startToken: o.StartToken}
}

func (o ScanEachOpts) params() readParams {
	return readParams{filter: o.Filter, consistent: o.Consistent, project: o.Project}
}

// single reads a variadic options argument: none gives the zero value. More than one is a coding mistake, so it panics, as Key does on a wrong key.
func single[O any](op string, opts []O) O {
	switch len(opts) {
	case 0:
		var zero O
		return zero
	case 1:
		return opts[0]
	}
	panic(fmt.Sprintf("dynamodbutil: %s takes at most one %T", op, opts[0]))
}
