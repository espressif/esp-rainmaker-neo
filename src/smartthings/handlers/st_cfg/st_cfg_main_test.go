// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSTCfg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SmartThings Config Suite")
}

const vaClientID = "va-client"

// seedVAClient adds the espuser-oauth-clients table and seeds the confidential va-client row Alexa/GVA/SmartThings update.
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
	os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
})

var _ = Describe("SmartThings Config", func() {
	var (
		ctx     context.Context
		request events.APIGatewayProxyRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		seedVAClient(ctx)
		os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
		userID := "51bbf520-70a1-70ac-627f-07d1af2a930c"

		// Set up super admin user
		_, _ = test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

		// Set up non-admin user for authorization tests
		_, _ = test_utils.SetupTestNonAdminUserInAdminPool(ctx, "test-dummy-user-id", "test-dummy-user-email")

		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	Describe("POST /v1/admin/integrations/smartthings/configuration", func() {
		It("should successfully store SmartThings configuration", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters[STSSMClientIDParam]).NotTo(BeNil())
			Expect(ssmMock.Parameters[STSSMClientSecretParam]).NotTo(BeNil())

			// The SmartThings callback URLs are registered on the OIDC va-client row
			Expect(vaClientURIs(ctx)).To(ContainElements(stRedirectURIs))
		})

		It("should fail when OIDC_VA_CLIENT_ID is not set", func() {
			os.Setenv("OIDC_VA_CLIENT_ID", "")
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("OIDC_VA_CLIENT_ID not configured"))
		})

		It("should overwrite previously stored values", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "first-client-id",
				ClientSecret: "first-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Store again with different values
			body, _ = json.Marshal(STCfgRequest{
				ClientID:     "second-client-id",
				ClientSecret: "second-client-secret",
			})
			request.Body = string(body)

			response, err = handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should reject empty client_id", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_id"))
			Expect(response.Body).To(ContainSubstring("must not be empty"))
		})

		It("should reject empty client_secret", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_secret"))
			Expect(response.Body).To(ContainSubstring("must not be empty"))
		})

		It("should reject client_id exceeding 256 characters", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     strings.Repeat("a", 257),
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_id"))
			Expect(response.Body).To(ContainSubstring("must not exceed 256 characters"))
		})

		It("should reject client_secret exceeding 1024 characters", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: strings.Repeat("s", maxClientSecretLength+1),
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("client_secret"))
			Expect(response.Body).To(ContainSubstring("must not exceed 1024 characters"))
		})

		It("should accept a real-length SmartThings client_secret", func() {
			// SmartThings issues 256 bytes hex-encoded, i.e. 512 characters. The
			// original 256 limit rejected every genuine credential.
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "68f8f3aa-80c2-4fc1-ac59-873e61956bbc",
				ClientSecret: strings.Repeat("a1b2c3d4", 64),
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).ToNot(Equal(http.StatusBadRequest),
				"a 512-character secret is what SmartThings actually issues: %s", response.Body)
		})

		It("should reject non-admin user with 403", func() {
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			nonAdminRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(body),
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

		It("should return 400 for invalid JSON body", func() {
			request.Body = "not-valid-json"

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("should return 500 when SSM store fails", func() {
			awscommon.GetSSMClient().(*mock.MockSSM).PutParameterError = fmt.Errorf("simulated SSM error")
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("failed to store configuration"))
		})
	})

	Describe("GET /v1/admin/integrations/smartthings/configuration", func() {
		It("should return client_id and omit client_secret", func() {
			// First store configuration
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)
			_, err := handler(ctx, request)
			Expect(err).To(BeNil())

			// Now GET
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var respBody STCfgGetResponse
			err = json.Unmarshal([]byte(response.Body), &respBody)
			Expect(err).To(BeNil())
			Expect(respBody.ClientID).To(Equal("test-client-id"))

			// Verify client_secret is not in the response
			var rawResp map[string]interface{}
			err = json.Unmarshal([]byte(response.Body), &rawResp)
			Expect(err).To(BeNil())
			Expect(rawResp).NotTo(HaveKey("client_secret"))
		})

		It("should return 404 when the integration has never been configured", func() {
			// A fresh deployment has no parameter at all. Clients tell that apart
			// from a lookup failure by the status code, and show a setup prompt
			// rather than an error.
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("SmartThings configuration not found"))
		})

		It("should return 500 when SSM get fails", func() {
			awscommon.GetSSMClient().(*mock.MockSSM).GetParameterErrors[STSSMClientIDParam] = fmt.Errorf("simulated SSM error")
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("failed to retrieve configuration"))
		})

		It("should reject non-admin user with 403", func() {
			nonAdminRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoAuthenticationProvider: ":CognitoSignIn:test-dummy-user-id",
					},
				},
			}

			response, err := handler(ctx, nonAdminRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("DELETE /v1/admin/integrations/smartthings/configuration", func() {
		It("should successfully delete SmartThings configuration", func() {
			// First store configuration
			body, _ := json.Marshal(STCfgRequest{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
			})
			request.Body = string(body)
			_, err := handler(ctx, request)
			Expect(err).To(BeNil())

			// Now DELETE
			request.HTTPMethod = "DELETE"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.DeletedParameters).To(ContainElement(STSSMClientIDParam))
			Expect(ssmMock.DeletedParameters).To(ContainElement(STSSMClientSecretParam))
		})

		It("should return 500 when SSM delete fails", func() {
			awscommon.GetSSMClient().(*mock.MockSSM).DeleteParameterError = fmt.Errorf("simulated SSM error")
			request.HTTPMethod = "DELETE"
			request.Body = ""

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("failed to delete configuration"))
		})

		It("should reject non-admin user with 403", func() {
			nonAdminRequest := events.APIGatewayProxyRequest{
				HTTPMethod: "DELETE",
				RequestContext: events.APIGatewayProxyRequestContext{
					Identity: events.APIGatewayRequestIdentity{
						CognitoAuthenticationProvider: ":CognitoSignIn:test-dummy-user-id",
					},
				},
			}

			response, err := handler(ctx, nonAdminRequest)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
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
