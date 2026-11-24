// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/url"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

// handleSignOutPage asks the person whether to end this browser's session (RP-Initiated Logout 1.0 s2); the page sends this URL's query back with the answer.
func handleSignOutPage(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	cookieValue := rmngrequest.Cookie(request, session.CookieName)
	// Find, not Lookup: asking re-issues no cookie, so it must not roll the session's retention either.
	sess, err := session.NewService(rmngCtx).Find(cookieValue)
	// A failed read is not "no session": sending the browser back to /oauth2/logout, whose own read may succeed, would bounce it between the two.
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("sign-out page: session unreadable")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	if sess == nil {
		// Nothing to ask: a sign-out's parameters go back to /oauth2/logout, which still owes the app its return, and a bare visit is where /oauth2/logout lands a signed-out browser.
		if len(request.MultiValueQueryStringParameters) > 0 {
			return redirect(routeLocation(request, pathLogout) + "?" + url.Values(request.MultiValueQueryStringParameters).Encode())
		}
		return renderPage(request, pageData{SignedOut: true})
	}
	logout := &logoutData{CSRFToken: session.LogoutCSRFToken(cookieValue)}
	if svc, err := auth.NewOAuthUserAuthService(ctx); err != nil {
		rlog.Warn(ctx).Err(err).Msg("sign-out page: auth service unavailable; the question goes without the account")
	} else {
		logout.Username = accountLabel(ctx, svc, sess.UserID)
	}
	return renderPage(request, pageData{Logout: logout})
}
