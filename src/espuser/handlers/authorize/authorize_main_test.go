// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
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
	"github.com/espressif/esp-rainmaker-neo/src/espuser/idp"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	jwtgo "github.com/golang-jwt/jwt/v5"
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

var _ = Describe("GET /oauth2/authorize", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE: utils.Ptr(true), // public clients always require PKCE
		})).To(Succeed())
	})

	It("302s to the login page with a flow_id cookie for a valid request", func() {
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(nil)))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathLogin + "?providers="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(flowCookieName + "="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("HttpOnly"))

		// Two reads and one write, every time a browser starts a login:
		//
		//   1  GetItem espuser-oauth-clients      the registry check inside StartAuthFlow
		//   2  PutItem espuser-auth-flows         the LOGIN flow record (write)
		//   3  Scan    espuser-identity-providers loginRedirect, deciding chooser vs. straight-through
		//
		// Read 3 is a Scan, on the hot path of every unauthenticated login. It is pinned here so that
		// caching the provider list, or moving that decision, shows up as a changed number rather
		// than staying invisible as one more full table scan per login.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(2))
		Expect(writeCnt).To(Equal(1), "exactly one flow record, and nothing else written")
	})

	It("names the flow cookie with the __Host- prefix and the exact attribute set it requires", func() {
		// Without the prefix, a page on a sibling host can plant a flow id: the victim then
		// completes a login into the ATTACKER's flow and the authorization code is delivered
		// to the attacker's registered redirect_uri. The prefix is honoured by the browser
		// only for a Secure, Path=/, Domain-less cookie, which is what makes it unplantable
		// from anywhere but this exact origin.
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(nil)))
		Expect(err).NotTo(HaveOccurred())
		cookie := resp.Headers["Set-Cookie"]
		Expect(flowCookieName).To(HavePrefix("__Host-"))
		Expect(cookie).To(ContainSubstring("Path=/"))
		Expect(cookie).To(ContainSubstring("Secure"))
		Expect(cookie).To(ContainSubstring("SameSite=Lax"),
			"Strict would suppress the cookie on the upstream's cross-site return and break every federated login")
		Expect(strings.ToLower(cookie)).NotTo(ContainSubstring("domain="),
			"a Domain attribute voids the __Host- prefix, and the browser then drops the cookie entirely")
	})

	It("preserves the API Gateway stage prefix in the login redirect on the execute-api host", func() {
		// API Gateway strips the stage from request.Path but exposes it on RequestContext; the redirect must re-prepend it so the browser keeps /<stage>.
		req := getRequest(pathAuthorize, validQuery(nil))
		req.RequestContext.Stage = "prod"
		req.RequestContext.DomainName = "abc123.execute-api.eu-west-1.amazonaws.com"
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("/prod/oauth2/login?providers="))
	})

	It("omits the stage prefix in the login redirect on a custom domain (negative)", func() {
		// A custom domain's base-path mapping pins the stage server-side; the public URL has no /<stage> prefix, so re-prepending it would 403 (/prod/oauth2/login does not exist there).
		req := getRequest(pathAuthorize, validQuery(nil))
		req.RequestContext.Stage = "prod"
		req.RequestContext.DomainName = "user.api.example.com"
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("/oauth2/login?providers="))
	})

	It("renders the error page for an unknown client (negative, no redirect)", func() {
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize, validQuery(map[string]string{"client_id": "ghost"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"), "an unvalidated request must not redirect")
		Expect(resp.Headers["Content-Type"]).To(ContainSubstring("text/html"))

		// One registry GetItem and nothing more. This is what keeps an unauthenticated caller from
		// filling the flows table: a rejected authorize costs one read and no write, whatever
		// client_id it names.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(1))
		Expect(writeCnt).To(BeZero(), "a rejected request must not create a flow record")
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

	It("redirects the client with invalid_target for an unregistered resource (negative)", func() {
		// Redirects rather than rendering, and the distinction is not arbitrary: by this
		// point redirect_uri has been checked against the registry, so the error can safely
		// go back to the client (RFC 6749 s4.1.2.1). The two cases above cannot, because
		// what failed was the client or the URI itself.
		//
		// It is caught here at all -- rather than at the code exchange -- so nobody
		// completes a whole login, provider round trip included, to be told the request was
		// never valid.
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathAuthorize,
			validQuery(map[string]string{"resource": "https://api.someone-else.example.com"})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("error=invalid_target"))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="), "no code is issued")
	})

	It("rejects two resource values without rendering a redirect (negative)", func() {
		// One resource, one aud. Two would be one token good at two APIs.
		req := getRequest(pathAuthorize, validQuery(nil))
		req.MultiValueQueryStringParameters = map[string][]string{
			"resource": {"https://a.example.com", "https://b.example.com"},
		}
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
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
		// A provider with no logo of its own still gets a mark, drawn inline. Inline rather
		// than an <img> so it inherits currentColor and works on either theme, which is also
		// why the CSP needs no remote image origin.
		Expect(resp.Body).To(ContainSubstring("<svg viewBox=\"0 0 24 24\""))
		Expect(resp.Headers["Content-Security-Policy"]).To(ContainSubstring("default-src 'none'"))
		Expect(resp.Headers["Content-Security-Policy"]).NotTo(ContainSubstring("https://"))
	})

	It("renders the Espressif login page", func() {
		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Body).To(ContainSubstring("Sign in · Espressif"))
		Expect(resp.Body).To(ContainSubstring(`<div class="brand">`))
		Expect(resp.Body).To(ContainSubstring("#e7352c"))
		// Still a complete page, not a placeholder.
		Expect(resp.Body).To(ContainSubstring("<form"))
	})

	It("keeps a display name that looks like markup out of the document structure", func() {
		db := identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		enabled := true
		Expect(db.CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "evil", Type: "oidc", Enabled: &enabled,
			DisplayName: `</a><script>alert(1)</script>`,
		})).To(Succeed())

		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathLogin, nil))
		Expect(err).NotTo(HaveOccurred())
		// The label is data. Only the page's own nonced script may execute.
		Expect(resp.Body).NotTo(ContainSubstring("<script>alert(1)</script>"))
		Expect(resp.Body).To(ContainSubstring("alert(1)"))
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

