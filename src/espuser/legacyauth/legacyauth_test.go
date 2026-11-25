// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package legacyauth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLegacyAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LegacyAuth Suite")
}

const (
	testPoolID    = "eu-west-2_testpool"
	testClientID2 = "native-client"
	poolJWKSParam = "/espuser/base/end-user-pool-jwks-test"
	testPassword  = "Password1!"
	// The code CognitoProviderMock issues for every signup (its DefaultConfirmationCode).
	mockConfirmationCode = "123456"
	neoResource          = "https://api.example.com"
	accountResource      = "https://account.example.com"
)

// seedPasswordProvider registers the enabled provider row NewService resolves its app client from.
func seedPasswordProvider(clientID string, attributeMapping map[string]string) {
	seedNamedPasswordProvider("cognito", clientID, attributeMapping)
}

func seedNamedPasswordProvider(name, clientID string, attributeMapping map[string]string) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
	yes := true
	Expect(identity_providers_db.NewIdentityProvidersDB(rmngCtx).CreateProvider(&identity_providers_db.ProviderEntry{
		ProviderName:     name,
		Type:             identity_providers_db.TypeOIDC,
		PasswordVerifier: identity_providers_db.PasswordVerifierCognito,
		Enabled:          &yes,
		ClientID:         clientID,
		ClientSecret:     "provider-client-secret",
		AttributeMapping: attributeMapping,
	})).To(Succeed())
}

// unverifiedAudience reads a token's aud without verifying it; these specs assert on what was minted.
func unverifiedAudience(token string) any {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	Expect(err).NotTo(HaveOccurred())
	return claims["aud"]
}

// verifiedCaller verifies a minted access token the way the password-change route does, so the specs hand NewServiceForCaller real claims.
func verifiedCaller(accessToken string) auth.TokenClaims {
	oauth, err := auth.NewOAuthUserAuthService(context.Background())
	Expect(err).NotTo(HaveOccurred())
	caller, err := oauth.VerifyUserTokenForAnyAPI(accessToken)
	Expect(err).NotTo(HaveOccurred())
	return caller
}

// changePassword is the password-change route's two steps: resolve the caller's provider from the token, then change the password there.
func changePassword(accessToken, oldPassword, newPassword string) error {
	svc, err := NewServiceForCaller(context.Background(), verifiedCaller(accessToken))
	if err != nil {
		return err
	}
	return svc.ChangePassword(context.Background(), oldPassword, newPassword)
}

// Which pool a password reaches is an authorization decision, so it must not depend on DynamoDB's scan order.
var _ = Describe("provider selection", func() {
	BeforeEach(func() {
		backend := test_utils.SetupEspUserBackend(context.Background())
		test_utils.PutParam(context.Background(), backend.SSMMock, poolJWKSParam, string(test_utils.TestJWKUtil.GetTestJWKS()))
	})

	DescribeTable("authenticates against the named provider, or the first by name when none is named",
		func(named, want string) {
			seedNamedPasswordProvider("beta", testClientID2, nil)
			seedNamedPasswordProvider("alpha", testClientID2, nil)

			svc, err := NewService(context.Background(), named)
			Expect(err).NotTo(HaveOccurred())
			Expect(svc.providerName).To(Equal(want))
		},
		Entry("none named", "", "alpha"),
		Entry("one named", "beta", "beta"),
	)

	It("refuses a provider that is not registered, rather than falling back to another one (negative)", func() {
		seedNamedPasswordProvider("alpha", testClientID2, nil)

		_, err := NewService(context.Background(), "nope")
		Expect(err).To(MatchError(ErrProviderNotFound))
	})

	It("never picks an oidc row naming no password_verifier, which signs in on the provider's own page (negative)", func() {
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "alpha", Type: identity_providers_db.TypeOIDC, Enabled: utils.Ptr(true), ClientID: testClientID2,
		})).To(Succeed())

		_, err := NewService(context.Background(), "")
		Expect(err).To(MatchError(ErrNoPasswordProvider))
	})

	It("never picks a row naming a password_verifier it cannot call, even when it is the only one (negative)", func() {
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "alpha", Type: identity_providers_db.TypeOIDC, PasswordVerifier: "ldap", Enabled: utils.Ptr(true), ClientID: testClientID2,
		})).To(Succeed())

		_, err := NewService(context.Background(), "alpha")
		Expect(err).To(MatchError(ErrProviderNotFound))
		_, err = NewService(context.Background(), "")
		Expect(err).To(MatchError(ErrNoPasswordProvider))
	})
})

