// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package signout ends a person's logins and then the session rows they hang off. Logins go first and every call stops at the first failure, so an interrupted sign-out never leaves a login working under a deleted session, and a retry finishes the job.
package signout

import (
	"slices"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type Service struct {
	ctx           *rmngctx.RmngContext
	refreshTokens *refreshtoken.Service
	sessions      *session.Service
}

func NewService(rmngCtx *rmngctx.RmngContext) *Service {
	return &Service{ctx: rmngCtx, refreshTokens: refreshtoken.NewService(rmngCtx), sessions: session.NewService(rmngCtx)}
}

// EndSession ends one session and every login opened through it; sessions_db.ErrSessionNotFound when the user has no session by that sid.
func (s *Service) EndSession(userID, sid string) error {
	if _, err := s.refreshTokens.DeleteFamiliesBySID(userID, sid); err != nil {
		return err
	}
	return s.sessions.DeleteSessionBySID(userID, sid)
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
