// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/oauth_clients_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/jwtutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	jwt "github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionsHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sessions API Suite")
}

const (
	me       = "u_amey"
	stranger = "u_someone_else"
	sidPhone = "sid_IPHONE"
	sidMac   = "sid_CHROME"

	uaSafariIPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	uaChromeMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
)

var backend *test_utils.EspUserBackend

func ctx() context.Context { return context.Background() }

func rctx() *rmngctx.RmngContext { return rmngctx.NewRmngContextWithCtx(context.Background(), nil) }

// tokenFor mints one of our own access tokens from the first-party app. sid ties it to a session; scopes is the grant. Both are what the endpoint gates on, so both are spec inputs rather than fixtures.
func tokenFor(userID, scopes, sid string) string {
	return tokenForClient(userID, dashboardClient, scopes, sid)
}

// tokenForClient is tokenFor with the issuing client named, for exercising the first-party pin with a third-party audience.
func tokenForClient(userID, clientID, scopes, sid string) string {
	minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
	token, err := minter.AccessToken(userID, clientID, scopes, "", "", jwtutil.Contact{}, sid)
	Expect(err).NotTo(HaveOccurred())
	return token
}

// machineToken mints a client_credentials (machine) access token: the right scope and even the first-party client id, but no human behind it.
func machineToken(scopes string) string {
	minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
	token, err := minter.ClientCredentialsToken(dashboardClient, scopes, "")
	Expect(err).NotTo(HaveOccurred())
	return token
}

// expiredToken mints one that was already dead when it was signed. Minting it rather than
// sleeping keeps the spec fast; what it asserts is the verifier's exp check.
func expiredToken(userID string) string {
	claims := jwt.MapClaims{
		"iss": backend.Issuer, "sub": userID, "aud": "esp-account-dashboard",
		"client_id": "esp-account-dashboard", "token_use": jwtutil.TokenUseAccess,
		"scope": scope.Sessions, "sid": sidMac,
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"exp": time.Now().Add(-time.Hour).Unix(),
	}
	token, err := jwtutil.SignRS256(claims, backend.SigningKey, oidc.SigningKeyID)
	Expect(err).NotTo(HaveOccurred())
	return token
}

// foreignIssuerToken is signed by OUR key but claims a different issuer. That isolates the
// iss check: a token failing on the signature would pass this spec for the wrong reason.
func foreignIssuerToken(userID string) string {
	claims := jwt.MapClaims{
		"iss": "https://accounts.evil.example.com", "sub": userID,
		"aud": "esp-account-dashboard", "client_id": "esp-account-dashboard",
		"token_use": jwtutil.TokenUseAccess, "scope": scope.Sessions,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	token, err := jwtutil.SignRS256(claims, backend.SigningKey, oidc.SigningKeyID)
	Expect(err).NotTo(HaveOccurred())
	return token
}

func req(method, path, token string) events.APIGatewayProxyRequest {
	r := events.APIGatewayProxyRequest{HTTPMethod: method, Path: path}
	if token != "" {
		r.Headers = map[string]string{"Authorization": "Bearer " + token}
	}
	// API Gateway sets the {sessionId} path parameter from the matched resource; these tests
	// drive handleRequest directly, so populate it from the last path segment.
	if id := strings.TrimPrefix(path, pathSessions+"/"); id != path && id != "" && !strings.Contains(id, "/") {
		r.PathParameters = map[string]string{"sessionId": id}
	}
	return r
}

// putSession writes a live browser session row directly. Going through the DB rather than a
// real login keeps the spec about the API's behaviour, not about the login that preceded it.
func putSession(userID, sid, userAgent string) {
	now := time.Now().Unix()
	Expect(sessions_db.NewSessionsDB(rctx()).CreateSession(&sessions_db.SessionEntry{
		SessionHash:     "hash_" + sid,
		SID:             sid,
		UserID:          userID,
		Provider:        "rainmaker-public",
		Origin:          "browser",
		AuthTime:        now - 60,
		ExpiresAt:       now + 7200,
		CookieExpiresAt: now + 7200,
		LastSeenAt:      now - 30,
		CreatedAt:       now,
		UserAgent:       userAgent,
		IPAddress:       "103.21.0.1",
	})).To(Succeed())
}

func putFamily(userID, clientID, familyID, sid string) {
	now := time.Now().Unix()
	Expect(refresh_tokens_db.NewRefreshTokensDB(rctx()).CreateFamily(&refresh_tokens_db.FamilyEntry{
		UserID: userID, ClientID: clientID, FamilyID: familyID,
		ClientFamily: clientID + "#" + familyID,
		SID:          sid, CreatedAt: now, RotatedAt: now, ExpiresOn: now + 86400,
	})).To(Succeed())
}

// familyClientIDs reads a user's surviving refresh families straight from the DB and returns
// their client ids. It replaces the removed orphan/unattached-products view: a sign-out that
// leaves a family standing is now caught by querying the table, not by a list-response field.
func familyClientIDs(userID string) []string {
	rows, err := refresh_tokens_db.NewRefreshTokensDB(rctx()).ListByUser(userID)
	Expect(err).NotTo(HaveOccurred())
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ClientID)
	}
	return ids
}

