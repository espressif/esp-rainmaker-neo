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
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/validation"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// OAuthUserAuthService handles authentication for passwordless OIDC users. issuer and jwks are
// resolved once at construction. There is intentionally no single client id: this service verifies
// tokens from every registered client (rm_mobile, va-client, mcp-oauth-client, ...); the per-call
// clientID is passed to the mint/refresh methods.
type OAuthUserAuthService struct {
	issuer string
	jwks   jwk.Set
	// audiences are this API's RFC 8707 names (see GetAcceptedAudiences); an access token must carry one of them.
	audiences []string
}

var _ AuthService = (*OAuthUserAuthService)(nil)

func NewOAuthUserAuthService(ctx context.Context) (*OAuthUserAuthService, error) {
	// Fetch JWKS from SSM Parameter Store
	userPoolJWKS, err := ssmutil.GetParameterWithCaching(ctx, os.Getenv("USER_JWKS_PARA_NAME"), false) // Caching as the info is not expected to change frequently and leads to throttling errors for sign in
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get JWKS from SSM parameter")
	}
	if userPoolJWKS == "" {
		return nil, rmerror.NewRMError(nil, "JWKS value is empty for parameter")
	}

	// Parse JWKS
	keySet, err := jwk.Parse([]byte(userPoolJWKS))
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse JWKS for parameter")
	}

	return &OAuthUserAuthService{
		issuer:    os.Getenv("USER_ISSUER"),
		jwks:      keySet,
		audiences: GetAcceptedAudiences(ctx),
	}, nil
}

// mintTokenSet mints signed access/id tokens (via jwt) plus a fresh refresh-token
// family (via refreshtoken). The two service packages compose symmetrically here.
// sid ties the family to the browser session the login ran under; empty means none.
func (s *OAuthUserAuthService) mintTokenSet(rmngCtx *rmngctx.RmngContext, userID, clientID, scope string, resources []string, audience, sid string, authTime int64) (*UserTokens, error) {
	// OIDC Back-Channel Logout 1.0 s2.7: an offline_access grant is not ended by logout, so it belongs to no session.
	if scopepkg.HasOfflineAccess(scope) {
		sid = ""
	}
	refreshToken, err := refreshtoken.NewService(rmngCtx).MintRefreshtoken(userID, clientID, scope, resources, sid, authTime)
	if err != nil {
		return nil, err
	}
	return s.signTokens(rmngCtx.Context, userID, clientID, scope, audience, refreshToken, sid, authTime)
}

// IDTokenHint is what a verified id_token_hint says.
type IDTokenHint struct {
	Subject  string
	Audience []string
	// SID is the session the token was minted under; empty for a token minted without one.
	SID string
}

// VerifyIDTokenHint verifies one of OUR OWN id tokens presented as an id_token_hint. Signature and issuer are checked; audience is not (the hint may have been issued to any registered client).
//
// EXPIRY IS DELIBERATELY NOT CHECKED. An RP sends the ID token it was last given, which by the
// time anyone clicks "sign out" is routinely past its hour -- a tab left open over lunch is
// enough -- and rejecting on exp made sign-out a silent no-op. Safe because a hint authorizes
// nothing: the cookie is the credential, and the caller still refuses when the verified subject
// is not its owner.
func (s *OAuthUserAuthService) VerifyIDTokenHint(hint string) (*IDTokenHint, error) {
	claims, err := jwtutil.VerifyJWTExpiredOK(hint, s.jwks)
	if err != nil {
		return nil, rmerror.NewRMError(err, "id_token_hint verification failed")
	}
	if iss, _ := claims["iss"].(string); iss != s.issuer {
		return nil, rmerror.NewRMError(nil, "id_token_hint issuer mismatch")
	}
	// An access token carries the same sub and sid and reaches every API the app calls, so only the ID token, which stays with the app, may speak for the session.
	if err := jwtutil.AssertTokenUse(claims, jwtutil.TokenUseID); err != nil {
		return nil, rmerror.NewRMError(err, "id_token_hint is not an ID token")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, rmerror.NewRMError(nil, "id_token_hint has no subject")
	}
	aud, _ := claims.GetAudience()
	sid, _ := claims["sid"].(string)
	return &IDTokenHint{Subject: sub, Audience: aud, SID: sid}, nil
}

// signTokens mints a signed access token and, when openid is in scope, an id token,
// pairing them with the supplied (already-persisted) refresh token.
func (s *OAuthUserAuthService) signTokens(ctx context.Context, userID, clientID, scope, audience, refreshToken, sid string, authTime int64) (*UserTokens, error) {
	minter, err := s.newMinter(ctx)
	if err != nil {
		return nil, err
	}

	contact := s.resolveTokenContact(ctx, userID, scope)

	authEventID := jwtutil.NewAuthEventID()

	accessToken, err := minter.AccessToken(userID, clientID, scope, authEventID, audience, contact, sid)
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
		Scope:        scope,
	}, nil
}

