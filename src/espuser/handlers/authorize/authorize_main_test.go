// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/auth_flows_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	hostedui "github.com/espressif/esp-rainmaker-neo/src/espuser/hosted-ui"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	jwtgo "github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/rs/zerolog"
)

func TestAuthorizeHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authorize Endpoint Suite")
}

const (
	testClientID        = "user-pool-client"
	redirectURI         = "com.example://callback"
	testResource        = "https://api.example.com"
	testAccountResource = "https://account.example.com"
)

// hardenedPageHeaders are what every page of the sign-in app is served with, the error page included.
var hardenedPageHeaders = map[string]string{
	"Content-Type":            "text/html; charset=utf-8",
	"Cache-Control":           "no-store",
	"X-Content-Type-Options":  "nosniff",
	"X-Frame-Options":         "DENY",
	"Referrer-Policy":         "no-referrer",
	"Content-Security-Policy": hostedui.CSP(),
}

// pageDataFrom decodes the block the app boots from; the page's contract is this JSON, not its markup.
func pageDataFrom(body string) pageData {
	_, rest, found := strings.Cut(body, `<script type="application/json" id="esp-page-data">`)
	Expect(found).To(BeTrue(), "the page must carry its data block")
	block, _, found := strings.Cut(rest, "</script>")
	Expect(found).To(BeTrue())
	var data pageData
	Expect(json.Unmarshal([]byte(block), &data)).To(Succeed())
	return data
}

func providerNames(body string) []string {
	var names []string
	for _, p := range pageDataFrom(body).Providers {
		names = append(names, p.Name)
	}
	return names
}

// getRequest builds a GET the way API Gateway delivers it, with every query parameter in both maps.
func getRequest(path string, query map[string]string) events.APIGatewayProxyRequest {
	multi := make(map[string][]string, len(query))
	for k, v := range query {
		multi[k] = []string{v}
	}
	return events.APIGatewayProxyRequest{HTTPMethod: http.MethodGet, Path: path, QueryStringParameters: query, MultiValueQueryStringParameters: multi}
}

// validQuery is a well-formed authorize request; overrides tweak/remove params per spec.
func validQuery(overrides map[string]string) map[string]string {
	q := map[string]string{
		"response_type":         "code",
		"client_id":             testClientID,
		"redirect_uri":          redirectURI,
		"scope":                 "openid email",
		"state":                 "xyz",
		"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"code_challenge_method": "S256",
	}
	for k, v := range overrides {
		if v == "" {
			delete(q, k)
		} else {
			q[k] = v
		}
	}
	return q
}

// establishSession creates a live session directly through the service and returns the
// Cookie header a browser holding it would send.
func establishSession(userID string, authTime int64) string {
	svc := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	cookieValue, _, err := svc.Establish(session.EstablishInput{UserID: userID, Provider: "up", AuthTime: authTime})
	Expect(err).NotTo(HaveOccurred())
	Expect(cookieValue).NotTo(BeEmpty())
	return session.CookieName + "=" + cookieValue
}

func withCookie(req events.APIGatewayProxyRequest, cookie string) events.APIGatewayProxyRequest {
	req.Headers = map[string]string{"Cookie": cookie}
	return req
}

func flowIDFrom(setCookie string) string {
	return strings.SplitN(strings.TrimPrefix(setCookie, auth.FlowCookieName+"="), ";", 2)[0]
}

var _ = Describe("GET /oauth2/authorize", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE:      utils.Ptr(true), // public clients always require PKCE
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
	})

	It("302s to the login page with a __Host- flow cookie for a valid request", func() {
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathLogin))
		// The prefix holds only for a Secure, Path=/, Domain-less cookie, which is what stops a sibling host planting a flow id; Lax, because Strict would drop it on the upstream's cross-site return.
		Expect(auth.FlowCookieName).To(HavePrefix("__Host-"))
		Expect(resp.Headers["Set-Cookie"]).To(MatchRegexp(`^` + auth.FlowCookieName + `=[^;]+; Path=/; HttpOnly; Secure; SameSite=Lax$`))

		//   1  GetItem espuser-oauth-clients      the registry check inside StartAuthFlow
		//   2  PutItem espuser-auth-flows         the LOGIN flow record (write)
		//   3  Scan    espuser-identity-providers loginRedirect, pinned because it is a Scan on every unauthenticated login
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(2))
		Expect(writeCnt).To(Equal(1), "exactly one flow record, and nothing else written")
	})

	// API Gateway strips the stage from request.Path; only the execute-api host carries it in the browser's URL.
	DescribeTable("keeps the stage prefix in the login redirect only where the browser's URL has one",
		func(domain, location string) {
			req := getRequest(pathAuthorize, validQuery(nil))
			req.RequestContext.Stage = "prod"
			req.RequestContext.DomainName = domain
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Headers["Location"]).To(Equal(location))
		},
		Entry("the execute-api host", "abc123.execute-api.eu-west-1.amazonaws.com", "/prod/user/login"),
		Entry("a custom domain, whose base-path mapping pins the stage (negative)", "user.api.example.com", "/user/login"),
	)

	It("renders the hardened error page for an unknown client, naming nothing it was handed (negative, no redirect)", func() {
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"client_id": "ghost"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).To(Equal(hardenedPageHeaders), "no Location: an unvalidated request must not redirect")
		Expect(pageDataFrom(resp.Body).Error).To(Equal(&errorData{Code: oidc.OAuthErrInvalidClient, Message: "Unknown client."}))
		// Anyone can reach this page by typing a URL; echoing the parameters back makes it a mirror.
		Expect(resp.Body).NotTo(ContainSubstring("ghost"))
		Expect(resp.Body).NotTo(ContainSubstring(redirectURI))

		// One registry GetItem and no write, so an unauthenticated caller cannot fill the flows table.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(1))
		Expect(writeCnt).To(BeZero(), "a rejected request must not create a flow record")
	})

	// redirect_uri must match a registered value EXACTLY (RFC 9700 §4.1): each classic prefix/substring/domain bypass fails closed to the error page, so no code leaks to an attacker endpoint.
	DescribeTable("rejects redirect_uri manipulation vectors (open-redirect defense)",
		func(evil string) {
			clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: "https_client", ClientType: oauth_clients_db.ClientTypePublic,
				RedirectURIs: []string{"https://app.example.com/cb"}, Scopes: []string{"openid", "email"},
				RequirePKCE:      utils.Ptr(true),
				AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
			})).To(Succeed())

			resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize,
				validQuery(map[string]string{"client_id": "https_client", "redirect_uri": evil})))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest), "vector must be rejected: %s", evil)
			Expect(resp.Headers).NotTo(HaveKey("Location"), "must not redirect to: %s", evil)
		},
		Entry("path suffix append", "https://app.example.com/cb/../evil"),
		Entry("extra path segment", "https://app.example.com/cb/extra"),
		Entry("trailing slash", "https://app.example.com/cb/"),
		Entry("subdomain swap", "https://app.example.com.evil.com/cb"),
		Entry("userinfo host trick", "https://app.example.com@evil.com/cb"),
		Entry("scheme downgrade to http", "http://app.example.com/cb"),
		Entry("extra query param", "https://app.example.com/cb?x=1"),
		Entry("open-redirect appended", "https://app.example.com/cb/redirect?to=https://evil.com"),
		Entry("different host entirely", "https://evil.com/cb"),
		Entry("another app's custom scheme", "com.evil://cb"),
	)

	DescribeTable("renders the error page for a malformed request (negative)",
		func(overrides map[string]string) {
			resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(overrides)))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Headers).NotTo(HaveKey("Location"))
		},
		Entry("no PKCE challenge from a require_pkce client", map[string]string{"code_challenge": ""}),
		Entry("a PKCE method other than S256", map[string]string{"code_challenge_method": "plain"}),
		Entry("a response_type other than code", map[string]string{"response_type": "token"}),
	)

	It("allows a confidential client with require_pkce=false to omit the challenge", func() {
		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "conf_nopkce", ClientType: oauth_clients_db.ClientTypeConfidential,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"},
			RequirePKCE:      utils.Ptr(false),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
		q := validQuery(map[string]string{"client_id": "conf_nopkce", "scope": "openid", "code_challenge": "", "code_challenge_method": ""})
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, q))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound), "no-PKCE confidential client may start a flow without a challenge")
	})

	// By this point redirect_uri has been checked against the registry, so the error goes back to the client (RFC 6749 s4.1.2.1), before anyone completes a login for a request that was never valid.
	DescribeTable("redirects the client with the error once redirect_uri is validated (negative)",
		func(overrides map[string]string, code string) {
			resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(overrides)))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			loc, err := url.Parse(resp.Headers["Location"])
			Expect(err).NotTo(HaveOccurred())
			Expect(loc.Scheme + "://" + loc.Host + loc.Path).To(Equal(redirectURI))
			Expect(loc.Query()).To(Equal(url.Values{"error": {code}, "state": {"xyz"}}), "no code is issued")
		},
		Entry("an unregistered resource", map[string]string{"resource": "https://api.someone-else.example.com"}, oidc.OAuthErrInvalidTarget),
		Entry("a scope outside the client's allowed set", map[string]string{"scope": "openid admin"}, oidc.OAuthErrInvalidScope),
	)

	It("lets one sign-in cover several resources (RFC 8707 s2.1)", func() {
		req := getRequest(pathAuthorize, validQuery(nil))
		req.MultiValueQueryStringParameters["resource"] = []string{testResource, testAccountResource}
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("error="))
	})
})

