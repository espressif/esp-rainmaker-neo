// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package pkceutil derives and verifies RFC 7636 PKCE code challenges. S256 only; "plain" is not
// accepted.
package pkceutil

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// The S256 method name is the OAuth-protocol constant oidc.PKCEMethodS256; this package is the mechanism.

// ChallengeS256 derives the code_challenge for a verifier.
func ChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// VerifyS256 reports whether verifier hashes to challenge under S256 (constant-time compare).
func VerifyS256(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(ChallengeS256(verifier)), []byte(challenge)) == 1
}
