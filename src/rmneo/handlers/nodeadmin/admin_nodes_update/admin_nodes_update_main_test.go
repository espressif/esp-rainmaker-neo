// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAdminNodesUpdate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admin Nodes Update API Suite")
}

var _ = Describe("Admin Nodes Update API", func() {
	var (
		adminCtx *rmngctx.RmngContext
		userID   string
		baseReq  events.APIGatewayProxyRequest
	)

	BeforeEach(func() {
		test_utils.TestSetup()
		userID = "test-admin-update"
		_, adminCtx = test_utils.SetupTestAdminUser(context.Background(), userID, "test-update-email")

		baseReq = events.APIGatewayProxyRequest{
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	// seedJob creates a parent job row of the given type with optional failure rows.
	seedJob := func(jobType string, certPath string, failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) string {
		seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
		seederCtx.SetAllow(utils.NodeAdminAdd, "*")
		seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

		requestID := "seed-" + jobType + "-" + fmt.Sprintf("%d", GinkgoRandomSeed())
		err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
			CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
				RequestID:      requestID,
				JobType:        jobType,
				CertFileS3Path: certPath,
				Status:         node_reg_req_db.NODE_REG_STATUS_COMPLETED,
			})
		Expect(err).NotTo(HaveOccurred())

		if len(failures) > 0 {
			err = node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(seederCtx).
				RecordFailures(requestID, failures)
			Expect(err).NotTo(HaveOccurred())
		}
		return requestID
	}

	Describe("POST /v1/admin/nodes/update-jobs", func() {
		It("creates an update job and returns 202 with a request_id", func() {
			// The mock ECS RunTask runs the bulk container synchronously, so
			// the input CSV must already exist in mock S3. A minimal CSV with
			// a single row pointing at a node we've pre-registered exercises
			// the full happy path end-to-end.
			bucket := os.Getenv("FILE_BUCKET_NAME")
			key := "system/update-test.csv"
			s3Path := "s3://" + bucket + "/" + key

			var csvBuf bytes.Buffer
			cw := csv.NewWriter(&csvBuf)
			Expect(cw.Write([]string{"node_id", "admin_groups"})).To(Succeed())
			Expect(cw.Write([]string{"never-registered-update-test", "g1"})).To(Succeed())
			cw.Flush()

			mockS3 := awscommon.GetS3Client().(*mock.S3ClientMock)
			mockS3.CreateBucketDirect(bucket)
			mockS3.Buckets[bucket][key] = csvBuf.String()

			body := map[string]interface{}{
				"cert_file_s3_path": s3Path,
				"tags":              []string{"env:prod"},
			}
			bytesBody, _ := json.Marshal(body)
			baseReq.HTTPMethod = "POST"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			baseReq.Body = string(bytesBody)

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusAccepted))

			var resp BulkUpdateNodesResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Status).To(Equal("success"))
			Expect(resp.RequestId).NotTo(BeEmpty())

			// The created row should have JobType=update so the registration
			// Lambda's /registration-jobs endpoints don't pick it up.
			readerCtx := rmngctx.NewRmngContext(user.NewUser("reader"))
			readerCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")
			entry, err := node_reg_req_db.NewNodeRegRequestsDB(readerCtx).GetNodeRegRequest(resp.RequestId)
			Expect(err).To(BeNil())
			Expect(entry.JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE))
		})

		It("rejects requests missing cert_file_s3_path", func() {
			body := map[string]interface{}{"tags": []string{"env:prod"}}
			bytesBody, _ := json.Marshal(body)
			baseReq.HTTPMethod = "POST"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			baseReq.Body = string(bytesBody)

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))

			var resp BulkUpdateNodesResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Status).To(Equal("error"))
		})
	})

	Describe("GET /v1/admin/nodes/update-jobs/{requestId}", func() {
		It("returns the update job status", func() {
			requestID := seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE, "", nil)

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/" + requestID
			baseReq.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var status UpdateNodeStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &status)).To(Succeed())
			Expect(status.RequestID).To(Equal(requestID))
			Expect(status.JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE))
		})

		It("returns 404 for an unknown request_id", func() {
			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/no-such-job"
			baseReq.PathParameters = map[string]string{"requestId": "no-such-job"}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 404 when the request_id is a registration job (cross-flow isolation)", func() {
			// Seed a registration job; the update Lambda must NOT return it.
			requestID := seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER, "", nil)

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/" + requestID
			baseReq.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns a presigned download URL when the job has a failed-nodes CSV", func() {
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

			requestID := "seed-update-failedfile-" + fmt.Sprintf("%d", GinkgoRandomSeed())
			failedKey := "system/" + requestID + "_failed_node_certs.csv"
			failedPath := "s3://" + os.Getenv("FILE_BUCKET_NAME") + "/" + failedKey
			Expect(node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
				CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
					RequestID:        requestID,
					JobType:          node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
					Status:           node_reg_req_db.NODE_REG_STATUS_COMPLETED,
					FailedFileS3Path: failedPath,
				})).To(Succeed())

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/" + requestID
			baseReq.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var status UpdateNodeStatusResponse
			Expect(json.Unmarshal([]byte(response.Body), &status)).To(Succeed())
			Expect(status.FailedFileS3Path).To(Equal(failedPath))
			Expect(status.FailedFileDownloadURL).To(ContainSubstring(failedKey))
			Expect(status.FailedFileDownloadURL).To(ContainSubstring("X-Amz-Signature"))
		})
	})

	Describe("GET .../failed-nodes", func() {
		It("returns failures for an update job", func() {
			requestID := seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE, "", []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "n1", Reason: "node not found"},
				{NodeID: "n2", Reason: "tag write failed"},
			})

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/" + requestID + "/failed-nodes"
			baseReq.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListFailedNodesResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.FailedNodes).To(HaveLen(2))
		})

		It("returns 404 when the request_id is a registration job", func() {
			requestID := seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER, "", []node_reg_failed_nodes_db.NodeRegFailedNodeEntry{
				{NodeID: "n1", Reason: "x"},
			})

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs/" + requestID + "/failed-nodes"
			baseReq.PathParameters = map[string]string{"requestId": requestID}

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Routing", func() {
		It("returns 404 for paths outside the update-jobs tree", func() {
			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/something-else"

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 405-equivalent (404) for an unsupported method on update-jobs", func() {
			baseReq.HTTPMethod = "DELETE"
			baseReq.Path = "/v1/admin/nodes/update-jobs"

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