// startFlow is what /oauth2/authorize leaves behind for a client: the flow's id, which the browser holds as a cookie.
func startFlow(clientID string) string {
	resp, err := handleAuthorizeRequest(context.Background(),
		getRequest(pathAuthorize, validQuery(map[string]string{"client_id": clientID})))
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusFound))
	return flowIDFrom(resp.Headers["Set-Cookie"])
}

func loginRequest(flowID string, query map[string]string) events.APIGatewayProxyRequest {
	return withCookie(getRequest(pathLogin, query), auth.FlowCookieName+"="+flowID)
}

var _ = Describe("GET /user/login", func() {
	var backend *test_utils.EspUserBackend

	enable := func(entry identity_providers_db.ProviderEntry) {
		entry.Enabled = utils.Ptr(true)
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&entry)).To(Succeed())
	}
	registerClient := func(clientID, name string, allowed []string) {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: clientID, ClientName: name, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
				RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
				AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
				AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
			})).To(Succeed())
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		registerClient(testClientID, "Acme App", nil)
	})

	It("serves the hardened sign-in app, naming the flow the cookie holds", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC})
		enable(identity_providers_db.ProviderEntry{ProviderName: "personal", Type: identity_providers_db.TypeOIDC})
		flowID := startFlow(testClientID)

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(flowID, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers).To(Equal(hardenedPageHeaders))
		// The app's own data is application/json, which the browser never executes, so nothing needs a nonce.
		Expect(hostedui.CSP()).To(ContainSubstring("default-src 'none'"))
		Expect(hostedui.CSP()).To(ContainSubstring("script-src 'sha256-"))
		Expect(hostedui.CSP()).To(ContainSubstring("frame-ancestors 'none'"))
		Expect(hostedui.CSP()).NotTo(ContainSubstring("nonce-"))
		Expect(hostedui.CSP()).NotTo(ContainSubstring("https://"), "no remote origin belongs on a sign-in page")
		Expect(resp.Body).To(ContainSubstring(`<div id="root">`))
		Expect(pageDataFrom(resp.Body).FlowID).To(Equal(flowID))
	})

	It("describes only the providers the flow's client allows, with that client's registered name", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", DisplayName: "Acme SSO", Type: identity_providers_db.TypeOIDC})
		enable(identity_providers_db.ProviderEntry{ProviderName: "personal", Type: identity_providers_db.TypeOIDC})
		registerClient("restricted-client", "Restricted App", []string{"acme"})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow("restricted-client"), nil))
		Expect(err).NotTo(HaveOccurred())
		data := pageDataFrom(resp.Body)
		Expect(data.Providers).To(Equal([]providerData{{Name: "acme", DisplayName: "Acme SSO", Method: "redirect"}}))
		Expect(data.Client.Name).To(Equal("Restricted App"))
	})

	It("describes every enabled provider for a client that restricts none, by name, with its display name", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "cognito", DisplayName: "Espressif Account", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, ClientID: "app"})
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", DisplayName: "Acme SSO", Type: identity_providers_db.TypeOIDC})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow(testClientID), nil))
		Expect(err).NotTo(HaveOccurred())
		data := pageDataFrom(resp.Body)
		Expect(data.Providers).To(Equal([]providerData{
			{Name: "acme", DisplayName: "Acme SSO", Method: "redirect"},
			{Name: "cognito", DisplayName: "Espressif Account", Method: "password"},
		}))
		Expect(data.Client.Name).To(Equal("Acme App"))
	})

	It("leaves out a password row without a client_id, whose form could not check a password and must not become a button (negative)", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito})
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow(testClientID), nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(providerNames(resp.Body)).To(Equal([]string{"acme"}))
	})

	It("keeps a display name that looks like markup out of the document structure", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "evil", Type: "oidc", DisplayName: `</a><script>alert(1)</script>`})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow(testClientID), nil))
		Expect(err).NotTo(HaveOccurred())
		// One unescaped </script> would end the data block; json.Marshal escapes < > &, a property of the encoder worth asserting rather than trusting.
		Expect(resp.Body).NotTo(ContainSubstring("<script>alert(1)</script>"))
		Expect(resp.Body).NotTo(ContainSubstring("</a>"))
		// Escaped, not stripped, or the page would quietly rename providers.
		Expect(pageDataFrom(resp.Body).Providers).To(Equal([]providerData{{Name: "evil", DisplayName: `</a><script>alert(1)</script>`, Method: "redirect"}}))
	})

	It("offers a code sign-in and no provider when only OTP is enabled", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "otp", Type: identity_providers_db.TypeOTP, DisplayName: "Code"})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow(testClientID), nil))
		Expect(err).NotTo(HaveOccurred())
		data := pageDataFrom(resp.Body)
		Expect(data.Providers).To(BeEmpty())
		Expect(data.OTPEnabled).To(BeTrue())
	})

	It("takes only login_hint and idp from the address, ignoring a providers or client it is handed (negative)", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC})
		enable(identity_providers_db.ProviderEntry{ProviderName: "personal", Type: identity_providers_db.TypeOIDC})
		registerClient("restricted-client", "Restricted App", []string{"acme"})

		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow("restricted-client"), map[string]string{
			"providers": "acme,personal", "client": "Your bank", "login_hint": "person@example.com", "idp": "acme",
		}))
		Expect(err).NotTo(HaveOccurred())
		data := pageDataFrom(resp.Body)
		Expect(providerNames(resp.Body)).To(Equal([]string{"acme"}))
		Expect(data.Client.Name).To(Equal("Restricted App"))
		Expect(data.LoginHint).To(Equal("person@example.com"))
		Expect(data.IDP).To(Equal("acme"))
	})

	It("reads one flow and one client beside the provider scan", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC})
		enable(identity_providers_db.ProviderEntry{ProviderName: "personal", Type: identity_providers_db.TypeOIDC})
		flowID := startFlow(testClientID)

		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), loginRequest(flowID, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		//   1  GetItem espuser-auth-flows         the flow the cookie names
		//   2  GetItem espuser-oauth-clients      that flow's client
		//   3  Scan    espuser-identity-providers the providers to draw
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(3))
		Expect(writeCnt).To(BeZero())
		Expect(profile.Accesses["espuser-auth-flows"].ReadCount).To(Equal(1))
		Expect(profile.Accesses["espuser-oauth-clients"].ReadCount).To(Equal(1))
	})

	// No form: a page that listed providers for a sign-in nobody can finish would only send the person into an error one step later.
	DescribeTable("draws the error screen and no form without a live flow (negative)",
		func(cookie func() string, status int, code, restartURL string) {
			enable(identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC})
			enable(identity_providers_db.ProviderEntry{ProviderName: "personal", Type: identity_providers_db.TypeOIDC})
			req := getRequest(pathLogin, nil)
			if c := cookie(); c != "" {
				req = withCookie(req, auth.FlowCookieName+"="+c)
			}
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(status))
			Expect(resp.Headers).To(Equal(hardenedPageHeaders))
			data := pageDataFrom(resp.Body)
			Expect(data.Error).NotTo(BeNil())
			Expect(data.Error.Code).To(Equal(code))
			Expect(data.RestartURL).To(Equal(restartURL))
			Expect(data.Providers).To(BeEmpty())
			Expect(data.FlowID).To(BeEmpty(), "nothing on the page can finish a sign-in")
		},
		Entry("no cookie, so there is nothing to restart", func() string { return "" }, http.StatusBadRequest, oidc.OAuthErrInvalidRequest, ""),
		Entry("a flow that does not exist, which Start again could not rebuild", func() string { return "fl_missing" }, http.StatusBadRequest, oidc.RMErrExpiredFlow, ""),
		Entry("an expired flow", func() string {
			Expect(auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateFlow(&auth_flows_db.AuthFlow{
				FlowID: "fl_expired", ClientID: testClientID, RedirectURI: redirectURI, State: "xyz",
				RequestedScope: []string{"openid"}, ExpiresOn: time.Now().Add(-time.Minute).Unix(),
			})).To(Succeed())
			return "fl_expired"
		}, http.StatusBadRequest, oidc.RMErrExpiredFlow, pathRestart),
		Entry("a flow whose client no longer exists, which never means every provider", func() string {
			flowID := startFlow(testClientID)
			Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).DeleteClient(testClientID)).To(Succeed())
			return flowID
		}, http.StatusBadRequest, oidc.RMErrExpiredFlow, pathRestart),
	)

	It("tells a browser with no flow cookie to start again from the application, in the message", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(pageDataFrom(resp.Body).Error.Message).To(ContainSubstring("start again from the application"))
	})

	It("logs a registry it cannot read as a warning, and says there is no way in rather than drawing an empty card", func() {
		flowID := startFlow(testClientID)
		Expect(os.Unsetenv("ESPUSER_REFRESH_SECRET_PARAM")).To(Succeed())
		DeferCleanup(func() { Expect(os.Setenv("ESPUSER_REFRESH_SECRET_PARAM", backend.RefreshSecretParam)).To(Succeed()) })
		level := zerolog.GlobalLevel()
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		DeferCleanup(func() { zerolog.SetGlobalLevel(level) })
		var logged bytes.Buffer
		ctx := zerolog.New(&logged).WithContext(context.Background())

		resp, err := handleAuthorizeRequest(ctx, loginRequest(flowID, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(pageDataFrom(resp.Body).Error.Code).To(Equal(oidc.OAuthErrServerError))
		Expect(logged.String()).To(And(ContainSubstring(`"level":"warn"`), ContainSubstring("provider registry")))
	})

	DescribeTable("answers a sign-in with no way in with the error screen, never an empty card (negative)",
		func(allowed []string, enabled ...identity_providers_db.ProviderEntry) {
			for _, entry := range enabled {
				enable(entry)
			}
			registerClient("stranded-client", "Stranded App", allowed)

			resp, err := handleAuthorizeRequest(context.Background(), loginRequest(startFlow("stranded-client"), nil))

			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			data := pageDataFrom(resp.Body)
			Expect(data.Error).NotTo(BeNil())
			Expect(data.Error.Code).To(Equal(oidc.OAuthErrServerError))
			Expect(data.FlowID).To(BeEmpty(), "nothing on the page can finish a sign-in")
		},
		Entry("the client allows only a provider that is not enabled", []string{"nope"},
			identity_providers_db.ProviderEntry{ProviderName: "acme", Type: identity_providers_db.TypeOIDC}),
		Entry("no provider is enabled at all", nil),
	)
})

