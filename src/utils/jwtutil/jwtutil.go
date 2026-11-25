// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package jwtutils holds reusable RS256 JWT + JWK helpers: sign a claim set with a
// kid-stamped header, verify a token against a JWK set, parse an RSA signing key,
// and build the public JWK/JWKS. Shared by the OTP token minter, the OIDC discovery
// publisher, and any token-verifying consumer.
package jwtutil

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// SigningAlg is the sole JWS algorithm; alg:none is never accepted (RFC 8725).
const SigningAlg = "RS256"

// JWK is a single RSA public verify key (RFC 7517/7518 required members).
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

// Assembles the JWS by hand because golang-jwt's RS256 method demands a concrete *rsa.PrivateKey,
// which would rule out a KMS-backed signer.
func SignRS256(claims jwt.Claims, signer crypto.Signer, kid string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signingString, err := token.SigningString()
	if err != nil {
		return "", fmt.Errorf("failed to build signing string: %w", err)
	}
	digest := sha256.Sum256([]byte(signingString))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	return signingString + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

const (
	AccessTokenTTL = time.Hour
	IDTokenTTL     = time.Hour
)

// token_use claim values. Resource endpoints require TokenUseAccess so an id token (meant for the
// client) cannot be replayed as an access token — see AssertTokenUse.
const (
	TokenUseAccess = "access"
	TokenUseID     = "id"
)

type Contact struct {
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Picture     string `json:"picture,omitempty"`
}

// UserClaims are the claims a user's tokens carry beyond the registered ones; each json tag is the claim's name, and a zero field is left out.
type UserClaims struct {
	Contact
	// AuthEventID ties the tokens of one sign-in together under Cognito's claim name, so RequireSameAuthEvent serves both issuers.
	AuthEventID string `json:"origin_jti,omitempty"`
	// SID names the browser session the token was minted under (OIDC Front-Channel Logout 1.0 s3); the session cookie never reaches a product's API.
	SID string `json:"sid,omitempty"`
	// AMR lists the methods ESP User itself verified (RFC 8176), e.g. ["pwd"]; a federated sign-in has none.
	AMR []string `json:"amr,omitempty"`
	// Provider names the provider row that authenticated the person; the claim name is Entra's and Okta's, not OIDC-registered.
	Provider string `json:"idp,omitempty"`
}

type Minter struct {
	issuer string
	signer crypto.Signer
	kid    string
}

func NewMinter(issuer string, signer crypto.Signer, kid string) *Minter {
	return &Minter{issuer: issuer, signer: signer, kid: kid}
}

// NewAuthEventID returns an identifier for one authentication event, to be stamped on
// every token issued from it. Two tokens carrying the same value provably came from a
// single sign-in, which is what lets a receiver refuse a mismatched pair — Cognito calls
// its equivalent `origin_jti`, and RequireSameAuthEvent compares either.
func NewAuthEventID() string {
	return uuid.NewString()
}

// AccessToken mints a signed RS256 access token for a user.
//
// audience is the RFC 8707 resource the token is for, the API it will be spent at (RFC 9068 s3); it is required, unlike IDToken's, which is the client by definition.
// RFC 9068 s2.2.1 allows amr (and auth_time, which we keep to the ID token); sid is OIDC Front-Channel Logout 1.0 s3.
func (m *Minter) AccessToken(userID, clientID, scope, audience string, claims UserClaims) (string, error) {
	if audience == "" {
		return "", ErrNoAudience
	}
	now := time.Now()
	token := jwt.MapClaims{
		"iss":       m.issuer,
		"sub":       userID,
		"aud":       audience,
		"client_id": clientID,
		"token_use": TokenUseAccess,
		"scope":     scope,
		"iat":       now.Unix(),
		"exp":       now.Add(AccessTokenTTL).Unix(),
	}
	if err := stampUserClaims(token, claims); err != nil {
		return "", err
	}
	return SignRS256(token, m.signer, m.kid)
}

// stampUserClaims adds each non-zero field of claims to token under its json name, e.g. Provider "google" becomes "idp": "google"; a name already in token is an error, so it never overwrites a registered claim.
func stampUserClaims(token jwt.MapClaims, claims UserClaims) error {
	encoded, err := json.Marshal(claims)
	if err != nil {
		return rmerror.NewRMError(err, "jwtutil: failed to encode user claims")
	}
	var optional map[string]any
	if err := json.Unmarshal(encoded, &optional); err != nil {
		return rmerror.NewRMError(err, "jwtutil: failed to decode user claims")
	}
	for name, value := range optional {
		if _, registered := token[name]; registered {
			return rmerror.NewRMError(nil, fmt.Sprintf("jwtutil: user claim %q would overwrite a registered claim", name))
		}
		token[name] = value
	}
	return nil
}

// ClientCredentialsToken mints an access token for the client itself (RFC 6749 s4.4).
//
// sub is the client, because no resource owner is involved (RFC 9068 s2.2.1); aud is the required resource, as for AccessToken.
//
// There is no contact and no auth event: both describe a human sign-in that did not happen.
func (m *Minter) ClientCredentialsToken(clientID, scope, audience string) (string, error) {
	if audience == "" {
		return "", ErrNoAudience
	}
	now := time.Now()
	return SignRS256(jwt.MapClaims{
		"iss":       m.issuer,
		"sub":       clientID,
		"aud":       audience,
		"client_id": clientID,
		"token_use": TokenUseAccess,
		"gty":       "client_credentials", // marks the machine grant so a resource server can refuse it where a human subject is required (RFC 6749 s4.4)
		"scope":     scope,
		"iat":       now.Unix(),
		"exp":       now.Add(AccessTokenTTL).Unix(),
	}, m.signer, m.kid)
}

// IDToken mints a signed RS256 id token (OIDC) for a user.
//
// authTime is when the person authenticated, not the mint time (OIDC Core s2); zero means now. claims.AuthEventID must be the one given to the AccessToken minted in the same sign-in.
func (m *Minter) IDToken(userID, clientID string, authTime int64, claims UserClaims) (string, error) {
	now := time.Now()
	if authTime <= 0 {
		authTime = now.Unix()
	}
	token := jwt.MapClaims{
		"iss":       m.issuer,
		"sub":       userID,
		"aud":       clientID,
		"token_use": TokenUseID,
		"auth_time": authTime,
		"iat":       now.Unix(),
		"exp":       now.Add(IDTokenTTL).Unix(),
	}
	if err := stampUserClaims(token, claims); err != nil {
		return "", err
	}
	return SignRS256(token, m.signer, m.kid)
}

// rs256KeyFunc resolves a token's RSA verify key from keySet by kid, rejecting non-RSA algorithms.
func rs256KeyFunc(keySet jwk.Set) jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid header not found in token")
		}
		key, ok := keySet.LookupKeyID(kid)
		if !ok {
			return nil, fmt.Errorf("key %v not found in JWKS", kid)
		}
		var rawKey interface{}
		if err := key.Raw(&rawKey); err != nil {
			return nil, fmt.Errorf("failed to get raw public key: %w", err)
		}
		return rawKey, nil
	}
}

