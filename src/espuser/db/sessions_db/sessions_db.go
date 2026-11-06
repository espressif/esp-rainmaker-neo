// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Table espuser-sessions (PK session_hash): the authorization server's own browser session,
// keyed by the SHA-256 of the __Host-esp_session cookie value so a table read never yields a
// usable cookie. The row's sid is the session's public name (emittable in tokens and logout
// messages); the cookie value itself is the secret and is never stored or emitted anywhere.
// Spec: espuser/docs/specs/sso-sessions.md.
package sessions_db

import (
	"errors"
	"sort"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
)

const (
	sessionsTableName = "espuser-sessions"

	sessionsHashKey = "session_hash"

	// SessionsByUserIndex answers "which sessions does this person have". The base table is
	// keyed by the SHA-256 of the cookie, and that hash cannot be derived from a sid -- so
	// without this index a session is reachable only from the browser that holds its cookie,
	// and signing out a user agent you are not on is impossible rather than merely awkward. It is
	// also the ONLY way to reach a cookie-less (app) row and the only way a back-channel
	// TouchExpiryBySID can find a user agent from a refresh token's sid.
	//
	// KEYS_ONLY on purpose: expires_at is rewritten on EVERY authorize and every token
	// refresh, and an index write happens only when a key or a projected attribute changes.
	// Projecting nothing means the hot path costs this index nothing; entries change on
	// create and delete only.
	SessionsByUserIndex = "espuser-sessions-by-user"

	sessionsUserKey = "user_id"
	sessionsSIDKey  = "sid"

	colExpiresAt       = "expires_at"
	colCookieExpiresAt = "cookie_expires_at"
	colLastSeenAt      = "last_seen_at"
	colMaxExpiresAt    = "max_expires_at"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionsDB struct {
	Ctx      *rmngctx.RmngContext
	sessions *ddbu.Table[SessionEntry]
}

func NewSessionsDB(ctx *rmngctx.RmngContext) *SessionsDB {
	return &SessionsDB{
		Ctx: ctx,
		sessions: ddbu.NewTable[SessionEntry](awscommon.GetDynamoDBClient(), ddbu.Schema{
			Name:    sessionsTableName,
			HashKey: sessionsHashKey,
			Indexes: map[string]ddbu.IndexKeys{
				SessionsByUserIndex: {HashKey: sessionsUserKey, RangeKey: sessionsSIDKey},
			},
		}),
	}
}

type SessionEntry struct {
	SessionHash string `dynamodbav:"session_hash"`
	// SID is the session's public identifier: safe to stamp into tokens and, later, into
	// back-channel logout messages. Deliberately distinct from the cookie value.
	SID      string `dynamodbav:"sid,omitempty"`
	UserID   string `dynamodbav:"user_id,omitempty"`
	Provider string `dynamodbav:"provider,omitempty"`
	// AuthTime is inherited from the upstream id token, never stamped at row creation:
	// it answers "when did this person actually authenticate", which max_age and step-up
	// authentication are computed from.
	AuthTime int64    `dynamodbav:"auth_time,omitempty"`
	AMR      []string `dynamodbav:"amr,omitempty"`
	ACR      string   `dynamodbav:"acr,omitempty"`
	// ExpiresAt is the row's retention deadline AND the DynamoDB TTL attribute, bumped by any
	// activity (a browser /oauth2/authorize or an app token refresh). No omitempty: a zero would
	// drop the TTL and leave the row un-swept, and it is always written with a positive value.
	ExpiresAt int64 `dynamodbav:"expires_at"`
	// MaxExpiresAt is auth_time + the effective provider cap, fixed at login (0 = no cap). It
	// lets a back-channel bump clamp expires_at without re-reading the provider row; a hot-path
	// denormalisation of "how far can this user agent's retention ever be pushed".
	MaxExpiresAt int64 `dynamodbav:"max_expires_at,omitempty"`
	// CookieExpiresAt records when the browser will drop the cookie -- bumped only when a
	// Set-Cookie is actually issued (a front-channel event), so it can lag expires_at. Absent
	// on a cookie-less (app) row, which never had a cookie to expire. A record, not a control.
	CookieExpiresAt int64 `dynamodbav:"cookie_expires_at,omitempty"`
	// LastSeenAt is the most recent activity of any kind, rendered as "last active". Distinct
	// from created_at and moved by both channels.
	LastSeenAt int64 `dynamodbav:"last_seen_at,omitempty"`
	// Origin is "browser" or "app": whether a cookie was ever issued for this row. Establish always writes it; absent only on rows written before the field existed, which read as browser.
	Origin    string `dynamodbav:"origin,omitempty"`
	CreatedAt int64  `dynamodbav:"created_at,omitempty"`
	// UserAgent and IPAddress are captured once, at the login that created the session, and
	// never updated -- they describe the authentication event, not the latest request.
	//
	// Both are HINTS FOR A HUMAN reading their own user agent list, never a security control.
	// User-Agent is self-reported and browsers are actively reducing it, so an empty value
	// is ordinary and must render as "Unknown user agent" rather than fail.
	UserAgent string `dynamodbav:"user_agent,omitempty"`
	IPAddress string `dynamodbav:"ip_address,omitempty"`
	// UserAgentName is the label supplied at the enterprise OTP initiate; absent for every other login path.
	UserAgentName string `dynamodbav:"user_agent_name,omitempty"`
}

func (db *SessionsDB) CreateSession(entry *SessionEntry) error {
	if err := db.sessions.Create(db.Ctx.Context, *entry); err != nil {
		return rmerror.NewRMError(err, "failed to put session")
	}
	return nil
}

// GetSession returns the live session for a cookie hash. Retention expiry is enforced here
// rather than left to the DynamoDB TTL sweep, which lags by up to ~48h; a swept-late row is
// unusable from the instant expires_at passes, whatever remains on disk.
func (db *SessionsDB) GetSession(hash string) (*SessionEntry, error) {
	result, found, err := db.sessions.Get(db.Ctx.Context, db.sessions.Key(hash))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get session")
	}
	if !found || isExpired(result, time.Now().Unix()) {
		return nil, ErrSessionNotFound
	}
	return &result, nil
}

