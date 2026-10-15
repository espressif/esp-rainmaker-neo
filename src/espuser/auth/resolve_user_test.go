// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/user_details_db"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResolveUser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Identity Resolution Suite")
}

var _ = Describe("ResolveOrCreateUserByContacts", func() {
	const (
		email = "ada@example.com"
		phone = "+15550100"
	)
	var (
		svc     *auth.OAuthUserAuthService
		rmngCtx *rmngctx.RmngContext
		backend *test_utils.EspUserBackend
	)

	BeforeEach(func() {
		backend = test_utils.SetupEspUserBackend(context.Background())
		var err error
		svc, err = auth.NewOAuthUserAuthService(context.Background())
		Expect(err).NotTo(HaveOccurred())
		rmngCtx = rmngctx.NewRmngContextWithCtx(context.Background(), nil)
	})

	entryFor := func(userID string) *user_details_db.UserDetailsEntry {
		e, err := user_details_db.NewUserDetailsDB(rmngCtx).GetUserDetailsByUserID(userID)
		Expect(err).NotTo(HaveOccurred())
		return e
	}

	It("stores both contacts when a login vouches for both", func() {
		userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, nil)
		Expect(err).NotTo(HaveOccurred())

		stored := entryFor(userID)
		Expect(stored.Email).To(Equal(email))
		Expect(stored.PhoneNumber).To(Equal(phone))
	})

	It("mints an opaque id that carries neither contact", func() {
		userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(userID).NotTo(BeEmpty())
		// An id computed from a contact would move the moment that contact changed.
		Expect(userID).NotTo(ContainSubstring(email))
		Expect(userID).NotTo(ContainSubstring("example.com"))
		Expect(userID).NotTo(ContainSubstring(phone))
	})

	It("gives two different accounts different ids", func() {
		first, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", nil)
		Expect(err).NotTo(HaveOccurred())
		second, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "other@example.com", "", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).NotTo(Equal(first))
	})

	It("finds the same user by phone after the account was created with both", func() {
		created, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, nil)
		Expect(err).NotTo(HaveOccurred())

		byPhone, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "", phone, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(byPhone).To(Equal(created), "a phone login must not create a second account")
	})

	It("backfills the email when the account was created by phone alone", func() {
		byPhone, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "", phone, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(entryFor(byPhone).Email).To(BeEmpty())

		// The same person, now with a verified email: the account must be reused, not duplicated.
		both, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(both).To(Equal(byPhone))
		Expect(entryFor(byPhone).Email).To(Equal(email))
	})

	It("keeps one account when a verified email appears on a later login (no silent switch)", func() {
		first, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "", phone, nil)
		Expect(err).NotTo(HaveOccurred())

		second, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).NotTo(Equal(first),
			"with no shared contact there is nothing to match on, so this is a distinct account")
	})

	It("refuses to guess when the two contacts already belong to different accounts (negative)", func() {
		byEmail, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", nil)
		Expect(err).NotTo(HaveOccurred())
		byPhone, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "", phone, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(byEmail).NotTo(Equal(byPhone))

		_, err = svc.ResolveOrCreateUserByContacts(rmngCtx, email, phone, nil)
		Expect(err).To(MatchError(auth.ErrContactsOnDifferentUsers))
	})

	Context("when the provider row maps sub", func() {
		const upstreamSub = "Google_StNysuBee2yLHzxLiY7PHo"

		upstream := func(provider, sub, externalSub string) *user_details_db.UpstreamProfile {
			return &user_details_db.UpstreamProfile{
				Provider:    provider,
				ExternalSub: externalSub,
				Sub:         sub,
				Name:        "Ada",
			}
		}

		It("opens the account under that sub, recording the provider and the upstream's own sub", func() {
			userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(userID).To(Equal(upstreamSub))
			entry := entryFor(userID)
			Expect(entry.Provider).To(Equal("cognito"))
			Expect(entry.ExternalSub).To(Equal("cog-sub-1"))
			Expect(entry.Name).To(Equal("Ada"))
		})

		It("mints an id when the profile carries no sub, still recording the upstream's own sub", func() {
			userID, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", "", "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(userID).NotTo(BeEmpty())
			Expect(entryFor(userID).ExternalSub).To(Equal("cog-sub-1"))
		})

		It("keeps an existing account's id: a matching contact wins over the upstream sub", func() {
			existing, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", nil)
			Expect(err).NotTo(HaveOccurred())

			again, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(again).To(Equal(existing))
		})

		It("refuses a taken user_id even to its owner once no contact matches (negative)", func() {
			_, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ResolveOrCreateUserByContacts(rmngCtx, "ada.new@example.com", "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).To(MatchError(auth.ErrUpstreamUserIDUnusable))
		})

		It("refuses another upstream identity naming a taken user_id, and leaves that user untouched (negative)", func() {
			holder, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ResolveOrCreateUserByContacts(rmngCtx, "attacker@example.com", "", upstream("cognito", upstreamSub, "cog-sub-attacker"))
			Expect(err).To(MatchError(auth.ErrUpstreamUserIDUnusable))
			Expect(entryFor(holder).Email).To(Equal(email))
		})

		It("refuses another provider naming a taken user_id (negative)", func() {
			_, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ResolveOrCreateUserByContacts(rmngCtx, "attacker@example.com", "", upstream("other-idp", upstreamSub, "cog-sub-1"))
			Expect(err).To(MatchError(auth.ErrUpstreamUserIDUnusable))
		})

		It("refuses an upstream naming a minted account (negative)", func() {
			minted, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", nil)
			Expect(err).NotTo(HaveOccurred())

			_, err = svc.ResolveOrCreateUserByContacts(rmngCtx, "attacker@example.com", "", upstream("cognito", minted, "cog-sub-attacker"))
			Expect(err).To(MatchError(auth.ErrUpstreamUserIDUnusable))
		})

		It("refuses a sub that cannot be an OIDC sub (negative)", func() {
			_, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", strings.Repeat("a", 256), "cog-sub-1"))
			Expect(err).To(MatchError(auth.ErrUpstreamUserIDUnusable))
		})

		It("reports a storage failure as an error, not as a refusal (negative)", func() {
			backend.DBMock.PutItemErr = errors.New("throttled")
			_, err := svc.ResolveOrCreateUserByContacts(rmngCtx, email, "", upstream("cognito", upstreamSub, "cog-sub-1"))
			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(auth.ErrUpstreamUserIDUnusable))
		})
	})

	It("requires at least one verified contact (negative)", func() {
		_, err := svc.ResolveOrCreateUserByContacts(rmngCtx, "", "", nil)
		Expect(err).To(HaveOccurred())
	})
})
