// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// verifier/challenge is a fixed valid S256 pair for the code-exchange specs.
const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

var _ = Describe("OAuth token endpoint (authorization_code grant)", func() {
	const redirectURI = "com.example://callback"

	// seedCode writes a CODE flow record (post-OTP state) redeemable by ExchangeAuthCode.
	seedCode := func(code string) {
		db := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(db.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID:              "fl_" + code,
			ClientID:            testClientID,
			RedirectURI:         redirectURI,
			RequestedScope:      []string{"openid", "email"},
			CodeChallenge:       testChallenge(),
			CodeChallengeMethod: "S256",
			ExpiresOn:           time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(db.IssueCode("fl_"+code, "user-123", []string{"openid", "email"}, code)).To(Succeed())
	}

	codeForm := func(overrides map[string]string) string {
		kv := map[string]string{
			"grant_type":    "authorization_code",
			"code":          "ac_valid",
			"code_verifier": testVerifier,
			"client_id":     testClientID,
			"redirect_uri":  redirectURI,
		}
		for k, v := range overrides {
			if v == "" {
				delete(kv, k)
			} else {
				kv[k] = v
			}
		}
		v := url.Values{}
		for k, val := range kv {
			v.Set(k, val)
		}
		return v.Encode()
	}

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		seedPublicClient(testClientID)
	})

	It("exchanges a valid code + PKCE verifier for the token set", func() {
		seedCode("ac_valid")
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		tokens := decodeBody[map[string]any](resp)
		Expect(tokens["access_token"]).NotTo(BeEmpty())
		Expect(tokens["refresh_token"]).NotTo(BeEmpty())
		Expect(tokens["id_token"]).NotTo(BeEmpty(), "openid was in scope")
	})

	It("rejects a reused code with invalid_grant (single-use, negative)", func() {
		seedCode("ac_valid")
		first, _ := handleTokenRequest(context.Background(), formRequest(codeForm(nil)))
		Expect(first.StatusCode).To(Equal(200))

		second, err := handleTokenRequest(context.Background(), formRequest(codeForm(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(second.StatusCode).To(Equal(400))
		Expect(second.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects a wrong PKCE verifier with invalid_grant (negative)", func() {
		seedCode("ac_valid")
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"code_verifier": "wrong-verifier"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects a registered client that does not match the code (negative, invalid_grant)", func() {
		seedCode("ac_valid")
		seedPublicClient("other-client") // registered, so it passes client auth but not the code's client match
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"client_id": "other-client"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects an unregistered client with invalid_client (negative, auth gate)", func() {
		seedCode("ac_valid")
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"client_id": "ghost-client"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(401))
		Expect(resp.Body).To(ContainSubstring("invalid_client"))
	})

	It("rejects a redirect_uri that does not match the code (negative)", func() {
		seedCode("ac_valid")
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"redirect_uri": "com.evil://cb"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects an unknown code with invalid_grant (negative, no oracle)", func() {
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"code": "ac_ghost"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects a missing verifier for a PKCE-bound code with invalid_grant (negative, downgrade guard)", func() {
		seedCode("ac_valid") // seeded with a challenge, so a verifier is mandatory
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"code_verifier": ""})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))
	})

	It("rejects a missing code with invalid_request (negative)", func() {
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"code": ""})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_request"))
	})

	Context("confidential client (HTTP Basic auth)", func() {
		const confID = "conf-token"
		var confSecret string

		// seedConfCode registers a confidential client (no require_pkce) and a code bound to it.
		seedConfCode := func(code string) {
			cs := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			res, err := cs.Create(clients.CreateInput{ClientID: confID, ClientName: "C", ClientType: "confidential", GrantTypes: []string{"authorization_code"}})
			Expect(err).NotTo(HaveOccurred())
			confSecret = res.ClientSecret
			db := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			Expect(db.CreateFlow(&auth_flows_db.AuthFlow{
				FlowID: "fl_" + code, ClientID: confID, RedirectURI: redirectURI,
				RequestedScope: []string{"openid"}, ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
			})).To(Succeed())
			Expect(db.IssueCode("fl_"+code, "user-123", []string{"openid"}, code)).To(Succeed())
		}

		basic := func(id, secret string) map[string]string {
			return map[string]string{"Content-Type": "application/x-www-form-urlencoded",
				"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))}
		}

		It("exchanges a code with a valid Basic secret (no PKCE needed)", func() {
			seedConfCode("ac_conf")
			req := formRequest(codeForm(map[string]string{"client_id": confID, "code": "ac_conf", "code_verifier": ""}))
			req.Headers = basic(confID, confSecret)
			resp, err := handleTokenRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("rejects a wrong secret with invalid_client (negative)", func() {
			seedConfCode("ac_conf")
			req := formRequest(codeForm(map[string]string{"client_id": confID, "code": "ac_conf", "code_verifier": ""}))
			req.Headers = basic(confID, "wrong")
			resp, err := handleTokenRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(401))
			Expect(resp.Body).To(ContainSubstring("invalid_client"))
		})

		It("rejects a confidential client with no secret (negative, form client_id only)", func() {
			seedConfCode("ac_conf")
			resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"client_id": confID, "code": "ac_conf", "code_verifier": ""})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(401))
			Expect(resp.Body).To(ContainSubstring("invalid_client"))
		})

		// Google account linking authenticates with client_secret_post (credentials in the
		// form body, RFC 6749 §2.3.1) rather than HTTP Basic — both must be accepted.
		It("exchanges a code with a valid secret in the form body (client_secret_post)", func() {
			seedConfCode("ac_conf")
			resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{
				"client_id": confID, "client_secret": confSecret, "code": "ac_conf", "code_verifier": ""})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(200))
		})

		It("rejects a wrong form-body secret with invalid_client (negative, client_secret_post)", func() {
			seedConfCode("ac_conf")
			resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{
				"client_id": confID, "client_secret": "wrong", "code": "ac_conf", "code_verifier": ""})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(401))
			Expect(resp.Body).To(ContainSubstring("invalid_client"))
		})
	})
})
