// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"errors"
	"fmt"
	"net/http"

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
			Expect(rmErr.ClientStatus).To(BeZero())
			Expect(rmErr.ClientMessage).To(BeEmpty())
		})

		It("should properly wrap and unwrap errors", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "first wrap").WithClientStatus(http.StatusBadRequest, "Bad input")
			rmErr2 := NewRMError(rmErr, "second wrap")

			// Test unwrapping chain
			unwrapped := errors.Unwrap(rmErr2)
			Expect(unwrapped).To(Equal(rmErr))
			Expect(rmErr.ClientStatus).To(Equal(http.StatusBadRequest))
			Expect(rmErr.ClientMessage).To(Equal("Bad input"))

			unwrapped = errors.Unwrap(unwrapped)
			Expect(unwrapped).To(Equal(baseErr))

			// Final unwrap should be nil
			unwrapped = errors.Unwrap(unwrapped)
			Expect(unwrapped).To(BeNil())
		})

		It("should work with errors.Is", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(baseErr, "wrapped message").WithClientStatus(http.StatusForbidden, "No access")

			// Should find the base error through wrapping, with or without a client status
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
			Expect(targetRMErr.ClientStatus).To(BeZero())
		})

		It("should work with errors.Is through multiple levels of wrapping", func() {
			baseErr := errors.New("base error")
			rmErr1 := NewRMError(baseErr, "first wrap").WithClientStatus(http.StatusNotFound, "Not found")
			rmErr2 := NewRMError(rmErr1, "second wrap")
			rmErr3 := NewRMError(rmErr2, "third wrap")

			// Should find the base error through multiple levels of wrapping
			Expect(errors.Is(rmErr3, baseErr)).To(BeTrue())

			// The deepest level's client status reaches the top unchanged
			status, message := clientError(rmErr3)
			Expect(status).To(Equal(http.StatusNotFound))
			Expect(message).To(Equal("Not found"))
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

	Context("ErrorWithStack", func() {
		It("lists every layer with its file and line", func() {
			stack := ErrorWithStack(NewRMError(NewRMError(errors.New("base error"), "first wrap"), "second wrap"))
			Expect(stack).To(ContainSubstring("[Error]"))
			Expect(stack).To(ContainSubstring("rmerror_test.go"))
			Expect(stack).To(ContainSubstring("second wrap"))
			Expect(stack).To(ContainSubstring("first wrap"))
			Expect(stack).To(ContainSubstring("base error"))
		})
	})

	Context("TopMessage", func() {
		It("returns only the outermost level's message", func() {
			err := NewRMError(NewRMError(errors.New("throttled"), "dynamodbutil: get groups"), "failed to get group")
			Expect(TopMessage(err)).To(Equal("failed to get group"))
		})

		It("returns a plain error's own text and an empty string for nil", func() {
			Expect(TopMessage(errors.New("plain"))).To(Equal("plain"))
			Expect(TopMessage(nil)).To(BeEmpty())
		})
	})

	Context("Error text", func() {
		It("returns the whole wrapped chain", func() {
			baseErr := errors.New("base error")
			rmErr := NewRMError(NewRMError(baseErr, "first wrap"), "second wrap")
			Expect(rmErr.Error()).To(Equal("second wrap: first wrap: base error"))
		})
	})
})

type statusResult struct {
	status  int
	message string
}

func resolve(err error) statusResult {
	status, message := clientError(err)
	return statusResult{status, message}
}

