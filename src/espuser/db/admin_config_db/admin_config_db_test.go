// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package admin_config_db_test

import (
	"context"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAdminConfigDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AdminConfigDB Suite")
}

var _ = Describe("AdminConfigDB", func() {
	var (
		dbMock *mock.DynamoDBMock
		db     *admin_config_db.AdminConfigDB
	)

	BeforeEach(func() {
		dbMock = test_utils.SetupEspUserBackend(context.Background()).DBMock
		db = admin_config_db.NewAdminConfigDB(rmngctx.NewRmngContextWithCtx(context.Background(), nil))
	})

	Describe("GetAll", func() {
		It("returns nil for a config_name with no rows", func() {
			rows, err := db.GetAll(admin_config_db.ConfigEmailSender)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeNil())
		})

		It("returns every row of the config_name in subtype order, across several query pages, and no other config's rows", func() {
			dbMock.MaxPageItems = 2
			for _, subtype := range []string{"otp", "global", "invite", "alerts", "reset"} {
				Expect(db.Put(admin_config_db.ConfigEmailSender, subtype, subtype+"@example.com", 100)).To(Succeed())
			}
			Expect(db.Put("other-config", admin_config_db.CategoryGlobal, "x", 100)).To(Succeed())

			rows, err := db.GetAll(admin_config_db.ConfigEmailSender)
			Expect(err).NotTo(HaveOccurred())
			subtypes := make([]string, 0, len(rows))
			for _, r := range rows {
				Expect(r.ConfigName).To(Equal(admin_config_db.ConfigEmailSender))
				Expect(r.Value).To(Equal(r.Subtype + "@example.com"))
				subtypes = append(subtypes, r.Subtype)
			}
			Expect(subtypes).To(Equal([]string{"alerts", "global", "invite", "otp", "reset"}))
		})
	})

	Describe("Put", func() {
		It("is an unconditional upsert: a second Put overwrites the row", func() {
			Expect(db.Put(admin_config_db.ConfigEmailSender, admin_config_db.CategoryGlobal, "first@example.com", 100)).To(Succeed())
			Expect(db.Put(admin_config_db.ConfigEmailSender, admin_config_db.CategoryGlobal, "second@example.com", 200)).To(Succeed())

			rows, err := db.GetAll(admin_config_db.ConfigEmailSender)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(Equal([]admin_config_db.ConfigEntry{{
				ConfigName: admin_config_db.ConfigEmailSender,
				Subtype:    admin_config_db.CategoryGlobal,
				Value:      "second@example.com",
				UpdatedAt:  200,
			}}))
		})
	})
})
