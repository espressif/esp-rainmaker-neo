// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package auth - passwordless OAuth/OIDC user authentication service.
//
// OAuth users are the standalone-IdP (ESP User) identities that authenticate via
// email/phone OTP or social federation, NOT Cognito. They have no password, so
// every password-shaped operation is unsupported here; identities live in
// espuser-user-details and tokens are minted/rotated against espuser-refresh-tokens.
// Spec: espuser/docs/en/specs/auth-flows.md.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	scopepkg "github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/validation"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// errPasswordlessUnsupported is returned by the password-shaped operations that
// do not apply to passwordless OAuth users (they authenticate via OTP/social).
var errPasswordlessUnsupported = fmt.Errorf("operation not supported for passwordless oauth users")

// OAuthUserAuthService handles authentication for passwordless OIDC users. issuer and jwks are
// resolved once at construction. There is intentionally no single client id: this service verifies
// tokens from every registered client (rm_mobile, va-client, mcp-oauth-client, ...); the per-call
// clientID is passed to the mint/refresh methods.
type OAuthUserAuthService struct {
	issuer string
	jwks   jwk.Set
}

var _ AuthService = (*OAuthUserAuthService)(nil)

func NewOAuthUserAuthService(ctx context.Context) (*OAuthUserAuthService, error) {
	// Fetch JWKS from SSM Parameter Store
	userPoolJWKS, err := ssmutil.GetParameterWithCaching(ctx, os.Getenv("USER_JWKS_PARA_NAME"), false) // Caching as the info is not expected to change frequently and leads to throttling errors for sign in
	if err != nil {
		return nil, rmerror.NewRMError(err, fmt.Sprintf("failed to get JWKS from SSM parameter"))
	}
	if userPoolJWKS == "" {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("JWKS value is empty for parameter"))
	}

	// Parse JWKS
	keySet, err := jwk.Parse([]byte(userPoolJWKS))
	if err != nil {
		return nil, rmerror.NewRMError(err, fmt.Sprintf("failed to parse JWKS for parameter"))
	}

	return &OAuthUserAuthService{issuer: os.Getenv("USER_ISSUER"), jwks: keySet}, nil
}

// mintTokenSet mints signed access/id tokens (via jwt) plus a fresh refresh-token
// family (via refreshtoken). The two service packages compose symmetrically here.
// sid ties the family to the browser session the login ran under; empty means none.
func (s *OAuthUserAuthService) mintTokenSet(rmngCtx *rmngctx.RmngContext, userID, clientID, scope, resource, sid string, authTime int64) (*UserTokens, error) {
	refreshToken, err := refreshtoken.NewService(rmngCtx).MintRefreshtoken(userID, clientID, scope, resource, sid, authTime)
	if err != nil {
		return nil, err
	}
	return s.signTokens(rmngCtx.Context, userID, clientID, scope, resource, refreshToken, sid, authTime)
}

// VerifyIDTokenHint verifies one of OUR OWN id tokens presented as an id_token_hint and returns
// its subject. Signature and issuer are checked; audience is not (the hint may have been issued
// to any registered client).
//
// EXPIRY IS DELIBERATELY NOT CHECKED. An RP sends the ID token it was last given, which by the
// time anyone clicks "sign out" is routinely past its hour -- a tab left open over lunch is
// enough -- and rejecting on exp made sign-out a silent no-op. Safe because a hint authorizes
// nothing: the cookie is the credential, and the caller still refuses when the verified subject
// is not its owner.
func (s *OAuthUserAuthService) VerifyIDTokenHint(hint string) (sub string, aud []string, err error) {
	claims, err := jwtutil.VerifyJWTExpiredOK(hint, s.jwks)
	if err != nil {
		return "", nil, rmerror.NewRMError(err, "id_token_hint verification failed")
	}
	if iss, _ := claims["iss"].(string); iss != s.issuer {
		return "", nil, rmerror.NewRMError(nil, "id_token_hint issuer mismatch")
	}
	sub, _ = claims["sub"].(string)
	if sub == "" {
		return "", nil, rmerror.NewRMError(nil, "id_token_hint has no subject")
	}
	aud, _ = claims.GetAudience()
	return sub, aud, nil
}