// MintClientCredentialsToken issues an access token for the client itself (RFC 6749 s4.4).
//
// No refresh token, deliberately: a refresh token exists to act for an absent user later,
// and here there is no user -- the client already holds credentials it can present again.
// No id token either; there is no human to describe.
//
// resource is the RFC 8707 resource the token is for, or "" for the client's default; the request is the whole grant, so one resource at most.
func (s *OAuthUserAuthService) MintClientCredentialsToken(ctx context.Context, client *clients.ClientResponse, scope, resource string) (*UserTokens, error) {
	var requested []string
	if resource != "" {
		requested = []string{resource}
	}
	resources, err := authorizedResources(client, requested)
	if err != nil {
		return nil, err
	}
	minter, err := s.newMinter(ctx)
	if err != nil {
		return nil, err
	}
	accessToken, err := minter.ClientCredentialsToken(client.ClientID, scope, resources[0])
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

	match, err := s.accountForContacts(rmngCtx.Context, email, phone)
	// A failed read is not an unheld contact: creating an account on it would split one person into two.
	if err != nil {
		return "", err
	}

	if match != nil {
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

// ErrUpstreamUserIDUnusable refuses a login whose upstream id cannot be adopted. One error for every reason, so the claim cannot be used to learn which ids exist.
var ErrUpstreamUserIDUnusable = errors.New("the upstream id cannot be used for this account")

// createUser opens an account under the upstream sub when the provider row maps it, otherwise under a minted id.
func (s *OAuthUserAuthService) createUser(rmngCtx *rmngctx.RmngContext, email, phone string, profile *user_details_db.UpstreamProfile) (string, error) {
	userID := ids.NewUserID()
	upstreamSub := ""
	if profile != nil && profile.Sub != "" {
		upstreamSub = profile.Sub
		if !isValidSubject(upstreamSub) {
			return "", rmerror.NewRMError(ErrUpstreamUserIDUnusable, fmt.Sprintf("%s named %q, which cannot be an OIDC subject", profile.Provider, upstreamSub))
		}
		userID = upstreamSub
	}

	// Uniqueness comes from CreateUserDetails' conditional write, not the lookups above.
	err := user_details_db.NewUserDetailsDB(rmngCtx).CreateUserDetails(newUserEntry(userID, email, phone, profile))
	if err == nil {
		return userID, nil
	}

	// No contact matched, so nothing shows this login owns the account already under that id.
	if upstreamSub != "" && errors.Is(err, user_details_db.ErrUserIDTaken) {
		return "", rmerror.NewRMError(ErrUpstreamUserIDUnusable, fmt.Sprintf("%s named %q, which is already taken", profile.Provider, upstreamSub))
	}
	return "", rmerror.NewRMError(err, "failed to JIT-create oauth user")
}

func newUserEntry(userID, email, phone string, profile *user_details_db.UpstreamProfile) *user_details_db.UserDetailsEntry {
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
		entry.ExternalSub = profile.ExternalSub
		entry.Name = profile.Name
		entry.Locale = profile.Locale
		entry.Picture = profile.Picture
	}
	return entry
}

// isValidSubject applies OIDC Core §2: a sub is 1 to 255 ASCII characters. Control characters and spaces are refused too, since the id lands in keys and URLs.
func isValidSubject(id string) bool {
	if id == "" || len(id) > 255 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] <= ' ' || id[i] > '~' {
			return false
		}
	}
	return true
}

// RefreshToken rotates the opaque refresh token (a replayed spent one revokes the whole family); resource narrows the renewed access token to one of the sign-in's resources (RFC 8707 s2.2).
func (s *OAuthUserAuthService) RefreshToken(ctx context.Context, client *clients.ClientResponse, refreshToken, resource string) (*UserTokens, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	refresh := refreshtoken.NewService(rmngCtx)

	// Rotate's precheck runs before the counter advances, so a refused audience leaves the login usable.
	var audience string
	rotation, err := refresh.Rotate(client.ClientID, refreshToken, func(family *refresh_tokens_db.FamilyEntry) error {
		// Taking offline_access from a client stops its grants at their next renewal instead of a year later.
		if family.IsOffline() && !client.AllowsScopes(scopepkg.OfflineAccess) {
			return ErrInvalidGrant
		}
		// A family minted before resources were recorded has no audience to renew into, so that login starts over.
		if len(family.Resources) == 0 {
			return ErrInvalidGrant
		}
		var err error
		audience, err = SetTokenAudienceFromResource(client, family.Resources, resource)
		return err
	})
	if err != nil {
		return nil, err
	}
	// sid and auth_time are the family's, so a renewal still names the original session and sign-in.
	return s.signTokens(ctx, rotation.UserID, client.ClientID, rotation.Scope, audience, rotation.Token, rotation.SID, rotation.AuthTime)
}

