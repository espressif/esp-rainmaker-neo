// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuthorizeHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Authorize Endpoint Suite")
}

const (
	testClientID = "user-pool-client"
	redirectURI  = "com.example://callback"
)

func getRequest(path string, query map[string]string) events.APIGatewayProxyRequest {
	return events.APIGatewayProxyRequest{HTTPMethod: http.MethodGet, Path: path, QueryStringParameters: query}
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

var _ = Describe("GET /oauth2/authorize", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE: utils.Ptr(true), // public clients always require PKCE
		})).To(Succeed())
	})

	It("302s to the login page with a flow_id cookie for a valid request", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathLogin))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(flowCookieName + "="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("HttpOnly"))
	})

	It("preserves the API Gateway stage prefix in the login redirect on the execute-api host", func() {
		// API Gateway strips the stage from request.Path but exposes it on RequestContext; the redirect must re-prepend it so the browser keeps /<stage>.
		req := getRequest(pathAuthorize, validQuery(nil))
		req.RequestContext.Stage = "prod"
		req.RequestContext.DomainName = "abc123.execute-api.eu-west-1.amazonaws.com"
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("/prod/oauth2/login"))
	})

	It("omits the stage prefix in the login redirect on a custom domain (negative)", func() {
		// A custom domain's base-path mapping pins the stage server-side; the public URL has no /<stage> prefix, so re-prepending it would 403 (/prod/oauth2/login does not exist there).
		req := getRequest(pathAuthorize, validQuery(nil))
		req.RequestContext.Stage = "prod"
		req.RequestContext.DomainName = "user.api.example.com"
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("/oauth2/login"))
	})

	It("renders the error page for an unknown client (negative, no redirect)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"client_id": "ghost"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"), "an unvalidated request must not redirect")
		Expect(resp.Headers["Content-Type"]).To(ContainSubstring("text/html"))
	})

	It("renders the error page for an unregistered redirect_uri (negative, open-redirect defense)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"redirect_uri": "com.evil://cb"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
	})

	// redirect_uri must match a registered value EXACTLY (RFC 9700 §4.1, OWASP). These are the
	// classic bypass vectors that defeat prefix/substring/domain-only matching; each must fail
	// closed to the error page and never redirect (no code leaks to an attacker endpoint). The
	// base registered value is "https://app.example.com/cb".
	DescribeTable("rejects redirect_uri manipulation vectors (open-redirect defense)",
		func(evil string) {
			// Register an https client whose exact callback the vectors try to subvert.
			clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
			Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: "https_client", ClientType: oauth_clients_db.ClientTypePublic,
				RedirectURIs: []string{"https://app.example.com/cb"}, Scopes: []string{"openid", "email"},
				RequirePKCE: utils.Ptr(true),
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
	)

	It("rejects a missing PKCE challenge for a require_pkce client (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"code_challenge": ""})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("allows a confidential client with require_pkce=false to omit the challenge", func() {
		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "conf_nopkce", ClientType: oauth_clients_db.ClientTypeConfidential,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"},
			RequirePKCE: utils.Ptr(false),
		})).To(Succeed())
		q := validQuery(map[string]string{"client_id": "conf_nopkce", "scope": "openid", "code_challenge": "", "code_challenge_method": ""})
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, q))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound), "no-PKCE confidential client may start a flow without a challenge")
	})

	It("rejects a non-S256 PKCE method with the error page (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"code_challenge_method": "plain"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("rejects a non-code response_type (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"response_type": "token"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	It("redirects the client with invalid_scope when a scope is outside the client's allowed set (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"scope": "openid admin"})))
		Expect(err).NotTo(HaveOccurred())
		// redirect_uri is validated by this point, so the error is safe to redirect.
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("error=invalid_scope"))
		Expect(resp.Headers["Location"]).To(ContainSubstring("state=xyz"))
	})
})

