// SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package signout_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSignout(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Signout Suite")
}

const (
	ravi       = "ravi"
	asha       = "asha"
	homeApp    = "rainmaker-neo-home-app-client"
	appB       = "app-b-client"
	webApp     = "example-web-app"
	alexa      = "rainmaker-neo-alexa-client"
	mcp        = "mcp-oauth-client"
	offline    = "openid offline_access"
	sessionful = "openid"
)

var profiles = map[string]*mock.Profile{}

var _ = AfterSuite(func() {
	for name, profile := range profiles {
		file, err := test_utils.CreateCommonSummaryFile("signout_" + uuid.New().String() + ".txt")
		if err != nil {
			continue
		}
		fmt.Fprintf(file, "\n--- %s ---\n", name)
		profile.Print(file)
		file.Close()
	}
})

// The design page's example person: two browser sessions, two native app sessions, a web login in each browser, a login per app, and two connected apps; asha is someone else whom no sign-out of ravi's may touch.
var _ = Describe("signout.Service", func() {
	var (
		backend *test_utils.EspUserBackend
		rmngCtx *rmngctx.RmngContext
		tokens  map[string]string
	)

	label := func(f refresh_tokens_db.FamilyEntry) string {
		return f.ClientID + "@" + cmp.Or(f.SID, "offline")
	}
	families := func(userID string) []string {
		rows, err := refresh_tokens_db.NewRefreshTokensDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		labels := make([]string, 0, len(rows))
		for i := range rows {
			labels = append(labels, label(rows[i]))
		}
		slices.Sort(labels)
		return labels
	}
	sessionSIDs := func(userID string) []string {
		rows, err := sessions_db.NewSessionsDB(rmngCtx).ListByUser(userID)
		Expect(err).NotTo(HaveOccurred())
		sids := make([]string, 0, len(rows))
		for i := range rows {
			sids = append(sids, rows[i].SID)
		}
		slices.Sort(sids)
		return sids
	}
	without := func(all []string, gone ...string) []string {
		return slices.DeleteFunc(slices.Clone(all), func(s string) bool { return slices.Contains(gone, s) })
	}
	putSession := func(userID, sid, origin string) {
		now := time.Now().Unix()
		Expect(sessions_db.NewSessionsDB(rmngCtx).CreateSession(&sessions_db.SessionEntry{
			SessionHash: "hash_" + sid, SID: sid, UserID: userID, Origin: origin,
			ExpiresAt: now + 3600, LastSeenAt: now, CreatedAt: now,
		})).To(Succeed())
	}
	mint := func(userID, clientID, scope, sid string) {
		token, err := refreshtoken.NewService(rmngCtx).MintRefreshtoken(userID, clientID, scope, nil, sid, 0)
		Expect(err).NotTo(HaveOccurred())
		tokens[userID+"/"+clientID+"@"+cmp.Or(sid, "offline")] = token
	}

	raviFamilies := []string{
		alexa + "@offline", appB + "@appSession2", webApp + "@browserSession1", webApp + "@browserSession2", mcp + "@offline", homeApp + "@appSession1",
	}
	raviSessions := []string{"appSession1", "appSession2", "browserSession1", "browserSession2"}
	ashaFamilies := []string{alexa + "@offline", webApp + "@ashaBrowser"}
	slices.Sort(raviFamilies)
	slices.Sort(ashaFamilies)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		backend.DBMock.MaxPageItems = 1
		rmngCtx = &rmngctx.RmngContext{Context: context.Background()}
		tokens = map[string]string{}

		putSession(ravi, "browserSession1", "browser")
		putSession(ravi, "browserSession2", "browser")
		putSession(ravi, "appSession1", "app")
		putSession(ravi, "appSession2", "app")
		putSession(asha, "ashaBrowser", "browser")

		mint(ravi, homeApp, sessionful, "appSession1")
		mint(ravi, appB, sessionful, "appSession2")
		mint(ravi, webApp, sessionful, "browserSession1")
		mint(ravi, webApp, sessionful, "browserSession2")
		mint(ravi, alexa, offline, "")
		mint(ravi, mcp, offline, "")
		mint(asha, webApp, sessionful, "ashaBrowser")
		mint(asha, alexa, offline, "")

		Expect(families(ravi)).To(Equal(raviFamilies))
		Expect(sessionSIDs(ravi)).To(Equal(raviSessions))
	})

	// readsWrites pins the DynamoDB calls with every listing paged one item at a time, so an extra read or a stray delete shows here.
	DescribeTable("each sign-out ends exactly its reach, and nothing of another person's",
		func(signOut func(*signout.Service) error, goneFamilies, goneSessions []string, readsWrites [2]int) {
			backend.DBMock.ProfileReset()
			Expect(signOut(signout.NewService(rmngCtx))).To(Succeed())
			profile := backend.DBMock.ProfileGet()
			profiles[CurrentSpecReport().LeafNodeText] = &profile
			reads, writes := profile.TotalCounts()
			Expect([2]int{reads, writes}).To(Equal(readsWrites))

			Expect(families(ravi)).To(Equal(without(raviFamilies, goneFamilies...)))
			Expect(sessionSIDs(ravi)).To(Equal(without(raviSessions, goneSessions...)))
			Expect(families(asha)).To(Equal(ashaFamilies))
			Expect(sessionSIDs(asha)).To(Equal([]string{"ashaBrowser"}))
		},
		Entry("1. App 1 signs itself out: its login and its app session, nothing else",
			func(s *signout.Service) error {
				return s.RevokeFamily(homeApp, tokens[ravi+"/"+homeApp+"@appSession1"])
			},
			[]string{homeApp + "@appSession1"}, []string{"appSession1"}, [2]int{8, 2}),
		Entry("2. One browser session signs out: that browser's web login; both apps and the connected apps stay",
			func(s *signout.Service) error { return s.EndSession(ravi, "browserSession1") },
			[]string{webApp + "@browserSession1"}, []string{"browserSession1"}, [2]int{7, 2}),
		Entry("3. One app session is removed from the list: that app alone",
			func(s *signout.Service) error { return s.EndSession(ravi, "appSession2") },
			[]string{appB + "@appSession2"}, []string{"appSession2"}, [2]int{7, 2}),
		Entry("4. A connected app is disconnected: its grant alone",
			func(s *signout.Service) error { return s.DisconnectClient(ravi, alexa) },
			[]string{alexa + "@offline"}, nil, [2]int{1, 1}),
		Entry("5 and 7. Sign out everywhere or a password reset: every session and its logins; connected apps stay",
			func(s *signout.Service) error { return s.EndAllSessions(ravi) },
			without(raviFamilies, alexa+"@offline", mcp+"@offline"), raviSessions, [2]int{10, 8}),
		Entry("6. A password change: everything, connected apps included",
			func(s *signout.Service) error { return s.EndAllSessionsAndConnectedApps(ravi) },
			raviFamilies, raviSessions, [2]int{10, 10}),
	)

	Describe("RevokeFamily", func() {
		It("keeps an app session while another login still carries its sid", func() {
			mint(ravi, appB, sessionful, "appSession1")
			Expect(signout.NewService(rmngCtx).RevokeFamily(homeApp, tokens[ravi+"/"+homeApp+"@appSession1"])).To(Succeed())
			Expect(sessionSIDs(ravi)).To(ContainElement("appSession1"))
		})

		It("never removes a browser session, whose cookie can still sign in", func() {
			Expect(signout.NewService(rmngCtx).RevokeFamily(webApp, tokens[ravi+"/"+webApp+"@browserSession1"])).To(Succeed())
			Expect(families(ravi)).To(Equal(without(raviFamilies, webApp+"@browserSession1")))
			Expect(sessionSIDs(ravi)).To(Equal(raviSessions))
		})

		It("refuses another client's token and ends nothing (negative)", func() {
			err := signout.NewService(rmngCtx).RevokeFamily(appB, tokens[ravi+"/"+homeApp+"@appSession1"])
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshClientMismatch))
			Expect(families(ravi)).To(Equal(raviFamilies))
			Expect(sessionSIDs(ravi)).To(Equal(raviSessions))
		})

		It("succeeds for a login already revoked", func() {
			svc := signout.NewService(rmngCtx)
			Expect(svc.RevokeFamily(homeApp, tokens[ravi+"/"+homeApp+"@appSession1"])).To(Succeed())
			Expect(svc.RevokeFamily(homeApp, tokens[ravi+"/"+homeApp+"@appSession1"])).To(Succeed())
		})

		It("still ends the login when the app session cannot be read", func() {
			awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock, IndexName: sessions_db.SessionsByUserIndex})
			Expect(signout.NewService(rmngCtx).RevokeFamily(homeApp, tokens[ravi+"/"+homeApp+"@appSession1"])).To(Succeed())
			awscommon.SetDynamoDBClient(backend.DBMock)
			Expect(families(ravi)).To(Equal(without(raviFamilies, homeApp+"@appSession1")))
		})
	})

	Describe("EndSession", func() {
		It("answers ErrSessionNotFound for another person's session and ends nothing of theirs (negative, cross-user)", func() {
			Expect(signout.NewService(rmngCtx).EndSession(ravi, "ashaBrowser")).To(MatchError(sessions_db.ErrSessionNotFound))
			Expect(families(asha)).To(Equal(ashaFamilies))
			Expect(sessionSIDs(asha)).To(Equal([]string{"ashaBrowser"}))
		})

		It("leaves the session in place when its logins cannot be ended, so nothing is left signed in under a deleted row (negative)", func() {
			awscommon.SetDynamoDBClient(&mock.FailingDeleteItem{DynamoDBMock: backend.DBMock, TableName: "espuser-refresh-tokens"})
			Expect(signout.NewService(rmngCtx).EndSession(ravi, "browserSession1")).NotTo(Succeed())
			awscommon.SetDynamoDBClient(backend.DBMock)
			Expect(sessionSIDs(ravi)).To(Equal(raviSessions))
		})
	})

	Describe("DisconnectClient", func() {
		It("ends the client's session logins too, and an app session left with none", func() {
			mint(ravi, appB, offline, "")
			Expect(signout.NewService(rmngCtx).DisconnectClient(ravi, appB)).To(Succeed())
			Expect(families(ravi)).To(Equal(without(raviFamilies, appB+"@appSession2")))
			Expect(sessionSIDs(ravi)).To(Equal(without(raviSessions, "appSession2")))
		})

		It("answers ErrNotConnected for a client holding only session logins and ends nothing (negative)", func() {
			Expect(signout.NewService(rmngCtx).DisconnectClient(ravi, homeApp)).To(MatchError(signout.ErrNotConnected))
			Expect(families(ravi)).To(Equal(raviFamilies))
		})

		It("answers ErrNotConnected for a client only someone else connected (negative, cross-user)", func() {
			mint(asha, "asha-only-client", offline, "")
			Expect(signout.NewService(rmngCtx).DisconnectClient(ravi, "asha-only-client")).To(MatchError(signout.ErrNotConnected))
			Expect(families(asha)).To(ContainElement("asha-only-client@offline"))
		})

		It("answers ErrNotConnected for a client whose only offline grant has expired, and ends nothing (negative)", func() {
			Expect(refresh_tokens_db.NewRefreshTokensDB(rmngCtx).CreateFamily(&refresh_tokens_db.FamilyEntry{
				UserID: ravi, ClientID: "lapsed-assistant", FamilyID: "fam_lapsed", Scope: offline, ExpiresOn: time.Now().Unix() - 60,
			})).To(Succeed())
			Expect(signout.NewService(rmngCtx).DisconnectClient(ravi, "lapsed-assistant")).To(MatchError(signout.ErrNotConnected))
			Expect(families(ravi)).To(ContainElement("lapsed-assistant@offline"))
		})
	})

	Describe("ending every session", func() {
		It("has already ended the logins when the session step fails, and a retry finishes the job (negative)", func() {
			awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock, IndexName: sessions_db.SessionsByUserIndex})
			Expect(signout.NewService(rmngCtx).EndAllSessions(ravi)).NotTo(Succeed())
			awscommon.SetDynamoDBClient(backend.DBMock)
			Expect(families(ravi)).To(ConsistOf(alexa+"@offline", mcp+"@offline"))
			Expect(sessionSIDs(ravi)).To(Equal(raviSessions))

			Expect(signout.NewService(rmngCtx).EndAllSessions(ravi)).To(Succeed())
			Expect(sessionSIDs(ravi)).To(BeEmpty())
		})

		It("deletes no session when the logins cannot be read (negative)", func() {
			awscommon.SetDynamoDBClient(&mock.FailingIndexQuery{DynamoDBMock: backend.DBMock})
			Expect(signout.NewService(rmngCtx).EndAllSessionsAndConnectedApps(ravi)).NotTo(Succeed())
			awscommon.SetDynamoDBClient(backend.DBMock)
			Expect(families(ravi)).To(Equal(raviFamilies))
			Expect(sessionSIDs(ravi)).To(Equal(raviSessions))
		})

		It("succeeds for a person with nothing to end", func() {
			Expect(signout.NewService(rmngCtx).EndAllSessionsAndConnectedApps("nobody")).To(Succeed())
		})
	})

})
