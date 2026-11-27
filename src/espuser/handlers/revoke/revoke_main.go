// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// RFC 7009 revoke endpoint: ends the presented refresh token's whole family, for the client it was issued to; an access token is a no-op. Spec: espuser/docs/en/specs/auth-flows.md.
const pathRevoke = "/oauth2/revoke"

type RevokeRequest struct {
	Token         string `json:"token" validate:"required"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
}

func handleRevoke(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req RevokeRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract revoke request")
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "token is required."), nil
	}

	// Client identification/auth (RFC 7009 §2.1/§5 → RFC 6749 §3.2.1): a confidential client presents
	// its secret via HTTP Basic; a public client identifies with client_id (Basic username or the body
	// parameter). Basic wins when present. AuthenticateClient enforces the per-type rules; every failure
	// collapses to invalid_client so the endpoint is no oracle.
	clientID, clientSecret := req.ClientID, ""
	if basicID, secret, ok := rmngrequest.BasicClientCreds(request); ok {
		clientID, clientSecret = basicID, secret
	}
	if clientID == "" {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidRequest, "client_id is required."), nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	client, errCode, internal := clients.NewService(rmngCtx).AuthenticateForOAuth(clientID, clientSecret)
	if errCode != "" {
		if internal {
			return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
		}
		return oidc.OAuthErrorResp(http.StatusUnauthorized, errCode, "client authentication failed."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
	}

	// RFC 7009 s2.1 and RFC 6749 s5.2: another client's token is refused; unknown or malformed tokens still get the uniform 200 (s2.2).
	err = svc.RevokeRefreshToken(ctx, client.ClientID, req.Token)
	if errors.Is(err, refresh_tokens_db.ErrRefreshClientMismatch) {
		return oidc.OAuthErrorResp(http.StatusBadRequest, oidc.OAuthErrInvalidGrant, "The token was not issued to this client."), nil
	}
	if err != nil {
		// RFC 7009 s2.2.1: a 503 tells the client the token still exists and the revocation should be retried.
		rlog.Error(ctx).Err(err).Msg("Failed to revoke refresh token")
		return oidc.OAuthErrorResp(http.StatusServiceUnavailable, oidc.OAuthErrTemporarilyUnavailable, "The token could not be revoked; retry later."), nil
	}

	return utils.APIGwRespText(http.StatusOK, ""), nil
}

func handleRevokeRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod != http.MethodPost {
		return oidc.OAuthErrorResp(http.StatusMethodNotAllowed, oidc.OAuthErrInvalidRequest, "Method not allowed."), nil
	}
	if request.Path != pathRevoke {
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "Not found."), nil
	}
	return handleRevoke(ctx, request)
}

func main() {
	lambda.Start(handleRevokeRequest)
}
