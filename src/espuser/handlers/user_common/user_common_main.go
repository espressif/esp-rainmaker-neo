// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type GetUserResponse struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

// handleGetUser serves GET /v1/users/{userId}. The route is unauthenticated at the gateway; the OIDC access token is the credential and is verified in-handler.
func handleGetUser(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	userIDParam := request.PathParameters["userId"]
	if userIDParam == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing userId")), nil
	}

	rctx := user.NewContextWithAnyAPIToken(ctx, request)
	claims := user.ClaimsFrom(rctx)
	if claims.Subject == "" {
		return oidc.OAuthUnauthorizedResp(), nil
	}
	// Any of our APIs' tokens is accepted here, so a third party's token is refused by its client until scopes gate the account's endpoints.
	if err := clients.NewService(rctx).RequireFirstParty(claims.ClientID); err != nil {
		if !errors.Is(err, clients.ErrNotFirstParty) {
			rlog.Error(ctx).Err(err).Str("client_id", claims.ClientID).Msg("Failed to read the client registration")
			return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error."), nil
		}
		return oidc.OAuthErrorResp(http.StatusForbidden, oidc.OAuthErrAccessDenied, "This client may not read the account."), nil
	}

	if userIDParam != "me" && userIDParam != claims.Subject {
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	details, dbErr := user_details_db.NewUserDetailsDB(rctx).GetUserDetails()
	if dbErr != nil {
		rlog.Error(ctx).Err(dbErr).Str("user_id", claims.Subject).Msg("Failed to load user_details")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to load user profile")), nil
	}

	resp := GetUserResponse{
		UserID:      details.UserID,
		Email:       details.Email,
		PhoneNumber: details.PhoneNumber,
	}
	return utils.APIGwRespJSON(http.StatusOK, resp), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if request.HTTPMethod == http.MethodGet && strings.HasPrefix(request.Path, "/v1/users/") {
		return handleGetUser(ctx, request)
	}
	return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
}

func main() {
	lambda.Start(handleRequest)
}
