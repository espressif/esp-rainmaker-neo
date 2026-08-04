// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package auth provides authentication services for both users and admins
package auth

import (
	"context"
	"errors"

	"github.com/aws/aws-lambda-go/events"
)

// AuthService resolves and verifies a caller against the provider that issued their token.
// Both implementations are reached only through the factory, which routes on the token's
// issuer: OIDC end users to the passwordless service, Cognito admins to the admin one.
// ErrUntrustedIssuer means the token names no issuer this deployment verifies, so no
// service can speak for it — an authentication failure, not an authorization one.
var ErrUntrustedIssuer = errors.New("untrusted token issuer")

type AuthService interface {
	// GetUserFromProvider resolves a provider identity (OIDC sub, or Cognito username) to a user.
	GetUserFromProvider(ctx context.Context, providerUserID string) (UserInfo, error)

	// GetUserFromProviderUsingToken verifies the token against its provider, then resolves the caller.
	GetUserFromProviderUsingToken(ctx context.Context, token string) (UserInfo, error)

	// VerifyToken validates signature and claims only, resolving no user.
	VerifyToken(ctx context.Context, token string) error

	// VerifyTokenPair validates an access token and an id token and requires both to come
	// from one sign-in, for a caller handed the two halves by different routes.
	VerifyTokenPair(ctx context.Context, accessToken, idToken string) error
}

type AuthServiceFactory interface {
	// CreateAuthServiceFromAPIRequest resolves the service + caller identity from the request (OIDC end user or Cognito admin).
	CreateAuthServiceFromAPIRequest(ctx context.Context, request events.APIGatewayProxyRequest) (AuthService, string, error)

	// CreateAuthServiceForToken selects the service verifying a bearer token, routed by the token's iss (OIDC user service, or Cognito admin).
	CreateAuthServiceForToken(ctx context.Context, token string) (AuthService, error)

	CreateUserAuthService(ctx context.Context) AuthService

	CreateAdminAuthService(ctx context.Context) AuthService
}
