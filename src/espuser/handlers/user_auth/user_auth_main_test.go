// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/legacyauth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserAuthHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Auth Handler Suite")
}

var _ = Describe("legacy token responses (RFC 6749 s5.1)", func() {
	const (
		poolID          = "eu-west-2_testpool"
		clientID        = "native-client"
		email           = "legacy@example.com"
		password        = "Password1!"
		neoResource     = "https://api.example.com"
		accountResource = "https://account.example.com"
		homeAppClientID = "rainmaker-neo-home-app-client"
		// The same address holds an account in a second pool, under its own password, so a sign-in only succeeds against the pool it names.
		secondPool     = "eu-west-2_secondpool"
		secondProvider = "second"
		secondPassword = "Second#123"
	)
	resetCode := mock.NewCognitoProviderMock().DefaultResetCode
	var (
		backend *test_utils.EspUserBackend
		cog     *mock.CognitoProviderMock
	)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		cog = mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect(poolID, clientID)
		cog.AddTestUserDirect(poolID, email, email, password, true)
		cog.AddTestUserPoolDirect(secondPool, "second-client")
		cog.AddTestUserDirect(secondPool, email, email, secondPassword, true)
		cog.IDTokenMinter = func(*mock.UserState) string {
			tok, err := jwtutil.SignRS256(jwt.MapClaims{"email": email, "email_verified": true}, backend.SigningKey, oidc.SigningKeyID)
			Expect(err).NotTo(HaveOccurred())
			return tok
		}
		awscommon.SetCognitoProviderClient(cog)
		yes := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, Enabled: &yes,
			ClientID: clientID, ClientSecret: "provider-client-secret",
		})).To(Succeed())
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: secondProvider, Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, Enabled: &yes,
			ClientID: "second-client", ClientSecret: "provider-client-secret",
		})).To(Succeed())
		// The password API signs in as the Home app client, covering every resource it is granted.
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: homeAppClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"com.example://cb"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{neoResource, accountResource}, DefaultResource: neoResource,
		})).To(Succeed())
	})

	postWithHeaders := func(path string, body map[string]any, headers map[string]string) events.APIGatewayProxyResponse {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		resp, err := handler(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: path, Body: string(raw), Headers: headers})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	post := func(path string, body map[string]any) events.APIGatewayProxyResponse {
		return postWithHeaders(path, body, nil)
	}

	changePassword := func(accessToken any, body map[string]any) events.APIGatewayProxyResponse {
		return postWithHeaders(pathPassword, body, map[string]string{"Authorization": "Bearer " + accessToken.(string)})
	}

	tokenSet := func(resp events.APIGatewayProxyResponse) map[string]any {
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		var tokens map[string]any
		Expect(json.Unmarshal([]byte(resp.Body), &tokens)).To(Succeed())
		return tokens
	}

	claimsOf := func(token any) jwt.MapClaims {
		claims := jwt.MapClaims{}
		_, _, err := jwt.NewParser().ParseUnverified(token.(string), claims)
		Expect(err).NotTo(HaveOccurred())
		return claims
	}

	audience := func(tokens map[string]any) any {
		return claimsOf(tokens["access_token"])["aud"]
	}

	It("issues an old app's sign-in, which names no client, to the Home app client and renews it", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		idAudience, err := claimsOf(signin["id_token"]).GetAudience()
		Expect(err).NotTo(HaveOccurred())
		Expect(idAudience).To(ConsistOf(homeAppClientID))
		Expect(claimsOf(signin["access_token"])["client_id"]).To(Equal(homeAppClientID))

		refresh := tokenSet(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}))
		Expect(refresh["refresh_token"]).NotTo(Equal(signin["refresh_token"]), "the refresh token rotates")
		Expect(claimsOf(refresh["access_token"])["client_id"]).To(Equal(homeAppClientID))
	})

	It("refuses a refresh token issued to user-pool-client, since a refresh token renews only with its own client (negative)", func() {
		ctx := context.Background()
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(ctx, nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "user-pool-client", ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"com.example://cb"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{neoResource, accountResource}, DefaultResource: neoResource,
		})).To(Succeed())
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		svc, err := auth.NewOAuthUserAuthService(ctx)
		Expect(err).NotTo(HaveOccurred())
		earlier, err := svc.MintTokenSetForFirstPartyLogin(ctx, claimsOf(signin["access_token"])["sub"].(string), "user-pool-client", "openid email phone", auth.FirstPartyLogin{
			AllAllowedResources: true, Origin: session.OriginApp,
		})
		Expect(err).NotTo(HaveOccurred())

		refresh := post(pathRefresh, map[string]any{"refresh_token": earlier.RefreshToken})
		Expect(refresh.StatusCode).To(Equal(http.StatusUnauthorized), refresh.Body)
		Expect(refresh.Body).To(ContainSubstring("Invalid refresh token"))
	})

	It("signs out its own login and removes the app's session from the signed-in list", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		userID := claimsOf(signin["access_token"])["sub"].(string)
		sessionSvc := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(sessionSvc.ListSessions(userID)).To(HaveLen(1))

		signout := post(pathSignout, map[string]any{"refresh_token": signin["refresh_token"]})
		Expect(signout.StatusCode).To(Equal(http.StatusOK), signout.Body)
		Expect(sessionSvc.ListSessions(userID)).To(BeEmpty())
		Expect(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}).StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("refuses to sign out a refresh token issued to another client and leaves it working (negative)", func() {
		refresh := refreshtoken.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		other, err := refresh.MintRefreshtoken("user-123", "another-app", "openid", nil, session.Session{})
		Expect(err).NotTo(HaveOccurred())

		signout := post(pathSignout, map[string]any{"refresh_token": other})
		Expect(signout.StatusCode).To(Equal(http.StatusUnauthorized), signout.Body)
		_, err = refresh.Rotate("another-app", other)
		Expect(err).NotTo(HaveOccurred())
	})

	It("answers a forged refresh token with 401 Invalid refresh token (negative)", func() {
		signout := post(pathSignout, map[string]any{"refresh_token": "not-a-token"})
		Expect(signout.StatusCode).To(Equal(http.StatusUnauthorized), signout.Body)
		Expect(signout.Body).To(MatchJSON(`{"message":"Invalid refresh token"}`))
	})

	It("answers a second sign-out of the same login with 200, since the login has already ended", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		Expect(post(pathSignout, map[string]any{"refresh_token": signin["refresh_token"]}).StatusCode).To(Equal(http.StatusOK))

		again := post(pathSignout, map[string]any{"refresh_token": signin["refresh_token"]})
		Expect(again.StatusCode).To(Equal(http.StatusOK), again.Body)
	})

	It("refuses a global sign-out with 400 and ends nothing (negative)", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		resp := post(pathSignout, map[string]any{"refresh_token": signin["refresh_token"], "global": "true"})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), resp.Body)
		Expect(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}).StatusCode).To(Equal(http.StatusOK))
	})

	It("answers a sign-out storage could not record with 503, and the login keeps working (negative)", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		healthy := awscommon.GetDynamoDBClient()
		awscommon.SetDynamoDBClient(&mock.FailingDeleteItem{DynamoDBMock: healthy.(*mock.DynamoDBMock), TableName: "espuser-refresh-tokens"})

		signout := post(pathSignout, map[string]any{"refresh_token": signin["refresh_token"]})
		Expect(signout.StatusCode).To(Equal(http.StatusServiceUnavailable), signout.Body)
		Expect(signout.Body).To(MatchJSON(`{"message":"Sign-out failed; try again"}`))

		awscommon.SetDynamoDBClient(healthy)
		Expect(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}).StatusCode).To(Equal(http.StatusOK))
	})

	It("marks the signin and refresh token sets uncacheable", func() {
		signin := post(pathToken, map[string]any{"username": email, "password": password})
		Expect(signin.StatusCode).To(Equal(http.StatusOK))
		Expect(signin.Headers).To(HaveKeyWithValue("Cache-Control", "no-store"))
		Expect(signin.Headers).To(HaveKeyWithValue("Pragma", "no-cache"))

		var tokens map[string]any
		Expect(json.Unmarshal([]byte(signin.Body), &tokens)).To(Succeed())
		refresh := post(pathRefresh, map[string]any{"refresh_token": tokens["refresh_token"].(string)})
		Expect(refresh.StatusCode).To(Equal(http.StatusOK))
		Expect(refresh.Headers).To(HaveKeyWithValue("Cache-Control", "no-store"))
		Expect(refresh.Headers).To(HaveKeyWithValue("Pragma", "no-cache"))
	})

	It("signs in for every resource the client is granted and narrows to the ESP User one on refresh (RFC 8707 s2.2)", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		Expect(audience(signin)).To(Equal(neoResource), "the client's default")
		refresh := tokenSet(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"], "resource": []string{accountResource}}))
		Expect(audience(refresh)).To(Equal(accountResource))
	})

	It("narrows a refresh to the one resource named when another value is empty (RFC 6749 s3.2)", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		refresh := tokenSet(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"], "resource": []string{"", accountResource}}))
		Expect(audience(refresh)).To(Equal(accountResource))
	})

	It("tells an account an admin created to get a new password, which only its right temporary password reveals", func() {
		awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock).ChallengeOnAuth = "NEW_PASSWORD_REQUIRED"
		resp := post(pathToken, map[string]any{"username": email, "password": password})
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(resp.Body).To(MatchJSON(`{"message":"` + legacyauth.NewPasswordRequiredMessage + `"}`))
	})

	It("refuses a pool the Home app client does not allow before the password reaches it, and still signs in with one it does (negative)", func() {
		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.DeleteClient(homeAppClientID)).To(Succeed())
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: homeAppClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"com.example://cb"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{neoResource, accountResource}, DefaultResource: neoResource,
			AllowedProviders: []string{"cognito"},
		})).To(Succeed())
		cog := awscommon.GetCognitoProviderClient().(*mock.CognitoProviderMock)

		refused := post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider})
		Expect(refused.StatusCode).To(Equal(http.StatusForbidden), refused.Body)
		Expect(refused.Body).To(MatchJSON(`{"message":"This application does not allow signing in with that provider."}`))
		Expect(cog.InitiateAuthCalls).To(BeZero(), "the second pool never saw the password")

		tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
	})

	It("signs in against the pool the request names, and against the first by name when it names none", func() {
		tokenSet(post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider}))
		unnamed := post(pathToken, map[string]any{"username": email, "password": secondPassword})
		Expect(unnamed.StatusCode).To(Equal(http.StatusUnauthorized), "the first pool by name does not hold this password (negative)")
	})

	It("stamps a password sign-in and its refresh with amr pwd and the pool's provider row as idp", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider}))
		refresh := tokenSet(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}))
		for _, token := range []any{signin["access_token"], signin["id_token"], refresh["access_token"], refresh["id_token"]} {
			Expect(claimsOf(token)["amr"]).To(Equal([]any{"pwd"}))
			Expect(claimsOf(token)["idp"]).To(Equal(secondProvider))
		}
	})

	It("changes the password in the pool the token signed in with, leaving the first pool's account alone", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider}))
		const newPassword = "Second#456"

		changed := changePassword(signin["access_token"], map[string]any{"old_password": secondPassword, "new_password": newPassword})
		Expect(changed.StatusCode).To(Equal(http.StatusOK), changed.Body)
		tokenSet(post(pathToken, map[string]any{"username": email, "password": newPassword, "provider": secondProvider}))
		tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
	})

	It("ignores a provider an old client still sends in the body, since the token's idp decides", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider}))
		const newPassword = "Second#456"

		changed := changePassword(signin["access_token"], map[string]any{"old_password": secondPassword, "new_password": newPassword, "provider": "cognito"})
		Expect(changed.StatusCode).To(Equal(http.StatusOK), changed.Body)
		tokenSet(post(pathToken, map[string]any{"username": email, "password": newPassword, "provider": secondProvider}))
		tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
	})

	It("refuses a token sent only in the body, since the caller is read from the Authorization header (RFC 6750 s2.1) (negative)", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		calls := cog.InitiateAuthCalls

		resp := post(pathPassword, map[string]any{"access_token": signin["access_token"], "old_password": password, "new_password": "NewPass#123"})
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(resp.Body).To(MatchJSON(`{"message":"Password change failed"}`))
		Expect(cog.InitiateAuthCalls).To(Equal(calls), "no pool is reached without a caller")
		tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
	})

	DescribeTable("refuses a bearer token that is not one of our user access tokens (negative)",
		func(token func() string) {
			bearer := token()
			calls := cog.InitiateAuthCalls

			resp := changePassword(bearer, map[string]any{"old_password": password, "new_password": "NewPass#123"})
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.Body).To(MatchJSON(`{"message":"Password change failed"}`))
			Expect(cog.InitiateAuthCalls).To(Equal(calls), "no pool is reached without a caller")
		},
		Entry("not a JWT", func() string { return "not-a-token" }),
		Entry("an id token", func() string {
			return tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))["id_token"].(string)
		}),
	)

	DescribeTable("refuses a sign-in with no password here, naming its provider, without reaching any pool (negative)",
		func(claims jwtutil.UserClaims, message string) {
			Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "google", DisplayName: "Google", Type: identity_providers_db.TypeOIDC, Enabled: utils.Ptr(true), ClientID: "google-client",
			})).To(Succeed())
			claims.Email = email
			token, err := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID).
				AccessToken("user-1", homeAppClientID, "openid email phone", neoResource, claims)
			Expect(err).NotTo(HaveOccurred())

			resp := changePassword(token, map[string]any{"old_password": password, "new_password": "NewPass#123"})
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Body).To(MatchJSON(`{"message":"` + message + `"}`))
			Expect(cog.InitiateAuthCalls).To(BeZero())
		},
		Entry("a federated sign-in, by the row's display name", jwtutil.UserClaims{Provider: "google"}, "You signed in through Google. This doesn't support password change."),
		Entry("a one-time code, by its idp since no row holds it", jwtutil.UserClaims{AMR: []string{"otp"}, Provider: "otp"}, "You signed in through otp. This doesn't support password change."),
	)

	It("changes the default password row's password for a token that names no idp", func() {
		mint := func() string {
			token, err := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID).
				AccessToken("user-1", homeAppClientID, "openid email phone", neoResource, jwtutil.UserClaims{Contact: jwtutil.Contact{Email: email}})
			Expect(err).NotTo(HaveOccurred())
			return token
		}
		const newPassword = "NewPass#456"

		other := changePassword(mint(), map[string]any{"old_password": secondPassword, "new_password": newPassword, "provider": secondProvider})
		Expect(other.StatusCode).To(Equal(http.StatusUnauthorized), "the default row's pool does not hold the second pool's password (negative)")

		changed := changePassword(mint(), map[string]any{"old_password": password, "new_password": newPassword})
		Expect(changed.StatusCode).To(Equal(http.StatusOK), changed.Body)
		tokenSet(post(pathToken, map[string]any{"username": email, "password": newPassword}))
		tokenSet(post(pathToken, map[string]any{"username": email, "password": secondPassword, "provider": secondProvider}))
	})

	It("refuses a refresh naming two resources, so no token is good at two APIs (negative)", func() {
		tokens := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		refresh := post(pathRefresh, map[string]any{"refresh_token": tokens["refresh_token"], "resource": []string{neoResource, accountResource}})
		Expect(refresh.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(refresh.Body).To(MatchJSON(`{"message":"Only one resource may be requested; a token is never issued for two audiences."}`))
	})

	It("signs every session out on a password change, the caller's included, so the person signs in again", func() {
		caller := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		other := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))

		changed := changePassword(caller["access_token"], map[string]any{"old_password": password, "new_password": "NewPass#123"})
		Expect(changed.StatusCode).To(Equal(http.StatusOK), changed.Body)

		Expect(post(pathRefresh, map[string]any{"refresh_token": other["refresh_token"]}).StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(post(pathRefresh, map[string]any{"refresh_token": caller["refresh_token"]}).StatusCode).To(Equal(http.StatusUnauthorized))
		tokenSet(post(pathToken, map[string]any{"username": email, "password": "NewPass#123"}))
	})

	It("signs every session out on a password reset", func() {
		signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))

		reset := post(pathRecoveryConfirm, map[string]any{"username": email, "code": resetCode, "new_password": "NewPass#123"})
		Expect(reset.StatusCode).To(Equal(http.StatusOK), reset.Body)

		Expect(post(pathRefresh, map[string]any{"refresh_token": signin["refresh_token"]}).StatusCode).To(Equal(http.StatusUnauthorized))
	})

	DescribeTable("answers a password that changed while the sessions could not be signed out with 500, saying the password did change (negative)",
		func(message string, send func(signin map[string]any) events.APIGatewayProxyResponse) {
			signin := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
			awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: awscommon.GetDynamoDBClient().(*mock.DynamoDBMock), IndexName: sessions_db.SessionsByUserIndex})

			resp := send(signin)
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(resp.Body).To(MatchJSON(`{"message":"` + message + `"}`))
		},
		Entry("a change", "Password changed, but sessions could not be signed out", func(signin map[string]any) events.APIGatewayProxyResponse {
			return changePassword(signin["access_token"], map[string]any{"old_password": password, "new_password": "NewPass#123"})
		}),
		Entry("a reset", "Password reset, but sessions could not be signed out", func(map[string]any) events.APIGatewayProxyResponse {
			return post(pathRecoveryConfirm, map[string]any{"username": email, "code": resetCode, "new_password": "NewPass#123"})
		}),
	)
})

