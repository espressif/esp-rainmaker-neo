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
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
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
		// Reset after establishing, so the profile below measures only the shortcut.
		backend.DBMock.ProfileReset()

		resp, err := handleAuthorizeRequest(context.Background(), withCookie(getRequest(pathAuthorize, validQuery(nil)), cookie))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(HavePrefix(redirectURI), "straight back to the client")
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
		Expect(resp.Headers["Location"]).To(ContainSubstring("state=xyz"))

		// One read and three writes:
		//
		//   1  GetItem    espuser-oauth-clients  the registry check inside StartAuthFlow
		//   2  PutItem    espuser-auth-flows     the LOGIN flow record
		//   3  UpdateItem espuser-sessions       Lookup: roll expiry and return the row (ALL_NEW)
		//   4  UpdateItem espuser-auth-flows     the flow moves to CODE and returns redirect_uri/state (ALL_NEW)
		//
		// The client row is read once and reused for allowed_providers; a second read means it stopped being threaded through.
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
	var backend *test_utils.EspUserBackend

	// A complete upstream: a token endpoint returning a signed id token whose auth_time is
	// hours old (the upstream reused its own session), and a JWKS URL publishing its key.
	// authTime 0 models an upstream whose token names no authentication time at all: no auth_time and no iat to fall back on.
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
				"exp": time.Now().Add(time.Hour).Unix(),
			}
			if authTime > 0 {
				claims["auth_time"], claims["iat"] = authTime, time.Now().Unix()
			}
			tok, err := jwtutil.SignRS256(claims, priv, kid)
			Expect(err).NotTo(HaveOccurred())
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": tok})
		})

		enabled := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: "up", Type: identity_providers_db.TypeOIDC, Enabled: &enabled,
				Issuer: srv.URL, ClientID: "broker-client",
				JWKSURL: srv.URL + "/jwks", TokenURL: srv.URL + "/token", AuthorizeURL: srv.URL + "/authorize",
			})).To(Succeed())
		return srv.URL
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		os.Setenv("ESPUSER_FEDERATION_CALLBACK_URL", "https://us.example/oauth2/federation/callback")
	})

	// runCallback drives the full brokered leg: flow row, upstream leg, then the callback carrying the given Cookie header ("" sends none).
	runCallback := func(cookie string) events.APIGatewayProxyResponse {
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(flowsDB.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_sso", ClientID: testClientID, RedirectURI: redirectURI,
			RequestedScope: []string{"openid"}, ExpiresOn: time.Now().Add(auth_flows_db.FlowTTL).Unix(),
		})).To(Succeed())

		leg, err := idp.NewUpstreamLeg("fl_sso", idp.StateHMACKey([]byte(backend.RefreshSecret)))
		Expect(err).NotTo(HaveOccurred())
		Expect(flowsDB.SetUpstreamLeg("fl_sso", "up", leg.State, leg.Nonce, leg.PKCEVerifier)).To(Succeed())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"},
			RequirePKCE:      utils.Ptr(false),
			AllowedResources: []string{testResource, testAccountResource}, DefaultResource: testResource,
		})).To(Succeed())

		req := getRequest(pathFederationCallback, map[string]string{"code": "upstream-code", "state": leg.State})
		if cookie != "" {
			req = withCookie(req, cookie)
		}
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
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
			resp := runCallback(cookie)
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
			Expect(resp.Headers).NotTo(HaveKey("Location"))
			Expect(backend.DBMock.ProfileGet().Accesses["espuser-auth-flows"].ReadCount).To(BeZero(), "a forged callback costs no flow read")
		},
		Entry("no flow cookie", ""),
		Entry("a flow cookie naming a different flow", auth.FlowCookieName+"=fl_someone_else"),
	)

	It("sets the __Host- session cookie and records the upstream's auth_time on the federation callback", func() {
		// Old enough to prove inheritance: auth_time is a fact carried from the upstream, never
		// the row's write time, and rolling retention on use must not move it.
		upstreamAuthTime := time.Now().Add(-30 * time.Minute).Unix()
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		setupUpstream(upstreamAuthTime, flowNonce)

		resp := runCallback(auth.FlowCookieName + "=fl_sso")
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))

		setCookie := resp.Headers["Set-Cookie"]
		Expect(setCookie).To(HavePrefix(session.CookieName + "="))
		Expect(setCookie).To(ContainSubstring("HttpOnly"))
		Expect(strings.ToLower(setCookie)).NotTo(ContainSubstring("domain"))

		cookieValue := strings.SplitN(strings.SplitN(setCookie, ";", 2)[0], "=", 2)[1]
		s := session.NewService(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).Lookup(cookieValue)
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

		resp := runCallback(auth.FlowCookieName + "=fl_sso")
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		loc, err := url.Parse(resp.Headers["Location"])
		Expect(err).NotTo(HaveOccurred())
		flow, err := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).GetFlowByCode(loc.Query().Get("code"))
		Expect(err).NotTo(HaveOccurred())
		Expect(flow.AuthTime).To(BeNumerically("~", before, 5), "with nothing upstream naming the sign-in, it happened on this request")
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
				ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic,
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
