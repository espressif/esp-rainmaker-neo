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
	"github.com/espressif/esp-rainmaker-neo/src/utils"
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
	var backend *test_utils.EspUserBackend

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
		Expect(db.IssueCode("fl_"+code, "user-123", []string{"openid", "email"}, code, "", 0)).Error().NotTo(HaveOccurred())
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
		backend = test_utils.SetupEspUserBackend(context.Background())
		seedPublicClient(testClientID)
	})

	It("exchanges a valid code + PKCE verifier for the token set", func() {
		seedCode("ac_valid")
		// Reset after seeding, so the profile below measures only the exchange itself.
		backend.DBMock.ProfileReset()
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		tokens := decodeBody[map[string]any](resp)
		Expect(tokens["access_token"]).NotTo(BeEmpty())
		Expect(tokens["refresh_token"]).NotTo(BeEmpty())
		Expect(tokens["id_token"]).NotTo(BeEmpty(), "openid was in scope")

		// Three reads and two writes per code exchange:
		//
		//   1  GetItem    espuser-oauth-clients   client authentication
		//   2  Query      espuser-auth-flows      the code -> flow lookup, on the code GSI
		//   3  DeleteItem espuser-auth-flows      the code is spent (write) -- what makes it single-use
		//   4  GetItem    espuser-user-details    the profile claims baked into the id token
		//   5  PutItem    espuser-refresh-tokens  the new refresh family (write)
		//
		// Read 2 is a GSI Query, so the flows row also carries an index write multiple on both of
		// its writes; read 4 happens on every exchange whether or not profile claims were asked for.
		// Neither shows up in any functional assertion -- only in the bill.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(3))
		Expect(writeCnt).To(Equal(2))
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
		backend.DBMock.ProfileReset()
		resp, err := handleTokenRequest(context.Background(), formRequest(codeForm(map[string]string{"code_verifier": "wrong-verifier"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(resp.Body).To(ContainSubstring("invalid_grant"))

		// Client auth read plus the code lookup, and no write of any kind. The zero matters twice:
		// the flow record must survive (a failed verifier must not burn the legitimate client's code)
		// and no refresh family may be minted. A caller brute-forcing the verifier therefore costs
		// two reads per attempt and can neither spend the code nor grow any table.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(2))
		Expect(writeCnt).To(BeZero())
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
			Expect(db.IssueCode("fl_"+code, "user-123", []string{"openid"}, code, "", 0)).Error().NotTo(HaveOccurred())
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

// The browser leg of RFC 8707. resource arrives at /authorize, minutes before the token is
// minted at /token, so the whole question is whether it survives the round trip -- through
// the flow record, through the code exchange, and through every later refresh. It carries its
// own fixtures because the feature IS the resource: a client registered with allowed_resources
// and a flow that carries one.
var _ = Describe("RFC 8707 resource on the authorization-code flow", func() {
	const (
		resClientID = "rm_res_web"
		resRedirect = "com.example://cb"
		resAPI      = "https://api.accounts.example.com"
		otherAPI    = "https://api.other.example.com"
	)

	seedClient := func() {
		svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		_, err := svc.Create(clients.CreateInput{
			ClientID: resClientID, ClientName: "Web", ClientType: "public",
			RedirectURIs: []string{resRedirect}, GrantTypes: []string{"authorization_code", "refresh_token"},
			Scopes: []string{"openid", "email"}, AllowedResources: []string{resAPI},
			RequirePKCE: utils.Ptr(true),
		})
		Expect(err).NotTo(HaveOccurred())
	}

	// seedCodeWithResource writes the post-login flow state a real /authorize would have left.
	seedCodeWithResource := func(code, resource string) {
		db := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(db.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_" + code, ClientID: resClientID, RedirectURI: resRedirect,
			RequestedScope: []string{"openid", "email"},
			CodeChallenge:  testChallenge(), CodeChallengeMethod: "S256",
			Resource:  resource,
			ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(db.IssueCode("fl_"+code, "user-123", []string{"openid", "email"}, code, "", 0)).Error().NotTo(HaveOccurred())
	}

	exchange := func(code string) map[string]any {
		form := url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"code_verifier": {testVerifier}, "client_id": {resClientID},
			"redirect_uri": {resRedirect},
		}
		resp, err := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		return decodeBody[map[string]any](resp)
	}

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		seedClient()
	})

	It("stamps the resource into the access token's aud while the ID token's stays the client id", func() {
		// One exchange pins both: the access token's audience is the API named by resource, while
		// the ID token's stays this client -- it is a statement to the client about who signed in,
		// and fixing the access token must not "fix" this one.
		seedCodeWithResource("ac_res", resAPI)
		tokens := exchange("ac_res")
		access := unverifiedClaims(tokens["access_token"].(string))
		Expect(access["aud"]).To(Equal(resAPI))
		Expect(access["aud"]).NotTo(Equal(resClientID), "aud is the API, not the caller")
		Expect(unverifiedClaims(tokens["id_token"].(string))["aud"]).To(Equal(resClientID))
	})

	It("falls back to aud = client_id when no resource was requested", func() {
		// The compatibility argument for the whole extension: a deployment that never uses
		// it sees exactly what it saw before.
		seedCodeWithResource("ac_nores", "")
		Expect(unverifiedClaims(exchange("ac_nores")["access_token"].(string))["aud"]).To(Equal(resClientID))
	})

	It("carries the resource through a refresh, so a renewed token is still usable", func() {
		// The access token is renewed constantly and silently. If the refresh forgot the
		// resource, every renewal would produce a token the API refuses -- and the failure
		// would appear an hour after login, nowhere near the cause.
		seedCodeWithResource("ac_refresh", resAPI)
		refresh := exchange("ac_refresh")["refresh_token"].(string)

		form := url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {resClientID},
		}
		resp, err := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		renewed := decodeBody[map[string]any](resp)["access_token"].(string)
		Expect(unverifiedClaims(renewed)["aud"]).To(Equal(resAPI))
	})

	// A relying party's max_age check reads auth_time, so a shortcut or refresh must not claim a fresh login.
	It("stamps the session's original auth_time on the ID token, through exchange and refresh", func() {
		signedIn := time.Now().Add(-180 * 24 * time.Hour).Unix()
		db := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(db.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_ac_authtime", ClientID: resClientID, RedirectURI: resRedirect,
			RequestedScope: []string{"openid", "email"},
			CodeChallenge:  testChallenge(), CodeChallengeMethod: "S256",
			ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(db.IssueCode("fl_ac_authtime", "user-123", []string{"openid", "email"}, "ac_authtime", "", signedIn)).Error().NotTo(HaveOccurred())

		tokens := exchange("ac_authtime")
		Expect(unverifiedClaims(tokens["id_token"].(string))["auth_time"]).To(BeNumerically("==", signedIn))

		form := url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "client_id": {resClientID},
		}
		resp, err := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		renewed := decodeBody[map[string]any](resp)["id_token"].(string)
		Expect(unverifiedClaims(renewed)["auth_time"]).To(BeNumerically("==", signedIn), "a refresh is not a new authentication")
	})

	It("stamps now when the flow recorded no auth_time (flows issued before the field existed)", func() {
		seedCodeWithResource("ac_noauthtime", "")
		before := time.Now().Unix()
		at := unverifiedClaims(exchange("ac_noauthtime")["id_token"].(string))["auth_time"]
		Expect(at).To(BeNumerically(">=", before))
	})

	It("stops renewing once the resource is taken off the client (negative)", func() {
		// A registry revocation has to reach logins already in flight. The resource is stamped
		// on the family at login and replayed on every renewal, so without a re-check, removing
		// an API from allowed_resources stops new logins asking for it and does nothing about
		// the ones already asking. The family's lifetime is re-stamped on each rotation, so a
		// client that keeps refreshing would never be cut off at all.
		seedCodeWithResource("ac_revoked", resAPI)
		refresh := exchange("ac_revoked")["refresh_token"].(string)

		svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		_, err := svc.Update(resClientID, clients.UpdateInput{
			ClientName: "Web", RedirectURIs: []string{resRedirect},
			GrantTypes: []string{"authorization_code", "refresh_token"},
			Scopes:     []string{"openid", "email"},
			// Re-pointed at a different API: this client may no longer request resAPI.
			AllowedResources: []string{otherAPI},
			RequirePKCE:      utils.Ptr(true),
		})
		Expect(err).NotTo(HaveOccurred())

		form := url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {resClientID},
		}
		resp, err := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400), resp.Body)
		// invalid_target, not invalid_grant: the token is good, the entitlement is gone, and a
		// client told invalid_grant would retry forever against something that cannot work.
		Expect(resp.Body).To(ContainSubstring("invalid_target"))
	})

	It("refuses without spending the token, so restoring the resource resumes the login", func() {
		// The refusal must be reversible. Rotate spends the presented token as its first durable
		// act, so a check placed after it would leave the client one counter behind -- the next
		// attempt reads as reuse and deletes the whole family. Then restoring the entitlement
		// would not help anyone, because the login is already gone.
		seedCodeWithResource("ac_restore", resAPI)
		refresh := exchange("ac_restore")["refresh_token"].(string)
		svc := clients.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		base := clients.UpdateInput{
			ClientName: "Web", RedirectURIs: []string{resRedirect},
			GrantTypes: []string{"authorization_code", "refresh_token"},
			Scopes:     []string{"openid", "email"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{otherAPI},
		}
		form := url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {resClientID},
		}

		_, err := svc.Update(resClientID, base) // withdraw
		Expect(err).NotTo(HaveOccurred())
		refused, _ := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(refused.StatusCode).To(Equal(400))

		withResource := base
		withResource.AllowedResources = []string{resAPI}
		_, err = svc.Update(resClientID, withResource) // restore
		Expect(err).NotTo(HaveOccurred())

		resp, err := handleTokenRequest(context.Background(), formRequest(form.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		Expect(unverifiedClaims(decodeBody[map[string]any](resp)["access_token"].(string))["aud"]).
			To(Equal(resAPI), "the same refresh token must still work, unspent by the refusal")
	})
})