// signTokens mints a signed access token and, when openid is in scope, an id token,
// pairing them with the supplied (already-persisted) refresh token.
func (s *OAuthUserAuthService) signTokens(ctx context.Context, userID, clientID, scope, resource, refreshToken, sid string, authTime int64) (*UserTokens, error) {
	minter, err := s.newMinter(ctx)
	if err != nil {
		return nil, err
	}

	contact := s.resolveTokenContact(ctx, userID, scope)

	authEventID := jwtutil.NewAuthEventID()

	// resource is the access token's audience; empty leaves it as the client id.
	accessToken, err := minter.AccessToken(userID, clientID, scope, authEventID, resource, contact, sid)
	if err != nil {
		return nil, err
	}

	// The ID token's audience is deliberately NOT the resource. An ID token is a statement
	// to this client about who signed in, so the client genuinely is its audience -- fixing
	// the access token must not "fix" this one.
	var idToken string
	if scopepkg.HasOpenID(scope) {
		if idToken, err = minter.IDToken(userID, clientID, authEventID, authTime, contact, sid); err != nil {
			return nil, err
		}
	}

	return &UserTokens{
		AccessToken:  accessToken,
		IDToken:      idToken,
		RefreshToken: refreshToken,
		TokenType:    oidc.TokenTypeBearer,
		ExpiresIn:    int(jwtutil.AccessTokenTTL.Seconds()),
	}, nil
}

// MintClientCredentialsToken issues an access token for the client itself (RFC 6749 s4.4).
//
// No refresh token, deliberately: a refresh token exists to act for an absent user later,
// and here there is no user -- the client already holds credentials it can present again.
// No id token either; there is no human to describe.
//
// resource is the RFC 8707 identifier of the API the token is for. The caller has already
// checked the client may target it; empty leaves the audience as the client id.
func (s *OAuthUserAuthService) MintClientCredentialsToken(ctx context.Context, clientID, scope, resource string) (*UserTokens, error) {
	minter, err := s.newMinter(ctx)
	if err != nil {
		return nil, err
	}
	accessToken, err := minter.ClientCredentialsToken(clientID, scope, resource)
	if err != nil {
		return nil, err
	}
	return &UserTokens{
		AccessToken: accessToken,
		TokenType:   oidc.TokenTypeBearer,
		ExpiresIn:   int(jwtutil.AccessTokenTTL.Seconds()),
	}, nil
}

// The kid is read back from the published JWKS rather than from kms:GetPublicKey, which would be a
// paid API call on every mint.
func (s *OAuthUserAuthService) newMinter(ctx context.Context) (*jwtutil.Minter, error) {
	if s.issuer == "" {
		return nil, rmerror.NewRMError(nil, "USER_ISSUER is required to mint tokens")
	}
	keyARN := os.Getenv("ESPUSER_KMS_SIGNING_KEY_ARN")
	if keyARN == "" {
		return nil, rmerror.NewRMError(nil, "ESPUSER_KMS_SIGNING_KEY_ARN is required to mint tokens")
	}
	kid, err := publishedKid(ctx)
	if err != nil {
		return nil, err
	}
	signer, err := kmsutil.NewRSASigner(ctx, keyARN)
	if err != nil {
		return nil, err
	}
	return jwtutil.NewMinter(s.issuer, signer, kid), nil
}

// Takes the last key because the publisher appends, so during a rotation overlap the newest key
// still wins.
func publishedKid(ctx context.Context) (string, error) {
	jwksJSON, err := ssmutil.GetParameterWithCaching(ctx, os.Getenv("USER_JWKS_PARA_NAME"), false)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to load published JWKS for the signing kid")
	}
	var set jwtutil.JWKS
	if err := json.Unmarshal([]byte(jwksJSON), &set); err != nil || len(set.Keys) == 0 {
		return "", rmerror.NewRMError(err, "published JWKS is empty or malformed")
	}
	return set.Keys[len(set.Keys)-1].Kid, nil
}

