// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package idp runs the upstream leg of a brokered login, with ESP User as a confidential client of
// an upstream IdP. It is deliberately independent of the client's downstream leg — see the "why
// brokered, not pass-through" argument in espuser/docs/en/specs/federation.md.
package idp

import (
	"context"
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/utils/secretutil"
)

// Identity is the normalized upstream identity. The *Verified flags default to false when the
// upstream omits them, so an unverified contact can never resolve an account by accident.
type Identity struct {
	ProviderName  string
	ExternalSub   string
	Email         string
	EmailVerified bool
	PhoneNumber   string
	PhoneVerified bool

	// Enrichment only; account resolution trusts nothing below this point.
	Name       string
	GivenName  string
	FamilyName string
	Locale     string
	Picture    string
}

// VerifiedContacts returns every contact the upstream vouched for, so an account can be found by
// either of them. Unverified values are dropped: identity is derived from a contact, so trusting one
// would let anyone who can set an arbitrary email or phone upstream claim the matching ESP User.
func (id Identity) VerifiedContacts() (email, phone string, err error) {
	if id.EmailVerified {
		email = id.Email
	}
	if id.PhoneVerified {
		phone = id.PhoneNumber
	}
	if email == "" && phone == "" {
		return "", "", errors.New("no verified contact on the upstream identity")
	}
	return email, phone, nil
}

// UpstreamLeg is per-flow and entirely ours: the callback correlates against these rather than
// against anything the client or the upstream chose.
type UpstreamLeg struct {
	State        string
	Nonce        string
	PKCEVerifier string
}

type Provider interface {
	Name() string
	AuthorizeRedirectURL(ctx context.Context, leg UpstreamLeg) (string, error)
	HandleCallback(ctx context.Context, code string, leg UpstreamLeg) (Identity, error)
}

func NewUpstreamLeg(flowID string, hmacKey []byte) (UpstreamLeg, error) {
	nonce, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return UpstreamLeg{}, err
	}
	verifier, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return UpstreamLeg{}, err
	}
	return UpstreamLeg{
		State:        encodeState(flowID, hmacKey),
		Nonce:        nonce,
		PKCEVerifier: verifier,
	}, nil
}
