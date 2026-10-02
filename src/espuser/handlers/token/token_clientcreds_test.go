// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	ccClientID = "rm_svc"
	ccResource = "https://api.accounts.example.com"
	ccOtherAPI = "https://api.secvuln.example.com"
	ccScopes   = "account.admin credits.consume"
)

// seedConfidentialClient registers a confidential client and returns its one-time secret.
func seedConfidentialClient(clientID string, allowedResources []string) string {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	res, err := svc.Create(clients.CreateInput{
		ClientID:         clientID,
		ClientName:       "Service",
		ClientType:       "confidential",
		GrantTypes:       []string{"client_credentials"},
		Scopes:           strings.Fields(ccScopes),
		AllowedResources: allowedResources,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(res.ClientSecret).NotTo(BeEmpty())
	return res.ClientSecret
}

// unverifiedClaims reads a token's payload without verifying it -- these specs assert on what
// the endpoint put in the token, which is a separate question from whether it verifies.
func unverifiedClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	Expect(parts).To(HaveLen(3), "not a JWT: %q", token)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	Expect(err).NotTo(HaveOccurred())
	var claims map[string]any
	Expect(json.Unmarshal(payload, &claims)).To(Succeed())
	return claims
}

func basicAuth(clientID, secret string) map[string]string {
	raw := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + secret))
	return map[string]string{
		"Content-Type":  "application/x-www-form-urlencoded",
		"Authorization": "Basic " + raw,
	}
}

func ccRequest(headers map[string]string, kv map[string]string) events.APIGatewayProxyRequest {
	v := url.Values{}
	for k, val := range kv {
		v.Set(k, val)
	}
	return events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       pathToken,
		Headers:    headers,
		Body:       v.Encode(),
	}
}

