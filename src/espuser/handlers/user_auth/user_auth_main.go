// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Command user_auth serves the native /v1/user/auth/* password APIs.
// Spec: espuser/docs/en/specs/legacy-user-auth.md.
package main

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/legacyauth"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	pathToken           = "/v1/user/auth/token"
	pathRefresh         = "/v1/user/auth/token/refresh"
	pathSignup          = "/v1/user/auth/signup"
	pathSignupVerify    = "/v1/user/auth/signup/verify"
	pathRecovery        = "/v1/user/auth/password-recovery"
	pathRecoveryConfirm = "/v1/user/auth/password-recovery/confirmation"
	pathSignout         = "/v1/user/auth/signout"
	pathPassword        = "/v1/user/auth/password"
)

// Messages name RainMaker only when the resolved provider IS RainMaker (see
// Service.IsRainMakerProvider); a bundled Cognito or any other provider gets the neutral wording.
const (
	rainmakerSignupMessage = "Code sent. Existing RainMaker users must signin or reset password."
	neutralSignupMessage   = "Code sent. Existing users must signin or reset password."

	unconfirmedAccountMessage = "Signin failed. Account not verified — reset your password."

	// One answer for every /signup/verify failure; the wording carries the sign-in hint.
	rainmakerVerifyFailedMessage = "Invalid code or RainMaker account already exists. Try signin or reset password."
	neutralVerifyFailedMessage   = "Invalid code or account already exists. Try signin or reset password."

	// Signup by someone who proved the password of an existing confirmed account: sign in, no code.
	rainmakerAccountExistsMessage = "Account already exists. RainMaker users: signin or reset password."
	neutralAccountExistsMessage   = "Account already exists. Signin or reset password."
)

func signupMessage(rainmaker bool) string {
	if rainmaker {
		return rainmakerSignupMessage
	}
	return neutralSignupMessage
}

func verifyFailedMessage(rainmaker bool) string {
	if rainmaker {
		return rainmakerVerifyFailedMessage
	}
	return neutralVerifyFailedMessage
}

func accountExistsMessage(rainmaker bool) string {
	if rainmaker {
		return rainmakerAccountExistsMessage
	}
	return neutralAccountExistsMessage
}

type tokenReq struct {
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type signupReq struct {
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Password    string `json:"password,omitempty"`
	Code        string `json:"code,omitempty"`
}

type recoveryReq struct {
	Username    string `json:"username,omitempty"`
	Code        string `json:"code,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

type signoutReq struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Global       string `json:"global,omitempty"`
}

type passwordReq struct {
	AccessToken string `json:"access_token,omitempty"`
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

func newService(ctx context.Context) (*legacyauth.Service, error) {
	return legacyauth.NewService(ctx)
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	svc, err := newService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to build legacy auth service")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	switch request.Path {
	case pathToken:
		return handleSignin(ctx, request, svc)
	case pathRefresh:
		return handleRefresh(ctx, request, svc)
	case pathSignup:
		return handleSignup(ctx, request, svc)
	case pathSignupVerify:
		return handleSignupVerify(ctx, request, svc)
	case pathRecovery:
		return handleRecovery(ctx, request, svc)
	case pathRecoveryConfirm:
		return handleRecoveryConfirm(ctx, request, svc)
	case pathSignout:
		return handleSignout(ctx, request, svc)
	case pathPassword:
		return handlePassword(ctx, request, svc)
	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
}

func handleSignin(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req tokenReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	tokens, err := svc.SigninWithPassword(ctx, req.Username, req.Password)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("legacy signin failed")
		// An unconfirmed account is named explicitly: the caller proved the password, so nothing
		// is disclosed that they did not already know, and a generic refusal would leave them
		// with no way to recover the account.
		if errors.Is(err, legacyauth.ErrUserNotConfirmed) {
			return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus(unconfirmedAccountMessage)), nil
		}
		// Uniform 401 so the response is no oracle; operators get the real cause from the log.
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Authentication failed")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, withBearer(tokens)), nil
}

func handleRefresh(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req tokenReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	tokens, err := svc.RefreshLegacy(ctx, req.RefreshToken)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("legacy refresh failed")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid refresh token")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, withBearer(tokens)), nil
}

func handleSignup(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req signupReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if (req.Email == "") == (req.PhoneNumber == "") {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Provide exactly one of email or phone_number")), nil
	}
	if req.Password == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Password is required")), nil
	}
	if err := svc.Signup(ctx, req.Email, req.PhoneNumber, req.Password); err != nil {
		// The caller proved the password of an existing confirmed account: nothing was created, so
		// this is a 409, and they are told to sign in rather than wait for a code that will not
		// come. Reachable only with the credential, so the distinct status leaks nothing.
		if errors.Is(err, legacyauth.ErrAccountAlreadyUsable) {
			return utils.APIGwRespJSON(http.StatusConflict, utils.NewAPIStatus(accountExistsMessage(svc.IsRainMakerProvider()))), nil
		}
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Failed to create user account")), nil
	}
	return utils.APIGwRespJSON(http.StatusCreated, map[string]any{
		"requires_verification": true,
		"message":               signupMessage(svc.IsRainMakerProvider()),
	}), nil
}

func handleSignupVerify(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req signupReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	username := req.Email
	if username == "" {
		username = req.PhoneNumber
	}
	if err := svc.VerifySignup(ctx, username, req.Code); err != nil {
		rlog.Error(ctx).Err(err).Msg("legacy signup verification failed")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(verifyFailedMessage(svc.IsRainMakerProvider()))), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Verified successfully. You can now login.")), nil
}

func handleRecovery(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req recoveryReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if err := svc.ForgotPassword(ctx, req.Username); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Failed to initiate password reset")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("If your account exists, you will receive a code")), nil
}

func handleRecoveryConfirm(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req recoveryReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if err := svc.ConfirmForgotPassword(ctx, req.Username, req.Code, req.NewPassword); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid verification code")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Password reset successful")), nil
}

func handleSignout(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req signoutReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if req.Global == "true" {
		return utils.APIGwRespJSON(http.StatusBadRequest,
			utils.NewAPIStatus("All-device signout is not supported; sign out each session with its refresh token")), nil
	}
	// Reported, not ignored: returning success for a signout that revoked nothing is worse than a 400.
	if req.RefreshToken == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Refresh token is required")), nil
	}
	if err := svc.Signout(ctx, req.RefreshToken); err != nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus(err.Error())), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Successfully signed out")), nil
}

func handlePassword(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req passwordReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if err := svc.ChangePassword(ctx, req.AccessToken, req.OldPassword, req.NewPassword); err != nil {
		rlog.Error(ctx).Err(err).Msg("legacy password change failed")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Password change failed")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Password changed successfully")), nil
}

func withBearer(t *auth.UserTokens) *auth.UserTokens {
	t.TokenType = oidc.TokenTypeBearer
	if t.ExpiresIn == 0 {
		t.ExpiresIn = 3600
	}
	return t
}

func main() { lambda.Start(handler) }
