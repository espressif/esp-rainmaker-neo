// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package ids

import (
	crand "crypto/rand"
	"fmt"
	"strings"

	"github.com/lithammer/shortuuid/v4"
)

// FormatUsername is the single canonical form (lower+trim) shared by the derived user_id, the stored email, and the email-index lookup key so they cannot drift.
func FormatUsername(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}

// NewUserID mints an opaque user_id. Ids are generated rather than derived from a contact so an
// account is not tied to the email or phone it was created with: a person may verify a second
// contact, or change one, without the identifier moving.
func NewUserID() string {
	return shortuuid.New()
}

// cryptoRandChar returns a uniformly random byte from charset using crypto/rand.
// Rejection sampling avoids modulo bias.
func cryptoRandChar(charset string) byte {
	maxByte := 256 - (256 % len(charset))
	buf := make([]byte, 1)
	for {
		if _, err := crand.Read(buf); err != nil {
			panic(fmt.Sprintf("crypto/rand read failed: %v", err))
		}
		if int(buf[0]) < maxByte {
			return charset[int(buf[0])%len(charset)]
		}
	}
}

// generateSecureRandomStr returns a length-char string drawn from crypto/rand with rejection sampling.
var generateSecureRandomStr = func(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = cryptoRandChar(charset)
	}
	return string(result)
}

const groupIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
const groupIDAlphabetCharset = "abcdefghijklmnopqrstuvwxyz"
const groupIDLength = 6
const subGroupIDCharset = "abcdefghijklmnopqrstuvwxyz0123456789" //same as groupIDCharset
const subGroupIDLength = 3

var GenerateRequestId = func() string {
	return generateSecureRandomStr(32)
}

var GenerateChallenge = func() string {
	return generateSecureRandomStr(64)
}

// GenerateGroupID generates a random group ID of 6 characters.
// The first character is guaranteed to be a lowercase letter, followed by 5 alphanumeric characters (lowercase letters and digits).
func GenerateGroupID() string {
	b := make([]byte, groupIDLength)
	b[0] = cryptoRandChar(groupIDAlphabetCharset)
	for i := 1; i < groupIDLength; i++ {
		b[i] = cryptoRandChar(groupIDCharset)
	}
	return string(b)
}

const scheduleIDLength = 4

// GenerateScheduleID mints a schedule ID. Kept short because it travels to the device on
// every schedule push and firmware bounds the field; drawn from the same lowercase
// alphanumeric charset as group IDs so it is unambiguous when a user reads it aloud.
var GenerateScheduleID = func() string {
	b := make([]byte, scheduleIDLength)
	for i := range b {
		b[i] = cryptoRandChar(groupIDCharset)
	}
	return string(b)
}

// GenerateSubGroupID generates a random subgroup ID of 3 characters.
// The ID consists of lowercase letters and digits.
func GenerateSubGroupID() string {
	b := make([]byte, subGroupIDLength)
	for i := range b {
		b[i] = cryptoRandChar(subGroupIDCharset)
	}
	return string(b)
}
