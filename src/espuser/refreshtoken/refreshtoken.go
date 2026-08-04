// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package refreshtoken is the ESP User refresh-token service: it owns the token
// lifecycle — mint a family on login, rotate-on-use with reuse=theft detection, and
// revoke — over the espuser-refresh-tokens store. Tokens are signed, self-describing
// structures (not opaque random strings): each carries user_id|client_id|family_id|counter
// with an HMAC, so validity is a signature check plus a single family-row counter compare,
// and nothing per-token is stored. It mirrors the jwt package (a stateful service over a
// lower layer): this sits over db/refresh_tokens_db, which stays a pure DynamoDB store.
package refreshtoken

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	familyIDBytes = 16
	tokenSep      = "."
	fieldSep      = "|"

	// envRefreshSecretParam names the SSM SecureString holding the dedicated HMAC key for
	// refresh-token signatures, isolated from the RS256 access-token signing key.
	envRefreshSecretParam = "ESPUSER_REFRESH_SECRET_PARAM"

	TokenTTL = 365 * 24 * time.Hour // 1 year; sliding window, re-stamped on each rotation

	// rotationGrace: a lost rotation response looks identical to reuse; without a grace it would
	// kill the family and permanently unlink the client on ordinary network loss (RFC 9700 §4.14.2).
	rotationGrace = 60 * time.Second
)

// Service issues and rotates signed refresh tokens against the refresh-tokens store.
type Service struct {
	db  *refresh_tokens_db.RefreshTokensDB
	ctx context.Context
}

// NewService builds a refresh-token service bound to the request context.
func NewService(rmngCtx *rmngctx.RmngContext) *Service {
	return &Service{db: refresh_tokens_db.NewRefreshTokensDB(rmngCtx), ctx: rmngCtx.Context}
}

// Rotation is the result of redeeming a refresh token: the freshly-minted token plus the
// user/scope carried on the family, from which the caller re-mints the access/id tokens.
type Rotation struct {
	Token  string
	UserID string
	Scope  string
}

// claims are the signed fields a refresh token carries.
type claims struct {
	UserID   string
	ClientID string
	FamilyID string
	Counter  int64
}

// getRefreshSecret resolves the dedicated HMAC key from its SecureString SSM param (cached),
// mirroring how the RS256 signing key is loaded.
func (s *Service) getRefreshSecret() ([]byte, error) {
	param := os.Getenv(envRefreshSecretParam)
	if param == "" {
		return nil, rmerror.NewRMError(nil, "refresh token secret param is not configured")
	}
	val, err := ssmutil.GetParameterWithCaching(s.ctx, param, true)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to load refresh token secret")
	}
	return []byte(val), nil
}

// signToken encodes the claims and appends an HMAC over them: base64url(user_id|client_id|family_id|counter).sig
// e.g. dXNlci0xMjN8cm1fbW9iaWxlfGFiYzEyM3ww.Zm9vYmFyc2ln (payload "user-123|rm_mobile|abc123|0" + HMAC).
func (s *Service) signToken(c claims) (string, error) {
	secret, err := s.getRefreshSecret()
	if err != nil {
		return "", err
	}
	payload := strings.Join([]string{c.UserID, c.ClientID, c.FamilyID, strconv.FormatInt(c.Counter, 10)}, fieldSep)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + tokenSep + sign(encoded, secret), nil
}

// parseToken verifies the HMAC and returns the signed claims.
func (s *Service) parseToken(token string) (claims, error) {
	secret, err := s.getRefreshSecret()
	if err != nil {
		return claims{}, err
	}
	parts := strings.SplitN(token, tokenSep, 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return claims{}, rmerror.NewRMError(nil, "malformed refresh token")
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(sign(parts[0], secret))) != 1 {
		return claims{}, rmerror.NewRMError(nil, "refresh token signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims{}, rmerror.NewRMError(err, "malformed refresh token payload")
	}
	fields := strings.Split(string(raw), fieldSep)
	if len(fields) != 4 {
		return claims{}, rmerror.NewRMError(nil, "malformed refresh token payload")
	}
	counter, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return claims{}, rmerror.NewRMError(err, "malformed refresh token counter")
	}
	return claims{UserID: fields[0], ClientID: fields[1], FamilyID: fields[2], Counter: counter}, nil
}

