// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
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
		poolID   = "eu-west-2_testpool"
		clientID = "native-client"
		email    = "legacy@example.com"
		password = "Password1!"
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
	})

	post := func(path string, body map[string]string) events.APIGatewayProxyResponse {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		resp, err := handler(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: path, Body: string(raw)})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	It("marks the signin and refresh token sets uncacheable", func() {
		signin := post(pathToken, map[string]string{"username": email, "password": password})
		Expect(signin.StatusCode).To(Equal(http.StatusOK))
		Expect(signin.Headers).To(HaveKeyWithValue("Cache-Control", "no-store"))
		Expect(signin.Headers).To(HaveKeyWithValue("Pragma", "no-cache"))

		var tokens map[string]any
		Expect(json.Unmarshal([]byte(signin.Body), &tokens)).To(Succeed())
		refresh := post(pathRefresh, map[string]string{"refresh_token": tokens["refresh_token"].(string)})
		Expect(refresh.StatusCode).To(Equal(http.StatusOK))
		Expect(refresh.Headers).To(HaveKeyWithValue("Cache-Control", "no-store"))
		Expect(refresh.Headers).To(HaveKeyWithValue("Pragma", "no-cache"))
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
