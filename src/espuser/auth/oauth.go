// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/url"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/otputil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/secretutil"

	"time"
)

// Authorize sentinels. StartAuthFlow's failures pick the handler's surface (error page vs
// redirect); ExchangeAuthCode collapses every code failure to ErrInvalidGrant (no oracle).
var (
	ErrInvalidClient   = fmt.Errorf("invalid_client")
	ErrInvalidRedirect = fmt.Errorf("invalid_redirect_uri")
	ErrInvalidRequest  = fmt.Errorf("invalid_request")
	ErrInvalidScope    = fmt.Errorf("invalid_scope")
	// ErrInvalidTarget is RFC 8707 s2: the client may not request a token for this resource.
	ErrInvalidTarget = fmt.Errorf("invalid_target")
	ErrInvalidGrant  = fmt.Errorf("invalid_grant")
)

// FlowTTL bounds the whole login (authorize -> OTP -> code exchange).
const FlowTTL = 10 * time.Minute

type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	// Resource is the RFC 8707 identifier of the API the access token is for (optional).
	Resource string
	// Prompt and MaxAge are recorded verbatim, not interpreted here. sessionShortCircuit
	// applies them to OUR session; the federation leg forwards them to the upstream. Both
	// need them and they are read at different moments, so the flow record is where they live.
	Prompt string
	MaxAge string
}

// StartAuthFlow validates the request, writes a LOGIN flow record, and returns its opaque flow id plus the client it validated against, so the caller need not re-read that row. Errors are typed so the handler knows whether it may redirect (scope) or must render the error page (client/redirect/PKCE).
func (s *OAuthUserAuthService) StartAuthFlow(ctx context.Context, req AuthorizeRequest) (flowID string, client *clients.ClientResponse, err error) {
	if req.ClientID == "" || req.RedirectURI == "" {
		return "", nil, ErrInvalidRequest
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, err = clients.NewService(rmngCtx).Get(req.ClientID)
	if err != nil {
		return "", nil, ErrInvalidClient
	}

	if !client.AllowsRedirectURI(req.RedirectURI) {
		return "", nil, ErrInvalidRedirect
	}
	// PKCE required per the client's policy (forced true for public — RFC 9700 §2.1.1); a
	// challenge that is present must be S256 (RFC 7636 §4.4.1 -> invalid_request otherwise).
	if client.RequirePKCE && req.CodeChallenge == "" {
		return "", nil, ErrInvalidRequest
	}
	if !oidc.IsValidPKCEChallenge(req.CodeChallenge, req.CodeChallengeMethod) {
		return "", nil, ErrInvalidRequest
	}
	if !client.AllowsScopes(req.Scope) {
		return "", nil, ErrInvalidScope
	}
	// Checked here, before the flow is written and long before the user is sent to a
	// provider. Deferring it to the code exchange would make someone complete an entire
	// login -- redirect, password, MFA -- only to be told the request was never valid.
	// Absent allowed_resources means none, never any.
	if req.Resource != "" && !client.AllowsResource(req.Resource) {
		return "", nil, ErrInvalidTarget
	}

	flowID, err = otputil.GenerateFlowID()
	if err != nil {
		return "", nil, err
	}
	if err := auth_flows_db.NewAuthFlowsDB(rmngCtx).CreateFlow(&auth_flows_db.AuthFlow{
		FlowID:              flowID,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		RequestedScope:      strings.Fields(req.Scope),
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Resource:            req.Resource,
		Prompt:              req.Prompt,
		MaxAge:              req.MaxAge,
		ExpiresOn:           time.Now().Add(FlowTTL).Unix(),
	}); err != nil {
		return "", nil, err
	}
	return flowID, client, nil
}

// CompleteAuthFlowForSubject issues the flow's single-use code for an authenticated subject.
// sid names the browser session the login ran under ("" when the session write failed); it rides the flow row so the code exchange can tie the refresh family to its parent session.
func (s *OAuthUserAuthService) CompleteAuthFlowForSubject(ctx context.Context, flowID, userID, sid string) (redirectTo string, err error) {
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).GetFlow(flowID)
	if err != nil {
		return "", err
	}
	// Reached only from a login that just happened, so the authentication is now.
	return s.CompleteStartedAuthFlow(ctx, flowID, userID, sid, flow.RequestedScope, time.Now().Unix())
}

// CompleteStartedAuthFlow is CompleteAuthFlowForSubject for a caller that already knows the flow's requested scope, skipping the read.
func (s *OAuthUserAuthService) CompleteStartedAuthFlow(ctx context.Context, flowID, userID, sid string, requestedScope []string, authTime int64) (redirectTo string, err error) {
	authCode, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return "", err
	}
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).IssueCode(flowID, userID, requestedScope, authCode, sid, authTime)
	if err != nil {
		return "", err
	}
	return appendCodeToRedirect(flow.RedirectURI, authCode, flow.State), nil
}

