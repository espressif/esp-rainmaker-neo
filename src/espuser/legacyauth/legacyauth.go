// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package legacyauth serves the native /v1/user/auth/* password APIs. Cognito authenticates, but the
// client only ever sees ESP User's own tokens. Spec: espuser/docs/en/specs/legacy-user-auth.md.
//
// A compatibility surface only — ROPC-shaped, deprecated by RFC 9700 §2.4 — so new clients use the
// OIDC browser flow or OTP.
package legacyauth

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/cognitoutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/golang-jwt/jwt/v5"
)

// Reuses the seeded first-party client so legacy tokens are not a distinct audience.
const legacyClientID = "user-pool-client"

const legacyScope = "openid email phone"

type Service struct {
	cognito *cognitoutil.CognitoService
	oauth   *auth.OAuthUserAuthService
}

// ErrNoPasswordProvider means no enabled provider advertises a password client, so this surface has
// nothing to authenticate against.
var ErrNoPasswordProvider = errors.New("legacyauth: no enabled provider offers a password grant")

// NewService resolves the password app client from the provider registry, so the upstream may live in
// any account and needs no deployment wiring here.
func NewService(ctx context.Context) (*Service, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	entries, err := identity_providers_db.NewIdentityProvidersDB(rmngCtx).ListEnabled()
	if err != nil {
		return nil, rmerror.NewRMError(err, "legacyauth: failed to read the provider registry")
	}
	var provider *identity_providers_db.ProviderEntry
	for i := range entries {
		if entries[i].OffersPasswordGrant() {
			provider = &entries[i]
			break
		}
	}
	if provider == nil {
		return nil, ErrNoPasswordProvider
	}
	// The app client is confidential, so Cognito demands SECRET_HASH on every call below.
	oauth, err := auth.NewOAuthUserAuthService(ctx)
	if err != nil {
		return nil, rmerror.NewRMError(err, "legacyauth: failed to build token issuer")
	}
	return &Service{
		// The provider's pool may live in another region than this deployment; InitiateAuth must
		// reach that region's Cognito endpoint, so the client is built for the issuer's region.
		cognito: cognitoutil.NewAppClientServiceForRegion(provider.ClientID, provider.ClientSecret, regionFromCognitoIssuer(provider.Issuer)),
		oauth:   oauth,
	}, nil
}

// regionFromCognitoIssuer extracts the AWS region from a Cognito issuer URL of the form
// https://cognito-idp.<region>.amazonaws.com/<poolId>. Returns "" for any other issuer shape, which
// falls back to the deployment-region client.
func regionFromCognitoIssuer(issuer string) string {
	const marker = "cognito-idp."
	i := strings.Index(issuer, marker)
	if i < 0 {
		return ""
	}
	rest := issuer[i+len(marker):]
	if j := strings.Index(rest, "."); j > 0 {
		return rest[:j]
	}
	return ""
}

