// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFileMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "File Main Suite")
}

var _ = Describe("File Handler", func() {
	var (
		ctx     context.Context
		userID  string
		request events.APIGatewayProxyRequest
	)

	Context("Upload URL", func() {
		BeforeEach(func() {
			ctx = context.Background()
			test_utils.TestSetup()
			userID = "test-user-id"

			// Set up super admin user using helper function
			test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

			requestBody := map[string]interface{}{
				"file_type": FILE_TYPE_NODE_CERT,
			}
			bodyBytes, _ := json.Marshal(requestBody)

			request = events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Path:       "/v1/admin/files/upload-urls",
				Resource:   "/v1/admin/files/upload-urls",
				Body:       string(bodyBytes),
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
					},
				},
			}
		})

		Describe("handleGetFileUploadUrl", func() {
			Context("Node Certificate", func() {
				It("should successfully generate upload URL for node_cert with filename", func() {
					requestBody := map[string]interface{}{
						"file_type": FILE_TYPE_NODE_CERT,
						"file_name": "testfile.csv",
					}
					bodyBytes, _ := json.Marshal(requestBody)
					request.Body = string(bodyBytes)

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusCreated))

					var uploadResponse GetFileUploadUrlResponse
					err = json.Unmarshal([]byte(response.Body), &uploadResponse)
					Expect(err).To(BeNil())
					Expect(uploadResponse.UploadURL).To(ContainSubstring("rmng-files"))
					Expect(uploadResponse.UploadURL).To(ContainSubstring("X-Amz-Algorithm"))
					Expect(uploadResponse.S3Path).To(ContainSubstring("s3://"))
					Expect(uploadResponse.S3Path).To(ContainSubstring("system/node_certs/"))
					Expect(uploadResponse.S3Path).To(ContainSubstring("testfile.csv"))
				})

				It("should successfully generate upload URL for node_cert without filename", func() {
					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusCreated))

					var uploadResponse GetFileUploadUrlResponse
					err = json.Unmarshal([]byte(response.Body), &uploadResponse)
					Expect(err).To(BeNil())
					Expect(uploadResponse.UploadURL).To(ContainSubstring("rmng-files"))
					Expect(uploadResponse.S3Path).To(ContainSubstring("s3://"))
					Expect(uploadResponse.S3Path).To(ContainSubstring("system/"))
					Expect(uploadResponse.S3Path).To(ContainSubstring(".csv"))
				})

				It("should preserve existing .csv extension", func() {
					requestBody := map[string]interface{}{
						"file_type": FILE_TYPE_NODE_CERT,
						"file_name": "myfile.csv",
					}
					bodyBytes, _ := json.Marshal(requestBody)
					request.Body = string(bodyBytes)

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusCreated))

					var uploadResponse GetFileUploadUrlResponse
					err = json.Unmarshal([]byte(response.Body), &uploadResponse)
					Expect(err).To(BeNil())
					Expect(uploadResponse.S3Path).To(ContainSubstring("myfile.csv"))
					Expect(uploadResponse.S3Path).ToNot(ContainSubstring("myfile.csv.csv"))
				})
			})

			Context("Push Text Config", func() {
				It("should successfully write push text config", func() {
					requestBody := map[string]interface{}{
						"file_type": FILE_TYPE_PUSH_TEXT_CONFIG,
					}
					bodyBytes, _ := json.Marshal(requestBody)
					request.Body = string(bodyBytes)

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusCreated))

					var uploadResponse GetFileUploadUrlResponse
					err = json.Unmarshal([]byte(response.Body), &uploadResponse)
					Expect(err).To(BeNil())
					Expect(uploadResponse.S3Path).To(Equal("s3://rmng-files-" + awscommon.GetAccountId() + "-" + awscommon.GetRmngRegion() + "-an/system/push_text_config.json"))
				})
			})

			Context("with invalid requests", func() {
				It("should return error for invalid file type", func() {
					requestBody := map[string]interface{}{
						"file_type": "invalid_type",
						"file_name": "testfile",
					}
					bodyBytes, _ := json.Marshal(requestBody)
					request.Body = string(bodyBytes)

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				})

				It("should return error for missing file type", func() {
					requestBody := map[string]interface{}{
						"file_name": "testfile",
					}
					bodyBytes, _ := json.Marshal(requestBody)
					request.Body = string(bodyBytes)

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				})

				It("should return error for empty request parameters", func() {
					request.Body = "{}"

					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				})

				// UUID prefix in filenames prevents duplicates by design — same filename always gets unique S3 path
				PContext("duplicate upload scenarios (legacy — no longer applicable with unique paths)", func() {
					BeforeEach(func() {
						// Set up mock S3 to simulate file existence
						test_utils.TestSetup()

						// Re-setup user after TestSetup resets mocks
						test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

						// Re-setup request with CognitoAuthenticationProvider
						request.RequestContext.Identity.CognitoAuthenticationProvider = ":CognitoSignIn:" + userID
					})

					It("should return error when attempting to upload duplicate file with same filename and extension", func() {
						// Setup - first upload with explicit .csv extension
						requestBody := map[string]interface{}{
							"file_type": FILE_TYPE_NODE_CERT,
							"file_name": "duplicate_with_ext.csv",
						}
						bodyBytes, _ := json.Marshal(requestBody)
						request.Body = string(bodyBytes)

						// First request should succeed
						response, err := handleRequest(ctx, request)
						Expect(err).To(BeNil())
						Expect(response.StatusCode).To(Equal(http.StatusCreated))

						// Simulate the file upload
						s3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
						bucketName := "rmng-files-" + awscommon.GetAccountId() + "-" + awscommon.GetRmngRegion() + "-an"
						fileKey := "system/node_certs/duplicate_with_ext.csv" // approximate, actual has UUID prefix
						s3Client.Buckets[bucketName][fileKey] = "test content"

						// Second request with same filename should fail
						response, err = handleRequest(ctx, request)
						Expect(err).To(BeNil())
						Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
					})

					It("should allow same filename for different users", func() {
						// First user uploads a file
						requestBody := map[string]interface{}{
							"file_type": FILE_TYPE_NODE_CERT,
							"file_name": "same_filename",
						}
						bodyBytes, _ := json.Marshal(requestBody)
						request.Body = string(bodyBytes)

						response, err := handleRequest(ctx, request)
						Expect(err).To(BeNil())
						Expect(response.StatusCode).To(Equal(http.StatusCreated))

						// Simulate first user's file upload
						s3Client := awscommon.GetS3Client().(*mock.S3ClientMock)
						bucketName := "rmng-files-" + awscommon.GetAccountId() + "-" + awscommon.GetRmngRegion() + "-an"
						fileKey1 := "USER_" + userID + "/FILE_TYPE_node_cert/same_filename.csv"
						s3Client.Buckets[bucketName][fileKey1] = "test content"

						// Different user uploads file with same name (should succeed as different S3 path)
						differentUserID := "different-user-id"

						// Set up second user using helper function
						test_utils.SetupTestAdminUser(ctx, differentUserID, "test-user2-email")

						requestBody2 := map[string]interface{}{
							"file_type": FILE_TYPE_NODE_CERT,
							"file_name": "same_filename",
						}
						bodyBytes2, _ := json.Marshal(requestBody2)

						request2 := events.APIGatewayProxyRequest{
							HTTPMethod: "POST",
							Path:       "/v1/admin/files/upload-urls",
							Resource:   "/v1/admin/files/upload-urls",
							Body:       string(bodyBytes2),
							RequestContext: events.APIGatewayProxyRequestContext{
								Identity: events.APIGatewayRequestIdentity{
									CognitoIdentityID:             differentUserID,
									CognitoAuthenticationProvider: ":CognitoSignIn:" + differentUserID,
								},
							},
						}

						response, err = handleRequest(ctx, request2)
						Expect(err).To(BeNil())
						Expect(response.StatusCode).To(Equal(http.StatusCreated))

						// Verify different S3 paths
						var uploadResponse GetFileUploadUrlResponse
						err = json.Unmarshal([]byte(response.Body), &uploadResponse)
						Expect(err).To(BeNil())
						Expect(uploadResponse.S3Path).To(ContainSubstring("same_filename.csv"))
					})
				})
			})
		})
	})
	Context("File Template", func() {
		BeforeEach(func() {
			ctx = context.Background()
			test_utils.TestSetup()
			userID = "test-user-id"

			// Set up super admin user using helper function
			test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

			request = events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Path:       "/v1/admin/file-templates/push_text_config",
				Resource:   "/v1/admin/file-templates/{templateType}",
				PathParameters: map[string]string{
					"templateType": FILE_TYPE_PUSH_TEXT_CONFIG,
				},
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoIdentityID:             userID,
						CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
					},
				},
			}
		})

		Describe("handleGetFileTemplate", func() {
			Context("with valid requests", func() {
				It("should successfully return push text config", func() {
					response, err := handleRequest(ctx, request)
					Expect(err).To(BeNil())
					Expect(response.StatusCode).To(Equal(http.StatusOK))
					var pushTextConfig push.PushTextConfig
					err = json.Unmarshal([]byte(response.Body), &pushTextConfig)
					Expect(err).To(BeNil())
					Expect(pushTextConfig.Default.Event["node_alert"]).ToNot(BeNil())
				})
			})
		})
	})
})
