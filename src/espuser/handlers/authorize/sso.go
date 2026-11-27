// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Single sign-on: /oauth2/authorize answers from our own live session; per OIDC Core s3.1.2.1, prompt=none answers silently or fails, login and select_account force a login, and consent keeps the session but shows the consent screen.
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

// AuthorizeSessionRequest is what the SSO shortcut takes from an already-validated /oauth2/authorize request, including the client row and skip_consent StartAuthFlow settled.
type AuthorizeSessionRequest struct {
	Request     events.APIGatewayProxyRequest
	Svc         *auth.OAuthUserAuthService
	Query       map[string]string
	FlowID      string
	Client      *clients.ClientResponse
	SkipConsent bool
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

	// The app sent prompt=none: answer at once and never show a page (OIDC Core s3.1.2.1).
	promptNone := false
	for _, p := range strings.Fields(q["prompt"]) {
		if p == "none" {
			promptNone = true
			continue
		}
		if p == "consent" {
			continue
		}
		// login / select_account ask for a fresh login, and prompt=login MUST always be honoured.
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
		verified, err := svc.VerifyIDTokenHint(hint)
		if err != nil || s == nil || verified.Subject != s.UserID {
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
		if promptNone {
			// Safe to redirect: StartAuthFlow already validated redirect_uri against the registry.
			return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrLoginRequired, q["state"]), true
		}
		return events.APIGatewayProxyResponse{}, false
	}

	// A prompt=none request cannot show the consent screen, and with no stored consents it cannot be answered either.
	if promptNone && !in.SkipConsent {
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrConsentRequired, q["state"]), true
	}

	// Same scope StartAuthFlow just stored on the flow, and the session's sign-in because nobody authenticated on this request.
	codeRedirect, awaitingConsent, err := svc.CompleteAuthFlow(ctx, auth.CompleteAuthFlowInput{
		FlowID: flowID, UserID: s.UserID, SID: s.SID, RequestedScope: strings.Fields(q["scope"]), AuthTime: s.AuthTime, SkipConsent: in.SkipConsent,
		Provider: s.Provider, AMR: s.AMR,
	})
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("sso: session shortcut failed; falling back to login")
		if promptNone {
			return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrLoginRequired, q["state"]), true
		}
		return events.APIGatewayProxyResponse{}, false
	}
	// Re-issuing the same cookie value with a fresh Max-Age keeps the browser's copy alive as long as the session row Lookup just rolled.
	rolledSession := ""
	if cookieValue != "" {
		rolledSession = session.SetCookieHeader(cookieValue, sess.MaxAge())
	}
	if awaitingConsent {
		// The consent screen finds the held sign-in by the flow cookie, which this request has not set yet.
		cookies := []string{flowCookie(flowID)}
		if rolledSession != "" {
			cookies = append(cookies, rolledSession)
		}
		resp := redirect(routeLocation(request, pathConsent))
		resp.MultiValueHeaders = map[string][]string{"Set-Cookie": cookies}
		return resp, true
	}
	resp := redirect(codeRedirect)
	if rolledSession != "" {
		resp.Headers["Set-Cookie"] = rolledSession
	}
	return resp, true
}
