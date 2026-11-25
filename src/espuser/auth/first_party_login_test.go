// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	jwtgo "github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CompleteAuthFlowForFirstPartyLogin, the enterprise OTP add-on's browser completion", func() {
	const (
		userID      = "user-otp-1"
		redirectURI = "com.example://callback"
	)
	var (
		svc     *auth.OAuthUserAuthService
		rmngCtx *rmngctx.RmngContext
		backend *test_utils.EspUserBackend
	)

	otpLogin := auth.FirstPartyLogin{Provider: "otp", AMR: []string{"otp"}, Origin: session.OriginBrowser}

	createClient := func(clientID string, allowed []string) {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngCtx).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"}, AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
		})).To(Succeed())
	}

	createFlow := func(flowID, clientID string, expiresIn time.Duration) {
		Expect(auth_flows_db.NewAuthFlowsDB(rmngCtx).CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: flowID, ClientID: clientID, RedirectURI: redirectURI, State: "xyz", RequestedScope: []string{"openid"},
			SkipConsent: true, ExpiresOn: time.Now().Add(expiresIn).Unix(),
		})).To(Succeed())
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		var err error
		svc, err = auth.NewOAuthUserAuthService(context.Background())
		Expect(err).NotTo(HaveOccurred())
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), nil)
	})

	It("issues the client's code and the browser's session for a provider the client allows", func() {
		createClient("otp-client", nil)
		createFlow("fl_otp", "otp-client", auth_flows_db.FlowTTL)
		redirectTo, setCookie, err := svc.CompleteAuthFlowForFirstPartyLogin(context.Background(), "fl_otp", userID, otpLogin)
		Expect(err).NotTo(HaveOccurred())
		Expect(redirectTo).To(MatchRegexp(`^com\.example://callback\?code=[^&]+&state=xyz$`))
		Expect(setCookie).To(HavePrefix(session.CookieName + "="))

		code := strings.TrimPrefix(strings.Split(redirectTo, "&")[0], redirectURI+"?code=")
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlowByCode(code)
		Expect(err).NotTo(HaveOccurred())
		Expect([]any{flow.Provider, flow.AMR}).To(Equal([]any{"otp", []string{"otp"}}), "the code exchange stamps the one-time code as the method and otp as the provider")
	})

	It("stamps a native one-time-code sign-in's tokens and refresh family with amr otp and idp otp", func() {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngCtx).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "otp-native", ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{"https://api.example.com"}, DefaultResource: "https://api.example.com",
		})).To(Succeed())
		nativeLogin := otpLogin
		nativeLogin.Origin = session.OriginApp

		tokens, err := svc.MintTokenSetForFirstPartyLogin(context.Background(), userID, "otp-native", "openid", nativeLogin)
		Expect(err).NotTo(HaveOccurred())
		for _, token := range []string{tokens.AccessToken, tokens.IDToken} {
			claims := jwtgo.MapClaims{}
			_, _, err := jwtgo.NewParser().ParseUnverified(token, claims)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims["amr"]).To(Equal([]any{"otp"}))
			Expect(claims["idp"]).To(Equal("otp"))
		}
		families, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		Expect(families).To(HaveLen(1))
		Expect([]any{families[0].AMR, families[0].Provider}).To(Equal([]any{[]string{"otp"}, "otp"}))
	})

	DescribeTable("refuses before anything is written when the flow's client does not admit the provider (negative)",
		func(register func()) {
			register()
			createFlow("fl_otp", "otp-client", auth_flows_db.FlowTTL)
			backend.DBMock.ProfileReset()

			redirectTo, setCookie, err := svc.CompleteAuthFlowForFirstPartyLogin(context.Background(), "fl_otp", userID, otpLogin)
			Expect(err).To(MatchError(auth.ErrProviderNotAllowed))
			Expect([]string{redirectTo, setCookie}).To(Equal([]string{"", ""}))
			for table, access := range backend.DBMock.ProfileGet().Accesses {
				Expect(access.WriteCount).To(BeZero(), table)
			}
		},
		Entry("a client that admits only another provider", func() { createClient("otp-client", []string{"corp"}) }),
		Entry("a client that cannot be read, which refuses rather than admits", func() {}),
	)

	It("passes an expired flow through for the caller to answer as one", func() {
		createClient("otp-client", nil)
		createFlow("fl_old", "otp-client", -time.Minute)
		_, _, err := svc.CompleteAuthFlowForFirstPartyLogin(context.Background(), "fl_old", userID, otpLogin)
		Expect(err).To(MatchError(auth_flows_db.ErrFlowExpired))
	})
})