var _ = Describe("GET /oauth2/login", func() {
	BeforeEach(func() {
		test_utils.SetupEspUserBackend(context.Background())
	})

	It("serves the login HTML with no-store and injects the flow id from the cookie", func() {
		req := getRequest(pathLogin, nil)
		req.Headers = map[string]string{"Cookie": flowCookieName + "=fl_abc123; other=x"}
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers["Content-Type"]).To(ContainSubstring("text/html"))
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
		Expect(strings.ToLower(resp.Body)).To(ContainSubstring("<form"))
		// The HttpOnly flow id is injected into the page JS (not read via document.cookie).
		Expect(resp.Body).To(ContainSubstring(`var flowId = "fl_abc123";`))
		// The escaped CSS width renders as a literal percent, not a format artifact.
		Expect(resp.Body).To(ContainSubstring("width: 100%;"))
	})

	It("renders a button per federated provider so the user can pick one", func() {
		db := identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		enabled := true
		for name, display := range map[string]string{"cognito": "Espressif Account", "acme": "Acme SSO"} {
			Expect(db.CreateProvider(&identity_providers_db.ProviderEntry{
				ProviderName: name, Type: "oidc", DisplayName: display, Enabled: &enabled,
			})).To(Succeed())
		}

		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		for name, display := range map[string]string{"cognito": "Espressif Account", "acme": "Acme SSO"} {
			Expect(resp.Body).To(ContainSubstring("federation/start?provider=" + name))
			Expect(resp.Body).To(ContainSubstring(display))
		}
		// The logo is inlined, so the CSP must permit data: images and nothing remote.
		Expect(resp.Body).To(ContainSubstring("src=\"data:image/svg+xml;base64,"))
		Expect(resp.Headers["Content-Security-Policy"]).To(ContainSubstring("img-src data:"))
	})

	It("renders no provider buttons when only the OTP provider is enabled (its form is the page)", func() {
		db := identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		enabled := true
		Expect(db.CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "otp", Type: "otp", DisplayName: "Email code", Enabled: &enabled,
		})).To(Succeed())

		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body).NotTo(ContainSubstring("federation/start?provider="))
		Expect(resp.Body).To(ContainSubstring("identifier-form"))
	})

	It("injects an empty flow id when the cookie is absent (page shows the expired message)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Body).To(ContainSubstring(`var flowId = "";`))
	})

	// Browser-layer hardening (OWASP): the login page carries a nonce-based CSP (so an injected
	// script without the nonce can't run), Referrer-Policy: no-referrer (so the auth code in a
	// downstream URL can't leak via Referer), anti-framing, and nosniff.
	It("sets a nonce-based CSP whose nonce matches the inline script, plus Referrer-Policy and anti-framing", func() {
		req := getRequest(pathLogin, nil)
		req.Headers = map[string]string{"Cookie": flowCookieName + "=fl_abc123"}
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"))
		Expect(resp.Headers["X-Frame-Options"]).To(Equal("DENY"))
		Expect(resp.Headers["X-Content-Type-Options"]).To(Equal("nosniff"))

		csp := resp.Headers["Content-Security-Policy"]
		Expect(csp).To(ContainSubstring("default-src 'none'"))
		Expect(csp).To(ContainSubstring("frame-ancestors 'none'"))
		Expect(csp).To(ContainSubstring("script-src 'nonce-"))
		// The CSP nonce must equal the nonce on the one inline <script>, or the browser blocks it.
		start := strings.Index(csp, "'nonce-") + len("'nonce-")
		nonce := csp[start : start+strings.Index(csp[start:], "'")]
		Expect(nonce).NotTo(BeEmpty())
		Expect(resp.Body).To(ContainSubstring(`<script nonce="` + nonce + `">`))
	})

	It("gives a fresh CSP nonce on each login render (not a fixed value)", func() {
		req := getRequest(pathLogin, nil)
		req.Headers = map[string]string{"Cookie": flowCookieName + "=fl_abc123"}
		r1, _ := handleAuthorizeRequest(context.Background(), req)
		r2, _ := handleAuthorizeRequest(context.Background(), req)
		Expect(r1.Headers["Content-Security-Policy"]).NotTo(Equal(r2.Headers["Content-Security-Policy"]))
	})

	It("hardens the error page with CSP, Referrer-Policy, and anti-framing", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"client_id": "ghost"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"))
		Expect(resp.Headers["X-Frame-Options"]).To(Equal("DENY"))
		Expect(resp.Headers["Content-Security-Policy"]).To(ContainSubstring("default-src 'none'"))
	})
})

var _ = Describe("routing", func() {
	It("rejects a non-GET method (negative)", func() {
		resp, err := handleAuthorizeRequest(context.Background(), events.APIGatewayProxyRequest{HTTPMethod: http.MethodPost, Path: pathAuthorize})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})
})