// FirstPartyLogin is what a surface that authenticated the person HERE -- the enterprise OTP
// addon, the legacy password API -- knows about the login it just completed. auth_time is the
// verify moment (Establish defaults it to now) and acr is empty: the authentication happened on
// this server, so there is no upstream context to inherit. Origin decides whether a browser is
// present to hold a cookie, and therefore whether the login can have single sign-on.
type FirstPartyLogin struct {
	Provider      string
	AMR           []string
	Origin        string // session.OriginBrowser or session.OriginApp
	UserAgent     string
	IPAddress     string
	UserAgentName string
	PriorCookie   string
}

// CompleteAuthFlowForFirstPartyLogin is the FRONT-channel first-party seam (a hosted login
// page's in-flow OTP verify): it establishes the browser's user agent, issues the flow's code, and
// returns both the redirect and the Set-Cookie the handler puts on the response. The cookie
// rides along because the verify is a same-origin request the browser itself made, so SSO
// becomes possible from here on.
func (s *OAuthUserAuthService) CompleteAuthFlowForFirstPartyLogin(ctx context.Context, flowID, userID string, login FirstPartyLogin) (redirectTo, setCookie string, err error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	sid, setCookie := establishFirstPartyUserAgent(rmngCtx, userID, login)
	redirectTo, err = s.CompleteAuthFlowForSubject(ctx, flowID, userID, sid)
	if err != nil {
		return "", "", err
	}
	return redirectTo, setCookie, nil
}

// MintTokenSetForFirstPartyLogin is the BACK-channel first-party seam (a native OTP verify, the
// legacy password API): it establishes a user agent and mints the token set stamped with that
// user agent's sid, so ending the session can reach the families opened through it and
// GET /oauth2/logout has a sid to delete by. An OriginApp login is cookie-less, so no Set-Cookie
// is returned; a login that establishes no user agent (a failed write) still mints, with sid == "".
func (s *OAuthUserAuthService) MintTokenSetForFirstPartyLogin(ctx context.Context, userID, clientID, scope string, login FirstPartyLogin) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	sid, _ := establishFirstPartyUserAgent(rmngCtx, userID, login)
	return s.mintTokenSet(rmngCtx, userID, clientID, scope, "", sid, time.Now().Unix())
}

// establishFirstPartyUserAgent records the user agent for a first-party login and returns its sid and,
// for a browser origin, the Set-Cookie to roll onto the response (empty for a cookie-less app).
// A session write that fails degrades to "no session": the login still succeeds with sid == ""
// and no cookie, because the user agent layer may cost single sign-on but must never block a login.
func establishFirstPartyUserAgent(rmngCtx *rmngctx.RmngContext, userID string, login FirstPartyLogin) (sid, setCookie string) {
	sess := session.NewService(rmngCtx)
	cookieValue, sid, err := sess.Establish(session.EstablishInput{
		UserID:        userID,
		Provider:      login.Provider,
		AMR:           login.AMR,
		Origin:        login.Origin,
		UserAgent:     login.UserAgent,
		IPAddress:     login.IPAddress,
		UserAgentName: login.UserAgentName,
		PriorCookie:   login.PriorCookie,
	})
	if err != nil {
		rlog.Warn(rmngCtx.Context).Err(err).Msg("first-party login: session not established")
		return "", ""
	}
	// Cookie-less (app) establish returns an empty cookie value: nothing to set.
	if cookieValue != "" {
		setCookie = session.SetCookieHeader(cookieValue, sess.MaxAge())
	}
	return sid, setCookie
}

// ExchangeAuthCode redeems a code for tokens: verify client/redirect/PKCE, consume (single-use), mint. Every failure is ErrInvalidGrant (no oracle).
func (s *OAuthUserAuthService) ExchangeAuthCode(ctx context.Context, code, verifier, clientID, redirectURI string) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngCtx)

	flow, err := flowsDB.GetFlowByCode(code)
	if err != nil { //If incorrect code, ErrFlowNotFound is returned here
		return nil, ErrInvalidGrant
	}
	// The redeeming client and redirect must match those that started the flow (RFC 6749 §4.1.3).
	if flow.ClientID != clientID || flow.RedirectURI != redirectURI {
		return nil, ErrInvalidGrant
	}
	// PKCE downgrade protection (RFC 9700 §2.1.1): a challenge iff a verifier. When the code was
	// bound to a challenge, the verifier must hash to it; a code issued without one takes none.
	if flow.CodeChallenge != "" {
		if !pkceutil.VerifyS256(verifier, flow.CodeChallenge) {
			return nil, ErrInvalidGrant
		}
	} else if verifier != "" {
		return nil, ErrInvalidGrant
	}
	// Single-use: a lost race or a replay fails the conditional consume — reuse is theft.
	if err := flowsDB.ConsumeCode(flow.FlowID); err != nil {
		return nil, ErrInvalidGrant
	}
	return s.mintTokenSet(rmngCtx, flow.Subject, flow.ClientID, strings.Join(flow.GrantedScope, " "), flow.Resource, flow.SID, flow.AuthTime)
}

// appendCodeToRedirect adds code+state as query params, preserving any query the client already set (RFC 6749 §4.1.2).
func appendCodeToRedirect(redirectURI, code, state string) string {
	q := url.Values{}
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	return oidc.AppendQuery(redirectURI, q)
}
