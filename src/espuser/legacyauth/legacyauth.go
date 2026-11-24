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
	"fmt"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/cognitoutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/golang-jwt/jwt/v5"
)

// The RainMaker Home app's seeded client (HOME_APP_CLIENT_ID in base_res_constants.py), so the app's password sign-ins keep renewing once it signs in through the browser with the same client.
const legacyClientID = "rainmaker-neo-home-app-client"

const legacyScope = "openid email phone"

// challengeNewPasswordRequired is Cognito's name for "this account has never set its own password".
const challengeNewPasswordRequired = "NEW_PASSWORD_REQUIRED"

type Service struct {
	cognito *cognitoutil.CognitoService
	oauth   *auth.OAuthUserAuthService

	providerName string
	// Read like the browser flow's, so both logins of one person reach the same account.
	attributeMapping map[string]string
}

// IsRainMakerProvider reports whether the resolved provider is RainMaker's own, so user-facing
// messages may name the product.
func (s *Service) IsRainMakerProvider() bool {
	return strings.EqualFold(s.providerName, "rainmaker")
}

// ErrNoPasswordProvider means no enabled provider advertises a password client, so this surface has
// nothing to authenticate against.
var ErrNoPasswordProvider = errors.New("legacyauth: no enabled provider has a password our page can check")

// ErrProviderNotFound refuses a named provider whose password our page cannot check, rather than falling back to another pool and another account.
var ErrProviderNotFound = errors.New("legacyauth: named provider is not one whose password our page can check")
var ErrUserNotConfirmed = errors.New("legacyauth: account is not confirmed")

// ErrNewPasswordRequired means the password was right but an admin created the account and it must choose its own.
var ErrNewPasswordRequired = errors.New("legacyauth: the account must choose a new password")

// ErrPasswordResetRequired means the password was right but an admin reset the account, so it must set a new one through forgot password.
var ErrPasswordResetRequired = errors.New("legacyauth: the account must reset its password")

// ErrSecondStepRequired means the password was right but the pool asks for a step this surface cannot take, such as an MFA code.
var ErrSecondStepRequired = errors.New("legacyauth: the pool asks for a second step")

// What a refused password sign-in tells the person, shared by the native API and the sign-in page so both say what to do next.
const (
	UnconfirmedAccountMessage    = "Signin failed. Account not verified — reset your password."
	NewPasswordRequiredMessage   = "Signin failed. This account needs a new password — contact support."
	PasswordResetRequiredMessage = "Signin failed. Password reset required — reset your password."
	InvalidCredentialsMessage    = "Signin failed. Check your email and password, or reset your password."
	ProviderNotAllowedMessage    = "This application does not allow signing in with that provider."
)

// ErrAuthenticationFailed is the credential being refused, deliberately without saying why, so the form cannot test whether an address is registered.
var ErrAuthenticationFailed = errors.New("legacyauth: authentication failed")

// ErrPasswordRefused means the pool's password policy refused a new password, which the caller can fix, unlike the failures each route answers with one sentence.
var ErrPasswordRefused = errors.New("legacyauth: the password does not meet the pool's policy")

// ErrSessionsNotEnded means the provider changed the password but the account's sessions could not all be signed out, so the caller must not report the change as failed.
var ErrSessionsNotEnded = errors.New("legacyauth: password changed but the sessions were not signed out")

// ErrAccountAlreadyUsable means a duplicate signup authenticated an existing confirmed account, so
// the caller owns it and should sign in rather than wait for a verification code. Not a failure —
// the handler still answers 201, only with a different message.
var ErrAccountAlreadyUsable = errors.New("legacyauth: account already exists and is usable")

// NewService resolves the password app client from the provider registry; an empty providerName takes the first by name.
func NewService(ctx context.Context, providerName string) (*Service, error) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	entries, err := identity_providers_db.NewIdentityProvidersDB(rmngCtx).ListEnabled()
	if err != nil {
		return nil, rmerror.NewRMError(err, "legacyauth: failed to read the provider registry")
	}
	var provider *identity_providers_db.ProviderEntry
	for i := range entries {
		if entries[i].OurPageTakesPassword() && (providerName == "" || entries[i].ProviderName == providerName) {
			provider = &entries[i]
			break
		}
	}
	switch {
	case provider == nil && providerName != "":
		return nil, ErrProviderNotFound
	case provider == nil:
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
		cognito:          cognitoutil.NewAppClientServiceForRegion(provider.ClientID, provider.ClientSecret, regionFromCognitoIssuer(provider.Issuer)),
		oauth:            oauth,
		providerName:     provider.ProviderName,
		attributeMapping: provider.AttributeMapping,
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

// SigninWithPassword covers every resource the password client may request, since this API takes no resource; a refresh narrows the access token to one of them.
func (s *Service) SigninWithPassword(ctx context.Context, username, password string) (*auth.UserTokens, error) {
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(legacyClientID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "legacyauth: password client unreadable")
	}
	// Checked before the password reaches the pool, as the sign-in page does, so a client limited to other providers cannot be signed in through this one.
	if !client.AllowsProvider(s.providerName) {
		return nil, auth.ErrProviderNotAllowed
	}
	userID, err := s.Authenticate(ctx, username, password)
	if err != nil {
		return nil, err
	}
	// A cookie-less (app) user agent, so this legacy login is listed on the user agent screen and its
	// families are reachable by sid, even though a ROPC client holds no browser and gets no SSO.
	// auth_time is now (Establish defaults it): the authentication happened here, not upstream.
	return s.oauth.MintTokenSetForClient(ctx, userID, client, legacyScope, auth.FirstPartyLogin{
		AllAllowedResources: true,
		Provider:            s.providerName,
		AMR:                 []string{"pwd"},
		Origin:              session.OriginApp,
	})
}

