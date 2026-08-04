// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-admin-config (PK config_name, SK subtype): admin-selected key-value config. First consumer is the active email sender per category (config_name="email-sender", subtype=<category>, value=<email>). Spec: espuser/docs/en/specs/email-sender.md.
package admin_config_db

import (
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/espdynamodb"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	adminConfigTableName = "espuser-admin-config"
	adminConfigHashKey   = "config_name"
	adminConfigRangeKey  = "subtype"

	ConfigEmailSender = "email-sender"
	CategoryGlobal    = "global"
)

type AdminConfigDB struct {
	espdynamodb.EspDB
}

func NewAdminConfigDB(ctx *rmngctx.RmngContext) *AdminConfigDB {
	return &AdminConfigDB{EspDB: espdynamodb.NewEspDB(ctx)}
}

type ConfigEntry struct {
	ConfigName string `dynamodbav:"config_name"`
	Subtype    string `dynamodbav:"subtype"`
	Value      string `dynamodbav:"value,omitempty"`
	UpdatedAt  int64  `dynamodbav:"updated_at,omitempty"`
}

// key-only struct so a Get never leaks non-key fields into the DynamoDB Key.

func getLastEvaluatedKey(e ConfigEntry, _ ...string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		adminConfigHashKey:  &types.AttributeValueMemberS{Value: e.ConfigName},
		adminConfigRangeKey: &types.AttributeValueMemberS{Value: e.Subtype},
	}
}

// Put is an unconditional upsert of one (configName, subtype) row.
func (db *AdminConfigDB) Put(configName, subtype, value string, updatedAt int64) error {
	av, err := attributevalue.MarshalMap(&ConfigEntry{
		ConfigName: configName, Subtype: subtype, Value: value, UpdatedAt: updatedAt,
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal admin config")
	}
	if _, err := db.DB.PutItem(db.Ctx.Context, &dynamodb.PutItemInput{
		TableName: aws.String(adminConfigTableName),
		Item:      av,
	}); err != nil {
		return rmerror.NewRMError(err, "failed to put admin config")
	}
	return nil
}

// Get returns the value, or empty string if absent.

// GetAll returns every row under configName.
func (db *AdminConfigDB) GetAll(configName string) ([]ConfigEntry, error) {
	keyCond := expression.Key(adminConfigHashKey).Equal(expression.Value(configName))
	expr, err := expression.NewBuilder().WithKeyCondition(keyCond).Build()
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to build admin config query")
	}
	rows, _, err := espdynamodb.DbQueryWithLoop(espdynamodb.QueryWithLoopInput[ConfigEntry]{
		DBHandle:  &db.EspDB,
		TableName: adminConfigTableName,
		Expr:      expr,
		GetKey:    getLastEvaluatedKey,
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query admin config")
	}
	return rows, nil
}
