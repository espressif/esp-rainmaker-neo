// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package pkceutil_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPKCEUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PKCE Util Suite")
}

func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

var _ = Describe("VerifyS256", func() {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	It("accepts a verifier whose S256 hash equals the challenge", func() {
		Expect(pkceutil.VerifyS256(verifier, challengeFor(verifier))).To(BeTrue())
	})

	It("rejects a wrong verifier (negative)", func() {
		Expect(pkceutil.VerifyS256("not-the-verifier", challengeFor(verifier))).To(BeFalse())
	})

	It("rejects an empty verifier or challenge (negative)", func() {
		Expect(pkceutil.VerifyS256("", challengeFor(verifier))).To(BeFalse())
		Expect(pkceutil.VerifyS256(verifier, "")).To(BeFalse())
	})

	It("rejects a plain (non-hashed) challenge equal to the verifier (negative, S256-only)", func() {
		Expect(pkceutil.VerifyS256(verifier, verifier)).To(BeFalse())
	})
})
