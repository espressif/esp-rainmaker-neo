// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FormatErrorChain", func() {
	It("returns empty for nil", func() {
		Expect(FormatErrorChain(nil)).To(Equal(""))
	})

	It("returns the layer message for a single RMError", func() {
		Expect(FormatErrorChain(NewRMError(nil, "boom"))).To(Equal("boom"))
	})

	It("joins RMError layers with ': ' and skips empty middle layers", func() {
		// Matches the real registration shape: top wrapper, an empty-message
		// RMError in the middle (used purely for stack annotation), and a
		// plain leaf error.
		leaf := errors.New("failed to parse the uploaded certificate")
		mid := NewRMError(leaf, "")
		top := NewRMError(mid, "failed to validate ca or cert and get node id")
		Expect(FormatErrorChain(top)).To(Equal(
			"failed to validate ca or cert and get node id: failed to parse the uploaded certificate"))
	})

	It("stops at the first non-RMError to avoid double-printing stdlib wraps", func() {
		// fmt.Errorf("%w") already renders its inner error inline. If we kept
		// walking, the inner text would appear twice.
		inner := errors.New("inner detail")
		wrapped := fmt.Errorf("outer detail: %w", inner)
		Expect(FormatErrorChain(wrapped)).To(Equal("outer detail: inner detail"))
	})

	It("returns the plain message for a non-wrapped non-RMError", func() {
		Expect(FormatErrorChain(errors.New("just text"))).To(Equal("just text"))
	})
})
