// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = BeforeSuite(func() {
	_, err := test_utils.CreateCommonSummaryFile("mcp_tools_tests.txt")
	Expect(err).NotTo(HaveOccurred(), "Failed to create timing summary file")
})

func TestMCPTools(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCP Tools Suite")
}
