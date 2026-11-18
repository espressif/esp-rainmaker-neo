// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

const (
	consentAllow = "allow"
	consentDeny  = "deny"
)

type consentAnswer struct {
	Decision string `json:"decision"`
	// FlowID is the sign-in the screen was drawn for, so an Allow never answers a newer one that took the cookie.
	FlowID string `json:"flow_id"`
}

// handleConsentPage draws the consent screen for the sign-in this browser's flow cookie holds.
func handleConsentPage(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	const ended = "This sign-in has ended. Please start again from the application."
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	if flowID == "" {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, ended, "")
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	flow, err := svc.FlowAwaitingConsent(ctx, flowID)
	switch {
	case errors.Is(err, auth.ErrNotAwaitingConsent), errors.Is(err, auth_flows_db.ErrFlowExpired):
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, ended, "")
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("consent page: flow unreadable")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	data := pageData{
		FlowID:     flowID,
		RestartURL: routeLocation(request, pathRestart),
		// A client with no name is shown by its id, so the person never approves a blank.
		Client:  clientData{Name: flow.ClientID},
		Consent: &consentData{Account: accountLabel(ctx, svc, flow.Subject), Scopes: flow.GrantedScope},
	}
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(flow.ClientID)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("consent page: client unreadable; naming it by its id")
		return renderPage(request, data)
	}
	if client.ClientName != "" {
		data.Client.Name = client.ClientName
	}
	data.Client.LogoURI = client.LogoURI
	return renderPage(request, data)
}

// accountLabel names the signed-in person on a screen; empty when unreadable, since the screen still works without it.
func accountLabel(ctx context.Context, svc *auth.OAuthUserAuthService, userID string) string {
	label, err := svc.AccountLabel(ctx, userID)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("account not readable; the screen goes without it")
		return ""
	}
	return label
}

// handleConsentAnswer records the person's Allow or Cancel and tells the page where to send the browser.
func handleConsentAnswer(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	var answer consentAnswer
	if err := rmngrequest.ExtractRequestStruct(request, &answer); err != nil || (answer.Decision != consentAllow && answer.Decision != consentDeny) {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "decision must be allow or deny.")
	}
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	if flowID == "" {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "No sign-in is waiting for an answer.")
	}
	if answer.FlowID != flowID {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.RMErrFlowReplaced, "Another sign-in was started in this browser.")
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	var redirectTo string
	if answer.Decision == consentAllow {
		// A held sign-in outlives a sign-out by minutes, so only the session that signed in may allow it; no session reads as the empty sid a sign-in without one was held under.
		var sid string
		if sess := session.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Peek(rmngrequest.Cookie(request, session.CookieName)); sess != nil {
			sid = sess.SID
		}
		redirectTo, err = svc.GrantConsent(ctx, flowID, sid)
	} else {
		redirectTo, err = svc.DenyConsent(ctx, flowID)
	}
	switch {
	case err == nil:
		resp := utils.APIGwRespJSON(http.StatusOK, auth.RedirectResponse{RedirectTo: redirectTo})
		// The redirect carries the code or the denial, so no cache may keep it.
		resp.Headers["Cache-Control"] = "no-store"
		return resp
	// A repeated Allow or Cancel finds its sign-in answered or gone, which reads the same as one that expired.
	case errors.Is(err, auth_flows_db.ErrFlowExpired), errors.Is(err, auth.ErrNotAwaitingConsent):
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.RMErrExpiredFlow, expiredFlowMessage)
	default:
		rlog.Error(ctx).Err(err).Msg("consent answer failed")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
}
