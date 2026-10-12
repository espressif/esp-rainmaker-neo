// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package identity_providers_db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/identity_providers_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIdentityProvidersDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IdentityProvidersDB Suite")
}

var _ = Describe("IdentityProvidersDB", func() {
	var (
		dbMock *mock.DynamoDBMock
		db     *identity_providers_db.IdentityProvidersDB
	)

	BeforeEach(func() {
		dbMock = test_utils.SetupEspUserBackend(context.Background()).DBMock
		db = identity_providers_db.NewIdentityProvidersDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	})

	newProvider := func(name string, enabled *bool) *identity_providers_db.ProviderEntry {
		return &identity_providers_db.ProviderEntry{
			ProviderName:     name,
			Type:             identity_providers_db.TypeOIDC,
			DisplayName:      "Provider " + name,
			Enabled:          enabled,
			Issuer:           "https://idp.example/" + name,
			AttributeMapping: map[string]string{"email": "mail"},
			CreatedAt:        100,
		}
	}

	Describe("GetProvider", func() {
		It("returns ErrProviderNotFound for an unknown provider", func() {
			entry, err := db.GetProvider("missing")
			Expect(err).To(BeIdenticalTo(identity_providers_db.ErrProviderNotFound))
			Expect(entry).To(BeNil())
		})

		It("round-trips every stored field, including a stored false Enabled", func() {
			Expect(db.CreateProvider(newProvider("idp_a", utils.Ptr(false)))).To(Succeed())

			got, err := db.GetProvider("idp_a")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Type).To(Equal(identity_providers_db.TypeOIDC))
			Expect(got.DisplayName).To(Equal("Provider idp_a"))
			Expect(got.Enabled).To(Equal(utils.Ptr(false)))
			Expect(got.Issuer).To(Equal("https://idp.example/idp_a"))
			Expect(got.AttributeMapping).To(Equal(map[string]string{"email": "mail"}))
			Expect(got.CreatedAt).To(Equal(int64(100)))
		})
	})

	Describe("CreateProvider", func() {
		It("fails on a duplicate provider_name and leaves the existing row untouched", func() {
			Expect(db.CreateProvider(newProvider("idp_dup", utils.Ptr(true)))).To(Succeed())

			clash := newProvider("idp_dup", utils.Ptr(false))
			clash.DisplayName = "Clobber"
			err := db.CreateProvider(clash)
			Expect(err).To(MatchError("failed to create identity provider"))
			var ccf *types.ConditionalCheckFailedException
			Expect(errors.As(err, &ccf)).To(BeTrue(), "error should wrap ConditionalCheckFailedException")

			got, err := db.GetProvider("idp_dup")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.DisplayName).To(Equal("Provider idp_dup"))
			Expect(got.IsEnabled()).To(BeTrue())
		})
	})

	Describe("ListEnabled", func() {
		It("returns nothing on an empty table", func() {
			enabled, err := db.ListEnabled()
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeNil())
		})

		It("returns only enabled providers, across several scan pages", func() {
			dbMock.MaxPageItems = 2
			Expect(db.CreateProvider(newProvider("idp_on_1", utils.Ptr(true)))).To(Succeed())
			Expect(db.CreateProvider(newProvider("idp_off", utils.Ptr(false)))).To(Succeed())
			Expect(db.CreateProvider(newProvider("idp_unset", nil))).To(Succeed())
			Expect(db.CreateProvider(newProvider("idp_on_2", utils.Ptr(true)))).To(Succeed())
			Expect(db.CreateProvider(newProvider("idp_on_3", utils.Ptr(true)))).To(Succeed())

			enabled, err := db.ListEnabled()
			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(enabled))
			for _, p := range enabled {
				names = append(names, p.ProviderName)
			}
			Expect(names).To(ConsistOf("idp_on_1", "idp_on_2", "idp_on_3"))
		})

		It("returns nothing when every provider is disabled", func() {
			Expect(db.CreateProvider(newProvider("idp_off", utils.Ptr(false)))).To(Succeed())

			enabled, err := db.ListEnabled()
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeNil())
		})
	})
})
