// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package bad

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

func handler(ctx context.Context) {
	rlog.Error(nil).Msg("oops")         // want `pass context to rlog.Error\(\) instead of nil`
	rlog.Info(nil).Msg("info")          // want `pass context to rlog.Info\(\) instead of nil`
	rlog.Warn(nil).Msg("warn")          // want `pass context to rlog.Warn\(\) instead of nil`
	rlog.Debug(nil).Msg("debug")        // want `pass context to rlog.Debug\(\) instead of nil`
	rlog.Trace(nil).Msg("trace")        // want `pass context to rlog.Trace\(\) instead of nil`
	rlog.Fatal(nil).Msg("fatal")        // want `pass context to rlog.Fatal\(\) instead of nil`
	rlog.Panic(nil).Msg("panic")        // want `pass context to rlog.Panic\(\) instead of nil`
	rlog.Err(nil, nil).Msg("err")       // want `pass context to rlog.Err\(\) instead of nil`
	rlog.Log(nil).Msg("log")            // want `pass context to rlog.Log\(\) instead of nil`
	rlog.WithLevel(nil, 0).Msg("level") // want `pass context to rlog.WithLevel\(\) instead of nil`
	rlog.Print(nil, "print")            // want `pass context to rlog.Print\(\) instead of nil`
	rlog.Printf(nil, "printf %s", "x")  // want `pass context to rlog.Printf\(\) instead of nil`

	_ = ctx
}
