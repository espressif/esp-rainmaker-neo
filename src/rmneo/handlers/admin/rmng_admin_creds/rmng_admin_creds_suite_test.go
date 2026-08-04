// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRmngAdminCredsMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Rmng Admin Creds Main Suite")
}