var _ = Describe("ClientError", func() {
	It("defaults to 500 when no level sets a status", func() {
		dbErr := errors.New("ProvisionedThroughputExceededException: rate exceeded")
		err := NewRMError(NewRMError(dbErr, "failed to query groups in DynamoDB"), "failed to check subgroup")
		Expect(resolve(err)).To(Equal(statusResult{http.StatusInternalServerError, "Internal Server Error"}))
	})

	It("defaults to 500 for a plain error and for nil", func() {
		Expect(resolve(errors.New("boom"))).To(Equal(statusResult{http.StatusInternalServerError, "Internal Server Error"}))
		Expect(resolve(nil)).To(Equal(statusResult{http.StatusInternalServerError, "Internal Server Error"}))
	})

	It("takes the status and message the deepest level set when no level above overrides", func() {
		err1 := NewRMError(nil, "subgroup does not exist").WithClientStatus(http.StatusBadRequest, "Subgroup not found")
		err3 := NewRMError(NewRMError(err1, "failed to check subgroup"), "failed to build scoped policy")
		Expect(resolve(err3)).To(Equal(statusResult{http.StatusBadRequest, "Subgroup not found"}))
	})

	It("lets an outer level override only the client message", func() {
		err1 := NewRMError(nil, "subgroup does not exist").WithClientStatus(http.StatusBadRequest, "Subgroup not found")
		err2 := NewRMError(err1, "assume role").WithClientMessage("Access denied")
		Expect(resolve(err2)).To(Equal(statusResult{http.StatusBadRequest, "Access denied"}))
	})

	It("lets an outer level override both status and message", func() {
		err1 := NewRMError(nil, "subgroup does not exist").WithClientStatus(http.StatusBadRequest, "Subgroup not found")
		err2 := NewRMError(err1, "assume role").WithClientStatus(http.StatusForbidden, "Access denied")
		Expect(resolve(err2)).To(Equal(statusResult{http.StatusForbidden, "Access denied"}))
	})

	It("uses the standard status text when a level sets a status without a message", func() {
		err := NewRMError(nil, "grant missing").WithClientStatus(http.StatusForbidden, "")
		Expect(resolve(err)).To(Equal(statusResult{http.StatusForbidden, "Forbidden"}))
	})

	It("finds a status through a stdlib %w wrap", func() {
		inner := NewRMError(nil, "grant missing").WithClientStatus(http.StatusForbidden, "No access")
		Expect(resolve(fmt.Errorf("context: %w", inner))).To(Equal(statusResult{http.StatusForbidden, "No access"}))
	})

	// Four levels, innermost first: level4 is the deepest (e.g. a DB helper) and level1 the outermost (the handler).
	type override func(*RMError)
	setStatus := func(status int, message string) override {
		return func(e *RMError) { e.WithClientStatus(status, message) }
	}
	setMessage := func(message string) override { return func(e *RMError) { e.WithClientMessage(message) } }
	noOverride := override(func(*RMError) {})

	DescribeTable("resolves a 4-level chain whose deepest level sets 404 \"Group not found\"",
		func(level3, level2, level1 override, want statusResult) {
			level4 := NewRMError(errors.New("item not found in DynamoDB"), "group lookup").WithClientStatus(http.StatusNotFound, "Group not found")
			err3 := NewRMError(level4, "load group")
			level3(err3)
			err2 := NewRMError(err3, "check access")
			level2(err2)
			err1 := NewRMError(err2, "handle request")
			level1(err1)
			Expect(resolve(err1)).To(Equal(want))
		},
		Entry("no level overrides: the deepest wins",
			noOverride, noOverride, noOverride, statusResult{http.StatusNotFound, "Group not found"}),
		Entry("outermost overrides both",
			noOverride, noOverride, setStatus(http.StatusForbidden, "Access denied"), statusResult{http.StatusForbidden, "Access denied"}),
		Entry("outermost overrides only the message: status still from the deepest",
			noOverride, noOverride, setMessage("Not available"), statusResult{http.StatusNotFound, "Not available"}),
		Entry("a middle level overrides both: outer levels without overrides keep it",
			noOverride, setStatus(http.StatusBadRequest, "Bad group id"), noOverride, statusResult{http.StatusBadRequest, "Bad group id"}),
		Entry("middle overrides both, outermost overrides only the message",
			noOverride, setStatus(http.StatusBadRequest, "Bad group id"), setMessage("Request rejected"), statusResult{http.StatusBadRequest, "Request rejected"}),
		Entry("every level overrides: the outermost wins",
			setStatus(http.StatusConflict, "Conflict at 3"), setStatus(http.StatusBadRequest, "Bad at 2"), setStatus(http.StatusForbidden, "Access denied"), statusResult{http.StatusForbidden, "Access denied"}),
		Entry("outermost sets a status with no message: the message comes from the deepest that set one",
			noOverride, noOverride, setStatus(http.StatusForbidden, ""), statusResult{http.StatusForbidden, "Group not found"}),
	)

	It("gives the same answer from the method on an *RMError", func() {
		err := NewRMError(nil, "grant missing").WithClientStatus(http.StatusForbidden, "No access")
		status, message := err.ClientError()
		Expect(statusResult{status, message}).To(Equal(statusResult{http.StatusForbidden, "No access"}))
	})

	It("keeps the wrapped cause reachable", func() {
		dbErr := errors.New("rate exceeded")
		err := NewRMError(dbErr, "failed to check subgroup").WithClientStatus(http.StatusServiceUnavailable, "Try again later")
		Expect(errors.Is(err, dbErr)).To(BeTrue())
		Expect(err.Error()).To(Equal("failed to check subgroup: rate exceeded"))
	})
})