// The page carries its own script and stylesheet, so there is nothing else to fetch and the CSP admits exactly those bytes.
var _ = Describe("the sign-in page's inlined app", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	inline := func(body, open, close string) string {
		_, rest, found := strings.Cut(body, open)
		Expect(found).To(BeTrue(), "the page must carry "+open)
		text, _, found := strings.Cut(rest, close)
		Expect(found).To(BeTrue())
		return text
	}
	hash := func(text string) string {
		sum := sha256.Sum256([]byte(text))
		return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}

	It("loads nothing from anywhere, and its CSP admits exactly the script and stylesheet it carries", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body).NotTo(ContainSubstring(" src="), "no external script")
		Expect(resp.Body).NotTo(ContainSubstring(`rel="stylesheet"`), "no external stylesheet")

		csp := resp.Headers["Content-Security-Policy"]
		Expect(csp).To(ContainSubstring("script-src " + hash(inline(resp.Body, `<script type="module">`, "</script>")) + ";"))
		Expect(csp).To(ContainSubstring("style-src " + hash(inline(resp.Body, "<style>", "</style>")) + ";"))
	})
})

// Restart replays the request a timed-out login began with, so what it rebuilds from is the whole of its security.
var _ = Describe("GET /user/login/restart", func() {
	var flowID string

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE:      utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())

		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		flowID = flowIDFrom(resp.Headers["Set-Cookie"])
	})

	restart := func(req events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	// Every parameter the client's own security rests on comes back, or the second attempt is weaker than the first: no state is no CSRF binding, no challenge is no PKCE.
	DescribeTable("rebuilds the authorize request the login began with, never from the request's own query",
		func(query map[string]string) {
			resp := restart(withCookie(getRequest(pathRestart, query), auth.FlowCookieName+"="+flowID))
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))

			location, err := url.Parse(resp.Headers["Location"])
			Expect(err).NotTo(HaveOccurred())
			Expect(location.Path).To(Equal(pathAuthorize))
			Expect(location.Query()).To(Equal(url.Values{
				"response_type":         {"code"},
				"client_id":             {testClientID},
				"redirect_uri":          {redirectURI},
				"scope":                 {"openid email"},
				"state":                 {"xyz"},
				"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
				"code_challenge_method": {"S256"},
				"resource":              {testResource},
			}))
		},
		Entry("a bare restart", nil),
		Entry("a restart carrying an attacker's parameters (negative)", map[string]string{
			"redirect_uri": "https://attacker.example/steal", "client_id": "some-other-client", "state": "attacker-state",
		}),
	)

	DescribeTable("tells the app there is nothing to restart (negative)",
		func(cookie string) {
			req := getRequest(pathRestart, nil)
			if cookie != "" {
				req = withCookie(req, cookie)
			}
			resp := restart(req)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Code).To(Equal(oidc.OAuthErrInvalidRequest))
		},
		Entry("no flow cookie", ""),
		Entry("a flow id that names nothing", auth.FlowCookieName+"=fl_not_a_flow"),
	)

	It("restarts a login that has already expired, which is the case it exists for", func() {
		flows := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(flows.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_long_expired", ClientID: testClientID, RedirectURI: redirectURI,
			State: "xyz", RequestedScope: []string{"openid", "email"},
			ExpiresOn: time.Now().Add(-30 * time.Minute).Unix(),
		})).To(Succeed())

		resp := restart(withCookie(getRequest(pathRestart, nil), auth.FlowCookieName+"=fl_long_expired"))
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("client_id=" + testClientID))
	})
})

var _ = Describe("routing", func() {
	It("rejects a non-GET method (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: pathAuthorize})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})
})

var _ = Describe("GET /oauth2/authorize with a session (SSO)", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE:      utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
	})

	It("falls through to the login page for a session cookie that resolves to no live session (negative)", func() {
		req := withCookie(getRequest(pathAuthorize, validQuery(nil)), session.CookieName+"=whatever")
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathLogin))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(auth.FlowCookieName + "="))

		// The login-page cost (2 reads + the flow write) plus one conditional sessions UpdateItem that fails and changes nothing.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(2))
		Expect(writeCnt).To(Equal(2), "the flow record and the failed conditional session touch")
		Expect(profile.Accesses["espuser-sessions"].ReadCount).To(BeZero())
		Expect(profile.Accesses["espuser-sessions"].WriteCount).To(Equal(1))
	})

	It("issues a code immediately for a live session: no login page, no upstream leg", func() {
		cookie := establishSession("user-sso-1", time.Now().Unix())
		backend.DBMock.ProfileReset()

		resp, err := handleAuthorizeRequest(context.Background(), withCookie(getRequest(pathAuthorize, validQuery(nil)), cookie))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(HavePrefix(redirectURI), "straight back to the client")
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
		Expect(resp.Headers["Location"]).To(ContainSubstring("state=xyz"))

		//   1  GetItem    espuser-oauth-clients  the registry check inside StartAuthFlow, reused for allowed_providers and consent
		//   2  PutItem    espuser-auth-flows     the LOGIN flow record
		//   3  UpdateItem espuser-sessions       Lookup: roll expiry and return the row (ALL_NEW)
		//   4  UpdateItem espuser-auth-flows     the flow moves to CODE and returns redirect_uri/state (ALL_NEW)
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(1))
		Expect(writeCnt).To(Equal(3))
		Expect(profile.Accesses["espuser-oauth-clients"].ReadCount).To(Equal(1),
			"the client row is read once and reused, never re-read for the provider check")
	})

	It("ties the issued code to the session's user: the exchanged token belongs to them", func() {
		cookie := establishSession("user-sso-2", time.Now().Unix())

		resp, err := handleAuthorizeRequest(context.Background(), withCookie(getRequest(pathAuthorize, validQuery(nil)), cookie))
		Expect(err).NotTo(HaveOccurred())
		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		code := loc.Query().Get("code")
		Expect(code).NotTo(BeEmpty())

		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlowByCode(code)
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.Subject).To(Equal("user-sso-2"))
		Expect(flow.SID).NotTo(BeEmpty(), "the flow carries the session id so the refresh family can be tied to it")
	})

	It("reads an empty resource as none requested: the code covers the client's default, not invalid_target (RFC 6749 s3.1)", func() {
		cookie := establishSession("user-sso-empty-resource", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(nil)), cookie)
		req.MultiValueQueryStringParameters["resource"] = []string{""}

		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		Expect(loc.Query().Get("error")).To(BeEmpty())
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.Resources).To(Equal([]string{testResource}))
	})

	It("prompt=none with a live session answers silently with a code", func() {
		cookie := establishSession("user-sso-3", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"prompt": "none"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
	})

	It("prompt=none with no session fails with login_required — never a visible prompt (negative)", func() {
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"prompt": "none"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(redirectURI + "?error=login_required&state=xyz"))

		// prompt=none is what a client can send on every page load, so its cost is pinned: the registry read and the flow write, and no sessions read or providers Scan.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(1))
		Expect(writeCnt).To(Equal(1))
		Expect(profile.Accesses).NotTo(HaveKey("espuser-sessions"))
	})

	DescribeTable("refuses prompt=none with any other value back to the client, starting no sign-in (OIDC Core s3.1.2.1, negative)",
		func(prompt string) {
			cookie := establishSession("user-sso", time.Now().Unix())
			resp, err := handleAuthorizeRequest(context.Background(), withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"prompt": prompt})), cookie))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			loc, err := url.Parse(resp.Headers["Location"])
			Expect(err).NotTo(HaveOccurred())
			Expect(loc.Query().Get("error")).To(Equal("invalid_request"))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"), "no flow was started")
		},
		Entry("none and login", "none login"),
		Entry("none and consent", "consent none"),
	)

	It("prompt=login re-authenticates even with a live session — always honoured", func() {
		cookie := establishSession("user-sso-4", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"prompt": "login"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(Equal(pathLogin), "prompt=login must never be shortcut past")
	})

	// max_age=0 is how a relying party forces a fresh authentication, so a session from this very second must not satisfy it either.
	DescribeTable("max_age=0 re-authenticates whatever the session's age (negative)",
		func(authTime time.Time) {
			cookie := establishSession("user-sso-maxage", authTime.Unix())
			req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"max_age": "0"})), cookie)
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(Equal(pathLogin))
		},
		Entry("a session minutes old", time.Now().Add(-time.Minute)),
		Entry("a session created this very second", time.Now()),
	)

	It("max_age accepts a session younger than the bound", func() {
		cookie := establishSession("user-sso-6", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"max_age": "300"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
	})

	DescribeTable("answers from the session only when id_token_hint names its subject",
		func(subject string, location types.GomegaMatcher) {
			cookie := establishSession("user-sso-8", time.Now().Unix())
			hint, err := jwtutil.SignRS256(jwtgo.MapClaims{
				"iss": os.Getenv("USER_ISSUER"), "sub": subject, "aud": testClientID, "token_use": jwtutil.TokenUseID,
				"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			}, backend.SigningKey, jwtutil.RSAThumbprint(&backend.SigningKey.PublicKey))
			Expect(err).NotTo(HaveOccurred())

			req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"id_token_hint": hint, "prompt": "none"})), cookie)
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(location)
		},
		Entry("the session's own subject", "user-sso-8", ContainSubstring("code=")),
		Entry("someone else, which drops the session rather than switching users (negative)", "someone-else", Equal(redirectURI+"?error=login_required&state=xyz")),
	)
})