var _ = Describe("resource refusals (RFC 8707 s2)", func() {
	DescribeTable("names what the client must fix",
		func(err error, message string) {
			resp, refused := resourceRefusal(err)
			Expect(refused).To(BeTrue())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(MatchJSON(`{"message":"` + message + `"}`))
		},
		Entry("resource not registered", auth.ErrResourceNotAllowed, "This client is not registered for the requested resource."),
		Entry("no resource and no default", auth.ErrResourceRequired, "A resource is required: this client has no default resource."),
		Entry("two resources", auth.ErrOneResourcePerToken, "Only one resource may be requested; a token is never issued for two audiences."),
		Entry("resource outside the authorization", auth.ErrResourceNotAuthorized, "The requested resource is not part of this authorization."),
		Entry("several authorized and none named", auth.ErrResourceAmbiguous, "This authorization covers several resources; name one with the resource parameter."),
		Entry("resource taken off the client", auth.ErrResourceNoLongerAllowed, "This login was issued for a resource the client may no longer request. Start a new authorization."),
	)

	It("leaves a refresh-token failure to the caller (negative)", func() {
		_, refused := resourceRefusal(auth.ErrInvalidGrant)
		Expect(refused).To(BeFalse())
	})
})

var _ = Describe("provider-specific wording", func() {
	// Both messages name RainMaker only when RainMaker's own pool is what backs the deployment;
	// against a registered external provider that sentence would name the wrong product.

	It("names RainMaker on signup for the inbuilt provider", func() {
		Expect(signupMessage(true)).To(Equal(
			"Code sent. Existing RainMaker users must signin or reset password."))
	})

	It("drops the product name on signup for an external provider (negative)", func() {
		Expect(signupMessage(false)).To(Equal(
			"Code sent. Existing users must signin or reset password."))
		Expect(signupMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("keeps the verify failure uniform but helpful, naming RainMaker on the inbuilt provider", func() {
		Expect(verifyFailedMessage(true)).To(Equal(
			"Invalid code or RainMaker account already exists. Try signin or reset password."))
	})

	It("drops the product name from the verify failure for an external provider (negative)", func() {
		Expect(verifyFailedMessage(false)).To(Equal(
			"Invalid code or account already exists. Try signin or reset password."))
		Expect(verifyFailedMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("keeps the signup and verify messages distinct, so one cannot be mistaken for the other", func() {
		Expect(signupMessage(true)).NotTo(Equal(verifyFailedMessage(true)))
		Expect(signupMessage(false)).NotTo(Equal(verifyFailedMessage(false)))
	})

	It("tells a proven owner to sign in, naming RainMaker on the inbuilt provider", func() {
		Expect(accountExistsMessage(true)).To(Equal("Account already exists. RainMaker users: signin or reset password."))
	})

	It("drops the product name from the account-exists message for an external provider (negative)", func() {
		Expect(accountExistsMessage(false)).To(Equal("Account already exists. Signin or reset password."))
		Expect(accountExistsMessage(false)).NotTo(ContainSubstring("RainMaker"))
	})

	It("tells an account an admin reset to reset its password, apart from the uniform refusal", func() {
		Expect(legacyauth.PasswordResetRequiredMessage).To(ContainSubstring("reset your password"))
		Expect(legacyauth.PasswordResetRequiredMessage).NotTo(Equal(legacyauth.UnconfirmedAccountMessage))
	})

	It("advises a password reset only for an unconfirmed sign-in", func() {
		Expect(legacyauth.UnconfirmedAccountMessage).To(ContainSubstring("reset your password"))
		// Distinct from the uniform refusal, which is what makes the branch observable at all.
		Expect(legacyauth.UnconfirmedAccountMessage).NotTo(Equal("Authentication failed"))
	})
})

var _ = Describe("against a pool with no such account", func() {
	var cog *mock.CognitoProviderMock

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		cog = mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect("eu-west-2_testpool", "native-client")
		awscommon.SetCognitoProviderClient(cog)
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, Enabled: utils.Ptr(true),
			ClientID: "native-client", ClientSecret: "provider-client-secret",
		})).To(Succeed())
	})

	post := func(path string, body map[string]any, headers map[string]string) events.APIGatewayProxyResponse {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		resp, err := handler(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: path, Body: string(raw), Headers: headers})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	DescribeTable("answers a password the policy refuses as one to fix, in one sentence of ours rather than Cognito's",
		func(path string, body map[string]any) {
			refused := &cognitotypes.InvalidPasswordException{Message: aws.String("Password did not conform with policy: Password must have symbol characters")}
			cog.SignUpError, cog.ConfirmForgotPasswordError = refused, refused
			resp := post(path, body, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(MatchJSON(`{"message":"Password does not meet the password requirements"}`))
		},
		Entry("at signup", pathSignup, map[string]any{"email": "new@example.com", "password": "Password1"}),
		Entry("at recovery confirmation", pathRecoveryConfirm, map[string]any{"username": "u@example.com", "code": "123456", "new_password": "Password1"}),
	)

	It("keeps a wrong recovery code on the route's own answer, never a password refusal (negative)", func() {
		cog.ConfirmForgotPasswordError = &cognitotypes.CodeMismatchException{Message: aws.String("Invalid verification code provided, please try again.")}
		resp := post(pathRecoveryConfirm, map[string]any{"username": "u@example.com", "code": "000000", "new_password": "NewPass#123"}, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Body).To(MatchJSON(`{"message":"Invalid verification code"}`))
	})

	// The sign-in page signs in through POST /user/login/password, so these routes keep the app's shape whatever a browser sends; refresh and signout reach no pool, so they ignore a provider too.
	DescribeTable("answers the native routes as the app API, reading no flow cookie",
		func(path string, body map[string]any, status int, message string) {
			resp := post(path, body, map[string]string{"Cookie": auth.FlowCookieName + "=fl_browser"})
			Expect(resp.StatusCode).To(Equal(status))
			Expect(resp.Body).To(MatchJSON(`{"message":"` + message + `"}`))
		},
		Entry("token", pathToken, map[string]any{"username": "legacy@example.com", "password": "Password1!", "flow_id": "fl_browser"},
			http.StatusUnauthorized, "Authentication failed"),
		Entry("refresh", pathRefresh, map[string]any{"refresh_token": "not-a-token", "provider": "nope"}, http.StatusUnauthorized, "Invalid refresh token"),
		Entry("signout", pathSignout, map[string]any{"provider": "nope"}, http.StatusBadRequest, "Refresh token is required"),
		Entry("password", pathPassword, map[string]any{"old_password": "a", "new_password": "b"},
			http.StatusUnauthorized, "Password change failed"),
	)

	DescribeTable("refuses an account route naming a provider that is not a password pool, rather than using another pool (negative)",
		func(path, provider string) {
			Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "retired", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, Enabled: utils.Ptr(false),
				ClientID: "native-client", ClientSecret: "provider-client-secret",
			})).To(Succeed())
			resp := post(path, map[string]any{"email": "new@example.com", "username": "new@example.com", "code": "123456", "password": "Password1!", "new_password": "Password1!", "provider": provider}, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(MatchJSON(`{"message":"Unknown provider."}`), "the account routes' own error shape")
		},
		Entry("signup, a name that does not exist", pathSignup, "nope"),
		Entry("signup verification, a name that does not exist", pathSignupVerify, "nope"),
		Entry("recovery, a name that does not exist", pathRecovery, "nope"),
		Entry("recovery confirmation, a name that does not exist", pathRecoveryConfirm, "nope"),
		Entry("sign-in, a name that does not exist", pathToken, "nope"),
		Entry("signup, a disabled password row", pathSignup, "retired"),
		Entry("sign-in, a disabled password row", pathToken, "retired"),
	)

	It("keeps the native token route's uniform refusal when the pool fails rather than refuses", func() {
		cog.InitiateAuthError = &cognitotypes.TooManyRequestsException{Message: aws.String("Rate exceeded")}
		resp := post(pathToken, map[string]any{"username": "u@example.com", "password": "Password1!"}, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(resp.Body).To(MatchJSON(`{"message":"Authentication failed"}`))
	})
})
