// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserAuthHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Auth Handler Suite")
}

var _ = Describe("provider-specific wording", func() {
	// Both messages name RainMaker only when RainMaker's own pool is what backs the deployment;
	// against a registered external provider that sentence would name the wrong product.

	It("names RainMaker on signup for the inbuilt provider", func() {
		Expect(signupMessage(true)).To(Equal(
			"Code sent. Existing RainMaker users must signin or reset password."))
	})

	It("drops the product name on signup for an external provider (negative)", func() {
		Expect(signupMessage(false)).To(Equal(
			"Code sent. Existing users must signin or reset password."))
		Expect(signupMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("keeps the verify failure uniform but helpful, naming RainMaker on the inbuilt provider", func() {
		Expect(verifyFailedMessage(true)).To(Equal(
			"Invalid code or RainMaker account already exists. Try signin or reset password."))
	})

	It("drops the product name from the verify failure for an external provider (negative)", func() {
		Expect(verifyFailedMessage(false)).To(Equal(
			"Invalid code or account already exists. Try signin or reset password."))
		Expect(verifyFailedMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("keeps the signup and verify messages distinct, so one cannot be mistaken for the other", func() {
		Expect(signupMessage(true)).NotTo(Equal(verifyFailedMessage(true)))
		Expect(signupMessage(false)).NotTo(Equal(verifyFailedMessage(false)))
	})

	It("tells a proven owner to sign in, naming RainMaker on the inbuilt provider", func() {
		Expect(accountExistsMessage(true)).To(Equal("Account already exists. RainMaker users: signin or reset password."))
	})

	It("drops the product name from the account-exists message for an external provider (negative)", func() {
		Expect(accountExistsMessage(false)).To(Equal("Account already exists. Signin or reset password."))
		Expect(accountExistsMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("advises a password reset only for an unconfirmed sign-in", func() {
		Expect(unconfirmedAccountMessage).To(ContainSubstring("reset your password"))
		// Distinct from the uniform refusal, which is what makes the branch observable at all.
		Expect(unconfirmedAccountMessage).NotTo(Equal("Authentication failed"))
	})
})