// resolveTokenContact resolves the subject's contact and returns only the fields the scope authorizes, mirroring ParseUserInfoFromToken's gates. A lookup failure is non-fatal: the token is still minted, just without contact claims.
func (s *OAuthUserAuthService) resolveTokenContact(ctx context.Context, userID, scope string) jwtutil.Contact {
	wantEmail := scopepkg.Has(scope, scopepkg.Email, scopepkg.Profile)
	wantPhone := scopepkg.Has(scope, scopepkg.Phone, scopepkg.Profile)
	wantProfile := scopepkg.Has(scope, scopepkg.Profile)
	if !wantEmail && !wantPhone && !wantProfile {
		return jwtutil.Contact{}
	}

	info, err := s.lookupUserByID(ctx, userID)
	if err != nil {
		rlog.Info(ctx).Err(err).Msg("Contact lookup failed; minting token without contact claims")
		return jwtutil.Contact{}
	}

	var contact jwtutil.Contact
	if wantEmail {
		contact.Email = info.Email
	}
	if wantPhone {
		contact.PhoneNumber = info.PhoneNumber
	}
	if wantProfile {
		contact.Name = info.Name
		contact.Locale = info.Locale
		contact.Picture = info.Picture
	}
	return contact
}

// The contact is what identifies the person, so one verified email reaches the same account whichever
// provider authenticated it. profile carries claims a federated login brought along and may be nil;
// they are stored with the account at creation.
func (s *OAuthUserAuthService) ResolveOrCreateUser(rmngCtx *rmngctx.RmngContext, username string, profile *user_details_db.UpstreamProfile) (string, error) {
	if validation.ValidateEmail(username) {
		return s.ResolveOrCreateUserByContacts(rmngCtx, username, "", profile)
	}
	return s.ResolveOrCreateUserByContacts(rmngCtx, "", username, profile)
}

// ErrContactsOnDifferentUsers means the two contacts already belong to separate accounts. Picking
// one would either strand data or attach this login to the wrong account, so the caller is told.
var ErrContactsOnDifferentUsers = fmt.Errorf("verified contacts resolve to different users")

// ResolveOrCreateUserByContacts resolves a login that vouched for an email, a phone, or both. Either
// contact finds the account, and a contact the account was missing is recorded, so the same person
// stays one user however they sign in next.
func (s *OAuthUserAuthService) ResolveOrCreateUserByContacts(rmngCtx *rmngctx.RmngContext, email, phone string, profile *user_details_db.UpstreamProfile) (string, error) {
	if email == "" && phone == "" {
		return "", rmerror.NewRMError(nil, "a verified email or phone is required")
	}

	var byEmail, byPhone *user_details_db.UserContactEntry
	if email != "" {
		byEmail = s.lookupContact(rmngCtx.Context, email)
	}
	if phone != "" {
		byPhone = s.lookupContact(rmngCtx.Context, phone)
	}

	switch {
	case byEmail != nil && byPhone != nil && byEmail.UserID != byPhone.UserID:
		return "", ErrContactsOnDifferentUsers
	case byEmail != nil || byPhone != nil:
		match := byEmail
		if match == nil {
			match = byPhone
		}
		// Only what the account is missing: the write is conditioned on those attributes being absent,
		// so naming one it already has would fail the whole update.
		addEmail, addPhone := "", ""
		if match.Email == "" {
			addEmail = email
		}
		if match.PhoneNumber == "" {
			addPhone = phone
		}
		if addEmail != "" || addPhone != "" {
			if err := user_details_db.NewUserDetailsDB(rmngCtx).RecordVerifiedContacts(match.UserID, addEmail, addPhone); err != nil {
				return "", rmerror.NewRMError(err, "failed to record the login's contact on the user")
			}
		}
		return match.UserID, nil
	}

	return s.createUser(rmngCtx, email, phone, profile)
}

