// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/url"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OAuth token endpoint (offline_access grants)", func() {
	const (
		redirectURI = "com.example://callback"
		userID      = "user-offline"
	)
	var (
		backend *test_utils.EspUserBackend
		rmngCtx *rmngctx.RmngContext
		sid     string
	)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		seedClient(offlineClient(nil))
		var err error
		_, sid, err = session.NewService(rmngCtx).Establish(session.EstablishInput{UserID: userID, Provider: "up", AuthTime: time.Now().Unix()})
		Expect(err).NotTo(HaveOccurred())
		Expect(sid).NotTo(BeEmpty())
	})

	// redeemCode issues a code for a sign-in under the browser session sid and exchanges it, as the person's app does.
	redeemCode := func(code string, granted []string) map[string]any {
		flows := auth_flows_db.NewAuthFlowsDB(rmngCtx)
		Expect(flows.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_" + code, ClientID: testClientID, RedirectURI: redirectURI, RequestedScope: granted,
			CodeChallenge: testChallenge(), CodeChallengeMethod: "S256", Resources: []string{testResource},
			ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(flows.IssueCode("fl_"+code, code, auth_flows_db.Login{Subject: userID, GrantedScope: granted, SID: sid, AuthTime: time.Now().Unix(), Provider: "up", AMR: []string{"pwd"}})).Error().NotTo(HaveOccurred())
		resp, err := handleTokenRequest(context.Background(), formRequest(url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {testVerifier},
			"client_id": {testClientID}, "redirect_uri": {redirectURI},
		}.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		return decodeBody[map[string]any](resp)
	}

	refresh := func(refreshToken string) (int, map[string]any) {
		resp, err := handleTokenRequest(context.Background(), formRequest(url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {testClientID},
		}.Encode()))
		Expect(err).NotTo(HaveOccurred())
		return resp.StatusCode, decodeBody[map[string]any](resp)
	}

	familySIDs := func() map[string]string {
		rows, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		sidByScope := map[string]string{}
		for i := range rows {
			sidByScope[rows[i].Scope] = rows[i].SID
		}
		return sidByScope
	}

	It("mints an offline grant tied to no session, with no sid in its access or ID token (OIDC Back-Channel Logout 1.0 s2.7)", func() {
		offline := redeemCode("ac_offline", []string{"openid", "email", "offline_access"})
		online := redeemCode("ac_online", []string{"openid", "email"})

		Expect(familySIDs()).To(Equal(map[string]string{"openid email offline_access": "", "openid email": sid}))
		Expect(tokenClaims(offline["access_token"].(string))).NotTo(HaveKey("sid"))
		Expect(tokenClaims(offline["id_token"].(string))).NotTo(HaveKey("sid"))
		Expect(tokenClaims(online["access_token"].(string))).To(HaveKeyWithValue("sid", sid), "a session grant keeps its sid")
	})

	It("keeps rotating an offline grant after the session it was signed in through has ended, while that session's own grant is refused", func() {
		offline := redeemCode("ac_offline", []string{"openid", "email", "offline_access"})
		online := redeemCode("ac_online", []string{"openid", "email"})
		Expect(session.NewService(rmngCtx).DeleteSessionBySID(userID, sid)).To(Succeed())

		status, body := refresh(offline["refresh_token"].(string))
		Expect(status).To(Equal(200))
		Expect(tokenClaims(body["access_token"].(string))).NotTo(HaveKey("sid"))
		for _, token := range []any{offline["access_token"], offline["id_token"], body["access_token"], body["id_token"]} {
			Expect(tokenClaims(token.(string))).To(HaveKeyWithValue("amr", []any{"pwd"}), "an offline grant still says how the person signed in")
			Expect(tokenClaims(token.(string))).To(HaveKeyWithValue("idp", "up"))
		}

		status, body = refresh(online["refresh_token"].(string))
		Expect(status).To(Equal(400))
		Expect(body["error"]).To(Equal("invalid_grant"))
	})

	It("refuses a code redeemed after the session that approved it ended, minting nothing (negative)", func() {
		flows := auth_flows_db.NewAuthFlowsDB(rmngCtx)
		Expect(flows.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_late", ClientID: testClientID, RedirectURI: redirectURI, RequestedScope: []string{"openid", "offline_access"},
			CodeChallenge: testChallenge(), CodeChallengeMethod: "S256", Resources: []string{testResource},
			ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(flows.IssueCode("fl_late", "ac_late", auth_flows_db.Login{Subject: userID, GrantedScope: []string{"openid", "offline_access"}, SID: sid, AuthTime: time.Now().Unix()})).Error().NotTo(HaveOccurred())
		Expect(session.NewService(rmngCtx).DeleteSessionBySID(userID, sid)).To(Succeed())

		resp, err := handleTokenRequest(context.Background(), formRequest(url.Values{
			"grant_type": {"authorization_code"}, "code": {"ac_late"}, "code_verifier": {testVerifier},
			"client_id": {testClientID}, "redirect_uri": {redirectURI},
		}.Encode()))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(400))
		Expect(familySIDs()).To(BeEmpty())
	})

	It("refuses an offline grant's refresh once its client may no longer hold offline_access, without spending the token (negative)", func() {
		offline := redeemCode("ac_offline", []string{"openid", "email", "offline_access"})
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngCtx).DeleteClient(testClientID)).To(Succeed())
		seedClient(offlineClient([]string{"openid", "email"}))

		status, body := refresh(offline["refresh_token"].(string))
		Expect(status).To(Equal(400))
		Expect(body["error"]).To(Equal("invalid_grant"))

		Expect(oauth_clients_db.NewOAuthClientsDB(rmngCtx).DeleteClient(testClientID)).To(Succeed())
		seedClient(offlineClient(nil))
		status, _ = refresh(offline["refresh_token"].(string))
		Expect(status).To(Equal(200), "a refused renewal leaves the grant usable once the scope is back")
	})

	It("costs an offline refresh the same three reads and one write as any refresh", func() {
		offline := redeemCode("ac_offline", []string{"openid", "email", "offline_access"})
		backend.DBMock.ProfileReset()

		status, _ := refresh(offline["refresh_token"].(string))
		Expect(status).To(Equal(200))

		// client GetItem, family GetItem, user-details GetItem; the family UpdateItem. No session read: the grant has no sid to clamp against.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(3))
		Expect(writeCnt).To(Equal(1))
	})
})