var _ = Describe("GET /oauth2/authorize with a session (SSO)", func() {
	var backend *test_utils.EspUserBackend

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
			RequirePKCE: utils.Ptr(true),
		})).To(Succeed())
	})

	It("falls through to the login page for a session cookie that resolves to no live session (negative)", func() {
		// A bogus or stale session cookie must be ignored -- no shortcut, no error -- and the
		// request must proceed as an ordinary login, flow cookie and all.
		req := withCookie(getRequest(pathAuthorize, validQuery(nil)), session.CookieName+"=whatever")
		backend.DBMock.ProfileReset()
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(pathLogin + "?providers="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(flowCookieName + "="))

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
		Expect(resp.Headers["Location"]).To(HavePrefix(redirectURI))
		Expect(resp.Headers["Location"]).To(ContainSubstring("error=login_required"))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="))

		// One registry read and the flow write, and nothing else -- no sessions read (there is no
		// cookie to look up) and no providers Scan, because login_required returns before
		// loginRedirect. prompt=none is the request a client can send on every page load, so it is
		// the one whose cost most needs pinning.
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(1))
		Expect(writeCnt).To(Equal(1))
		Expect(profile.Accesses).NotTo(HaveKey("espuser-sessions"))
	})

	It("prompt=login re-authenticates even with a live session — always honoured", func() {
		cookie := establishSession("user-sso-4", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"prompt": "login"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(Equal(pathLogin+"?providers="), "prompt=login must never be shortcut past")
	})

	It("max_age=0 rejects a session minutes old and re-authenticates (negative)", func() {
		cookie := establishSession("user-sso-5", time.Now().Add(-time.Minute).Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"max_age": "0"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(Equal(pathLogin + "?providers="))
	})

	It("max_age=0 rejects a session created this very second (negative)", func() {
		// The boundary the minutes-old spec above cannot reach. max_age=0 is what a relying
		// party sends to force a fresh authentication; a session whose auth_time is NOW must
		// not satisfy it, or the demand is silently answered from the session it was meant to
		// bypass -- and only for logins less than a second old, which is exactly the kind of
		// window that survives every test written with a comfortable margin.
		cookie := establishSession("user-sso-maxage-now", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"max_age": "0"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(Equal(pathLogin + "?providers="))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="))
	})

	It("max_age accepts a session younger than the bound", func() {
		cookie := establishSession("user-sso-6", time.Now().Unix())
		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"max_age": "300"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
	})

	It("id_token_hint naming a different subject drops the session rather than switching users (negative)", func() {
		cookie := establishSession("user-sso-7", time.Now().Unix())

		hint, err := jwtutil.SignRS256(jwtgo.MapClaims{
			"iss": os.Getenv("USER_ISSUER"), "sub": "someone-else", "aud": testClientID,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}, backend.SigningKey, jwtutil.RSAThumbprint(&backend.SigningKey.PublicKey))
		Expect(err).NotTo(HaveOccurred())

		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"id_token_hint": hint, "prompt": "none"})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(ContainSubstring("error=login_required"))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="))
	})

	It("id_token_hint naming the session's subject passes", func() {
		cookie := establishSession("user-sso-8", time.Now().Unix())

		hint, err := jwtutil.SignRS256(jwtgo.MapClaims{
			"iss": os.Getenv("USER_ISSUER"), "sub": "user-sso-8", "aud": testClientID,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}, backend.SigningKey, jwtutil.RSAThumbprint(&backend.SigningKey.PublicKey))
		Expect(err).NotTo(HaveOccurred())

		req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{"id_token_hint": hint})), cookie)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
	})

})

