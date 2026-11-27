// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
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
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OAuth token endpoint (a native app's own session)", func() {
	const (
		nativeClientID = "native-app"
		redirectURI    = "com.example://callback"
		userID         = "user-native"
		appUserAgent   = "RainMakerHome/3.2 (Android 14)"
		appIP          = "203.0.113.7"
	)
	var (
		backend    *test_utils.EspUserBackend
		rmngCtx    *rmngctx.RmngContext
		browserSID string
		browser    *sessions_db.SessionEntry
	)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		seedPublicClient(testClientID)
		seedClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: nativeClientID, ClientType: oauth_clients_db.ClientTypePublic, ApplicationType: oauth_clients_db.ApplicationTypeNative,
			RequirePKCE: utils.Ptr(true), Scopes: []string{"openid", "email", "offline_access"},
			AllowedResources: []string{testResource}, DefaultResource: testResource,
		})
		var err error
		_, browserSID, err = session.NewService(rmngCtx).Establish(session.EstablishInput{
			UserID: userID, Provider: "up", AuthTime: time.Now().Add(-time.Minute).Unix(), AMR: []string{"pwd"},
			ProviderMaxTTLSeconds: 3600, UserAgent: "Chrome on Android",
		})
		Expect(err).NotTo(HaveOccurred())
		browser, err = session.NewService(rmngCtx).FindBySID(userID, browserSID)
		Expect(err).NotTo(HaveOccurred())
	})

	// exchange issues a code approved in the browser session and redeems it from the app, as a sign-in through a Custom Tab does.
	exchange := func(clientID, code string, granted []string) events.APIGatewayProxyResponse {
		flows := auth_flows_db.NewAuthFlowsDB(rmngCtx)
		Expect(flows.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_" + code, ClientID: clientID, RedirectURI: redirectURI, RequestedScope: granted,
			CodeChallenge: testChallenge(), CodeChallengeMethod: "S256", Resources: []string{testResource},
			ExpiresOn: time.Now().Add(10 * time.Minute).Unix(),
		})).To(Succeed())
		Expect(flows.IssueCode("fl_"+code, code, auth_flows_db.Login{Subject: userID, GrantedScope: granted, SID: browserSID, AuthTime: browser.AuthTime})).Error().NotTo(HaveOccurred())
		request := formRequest(url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {testVerifier},
			"client_id": {clientID}, "redirect_uri": {redirectURI},
		}.Encode())
		request.RequestContext.Identity.UserAgent = appUserAgent
		request.RequestContext.Identity.SourceIP = appIP
		resp, err := handleTokenRequest(context.Background(), request)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}
	signIn := func(clientID, code string, granted ...string) map[string]any {
		resp := exchange(clientID, code, granted)
		Expect(resp.StatusCode).To(Equal(200), resp.Body)
		return decodeBody[map[string]any](resp)
	}
	refresh := func(clientID string, tokens map[string]any) int {
		resp, err := handleTokenRequest(context.Background(), formRequest(url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {tokens["refresh_token"].(string)}, "client_id": {clientID},
		}.Encode()))
		Expect(err).NotTo(HaveOccurred())
		return resp.StatusCode
	}
	familySID := func(clientID string) []string {
		rows, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUserAndClient(userID, clientID)
		Expect(err).NotTo(HaveOccurred())
		sids := make([]string, 0, len(rows))
		for i := range rows {
			sids = append(sids, rows[i].SID)
		}
		return sids
	}
	sessionCount := func() int {
		rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		return len(rows)
	}

	It("gives a native app's login a session of its own, carrying the sign-in session's provider, sign-in time and cap", func() {
		tokens := signIn(nativeClientID, "ac_native", "openid", "email")

		appSID := familySID(nativeClientID)
		Expect(appSID).To(HaveLen(1))
		Expect(appSID[0]).NotTo(BeElementOf("", browserSID))
		app, err := session.NewService(rmngCtx).FindBySID(userID, appSID[0])
		Expect(err).NotTo(HaveOccurred())
		type signIn struct {
			Origin, Provider, UserAgent, IPAddress  string
			AuthTime, MaxExpiresAt, CookieExpiresAt int64
			AMR                                     []string
		}
		Expect(signIn{app.Origin, app.Provider, app.UserAgent, app.IPAddress, app.AuthTime, app.MaxExpiresAt, app.CookieExpiresAt, app.AMR}).To(Equal(
			signIn{session.OriginApp, "up", appUserAgent, appIP, browser.AuthTime, browser.MaxExpiresAt, 0, []string{"pwd"}}))
		Expect(tokenClaims(tokens["id_token"].(string))).To(HaveKeyWithValue("sid", appSID[0]))
		Expect(tokenClaims(tokens["access_token"].(string))).To(HaveKeyWithValue("sid", appSID[0]))
	})

	It("keeps a native app signed in when the browser that approved it signs out, while a web app's login there ends", func() {
		native := signIn(nativeClientID, "ac_native", "openid")
		web := signIn(testClientID, "ac_web", "openid")
		Expect(familySID(testClientID)).To(Equal([]string{browserSID}))

		Expect(signout.NewService(rmngCtx).EndSession(userID, browserSID)).To(Succeed())

		Expect(refresh(nativeClientID, native)).To(Equal(200))
		Expect(refresh(testClientID, web)).To(Equal(400))
	})

	It("removes only the app's own session when the native app signs itself out", func() {
		native := signIn(nativeClientID, "ac_native", "openid")
		Expect(sessionCount()).To(Equal(2))

		Expect(signout.NewService(rmngCtx).RevokeFamily(nativeClientID, native["refresh_token"].(string))).To(Succeed())

		rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].SID).To(Equal(browserSID), "the browser stays signed in")
	})

	It("refuses a native app's code once the session that approved it has ended, minting nothing (negative)", func() {
		Expect(session.NewService(rmngCtx).DeleteSessionBySID(userID, browserSID)).To(Succeed())

		resp := exchange(nativeClientID, "ac_late", []string{"openid"})
		Expect(resp.StatusCode).To(Equal(400))
		Expect(decodeBody[map[string]any](resp)["error"]).To(Equal("invalid_grant"))
		Expect(familySID(nativeClientID)).To(BeEmpty())
		Expect(sessionCount()).To(BeZero())
	})

	It("gives a native app asking for offline_access an offline grant and no session", func() {
		signIn(nativeClientID, "ac_offline", "openid", "offline_access")
		Expect(familySID(nativeClientID)).To(Equal([]string{""}))
		Expect(sessionCount()).To(Equal(1))
	})

	It("leaves the login under the browser session when the app session cannot be written (negative)", func() {
		awscommon.SetDynamoDBClient(&mock.FailingPutItem{DynamoDBMock: backend.DBMock, TableName: "espuser-sessions"})
		signIn(nativeClientID, "ac_native", "openid")
		awscommon.SetDynamoDBClient(backend.DBMock)
		Expect(familySID(nativeClientID)).To(Equal([]string{browserSID}))
	})

	It("costs a native exchange the session read and the app session write on top of a web exchange", func() {
		backend.DBMock.ProfileReset()
		signIn(testClientID, "ac_web", "openid")
		webProfile := backend.DBMock.ProfileGet()
		webReads, webWrites := webProfile.TotalCounts()

		backend.DBMock.ProfileReset()
		signIn(nativeClientID, "ac_native", "openid")
		nativeProfile := backend.DBMock.ProfileGet()
		nativeReads, nativeWrites := nativeProfile.TotalCounts()

		// The sign-in session: an index Query and a GetItem. The app session: one PutItem.
		Expect([2]int{nativeReads - webReads, nativeWrites - webWrites}).To(Equal([2]int{2, 1}))
	})
})
