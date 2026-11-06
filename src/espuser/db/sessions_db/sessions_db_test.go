// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package sessions_db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/sessions_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSessionsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SessionsDB Suite")
}

func sids(rows []sessions_db.SessionEntry) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].SID)
	}
	return out
}

var _ = Describe("SessionsDB", func() {
	var (
		backend *test_utils.EspUserBackend
		db      *sessions_db.SessionsDB
		now     int64
	)

	session := func(hash, userID, sid string, createdAt, expiresAt int64) *sessions_db.SessionEntry {
		return &sessions_db.SessionEntry{SessionHash: hash, UserID: userID, SID: sid, CreatedAt: createdAt, ExpiresAt: expiresAt}
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		db = sessions_db.NewSessionsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		now = time.Now().Unix()
	})

	Describe("CreateSession and GetSession", func() {
		It("round-trips a live session", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+3600))).To(Succeed())

			got, err := db.GetSession("h1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UserID).To(Equal("u1"))
			Expect(got.SID).To(Equal("s1"))
			Expect(got.ExpiresAt).To(Equal(now + 3600))
		})

		It("refuses a second session under the same hash and keeps the first", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+3600))).To(Succeed())

			err := db.CreateSession(session("h1", "u2", "s2", now, now+3600))
			Expect(err).To(MatchError("failed to put session"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			got, err := db.GetSession("h1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UserID).To(Equal("u1"))
		})

		It("returns ErrSessionNotFound for an unknown hash", func() {
			_, err := db.GetSession("missing")
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
		})

		It("returns ErrSessionNotFound for a row past expires_at", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now-1))).To(Succeed())

			_, err := db.GetSession("h1")
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
		})

		It("treats expires_at 0 as no deadline", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, 0))).To(Succeed())

			_, err := db.GetSession("h1")
			Expect(err).NotTo(HaveOccurred())
		})

		It("reports a failed read as an error, not as not-found", func() {
			boom := errors.New("throttled")
			backend.DBMock.NextGetItemError = boom

			_, err := db.GetSession("h1")
			Expect(err).To(MatchError("failed to get session"))
			Expect(errors.Is(err, boom)).To(BeTrue())
		})
	})

	Describe("TouchLiveSession", func() {
		It("rolls every clock on a live row and returns the whole row", func() {
			entry := session("h1", "u1", "s1", now, now+60)
			entry.Provider = "cognito"
			Expect(db.CreateSession(entry)).To(Succeed())

			row, err := db.TouchLiveSession("h1", now, now+3600)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.ExpiresAt).To(Equal(now + 3600))
			Expect(row.CookieExpiresAt).To(Equal(now + 3600))
			Expect(row.LastSeenAt).To(Equal(now))
			Expect(row.Provider).To(Equal("cognito"), "the returned row is the whole item, not only the updated attributes")
		})

		It("returns ErrSessionNotFound and creates nothing for an unknown hash", func() {
			row, err := db.TouchLiveSession("missing", now, now+3600)
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
			Expect(row).To(BeNil())

			_, err = db.GetSession("missing")
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
		})

		It("returns ErrSessionNotFound and leaves an expired row untouched", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now-7200, now-1))).To(Succeed())

			row, err := db.TouchLiveSession("h1", now, now+3600)
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
			Expect(row).To(BeNil())

			var stored sessions_db.SessionEntry
			Expect(backend.DBMock.GetDirect("espuser-sessions", "h1", "", &stored)).To(Succeed())
			Expect(stored.ExpiresAt).To(Equal(now - 1))
			Expect(stored.LastSeenAt).To(BeZero())
		})

		It("clamps to max_expires_at, read from the old row on the failed write, when the request passes the cap", func() {
			entry := session("h1", "u1", "s1", now, now+60)
			entry.MaxExpiresAt = now + 600
			Expect(db.CreateSession(entry)).To(Succeed())

			row, err := db.TouchLiveSession("h1", now, now+3600)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.ExpiresAt).To(Equal(now + 600))
			Expect(row.CookieExpiresAt).To(Equal(now + 600))
			Expect(row.LastSeenAt).To(Equal(now))
		})

		It("does not clamp when the cap lies beyond the request", func() {
			entry := session("h1", "u1", "s1", now, now+60)
			entry.MaxExpiresAt = now + 7200
			Expect(db.CreateSession(entry)).To(Succeed())

			row, err := db.TouchLiveSession("h1", now, now+3600)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.ExpiresAt).To(Equal(now + 3600))
		})
	})

	Describe("TouchExpiryBySID", func() {
		It("is a no-op for an empty user or sid", func() {
			Expect(db.TouchExpiryBySID("", "s1", now, 3600)).To(BeZero())
			Expect(db.TouchExpiryBySID("u1", "", now, 3600)).To(BeZero())
		})

		It("reports the session ended when the user has no session with that sid", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+60))).To(Succeed())

			_, err := db.TouchExpiryBySID("u1", "other", now, 3600)
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
			_, err = db.TouchExpiryBySID("u2", "s1", now, 3600)
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
		})

		It("bumps expires_at and last_seen_at but never cookie_expires_at", func() {
			entry := session("h1", "u1", "s1", now, now+60)
			entry.CookieExpiresAt = now + 60
			Expect(db.CreateSession(entry)).To(Succeed())

			Expect(db.TouchExpiryBySID("u1", "s1", now, 3600)).To(Equal(now + 3600))

			got, err := db.GetSession("h1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ExpiresAt).To(Equal(now + 3600))
			Expect(got.LastSeenAt).To(Equal(now))
			Expect(got.CookieExpiresAt).To(Equal(now + 60))
		})

		It("clamps the bump to max_expires_at", func() {
			entry := session("h1", "u1", "s1", now, now+60)
			entry.MaxExpiresAt = now + 600
			Expect(db.CreateSession(entry)).To(Succeed())

			Expect(db.TouchExpiryBySID("u1", "s1", now, 3600)).To(Equal(now + 600))
		})

		It("reports a failed base-row read as an error", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+60))).To(Succeed())
			backend.DBMock.NextGetItemError = errors.New("throttled")

			_, err := db.TouchExpiryBySID("u1", "s1", now, 3600)
			Expect(err).To(MatchError("failed to read session for retention bump"))
		})
	})

	Describe("DeleteSession", func() {
		It("deletes an existing session", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+60))).To(Succeed())

			Expect(db.DeleteSession("h1")).To(Succeed())

			_, err := db.GetSession("h1")
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
		})

		It("fails on an unknown hash", func() {
			err := db.DeleteSession("missing")
			Expect(err).To(MatchError("failed to delete session"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
		})
	})

	Describe("ListByUser", func() {
		BeforeEach(func() {
			// Mirror the real KEYS_ONLY index and force one item per page, so the list has to page the index and read each row from the base table.
			Expect(backend.DBMock.SetIndexProjection(sessions_db.SessionsByUserIndex, types.ProjectionTypeKeysOnly)).To(Succeed())
			backend.DBMock.MaxPageItems = 1
		})

		It("returns the user's live sessions, newest first", func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now-300, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h2", "u1", "s2", now-100, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h3", "u1", "s3", now-200, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h4", "u1", "s4", now-50, now-1))).To(Succeed())
			Expect(db.CreateSession(session("h5", "u2", "s5", now, now+60))).To(Succeed())

			rows, err := db.ListByUser("u1")
			Expect(err).NotTo(HaveOccurred())
			Expect(sids(rows)).To(Equal([]string{"s2", "s3", "s1"}))
			Expect(rows[0].ExpiresAt).To(Equal(now+60), "rows are read from the base table, not the keys-only index")
		})

		It("returns an empty, non-nil list for a user with no sessions", func() {
			rows, err := db.ListByUser("nobody")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).NotTo(BeNil())
			Expect(rows).To(BeEmpty())
		})
	})

	Describe("DeleteByUserAndSID", func() {
		BeforeEach(func() {
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h2", "u1", "s2", now, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h3", "u2", "s3", now, now+60))).To(Succeed())
		})

		It("deletes only the named session", func() {
			Expect(db.DeleteByUserAndSID("u1", "s1")).To(Succeed())

			_, err := db.GetSession("h1")
			Expect(err).To(MatchError(sessions_db.ErrSessionNotFound))
			_, err = db.GetSession("h2")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrSessionNotFound for another user's sid and leaves it alone", func() {
			Expect(db.DeleteByUserAndSID("u1", "s3")).To(MatchError(sessions_db.ErrSessionNotFound))

			_, err := db.GetSession("h3")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns ErrSessionNotFound for an unknown sid", func() {
			Expect(db.DeleteByUserAndSID("u1", "nope")).To(MatchError(sessions_db.ErrSessionNotFound))
		})
	})

	Describe("DeleteAllForUser", func() {
		It("deletes every session of the user and no one else's", func() {
			backend.DBMock.MaxPageItems = 1
			Expect(db.CreateSession(session("h1", "u1", "s1", now, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h2", "u1", "s2", now, now+60))).To(Succeed())
			Expect(db.CreateSession(session("h3", "u2", "s3", now, now+60))).To(Succeed())

			Expect(db.DeleteAllForUser("u1")).To(Succeed())

			Expect(db.ListByUser("u1")).To(BeEmpty())
			Expect(db.ListByUser("u2")).To(HaveLen(1))
		})

		It("succeeds for a user with no sessions", func() {
			Expect(db.DeleteAllForUser("nobody")).To(Succeed())
		})
	})
})
