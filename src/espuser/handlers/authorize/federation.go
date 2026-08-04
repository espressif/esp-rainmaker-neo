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

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
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
	if _, err := flowsDB.GetFlow(flowID); err != nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Login session expired. Please try again."), nil
	}

	provider, err := registry.Provider(providerName)
	if err != nil || provider == nil {
		return errorPage(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "Unknown or unavailable provider."), nil
	}

	leg, err := registry.NewUpstreamLeg(flowID)
	if err != nil {
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
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
	redirectTo, err := svc.CompleteAuthFlowForSubject(ctx, flowID, userID)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("federation callback: issue code")
		return errorPage(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusFound,
		Headers:    map[string]string{"Location": redirectTo, "Cache-Control": "no-store", "Referrer-Policy": "no-referrer"},
	}, nil
}
