// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package node_reg_req_db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestNodeRegRequestsDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeRegRequestsDB Suite")
}

var _ = Describe("NodeRegRequestsDB", func() {
	var (
		testCtx          *rmngctx.RmngContext
		nodeRegRequestDB *node_reg_req_db.NodeRegRequestsDB
		testRequestID    string
		testUserID       string
		testEntry        node_reg_req_db.NodeRegRequestsEntry
	)

	BeforeEach(func() {
		// Setup test context and mock DB
		testUserID = "test-user-id"
		testCtx = rmngctx.NewRmngContext(user.NewUser(testUserID))
		testCtx.SetAllow(utils.NodeAdminAdd, "*")
		testCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

		test_utils.TestSetup()
		nodeRegRequestDB = node_reg_req_db.NewNodeRegRequestsDB(testCtx)

		// Test data
		testRequestID = "test-request-id"
		testEntry = node_reg_req_db.NodeRegRequestsEntry{
			RequestID:            testRequestID,
			CertFileS3Path:       "s3://test-bucket/test-cert.csv",
			AdminGroupNames:      []string{"test-group"},
			AdminParentGroupName: "test-parent",
			Tags:                 []string{"test-tag"},
			TotalCount:           10,
			SuccessCount:         aws.Int(0),
			FailedCount:          aws.Int(0),
			UserID:               testUserID,
			Status:               node_reg_req_db.NODE_REG_STATUS_STARTED,
		}
	})

	Describe("CreateNodeRegRequest", func() {
		It("should create node registration request successfully", func() {
			err := nodeRegRequestDB.CreateNodeRegRequest(testEntry)
			Expect(err).NotTo(HaveOccurred())

			entry, err := nodeRegRequestDB.GetNodeRegRequest(testRequestID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CreatedAt).NotTo(BeZero())
			Expect(entry.LastUpdatedAt).NotTo(BeZero())
			Expect(entry.UserID).To(Equal(testUserID))
			Expect(entry.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_STARTED))
			Expect(entry.AdminGroupNames).To(Equal([]string{"test-group"}))
			Expect(entry.AdminParentGroupName).To(Equal("test-parent"))
			Expect(entry.Tags).To(Equal([]string{"test-tag"}))
		})

		It("should return error for duplicate request ID", func() {
			// First create
			err := nodeRegRequestDB.CreateNodeRegRequest(testEntry)
			Expect(err).NotTo(HaveOccurred())

			// Try to create again with same request ID
			err = nodeRegRequestDB.CreateNodeRegRequest(testEntry)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create node registration request"))
		})
	})

	Describe("ListNodeRegRequests", func() {
		It("should return empty list when no requests exist", func() {
			result, err := nodeRegRequestDB.ListNodeRegRequests(20, "", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(BeEmpty())
			Expect(result.NextKey).To(BeEmpty())
		})

		It("should list all created requests", func() {
			// Create multiple requests
			for i := 0; i < 3; i++ {
				entry := node_reg_req_db.NodeRegRequestsEntry{
					RequestID:       fmt.Sprintf("request-%d", i),
					CertFileS3Path:  "s3://test-bucket/test.csv",
					AdminGroupNames: []string{"group-1"},
					Tags:            []string{"tag-1"},
					UserID:          testUserID,
					Status:          node_reg_req_db.NODE_REG_STATUS_STARTED,
				}
				err := nodeRegRequestDB.CreateNodeRegRequest(entry)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := nodeRegRequestDB.ListNodeRegRequests(20, "", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(HaveLen(3))
		})

		It("should respect limit parameter", func() {
			for i := 0; i < 3; i++ {
				entry := node_reg_req_db.NodeRegRequestsEntry{
					RequestID: fmt.Sprintf("request-%d", i),
					UserID:    testUserID,
					Status:    node_reg_req_db.NODE_REG_STATUS_STARTED,
				}
				err := nodeRegRequestDB.CreateNodeRegRequest(entry)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := nodeRegRequestDB.ListNodeRegRequests(2, "", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(HaveLen(2))
			Expect(result.NextKey).NotTo(BeEmpty())
		})

		It("should paginate with start_key", func() {
			for i := 0; i < 3; i++ {
				entry := node_reg_req_db.NodeRegRequestsEntry{
					RequestID: fmt.Sprintf("request-%d", i),
					UserID:    testUserID,
					Status:    node_reg_req_db.NODE_REG_STATUS_STARTED,
				}
				err := nodeRegRequestDB.CreateNodeRegRequest(entry)
				Expect(err).NotTo(HaveOccurred())
			}

			// Get first page
			result, err := nodeRegRequestDB.ListNodeRegRequests(2, "", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(HaveLen(2))
			Expect(result.NextKey).NotTo(BeEmpty())

			// Get second page using NextKey
			result2, err := nodeRegRequestDB.ListNodeRegRequests(2, result.NextKey, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result2.Entries).To(HaveLen(1))
		})

		It("should filter by status", func() {
			// Create requests with different statuses
			statuses := []string{
				node_reg_req_db.NODE_REG_STATUS_STARTED,
				node_reg_req_db.NODE_REG_STATUS_COMPLETED,
				node_reg_req_db.NODE_REG_STATUS_STARTED,
			}
			for i, s := range statuses {
				entry := node_reg_req_db.NodeRegRequestsEntry{
					RequestID: fmt.Sprintf("filter-request-%d", i),
					UserID:    testUserID,
					Status:    s,
				}
				err := nodeRegRequestDB.CreateNodeRegRequest(entry)
				Expect(err).NotTo(HaveOccurred())
			}

			// Filter for started only
			result, err := nodeRegRequestDB.ListNodeRegRequests(20, "", node_reg_req_db.NODE_REG_STATUS_STARTED)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(HaveLen(2))
			for _, e := range result.Entries {
				Expect(e.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_STARTED))
			}

			// Filter for completed only
			result, err = nodeRegRequestDB.ListNodeRegRequests(20, "", node_reg_req_db.NODE_REG_STATUS_COMPLETED)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Entries).To(HaveLen(1))
			Expect(result.Entries[0].Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_COMPLETED))
		})
	})

	Describe("UpdateNodeRegRequest", func() {
		BeforeEach(func() {
			// Create a request first
			err := nodeRegRequestDB.CreateNodeRegRequest(testEntry)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update node registration request successfully", func() {
			// Update the request
			testEntry.Status = node_reg_req_db.NODE_REG_STATUS_COMPLETED
			testEntry.SuccessCount = aws.Int(5)
			testEntry.FailedCount = aws.Int(5)
			oldLastUpdatedAt := testEntry.LastUpdatedAt
			time.Sleep(1 * time.Second)

			err := nodeRegRequestDB.UpdateNodeRegRequest(testEntry)
			Expect(err).NotTo(HaveOccurred())

			// Verify the updated timestamp
			entry, err := nodeRegRequestDB.GetNodeRegRequest(testRequestID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CreatedAt).NotTo(BeZero())
			Expect(entry.LastUpdatedAt).NotTo(BeZero())
			Expect(entry.UserID).To(Equal(testUserID))
			Expect(entry.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_COMPLETED))
			Expect(entry.SuccessCount).To(Equal(aws.Int(5)))
			Expect(entry.FailedCount).To(Equal(aws.Int(5)))
			Expect(entry.LastUpdatedAt).To(BeNumerically(">", oldLastUpdatedAt))
		})

		It("should return error for non-existent request", func() {
			nonExistentEntry := node_reg_req_db.NodeRegRequestsEntry{
				RequestID: "non-existent",
				Status:    node_reg_req_db.NODE_REG_STATUS_COMPLETED,
			}

			err := nodeRegRequestDB.UpdateNodeRegRequest(nonExistentEntry)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update node registration request"))
		})

		It("should return error for invalid status update", func() {
			testEntry.Status = "invalid-status"
			err := nodeRegRequestDB.UpdateNodeRegRequest(testEntry)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid node registration status"))
		})

		It("should return error when different user tries to update the request", func() {
			// Create a new context with a different user
			differentUserID := "different-user-id"
			differentUserCtx := rmngctx.NewRmngContext(user.NewUser(differentUserID))
			differentUserCtx.SetAllow(utils.NodeAdminAdd, "*")
			differentUserCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

			// Create DB instance with different user context
			differentUserDB := node_reg_req_db.NewNodeRegRequestsDB(differentUserCtx)

			// Try to update with different user
			updateEntry := testEntry
			updateEntry.Status = node_reg_req_db.NODE_REG_STATUS_COMPLETED
			err := differentUserDB.UpdateNodeRegRequest(updateEntry)

			// Should fail because different user doesn't own the request
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update node registration request"))

			// Verify original entry is unchanged
			entry, err := nodeRegRequestDB.GetNodeRegRequest(testRequestID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_STARTED))
		})
	})
})
