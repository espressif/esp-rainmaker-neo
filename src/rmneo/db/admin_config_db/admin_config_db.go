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
- timeseries (String): "direct" | "sqs"
- updated_at (Number): unix milliseconds
- updated_by (String): admin user id (or "system" when written by
  the deploy-time reapply path)

Access Control:
- All access goes through the AdminConfigDB methods below.
- Reads require AdminConfigGet on the config_key.
- Writes require AdminConfigSet on the config_key.
- Granted only to SystemActor (deploy-time reapply path, full access)
  or to admin callers (after IsAdmin passes at the lambda
  layer, which then constructs a SystemActor context for AdminConfigDB access).
- This table is never reachable from regular user-facing APIs.

See:
- docs/en/specs/iot_event_mode.md §4.4 for the drift-correction reapply
  flow that consumes this table.
- src/rmneo/handlers/admin/admin_config/base.py for the CDK provisioning.
*/

package admin_config_db

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	AdminConfigTableName    = "rmng-admin-configs"
	adminConfigPartitionKey = "config_key"
	IoTEventModeConfigKey   = "iot_event_mode"
)

// IoTEventModeConfig is the row stored under config_key="iot_event_mode".
// Presence, PublishInput, and Timeseries are one of "direct" | "sqs".
type IoTEventModeConfig struct {
	Presence     string `dynamodbav:"presence"`
	PublishInput string `dynamodbav:"publish_input"`
	Timeseries   string `dynamodbav:"timeseries"`
	UpdatedAt    int64  `dynamodbav:"updated_at"`
	UpdatedBy    string `dynamodbav:"updated_by"`
}

type AdminConfigDB struct {
	Ctx     *rmngctx.RmngContext
	configs *ddbu.Table[IoTEventModeConfig]
}

func NewAdminConfigDB(ctx *rmngctx.RmngContext) *AdminConfigDB {
	return &AdminConfigDB{
		Ctx: ctx,
		configs: ddbu.NewTable[IoTEventModeConfig](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:    AdminConfigTableName,
			HashKey: adminConfigPartitionKey,
		}),
	}
}

// GetIoTEventMode returns the durably-stored IoT-event-mode row. A nil
// return value with nil error means the row does not exist (fresh
// deployment, never flipped) — callers treat that as "stay on the
// CFN-synthesized default".
func (a *AdminConfigDB) GetIoTEventMode() (*IoTEventModeConfig, error) {
	if err := a.Ctx.IsAuthorized(utils.AdminConfigGet, IoTEventModeConfigKey); err != nil {
		return nil, err
	}

	cfg, found, err := a.configs.Get(a.Ctx.Context, a.configs.Key(IoTEventModeConfigKey))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get iot_event_mode admin config")
	}
	if !found {
		return nil, nil
	}
	return &cfg, nil
}

// SetIoTEventMode persists the runtime-set modes so the reapply custom
// resource can restore them on the next stack update. updatedBy should be
// the caller's user id (or utils.SYSTEM_ACTOR for system-driven writes).
func (a *AdminConfigDB) SetIoTEventMode(presence, publishInput, timeseries, updatedBy string) error {
	if err := a.Ctx.IsAuthorized(utils.AdminConfigSet, IoTEventModeConfigKey); err != nil {
		return err
	}

	update := expression.Set(expression.Name("presence"), expression.Value(presence)).
		Set(expression.Name("publish_input"), expression.Value(publishInput)).
		Set(expression.Name("timeseries"), expression.Value(timeseries)).
		Set(expression.Name("updated_at"), expression.Value(time.Now().UnixMilli())).
		Set(expression.Name("updated_by"), expression.Value(updatedBy))
	if _, err := a.configs.Upsert(a.Ctx.Context, a.configs.Key(IoTEventModeConfigKey), update); err != nil {
		return rmerror.NewRMError(err, "failed to write iot_event_mode admin config")
	}
	return nil
}
