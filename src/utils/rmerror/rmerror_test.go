// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// CustomError is a test error type
type CustomError struct {
	msg string
}

func (e *CustomError) Error() string { return e.msg }

var _ = Describe("RMError", func() {
	Context("Error Creation and Wrapping", func() {
		It("should create a new RMError with correct file and line info", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "wrapped message")

			Expect(rmErr.Err).To(Equal(baseErr))
			Expect(rmErr.Message).To(Equal("wrapped message"))
			Expect(rmErr.File).To(ContainSubstring("rmerror_test.go"))
			Expect(rmErr.Line).To(BeNumerically(">", 0))
		})

		It("should properly wrap and unwrap errors", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "first wrap")
			rmErr2 := NewRMError(rmErr, "second wrap")

			// Test unwrapping chain
			unwrapped := errors.Unwrap(rmErr2)
			Expect(unwrapped).To(Equal(rmErr))

			unwrapped = errors.Unwrap(unwrapped)
			Expect(unwrapped).To(Equal(baseErr))

			// Final unwrap should be nil
			unwrapped = errors.Unwrap(unwrapped)
			Expect(unwrapped).To(BeNil())
		})

		It("should work with errors.Is", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "wrapped message")

			// Should find the base error through wrapping
			Expect(errors.Is(rmErr, baseErr)).To(BeTrue())

			// Should not match a different error
			otherErr := errors.New("different error")
			Expect(errors.Is(rmErr, otherErr)).To(BeFalse())
		})

		It("should work with errors.As", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "wrapped message")

			var targetRMErr *RMError
			Expect(errors.As(rmErr, &targetRMErr)).To(BeTrue())
			Expect(targetRMErr.Message).To(Equal("wrapped message"))
		})

		It("should work with errors.Is through multiple levels of wrapping", func() {
			baseErr := errors.New("base error")
			rmErr1 := NewRMError(baseErr, "first wrap")
			rmErr2 := NewRMError(rmErr1, "second wrap")
			rmErr3 := NewRMError(rmErr2, "third wrap")

			// Should find the base error through multiple levels of wrapping
			Expect(errors.Is(rmErr3, baseErr)).To(BeTrue())
			Expect(errors.Is(rmErr3, rmErr1)).To(BeTrue())
			Expect(errors.Is(rmErr3, rmErr2)).To(BeTrue())

			// Should match itself
			Expect(errors.Is(rmErr3, rmErr3)).To(BeTrue())

			// Should not match unrelated errors
			otherErr := errors.New("different error")
			otherRMErr := NewRMError(otherErr, "other wrap")
			Expect(errors.Is(rmErr3, otherErr)).To(BeFalse())
			Expect(errors.Is(rmErr3, otherRMErr)).To(BeFalse())
		})

		It("should work with errors.As for different error types", func() {
			customErr := &CustomError{msg: "custom error"}
			rmErr1 := NewRMError(customErr, "first wrap")
			rmErr2 := NewRMError(rmErr1, "second wrap")

			// Should be able to extract RMError
			var targetRMErr *RMError
			Expect(errors.As(rmErr2, &targetRMErr)).To(BeTrue())
			Expect(targetRMErr.Message).To(Equal("second wrap"))

			// Should be able to extract the custom error type
			var targetCustomErr *CustomError
			Expect(errors.As(rmErr2, &targetCustomErr)).To(BeTrue())
			Expect(targetCustomErr.msg).To(Equal("custom error"))

		})
	})

	Context("Error Logging", func() {
		It("should generate correct error stack trace string", func() {
			baseErr := errors.New("base error")
			rmErr1 := NewRMError(baseErr, "first wrap")
			rmErr2 := NewRMError(rmErr1, "second wrap")

			stackTrace := logErrorToStr("test.go", 100, rmErr2)

			// Verify stack trace format
			Expect(stackTrace).To(ContainSubstring("----"))
			Expect(stackTrace).To(ContainSubstring("[Error][test.go:100]"))
			Expect(stackTrace).To(ContainSubstring("second wrap"))
			Expect(stackTrace).To(ContainSubstring("first wrap"))
			Expect(stackTrace).To(ContainSubstring("base error"))
		})

		It("should handle mixed error types in stack", func() {
			baseErr := errors.New("base error")
			stdErr := errors.New("standard error")
			rmErr1 := NewRMError(baseErr, "first wrap")
			rmErr2 := NewRMError(stdErr, "second wrap")

			// Test both error chains
			stackTrace1 := logErrorToStr("test.go", 100, rmErr1)
			stackTrace2 := logErrorToStr("test.go", 100, rmErr2)

			// Verify all error types are included
			Expect(stackTrace1).To(ContainSubstring("first wrap"))
			Expect(stackTrace1).To(ContainSubstring("base error"))
			Expect(stackTrace2).To(ContainSubstring("second wrap"))
			Expect(stackTrace2).To(ContainSubstring("standard error"))
		})

		It("should format error message correctly", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "test message")

			errStr := rmErr.Error()
			Expect(errStr).To(Equal("test message"))
		})
	})

	Context("Error Utilities", func() {
		It("should generate stack trace with ErrorWithStack", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "wrapped error")

			stackTrace := ErrorWithStack(rmErr)

			// Verify basic stack trace components
			Expect(stackTrace).To(ContainSubstring("----"))
			Expect(stackTrace).To(ContainSubstring("[Error]"))
			Expect(stackTrace).To(ContainSubstring("wrapped error"))
			Expect(stackTrace).To(ContainSubstring("base error"))
		})

		It("should handle nil errors gracefully", func() {
			stackTrace := logErrorToStr("test.go", 100, nil)

			// Should still format basic info even with nil error
			Expect(stackTrace).To(ContainSubstring("[Error][test.go:100]"))
			Expect(strings.Count(stackTrace, "\n")).To(Equal(3)) // Header, empty line, footer
		})
	})
})
