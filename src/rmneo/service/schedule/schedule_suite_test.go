// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package schedule_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSchedule(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Schedule Suite")
}