func (s *OAuthUserAuthService) createUser(rmngCtx *rmngctx.RmngContext, email, phone string, profile *user_details_db.UpstreamProfile) (string, error) {
	userID := ids.NewUserID()
	entry := &user_details_db.UserDetailsEntry{
		UserID:      userID,
		Email:       email,
		PhoneNumber: phone,
		UserType:    user_details_db.UserTypeUser,
		Provider:    user_details_db.ProviderOIDC,
	}
	// Claims are stored with the account, so resolving a user is a single write.
	if profile != nil {
		if profile.Provider != "" {
			entry.Provider = profile.Provider
		}
		entry.Sub = profile.ExternalSub
		entry.Name = profile.Name
		entry.Locale = profile.Locale
		entry.Picture = profile.Picture
	}
	// Uniqueness comes from CreateUserDetails' conditional write, not the lookups above.
	if err := user_details_db.NewUserDetailsDB(rmngCtx).CreateUserDetails(entry); err != nil {
		return "", rmerror.NewRMError(err, "failed to JIT-create oauth user")
	}
	return userID, nil
}

// ErrResourceNoLongerAllowed is a renewal refused because the family's RFC 8707 resource has
// since been taken off the client's allowed_resources. Distinct from every other refresh
// failure because it must not read as invalid_grant: the token is perfectly good, the
// entitlement behind it is not, and the caller's remedy is a fresh authorization without
// that resource rather than another retry.
var ErrResourceNoLongerAllowed = errors.New("the family's resource is no longer allowed for this client")

