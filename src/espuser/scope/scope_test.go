// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package scope_test

import (
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HasOfflineAccess", func() {
	DescribeTable("matches only the whole offline_access value",
		func(requested string, want bool) {
			Expect(scope.HasOfflineAccess(requested)).To(Equal(want))
		},
		Entry("present", "openid offline_access email", true),
		Entry("absent", "openid email", false),
		Entry("look-alike prefix", "offline_access_extra", false),
		Entry("look-alike suffix", "x:offline_access", false),
	)
})

var _ = Describe("Without", func() {
	DescribeTable("removes every dropped value and keeps the order",
		func(requested string, drop []string, want string) {
			Expect(scope.Without(requested, drop...)).To(Equal(want))
		},
		Entry("drops one", "openid offline_access email", []string{scope.OfflineAccess}, "openid email"),
		Entry("drops several", "openid offline_access email phone", []string{scope.OfflineAccess, scope.Phone}, "openid email"),
		Entry("drops a repeated value", "offline_access openid offline_access", []string{scope.OfflineAccess}, "openid"),
		Entry("normalises extra spaces", "  openid   email ", []string{scope.OfflineAccess}, "openid email"),
		Entry("leaves a look-alike", "openid offline_access_extra", []string{scope.OfflineAccess}, "openid offline_access_extra"),
		Entry("nothing to drop", "openid email", nil, "openid email"),
		Entry("everything dropped", "offline_access", []string{scope.OfflineAccess}, ""),
		Entry("empty scope", "", []string{scope.OfflineAccess}, ""),
	)
})
