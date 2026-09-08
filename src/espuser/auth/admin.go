// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package auth - Admin authentication service for admin users
package auth

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/cognitoutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/cognito"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
)

type AdminAuthService struct {
	cognitoService *cognitoutil.CognitoService
}

func NewAdminAuthService(ctx context.Context) (*AdminAuthService, error) {
	cognitoSvc, err := cognitoutil.NewCognitoService(ctx, os.Getenv("ADMIN_USER_POOL_ID"), os.Getenv("ADMIN_USER_POOL_CLIENT_ID"), os.Getenv("ADMIN_USER_POOL_JWKS_PARA_NAME"))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to create cognito service")
	}

	return &AdminAuthService{
		cognitoService: cognitoSvc,
	}, nil
}

// Sentinel errors from RequireAdmin so callers can map to HTTP status without HTTP types leaking here.
var (
	ErrMissingToken = fmt.Errorf("authorization token is required")
	ErrNotAdmin     = fmt.Errorf("admin privilege required")
)

// RequireAdmin returns the caller's UserInfo if the token verifies against the admin
// user pool (empty ⇒ ErrMissingToken, anything the admin pool did not issue ⇒ ErrNotAdmin).
// Membership of the admin pool is the whole privilege: there is no second tier above it.
func RequireAdmin(ctx context.Context, token string) (UserInfo, error) {
	if token == "" {
		return UserInfo{}, ErrMissingToken
	}
	svc, err := NewAdminAuthService(ctx)
	if err != nil {
		return UserInfo{}, err
	}
	info, err := svc.ParseUserInfoFromToken(ctx, token)
	if err != nil {
		return UserInfo{}, ErrNotAdmin
	}
	return info, nil
}

func (s *AdminAuthService) GetUserFromProviderUsingToken(ctx context.Context, token string) (UserInfo, error) {
	attributes, err := s.cognitoService.GetVerifiedUser(ctx, token)
	if err != nil {
		return UserInfo{}, rmerror.NewRMError(err, "Failed to get user from Cognito")
	}
	cogntioUserInfo := cognito.ParseCognitoAttributes(attributes)
	return UserInfo{
		Email:           cogntioUserInfo.Email,
		PhoneNumber:     cogntioUserInfo.PhoneNumber,
		CognitoUsername: cogntioUserInfo.Username,
		UserID:          cogntioUserInfo.UserID,
		IsSuperAdmin:    cogntioUserInfo.SuperAdmin,
		IsAdmin:         true,
	}, nil
}

// VerifyToken validates the Cognito token against the admin pool JWKS only (no live GetUser).
func (s *AdminAuthService) VerifyTokenPair(ctx context.Context, accessToken, idToken string) error {
	clientID := os.Getenv("ADMIN_USER_POOL_CLIENT_ID")
	if clientID == "" {
		return rmerror.NewRMError(nil, "admin pool app client id is not configured")
	}
	_, err := jwtutil.VerifyCognitoTokenPair(jwtutil.CognitoPool{
		PoolID:           s.cognitoService.UserPoolID,
		JWKS:             s.cognitoService.UserPoolJWKS,
		AllowedClientIDs: []string{clientID},
	}, accessToken, idToken)
	return err
}

func (s *AdminAuthService) VerifyToken(ctx context.Context, token string) error {
	_, err := jwtutil.ExtractCognitoClaimsFromIDOrAccessToken(s.cognitoService.UserPoolJWKS, s.cognitoService.UserPoolID, token)
	return err
}

func (s *AdminAuthService) ParseUserInfoFromToken(ctx context.Context, token string) (UserInfo, error) {
	claims, err := jwtutil.ExtractCognitoClaimsFromIDOrAccessToken(s.cognitoService.UserPoolJWKS, s.cognitoService.UserPoolID, token)
	if err != nil {
		return UserInfo{}, err
	}

	return UserInfo{
		Email:           claims.Email,
		PhoneNumber:     claims.PhoneNumber,
		CognitoUsername: claims.UserName,
		UserID:          claims.UserId,
		IsSuperAdmin:    claims.IsSuperAdmin == "true",
		IsAdmin:         true,
	}, nil
}

func (s *AdminAuthService) GetUserFromProvider(ctx context.Context, providerUserID string) (UserInfo, error) {
	cognitoUser, err := s.cognitoService.AdminGetUser(ctx, providerUserID)
	if err != nil {
		return UserInfo{}, rmerror.NewRMError(err, "Failed to get user from Cognito")
	}
	cogntioUserInfo := cognito.ParseCognitoAttributes(cognitoUser.UserAttributes)
	return UserInfo{
		Email:           cogntioUserInfo.Email,
		PhoneNumber:     cogntioUserInfo.PhoneNumber,
		CognitoUsername: cogntioUserInfo.Username,
		UserID:          cogntioUserInfo.UserID,
		IsSuperAdmin:    cogntioUserInfo.SuperAdmin,
		IsAdmin:         true,
	}, nil
}
