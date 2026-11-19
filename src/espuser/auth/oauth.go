// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	scopepkg "github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/otputil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/secretutil"
)

// Authorize sentinels. StartAuthFlow's failures pick the handler's surface (error page vs
// redirect); ExchangeAuthCode collapses every code failure to ErrInvalidGrant (no oracle).
var (
	ErrInvalidClient   = fmt.Errorf("invalid_client")
	ErrInvalidRedirect = fmt.Errorf("invalid_redirect_uri")
	ErrInvalidRequest  = fmt.Errorf("invalid_request")
	ErrInvalidScope    = fmt.Errorf("invalid_scope")
	// ErrInvalidPrompt is prompt=none sent with another value, which OIDC Core s3.1.2.1 makes an error; unlike ErrInvalidRequest it is found after redirect_uri is validated, so it goes back to the client.
	ErrInvalidPrompt = fmt.Errorf("invalid_request: prompt=none with another value")
	// ErrInvalidTarget is RFC 8707 s2: the client may not request a token for this resource.
	ErrInvalidTarget = fmt.Errorf("invalid_target")
	ErrInvalidGrant  = fmt.Errorf("invalid_grant")
)

// FlowCookieName carries the sign-in in progress; __Host- stops a sibling host planting a flow id, so every endpoint that issues a code reads the flow from this cookie.
const FlowCookieName = "__Host-esp_flow_id"

// RedirectResponse is what a browser mid-sign-in gets instead of tokens: somewhere to go next, and nothing it could spend.
type RedirectResponse struct {
	RedirectTo string `json:"redirect_to"`
}

type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	// Resources are the RFC 8707 resources the sign-in is for; none means the client's default.
	Resources []string
	// Prompt and MaxAge are recorded verbatim, not interpreted here. sessionShortCircuit
	// applies them to OUR session; the federation leg forwards them to the upstream. Both
	// need them and they are read at different moments, so the flow record is where they live.
	Prompt string
	MaxAge string
}

// StartAuthFlow returns the client it validated and the skipConsent it stored so the caller need not re-read either; its errors are typed because only a prompt, scope or resource error may redirect to the client.
func (s *OAuthUserAuthService) StartAuthFlow(ctx context.Context, req AuthorizeRequest) (flowID string, client *clients.ClientResponse, skipConsent bool, err error) {
	if req.ClientID == "" || req.RedirectURI == "" {
		return "", nil, false, ErrInvalidRequest
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, err = clients.NewService(rmngCtx).Get(req.ClientID)
	if err != nil {
		return "", nil, false, ErrInvalidClient
	}

	if !client.AllowsRedirectURI(req.RedirectURI) {
		return "", nil, false, ErrInvalidRedirect
	}
	// PKCE required per the client's policy (forced true for public — RFC 9700 §2.1.1); a
	// challenge that is present must be S256 (RFC 7636 §4.4.1 -> invalid_request otherwise).
	if client.RequirePKCE && req.CodeChallenge == "" {
		return "", nil, false, ErrInvalidRequest
	}
	if !oidc.IsValidPKCEChallenge(req.CodeChallenge, req.CodeChallengeMethod) {
		return "", nil, false, ErrInvalidRequest
	}
	if prompts := strings.Fields(req.Prompt); slices.Contains(prompts, "none") && len(prompts) > 1 {
		return "", nil, false, ErrInvalidPrompt
	}
	if !client.AllowsScopes(req.Scope) {
		return "", nil, false, ErrInvalidScope
	}
	// Settled before the flow is written, so nobody completes a whole login for a request that was never valid.
	resources, err := authorizedResources(client, req.Resources)
	if err != nil {
		return "", nil, false, err
	}

	flowID, err = otputil.GenerateFlowID()
	if err != nil {
		return "", nil, false, err
	}
	skipConsent = SkipsConsent(client, req.Prompt, req.Scope)
	if err := auth_flows_db.NewAuthFlowsDB(rmngCtx).CreateFlow(&auth_flows_db.AuthFlow{
		FlowID:              flowID,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		RequestedScope:      strings.Fields(req.Scope),
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Resources:           resources,
		Prompt:              req.Prompt,
		MaxAge:              req.MaxAge,
		SkipConsent:         skipConsent,
		ExpiresOn:           time.Now().Add(auth_flows_db.FlowTTL).Unix(),
	}); err != nil {
		return "", nil, false, err
	}
	return flowID, client, skipConsent, nil
}

// ConsentPath is where a sign-in held for consent continues, relative to the sign-in page at /user/login.
const ConsentPath = "login/consent"

// ErrNotAwaitingConsent is a consent answer with no sign-in waiting for it: never signed in, already answered, or unknown.
var ErrNotAwaitingConsent = errors.New("no sign-in is waiting for consent")

// SkipsConsent is true only for a client we ship that did not send prompt=consent; RFC 6749 s10.2 and RFC 8252 s8.6 have the person approve any other client, and OIDC Core 1.0 s11 requires consent for offline_access from every client.
func SkipsConsent(client *clients.ClientResponse, prompt, requestedScope string) bool {
	return client.FirstParty && !slices.Contains(strings.Fields(prompt), "consent") && !scopepkg.HasOfflineAccess(requestedScope)
}

// CompleteAuthFlowInput is an authentication that has just finished a flow's login step.
type CompleteAuthFlowInput struct {
	FlowID         string
	UserID         string
	SID            string
	RequestedScope []string
	AuthTime       int64
	// SkipConsent is the flow's own decision from StartAuthFlow.
	SkipConsent bool
}

// CompleteAuthFlow issues the flow's code, or, unless in.SkipConsent, holds the sign-in for the person's answer and returns awaitingConsent.
func (s *OAuthUserAuthService) CompleteAuthFlow(ctx context.Context, in CompleteAuthFlowInput) (codeRedirect string, awaitingConsent bool, err error) {
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if !in.SkipConsent {
		return "", true, flowsDB.AwaitConsent(in.FlowID, in.UserID, in.RequestedScope, in.SID, in.AuthTime)
	}
	authCode, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return "", false, err
	}
	flow, err := flowsDB.IssueCode(in.FlowID, in.UserID, in.RequestedScope, authCode, in.SID, in.AuthTime)
	if err != nil {
		return "", false, err
	}
	return appendCodeToRedirect(flow.RedirectURI, authCode, flow.State), false, nil
}

