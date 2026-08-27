// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"errors"
	"fmt"
)

// guidanceError carries a message written for the model: what it got wrong and what to do
// instead. Everything else a tool can return — a DynamoDB read, an IoT publish, a shadow
// fetch — is infrastructure detail the model must never be shown, so the handler needs to
// tell the two apart rather than forwarding whatever error reached it.
type guidanceError struct {
	message string
}

func (e *guidanceError) Error() string { return e.message }

// guidancef builds an error whose text is safe to hand back to the model verbatim.
func guidancef(format string, args ...interface{}) error {
	return &guidanceError{message: fmt.Sprintf(format, args...)}
}

// IsGuidance reports whether the error's message was written for the model. Handlers use it
// to decide between forwarding the message and substituting their own.
func IsGuidance(err error) bool {
	var guidance *guidanceError
	return errors.As(err, &guidance)
}
