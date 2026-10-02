// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package session is the authorization server's own browser session — the thing that makes
// single sign-on OUR property instead of one inherited from whichever upstream provider
// happens to keep a cookie. The session cookie is NOT where any token is stored: it is an
// opaque revocable identifier whose server-side record decides only whether the person must
// authenticate again. Every failure inside this package degrades to "no session" -- the login
// proceeds via the upstream leg -- rather than blocking a login or assuming validity.
// Spec: espuser/docs/specs/sso-sessions.md.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/secretutil"
)

// CookieName carries the __Host- prefix deliberately: browsers honour it only for a Secure,
// Path=/, Domain-less cookie, and a cookie set by another host WITH a Domain cannot use the
// prefix — so another API Gateway on the shared execute-api hostname cannot shadow ours
// (session fixation). The name survives the move to a real domain unchanged.
const CookieName = "__Host-esp_session"

const (
	// SessionTTLSeconds is the rolling inactivity window: how long a user agent stays "known" after
	// its last activity of ANY kind (a browser /oauth2/authorize or an app token refresh). It
	// is also the cookie's Max-Age. 400 days is the browser ceiling (Chrome 104+ and RFC 6265bis
	// clamp anything larger), so it is never set higher. There is no idle timeout and no absolute cap by default, so a user agent never expires while the refresh families it created keep working.
	//
	// Constraint: refreshtoken.TokenTTL (365d) must stay <= SessionTTLSeconds, so a family can be
	// clamped to its user agent's expires_at without a fresh login always shortening it.
	SessionTTLSeconds = 400 * 24 * 60 * 60

	// OriginBrowser and OriginApp record whether a browser was present to hold a cookie. A
	// browser row is cookie-bearing and can drive SSO; an app row is cookie-less by construction
	// (the cookie value is hashed and discarded) and never can.
	OriginBrowser = "browser"
	OriginApp     = "app"
)

// Session is the live-session view handed to the authorize path: everything it needs to
// decide whether to skip the login leg, and nothing secret (the cookie value stays with
// the browser; only its hash ever reached storage).
type Session struct {
	SID      string
	UserID   string
	Provider string
	AuthTime int64
}

// Service reads and writes sessions.
type Service struct {
	ctx *rmngctx.RmngContext
}

func NewService(ctx *rmngctx.RmngContext) *Service { return &Service{ctx: ctx} }

// EstablishInput carries what a new session records about the login that created it.
// ProviderMaxTTLSeconds is the authenticating provider's session_max_ttl_seconds row value
// (0 = none configured); the effective absolute lifetime is min(global, provider), so a
// provider row can only ever shorten the deployment-wide cap, never extend it.
type EstablishInput struct {
	UserID                string
	Provider              string
	AuthTime              int64
	AMR                   []string
	ACR                   string
	ProviderMaxTTLSeconds int64
	// UserAgent and IPAddress describe the browser that authenticated, for the user's own
	// user agent list. Both are optional and both are hints: an absent User-Agent is ordinary.
	UserAgent string
	IPAddress string
	// PriorCookie is the session cookie this browser presented on the way in, if any. It is
	// what tells a re-authentication apart from a first login; see Establish. Ignored for an
	// OriginApp establish -- an app's call is not evidence about a browser.
	PriorCookie string
	// Origin is OriginBrowser (a cookie is issued, SSO is possible) or OriginApp (cookie-less,
	// never SSO). Empty reads as OriginBrowser.
	Origin string
	// UserAgentName is the label the person gave this login, supplied on the enterprise OTP initiate (espuser_pro) and carried to the session row so the list can name an app by something they chose.
	UserAgentName string
}

