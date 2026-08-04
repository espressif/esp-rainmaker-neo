// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// GET /oauth2/authorize (validate -> LOGIN flow record -> 302 to the login page with a flow_id cookie) and the served login UI. Spec: espuser/docs/en/specs/authorize-code-flow.md.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	pathAuthorize = "/oauth2/authorize"
	pathLogin     = "/oauth2/login"

	flowCookieName = "esp_flow_id"
)

func handleAuthorize(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := request.QueryStringParameters
	if code := oidc.ValidateResponseType(q["response_type"]); code != "" {
		return errorPage(http.StatusBadRequest, code, "response_type must be code."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	flowID, err := svc.StartAuthFlow(ctx, auth.AuthorizeRequest{
		ClientID:            q["client_id"],
		RedirectURI:         q["redirect_uri"],
		Scope:               q["scope"],
		State:               q["state"],
		CodeChallenge:       q["code_challenge"],
		CodeChallengeMethod: q["code_challenge_method"],
	})
	if err != nil {
		return authorizeError(q, err), nil
	}

	// Location is built from the request path so the API Gateway stage prefix survives.
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers: map[string]string{
			"Location": loginRedirect(ctx, request),
			// HttpOnly keeps the flow id out of JS; Secure/SameSite=Lax blunt leak/CSRF.
			"Set-Cookie":    fmt.Sprintf("%s=%s; Path=/; HttpOnly; Secure; SameSite=Lax", flowCookieName, flowID),
			"Cache-Control": "no-store",
		},
	}, nil
}

// A single enabled provider needs no chooser, so skip straight to it; anything else lands on the
// login page. A registry read failure falls back there too rather than blocking login.
func loginRedirect(ctx context.Context, request events.APIGatewayProxyRequest) string {
	stage := stageFor(request)
	loginPage := loginLocation(request.Path, stage)
	registry, err := newRegistry(ctx)
	if err != nil {
		return loginPage
	}
	enabled, err := registry.EnabledEntries()
	if err != nil || len(enabled) != 1 {
		return loginPage
	}
	p := enabled[0]
	if p.Type == "otp" && p.AuthorizeURL != "" {
		return withStage(p.AuthorizeURL, stage)
	}
	base := strings.TrimSuffix(loginLocation(request.Path, stage), "login")
	return base + "federation/start?provider=" + url.QueryEscape(p.ProviderName)
}

// stageFor returns the stage segment Location headers must re-prepend. On the default
// <api-id>.execute-api host the browser-facing URL carries /<stage>, which API Gateway
// strips from request.Path. Through a custom domain the base-path mapping pins the
// stage server-side and the public URL has no /<stage> prefix, so re-prepending it
// there would redirect to a path that does not exist (e.g. /prod/oauth2/login).
func stageFor(request events.APIGatewayProxyRequest) string {
	if strings.Contains(request.RequestContext.DomainName, ".execute-api.") {
		return request.RequestContext.Stage
	}
	return ""
}

// request.Path omits the API Gateway stage, which a Location header needs.
func withStage(path, stage string) string {
	if stage == "" {
		return path
	}
	return "/" + stage + path
}

// authorizeError picks the surface: client/redirect/PKCE render the error page (can't redirect to an unvalidated URI); scope redirects (redirect_uri already validated).
func authorizeError(q map[string]string, err error) events.APIGatewayProxyResponse {
	switch {
	case errors.Is(err, auth.ErrInvalidClient):
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidClient, "Unknown client.")
	case errors.Is(err, auth.ErrInvalidRedirect):
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "redirect_uri is not registered for this client.")
	case errors.Is(err, auth.ErrInvalidRequest):
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing or invalid request parameters (PKCE S256 is required).")
	case errors.Is(err, auth.ErrInvalidScope):
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrInvalidScope, q["state"])
	default:
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
}

// loginLocation swaps the trailing "authorize" for "login" and re-prepends the stage (API Gateway strips it from request.Path) so the browser keeps the /<stage> prefix.
func loginLocation(requestPath, stage string) string {
	loginPath := strings.TrimSuffix(requestPath, "authorize") + "login"
	if stage == "" {
		return loginPath
	}
	return "/" + stage + loginPath
}

// providerButtons renders one button per enabled federated provider, so a deployment with more
// than one upstream lets the user pick. OTP providers get no button: their form is the page. A
// registry failure yields no buttons rather than blocking the passwordless path.
func providerButtons(ctx context.Context) string {
	registry, err := newRegistry(ctx)
	if err != nil {
		return ""
	}
	enabled, err := registry.EnabledEntries()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range enabled {
		if p.Type == "otp" {
			continue
		}
		label := p.DisplayName
		if label == "" {
			label = p.ProviderName
		}
		// Relative to /oauth2/login, so the API Gateway stage prefix carries over untouched.
		href := "federation/start?provider=" + url.QueryEscape(p.ProviderName)
		b.WriteString(fmt.Sprintf(providerChooserHTML,
			html.EscapeString(href), providerLogoDataURI, html.EscapeString(label)))
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "  <div class=\"sep\">or</div>\n"
}

func handleLogin(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// The flow id is HttpOnly, so JS can't read it via document.cookie; inject it into the page server-side instead.
	flowID := rmngrequest.Cookie(request, flowCookieName)
	// json.Marshal yields a valid JS string literal, HTML-escaping <, >, & (SetEscapeHTML default) so it can't break out of the <script>.
	flowIDLit, err := json.Marshal(flowID)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to encode flow id for login page")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	// Per-response nonce ties the CSP to our one inline <script>: an injected script (no nonce) is
	// refused by the browser, so a reflected/DOM XSS can't execute even if one were introduced.
	nonce, err := newCSPNonce()
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to generate CSP nonce")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":           "text/html; charset=utf-8",
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
			// img-src data: carries the provider logos, which are inlined rather than fetched.
			"Content-Security-Policy": "default-src 'none'; script-src 'nonce-" + nonce + "'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
		},
		Body: fmt.Sprintf(loginPageHTML, providerButtons(ctx), nonce, flowIDLit),
	}, nil
}

// newCSPNonce returns a fresh base64 nonce for the login page's inline-script CSP.
func newCSPNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// errorPage renders a branded-neutral, non-leaking HTML error (never echoes upstream detail).
func errorPage(status int, code, description string) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":            "text/html; charset=utf-8",
			"Cache-Control":           "no-store",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "no-referrer",
			"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'",
		},
		Body: fmt.Sprintf(errorPageHTML, code, description),
	}
}

func handleAuthorizeRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodGet {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	switch request.Path {
	case pathAuthorize:
		return handleAuthorize(ctx, request)
	case pathLogin:
		return handleLogin(ctx, request)
	case pathFederationStart:
		return handleFederationStart(ctx, request)
	case pathFederationCallback:
		return handleFederationCallback(ctx, request)
	default:
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "Not found."), nil
	}
}

func main() {
	lambda.Start(handleAuthorizeRequest)
}
