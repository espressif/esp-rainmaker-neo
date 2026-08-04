// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package legacyauth

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

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
)

// seedPasswordProvider registers the enabled provider row NewService resolves its app client from.
func seedPasswordProvider(clientID string) {
	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
	yes := true
	Expect(identity_providers_db.NewIdentityProvidersDB(rmngCtx).CreateProvider(&identity_providers_db.ProviderEntry{
		ProviderName:  "cognito",
		Type:          identity_providers_db.TypeOIDC,
		Enabled:       &yes,
		ClientID:      clientID,
		ClientSecret:  "provider-client-secret",
		PasswordGrant: &yes,
	})).To(Succeed())
}

var _ = Describe("SigninWithPassword", func() {
	var backend *test_utils.EspUserBackend

	newService := func(email string, emailVerified bool) *Service {
		cog := mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect(testPoolID, testClientID2)
		cog.AddTestUserDirect(testPoolID, email, email, testPassword, true)
		cog.IDTokenMinter = func(user *mock.UserState) string {
			tok, err := jwtutil.SignRS256(jwt.MapClaims{
				"email": email, "email_verified": emailVerified,
			}, backend.SigningKey, oidc.SigningKeyID)
			Expect(err).NotTo(HaveOccurred())
			return tok
		}
		awscommon.SetCognitoProviderClient(cog)

		seedPasswordProvider(testClientID2)
		svc, err := NewService(context.Background())
		Expect(err).NotTo(HaveOccurred())
		return svc
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
		Expect(claims["aud"]).To(Equal(legacyClientID))
		Expect(claims["scope"]).To(Equal(legacyScope))
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

	It("fails closed when no enabled provider offers a password grant (negative)", func() {
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		yes := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngCtx).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "oidc-only",
			Type:         identity_providers_db.TypeOIDC,
			Enabled:      &yes,
			ClientID:     "some-client",
		})).To(Succeed())

		_, err := NewService(context.Background())
		Expect(err).To(MatchError(ErrNoPasswordProvider))
	})

	It("rejects a password change whose access token is not ours (negative)", func() {
		svc := newService("u@example.com", true)
		err := svc.ChangePassword(context.Background(), "not-a-jwt", testPassword, "NewPass#123")
		Expect(err).To(HaveOccurred())
	})

	It("rejects a password change with a missing field (negative)", func() {
		svc := newService("u@example.com", true)
		Expect(svc.ChangePassword(context.Background(), "", testPassword, "NewPass#123")).NotTo(Succeed())
		Expect(svc.ChangePassword(context.Background(), "tok", "", "NewPass#123")).NotTo(Succeed())
		Expect(svc.ChangePassword(context.Background(), "tok", testPassword, "")).NotTo(Succeed())
	})

	It("changes the password when the caller's own token and old password both check out", func() {
		svc := newService("u@example.com", true)
		tokens, err := svc.SigninWithPassword(context.Background(), "u@example.com", testPassword)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.ChangePassword(context.Background(), tokens.AccessToken, testPassword, "NewPass#123")).To(Succeed())
	})

	Describe("enumeration resistance", func() {
		It("reports signup success for an already confirmed account without sending a code", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "u@example.com", "", testPassword)).To(Succeed())
		})

		It("reports signup success for an unconfirmed account by re-sending the code", func() {
			svc := newService("u@example.com", true)
			Expect(svc.Signup(context.Background(), "fresh@example.com", "", testPassword)).To(Succeed())
			Expect(svc.Signup(context.Background(), "fresh@example.com", "", testPassword)).To(Succeed())
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
