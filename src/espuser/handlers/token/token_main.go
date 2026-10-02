// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const pathToken = "/oauth2/token"

// TokenRequest is the form-encoded /oauth2/token body; rmngrequest maps form fields via the
// json tags. Fields are shared across grants (refresh_token and authorization_code).
type TokenRequest struct {
	GrantType    string `json:"grant_type" validate:"required"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Code         string `json:"code,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	Scope        string `json:"scope,omitempty"`
	// Resource is the RFC 8707 identifier of the API the token is for. Optional; omitted
	// leaves the audience as the client id, which is what every caller sees today.
	Resource string `json:"resource,omitempty"`
}

func handleToken(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req TokenRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract token request")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "grant_type is required."), nil
	}

	// Client authentication (RFC 6749 §3.2.1 → §2.3.1): HTTP Basic or form-body credentials
	// (client_secret_post). Google account linking sends body credentials, Alexa sends Basic —
	// both must work. A confidential client MUST present its secret; a public client presents
	// client_id with none. Basic wins over the form fields so the authenticated identity is
	// what the grant then uses.
	clientSecret := req.ClientSecret
	if basicID, secret, ok := rmngrequest.BasicClientCreds(request); ok {
		req.ClientID, clientSecret = basicID, secret
	}
	if req.ClientID != "" {
		if resp, authed := authClient(ctx, req.ClientID, clientSecret); !authed {
			return resp, nil
		}
	}

	switch req.GrantType {
	case oidc.GrantRefreshToken:
		return handleRefreshTokenGrant(ctx, req)
	case oidc.GrantAuthorizationCode:
		return handleAuthorizationCodeGrant(ctx, req)
	case oidc.GrantClientCredentials:
		return handleClientCredentialsGrant(ctx, request, req)
	default:
		// token-exchange is a separate later slice.
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrUnsupportedGrantType, "Unsupported grant_type."), nil
	}
}

// authClient authenticates the client via the registry and, on failure, returns the OAuth error
// response (500 for an internal error, else 401 invalid_client) with authed=false.
func authClient(ctx context.Context, clientID, clientSecret string) (events.APIGatewayProxyResponse, bool) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	errCode, internal := clients.NewService(rmngCtx).AuthenticateForOAuth(clientID, clientSecret)
	if errCode == "" {
		return events.APIGatewayProxyResponse{}, true
	}
	status := http.StatusUnauthorized
	if internal {
		status = http.StatusInternalServerError
	}
	return oidc.OAuthErrorResp(status, errCode, "client authentication failed."), false
}

// handleClientCredentialsGrant issues a token to the client itself (RFC 6749 s4.4).
//
// The caller has already been authenticated above -- but only if it presented a client_id.
// This grant IS client authentication, so an anonymous request must be refused here rather
// than falling through as one with no client.
func handleClientCredentialsGrant(ctx context.Context, request events.APIGatewayProxyRequest, req TokenRequest) (events.APIGatewayProxyResponse, error) {
	if req.ClientID == "" {
		return oidc.OAuthErrorResp(http.StatusUnauthorized, oidc.OAuthErrInvalidClient, "client authentication failed."), nil
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, err := clients.NewService(rmngCtx).Get(req.ClientID)
	if err != nil {
		// Authentication already succeeded, so an error here is not "unknown client".
		rlog.Error(ctx).Err(err).Str("client_id", req.ClientID).Msg("Failed to read client for client_credentials")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	// A public client authenticates by presenting nothing, which is no authentication at
	// all. unauthorized_client, not invalid_client: we know who it is, the grant is not
	// theirs to use (RFC 6749 s5.2).
	if client.ClientType != oauth_clients_db.ClientTypeConfidential {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrUnauthorizedClient,
			"The client_credentials grant requires a confidential client."), nil
	}
	if !collections.Contains(client.GrantTypes, oidc.GrantClientCredentials) {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrUnauthorizedClient,
			"This client is not registered for the client_credentials grant."), nil
	}

	// No scope requested means everything the client is registered for -- there is no user
	// whose consent could narrow it, so the registration is the only bound that exists.
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = strings.Join(client.Scopes, " ")
	} else if !client.AllowsScopes(scope) {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidScope,
			"One or more requested scopes are not registered for this client."), nil
	}

	resource, resp, ok := resolveResource(request, req, client)
	if !ok {
		return resp, nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	tokens, err := svc.MintClientCredentialsToken(ctx, req.ClientID, scope, resource)
	if err != nil {
		rlog.Error(ctx).Err(err).Str("client_id", req.ClientID).Msg("Failed to mint client_credentials token")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, tokens), nil
}

