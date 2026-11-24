// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"

	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	dashboardClient = "esp-account-dashboard"
	registeredExit  = "https://accounts.espressif.com/signed-out"
)

// establish creates a real session through the service and returns the cookie a browser
// holding it would send. Real rather than hand-written, because logout's whole first step is
// resolving a cookie the way a login produced it.
func establish(userID string) (cookieHeader, sid string) {
	svc := session.NewService(rctx())
	cookieValue, newSID, err := svc.Establish(session.EstablishInput{
		UserID: userID, Provider: "rainmaker-public", AuthTime: time.Now().Unix(),
		UserAgent: uaChromeMac,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(cookieValue).NotTo(BeEmpty())
	return session.CookieName + "=" + cookieValue, newSID
}

func registerClient(postLogout ...string) {
	Expect(oauth_clients_db.NewOAuthClientsDB(rctx()).CreateClient(&oauth_clients_db.OAuthClientEntry{
		ClientID: dashboardClient, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
		RedirectURIs: []string{"https://accounts.espressif.com/callback"},
		Scopes:       []string{"openid", scope.Sessions}, RequirePKCE: utils.Ptr(true),
		PostLogoutRedirectURIs: postLogout,
	})).To(Succeed())
}

// expiredIDToken mints one of OUR OWN id tokens with an exp in the past -- correctly signed,
// correct issuer, simply old. This is what a browser tab open for more than an hour sends.
func expiredIDToken(userID, sid string) string {
	claims := jwt.MapClaims{
		"iss": backend.Issuer, "sub": userID, "aud": dashboardClient,
		"token_use": jwtutil.TokenUseID, "sid": sid,
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"exp": time.Now().Add(-time.Hour).Unix(),
	}
	token, err := jwtutil.SignRS256(claims, backend.SigningKey, oidc.SigningKeyID)
	Expect(err).NotTo(HaveOccurred())
	return token
}

// federationLogoutCallback drives the hand-back the provider performs, carrying whatever memo the browser
// holds. No query parameters: the provider matches our return URL exactly, so nothing can be
// appended to it -- which is precisely why the destination travels in a cookie.
func federationLogoutCallback(cookie string) events.APIGatewayProxyResponse {
	req := events.APIGatewayProxyRequest{HTTPMethod: http.MethodGet, Path: pathFederationLogoutCallback, Resource: pathFederationLogoutCallback}
	if cookie != "" {
		req.Headers = map[string]string{"Cookie": cookie}
	}
	resp, err := handleRequest(ctx(), req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// hintFor mints the id token an app signed in under sid holds: what a well-behaved app sends as id_token_hint.
func hintFor(userID, sid string) string {
	hint, err := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID).IDToken(userID, dashboardClient, "", 0, jwtutil.Contact{}, sid)
	Expect(err).NotTo(HaveOccurred())
	return hint
}

// csrfTokenFor is the token the sign-out page hands the browser holding cookie.
func csrfTokenFor(cookie string) string {
	return session.LogoutCSRFToken(strings.TrimPrefix(cookie, session.CookieName+"="))
}

// confirm drives the person's yes on the sign-out page: the page posts the parameters it was opened with to POST /user/logout, with the token it was given.
func confirm(cookie string, params map[string]string, csrfToken string) events.APIGatewayProxyResponse {
	body := map[string]string{"csrf_token": csrfToken}
	for k, v := range params {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	Expect(err).NotTo(HaveOccurred())
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodPost, Path: pathUserLogout, Resource: pathUserLogout,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(raw),
	}
	if cookie != "" {
		req.Headers["Cookie"] = cookie
	}
	resp, err := handleRequest(ctx(), req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

func urlValues(m map[string]string) url.Values {
	v := url.Values{}
	for k, val := range m {
		v.Set(k, val)
	}
	return v
}

func redirectTo(resp events.APIGatewayProxyResponse) string {
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
	return out.RedirectTo
}

func sessionsOf(userID, sid string) []SessionView {
	return listBody(call(http.MethodGet, pathSessions, tokenFor(userID, scope.Sessions, sid))).Sessions
}

func setCookies(resp events.APIGatewayProxyResponse) []string {
	if len(resp.MultiValueHeaders["Set-Cookie"]) > 0 {
		return resp.MultiValueHeaders["Set-Cookie"]
	}
	if v := resp.Headers["Set-Cookie"]; v != "" {
		return []string{v}
	}
	return nil
}

// seedUpstream registers a provider that names an end-session endpoint, in Cognito's
// shape: client_id baked into the stored URL, and the return parameter named logout_uri
// rather than the OIDC-standard one.
func seedUpstream() {
	enabled := true
	Expect(identity_providers_db.NewIdentityProvidersDB(rctx()).CreateProvider(&identity_providers_db.ProviderEntry{
		ProviderName: "rainmaker-public", Type: identity_providers_db.TypeOIDC, Enabled: &enabled,
		EndSessionURL:           "https://pool.auth.ap-south-1.amazoncognito.com/logout?client_id=abc123",
		EndSessionRedirectParam: "logout_uri",
	})).To(Succeed())
}

// withDoneURL sets our own return URL for a spec, the value CDK supplies in production.
func withDoneURL() {
	os.Setenv("ESPUSER_FEDERATION_LOGOUT_CALLBACK_URL", "https://issuer.test/oauth2/federation/logout/callback")
	DeferCleanup(func() { os.Unsetenv("ESPUSER_FEDERATION_LOGOUT_CALLBACK_URL") })
}

func logout(cookie string, query map[string]string) events.APIGatewayProxyResponse {
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodGet, Path: pathLogout, Resource: pathLogout, QueryStringParameters: query,
	}
	if cookie != "" {
		req.Headers = map[string]string{"Cookie": cookie}
	}
	resp, err := handleRequest(ctx(), req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

var _ = Describe("GET /oauth2/logout", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
	})

	It("ends the session and every product opened under it when the hint was minted under this session", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)
		putFamily(me, dashboardClient, "fam_3", sid)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
		// Max-Age=0 is what actually removes it from the browser.
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(session.CookieName + "="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age=0"))

		Expect(sessionsOf(me, sid)).To(BeEmpty(), "the session row must be gone")
		Expect(familyClientIDs(me)).To(BeEmpty(), "both families must be gone, not orphaned")
	})

	It("answers a session it cannot read with 500 and ends nothing, never telling the browser it is signed out (negative)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, dashboardClient, "fam_2", sid)
		backend.DBMock.NextGetItemError = errors.New("throttled")

		resp := logout(cookie, map[string]string{})

		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
		Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
		Expect(sessionsOf(me, sid)).NotTo(BeEmpty())
		Expect(familyClientIDs(me)).To(ConsistOf(dashboardClient))
	})

	It("answers 500 and never returns the browser to the app when the session's logins cannot be ended, so the app is not told it signed out (negative)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, dashboardClient, "fam_2", sid)
		awscommon.SetDynamoDBClient(&mock.FailingDeleteItem{DynamoDBMock: backend.DBMock, TableName: "espuser-refresh-tokens"})

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		})
		awscommon.SetDynamoDBClient(backend.DBMock)

		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
		Expect(resp.Headers).NotTo(HaveKey("Set-Cookie"))
		Expect(sessionsOf(me, sid)).NotTo(BeEmpty())
		Expect(familyClientIDs(me)).To(ConsistOf(dashboardClient))
	})

	It("leaves an offline_access grant working while ending the session's own families (OIDC Back-Channel Logout 1.0 s2.7)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_session", sid)
		putOfflineFamily(me, "voice-assistant", "fam_offline")

		resp := logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hintFor(me, sid)})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))

		Expect(sessionsOf(me, sid)).To(BeEmpty())
		Expect(familyClientIDs(me)).To(ConsistOf("voice-assistant"), "only the offline grant survives the sign-out")
	})

	It("ends everything when that hint has expired, which is what a tab left open past the token's hour sends", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": expiredIDToken(me, sid)})

		Expect(sessionsOf(me, sid)).To(BeEmpty(), "an expired hint is still a valid hint")
		Expect(familyClientIDs(me)).To(BeEmpty())
	})

	It("echoes the RP's state on the post-logout redirect (RP-Initiated Logout 1.0 §2)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "state": "xyz123", "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(HavePrefix(registeredExit))
		Expect(resp.Headers["Location"]).To(ContainSubstring("state=xyz123"))
	})

	It("takes the client from id_token_hint when client_id is absent (RP-Initiated Logout 1.0 §2)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		resp := logout(cookie, map[string]string{"id_token_hint": hintFor(me, sid), "post_logout_redirect_uri": registeredExit})
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
	})

	It("ignores an unregistered post_logout_redirect_uri but still signs the user out (negative, open redirect)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": "https://evil.example.com/steal", "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.Headers["Location"]).To(Equal(session.SignOutPage), "an unvalidated return URL is never redirected to; our own page is")
		// The point: the redirect is refused, the sign-out is NOT. Leaving a session alive
		// over a cosmetic problem would be the worse failure.
		Expect(sessionsOf(me, sid)).To(BeEmpty())
	})

	It("ignores the return URL when client_id is not the id_token_hint's audience, but still signs out (negative)", func() {
		registerClient(registeredExit)
		Expect(oauth_clients_db.NewOAuthClientsDB(rctx()).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "other-client", ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"https://other.example/callback"}, Scopes: []string{"openid"},
			RequirePKCE: utils.Ptr(true), PostLogoutRedirectURIs: []string{registeredExit},
		})).To(Succeed())
		cookie, sid := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": "other-client", "id_token_hint": hintFor(me, sid), "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(Equal(session.SignOutPage), "other-client registers the URL, but the hint was issued to the dashboard")
		Expect(sessionsOf(me, sid)).To(BeEmpty(), "a bad return URL never blocks the sign-out itself")
	})

	It("ignores a return URL from a client that registered none (negative, absent is not any)", func() {
		registerClient()
		resp := logout("", map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(Equal(session.SignOutPage))
	})

	It("ignores a return URL quoted by an unknown client (negative)", func() {
		resp := logout("", map[string]string{
			"client_id": "ghost-client", "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(Equal(session.SignOutPage))
	})

	// Any site can navigate a browser here, so a request that cannot show it comes from an app signed in through THIS session is a question, not an instruction (RP-Initiated Logout 1.0 s2, s6).
	DescribeTable("asks the person instead of ending anything",
		func(hint func(sid string) string) {
			registerClient(registeredExit)
			cookie, sid := establish(me)
			putFamily(me, "secvuln-web", "fam_2", sid)
			query := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "state": "s1", "unrelated": "x"}
			if h := hint(sid); h != "" {
				query["id_token_hint"] = h
			}

			resp := logout(cookie, query)

			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			loc, err := url.Parse(resp.Headers["Location"])
			Expect(err).NotTo(HaveOccurred())
			Expect(loc.Path).To(Equal(session.SignOutPage), "relative, so the stage or base path the browser came in on carries over")
			delete(query, "unrelated")
			Expect(loc.Query()).To(Equal(urlValues(query)), "only the s2 parameters travel to the question")
			Expect(setCookies(resp)).To(BeEmpty(), "the cookie is not cleared before the person answers")
			Expect(sessionsOf(me, sid)).To(HaveLen(1))
			Expect(familyClientIDs(me)).To(ConsistOf("secvuln-web"))
		},
		Entry("with no id_token_hint", func(string) string { return "" }),
		Entry("with a hint that does not verify", func(string) string { return foreignIssuerToken(me) }),
		Entry("with a hint minted under another session of the same person", func(string) string { return hintFor(me, "sid_another_browser") }),
		Entry("with a hint minted under no session at all", func(string) string { return hintFor(me, "") }),
		Entry("with a hint naming someone else, even under this sid", func(sid string) string { return hintFor(stranger, sid) }),
		Entry("with someone else's expired hint", func(sid string) string { return expiredIDToken(stranger, sid) }),
		Entry("with an access token minted under this session, which every API the app calls holds", func(sid string) string { return tokenFor(me, scope.Sessions, sid) }),
	)

	It("costs the question one client read and one session read, and writes nothing", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)
		backend.DBMock.ProfileReset()

		logout(cookie, map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit})

		//   1  GetItem espuser-oauth-clients  post_logout_redirect_uri checked against the registry
		//   2  GetItem espuser-sessions       the cookie's row, read without rolling it: this response re-issues no cookie
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(2))
		Expect(writeCnt).To(BeZero(), "no touch, and no delete of a family or the session")
		Expect(profile.Accesses["espuser-sessions"].ReadCount).To(Equal(1))
	})

	// A hint that cannot be checked is not one that does not verify, which would forfeit the app's return (s4); nothing ends on a guess either way.
	DescribeTable("answers server_error and ends nothing when the id_token_hint cannot be checked at all",
		func(send func(cookie, hint string) events.APIGatewayProxyResponse) {
			registerClient(registeredExit)
			cookie, sid := establish(me)
			hint := hintFor(me, sid)
			DeferCleanup(os.Setenv, "USER_JWKS_PARA_NAME", os.Getenv("USER_JWKS_PARA_NAME"))
			os.Setenv("USER_JWKS_PARA_NAME", "/espuser/base/no-such-jwks")

			resp := send(cookie, hint)

			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(resp.Body).To(ContainSubstring(oidc.OAuthErrServerError))
			Expect(setCookies(resp)).To(BeEmpty())
			Expect(session.NewService(rctx()).Peek(strings.TrimPrefix(cookie, session.CookieName+"="))).NotTo(BeNil())
		},
		Entry("GET /oauth2/logout", func(cookie, hint string) events.APIGatewayProxyResponse {
			return logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hint})
		}),
		Entry("POST /user/logout", func(cookie, hint string) events.APIGatewayProxyResponse {
			return confirm(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hint}, csrfTokenFor(cookie))
		}),
	)

	It("sends the browser on to the provider's end-session endpoint when the row names one", func() {
		registerClient(registeredExit)
		seedUpstream()
		withDoneURL()
		cookie, sid := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(ContainSubstring("amazoncognito.com/logout"))
		Expect(resp.Headers["Location"]).To(ContainSubstring("client_id=abc123"), "the row's own query must survive")
		Expect(resp.Headers["Location"]).To(ContainSubstring("logout_uri="))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring("post_logout_redirect_uri="),
			"the parameter name comes from the row, not from a provider-type branch in code")
	})

	It("hands the provider OUR return URL, never the caller's", func() {
		// The whole point of the hop's shape. The upstream knows exactly one relying party --
		// us -- just as it does on the login leg, where it holds a single callback URL. Handing
		// it each product's own page instead would mean registering every new product at the
		// upstream, with whoever administers it.
		registerClient(registeredExit)
		seedUpstream()
		withDoneURL()
		cookie, sid := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.Headers["Location"]).To(ContainSubstring(url.QueryEscape("https://issuer.test/oauth2/federation/logout/callback")))
		Expect(resp.Headers["Location"]).NotTo(ContainSubstring(url.QueryEscape(registeredExit)),
			"the caller's page must never reach the provider")
	})

	It("clears the session AND stores the destination on the same response", func() {
		// Two cookies on one response. APIGatewayProxyResponse.Headers holds a single
		// Set-Cookie, so getting this wrong drops one silently: either the browser keeps a
		// session cookie whose row is gone, or the memo is lost and the person lands on the
		// issuer's page instead of the product they signed out of.
		registerClient(registeredExit)
		seedUpstream()
		withDoneURL()
		cookie, sid := establish(me)

		cookies := setCookies(logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		}))
		Expect(cookies).To(HaveLen(2))
		joined := strings.Join(cookies, "\n")
		Expect(joined).To(ContainSubstring(session.CookieName+"=; "), "the session cookie must be cleared")
		Expect(joined).To(ContainSubstring("Max-Age=0"))
		Expect(joined).To(ContainSubstring("__Host-esp_logout_to="))
		Expect(joined).To(ContainSubstring("HttpOnly"))
		Expect(joined).To(ContainSubstring("SameSite=Lax"),
			"Strict would suppress the memo on the provider's hand-back, which is a top-level navigation")
	})

	It("stores no destination when the caller asked for none (negative)", func() {
		seedUpstream()
		withDoneURL()
		cookie, sid := establish(me)

		cookies := setCookies(logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hintFor(me, sid)}))
		Expect(cookies).To(HaveLen(1), "nothing to remember, so no memo")
		Expect(cookies[0]).To(ContainSubstring(session.CookieName + "=; "))
	})

	It("goes straight to the caller when the provider names no end-session endpoint (negative)", func() {
		// The standalone default: a deployment whose provider row configures no logout URL
		// behaves exactly as it did before the hop existed.
		registerClient(registeredExit)
		withDoneURL()
		cookie, sid := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid),
		})
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
		Expect(setCookies(resp)).To(HaveLen(1))
	})

	It("never leaks an id_token_hint to the destination via Referer", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		// The question first: it ends nothing, so the same cookie still signs out after it.
		for _, query := range []map[string]string{
			{"client_id": dashboardClient, "id_token_hint": hintFor(me, "sid_another_browser")},
			{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid)},
		} {
			resp := logout(cookie, query)
			Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"))
			Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
		}
	})

	It("succeeds with no cookie at all — signing out twice is not an error", func() {
		registerClient(registeredExit)
		resp := logout("", map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
	})

	It("clears a cookie whose session is already gone, since it can never work again", func() {
		resp := logout(session.CookieName+"=never-issued", map[string]string{"client_id": dashboardClient})
		Expect(setCookies(resp)).To(ConsistOf(ContainSubstring("Max-Age=0")))
	})

	It("does not send the browser back to the RP after a hint that does not verify, even with nothing to end (RP-Initiated Logout 1.0 s4)", func() {
		registerClient(registeredExit)
		resp := logout("", map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": "not.a.jwt",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(session.SignOutPage), "the issuer's own page, never the RP's")
	})

	It("405s a verb RP-Initiated Logout does not define (negative)", func() {
		resp, err := handleRequest(ctx(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodDelete, Path: pathLogout, Resource: pathLogout,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})
})

var _ = Describe("POST /user/logout, the person's yes", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		registerClient(registeredExit)
	})

	exitQuery := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "state": "s1"}

	It("ends the session, every family under it and the cookie, then sends the browser to the validated destination", func() {
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)
		putFamily(me, dashboardClient, "fam_3", sid)

		resp := confirm(cookie, exitQuery, csrfTokenFor(cookie))

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(redirectTo(resp)).To(Equal(registeredExit+"?state=s1"), "a missing hint is no error, so the RP still gets its state back")
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
		Expect(setCookies(resp)).To(ConsistOf(And(HavePrefix(session.CookieName+"=;"), ContainSubstring("Max-Age=0"))))
		Expect(sessionsOf(me, sid)).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty())
	})

	It("answers a session it cannot read with 500 and ends nothing, never telling the page the browser is signed out (negative)", func() {
		cookie, sid := establish(me)
		putFamily(me, dashboardClient, "fam_3", sid)
		backend.DBMock.NextGetItemError = errors.New("throttled")

		resp := confirm(cookie, map[string]string{}, csrfTokenFor(cookie))

		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(setCookies(resp)).To(BeEmpty())
		Expect(sessionsOf(me, sid)).NotTo(BeEmpty())
		Expect(familyClientIDs(me)).To(ConsistOf(dashboardClient))
	})

	It("costs what a sign-out with a matching hint costs, and the token costs no read", func() {
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)
		putFamily(me, dashboardClient, "fam_3", sid)
		backend.DBMock.ProfileReset()

		confirm(cookie, exitQuery, csrfTokenFor(cookie))

		//   reads:  GetItem espuser-oauth-clients (destination), GetItem espuser-sessions (the cookie's row), Query espuser-refresh-tokens (families), Query espuser-sessions by sid (the row to delete), GetItem espuser-identity-providers (upstream hop)
		//   writes: DeleteItem x2 espuser-refresh-tokens, DeleteItem espuser-sessions
		profile := backend.DBMock.ProfileGet()
		readCnt, writeCnt := profile.TotalCounts()
		Expect(readCnt).To(Equal(5))
		Expect(writeCnt).To(Equal(3), "no touch of a session about to be deleted")
		Expect(profile.Accesses["espuser-refresh-tokens"].WriteCount).To(Equal(2), "one delete per family under the sid")
	})

	It("lands on the issuer's own page, never the RP, after a hint that does not verify (RP-Initiated Logout 1.0 s4)", func() {
		cookie, sid := establish(me)
		query := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": foreignIssuerToken(me)}

		resp := confirm(cookie, query, csrfTokenFor(cookie))

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(redirectTo(resp)).To(BeEmpty(), "the page then says the sign-out is done")
		Expect(sessionsOf(me, sid)).To(BeEmpty(), "the person said yes, so the session still ends")
	})

	It("still returns to the RP for a valid hint from another of the person's sessions", func() {
		cookie, sid := establish(me)
		query := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, "sid_another_browser")}

		Expect(redirectTo(confirm(cookie, query, csrfTokenFor(cookie)))).To(Equal(registeredExit))
		Expect(sessionsOf(me, sid)).To(BeEmpty())
	})

	It("hands the browser to the provider's end-session endpoint and remembers the destination", func() {
		seedUpstream()
		withDoneURL()
		cookie, _ := establish(me)

		resp := confirm(cookie, exitQuery, csrfTokenFor(cookie))

		Expect(redirectTo(resp)).To(HavePrefix("https://pool.auth.ap-south-1.amazoncognito.com/logout?"))
		Expect(redirectTo(resp)).To(ContainSubstring(url.QueryEscape("https://issuer.test/oauth2/federation/logout/callback")))
		joined := strings.Join(setCookies(resp), "\n")
		Expect(joined).To(ContainSubstring(session.CookieName + "=; "))
		Expect(joined).To(ContainSubstring("__Host-esp_logout_to=" + url.QueryEscape(registeredExit+"?state=s1")))
		Expect(federationLogoutCallback("__Host-esp_logout_to=" + url.QueryEscape(registeredExit+"?state=s1")).Headers["Location"]).
			To(Equal(registeredExit+"?state=s1"), "the provider's hand-back still finishes at the RP")
	})

	It("still ends the provider's session after a hint that does not verify, but leaves its hand-back nowhere to send the browser but the issuer", func() {
		seedUpstream()
		withDoneURL()
		cookie, _ := establish(me)
		query := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": foreignIssuerToken(me)}

		resp := confirm(cookie, query, csrfTokenFor(cookie))

		Expect(redirectTo(resp)).To(HavePrefix("https://pool.auth.ap-south-1.amazoncognito.com/logout?"))
		Expect(setCookies(resp)).To(HaveLen(1), "no __Host-esp_logout_to memo, so /oauth2/federation/logout/callback lands on the issuer")
		Expect(setCookies(resp)[0]).To(HavePrefix(session.CookieName + "=;"))
	})

	It("ends nothing for a POST without the session cookie, which is what another site's POST looks like (negative)", func() {
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		resp := confirm("", exitQuery, csrfTokenFor(cookie))

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(setCookies(resp)).To(BeEmpty(), "a top-level cross-site POST could still clear a Lax cookie through this response")
		Expect(sessionsOf(me, sid)).To(HaveLen(1))
		Expect(familyClientIDs(me)).To(ConsistOf("secvuln-web"))
	})

	DescribeTable("ends nothing without this session's own token (negative)",
		func(token func(cookie string) string) {
			cookie, sid := establish(me)
			putFamily(me, "secvuln-web", "fam_2", sid)

			resp := confirm(cookie, exitQuery, token(cookie))

			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(resp.Body).To(ContainSubstring(oidc.OAuthErrInvalidRequest))
			Expect(setCookies(resp)).To(BeEmpty())
			Expect(sessionsOf(me, sid)).To(HaveLen(1))
			Expect(familyClientIDs(me)).To(ConsistOf("secvuln-web"))
		},
		Entry("no token, as a same-site page that never saw the sign-out page sends", func(string) string { return "" }),
		Entry("another browser's token", func(string) string {
			other, _ := establish(stranger)
			return csrfTokenFor(other)
		}),
		Entry("a guessed value", func(string) string { return "AAAA" }),
	)

	It("ends this browser's session on the person's yes whoever the hint named, and returns them to the app", func() {
		cookie, sid := establish(me)
		query := map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(stranger, "sid_theirs")}

		resp := confirm(cookie, query, csrfTokenFor(cookie))

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(redirectTo(resp)).To(Equal(registeredExit), "a stale token from the app still names where it lives")
		Expect(sessionsOf(me, sid)).To(BeEmpty())
	})

	It("refuses a body that is not JSON (negative)", func() {
		cookie, sid := establish(me)
		resp, err := handleRequest(ctx(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost, Path: pathUserLogout, Resource: pathUserLogout,
			Headers: map[string]string{"Cookie": cookie, "Content-Type": "application/json"}, Body: "csrf_token",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(sessionsOf(me, sid)).To(HaveLen(1))
	})

	It("405s a GET, so a link or an image can never answer for the person (negative)", func() {
		cookie, sid := establish(me)
		resp, err := handleRequest(ctx(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodGet, Path: pathUserLogout, Resource: pathUserLogout, Headers: map[string]string{"Cookie": cookie},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		Expect(sessionsOf(me, sid)).To(HaveLen(1))
	})
})

var _ = Describe("POST /oauth2/logout (RP-Initiated Logout 1.0 s2)", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
	})

	post := func(headers map[string]string, body string, base64Encoded bool) events.APIGatewayProxyResponse {
		resp, err := handleRequest(ctx(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost, Path: pathLogout, Resource: pathLogout, Headers: headers, Body: body, IsBase64Encoded: base64Encoded,
		})
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	DescribeTable("turns a form POST into a GET of the same parameters, ending nothing itself",
		func(base64Encoded bool) {
			registerClient()
			cookie, sid := establish(me)
			putFamily(me, dashboardClient, "fam_post", sid)
			body := url.Values{"client_id": {dashboardClient}, "post_logout_redirect_uri": {registeredExit}, "state": {"s1", "s2"}, "ui_locales": {"en"}, "unrelated": {"x"}}.Encode()
			if base64Encoded {
				body = base64.StdEncoding.EncodeToString([]byte(body))
			}

			resp := post(map[string]string{"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8", "Cookie": cookie}, body, base64Encoded)
			Expect(resp.StatusCode).To(Equal(http.StatusSeeOther))
			Expect(resp.Headers).To(Equal(map[string]string{
				"Location":        "?" + url.Values{"client_id": {dashboardClient}, "post_logout_redirect_uri": {registeredExit}, "state": {"s1"}}.Encode(),
				"Cache-Control":   "no-store",
				"Referrer-Policy": "no-referrer",
			}), "only the s2 parameters (the first value of each), never cached, the hint never sent as a Referer, and no cookie touched")
			Expect(listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid))).Sessions).To(HaveLen(1), "the POST ends nothing; the GET it becomes does")
			Expect(familyClientIDs(me)).To(ConsistOf(dashboardClient))
		},
		Entry("plain body", false),
		Entry("base64-encoded body", true),
	)

	It("refuses a body that is not a form (negative)", func() {
		resp := post(map[string]string{"Content-Type": "application/json"}, `{"client_id":"`+dashboardClient+`"}`, false)
		Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		Expect(resp.Body).To(ContainSubstring(oidc.OAuthErrInvalidRequest))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
	})
})