// TouchLiveSession rolls a live session's clocks to expiresAt (capped at max_expires_at) and returns the row from the write itself; a second UpdateItem runs only when the cap applies.
func (db *SessionsDB) TouchLiveSession(hash string, now, expiresAt int64) (*SessionEntry, error) {
	row, old, err := db.touchLive(hash, now, expiresAt, true)
	if err == nil || old == nil || isExpired(*old, now) {
		return row, err
	}
	if old.MaxExpiresAt > 0 && old.MaxExpiresAt < expiresAt {
		row, _, err = db.touchLive(hash, now, old.MaxExpiresAt, false)
		return row, err
	}
	return nil, ErrSessionNotFound
}

// touchLive returns the old row when the condition failed on a row that still exists.
func (db *SessionsDB) touchLive(hash string, now, expiresAt int64, checkCap bool) (*SessionEntry, *SessionEntry, error) {
	update := expression.
		Set(expression.Name(colExpiresAt), expression.Value(expiresAt)).
		Set(expression.Name(colCookieExpiresAt), expression.Value(expiresAt)).
		Set(expression.Name(colLastSeenAt), expression.Value(now))
	cond := expression.Or(
		expression.Name(colExpiresAt).Equal(expression.Value(0)),
		expression.Name(colExpiresAt).GreaterThan(expression.Value(now)),
	)
	if checkCap {
		cond = cond.And(expression.Or(
			expression.Name(colMaxExpiresAt).AttributeNotExists(),
			expression.Name(colMaxExpiresAt).Equal(expression.Value(0)),
			expression.Name(colMaxExpiresAt).GreaterThanEqual(expression.Value(expiresAt)),
		))
	}
	row, err := db.sessions.Update(db.Ctx.Context, db.sessions.Key(hash), update, ddbu.WriteOpts{If: cond, ReturnOld: true})
	if err != nil {
		if !ddbu.IsConditionFailed(err) {
			return nil, nil, rmerror.NewRMError(err, "failed to touch session")
		}
		old, ok := ddbu.OldItem[SessionEntry](err)
		if !ok {
			return nil, nil, ErrSessionNotFound
		}
		return nil, &old, ErrSessionNotFound
	}
	return &row, nil, nil
}

// TouchExpiryBySID rolls expires_at forward from a BACK-channel event (a token mint or
// refresh), where no browser is present so the cookie cannot be re-issued: cookie_expires_at
// is deliberately left untouched, and that asymmetry is the whole reason the two clocks exist.
//
// The user agent is resolved from the by-user index (a sid cannot be turned back into a cookie
// hash), then the base row is read for its max_expires_at so the new expires_at is clamped to
// the provider cap without re-reading the provider row. sessionTTLSeconds is passed in so this
// package keeps no dependency on the session package.
//
// Every row the index lists for the sid is tried, because during a re-sign-in the index can list the replaced row, the new one, or both.
//
// Returns the new expires_at so the token path can clamp the family's expiry to it. ErrSessionNotFound means the session has ended: the index lists no row for the sid, or every listed row is past expires_at. (0, nil) means it cannot tell, because a listed row is missing, which a lagging index or read shows mid re-sign-in.
func (db *SessionsDB) TouchExpiryBySID(userID, sid string, now, sessionTTLSeconds int64) (int64, error) {
	if userID == "" || sid == "" {
		return 0, nil
	}
	hashes, err := db.sessionHashesForUser(userID, sid)
	if err != nil {
		return 0, err
	}
	ended := true
	for _, hash := range hashes {
		expiresAt, err := db.touchExpiry(hash, now, sessionTTLSeconds)
		if errors.Is(err, ErrSessionNotFound) {
			continue
		}
		if err != nil || expiresAt > 0 {
			return expiresAt, err
		}
		ended = false
	}
	if ended {
		return 0, ErrSessionNotFound
	}
	return 0, nil
}