// resolveResource validates the RFC 8707 resource parameter against the client's registry
// entry. Returns the audience to stamp, or an error response.
//
// Exactly one resource is permitted. A multi-audience token is one credential that opens two
// doors, so a leak costs twice what it should; a client needing two APIs asks twice
// (src/espuser/docs/specs/resource-indicators.md).
func resolveResource(request events.APIGatewayProxyRequest, req TokenRequest, client *clients.ClientResponse) (string, events.APIGatewayProxyResponse, bool) {
	values := rmngrequest.FormValues(request, "resource")
	if len(values) > 1 {
		return "", oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidTarget,
			"Only one resource may be requested; a token is never issued for two audiences."), false
	}
	resource := req.Resource
	if len(values) == 1 {
		resource = values[0]
	}
	if resource == "" {
		return "", events.APIGatewayProxyResponse{}, true
	}
	// Absent allowed_resources means none, never any -- otherwise the parameter enforces
	// nothing for exactly the clients nobody configured.
	if !client.AllowsResource(resource) {
		return "", oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidTarget,
			"This client is not registered for the requested resource."), false
	}
	return resource, events.APIGatewayProxyResponse{}, true
}

func handleAuthorizationCodeGrant(ctx context.Context, req TokenRequest) (events.APIGatewayProxyResponse, error) {
	// code_verifier is not required here: whether one is needed depends on the code's PKCE
	// binding, which ExchangeAuthCode enforces (RFC 9700 §2.1.1 downgrade protection).
	if req.Code == "" || req.ClientID == "" || req.RedirectURI == "" {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "code, client_id, and redirect_uri are required."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	tokens, err := svc.ExchangeAuthCode(ctx, req.Code, req.CodeVerifier, req.ClientID, req.RedirectURI)
	if err != nil {
		// Unknown / expired / consumed code, client-or-redirect mismatch, and PKCE failure all collapse to invalid_grant (no oracle).
		rlog.Error(ctx).Err(err).Msg("Authorization code exchange rejected")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidGrant, "The authorization code is invalid, expired, or already used."), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, tokens), nil
}

func handleRefreshTokenGrant(ctx context.Context, req TokenRequest) (events.APIGatewayProxyResponse, error) {
	if req.ClientID == "" || req.RefreshToken == "" {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "client_id and refresh_token are required."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	tokens, err := svc.RefreshToken(ctx, req.ClientID, req.RefreshToken)
	if errors.Is(err, auth.ErrResourceNoLongerAllowed) {
		// NOT invalid_grant: the token is fine, the entitlement behind it is gone. Saying
		// invalid_grant would send a client into a retry loop against a token that will never
		// work again; invalid_target names the actual problem, and the remedy is a fresh
		// authorization without that resource.
		rlog.Warn(ctx).Str("client_id", req.ClientID).Msg("Refresh refused: resource no longer allowed")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidTarget,
			"This login was issued for a resource the client may no longer request. Start a new authorization."), nil
	}
	if err != nil {
		// Unknown / expired / spent / revoked all collapse to invalid_grant (no reuse oracle).
		rlog.Error(ctx).Err(err).Msg("Refresh token rejected")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidGrant, "The refresh token is invalid, expired, or revoked."), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, tokens), nil
}

func handleTokenRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodPost {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	if request.Path != pathToken {
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "Not found."), nil
	}
	return handleToken(ctx, request)
}

func main() {
	lambda.Start(handleTokenRequest)
}
