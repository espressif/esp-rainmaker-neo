// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package good

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

func handler(ctx context.Context) {
	// Passing context — no diagnostics expected.
	rlog.Error(ctx).Msg("oops")
	rlog.Info(ctx).Msg("info")
	rlog.Warn(ctx).Msg("warn")
	rlog.Debug(ctx).Msg("debug")

	// Non-level package-level helpers are fine.
	_ = rlog.With()
}

func noContext() {
	// No context parameter — nil is acceptable.
	rlog.Error(nil).Msg("no context available")
	rlog.Info(nil).Msg("init code")
	rlog.Debug(nil).Msg("startup")
}