var _ = Describe("what a Sign out button actually reaches", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		registerClient(registeredExit)
	})

	// The question a person is really asking when they click Sign out in one product's
	// sidebar: does this end my Espressif session, or only this app?
	It("ends every product opened in this browser, not just the one clicked in", func() {
		cookie, sid := establish(me)
		putFamily(me, "esp-account-dashboard", "fam_dash", sid)
		putFamily(me, "secvuln-web", "fam_secvuln", sid)
		putFamily(me, "esp-test-2-web", "fam_test2", sid)

		logout(cookie, map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "id_token_hint": hintFor(me, sid)})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty(), "all three families must be gone, not orphaned")
	})

	It("leaves another browser's session alone (negative)", func() {
		cookieMac, sidMacLive := establish(me)
		_, sidPhoneLive := establish(me)
		putFamily(me, "secvuln-web", "fam_mac", sidMacLive)
		putFamily(me, "secvuln-web", "fam_phone", sidPhoneLive)

		logout(cookieMac, map[string]string{"client_id": dashboardClient, "id_token_hint": hintFor(me, sidMacLive)})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidPhoneLive)))
		Expect(body.Sessions).To(HaveLen(1), "signing out here must not sign out the phone")
		Expect(body.Sessions[0].Clients).To(HaveLen(1))
	})

	// The one gap, asserted so it is a known shape rather than a surprise. A family with no
	// sid was minted while the session feature was off, and nothing ties it to THIS browser
	// -- it may belong to another one entirely. Per-session logout cannot prove ownership, so
	// it leaves it; "sign out everywhere" is keyed on the user and takes it.
	It("leaves a family that predates sessions — and sign-out-everywhere still takes it", func() {
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_current", sid)
		putFamily(me, "legacy-app", "fam_no_sid", "")

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hintFor(me, sid)})

		Expect(familyClientIDs(me)).To(ContainElement("legacy-app"), "the sid-less family survives a per-session logout")

		Expect(call(http.MethodDelete, pathSessions, tokenFor(me, scope.Sessions, sid)).StatusCode).
			To(Equal(http.StatusNoContent))

		after := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(familyClientIDs(me)).To(BeEmpty(), "sign out everywhere is keyed on the user, so it reaches it")
		Expect(after.Sessions).To(BeEmpty())
	})

	It("sign-out-everywhere ends every browser, not only the calling one", func() {
		_, sidA := establish(me)
		_, sidB := establish(me)
		putFamily(me, "secvuln-web", "fam_a", sidA)
		putFamily(me, "secvuln-web", "fam_b", sidB)

		Expect(call(http.MethodDelete, pathSessions, tokenFor(me, scope.Sessions, sidA)).StatusCode).
			To(Equal(http.StatusNoContent))

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidA)))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty())
	})
})