// FlowAwaitingConsent returns the sign-in waiting for the person's consent, for the consent screen to describe.
func (s *OAuthUserAuthService) FlowAwaitingConsent(ctx context.Context, flowID string) (*auth_flows_db.AuthFlow, error) {
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).GetFlow(flowID)
	if errors.Is(err, auth_flows_db.ErrFlowNotFound) {
		return nil, ErrNotAwaitingConsent
	}
	if err != nil {
		return nil, err
	}
	if !flow.AwaitingConsent() {
		return nil, ErrNotAwaitingConsent
	}
	return flow, nil
}

// GrantConsent issues the code a sign-in was held for, to the browser session sid that signed in; a flow the conditional write refuses (expired, already answered, never signed in, another session's) has to start again.
func (s *OAuthUserAuthService) GrantConsent(ctx context.Context, flowID, sid string) (codeRedirect string, err error) {
	authCode, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return "", err
	}
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).IssueConsentedCode(flowID, authCode, sid)
	if errors.Is(err, auth_flows_db.ErrFlowNotFound) {
		return "", auth_flows_db.ErrFlowExpired
	}
	if err != nil {
		return "", err
	}
	return appendCodeToRedirect(flow.RedirectURI, authCode, flow.State), nil
}

// DenyConsent ends a held sign-in and returns the client's redirect carrying access_denied (RFC 6749 s4.1.2.1).
func (s *OAuthUserAuthService) DenyConsent(ctx context.Context, flowID string) (errorRedirect string, err error) {
	flow, err := s.FlowAwaitingConsent(ctx, flowID)
	if err != nil {
		return "", err
	}
	return s.EndFlowDenied(ctx, flow)
}

// EndFlowDenied ends a sign-in the person declined and returns the client's redirect carrying access_denied (RFC 6749 s4.1.2.1).
func (s *OAuthUserAuthService) EndFlowDenied(ctx context.Context, flow *auth_flows_db.AuthFlow) (errorRedirect string, err error) {
	if err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).DeleteFlow(flow.FlowID); err != nil {
		return "", err
	}
	return oidc.ErrorRedirectURL(flow.RedirectURI, oidc.OAuthErrAccessDenied, flow.State), nil
}

