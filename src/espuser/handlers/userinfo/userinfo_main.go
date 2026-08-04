// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// OIDC UserInfo endpoint (OIDC Core §5.3); spec: espuser/docs/en/specs/auth-flows.md.
const pathUserinfo = "/oauth2/userinfo"

// unauthorized is the RFC 6750 §3 bearer challenge; every token problem collapses to invalid_token (no oracle).
func unauthorized() events.APIGatewayProxyResponse {
	resp := oidc.OAuthErrorResp(http.StatusUnauthorized, "invalid_token", "The access token is missing, expired, or invalid.")
	resp.Headers["WWW-Authenticate"] = `Bearer error="invalid_token"`
	return resp
}

func handleUserinfo(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Token may ride the Authorization header or, on a form POST, the access_token field (RFC 6750 §2).
	accessToken := rmngrequest.ExtractAuthToken(request.Headers)
	if accessToken == "" {
		accessToken = accessTokenFromForm(request)
	}
	if accessToken == "" {
		return unauthorized(), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	claims, err := svc.ParseUserInfoFromToken(ctx, accessToken)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Userinfo token rejected")
		return unauthorized(), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, claims), nil
}

// accessTokenFromForm reads access_token from a form-encoded POST body (RFC 6750 §2.2).
func accessTokenFromForm(request events.APIGatewayProxyRequest) string {
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := rmngrequest.GetRequest(request, &body); err != nil {
		return ""
	}
	return body.AccessToken
}

func handleUserinfoRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// OIDC §5.3.1: UserInfo must accept both GET and POST.
	if request.HTTPMethod != http.MethodGet && request.HTTPMethod != http.MethodPost {
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
	if request.Path != pathUserinfo {
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
	return handleUserinfo(ctx, request)
}

func main() {
	lambda.Start(handleUserinfoRequest)
}
