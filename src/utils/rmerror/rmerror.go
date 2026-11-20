// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// RMError is a custom error type that includes file, line, and message information
type RMError struct {
	Err           error
	File          string
	Line          int
	Message       string
	ClientStatus  int    //http status code
	ClientMessage string //error message to be returned to the client
}

// WithClientStatus sets the HTTP status and client message this level reports, overriding any set deeper in the chain.
func (e *RMError) WithClientStatus(status int, clientMessage string) *RMError {
	e.ClientStatus = status
	e.ClientMessage = clientMessage
	return e
}

// WithClientMessage overrides only the client message; the status still comes from this level or a deeper one.
func (e *RMError) WithClientMessage(clientMessage string) *RMError {
	e.ClientMessage = clientMessage
	return e
}

// ClientError is what the API caller sees for this level and everything it wraps; see clientError.
func (e *RMError) ClientError() (int, string) {
	return clientError(e)
}

// clientError resolves what the API caller sees for any error: status and message each come from the outermost RMError that set them, else 500 "Internal Server Error". A deepest WithStatus(400, "Subgroup not found") under an outer WithClientMessage("Access denied") -> (400, "Access denied").
func clientError(err error) (int, string) {
	status, message := 0, ""
	for cur := range Layers(err) {
		rm, ok := cur.(*RMError) //nolint:errorlint // inspects one layer at a time; errors.As would skip to a deeper RMError
		if !ok {
			continue
		}
		if status == 0 {
			status = rm.ClientStatus
		}
		if message == "" {
			message = rm.ClientMessage
		}
		if status != 0 && message != "" {
			break
		}
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return status, message
}

// Error returns the whole wrapped chain, layer messages joined by ": ": NewRMError(NewRMError(dbErr, "query groups"), "check subgroup") -> "check subgroup: query groups: <dbErr>".
func (e *RMError) Error() string {
	return FormatErrorChain(e)
}

// TODO: Temporary function to be removed after all code is migrated to use ClientError
// TopMessage is the message of err's outermost level, which is what Error() returned before it rendered the whole chain: NewRMError(dbErr, "failed to get group") -> "failed to get group". Any other error returns its own Error(), and nil returns "".
func TopMessage(err error) string {
	if err == nil {
		return ""
	}
	if rm, ok := err.(*RMError); ok { //nolint:errorlint // only the outermost level counts; errors.As would reach a deeper RMError
		return rm.Message
	}
	return err.Error()
}

// Unwrap returns the wrapped error required for errors.Is and errors.As
func (e *RMError) Unwrap() error {
	return e.Err
}

// NewRMError creates a new RMError
func NewRMError(err error, message string) *RMError {
	_, file, line, _ := runtime.Caller(1)
	return &RMError{
		Err:     err,
		File:    file,
		Line:    line,
		Message: message,
	}
}

// ErrorWithStack renders err as one line per wrapped layer, with the caller's file and line on top, for test failure messages.
func ErrorWithStack(err error) string {
	_, file, line, _ := runtime.Caller(1)
	var sb strings.Builder
	fmt.Fprintf(&sb, "----\n[Error][%s:%d]\n", file, line)
	for cur := range Layers(err) {
		if rm, ok := cur.(*RMError); ok { //nolint:errorlint // inspects one layer at a time; errors.As would match a deeper RMError
			fmt.Fprintf(&sb, "  %s:%d: %s\n", rm.File, rm.Line, rm.Message)
			continue
		}
		fmt.Fprintf(&sb, "  %v\n", cur)
	}
	sb.WriteString("----\n")
	return sb.String()
}
