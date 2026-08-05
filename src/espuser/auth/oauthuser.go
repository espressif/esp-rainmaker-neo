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
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/ids"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/kmsutil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
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

func (s *OAuthUserAuthService) MintTokenSet(ctx context.Context, userID, clientID, scope string) (*UserTokens, error) {
	return s.mintTokenSet(rmngctx.NewRmngContextWithCtx(ctx, nil), userID, clientID, scope)
}

// mintTokenSet mints signed access/id tokens (via jwt) plus a fresh refresh-token
// family (via refreshtoken). The two service packages compose symmetrically here.
func (s *OAuthUserAuthService) mintTokenSet(rmngCtx *rmngctx.RmngContext, userID, clientID, scope string) (*UserTokens, error) {
	refreshToken, err := refreshtoken.NewService(rmngCtx).MintRefreshtoken(userID, clientID, scope)
	if err != nil {
		return nil, err
	}
	return s.signTokens(rmngCtx.Context, userID, clientID, scope, refreshToken)
}

// signTokens mints a signed access token and, when openid is in scope, an id token,
// pairing them with the supplied (already-persisted) refresh token.
func (s *OAuthUserAuthService) signTokens(ctx context.Context, userID, clientID, scope, refreshToken string) (*UserTokens, error) {
	minter, err := s.newMinter(ctx)
	if err != nil {
		return nil, err
	}

	contact := s.resolveTokenContact(ctx, userID, scope)

	authEventID := jwtutil.NewAuthEventID()

	accessToken, err := minter.AccessToken(userID, clientID, scope, authEventID, contact)
	if err != nil {
		return nil, err
	}

	var idToken string
	if scopepkg.HasOpenID(scope) {
		if idToken, err = minter.IDToken(userID, clientID, authEventID, contact); err != nil {
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
		UserID:       userID,
		Email:        email,
		PhoneNumber:  phone,
		UserType:     user_details_db.UserTypeUser,
		Provider:     user_details_db.ProviderOIDC,
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

// RefreshToken rotates the opaque refresh token (rotate-on-use + reuse detection); a replayed spent token revokes the whole family.
func (s *OAuthUserAuthService) RefreshToken(ctx context.Context, clientID, refreshToken string) (*UserTokens, error) {
	rotation, err := refreshtoken.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Rotate(clientID, refreshToken)
	if err != nil {
		return nil, err
	}
	return s.signTokens(ctx, rotation.UserID, clientID, rotation.Scope, rotation.Token)
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

// VerifyToken validates the RS256 token against the issuer's JWKS only (no user-details lookup).
func (s *OAuthUserAuthService) VerifyToken(ctx context.Context, token string) error {
	_, err := s.ParseUserInfoFromToken(ctx, token)
	return err
}

// oidcTokenClaims are the claims our own RS256 tokens carry. sub/iss/exp come from the embedded
// RegisteredClaims; email/phone_number are scope-gated at minting (addContact), so what the token
// holds is what the scope authorized.
type oidcTokenClaims struct {
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Name        string `json:"name,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Picture     string `json:"picture,omitempty"`
	TokenUse    string `json:"token_use,omitempty"`
	// OriginJTI ties an access token to the id token minted in the same sign-in.
	OriginJTI string `json:"origin_jti,omitempty"`
	jwtgo.RegisteredClaims
}

func (s *OAuthUserAuthService) verifyOwnToken(token, wantTokenUse string) (oidcTokenClaims, error) {
	if s.jwks == nil {
		return oidcTokenClaims{}, rmerror.NewRMError(nil, "JWKS not loaded for token verification")
	}
	var claims oidcTokenClaims
	if err := jwtutil.VerifyJWTInto(token, s.jwks, &claims); err != nil {
		return oidcTokenClaims{}, err
	}
	if err := jwtutil.AssertIssuerAndSubject(claims.Issuer, claims.Subject, s.issuer); err != nil {
		return oidcTokenClaims{}, err
	}
	if claims.TokenUse != wantTokenUse {
		return oidcTokenClaims{}, rmerror.NewRMError(nil, fmt.Sprintf("invalid token_use: token is not a %s token", wantTokenUse))
	}
	return claims, nil
}

func (s *OAuthUserAuthService) VerifyTokenPair(ctx context.Context, accessToken, idToken string) error {
	accessClaims, err := s.verifyOwnToken(accessToken, jwtutil.TokenUseAccess)
	if err != nil {
		return rmerror.NewRMError(err, "access token failed validation")
	}
	idClaims, err := s.verifyOwnToken(idToken, jwtutil.TokenUseID)
	if err != nil {
		return rmerror.NewRMError(err, "id_token failed validation")
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
	claims, err := s.verifyOwnToken(token, jwtutil.TokenUseAccess)
	if err != nil {
		return UserInfo{}, err
	}
	// Every claim the token carries is reflected: the scope gate was applied when it was minted, so
	// dropping one here would hide a claim the client was granted.
	return UserInfo{
		UserID:      claims.Subject,
		Sub:         claims.Subject,
		Email:       claims.Email,
		PhoneNumber: claims.PhoneNumber,
		Name:        claims.Name,
		Locale:      claims.Locale,
		Picture:     claims.Picture,
	}, nil
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
