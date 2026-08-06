// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package db_test

import (
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/automation_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A throttled BatchWriteItem is answered with 200 and an UnprocessedItems list, not an error. The
// batch delete used to discard that response, so a partially-throttled delete reported success
// while leaving rows behind — in every delete path in the codebase: unshare, leave, group delete,
// the automation wipe and the timeseries purge.
var _ = Describe("Batch delete when DynamoDB reports unprocessed items", func() {
	const groupID = "unproc"

	var (
		mockDB       *mock.DynamoDBMock
		automationDB *automation_db.AutomationDB
		ctx          *rmngctx.RmngContext
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

	remaining := func() int {
		items, err := automationDB.ListGroupAutomations(groupID)
		Expect(err).ToNot(HaveOccurred())
		return len(items)
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.ProfileReset()

		u := utils.NewSystemActor()
		ctx = rmngctx.NewRmngContext(u)
		automationDB = automation_db.NewAutomationDB(ctx)
	})

	AfterEach(func() {
		mockDB.NextBatchWriteUnprocessedCount = 0
	})

	It("re-submits unprocessed items until every row is gone", func() {
		seed(6)
		Expect(remaining()).To(Equal(6))

		// Hold back 3 items on the first call, 2 on the next, then 1 — a caller that retries
		// drains it, a caller that discards the response leaves rows behind.
		mockDB.NextBatchWriteUnprocessedCount = 3

		Expect(automationDB.DeleteAllGroupAutomations(groupID)).To(Succeed())
		Expect(remaining()).To(BeZero(), "rows survived a delete that reported success")
	})

	It("reports an error instead of success when the items never drain", func() {
		seed(4)

		// Larger than the retry budget, so the mock keeps holding items back for every attempt.
		mockDB.NextBatchWriteUnprocessedCount = 50

		err := automationDB.DeleteAllGroupAutomations(groupID)
		Expect(err).To(HaveOccurred(), "a delete that could not finish must not report success")
		Expect(err.Error()).To(ContainSubstring("unprocessed"))
		Expect(remaining()).To(BeNumerically(">", 0), "precondition: rows should still be present")
	})

	It("still deletes cleanly when nothing is throttled", func() {
		seed(3)
		Expect(automationDB.DeleteAllGroupAutomations(groupID)).To(Succeed())
		Expect(remaining()).To(BeZero())
	})
})
