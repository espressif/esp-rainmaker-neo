// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package rmerror

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// RMError is a custom error type that includes file, line, and message information
type RMError struct {
	Err     error
	File    string
	Line    int
	Message string
}

// Error returns the error message along with the file, line, and function information
func (e *RMError) Error() string {
	return e.Message
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

// LogError logs all wrapped errors
func logErrorToStr(file string, line int, err error) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("----\n[Error][%s:%d]\n", file, line))
	for err != nil {
		switch e := err.(type) {
		case *RMError:
			sb.WriteString(fmt.Sprintf("  %s:%d: %s\n", e.File, e.Line, e.Message))
		default:
			sb.WriteString(fmt.Sprintf("  %v\n", err))
		}
		err = errors.Unwrap(err)
	}
	sb.WriteString("----\n")
	return sb.String()
}

func LogError(err error) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Println(logErrorToStr(file, line, err))
}

func ErrorWithStack(err error) string {
	_, file, line, _ := runtime.Caller(1)
	return logErrorToStr(file, line, err)
}

func LogErrorStr(errStr string) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Printf("[%s:%d] Error: %s\n", file, line, errStr)
}
