// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package norlog_test

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/tools/rmng-lint/norlog"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNorlog(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, norlog.Analyzer, "bad", "good")
}
