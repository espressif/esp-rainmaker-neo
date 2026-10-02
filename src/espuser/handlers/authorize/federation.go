// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// The federation broker endpoints run the upstream leg only; the client's downstream leg is
// untouched. Spec: espuser/docs/en/specs/federation.md.
package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
)

const (
	pathFederationStart    = "/oauth2/federation/start"
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
	flowID := rmngrequest.Cookie(request, flowCookieName)
	providerName := request.QueryStringParameters["provider"]
	if flowID == "" || providerName == "" {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing flow or provider."), nil
	}

	registry, err := newRegistry(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation start: registry")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flowsDB := auth_flows_db.NewAuthFlowsDB(rmngCtx)
	flow, err := flowsDB.GetFlow(flowID)
	if err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again."), nil
	}

	provider, err := registry.Provider(providerName)
	if err != nil || provider == nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider."), nil
	}

	// allowed_providers is enforced HERE, not on the login page. The chooser only decides
	// which buttons are drawn, and this endpoint is reachable by typing a provider name into
	// the address bar -- a menu is not an access control. A client that registered
	// "corporate IdP only" must not be enterable with a personal account by anyone who edits
	// the URL. An unreadable registry refuses rather than admits: this is the gate.
	if !clientAllowsProvider(ctx, rmngCtx, flow.ClientID, providerName) {
		rlog.Warn(ctx).Str("client_id", flow.ClientID).Str("provider", providerName).
			Msg("federation start: provider not allowed for this client")
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider."), nil
	}

	leg, err := registry.NewUpstreamLeg(flowID)
	if err != nil {
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	// Forward prompt/max_age upstream; otherwise the provider re-authenticates from its own cookie.
	leg.Prompt, leg.MaxAge = flow.Prompt, flow.MaxAge
	if err := flowsDB.SetUpstreamLeg(flowID, providerName, leg.State, leg.Nonce, leg.PKCEVerifier); err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again."), nil
	}

	url, err := provider.AuthorizeRedirectURL(ctx, leg)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation start: authorize url")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers:    map[string]string{"Location": url, "Cache-Control": "no-store"},
	}, nil
}

// clientAllowsProvider reports whether the client that started this flow may use this
// identity provider. An absent list means every provider (see clients.AllowsProvider); a
// client we cannot read is refused, because failing open here would make the restriction
// disappear on exactly the day the registry is unhealthy.
func clientAllowsProvider(ctx context.Context, rmngCtx *rmngctx.RmngContext, clientID, providerName string) bool {
	if clientID == "" {
		return true
	}
	client, err := clients.NewService(rmngCtx).Get(clientID)
	if err != nil || client == nil {
		rlog.Error(ctx).Err(err).Str("client_id", clientID).Msg("federation start: client unreadable")
		return false
	}
	return client.AllowsProvider(providerName)
}

func handleFederationCallback(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	q := request.QueryStringParameters
	if upstreamErr := q["error"]; upstreamErr != "" {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Upstream sign-in was not completed."), nil
	}
	code, state := q["code"], q["state"]
	if code == "" || state == "" {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Missing code or state."), nil
	}

	registry, err := newRegistry(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: registry")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	flowID, err := registry.FlowIDFromState(state)
	if err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Invalid or expired sign-in state."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlow(flowID)
	if err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again."), nil
	}
	// A state valid for one flow must not be spliceable onto another.
	if flow.UpstreamState == "" || flow.UpstreamState != state {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Invalid sign-in state."), nil
	}

	provider, err := registry.Provider(flow.Provider)
	if err != nil || provider == nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider."), nil
	}

	identity, err := provider.HandleCallback(ctx, code, idp.UpstreamLeg{
		State: flow.UpstreamState, Nonce: flow.UpstreamNonce, PKCEVerifier: flow.UpstreamPKCEVerifier,
	})
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: upstream verification failed")
		return errorPage(http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Could not verify the upstream sign-in."), nil
	}

	email, phone, err := identity.VerifiedContacts()
	if err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrAccessDenied, "Your account has no verified email or phone."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	// Persisting the claims with the user is what lets the token endpoint stamp them without ever
	// calling upstream.
	userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, &user_details_db.UpstreamProfile{
		Provider:    identity.ProviderName,
		ExternalSub: identity.ExternalSub,
		Name:        identity.Name,
		Locale:      identity.Locale,
		Picture:     identity.Picture,
	})
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: resolve user")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	// Our own session, so single sign-on stops depending on the upstream's cookie. Any
	// failure here degrades to "no session": the login itself must never be blocked by the
	// session layer, it only loses SSO until the next login.
	sid, sessionCookie := "", ""
	sess := session.NewService(rmngCtx)
	// The per-provider cap lives only on OIDC rows; other provider kinds fall back to the
	// global cap (see idp.OIDCProvider.SessionMaxTTL on why this is a type assertion).
	var providerMaxTTL int64
	if op, ok := provider.(*idp.OIDCProvider); ok {
		providerMaxTTL = op.SessionMaxTTL()
	}
	// The user agent is recorded HERE and nowhere else: this is the one request we know came
	// from the browser itself, mid-redirect. A later API call arrives from the product's
	// page and would describe whatever was calling, not who authenticated.
	cookieValue, newSID, err := sess.Establish(session.EstablishInput{
		UserID:                userID,
		Provider:              identity.ProviderName,
		AuthTime:              identity.AuthTime,
		AMR:                   identity.AMR,
		ACR:                   identity.ACR,
		ProviderMaxTTLSeconds: providerMaxTTL,
		UserAgent:             utils.UserAgent(request),
		IPAddress:             request.RequestContext.Identity.SourceIP,
		// What this browser presented on the way in. Establish uses it to tell a
		// re-authentication from a first login and retire the cookie being replaced.
		PriorCookie: rmngrequest.Cookie(request, session.CookieName),
	})
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("federation callback: session not established")
	} else {
		sid = newSID
		sessionCookie = session.SetCookieHeader(cookieValue, sess.MaxAge())
	}

	redirectTo, err := svc.CompleteStartedAuthFlow(ctx, flowID, userID, sid, flow.RequestedScope, time.Now().Unix())
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: issue code")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	headers := map[string]string{"Location": redirectTo, "Cache-Control": "no-store", "Referrer-Policy": "no-referrer"}
	if sessionCookie != "" {
		headers["Set-Cookie"] = sessionCookie
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers:    headers,
	}, nil
}
