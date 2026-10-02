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

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
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
	token, err := svc.MintRefreshtoken(userID, testClientID, scope, "", "", 0)
	Expect(err).NotTo(HaveOccurred())
	return token
}

var _ = Describe("OAuth token endpoint (refresh_token grant)", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
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
		It("keeps minting today's audience for a family that carries no resource", func() {
			// Families predating the resource field store none. They must go on working and
			// go on producing aud = client_id, never an empty audience.
			token := mintRefreshToken("user-legacy", "openid email")
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": token, "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200), resp.Body)
			claims := unverifiedClaims(decodeBody[map[string]any](resp)["access_token"].(string))
			Expect(claims["aud"]).To(Equal(testClientID))
		})

		It("rotates a valid refresh token and returns a fresh token set (openid → id_token present)", func() {
			token := mintRefreshToken("user-123", "openid email")
			// Reset after minting, so the profile below measures only the refresh itself.
			backend.DBMock.ProfileReset()
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

			// Three reads and one write per refresh:
			//
			//   1  GetItem    espuser-oauth-clients   client authentication
			//   2  GetItem    espuser-refresh-tokens  the family row, by user + client#family
			//   3  UpdateItem espuser-refresh-tokens  rotation: the new token replaces the old (write)
			//   4  GetItem    espuser-user-details    the profile claims baked into the id token
			//
			// One write, not two: rotation updates the family row in place rather than writing a new
			// row and deleting the old. This is the endpoint every signed-in app hits on a timer, so
			// its per-call cost multiplies hardest across the fleet -- read 4 in particular is a whole
			// extra GetItem spent on claims that rarely change between one refresh and the next.
			profile := backend.DBMock.ProfileGet()
			readCnt, writeCnt := profile.TotalCounts()
			Expect(readCnt).To(Equal(3))
			Expect(writeCnt).To(Equal(1))
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
			backend.DBMock.ProfileReset()
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "refresh_token", "refresh_token": "nope.nope", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_grant"))

			// One read total: the client-auth GetItem. A token that fails its HMAC never reaches the
			// refresh-tokens table at all, so a caller spraying forged tokens cannot make this endpoint
			// do per-guess work against the token store, and cannot write anything.
			profile := backend.DBMock.ProfileGet()
			readCnt, writeCnt := profile.TotalCounts()
			Expect(readCnt).To(Equal(1))
			Expect(writeCnt).To(BeZero())
			Expect(profile.Accesses).NotTo(HaveKey("espuser-refresh-tokens"),
				"a forged token must be rejected by signature, before any lookup")
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
			// client_credentials used to be the example here; it is implemented now, so a
			// grant this server genuinely does not offer is needed to test the default arm.
			for _, grant := range []string{"urn:ietf:params:oauth:grant-type:token-exchange", "password", "implicit"} {
				resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
					"grant_type": grant, "client_id": testClientID,
				})))
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(400), "grant %q", grant)
				Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("unsupported_grant_type"), "grant %q", grant)
			}
		})

		It("refuses client_credentials to a public client rather than treating it as unsupported (negative)", func() {
			// The distinction matters: unsupported_grant_type says "this server does not do
			// that", unauthorized_client says "it does, but not for you".
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"grant_type": "client_credentials", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("unauthorized_client"))
		})

		It("rejects a missing grant_type with invalid_request (negative)", func() {
			resp, err := handleTokenRequest(context.Background(), formRequest(form(map[string]string{
				"refresh_token": "a.b", "client_id": testClientID,
			})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(400))
			Expect(decodeBody[oidc.OAuthError](resp).Error).To(Equal("invalid_request"))
		})

		It("refuses a resource the client is not registered for, before any redirect", func() {
			// A client may request only an API it registered in allowed_resources; the gate is the client model's own.
			const resAPI, otherAPI = "https://api.accounts.example.com", "https://api.other.example.com"
			svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			_, err := svc.Create(clients.CreateInput{
				ClientID: "rm_res_dispatch", ClientName: "Web", ClientType: "public",
				RedirectURIs: []string{"com.example://cb"}, GrantTypes: []string{"authorization_code", "refresh_token"},
				Scopes: []string{"openid", "email"}, AllowedResources: []string{resAPI},
				RequirePKCE: utils.Ptr(true),
			})
			Expect(err).NotTo(HaveOccurred())
			client, err := svc.Get("rm_res_dispatch")
			Expect(err).NotTo(HaveOccurred())
			Expect(client.AllowsResource(otherAPI)).To(BeFalse())
			Expect(client.AllowsResource(resAPI)).To(BeTrue())
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
