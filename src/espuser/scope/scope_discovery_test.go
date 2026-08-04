// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package scope_test

import (
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestScopeDiscovery(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scope Discovery Suite")
}

var _ = Describe("advertised scopes", func() {
	It("matches the scopes the authorize endpoint grants", func() {
		Expect(oidc.SupportedScopes).To(ConsistOf(
			scope.OpenID, scope.Email, scope.Profile, scope.Phone,
		))
	})
})
