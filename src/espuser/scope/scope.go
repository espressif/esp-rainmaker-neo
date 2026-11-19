// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package scope is the single source of truth for ESP User's OAuth/OIDC scope values and space-delimited membership tests (RFC 6749 §3.3).
package scope

import (
	"slices"
	"strings"
)

// openid triggers id-token issuance; email/profile and phone gate which contact claims userinfo returns.
const (
	OpenID  = "openid"
	Email   = "email"
	Profile = "profile"
	Phone   = "phone"
	// Sessions gates the account's own surface (its user agents and the apps holding offline access) and is deliberately not implied by openid, because signing a person in and enumerating where they are signed in are different powers.
	Sessions = "account:sessions"
	// OfflineAccess asks for a refresh token that outlives every sign-out (OIDC Core 1.0 s11).
	OfflineAccess = "offline_access"
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

// HasOfflineAccess reports whether the offline_access scope is present (=> a grant no sign-out ends).
func HasOfflineAccess(scope string) bool {
	return Has(scope, OfflineAccess)
}

// Without returns scope with every drop value removed, order kept: Without("openid offline_access email", "offline_access") is "openid email".
func Without(scope string, drop ...string) string {
	kept := slices.DeleteFunc(strings.Fields(scope), func(value string) bool {
		return slices.Contains(drop, value)
	})
	return strings.Join(kept, " ")
}
