// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package otputil_test

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils/otputil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOTPUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OTPUtils Suite")
}

var _ = Describe("OTPUtils", func() {
	It("generates a 6-digit code that verifies against its own hash", func() {
		code, err := otputil.GenerateOTPCode()
		Expect(err).NotTo(HaveOccurred())
		Expect(code).To(HaveLen(6))

		h, err := otputil.HashOTP(code)
		Expect(err).NotTo(HaveOccurred())
		Expect(otputil.VerifyOTP(code, h)).To(BeTrue())
	})

	It("never stores the plaintext code in the hash", func() {
		h, err := otputil.HashOTP("987654")
		Expect(err).NotTo(HaveOccurred())
		Expect(h).NotTo(ContainSubstring("987654"))
	})

	It("rejects a wrong code (negative)", func() {
		h, err := otputil.HashOTP("123456")
		Expect(err).NotTo(HaveOccurred())
		Expect(otputil.VerifyOTP("000000", h)).To(BeFalse())
	})

	It("rejects a malformed stored hash without panicking (negative)", func() {
		Expect(otputil.VerifyOTP("123456", "not-a-valid-hash")).To(BeFalse())
	})

	It("mints opaque flow ids with the fl_ prefix", func() {
		id, err := otputil.GenerateFlowID()
		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(HavePrefix("fl_"))
	})
})
