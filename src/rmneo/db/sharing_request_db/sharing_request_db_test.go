// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package sharing_request_db_test

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/sharing_request_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSharingRequestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SharingRequestDB Suite")
}

var _ = Describe("SharingRequestDB", func() {
	// A Query returns at most 1 MB regardless of Limit; MaxPageItems stands in for that cap.
	It("returns every sharing request addressed to a user across several DynamoDB pages", func() {
		const (
			userID  = "paginated-user"
			groupID = "grp001"
		)
		test_utils.TestSetup()
		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.MaxPageItems = 3
		DeferCleanup(func() { mockDB.MaxPageItems = 0 })

		ctx := rmngctx.NewRmngContext(user.NewUser(userID))
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		sharingDB := sharing_request_db.NewSharingRequestDB(ctx)
		for range 10 {
			_, err := sharingDB.CreateSharingRequest(userID, groupID, "", string(utils.GroupSecondaryAccess), "primary@example.com", "")
			Expect(err).ToNot(HaveOccurred())
		}

		entries, err := sharingDB.GetMySharingRequests()
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(10))
	})
})