var _ = Describe("GET /oauth2/federation/callback establishes the session", func() {
	const (
		upstreamACR        = "urn:example:loa:2"
		providerSessionCap = int64(2 * 60 * 60)
		browserUA          = "Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/128.0"
		browserIP          = "198.51.100.4"
	)
	var (
		backend *test_utils.EspUserBackend
		// rowMapping is the provider row's attribute_mapping; nil reads every claim by its default name.
		rowMapping map[string]string
		// tokenClaims override the upstream id token's defaults.
		tokenClaims jwtgo.MapClaims
	)

	// A complete upstream: a token endpoint returning a signed id token, and a JWKS URL publishing its key. authTime 0 models a token that names no authentication time at all.
	setupUpstream := func(authTime int64, nonceFor func() string) (issuer string) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		kid := jwtutil.RSAThumbprint(&priv.PublicKey)
		jwks, err := json.Marshal(jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid)))
		Expect(err).NotTo(HaveOccurred())

		mux := http.NewServeMux()
		srv := httptest.NewServer(mux)
		DeferCleanup(srv.Close)
		mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(jwks) })
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			claims := jwtgo.MapClaims{
				"iss": srv.URL, "aud": "broker-client", "sub": "up-sub-1", "nonce": nonceFor(),
				"email": "sso@example.com", "email_verified": true,
				"amr": []string{"pwd", "mfa"}, "acr": upstreamACR,
				"exp": time.Now().Add(time.Hour).Unix(),
			}
			if authTime > 0 {
				claims["auth_time"], claims["iat"] = authTime, time.Now().Unix()
			}
			for k, v := range tokenClaims {
				claims[k] = v
			}
			tok, err := jwtutil.SignRS256(claims, priv, kid)
			Expect(err).NotTo(HaveOccurred())
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": tok})
		})

		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "up", Type: identity_providers_db.TypeOIDC, Enabled: utils.Ptr(true),
				Issuer: srv.URL, ClientID: "broker-client",
				JWKSURL: srv.URL + "/jwks", TokenURL: srv.URL + "/token", AuthorizeURL: srv.URL + "/authorize",
				SessionMaxTTLSeconds: providerSessionCap, AttributeMapping: rowMapping,
			})).To(Succeed())
		return srv.URL
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		rowMapping, tokenClaims = nil, nil
		os.Setenv("ESPUSER_FEDERATION_CALLBACK_URL", "https://us.example/oauth2/federation/callback")
	})

	// startLeg returns the state the upstream will echo back.
	startLeg := func(skipConsent bool) string {
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(flowsDB.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_sso", ClientID: testClientID, RedirectURI: redirectURI, State: "xyz", SkipConsent: skipConsent,
			RequestedScope: []string{"openid"}, ExpiresOn: time.Now().Add(auth_flows_db.FlowTTL).Unix(),
		})).To(Succeed())

		leg, err := idp.NewUpstreamLeg("fl_sso", idp.StateHMACKey([]byte(backend.RefreshSecret)))
		Expect(err).NotTo(HaveOccurred())
		Expect(flowsDB.SetUpstreamLeg("fl_sso", "up", leg.State, leg.Nonce, leg.PKCEVerifier)).To(Succeed())
		return leg.State
	}

	callback := func(cookie string, query map[string]string) events.APIGatewayProxyResponse {
		req := getRequest(pathFederationCallback, query)
		if cookie != "" {
			req = withCookie(req, cookie)
		}
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	runCallback := func(cookie string, skipConsent bool) events.APIGatewayProxyResponse {
		return callback(cookie, map[string]string{"code": "upstream-code", "state": startLeg(skipConsent)})
	}

	// The upstream nonce is minted when the leg is set up, so the fake upstream reads it back off the flow.
	flowNonce := func() string {
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlow("fl_sso")
		Expect(err).NotTo(HaveOccurred())
		return flow.UpstreamNonce
	}

	// The specs below that send the right cookie are the positive control.
	DescribeTable("refuses a callback from a browser that did not start the flow, before reading it (RFC 6749 s10.12, negative)",
		func(cookie string) {
			setupUpstream(time.Now().Unix(), flowNonce)
			resp := runCallback(cookie, true)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			Expect(resp.Headers).NotTo(HaveKey("Location"))
			Expect(backend.DBMock.ProfileGet().Accesses["espuser-auth-flows"].ReadCount).To(BeZero(), "a forged callback costs no flow read")
		},
		Entry("no flow cookie", ""),
		Entry("a flow cookie naming a different flow", auth.FlowCookieName+"=fl_someone_else"),
	)

	Context("a person who backed out at the provider", func() {
		addProvider := func(name, providerType, passwordVerifier string) {
			Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
				CreateProvider(&identity_providers_db.ProviderEntry{
					ProviderName: name, Type: providerType, PasswordVerifier: passwordVerifier, Enabled: utils.Ptr(true), ClientID: name + "-client",
				})).To(Succeed())
		}
		addClient := func(allowed []string) {
			Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic, RedirectURIs: []string{redirectURI},
				Scopes: []string{"openid"}, AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
				AllowedResources: []string{testResource}, DefaultResource: testResource,
			})).To(Succeed())
		}
		cancel := func(upstreamErr string) events.APIGatewayProxyResponse {
			state := startLeg(true)
			return callback(auth.FlowCookieName+"=fl_sso", map[string]string{"error": upstreamErr, "state": state})
		}
		flowLive := func() bool {
			_, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlow("fl_sso")
			return err == nil
		}

		DescribeTable("goes back to our page, on the same sign-in, when it offers another way in",
			func(prepare func()) {
				prepare()
				resp := cancel(oidc.OAuthErrAccessDenied)
				Expect(resp.StatusCode).To(Equal(http.StatusFound))
				Expect(resp.Headers["Location"]).To(Equal(pathLogin))
				Expect(flowLive()).To(BeTrue(), "the page continues this sign-in")
			},
			Entry("a password row the client allows", func() {
				addProvider("up", identity_providers_db.TypeOIDC, "")
				addProvider("cognito", identity_providers_db.TypeOIDC, identity_providers_db.PasswordVerifierCognito)
				addClient(nil)
			}),
			Entry("an otp row the client allows", func() {
				addProvider("up", identity_providers_db.TypeOIDC, "")
				addProvider("otp", identity_providers_db.TypeOTP, "")
				addClient(nil)
			}),
			Entry("a second provider the client allows", func() {
				addProvider("up", identity_providers_db.TypeOIDC, "")
				addProvider("other", identity_providers_db.TypeOIDC, "")
				addClient(nil)
			}),
		)

		DescribeTable("returns to the client with access_denied and ends the sign-in when the provider was the only way in, since authorize would send them straight back to it",
			func(upstreamErr string, prepare func()) {
				prepare()
				resp := cancel(upstreamErr)
				Expect(resp.StatusCode).To(Equal(http.StatusFound))
				Expect(resp.Headers["Location"]).To(Equal(redirectURI + "?error=access_denied&state=xyz"))
				Expect(flowLive()).To(BeFalse())
			},
			Entry("the only enabled provider", oidc.OAuthErrAccessDenied, func() { addProvider("up", identity_providers_db.TypeOIDC, ""); addClient(nil) }),
			Entry("Apple's cancel", "user_cancelled_authorize", func() { addProvider("up", identity_providers_db.TypeOIDC, ""); addClient(nil) }),
			Entry("the only provider this client allows, though others are enabled", oidc.OAuthErrAccessDenied, func() {
				addProvider("up", identity_providers_db.TypeOIDC, "")
				addProvider("cognito", identity_providers_db.TypeOIDC, identity_providers_db.PasswordVerifierCognito)
				addProvider("otp", identity_providers_db.TypeOTP, "")
				addClient([]string{"up"})
			}),
		)
	})

	It("keeps the error page for another provider failure, with a way to start again", func() {
		state := startLeg(true)
		resp := callback(auth.FlowCookieName+"=fl_sso", map[string]string{"error": "server_error", "state": state})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(pageDataFrom(resp.Body).RestartURL).To(Equal(pathRestart))
	})

	DescribeTable("leads nowhere from a provider error it cannot tie to this browser's sign-in (negative)",
		func(cookie string, state func() string) {
			resp := callback(cookie, map[string]string{"error": "access_denied", "state": state()})
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Headers).NotTo(HaveKey("Location"))
			Expect(pageDataFrom(resp.Body).RestartURL).To(BeEmpty())
		},
		Entry("another browser's sign-in", auth.FlowCookieName+"=fl_someone_else", func() string { return startLeg(true) }),
		Entry("no state at all", auth.FlowCookieName+"=fl_sso", func() string { return "" }),
	)

	It("sets the __Host- session cookie and records the upstream's auth_time on the federation callback", func() {
		// Old enough to prove inheritance: auth_time is the upstream's, never the row's write time.
		upstreamAuthTime := time.Now().Add(-30 * time.Minute).Unix()
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		setupUpstream(upstreamAuthTime, flowNonce)

		resp := runCallback(auth.FlowCookieName+"=fl_sso", true)
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))

		setCookie := resp.Headers["Set-Cookie"]
		Expect(setCookie).To(HavePrefix(session.CookieName + "="))
		Expect(setCookie).To(ContainSubstring("HttpOnly"))
		Expect(strings.ToLower(setCookie)).NotTo(ContainSubstring("domain"))

		cookieValue := strings.SplitN(strings.SplitN(setCookie, ";", 2)[0], "=", 2)[1]
		s := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Peek(cookieValue)
		Expect(s).NotTo(BeNil())
		Expect(s.AuthTime).To(Equal(upstreamAuthTime), "auth_time is the upstream's — 12:00 must not be recorded for a 09:00 login")

		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		flow, err := flowsDB.GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AuthTime).To(Equal(upstreamAuthTime), "the ID token minted from this code must name the upstream sign-in too (OIDC Core s2)")
	})

	It("stamps the code with the callback time when the upstream's token names no authentication time", func() {
		setupUpstream(0, flowNonce)
		before := time.Now().Unix()

		resp := runCallback(auth.FlowCookieName+"=fl_sso", true)
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AuthTime).To(BeNumerically("~", before, 5), "with nothing upstream naming the sign-in, it happened on this request")
	})

	It("records the whole sign-in on the session it rotates: the provider's cap, the upstream's auth_time, amr and acr, and the browser", func() {
		upstreamAuthTime := time.Now().Add(-10 * time.Minute).Unix()
		setupUpstream(upstreamAuthTime, flowNonce)
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		oauth, err := auth.NewOAuthUserAuthService(context.Background())
		Expect(err).NotTo(HaveOccurred())
		userID, err := oauth.ResolveOrCreateUserByContacts(rmngCtx, "sso@example.com", "", nil)
		Expect(err).NotTo(HaveOccurred())
		priorCookie := establishSession(userID, upstreamAuthTime-3600)
		sessions := session.NewService(rmngCtx)
		prior := sessions.Peek(strings.TrimPrefix(priorCookie, session.CookieName+"="))
		priorRows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())

		req := getRequest(pathFederationCallback, map[string]string{"code": "upstream-code", "state": startLeg(true)})
		req = withCookie(req, auth.FlowCookieName+"=fl_sso; "+priorCookie)
		req.RequestContext.Identity.UserAgent = browserUA
		req.RequestContext.Identity.SourceIP = browserIP
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(MatchRegexp(`^com\.example://callback\?code=[^&]+&state=xyz$`))
		Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"), "the code in Location must not leak to the client's page as a Referer")
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
		cookieValue := strings.SplitN(strings.SplitN(resp.Headers["Set-Cookie"], ";", 2)[0], "=", 2)[1]
		Expect(resp.Headers["Set-Cookie"]).To(Equal(session.SetCookieHeader(cookieValue, sessions.MaxAge())))
		Expect(sessions.Peek(strings.TrimPrefix(priorCookie, session.CookieName+"="))).To(BeNil(), "the cookie presented on the way in is retired")

		rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		row := rows[0]
		Expect(row).To(Equal(sessions_db.SessionEntry{
			SessionHash: row.SessionHash, SID: prior.SID, UserID: userID, Provider: "up",
			AuthTime: upstreamAuthTime, AMR: []string{"pwd", "mfa"}, ACR: upstreamACR,
			ExpiresAt: upstreamAuthTime + providerSessionCap, MaxExpiresAt: upstreamAuthTime + providerSessionCap,
			CookieExpiresAt: upstreamAuthTime + providerSessionCap, LastSeenAt: row.LastSeenAt,
			Origin: session.OriginBrowser, CreatedAt: priorRows[0].CreatedAt, UserAgent: browserUA, IPAddress: browserIP,
		}))

		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngCtx).GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect([]any{flow.Subject, flow.SID, flow.AuthTime}).To(Equal([]any{userID, prior.SID, upstreamAuthTime}))
	})

	DescribeTable("refuses an account the upstream vouched for that we cannot use, with one answer that names nobody (negative)",
		func(prepare func()) {
			prepare()
			setupUpstream(time.Now().Unix(), flowNonce)
			resp := runCallback(auth.FlowCookieName+"=fl_sso", true)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(pageDataFrom(resp.Body).Error).To(Equal(&errorData{Code: oidc.OAuthErrAccessDenied, Message: "This account cannot be signed in. Please contact support."}))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
		},
		Entry("an upstream id another account already holds, with no contact tying this login to it", func() {
			rowMapping = map[string]string{idp.SubAttribute: "sub"}
			Expect(user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateUserDetails(&user_details_db.UserDetailsEntry{
				UserID: "up-sub-1", Email: "someone-else@example.com", UserType: user_details_db.UserTypeUser,
			})).To(Succeed())
		}),
		Entry("no verified email or phone", func() {
			tokenClaims = jwtgo.MapClaims{"email_verified": false}
		}),
		Entry("an email and a phone held by two different accounts", func() {
			oauth, err := auth.NewOAuthUserAuthService(context.Background())
			Expect(err).NotTo(HaveOccurred())
			rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
			_, err = oauth.ResolveOrCreateUserByContacts(rmngCtx, "sso@example.com", "", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = oauth.ResolveOrCreateUserByContacts(rmngCtx, "", "+15550100", nil)
			Expect(err).NotTo(HaveOccurred())
			tokenClaims = jwtgo.MapClaims{"phone_number": "+15550100", "phone_number_verified": true}
		}),
	)

	It("answers a flow finished meanwhile as an expired sign-in, and still hands over the session it established so the browser is never left with a retired cookie", func() {
		setupUpstream(time.Now().Unix(), flowNonce)
		state := startLeg(true)
		_, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			IssueCode("fl_sso", "someone", []string{"openid"}, "other-tabs-code", "", time.Now().Unix())
		Expect(err).NotTo(HaveOccurred())

		resp := callback(auth.FlowCookieName+"=fl_sso", map[string]string{"code": "upstream-code", "state": state})
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
		data := pageDataFrom(resp.Body)
		Expect(data.Error).NotTo(BeNil())
		Expect(data.Error.Code).To(Equal(oidc.RMErrExpiredFlow), "the answer an expired flow gets, not the 500 page")
		Expect(data.RestartURL).To(Equal(pathRestart))
		Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(session.CookieName + "="))
		cookieValue := strings.SplitN(strings.SplitN(resp.Headers["Set-Cookie"], ";", 2)[0], "=", 2)[1]
		Expect(session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Peek(cookieValue)).NotTo(BeNil())
	})

	It("sends a sign-in held for consent to the consent screen, still establishing the session and reading no client", func() {
		setupUpstream(time.Now().Unix(), flowNonce)

		resp := runCallback(auth.FlowCookieName+"=fl_sso", false)
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathConsent))
		Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"))
		Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(session.CookieName + "="))
		Expect(backend.DBMock.ProfileGet().Accesses).NotTo(HaveKey("espuser-oauth-clients"), "the flow carries the decision")

		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlow("fl_sso")
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AwaitingConsent()).To(BeTrue())
	})
})

