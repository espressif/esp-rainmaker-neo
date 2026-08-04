// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

/*
Package db: rmng-admin-configs table — the shared store for runtime-set
admin configuration that needs to survive CloudFormation redeploys.

Table Name: rmng-admin-configs
Primary Key: config_key (Partition Key, String)

Schema (per-feature, attributes other than config_key are opaque to the
table):
- config_key (String): partition key. Each runtime-flippable feature
  picks its own opaque key — e.g., "iot_event_mode".

For config_key="iot_event_mode" the attributes are:
- presence (String): "direct" | "sqs"
- publish_input (String): "direct" | "sqs"
- updated_at (Number): unix milliseconds
- updated_by (String): superAdmin user id (or "system" when written by
  the deploy-time reapply path)

Access Control:
- All access goes through the AdminConfigDB methods below.
- Reads require AdminConfigGet on the config_key.
- Writes require AdminConfigSet on the config_key.
- Granted only to SystemActor (deploy-time reapply path, full access)
  or to superAdmin callers (after IsSuperAdmin passes at the lambda
  layer, which then constructs a SystemActor context for dbcore.DB access).
- This table is never reachable from regular user-facing APIs.

See:
- misc/specs/iot_event_mode.md §4.4 for the drift-correction reapply
  flow that consumes this table.
- src/admin/admin_config/base.py for the CDK provisioning.
*/

package admin_config_db

import (
	"fmt"
	dbcore "github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	AdminConfigTableName    = "rmng-admin-configs"
	adminConfigPartitionKey = "config_key"
	IoTEventModeConfigKey   = "iot_event_mode"
)

// IoTEventModeConfig is the row stored under config_key="iot_event_mode".
// Both Presence and PublishInput are one of "direct" | "sqs".
type IoTEventModeConfig struct {
	Presence     string `dynamodbav:"presence"`
	PublishInput string `dynamodbav:"publish_input"`
	UpdatedAt    int64  `dynamodbav:"updated_at"`
	UpdatedBy    string `dynamodbav:"updated_by"`
}

type AdminConfigDB struct {
	dbcore.DB
}

func NewAdminConfigDB(ctx *rmngctx.RmngContext) *AdminConfigDB {
	return &AdminConfigDB{DB: *dbcore.NewDB(ctx)}
}

// GetIoTEventMode returns the durably-stored IoT-event-mode row. A nil
// return value with nil error means the row does not exist (fresh
// deployment, never flipped) — callers treat that as "stay on the
// CFN-synthesized default".
func (a *AdminConfigDB) GetIoTEventMode() (*IoTEventModeConfig, error) {
	if err := a.IsAuthorized(utils.AdminConfigGet, IoTEventModeConfigKey); err != nil {
		return nil, err
	}

	out, err := a.GetItem(a.Ctx.Context, &dynamodb.GetItemInput{
		TableName: aws.String(AdminConfigTableName),
		Key: map[string]types.AttributeValue{
			adminConfigPartitionKey: &types.AttributeValueMemberS{Value: IoTEventModeConfigKey},
		},
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get iot_event_mode admin config")
	}
	if out.Item == nil {
		return nil, nil
	}

	var cfg IoTEventModeConfig
	if err := attributevalue.UnmarshalMap(out.Item, &cfg); err != nil {
		return nil, rmerror.NewRMError(err, "failed to unmarshal iot_event_mode admin config")
	}
	return &cfg, nil
}

// SetIoTEventMode persists the runtime-set modes so the reapply custom
// resource can restore them on the next stack update. updatedBy should be
// the caller's user id (or utils.SYSTEM_ACTOR for system-driven writes).
func (a *AdminConfigDB) SetIoTEventMode(presence, publishInput, updatedBy string) error {
	if err := a.IsAuthorized(utils.AdminConfigSet, IoTEventModeConfigKey); err != nil {
		return err
	}

	_, err := a.UpdateItem(a.Ctx.Context, &dynamodb.UpdateItemInput{
		TableName: aws.String(AdminConfigTableName),
		Key: map[string]types.AttributeValue{
			adminConfigPartitionKey: &types.AttributeValueMemberS{Value: IoTEventModeConfigKey},
		},
		UpdateExpression: aws.String("SET presence = :p, publish_input = :pi, updated_at = :ts, updated_by = :ub"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":p":  &types.AttributeValueMemberS{Value: presence},
			":pi": &types.AttributeValueMemberS{Value: publishInput},
			":ts": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().UnixMilli())},
			":ub": &types.AttributeValueMemberS{Value: updatedBy},
		},
	})
	if err != nil {
		return rmerror.NewRMError(err, "failed to write iot_event_mode admin config")
	}
	return nil
}
