// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/events"
)

type DefaultAuthServiceFactory struct{}

func NewAuthServiceFactory() AuthServiceFactory {
	return &DefaultAuthServiceFactory{}
}

func (f *DefaultAuthServiceFactory) CreateAuthServiceFromAPIRequest(ctx context.Context, request events.APIGatewayProxyRequest) (AuthService, string, error) {
	identity, isOIDC := extractCallerIdentity(request)
	if isOIDC {
		return f.CreateUserAuthService(ctx), identity, nil
	}

	if identity == "" {
		return nil, "", rmerror.NewRMError(nil, "unauthorized: no caller identity")
	}
	return f.CreateAdminAuthService(ctx), identity, nil
}

func extractCallerIdentity(request events.APIGatewayProxyRequest) (identity string, isOIDC bool) {
	provider := request.RequestContext.Identity.CognitoAuthenticationProvider

	if cognito := strings.Split(provider, "CognitoSignIn:"); len(cognito) == 2 {
		return cognito[1], false
	}

	if provider != "" {
		if idx := strings.LastIndex(provider, ":"); idx >= 0 && idx < len(provider)-1 {
			return provider[idx+1:], true
		}
	}

	rlog.Warn(nil).Msg("missing or invalid cognito authentication provider")
	return "", false
}

func (f *DefaultAuthServiceFactory) CreateAuthServiceForToken(ctx context.Context, token string) (AuthService, error) {
	issuer, err := jwtutil.UnverifiedIssuer(token)
	if err != nil {
		return nil, err
	}
	switch jwtutil.ClassifyIssuer(issuer, os.Getenv("USER_ISSUER")) {
	case jwtutil.IssuerESPUser:
		return f.CreateUserAuthService(ctx), nil
	case jwtutil.IssuerCognito:
		return f.CreateAdminAuthService(ctx), nil
	default:
		return nil, ErrUntrustedIssuer
	}
}

func (f *DefaultAuthServiceFactory) CreateUserAuthService(ctx context.Context) AuthService {
	userService, err := NewOAuthUserAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to create user auth service")
		return nil
	}
	return userService
}

func (f *DefaultAuthServiceFactory) CreateAdminAuthService(ctx context.Context) AuthService {
	adminService, err := NewAdminAuthService(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to create admin auth service")
		return nil
	}
	return adminService
}