// allowed_providers is checked at all three ways in: the login page draws the buttons, /user/login/federation/start can be typed, and the SSO shortcut skips both.
var _ = Describe("allowed_providers is enforced, not decorative", func() {
	const (
		restrictedClientID = "restricted-client"
		openClientID       = "open-client"
	)

	// Where each provider's fake upstream lives, so the positive controls can assert the browser was actually sent there.
	providerAuthorizeURL := map[string]string{}

	registerClient := func(clientID string, allowed []string) {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
				RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
				AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
				AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
			})).To(Succeed())
	}

	flowIDFor := func(clientID string) string {
		resp, err := handleAuthorizeRequest(context.Background(),
			getRequest(pathAuthorize, validQuery(map[string]string{"client_id": clientID})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(auth.FlowCookieName + "="))
		return flowIDFrom(resp.Headers["Set-Cookie"])
	}

	startFederation := func(flowID, provider string) events.APIGatewayProxyResponse {
		req := withCookie(getRequest(pathFederationStart, map[string]string{"provider": provider, "flow_id": flowID}),
			auth.FlowCookieName+"="+flowID)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		os.Setenv("ESPUSER_FEDERATION_CALLBACK_URL", "https://us.example/oauth2/federation/callback")

		// Both providers are fully usable, so a refusal below can only be the restriction: an unfetchable row fails with the same message the gate returns.
		db := identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		for _, name := range []string{"acme", "personal"} {
			priv, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).NotTo(HaveOccurred())
			kid := jwtutil.RSAThumbprint(&priv.PublicKey)
			jwks, err := json.Marshal(jwtutil.BuildJWKS(jwtutil.BuildJWK(&priv.PublicKey, kid)))
			Expect(err).NotTo(HaveOccurred())
			mux := http.NewServeMux()
			mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(jwks) })
			srv := httptest.NewServer(mux)
			DeferCleanup(srv.Close)
			providerAuthorizeURL[name] = srv.URL + "/authorize"
			Expect(db.CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: name, Type: identity_providers_db.TypeOIDC, Enabled: utils.Ptr(true),
				DisplayName: name, Issuer: srv.URL, ClientID: "broker-client",
				JWKSURL:      srv.URL + "/jwks",
				TokenURL:     srv.URL + "/token",
				AuthorizeURL: srv.URL + "/authorize",
			})).To(Succeed())
		}
		registerClient(restrictedClientID, []string{"acme"})
		registerClient(openClientID, nil)
	})

	Describe("at /user/login/federation/start — the gate, because this URL can be typed", func() {
		It("refuses a provider the client did not register (negative)", func() {
			resp := startFederation(flowIDFor(restrictedClientID), "personal")
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Message).To(Equal("Unknown or unavailable provider."),
				"and it must read like an unknown provider: whether a client restricts one is not the caller's business")
		})

		It("refuses when the flow's client can no longer be read, rather than admit every provider (negative)", func() {
			flowID := flowIDFor(openClientID)
			Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).DeleteClient(openClientID)).To(Succeed())

			resp := startFederation(flowID, "acme")
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Message).To(Equal("Unknown or unavailable provider."))
		})

		It("tells a browser whose flow is gone that the sign-in expired (negative)", func() {
			resp := startFederation("fl_missing", "acme")
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Message).To(Equal("Login session expired. Please try again."))
		})

		It("refuses a link drawn for another sign-in, after a newer one in this browser took the cookie, and leaves for no provider (negative)", func() {
			drawnFor, current := flowIDFor(restrictedClientID), flowIDFor(openClientID)
			req := withCookie(getRequest(pathFederationStart, map[string]string{"provider": "acme", "flow_id": drawnFor}), auth.FlowCookieName+"="+current)
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Code).To(Equal(oidc.RMErrFlowReplaced))
			flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlow(current)
			Expect(err).NotTo(HaveOccurred())
			Expect(flow.UpstreamState).To(BeEmpty(), "the current sign-in did not leave for a provider")
		})

		It("refuses a link that names no sign-in (negative)", func() {
			req := withCookie(getRequest(pathFederationStart, map[string]string{"provider": "acme"}), auth.FlowCookieName+"="+flowIDFor(openClientID))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Code).To(Equal(oidc.OAuthErrInvalidRequest))
		})

		It("admits a provider the client did register", func() {
			resp := startFederation(flowIDFor(restrictedClientID), "acme")
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Headers["Location"]).To(HavePrefix(providerAuthorizeURL["acme"]))
		})

		It("admits every provider when the client registered none (absent means all, not none)", func() {
			for _, provider := range []string{"acme", "personal"} {
				resp := startFederation(flowIDFor(openClientID), provider)
				Expect(resp.StatusCode).To(Equal(http.StatusFound), provider)
			}
		})

		It("refuses a row whose password our page takes, so its pool's own page is never reached by this URL (negative)", func() {
			Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "pool", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito,
				Enabled: utils.Ptr(true), Issuer: "https://idp.example.com", ClientID: "broker-client",
				AuthorizeURL: "https://idp.example.com/authorize", TokenURL: "https://idp.example.com/token", JWKSURL: "https://idp.example.com/jwks",
			})).To(Succeed())

			resp := startFederation(flowIDFor(openClientID), "pool")
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(pageDataFrom(resp.Body).Error.Message).To(Equal("Unknown or unavailable provider."))
		})
	})

	Describe("on the login page — presentation, so it only has to agree with the gate", func() {
		It("describes only a provider the flow's client admits", func() {
			resp, err := handleAuthorizeRequest(context.Background(),
				withCookie(getRequest(pathLogin, nil), auth.FlowCookieName+"="+flowIDFor(restrictedClientID)))
			Expect(err).NotTo(HaveOccurred())
			Expect(providerNames(resp.Body)).To(Equal([]string{"acme"}))
		})

		It("describes them all for a client that restricts none", func() {
			resp, err := handleAuthorizeRequest(context.Background(),
				withCookie(getRequest(pathLogin, nil), auth.FlowCookieName+"="+flowIDFor(openClientID)))
			Expect(err).NotTo(HaveOccurred())
			Expect(providerNames(resp.Body)).To(Equal([]string{"acme", "personal"}))
		})
	})

	Describe("at the SSO shortcut — the second door into the same room", func() {
		// Without this the restriction holds for the first login of a browser and evaporates for every one after it.
		sessionVia := func(provider string) string {
			svc := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			cookieValue, _, err := svc.Establish(session.EstablishInput{
				UserID: "user-provider-gate", Provider: provider, AuthTime: time.Now().Unix(),
			})
			Expect(err).NotTo(HaveOccurred())
			return session.CookieName + "=" + cookieValue
		}

		It("does not shortcut on a session from a provider the client forbids (negative)", func() {
			req := withCookie(getRequest(pathAuthorize,
				validQuery(map[string]string{"client_id": restrictedClientID})), sessionVia("personal"))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(Equal(pathLogin),
				"single sign-on must not be a way around allowed_providers")
		})

		It("answers prompt=none with login_required rather than a code (negative)", func() {
			req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{
				"client_id": restrictedClientID, "prompt": "none",
			})), sessionVia("personal"))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(Equal(redirectURI + "?error=login_required&state=xyz"))
		})

		It("shortcuts normally on a session from a provider the client allows, and rolls the cookie", func() {
			req := withCookie(getRequest(pathAuthorize,
				validQuery(map[string]string{"client_id": restrictedClientID})), sessionVia("acme"))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
			// A front-channel event re-issues the same cookie value with a fresh Max-Age, so cookie_expires_at keeps step with expires_at.
			Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(session.CookieName + "="))
			Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age="))
		})
	})
})

