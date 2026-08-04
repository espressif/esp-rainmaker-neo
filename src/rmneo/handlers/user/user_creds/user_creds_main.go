// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Command user_creds backs POST /v1/user/credentials: it verifies the presented bearer
// access token in-handler (no gateway authorizer) and exchanges it for Cognito Identity
// Pool credentials. Two token families are accepted, distinguished by the `iss` claim:
//   - ESP User OIDC access tokens (end users): RS256 verified against ESPUSER_ISSUER's JWKS.
//   - Cognito access/id tokens (admins): verified against the admin pool JWKS.
//
// The identity pool's role mapping (authenticated -> DeviceUsersRole, admin super_admin
// rule -> AdminDeviceUsersRole) runs downstream at GetCredentialsForIdentity.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/cognitoidputil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// UserCredsRequest carries the ID token in the body. The Authorization header holds
// the access token, which this handler verifies itself (the route carries no gateway
// authorizer — see stack.py), while the Identity Pool exchange behind this endpoint
// accepts an OIDC ID token and nothing else — so both tokens have to arrive, by
// different routes.
type UserCredsRequest struct {
	IDToken string `json:"id_token" validate:"required"`
}

type UserCredsResponse struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	Expiration      int64  `json:"expiration"`
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	identityPoolID := os.Getenv("IDENTITY_POOL_ID")
	if identityPoolID == "" {
		rlog.Error(ctx).Msg("IDENTITY_POOL_ID environment variable is not set")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	// The endpoint is an authorization action, so the access token arrives in the header.
	accessToken := rmngrequest.ExtractAuthToken(request.Headers)
	if accessToken == "" {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	var req UserCredsRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract request struct")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("id_token is required in the request body")), nil
	}

	if err := verifyPair(ctx, accessToken, req.IDToken); err != nil {
		rlog.Warn(ctx).Err(err).Msg("credentials request denied")
		// An unreadable or untrusted issuer means the caller is unauthenticated; a verified
		// token whose pair does not match is authenticated but forbidden this exchange.
		if errors.Is(err, auth.ErrUntrustedIssuer) || jwtutil.IsUnreadableToken(err) {
			return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
		}
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("id_token does not match the authenticated session")), nil
	}

	identityPoolService := cognitoidputil.NewCognitoIdentityPoolService(identityPoolID)
	credsResp, err := identityPoolService.GetUserCredentials(ctx, req.IDToken)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to get user credentials")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to get credentials")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, UserCredsResponse{
		AccessKeyID:     credsResp.AccessKeyID,
		SecretAccessKey: credsResp.SecretAccessKey,
		SessionToken:    credsResp.SessionToken,
		Expiration:      credsResp.Expiration,
	}), nil
}

func verifyPair(ctx context.Context, accessToken, idToken string) error {
	authService, err := auth.NewAuthServiceFactory().CreateAuthServiceForToken(ctx, accessToken)
	if err != nil {
		return err
	}
	return authService.VerifyTokenPair(ctx, accessToken, idToken)
}

func main() {
	lambda.Start(handleRequest)
}