// VerifyJWTInto verifies an RS256 token into a caller-supplied jwt.Claims (typed struct or MapClaims), checking signature + kid + RSA-alg + expiry. Issuer/token_use policy is the caller's to enforce on dest.
func VerifyJWTInto(tokenString string, keySet jwk.Set, dest jwt.Claims) error {
	// WithExpirationRequired: a token carrying no exp is refused rather than treated as
	// never expiring, which is what an absent claim would otherwise mean.
	if _, err := jwt.ParseWithClaims(tokenString, dest, rs256KeyFunc(keySet), jwt.WithExpirationRequired()); err != nil {
		return fmt.Errorf("failed to verify token: %w", err)
	}
	// Re-checked explicitly so the expiry guarantee is not left implicit in the parser options.
	exp, err := dest.GetExpirationTime()
	if err != nil {
		return fmt.Errorf("failed to read token expiry: %w", err)
	}
	if exp == nil {
		return fmt.Errorf("token has no expiry claim")
	}
	if time.Now().After(exp.Time) {
		return fmt.Errorf("token is expired")
	}
	return nil
}

// VerifyJWTExpiredOK verifies signature + kid + RSA-alg and returns the claims WITHOUT
// enforcing expiry. It exists for exactly one caller shape: a token presented as a HINT
// rather than as a credential.
//
// Nothing is authorized on the strength of this. The caller still has to hold a real
// credential (a session cookie, in the only current use) and must compare the claims it
// finds here against that credential. What the signature check still proves is that WE
// minted the token, which is all a hint is asked to establish.
//
// Never call this where a token grants access. For that, expiry IS the security property
// and VerifyJWT is the function.
func VerifyJWTExpiredOK(tokenString string, keySet jwk.Set) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	// WithoutClaimsValidation skips the registered-claim checks (exp, nbf, iat) but keeps
	// signature and algorithm verification, which is the whole point.
	if _, err := jwt.ParseWithClaims(tokenString, claims, rs256KeyFunc(keySet), jwt.WithoutClaimsValidation()); err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}
	return claims, nil
}

