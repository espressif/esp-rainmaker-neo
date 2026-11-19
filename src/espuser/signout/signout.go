// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package signout ends a person's logins and then the session rows they hang off. Logins go first and every call stops at the first failure, so an interrupted sign-out never leaves a login working under a deleted session, and a retry finishes the job.
package signout

import (
	"errors"
	"slices"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// ErrNotConnected is a disconnect for a client that holds no offline_access grant for the person.
var ErrNotConnected = errors.New("signout: the client holds no offline access for this user")

type Service struct {
	ctx           *rmngctx.RmngContext
	refreshTokens *refreshtoken.Service
	sessions      *session.Service
}

func NewService(rmngCtx *rmngctx.RmngContext) *Service {
	return &Service{ctx: rmngCtx, refreshTokens: refreshtoken.NewService(rmngCtx), sessions: session.NewService(rmngCtx)}
}

// RevokeFamily is an app ending its own login (RFC 7009); the errors are refreshtoken.Service.RevokeFamily's. An app session left with no login goes too, so the signed-in list stops showing an app that cannot renew.
func (s *Service) RevokeFamily(clientID, refreshToken string) error {
	revoked, err := s.refreshTokens.RevokeFamily(clientID, refreshToken)
	if err != nil || revoked == nil {
		return err
	}
	s.endEmptyAppSessions(revoked.UserID, []refresh_tokens_db.FamilyEntry{*revoked})
	return nil
}

// EndSession ends one session and every login opened through it; sessions_db.ErrSessionNotFound when the user has no session by that sid.
func (s *Service) EndSession(userID, sid string) error {
	if _, err := s.refreshTokens.DeleteFamiliesBySID(userID, sid); err != nil {
		return err
	}
	return s.sessions.DeleteSessionBySID(userID, sid)
}

// DisconnectClient ends every login one client holds for the person, offline and session alike; ErrNotConnected, with nothing ended, when none of them is a live offline grant.
func (s *Service) DisconnectClient(userID, clientID string) error {
	families, err := s.refreshTokens.ListClientFamilies(userID, clientID)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if !slices.ContainsFunc(families, func(family refresh_tokens_db.FamilyEntry) bool { return family.IsOffline() && !family.IsExpired(now) }) {
		return ErrNotConnected
	}
	if err := s.refreshTokens.DeleteFamilies(families); err != nil {
		return err
	}
	s.endEmptyAppSessions(userID, families)
	return nil
}

// EndAllSessions signs the person out of every browser and app and keeps their offline grants (OIDC Back-Channel Logout 1.0 s2.7).
func (s *Service) EndAllSessions(userID string) error {
	families, err := s.refreshTokens.ListFamilies(userID)
	if err != nil {
		return err
	}
	if err := s.refreshTokens.DeleteFamilies(slices.DeleteFunc(families, func(family refresh_tokens_db.FamilyEntry) bool { return family.IsOffline() })); err != nil {
		return err
	}
	return s.sessions.DeleteAllSessions(userID)
}

// EndAllSessionsAndConnectedApps signs the person out of every browser and app and disconnects every connected app.
func (s *Service) EndAllSessionsAndConnectedApps(userID string) error {
	families, err := s.refreshTokens.ListFamilies(userID)
	if err != nil {
		return err
	}
	if err := s.refreshTokens.DeleteFamilies(families); err != nil {
		return err
	}
	return s.sessions.DeleteAllSessions(userID)
}

// endEmptyAppSessions removes each app session the ended logins hung off once no login of the user carries its sid; a browser session stays, because its cookie can still sign in. Failures are logged, not returned: the logins are already gone.
func (s *Service) endEmptyAppSessions(userID string, ended []refresh_tokens_db.FamilyEntry) {
	sids := map[string]bool{}
	for i := range ended {
		if ended[i].SID != "" {
			sids[ended[i].SID] = true
		}
	}
	if len(sids) == 0 {
		return
	}
	remaining, err := s.refreshTokens.ListFamilies(userID)
	if err != nil {
		rlog.Warn(s.ctx.Context).Err(err).Msg("signout: could not list families after ending logins; app sessions left in place")
		return
	}
	// An eventually consistent query can still return an ended family, so those are skipped by key.
	endedKeys := map[string]bool{}
	for i := range ended {
		endedKeys[ended[i].ClientFamily] = true
	}
	for i := range remaining {
		if !endedKeys[remaining[i].ClientFamily] {
			delete(sids, remaining[i].SID)
		}
	}
	for sid := range sids {
		if err := s.sessions.DeleteAppSession(userID, sid); err != nil && !errors.Is(err, sessions_db.ErrSessionNotFound) {
			rlog.Warn(s.ctx.Context).Err(err).Str("sid", sid).Msg("signout: could not remove an app session left with no login")
		}
	}
}
