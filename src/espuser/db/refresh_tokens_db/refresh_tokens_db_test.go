// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package refresh_tokens_db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRefreshTokensDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RefreshTokensDB Suite")
}

func clientFamilies(rows []refresh_tokens_db.FamilyEntry) []string {
	out := make([]string, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].ClientFamily)
	}
	return out
}

var _ = Describe("RefreshTokensDB", func() {
	var (
		backend *test_utils.EspUserBackend
		db      *refresh_tokens_db.RefreshTokensDB
		now     int64
	)

	create := func(userID, clientID, familyID, sid string) {
		Expect(db.CreateFamily(&refresh_tokens_db.FamilyEntry{
			UserID: userID, ClientID: clientID, FamilyID: familyID, SID: sid, Scope: "openid", ExpiresOn: now + 3600,
		})).To(Succeed())
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		db = refresh_tokens_db.NewRefreshTokensDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
		now = time.Now().Unix()
	})

	Describe("CreateFamily and GetFamily", func() {
		It("composes the sort key from client and family and starts at counter 0", func() {
			create("u1", "c1", "f1", "sid-1")

			got, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ClientFamily).To(Equal("c1#f1"))
			Expect(got.ClientID).To(Equal("c1"))
			Expect(got.FamilyID).To(Equal("f1"))
			Expect(got.Counter).To(BeZero())
			Expect(got.SID).To(Equal("sid-1"))
		})

		It("refuses a family that already exists and keeps the first", func() {
			create("u1", "c1", "f1", "sid-1")

			err := db.CreateFamily(&refresh_tokens_db.FamilyEntry{UserID: "u1", ClientID: "c1", FamilyID: "f1", SID: "sid-2"})
			Expect(err).To(MatchError("failed to create refresh token family"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			got, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.SID).To(Equal("sid-1"))
		})

		It("returns ErrRefreshNotFound for an unknown family", func() {
			_, err := db.GetFamily("u1", "c1", "missing")
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshNotFound))
		})

		It("reports a failed read as an error, not as not-found", func() {
			boom := errors.New("throttled")
			backend.DBMock.NextGetItemError = boom

			_, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).To(MatchError("failed to get refresh token family"))
			Expect(errors.Is(err, boom)).To(BeTrue())
		})
	})

	Describe("AdvanceCounter", func() {
		BeforeEach(func() {
			create("u1", "c1", "f1", "")
		})

		It("moves the counter from expected to expected+1 and stamps expiry and rotation", func() {
			Expect(db.AdvanceCounter("u1", "c1", "f1", 0, now+7200, now)).To(Succeed())

			got, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Counter).To(Equal(int64(1)))
			Expect(got.ExpiresOn).To(Equal(now + 7200))
			Expect(got.RotatedAt).To(Equal(now))
			Expect(got.Scope).To(Equal("openid"))
		})

		It("fails on a stale counter and leaves the row alone", func() {
			Expect(db.AdvanceCounter("u1", "c1", "f1", 0, now+7200, now)).To(Succeed())

			err := db.AdvanceCounter("u1", "c1", "f1", 0, now+9000, now+1)
			Expect(err).To(MatchError("failed to advance refresh token counter"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			got, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Counter).To(Equal(int64(1)))
			Expect(got.ExpiresOn).To(Equal(now + 7200))
		})

		It("fails and creates nothing for an unknown family", func() {
			err := db.AdvanceCounter("u1", "c1", "missing", 0, now+7200, now)
			Expect(err).To(MatchError("failed to advance refresh token counter"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			_, err = db.GetFamily("u1", "c1", "missing")
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshNotFound))
		})
	})

	Describe("DeleteFamily", func() {
		It("deletes an existing family", func() {
			create("u1", "c1", "f1", "")

			Expect(db.DeleteFamily("u1", "c1", "f1")).To(Succeed())

			_, err := db.GetFamily("u1", "c1", "f1")
			Expect(err).To(MatchError(refresh_tokens_db.ErrRefreshNotFound))
		})

		It("fails on an unknown family", func() {
			err := db.DeleteFamily("u1", "c1", "missing")
			Expect(err).To(MatchError("failed to delete refresh token family"))
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
		})
	})

	Describe("ListByUser", func() {
		BeforeEach(func() {
			backend.DBMock.MaxPageItems = 1
		})

		It("returns every family of the user across pages, in sort-key order", func() {
			create("u1", "c2", "f1", "")
			create("u1", "c1", "f2", "")
			create("u1", "c1", "f1", "")
			create("u2", "c1", "f1", "")

			rows, err := db.ListByUser("u1")
			Expect(err).NotTo(HaveOccurred())
			Expect(clientFamilies(rows)).To(Equal([]string{"c1#f1", "c1#f2", "c2#f1"}))
		})

		It("returns nil for a user with no families", func() {
			rows, err := db.ListByUser("nobody")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeNil())
		})
	})

	Describe("DeleteAllForUser", func() {
		It("deletes every family of the user and no one else's", func() {
			backend.DBMock.MaxPageItems = 1
			create("u1", "c1", "f1", "")
			create("u1", "c2", "f2", "")
			create("u2", "c1", "f3", "")

			Expect(db.DeleteAllForUser("u1")).To(Succeed())

			Expect(db.ListByUser("u1")).To(BeEmpty())
			Expect(db.ListByUser("u2")).To(HaveLen(1))
		})

		It("succeeds for a user with no families", func() {
			Expect(db.DeleteAllForUser("nobody")).To(Succeed())
		})
	})

	Describe("DeleteBySID", func() {
		BeforeEach(func() {
			create("u1", "c1", "f1", "sid-a")
			create("u1", "c2", "f2", "sid-a")
			create("u1", "c1", "f3", "sid-b")
			create("u1", "c1", "f4", "")
			create("u2", "c1", "f5", "sid-a")
		})

		It("deletes only the user's families created under the sid and counts them", func() {
			Expect(db.DeleteBySID("u1", "sid-a")).To(Equal(2))

			rows, err := db.ListByUser("u1")
			Expect(err).NotTo(HaveOccurred())
			Expect(clientFamilies(rows)).To(Equal([]string{"c1#f3", "c1#f4"}))
			Expect(db.ListByUser("u2")).To(HaveLen(1))
		})

		It("deletes nothing for an empty sid", func() {
			Expect(db.DeleteBySID("u1", "")).To(BeZero())

			Expect(db.ListByUser("u1")).To(HaveLen(4))
		})

		It("deletes nothing for an unknown sid", func() {
			Expect(db.DeleteBySID("u1", "sid-z")).To(BeZero())

			Expect(db.ListByUser("u1")).To(HaveLen(4))
		})
	})
})