// VerifyJWT verifies an RS256 token against keySet and returns the parsed claims as a map.
func VerifyJWT(tokenString string, keySet jwk.Set) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	if err := VerifyJWTInto(tokenString, keySet, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// BuildJWK builds the public JWK for an RSA key; n and e are unpadded base64url per RFC 7518.
func BuildJWK(pub *rsa.PublicKey, kid string) JWK {
	return JWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: SigningAlg,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// BuildJWKS wraps keys into a JWK Set; variadic so rotation can publish an overlap set.
func BuildJWKS(keys ...JWK) JWKS {
	return JWKS{Keys: keys}
}

// RSAThumbprint is the RFC 7638 JWK thumbprint, used as the kid: minter and JWKS publisher derive
// it independently from the same public key, so they agree without sharing state, and it changes
// exactly when the key does.
func RSAThumbprint(pub *rsa.PublicKey) string {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	canonical := fmt.Sprintf(`{"e":"%s","kty":"RSA","n":"%s"}`, e, n)
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// UnverifiedIssuer reads the iss claim without verifying the signature, only to route the token to the right verifier.
func UnverifiedIssuer(tokenString string) (string, error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadableToken, err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("%w: invalid token claims", ErrUnreadableToken)
	}
	issuer, _ := claims["iss"].(string)
	if issuer == "" {
		return "", fmt.Errorf("%w: token has no issuer", ErrUnreadableToken)
	}
	return issuer, nil
}

// ErrUnreadableToken means the token could not be parsed far enough to name its issuer, so
// no verifier can be chosen for it.
var ErrUnreadableToken = errors.New("unreadable token")

// ErrNoAudience refuses to mint an access token that names no API, since a client id is never an audience.
var ErrNoAudience = errors.New("access token has no audience")

// IsUnreadableToken reports whether err came from a token that could not be parsed.
// ErrInvalidAccessToken means the bearer access token itself failed verification (expired, badly
// signed, not an access token, or for another API), which RFC 6750 s3.1 answers with 401 invalid_token.
var ErrInvalidAccessToken = errors.New("invalid access token")

func IsUnreadableToken(err error) bool {
	return errors.Is(err, ErrUnreadableToken)
}

// IssuerKind classifies a token issuer for demux; verification stays with the caller.
type IssuerKind int

const (
	IssuerUnknown IssuerKind = iota
	IssuerESPUser
	IssuerCognito
)

// cognitoIssuerPrefix is the scheme+host prefix of every Cognito user-pool issuer URL.
const cognitoIssuerPrefix = "https://cognito-idp."

// ClassifyIssuer routes a token's iss to its provider: the configured ESP User issuer, or
// any Cognito user pool. espUserIssuer is ESPUSER_ISSUER ("" when unset); both sides are
// trailing-slash-normalized. This is pure routing — the caller verifies against the matched provider.
func ClassifyIssuer(issuer, espUserIssuer string) IssuerKind {
	espUserIssuer = strings.TrimRight(espUserIssuer, "/")
	if espUserIssuer != "" && strings.TrimRight(issuer, "/") == espUserIssuer {
		return IssuerESPUser
	}
	if strings.HasPrefix(issuer, cognitoIssuerPrefix) {
		return IssuerCognito
	}
	return IssuerUnknown
}

// ParseJWKS parses a JWK set from its JSON form (e.g. an SSM snapshot).
func ParseJWKS(jwksJSON string) (jwk.Set, error) {
	keySet, err := jwk.Parse([]byte(jwksJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWKS: %w", err)
	}
	return keySet, nil
}

// VerifyOIDCToken parses jwksJSON (cached), verifies the RS256 token against it, and asserts the
// OIDC claim policy (exp, iss==issuer, non-empty sub). The one composing entry point for the
// SSM-JWKS verify sites; callers fetch jwksJSON (env-cached) and pass it in.
func VerifyOIDCToken(jwksJSON, issuer, token string) (jwt.MapClaims, error) {
	keySet, err := ParseJWKS(jwksJSON)
	if err != nil {
		return nil, err
	}
	claims, err := VerifyJWT(token, keySet)
	if err != nil {
		return nil, err
	}
	if err := AssertOIDCClaims(claims, issuer); err != nil {
		return nil, err
	}
	return claims, nil
}

// AssertOIDCClaims enforces the OIDC claim policy on a signature-verified token (VerifyJWT already
// checked signature + kid + RSA-alg + expiry): a present expiry, iss==issuer, and a non-empty sub.
// Shared so every verify site applies the identical policy.
func AssertOIDCClaims(claims jwt.MapClaims, issuer string) error {
	if exp, err := claims.GetExpirationTime(); err != nil || exp == nil {
		return fmt.Errorf("token has no expiry")
	}
	iss, _ := claims["iss"].(string)
	sub, _ := claims["sub"].(string)
	return AssertIssuerAndSubject(iss, sub, issuer)
}

// AssertIssuerAndSubject requires a token to name the expected issuer and to carry a
// subject. Every verifier here applies both, whichever provider minted the token: the
// issuer is what binds a signature to a trusted key set, and a token with no subject
// identifies nobody.
func AssertIssuerAndSubject(issuer, subject, expectedIssuer string) error {
	if issuer != expectedIssuer {
		return fmt.Errorf("invalid token issuer")
	}
	if subject == "" {
		return fmt.Errorf("token has no subject")
	}
	return nil
}

// AssertTokenUse enforces the token_use claim so an id token cannot be presented where an access
// token is required (and vice versa). id and access tokens are otherwise structurally similar
// (same issuer, sub, signature), so without this check they are interchangeable — an id token,
// which is meant for the client, would be accepted as a resource-server credential. See RFC 9700
// §4 (token substitution) and the id-vs-access separation in docs/en/specs/oidc-oauth2.md.
func AssertTokenUse(claims jwt.MapClaims, want string) error {
	tu, _ := claims["token_use"].(string)
	if tu != want {
		return fmt.Errorf("invalid token_use: token is not a %s token", want)
	}
	return nil
}

// AssertTokenUseOneOf is AssertTokenUse for an extractor that accepts more than one use.
func AssertTokenUseOneOf(tokenUse string, allowed ...string) error {
	if !slices.Contains(allowed, tokenUse) {
		return fmt.Errorf("invalid token use: expected one of %v, got '%s'", allowed, tokenUse)
	}
	return nil
}

// AssertAudience reports whether the token's aud claim (RFC 7519: a string or an array) names one of accepted; an empty aud or an empty accepted list never matches.
func AssertAudience(claims jwt.Claims, accepted ...string) error {
	auds, err := claims.GetAudience()
	if err != nil {
		return fmt.Errorf("failed to read token audience: %w", err)
	}
	for _, a := range auds {
		if a != "" && slices.Contains(accepted, a) {
			return nil
		}
	}
	return fmt.Errorf("invalid token audience")
}

// jwksCache memoizes fetched key sets per URI for the container lifetime; upstream key rotation is
// picked up on cold start.
var (
	jwksMu    sync.Mutex
	jwksCache = map[string]string{}
)

// jwksMaxBodyBytes bounds a key-set response so a hostile or misbehaving issuer cannot stream
// unbounded data into memory.
const jwksMaxBodyBytes = 1 << 20

// FetchJWKS retrieves a key set as raw JSON from jwksURI, cached per URI. Pass a nil client for the
// shared one. Used when a key set is only known by URI rather than pinned as a stored snapshot.
func FetchJWKS(ctx context.Context, jwksURI string, client httpclient.Client) (string, error) {
	jwksMu.Lock()
	cached, ok := jwksCache[jwksURI]
	jwksMu.Unlock()
	if ok {
		return cached, nil
	}

	body, err := httpclient.FetchBounded(ctx, jwksURI, client, jwksMaxBodyBytes)
	if err != nil {
		return "", err
	}

	jwksMu.Lock()
	jwksCache[jwksURI] = string(body)
	jwksMu.Unlock()
	return string(body), nil
}
func RequireAllowedClientID(tokenClientIDs []string, allowedClientIDs []string) error {
	for _, clientID := range tokenClientIDs {
		if clientID != "" && slices.Contains(allowedClientIDs, clientID) {
			return nil
		}
	}
	return fmt.Errorf("no allowed app client named in the token")
}
func RequireSameAuthEvent(accessClaims, idClaims CognitoClaims) error {
	return RequireSameAuthEventValues(
		accessClaims.Subject, idClaims.Subject,
		accessClaims.OriginJTI, idClaims.OriginJTI,
	)
}
func RequireSameAuthEventValues(accessSub, idSub, accessEvent, idEvent string) error {
	if accessSub == "" || idSub == "" {
		return fmt.Errorf("token is missing its sub claim")
	}
	if accessSub != idSub {
		return fmt.Errorf("id_token does not belong to the authenticated user")
	}

	if accessEvent == "" || idEvent == "" {
		return fmt.Errorf("token is missing its origin_jti claim")
	}
	if accessEvent != idEvent {
		return fmt.Errorf("id_token comes from a different sign-in than the access token")
	}

	return nil
}

// TokenFingerprint returns a short, non-reversible identifier for a secret
// (refresh token, access token, API key, ...) that is safe to log. It is the
// leading hex characters of the token's SHA-256 digest, so the same token
// always maps to the same fingerprint — enough to correlate log lines while
// debugging — but the raw secret can never be recovered from it. An empty token
// returns "empty" so the log line stays unambiguous.
const tokenFingerprintLen = 12

func TokenFingerprint(token string) string {
	if token == "" {
		return "empty"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:tokenFingerprintLen]
}
