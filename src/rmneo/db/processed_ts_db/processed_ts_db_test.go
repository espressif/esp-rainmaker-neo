// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package processed_ts_db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/processed_ts_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/timeseries/timewindow"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProcessedTsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProcessedTsDB Suite")
}

// MaxPageItems stands in for DynamoDB's 1 MB page cap; GetWindowEntries feeds the historical-aggregates handler, which treats its result as the whole window range.
var _ = Describe("GetWindowEntries across more than one DynamoDB page", func() {
	const (
		nodeID     = "paged-node"
		pageSize   = 2
		totalDays  = 5
		dataKey    = "temperature"
		dataType   = "float"
		windowType = timewindow.WindowDaily
	)

	var (
		mockDB *mock.DynamoDBMock
		db     *processed_ts_db.ProcessedTsDB
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		mockDB = awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		mockDB.ProfileReset()
		db = processed_ts_db.NewProcessedTsDB(rmngctx.NewRmngContext(utils.NewSystemActor()))

		for day := 1; day <= totalDays; day++ {
			entry := &processed_ts_db.ProcessedTsEntry{
				NodeKeyDt:  fmt.Sprintf("%s.%s.%s", nodeID, dataKey, dataType),
				NodeID:     nodeID,
				DataKey:    dataKey,
				DataType:   dataType,
				WindowType: string(windowType),
			}
			Expect(db.CreateWindowEntry(entry, fmt.Sprintf("daily#2026-01-%02d", day))).To(Succeed())
		}
		mockDB.MaxPageItems = pageSize
	})

	AfterEach(func() {
		mockDB.MaxPageItems = 0
	})

	It("returns every window in the range, not just the first page", func() {
		entries, err := db.GetWindowEntries(nodeID, dataKey, dataType, windowType, time.Time{}, time.Time{})
		Expect(err).ToNot(HaveOccurred())

		got := make([]string, 0, len(entries))
		for _, e := range entries {
			got = append(got, e.IntervalKey)
		}
		Expect(got).To(Equal([]string{
			"daily#2026-01-05", "daily#2026-01-04", "daily#2026-01-03", "daily#2026-01-02", "daily#2026-01-01",
		}))
	})

	It("rejects a pagination token that does not decode", func() {
		_, err := db.GetWindowEntriesWithPagination(nodeID, dataKey, dataType, windowType, time.Time{}, time.Time{}, pageSize, "not-a-valid-base64-token!!!")
		Expect(err).To(MatchError("invalid pagination token"))
	})

	It("still returns one page and a token when a limit is given", func() {
		page, err := db.GetWindowEntriesWithPagination(nodeID, dataKey, dataType, windowType, time.Time{}, time.Time{}, pageSize, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(page.Entries).To(HaveLen(pageSize))
		Expect(page.NextToken).ToNot(BeEmpty())

		next, err := db.GetWindowEntriesWithPagination(nodeID, dataKey, dataType, windowType, time.Time{}, time.Time{}, pageSize, page.NextToken)
		Expect(err).ToNot(HaveOccurred())
		Expect(next.Entries[0].IntervalKey).To(Equal("daily#2026-01-03"))
	})
})