var _ = Describe("OAuth token endpoint (client_credentials grant)", func() {
	var secret string

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		secret = seedConfidentialClient(ccClientID, []string{ccResource})
	})

	Describe("the happy path", func() {
		It("issues one access token and nothing else, and the token verifies", func() {
			// Three assertions about one response, so one exchange makes them: what is present,
			// what is deliberately absent, and that the thing returned is a token a resource
			// server would actually accept rather than merely a non-empty string.
			//
			// Absent by design: a refresh token is a way to act on an absent user's behalf later,
			// and there is no user here -- the client already holds credentials it can present
			// again. An ID token describes a human, and there is none; a machine client is not
			// registered for `openid` either, so asking for one is refused a step earlier.
			resp, err := handleTokenRequest(context.Background(), ccRequest(
				basicAuth(ccClientID, secret),
				map[string]string{"grant_type": "client_credentials", "scope": "account.admin"},
			))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))

			body := decodeBody[map[string]any](resp)
			Expect(body["token_type"]).To(Equal("Bearer"))
			Expect(body["expires_in"]).NotTo(BeZero())
			_, hasRefresh := body["refresh_token"]
			Expect(hasRefresh).To(BeFalse(), "there is no user to act for later")
			_, hasID := body["id_token"]
			Expect(hasID).To(BeFalse(), "there is no human to describe")

			access, ok := body["access_token"].(string)
			Expect(ok).To(BeTrue())
			claims := unverifiedClaims(access)
			Expect(claims["token_use"]).To(Equal("access"))
			Expect(claims["client_id"]).To(Equal(ccClientID))
			Expect(claims["sub"]).To(Equal(ccClientID), "RFC 9068 s2.2.1: with no resource owner, sub is the client")
			Expect(claims["exp"]).NotTo(BeNil())
		})

		It("accepts client_secret_post as well as Basic", func() {
			// Both must work: Google account linking sends body credentials, Alexa sends Basic.
			resp, err := handleTokenRequest(context.Background(), ccRequest(
				map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
				map[string]string{
					"grant_type": "client_credentials", "client_id": ccClientID,
					"client_secret": secret, "scope": "account.admin",
				},
			))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
		})

	})

	Describe("the claims", func() {
		mint := func(kv map[string]string) map[string]any {
			kv["grant_type"] = "client_credentials"
			resp, err := handleTokenRequest(context.Background(), ccRequest(basicAuth(ccClientID, secret), kv))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200), resp.Body)
			raw, _ := decodeBody[map[string]any](resp)["access_token"].(string)
			return unverifiedClaims(raw)
		}

		It("sets sub to the client itself (RFC 9068: no resource owner is involved)", func() {
			claims := mint(map[string]string{"scope": "account.admin"})
			Expect(claims["sub"]).To(Equal(ccClientID))
			Expect(claims["client_id"]).To(Equal(ccClientID))
			Expect(claims["token_use"]).To(Equal("access"))
		})

		It("sets aud to the requested resource, not the client", func() {
			// The point of the whole exercise: aud names the callee. A resource server can
			// then refuse a token minted for a different API.
			claims := mint(map[string]string{"scope": "account.admin", "resource": ccResource})
			Expect(claims["aud"]).To(Equal(ccResource))
			Expect(claims["aud"]).NotTo(Equal(claims["client_id"]))
		})

		It("falls back to aud = client_id when no resource is requested", func() {
			// RFC 8707 is an optional extension; a caller that does not use it must still get
			// today's shape rather than a token with no audience at all.
			claims := mint(map[string]string{"scope": "account.admin"})
			Expect(claims["aud"]).To(Equal(ccClientID))
		})

		It("grants only the scopes asked for", func() {
			claims := mint(map[string]string{"scope": "credits.consume"})
			Expect(claims["scope"]).To(Equal("credits.consume"))
		})

		It("defaults to the client's registered scopes when none are requested", func() {
			claims := mint(map[string]string{})
			Expect(strings.Fields(claims["scope"].(string))).To(ConsistOf("account.admin", "credits.consume"))
		})
	})

	Describe("client authentication (negative)", func() {
		expectError := func(resp events.APIGatewayProxyResponse, status int, code string) {
			Expect(resp.StatusCode).To(Equal(status), resp.Body)
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal(code))
		}

		It("rejects a wrong secret", func() {
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				basicAuth(ccClientID, "not-the-secret"),
				map[string]string{"grant_type": "client_credentials"},
			))
			expectError(resp, 401, "invalid_client")
		})

		It("rejects an unknown client with the SAME error as a wrong secret", func() {
			// Otherwise the endpoint is an oracle for which client ids exist (RFC 6749 s5.2).
			unknown, _ := handleTokenRequest(context.Background(), ccRequest(
				basicAuth("rm_does_not_exist", "whatever"),
				map[string]string{"grant_type": "client_credentials"},
			))
			expectError(unknown, 401, "invalid_client")
		})

		It("rejects a request carrying no client credentials at all", func() {
			// The grant IS client authentication. Without credentials there is nothing to
			// authenticate, and it must not fall through as an anonymous request.
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
				map[string]string{"grant_type": "client_credentials"},
			))
			expectError(resp, 401, "invalid_client")
		})

		It("rejects a public client even with correct client_id", func() {
			// A public client cannot keep a secret, so it cannot authenticate as itself.
			seedPublicClient("rm_public_svc")
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
				map[string]string{"grant_type": "client_credentials", "client_id": "rm_public_svc"},
			))
			Expect(resp.StatusCode).To(BeNumerically(">=", 400))
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("unauthorized_client"))
		})
	})

	Describe("scope and resource (negative)", func() {
		It("rejects a scope the client is not registered for", func() {
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				basicAuth(ccClientID, secret),
				map[string]string{"grant_type": "client_credentials", "scope": "account.admin nope.write"},
			))
			Expect(resp.StatusCode).To(Equal(400), resp.Body)
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("invalid_scope"))
		})

		It("rejects a resource the client is not registered for", func() {
			// Without this the parameter is decorative: any client could mint a token for
			// any API simply by asking.
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				basicAuth(ccClientID, secret),
				map[string]string{"grant_type": "client_credentials", "scope": "account.admin", "resource": ccOtherAPI},
			))
			Expect(resp.StatusCode).To(Equal(400), resp.Body)
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("invalid_target"))
		})

		It("rejects any resource when the client registered none", func() {
			s2 := seedConfidentialClient("rm_svc_nores", nil)
			resp, _ := handleTokenRequest(context.Background(), ccRequest(
				basicAuth("rm_svc_nores", s2),
				map[string]string{"grant_type": "client_credentials", "scope": "account.admin", "resource": ccResource},
			))
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("invalid_target"))
		})

		It("never mints a multi-audience token", func() {
			// One resource, one aud. A client needing two APIs makes two
			// requests, so that a leaked token is useful against exactly one of them.
			raw := "grant_type=client_credentials&scope=account.admin" +
				"&resource=" + url.QueryEscape(ccResource) +
				"&resource=" + url.QueryEscape(ccOtherAPI)
			resp, _ := handleTokenRequest(context.Background(), events.APIGatewayProxyRequest{
				HTTPMethod: "POST", Path: pathToken,
				Headers: basicAuth(ccClientID, secret), Body: raw,
				MultiValueQueryStringParameters: map[string][]string{},
			})
			Expect(resp.StatusCode).To(Equal(400), resp.Body)
			Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("invalid_target"))
		})
	})

	Describe("registration", func() {
		It("refuses client_credentials on a public client at write time", func() {
			svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			_, err := svc.Create(clients.CreateInput{
				ClientID: "rm_pub_cc", ClientName: "App", ClientType: "public",
				RedirectURIs: []string{"com.example://cb"},
				GrantTypes:   []string{"client_credentials"},
				RequirePKCE:  utils.Ptr(true),
			})
			Expect(err).To(HaveOccurred(), "a public client cannot hold credentials to present")
		})

		It("rejects a relative or fragment-bearing allowed_resource at write time", func() {
			svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			for _, bad := range []string{"/api", "api.example.com", "https://api.example.com/x#frag"} {
				_, err := svc.Create(clients.CreateInput{
					ClientID: "rm_bad_res", ClientName: "S", ClientType: "confidential",
					GrantTypes: []string{"client_credentials"}, AllowedResources: []string{bad},
				})
				Expect(err).To(HaveOccurred(), "RFC 8707 requires an absolute URI without a fragment: %q", bad)
			}
		})
	})
})
