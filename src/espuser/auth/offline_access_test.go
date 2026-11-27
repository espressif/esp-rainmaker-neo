// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
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

var _ = Describe("offline_access on first-party sign-ins", func() {
	const (
		clientID    = "first-party-app"
		redirectURI = "com.example://callback"
		resource    = "https://api.example.com"
		userID      = "user-offline-1"
	)
	var (
		svc     *auth.OAuthUserAuthService
		rmngCtx *rmngctx.RmngContext
	)

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		var err error
		svc, err = auth.NewOAuthUserAuthService(context.Background())
		Expect(err).NotTo(HaveOccurred())
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngCtx).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email", "offline_access"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{resource}, DefaultResource: resource,
		})).To(Succeed())
	})

	It("ignores offline_access on the back channel, which returns no code and shows no consent", func() {
		tokens, err := svc.MintTokenSetForFirstPartyLogin(context.Background(), userID, clientID, "openid offline_access email",
			auth.FirstPartyLogin{AllAllowedResources: true, Origin: session.OriginApp})
		Expect(err).NotTo(HaveOccurred())
		Expect(tokens.Scope).To(Equal("openid email"), "RFC 6749 s3.3: the response names the narrowed scope")

		claims := jwtgo.MapClaims{}
		_, _, err = jwtgo.NewParser().ParseUnverified(tokens.AccessToken, claims)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims["scope"]).To(Equal("openid email"))
		Expect(claims["sid"]).NotTo(BeEmpty(), "the login stays tied to its app session")

		families, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		Expect(families).To(HaveLen(1))
		Expect(families[0].Scope).To(Equal("openid email"))
		Expect(families[0].SID).To(Equal(claims["sid"]))
	})
})
