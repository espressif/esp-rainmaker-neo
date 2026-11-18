// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// The federation broker endpoints run the upstream leg only; the client's downstream leg is
// untouched. Spec: espuser/docs/en/specs/federation.md.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

const (
	pathFederationStart    = "/user/login/federation/start"
	pathFederationCallback = "/oauth2/federation/callback"
)

func newRegistry(ctx context.Context) (*idp.Registry, error) {
	secret, err := ssmutil.GetParameterWithCaching(ctx, os.Getenv("ESPUSER_REFRESH_SECRET_PARAM"), true)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to load state hmac secret")
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	return idp.NewRegistry(rmngCtx, os.Getenv("ESPUSER_FEDERATION_CALLBACK_URL"), idp.StateHMACKey([]byte(secret))), nil
}

func handleFederationStart(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	flowID := rmngrequest.Cookie(request, auth.FlowCookieName)
	providerName := request.QueryStringParameters["provider"]
	if flowID == "" || providerName == "" || request.QueryStringParameters["flow_id"] == "" {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing flow or provider.", ""), nil
	}
	// The link names the sign-in its page was drawn for; a newer sign-in in another tab took the cookie, and following it would finish that tab's app.
	if request.QueryStringParameters["flow_id"] != flowID {
		return errorPage(request, http.StatusBadRequest, oidc.RMErrFlowReplaced, "Another sign-in was started in this browser.", ""), nil
	}

	registry, err := newRegistry(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation start: registry")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation start: auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	// allowed_providers is enforced HERE, not on the login page: the chooser only decides which buttons are drawn, and this URL can be typed. An unreadable client refuses rather than admits.
	flow, err := svc.FlowAllowingProvider(ctx, flowID, providerName)
	switch {
	case errors.Is(err, auth.ErrProviderNotAllowed):
		rlog.Warn(ctx).Str("provider", providerName).Msg("federation start: provider not allowed for this client")
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider.", ""), nil
	case err != nil:
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again.", ""), nil
	}

	provider, err := registry.Provider(providerName)
	if err != nil || provider == nil {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider.", ""), nil
	}

	leg, err := registry.NewUpstreamLeg(flowID)
	if err != nil {
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	// Forward prompt/max_age upstream; otherwise the provider re-authenticates from its own cookie.
	leg.Prompt, leg.MaxAge = flow.Prompt, flow.MaxAge
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if err := flowsDB.SetUpstreamLeg(flowID, providerName, leg.State, leg.Nonce, leg.PKCEVerifier); err != nil {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again.", ""), nil
	}

	url, err := provider.AuthorizeRedirectURL(ctx, leg)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation start: authorize url")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	return redirect(url), nil
}

// upstreamCancels are what a provider answers when the person backs out (Google's access_denied, Apple's user_cancelled_authorize): they go back to our page when the client has another way in there, else to the client with access_denied.
var upstreamCancels = map[string]bool{oidc.OAuthErrAccessDenied: true, "user_cancelled_authorize": true}

func handleFederationCallback(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := request.QueryStringParameters
	code, state, upstreamErr := q["code"], q["state"], q["error"]
	if upstreamErr == "" && (code == "" || state == "") {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing code or state.", ""), nil
	}

	registry, err := newRegistry(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: registry")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flow, refusal := callbackFlow(rmngCtx, registry, request, state)
	if upstreamErr != "" {
		return upstreamRefusal(ctx, request, registry, flow, upstreamErr), nil
	}
	if refusal != "" {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, refusal, ""), nil
	}

	provider, err := registry.Provider(flow.Provider)
	if err != nil || provider == nil {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider.", ""), nil
	}

	identity, err := provider.HandleCallback(ctx, code, idp.UpstreamLeg{
		State: flow.UpstreamState, Nonce: flow.UpstreamNonce, PKCEVerifier: flow.UpstreamPKCEVerifier,
	})
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: upstream verification failed")
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Could not verify the upstream sign-in.", ""), nil
	}

	email, phone, err := identity.VerifiedContacts()
	if err != nil {
		return errorPage(request, http.StatusForbidden, unusableAccount.Code, unusableAccount.Message, ""), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	// Persisting the claims with the user is what lets the token endpoint stamp them without ever
	// calling upstream.
	userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, &identity.Profile)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: resolve user")
		if isUnusableAccount(err) {
			return errorPage(request, http.StatusForbidden, unusableAccount.Code, unusableAccount.Message, ""), nil
		}
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", ""), nil
	}
	// The per-provider cap lives only on OIDC rows; other provider kinds fall back to the
	// global cap (see idp.OIDCProvider.SessionMaxTTL on why this is a type assertion).
	var providerMaxTTL int64
	if op, ok := provider.(*idp.OIDCProvider); ok {
		providerMaxTTL = op.SessionMaxTTL()
	}
	// The user agent is recorded HERE and nowhere else: this is the one request we know came
	// from the browser itself, mid-redirect. A later API call arrives from the product's
	// page and would describe whatever was calling, not who authenticated.
	redirectTo, setCookie, err := svc.CompleteSignIn(ctx, flow, userID, auth.FirstPartyLogin{
		Provider:              identity.Profile.Provider,
		AuthTime:              identity.AuthTime,
		AMR:                   identity.AMR,
		ACR:                   identity.ACR,
		ProviderMaxTTLSeconds: providerMaxTTL,
		Origin:                session.OriginBrowser,
		UserAgent:             utils.UserAgent(request),
		IPAddress:             request.RequestContext.Identity.SourceIP,
		PriorCookie:           rmngrequest.Cookie(request, session.CookieName),
	})
	var resp events.APIGatewayProxyResponse
	switch {
	// The flow's conditional write refuses one that expired, or was finished by another tab, since it was read.
	case errors.Is(err, auth_flows_db.ErrFlowNotFound):
		resp = errorPage(request, http.StatusBadRequest, oidc.RMErrExpiredFlow, expiredFlowMessage, routeLocation(request, pathRestart))
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("federation callback: issue code")
		resp = errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	default:
		// ConsentPath is relative to the sign-in page, and this redirect leaves from the callback.
		if redirectTo == auth.ConsentPath {
			redirectTo = routeLocation(request, pathConsent)
		}
		resp = redirect(redirectTo)
		resp.Headers["Referrer-Policy"] = "no-referrer"
	}
	if setCookie != "" {
		resp.Headers["Set-Cookie"] = setCookie
	}
	return resp, nil
}

