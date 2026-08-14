// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

type UserInfo struct {
	Email           string `json:"email,omitempty"`
	PhoneNumber     string `json:"phone_number,omitempty"`
	CognitoUsername string `json:"cognito_username,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	IsSuperAdmin    bool   `json:"is_super_admin"`
	IsAdmin         bool   `json:"is_admin"`
	Sub             string `json:"sub,omitempty"`

	Name    string `json:"name,omitempty"`
	Locale  string `json:"locale,omitempty"`
	Picture string `json:"picture,omitempty"`
}

// JSON tags are the OAuth 2.0 token-response names (RFC 6749 §5.1); id_token is
// omitempty since it is present only when openid is in scope.
type UserTokens struct {
	AccessToken        string `json:"access_token"`
	RefreshToken       string `json:"refresh_token,omitempty"`
	IDToken            string `json:"id_token,omitempty"`
	TokenType          string `json:"token_type"`
	ExpiresIn          int    `json:"expires_in"`
	MustChangePassword bool   `json:"must_change_password,omitempty"`
}

// storeUserDetails resolves emailOrPhone to an internal user id (reusing an existing
// one, else minting), stamps it on the rmng context, and creates the user-details row
// for the given provider if the user is new. Returns the user id and whether it existed.
// Shared by the Cognito and OAuth services; the Cognito service additionally syncs the
// id onto its Cognito user.

// IsUserNotExistsError checks if an error indicates that a user does not exist
func IsUserNotExistsError(err error) bool {
	if err == nil {
		return false
	}
	var userNotFound *types.UserNotFoundException
	var resourceNotFound *types.ResourceNotFoundException
	if errors.As(err, &userNotFound) || errors.As(err, &resourceNotFound) {
		rlog.Info(context.TODO()).Msg("User not found, returning success for security")
		return true
	}
	return false
}

func IsUserNotConfirmedError(err error) bool {
	if err == nil {
		return false
	}
	var notConfirmed *types.UserNotConfirmedException
	return errors.As(err, &notConfirmed)
}

// IsUserAlreadyExistsError checks if an error indicates that a user already exists
func IsUserAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var usernameExists *types.UsernameExistsException
	var aliasExists *types.AliasExistsException
	if errors.As(err, &usernameExists) || errors.As(err, &aliasExists) {
		rlog.Info(context.TODO()).Msg("User already exists, returning success for security")
		return true
	}
	return false
}