// putFirstPartyClient registers clientID as a client this account ships -- what the sessions endpoint now gates on (a client-registry attribute, not an env var). Every spec that reaches the gate with tokenFor needs the dashboard client to exist and be first-party.
func putFirstPartyClient(clientID string) {
	Expect(oauth_clients_db.NewOAuthClientsDB(rctx()).CreateClient(&oauth_clients_db.OAuthClientEntry{
		ClientID: clientID, ClientType: oauth_clients_db.ClientTypePublic, FirstParty: true,
	})).To(Succeed())
}

// putThirdPartyClient registers a client that IS in the registry but is not first-party -- a delegated integration (voice assistant, MCP). Its user token is refused for that reason, not because the client is unknown.
func putThirdPartyClient(clientID string) {
	Expect(oauth_clients_db.NewOAuthClientsDB(rctx()).CreateClient(&oauth_clients_db.OAuthClientEntry{
		ClientID: clientID, ClientType: oauth_clients_db.ClientTypeConfidential, FirstParty: false,
	})).To(Succeed())
}

func listBody(resp events.APIGatewayProxyResponse) listResponse {
	var out listResponse
	Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
	return out
}

func call(method, path, token string) events.APIGatewayProxyResponse {
	resp, err := handleRequest(ctx(), req(method, path, token))
	Expect(err).NotTo(HaveOccurred())
	return resp
}

var _ = Describe("GET /v1/user/sessions", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putSession(me, sidMac, uaChromeMac)
		putSession(me, sidPhone, uaSafariIPhone)
		putFamily(me, "secvuln-web", "fam_1", sidPhone)
		putFamily(me, "secvuln-web", "fam_2", sidMac)
		putFamily(me, "esp-account-dashboard", "fam_3", sidMac)
	})

	It("groups every product under the browser it was opened in", func() {
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, "openid "+scope.Sessions, sidMac)))
		Expect(body.Sessions).To(HaveLen(2))

		bySID := map[string]SessionView{}
		for _, s := range body.Sessions {
			bySID[s.SID] = s
		}
		// One browser, two products -- which is exactly what makes one sign-out reach both.
		Expect(bySID[sidMac].Clients).To(HaveLen(2))
		Expect(bySID[sidPhone].Clients).To(HaveLen(1))
		Expect(bySID[sidMac].UserAgentType).To(Equal("Chrome on macOS"))
		Expect(bySID[sidPhone].UserAgentType).To(Equal("Safari on iPhone"))
	})

	It("marks the session this very token was minted under, and only that one", func() {
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac)))
		for _, s := range body.Sessions {
			Expect(s.Current).To(Equal(s.SID == sidMac), "current must follow the token's sid")
		}
	})

	It("never returns the session hash (negative)", func() {
		// It is the hash of the cookie secret. It cannot be reversed, but it is an internal
		// key and a client has no use for one -- the delete path re-resolves a sid instead.
		resp := call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac))
		Expect(resp.Body).NotTo(ContainSubstring("hash_"))
		Expect(resp.Body).NotTo(ContainSubstring("session_hash"))
	})

	It("shows one user nothing of another's (negative, cross-user)", func() {
		putSession(stranger, "sid_THEIRS", uaChromeMac)
		putFamily(stranger, "secvuln-web", "fam_9", "sid_THEIRS")

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac)))
		for _, s := range body.Sessions {
			Expect(s.SID).NotTo(Equal("sid_THEIRS"))
		}
		Expect(body.Sessions).To(HaveLen(2))
	})

	It("omits an expired session, without waiting for the TTL sweep (negative)", func() {
		past := time.Now().Unix() - 10
		Expect(sessions_db.NewSessionsDB(rctx()).CreateSession(&sessions_db.SessionEntry{
			SessionHash: "hash_stale", SID: "sid_STALE", UserID: me,
			ExpiresAt: past, CreatedAt: past,
		})).To(Succeed())

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac)))
		for _, s := range body.Sessions {
			Expect(s.SID).NotTo(Equal("sid_STALE"), "DynamoDB TTL lags by up to ~48h; a list that shows a session the server would refuse is a lie")
		}
	})
})

