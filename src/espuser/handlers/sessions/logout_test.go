// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/session"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
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

// logoutDone drives the hand-back the provider performs, carrying whatever memo the browser
// holds. No query parameters: the provider matches our return URL exactly, so nothing can be
// appended to it -- which is precisely why the destination travels in a cookie.
func logoutDone(cookie string) events.APIGatewayProxyResponse {
	req := events.APIGatewayProxyRequest{HTTPMethod: http.MethodGet, Path: pathLogoutDone}
	if cookie != "" {
		req.Headers = map[string]string{"Cookie": cookie}
	}
	resp, err := handleRequest(ctx(), req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

func logout(cookie string, query map[string]string) events.APIGatewayProxyResponse {
	req := events.APIGatewayProxyRequest{
		HTTPMethod: http.MethodGet, Path: pathLogout, QueryStringParameters: query,
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

	It("ends the session and every product opened under it", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)
		putFamily(me, dashboardClient, "fam_3", sid)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
		// Max-Age=0 is what actually removes it from the browser.
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring(session.CookieName + "="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age=0"))

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty(), "the session row must be gone")
		Expect(familyClientIDs(me)).To(BeEmpty(), "both families must be gone, not orphaned")
	})

	It("echoes the RP's state on the post-logout redirect (RP-Initiated Logout 1.0 §2)", func() {
		registerClient(registeredExit)
		cookie, _ := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit, "state": "xyz123",
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(HavePrefix(registeredExit))
		Expect(resp.Headers["Location"]).To(ContainSubstring("state=xyz123"))
	})

	It("ignores an unregistered post_logout_redirect_uri but still signs the user out (negative, open redirect)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": "https://evil.example.com/steal",
		})
		Expect(resp.StatusCode).NotTo(Equal(http.StatusFound), "an unvalidated return URL must never be redirected to")
		Expect(resp.Headers["Location"]).To(BeEmpty())
		// The point: the redirect is refused, the sign-out is NOT. Leaving a session alive
		// over a cosmetic problem would be the worse failure.
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty())
	})

	It("ignores a return URL from a client that registered none (negative, absent is not any)", func() {
		registerClient()
		cookie, _ := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(BeEmpty())
	})

	It("ignores a return URL quoted by an unknown client (negative)", func() {
		cookie, _ := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": "ghost-client", "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(BeEmpty())
	})

	It("takes the client from id_token_hint when client_id is absent (RP-Initiated Logout 1.0 §2)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		hint, err := minter.IDToken(me, dashboardClient, "", 0, jwtutil.Contact{}, sid)
		Expect(err).NotTo(HaveOccurred())
		resp := logout(cookie, map[string]string{"id_token_hint": hint, "post_logout_redirect_uri": registeredExit})
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
	})

	It("ignores the return URL when client_id is not the id_token_hint's audience, but still signs out (negative)", func() {
		registerClient(registeredExit)
		Expect(oauth_clients_db.NewOAuthClientsDB(rctx()).CreateClient(&oauth_clients_db.OAuthClientEntry{
			ClientID: "other-client", ClientType: oauth_clients_db.ClientTypePublic,
			RedirectURIs: []string{"https://other.example/callback"}, Scopes: []string{"openid"},
			RequirePKCE: utils.Ptr(true), PostLogoutRedirectURIs: []string{registeredExit},
		})).To(Succeed())
		cookie, sid := establish(me)
		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		hint, err := minter.IDToken(me, dashboardClient, "", 0, jwtutil.Contact{}, sid)
		Expect(err).NotTo(HaveOccurred())
		resp := logout(cookie, map[string]string{
			"client_id": "other-client", "id_token_hint": hint, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(BeEmpty(), "other-client registers the URL, but the hint was issued to the dashboard")
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty(), "a bad return URL never blocks the sign-out itself")
	})

	It("ends nothing when id_token_hint names a different subject (negative, cross-user)", func() {
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		hint, err := minter.IDToken(stranger, dashboardClient, "", 0, jwtutil.Contact{})
		Expect(err).NotTo(HaveOccurred())

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hint})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(HaveLen(1), "a stranger's hint must not end this person's session")
	})

	It("ends EVERYTHING when the hint is valid and names this person (the SDK's normal request)", func() {
		// The case every real sign-out takes, and the one nothing covered: the SDK always
		// sends the id_token it holds. Two negatives around this path passed happily while
		// the path itself was broken in production.
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		hint, err := minter.IDToken(me, dashboardClient, "", 0, jwtutil.Contact{}, sid)
		Expect(err).NotTo(HaveOccurred())

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": hint})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty(), "the session row must be gone")
		Expect(familyClientIDs(me)).To(BeEmpty(), "every product's refresh family must be gone")
	})

	It("ends everything when the hint is EXPIRED (the production bug)", func() {
		// An ID token lives an hour; a tab left open outlives it. Rejecting the hint on exp
		// turned sign-out into a silent no-op -- cookie cleared, browser redirected, session
		// row and every refresh family untouched -- which is exactly what was observed:
		// signing out of the dashboard left SecVuln's family standing.
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": expiredIDToken(me, sid)})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty(), "an expired hint is still a valid hint")
		Expect(familyClientIDs(me)).To(BeEmpty())
	})

	It("still refuses a STRANGER's expired hint (negative — the veto must survive the expiry fix)", func() {
		// This is the spec that keeps the two fixes honest. Accepting an expired hint is only
		// safe if an expired hint is still CHECKED: were expiry to reject it again, it would
		// fall into the "unverifiable, therefore ignored" branch and a stranger's stale token
		// would end this person's session. Verified-but-mismatched must stay a veto.
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		logout(cookie, map[string]string{
			"client_id": dashboardClient, "id_token_hint": expiredIDToken(stranger, sid),
		})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(HaveLen(1), "a stranger's hint must not end this person's session, expired or not")
	})

	It("ignores a hint it cannot verify rather than refusing to sign anyone out", func() {
		// Refusing here bought nothing: omitting id_token_hint entirely is already allowed,
		// so a garbage hint gives a caller no capability an absent one does not. It only
		// cost real users a sign-out that silently did nothing.
		registerClient(registeredExit)
		cookie, sid := establish(me)
		putFamily(me, "secvuln-web", "fam_2", sid)

		logout(cookie, map[string]string{"client_id": dashboardClient, "id_token_hint": foreignIssuerToken(me)})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty())
	})

	// seedUpstream registers a provider that names an end-session endpoint, in Cognito's
	// shape: client_id baked into the stored URL, and the return parameter named logout_uri
	// rather than the OIDC-standard one.
	seedUpstream := func() {
		enabled := true
		Expect(identity_providers_db.NewIdentityProvidersDB(rctx()).CreateProvider(&identity_providers_db.ProviderEntry{
			ProviderName: "rainmaker-public", Type: identity_providers_db.TypeOIDC, Enabled: &enabled,
			EndSessionURL:           "https://pool.auth.ap-south-1.amazoncognito.com/logout?client_id=abc123",
			EndSessionRedirectParam: "logout_uri",
		})).To(Succeed())
	}

	// withDoneURL sets our own return URL for a spec, the value CDK supplies in production.
	withDoneURL := func() {
		os.Setenv("ESPUSER_LOGOUT_DONE_URL", "https://issuer.test/oauth2/logout/done")
		DeferCleanup(func() { os.Unsetenv("ESPUSER_LOGOUT_DONE_URL") })
	}

	setCookies := func(resp events.APIGatewayProxyResponse) []string {
		if len(resp.MultiValueHeaders["Set-Cookie"]) > 0 {
			return resp.MultiValueHeaders["Set-Cookie"]
		}
		if v := resp.Headers["Set-Cookie"]; v != "" {
			return []string{v}
		}
		return nil
	}

	It("sends the browser on to the provider's end-session endpoint when the row names one", func() {
		registerClient(registeredExit)
		seedUpstream()
		withDoneURL()
		cookie, _ := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
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
		cookie, _ := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(ContainSubstring(url.QueryEscape("https://issuer.test/oauth2/logout/done")))
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
		cookie, _ := establish(me)

		cookies := setCookies(logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
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
		cookie, _ := establish(me)

		cookies := setCookies(logout(cookie, map[string]string{"client_id": dashboardClient}))
		Expect(cookies).To(HaveLen(1), "nothing to remember, so no memo")
		Expect(cookies[0]).To(ContainSubstring(session.CookieName + "=; "))
	})

	It("goes straight to the caller when the provider names no end-session endpoint (negative)", func() {
		// The standalone default: a deployment whose provider row configures no logout URL
		// behaves exactly as it did before the hop existed.
		registerClient(registeredExit)
		withDoneURL()
		cookie, _ := establish(me)

		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
		Expect(setCookies(resp)).To(HaveLen(1))
	})

	It("never leaks an id_token_hint to the destination via Referer", func() {
		registerClient(registeredExit)
		cookie, _ := establish(me)
		resp := logout(cookie, map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.Headers["Referrer-Policy"]).To(Equal("no-referrer"))
		Expect(resp.Headers["Cache-Control"]).To(Equal("no-store"))
	})

	It("succeeds with no cookie at all — signing out twice is not an error", func() {
		registerClient(registeredExit)
		resp := logout("", map[string]string{
			"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit,
		})
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal(registeredExit))
	})

	It("405s a verb RP-Initiated Logout does not define (negative)", func() {
		resp, err := handleRequest(ctx(), events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodDelete, Path: pathLogout,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
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

		logout(cookie, map[string]string{"client_id": dashboardClient, "post_logout_redirect_uri": registeredExit})

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sid)))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty(), "all three families must be gone, not orphaned")
	})

	It("leaves another browser's session alone (negative)", func() {
		cookieMac, sidMacLive := establish(me)
		_, sidPhoneLive := establish(me)
		putFamily(me, "secvuln-web", "fam_mac", sidMacLive)
		putFamily(me, "secvuln-web", "fam_phone", sidPhoneLive)

		logout(cookieMac, map[string]string{"client_id": dashboardClient})

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

		logout(cookie, map[string]string{"client_id": dashboardClient})

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

var _ = Describe("GET /oauth2/logout/done — the provider's hand-back", func() {
	const memo = "__Host-esp_logout_to="

	BeforeEach(func() { backend = test_utils.SetupEspUserBackend(ctx()) })

	It("forwards to the destination the memo carries, and clears the memo", func() {
		resp := logoutDone(memo + url.QueryEscape("https://test1.example/bye"))
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("https://test1.example/bye"))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("__Host-esp_logout_to="))
		Expect(resp.Headers["Set-Cookie"]).To(ContainSubstring("Max-Age=0"),
			"a memo that outlives its sign-out is a stale redirect waiting to happen")
	})

	It("lands on the issuer's own page when there is no memo (negative)", func() {
		// The browser abandoned the chain, or the memo expired. Say the sign-out worked
		// rather than inventing a destination.
		resp := logoutDone("")
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Headers).NotTo(HaveKey("Location"))
	})

	It("refuses to forward to a scheme that names code, not a place (negative)", func() {
		// Defence in depth. The value was validated against the client registry before the
		// session was destroyed and the cookie is HttpOnly and __Host- prefixed, so nothing
		// but this server could have written it -- but an endpoint that forwards a browser is
		// one mistake away from being an open redirect wearing the issuer's own hostname.
		for _, bad := range []string{"javascript:alert(1)", "data:text/html,<script>", "/relative", "not a url at all"} {
			resp := logoutDone(memo + url.QueryEscape(bad))
			Expect(resp.StatusCode).To(Equal(http.StatusOK), bad)
			Expect(resp.Headers).NotTo(HaveKey("Location"), bad)
		}
	})

	It("forwards to a native app's private-use scheme (RFC 8252 s7.1)", func() {
		resp := logoutDone(memo + url.QueryEscape("com.espressif.rainmaker://signed-out"))
		Expect(resp.StatusCode).To(Equal(http.StatusFound))
		Expect(resp.Headers["Location"]).To(Equal("com.espressif.rainmaker://signed-out"))
	})

	It("is not treated as a sign-out request (negative, routing)", func() {
		// /oauth2/logout/done sits under /oauth2/logout. A prefix match on the latter would
		// route the hand-back into handleLogout, which would find no session and no
		// destination and land the person on the issuer's page -- correct-looking, and wrong.
		cookie, _ := establish(me)
		value := strings.TrimPrefix(cookie, session.CookieName+"=")
		resp := logoutDone(memo + url.QueryEscape("https://test1.example/bye"))
		Expect(resp.Headers["Location"]).To(Equal("https://test1.example/bye"))
		Expect(session.NewService(rctx()).Lookup(value)).NotTo(BeNil(),
			"the hand-back must not destroy a session; the sign-out already did that")
	})
})
