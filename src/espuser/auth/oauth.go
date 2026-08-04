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
	"github.com/espressif/esp-rainmaker-neo/src/utils/otputil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/pkceutil"
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
	ErrInvalidGrant    = fmt.Errorf("invalid_grant")
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
}

// StartAuthFlow validates the request, writes a LOGIN flow record, and returns its opaque flow id. Errors are typed so the handler knows whether it may redirect (scope) or must render the error page (client/redirect/PKCE).
func (s *OAuthUserAuthService) StartAuthFlow(ctx context.Context, req AuthorizeRequest) (flowID string, err error) {
	if req.ClientID == "" || req.RedirectURI == "" {
		return "", ErrInvalidRequest
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, err := clients.NewService(rmngCtx).Get(req.ClientID)
	if err != nil {
		return "", ErrInvalidClient
	}

	if !client.AllowsRedirectURI(req.RedirectURI) {
		return "", ErrInvalidRedirect
	}
	// PKCE required per the client's policy (forced true for public — RFC 9700 §2.1.1); a
	// challenge that is present must be S256 (RFC 7636 §4.4.1 -> invalid_request otherwise).
	if client.RequirePKCE && req.CodeChallenge == "" {
		return "", ErrInvalidRequest
	}
	if !oidc.IsValidPKCEChallenge(req.CodeChallenge, req.CodeChallengeMethod) {
		return "", ErrInvalidRequest
	}
	if !client.AllowsScopes(req.Scope) {
		return "", ErrInvalidScope
	}

	flowID, err = otputil.GenerateFlowID()
	if err != nil {
		return "", err
	}
	if err := auth_flows_db.NewAuthFlowsDB(rmngCtx).CreateFlow(&auth_flows_db.AuthFlow{
		FlowID:              flowID,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		RequestedScope:      strings.Fields(req.Scope),
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		ExpiresOn:           time.Now().Add(FlowTTL).Unix(),
	}); err != nil {
		return "", err
	}
	return flowID, nil
}

func (s *OAuthUserAuthService) CompleteAuthFlowForSubject(ctx context.Context, flowID, userID string) (redirectTo string, err error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngCtx)

	authCode, err := secretutil.GenRandom(secretutil.DefaultSecretBytes)
	if err != nil {
		return "", err
	}
	flow, err := flowsDB.GetFlow(flowID)
	if err != nil {
		return "", err
	}
	if err := flowsDB.IssueCode(flowID, userID, flow.RequestedScope, authCode); err != nil {
		return "", err
	}
	return appendCodeToRedirect(flow.RedirectURI, authCode, flow.State), nil
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
	return s.mintTokenSet(rmngCtx, flow.Subject, flow.ClientID, strings.Join(flow.GrantedScope, " "))
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