var _ = Describe("GET /oauth2/federation/callback establishes the session", func() {
	var backend *test_utils.EspUserBackend

	// A complete upstream: a token endpoint returning a signed id token whose auth_time is
	// hours old (the upstream reused its own session), and a JWKS URL publishing its key.
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
			tok, err := jwtutil.SignRS256(jwtgo.MapClaims{
				"iss": srv.URL, "aud": "broker-client", "sub": "up-sub-1", "nonce": nonceFor(),
				"email": "sso@example.com", "email_verified": true,
				"auth_time": authTime,
				"iat":       time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			}, priv, kid)
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

	// runCallback drives the full brokered leg: flow row, upstream leg, then the callback.
	runCallback := func() events.APIGatewayProxyResponse {
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(flowsDB.CreateFlow(&auth_flows_db.AuthFlow{
			FlowID: "fl_sso", ClientID: testClientID, RedirectURI: redirectURI,
			RequestedScope: []string{"openid"}, ExpiresOn: time.Now().Add(auth.FlowTTL).Unix(),
		})).To(Succeed())

		leg, err := idp.NewUpstreamLeg("fl_sso", idp.StateHMACKey([]byte(backend.RefreshSecret)))
		Expect(err).NotTo(HaveOccurred())
		Expect(flowsDB.SetUpstreamLeg("fl_sso", "up", leg.State, leg.Nonce, leg.PKCEVerifier)).To(Succeed())

		clientsDB := oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		Expect(clientsDB.CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: testClientID, ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{redirectURI}, Scopes: []string{"openid"},
			RequirePKCE: utils.Ptr(false),
		})).To(Succeed())

		resp, err := handleAuthorizeRequest(context.Background(), getRequest(pathFederationCallback,
			map[string]string{"code": "upstream-code", "state": leg.State}))
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	It("sets the __Host- session cookie and records the upstream's auth_time on the federation callback", func() {
		// Old enough to prove inheritance: auth_time is a fact carried from the upstream, never
		// the row's write time, and rolling retention on use must not move it.
		upstreamAuthTime := time.Now().Add(-30 * time.Minute).Unix()
		var currentNonce string
		flowsDB := auth_flows_db.NewAuthFlowsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		setupUpstream(upstreamAuthTime, func() string {
			flow, err := flowsDB.GetFlow("fl_sso")
			Expect(err).NotTo(HaveOccurred())
			currentNonce = flow.UpstreamNonce
			return currentNonce
		})

		resp := runCallback()
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
	})
})