// accountForContacts returns the account the verified contacts name, nil when no account holds either, and ErrContactsOnDifferentUsers when they name two; sign-in and a password reset both resolve through it, so they always reach the same account.
func (s *OAuthUserAuthService) accountForContacts(ctx context.Context, email, phone string) (*user_details_db.UserContactEntry, error) {
	var byEmail, byPhone *user_details_db.UserContactEntry
	var err error
	if email != "" {
		// ValidateEmail reads a mixed-case address as a phone, so it is lowercased before findContact picks the index.
		if byEmail, err = s.findContact(ctx, ids.FormatUsername(email)); err != nil {
			return nil, err
		}
	}
	if phone != "" {
		if byPhone, err = s.findContact(ctx, phone); err != nil {
			return nil, err
		}
	}
	if byEmail != nil && byPhone != nil && byEmail.UserID != byPhone.UserID {
		// Picking either account would hand one person's account to the other.
		return nil, ErrContactsOnDifferentUsers
	}
	if byEmail != nil {
		return byEmail, nil
	}
	return byPhone, nil
}

// UserIDByVerifiedContacts resolves contacts as ResolveOrCreateUserByContacts does but never creates an account: "" with a nil error when no account holds either contact.
func (s *OAuthUserAuthService) UserIDByVerifiedContacts(ctx context.Context, email, phone string) (string, error) {
	owner, err := s.accountForContacts(ctx, email, phone)
	if err != nil || owner == nil {
		return "", err
	}
	return owner.UserID, nil
}

// findContact reads the contact index the value belongs to; (nil, nil) means no account holds it.
func (s *OAuthUserAuthService) findContact(ctx context.Context, emailOrPhone string) (*user_details_db.UserContactEntry, error) {
	userDetailsDB := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(ctx, nil))
	var entry *user_details_db.UserContactEntry
	var err error
	if validation.ValidateEmail(emailOrPhone) {
		entry, err = userDetailsDB.LookupContactByEmail(emailOrPhone)
	} else {
		entry, err = userDetailsDB.LookupContactByPhoneNumber(emailOrPhone)
	}
	if errors.Is(err, user_details_db.ErrUserNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to look up the account holding a contact")
	}
	if entry == nil || entry.UserID == "" {
		return nil, nil
	}
	return entry, nil
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

// AccountLabel is how the consent and sign-out screens name the signed-in person: their email, else their phone.
func (s *OAuthUserAuthService) AccountLabel(ctx context.Context, userID string) (string, error) {
	user, err := s.lookupUserByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user.Email != "" {
		return user.Email, nil
	}
	return user.PhoneNumber, nil
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
		return fmt.Errorf("%w: %w", jwtutil.ErrInvalidAccessToken, err)
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

// FirstPartyClientIDs lists the OAuth clients that are our own apps rather than third parties that authenticate our users, from the comma-separated USER_CLIENT_ID that CDK builds with first_party_client_ids() in base_res_constants.py.
func FirstPartyClientIDs() []string {
	clientIDs := make([]string, 0)
	for _, id := range strings.Split(os.Getenv("USER_CLIENT_ID"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			clientIDs = append(clientIDs, id)
		}
	}
	return clientIDs
}

// VerifyAccessToken verifies one of our own user access tokens addressed to this API, whose aud must be one of its names (RFC 9068 s4).
func (s *OAuthUserAuthService) VerifyAccessToken(token string) (TokenClaims, error) {
	claims, err := s.verifyUserAccessToken(token)
	if err != nil {
		return TokenClaims{}, err
	}
	if len(s.audiences) == 0 {
		return TokenClaims{}, rmerror.NewRMError(nil, "this API has no names to accept, so no access token is accepted")
	}
	if err := jwtutil.AssertAudience(claims, s.audiences...); err != nil {
		return TokenClaims{}, rmerror.NewRMError(err, "access token is not addressed to this API")
	}
	return claims, nil
}

// VerifyUserTokenForAnyAPI verifies one of our user access tokens whatever API it is addressed to, for the ESP User API's own endpoints, which serve the signed-in account rather than one API: /oauth2/userinfo (OIDC Core s5.3), /v1/users/*, /v1/user/sessions, /v1/user/connected-apps and the legacy password change.
func (s *OAuthUserAuthService) VerifyUserTokenForAnyAPI(token string) (TokenClaims, error) {
	return s.verifyUserAccessToken(token)
}

// verifyUserAccessToken verifies one of our own user access tokens. An id token is refused (RFC 9700 token substitution), as is a client_credentials token: its subject is a client id, not a person.
func (s *OAuthUserAuthService) verifyUserAccessToken(token string) (TokenClaims, error) {
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

// RevokeRefreshToken revokes the presented token's whole family (RFC 7009 s2.1) and returns refresh_tokens_db.ErrRefreshClientMismatch for a token issued to another client.
// An invalid token is a no-op so the endpoint stays a non-oracle 200 (RFC 7009 s2.2); a storage failure is returned so the client keeps the token and retries (s2.2.1).
func (s *OAuthUserAuthService) RevokeRefreshToken(ctx context.Context, clientID, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	err := signout.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).RevokeFamily(clientID, refreshToken)
	if errors.Is(err, refresh_tokens_db.ErrRefreshInvalid) {
		rlog.Warn(ctx).Err(err).Msg("Revoke no-op for an invalid refresh token")
		return nil
	}
	return err
}