func sign(encoded string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomID(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", rmerror.NewRMError(err, "failed to generate random id")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func expiresOn() int64 {
	return time.Now().Add(TokenTTL).Unix()
}

func withinRotationGrace(rotatedAt int64) bool {
	return rotatedAt > 0 && time.Now().Unix()-rotatedAt <= int64(rotationGrace.Seconds())
}

// MintRefreshtoken creates a new rotation family for a login: a family row at counter 0 and
// the first signed token. The family carries user + scope so a later Rotate re-mints the access/id tokens.
func (s *Service) MintRefreshtoken(userID, clientID, scope string) (token string, err error) {
	familyID, err := randomID(familyIDBytes)
	if err != nil {
		return "", err
	}
	if err := s.db.CreateFamily(&refresh_tokens_db.FamilyEntry{
		UserID:    userID,
		ClientID:  clientID,
		FamilyID:  familyID,
		Counter:   0,
		Scope:     scope,
		ExpiresOn: expiresOn(),
		RotatedAt: time.Now().Unix(),
	}); err != nil {
		return "", err
	}
	return s.signToken(claims{UserID: userID, ClientID: clientID, FamilyID: familyID, Counter: 0})
}

// Rotate redeems a refresh token: verify signature, match the family's counter, then advance
// it atomically and issue the next token. A presented counter below the family's current one
// is reuse (theft): the family is deleted and an error returned.
func (s *Service) Rotate(clientID, refreshToken string) (*Rotation, error) {
	if refreshToken == "" || clientID == "" {
		return nil, rmerror.NewRMError(nil, "client id and refresh token are required")
	}

	c, err := s.parseToken(refreshToken)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid refresh token")
	}
	if c.ClientID != clientID {
		return nil, rmerror.NewRMError(nil, "refresh token client mismatch")
	}

	family, err := s.db.GetFamily(c.UserID, c.ClientID, c.FamilyID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "refresh token rejected")
	}

	// Reject an expired family at redemption. The row also carries a DynamoDB TTL, but TTL deletion
	// lags (up to ~48h), so an expired refresh token would otherwise still rotate until physically
	// swept. Enforcing here closes that window (RFC 9700: bound refresh-token lifetime).
	if family.ExpiresOn > 0 && time.Now().Unix() >= family.ExpiresOn {
		_ = s.db.DeleteFamily(c.UserID, c.ClientID, c.FamilyID)
		return nil, rmerror.NewRMError(refresh_tokens_db.ErrRefreshExpired, "refresh token expired")
	}

	if c.Counter != family.Counter {
		// One step behind within the grace window is a lost-response retry, not reuse: re-issue the
		// current token idempotently rather than killing the family.
		if c.Counter == family.Counter-1 && withinRotationGrace(family.RotatedAt) {
			token, err := s.signToken(claims{UserID: c.UserID, ClientID: c.ClientID, FamilyID: c.FamilyID, Counter: family.Counter})
			if err != nil {
				return nil, err
			}
			return &Rotation{Token: token, UserID: family.UserID, Scope: family.Scope}, nil
		}
		// Any older (or stale-by-one past the grace) token is spent — reuse=theft, kill the family.
		_ = s.db.DeleteFamily(c.UserID, c.ClientID, c.FamilyID)
		return nil, rmerror.NewRMError(refresh_tokens_db.ErrRefreshReuse, "refresh token reuse detected")
	}

	next := family.Counter + 1
	now := time.Now()
	if err := s.db.AdvanceCounter(c.UserID, c.ClientID, c.FamilyID, family.Counter, expiresOn(), now.Unix()); err != nil {
		// Lost the conditional race (a concurrent rotation already advanced it): reuse.
		return nil, rmerror.NewRMError(err, "refresh token rejected")
	}

	token, err := s.signToken(claims{UserID: c.UserID, ClientID: c.ClientID, FamilyID: c.FamilyID, Counter: next})
	if err != nil {
		return nil, err
	}
	return &Rotation{Token: token, UserID: family.UserID, Scope: family.Scope}, nil
}

// RevokeFamily terminates the whole family a token belongs to — the /oauth2/revoke endpoint
// (RFC 7009 §2.1: revoking a token MAY revoke the underlying grant), sign-out, and detected
// theft all end the login by deleting its family row. An unparseable token is a no-op error.
func (s *Service) RevokeFamily(refreshToken string) error {
	c, err := s.parseToken(refreshToken)
	if err != nil {
		return rmerror.NewRMError(err, "invalid refresh token")
	}
	if err := s.db.DeleteFamily(c.UserID, c.ClientID, c.FamilyID); err != nil {
		return rmerror.NewRMError(err, "failed to revoke refresh token family")
	}
	return nil
}

// RevokeAllForUser ends every session for a user — "sign out everywhere", password reset, or
// compromise response — by deleting all their family rows in one user-partition query.
func (s *Service) RevokeAllForUser(userID string) error {
	if userID == "" {
		return rmerror.NewRMError(nil, "user id is required")
	}
	if err := s.db.DeleteAllForUser(userID); err != nil {
		return rmerror.NewRMError(err, "failed to revoke user refresh tokens")
	}
	return nil
}

// IsReuse reports whether an error from Rotate is the reuse/theft signal.
func IsReuse(err error) bool {
	return errors.Is(err, refresh_tokens_db.ErrRefreshReuse)
}
