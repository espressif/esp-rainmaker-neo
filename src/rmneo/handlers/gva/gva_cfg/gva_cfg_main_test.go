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
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/gva"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGVACfg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GVA Config Suite")
}

const vaClientID = "va-client"

// seedVAClient adds the espuser-oauth-clients table and seeds the confidential va-client row Alexa/GVA update.
func seedVAClient(ctx context.Context) {
	awscommon.GetDynamoDBClient().(*mock.DynamoDBMock).AddTable("espuser-oauth-clients", "client_id", "")
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	_, err := svc.Create(clients.CreateInput{
		ClientID:   vaClientID,
		ClientName: "Voice Assistant",
		ClientType: "confidential",
		GrantTypes: []string{"authorization_code", "refresh_token"},
		Scopes:     []string{"openid", "email", "phone", "profile"},
	})
	Expect(err).To(BeNil())
}

// vaClientURIs reads the seeded va-client row's currently registered redirect URIs.
func vaClientURIs(ctx context.Context) []string {
	c, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(vaClientID)
	Expect(err).To(BeNil())
	return c.RedirectURIs
}

var _ = BeforeSuite(func() {
	fmt.Printf("Setting environment variables for testing\n")
	os.Setenv("GVA_SKILL_FUNCTION_NAME", "test-gva-skill-function")
	os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
})

