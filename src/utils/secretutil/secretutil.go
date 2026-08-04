// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package secretutil generates cryptographically-random secrets/identifiers.
package secretutil

import (
	"crypto/rand"
	"encoding/base64"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// DefaultSecretBytes is the entropy (in bytes) of a generated secret before encoding.
const DefaultSecretBytes = 32

// GenRandom returns nBytes of CSPRNG randomness as an unpadded base64url string.
func GenRandom(nBytes int) (string, error) {
	raw := make([]byte, nBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", rmerror.NewRMError(err, "failed to generate random bytes")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
