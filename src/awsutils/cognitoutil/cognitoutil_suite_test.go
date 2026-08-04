// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package cognitoutil_test is an external test package. test/testutil reaches
// cognitoutil transitively through src/mock, so an in-package test could not import the
// shared harness. Testing from outside also confines these specs to the exported surface
// callers actually use.
package cognitoutil_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCognitoutil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cognitoutil Suite")
}
