// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_reg_req_db_test

import (
	"context"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Every request is created in the same second, so created_at ties and the resume point rests on the base-table key the index carries as a tie-break.
var _ = Describe("Cursor pagination while rows are deleted", func() {
	It("resumes at the next request after the one the cursor names is deleted", func() {
		const (
			pageSize   = 3
			totalItems = 10
		)
		test_utils.TestSetup()
		mockDB := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
		reqDB := node_reg_req_db.NewNodeRegRequestsDB(rmngctx.NewRmngContext(utils.NewSystemActor()))

		for i := range totalItems {
			Expect(reqDB.CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
				RequestID: fmt.Sprintf("request-%02d", i),
				UserID:    "admin",
				Status:    node_reg_req_db.NODE_REG_STATUS_STARTED,
			})).To(Succeed())
		}

		page1, err := reqDB.ListNodeRegRequests(pageSize, "", "", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(page1.Entries).To(HaveLen(pageSize))
		Expect(page1.NextKey).ToNot(BeEmpty())

		cursorRequestID := page1.Entries[len(page1.Entries)-1].RequestID
		_, err = mockDB.DeleteItem(context.Background(), &dynamodb.DeleteItemInput{
			TableName: aws.String(node_reg_req_db.NodeRegReqsTable),
			Key:       map[string]types.AttributeValue{"request_id": &types.AttributeValueMemberS{Value: cursorRequestID}},
		})
		Expect(err).ToNot(HaveOccurred())

		seen := make([]string, 0, totalItems)
		for _, entry := range page1.Entries {
			seen = append(seen, entry.RequestID)
		}
		for startKey := page1.NextKey; startKey != ""; {
			page, err := reqDB.ListNodeRegRequests(pageSize, startKey, "", "")
			Expect(err).ToNot(HaveOccurred())
			for _, entry := range page.Entries {
				Expect(seen).ToNot(ContainElement(entry.RequestID), "page restarted and re-served a request already returned")
				seen = append(seen, entry.RequestID)
			}
			startKey = page.NextKey
		}

		Expect(seen).To(HaveLen(totalItems))
		Expect(seen).To(ContainElement(cursorRequestID), "the deleted row was already returned on page 1")
	})
})