var _ = Describe("GET /v1/user/sessions — origin, last active, and cookie state", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
	})

	It("reports the inferred name and the person's own label as separate fields, and carries no cookie clock", func() {
		now := time.Now().Unix()
		Expect(sessions_db.NewSessionsDB(rctx()).CreateSession(&sessions_db.SessionEntry{
			SessionHash: "hash_app", SID: "sid_APP", UserID: me, Provider: "otp",
			Origin: "app", UserAgent: "RainMaker/3.2 (Android 14; Pixel 8)", UserAgentName: "John's iPhone", AuthTime: now - 60,
			ExpiresAt: now + 7200, LastSeenAt: now - 10, CreatedAt: now,
		})).To(Succeed())

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, "")))
		Expect(body.Sessions).To(HaveLen(1))
		app := body.Sessions[0]
		Expect(app.Origin).To(Equal("app"))
		Expect(app.UserAgentType).To(Equal("Android app"), "a native User-Agent names an SDK, so only its OS is worth rendering")
		Expect(app.UserAgentName).To(Equal("John's iPhone"), "the person's own label stays its own field, never folded into the inferred one")
		Expect(app.LastSeenAt).To(Equal(now-10), "last_seen_at renders as last active")
		Expect(app.CookieExpiresAt).To(BeZero(), "an app row never had a cookie")
	})

	It("leaves a lapsed cookie visible as a past cookie_expires_at on a still-live row", func() {
		now := time.Now().Unix()
		Expect(sessions_db.NewSessionsDB(rctx()).CreateSession(&sessions_db.SessionEntry{
			SessionHash: "hash_lapsed", SID: "sid_LAPSED", UserID: me, Provider: "rainmaker-public",
			Origin: "browser", AuthTime: now - 3600, UserAgent: uaChromeMac,
			// The user agent is still live (expires_at is well ahead) but the cookie lapsed: used
			// only through an app since, so no front-channel event re-issued it.
			ExpiresAt: now + 7200, CookieExpiresAt: now - 60, LastSeenAt: now - 30, CreatedAt: now - 3600,
		})).To(Succeed())

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, "")))
		Expect(body.Sessions).To(HaveLen(1))
		Expect(body.Sessions[0].CookieExpiresAt).To(BeNumerically("<", now),
			"a client reads must-sign-in-again from cookie_expires_at alone; an app row simply has none")
		Expect(body.Sessions[0].ExpiresAt).To(BeNumerically(">", now), "the user agent itself is still live")
	})
})

