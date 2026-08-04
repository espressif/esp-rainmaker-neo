// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"

	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMCPAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCP OIDC Auth Suite")
}

const testClientID = "this-servers-client-id"

type stubUser struct {
	id  string
	ctx context.Context
}

func (s stubUser) GetUserID() string          { return s.id }
func (s stubUser) GoContext() context.Context { return s.ctx }

var _ = Describe("MCP OIDC authenticator", func() {
	var (
		backend  *test_utils.EspUserBackend
		auth     Authenticator
		resolved string
	)

	signToken := func(kid string, claims jwt.MapClaims) string {
		tok, err := jwtutil.SignRS256(claims, backend.SigningKey, kid)
		Expect(err).NotTo(HaveOccurred())
		return tok
	}

	validAccessClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"iss":       backend.Issuer,
			"sub":       "user-abc",
			"aud":       testClientID,
			"exp":       time.Now().Add(time.Hour).Unix(),
			"iat":       time.Now().Unix(),
			"token_use": jwtutil.TokenUseAccess,
		}
	}

	requestWith := func(token string) events.APIGatewayV2HTTPRequest {
		return events.APIGatewayV2HTTPRequest{Headers: map[string]string{"authorization": "Bearer " + token}}
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		resolved = ""
		resolver := func(ctx context.Context, userID string) (UserContext, error) {
			resolved = userID
			return stubUser{id: userID, ctx: ctx}, nil
		}
		auth = NewOIDCAuthenticator(backend.Issuer, testClientID, test_utils.EspUserJWKSParam, resolver)
	})

	AfterEach(func() { backend.Close() })

	It("accepts a well-formed access token and resolves its sub", func() {
		user, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, validAccessClaims())))
		Expect(err).NotTo(HaveOccurred())
		Expect(user.GetUserID()).To(Equal("user-abc"))
		Expect(resolved).To(Equal("user-abc"))
	})

	It("rejects a missing Authorization header (negative)", func() {
		_, err := auth(context.Background(), events.APIGatewayV2HTTPRequest{Headers: map[string]string{}})
		Expect(err).To(HaveOccurred())
	})

	It("rejects a header without the Bearer scheme (negative)", func() {
		_, err := auth(context.Background(), events.APIGatewayV2HTTPRequest{Headers: map[string]string{"authorization": "Basic xyz"}})
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token from the wrong issuer (negative)", func() {
		claims := validAccessClaims()
		claims["iss"] = "https://evil.example.com"
		_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token minted for a different audience/client (negative)", func() {
		claims := validAccessClaims()
		claims["aud"] = "some-other-client"
		_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
		Expect(err).To(HaveOccurred())
	})

	It("rejects an expired token (negative)", func() {
		claims := validAccessClaims()
		claims["exp"] = time.Now().Add(-time.Minute).Unix()
		_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
		Expect(err).To(HaveOccurred())
	})

	It("rejects an id token on the resource path — only access tokens authorize (negative, RFC 9700)", func() {
		claims := validAccessClaims()
		claims["token_use"] = jwtutil.TokenUseID
		_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
		Expect(err).To(HaveOccurred())
	})

	It("rejects a token signed under an unknown kid (negative, no matching JWK)", func() {
		_, err := auth(context.Background(), requestWith(signToken("unknown-kid", validAccessClaims())))
		Expect(err).To(HaveOccurred())
	})
})
