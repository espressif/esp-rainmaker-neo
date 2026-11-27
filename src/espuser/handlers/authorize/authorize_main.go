// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// GET /oauth2/authorize (validate -> LOGIN flow record -> 302 to the login page with a flow_id cookie) and the served login UI. Spec: espuser/docs/en/specs/authorize-code-flow.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	hostedui "github.com/espressif/esp-rainmaker-neo/src/espuser/hosted-ui"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	pathAuthorize = "/oauth2/authorize"
	pathLogout    = "/oauth2/logout"
	pathLogin     = "/user/login"
	pathRestart   = pathLogin + "/restart"
	pathConsent   = "/user/" + auth.ConsentPath
	pathSignOut   = "/user/logout"
	pathPassword  = pathLogin + "/password"
)

// expiredFlowMessage is what every expired or lost sign-in says, page or JSON.
const expiredFlowMessage = "This sign-in took too long. Start again."

func handleAuthorize(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := request.QueryStringParameters
	if code := oidc.ValidateResponseType(q["response_type"]); code != "" {
		return errorPage(request, http.StatusBadRequest, code, "response_type must be code.", ""), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}

	flowID, client, skipConsent, err := svc.StartAuthFlow(ctx, auth.AuthorizeRequest{
		ClientID:            q["client_id"],
		RedirectURI:         q["redirect_uri"],
		Scope:               q["scope"],
		State:               q["state"],
		CodeChallenge:       q["code_challenge"],
		CodeChallengeMethod: q["code_challenge_method"],
		// Repeatable (RFC 8707 s2.1), so only the multi-value map holds every one.
		Resources: request.MultiValueQueryStringParameters["resource"],
		Prompt:    q["prompt"],
		MaxAge:    q["max_age"],
	})
	if err != nil {
		return authorizeError(request, err), nil
	}

	// A live session of our own answers without the login page or the upstream leg (SSO).
	// Runs only after StartAuthFlow validated the request, so its error redirects are safe.
	if resp, done := sessionShortCircuit(ctx, AuthorizeSessionRequest{Request: request, Svc: svc, Query: q, FlowID: flowID, Client: client, SkipConsent: skipConsent}); done {
		return resp, nil
	}

	resp := redirect(loginRedirect(ctx, request, flowID))
	resp.Headers["Set-Cookie"] = flowCookie(flowID)
	return resp, nil
}

// flowCookie names the sign-in in progress; HttpOnly keeps it from scripts and SameSite=Lax keeps it off another site's POST.
func flowCookie(flowID string) string {
	return fmt.Sprintf("%s=%s; Path=/; HttpOnly; Secure; SameSite=Lax", auth.FlowCookieName, flowID)
}

func redirect(location string) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers:    map[string]string{"Location": location, "Cache-Control": "no-store"},
	}
}

// loginRedirect sends a lone oidc provider straight to its own page, since there is nothing to choose; anything else gets our page, which reads the flow's client itself.
func loginRedirect(ctx context.Context, request events.APIGatewayProxyRequest, flowID string) string {
	loginPage := routeLocation(request, pathLogin)
	registry, err := newRegistry(ctx)
	if err != nil {
		return loginPage
	}
	enabled, err := registry.EnabledEntries()
	if err != nil {
		return loginPage
	}
	// A lone row answered on our own page (a password_verifier or otp) is not skipped to, so only a provider that redirects is.
	if len(enabled) == 1 && enabled[0].RedirectsToProvider() {
		return routeLocation(request, pathFederationStart+"?"+url.Values{"provider": {enabled[0].ProviderName}, "flow_id": {flowID}}.Encode())
	}
	q := url.Values{}
	for _, hint := range []string{"login_hint", "idp"} {
		if v := request.QueryStringParameters[hint]; v != "" {
			q.Set(hint, v)
		}
	}
	if len(q) == 0 {
		return loginPage
	}
	return loginPage + "?" + q.Encode()
}

