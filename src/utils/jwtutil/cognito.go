// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package jwtutil

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

type CognitoClaims struct {
	jwt.RegisteredClaims
	TokenUse string `json:"token_use,omitempty"`
	Scope    any    `json:"scope,omitempty"`
	AuthTime int    `json:"auth_time,omitempty"`
	Version  int    `json:"version,omitempty"`
	ClientId string `json:"client_id,omitempty"`
	// OriginJTI ties an ID token and an access token to one authentication event:
	// Cognito stamps the same value on both when they are issued together.
	OriginJTI     string `json:"origin_jti,omitempty"`
	UserName      string `json:"username,omitempty"`
	Email         string `json:"email,omitempty"`
	UserId        string `json:"custom:user_id,omitempty"`
	IsSuperAdmin  string `json:"custom:super_admin,omitempty"`
	PhoneNumber   string `json:"phone_number,omitempty"`
	Azp           string `json:"azp,omitempty"`
	Name          string `json:"given_name,omitempty"`
	Picture       string `json:"picture,omitempty"`
	Locale        string `json:"locale,omitempty"`
	AtHash        string `json:"at_hash,omitempty"`
	EmailVerified any    `json:"email_verified,omitempty"`
	AccessKeyId   string `json:"custom_access_key_id,omitempty"`
	SecretKey     string `json:"custom_secret_key,omitempty"`
	SessionToken  string `json:"custom_session_token,omitempty"`
	IdentityId    string `json:"custom_identity_id,omitempty"`
}

// userPoolRegion returns the region a Cognito user pool lives in, taken from the pool ID
// itself: Cognito always formats it as "<region>_<id>".
//
// The pool's region, not the runtime's. Reading AWS_REGION here would be wrong for any
// Lambda that runs outside the pool's region, and RMNG has those: the Alexa smart-home
// skill is deployed to us-east-1, eu-west-1 and us-west-2 (see AWS_REGION_TO_ALEXA in
// tools/alexa_setup.py) while authenticating against the single ESP user pool. An
// EU or FE user's token is issued by the pool's home region and would never match an
// issuer built from the Lambda's own region.
func userPoolRegion(userPoolID string) (string, error) {
	region, _, found := strings.Cut(userPoolID, "_")
	if !found || region == "" {
		return "", fmt.Errorf("malformed user pool ID %q", userPoolID)
	}
	return region, nil
}

// ExtractCogntioClaimsFromOurToken verifies an access token against the given pool
// and returns its claims.
//
// Access tokens only. It backs GetVerifiedUser, whose next step
// (cognito-idp:GetUser) accepts nothing else, and an ID token's `aud` names the app
// client rather than this API — so an ID token here would let a token minted for any
// other client of the pool authenticate as its subject.
func ExtractCogntioClaimsFromOurToken(keySet jwk.Set, userPoolID string, tokenString string) (CognitoClaims, error) {
	return extractClaimsFromToken(keySet, userPoolID, tokenString, TokenUseAccess)
}

// ExtractCognitoClaimsFromIDOrAccessToken verifies either an ID or an access token
// against the given pool and returns its claims.
//
// For reading profile claims that Cognito only puts in ID tokens (email,
// phone_number, custom:user_id). Not for authenticating an API caller: it cannot
// tell whether an ID token was minted for this API or for another client of the same
// pool. Authenticate with GetVerifiedUser.
func ExtractCognitoClaimsFromIDOrAccessToken(keySet jwk.Set, userPoolID string, tokenString string) (CognitoClaims, error) {
	return extractClaimsFromToken(keySet, userPoolID, tokenString, TokenUseID, TokenUseAccess)
}

// ExtractCognitoIDTokenClaims verifies an ID token against the given pool and returns
// its claims. Rejects an access token.
//
// For the one flow that legitimately consumes an ID token: exchanging it at a Cognito
// Identity Pool. Callers must additionally pin `aud` to their own app clients, because
// every client of a pool shares its issuer and signing keys.
func ExtractCognitoIDTokenClaims(keySet jwk.Set, userPoolID string, tokenString string) (CognitoClaims, error) {
	return extractClaimsFromToken(keySet, userPoolID, tokenString, TokenUseID)
}

// CognitoIssuer returns the exact `iss` value Cognito stamps on tokens from the given
// user pool. Callers compare against the whole string rather than matching on the pool
// ID alone, so an issuer like https://attacker.example/us-east-1_OurPool cannot pass.
func CognitoIssuer(userPoolID string) (string, error) {
	region, err := userPoolRegion(userPoolID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID), nil
}

func extractClaimsFromToken(keySet jwk.Set, userPoolID string, tokenString string, allowedTokenUses ...string) (CognitoClaims, error) {
	var claims CognitoClaims
	if err := VerifyJWTInto(tokenString, keySet, &claims); err != nil {
		return CognitoClaims{}, err
	}

	expectedIssuer, err := CognitoIssuer(userPoolID)
	if err != nil {
		return CognitoClaims{}, err
	}
	if err := AssertIssuerAndSubject(claims.Issuer, claims.Subject, expectedIssuer); err != nil {
		return CognitoClaims{}, err
	}

	if err := AssertTokenUseOneOf(claims.TokenUse, allowedTokenUses...); err != nil {
		return CognitoClaims{}, err
	}

	return claims, nil
}

// CognitoPool is the pool a token is verified against: its id, the JWKS to verify with,
// and the app clients whose tokens the caller accepts. Every client of one pool shares
// the pool's issuer and signing keys, so the client set is what narrows a token to this
// caller rather than any other client of the same pool.
type CognitoPool struct {
	PoolID           string
	JWKS             jwk.Set
	AllowedClientIDs []string
}

// VerifyCognitoTokenPair verifies an access token and an ID token against the same pool
func VerifyCognitoTokenPair(pool CognitoPool, accessToken, idToken string) (CognitoClaims, error) {
	if accessToken == "" || idToken == "" {
		return CognitoClaims{}, fmt.Errorf("both an access token and an id_token are required")
	}

	idClaims, err := ExtractCognitoIDTokenClaims(pool.JWKS, pool.PoolID, idToken)
	if err != nil {
		return CognitoClaims{}, fmt.Errorf("id_token failed validation: %w", err)
	}
	if err := RequireAllowedClientID(idClaims.Audience, pool.AllowedClientIDs); err != nil {
		return CognitoClaims{}, fmt.Errorf("id_token was issued for a different app client: %w", err)
	}

	// Verified here rather than taken from an authorizer's claim passthrough, so the
	// comparison below is between values this function checked itself.
	accessClaims, err := ExtractCogntioClaimsFromOurToken(pool.JWKS, pool.PoolID, accessToken)
	if err != nil {
		return CognitoClaims{}, fmt.Errorf("access token failed validation: %w", err)
	}
	// Access tokens name the client in `client_id` rather than `aud`.
	if err := RequireAllowedClientID([]string{accessClaims.ClientId}, pool.AllowedClientIDs); err != nil {
		return CognitoClaims{}, fmt.Errorf("access token was issued for a different app client: %w", err)
	}

	if err := RequireSameAuthEvent(accessClaims, idClaims); err != nil {
		return CognitoClaims{}, err
	}
	return idClaims, nil
}
