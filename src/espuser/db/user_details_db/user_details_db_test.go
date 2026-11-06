// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_details_db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ddbu "github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUserDetailsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UserDetailsDB Suite")
}

var _ = Describe("UserDetailsDB", func() {
	var (
		backend *test_utils.EspUserBackend
		db      *user_details_db.UserDetailsDB
	)

	create := func(userID, email, phone string) {
		Expect(db.CreateUserDetails(&user_details_db.UserDetailsEntry{
			UserID: userID, Email: email, PhoneNumber: phone, Name: "Name " + userID, UserType: user_details_db.UserTypeUser,
		})).To(Succeed())
	}

	stored := func(userID string) *user_details_db.UserDetailsEntry {
		entry, err := db.GetUserDetailsByUserID(userID)
		Expect(err).NotTo(HaveOccurred())
		return entry
	}

	asAccessor := func(userID string) *user_details_db.UserDetailsDB {
		return user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), user.NewUser(userID)))
	}

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		db = user_details_db.NewUserDetailsDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	})

	Describe("CreateUserDetails", func() {
		It("stores the email in canonical lowercased form", func() {
			create("u1", "  Alice@Example.COM ", "")

			Expect(stored("u1").Email).To(Equal("alice@example.com"))
		})

		It("defaults the user id to the accessor", func() {
			entry := &user_details_db.UserDetailsEntry{Email: "a@example.com"}
			Expect(asAccessor("u-acc").CreateUserDetails(entry)).To(Succeed())

			Expect(entry.UserID).To(Equal("u-acc"))
			Expect(stored("u-acc").Email).To(Equal("a@example.com"))
		})

		It("refuses a second entry for the same user and keeps the first", func() {
			create("u1", "a@example.com", "")

			err := db.CreateUserDetails(&user_details_db.UserDetailsEntry{UserID: "u1", Email: "b@example.com"})
			Expect(err).To(MatchError(user_details_db.ErrUserIDTaken))
			Expect(stored("u1").Email).To(Equal("a@example.com"))
		})
	})

	Describe("GetUserDetails", func() {
		It("reads the accessor's own entry", func() {
			create("u-acc", "a@example.com", "+15550000001")

			entry, err := asAccessor("u-acc").GetUserDetails()
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.Email).To(Equal("a@example.com"))
			Expect(entry.PhoneNumber).To(Equal("+15550000001"))
			Expect(entry.Name).To(Equal("Name u-acc"))
		})

		It("returns ErrUserNotFound when the accessor has no entry", func() {
			_, err := asAccessor("nobody").GetUserDetails()
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
		})

		It("reports a failed read as an error, not as not-found", func() {
			boom := errors.New("throttled")
			backend.DBMock.NextGetItemError = boom

			_, err := asAccessor("u-acc").GetUserDetails()
			Expect(err).To(MatchError("failed to get user details"))
			Expect(errors.Is(err, boom)).To(BeTrue())
		})
	})

	Describe("GetUserDetailsByUserID", func() {
		It("returns ErrUserNotFound for an unknown user", func() {
			_, err := db.GetUserDetailsByUserID("nobody")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
		})

		It("reports a failed read as an error, not as not-found", func() {
			boom := errors.New("throttled")
			backend.DBMock.NextGetItemError = boom

			_, err := db.GetUserDetailsByUserID("u1")
			Expect(err).To(MatchError("failed to get user details by user id"))
			Expect(errors.Is(err, boom)).To(BeTrue())
		})
	})

	Describe("contact lookups", func() {
		BeforeEach(func() {
			create("u1", "alice@example.com", "+15550000001")
			create("u2", "bob@example.com", "")
		})

		It("resolves an email in any case to the account and both its contacts", func() {
			contact, err := db.LookupContactByEmail(" ALICE@example.com")
			Expect(err).NotTo(HaveOccurred())
			Expect(*contact).To(Equal(user_details_db.UserContactEntry{UserID: "u1", Email: "alice@example.com", PhoneNumber: "+15550000001"}))
		})

		It("resolves a phone number to the account and both its contacts", func() {
			contact, err := db.LookupContactByPhoneNumber("+15550000001")
			Expect(err).NotTo(HaveOccurred())
			Expect(*contact).To(Equal(user_details_db.UserContactEntry{UserID: "u1", Email: "alice@example.com", PhoneNumber: "+15550000001"}))
		})

		It("resolves an email or a phone number to the user id", func() {
			Expect(db.LookupUserIDByEmail("Bob@Example.com")).To(Equal("u2"))
			Expect(db.LookupUserIDByPhoneNumber("+15550000001")).To(Equal("u1"))
		})

		It("returns ErrUserNotFound for an unknown contact", func() {
			_, err := db.LookupContactByEmail("nobody@example.com")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
			_, err = db.LookupContactByPhoneNumber("+15559999999")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))

			id, err := db.LookupUserIDByEmail("nobody@example.com")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
			Expect(id).To(BeEmpty())
			id, err = db.LookupUserIDByPhoneNumber("+15559999999")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
			Expect(id).To(BeEmpty())
		})
	})

	Describe("BatchGetUserDetailsByIDs", func() {
		It("returns nil for no ids", func() {
			Expect(db.BatchGetUserDetailsByIDs(nil)).To(BeNil())
		})

		It("returns only the users that exist, projected to id, email and phone", func() {
			create("u1", "alice@example.com", "+15550000001")
			create("u2", "bob@example.com", "")

			rows, err := db.BatchGetUserDetailsByIDs([]string{"u1", "u2", "u3"})
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(ConsistOf(
				user_details_db.UserDetailsEntry{UserID: "u1", Email: "alice@example.com", PhoneNumber: "+15550000001"},
				user_details_db.UserDetailsEntry{UserID: "u2", Email: "bob@example.com"},
			))
		})

		It("returns nil when no id exists", func() {
			Expect(db.BatchGetUserDetailsByIDs([]string{"x1", "x2"})).To(BeNil())
		})

		It("reads past the 100-key batch limit", func() {
			ids := make([]string, 0, 150)
			for i := 0; i < 150; i++ {
				id := fmt.Sprintf("u%03d", i)
				ids = append(ids, id)
				if i < 120 {
					create(id, id+"@example.com", "")
				}
			}

			Expect(db.BatchGetUserDetailsByIDs(ids)).To(HaveLen(120))
		})
	})

	Describe("RecordVerifiedContacts", func() {
		It("writes nothing when given no contact", func() {
			Expect(db.RecordVerifiedContacts("nobody", "", "")).To(Succeed())

			_, err := db.GetUserDetailsByUserID("nobody")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
		})

		It("adds a missing email in canonical form and keeps the phone", func() {
			create("u1", "", "+15550000001")

			Expect(db.RecordVerifiedContacts("u1", "Bob@Example.com", "")).To(Succeed())

			entry := stored("u1")
			Expect(entry.Email).To(Equal("bob@example.com"))
			Expect(entry.PhoneNumber).To(Equal("+15550000001"))
			Expect(entry.Name).To(Equal("Name u1"))
		})

		It("adds both contacts when neither is held", func() {
			create("u1", "", "")

			Expect(db.RecordVerifiedContacts("u1", "a@example.com", "+15550000001")).To(Succeed())

			entry := stored("u1")
			Expect(entry.Email).To(Equal("a@example.com"))
			Expect(entry.PhoneNumber).To(Equal("+15550000001"))
		})

		It("refuses to overwrite a contact the account already holds", func() {
			create("u1", "a@example.com", "")

			err := db.RecordVerifiedContacts("u1", "b@example.com", "")
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
			Expect(stored("u1").Email).To(Equal("a@example.com"))
		})

		It("writes neither contact when one of them is already held", func() {
			create("u1", "a@example.com", "")

			err := db.RecordVerifiedContacts("u1", "b@example.com", "+15550000001")
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())
			entry := stored("u1")
			Expect(entry.Email).To(Equal("a@example.com"))
			Expect(entry.PhoneNumber).To(BeEmpty())
		})

		It("fails and creates nothing for an unknown user", func() {
			err := db.RecordVerifiedContacts("nobody", "a@example.com", "")
			Expect(ddbu.IsConditionFailed(err)).To(BeTrue())

			_, err = db.GetUserDetailsByUserID("nobody")
			Expect(err).To(MatchError(user_details_db.ErrUserNotFound))
		})
	})
})
