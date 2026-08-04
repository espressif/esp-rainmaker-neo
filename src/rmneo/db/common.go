// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type DB struct {
	DBUtil
	// Should a DB Context have a 'User'? Well at least we need a
	// Accessor - ID and their permission set
	Ctx *rmngctx.RmngContext
}

func NewDB(ctx *rmngctx.RmngContext) *DB {
	return &DB{
		DBUtil: DBUtil{
			DynamoDBClientInterface: awscommon.GetDynamoDBClient(),
		},
		Ctx: ctx,
	}
}

func (db *DB) IsAuthorized(action fmt.Stringer, resource string) error {
	return db.Ctx.IsAuthorized(action, resource)
}

func (db *DB) SetAllow(action fmt.Stringer, resource string) error {
	return db.Ctx.SetAllow(action, resource)
}

type DBUtil struct {
	awscommon.DynamoDBClientInterface
}

// GetStringAttr returns the value of a DynamoDB String (S) attribute. ok is false when the attribute is absent or is not a string, letting callers guard against malformed items instead of panicking on a raw type assertion. Used where a full attributevalue.UnmarshalMap is unwarranted (single scalar reads, or reading a raw attribute name).
func GetStringAttr(item map[string]types.AttributeValue, key string) (value string, ok bool) {
	s, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return "", false
	}
	return s.Value, true
}