// SigninToFlow is SigninWithPassword for our sign-in page: it finishes the browser's flow with a code or a consent hold, never tokens, and checks the flow's client allows this provider before the password reaches the pool.
func (s *Service) SigninToFlow(ctx context.Context, flowID, username, password string, login auth.FirstPartyLogin) (redirectTo, setCookie string, err error) {
	flow, err := s.oauth.FlowAllowingProvider(ctx, flowID, s.providerName)
	if err != nil {
		return "", "", err
	}
	userID, err := s.Authenticate(ctx, username, password)
	if err != nil {
		return "", "", err
	}
	login.Provider = s.providerName
	login.AMR = []string{"pwd"}
	return s.oauth.CompleteSignIn(ctx, flow, userID, login)
}

// Authenticate checks the credential and resolves the person, the part the native API and the sign-in page share.
func (s *Service) Authenticate(ctx context.Context, username, password string) (string, error) {
	// A blank field is a failed sign-in like any other, never a server error.
	if username == "" || password == "" {
		return "", ErrAuthenticationFailed
	}
	login, err := s.cognito.InitiateAuth(ctx, &cognitoutil.LoginRequest{Username: username, Password: password})
	if err != nil || !login.Success {
		if auth.IsUserNotConfirmedError(err) {
			return "", ErrUserNotConfirmed
		}
		// Cognito raises this only once the password matches, so naming it tells a caller nothing they did not already hold.
		if errors.As(err, new(*cognitotypes.PasswordResetRequiredException)) {
			return "", ErrPasswordResetRequired
		}
		if login != nil && login.ChallengeName == challengeNewPasswordRequired {
			return "", ErrNewPasswordRequired
		}
		if login != nil && login.ChallengeName != "" {
			return "", ErrSecondStepRequired
		}
		if err == nil {
			return "", ErrAuthenticationFailed
		}
		if isCredentialRefusal(err) {
			// Wrapped so the handler's log names the cause; callers still match only ErrAuthenticationFailed.
			return "", fmt.Errorf("%w: %w", ErrAuthenticationFailed, err)
		}
		return "", rmerror.NewRMError(err, "legacyauth: the pool could not check the password")
	}
	email, phone, err := verifiedContacts(login.IDToken)
	if err != nil {
		return "", err
	}
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, nil)
	return s.oauth.ResolveOrCreateUserByContacts(rmngCtx, email, phone, s.loginProfile(login.IDToken))
}

// isCredentialRefusal is the pool turning this person away, a pre-auth trigger's no included; a NotAuthorizedException about the app client's secret is the deployment's fault instead.
func isCredentialRefusal(err error) bool {
	var notAuthorized *cognitotypes.NotAuthorizedException
	if errors.As(err, &notAuthorized) {
		message := strings.ToLower(notAuthorized.ErrorMessage())
		return !strings.Contains(message, "secret hash") && !strings.Contains(message, "secret_hash")
	}
	var notFound *cognitotypes.UserNotFoundException
	var triggerRefused *cognitotypes.UserLambdaValidationException
	return errors.As(err, &notFound) || errors.As(err, &triggerRefused)
}

// RefreshLegacy renews a password sign-in; resource narrows the access token to one of its resources, or "" to let the sign-in decide.
func (s *Service) RefreshLegacy(ctx context.Context, refreshToken, resource string) (*auth.UserTokens, error) {
	client, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(legacyClientID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "legacyauth: password client unreadable")
	}
	return s.oauth.RefreshToken(ctx, client, refreshToken, resource)
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
			// The account exists. InitiateAuth with the submitted password tells the states apart:
			login, authErr := s.cognito.InitiateAuth(ctx, &cognitoutil.LoginRequest{Username: username, Password: password})
			if authErr == nil && login.Success {
				// Confirmed account, correct password: the caller owns it, so route to sign-in
				// rather than mail a code they cannot use.
				return ErrAccountAlreadyUsable
			}
			// Unconfirmed (correct password), or an incorrect password (either state): resend the
			// code, so an owner who cannot supply the password can still recover. The dropped error
			// and uniform 201 keep the response from revealing that the account exists.
			_ = s.cognito.ResendSignupCode(ctx, username)
			return nil
		}
		if auth.IsPasswordPolicyError(err) {
			return fmt.Errorf("%w: %w", ErrPasswordRefused, err)
		}
		return rmerror.NewRMError(err, "signup failed")
	}
	return nil
}

