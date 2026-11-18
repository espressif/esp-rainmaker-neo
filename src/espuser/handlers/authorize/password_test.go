// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/legacyauth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	cognitotypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	jwtgo "github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("POST /user/login/password", func() {
	const (
		poolID          = "eu-west-2_testpool"
		poolClientID    = "pool-app-client"
		personEmail     = "person@example.com"
		unconfirmedMail = "unconfirmed@example.com"
		personPassword  = "Password1!"
		thirdPartyID    = "third-party-client"
		restrictedID    = "restricted-client"
		browserUA       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Safari/605.1.15"
		browserIP       = "203.0.113.7"
	)

	var (
		backend *test_utils.EspUserBackend
		cog     *mock.CognitoProviderMock
	)

	createClient := func(clientID string, firstParty bool, allowed []string) {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: firstParty,
				RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
				AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
				AllowedResources: []string{testResource}, DefaultResource: testResource,
			})).To(Succeed())
	}

	createProvider := func(name, providerType, passwordVerifier string) {
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: name, Type: providerType, PasswordVerifier: passwordVerifier, Enabled: utils.Ptr(true), DisplayName: name,
				ClientID: poolClientID, ClientSecret: "pool-app-secret",
			})).To(Succeed())
	}

	// createFlow is what /oauth2/authorize leaves behind; skipConsent is the decision StartAuthFlow stored.
	createFlow := func(flowID, clientID string, skipConsent bool, expiresIn time.Duration) {
		Expect(auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: flowID, ClientID: clientID, RedirectURI: redirectURI, State: "xyz",
			RequestedScope: []string{"openid", "email"}, SkipConsent: skipConsent,
			ExpiresOn: time.Now().Add(expiresIn).Unix(),
		})).To(Succeed())
	}

	signInBody := func(flowID string, overrides map[string]any) string {
		body := map[string]any{"flow_id": flowID, "username": personEmail, "password": personPassword}
		for k, v := range overrides {
			body[k] = v
		}
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		return string(raw)
	}

	post := func(cookie, body string) events.APIGatewayProxyResponse {
		req := events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: pathPassword, Body: body, Headers: map[string]string{}}
		if cookie != "" {
			req.Headers["Cookie"] = cookie
		}
		req.RequestContext.Identity.UserAgent = browserUA
		req.RequestContext.Identity.SourceIP = browserIP
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	flowCookie := func(flowID string) string { return auth.FlowCookieName + "=" + flowID }

	sessionCookieValue := func(resp events.APIGatewayProxyResponse) string {
		setCookie := resp.Headers["Set-Cookie"]
		Expect(setCookie).To(HavePrefix(session.CookieName + "="))
		return strings.SplitN(strings.SplitN(setCookie, ";", 2)[0], "=", 2)[1]
	}

	// noTokens holds for every answer: a token in the page would step over the client's PKCE binding.
	noTokens := func(resp events.APIGatewayProxyResponse) {
		for _, field := range []string{"access_token", "id_token", "refresh_token"} {
			Expect(resp.Body).NotTo(ContainSubstring(field))
		}
		Expect(resp.Headers).To(HaveKeyWithValue("Cache-Control", "no-store"))
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		cog = mock.NewCognitoProviderMock()
		cog.AddTestUserPoolDirect(poolID, poolClientID)
		cog.AddTestUserDirect(poolID, personEmail, personEmail, personPassword, true)
		cog.AddTestUserDirect(poolID, unconfirmedMail, unconfirmedMail, personPassword, false)
		cog.IDTokenMinter = func(user *mock.UserState) string {
			tok, err := jwtutil.SignRS256(jwtgo.MapClaims{"email": user.Email, "email_verified": true}, backend.SigningKey, oidc.SigningKeyID)
			Expect(err).NotTo(HaveOccurred())
			return tok
		}
		awscommon.SetCognitoProviderClient(cog)

		createProvider("cognito", identity_providers_db.TypeOIDC, identity_providers_db.PasswordVerifierCognito)
		createProvider("second", identity_providers_db.TypeOIDC, identity_providers_db.PasswordVerifierCognito)
		createProvider("corp", identity_providers_db.TypeOIDC, "")
		createClient(testClientID, true, nil)
		createClient(thirdPartyID, false, nil)
		createClient(restrictedID, true, []string{"second"})
		createFlow("fl_page", testClientID, true, auth_flows_db.FlowTTL)
	})

	It("finishes a first-party sign-in with the client's code and the browser's session, never a token", func() {
		resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		noTokens(resp)

		var body map[string]string
		Expect(json.Unmarshal([]byte(resp.Body), &body)).To(Succeed())
		Expect(body).To(HaveLen(1))
		Expect(body["redirect_to"]).To(MatchRegexp(`^com\.example://callback\?code=[^&]+&state=xyz$`))

		s := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Peek(sessionCookieValue(resp))
		Expect(s).NotTo(BeNil())
		Expect(s.Provider).To(Equal("cognito"), "with no provider named, the first password row by name answers")
		Expect(cog.InitiateAuthCalls).To(Equal(1))
	})

	It("holds a third-party sign-in for consent, sending the page to the consent route with no code yet", func() {
		createFlow("fl_third", thirdPartyID, false, auth_flows_db.FlowTTL)

		resp := post(flowCookie("fl_third"), signInBody("fl_third", nil))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		noTokens(resp)
		Expect(resp.Body).To(MatchJSON(`{"redirect_to":"` + auth.ConsentPath + `"}`))
		Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(session.CookieName+"="), "the person signed in, so the session exists before they answer")

		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlow("fl_third")
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AwaitingConsent()).To(BeTrue())
	})

	It("checks the password against the named pool and records it on the session", func() {
		resp := post(flowCookie("fl_page"), signInBody("fl_page", map[string]any{"provider": "second"}))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		s := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Peek(sessionCookieValue(resp))
		Expect(s.Provider).To(Equal("second"))
	})

	It("records the browser that signed in on its session, for the person's own device list", func() {
		resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		s := session.NewService(rmngCtx).Peek(sessionCookieValue(resp))
		rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(s.UserID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect([]string{rows[0].UserAgent, rows[0].IPAddress}).To(Equal([]string{browserUA, browserIP}))
	})

	It("rotates the session this browser already held, so signing in again retires the old cookie", func() {
		first := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(first.StatusCode).To(Equal(http.StatusOK), first.Body)
		oldCookie := sessionCookieValue(first)
		sessions := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		oldSID := sessions.Peek(oldCookie).SID

		createFlow("fl_again", testClientID, true, auth_flows_db.FlowTTL)
		again := post(flowCookie("fl_again")+"; "+session.CookieName+"="+oldCookie, signInBody("fl_again", nil))
		Expect(again.StatusCode).To(Equal(http.StatusOK), again.Body)

		Expect(sessions.Peek(oldCookie)).To(BeNil(), "the cookie presented on the way in must stop working")
		Expect(sessions.Peek(sessionCookieValue(again)).SID).To(Equal(oldSID), "one browser stays one session")
	})

	It("reads the flow and its client once each", func() {
		resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
		accesses := backend.DBMock.ProfileGet().Accesses
		Expect(accesses["espuser-auth-flows"].ReadCount).To(Equal(1))
		Expect(accesses["espuser-oauth-clients"].ReadCount).To(Equal(1))
	})

	DescribeTable("refuses before the password reaches the pool (negative)",
		func(cookie, body func() string, status int, code string) {
			resp := post(cookie(), body())
			Expect(resp.StatusCode).To(Equal(status), resp.Body)
			Expect(resp.Body).To(ContainSubstring(`"error":"` + code + `"`))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			noTokens(resp)
			Expect(cog.InitiateAuthCalls).To(BeZero())
		},
		Entry("no flow cookie", func() string { return "" }, func() string { return signInBody("fl_page", nil) },
			http.StatusBadRequest, oidc.RMErrExpiredFlow),
		Entry("a page drawn for another sign-in than the cookie's", func() string { return flowCookie("fl_page") },
			func() string { return signInBody("fl_older", nil) }, http.StatusBadRequest, oidc.RMErrFlowReplaced),
		Entry("an expired flow", func() string {
			createFlow("fl_expired", testClientID, true, -time.Minute)
			return flowCookie("fl_expired")
		}, func() string { return signInBody("fl_expired", nil) }, http.StatusBadRequest, oidc.RMErrExpiredFlow),
		Entry("a flow that does not exist", func() string { return flowCookie("fl_gone") },
			func() string { return signInBody("fl_gone", nil) }, http.StatusBadRequest, oidc.RMErrExpiredFlow),
		Entry("a body that is not JSON", func() string { return flowCookie("fl_page") }, func() string { return "{" },
			http.StatusBadRequest, oidc.OAuthErrInvalidRequest),
		Entry("no flow cookie and a body that is not JSON, which is still a sign-in to start again", func() string { return "" },
			func() string { return "{" }, http.StatusBadRequest, oidc.RMErrExpiredFlow),
		Entry("an unknown provider", func() string { return flowCookie("fl_page") },
			func() string { return signInBody("fl_page", map[string]any{"provider": "nope"}) }, http.StatusBadRequest, oidc.OAuthErrInvalidRequest),
		Entry("a provider that signs in on its own page", func() string { return flowCookie("fl_page") },
			func() string { return signInBody("fl_page", map[string]any{"provider": "corp"}) }, http.StatusBadRequest, oidc.OAuthErrInvalidRequest),
		Entry("a provider the client does not allow", func() string {
			createFlow("fl_restricted", restrictedID, true, auth_flows_db.FlowTTL)
			return flowCookie("fl_restricted")
		}, func() string { return signInBody("fl_restricted", nil) }, http.StatusForbidden, oidc.RMErrProviderNotAllowed),
		Entry("a client that cannot be read, which refuses rather than admits", func() string {
			createFlow("fl_orphan", "no-such-client", true, auth_flows_db.FlowTTL)
			return flowCookie("fl_orphan")
		}, func() string { return signInBody("fl_orphan", nil) }, http.StatusForbidden, oidc.RMErrProviderNotAllowed),
	)

	It("admits the provider a restricted client does allow, the positive control for the refusal above", func() {
		createFlow("fl_restricted", restrictedID, true, auth_flows_db.FlowTTL)
		resp := post(flowCookie("fl_restricted"), signInBody("fl_restricted", map[string]any{"provider": "second"}))
		Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
	})

	It("describes a refusal in the native API's words, saying what to do", func() {
		resp := post(flowCookie("fl_page"), signInBody("fl_page", map[string]any{"password": "not-it"}))
		Expect(resp.Body).To(MatchJSON(`{"error":"` + oidc.RMErrInvalidCredentials + `","error_description":"` + legacyauth.InvalidCredentialsMessage + `"}`))
	})

	DescribeTable("answers a password the pool did not accept with the code the page acts on (negative)",
		func(prepare func(), body func() string, status int, code string) {
			prepare()
			resp := post(flowCookie("fl_page"), body())
			Expect(resp.StatusCode).To(Equal(status), resp.Body)
			Expect(resp.Body).To(ContainSubstring(`"error":"` + code + `"`))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			noTokens(resp)
		},
		Entry("a wrong password", func() {}, func() string { return signInBody("fl_page", map[string]any{"password": "not-it"}) },
			http.StatusUnauthorized, oidc.RMErrInvalidCredentials),
		Entry("an account that does not exist", func() {}, func() string {
			return signInBody("fl_page", map[string]any{"username": "nobody@example.com", "password": "not-it"})
		}, http.StatusUnauthorized, oidc.RMErrInvalidCredentials),
		Entry("an account never verified", func() {}, func() string { return signInBody("fl_page", map[string]any{"username": unconfirmedMail}) },
			http.StatusForbidden, oidc.RMErrUnconfirmedAccount),
		Entry("an account that must choose a new password", func() { cog.ChallengeOnAuth = "NEW_PASSWORD_REQUIRED" },
			func() string { return signInBody("fl_page", nil) }, http.StatusForbidden, oidc.RMErrNewPasswordRequired),
		Entry("an account an admin reset", func() {
			cog.InitiateAuthError = &cognitotypes.PasswordResetRequiredException{Message: aws.String("Password reset required for the user")}
		}, func() string { return signInBody("fl_page", nil) }, http.StatusForbidden, oidc.RMErrPasswordResetRequired),
		Entry("a right password the pool follows with a step this page cannot take, which must not read as a wrong password",
			func() { cog.ChallengeOnAuth = "SOFTWARE_TOKEN_MFA" },
			func() string { return signInBody("fl_page", nil) }, http.StatusForbidden, oidc.OAuthErrAccessDenied),
		Entry("our own step failing after the password was accepted, which must not read as a wrong password",
			func() { cog.IDTokenMinter = func(*mock.UserState) string { return "not-a-jwt" } },
			func() string { return signInBody("fl_page", nil) }, http.StatusInternalServerError, oidc.OAuthErrServerError),
	)

	DescribeTable("answers server_error when the pool fails rather than refuses, so an outage never reads as a wrong password (negative)",
		func(poolErr error) {
			cog.InitiateAuthError = poolErr
			resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError), resp.Body)
			Expect(resp.Body).To(ContainSubstring(`"error":"` + oidc.OAuthErrServerError + `"`))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			noTokens(resp)
		},
		Entry("throttled", &cognitotypes.TooManyRequestsException{}),
		Entry("the row's app client is gone", &cognitotypes.ResourceNotFoundException{}),
		Entry("the row's client secret is wrong", &cognitotypes.NotAuthorizedException{Message: aws.String("Unable to verify secret hash for client pool-app-client")}),
		Entry("the pool unreachable", errors.New("dial tcp: i/o timeout")),
	)

	DescribeTable("refuses an account the pool vouched for that we cannot use, with the federation callback's answer (negative)",
		func(prepare func()) {
			prepare()
			resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden), resp.Body)
			Expect(resp.Body).To(MatchJSON(`{"error":"access_denied","error_description":"This account cannot be signed in. Please contact support."}`))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			noTokens(resp)
		},
		Entry("no verified email or phone", func() {
			cog.IDTokenMinter = func(user *mock.UserState) string {
				tok, err := jwtutil.SignRS256(jwtgo.MapClaims{"email": user.Email, "email_verified": false}, backend.SigningKey, oidc.SigningKeyID)
				Expect(err).NotTo(HaveOccurred())
				return tok
			}
		}),
		Entry("an email and a phone held by two different accounts", func() {
			oauth, err := auth.NewOAuthUserAuthService(context.Background())
			Expect(err).NotTo(HaveOccurred())
			rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
			_, err = oauth.ResolveOrCreateUserByContacts(rmngCtx, personEmail, "", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = oauth.ResolveOrCreateUserByContacts(rmngCtx, "", "+15550100", nil)
			Expect(err).NotTo(HaveOccurred())
			cog.IDTokenMinter = func(user *mock.UserState) string {
				tok, err := jwtutil.SignRS256(jwtgo.MapClaims{
					"email": user.Email, "email_verified": true, "phone_number": "+15550100", "phone_number_verified": true,
				}, backend.SigningKey, oidc.SigningKeyID)
				Expect(err).NotTo(HaveOccurred())
				return tok
			}
		}),
	)

	It("hands over the session it rotated even when another tab finished the flow first, so the browser is never left with a retired cookie", func() {
		first := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(first.StatusCode).To(Equal(http.StatusOK), first.Body)
		oldCookie := sessionCookieValue(first)
		sessions := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		oldSID := sessions.Peek(oldCookie).SID

		createFlow("fl_raced", testClientID, true, auth_flows_db.FlowTTL)
		_, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			IssueCode("fl_raced", "someone", []string{"openid"}, "other-tabs-code", "", time.Now().Unix())
		Expect(err).NotTo(HaveOccurred())

		raced := post(flowCookie("fl_raced")+"; "+session.CookieName+"="+oldCookie, signInBody("fl_raced", nil))
		Expect(raced.StatusCode).To(Equal(http.StatusBadRequest), raced.Body)
		Expect(raced.Body).To(ContainSubstring(`"error":"` + oidc.RMErrExpiredFlow + `"`))
		noTokens(raced)
		Expect(sessions.Peek(oldCookie)).To(BeNil())
		Expect(sessions.Peek(sessionCookieValue(raced)).SID).To(Equal(oldSID), "the cookie the old one was rotated into")
	})

	It("answers server_error when no provider takes a password on our page at all, a deployment fault rather than the person's (negative)", func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		awscommon.SetCognitoProviderClient(cog)
		createProvider("corp", identity_providers_db.TypeOIDC, "")
		createClient(testClientID, true, nil)
		createFlow("fl_page", testClientID, true, auth_flows_db.FlowTTL)

		resp := post(flowCookie("fl_page"), signInBody("fl_page", nil))
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError), resp.Body)
		Expect(resp.Body).To(ContainSubstring(`"error":"` + oidc.OAuthErrServerError + `"`))
		noTokens(resp)
		Expect(cog.InitiateAuthCalls).To(BeZero())
	})

	It("gives a wrong password and an unknown account the same answer, so the form is no account-existence oracle", func() {
		wrong := post(flowCookie("fl_page"), signInBody("fl_page", map[string]any{"password": "not-it"}))
		unknown := post(flowCookie("fl_page"), signInBody("fl_page", map[string]any{"username": "nobody@example.com", "password": "not-it"}))
		Expect(wrong.StatusCode).To(Equal(unknown.StatusCode))
		Expect(wrong.Body).To(Equal(unknown.Body))
	})
})