// stageFor returns the stage segment Location headers must re-prepend. On the default
// <api-id>.execute-api host the browser-facing URL carries /<stage>, which API Gateway
// strips from request.Path. Through a custom domain the base-path mapping pins the
// stage server-side and the public URL has no /<stage> prefix, so re-prepending it
// there would redirect to a path that does not exist (e.g. /prod/user/login).
func stageFor(request events.APIGatewayProxyRequest) string {
	if strings.Contains(request.RequestContext.DomainName, ".execute-api.") {
		return request.RequestContext.Stage
	}
	return ""
}

// authorizeError picks the surface: client/redirect/PKCE render the error page (can't redirect to an unvalidated URI); scope redirects (redirect_uri already validated).
func authorizeError(request events.APIGatewayProxyRequest, err error) events.APIGatewayProxyResponse {
	q := request.QueryStringParameters
	switch {
	case errors.Is(err, auth.ErrInvalidClient):
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidClient, "Unknown client.", "")
	case errors.Is(err, auth.ErrInvalidRedirect):
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "redirect_uri is not registered for this client.", "")
	case errors.Is(err, auth.ErrInvalidRequest):
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing or invalid request parameters (PKCE S256 is required).", "")
	case errors.Is(err, auth.ErrInvalidPrompt):
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrInvalidRequest, q["state"])
	case errors.Is(err, auth.ErrInvalidScope):
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrInvalidScope, q["state"])
	case errors.Is(err, auth.ErrInvalidTarget):
		// Redirects, like scope: by this point redirect_uri has been validated against the
		// registry, so sending the error back to the client is safe and is what RFC 6749
		// s4.1.2.1 asks for.
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrInvalidTarget, q["state"])
	default:
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
}

// withStagePrefix re-prepends the API Gateway stage a Location needs: on the default execute-api host the browser-facing URL carries /<stage>, which the gateway strips before we see it.
func withStagePrefix(path, stage string) string {
	if stage == "" {
		return path
	}
	return "/" + stage + path
}

// routeLocation is the browser-facing URL of one of our routes: "/user/login/consent" on the execute-api host -> "/prod/user/login/consent".
func routeLocation(request events.APIGatewayProxyRequest, route string) string {
	return withStagePrefix(route, stageFor(request))
}

// handleRestart re-runs an expired login's original authorization request; the expired flow holds only the client's request, never a credential, and /oauth2/authorize validates it afresh.
func handleRestart(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	if flowID == "" {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "There is no sign-in to restart.", "")
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	q, err := svc.RestartQuery(ctx, flowID)
	switch {
	case errors.Is(err, auth_flows_db.ErrFlowNotFound):
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "This sign-in can no longer be restarted. Please start from the application.", "")
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("restart: flow unreadable")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	return redirect(routeLocation(request, pathAuthorize) + "?" + q.Encode())
}

// handleLogin draws the page for the cookie's flow; a browser with no flow cookie gets no restart link, since there is nothing to restart.
func handleLogin(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	if flowID == "" {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "There is no sign-in in progress. Please start again from the application.", "")
	}
	restartURL := routeLocation(request, pathRestart)
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", restartURL)
	}
	_, client, err := svc.FlowWithClient(ctx, flowID)
	switch {
	// Restart rebuilds the request from the flow row, so once the row is gone there is nothing for Start again to do.
	case errors.Is(err, auth_flows_db.ErrFlowNotFound):
		return errorPage(request, http.StatusBadRequest, oidc.RMErrExpiredFlow, "This sign-in took too long. Please start again from the application.", "")
	// A client deleted since the flow began ends it like an expiry; a restart then reports the client's own error.
	case errors.Is(err, auth_flows_db.ErrFlowExpired), errors.Is(err, clients.ErrClientNotFound):
		return errorPage(request, http.StatusBadRequest, oidc.RMErrExpiredFlow, expiredFlowMessage, restartURL)
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("login page: flow or client unreadable")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", restartURL)
	}
	// One registry scan describes the whole login; the app renders from that description.
	data := buildPageData(enabledProviders(ctx), client.AllowsProvider, client.ClientName, request.QueryStringParameters, restartURL, flowID)
	// A page with no way in would be an empty card, so the person is told instead.
	if len(data.Providers) == 0 && !data.OTPEnabled {
		rlog.Warn(ctx).Str("client_id", client.ClientID).Msg("login page: no sign-in method for this client")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "No sign-in method is available for this application.", "")
	}
	return renderPage(request, data)
}

