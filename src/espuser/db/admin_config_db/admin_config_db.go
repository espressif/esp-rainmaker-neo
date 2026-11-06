// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-admin-config (PK config_name, SK subtype): admin-selected key-value config. First consumer is the active email sender per category (config_name="email-sender", subtype=<category>, value=<email>). Spec: espuser/docs/en/specs/email-sender.md.
package admin_config_db

import (
	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

const (
	adminConfigTableName = "espuser-admin-config"
	adminConfigHashKey   = "config_name"
	adminConfigRangeKey  = "subtype"

	ConfigEmailSender = "email-sender"
	CategoryGlobal    = "global"
)

type AdminConfigDB struct {
	Ctx     *rmngctx.RmngContext
	configs *ddbu.Table[ConfigEntry]
}

func NewAdminConfigDB(ctx *rmngctx.RmngContext) *AdminConfigDB {
	return &AdminConfigDB{
		Ctx: ctx,
		configs: ddbu.NewTable[ConfigEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:     adminConfigTableName,
			HashKey:  adminConfigHashKey,
			RangeKey: adminConfigRangeKey,
		}),
	}
}

type ConfigEntry struct {
	ConfigName string `dynamodbav:"config_name"`
	Subtype    string `dynamodbav:"subtype"`
	Value      string `dynamodbav:"value,omitempty"`
	UpdatedAt  int64  `dynamodbav:"updated_at,omitempty"`
}

// Put is an unconditional upsert of one (configName, subtype) row.
func (db *AdminConfigDB) Put(configName, subtype, value string, updatedAt int64) error {
	entry := ConfigEntry{ConfigName: configName, Subtype: subtype, Value: value, UpdatedAt: updatedAt}
	if err := db.configs.Put(db.Ctx.Context, entry); err != nil {
		return rmerror.NewRMError(err, "failed to put admin config")
	}
	return nil
}

// GetAll returns every row under configName.
func (db *AdminConfigDB) GetAll(configName string) ([]ConfigEntry, error) {
	page, err := db.configs.Query(db.Ctx.Context, expression.Key(adminConfigHashKey).Equal(expression.Value(configName)))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query admin config")
	}
	return page.Items, nil
}
