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
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
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
	)

	BeforeEach(func() {
		backend := test_utils.SetupEspUserBackend(context.Background())
		cog := mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect(poolID, clientID)
		cog.AddTestUserDirect(poolID, email, email, password, true)
		cog.IDTokenMinter = func(*mock.UserState) string {
			tok, err := jwtutil.SignRS256(jwt.MapClaims{"email": email, "email_verified": true}, backend.SigningKey, oidc.SigningKeyID)
			Expect(err).NotTo(HaveOccurred())
			return tok
		}
		awscommon.SetCognitoProviderClient(cog)
		yes := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, Enabled: &yes,
			ClientID: clientID, ClientSecret: "provider-client-secret", PasswordGrant: &yes,
		})).To(Succeed())
		// The password API signs in as the Home app client, covering every resource it is granted.
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: homeAppClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"com.example://cb"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{neoResource, accountResource}, DefaultResource: neoResource,
		})).To(Succeed())
	})

	post := func(path string, body map[string]any) events.APIGatewayProxyResponse {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		resp, err := handler(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: path, Body: string(raw)})
		Expect(err).NotTo(HaveOccurred())
		return resp
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

	It("refuses a refresh naming two resources, so no token is good at two APIs (negative)", func() {
		tokens := tokenSet(post(pathToken, map[string]any{"username": email, "password": password}))
		refresh := post(pathRefresh, map[string]any{"refresh_token": tokens["refresh_token"], "resource": []string{neoResource, accountResource}})
		Expect(refresh.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(refresh.Body).To(MatchJSON(`{"message":"Only one resource may be requested; a token is never issued for two audiences."}`))
	})
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

	It("advises a password reset only for an unconfirmed sign-in", func() {
		Expect(unconfirmedAccountMessage).To(ContainSubstring("reset your password"))
		// Distinct from the uniform refusal, which is what makes the branch observable at all.
		Expect(unconfirmedAccountMessage).NotTo(Equal("Authentication failed"))
	})
})
