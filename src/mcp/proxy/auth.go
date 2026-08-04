// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/aws/aws-lambda-go/events"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// UserContext is implemented by each project to carry authenticated user info.
type UserContext interface {
	GetUserID() string
	GoContext() context.Context
}

// Authenticator validates an API Gateway request and returns a UserContext.
type Authenticator func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (UserContext, error)

// UserResolver looks up a project-specific user by their user_id (the token sub).
type UserResolver func(ctx context.Context, userID string) (UserContext, error)

// oidcAuthState holds the ESP User OIDC issuer/client, the JWKS param, and the resolver.
type oidcAuthState struct {
	issuer       string
	clientID     string
	jwksParaName string
	resolver     UserResolver

	mu       sync.Mutex
	jwks     jwk.Set
	jwksOnce sync.Once
}

// NewOIDCAuthenticator creates an Authenticator that validates ESP User OIDC JWTs (iss==issuer, aud==clientID) against the JWKS in jwksParamName and resolves the token's sub (== opaque user_id) to a user via the provided UserResolver.
func NewOIDCAuthenticator(issuer, clientID, jwksParamName string, resolver UserResolver) Authenticator {
	state := &oidcAuthState{
		issuer:       strings.TrimRight(issuer, "/"),
		clientID:     clientID,
		jwksParaName: jwksParamName,
		resolver:     resolver,
	}

	return state.authenticate
}

func (s *oidcAuthState) getJWKS(ctx context.Context) (jwk.Set, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.jwks != nil {
		return s.jwks, nil
	}

	jwksJSON, err := ssmutil.GetParameterWithCaching(ctx, s.jwksParaName, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS: %w", err)
	}
	if jwksJSON == "" {
		return nil, fmt.Errorf("JWKS value is empty for parameter %s", s.jwksParaName)
	}

	keySet, err := jwtutil.ParseJWKS(jwksJSON)
	if err != nil {
		return nil, err
	}

	s.jwks = keySet
	return keySet, nil
}

func (s *oidcAuthState) authenticate(ctx context.Context, request events.APIGatewayV2HTTPRequest) (UserContext, error) {
	authHeader := request.Headers["Authorization"]
	if authHeader == "" {
		authHeader = request.Headers["authorization"]
	}
	if authHeader == "" {
		return nil, fmt.Errorf("missing Authorization header")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return nil, fmt.Errorf("invalid Authorization header format")
	}

	keySet, err := s.getJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JWKS: %w", err)
	}

	claims, err := jwtutil.VerifyJWT(token, keySet)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if err := jwtutil.AssertOIDCClaims(claims, s.issuer); err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	// Resource-server path: only an access token is accepted (an id token must not authorize
	// MCP calls). RFC 9700 token substitution.
	if err := jwtutil.AssertTokenUse(claims, jwtutil.TokenUseAccess); err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if s.clientID != "" {
		if err := jwtutil.AssertAudience(claims, s.clientID); err != nil {
			return nil, err
		}
	}

	userID, _ := claims["sub"].(string)
	return s.resolver(ctx, userID)
}
