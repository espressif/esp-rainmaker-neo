// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package scope is the single source of truth for ESP User's OAuth/OIDC scope values and space-delimited membership tests (RFC 6749 §3.3).
package scope

import "strings"

// openid triggers id-token issuance; email/profile and phone gate which contact claims userinfo returns.
const (
	OpenID  = "openid"
	Email   = "email"
	Profile = "profile"
	Phone   = "phone"
)

// Has reports whether any of want is present in the space-delimited scope string.
func Has(scope string, want ...string) bool {
	for _, s := range strings.Fields(scope) {
		for _, w := range want {
			if s == w {
				return true
			}
		}
	}
	return false
}

// HasOpenID reports whether the openid scope is present (⇒ mint an id token).
func HasOpenID(scope string) bool {
	return Has(scope, OpenID)
}
