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
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/nodeadmin/jobs_api"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_failed_nodes_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_reg_req_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
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

	seedSeq := 0
	// seedJobWithStatus creates a parent job row; seedJob defaults status to completed.
	seedJobWithStatus := func(jobType, certPath, status string, failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) string {
		seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
		seederCtx.SetAllow(utils.NodeAdminAdd, "*")
		seederCtx.SetAllow(utils.NodeAdminRegisterStatus, "*")

		seedSeq++
		requestID := fmt.Sprintf("seed-%s-%d-%d", jobType, GinkgoRandomSeed(), seedSeq)
		err := node_reg_req_db.NewNodeRegRequestsDB(seederCtx).
			CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
				RequestID:      requestID,
				JobType:        jobType,
				CertFileS3Path: certPath,
				Status:         status,
			})
		Expect(err).NotTo(HaveOccurred())

		if len(failures) > 0 {
			err = node_reg_failed_nodes_db.NewNodeRegFailedNodesDB(seederCtx).
				RecordFailures(requestID, failures)
			Expect(err).NotTo(HaveOccurred())
		}
		return requestID
	}
	seedJob := func(jobType string, certPath string, failures []node_reg_failed_nodes_db.NodeRegFailedNodeEntry) string {
		return seedJobWithStatus(jobType, certPath, node_reg_req_db.NODE_REG_STATUS_COMPLETED, failures)
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

			var status jobs_api.StatusResponse
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

			var status jobs_api.StatusResponse
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

			var resp jobs_api.ListFailedNodesResponse
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

	Describe("GET /v1/admin/nodes/update-jobs", func() {
		It("returns an empty list when no update jobs exist", func() {
			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp jobs_api.ListResponse
			Expect(json.Unmarshal([]byte(response.Body), &listResp)).To(Succeed())
			Expect(listResp.Jobs).To(HaveLen(0))
			Expect(listResp.NextKey).To(BeEmpty())
			Expect(listResp.PageTotal).To(Equal(0))
		})

		It("lists update jobs and omits registration jobs", func() {
			updateID := seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE, "s3://bucket/update.csv", nil)
			_ = seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER, "s3://bucket/register.csv", nil)
			// Legacy / empty job_type rows are treated as register and must stay off this list.
			seederCtx := rmngctx.NewRmngContext(user.NewUser("seeder"))
			seederCtx.SetAllow(utils.NodeAdminAdd, "*")
			Expect(node_reg_req_db.NewNodeRegRequestsDB(seederCtx).CreateNodeRegRequest(node_reg_req_db.NodeRegRequestsEntry{
				RequestID: "legacy-register-hidden-from-update-list",
				UserID:    "admin",
				Status:    node_reg_req_db.NODE_REG_STATUS_COMPLETED,
			})).To(Succeed())

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp jobs_api.ListResponse
			Expect(json.Unmarshal([]byte(response.Body), &listResp)).To(Succeed())
			Expect(listResp.Jobs).To(HaveLen(1))
			Expect(listResp.PageTotal).To(Equal(1))
			Expect(listResp.Jobs[0].RequestID).To(Equal(updateID))
			Expect(listResp.Jobs[0].JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE))
			Expect(listResp.Jobs[0].CertFileS3Path).To(Equal("s3://bucket/update.csv"))
		})

		It("respects page_size and start_key pagination", func() {
			for i := 0; i < 3; i++ {
				_ = seedJob(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE, fmt.Sprintf("s3://bucket/page-%d.csv", i), nil)
			}

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			baseReq.QueryStringParameters = map[string]string{"page_size": "2"}
			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var page1 jobs_api.ListResponse
			Expect(json.Unmarshal([]byte(response.Body), &page1)).To(Succeed())
			Expect(page1.Jobs).To(HaveLen(2))
			Expect(page1.PageTotal).To(Equal(2))
			Expect(page1.NextKey).NotTo(BeEmpty())
			for _, job := range page1.Jobs {
				Expect(job.JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE))
			}

			baseReq.QueryStringParameters = map[string]string{
				"page_size": "2",
				"start_key": page1.NextKey,
			}
			response, err = handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var page2 jobs_api.ListResponse
			Expect(json.Unmarshal([]byte(response.Body), &page2)).To(Succeed())
			Expect(page2.Jobs).To(HaveLen(1))
			Expect(page2.NextKey).To(BeEmpty())

			seen := map[string]bool{}
			for _, job := range append(page1.Jobs, page2.Jobs...) {
				Expect(seen[job.RequestID]).To(BeFalse(), "duplicate request_id across pages")
				seen[job.RequestID] = true
			}
			baseReq.QueryStringParameters = nil
		})

		It("filters update jobs by status", func() {
			completedID := seedJobWithStatus(
				node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
				"s3://bucket/completed.csv",
				node_reg_req_db.NODE_REG_STATUS_COMPLETED,
				nil,
			)
			_ = seedJobWithStatus(
				node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE,
				"s3://bucket/started.csv",
				node_reg_req_db.NODE_REG_STATUS_STARTED,
				nil,
			)
			// Register job with matching status must not leak into the update list.
			_ = seedJobWithStatus(
				node_reg_req_db.NODE_REG_JOB_TYPE_REGISTER,
				"s3://bucket/register-completed.csv",
				node_reg_req_db.NODE_REG_STATUS_COMPLETED,
				nil,
			)

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			baseReq.QueryStringParameters = map[string]string{"status": node_reg_req_db.NODE_REG_STATUS_COMPLETED}
			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var listResp jobs_api.ListResponse
			Expect(json.Unmarshal([]byte(response.Body), &listResp)).To(Succeed())
			Expect(listResp.Jobs).To(HaveLen(1))
			Expect(listResp.Jobs[0].RequestID).To(Equal(completedID))
			Expect(listResp.Jobs[0].Status).To(Equal(node_reg_req_db.NODE_REG_STATUS_COMPLETED))
			Expect(listResp.Jobs[0].JobType).To(Equal(node_reg_req_db.NODE_REG_JOB_TYPE_UPDATE))

			baseReq.QueryStringParameters = map[string]string{"status": node_reg_req_db.NODE_REG_STATUS_DATA_LOADED}
			response, err = handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(json.Unmarshal([]byte(response.Body), &listResp)).To(Succeed())
			Expect(listResp.Jobs).To(HaveLen(0))

			baseReq.QueryStringParameters = nil
		})

		It("returns 403 for a non-admin user", func() {
			nonAdminID := "test-non-admin-update-list"
			test_utils.SetupTestUser(context.Background(), nonAdminID, "non-admin@example.com")

			baseReq.HTTPMethod = "GET"
			baseReq.Path = "/v1/admin/nodes/update-jobs"
			baseReq.RequestContext.Identity.CognitoIdentityID = nonAdminID
			baseReq.RequestContext.Identity.CognitoAuthenticationProvider = test_utils.OIDCAuthProvider(nonAdminID)

			response, err := handleRequest(adminCtx.Context, baseReq)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
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