// callbackFlow refuses a state from no flow, a flow this browser did not start (RFC 6749 s10.12: an attacker's own sign-in finished in the victim's browser), or a state spliced from another flow.
func callbackFlow(rmngCtx *rmngctx.RmngContext, registry *idp.Registry, request events.APIGatewayProxyRequest, state string) (*auth_flows_db.AuthFlow, string) {
	flowID, err := registry.FlowIDFromState(state)
	if err != nil {
		return nil, "Invalid or expired sign-in state."
	}
	if rmngrequest.Cookie(request, auth.FlowCookieName) != flowID {
		return nil, "Invalid sign-in state."
	}
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlow(flowID)
	if err != nil {
		return nil, "Login session expired. Please try again."
	}
	if flow.UpstreamState == "" || flow.UpstreamState != state {
		return nil, "Invalid sign-in state."
	}
	return flow, ""
}

// upstreamRefusal answers a provider that did not sign the person in, offering a way back only to this browser's own sign-in (flow is nil for any other).
func upstreamRefusal(ctx context.Context, request events.APIGatewayProxyRequest, registry *idp.Registry, flow *auth_flows_db.AuthFlow, upstreamErr string) events.APIGatewayProxyResponse {
	if flow == nil {
		return errorPage(request, http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Sign-in with that provider was not completed.", "")
	}
	if upstreamCancels[upstreamErr] {
		return upstreamCancel(ctx, request, registry, flow)
	}
	return errorPage(request, http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Sign-in with that provider was not completed.", routeLocation(request, pathRestart))
}

// upstreamCancel sends a person who backed out to our page when it offers another way in, else back to the client with access_denied, since /oauth2/authorize would only send them straight to the same provider again.
func upstreamCancel(ctx context.Context, request events.APIGatewayProxyRequest, registry *idp.Registry, flow *auth_flows_db.AuthFlow) events.APIGatewayProxyResponse {
	signInPage := redirect(routeLocation(request, pathLogin))
	enabled, err := registry.EnabledEntries()
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("federation cancel: provider registry unreadable")
		return signInPage
	}
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(flow.ClientID)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("federation cancel: client unreadable")
		return signInPage
	}
	if buildPageData(enabled, client.AllowsProvider, "", nil, "", "").offersAnotherWay() {
		return signInPage
	}
	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation cancel: auth service")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	errorRedirect, err := svc.EndFlowDenied(ctx, flow)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation cancel: end flow")
		return errorPage(request, http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.", "")
	}
	return redirect(errorRedirect)
}
