// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
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
	// Resource is the one RFC 8707 resource this access token is for; omitted, the sign-in or the client's default decides.
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
	var client *clients.ClientResponse
	if req.ClientID != "" {
		var resp events.APIGatewayProxyResponse
		var authed bool
		if client, resp, authed = authClient(ctx, req.ClientID, clientSecret); !authed {
			return resp, nil
		}
	}

	// One token is for one API (RFC 8707 s2.2); a client that needs two asks twice. Only a form body can repeat the parameter, so a JSON body's single value is taken as decoded.
	resourceValues := []string{req.Resource}
	if rmngrequest.IsFormRequest(request) {
		resourceValues = rmngrequest.FormValues(request, "resource")
	}
	resource, err := auth.RequestedResourceForTokenRequest(resourceValues)
	if resp, refused := targetRefusal(err); refused {
		return resp, nil
	}
	req.Resource = resource

	switch req.GrantType {
	case oidc.GrantRefreshToken:
		return handleRefreshTokenGrant(ctx, req, client)
	case oidc.GrantAuthorizationCode:
		return handleAuthorizationCodeGrant(ctx, req, client)
	case oidc.GrantClientCredentials:
		return handleClientCredentialsGrant(ctx, req, client)
	default:
		// token-exchange is a separate later slice.
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrUnsupportedGrantType, "Unsupported grant_type."), nil
	}
}

// authClient authenticates the client via the registry and returns it; on failure it returns the OAuth error
// response (500 for an internal error, else 401 invalid_client) with authed=false.
func authClient(ctx context.Context, clientID, clientSecret string) (*clients.ClientResponse, events.APIGatewayProxyResponse, bool) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, errCode, internal := clients.NewService(rmngCtx).AuthenticateForOAuth(clientID, clientSecret)
	if errCode == "" {
		return client, events.APIGatewayProxyResponse{}, true
	}
	status := http.StatusUnauthorized
	if internal {
		status = http.StatusInternalServerError
	}
	return nil, oidc.OAuthErrorResp(status, errCode, "client authentication failed."), false
}

// handleClientCredentialsGrant issues a token to the client itself (RFC 6749 s4.4).
//
// The caller has already been authenticated above -- but only if it presented a client_id.
// This grant IS client authentication, so an anonymous request must be refused here rather
// than falling through as one with no client.
func handleClientCredentialsGrant(ctx context.Context, req TokenRequest, client *clients.ClientResponse) (events.APIGatewayProxyResponse, error) {
	if client == nil {
		return oidc.OAuthErrorResp(http.StatusUnauthorized, oidc.OAuthErrInvalidClient, "client authentication failed."), nil
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

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	tokens, err := svc.MintClientCredentialsToken(ctx, client, scope, req.Resource)
	if resp, refused := targetRefusal(err); refused {
		return resp, nil
	}
	if err != nil {
		rlog.Error(ctx).Err(err).Str("client_id", req.ClientID).Msg("Failed to mint client_credentials token")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}
	return oidc.OAuthTokenResp(tokens), nil
}

// targetRefusal answers an RFC 8707 refusal with invalid_target and a description that tells the client what to fix.
func targetRefusal(err error) (events.APIGatewayProxyResponse, bool) {
	description, ok := auth.TargetDescription(err)
	if !ok {
		return events.APIGatewayProxyResponse{}, false
	}
	return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidTarget, description), true
}

func handleAuthorizationCodeGrant(ctx context.Context, req TokenRequest, client *clients.ClientResponse) (events.APIGatewayProxyResponse, error) {
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

	tokens, err := svc.ExchangeAuthCode(ctx, client, req.Code, req.CodeVerifier, req.RedirectURI, req.Resource)
	if resp, refused := targetRefusal(err); refused {
		return resp, nil
	}
	if err != nil {
		// Unknown / expired / consumed code, client-or-redirect mismatch, and PKCE failure all collapse to invalid_grant (no oracle).
		rlog.Error(ctx).Err(err).Msg("Authorization code exchange rejected")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidGrant, "The authorization code is invalid, expired, or already used."), nil
	}

	return oidc.OAuthTokenResp(tokens), nil
}

func handleRefreshTokenGrant(ctx context.Context, req TokenRequest, client *clients.ClientResponse) (events.APIGatewayProxyResponse, error) {
	if req.ClientID == "" || req.RefreshToken == "" {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "client_id and refresh_token are required."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	tokens, err := svc.RefreshToken(ctx, client, req.RefreshToken, req.Resource)
	// invalid_target, not invalid_grant: the token is still good, so retrying it is not the remedy.
	if resp, refused := targetRefusal(err); refused {
		rlog.Warn(ctx).Err(err).Str("client_id", req.ClientID).Msg("Refresh refused on its resource")
		return resp, nil
	}
	if err != nil {
		// Unknown / expired / spent / revoked all collapse to invalid_grant (no reuse oracle).
		rlog.Error(ctx).Err(err).Msg("Refresh token rejected")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidGrant, "The refresh token is invalid, expired, or revoked."), nil
	}

	return oidc.OAuthTokenResp(tokens), nil
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
