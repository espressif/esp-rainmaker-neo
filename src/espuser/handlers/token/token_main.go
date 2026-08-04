// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
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
	default:
		// client_credentials / token-exchange are separate later slices.
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
