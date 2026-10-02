// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// GET /oauth2/authorize (validate -> LOGIN flow record -> 302 to the login page with a flow_id cookie) and the served login UI. Spec: espuser/docs/en/specs/authorize-code-flow.md.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	pathAuthorize = "/oauth2/authorize"
	pathLogin     = "/oauth2/login"
	// providersParam carries the login page's button list from /authorize.
	providersParam = "providers"

	// __Host- for the same reason the session cookie carries it, and against the same attack:
	// the prefix is honoured only for a Secure, Path=/, Domain-less cookie, so a page on a
	// sibling host cannot plant one. Without it, an attacker starts a flow of their own,
	// writes its id into the victim's browser, and the victim's login completes into the
	// ATTACKER's flow -- the authorization code lands at the attacker's registered
	// redirect_uri. That is login CSRF / code injection, and the prefix is what closes it.
	flowCookieName = "__Host-esp_flow_id"
)

func handleAuthorize(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := request.QueryStringParameters
	if code := oidc.ValidateResponseType(q["response_type"]); code != "" {
		return errorPage(http.StatusBadRequest, code, "response_type must be code."), nil
	}

	// At most one resource. Two would mean a token good at two APIs, so a single leak opens
	// both doors; a client needing two makes two requests. The single-value
	// map cannot tell one from two, so the multi-value map is the one to ask.
	if len(request.MultiValueQueryStringParameters["resource"]) > 1 {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidTarget,
			"Only one resource may be requested."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	flowID, client, err := svc.StartAuthFlow(ctx, auth.AuthorizeRequest{
		ClientID:            q["client_id"],
		RedirectURI:         q["redirect_uri"],
		Scope:               q["scope"],
		State:               q["state"],
		CodeChallenge:       q["code_challenge"],
		CodeChallengeMethod: q["code_challenge_method"],
		Resource:            q["resource"],
		Prompt:              q["prompt"],
		MaxAge:              q["max_age"],
	})
	if err != nil {
		return authorizeError(q, err), nil
	}

	// A live session of our own answers without the login page or the upstream leg (SSO).
	// Runs only after StartAuthFlow validated the request, so its error redirects are safe.
	if resp, done := sessionShortCircuit(ctx, AuthorizeSessionRequest{Request: request, Svc: svc, Query: q, FlowID: flowID, Client: client}); done {
		return resp, nil
	}

	// Location is built from the request path so the API Gateway stage prefix survives.
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers: map[string]string{
			"Location": loginRedirect(ctx, request, client),
			// HttpOnly keeps the flow id out of JS; Secure/SameSite=Lax blunt leak/CSRF.
			"Set-Cookie":    fmt.Sprintf("%s=%s; Path=/; HttpOnly; Secure; SameSite=Lax", flowCookieName, flowID),
			"Cache-Control": "no-store",
		},
	}, nil
}