// enabledProviders is the enabled registry rows, or none when the registry cannot be read: the page then draws what it can rather than failing.
func enabledProviders(ctx context.Context) []identity_providers_db.ProviderEntry {
	registry, err := newRegistry(ctx)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("login page: provider registry unavailable")
		return nil
	}
	enabled, err := registry.EnabledEntries()
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("login page: provider registry unreadable")
	}
	return enabled
}

// renderShell is hostedui.Shell; specs swap it to serve with the bundle missing.
var renderShell = hostedui.Shell

// renderPage serves the sign-in app with its page data.
func renderPage(request events.APIGatewayProxyRequest, data pageData) events.APIGatewayProxyResponse {
	data.Links = footerLinks()
	// Cannot fail: pageData holds only strings, bools and slices.
	encoded, _ := json.Marshal(data)
	body, err := renderShell(encoded)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Headers:    map[string]string{"Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"},
			Body:       "The sign-in page is not available.",
		}
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "text/html; charset=utf-8",
			// One person's login in progress: nothing about it is cacheable.
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
			// The frame-ancestors of older browsers: a sign-in page is exactly what clickjacking targets.
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "no-referrer",
			"Content-Security-Policy": hostedui.CSP(),
		},
		Body: body,
	}
}

// errorPage renders the same app told it cannot proceed, and never echoes upstream detail; a non-empty restartURL adds "Start again", for a sign-in the person can resume from the application's own request.
func errorPage(request events.APIGatewayProxyRequest, status int, code, description, restartURL string) events.APIGatewayProxyResponse {
	resp := renderPage(request, pageData{Error: &errorData{Code: code, Message: description}, RestartURL: restartURL})
	// A page that could not be rendered keeps its 500, which is the truer answer.
	if resp.StatusCode == http.StatusOK {
		resp.StatusCode = status
	}
	return resp
}

// unusableAccount is the one 403 answer, page or JSON, for a person a provider vouched for whom no account of ours can take; one sentence for every reason, so it names no account.
var unusableAccount = errorData{Code: oidc.OAuthErrAccessDenied, Message: "This account cannot be signed in. Please contact support."}

// isUnusableAccount is a login with no verified email or phone, with contacts held by two accounts, or with an upstream id another account holds.
func isUnusableAccount(err error) bool {
	return errors.Is(err, idp.ErrNoVerifiedContact) || errors.Is(err, auth.ErrContactsOnDifferentUsers) || errors.Is(err, auth.ErrUpstreamUserIDUnusable)
}

func handleAuthorizeRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.Path == pathConsent && request.HTTPMethod == http.MethodPost {
		return handleConsentAnswer(ctx, request), nil
	}
	if request.Path == pathPassword && request.HTTPMethod == http.MethodPost {
		return handlePasswordSignIn(ctx, request), nil
	}
	if request.HTTPMethod != http.MethodGet {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	switch request.Path {
	case pathRestart:
		return handleRestart(ctx, request), nil
	case pathAuthorize:
		return handleAuthorize(ctx, request)
	case pathLogin:
		return handleLogin(ctx, request), nil
	case pathConsent:
		return handleConsentPage(ctx, request), nil
	case pathSignOut:
		return handleSignOutPage(ctx, request), nil
	case pathFederationStart:
		return handleFederationStart(ctx, request)
	case pathFederationCallback:
		return handleFederationCallback(ctx, request)
	default:
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "Not found."), nil
	}
}

func main() {
	if err := hostedui.Err(); err != nil {
		rlog.Error(context.Background()).Err(err).Msg("sign-in pages unavailable; every page route answers 500")
	}
	lambda.Start(handleAuthorizeRequest)
}
