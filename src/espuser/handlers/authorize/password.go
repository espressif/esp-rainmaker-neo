// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/legacyauth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

// passwordSignIn is the sign-in page's password form; Provider names a row whose password our page takes, and none means the first such row by name.
type passwordSignIn struct {
	FlowID   string `json:"flow_id"`
	Provider string `json:"provider,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handlePasswordSignIn finishes the browser's flow with a code or a consent hold and a session cookie, never tokens, which would step over the client's PKCE binding.
func handlePasswordSignIn(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	resp, err := signInWithPassword(ctx, request)
	if err != nil {
		rlog.Error(ctx).Err(err).Int("status", resp.StatusCode).Msg("password sign-in failed")
	}
	resp.Headers["Cache-Control"] = "no-store"
	return resp
}

// signInWithPassword returns the answer and, only for a failure support has to look at rather than the person's own mistake, the error to log.
func signInWithPassword(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	if flowID == "" {
		return expiredFlowResp(), nil
	}
	var in passwordSignIn
	if err := rmngrequest.ExtractRequestStruct(request, &in); err != nil {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Invalid request body."), nil
	}
	// A newer sign-in in another tab took the cookie; finishing it here would send this person to that tab's app.
	if in.FlowID != flowID {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.RMErrFlowReplaced, "Another sign-in was started in this browser."), nil
	}

	svc, err := legacyauth.NewService(ctx, in.Provider)
	switch {
	case errors.Is(err, legacyauth.ErrProviderNotFound):
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown provider."), nil
	// No provider takes a password on our page at all (ErrNoPasswordProvider): the deployment's fault, not the person's.
	case err != nil:
		return serverErrorResp(), err
	}

	redirectTo, setCookie, err := svc.SigninToFlow(ctx, flowID, in.Username, in.Password, auth.FirstPartyLogin{
		Origin:      session.OriginBrowser,
		UserAgent:   utils.UserAgent(request),
		IPAddress:   request.RequestContext.Identity.SourceIP,
		PriorCookie: rmngrequest.Cookie(request, session.CookieName),
	})
	refusal, refused := passwordRefusal(err)
	var resp events.APIGatewayProxyResponse
	switch {
	// Gone before the password was checked, or finished by another tab before the code was written.
	case errors.Is(err, auth_flows_db.ErrFlowExpired), errors.Is(err, auth_flows_db.ErrFlowNotFound):
		resp, err = expiredFlowResp(), nil
	case errors.Is(err, auth.ErrProviderNotAllowed):
		resp, err = oidc.OAuthErrorResp(http.StatusForbidden, oidc.RMErrProviderNotAllowed, legacyauth.ProviderNotAllowedMessage), nil
	case refused:
		resp, err = refusal, nil
	case isUnusableAccount(err):
		resp = oidc.OAuthErrorResp(http.StatusForbidden, unusableAccount.Code, unusableAccount.Message)
	case err != nil:
		resp = serverErrorResp()
	default:
		resp = utils.APIGwRespJSON(http.StatusOK, auth.RedirectResponse{RedirectTo: redirectTo})
	}
	if setCookie != "" {
		resp.Headers["Set-Cookie"] = setCookie
	}
	return resp, err
}

// passwordRefusal answers a password the pool did not accept; refused is false for a failure of ours after it was accepted.
func passwordRefusal(err error) (resp events.APIGatewayProxyResponse, refused bool) {
	switch {
	case errors.Is(err, legacyauth.ErrUserNotConfirmed):
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.RMErrUnconfirmedAccount, legacyauth.UnconfirmedAccountMessage), true
	case errors.Is(err, legacyauth.ErrNewPasswordRequired):
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.RMErrNewPasswordRequired, legacyauth.NewPasswordRequiredMessage), true
	case errors.Is(err, legacyauth.ErrPasswordResetRequired):
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.RMErrPasswordResetRequired, legacyauth.PasswordResetRequiredMessage), true
	case errors.Is(err, legacyauth.ErrSecondStepRequired):
		return oidc.OAuthErrorResp(http.StatusForbidden, unusableAccount.Code, unusableAccount.Message), true
	case errors.Is(err, legacyauth.ErrAuthenticationFailed):
		return oidc.OAuthErrorResp(http.StatusUnauthorized, oidc.RMErrInvalidCredentials, legacyauth.InvalidCredentialsMessage), true
	default:
		return events.APIGatewayProxyResponse{}, false
	}
}

func expiredFlowResp() events.APIGatewayProxyResponse {
	return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.RMErrExpiredFlow, expiredFlowMessage)
}

// serverErrorResp is for a failure of ours, which must never read as a wrong password.
func serverErrorResp() events.APIGatewayProxyResponse {
	return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "We could not finish signing you in. Please try again.")
}
