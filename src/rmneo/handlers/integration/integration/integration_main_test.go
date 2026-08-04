// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = Describe("Admin Integrations", func() {
	var (
		ctx     context.Context
		request events.APIGatewayProxyRequest
		userID  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		userID = "51bbf520-70a1-70ac-627f-07d1af2a930c"

		_, _ = test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

		// Pre-populate SNS mock with a few platform applications for list/get tests.
		snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
		mockPlatforms := []types.PlatformApplication{
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/APNS/RainMaker")},
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/APNS_SANDBOX/RainMaker")},
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/GCM/RainMaker")},
		}
		snsClient.SetMockPlatformApplications(mockPlatforms)

		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			// Admin tree: handleRequest distinguishes admin vs public by the
			// resource template, so the admin specs must carry an "/admin/" path.
			Resource: "/v1/admin/integrations",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	// Matches the GCM/RainMaker entry in the mock platform list so list/get tests can exercise a stored credential.
	rainMakerGSA := googleServiceAccount{
		Type:                    "service_account",
		ProjectID:               "RainMaker",
		PrivateKeyID:            "rainmaker-key-id",
		PrivateKey:              "-----BEGIN PRIVATE KEY-----\nrainmaker-key\n-----END PRIVATE KEY-----",
		ClientEmail:             "push@rainmaker.iam.gserviceaccount.com",
		ClientID:                "123456789",
		AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
		TokenURI:                "https://oauth2.googleapis.com/token",
		AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
		ClientX509CertURL:       "https://www.googleapis.com/robot/v1/metadata/x509/push%40rainmaker.iam.gserviceaccount.com",
		UniverseDomain:          "googleapis.com",
	}

	// Stores a GCM credential through the real PUT flow so GET tests verify what a configured integration leaks back.
	storeGCMCredential := func(sa googleServiceAccount) {
		putReq := request
		putReq.HTTPMethod = "PUT"
		putReq.PathParameters = map[string]string{"integrationId": "gcm_" + sa.ProjectID}
		body, err := json.Marshal(RegisterIntegrationRequest{googleServiceAccount: sa})
		Expect(err).To(BeNil())
		putReq.Body = string(body)

		response, err := handleRequest(ctx, putReq)
		Expect(err).To(BeNil())
		Expect(response.StatusCode).To(Equal(http.StatusOK))
	}

	Describe("POST /v1/admin/integrations", func() {
		Context("apns", func() {
			It("registers an apns integration", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "apns"}
				req := RegisterIntegrationRequest{
					AuthenticationKey: "-----BEGIN PRIVATE KEY-----\ntest-p8-key-content\n-----END PRIVATE KEY-----",
					KeyID:             "ABC123DEF4",
					TeamID:            "TEAM123456",
					BundleID:          "com.test.app",
				}
				body, _ := json.Marshal(req)
				request.Body = string(body)

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusCreated))

				var resp RegisterIntegrationResponse
				Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
				Expect(resp.IntegrationID).To(Equal("apns_com.test.app"))

				snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
				createCalls := snsClient.GetCreatePlatformApplicationCalls()
				Expect(createCalls).To(HaveLen(1))
				call := createCalls[0]
				Expect(*call.Name).To(Equal("com.test.app"))
				Expect(*call.Platform).To(Equal("APNS"))
				Expect(call.Attributes["PlatformCredential"]).To(Equal(req.AuthenticationKey))
				Expect(call.Attributes["PlatformPrincipal"]).To(Equal("ABC123DEF4"))
				Expect(call.Attributes["ApplePlatformTeamID"]).To(Equal("TEAM123456"))
				Expect(call.Attributes["ApplePlatformBundleID"]).To(Equal("com.test.app"))
			})

			It("registers an apns_sandbox integration", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "apns_sandbox"}
				req := RegisterIntegrationRequest{
					AuthenticationKey: "-----BEGIN PRIVATE KEY-----\ntest-p8-key-content\n-----END PRIVATE KEY-----",
					KeyID:             "ABC123DEF4",
					TeamID:            "TEAM123456",
					BundleID:          "com.test.app",
				}
				body, _ := json.Marshal(req)
				request.Body = string(body)

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusCreated))

				snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
				Expect(snsClient.GetCreatePlatformApplicationCalls()).To(HaveLen(1))
				Expect(*snsClient.GetCreatePlatformApplicationCalls()[0].Platform).To(Equal("APNS_SANDBOX"))

				var resp RegisterIntegrationResponse
				Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
				Expect(resp.IntegrationID).To(Equal("apns_sandbox_com.test.app"))
			})

			It("fails with missing authentication key", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "apns"}
				req := RegisterIntegrationRequest{
					KeyID:    "ABC123DEF4",
					TeamID:   "TEAM123456",
					BundleID: "com.test.app",
				}
				body, _ := json.Marshal(req)
				request.Body = string(body)

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				Expect(response.Body).To(ContainSubstring("Authentication key, key ID, team ID, and bundle ID are required for APNS token-based authentication"))
			})
		})

		Context("gcm", func() {
			gcmSA := googleServiceAccount{
				Type:                    "service_account",
				ProjectID:               "test-project-123",
				PrivateKeyID:            "key123",
				PrivateKey:              "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----",
				ClientEmail:             "test@test-project-123.iam.gserviceaccount.com",
				ClientID:                "123456789",
				AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
				TokenURI:                "https://oauth2.googleapis.com/token",
				AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
				ClientX509CertURL:       "https://www.googleapis.com/robot/v1/metadata/x509/test%40test-project-123.iam.gserviceaccount.com",
				UniverseDomain:          "googleapis.com",
			}

			It("registers an gcm integration", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "gcm"}
				req := RegisterIntegrationRequest{googleServiceAccount: gcmSA}
				body, _ := json.Marshal(req)
				request.Body = string(body)

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusCreated))

				var resp RegisterIntegrationResponse
				Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
				Expect(resp.IntegrationID).To(Equal("gcm_test-project-123"))

				snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
				calls := snsClient.GetCreatePlatformApplicationCalls()
				Expect(calls).To(HaveLen(1))
				Expect(*calls[0].Name).To(Equal("test-project-123"))
				Expect(*calls[0].Platform).To(Equal("GCM"))
				// PlatformCredential is the marshalled GSA JSON.
				cred := calls[0].Attributes["PlatformCredential"]
				Expect(cred).To(ContainSubstring(`"project_id":"test-project-123"`))
				Expect(cred).To(ContainSubstring(`"client_email":"test@test-project-123.iam.gserviceaccount.com"`))
			})

			It("fails with missing required GSA fields", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "gcm"}
				request.Body = `{}`
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				Expect(response.Body).To(ContainSubstring("All Google service-account fields are required for GCM"))
			})
		})

		Context("validation", func() {
			It("fails with unsupported integration_type", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "invalid"}
				request.Body = `{}`
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(response.Body).To(ContainSubstring("Unsupported integration_type: invalid"))
			})

			It("fails with missing integration_type query param", func() {
				request.Body = `{}`
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(response.Body).To(ContainSubstring("Missing integration_type query parameter"))
			})

			It("fails with invalid request body", func() {
				request.QueryStringParameters = map[string]string{"integration_type": "apns"}
				request.Body = "invalid-json"
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(response.Body).To(ContainSubstring("Invalid request body"))
			})
		})

		Context("authorization", func() {
			It("requires super-admin", func() {
				nonAdminUserID := "non-admin-user"
				_, _ = test_utils.SetupTestNonAdminUserInAdminPool(ctx, nonAdminUserID, "non-admin-user-email")
				request.RequestContext.Identity.CognitoAuthenticationProvider = ":CognitoSignIn:" + nonAdminUserID
				request.QueryStringParameters = map[string]string{"integration_type": "apns"}
				request.Body = `{}`

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusForbidden))
				Expect(response.Body).To(ContainSubstring("Forbidden"))
			})
		})

		Context("SNS errors", func() {
			It("handles CreatePlatformApplication errors", func() {
				awscommon.GetSNSClient().(*mock.SNSMock).CreatePlatformApplicationError = fmt.Errorf("simulated SNS error")
				request.QueryStringParameters = map[string]string{"integration_type": "apns"}
				req := RegisterIntegrationRequest{
					AuthenticationKey: "-----BEGIN PRIVATE KEY-----\ntest-p8-key-content\n-----END PRIVATE KEY-----",
					KeyID:             "ABC123DEF4",
					TeamID:            "TEAM123456",
					BundleID:          "com.test.app",
				}
				body, _ := json.Marshal(req)
				request.Body = string(body)

				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
				Expect(response.Body).To(ContainSubstring("Failed to create platform application"))
			})
		})
	})

	Describe("GET /v1/admin/integrations (list)", func() {
		It("lists all configured integrations", func() {
			request.HTTPMethod = "GET"
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListIntegrationsResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Integrations).To(HaveLen(3))

			ids := make([]string, 0, len(resp.Integrations))
			for _, it := range resp.Integrations {
				ids = append(ids, it.IntegrationID+"/"+it.IntegrationType)
			}
			Expect(ids).To(ContainElements(
				"apns_RainMaker/apns",
				"apns_sandbox_RainMaker/apns_sandbox",
				"gcm_RainMaker/gcm",
			))

			// Each GCM list entry includes the full Google service-account JSON
			// when one is stored (here the test fixture only registers ARNs;
			// project_id still falls out of integration_id as the fallback).
			for _, it := range resp.Integrations {
				if it.IntegrationType == "gcm" {
					Expect(it.ProjectID).To(Equal("RainMaker"))
				}
				if it.IntegrationType == "apns" || it.IntegrationType == "apns_sandbox" {
					Expect(it.BundleID).To(Equal("RainMaker"))
				}
			}
		})

		It("filters by integration_type query param", func() {
			request.HTTPMethod = "GET"
			request.QueryStringParameters = map[string]string{"integration_type": "gcm"}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListIntegrationsResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Integrations).To(HaveLen(1))
			Expect(resp.Integrations[0].IntegrationID).To(Equal("gcm_RainMaker"))
			Expect(resp.Integrations[0].IntegrationType).To(Equal("gcm"))
			Expect(resp.Integrations[0].ProjectID).To(Equal("RainMaker"))
		})

		It("omits the stored GCM private key from the list response", func() {
			storeGCMCredential(rainMakerGSA)

			request.HTTPMethod = "GET"
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListIntegrationsResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			for _, integration := range resp.Integrations {
				if integration.IntegrationType == "gcm" {
					Expect(integration.ClientEmail).To(Equal(rainMakerGSA.ClientEmail))
					Expect(integration.PrivateKeyID).To(Equal(rainMakerGSA.PrivateKeyID))
					Expect(integration.PrivateKey).To(BeEmpty())
				}
			}
			Expect(response.Body).NotTo(ContainSubstring(`"private_key":`))
		})

		It("requires super-admin", func() {
			nonAdminUserID := "non-admin-user"
			_, _ = test_utils.SetupTestNonAdminUserInAdminPool(ctx, nonAdminUserID, "non-admin-user-email")
			request.RequestContext.Identity.CognitoAuthenticationProvider = ":CognitoSignIn:" + nonAdminUserID
			request.HTTPMethod = "GET"

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("GET /v1/admin/integrations/{integrationId}", func() {
		It("returns apns integration details", func() {
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"integrationId": "apns_RainMaker"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp GetIntegrationResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.IntegrationID).To(Equal("apns_RainMaker"))
			Expect(resp.IntegrationType).To(Equal("apns"))
			Expect(resp.BundleID).To(Equal("RainMaker"))
		})

		It("returns gcm integration details", func() {
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"integrationId": "gcm_RainMaker"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp GetIntegrationResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.IntegrationID).To(Equal("gcm_RainMaker"))
			Expect(resp.IntegrationType).To(Equal("gcm"))
			Expect(resp.ProjectID).To(Equal("RainMaker"))
		})

		It("omits the stored GCM private key from integration details", func() {
			storeGCMCredential(rainMakerGSA)

			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"integrationId": "gcm_RainMaker"}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp GetIntegrationResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.IntegrationID).To(Equal("gcm_RainMaker"))
			Expect(resp.ProjectID).To(Equal("RainMaker"))
			Expect(resp.ClientEmail).To(Equal(rainMakerGSA.ClientEmail))
			Expect(resp.PrivateKeyID).To(Equal(rainMakerGSA.PrivateKeyID))

			// The credential itself must never round-trip through the admin GET API.
			var raw map[string]interface{}
			Expect(json.Unmarshal([]byte(response.Body), &raw)).To(Succeed())
			Expect(raw).NotTo(HaveKey("private_key"))
		})

		It("returns 404 for an unknown integration", func() {
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"integrationId": "apns_DoesNotExist"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("rejects invalid integrationId for non-push types", func() {
			request.HTTPMethod = "GET"
			request.PathParameters = map[string]string{"integrationId": "alexa"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("PUT /v1/admin/integrations/{integrationId}", func() {
		It("updates apns credentials", func() {
			request.HTTPMethod = "PUT"
			request.PathParameters = map[string]string{"integrationId": "apns_com.test.app"}

			body := RegisterIntegrationRequest{
				AuthenticationKey: "-----BEGIN PRIVATE KEY-----\nUPDATED-KEY\n-----END PRIVATE KEY-----",
				KeyID:             "UPDATED123",
				TeamID:            "TEAMUPDATED",
				BundleID:          "com.test.app",
			}
			b, _ := json.Marshal(body)
			request.Body = string(b)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
			updateCalls := snsClient.GetSetPlatformApplicationAttributesCalls()
			Expect(updateCalls).To(HaveLen(1))
			Expect(updateCalls[0].Attributes["PlatformCredential"]).To(Equal(body.AuthenticationKey))
		})

		It("updates apns_sandbox credentials", func() {
			request.HTTPMethod = "PUT"
			request.PathParameters = map[string]string{"integrationId": "apns_sandbox_com.test.app"}

			body := RegisterIntegrationRequest{
				AuthenticationKey: "-----BEGIN PRIVATE KEY-----\nUPDATED-KEY\n-----END PRIVATE KEY-----",
				KeyID:             "UPDATED123",
				TeamID:            "TEAMUPDATED",
				BundleID:          "com.test.app",
			}
			b, _ := json.Marshal(body)
			request.Body = string(b)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("updates gcm credentials", func() {
			request.HTTPMethod = "PUT"
			request.PathParameters = map[string]string{"integrationId": "gcm_test-project-123"}

			body := RegisterIntegrationRequest{
				googleServiceAccount: googleServiceAccount{
					Type:                    "service_account",
					ProjectID:               "test-project-123",
					PrivateKeyID:            "rotated_key",
					PrivateKey:              "-----BEGIN PRIVATE KEY-----\nrotated\n-----END PRIVATE KEY-----",
					ClientEmail:             "test@test-project-123.iam.gserviceaccount.com",
					ClientID:                "123456789",
					AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
					TokenURI:                "https://oauth2.googleapis.com/token",
					AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
					ClientX509CertURL:       "https://www.googleapis.com/robot/v1/metadata/x509/test%40test-project-123.iam.gserviceaccount.com",
					UniverseDomain:          "googleapis.com",
				},
			}
			b, _ := json.Marshal(body)
			request.Body = string(b)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("fails with invalid integrationId format", func() {
			request.HTTPMethod = "PUT"
			request.PathParameters = map[string]string{"integrationId": "INVALID_FORMAT"}
			request.Body = `{}`

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("fails with invalid request body", func() {
			request.HTTPMethod = "PUT"
			request.PathParameters = map[string]string{"integrationId": "apns_com.test.app"}
			request.Body = "invalid-json"

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})
	})

	Describe("DELETE /v1/admin/integrations/{integrationId}", func() {
		It("deletes an apns integration", func() {
			request.HTTPMethod = "DELETE"
			request.PathParameters = map[string]string{"integrationId": "apns_com.test.app"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
			Expect(snsClient.GetDeletePlatformApplicationCalls()).To(HaveLen(1))
		})

		It("deletes an apns_sandbox integration", func() {
			request.HTTPMethod = "DELETE"
			request.PathParameters = map[string]string{"integrationId": "apns_sandbox_com.test.app"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(awscommon.GetSNSClient().(*mock.SNSMock).GetDeletePlatformApplicationCalls()).To(HaveLen(1))
		})

		It("deletes an gcm integration", func() {
			request.HTTPMethod = "DELETE"
			request.PathParameters = map[string]string{"integrationId": "gcm_test-project-123"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(awscommon.GetSNSClient().(*mock.SNSMock).GetDeletePlatformApplicationCalls()).To(HaveLen(1))
		})

		It("fails with invalid integrationId format", func() {
			request.HTTPMethod = "DELETE"
			request.PathParameters = map[string]string{"integrationId": "INVALID_FORMAT"}

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("HTTP methods", func() {
		It("rejects unsupported methods", func() {
			request.HTTPMethod = "PATCH"

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			Expect(response.Body).To(ContainSubstring("Method not allowed"))
		})
	})
})

var _ = Describe("Public Integrations (non-admin)", func() {
	var (
		ctx     context.Context
		request events.APIGatewayProxyRequest
		userID  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		userID = "non-admin-user"

		// A plain, non-admin user: the public list must succeed for them.
		_, _ = test_utils.SetupTestNonAdminUserInAdminPool(ctx, userID, "non-admin-user-email")

		snsClient := awscommon.GetSNSClient().(*mock.SNSMock)
		snsClient.SetMockPlatformApplications([]types.PlatformApplication{
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/APNS/RainMaker")},
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/APNS_SANDBOX/RainMaker")},
			{PlatformApplicationArn: aws.String("arn:aws:sns:us-east-1:123456789012:app/GCM/RainMaker")},
		})

		request = events.APIGatewayProxyRequest{
			HTTPMethod: "GET",
			// Public tree: resource template carries no "/admin/" segment.
			Resource: "/v1/integrations",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	Describe("GET /v1/integrations", func() {
		It("lists id+type for every integration without requiring admin", func() {
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListPublicIntegrationsResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Integrations).To(HaveLen(3))

			ids := make([]string, 0, len(resp.Integrations))
			for _, it := range resp.Integrations {
				ids = append(ids, it.IntegrationID+"/"+it.IntegrationType)
			}
			Expect(ids).To(ContainElements(
				"apns_RainMaker/apns",
				"apns_sandbox_RainMaker/apns_sandbox",
				"gcm_RainMaker/gcm",
			))
		})

		It("exposes no credential fields in the response body", func() {
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			// The summary shape carries only the two public identifiers.
			Expect(response.Body).NotTo(ContainSubstring("bundle_id"))
			Expect(response.Body).NotTo(ContainSubstring("project_id"))
			Expect(response.Body).NotTo(ContainSubstring("private_key"))
		})

		It("filters by integration_type query param", func() {
			request.QueryStringParameters = map[string]string{"integration_type": "gcm"}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var resp ListPublicIntegrationsResponse
			Expect(json.Unmarshal([]byte(response.Body), &resp)).To(Succeed())
			Expect(resp.Integrations).To(HaveLen(1))
			Expect(resp.Integrations[0].IntegrationID).To(Equal("gcm_RainMaker"))
			Expect(resp.Integrations[0].IntegrationType).To(Equal("gcm"))
		})

		It("returns an empty list (not null) when nothing matches the filter", func() {
			request.QueryStringParameters = map[string]string{"integration_type": "alexa"}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).To(ContainSubstring(`"integrations":[]`))
		})

		It("surfaces SNS list failures as 500", func() {
			awscommon.GetSNSClient().(*mock.SNSMock).ListPlatformApplicationsError = fmt.Errorf("simulated SNS error")
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to list integrations"))
		})
	})

	Describe("non-GET methods on the public tree", func() {
		It("rejects POST with 405", func() {
			request.HTTPMethod = "POST"
			request.Body = `{}`
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			Expect(response.Body).To(ContainSubstring("Method not allowed"))
		})
	})
})