func (s *Service) VerifySignup(ctx context.Context, username, code string) error {
	if err := s.cognito.ConfirmSignUp(ctx, username, code); err != nil {
		// One answer for every failure, unknown user included — Cognito checks the code before the
		// account state, so nothing more specific is knowable here.
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

// ConfirmForgotPassword resets the password, then signs out every session of the account the pool's verified contacts name, which is the account sign-in reaches and not whichever account holds the typed username.
func (s *Service) ConfirmForgotPassword(ctx context.Context, username, code, newPassword string) error {
	if err := s.cognito.ConfirmForgotPassword(ctx, username, code, newPassword); err != nil {
		if auth.IsPasswordPolicyError(err) {
			return fmt.Errorf("%w: %w", ErrPasswordRefused, err)
		}
		return rmerror.NewRMError(err, "password recovery confirmation failed")
	}
	login, err := s.cognito.InitiateAuth(ctx, &cognitoutil.LoginRequest{Username: username, Password: newPassword})
	if err != nil {
		return rmerror.NewRMError(fmt.Errorf("%w: %w", ErrSessionsNotEnded, err), "legacyauth: sign-in with the new password failed")
	}
	if login.IDToken == "" {
		rlog.Warn(ctx).Str("challenge", login.ChallengeName).Msg("password reset: the pool answered the new password with a challenge, so the account could not be confirmed and no sessions were ended")
		return nil
	}
	email, phone, err := verifiedContacts(login.IDToken)
	if err != nil {
		return rmerror.NewRMError(fmt.Errorf("%w: %w", ErrSessionsNotEnded, err), "legacyauth: the reset account could not be identified")
	}
	userID, err := s.oauth.UserIDByVerifiedContacts(ctx, email, phone)
	if err != nil {
		return rmerror.NewRMError(fmt.Errorf("%w: %w", ErrSessionsNotEnded, err), "legacyauth: the reset account could not be identified")
	}
	if userID == "" {
		// A pool user who never signed in has no account and so no sessions.
		return nil
	}
	return endSessionsWithoutConnectedApps(ctx, userID)
}

// ChangePassword re-authenticates with the old password to obtain a provider access token, because
// the client holds our tokens and the provider will not accept one. The caller's own access token is
// verified first: it names whose password is being changed, so an unverified one would let anyone
// repassword anyone.
func (s *Service) ChangePassword(ctx context.Context, accessToken, oldPassword, newPassword string) error {
	if accessToken == "" || oldPassword == "" || newPassword == "" {
		return rmerror.NewRMError(nil, "access token, old password and new password are required")
	}
	// Any API's token will do: a legacy sign-in's is addressed to its client's default API, and the token only names the account, whose old password is the proof.
	claims, err := s.oauth.VerifyUserTokenForAnyAPI(accessToken)
	if err != nil {
		return rmerror.NewRMError(err, "legacyauth: access token rejected")
	}
	info := claims.UserInfo()
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
	return endSessionsAndConnectedApps(ctx, claims.Subject)
}

// endSessionsWithoutConnectedApps signs the person out of every browser and app after a password reset; their connected apps stay, because forgetting a password says nothing about who else holds it.
func endSessionsWithoutConnectedApps(ctx context.Context, userID string) error {
	if err := signout.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).EndAllSessions(userID); err != nil {
		return rmerror.NewRMError(fmt.Errorf("%w: %w", ErrSessionsNotEnded, err), "legacyauth: sessions not ended")
	}
	return nil
}

// endSessionsAndConnectedApps signs the person out everywhere and disconnects every connected app after a password change, which they make when they think someone else knows the old one.
func endSessionsAndConnectedApps(ctx context.Context, userID string) error {
	if err := signout.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).EndAllSessionsAndConnectedApps(userID); err != nil {
		return rmerror.NewRMError(fmt.Errorf("%w: %w", ErrSessionsNotEnded, err), "legacyauth: sessions and connected apps not ended")
	}
	return nil
}

// ErrGlobalSignoutUnsupported is returned for a global signout. Our sessions are refresh
// families of ours, which a provider-side sign-out does not touch, so honouring the request would
// require revoking every family — deliberately not done. Reporting it beats a false success.
var ErrGlobalSignoutUnsupported = errors.New("legacyauth: global signout is not supported")

func (s *Service) Signout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return signout.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).RevokeFamily(legacyClientID, refreshToken)
}

// Read unverified because we obtained this token ourselves
func (s *Service) loginProfile(idToken string) *user_details_db.UpstreamProfile {
	parsed, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil
	}
	claims, _ := parsed.Claims.(jwt.MapClaims)
	profile := idp.ReadProfile(s.providerName, claims, s.attributeMapping)
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
	return idp.UpstreamIdentity{
		Email:         rawEmail,
		EmailVerified: emailVerified,
		PhoneNumber:   rawPhone,
		PhoneVerified: phoneVerified,
	}.VerifiedContacts()
}
