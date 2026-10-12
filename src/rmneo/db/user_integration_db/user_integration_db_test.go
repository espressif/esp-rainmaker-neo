// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package user_integration_db_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestUserIntegrationDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UserIntegrationDB Suite")
}

var _ = Describe("UserDB", func() {
	const gcm, apns = "MOCK_GCM_TEST", "MOCK_APNS_TEST"

	var userDB *user_integration_db.UserDB

	dbFor := func(userID string) *user_integration_db.UserDB {
		return user_integration_db.NewUserDB(rmngctx.NewRmngContext(user.NewUser(userID)))
	}
	register := func(db *user_integration_db.UserDB, integrationID string, n int) {
		for i := range n {
			Expect(db.RegisterClient(user_integration_db.UserIntegrationEntry{
				IntegrationID:  integrationID,
				EndpointID:     fmt.Sprintf("ep-%02d", i),
				SNSEndpointARN: fmt.Sprintf("mock-arn-%02d", i),
			})).To(Succeed())
		}
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		userDB = dbFor("user-1")
	})

	Describe("GetUserEntriesByIntegration", func() {
		It("returns every entry for the integration across service pages, and only the caller's", func() {
			register(userDB, gcm, 7)
			register(userDB, apns, 2)
			register(dbFor("user-2"), gcm, 1)
			// A Query returns at most 1 MB regardless of Limit; MaxPageItems stands in for that cap.
			awscommon.GetDynamoDBClient().(*mock.DynamoDBMock).MaxPageItems = 2

			entries, err := userDB.GetUserEntriesByIntegration(gcm)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(7))
			for _, e := range entries {
				Expect(e.IntegrationID).To(Equal(gcm))
				Expect(e.UserID).To(Equal("user-1"))
			}
		})

		It("reports a user with no entries for the integration", func() {
			register(userDB, apns, 1)
			_, err := userDB.GetUserEntriesByIntegration(gcm)
			Expect(err).To(MatchError(ContainSubstring("user entry not found")))
		})
	})
})