func (s *Service) SigninWithPassword(ctx context.Context, username, password string) (*auth.UserTokens, error) {
	if username == "" || password == "" {
		return nil, rmerror.NewRMError(nil, "username and password are required")
	}
	login, err := s.cognito.InitiateAuth(ctx, &cognitoutil.LoginRequest{Username: username, Password: password})
	if err != nil || !login.Success {
		return nil, rmerror.NewRMError(err, "authentication failed")
	}
	email, phone, err := verifiedContacts(login.IDToken)
	if err != nil {
		return nil, err
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	userID, err := s.oauth.ResolveOrCreateUserByContacts(rmngCtx, email, phone, loginProfile(login.IDToken))
	if err != nil {
		return nil, err
	}
	return s.oauth.MintTokenSet(ctx, userID, legacyClientID, legacyScope)
}

func (s *Service) RefreshLegacy(ctx context.Context, refreshToken string) (*auth.UserTokens, error) {
	return s.oauth.RefreshToken(ctx, legacyClientID, refreshToken)
}

// Signup registers the credential with the provider. No user_id is returned: ids are opaque and are
// minted with the account, which happens once the contact is verified and first signs in — so any id
// offered here would be one the account never has.
func (s *Service) Signup(ctx context.Context, email, phone, password string) error {
	username := email
	if username == "" {
		username = phone
	}
	if username == "" || password == "" {
		return rmerror.NewRMError(nil, "email or phone, and password, are required")
	}
	if _, err := s.cognito.SignUp(ctx, username, email, phone, password); err != nil {
		if auth.IsUserAlreadyExistsError(err) {
			// Confirmed accounts get no code (the provider refuses the resend);
			// dropped either way so the response never says the account exists.
			_ = s.cognito.ResendSignupCode(ctx, username)
			return nil
		}
		return rmerror.NewRMError(err, "signup failed")
	}
	return nil
}

func (s *Service) VerifySignup(ctx context.Context, username, code string) error {
	if err := s.cognito.ConfirmSignUp(ctx, username, code); err != nil {
		// Unknown user fails exactly like a wrong code, so the answer never says
		// whether the account exists.
		return rmerror.NewRMError(err, "verification failed")
	}
	return nil
}

func (s *Service) ForgotPassword(ctx context.Context, username string) error {
	if err := s.cognito.ForgotPassword(ctx, username); err != nil {
		if auth.IsUserNotExistsError(err) {
			return nil // non-enumerating
		}
		return rmerror.NewRMError(err, "password recovery failed")
	}
	return nil
}

func (s *Service) ConfirmForgotPassword(ctx context.Context, username, code, newPassword string) error {
	if err := s.cognito.ConfirmForgotPassword(ctx, username, code, newPassword); err != nil {
		return rmerror.NewRMError(err, "password recovery confirmation failed")
	}
	return nil
}

// ChangePassword re-authenticates with the old password to obtain a provider access token, because
// the client holds our tokens and the provider will not accept one. The caller's own access token is
// verified first: it names whose password is being changed, so an unverified one would let anyone
// repassword anyone.
func (s *Service) ChangePassword(ctx context.Context, accessToken, oldPassword, newPassword string) error {
	if accessToken == "" || oldPassword == "" || newPassword == "" {
		return rmerror.NewRMError(nil, "access token, old password and new password are required")
	}
	info, err := s.oauth.ParseUserInfoFromToken(ctx, accessToken)
	if err != nil {
		return rmerror.NewRMError(err, "legacyauth: access token rejected")
	}
	username := info.Email
	if username == "" {
		username = info.PhoneNumber
	}
	if username == "" {
		return rmerror.NewRMError(nil, "token carries no contact to identify the provider account")
	}

	login, err := s.cognito.InitiateAuth(ctx, &cognitoutil.LoginRequest{Username: username, Password: oldPassword})
	if err != nil {
		return rmerror.NewRMError(err, "legacyauth: old password rejected")
	}
	if err := s.cognito.ChangePassword(ctx, login.AccessToken, oldPassword, newPassword); err != nil {
		return rmerror.NewRMError(err, "legacyauth: password change rejected")
	}
	return nil
}

// ErrGlobalSignoutUnsupported is returned for an all-devices signout. Our sessions are refresh
// families of ours, which a provider-side sign-out does not touch, so honouring the request would
// require revoking every family — deliberately not done. Reporting it beats a false success.
var ErrGlobalSignoutUnsupported = errors.New("legacyauth: all-device signout is not supported")

func (s *Service) Signout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return refreshtoken.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).RevokeFamily(refreshToken)
}

// Read unverified because we obtained this token ourselves
func loginProfile(idToken string) *user_details_db.UpstreamProfile {
	parsed, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil
	}
	claims, _ := parsed.Claims.(jwt.MapClaims)
	profile := user_details_db.UpstreamProfile{}
	profile.ExternalSub, _ = claims["sub"].(string)
	profile.Name, _ = claims["name"].(string)
	profile.Locale, _ = claims["locale"].(string)
	profile.Picture, _ = claims["picture"].(string)
	if profile == (user_details_db.UpstreamProfile{}) {
		return nil
	}
	return &profile
}

func verifiedContacts(idToken string) (email, phone string, err error) {
	parsed, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return "", "", rmerror.NewRMError(err, "legacyauth: unparseable id_token")
	}
	claims, _ := parsed.Claims.(jwt.MapClaims)
	rawEmail, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	rawPhone, _ := claims["phone_number"].(string)
	phoneVerified, _ := claims["phone_number_verified"].(bool)
	return idp.Identity{
		Email:         rawEmail,
		EmailVerified: emailVerified,
		PhoneNumber:   rawPhone,
		PhoneVerified: phoneVerified,
	}.VerifiedContacts()
}