// A single enabled provider needs no chooser, so skip straight to it; anything else lands on the
// login page. A registry read failure falls back there too rather than blocking login.
func loginRedirect(ctx context.Context, request events.APIGatewayProxyRequest, client *clients.ClientResponse) string {
	stage := stageFor(request)
	loginPage := loginLocation(request.Path, stage)
	registry, err := newRegistry(ctx)
	if err != nil {
		return loginPage
	}
	enabled, err := registry.EnabledEntries()
	if err != nil || client == nil {
		return loginPage
	}
	if len(enabled) != 1 {
		// The page draws these instead of re-reading the flow and client; tampering only changes the buttons, federation/start is the gate.
		return loginPage + "?" + providersParam + "=" + url.QueryEscape(strings.Join(buttonProviders(enabled, client.AllowsProvider), ","))
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
	case errors.Is(err, auth.ErrInvalidTarget):
		// Redirects, like scope: by this point redirect_uri has been validated against the
		// registry, so sending the error back to the client is safe and is what RFC 6749
		// s4.1.2.1 asks for.
		return oidc.OAuthErrorRedirect(q["redirect_uri"], oidc.OAuthErrInvalidTarget, q["state"])
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

// providerViews is one entry per enabled federated provider that allows admits, in the order an
// operator gave them. OTP providers get none: the one-time-code form is the page's other column,
// not a button. A registry failure yields no buttons rather than blocking the passwordless path.
//
// The allowed_providers filter here is presentation only -- it stops us drawing a button
// that would be refused on click. The gate is in handleFederationStart, which is reachable
// without this page at all, so this function fails OPEN (unreadable client => draw them all)
// while that one fails closed.
func providerViews(ctx context.Context, allows func(string) bool) []providerView {
	registry, err := newRegistry(ctx)
	if err != nil {
		return nil
	}
	enabled, err := registry.EnabledEntries()
	if err != nil {
		return nil
	}
	// By provider name, so the chooser has a stable order rather than whatever the table
	// happened to return.
	sort.SliceStable(enabled, func(i, j int) bool {
		return enabled[i].ProviderName < enabled[j].ProviderName
	})
	var views []providerView
	for _, p := range enabled {
		if p.Type == "otp" {
			continue
		}
		if !allows(p.ProviderName) {
			continue
		}
		label := p.DisplayName
		if label == "" {
			label = p.ProviderName
		}
		views = append(views, providerView{
			Label: label,
			// Relative to /oauth2/login, so the API Gateway stage prefix carries over untouched.
			Href: "federation/start?provider=" + url.QueryEscape(p.ProviderName),
			// The row's own SVG when it carries one; otherwise the page draws its default.
			LogoSVG: template.HTML(p.Logo),
		})
	}
	return views
}

// buttonProviders names the enabled federated providers allows admits: the buttons the login page draws.
func buttonProviders(enabled []identity_providers_db.ProviderEntry, allows func(string) bool) []string {
	var names []string
	for _, p := range enabled {
		if p.Type != "otp" && allows(p.ProviderName) {
			names = append(names, p.ProviderName)
		}
	}
	return names
}

// loginProviderFilter prefers the providers= list /authorize computed; without it (a stale link or bookmark) it falls back to reading the flow's client.
func loginProviderFilter(ctx context.Context, request events.APIGatewayProxyRequest, flowID string) func(string) bool {
	listed, ok := request.QueryStringParameters[providersParam]
	if !ok {
		return clientProviderFilter(ctx, clientIDForFlow(ctx, flowID))
	}
	names := map[string]bool{}
	for _, n := range strings.Split(listed, ",") {
		names[n] = true
	}
	return func(name string) bool { return names[name] }
}

// clientIDForFlow reads the client this login flow belongs to. "" when the flow is missing
// or unreadable, which the caller treats as "no restriction known".
func clientIDForFlow(ctx context.Context, flowID string) string {
	if flowID == "" {
		return ""
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlow(flowID)
	if err != nil || flow == nil {
		return ""
	}
	return flow.ClientID
}

// clientProviderFilter returns the predicate the chooser draws by. Unknown client => admit
// everything; see providerViews on why this direction is the safe one here.
func clientProviderFilter(ctx context.Context, clientID string) func(string) bool {
	if clientID == "" {
		return func(string) bool { return true }
	}
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(clientID)
	if err != nil || client == nil {
		return func(string) bool { return true }
	}
	return client.AllowsProvider
}

func handleLogin(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// The flow id is HttpOnly, so JS can't read it via document.cookie; inject it into the page server-side instead.
	flowID := rmngrequest.Cookie(request, flowCookieName)
	// Per-response nonce ties the CSP to our one inline <script>: an injected script (no nonce) is
	// refused by the browser, so a reflected/DOM XSS can't execute even if one were introduced.
	nonce, err := newCSPNonce()
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to generate CSP nonce")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	body, err := renderLoginPage(providerViews(ctx, loginProviderFilter(ctx, request, flowID)), nonce, flowID)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to render login page")
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
		Body: body,
	}, nil
}

// newCSPNonce returns a fresh base64 nonce for the login page's inline-script CSP.
func newCSPNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// URL alphabet, unpadded: the value appears both in a header and in an HTML attribute,
	// and the standard alphabet's "+" is escaped to &#43; by the templating layer. A browser
	// decodes that entity before comparing, so it would work -- but a security primitive
	// should not depend on entity decoding to match. A-Za-z0-9-_ needs no escaping anywhere.
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// errorPage renders a non-leaking HTML error (never echoes upstream detail).
func errorPage(status int, code, description string) events.APIGatewayProxyResponse {
	body, err := renderErrorPage(code, description)
	if err != nil {
		// The error page failing to render must not become a second, worse error: fall back
		// to text rather than recursing into this function.
		body = "We couldn't sign you in. " + code
	}
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
		Body: body,
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
