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

// RFC 7009 revoke endpoint: revokes only the one presented refresh token (D13); an access token is a no-op. Spec: espuser/docs/en/specs/auth-flows.md.
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
	if errCode, internal := clients.NewService(rmngCtx).AuthenticateForOAuth(clientID, clientSecret); errCode != "" {
		status := http.StatusUnauthorized
		if internal {
			status = http.StatusInternalServerError
		}
		return oidc.OAuthErrorResp(status, errCode, "client authentication failed."), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	// Uniform 200 (RFC 7009 §2.2): unknown/malformed tokens are swallowed so the endpoint is no oracle.
	_ = svc.RevokeRefreshToken(ctx, req.Token)

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