var _ = Describe("consent for a client we do not ship", func() {
	const (
		thirdPartyID = "third-party-app"
		thirdPartyCB = "https://third.example/cb"
	)
	var flowsDB *auth_flows_db.AuthFlowsDB

	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		flowsDB = auth_flows_db.NewAuthFlowsDB(rmngCtx)
		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngCtx)
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: thirdPartyID, ClientName: "Third Party App", ClientType: oauth_clients_db.ClientTypePublic,
			LogoURI:      "data:image/png;base64,iVBORw0KGgo=",
			RedirectURIs: []string{thirdPartyCB}, Scopes: []string{"openid", "email"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
		Expect(user_details_db.NewUserDetailsDB(rmngCtx).CreateUserDetails(&user_details_db.UserDetailsEntry{
			UserID: "user-consent", Email: "person@example.com",
		})).To(Succeed())
	})

	thirdPartyQuery := func(overrides map[string]string) map[string]string {
		q := validQuery(map[string]string{"client_id": thirdPartyID, "redirect_uri": thirdPartyCB})
		for k, v := range overrides {
			q[k] = v
		}
		return q
	}

	// startSignIn runs /oauth2/authorize with no session and returns the flow it started, which nobody has signed in to.
	startSignIn := func(query map[string]string) string {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, query))
		Expect(err).NotTo(HaveOccurred())
		return flowIDFrom(resp.Headers["Set-Cookie"])
	}

	// browserSession is the session cookie of the browser that signed in, which its consent answers carry.
	var browserSession string
	BeforeEach(func() { browserSession = "" })

	// consentCookies is what the browser sends with an answer: the flow cookie and, when it has one, its session.
	consentCookies := func(flowID string) string {
		cookies := auth.FlowCookieName + "=" + flowID
		if browserSession != "" {
			cookies += "; " + browserSession
		}
		return cookies
	}

	signInWithSession := func(query map[string]string) (events.APIGatewayProxyResponse, string) {
		cookie := establishSession("user-consent", time.Now().Unix())
		browserSession = cookie
		resp, err := handleAuthorizeRequest(context.Background(), withCookie(getRequest(pathAuthorize, query), cookie))
		Expect(err).NotTo(HaveOccurred())
		for _, c := range resp.MultiValueHeaders["Set-Cookie"] {
			if strings.HasPrefix(c, auth.FlowCookieName+"=") {
				return resp, flowIDFrom(c)
			}
		}
		return resp, ""
	}

	heldSignIn := func() string {
		_, flowID := signInWithSession(thirdPartyQuery(nil))
		Expect(flowID).NotTo(BeEmpty(), "the consent screen finds the sign-in by its flow cookie")
		return flowID
	}

	// answer posts the person's answer as the page does, naming the flow it was drawn for unless the body already names one.
	answer := func(flowID, body string) (int, oidc.OAuthError) {
		fields := map[string]any{}
		Expect(json.Unmarshal([]byte(body), &fields)).To(Succeed())
		if _, named := fields["flow_id"]; !named {
			fields["flow_id"] = flowID
		}
		raw, err := json.Marshal(fields)
		Expect(err).NotTo(HaveOccurred())
		req := events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: pathConsent, Body: string(raw)}
		if flowID != "" {
			req = withCookie(req, consentCookies(flowID))
		}
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		var refused oidc.OAuthError
		Expect(json.Unmarshal([]byte(resp.Body), &refused)).To(Succeed())
		return resp.StatusCode, refused
	}

	allow := func(flowID string) *url.URL {
		resp, err := handleAuthorizeRequest(context.Background(), withCookie(events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost, Path: pathConsent, Body: `{"decision":"allow","flow_id":"` + flowID + `"}`,
		}, consentCookies(flowID)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"), "the redirect carries a code")
		var out auth.RedirectResponse
		Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
		loc, err := url.Parse(out.RedirectTo)
		Expect(err).NotTo(HaveOccurred())
		return loc
	}

	DescribeTable("decides at /oauth2/authorize whether the sign-in asks, and stores it on the flow",
		func(query map[string]string, skip bool) {
			flow, err := flowsDB.GetFlow(startSignIn(query))
			Expect(err).NotTo(HaveOccurred())
			Expect(flow.SkipConsent).To(Equal(skip))
		},
		Entry("a client we ship skips it", validQuery(nil), true),
		Entry("a client we do not ship asks", thirdPartyQuery(nil), false),
		Entry("a client we ship that sent prompt=consent asks", validQuery(map[string]string{"prompt": "consent"}), false),
	)

	DescribeTable("on an existing session, answers a client we ship at once and holds any other for consent",
		func(query map[string]string, location types.GomegaMatcher) {
			resp, _ := signInWithSession(query)
			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Headers["Location"]).To(location)
		},
		Entry("a client we do not ship", thirdPartyQuery(nil), Equal(pathConsent)),
		Entry("a client we ship", validQuery(nil), HavePrefix(redirectURI+"?code=")),
		Entry("a client we ship that sent prompt=consent", validQuery(map[string]string{"prompt": "consent"}), Equal(pathConsent)),
		Entry("prompt=none for a client we do not ship, which cannot show a screen (negative)",
			thirdPartyQuery(map[string]string{"prompt": "none"}), Equal(thirdPartyCB+"?error=consent_required&state=xyz")),
	)

	It("describes the app, the account and what is asked for on the consent screen", func() {
		flowID := heldSignIn()
		resp, err := handleAuthorizeRequest(context.Background(),
			withCookie(getRequest(pathConsent, nil), auth.FlowCookieName+"="+flowID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers).To(Equal(hardenedPageHeaders), "frame-ancestors 'none', so the Allow click cannot be borrowed")

		data := pageDataFrom(resp.Body)
		Expect(data.FlowID).To(Equal(flowID), "the page sends it back with the answer")
		Expect(data.Client).To(Equal(clientData{Name: "Third Party App", LogoURI: "data:image/png;base64,iVBORw0KGgo="}))
		Expect(data.Consent).To(Equal(&consentData{Account: "person@example.com", Scopes: []string{"openid", "email"}}))
	})

	DescribeTable("gives the consent screen the restart link, stage prefix included, so the page never guesses one",
		func(domain, restart string) {
			flowID := heldSignIn()
			req := withCookie(getRequest(pathConsent, nil), auth.FlowCookieName+"="+flowID)
			req.RequestContext = events.APIGatewayProxyRequestContext{DomainName: domain, Stage: "prod"}
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(pageDataFrom(resp.Body).RestartURL).To(Equal(restart))
		},
		Entry("the execute-api host", "abc123.execute-api.eu-west-1.amazonaws.com", "/prod/user/login/restart"),
		Entry("a custom domain", "user.api.example.com", "/user/login/restart"),
	)

	DescribeTable("stores the skip_consent StartAuthFlow returns, and the session shortcut follows the value it is handed",
		func(query map[string]string, stored bool, location types.GomegaMatcher) {
			ctx := context.Background()
			svc, err := auth.NewOAuthUserAuthService(ctx)
			Expect(err).NotTo(HaveOccurred())
			flowID, client, skipConsent, err := svc.StartAuthFlow(ctx, auth.AuthorizeRequest{
				ClientID: query["client_id"], RedirectURI: query["redirect_uri"], Scope: query["scope"], State: query["state"],
				CodeChallenge: query["code_challenge"], CodeChallengeMethod: query["code_challenge_method"],
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(skipConsent).To(Equal(stored))
			flow, err := flowsDB.GetFlow(flowID)
			Expect(err).NotTo(HaveOccurred())
			Expect(flow.SkipConsent).To(Equal(skipConsent))

			// The opposite of what the client implies: only a value the shortcut recomputed from the client would ignore it.
			cookie := establishSession("user-consent", time.Now().Unix())
			resp, done := sessionShortCircuit(ctx, AuthorizeSessionRequest{
				Request: withCookie(getRequest(pathAuthorize, query), cookie), Svc: svc, Query: query,
				FlowID: flowID, Client: client, SkipConsent: !skipConsent,
			})
			Expect(done).To(BeTrue())
			Expect(resp.Headers["Location"]).To(location)
		},
		Entry("a client we do not ship, handed true, is answered with a code", thirdPartyQuery(nil), false, HavePrefix(thirdPartyCB+"?code=")),
		Entry("a client we ship, handed false, is held for consent", validQuery(nil), true, Equal(pathConsent)),
	)

	It("names an app registered without a name by its client_id, so the person never approves a blank", func() {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "unnamed-app", ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{thirdPartyCB}, Scopes: []string{"openid", "email"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
		_, flowID := signInWithSession(thirdPartyQuery(map[string]string{"client_id": "unnamed-app"}))
		resp, err := handleAuthorizeRequest(context.Background(),
			withCookie(getRequest(pathConsent, nil), auth.FlowCookieName+"="+flowID))
		Expect(err).NotTo(HaveOccurred())
		Expect(pageDataFrom(resp.Body).Client).To(Equal(clientData{Name: "unnamed-app"}))
	})

	It("shows no consent screen for a sign-in nobody has completed (negative)", func() {
		page, err := handleAuthorizeRequest(context.Background(),
			withCookie(getRequest(pathConsent, nil), auth.FlowCookieName+"="+startSignIn(thirdPartyQuery(nil))))
		Expect(err).NotTo(HaveOccurred())
		Expect(page.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(pageDataFrom(page.Body).Consent).To(BeNil())
	})

	It("issues the code to the person who signed in once they allow the app", func() {
		loc := allow(heldSignIn())
		Expect(loc.Scheme + "://" + loc.Host + loc.Path).To(Equal(thirdPartyCB))
		Expect(loc.Query().Get("state")).To(Equal("xyz"))

		flow, err := flowsDB.GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.Subject).To(Equal("user-consent"))
	})

	It("sends the app access_denied and ends the sign-in when the person cancels", func() {
		flowID := heldSignIn()
		resp, err := handleAuthorizeRequest(context.Background(), withCookie(events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost, Path: pathConsent, Body: `{"decision":"deny","flow_id":"` + flowID + `"}`,
		}, auth.FlowCookieName+"="+flowID))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
		Expect(resp.Body).To(MatchJSON(`{"redirect_to":"` + thirdPartyCB + `?error=access_denied&state=xyz"}`))

		_, err = flowsDB.GetFlow(flowID)
		Expect(err).To(MatchError(auth_flows_db.ErrFlowNotFound))
	})

	// Single use and the subject binding are the auth-flows table's own conditions (auth_flows_db_test); these pin what the page is told.
	DescribeTable("refuses an answer that cannot be honoured (negative)",
		func(prepare func() string, body string, code string) {
			status, refused := answer(prepare(), body)
			Expect(status).To(Equal(http.StatusBadRequest))
			Expect(refused.Error).To(Equal(code))
		},
		Entry("a second Allow for one sign-in", func() string {
			flowID := heldSignIn()
			allow(flowID)
			return flowID
		}, `{"decision":"allow"}`, oidc.RMErrExpiredFlow),
		Entry("a second Cancel for one sign-in", func() string {
			flowID := heldSignIn()
			status, _ := answer(flowID, `{"decision":"deny"}`)
			Expect(status).To(Equal(http.StatusOK))
			return flowID
		}, `{"decision":"deny"}`, oidc.RMErrExpiredFlow),
		Entry("a Cancel after the Allow", func() string {
			flowID := heldSignIn()
			allow(flowID)
			return flowID
		}, `{"decision":"deny"}`, oidc.RMErrExpiredFlow),
		Entry("an Allow after the Cancel", func() string {
			flowID := heldSignIn()
			status, _ := answer(flowID, `{"decision":"deny"}`)
			Expect(status).To(Equal(http.StatusOK))
			return flowID
		}, `{"decision":"allow"}`, oidc.RMErrExpiredFlow),
		Entry("a Cancel for a sign-in nobody has completed", func() string {
			return startSignIn(thirdPartyQuery(nil))
		}, `{"decision":"deny"}`, oidc.RMErrExpiredFlow),
		Entry("no sign-in behind it", func() string { return "" }, `{"decision":"allow"}`, oidc.OAuthErrInvalidRequest),
		Entry("a sign-in nobody has completed, so consent cannot stand in for a login", func() string {
			return startSignIn(thirdPartyQuery(nil))
		}, `{"decision":"allow"}`, oidc.RMErrExpiredFlow),
		Entry("an expired sign-in", func() string {
			Expect(flowsDB.CreateFlow(&auth_flows_db.AuthFlow{
				FlowID: "flow-stale", ClientID: thirdPartyID, RedirectURI: thirdPartyCB, Subject: "user-consent",
				ExpiresOn: time.Now().Add(-time.Second).Unix(),
			})).To(Succeed())
			return "flow-stale"
		}, `{"decision":"allow"}`, oidc.RMErrExpiredFlow),
		Entry("neither allow nor deny", heldSignIn, `{"decision":"maybe"}`, oidc.OAuthErrInvalidRequest),
	)

	// A held sign-in outlives a sign-out by minutes, so the flow cookie alone must not be enough to allow it.
	DescribeTable("refuses an Allow from a browser that is not signed in as the person the sign-in is held for (negative)",
		func(change func()) {
			flowID := heldSignIn()
			change()

			status, refused := answer(flowID, `{"decision":"allow"}`)

			Expect(status).To(Equal(http.StatusBadRequest))
			Expect(refused.Error).To(Equal(oidc.RMErrExpiredFlow))
			flow, err := flowsDB.GetFlow(flowID)
			Expect(err).NotTo(HaveOccurred())
			Expect(flow.AwaitingConsent()).To(BeTrue(), "no code is issued")
		},
		Entry("no session cookie", func() { browserSession = "" }),
		Entry("another person's session", func() { browserSession = establishSession("user-someone-else", time.Now().Unix()) }),
		Entry("the session that signed in, ended since", func() {
			held := strings.TrimPrefix(browserSession, session.CookieName+"=")
			sessions := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			Expect(sessions.DeleteSessionBySID("user-consent", sessions.Peek(held).SID)).To(Succeed())
		}),
	)

	It("refuses an answer from a page drawn for another sign-in, which a later one in this browser replaced (negative)", func() {
		flowID := heldSignIn()
		status, refused := answer(flowID, `{"decision":"allow","flow_id":"flow-the-page-showed"}`)
		Expect(status).To(Equal(http.StatusBadRequest))
		Expect(refused.Error).To(Equal(oidc.RMErrFlowReplaced))

		flow, err := flowsDB.GetFlow(flowID)
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AwaitingConsent()).To(BeTrue(), "the sign-in the cookie names is not answered on the other page's say-so")
	})
})

