// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package bulk_container_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/s3util"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/bulk_container"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func TestBulkContainer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bulk Container Suite")
}

var _ = Describe("Bulk Container", func() {
	var (
		mockS3Client *mock.S3ClientMock
		testTaskID   string
		testCert1    string
		testCert2    string
		dbClient     *node_reg_req_db.NodeRegRequestsDB
		fileBucket   string
		testConfig   *bulk_container.ContainerConfig
	)

	BeforeEach(func() {
		// Initialize the mocks
		test_utils.TestSetup()
		mockS3Client = awscommon.GetS3Client().(*mock.S3ClientMock)

		testTaskID = "test-task"

		// Create test certificates in PEM format
		// Node Id: bulknode1
		testCert1 = "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUJxFymgxSNmN/Y1VA1xpjsfyE+P8wDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUxMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUxMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEAxkOoaj9mf4bw7N9SV1zHvgtvszvauaay+k1eSeqbgOde\nfu0qwSZ8BLNtMstibHOwmpS4OPoxbW5KhyoBRdhcO2wUamEk6UdapXcOJiKa+u7I\n3AcpqMe5i3WVSAFttotfSeI0nTqAGPkTZOrDqZCwp2Hg+m6SFH2i1efXRYyMlGBP\nmU8B4HC84HoM19EJw4CIMUIUWR8WEugvHuaf5ano00lGr6QoHsgCWNyj533KgQ4A\nNwdqQ0h1gnv+Bdz/mCZ+FmveUn1jFfRokbceZqxaMmm5BN9cEmv2abpZgC9A5If6\n3rcv59aSLAIn/Sj/x1N9G9d/IyKQbwkKw1zquuyUdQIDAQABo1MwUTAdBgNVHQ4E\nFgQU4aX1iM6cEWbWl+Aua5WxJyaj9YswHwYDVR0jBBgwFoAU4aX1iM6cEWbWl+Au\na5WxJyaj9YswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAVrsy\n8Fdeds02qFb5A5r2usNfSw3c30qIDtv0HSgfd5lVpvG1p5CE+ziyWtBuwxkzwEdE\n4JPJmXX5bQGyrZKlkD60K3kHq+Ed2hakiLJjB15DqNTk8pKTHjfYA0/mfXZKUtqP\nc03a8DPqjfbncHUOIyUaVr+o8O5dZIouGx9M84/RDbbemPjHAapshDNejLLA/gzT\n7/PGRQ7lGRlHu3NgIaoLAE0Q4Uwj7cycNugfQCnQF8nWSZyR192gh00alLb38p98\nE8tvw9wWDS8RMYV8KTP2nuB59OWxUSoNGtY+dnb88tLIiUE7odMjaBQY8id+Pg/9\n/fz2Ocw6G8/996nJdw==\n-----END CERTIFICATE-----"

		// Node Id: bulknode2
		testCert2 = "-----BEGIN CERTIFICATE-----\nMIIDCTCCAfGgAwIBAgIUFSmWQs0md/PJBWOtaJjxuRlsPKswDQYJKoZIhvcNAQEL\nBQAwFDESMBAGA1UEAwwJYnVsa25vZGUyMB4XDTI1MDYyMDE3MDA0N1oXDTI2MDYy\nMDE3MDA0N1owFDESMBAGA1UEAwwJYnVsa25vZGUyMIIBIjANBgkqhkiG9w0BAQEF\nAAOCAQ8AMIIBCgKCAQEA6s7QeHQs2k+YhXX5Ri3Fvlgh5W0OOfABq87jTSadI38F\nqKILXuKKEFEf6TXIwmXil5VscSJW4D1YeCqtWQBvXKC9/hSyCULt8BmWBbJ7xypW\n+eFCYZWDEonTe5J+yMChbLFK21ghJL3nhN3EKfwje720zM9cPCc6Zixu+3qHlgsE\nIxMaruqsncsnG3v2+EoL21W/xyXfgLDkzDBYGJE/SEVpQqaOq723OW0EL7f95sPW\nJpu09PwrpxvVUGVEic+sm9bFmXa11Swa9AdFrwbKHwpLxVAUKOz5sA19H//78dyy\nGA7Q5mgkRgq4YM511iiJc5Tb8qBCPo7OJZf6SrjJ/QIDAQABo1MwUTAdBgNVHQ4E\nFgQUYgFUcxifQE24cB5PxmIjNOgU/9swHwYDVR0jBBgwFoAUYgFUcxifQE24cB5P\nxmIjNOgU/9swDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAWrJ/\nD76x7n7slHBFbbPo3m32XS0kFBr7qoJqUfPAs2d+/5cXJY5sI0gilLQ7+ugyBePP\n+3JTHPehye25zcBnIQqnULrj6Xw/Zw5r8xZT0yeqnLDyAL45Ns7XBO36tqMzXLt2\ndjhepka5RT0RZOIM2xfYtOFkMBGfKLw+lKYOcl6B1MIiJ2LOtZjdL3Gk2W/Y21IJ\nPEl7Esd0RY6P9cfHa8KRIW476A/qCPOp1AhbGKLxt/f0gdFjLRD0tcORTqW3ZLmx\n951udgLtXpP0DpTSdfN+EHxByqZTh6z8vJv46nOHPIA89KGt0H0W1aOFjyFGVipr\nXWMlT8nc6zw9MuNjYg==\n-----END CERTIFICATE-----"
		fileBucket = os.Getenv("FILE_BUCKET_NAME")

		// Create test config
		testConfig = &bulk_container.ContainerConfig{
			CertFileS3Path:  "s3://" + fileBucket + "/system/test-key.csv",
			UserID:          "test-user",
			AdminGroupNames: []string{},
			Tags:            []string{},
			RequestID:       testTaskID,
		}

		testUser := user.NewUser("test-user")
		rmngCtx := rmngctx.NewRmngContext(testUser)
		rmngCtx.SetAllow(utils.NodeAdminAdd, "*")
		rmngCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
		dbClient = node_reg_req_db.NewNodeRegRequestsDB(rmngCtx)

		// Seed the registration request record (normally created by the Lambda handler)
		err := dbClient.CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
			RequestID:      testTaskID,
			UserID:         "test-user",
			Status:         node_reg_req_db.NODE_REG_STATUS_REQUESTED,
			CertFileS3Path: testConfig.CertFileS3Path,
		})
		Expect(err).NotTo(HaveOccurred())
	})

	Context("HandleContainer", func() {
		It("should apply common admin group names to all nodes", func() {
			// Set common admin group names on the config
			testConfig.AdminGroupNames = []string{"CommonGroup1", "CommonGroup2"}

			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups", "key1"})
			w.Write([]string{"bulknode1", testCert1, "", "value1"})
			w.Write([]string{"bulknode2", testCert2, "", "value2"})
			w.Flush()

			mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

			err := bulk_container.HandleContainer(testConfig)
			Expect(err).NotTo(HaveOccurred())

			// Verify both nodes were added to the common groups
			iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
			node1Groups := iotClient.GetThingGroupsDirect("bulknode1")
			node2Groups := iotClient.GetThingGroupsDirect("bulknode2")
			Expect(node1Groups).To(ContainElement("CommonGroup1"))
			Expect(node1Groups).To(ContainElement("CommonGroup2"))
			Expect(node2Groups).To(ContainElement("CommonGroup1"))
			Expect(node2Groups).To(ContainElement("CommonGroup2"))
		})

		It("should merge common and per-node admin groups", func() {
			testConfig.AdminGroupNames = []string{"CommonGroup"}

			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups"})
			w.Write([]string{"bulknode1", testCert1, "PerNodeGroup1"})
			w.Write([]string{"bulknode2", testCert2, "PerNodeGroup2"})
			w.Flush()

			mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

			err := bulk_container.HandleContainer(testConfig)
			Expect(err).NotTo(HaveOccurred())

			iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
			// Node 1 should be in both common and its per-node group
			node1Groups := iotClient.GetThingGroupsDirect("bulknode1")
			Expect(node1Groups).To(ContainElement("CommonGroup"))
			Expect(node1Groups).To(ContainElement("PerNodeGroup1"))

			// Node 2 should be in both common and its per-node group
			node2Groups := iotClient.GetThingGroupsDirect("bulknode2")
			Expect(node2Groups).To(ContainElement("CommonGroup"))
			Expect(node2Groups).To(ContainElement("PerNodeGroup2"))
		})

		It("should handle empty admin_groups column gracefully", func() {
			testConfig.AdminGroupNames = []string{"CommonGroup"}

			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups"})
			w.Write([]string{"bulknode1", testCert1, ""})
			w.Flush()

			mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

			err := bulk_container.HandleContainer(testConfig)
			Expect(err).NotTo(HaveOccurred())

			// Should only be in the common group, not in an empty-string group
			iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
			node1Groups := iotClient.GetThingGroupsDirect("bulknode1")
			Expect(node1Groups).To(ContainElement("CommonGroup"))
			Expect(node1Groups).ToNot(ContainElement(""))
		})

		It("should successfully process nodes from CSV", func() {
			// Mock data with proper certificate
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write([]string{"node_id", "certs", "admin_groups", "key1", "key2", "key3", "key4"})
			w.Write([]string{"bulknode1", testCert1, "group1", "value1", "value2", "", ""})
			w.Write([]string{"bulknode2", testCert2, "group2", "", "", "value3", "value4"})
			w.Flush()
			csvContent := buf.String()

			// Store CSV in mock S3
			mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = csvContent

			// Execute
			err := bulk_container.HandleContainer(testConfig)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			entry, err := dbClient.GetNodeRegRequest(testTaskID)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_COMPLETED))
			Expect(*entry.SuccessCount).To(Equal(2))
			Expect(*entry.FailedCount).To(Equal(0))
			Expect(entry.CertFileS3Path).To(Equal("s3://" + fileBucket + "/system/test-key.csv"))
			Expect(entry.UserID).To(Equal("test-user"))
		})

		It("should fail when S3 file read fails", func() {
			// Don't store the file in the mock bucket
			err := bulk_container.HandleContainer(testConfig)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read nodes from S3 CSV"))
		})

		It("should fail when CSV file is empty", func() {
			csvContent := "node_id,certs,admin_groups,tag1,tag2"
			mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = csvContent

			err := bulk_container.HandleContainer(testConfig)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no nodes found in CSV file"))
		})

		Context("failure detail", func() {
			var failuresDB *node_reg_failed_nodes_db.NodeRegFailedNodesDB

			BeforeEach(func() {
				readerCtx := rmngctx.NewRmngContext(user.NewUser("test-user"))
				readerCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
				failuresDB = node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(readerCtx)
			})

			It("records a row in node_reg_failed_nodes for every failed registration", func() {
				// One valid cert and one malformed cert that will fail at PEM parse.
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{"bulknode1", testCert1, ""})
				w.Write([]string{"bulknode-bad", "not-a-valid-pem", ""})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err := bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.SuccessCount).To(Equal(1))
				Expect(*entry.FailedCount).To(Equal(1))

				out, err := failuresDB.ListFailures(testTaskID, 100, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(out.Entries).To(HaveLen(1))
				failed := out.Entries[0]
				Expect(failed.NodeID).To(Equal("bulknode-bad"))
				Expect(failed.Reason).NotTo(BeEmpty())
				Expect(failed.Code).To(Equal(string(node_reg_failed_nodes_db.FailureCodeInvalidCert)))
				Expect(failed.RecordedAt).To(BeNumerically(">", int64(0)))
			})

			It("records every row when the entire CSV fails", func() {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{"bad-1", "not-pem-1", ""})
				w.Write([]string{"bad-2", "not-pem-2", ""})
				w.Write([]string{"bad-3", "not-pem-3", ""})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err := bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.SuccessCount).To(Equal(0))
				Expect(*entry.FailedCount).To(Equal(3))

				out, err := failuresDB.ListFailures(testTaskID, 100, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(out.Entries).To(HaveLen(3))

				ids := []string{}
				for _, e := range out.Entries {
					ids = append(ids, e.NodeID)
				}
				Expect(ids).To(ConsistOf("bad-1", "bad-2", "bad-3"))
			})

			It("writes nothing to the failures table when every row succeeds", func() {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{"bulknode1", testCert1, ""})
				w.Write([]string{"bulknode2", testCert2, ""})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err := bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				out, err := failuresDB.ListFailures(testTaskID, 100, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(out.Entries).To(BeEmpty())
			})
		})

		Context("failed-nodes CSV (eager S3 write)", func() {
			// Mirrors the container's deterministic naming: same prefix as
			// the input CSV, "<requestId>_failed_node_certs.csv". Computed
			// inside each spec because testTaskID is assigned in BeforeEach.
			It("writes a cert-bearing CSV of only the failed rows and stamps the job row", func() {
				failedCSVKey := "system/" + testTaskID + "_failed_node_certs.csv"
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups", "env"})
				w.Write([]string{"bulknode1", testCert1, "", "prod"})             // succeeds
				w.Write([]string{"bulknode-bad", "not-a-valid-pem", "", "stage"}) // fails at cert parse
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				Expect(bulk_container.HandleContainer(testConfig)).To(Succeed())

				// The job row carries the S3 path of the failed-rows CSV.
				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(entry.FailedFileS3Path).To(Equal("s3://" + fileBucket + "/" + failedCSVKey))

				// The CSV holds the header (original column order) plus only the
				// failed row, with cert and tag columns intact — re-uploadable.
				content, err := s3util.GetObjectContent(context.Background(), fileBucket, failedCSVKey)
				Expect(err).NotTo(HaveOccurred())
				rows, err := csv.NewReader(bytes.NewReader(content)).ReadAll()
				Expect(err).NotTo(HaveOccurred())
				Expect(rows[0]).To(Equal([]string{"node_id", "certs", "admin_groups", "env"}))
				Expect(rows).To(HaveLen(2)) // header + the single failed row
				Expect(rows[1][0]).To(Equal("bulknode-bad"))
				Expect(rows[1][1]).To(Equal("not-a-valid-pem"))
				Expect(rows[1][3]).To(Equal("stage"))
			})

			It("writes no CSV and leaves the path empty when every row succeeds", func() {
				failedCSVKey := "system/" + testTaskID + "_failed_node_certs.csv"
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{"bulknode1", testCert1, ""})
				w.Write([]string{"bulknode2", testCert2, ""})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				Expect(bulk_container.HandleContainer(testConfig)).To(Succeed())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(entry.FailedFileS3Path).To(BeEmpty())

				_, err = s3util.GetObjectContent(context.Background(), fileBucket, failedCSVKey)
				Expect(err).To(HaveOccurred()) // object was never written
			})

			It("completes with the path unset and a flagged message when the S3 write fails", func() {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "certs", "admin_groups"})
				w.Write([]string{"bulknode1", testCert1, ""})            // succeeds
				w.Write([]string{"bulknode-bad", "not-a-valid-pem", ""}) // fails
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				// Force the failed-rows CSV write to fail; the DDB audit write is
				// unaffected (it goes through DynamoDB, not S3).
				mockS3Client.ForcePutObjectErr = errors.New("simulated s3 outage")

				Expect(bulk_container.HandleContainer(testConfig)).To(Succeed())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.FailedCount).To(Equal(1))
				Expect(entry.FailedFileS3Path).To(BeEmpty())
				Expect(entry.Message).To(ContainSubstring("CSV unavailable"))

				// DDB audit detail still recorded despite the CSV write failure.
				readerCtx := rmngctx.NewRmngContext(user.NewUser("reader"))
				readerCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
				out, err := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(readerCtx).ListFailures(testTaskID, 100, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(out.Entries).To(HaveLen(1))
			})
		})

		Context("update mode (JobType=update)", func() {
			BeforeEach(func() {
				testConfig.JobType = node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE
			})

			It("updates tags and admin groups on already-registered nodes", func() {
				// Pre-register the nodes so node_details exists, then run an
				// update job over the same node_ids.
				seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
				seederCtx.SetAllow(utils.NodeAdminAdd, "*")
				seederCtx.SetAllow(utils.NodeAll, "*")
				seederCtx.SetAllow(utils.NodeWriteShadow, "*")
				_, err := node.RegisterNodeInRmng(seederCtx, testCert1, "", nil, nil, "test-user", nil)
				Expect(err).NotTo(HaveOccurred())
				_, err = node.RegisterNodeInRmng(seederCtx, testCert2, "", nil, nil, "test-user", nil)
				Expect(err).NotTo(HaveOccurred())

				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "admin_groups", "env"})
				w.Write([]string{"bulknode1", "G1", "prod"})
				w.Write([]string{"bulknode2", "G2", "stage"})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err = bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.SuccessCount).To(Equal(2))
				Expect(*entry.FailedCount).To(Equal(0))

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyThingInGroup("bulknode1", "G1")).To(BeTrue())
				Expect(iotClient.VerifyThingInGroup("bulknode2", "G2")).To(BeTrue())
			})

			It("records 'node not found' for rows that don't refer to a registered node", func() {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "admin_groups", "env"})
				w.Write([]string{"never-registered", "G1", "prod"})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				readerCtx := rmngctx.NewRmngContext(user.NewUser("reader"))
				readerCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
				failuresDB := node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(readerCtx)

				err := bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.SuccessCount).To(Equal(0))
				Expect(*entry.FailedCount).To(Equal(1))

				out, err := failuresDB.ListFailures(testTaskID, 100, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(out.Entries).To(HaveLen(1))
				Expect(out.Entries[0].NodeID).To(Equal("never-registered"))
				Expect(out.Entries[0].Reason).To(ContainSubstring("node not found"))
				Expect(out.Entries[0].Code).To(Equal(string(node_reg_failed_nodes_db.FailureCodeUnknown)))
			})

			It("rejects rows with a missing node_id column", func() {
				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				w.Write([]string{"node_id", "admin_groups", "env"})
				w.Write([]string{"", "G1", "prod"})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err := bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.FailedCount).To(Equal(1))
			})

			It("replaces certs end-to-end when the CSV row has a certs column", func() {
				// Pre-register bulknode1 with testCert1; then an update job
				// with a CSV that points bulknode1 at testCert2 must end
				// with bulknode1's binding moved to testCert2 and testCert1
				// deactivated.
				seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
				seederCtx.SetAllow(utils.NodeAdminAdd, "*")
				seederCtx.SetAllow(utils.NodeAll, "*")
				seederCtx.SetAllow(utils.NodeWriteShadow, "*")
				_, err := node.RegisterNodeInRmng(seederCtx, testCert1, "", nil, nil, "test-user", nil)
				Expect(err).NotTo(HaveOccurred())

				var buf bytes.Buffer
				w := csv.NewWriter(&buf)
				// CSV uses bulknode1 as the node_id but supplies testCert2
				// as the replacement (a "wrong cert was registered, here's
				// the right one" scenario).
				w.Write([]string{"node_id", "certs"})
				w.Write([]string{"bulknode1", testCert2})
				w.Flush()
				mockS3Client.Buckets[fileBucket]["system/test-key.csv"] = buf.String()

				err = bulk_container.HandleContainer(testConfig)
				Expect(err).NotTo(HaveOccurred())

				entry, err := dbClient.GetNodeRegRequest(testTaskID)
				Expect(err).NotTo(HaveOccurred())
				Expect(*entry.SuccessCount).To(Equal(1))
				Expect(*entry.FailedCount).To(Equal(0))

				iotClient := awscommon.GetIoTClient().(*mock.IoTClientMock)
				Expect(iotClient.VerifyCertificateActive(testCert2)).To(BeTrue())
				Expect(iotClient.VerifyCertificateActive(testCert1)).To(BeFalse())
			})
		})
	})
})