var _ = Describe("GVA Config", func() {
	var (
		ctx     context.Context
		req     gva.ServiceAccount
		request events.APIGatewayProxyRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		seedVAClient(ctx)
		userID := "51bbf520-70a1-70ac-627f-07d1af2a930c"

		// Set up super admin user using helper function
		_, _ = test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

		// Set up dummy non-admin user (in the admin pool) for the authorization test
		_, _ = test_utils.SetupTestNonAdminUserInAdminPool(ctx, "test-dummy-user-id", "test-dummy-user-email")

		// Setup default request (service account JSON)
		req = gva.ServiceAccount{
			Type:                    "service_account",
			ProjectID:               "test-project-id",
			PrivateKeyID:            "test-private-key-id",
			PrivateKey:              "-----BEGIN PRIVATE KEY-----\ntest-private-key\n-----END PRIVATE KEY-----\n",
			ClientEmail:             "test@test-project-id.iam.gserviceaccount.com",
			ClientID:                "123456789",
			AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
			TokenURI:                "https://oauth2.googleapis.com/token",
			AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
			ClientX509CertURL:       "https://www.googleapis.com/robot/v1/metadata/x509/test",
			UniverseDomain:          "googleapis.com",
		}

		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	Describe("POST /v1/admin/integrations/gva/configuration", func() {
		It("should successfully configure GVA settings", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			// Validate that the service account JSON is stored as a single SSM parameter
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters[gva.GVASSMServiceAccountJSONParam]).NotTo(BeNil())

			// Validate that the redirect URI is registered on the OIDC va-client row
			Expect(vaClientURIs(ctx)).To(Equal([]string{
				"https://oauth-redirect.googleusercontent.com/r/test-project-id", // Auto-calculated from project_id
			}))
		})

		It("should avoid duplicate redirect URIs", func() {
			// This test is no longer relevant since we don't accept additional redirect URIs
			// GVA only uses the auto-calculated URL from project_id
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Should contain only the auto-calculated URL
			Expect(vaClientURIs(ctx)).To(Equal([]string{
				"https://oauth-redirect.googleusercontent.com/r/test-project-id", // Auto-calculated from project_id
			}))
		})

		It("should work without additional redirect URIs", func() {
			// This test is now redundant since we always only use the auto-calculated URL
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			// Validate that the service account JSON is stored
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters[gva.GVASSMServiceAccountJSONParam]).NotTo(BeNil())

			// Should contain only the auto-calculated URL
			Expect(vaClientURIs(ctx)).To(Equal([]string{
				"https://oauth-redirect.googleusercontent.com/r/test-project-id", // Auto-calculated from project_id
			}))
		})

		It("should be idempotent when called twice with same config", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			// First call
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Second call with same config
			response, err = handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should update values when called with different config", func() {
			// First call with original config
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Second call with updated config
			updatedReq := gva.ServiceAccount{
				Type:                    "service_account",
				ProjectID:               "updated-project-id",
				PrivateKeyID:            "updated-private-key-id",
				PrivateKey:              "-----BEGIN PRIVATE KEY-----\nupdated-key\n-----END PRIVATE KEY-----\n",
				ClientEmail:             "updated@updated-project-id.iam.gserviceaccount.com",
				ClientID:                "987654321",
				AuthURI:                 "https://accounts.google.com/o/oauth2/auth",
				TokenURI:                "https://oauth2.googleapis.com/token",
				AuthProviderX509CertURL: "https://www.googleapis.com/oauth2/v1/certs",
				ClientX509CertURL:       "https://www.googleapis.com/robot/v1/metadata/x509/updated",
				UniverseDomain:          "googleapis.com",
			}
			requestBody, _ = json.Marshal(updatedReq)
			request.Body = string(requestBody)

			response, err = handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify SSM parameter was updated with the new JSON
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters[gva.GVASSMServiceAccountJSONParam]).NotTo(BeNil())

			// Verify the OIDC va-client redirect URIs contain the new project URI (unioned onto existing)
			Expect(vaClientURIs(ctx)).To(ContainElement("https://oauth-redirect.googleusercontent.com/r/updated-project-id"))
		})

		It("should fail with missing required fields", func() {
			req.ProjectID = ""
			req.ClientEmail = ""
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("failed to validate request"))
		})

		It("should fail with invalid type", func() {
			req.Type = "not_service_account"
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("failed to validate request"))
		})

		It("should fail for non-admin user", func() {
			requestBody, _ := json.Marshal(req)
			nonAdminRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoAuthenticationProvider: ":CognitoSignIn:test-dummy-user-id",
					},
				},
			}

			response, err := handler(ctx, nonAdminRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})

		It("should fail when OIDC_VA_CLIENT_ID is not set", func() {
			os.Setenv("OIDC_VA_CLIENT_ID", "")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("OIDC_VA_CLIENT_ID not configured"))

			// Restore environment variable
			os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
		})

		It("should fail when the OIDC va-client cannot be resolved", func() {
			// Remove the seeded va-client row so the registry read fails.
			Expect(clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Delete(vaClientID)).To(BeNil())
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("failed to update OIDC va-client"))
		})
	})

	Describe("GET /v1/admin/integrations/gva/configuration", func() {
		It("should successfully get GVA configuration", func() {
			// First store configuration
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			_, err := handler(ctx, request)
			Expect(err).To(BeNil())

			// Now get the configuration
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var respCfg GVAGetCfgResponse
			Expect(json.Unmarshal([]byte(response.Body), &respCfg)).To(Succeed())
			Expect(respCfg).To(Equal(GVAGetCfgResponse{
				Type:                    req.Type,
				ProjectID:               req.ProjectID,
				PrivateKeyID:            req.PrivateKeyID,
				ClientEmail:             req.ClientEmail,
				ClientID:                req.ClientID,
				AuthURI:                 req.AuthURI,
				TokenURI:                req.TokenURI,
				AuthProviderX509CertURL: req.AuthProviderX509CertURL,
				ClientX509CertURL:       req.ClientX509CertURL,
				UniverseDomain:          req.UniverseDomain,
				RedirectURIs:            []string{"https://oauth-redirect.googleusercontent.com/r/" + req.ProjectID},
			}))

			// M-13: the stored private key must never leave the backend via GET.
			var respBody map[string]interface{}
			Expect(json.Unmarshal([]byte(response.Body), &respBody)).To(Succeed())
			Expect(respBody).NotTo(HaveKey("private_key"))
		})

		It("should return not found when GVA is not configured", func() {
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("GVA configuration not found"))
		})

		It("should fail when SSM parameter retrieval fails", func() {
			// Break only the service-account read: failing every read would also break the JWKS
			// read the request's own authorization depends on, testing nothing about this handler.
			awscommon.GetSSMClient().(*mock.MockSSM).GetParameterErrors[gva.GVASSMServiceAccountJSONParam] = fmt.Errorf("simulated error")
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("GVA configuration not found"))
		})
	})

	Describe("DELETE /v1/admin/integrations/gva/configuration", func() {
		It("should successfully delete GVA configuration", func() {
			// First store configuration
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			_, err := handler(ctx, request)
			Expect(err).To(BeNil())

			// Now delete the configuration
			request.HTTPMethod = "DELETE"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			// Verify parameter is actually deleted
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.DeletedParameters).To(ContainElement(gva.GVASSMServiceAccountJSONParam))
		})

		It("should fail when SSM parameter deletion fails", func() {
			awscommon.GetSSMClient().(*mock.MockSSM).DeleteParameterError = fmt.Errorf("simulated error")
			request.HTTPMethod = "DELETE"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("failed to delete configuration"))
		})
	})

	Describe("Unsupported HTTP methods", func() {
		It("should return method not allowed for unsupported methods", func() {
			request.HTTPMethod = "PUT"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			Expect(response.Body).To(ContainSubstring("Method not allowed"))
		})
	})
})
