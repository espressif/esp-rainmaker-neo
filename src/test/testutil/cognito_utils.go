// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	"github.com/golang-jwt/jwt/v5"
)

const (

	// foreignKid identifies the key of a pool RMNG does not own. It is deliberately
	// absent from GetTestJWKS, so anything signed with it fails verification here.
	foreignKid = "foreign-key-id"
	// foreignClientID is the app client of that same untrusted pool.
	foreignClientID = "foreign-client-id"
)

// testKid is the shared test signing-key id. It equals oidc.SigningKeyID so a single key+JWKS verifies BOTH Cognito-admin and ESP-User-OIDC test tokens (they all stamp this kid).
var testKid = oidc.SigningKeyID

type CognitoUtils struct {
	testPrivateKey *rsa.PrivateKey
	testKid        string
	testRegion     string
	jwksBytes      []byte

	// foreignPrivateKey signs tokens for a pool outside RMNG's trust boundary. It is
	// never published in jwksBytes.
	foreignPrivateKey *rsa.PrivateKey
}

func InitCognitoUtils(region string) *CognitoUtils {
	cu := &CognitoUtils{}
	var err error

	// Generate a test RSA key pair
	cu.testPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	cu.testKid = testKid

	// Anyone can create a Cognito user pool and mint correctly signed tokens in it. This
	// key stands in for that attacker-controlled pool.
	cu.foreignPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kid": testKid,
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(cu.testPrivateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(cu.testPrivateKey.PublicKey.E)).Bytes()),
			},
		},
	}

	cu.jwksBytes, err = json.Marshal(jwks)
	if err != nil {
		panic(err)
	}

	cu.testRegion = region

	return cu
}

func (cu *CognitoUtils) GetTestRegion() string {
	return cu.testRegion
}

func (cu *CognitoUtils) GetTestJWKS() []byte {
	return cu.jwksBytes
}

// SigningKey returns the shared test private key so callers can mint OIDC tokens (via jwtutil.NewMinter) that verify against the same JWKS this util publishes.
func (cu *CognitoUtils) SigningKey() *rsa.PrivateKey {
	return cu.testPrivateKey
}

// GetIdentityToken mints a signed Cognito-shaped token.
//
// originJTIOverride is optional. Real Cognito stamps the same `origin_jti` on the ID
// and access tokens issued by one authentication event, so by default this derives it
// from `sub` — which makes any two tokens for the same user a matched pair, the common
// case in tests. Pass an override to model tokens from two different sign-ins.
func (cu *CognitoUtils) GetIdentityToken(sub string, issuer, tokenUse string, expired bool, isSuperAdmin bool, email string, userID string, originJTIOverride ...string) string {
	exp := time.Now().Add(time.Hour * 24 * 365).Unix()
	if expired {
		exp = time.Now().Add(time.Hour * -1).Unix()
	}
	originJTI := "origin-" + sub
	if len(originJTIOverride) > 0 && originJTIOverride[0] != "" {
		originJTI = originJTIOverride[0]
	}
	claims := jwt.MapClaims{
		"sub":        sub,
		"token_use":  tokenUse,
		"iss":        issuer,
		"exp":        exp,
		"iat":        time.Now().Unix(),
		"origin_jti": originJTI,
	}

	// Real Cognito always stamps the app client on the token: access tokens carry it in
	// `client_id`, ID tokens in `aud`. Validators that pin the client (see
	// validateCognitoToken in mcp/proxy/auth.go) reject a token that carries
	// neither, so mint it the same way here or such tokens fail before any other check.
	clientID := tokenClientID(isSuperAdmin)
	if tokenUse == "id" {
		claims["aud"] = clientID
	} else {
		claims["client_id"] = clientID
	}

	if isSuperAdmin {
		claims["custom:super_admin"] = "true"
	}

	if email != "" {
		claims["email"] = email
	}

	if userID != "" {
		claims["custom:user_id"] = userID
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = cu.testKid

	tokenString, err := token.SignedString(cu.testPrivateKey)
	if err != nil {
		panic(err)
	}

	// Automatically register session for access tokens
	if tokenUse == "access" {
		// Extract user pool ID from issuer: https://cognito-idp.{region}.amazonaws.com/{userPoolID}
		var userPoolID string
		if issuer != "" {
			parts := splitIssuer(issuer)
			if len(parts) > 0 {
				userPoolID = parts[len(parts)-1]
			}
		}

		username := sub
		if userID != "" {
			username = userID
		}

		if userPoolID != "" {
			cu.registerSessionForToken(tokenString, userPoolID, clientID, username)
		}
	}

	return tokenString
}

// tokenClientID resolves the app client a minted test token belongs to. The session
// registry and the token claims must agree on it, so both read it from here.
func tokenClientID(isSuperAdmin bool) string {
	if isSuperAdmin {
		if clientID := os.Getenv("ADMIN_USER_POOL_CLIENT_ID"); clientID != "" {
			return clientID
		}
		return "test-admin-client-id"
	}
	if clientID := os.Getenv("UPSTREAM_USER_POOL_CLIENT_ID"); clientID != "" {
		return clientID
	}
	return "test-client-id"
}

// splitIssuer splits an issuer URL to extract the user pool ID
func splitIssuer(issuer string) []string {
	// Remove https:// prefix if present
	if strings.HasPrefix(issuer, "https://") {
		issuer = strings.TrimPrefix(issuer, "https://")
	}
	// Split by /
	return strings.Split(issuer, "/")
}

func (cu *CognitoUtils) GetAccessToken(sub string, isAdmin bool) string {
	var userPoolID string
	var clientID string
	if isAdmin {
		userPoolID = os.Getenv("ADMIN_USER_POOL_ID")
		clientID = os.Getenv("ADMIN_USER_POOL_CLIENT_ID")
		if clientID == "" {
			clientID = "test-admin-client-id"
		}
	} else {
		userPoolID = os.Getenv("UPSTREAM_USER_POOL_ID")
		clientID = os.Getenv("UPSTREAM_USER_POOL_CLIENT_ID")
		if clientID == "" {
			clientID = "test-client-id"
		}
	}
	tokenString := cu.GetIdentityToken(sub, fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cu.testRegion, userPoolID), "access", false, false, "", sub)

	// Automatically register session for the token in Cognito mock (if available)
	cu.registerSessionForToken(tokenString, userPoolID, clientID, sub)

	return tokenString
}