// RestartQuery rebuilds the /oauth2/authorize query an expired sign-in began with, so "start again" resumes the client's own request.
func (s *OAuthUserAuthService) RestartQuery(ctx context.Context, flowID string) (url.Values, error) {
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).GetFlowIgnoringExpiry(flowID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("response_type", oidc.ResponseTypeCode)
	q.Set("client_id", flow.ClientID)
	q.Set("redirect_uri", flow.RedirectURI)
	for key, value := range map[string]string{
		"scope":                 strings.Join(flow.RequestedScope, " "),
		"state":                 flow.State,
		"code_challenge":        flow.CodeChallenge,
		"code_challenge_method": flow.CodeChallengeMethod,
		"prompt":                flow.Prompt,
		"max_age":               flow.MaxAge,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	for _, resource := range flow.Resources {
		q.Add("resource", resource)
	}
	return q, nil
}

// FirstPartyLogin is what a surface that authenticated the person knows about that login; Origin says whether a browser is present to hold a cookie, and so whether the login gets single sign-on.
type FirstPartyLogin struct {
	// AllAllowedResources makes the login cover every resource the client may request, for a surface that takes no resource parameter (the legacy password API); otherwise it covers the client's default.
	AllAllowedResources bool
	Provider            string
	// AuthTime is when the person authenticated (the upstream's auth_time, OIDC Core s2); zero means on this request.
	AuthTime int64
	AMR      []string
	ACR      string
	// ProviderMaxTTLSeconds is the provider row's session cap; zero leaves the global one.
	ProviderMaxTTLSeconds int64
	Origin                string // session.OriginBrowser or session.OriginApp
	UserAgent             string
	IPAddress             string
	UserAgentName         string
	PriorCookie           string
}

// ErrProviderNotAllowed is a sign-in with a provider the flow's client did not register, or whose client cannot be read.
var ErrProviderNotAllowed = errors.New("the client does not allow this provider")

// FlowWithClient returns the flow alongside an unreadable client's error, so a caller can tell a lost flow from a lost client.
func (s *OAuthUserAuthService) FlowWithClient(ctx context.Context, flowID string) (*auth_flows_db.AuthFlow, *clients.ClientResponse, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlow(flowID)
	if err != nil {
		return nil, nil, err
	}
	client, err := clients.NewService(rmngCtx).Get(flow.ClientID)
	if err != nil {
		return flow, nil, rmerror.NewRMError(err, "failed to read the flow's client")
	}
	return flow, client, nil
}

// FlowAllowingProvider reads the flow and returns it only when its client allows the provider; an unreadable client refuses, or the restriction would vanish the day the registry is unhealthy.
func (s *OAuthUserAuthService) FlowAllowingProvider(ctx context.Context, flowID, provider string) (*auth_flows_db.AuthFlow, error) {
	flow, client, err := s.FlowWithClient(ctx, flowID)
	if err != nil {
		if flow == nil {
			return nil, err
		}
		rlog.Warn(ctx).Err(err).Str("client_id", flow.ClientID).Msg("sign-in refused: client unreadable")
		return nil, ErrProviderNotAllowed
	}
	if !client.AllowsProvider(provider) {
		return nil, ErrProviderNotAllowed
	}
	return flow, nil
}

// CompleteAuthFlowForFirstPartyLogin checks the flow's client allows login.Provider, then completes the flow as CompleteSignIn does; the enterprise OTP add-on's browser verify calls it.
func (s *OAuthUserAuthService) CompleteAuthFlowForFirstPartyLogin(ctx context.Context, flowID, userID string, login FirstPartyLogin) (redirectTo, setCookie string, err error) {
	flow, err := s.FlowAllowingProvider(ctx, flowID, login.Provider)
	if err != nil {
		return "", "", err
	}
	return s.CompleteSignIn(ctx, flow, userID, login)
}

// CompleteSignIn returns the code redirect or ConsentPath, and setCookie even alongside an error, because the new session has already replaced the one the browser presented.
func (s *OAuthUserAuthService) CompleteSignIn(ctx context.Context, flow *auth_flows_db.AuthFlow, userID string, login FirstPartyLogin) (redirectTo, setCookie string, err error) {
	if login.AuthTime <= 0 {
		login.AuthTime = time.Now().Unix()
	}
	sid, setCookie := establishFirstPartyUserAgent(rmngctx.NewRmngContextWithCtx(ctx, nil), userID, login)
	codeRedirect, awaitingConsent, err := s.CompleteAuthFlow(ctx, CompleteAuthFlowInput{
		FlowID: flow.FlowID, UserID: userID, SID: sid, RequestedScope: flow.RequestedScope, AuthTime: login.AuthTime, SkipConsent: flow.SkipConsent,
	})
	if err != nil {
		return "", setCookie, err
	}
	if awaitingConsent {
		return ConsentPath, setCookie, nil
	}
	return codeRedirect, setCookie, nil
}

// MintTokenSetForFirstPartyLogin is the BACK-channel first-party seam (a native OTP verify, the
// legacy password API): it establishes a user agent and mints the token set stamped with that
// user agent's sid, so ending the session can reach the families opened through it and
// GET /oauth2/logout has a sid to delete by. An OriginApp login is cookie-less, so no Set-Cookie
// is returned; a login that establishes no user agent (a failed write) still mints, with sid == "".
func (s *OAuthUserAuthService) MintTokenSetForFirstPartyLogin(ctx context.Context, userID, clientID, scope string, login FirstPartyLogin) (*UserTokens, error) {
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(clientID)
	if err != nil {
		return nil, err
	}
	return s.MintTokenSetForClient(ctx, userID, client, scope, login)
}

// MintTokenSetForClient is MintTokenSetForFirstPartyLogin for a caller that already read the client row, so the sign-in reads it once.
func (s *OAuthUserAuthService) MintTokenSetForClient(ctx context.Context, userID string, client *clients.ClientResponse, scope string, login FirstPartyLogin) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	var requested []string
	if login.AllAllowedResources {
		requested = client.AllowedResources
	}
	resources, err := authorizedResources(client, requested)
	if err != nil {
		return nil, err
	}
	audience, err := SetTokenAudienceFromResource(client, resources, "")
	if err != nil {
		return nil, err
	}
	sid, _ := establishFirstPartyUserAgent(rmngCtx, userID, login)
	// OIDC Core 1.0 s11: offline_access is ignored unless a code is returned, and this channel returns none and shows no consent.
	grantedScope := scopepkg.Without(scope, scopepkg.OfflineAccess)
	return s.mintTokenSet(rmngCtx, userID, client.ClientID, grantedScope, resources, audience, sid, time.Now().Unix())
}