var _ = Describe("the footer links the operator configures", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	setLinks := func(support, privacy, terms string) {
		for name, value := range map[string]string{envSupportURL: support, envPrivacyURL: privacy, envTermsURL: terms} {
			Expect(os.Setenv(name, value)).To(Succeed())
			DeferCleanup(os.Unsetenv, name)
		}
	}

	It("hands every page the support, privacy and terms URLs, the error page included", func() {
		setLinks("https://example.com/support", "https://example.com/privacy", "https://example.com/terms")
		want := &linksData{Support: "https://example.com/support", Privacy: "https://example.com/privacy", Terms: "https://example.com/terms"}

		login, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(pageDataFrom(login.Body).Links).To(Equal(want))

		failed, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"client_id": "nobody"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(pageDataFrom(failed.Body).Links).To(Equal(want))
	})

	DescribeTable("names only a configured https link, so the page never draws an empty one or puts script behind one",
		func(support, privacy, terms string, want *linksData) {
			setLinks(support, privacy, terms)
			resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
			Expect(err).NotTo(HaveOccurred())
			Expect(pageDataFrom(resp.Body).Links).To(Equal(want))
		},
		Entry("some unset", "", "https://example.com/privacy", "", &linksData{Privacy: "https://example.com/privacy"}),
		Entry("none https (negative)", "javascript:alert(1)", "http://example.com/privacy", "https://", nil),
	)
})