// allowed_providers decides which identity providers a client may be entered through. It is
// checked in three places because there are three ways in, and a control that holds at one
// door is not a control: the login page draws the buttons, /oauth2/federation/start is
// reachable by typing a provider name into the address bar, and the SSO shortcut skips both.
var _ = Describe("allowed_providers is enforced, not decorative", func() {
	const (
		restrictedClientID = "restricted-client"
		openClientID       = "open-client"
	)

	var backend *test_utils.EspUserBackend

	// Where each provider's fake upstream lives, so the positive controls can assert the
	// browser was actually sent there rather than merely not refused.
	providerAuthorizeURL := map[string]string{}

	// registerClient puts one client in the registry with the given provider restriction.
	registerClient := func(clientID string, allowed []string) {
		Expect(oauth_clients_db.NewOAuthClientsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil)).
			CreateClient(&oauth_clients_db.OAuthClientEntry{
				ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic,
				RedirectURIs: []string{redirectURI}, Scopes: []string{"openid", "email"},
				AllowedProviders: allowed, RequirePKCE: utils.Ptr(true),
			})).To(Succeed())
	}

	// flowIDFor drives a real /oauth2/authorize and returns the flow id it planted, so the
	// specs below reach federation/start and the login page exactly as a browser would.
	flowIDFor := func(clientID string) string {
		resp, err := handleAuthorizeRequest(context.Background(),
			getRequest(pathAuthorize, validQuery(map[string]string{"client_id": clientID})))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		cookie := resp.Headers["Set-Cookie"]
		Expect(cookie).To(HavePrefix(flowCookieName + "="))
		return strings.SplitN(strings.TrimPrefix(cookie, flowCookieName+"="), ";", 2)[0]
	}

	startFederation := func(flowID, provider string) events.APIGatewayProxyResponse {
		req := withCookie(getRequest(pathFederationStart, map[string]string{"provider": provider}),
			flowCookieName+"="+flowID)
		resp, err := handleAuthorizeRequest(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		os.Setenv("ESPUSER_FEDERATION_CALLBACK_URL", "https://us.example/oauth2/federation/callback")

		// Both providers are fully usable, so a refusal below can only be the restriction and
		// never an unreachable upstream -- the registry fetches a provider's JWKS before it
		// will hand one back, and an unfetchable row fails with the SAME message this gate
		// returns.
		enabled := true
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
				ProviderName: name, Type: identity_providers_db.TypeOIDC, Enabled: &enabled,
				DisplayName: name, Issuer: srv.URL, ClientID: "broker-client",
				JWKSURL:      srv.URL + "/jwks",
				TokenURL:     srv.URL + "/token",
				AuthorizeURL: srv.URL + "/authorize",
			})).To(Succeed())
		}
		registerClient(restrictedClientID, []string{"acme"})
		registerClient(openClientID, nil)
	})

	Describe("at /oauth2/federation/start — the gate, because this URL can be typed", func() {
		It("refuses a provider the client did not register (negative)", func() {
			resp := startFederation(flowIDFor(restrictedClientID), "personal")
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(ContainSubstring("Unknown or unavailable provider"),
				"and it must read like an unknown provider: whether a client restricts one is not the caller's business")
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
		It("draws a button only for a provider the flow's client admits", func() {
			req := withCookie(getRequest(pathLogin, nil), flowCookieName+"="+flowIDFor(restrictedClientID))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body).To(ContainSubstring("federation/start?provider=acme"))
			Expect(resp.Body).NotTo(ContainSubstring("federation/start?provider=personal"),
				"a button that would be refused on click is a dead end wearing a login's clothes")
		})

		It("hands the page the client's buttons in the login redirect", func() {
			for clientID, want := range map[string]string{restrictedClientID: "acme", openClientID: "acme%2Cpersonal"} {
				resp, err := handleAuthorizeRequest(context.Background(),
					getRequest(pathAuthorize, validQuery(map[string]string{"client_id": clientID})))
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.Headers["Location"]).To(Equal(pathLogin+"?providers="+want), clientID)
			}
		})

		It("draws the listed buttons without reading the flow or the client", func() {
			req := withCookie(getRequest(pathLogin, map[string]string{providersParam: "acme"}),
				flowCookieName+"="+flowIDFor(openClientID))
			backend.DBMock.ProfileReset()
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Body).To(ContainSubstring("federation/start?provider=acme"))
			Expect(resp.Body).NotTo(ContainSubstring("federation/start?provider=personal"))
			profile := backend.DBMock.ProfileGet()
			readCnt, _ := profile.TotalCounts()
			Expect(readCnt).To(Equal(1), "only the provider scan for labels and logos")
			Expect(profile.Accesses["espuser-auth-flows"].ReadCount).To(BeZero())
			Expect(profile.Accesses["espuser-oauth-clients"].ReadCount).To(BeZero())
		})

		It("draws them all for a client that restricts none", func() {
			req := withCookie(getRequest(pathLogin, nil), flowCookieName+"="+flowIDFor(openClientID))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			for _, provider := range []string{"acme", "personal"} {
				Expect(resp.Body).To(ContainSubstring("federation/start?provider=" + provider))
			}
		})
	})

	Describe("at the SSO shortcut — the second door into the same room", func() {
		// A session established through one provider must not open a client that admits only
		// another. Without this the restriction holds for the first login of a browser and
		// evaporates for every one after it.
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
			Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="),
				"single sign-on must not be a way around allowed_providers")
			Expect(resp.Headers["Location"]).To(Equal(pathLogin + "?providers=acme"))
		})

		It("answers prompt=none with login_required rather than a code (negative)", func() {
			req := withCookie(getRequest(pathAuthorize, validQuery(map[string]string{
				"client_id": restrictedClientID, "prompt": "none",
			})), sessionVia("personal"))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(ContainSubstring("error=login_required"))
			Expect(resp.Headers["Location"]).NotTo(ContainSubstring("code="))
		})

		It("shortcuts normally on a session from a provider the client allows, and rolls the cookie", func() {
			req := withCookie(getRequest(pathAuthorize,
				validQuery(map[string]string{"client_id": restrictedClientID})), sessionVia("acme"))
			resp, err := handleAuthorizeRequest(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Headers["Location"]).To(ContainSubstring("code="))
			// The shortcut is a front-channel event: it re-issues the session cookie (rolled)
			// so cookie_expires_at keeps step with expires_at, not only at the federation
			// callback. Same opaque value -- the server never learns a new one.
			Expect(resp.Headers["Set-Cookie"]).To(HavePrefix(session.CookieName + "="))
			Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age="))
		})
	})
})
