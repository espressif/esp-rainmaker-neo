// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package sender_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/sender"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSenderService(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Email Sender Service Suite")
}

var _ = Describe("sender.Service", func() {
	const global = sender.CategoryGlobal

	var (
		ctx     context.Context
		sesMock *mock.MockSES
		svc     *sender.Service
		config  *admin_config_db.AdminConfigDB
	)

	BeforeEach(func() {
		ctx = context.Background()
		rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), nil)
		sesMock = test_utils.SetupEspUserBackend(ctx, test_utils.EspUserBackendOpts{WithSES: true}).SESMock
		svc = sender.NewService(rmngCtx)
		config = admin_config_db.NewAdminConfigDB(rmngCtx)
	})

	// SES owns which identities exist, so the mock stands in for the account's identities.
	addIdentity := func(address string) {
		_, err := sesMock.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{
			EmailIdentity: aws.String(address),
		})
		Expect(err).NotTo(HaveOccurred())
	}

	// The active choice is a plain admin-config row; nothing in the send path writes it.
	selectActive := func(category, address string) {
		Expect(config.Put(admin_config_db.ConfigEmailSender, category, address, time.Now().Unix())).To(Succeed())
	}

	activeSenders := func() sender.ActiveSenders {
		active, err := svc.GetActiveSenders(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		return active
	}

	Describe("GetActiveSenders", func() {
		It("returns an empty map when no sender exists at all", func() {
			Expect(activeSenders().Senders).To(BeEmpty())
		})

		It("returns the explicitly selected sender", func() {
			addIdentity("chosen@example.com")
			sesMock.SetVerified("chosen@example.com")
			selectActive(global, "chosen@example.com")
			Expect(activeSenders().Senders[global]).To(Equal("chosen@example.com"))
		})

		It("lists identities from SES when the caller passes no hint", func() {
			addIdentity("only@example.com")
			sesMock.SetVerified("only@example.com")
			// senders == nil, so the fallback has to fetch them itself.
			active, err := svc.GetActiveSenders(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(active.Senders[global]).To(Equal("only@example.com"))
		})
	})

	Describe("fallback to the sole verified sender", func() {
		It("uses it when none is explicitly active", func() {
			addIdentity("only@example.com")
			sesMock.SetVerified("only@example.com")
			Expect(activeSenders().Senders[global]).To(Equal("only@example.com"))
		})

		It("does not fall back when more than one is verified (ambiguous)", func() {
			for _, a := range []string{"a@example.com", "b@example.com"} {
				addIdentity(a)
				sesMock.SetVerified(a)
			}
			Expect(activeSenders().Senders).NotTo(HaveKey(global))
		})

		It("does not fall back when the sole identity is unverified", func() {
			addIdentity("pending@example.com")
			Expect(activeSenders().Senders).NotTo(HaveKey(global))
		})

		It("prefers an explicit selection over the fallback", func() {
			for _, a := range []string{"chosen@example.com", "other@example.com"} {
				addIdentity(a)
				sesMock.SetVerified(a)
			}
			selectActive(global, "chosen@example.com")
			Expect(activeSenders().Senders[global]).To(Equal("chosen@example.com"))
		})

		It("surfaces an SES listing failure rather than reporting no sender", func() {
			sesMock.ListEmailIdentitiesError = errors.New("ses unavailable")
			_, err := svc.GetActiveSenders(ctx, nil)
			Expect(err).To(HaveOccurred())
		})
	})
})
