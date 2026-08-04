// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// One suite bootstrap for the merged claim handler. The initiate and verify
// specs live in their own files and are collected into this single run.
func TestClaimHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Claim Handler Suite")
}
