// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Single sign-on: /oauth2/authorize consults the authorization server's OWN session and, when
// one is live, issues the code immediately — no provider chooser, no upstream round trip.
// Implemented prompt semantics (OIDC Core s3.1.2.1): prompt=none answers silently or fails
// with login_required; any other requested prompt (login, consent, select_account) disables
// the shortcut so an interactive login proceeds; max_age bounds the session's auth_time; and
// id_token_hint pins the session to a named subject.
package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

// AuthorizeSessionRequest is what the SSO shortcut needs from an already-validated
// /oauth2/authorize request: the request itself, the auth service, the parsed query, the
// flow id the login is running under, and the client row StartAuthFlow already read.
type AuthorizeSessionRequest struct {
	Request events.APIGatewayProxyRequest
	Svc     *auth.OAuthUserAuthService
	Query   map[string]string
	FlowID  string
	Client  *clients.ClientResponse
}

// sessionShortCircuit runs after StartAuthFlow has validated the request (so redirect_uri is
// registry-checked and error redirects are safe). It returns (response, true) when it fully
// answered the request — a code 302 on a live session, or a login_required error redirect for
// a failed prompt=none — and (zero, false) to fall through to the normal login page. Every
// internal failure falls through rather than blocking login: fail to "no session", never open.
func sessionShortCircuit(ctx context.Context, in AuthorizeSessionRequest) (events.APIGatewayProxyResponse, bool) {
	request, svc, q, flowID := in.Request, in.Svc, in.Query, in.FlowID

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	sess := session.NewService(rmngCtx)

	silent := false
	for _, p := range strings.Fields(q["prompt"]) {
		if p == "none" {
			silent = true
			continue
		}
		// login / consent / select_account: the client explicitly asked for an interactive
		// step. prompt=login in particular MUST always be honoured — never shortcut past it.
		return events.APIGatewayProxyResponse{}, false
	}

	cookieValue := rmngrequest.Cookie(request, session.CookieName)
	s := sess.Lookup(cookieValue)

	// max_age (OIDC Core s3.1.2.1) bounds the AUTHENTICATION's age, which is why auth_time is
	// inherited from the upstream rather than stamped at session creation. Unparseable counts
	// as failed.
	//
	// >= not >: the spec leaves the equal case to the OP, and under > a session created in the
	// same second satisfies max_age=0 -- silently answering a demand for a fresh login from a
	// cached one.
	if s != nil && q["max_age"] != "" {
		maxAge, err := strconv.ParseInt(q["max_age"], 10, 64)
		if err != nil || time.Now().Unix()-s.AuthTime >= maxAge {
			s = nil
		}
	}

	// id_token_hint: the session must belong to the named subject. A hint that does not
	// verify, or that names someone else, drops the session rather than switching users.
	if hint := q["id_token_hint"]; hint != "" {
		sub, _, err := svc.VerifyIDTokenHint(hint)
		if err != nil || s == nil || sub != s.UserID {
			s = nil
		}
	}

	// allowed_providers again, because the shortcut is a second door into the same room. A
	// client that admits only one provider must not be entered on a session established with
	// a different one -- otherwise the restriction holds for the first login of a browser and
	// evaporates for every one after it, which is the worst shape a control can have.
	// A nil Client is a check that did not happen, not an unrestricted one, so it drops the session too.
	if s != nil && (in.Client == nil || !in.Client.AllowsProvider(s.Provider)) {
		rlog.Info(ctx).Str("client_id", q["client_id"]).Str("provider", s.Provider).
			Msg("sso: session provider not allowed for this client; no shortcut")
		s = nil
	}

	if s == nil {
		if silent {
			// Safe to redirect: StartAuthFlow already validated redirect_uri against the registry.
			return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrLoginRequired, q["state"]), true
		}
		return events.APIGatewayProxyResponse{}, false
	}

	// Same scope StartAuthFlow just stored on the flow, so the flow need not be re-read.
	// The session's auth_time, not now: nobody authenticated on this request.
	redirectTo, err := svc.CompleteStartedAuthFlow(ctx, flowID, s.UserID, s.SID, strings.Fields(q["scope"]), s.AuthTime)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("sso: session shortcut failed; falling back to login")
		if silent {
			return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrLoginRequired, q["state"]), true
		}
		return events.APIGatewayProxyResponse{}, false
	}
	// Roll the cookie on the shortcut too, not only at the federation callback: this is a
	// front-channel event, Lookup already bumped both clocks, and re-issuing the same opaque
	// value with a fresh Max-Age keeps cookie_expires_at in step so the browser does not drop a
	// cookie whose user agent is still live. Same value, because the server never learns a new one.
	headers := map[string]string{"Location": redirectTo, "Cache-Control": "no-store"}
	if cookieValue != "" {
		headers["Set-Cookie"] = session.SetCookieHeader(cookieValue, sess.MaxAge())
	}
	return events.APIGatewayProxyResponse{
		StatusCode: 302,
		Headers:    headers,
	}, true
}
