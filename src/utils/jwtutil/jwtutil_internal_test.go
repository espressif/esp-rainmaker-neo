// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package jwtutil

import (
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("stampUserClaims", func() {
	It("adds a set claim under its json name next to the registered ones", func() {
		token := jwt.MapClaims{"sub": "u1"}
		Expect(stampUserClaims(token, UserClaims{Provider: "google"})).To(Succeed())
		Expect(token).To(Equal(jwt.MapClaims{"sub": "u1", "idp": "google"}))
	})

	It("refuses to overwrite a claim the token already has, leaving its value alone (negative)", func() {
		token := jwt.MapClaims{"sid": "registered"}
		Expect(stampUserClaims(token, UserClaims{SID: "sid-1"})).To(MatchError(ContainSubstring(`"sid" would overwrite`)))
		Expect(token["sid"]).To(Equal("registered"))
	})
})