var _ = Describe("the five negatives, on every session endpoint", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putThirdPartyClient("va-client")
		putSession(me, sidMac, uaChromeMac)
	})

	It("401s with no token at all", func() {
		Expect(call(http.MethodGet, pathSessions, "").StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("401s a garbage token", func() {
		Expect(call(http.MethodGet, pathSessions, "not.a.token").StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("403s a token granted every scope but this one (negative, missing scope)", func() {
		resp := call(http.MethodGet, pathSessions, tokenFor(me, "openid email profile phone", sidMac))
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		Expect(resp.Headers["WWW-Authenticate"]).To(ContainSubstring("insufficient_scope"))
	})

	It("403s a scope that merely looks like ours (negative, no prefix matching)", func() {
		Expect(call(http.MethodGet, pathSessions, tokenFor(me, "account:sessions.read", sidMac)).StatusCode).
			To(Equal(http.StatusForbidden))
	})

	It("401s an expired token (negative)", func() {
		Expect(call(http.MethodGet, pathSessions, expiredToken(me)).StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("401s a token from another issuer (negative, wrong iss)", func() {
		Expect(call(http.MethodGet, pathSessions, foreignIssuerToken(me)).StatusCode).
			To(Equal(http.StatusUnauthorized))
	})

	It("401s an id token replayed as an access token (negative, token substitution)", func() {
		minter := jwtutil.NewMinter(backend.Issuer, backend.SigningKey, oidc.SigningKeyID)
		idToken, err := minter.IDToken(me, "esp-account-dashboard", "", 0, jwtutil.Contact{}, sidMac)
		Expect(err).NotTo(HaveOccurred())
		Expect(call(http.MethodGet, pathSessions, idToken).StatusCode).To(Equal(http.StatusUnauthorized))
	})

	It("401s a client_credentials machine token, even with the right scope and the first-party client id (negative, non-human grant)", func() {
		Expect(call(http.MethodGet, pathSessions, machineToken(scope.Sessions)).StatusCode).
			To(Equal(http.StatusUnauthorized))
	})

	It("403s a valid user token from a third-party client, even with the scope (negative, not first-party)", func() {
		resp := call(http.MethodGet, pathSessions, tokenForClient(me, "va-client", scope.Sessions, sidMac))
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		Expect(resp.Body).To(ContainSubstring("access_denied"))
	})
})

var _ = Describe("DELETE /v1/user/sessions", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putSession(me, sidMac, uaChromeMac)
		putSession(me, sidPhone, uaSafariIPhone)
		putFamily(me, "secvuln-web", "fam_1", sidPhone)
		putFamily(me, "secvuln-web", "fam_2", sidMac)
		putFamily(me, "esp-account-dashboard", "fam_3", sidMac)
	})

	It("ends one browser and every product in it, leaving the other browser alone", func() {
		// The whole point of the index: this call is made FROM the Mac, about the iPhone,
		// and the iPhone's cookie secret exists nowhere on the server.
		Expect(call(http.MethodDelete, pathSessions+"/"+sidPhone, tokenFor(me, scope.Sessions, sidMac)).StatusCode).
			To(Equal(http.StatusNoContent))

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac)))
		Expect(body.Sessions).To(HaveLen(1))
		Expect(body.Sessions[0].SID).To(Equal(sidMac))
		Expect(body.Sessions[0].Clients).To(HaveLen(2), "the Mac's own products must survive")
		Expect(familyClientIDs(me)).To(ConsistOf("secvuln-web", "esp-account-dashboard"),
			"the iPhone's family must be gone, not orphaned — only the Mac's two survive")
	})

	It("is idempotent — a second delete still succeeds", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		Expect(call(http.MethodDelete, pathSessions+"/"+sidPhone, token).StatusCode).To(Equal(http.StatusNoContent))
		Expect(call(http.MethodDelete, pathSessions+"/"+sidPhone, token).StatusCode).To(Equal(http.StatusNotFound))
	})

	It("404s somebody else's session, and never 403s it (negative, cross-user)", func() {
		putSession(stranger, "sid_THEIRS", uaChromeMac)
		putFamily(stranger, "secvuln-web", "fam_9", "sid_THEIRS")

		resp := call(http.MethodDelete, pathSessions+"/sid_THEIRS", tokenFor(me, scope.Sessions, sidMac))
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound),
			"403 would confirm the sid exists, making this an oracle for guessing session ids")

		// And it must not have been touched.
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(stranger, scope.Sessions, "sid_THEIRS")))
		Expect(body.Sessions).To(HaveLen(1))
	})

	It("ends everything, this browser included, on the collection delete", func() {
		Expect(call(http.MethodDelete, pathSessions, tokenFor(me, scope.Sessions, sidMac)).StatusCode).
			To(Equal(http.StatusNoContent))

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, sidMac)))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty(), "every family must be gone, not orphaned")
	})

	It("leaves another user untouched by sign-out-everywhere (negative, cross-user)", func() {
		putSession(stranger, "sid_THEIRS", uaChromeMac)
		Expect(call(http.MethodDelete, pathSessions, tokenFor(me, scope.Sessions, sidMac)).StatusCode).
			To(Equal(http.StatusNoContent))

		body := listBody(call(http.MethodGet, pathSessions, tokenFor(stranger, scope.Sessions, "sid_THEIRS")))
		Expect(body.Sessions).To(HaveLen(1))
	})

	// Every response a BROWSER reads must carry the CORS header. This is invisible to Go
	// tests and to curl, and it fails in the worst possible direction: the server does the
	// work, the browser discards the reply, and the page says the action failed when the
	// session really is gone. The dashboard hit exactly this on the delete path.
	It("returns Access-Control-Allow-Origin on every response the dashboard reads", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		for _, probe := range []struct {
			what     string
			response events.APIGatewayProxyResponse
		}{
			{"list", call(http.MethodGet, pathSessions, token)},
			{"delete one", call(http.MethodDelete, pathSessions+"/"+sidPhone, token)},
			{"delete one, again (404)", call(http.MethodDelete, pathSessions+"/"+sidPhone, token)},
			{"delete all", call(http.MethodDelete, pathSessions, token)},
			{"no token (401)", call(http.MethodGet, pathSessions, "")},
			{"wrong scope (403)", call(http.MethodGet, pathSessions, tokenFor(me, "openid", sidMac))},
		} {
			Expect(probe.response.Headers).To(HaveKeyWithValue("Access-Control-Allow-Origin", "*"),
				"the "+probe.what+" response is unreadable from a browser without this")
		}
	})

	It("405s a method the collection does not offer (negative)", func() {
		Expect(call(http.MethodPost, pathSessions, tokenFor(me, scope.Sessions, sidMac)).StatusCode).
			To(Equal(http.StatusMethodNotAllowed))
	})
})

var _ = Describe("the standalone check — sessions unconfigured", func() {
	It("returns an empty list rather than an error when the feature was never turned on", func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		// No session rows exist because nothing ever wrote one.
		body := listBody(call(http.MethodGet, pathSessions, tokenFor(me, scope.Sessions, "")))
		Expect(body.Sessions).To(BeEmpty())
		Expect(familyClientIDs(me)).To(BeEmpty())
	})
})