var _ = Describe("SigninWithPassword", func() {
	var backend *test_utils.EspUserBackend

	// upstream describes the provider row and the claims its ID token carries beyond the contact.
	type upstream struct {
		mapping     map[string]string
		extraClaims jwt.MapClaims
	}

	newServiceWith := func(email string, emailVerified bool, up upstream) *Service {
		cog := mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect(testPoolID, testClientID2)
		cog.AddTestUserDirect(testPoolID, email, email, testPassword, true)
		cog.IDTokenMinter = func(user *mock.UserState) string {
			claims := jwt.MapClaims{"email": email, "email_verified": emailVerified}
			for k, v := range up.extraClaims {
				claims[k] = v
			}
			tok, err := jwtutil.SignRS256(claims, backend.SigningKey, oidc.SigningKeyID)
			Expect(err).NotTo(HaveOccurred())
			return tok
		}
		awscommon.SetCognitoProviderClient(cog)

		seedPasswordProvider(testClientID2, up.mapping)
		_, err := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Create(clients.CreateInput{
			ClientID: legacyClientID, ClientName: "RainMaker", ClientType: "public", RedirectURIs: []string{"com.example://cb"},
			GrantTypes: []string{"authorization_code", "refresh_token"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{neoResource, accountResource}, DefaultResource: neoResource,
		})
		Expect(err).NotTo(HaveOccurred())
		svc, err := NewService(context.Background(), "")
		Expect(err).NotTo(HaveOccurred())
		return svc
	}

	newService := func(email string, emailVerified bool) *Service {
		return newServiceWith(email, emailVerified, upstream{})
	}

	subjectOf := func(accessToken string) string {
		parsed, _, err := jwt.NewParser().ParseUnverified(accessToken, jwt.MapClaims{})
		Expect(err).NotTo(HaveOccurred())
		return parsed.Claims.(jwt.MapClaims)["sub"].(string)
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		test_utils.PutParam(context.Background(), backend.SSMMock, poolJWKSParam, string(test_utils.TestJWKUtil.GetTestJWKS()))
	})

	It("converts a verified-email Cognito login into OUR tokens (never Cognito's)", func() {
		svc := newService("u@example.com", true)

		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(tokens.AccessToken).NotTo(BeEmpty())
		Expect(tokens.RefreshToken).NotTo(BeEmpty())

		jwksJSON, err := json.Marshal(jwtutil.BuildJWKS(
			jwtutil.BuildJWK(&backend.SigningKey.PublicKey, jwtutil.RSAThumbprint(&backend.SigningKey.PublicKey)),
		))
		Expect(err).NotTo(HaveOccurred())
		keySet, err := jwtutil.ParseJWKS(string(jwksJSON))
		Expect(err).NotTo(HaveOccurred())
		claims, err := jwtutil.VerifyJWT(tokens.AccessToken, keySet)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims["iss"]).To(Equal(backend.Issuer))
		Expect(claims["aud"]).To(Equal(neoResource), "the client's default, never the client id")
		Expect(claims["scope"]).To(Equal(legacyScope))
	})

	It("signs a password login in under the claim the row maps to sub, recording the provider", func() {
		svc := newServiceWith("legacy@example.com", true, upstream{
			mapping:     map[string]string{"sub": "custom:user_id"},
			extraClaims: jwt.MapClaims{"sub": "cog-sub-9", "custom:user_id": "Google_legacy123"},
		})

		tokens, err := svc.SigninWithPassword(context.Background(), "legacy@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(subjectOf(tokens.AccessToken)).To(Equal("Google_legacy123"))

		entry, err := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetUserDetailsByUserID("Google_legacy123")
		Expect(err).NotTo(HaveOccurred())
		Expect(entry.Provider).To(Equal("cognito"))
		Expect(entry.ExternalSub).To(Equal("cog-sub-9"))
	})

	It("mints an id when the row does not map sub (negative)", func() {
		svc := newServiceWith("legacy@example.com", true, upstream{extraClaims: jwt.MapClaims{"sub": "cog-sub-9", "custom:user_id": "Google_legacy123"}})

		tokens, err := svc.SigninWithPassword(context.Background(), "legacy@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(subjectOf(tokens.AccessToken)).NotTo(Equal("Google_legacy123"))
	})

	It("reads sub and the profile through the row's attribute_mapping, as federation does", func() {
		svc := newServiceWith("legacy@example.com", true, upstream{
			mapping:     map[string]string{"sub": "custom:legacy_id", "name": "custom:display_name"},
			extraClaims: jwt.MapClaims{"sub": "cog-sub-9", "custom:user_id": "unmapped", "custom:legacy_id": "Google_legacy123", "custom:display_name": "Ada"},
		})

		tokens, err := svc.SigninWithPassword(context.Background(), "legacy@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(subjectOf(tokens.AccessToken)).To(Equal("Google_legacy123"))

		entry, err := user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetUserDetailsByUserID("Google_legacy123")
		Expect(err).NotTo(HaveOccurred())
		Expect(entry.ExternalSub).To(Equal("cog-sub-9"))
		Expect(entry.Name).To(Equal("Ada"))
	})

	It("covers every resource the password client may request, and a refresh narrows to the ESP User one (RFC 8707 s2.2)", func() {
		svc := newService("multi@example.com", true)
		backend.DBMock.ProfileReset()
		tokens, err := svc.SigninWithPassword(context.Background(), "multi@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(unverifiedAudience(tokens.AccessToken)).To(Equal(neoResource), "the client's default")
		Expect(backend.DBMock.ProfileGet().Accesses["espuser-oauth-clients"].ReadCount).To(Equal(1), "the client row the mint reads supplies the resources")

		renewed, err := svc.RefreshLegacy(context.Background(), tokens.RefreshToken, accountResource)
		Expect(err).NotTo(HaveOccurred())
		Expect(unverifiedAudience(renewed.AccessToken)).To(Equal(accountResource))
	})

	It("refuses a refresh naming a resource the password client was not granted (negative)", func() {
		svc := newService("other@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "other@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		_, err = svc.RefreshLegacy(context.Background(), tokens.RefreshToken, "https://other.example.com")
		Expect(err).To(MatchError(auth.ErrResourceNotAuthorized))
	})

	It("refuses the sign-in when the password client is granted no resource (negative)", func() {
		svc := newService("bare@example.com", true)
		clientsSvc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsSvc.Delete(legacyClientID)).To(Succeed())
		_, err := clientsSvc.Create(clients.CreateInput{
			ClientID: legacyClientID, ClientName: "RainMaker", ClientType: "public", RedirectURIs: []string{"com.example://cb"},
			GrantTypes: []string{"authorization_code", "refresh_token"}, RequirePKCE: utils.Ptr(true),
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = svc.SigninWithPassword(context.Background(), "bare@example.com", testPassword)
		Expect(err).To(MatchError(auth.ErrResourceRequired))
	})

	It("answers a blank password as a failed sign-in, not as a server error (negative)", func() {
		svc := newService("blank@example.com", true)
		_, err := svc.SigninWithPassword(context.Background(), "blank@example.com", "")
		Expect(err).To(MatchError(ErrAuthenticationFailed))
	})

	// An admin-created account answers its correct temporary password with a challenge, and "authentication failed" would strand the person on a password they know is right.
	It("reports a forced password change as its own outcome, not as a failed sign-in", func() {
		svc := newService("forced@example.com", true)
		awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock).ChallengeOnAuth = challengeNewPasswordRequired

		_, err := svc.SigninWithPassword(context.Background(), "forced@example.com", testPassword)
		Expect(err).To(MatchError(ErrNewPasswordRequired))
	})

	It("rejects a login whose contact is unverified (negative)", func() {
		svc := newService("u2@example.com", false)
		_, err := svc.SigninWithPassword(context.Background(), "u2@example.com", testPassword)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a wrong password (negative, Cognito authentication fails)", func() {
		svc := newService("u3@example.com", true)
		_, err := svc.SigninWithPassword(context.Background(), "u3@example.com", "wrong-password")
		Expect(err).To(HaveOccurred())
	})

	DescribeTable("answers the pool refusing the person as a failed sign-in",
		func(poolErr error) {
			svc := newService("u4@example.com", true)
			awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock).InitiateAuthError = poolErr
			_, err := svc.Authenticate(context.Background(), "u4@example.com", testPassword)
			Expect(err).To(MatchError(ErrAuthenticationFailed))
			Expect(err).To(MatchError(poolErr), "the cause stays on the error for the handler's log")
		},
		Entry("a wrong password", &cognitotypes.NotAuthorizedException{Message: aws.String("Incorrect username or password.")}),
		Entry("no such account", &cognitotypes.UserNotFoundException{}),
		Entry("a pre-authentication trigger refusing the person", &cognitotypes.UserLambdaValidationException{Message: aws.String("PreAuthentication failed with error Blocked.")}),
		Entry("a NotAuthorizedException that only mentions a secret elsewhere", &cognitotypes.NotAuthorizedException{Message: aws.String("Incorrect username or password; keep your secret safe.")}),
	)

	It("names an account an admin reset, which Cognito says only after the password matches", func() {
		svc := newService("u4@example.com", true)
		awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock).InitiateAuthError = &cognitotypes.PasswordResetRequiredException{Message: aws.String("Password reset required for the user")}
		_, err := svc.Authenticate(context.Background(), "u4@example.com", testPassword)
		Expect(err).To(MatchError(ErrPasswordResetRequired))
		Expect(errors.Is(err, ErrAuthenticationFailed)).To(BeFalse())
	})

	DescribeTable("keeps the pool failing apart from a refusal, with its cause for the handler's log (negative)",
		func(poolErr error) {
			svc := newService("u4@example.com", true)
			awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock).InitiateAuthError = poolErr
			_, err := svc.Authenticate(context.Background(), "u4@example.com", testPassword)
			Expect(errors.Is(err, ErrAuthenticationFailed)).To(BeFalse())
			Expect(err).To(MatchError(poolErr))
		},
		Entry("throttled", &cognitotypes.TooManyRequestsException{}),
		Entry("an app client the pool does not know", &cognitotypes.ResourceNotFoundException{}),
		Entry("a wrong app client secret", &cognitotypes.NotAuthorizedException{Message: aws.String("Unable to verify secret hash for client native-client")}),
		Entry("a secret hash named as the parameter", &cognitotypes.NotAuthorizedException{Message: aws.String("Unable to verify SECRET_HASH for client native-client")}),
		Entry("an app client with a secret the provider row does not carry", &cognitotypes.NotAuthorizedException{Message: aws.String("Client native-client is configured with secret but SECRET_HASH was not received")}),
	)

	It("fails closed when no enabled provider has a password our page can check (negative)", func() {
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		yes := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngCtx).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "oidc-only",
			Type:         identity_providers_db.TypeOIDC,
			Enabled:      &yes,
			ClientID:     "some-client",
		})).To(Succeed())

		_, err := NewService(context.Background(), "")
		Expect(err).To(MatchError(ErrNoPasswordProvider))
	})

	It("refuses a password change on a service built without a caller (negative)", func() {
		svc := newService("u@example.com", true)
		Expect(svc.ChangePassword(context.Background(), testPassword, "NewPass#123")).To(MatchError(ErrNoVerifiedCaller))
	})

	It("rejects a password change with a missing field (negative)", func() {
		svc := newService("u@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())
		Expect(changePassword(tokens.AccessToken, "", "NewPass#123")).NotTo(Succeed())
		Expect(changePassword(tokens.AccessToken, testPassword, "")).NotTo(Succeed())
	})

	It("changes the password when the caller's own token and old password both check out", func() {
		svc := newService("u@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())

		Expect(changePassword(tokens.AccessToken, testPassword, "NewPass#123")).To(Succeed())
	})

	It("changes the password with the token a legacy sign-in returns, although it is addressed to the RainMaker API and not this one", func() {
		GinkgoT().Setenv("ACCEPTED_AUDIENCES", accountResource)
		svc := newService("u@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())

		Expect(changePassword(tokens.AccessToken, testPassword, "NewPass#123")).To(Succeed())
	})

	It("stamps a password sign-in's tokens with amr pwd and the provider row as idp", func() {
		svc := newService("u@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())

		for _, token := range []string{tokens.AccessToken, tokens.IDToken} {
			claims := jwt.MapClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(token, claims)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims["amr"]).To(Equal([]any{"pwd"}))
			Expect(claims["idp"]).To(Equal("cognito"))
		}
	})

	Describe("choosing the provider whose password changes", func() {
		// tokenFor mints an access token for a sign-in a spec describes directly, such as a federated one or one from before idp was stamped.
		tokenFor := func(claims jwtutil.UserClaims) string {
			claims.Email = "u@example.com"
			token, err := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID).
				AccessToken("user-1", legacyClientID, legacyScope, neoResource, claims)
			Expect(err).NotTo(HaveOccurred())
			return token
		}
		providerFor := func(accessToken string) (string, error) {
			svc, err := NewServiceForCaller(context.Background(), verifiedCaller(accessToken))
			if err != nil {
				return "", err
			}
			return svc.providerName, nil
		}

		BeforeEach(func() {
			newService("u@example.com", true)
			seedNamedPasswordProvider("zeta", testClientID2, nil)
			rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
			Expect(identity_providers_db.NewIdentityProvidersDB(rmngCtx).CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "google", DisplayName: "Google", Type: identity_providers_db.TypeOIDC, Enabled: utils.Ptr(true), ClientID: "google-client",
			})).To(Succeed())
		})

		It("takes the provider the token's idp names, not the first by name", func() {
			Expect(providerFor(tokenFor(jwtutil.UserClaims{AMR: []string{"pwd"}, Provider: "zeta"}))).To(Equal("zeta"))
		})

		DescribeTable("refuses a sign-in with no password ESP User checked, naming its provider (negative)",
			func(claims jwtutil.UserClaims, name string) {
				_, err := providerFor(tokenFor(claims))
				Expect(err).To(MatchError(ErrNoPasswordToChange))
				var refusal *NoPasswordToChangeError
				Expect(errors.As(err, &refusal)).To(BeTrue())
				Expect(refusal.Provider).To(Equal(name))
			},
			Entry("a federated sign-in, by the row's display name", jwtutil.UserClaims{Provider: "google"}, "Google"),
			Entry("a one-time code, by its idp since no row holds it", jwtutil.UserClaims{AMR: []string{"otp"}, Provider: "otp"}, "otp"),
			Entry("a password row's sign-in that checked no password, by the row's name when it has no display name", jwtutil.UserClaims{Provider: "zeta"}, "zeta"),
			Entry("a provider row missing from the registry, by its idp", jwtutil.UserClaims{AMR: []string{"pwd"}, Provider: "retired"}, "retired"),
		)

		It("refuses a sign-in without a password by its idp when the registry is unreadable (negative)", func() {
			awscommon.SetDynamoDBClient(&mock.FailingScan{DynamoDBMock: backend.DBMock, TableName: "espuser-identity-providers"})
			_, err := providerFor(tokenFor(jwtutil.UserClaims{Provider: "google"}))
			var refusal *NoPasswordToChangeError
			Expect(errors.As(err, &refusal)).To(BeTrue())
			Expect(refusal.Provider).To(Equal("google"))
		})

		It("fails rather than refuse a password sign-in when the registry is unreadable (negative)", func() {
			awscommon.SetDynamoDBClient(&mock.FailingScan{DynamoDBMock: backend.DBMock, TableName: "espuser-identity-providers"})
			_, err := providerFor(tokenFor(jwtutil.UserClaims{AMR: []string{"pwd"}, Provider: "zeta"}))
			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(ErrNoPasswordToChange))
		})

		It("uses the default password row for a token that names no idp", func() {
			Expect(providerFor(tokenFor(jwtutil.UserClaims{}))).To(Equal("cognito"))
		})
	})

	It("names a password the pool's policy refuses at signup and recovery confirmation, not the route's catch-all", func() {
		svc := newService("u@example.com", true)
		refused := &cognitotypes.InvalidPasswordException{Message: aws.String("Password did not conform with policy: Password must have symbol characters")}
		cog := awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
		cog.SignUpError, cog.ConfirmForgotPasswordError = refused, refused

		Expect(svc.Signup(context.Background(), "new@example.com", "", "Password1")).To(MatchError(ErrPasswordRefused))
		Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", mockConfirmationCode, "Password1")).To(MatchError(ErrPasswordRefused))
	})

	Describe("password events sign the account out", func() {
		const newPassword = "NewPass#123"
		var (
			svc    *Service
			cog    *mock.CognitoProviderMock
			caller *auth.UserTokens
			userID string
		)
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		// familiesOf describes each family as scope|sid, so a spec names exactly which logins survive.
		familiesOf := func(owner string) []string {
			rows, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(owner)
			Expect(err).NotTo(HaveOccurred())
			described := make([]string, 0, len(rows))
			for i := range rows {
				described = append(described, rows[i].Scope+"|"+rows[i].SID)
			}
			return described
		}
		families := func() []string { return familiesOf(userID) }
		sessionSIDs := func() []string {
			rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
			Expect(err).NotTo(HaveOccurred())
			sids := make([]string, 0, len(rows))
			for i := range rows {
				sids = append(sids, rows[i].SID)
			}
			return sids
		}
		sidOf := func(accessToken string) string {
			parsed, _, err := jwt.NewParser().ParseUnverified(accessToken, jwt.MapClaims{})
			Expect(err).NotTo(HaveOccurred())
			return parsed.Claims.(jwt.MapClaims)["sid"].(string)
		}

		var callerSID, otherSID, resetCode string
		BeforeEach(func() {
			backend.DBMock.MaxPageItems = 1
			svc = newService("u@example.com", true)
			cog = awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)
			// The ID token carries the pool user's own contacts, so a spec sets which of them the pool vouches for.
			cog.IDTokenMinter = func(user *mock.UserState) string {
				claims := jwt.MapClaims{"email": user.Email, "email_verified": user.Attributes["email_verified"] != "false"}
				if user.PhoneNumber != "" {
					claims["phone_number"] = user.PhoneNumber
					claims["phone_number_verified"] = user.Attributes["phone_number_verified"] == "true"
				}
				tok, err := jwtutil.SignRS256(claims, backend.SigningKey, oidc.SigningKeyID)
				Expect(err).NotTo(HaveOccurred())
				return tok
			}
			resetCode = cog.DefaultResetCode
			var err error
			caller, err = svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
			Expect(err).NotTo(HaveOccurred())
			other, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
			Expect(err).NotTo(HaveOccurred())
			userID, callerSID, otherSID = subjectOf(caller.AccessToken), sidOf(caller.AccessToken), sidOf(other.AccessToken)
			tokens := refreshtoken.NewService(rmngCtx)
			_, err = tokens.MintRefreshtoken(userID, legacyClientID, "openid", nil, session.Session{})
			Expect(err).NotTo(HaveOccurred())
			_, err = tokens.MintRefreshtoken(userID, "assistant-client", "openid offline_access", nil, session.Session{})
			Expect(err).NotTo(HaveOccurred())
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
		})

		It("ends every session, the caller's included, and every connected app on a change, so the person signs in again", func() {
			Expect(changePassword(caller.AccessToken, testPassword, newPassword)).To(Succeed())

			Expect(families()).To(BeEmpty())
			Expect(sessionSIDs()).To(BeEmpty())
		})

		It("ends every session and its logins on a reset, keeping the offline grant", func() {
			Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)).To(Succeed())

			Expect(families()).To(ConsistOf("openid offline_access|"))
			Expect(sessionSIDs()).To(BeEmpty())
		})

		// secondAccount opens another ESP User account holding phone, with one session family, and gives the pool user u@example.com that phone, verified.
		secondAccount := func(phone string) string {
			otherID, err := svc.oauth.ResolveOrCreateUserByContacts(rmngCtx, "", phone, nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = refreshtoken.NewService(rmngCtx).MintRefreshtoken(otherID, legacyClientID, "openid", nil, session.Session{})
			Expect(err).NotTo(HaveOccurred())
			poolUser := cog.GetUserByUsername(testPoolID, "u@example.com")
			poolUser.PhoneNumber = phone
			poolUser.Attributes["phone_number_verified"] = "true"
			return otherID
		}

		It("ends the sessions of the account the verified phone names, never those of the account holding the unverified email typed (negative)", func() {
			otherID := secondAccount("+15550123")
			cog.GetUserByUsername(testPoolID, "u@example.com").Attributes["email_verified"] = "false"

			Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)).To(Succeed())

			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
			Expect(familiesOf(otherID)).To(BeEmpty())
		})

		It("ends nothing when the verified contacts belong to two accounts, as sign-in refuses them (negative)", func() {
			otherID := secondAccount("+15550123")

			err := svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)
			Expect(err).To(MatchError(ErrSessionsNotEnded))
			Expect(err).To(MatchError(auth.ErrContactsOnDifferentUsers))

			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
			Expect(familiesOf(otherID)).To(HaveLen(1))
		})

		It("resets the password but ends nothing when the new password's sign-in asks for a second step (negative)", func() {
			cog.ChallengeOnAuth = "SOFTWARE_TOKEN_MFA"

			Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)).To(Succeed())

			Expect(cog.GetUserByUsername(testPoolID, "u@example.com").Password).To(Equal(newPassword))
			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
		})

		It("reports a reset whose sign-in with the new password fails as sessions not ended (negative)", func() {
			cog.InitiateAuthError = &cognitotypes.InternalErrorException{Message: aws.String("unavailable")}

			Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)).To(MatchError(ErrSessionsNotEnded))

			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
		})

		It("resets the password of a pool user no account holds yet and ends nobody's sessions (negative)", func() {
			cog.AddTestUserDirect(testPoolID, "fresh@example.com", "fresh@example.com", testPassword, true)

			Expect(svc.ConfirmForgotPassword(context.Background(), "fresh@example.com", resetCode, newPassword)).To(Succeed())

			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
		})

		It("ends nothing when the reset itself is refused (negative)", func() {
			Expect(svc.ConfirmForgotPassword(context.Background(), "u@example.com", "000000", newPassword)).NotTo(Succeed())

			Expect(families()).To(HaveLen(4))
			Expect(sessionSIDs()).To(ConsistOf(callerSID, otherSID))
		})

		DescribeTable("reports a password that changed while the sessions could not be signed out, not a refused change (negative)",
			func(failingIndexName string, event func() error) {
				awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock, IndexName: failingIndexName})
				Expect(event()).To(MatchError(ErrSessionsNotEnded))
			},
			Entry("a change, the session rows unreachable", sessions_db.SessionsByUserIndex, func() error {
				return changePassword(caller.AccessToken, testPassword, newPassword)
			}),
			Entry("a reset, the account lookup unreachable", "espuser-user-details-by-email", func() error {
				return svc.ConfirmForgotPassword(context.Background(), "u@example.com", resetCode, newPassword)
			}),
		)
	})

	Describe("enumeration resistance", func() {
		It("routes a confirmed account to sign-in when the duplicate signup proves the password", func() {
			// InitiateAuth authenticates, so the caller owns the account: no code, sign-in message.
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "u@example.com", "", testPassword)).To(MatchError(ErrAccountAlreadyUsable))
		})

		It("keeps a wrong-password duplicate signup uniform, never routing to sign-in (negative)", func() {
			// A wrong password proves nothing, so the plain success is indistinguishable from a
			// fresh signup — no ErrAccountAlreadyUsable.
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "u@example.com", "", "Different-9!x")).To(Succeed())
		})

		It("reports signup success for an unconfirmed account by re-sending the code", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "fresh@example.com", "", testPassword)).To(Succeed())
			Expect(svc.Signup(context.Background(), "fresh@example.com", "", testPassword)).To(Succeed())
		})

		It("fails verification of an already confirmed account like any other failure (negative)", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "twice@example.com", "", testPassword)).To(Succeed())
			Expect(svc.VerifySignup(context.Background(), "twice@example.com", mockConfirmationCode)).To(Succeed())

			Expect(svc.VerifySignup(context.Background(), "twice@example.com", mockConfirmationCode)).NotTo(Succeed())
		})

		It("names an unconfirmed account at sign-in, so it can be recovered", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "pending2@example.com", "", testPassword)).To(Succeed())

			_, err := svc.SigninWithPassword(context.Background(), "pending2@example.com", testPassword)
			Expect(err).To(MatchError(ErrUserNotConfirmed))
		})

		It("keeps a wrong password against an unconfirmed account uniform (negative)", func() {
			// The disclosure above is bounded by Cognito raising UserNotConfirmed only after the
			// password matches; a guess must stay indistinguishable from any other failure.
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "pending3@example.com", "", testPassword)).To(Succeed())

			_, err := svc.SigninWithPassword(context.Background(), "pending3@example.com", "wrong-password")
			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(ErrUserNotConfirmed))
		})

		It("fails signup verification for an unknown user exactly like a wrong code (negative)", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "pending@example.com", "", testPassword)).To(Succeed())

			unknownErr := svc.VerifySignup(context.Background(), "ghost@example.com", "000000")
			wrongCodeErr := svc.VerifySignup(context.Background(), "pending@example.com", "000000")
			Expect(unknownErr).To(HaveOccurred())
			Expect(wrongCodeErr).To(HaveOccurred())
		})

		It("reports password-recovery success for an unknown user", func() {
			svc := newService("u@example.com", true)
			Expect(svc.ForgotPassword(context.Background(), "ghost@example.com")).To(Succeed())
		})
	})
})
