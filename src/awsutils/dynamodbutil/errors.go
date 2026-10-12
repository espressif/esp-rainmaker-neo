// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil

import (
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// ErrInvalidToken marks a StartToken that does not decode, so callers can answer it as a bad request rather than a query failure.
var ErrInvalidToken = errors.New("invalid pagination token")

// IsConditionFailed reports whether err comes from a failed condition: a conditional write, or a transaction cancelled by one of its conditions.
func IsConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return true
	}
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		for _, r := range tce.CancellationReasons {
			if aws.ToString(r.Code) == "ConditionalCheckFailed" {
				return true
			}
		}
	}
	return false
}

// OldItem returns the item a condition failure carries when the write set WriteOpts.ReturnOld.
func OldItem[T any](err error) (T, bool) {
	var item T
	var ccf *types.ConditionalCheckFailedException
	if !errors.As(err, &ccf) || len(ccf.Item) == 0 {
		return item, false
	}
	if attributevalue.UnmarshalMap(ccf.Item, &item) != nil {
		return item, false
	}
	return item, true
}