// RefreshToken rotates the opaque refresh token (rotate-on-use + reuse detection); a replayed spent token revokes the whole family.
func (s *OAuthUserAuthService) RefreshToken(ctx context.Context, clientID, refreshToken string) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	refresh := refreshtoken.NewService(rmngCtx)

	// A registry revocation has to reach tokens already in flight. The resource is stamped on
	// the family at login and replayed on every renewal, so removing an API from
	// allowed_resources must stop the family renewing, not only stop new logins asking for it.
	// This runs as Rotate's precheck -- after the family is loaded, before the counter advances --
	// so a refusal leaves the login untouched and restoring the entitlement resumes it, and the
	// family is read once rather than once here and once inside Rotate.
	rotation, err := refresh.Rotate(clientID, refreshToken, func(family *refresh_tokens_db.FamilyEntry) error {
		if family.Resource == "" {
			return nil
		}
		client, err := clients.NewService(rmngCtx).Get(clientID)
		if err != nil || client == nil || !client.AllowsResource(family.Resource) {
			rlog.Warn(ctx).Str("client_id", clientID).Str("resource", family.Resource).
				Msg("refresh: family resource is no longer in the client's allowed_resources; refusing to renew")
			return ErrResourceNoLongerAllowed
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The family remembers the resource the original login asked for, so a silent renewal
	// stays addressed to the same API instead of quietly reverting to aud = client_id.
	// The rotated token keeps the family's sid, so a refreshed access token still names the
	// session it belongs to and a sessions list stays correct across a refresh.
	// auth_time is the family's, so a refreshed ID token still names the original sign-in.
	return s.signTokens(ctx, rotation.UserID, clientID, rotation.Scope, rotation.Resource, rotation.Token, rotation.SID, rotation.AuthTime)
}

// lookupContact resolves an email/phone to the account holding it, or nil when no account does — a
// contact nobody owns is an ordinary outcome of a first login, not a failure.
func (s *OAuthUserAuthService) lookupContact(ctx context.Context, emailOrPhone string) *user_details_db.UserContactEntry {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	userDetailsDB := user_details_db.NewUserDetailsDB(rmngCtx)

	var entry *user_details_db.UserContactEntry
	var err error
	if validation.ValidateEmail(emailOrPhone) {
		entry, err = userDetailsDB.LookupContactByEmail(emailOrPhone)
	} else {
		entry, err = userDetailsDB.LookupContactByPhoneNumber(emailOrPhone)
	}
	if err != nil || entry == nil || entry.UserID == "" {
		return nil
	}
	return entry
}

// lookupUserByID resolves a UserInfo from a user_id (the token subject).
func (s *OAuthUserAuthService) lookupUserByID(ctx context.Context, userID string) (UserInfo, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	entry, err := user_details_db.NewUserDetailsDB(rmngCtx).GetUserDetailsByUserID(userID)
	if err != nil {
		return UserInfo{}, rmerror.NewRMError(err, "Failed to get oauth user by id")
	}
	if entry == nil || entry.UserID == "" {
		return UserInfo{}, rmerror.NewRMError(nil, "oauth user not found")
	}
	return userInfoFromEntry(entry), nil
}

func userInfoFromEntry(entry *user_details_db.UserDetailsEntry) UserInfo {
	return UserInfo{
		Email:        entry.Email,
		PhoneNumber:  entry.PhoneNumber,
		UserID:       entry.UserID,
		IsSuperAdmin: false,
		IsAdmin:      false,
		Name:         entry.Name,
		Locale:       entry.Locale,
		Picture:      entry.Picture,
	}
}

// GetUserFromProvider resolves the OIDC provider identity — the `sub`, which is the opaque
// user_id — to a user. The subject is treated as opaque and never re-derived from email/phone.
func (s *OAuthUserAuthService) GetUserFromProvider(ctx context.Context, sub string) (UserInfo, error) {
	return s.lookupUserByID(ctx, sub)
}

// GetUserFromProviderUsingToken verifies the RS256 access token and resolves its subject to a user.
func (s *OAuthUserAuthService) GetUserFromProviderUsingToken(ctx context.Context, token string) (UserInfo, error) {
	return s.ParseUserInfoFromToken(ctx, token)
}

// TokenClaims are the claims our own RS256 tokens carry. sub/iss/exp come from the embedded
// RegisteredClaims; email/phone_number are scope-gated at minting (addContact), so what the token
// holds is what the scope authorized.
type TokenClaims struct {
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Picture     string `json:"picture,omitempty"`
	TokenUse    string `json:"token_use,omitempty"`
	// ClientID names the client an access token was minted for; id tokens name it in `aud`.
	ClientID string `json:"client_id,omitempty"`
	// GrantType is the `gty` claim: `client_credentials` on a machine token, absent on a user grant.
	GrantType string `json:"gty,omitempty"`
	// OriginJTI ties an access token to the id token minted in the same sign-in.
	OriginJTI string `json:"origin_jti,omitempty"`
	// Scope is the space-separated grant. Present on every access token we mint.
	Scope string `json:"scope,omitempty"`
	// SID names the browser session this token was minted under; absent when the login
	// established none.
	SID string `json:"sid,omitempty"`
	jwtgo.RegisteredClaims
}

func (s *OAuthUserAuthService) verifyOwnToken(token, wantTokenUse string) (TokenClaims, error) {
	if s.jwks == nil {
		return TokenClaims{}, rmerror.NewRMError(nil, "JWKS not loaded for token verification")
	}
	var claims TokenClaims
	if err := jwtutil.VerifyJWTInto(token, s.jwks, &claims); err != nil {
		return TokenClaims{}, err
	}
	if err := jwtutil.AssertIssuerAndSubject(claims.Issuer, claims.Subject, s.issuer); err != nil {
		return TokenClaims{}, err
	}
	if claims.TokenUse != wantTokenUse {
		return TokenClaims{}, rmerror.NewRMError(nil, fmt.Sprintf("invalid token_use: token is not a %s token", wantTokenUse))
	}
	return claims, nil
}

// VerifyTokenPair verifies the two halves of one sign-in and requires both to name the first-party client. The voice-assistant and MCP audiences are delegated to third parties, so a pair minted for one of them must not be exchanged for credentials, whatever else it is allowed to do. Pinned here rather than in verifyOwnToken because those audiences may legitimately call the ordinary APIs; they just may not vend credentials.
func (s *OAuthUserAuthService) VerifyTokenPair(ctx context.Context, accessToken, idToken string) error {
	allowedClientIDs := FirstPartyClientIDs()
	if len(allowedClientIDs) == 0 {
		return rmerror.NewRMError(nil, "USER_CLIENT_ID is required to pin the token pair's audience")
	}

	accessClaims, err := s.VerifyAccessToken(accessToken)
	if err != nil {
		return rmerror.NewRMError(err, "access token failed validation")
	}
	if err := jwtutil.RequireAllowedClientID([]string{accessClaims.ClientID}, allowedClientIDs); err != nil {
		return rmerror.NewRMError(err, "access token was issued for a different app client")
	}
	idClaims, err := s.VerifyIDToken(idToken)
	if err != nil {
		return rmerror.NewRMError(err, "id_token failed validation")
	}
	if err := jwtutil.RequireAllowedClientID(idClaims.Audience, allowedClientIDs); err != nil {
		return rmerror.NewRMError(err, "id_token was issued for a different app client")
	}
	return jwtutil.RequireSameAuthEventValues(
		accessClaims.Subject, idClaims.Subject,
		accessClaims.OriginJTI, idClaims.OriginJTI,
	)
}

// ParseUserInfoFromToken verifies our RS256 token and maps its claims to a UserInfo. The token
// already carries the (scope-gated) sub/email/phone we minted, so no user-details lookup is needed.
// This is a resource-server path, so only an access token is accepted: an id token (meant for the
// client) must not be replayable here (RFC 9700 token substitution).
func (s *OAuthUserAuthService) ParseUserInfoFromToken(ctx context.Context, token string) (UserInfo, error) {
	claims, err := s.VerifyAccessToken(token)
	if err != nil {
		return UserInfo{}, err
	}
	return claims.UserInfo(), nil
}

func (c TokenClaims) UserInfo() UserInfo {
	return UserInfo{
		UserID:      c.Subject,
		Sub:         c.Subject,
		Email:       c.Email,
		PhoneNumber: c.PhoneNumber,
		Name:        c.Name,
		Locale:      c.Locale,
		Picture:     c.Picture,
	}
}

func (c TokenClaims) IsMachine() bool {
	return c.GrantType == oidc.GrantClientCredentials
}

// FirstPartyClientIDs is the set of OAuth clients that ARE this account -- the web dashboard, the mobile app, any surface we ship -- as opposed to third parties that authenticate our users but are not us. Comma-separated in USER_CLIENT_ID so a new first-party app is added by config, not code: one id today, "web,mobile" tomorrow.
func FirstPartyClientIDs() []string {
	clientIDs := make([]string, 0)
	for _, id := range strings.Split(os.Getenv("USER_CLIENT_ID"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			clientIDs = append(clientIDs, id)
		}
	}
	return clientIDs
}

// VerifyAccessToken verifies one of our own user access tokens. An id token is refused (RFC 9700 token substitution), as is a client_credentials token: its subject is a client id, not a person.
func (s *OAuthUserAuthService) VerifyAccessToken(token string) (TokenClaims, error) {
	claims, err := s.verifyOwnToken(token, jwtutil.TokenUseAccess)
	if err != nil {
		return TokenClaims{}, err
	}
	if claims.IsMachine() {
		return TokenClaims{}, rmerror.NewRMError(nil, "client_credentials token cannot act for a user")
	}
	return claims, nil
}

func (s *OAuthUserAuthService) VerifyIDToken(token string) (TokenClaims, error) {
	return s.verifyOwnToken(token, jwtutil.TokenUseID)
}

// RevokeRefreshToken revokes the presented token's whole family (RFC 7009 §2.1: revoking a
// token MAY revoke the underlying grant — here the login's family), which also ends the session.
// Errors are swallowed so the endpoint stays a non-oracle 200 (§2.2). Access tokens are stateless
// RS256 JWTs and cannot be revoked server-side, so the §2.2 SHOULD for access tokens does not apply.
func (s *OAuthUserAuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	if err := refreshtoken.NewService(rmngCtx).RevokeFamily(refreshToken); err != nil {
		rlog.Info(ctx).Err(err).Msg("Revoke no-op (token unknown or malformed)")
	}
	return nil
}