// establishFirstPartyUserAgent degrades a failed session write to sid "" and no cookie, because a lost single sign-on must never block the login.
func establishFirstPartyUserAgent(rmngCtx *rmngctx.RmngContext, userID string, login FirstPartyLogin) (sid, setCookie string) {
	sess := session.NewService(rmngCtx)
	cookieValue, sid, err := sess.Establish(session.EstablishInput{
		UserID:                userID,
		Provider:              login.Provider,
		AuthTime:              login.AuthTime,
		AMR:                   login.AMR,
		ACR:                   login.ACR,
		ProviderMaxTTLSeconds: login.ProviderMaxTTLSeconds,
		Origin:                login.Origin,
		UserAgent:             login.UserAgent,
		IPAddress:             login.IPAddress,
		UserAgentName:         login.UserAgentName,
		PriorCookie:           login.PriorCookie,
	})
	if err != nil {
		rlog.Warn(rmngCtx.Context).Err(err).Msg("sign-in: session not established")
		return "", ""
	}
	// Cookie-less (app) establish returns an empty cookie value: nothing to set.
	if cookieValue != "" {
		setCookie = session.SetCookieHeader(cookieValue, sess.MaxAge())
	}
	return sid, setCookie
}

// ExchangeAuthCode redeems a code: verify client/redirect/PKCE, pick the access token's resource, consume (single-use), mint. Every failure but the resource's and a storage failure is ErrInvalidGrant (no oracle).
func (s *OAuthUserAuthService) ExchangeAuthCode(ctx context.Context, client *clients.ClientResponse, code, verifier, redirectURI, resource string) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngCtx)

	flow, err := flowsDB.GetFlowByCode(code)
	if err != nil { //If incorrect code, ErrFlowNotFound is returned here
		return nil, ErrInvalidGrant
	}
	// The redeeming client and redirect must match those that started the flow (RFC 6749 §4.1.3).
	if flow.ClientID != client.ClientID || flow.RedirectURI != redirectURI {
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
	// Before the consume, so a client that named a wrong resource can retry with the same code.
	audience, err := SetTokenAudienceFromResource(client, flow.Resources, resource)
	if err != nil {
		return nil, err
	}
	scope := strings.Join(flow.GrantedScope, " ")
	// An offline grant will not hang off the browser session that approved it, so nothing would later end one redeemed after that session ended.
	if scopepkg.HasOfflineAccess(scope) && flow.SID != "" {
		if _, err := liveSession(rmngCtx, flow.Subject, flow.SID); err != nil {
			return nil, err
		}
	}
	// Single-use: a lost race or a replay fails the conditional consume — reuse is theft.
	if err := flowsDB.ConsumeCode(flow.FlowID); err != nil {
		return nil, ErrInvalidGrant
	}
	return s.mintTokenSet(rmngCtx, flow.Subject, flow.ClientID, scope, flow.Resources, audience, flow.SID, flow.AuthTime)
}

// liveSession returns the user's session named sid, or ErrInvalidGrant when it has ended.
func liveSession(rmngCtx *rmngctx.RmngContext, userID, sid string) (*sessions_db.SessionEntry, error) {
	row, err := session.NewService(rmngCtx).FindBySID(userID, sid)
	if errors.Is(err, sessions_db.ErrSessionNotFound) {
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, err
	}
	if row.ExpiresAt != 0 && time.Now().Unix() >= row.ExpiresAt {
		return nil, ErrInvalidGrant
	}
	return row, nil
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
