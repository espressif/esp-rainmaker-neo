// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"errors"
	types "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Sentinel errors returned by the DB layer so callers can branch on them with
// errors.Is instead of matching on error text (rewording a message must not
// change an API status code). Each is wrapped at exactly one origin.
var (
	// ErrLastPrimaryUser is returned when removing a user would leave the group with no primary user. Handlers map it to HTTP 409.
	ErrLastPrimaryUser = errors.New("cannot remove the last primary user from the group")
	// ErrSubGroupNotFound is returned when a subgroup operation targets a subgroup that does not exist. Handlers map it to HTTP 404.
	ErrSubGroupNotFound = errors.New("subgroup not found")
)

// IsConditionalCheckFailedException checks if the error is a ConditionalCheckFailedException
func IsConditionalCheckFailedException(err error) bool {
	var ccfe *types.ConditionalCheckFailedException
	return errors.As(err, &ccfe)
}