var _ = Describe("where /oauth2/authorize sends a browser with no session", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"}, RequirePKCE: utils.Ptr(true),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())
	})

	enable := func(entry identity_providers_db.ProviderEntry) {
		entry.Enabled = utils.Ptr(true)
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).CreateProvider(&entry)).To(Succeed())
	}
	authorize := func(extra map[string]string) string {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(extra)))
		Expect(err).NotTo(HaveOccurred())
		return resp.Headers["Location"]
	}

	It("shows our page for a lone password pool, handing it the client's login_hint and idp to prefill and preselect", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "cognito", Type: identity_providers_db.TypeOIDC, PasswordVerifier: identity_providers_db.PasswordVerifierCognito, ClientID: "app"})
		loc, err := url.Parse(authorize(map[string]string{"login_hint": "person@example.com", "idp": "cognito"}))
		Expect(err).NotTo(HaveOccurred())
		Expect(loc.Path).To(Equal(pathLogin))
		Expect(loc.Query()).To(Equal(url.Values{"login_hint": {"person@example.com"}, "idp": {"cognito"}}))
	})

	It("shows our page for a lone otp row without reading its authorize_url", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "otp", Type: identity_providers_db.TypeOTP, AuthorizeURL: "https://login.example.com/otp"})
		Expect(authorize(nil)).To(HavePrefix(pathLogin))
	})

	It("goes straight to a lone oidc provider, since there is nothing to choose", func() {
		enable(identity_providers_db.ProviderEntry{ProviderName: "google", Type: identity_providers_db.TypeOIDC})
		loc, err := url.Parse(authorize(nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(loc.Path).To(Equal(pathFederationStart))
		Expect(loc.Query().Get("provider")).To(Equal("google"))
		Expect(loc.Query().Get("flow_id")).NotTo(BeEmpty(), "the link names the sign-in authorize just started")
	})
})

// A sign-in held for consent answers redirect_to with auth.ConsentPath, which the page at /user/login follows as a relative link.
var _ = Describe("the consent link the sign-in page follows", func() {
	It("resolves from /user/login to the consent route, under any stage prefix", func() {
		for _, prefix := range []string{"", "/prod"} {
			base, err := url.Parse(prefix + pathLogin)
			Expect(err).NotTo(HaveOccurred())
			ref, err := url.Parse(auth.ConsentPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(base.ResolveReference(ref).Path).To(Equal(prefix + pathConsent))
		}
	})
})

// The sign-in app matches these exact strings, so a rename here is a break there; they are an extension block, never a code of RFC 6749, RFC 6750 or OIDC Core.
var _ = Describe("the sign-in page's error codes", func() {
	pageCodes := []string{
		oidc.RMErrUnconfirmedAccount, oidc.RMErrNewPasswordRequired, oidc.RMErrExpiredFlow,
		oidc.RMErrFlowReplaced, oidc.RMErrInvalidCredentials, oidc.RMErrProviderNotAllowed,
	}

	It("keeps the wire value the page matches for each", func() {
		Expect(pageCodes).To(Equal([]string{
			"unconfirmed_account", "new_password_required", "expired_flow",
			"flow_replaced", "invalid_credentials", "provider_not_allowed",
		}))
	})

	It("never reuses a code of the standards (negative)", func() {
		standards := []string{
			oidc.OAuthErrInvalidRequest, oidc.OAuthErrInvalidClient, oidc.OAuthErrInvalidGrant, oidc.OAuthErrUnauthorizedClient,
			oidc.OAuthErrUnsupportedGrantType, oidc.OAuthErrInvalidTarget, oidc.OAuthErrInvalidScope, oidc.OAuthErrServerError,
			oidc.OAuthErrUnsupportedResponseType, oidc.OAuthErrAccessDenied, oidc.OAuthErrTemporarilyUnavailable,
			oidc.OAuthErrLoginRequired, oidc.OAuthErrConsentRequired, oidc.OAuthErrInvalidToken, oidc.OAuthErrInsufficientScope,
		}
		for _, code := range pageCodes {
			Expect(standards).NotTo(ContainElement(code))
		}
	})
})

var _ = Describe("a deployment built without the sign-in bundle", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
		previous := renderShell
		renderShell = func([]byte) (string, error) { return "", errors.New("hosted-ui bundle missing") }
		DeferCleanup(func() { renderShell = previous })
	})

	DescribeTable("answers 500 on every page route, even where the page would have carried a 400 (negative)",
		func(path string) {
			resp, err := handleAuthorizeRequest(context.Background(), getRequest(path, nil))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(resp.Body).To(Equal("The sign-in page is not available."))
		},
		Entry("the sign-in page with no flow cookie", pathLogin),
		Entry("authorize with no client", pathAuthorize),
	)
})
