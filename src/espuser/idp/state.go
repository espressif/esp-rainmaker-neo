// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package idp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// The upstream state carries its own flow id under an HMAC, so the callback needs no by-state GSI
// and a forged state is rejected. It is the upstream CSRF and issuer-mix-up guard (RFC 9207), and is
// distinct from the client's downstream state. The callback also compares it against the value on
// the flow record, so a state valid for one flow cannot be spliced onto another.

var errBadState = errors.New("invalid federation state")

// Domain-separated from the refresh secret so federation state and refresh tokens never share a raw
// key, despite both deriving from the one provisioned secret.
func StateHMACKey(refreshSecret []byte) []byte {
	mac := hmac.New(sha256.New, refreshSecret)
	mac.Write([]byte("espuser-idp-state-v1"))
	return mac.Sum(nil)
}

func encodeState(flowID string, key []byte) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(flowID))
	return payload + "." + tag(payload, key)
}

func decodeState(state string, key []byte) (flowID string, err error) {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return "", errBadState
	}
	if !hmac.Equal([]byte(parts[1]), []byte(tag(parts[0], key))) {
		return "", errBadState
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errBadState
	}
	return string(raw), nil
}

func tag(payload string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
