// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package automation_db_test

import (
	"fmt"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAutomationDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AutomationDB Suite")
}

const groupID = "grp001"

var _ = Describe("AutomationDB", func() {
	var (
		mockDB       *mock.DynamoDBMock
		automationDB *automation_db.AutomationDB
	)

	seed := func(n int) []string {
		ids := make([]string, 0, n)
		for i := range n {
			id := fmt.Sprintf("a%02d", i)
			Expect(automationDB.CreateAutomation(groupID, id, map[string]interface{}{"name": id})).To(Succeed())
			ids = append(ids, id)
		}
		return ids
	}

	listIDs := func() []string {
		items, err := automationDB.ListGroupAutomations(groupID)
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.AutomationID)
		}
		return ids
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.ProfileReset()
		DeferCleanup(func() {
			mockDB.MaxPageItems = 0
			mockDB.NextBatchWriteUnprocessedCount = 0
		})
	})

	// A Query returns at most 1 MB regardless of Limit; MaxPageItems stands in for that cap.
	It("lists every automation in a group spanning several DynamoDB pages", func() {
		ctx := rmngctx.NewRmngContext(user.NewUser("paginated-user"))
		ctx.SetAllowMultiple(utils.GetGroupPermissions(utils.GroupPrimaryAccess), groupID)
		automationDB = automation_db.NewAutomationDB(ctx)
		mockDB.MaxPageItems = 3

		want := seed(10)
		Expect(listIDs()).To(ConsistOf(want))
	})

	// A throttled BatchWriteItem answers 200 with UnprocessedItems, so a delete that discards the response reports success while rows survive.
	Describe("DeleteAllGroupAutomations when DynamoDB reports unprocessed items", func() {
		BeforeEach(func() {
			automationDB = automation_db.NewAutomationDB(rmngctx.NewRmngContext(utils.NewSystemActor()))
		})

		It("re-submits unprocessed items until every row is gone", func() {
			seed(6)
			Expect(listIDs()).To(HaveLen(6))
			// The mock holds back one item fewer on each call, so this finishes on the last allowed attempt.
			mockDB.NextBatchWriteUnprocessedCount = awscommon.SDK_MAX_ATTEMPTS - 1
			Expect(automationDB.DeleteAllGroupAutomations(groupID)).To(Succeed())
			Expect(listIDs()).To(BeEmpty(), "rows survived a delete that reported success")
		})

		It("reports an error instead of success when the items never drain", func() {
			seed(4)
			mockDB.NextBatchWriteUnprocessedCount = 50
			err := automationDB.DeleteAllGroupAutomations(groupID)
			Expect(err).To(HaveOccurred(), "a delete that could not finish must not report success")
			Expect(err.Error()).To(ContainSubstring("unprocessed"))
			Expect(listIDs()).ToNot(BeEmpty())
		})

		It("deletes cleanly when nothing is throttled", func() {
			seed(3)
			Expect(automationDB.DeleteAllGroupAutomations(groupID)).To(Succeed())
			Expect(listIDs()).To(BeEmpty())
		})
	})
})
