// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"net/url"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTokenHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Token Endpoint Suite")
}

const testClientID = "rm_mobile"

func formRequest(body string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Path:       pathToken,
		Headers:    map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:       body,
	}
}

func decodeBody[T any](resp events.APIGatewayProxyResponse) T {
	var out T
	Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
	return out
}

// seedPublicClient registers a public client so the token endpoint's client auth lets it through.
func seedPublicClient(clientID string) {
	db := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	Expect(db.CreateClient(&oauth_clients_db.OAuthClientEntry{
		ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, RequirePKCE: utils.Ptr(true),
	})).To(Succeed())
}

// mintRefreshToken seeds a live refresh-token family and returns its opaque token.
func mintRefreshToken(userID, scope string) string {
	svc := refreshtoken.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	token, err := svc.MintRefreshtoken(userID, testClientID, scope)
	Expect(err).NotTo(HaveOccurred())
	return token
}

var _ = Describe("OAuth token endpoint (refresh_token grant)", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		seedPublicClient(testClientID)
	})

	form := func(kv map[string]string) string {
		v := url.Values{}
		for k, val := range kv {
			v.Set(k, val)
		}
		return v.Encode()
	}

	Describe("refresh_token grant", func() {
		It("rotates a valid refresh token and returns a fresh token set (openid → id_token present)", func() {
			token := mintRefreshToken("user-123", "openid email")
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))

			body := decodeBody[map[string]any](resp)
			Expect(body["access_token"]).NotTo(BeEmpty())
			Expect(body["id_token"]).NotTo(BeEmpty())
			Expect(body["token_type"]).To(Equal("Bearer"))
			// The returned refresh token is new (the presented one is now spent).
			Expect(body["refresh_token"]).NotTo(Equal(token))
		})

		It("omits id_token when openid is not in scope (negative)", func() {
			token := mintRefreshToken("user-123", "email")
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
			_, hasID := decodeBody[map[string]any](resp)["id_token"]
			Expect(hasID).To(BeFalse())
		})

		It("re-issues the rotated token on a lost-response retry within the grace window", func() {
			token := mintRefreshToken("user-123", "openid")
			first, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(first.StatusCode).To(Equal(200))
			rotated := decodeBody[map[string]any](first)["refresh_token"]

			// Re-presenting the just-spent token within the grace window is a lost-response retry:
			// it re-issues the rotated token rather than failing as reuse.
			retry, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(retry.StatusCode).To(Equal(200))
			Expect(decodeBody[map[string]any](retry)["refresh_token"]).To(Equal(rotated))
		})

		It("rejects replay of a token more than one step behind with invalid_grant (reuse=theft)", func() {
			token := mintRefreshToken("user-123", "openid")
			first, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(first.StatusCode).To(Equal(200))
			rotated := decodeBody[map[string]any](first)["refresh_token"].(string)

			// Rotate again so the family advances past the one-step grace.
			second, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": rotated, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(second.StatusCode).To(Equal(200))

			// The original is now two steps behind — genuine reuse, which revokes the family.
			replay, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(replay.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](replay).Error).To(Equal("invalid_grant"))
		})

		It("rejects an unknown token with invalid_grant (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": "nope.nope", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_grant"))
		})

		It("rejects a token presented under a different registered client_id (negative, per-client scoping)", func() {
			token := mintRefreshToken("user-123", "openid")
			seedPublicClient("other_client") // registered, so it clears client auth but not the token's per-client scoping
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": "other_client",
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_grant"))
		})

		It("rejects a missing refresh_token or client_id with invalid_request (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_request"))
		})
	})

	Describe("grant dispatch", func() {
		It("rejects an unsupported grant_type (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "client_credentials", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("unsupported_grant_type"))
		})

		It("rejects a missing grant_type with invalid_request (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"refresh_token": "a.b", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_request"))
		})
	})

	Describe("routing", func() {
		It("rejects non-POST methods (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: "GET", Path: pathToken})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(405))
		})
	})
})
