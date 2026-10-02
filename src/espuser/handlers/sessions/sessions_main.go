// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// The account's own session-management surface: which browsers am I signed in on, what is
// signed in on each, and end one or all of them.
//
// Two tables answer this together. Sessions are keyed by the SHA-256 of the cookie, which
// cannot be derived from a sid, so reaching one from anywhere but the browser holding it needs
// the espuser-sessions-by-user index. Families are keyed by user_id and joined to a session by
// sid -- which is what makes one sign-out reach every product opened in that browser and no
// other.
//
// Spec: espuser/docs/specs/sso-sessions.md.
package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	pathSessions = "/v1/user/sessions"
	pathLogout   = "/oauth2/logout"
	// pathLogoutDone is where the upstream provider hands the browser back: a distinct exact
	// route from pathLogout.
	pathLogoutDone = "/oauth2/logout/done"
)

// SessionView is one browser. Clients are nested under it rather than listed beside it because that is the shape of the thing: you do not sign out of an application, you sign out of a browser, and every application opened in it goes with it.
type SessionView struct {
	SID string `json:"session_id"`
	// Current marks the session this request's own token was minted under, so a UI can label it and warn before ending it. False when the token carries no sid: it predates sessions, or the session write failed.
	Current bool `json:"current"`
	// UserAgentType is the readable rendering of UserAgent, derived at render time rather than stored so the parser can improve without a migration.
	UserAgentType string `json:"user_agent_type"`
	// UserAgentName is the label the person gave this login at the enterprise OTP initiate, kept separate from UserAgentType rather than merged into it: one is what they chose, the other is what we inferred, and a client that shows a name should prefer theirs.
	UserAgentName string `json:"user_agent_name,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
	IPAddress     string `json:"ip_address,omitempty"`
	Provider      string `json:"provider,omitempty"`
	// Origin is "browser" or "app": whether this sign-in origin can have single sign-on. A UI renders an app differently from a browser, and it is why only a browser row carries cookie_expires_at.
	Origin     string `json:"origin,omitempty"`
	SignedInAt int64  `json:"signed_in_at,omitempty"`
	// LastSeenAt is the most recent activity of any kind on this user agent -- "last active".
	LastSeenAt int64 `json:"last_seen_at,omitempty"`
	// ExpiresAt is the user agent's retention deadline: when it is swept if nothing touches it.
	ExpiresAt int64 `json:"expires_at,omitempty"`
	// CookieExpiresAt is when the browser will drop the cookie (browser rows only). A record of
	// what we told the browser, not a control -- absent on an app row, which never had a cookie.
	CookieExpiresAt int64        `json:"cookie_expires_at,omitempty"`
	Clients         []ClientView `json:"clients"`
}

// ClientView is one application holding a live refresh family under a session.
type ClientView struct {
	ClientID string `json:"client_id"`
	// CreatedAt is when this application first got tokens under this login; LastUsed is the
	// most recent refresh. CreatedAt is absent (omitempty) for families minted before it was
	// recorded -- the client falls back to LastUsed. Neither means "a tab is open now": this
	// list is standing access, not presence, and the UI must not imply otherwise.
	CreatedAt int64 `json:"created_at,omitempty"`
	LastUsed  int64 `json:"last_used,omitempty"`
}

type listResponse struct {
	Sessions []SessionView `json:"sessions"`
}

func forbidden() events.APIGatewayProxyResponse {
	resp := oidc.OAuthErrorResp(http.StatusForbidden, oidc.OAuthErrInsufficientScope, "This token was not granted the "+scope.Sessions+" scope.")
	resp.Headers["WWW-Authenticate"] = `Bearer error="insufficient_scope", scope="` + scope.Sessions + `"`
	return resp
}

// handleList renders the caller's sessions with the clients under each.
//
// Note what is absent from the response: session_hash. It is the hash of a secret and has
// no business leaving the server, so the delete path re-resolves a sid through the index
// rather than trusting a client to hand back an internal key.
func handleList(ctx context.Context, rmngCtx *rmngctx.RmngContext, identity auth.TokenClaims) events.APIGatewayProxyResponse {
	sessions, err := session.NewService(rmngCtx).ListSessions(identity.Subject)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("sessions: list sessions")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	families, err := refreshtoken.NewService(rmngCtx).ListFamilies(identity.Subject)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("sessions: list families")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}

	bySID := map[string][]ClientView{}
	for _, f := range families {
		bySID[f.SID] = append(bySID[f.SID], ClientView{
			ClientID:  f.ClientID,
			CreatedAt: f.CreatedAt,
			LastUsed:  f.RotatedAt,
		})
	}

	out := listResponse{Sessions: make([]SessionView, 0, len(sessions))}
	for _, sess := range sessions {
		clients := bySID[sess.SID]
		if clients == nil {
			clients = []ClientView{}
		}
		out.Sessions = append(out.Sessions, SessionView{
			SID:             sess.SID,
			Current:         identity.SID != "" && identity.SID == sess.SID,
			UserAgentType:   utils.UserAgentType(sess.UserAgent, sess.Origin),
			UserAgentName:   sess.UserAgentName,
			UserAgent:       sess.UserAgent,
			IPAddress:       sess.IPAddress,
			Provider:        sess.Provider,
			Origin:          sess.Origin,
			SignedInAt:      sess.AuthTime,
			LastSeenAt:      sess.LastSeenAt,
			ExpiresAt:       sess.ExpiresAt,
			CookieExpiresAt: sess.CookieExpiresAt,
			Clients:         clients,
		})
	}
	return utils.APIGwRespJSON(http.StatusOK, out)
}

// handleDeleteOne ends one session and every family created through it.
//
// A sid belonging to somebody else returns 404, never 403. The two must be
// indistinguishable: a 403 would confirm that the sid exists, turning this endpoint into an
// oracle for guessing valid session identifiers.
func handleDeleteOne(ctx context.Context, rmngCtx *rmngctx.RmngContext, identity auth.TokenClaims, sid string) events.APIGatewayProxyResponse {
	// Families first. If the session delete succeeded and this failed, the browser would
	// hold no session but its applications would keep refreshing -- signed out of the login
	// but not of anything it opened, which is the worse half to leave behind.
	if _, err := refreshtoken.NewService(rmngCtx).DeleteFamiliesBySID(identity.Subject, sid); err != nil {
		rlog.Error(ctx).Err(err).Msg("sessions: delete families")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	switch err := session.NewService(rmngCtx).DeleteSessionBySID(identity.Subject, sid); {
	case err == nil:
		return noContent()
	case errors.Is(err, sessions_db.ErrSessionNotFound):
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "No such session.")
	default:
		rlog.Error(ctx).Err(err).Msg("sessions: delete session")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
}

// handleDeleteAll is "sign out everywhere", and it means everywhere: this browser included.
// Signing a person out of every user agent except the one they are asking from would be a
// different feature wearing this one's name.
func handleDeleteAll(ctx context.Context, rmngCtx *rmngctx.RmngContext, identity auth.TokenClaims) events.APIGatewayProxyResponse {
	if err := refreshtoken.NewService(rmngCtx).RevokeAllForUser(identity.Subject); err != nil {
		rlog.Error(ctx).Err(err).Msg("sessions: delete all families")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	if err := session.NewService(rmngCtx).DeleteAllSessions(identity.Subject); err != nil {
		rlog.Error(ctx).Err(err).Msg("sessions: delete all sessions")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	return noContent()
}

// noContent is the success shape for both deletes: a bodyless 204. Built by hand rather than
// through utils.APIGwRespJSON because that always attaches a JSON body and Content-Type, and a
// 204 must carry neither.
//
// The CORS header is not optional and is easy to lose here, because this is the one response
// in the file built by hand rather than through utils.APIGwRespJSON (which sets it). A 204
// without it reaches the browser and is then discarded by the CORS check, so the request
// SUCCEEDS on the server and the page reports a network failure -- the session really is
// gone and the UI says it could not be ended.
func noContent() events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusNoContent,
		Headers: map[string]string{
			"Access-Control-Allow-Origin": "*",
			"Cache-Control":               "no-store",
			"X-Content-Type-Options":      "nosniff",
		},
	}
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	path := strings.TrimRight(request.Path, "/")

	if path == pathLogoutDone {
		return handleLogoutDone(ctx, request)
	}
	if path == pathLogout {
		return handleLogout(ctx, request)
	}
	if !strings.HasPrefix(path, pathSessions) {
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "Not found."), nil
	}

	rmngCtx := user.NewContextWithAPIRequest(ctx, request)
	identity := user.ClaimsFrom(rmngCtx)
	if identity.Subject == "" {
		rlog.Info(ctx).Msg("sessions: token rejected")
		return oidc.OAuthUnauthorizedResp(), nil
	}
	// First-party is a client-registration attribute, not a token claim, so revoking a client's standing takes effect on its next call rather than when its tokens expire.
	// A registry failure is ours (500); a valid token from a non-first-party client is authenticated but not allowed (403), since 401 invalid_token would make it retry with an identical token forever.
	client, err := clients.NewService(rmngCtx).Get(identity.ClientID)
	if err != nil && !errors.Is(err, clients.ErrClientNotFound) {
		rlog.Error(ctx).Err(err).Str("client_id", identity.ClientID).Msg("sessions: client registry read failed")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	if client == nil || !client.FirstParty {
		rlog.Info(ctx).Str("client_id", identity.ClientID).Msg("sessions: refused non-first-party client")
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.OAuthErrAccessDenied, "This client may not manage sessions."), nil
	}
	if !scope.Has(identity.Scope, scope.Sessions) {
		return forbidden(), nil
	}

	// Empty for the collection path; API Gateway always sets it on the {sessionId} resource.
	sid := request.PathParameters["sessionId"]

	switch request.HTTPMethod {
	case http.MethodGet:
		if sid == "" {
			return handleList(ctx, rmngCtx, identity), nil
		}
	case http.MethodDelete:
		if sid == "" {
			return handleDeleteAll(ctx, rmngCtx, identity), nil
		}
		return handleDeleteOne(ctx, rmngCtx, identity, sid), nil
	}
	return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
}

func main() {
	lambda.Start(handleRequest)
}
