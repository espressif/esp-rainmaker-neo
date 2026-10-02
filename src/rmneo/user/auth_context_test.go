// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// NewContextWithAPIRequest is the single seam every rmneo handler resolves its caller
// through -- group, node, claim, alexa, gva and smartthings, some forty-odd entry points --
// so what it accepts is asserted once here rather than restated in each handler's suite.
//
// The bearer branch runs only when the gateway left no identity on the request, and the
// refusals below are the whole of what stands between a token and every one of those
// handlers. They live in src/espuser/auth, a package away from the callers that depend on
// them, which is exactly why they are pinned from this side.
var _ = Describe("NewContextWithAPIRequest", func() {
	const (
		caller     = "u_amey"
		gatewayGuy = "u_from_the_gateway"
		firstParty = "esp-account-dashboard"
		grant      = "openid email"
	)

	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
	})

	AfterEach(func() {
		backend.Close()
	})

	minter := func() *jwtutil.Minter {
		return jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
	}

	// No CognitoAuthenticationProvider on the request: the gateway authorized nobody, which
	// is the condition that hands the caller to the bearer branch (auth.extractCallerIdentity).
	bearer := func(token string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			Headers: map[string]string{"Authorization": "Bearer " + token},
		}
	}

	seedUser := func(userID string) {
		rctx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		Expect(user_details_db.NewUserDetailsDB(rctx).CreateUserDetails(&user_details_db.UserDetailsEntry{
			UserID:   userID,
			Email:    userID + "@example.com",
			UserType: user_details_db.UserTypeUser,
			Provider: user_details_db.ProviderOIDC,
		})).To(Succeed())
	}

	It("resolves a first-party user access token and carries its claims to the handler", func() {
		token, err := minter().AccessToken(caller, firstParty, grant, "", "", jwtutil.Contact{}, "sid_CHROME")
		Expect(err).NotTo(HaveOccurred())

		rctx := user.NewContextWithAPIRequest(context.Background(), bearer(token))
		Expect(rctx).NotTo(BeNil())
		Expect(rctx.GetAccessor().GetID()).To(Equal(caller), "the handler acts as the token's subject")

		// The claims ride the context so a handler can gate on scope or sid without re-parsing.
		claims := user.ClaimsFrom(rctx)
		Expect(claims.Subject).To(Equal(caller))
		Expect(claims.ClientID).To(Equal(firstParty))
		Expect(claims.Scope).To(Equal(grant))
		Expect(claims.SID).To(Equal("sid_CHROME"))
	})

	DescribeTable("refuses a bearer credential that does not speak for a person (negative)",
		func(build func() string, why string) {
			Expect(user.NewContextWithAPIRequest(context.Background(), bearer(build()))).To(BeNil(), why)
		},
		Entry("a client_credentials token", func() string {
			token, err := minter().ClientCredentialsToken(firstParty, grant, "")
			Expect(err).NotTo(HaveOccurred())
			return token
		}, "RFC 6749 s4.4: its sub is a client id, so admitting it would make a machine the owner of every user_id-keyed row it touched"),

		Entry("an id token presented as an access token", func() string {
			token, err := minter().IDToken(caller, firstParty, "", 0, jwtutil.Contact{})
			Expect(err).NotTo(HaveOccurred())
			return token
		}, "RFC 9700 token substitution: an id token is minted for the client, never for an API"),

		Entry("a token signed by somebody else's key", func() string {
			// A fresh key, not the suite's shared one, or this would assert nothing.
			other, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).NotTo(HaveOccurred())
			token, err := jwtutil.NewMinter(backend.Issuer, other, oidc.SigningKeyID).
				AccessToken(caller, firstParty, grant, "", "", jwtutil.Contact{})
			Expect(err).NotTo(HaveOccurred())
			return token
		}, "a valid-looking token from an untrusted signer is the whole point of verifying the signature"),

		Entry("a token from another issuer", func() string {
			token, err := jwtutil.NewMinter("https://evil.example.com", backend.SigningKey, oidc.SigningKeyID).
				AccessToken(caller, firstParty, grant, "", "", jwtutil.Contact{})
			Expect(err).NotTo(HaveOccurred())
			return token
		}, "our own key can only vouch for our own issuer"),

		Entry("not a JWT at all", func() string {
			return "not-a-token"
		}, "malformed input must refuse rather than panic"),

		Entry("an empty bearer value", func() string {
			return ""
		}, "an Authorization header with nothing in it is no credential"),
	)

	It("returns nil when the request carries neither a gateway identity nor a token", func() {
		Expect(user.NewContextWithAPIRequest(context.Background(), events.APIGatewayProxyRequest{})).To(BeNil())
	})

	It("keeps the gateway's identity when one is present, whoever the bearer token names", func() {
		seedUser(gatewayGuy)
		token, err := minter().AccessToken(caller, firstParty, grant, "", "", jwtutil.Contact{})
		Expect(err).NotTo(HaveOccurred())

		request := bearer(token)
		request.RequestContext.Identity.CognitoAuthenticationProvider = backend.Issuer + ":" + gatewayGuy

		rctx := user.NewContextWithAPIRequest(context.Background(), request)
		Expect(rctx).NotTo(BeNil())
		Expect(rctx.GetAccessor().GetID()).To(Equal(gatewayGuy), "the gateway resolved the caller; the token is never consulted")

		// Documented on User.Claims: an AWS_IAM route survives the identity-pool exchange with
		// a subject and nothing else, so a handler must not expect scope or sid there.
		Expect(user.ClaimsFrom(rctx)).To(Equal(auth.TokenClaims{}))
	})
})
