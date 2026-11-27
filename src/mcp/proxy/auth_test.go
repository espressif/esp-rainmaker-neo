// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
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

// The MCP server's two names: its execute-api URL and its custom domain's, each with the /v1/mcp resource path.
const (
	mcpExecuteAPI = "https://abc123.execute-api.us-east-1.amazonaws.com/v1/mcp"
	mcpDomain     = "https://mcp.example.com/v1/mcp"
)

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
			"aud":       mcpDomain,
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
		auth = NewOIDCAuthenticator(backend.Issuer, []string{mcpExecuteAPI, mcpDomain}, test_utils.EspUserJWKSParam, resolver)
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

	It("accepts a token addressed to the server's execute-api name, so tokens minted before a domain was mapped keep working", func() {
		claims := validAccessClaims()
		claims["aud"] = mcpExecuteAPI
		_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("rejects a token addressed to anything but this server (negative, RFC 9068 s4)",
		func(aud string) {
			claims := validAccessClaims()
			claims["aud"] = aud
			_, err := auth(context.Background(), requestWith(signToken(oidc.SigningKeyID, claims)))
			Expect(err).To(HaveOccurred())
		},
		Entry("the proxy's client id", "mcp-oauth-client"),
		Entry("another of our APIs", "https://api.example.com"),
	)

	It("rejects every token when the server was given no audiences (negative)", func() {
		closed := NewOIDCAuthenticator(backend.Issuer, nil, test_utils.EspUserJWKSParam, func(ctx context.Context, userID string) (UserContext, error) {
			return stubUser{id: userID, ctx: ctx}, nil
		})
		_, err := closed(context.Background(), requestWith(signToken(oidc.SigningKeyID, validAccessClaims())))
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