// Establish creates the session record and returns the cookie value (the secret the browser
// holds) and the session's public sid. AuthTime is inherited from
// the upstream id token by the caller; a zero value falls back to now, which is only correct
// because a fresh upstream authentication has just completed on this code path.
func (s *Service) Establish(in EstablishInput) (cookieValue, sid string, err error) {
	cookieValue, err = secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return "", "", err
	}

	db := sessions_db.NewSessionsDB(s.ctx)

	// A browser holding a live session for THIS user is re-authenticating, so rotate rather
	// than add: inherit the old sid (families stay attached, one sign-out still reaches them
	// all) and delete the old row. Signing in again is what someone does when they think their
	// session was stolen; without the delete it changes nothing.
	//
	// A DIFFERENT user's prior session is left alone -- destroying another account's record of
	// a browser, on a login it did not authorise, is not this path's business.
	// An app's call carries no browser, so any cookie presented on it is not evidence about a
	// user agent and the prior-cookie lookup is skipped entirely for a cookie-less establish.
	priorHash := ""
	var priorCreatedAt int64
	if in.Origin != OriginApp && in.PriorCookie != "" {
		if prior, err := db.GetSession(hashCookie(in.PriorCookie)); err == nil && prior.UserID == in.UserID {
			sid, priorHash, priorCreatedAt = prior.SID, prior.SessionHash, prior.CreatedAt
		}
	}
	// A fresh sid only when none was inherited above.
	if sid == "" {
		sid, err = secretutil.GenRandom(secretutil.DefaultSecretBytes)
		if err != nil {
			return "", "", err
		}
	}

	now := time.Now().Unix()
	// created_at is "first signed in", so a re-authentication keeps it along with the sid.
	createdAt := now
	if priorCreatedAt > 0 {
		createdAt = priorCreatedAt
	}
	authTime := in.AuthTime
	if authTime <= 0 {
		authTime = now
	}
	// The ceiling is auth_time + the provider's cap, fixed at login (0 = no cap). A provider row
	// can only ever SHORTEN retention: without a cap the user agent rolls for the full SessionTTL.
	// Stored so a later back-channel bump can clamp without re-reading the provider row.
	var ceiling int64
	if in.ProviderMaxTTLSeconds > 0 {
		ceiling = authTime + in.ProviderMaxTTLSeconds
	}
	expiresAt := now + int64(SessionTTLSeconds)
	if ceiling > 0 && ceiling < expiresAt {
		expiresAt = ceiling
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginBrowser
	}
	// Only a browser row records cookie_expires_at; an app row never had a cookie (omitempty).
	var cookieExpiresAt int64
	if origin != OriginApp {
		cookieExpiresAt = expiresAt
	}

	if err := db.CreateSession(&sessions_db.SessionEntry{
		SessionHash:     hashCookie(cookieValue),
		SID:             sid,
		UserID:          in.UserID,
		Provider:        in.Provider,
		AuthTime:        authTime,
		AMR:             in.AMR,
		ACR:             in.ACR,
		ExpiresAt:       expiresAt,
		MaxExpiresAt:    ceiling,
		CookieExpiresAt: cookieExpiresAt,
		LastSeenAt:      now,
		Origin:          origin,
		CreatedAt:       createdAt,
		UserAgent:       in.UserAgent,
		IPAddress:       in.IPAddress,
		UserAgentName:   in.UserAgentName,
	}); err != nil {
		return "", "", err
	}
	// After the new row exists, never before: a failure between the two must leave the
	// browser with a working session, and an extra row is exactly the status quo this
	// rotation removes -- a worse outcome than leaving it one more time.
	if priorHash != "" {
		if err := db.DeleteSession(priorHash); err != nil {
			// The login still succeeds, but the cookie this one replaced goes on working -- the
			// property the rotation exists to remove. A missing DeleteItem grant lands here, and
			// this is the only evidence, so it must survive a deployment that filters Info and Warn.
			rlog.Error(s.ctx).Err(err).
				Msg("session: previous session row not retired (IAM DeleteItem grant?); the replaced cookie REMAINS VALID")
		}
	}
	// Cookie-less establish: the cookie value was generated only to key the row and is now
	// discarded, so nobody holds a secret that resolves it and the shortcut can never take it.
	if in.Origin == OriginApp {
		return "", sid, nil
	}
	return cookieValue, sid, nil
}

// Lookup resolves a cookie value to its live session and rolls retention forward. It is the
// FRONT-channel read (the /oauth2/authorize shortcut), so it bumps BOTH clocks -- expires_at
// and cookie_expires_at -- because the caller re-issues the cookie on the same response; the
// caller holds the cookie value and rolls it with SetCookieHeader(cookieValue, MaxAge()).
//
// Every failure — no cookie, unknown, expired, storage error — returns nil: the caller falls
// through to a normal login (fail to "no session", never fail open).
func (s *Service) Lookup(cookieValue string) *Session {
	if cookieValue == "" {
		return nil
	}
	// Read and retention roll in one write; cookie_expires_at moves too since this response carries a Set-Cookie.
	now := time.Now().Unix()
	entry, err := sessions_db.NewSessionsDB(s.ctx).TouchLiveSession(hashCookie(cookieValue), now, now+int64(SessionTTLSeconds))
	if err != nil {
		if !errors.Is(err, sessions_db.ErrSessionNotFound) {
			rlog.Warn(s.ctx.Context).Err(err).Msg("session: lookup failed")
		}
		return nil
	}
	return &Session{SID: entry.SID, UserID: entry.UserID, Provider: entry.Provider, AuthTime: entry.AuthTime}
}

// BumpRetentionBySID rolls a user agent's expires_at forward from a BACK-channel token event and
// returns the new expires_at so the token path can clamp the refresh family's expiry to it.
// A no-op returning (0, nil) when sid is empty (a pre-feature family) or the user agent is gone (a
// refresh that outlived its browser session); cookie_expires_at is never touched here.
func (s *Service) BumpRetentionBySID(userID, sid string) (expiresAt int64, err error) {
	if sid == "" {
		return 0, nil
	}
	return sessions_db.NewSessionsDB(s.ctx).TouchExpiryBySID(userID, sid, time.Now().Unix(), int64(SessionTTLSeconds))
}

// SetCookieHeader renders the session Set-Cookie value. No Domain (the __Host- prefix is
// void with one), SameSite=Lax because /oauth2/authorize arrives as a cross-site top-level
// navigation and Strict would suppress the cookie and defeat single sign-on entirely.
func SetCookieHeader(cookieValue string, maxAge int64) string {
	return fmt.Sprintf("%s=%s; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=%d", CookieName, cookieValue, maxAge)
}

// MaxAge is the Set-Cookie lifetime: the rolling retention window, re-issued on every
// front-channel event and recorded on the row as cookie_expires_at.
func (s *Service) MaxAge() int64 { return int64(SessionTTLSeconds) }

// ListSessions returns every live session the user has, newest first, for the user agent list.
func (s *Service) ListSessions(userID string) ([]sessions_db.SessionEntry, error) {
	return sessions_db.NewSessionsDB(s.ctx).ListByUser(userID)
}

// DeleteSessionBySID ends one session the user owns. sessions_db.ErrSessionNotFound when no
// such session exists for the caller -- the cross-user case included.
func (s *Service) DeleteSessionBySID(userID, sid string) error {
	return sessions_db.NewSessionsDB(s.ctx).DeleteByUserAndSID(userID, sid)
}

// DeleteAllSessions ends every session the user has -- sign out everywhere.
func (s *Service) DeleteAllSessions(userID string) error {
	return sessions_db.NewSessionsDB(s.ctx).DeleteAllForUser(userID)
}

func hashCookie(cookieValue string) string {
	sum := sha256.Sum256([]byte(cookieValue))
	return hex.EncodeToString(sum[:])
}