var _ = Describe("GET /oauth2/federation/logout/callback — the provider's hand-back", func() {
	const memo = "__Host-esp_logout_to="

	BeforeEach(func() { backend = test_utils.SetupEspUserBackend(ctx()) })

	It("forwards to the destination the memo carries, and clears the memo", func() {
		resp := federationLogoutCallback(memo + url.QueryEscape("https://test1.example/bye"))
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("https://test1.example/bye"))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("__Host-esp_logout_to="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age=0"),
			"a memo that outlives its sign-out is a stale redirect waiting to happen")
	})

	It("lands on the issuer's own page when there is no memo (negative)", func() {
		// The browser abandoned the chain, or the memo expired. Say the sign-out worked
		// rather than inventing a destination.
		resp := federationLogoutCallback("")
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("../../"+session.SignOutPage), "relative from /oauth2/federation/logout/callback to /user/logout")
	})

	It("lands on /user/logout from both routes that redirect there, under any stage prefix", func() {
		resolve := func(from, location string) string {
			base, err := url.Parse(from)
			Expect(err).NotTo(HaveOccurred())
			ref, err := url.Parse(location)
			Expect(err).NotTo(HaveOccurred())
			return base.ResolveReference(ref).Path
		}
		Expect(resolve("/prod/oauth2/logout", session.SignOutPage)).To(Equal("/prod/user/logout"))
		Expect(resolve("/prod/oauth2/federation/logout/callback", "../../"+session.SignOutPage)).To(Equal("/prod/user/logout"))
		Expect(resolve("/oauth2/logout", session.SignOutPage)).To(Equal("/user/logout"))
	})

	It("refuses to forward to a scheme that names code, not a place (negative)", func() {
		// Defence in depth. The value was validated against the client registry before the
		// session was destroyed and the cookie is HttpOnly and __Host- prefixed, so nothing
		// but this server could have written it -- but an endpoint that forwards a browser is
		// one mistake away from being an open redirect wearing the issuer's own hostname.
		for _, bad := range []string{"javascript:alert(1)", "data:text/html,<script>", "/relative", "not a url at all"} {
			resp := federationLogoutCallback(memo + url.QueryEscape(bad))
			Expect(resp.Headers["Location"]).To(Equal("../../"+session.SignOutPage), bad)
		}
	})

	It("forwards to a native app's private-use scheme (RFC 8252 s7.1)", func() {
		resp := federationLogoutCallback(memo + url.QueryEscape("com.espressif.rainmaker://signed-out"))
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("com.espressif.rainmaker://signed-out"))
	})

	It("forwards without ending a session (negative)", func() {
		// The sign-out that sent the browser upstream already ended the session; the hand-back only forwards.
		cookie, _ := establish(me)
		value := strings.TrimPrefix(cookie, session.CookieName+"=")
		resp := federationLogoutCallback(memo + url.QueryEscape("https://test1.example/bye"))
		Expect(resp.Headers["Location"]).To(Equal("https://test1.example/bye"))
		Expect(session.NewService(rctx()).Peek(value)).NotTo(BeNil(),
			"the hand-back must not destroy a session; the sign-out already did that")
	})
})