// touchExpiry rolls one row's expires_at forward. A row past expires_at is ErrSessionNotFound (the TTL sweep lags, so an ended session can still be on disk); a missing row is 0 with no error, since it may only not be readable yet.
func (db *SessionsDB) touchExpiry(hash string, now, sessionTTLSeconds int64) (int64, error) {
	row, found, err := db.sessions.Get(db.Ctx.Context, db.sessions.Key(hash))
	if err != nil {
		return 0, rmerror.NewRMError(err, "failed to read session for retention bump")
	}
	if !found {
		return 0, nil
	}
	if isExpired(row, now) {
		return 0, ErrSessionNotFound
	}

	expiresAt := now + sessionTTLSeconds
	if row.MaxExpiresAt > 0 && row.MaxExpiresAt < expiresAt {
		expiresAt = row.MaxExpiresAt
	}

	update := expression.
		Set(expression.Name(colExpiresAt), expression.Value(expiresAt)).
		Set(expression.Name(colLastSeenAt), expression.Value(now))
	live := expression.Or(
		expression.Name(colExpiresAt).Equal(expression.Value(0)),
		expression.Name(colExpiresAt).GreaterThan(expression.Value(now)),
	)
	if _, err := db.sessions.Update(db.Ctx.Context, db.sessions.Key(hash), update, ddbu.WriteOpts{If: live}); err != nil {
		// The row changed between the read and the write, so whether the session ended is unknown.
		if ddbu.IsConditionFailed(err) {
			return 0, nil
		}
		return 0, rmerror.NewRMError(err, "failed to bump session retention")
	}
	return expiresAt, nil
}

func (db *SessionsDB) DeleteSession(hash string) error {
	if err := db.sessions.Delete(db.Ctx.Context, db.sessions.Key(hash)); err != nil {
		return rmerror.NewRMError(err, "failed to delete session")
	}
	return nil
}

// ListByUser returns every live session for a user, newest first.
//
// Two hops on purpose: the index is KEYS_ONLY (see SessionsByUserIndex), so it yields
// session hashes and the rows are then read from the base table. That keeps the write path
// free, and the second hop is a handful of point reads -- a person has single-digit
// sessions, not pages of them.
//
// Expired rows are filtered here rather than trusted to the DynamoDB TTL sweep, which lags
// by up to ~48h; a user agent list that shows a session the server would refuse is a lie.
func (db *SessionsDB) ListByUser(userID string) ([]SessionEntry, error) {
	hashes, err := db.sessionHashesForUser(userID, "")
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	sessions := make([]SessionEntry, 0, len(hashes))
	for _, hash := range hashes {
		row, found, err := db.sessions.Get(db.Ctx.Context, db.sessions.Key(hash))
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to read session")
		}
		if !found || isExpired(row, now) {
			continue
		}
		sessions = append(sessions, row)
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].CreatedAt > sessions[j].CreatedAt })
	return sessions, nil
}

// DeleteByUserAndSID ends one named session. Scoped by user_id as well as sid so a caller
// can only ever reach their own -- the ownership check is the query, not a comparison the
// handler could forget to write.
//
// Returns ErrSessionNotFound when nothing matched, which is also what a caller gets for
// somebody else's sid. Deliberate: a 404 that means "not yours" and a 404 that means "gone"
// must be indistinguishable, or the endpoint tells a stranger which sids exist.
func (db *SessionsDB) DeleteByUserAndSID(userID, sid string) error {
	hashes, err := db.sessionHashesForUser(userID, sid)
	if err != nil {
		return err
	}
	if len(hashes) == 0 {
		return ErrSessionNotFound
	}
	for _, hash := range hashes {
		if err := db.DeleteSession(hash); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAllForUser ends every session a user has. Used by "sign out everywhere".
func (db *SessionsDB) DeleteAllForUser(userID string) error {
	hashes, err := db.sessionHashesForUser(userID, "")
	if err != nil {
		return err
	}
	for _, hash := range hashes {
		if err := db.DeleteSession(hash); err != nil {
			return err
		}
	}
	return nil
}

// sessionHashesForUser queries the index. An empty sid returns every session the user has;
// a non-empty one narrows to that single session.
func (db *SessionsDB) sessionHashesForUser(userID, sid string) ([]string, error) {
	keyCond := expression.Key(sessionsUserKey).Equal(expression.Value(userID))
	if sid != "" {
		keyCond = keyCond.And(expression.Key(sessionsSIDKey).Equal(expression.Value(sid)))
	}
	page, err := db.sessions.Query(db.Ctx.Context, keyCond, ddbu.QueryOpts{Index: SessionsByUserIndex})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to query sessions by user")
	}
	hashes := make([]string, 0, len(page.Items))
	for i := range page.Items {
		hashes = append(hashes, page.Items[i].SessionHash)
	}
	return hashes, nil
}

// isExpired enforces expires_at only. There is no idle or absolute clock any more: a user agent
// is "known" until its retention deadline passes, and nothing else expires it.
func isExpired(row SessionEntry, now int64) bool {
	return row.ExpiresAt != 0 && now >= row.ExpiresAt
}
