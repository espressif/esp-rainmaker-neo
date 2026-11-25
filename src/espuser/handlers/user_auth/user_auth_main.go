// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Command user_auth serves the native /v1/user/auth/* password APIs.
// Spec: espuser/docs/en/specs/legacy-user-auth.md.
package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/legacyauth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
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

	// One answer for every /signup/verify failure; the wording carries the sign-in hint.
	rainmakerVerifyFailedMessage = "Invalid code or RainMaker account already exists. Try signin or reset password."
	neutralVerifyFailedMessage   = "Invalid code or account already exists. Try signin or reset password."

	// Signup by someone who proved the password of an existing confirmed account: sign in, no code.
	rainmakerAccountExistsMessage = "Account already exists. RainMaker users: signin or reset password."
	neutralAccountExistsMessage   = "Account already exists. Signin or reset password."
)

// passwordRefusedMessage answers signup and recovery confirmation with a password the pool's policy refused; Cognito's own reason is its text, not ours to echo, and the sign-in page shows the rules beside the field.
const passwordRefusedMessage = "Password does not meet the password requirements"

// changeSessionsNotEndedMessage is a server failure after the password changed, so the person knows the new password works and can sign out everywhere themselves.
const changeSessionsNotEndedMessage = "Password changed, but sessions could not be signed out"

const resetSessionsNotEndedMessage = "Password reset, but sessions could not be signed out"

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
	// Resource is read by refresh only: at most one RFC 8707 resource, the API the renewed access token is for.
	Resource []string `json:"resource,omitempty"`
}

// providerReq is read before the service exists, because the pool decides what it is built against; absent means the first row by name whose password our page takes.
type providerReq struct {
	Provider string `json:"provider,omitempty"`
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
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

// accountRoutes reach a Cognito pool, so each takes an optional provider naming which; refresh and signout touch only our own tokens, and a password change picks its pool from the caller's token.
var accountRoutes = map[string]bool{pathToken: true, pathSignup: true, pathSignupVerify: true, pathRecovery: true, pathRecoveryConfirm: true}

const unknownProviderMessage = "Unknown provider."

func newService(ctx context.Context, request events.APIGatewayProxyRequest) (*legacyauth.Service, error) {
	var req providerReq
	if accountRoutes[request.Path] {
		// An unparsable body names no provider here; the per-path handler answers it with 400 "Invalid request body".
		_ = rmngrequest.ExtractRequestStruct(request, &req)
	}
	return legacyauth.NewService(ctx, req.Provider)
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// The caller's token names the pool, so the service is built from it rather than from the body alone.
	if request.Path == pathPassword {
		return handlePassword(ctx, request)
	}
	svc, err := newService(ctx, request)
	if err != nil {
		if errors.Is(err, legacyauth.ErrProviderNotFound) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(unknownProviderMessage)), nil
		}
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
		// Refused before the password was checked, so it says nothing about the account.
		if errors.Is(err, auth.ErrProviderNotAllowed) {
			return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus(legacyauth.ProviderNotAllowedMessage)), nil
		}
		// An unconfirmed account is named explicitly: the caller proved the password, so nothing
		// is disclosed that they did not already know, and a generic refusal would leave them
		// with no way to recover the account.
		if errors.Is(err, legacyauth.ErrUserNotConfirmed) {
			return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus(legacyauth.UnconfirmedAccountMessage)), nil
		}
		if errors.Is(err, legacyauth.ErrPasswordResetRequired) {
			return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus(legacyauth.PasswordResetRequiredMessage)), nil
		}
		if errors.Is(err, legacyauth.ErrNewPasswordRequired) {
			return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus(legacyauth.NewPasswordRequiredMessage)), nil
		}
		// Uniform 401 so the response is no oracle; operators get the real cause from the log.
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Authentication failed")), nil
	}
	return oidc.OAuthTokenResp(withBearer(tokens)), nil
}

func handleRefresh(ctx context.Context, request events.APIGatewayProxyRequest, svc *legacyauth.Service) (events.APIGatewayProxyResponse, error) {
	var req tokenReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	resource, err := auth.RequestedResourceForTokenRequest(req.Resource)
	if resp, refused := resourceRefusal(err); refused {
		return resp, nil
	}
	tokens, err := svc.RefreshLegacy(ctx, req.RefreshToken, resource)
	if resp, refused := resourceRefusal(err); refused {
		return resp, nil
	}
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("legacy refresh failed")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid refresh token")), nil
	}
	return oidc.OAuthTokenResp(withBearer(tokens)), nil
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
		if errors.Is(err, legacyauth.ErrPasswordRefused) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(passwordRefusedMessage)), nil
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
		if errors.Is(err, legacyauth.ErrSessionsNotEnded) {
			rlog.Error(ctx).Err(err).Msg("password reset: sessions not signed out")
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(resetSessionsNotEndedMessage)), nil
		}
		if errors.Is(err, legacyauth.ErrPasswordRefused) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(passwordRefusedMessage)), nil
		}
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
			utils.NewAPIStatus("Global signout is not supported; sign out each session with its refresh token")), nil
	}
	// Reported, not ignored: returning success for a signout that revoked nothing is worse than a 400.
	if req.RefreshToken == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Refresh token is required")), nil
	}
	err := svc.Signout(ctx, req.RefreshToken)
	switch {
	case errors.Is(err, refresh_tokens_db.ErrRefreshInvalid), errors.Is(err, refresh_tokens_db.ErrRefreshClientMismatch):
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid refresh token")), nil
	// The login is still alive, so the app keeps its token and retries rather than treat it as ended.
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("signout: the refresh family could not be revoked")
		return utils.APIGwRespJSON(http.StatusServiceUnavailable, utils.NewAPIStatus("Sign-out failed; try again")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Successfully signed out")), nil
}

func handlePassword(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req passwordReq
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	// Any API's token will do: a legacy sign-in's is addressed to its client's default API, and the token only names the account, whose old password is the proof.
	caller := user.ClaimsFrom(user.NewContextWithAnyAPIToken(ctx, request))
	if caller.Subject == "" {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Password change failed")), nil
	}
	svc, err := legacyauth.NewServiceForCaller(ctx, caller)
	var noPassword *legacyauth.NoPasswordToChangeError
	switch {
	// A verified token whose sign-in may not do this is an authorization refusal, not a malformed request.
	case errors.As(err, &noPassword):
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus(legacyauth.NoPasswordToChangeMessage(noPassword.Provider))), nil
	case err != nil:
		rlog.Error(ctx).Err(err).Msg("failed to build legacy auth service")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}
	if err := svc.ChangePassword(ctx, req.OldPassword, req.NewPassword); err != nil {
		if errors.Is(err, legacyauth.ErrSessionsNotEnded) {
			rlog.Error(ctx).Err(err).Msg("password change: sessions not signed out")
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(changeSessionsNotEndedMessage)), nil
		}
		rlog.Error(ctx).Err(err).Msg("legacy password change failed")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Password change failed")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("Password changed successfully")), nil
}

// resourceRefusal answers an RFC 8707 refusal with a 400 naming it, so a client can fix its request rather than retry.
func resourceRefusal(err error) (events.APIGatewayProxyResponse, bool) {
	message, ok := auth.TargetDescription(err)
	if !ok {
		return events.APIGatewayProxyResponse{}, false
	}
	return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(message)), true
}

func withBearer(t *auth.UserTokens) *auth.UserTokens {
	t.TokenType = oidc.TokenTypeBearer
	if t.ExpiresIn == 0 {
		t.ExpiresIn = 3600
	}
	return t
}

func main() { lambda.Start(handler) }
