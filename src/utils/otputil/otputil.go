// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package otputils holds generic, table-agnostic OTP helpers: id/code generation
// and salted-hash create/verify. No DynamoDB or table knowledge lives here.
package otputil

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"math/big"
)

const otpSaltBytes = 16

// Opaque URL-safe flow id, e.g. "fl_8xLQ2h7ZK3pR9tVmNq0wYbC1".
func GenerateFlowID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", rmerror.NewRMError(err, "failed to generate flow id")
	}
	return "fl_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

// Zero-padded 6-digit CSPRNG code, e.g. "042931".
func GenerateOTPCode() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to generate otp code")
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func HashOTP(code string) (string, error) {
	salt := make([]byte, otpSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", rmerror.NewRMError(err, "failed to generate otp salt")
	}
	return hashOTPWithSalt(code, salt), nil
}

// Constant-time compare; a malformed stored hash compares false and is treated as a mismatch.
func VerifyOTP(code, storedHash string) bool {
	saltHex, _, ok := splitHash(storedHash)
	if !ok {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	computed := hashOTPWithSalt(code, salt)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

func hashOTPWithSalt(code string, salt []byte) string {
	sum := sha256.Sum256(append(salt, []byte(code)...))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(sum[:])
}

func splitHash(stored string) (saltHex, digestHex string, ok bool) {
	for i := 0; i < len(stored); i++ {
		if stored[i] == ':' {
			return stored[:i], stored[i+1:], true
		}
	}
	return "", "", false
}
