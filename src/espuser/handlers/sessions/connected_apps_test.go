// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/scope"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func connectedAppsBody(resp events.APIGatewayProxyResponse) connectedAppsResponse {
	Expect(resp.StatusCode).To(Equal(http.StatusOK), resp.Body)
	var out connectedAppsResponse
	Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
	return out
}

var _ = Describe("GET /v1/user/connected-apps", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		backend.DBMock.MaxPageItems = 1
		putFirstPartyClient(dashboardClient)
	})

	It("lists one entry per client holding offline grants, earliest created and latest used, most recently used first", func() {
		putGrant(me, "voice-assistant", "fam_a", 1000, 5000)
		putGrant(me, "voice-assistant", "fam_b", 2000, 9000)
		putGrant(me, "mcp-connector", "fam_c", 3000, 7000)
		// Minted before created_at was recorded: rotated_at stands in for it.
		putGrant(me, "smart-home", "fam_d", 0, 8000)

		body := connectedAppsBody(call(http.MethodGet, pathConnectedApps, tokenFor(me, scope.Sessions, sidMac)))
		Expect(body.ConnectedApps).To(Equal([]ClientView{
			{ClientID: "voice-assistant", CreatedAt: 1000, LastUsed: 9000},
			{ClientID: "smart-home", CreatedAt: 8000, LastUsed: 8000},
			{ClientID: "mcp-connector", CreatedAt: 3000, LastUsed: 7000},
		}))
	})

	It("never lists a session family, which belongs under its session instead, an expired grant the TTL has not swept, nor another person's grant (negative)", func() {
		putSession(me, sidMac, uaChromeMac)
		putFamily(me, "secvuln-web", "fam_session", sidMac)
		putFamily(me, "legacy-app", "fam_nosid", "")
		Expect(refresh_tokens_db.NewRefreshTokensDB(rctx()).CreateFamily(&refresh_tokens_db.FamilyEntry{
			UserID: me, ClientID: "lapsed-assistant", FamilyID: "fam_lapsed", Scope: "openid offline_access", ExpiresOn: time.Now().Unix() - 60,
		})).To(Succeed())
		putGrant(stranger, "voice-assistant", "fam_theirs", 1000, 2000)

		resp := call(http.MethodGet, pathConnectedApps, tokenFor(me, scope.Sessions, sidMac))
		Expect(resp.Body).To(MatchJSON(`{"connected_apps": []}`), "an empty list is [], never null")
	})
})

var _ = Describe("DELETE /v1/user/connected-apps/{clientId}", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putSession(me, sidMac, uaChromeMac)
		putOfflineFamily(me, "voice-assistant", "fam_offline_1")
		putOfflineFamily(me, "voice-assistant", "fam_offline_2")
		putFamily(me, "voice-assistant", "fam_session", sidMac)
		putOfflineFamily(me, "mcp-connector", "fam_mcp")
	})

	It("ends every login of that client, its session login too, and leaves the other connected apps", func() {
		resp := call(http.MethodDelete, pathConnectedApps+"/voice-assistant", tokenFor(me, scope.Sessions, sidMac))
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		Expect(resp.Body).To(BeEmpty())
		Expect(resp.Headers).To(HaveKeyWithValue("Access-Control-Allow-Origin", "*"))

		Expect(familyClientIDs(me)).To(ConsistOf("mcp-connector"), "only the other app survives")
		Expect(connectedAppsBody(call(http.MethodGet, pathConnectedApps, tokenFor(me, scope.Sessions, sidMac))).ConnectedApps).
			To(HaveExactElements(HaveField("ClientID", "mcp-connector")))
	})

	It("404s a second disconnect, and a client that holds only session logins, ending none of them (negative)", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		Expect(call(http.MethodDelete, pathConnectedApps+"/voice-assistant", token).StatusCode).To(Equal(http.StatusNoContent))
		again := call(http.MethodDelete, pathConnectedApps+"/voice-assistant", token)
		Expect(again.StatusCode).To(Equal(http.StatusNotFound))
		Expect(again.Body).To(ContainSubstring("No such connected app."))

		putFamily(me, "web-only", "fam_web", sidMac)
		Expect(call(http.MethodDelete, pathConnectedApps+"/web-only", token).StatusCode).To(Equal(http.StatusNotFound))
		Expect(familyClientIDs(me)).To(ContainElement("web-only"), "a client with no offline grant is not a connected app")
	})

	It("404s a client the caller has not connected, even one another person has, and leaves that person's grant alone (negative, cross-user)", func() {
		putOfflineFamily(stranger, "their-assistant", "fam_theirs")

		resp := call(http.MethodDelete, pathConnectedApps+"/their-assistant", tokenFor(me, scope.Sessions, sidMac))
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound), "the same answer as an unknown client, so nothing leaks")
		Expect(familyClientIDs(stranger)).To(ConsistOf("their-assistant"))
	})

	It("405s a method a resource does not offer (negative)", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		Expect(call(http.MethodDelete, pathConnectedApps, token).StatusCode).To(Equal(http.StatusMethodNotAllowed))
		Expect(call(http.MethodGet, pathConnectedApps+"/voice-assistant", token).StatusCode).To(Equal(http.StatusMethodNotAllowed))
		Expect(familyClientIDs(me)).To(HaveLen(4))
	})
})

var _ = Describe("the session API's gates, on the connected apps endpoints", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putThirdPartyClient("va-client")
		putOfflineFamily(me, "voice-assistant", "fam_offline")
	})

	DescribeTable("refuses, and disconnects nothing",
		func(token func() string, status int, body string) {
			for _, probe := range []struct{ method, path string }{
				{http.MethodGet, pathConnectedApps},
				{http.MethodDelete, pathConnectedApps + "/voice-assistant"},
			} {
				resp := call(probe.method, probe.path, token())
				Expect(resp.StatusCode).To(Equal(status), probe.method)
				Expect(resp.Body).To(ContainSubstring(body), probe.method)
			}
			Expect(familyClientIDs(me)).To(ConsistOf("voice-assistant"))
		},
		Entry("a token without account:sessions", func() string { return tokenFor(me, "openid email offline_access", sidMac) }, http.StatusForbidden, "insufficient_scope"),
		Entry("a third-party client's token, even with the scope", func() string { return tokenForClient(me, "va-client", scope.Sessions, sidMac) }, http.StatusForbidden, "access_denied"),
	)
})

var _ = Describe("the connected apps endpoints, when storage fails", func() {
	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(ctx())
		putFirstPartyClient(dashboardClient)
		putOfflineFamily(me, "voice-assistant", "fam_offline")
		putFamily(me, "voice-assistant", "fam_session", sidMac)
	})

	failQueries := func() {
		awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock})
		DeferCleanup(awscommon.SetDynamoDBClient, backend.DBMock)
	}

	It("answers 500 server_error when the grants cannot be listed (negative)", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		failQueries()

		resp := call(http.MethodGet, pathConnectedApps, token)
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Body).To(ContainSubstring("server_error"))
	})

	It("answers 500 server_error and disconnects nothing when the grants cannot be listed (negative)", func() {
		token := tokenFor(me, scope.Sessions, sidMac)
		failQueries()

		resp := call(http.MethodDelete, pathConnectedApps+"/voice-assistant", token)
		Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(resp.Body).To(ContainSubstring("server_error"))

		awscommon.SetDynamoDBClient(backend.DBMock)
		Expect(familyClientIDs(me)).To(ConsistOf("voice-assistant", "voice-assistant"))
	})
})
