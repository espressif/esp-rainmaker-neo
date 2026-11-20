// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"errors"
	"iter"
	"strings"
)

// IsS3PermanentRedirectError checks if the error is an S3 permanent redirect error
func IsS3PermanentRedirectError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "PermanentRedirect") ||
		strings.Contains(err.Error(), "301")
}

// FormatErrorChain renders the full wrapped error chain into a single human-
// readable string, layer messages joined by ": " — mirroring what
// fmt.Errorf("%w", inner) would produce for a chain built that way.
//
// Needed because RMError.Error() returns only its own layer's Message (not the
// concatenated chain), so storing err.Error() verbatim drops the root cause.
// For mixed chains (RMError wrapping a stdlib error), the deepest non-RMError
// layer is included as-is and traversal stops there to avoid double-printing
// stdlib errors that already embed their inner text.
func FormatErrorChain(err error) string {
	var parts []string
	for cur := range Layers(err) {
		rm, ok := cur.(*RMError) //nolint:errorlint // inspects one layer at a time; errors.As would match a deeper RMError
		if !ok {
			// Standard wrappers like fmt.Errorf("%w") already render the inner chain themselves.
			if msg := cur.Error(); msg != "" {
				parts = append(parts, msg)
			}
			break
		}
		if rm.Message != "" {
			parts = append(parts, rm.Message)
		}
	}
	return strings.Join(parts, ": ")
}

// Layers yields err and every error it wraps, outermost first: NewRMError(NewRMError(dbErr, "a"), "b") -> the "b" RMError, the "a" RMError, dbErr. A nil err yields nothing.
func Layers(err error) iter.Seq[error] {
	return func(yield func(error) bool) {
		for cur := err; cur != nil; cur = errors.Unwrap(cur) {
			if !yield(cur) {
				return
			}
		}
	}
}
