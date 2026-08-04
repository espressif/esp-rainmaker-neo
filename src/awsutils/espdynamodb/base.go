// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package espdynamodb

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type EspDB struct {
	DB *db.DB
	// Should a DB Context have a 'User'? Well at least we need a
	// Accessor - ID and their permission set
	Ctx *rmngctx.RmngContext
}

func NewEspDB(ctx *rmngctx.RmngContext) EspDB {
	return EspDB{
		DB:  db.NewDB(ctx),
		Ctx: ctx,
	}
}
