// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/espressif/esp-rainmaker-neo/src/tools/rmng-lint/norlog"

	"golang.org/x/tools/go/analysis/multichecker"
)

func main() {
	multichecker.Main(
		norlog.Analyzer,
	)
}