// registerSessionForToken registers a session for a token in the Cognito mock
// This is called automatically when GetAccessToken is used, so tests don't need to manually register sessions
func (cu *CognitoUtils) registerSessionForToken(tokenString, userPoolID, clientID, username string) {
	// Try to get Cognito mock - if not available (e.g., in non-test code), silently return
	cognitoClient := awscommon.GetCognitoProviderClient()
	if cognitoClient == nil {
		return
	}

	cognitoMock, ok := cognitoClient.(*mock.CognitoProviderMock)
	if !ok {
		return
	}

	// Register session
	cognitoMock.RegisterSessionForToken(tokenString, userPoolID, clientID, username)
}

func (cu *CognitoUtils) GetExpiredAccessToken(sub string, isAdmin bool) string {
	var userPoolID string
	if isAdmin {
		userPoolID = os.Getenv("ADMIN_USER_POOL_ID")
	} else {
		userPoolID = os.Getenv("UPSTREAM_USER_POOL_ID")
	}
	return cu.GetIdentityToken(sub, fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cu.testRegion, userPoolID), "access", true, false, "", sub)
}

func (cu *CognitoUtils) GetAccessTokenWithWrongIssuer(sub string) string {
	return cu.GetIdentityToken(sub, "https://wrong-issuer.com", "access", false, false, "", sub)
}

func (cu *CognitoUtils) GetAccessTokenWithWrongTokenUse(sub string, isAdmin bool) string {
	var userPoolID string
	if isAdmin {
		userPoolID = os.Getenv("ADMIN_USER_POOL_ID")
	} else {
		userPoolID = os.Getenv("UPSTREAM_USER_POOL_ID")
	}
	return cu.GetIdentityToken(sub, fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cu.testRegion, userPoolID), "wrong-token-use", false, false, "", sub)
}

func (cu *CognitoUtils) GetAccessTokenWithSuperAdmin(sub, email string) string {
	return cu.GetIdentityToken(sub, fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cu.testRegion, os.Getenv("ADMIN_USER_POOL_ID")), "access", false, true, email, sub)
}

// GetForeignPoolID names a Cognito user pool outside RMNG's trust boundary, standing in
// for one an attacker creates in their own AWS account.
func (cu *CognitoUtils) GetForeignPoolID() string {
	return cu.testRegion + "_ForeignPool"
}

// GetForeignPoolAccessToken mints an access token in a pool RMNG does not own, carrying
// victimUserID in custom:user_id.
//
// Use it to assert that a code path binds a token to RMNG's own pool before reading an
// identity off it. The token is genuine, correctly signed, unexpired, and Cognito-shaped.
// Only the issuer marks it as foreign, so a caller that skips issuer verification cannot
// tell it apart from a real one and will act on the victim's identity.
//
// The signing key is absent from GetTestJWKS, and the mock is primed so GetUser succeeds
// for this token. That combination is deliberate: in production GetUser is an anonymous
// call carrying no pool ID, so Cognito resolves the token against the attacker's own pool
// and answers normally. A mock that rejected the token would make the test pass whether
// or not the verification exists.
func (cu *CognitoUtils) GetForeignPoolAccessToken(username, victimUserID string) string {
	poolID := cu.GetForeignPoolID()

	claims := jwt.MapClaims{
		"sub":            username,
		"token_use":      "access",
		"iss":            fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cu.testRegion, poolID),
		"client_id":      foreignClientID,
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"custom:user_id": victimUserID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = foreignKid

	tokenString, err := token.SignedString(cu.foreignPrivateKey)
	if err != nil {
		panic(err)
	}

	cu.primeForeignPool(tokenString, poolID, username, victimUserID)

	return tokenString
}

// primeForeignPool gives the Cognito mock the state real Cognito would have: the attacker
// owns this pool, so the user exists there and the session is live.
func (cu *CognitoUtils) primeForeignPool(tokenString, poolID, username, victimUserID string) {
	cognitoClient := awscommon.GetCognitoProviderClient()
	if cognitoClient == nil {
		return
	}

	cognitoMock, ok := cognitoClient.(*mock.CognitoProviderMock)
	if !ok {
		return
	}

	cognitoMock.AddTestUserPoolDirect(poolID, foreignClientID)

	// The attacker sets custom:user_id on their own user to whatever they please; Cognito
	// enforces no relationship between it and RMNG's tenant IDs.
	user := cognitoMock.AddTestUserDirect(poolID, username, username+"@attacker.example", "Attacker@123", true)
	user.Attributes["custom:user_id"] = victimUserID

	cognitoMock.RegisterSessionForToken(tokenString, poolID, foreignClientID, username)
}
