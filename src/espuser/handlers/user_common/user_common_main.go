// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

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

	token := rmngrequest.ExtractAuthToken(request.Headers)
	if token == "" {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	svc, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to build auth service")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	userInfo, err := svc.ParseUserInfoFromToken(ctx, token)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to verify access token")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	if userInfo.Sub == "" {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	if userIDParam != "me" && userIDParam != userInfo.Sub {
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	rctx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userInfo.Sub))
	details, dbErr := user_details_db.NewUserDetailsDB(rctx).GetUserDetails()
	if dbErr != nil {
		rlog.Error(ctx).Err(dbErr).Str("user_id", userInfo.Sub).Msg("Failed to load user_details")
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
