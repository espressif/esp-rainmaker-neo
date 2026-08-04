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

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAlexaCfg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alexa Config Suite")
}

const vaClientID = "va-client"

var region = "us-east-1"

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
	os.Setenv("RMNG_REGION", region)
	os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
	// Route regional AddPermission to the global mock (production uses a client per Alexa region).
	newRegionalLambdaClient = func(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
		return awscommon.GetLambdaClient(), nil
	}
})

var _ = AfterSuite(func() {
	newRegionalLambdaClient = defaultRegionalLambdaClient
})

var _ = Describe("Alexa Config", func() {
	var (
		ctx     context.Context
		req     AlexaCfgRequest
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

		// Setup default request
		req = AlexaCfgRequest{
			RedirectURIs: []string{"https://pitangui.amazon.com/api/skill/link/ABCD1234", "https://layla.amazon.com/api/skill/link/ABCD1234"},
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			SkillID:      "amzn1.ask.skill.test-skill-id",
		}

		request = events.APIGatewayProxyRequest{
			HTTPMethod: "POST",
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             userID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + userID,
				},
			},
		}
	})

	Describe("handleAlexaCfg", func() {
		It("should successfully configure Alexa settings", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Validate that the redirect URIs are registered on the OIDC va-client row
			Expect(vaClientURIs(ctx)).To(Equal([]string{
				"https://pitangui.amazon.com/api/skill/link/ABCD1234",
				"https://layla.amazon.com/api/skill/link/ABCD1234",
			}))

			// Validate that the SSM parameter is set correctly
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters["/rmng/alexa/client_id"].Value).To(Equal(&req.ClientID))
			Expect(ssmMock.Parameters["/rmng/alexa/client_secret"].Value).To(Equal(&req.ClientSecret))

			// Validate that the Lambda trigger is set correctly
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaPermission := lambdaMock.Permissions["rmng-alexa-skill-"+region]
			Expect(*lambdaPermission.Action).To(Equal("lambda:InvokeFunction"))
			Expect(*lambdaPermission.Principal).To(Equal("alexa-connectedhome.amazon.com"))
			Expect(*lambdaPermission.EventSourceToken).To(Equal(req.SkillID))
		})

		It("should be idempotent when called twice with same config", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			// First call
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Second call with same config
			response, err = handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
		})

		It("should update values when called with different config", func() {
			// First call with original config
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Second call with updated config
			updatedReq := AlexaCfgRequest{
				RedirectURIs: []string{"https://new-redirect.example.com/callback"},
				ClientID:     "updated-client-id",
				ClientSecret: "updated-client-secret",
				SkillID:      "amzn1.ask.skill.updated-skill-id",
			}
			requestBody, _ = json.Marshal(updatedReq)
			request.Body = string(requestBody)

			response, err = handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Verify SSM parameters were updated
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters["/rmng/alexa/client_id"].Value).To(Equal(&updatedReq.ClientID))
			Expect(ssmMock.Parameters["/rmng/alexa/client_secret"].Value).To(Equal(&updatedReq.ClientSecret))
			Expect(ssmMock.Parameters["/rmng/alexa/skill_id"].Value).To(Equal(&updatedReq.SkillID))

			// Verify the OIDC va-client redirect URIs contain the new URI (unioned onto existing)
			Expect(vaClientURIs(ctx)).To(ContainElement("https://new-redirect.example.com/callback"))

			// Verify Lambda trigger was updated to new skill ID
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaPermission := lambdaMock.Permissions["rmng-alexa-skill-"+region]
			Expect(*lambdaPermission.EventSourceToken).To(Equal(updatedReq.SkillID))
		})

		It("should store the manufacturer name when one is supplied", func() {
			req.ManufacturerName = utils.Ptr("Acme Devices")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(*ssmMock.Parameters[alexa_skill.AlexaSSMManufacturerNameParam].Value).To(Equal("Acme Devices"))
		})

		It("should clear the manufacturer name by deleting the parameter, not storing empty", func() {
			// SSM rejects an empty parameter value, so resetting to the default brand has to
			// delete the parameter instead of overwriting it with "".
			req.ManufacturerName = utils.Ptr("Acme Devices")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			req.ManufacturerName = utils.Ptr("")
			requestBody, _ = json.Marshal(req)
			request.Body = string(requestBody)
			response, err = handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.DeletedParameters).To(ContainElement(alexa_skill.AlexaSSMManufacturerNameParam))
			Expect(ssmMock.Parameters).ToNot(HaveKey(alexa_skill.AlexaSSMManufacturerNameParam))
		})

		It("should leave a stored manufacturer name untouched when the field is omitted", func() {
			// Rotating credentials must not silently reset an OEM's branding.
			req.ManufacturerName = utils.Ptr("Acme Devices")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			req.ManufacturerName = nil
			req.ClientSecret = "rotated-client-secret"
			requestBody, _ = json.Marshal(req)
			request.Body = string(requestBody)
			response, err = handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(*ssmMock.Parameters[alexa_skill.AlexaSSMManufacturerNameParam].Value).To(Equal("Acme Devices"))
			Expect(*ssmMock.Parameters[alexa_skill.AlexaSSMClientSecretParam].Value).To(Equal("rotated-client-secret"))
		})

		It("should fail with missing redirect URIs", func() {
			req.RedirectURIs = []string{}
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing redirect URIs"))
		})

		It("should fail with missing client ID", func() {
			req.ClientID = ""
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing client ID"))
		})

		It("should fail with missing client secret", func() {
			req.ClientSecret = ""
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing client secret"))
		})

		It("should fail with missing skill ID", func() {
			req.SkillID = ""
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Missing skill ID"))
		})

		It("should fail when RMNG_REGION is not configured", func() {
			_ = os.Unsetenv("RMNG_REGION")
			defer func() { _ = os.Setenv("RMNG_REGION", region) }()
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("RMNG_REGION not configured"))
		})

		It("should fail when the OIDC va-client cannot be resolved", func() {
			// Remove the seeded va-client row so the registry read fails.
			Expect(clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Delete(vaClientID)).To(BeNil())
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to update OIDC va-client"))
		})

		It("should fail when SSM parameter store fails", func() {
			awscommon.GetSSMClient().(*mock.MockSSM).PutParameterError = fmt.Errorf("simulated error")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to store"))
		})

		It("should fail with invalid request body", func() {
			request.Body = "invalid json"

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(response.Body).To(ContainSubstring("Invalid request body"))
		})

		It("should fail with method not allowed for unsupported methods", func() {
			request.HTTPMethod = "DELETE"

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusMethodNotAllowed))
			Expect(response.Body).To(ContainSubstring("Method not allowed"))
		})

		It("should fail when Lambda permission update fails", func() {
			awscommon.GetLambdaClient().(*mock.LambdaMock).AddPermissionError = fmt.Errorf("simulated error")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to add Alexa trigger"))
		})

		It("should fail when user is not super admin", func() {
			request.RequestContext.Identity.CognitoIdentityID = "test-dummy-user-id"
			request.RequestContext.Identity.CognitoAuthenticationProvider = ":CognitoSignIn:test-dummy-user-id"
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})
	})

	Describe("GET /v1/admin/integrations/alexa/configuration", func() {
		It("should successfully get Alexa configuration", func() {
			// First store configuration via POST
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			_, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())

			// Now get the configuration
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var respBody AlexaCfgGetResponse
			err = json.Unmarshal([]byte(response.Body), &respBody)
			Expect(err).To(BeNil())
			Expect(respBody.ClientID).To(Equal(req.ClientID))
			Expect(respBody.SkillID).To(Equal(req.SkillID))
			Expect(respBody.RedirectURIs).To(Equal(req.RedirectURIs))
		})

		It("should report the default manufacturer name when none is configured", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			request.HTTPMethod = "GET"
			request.Body = ""
			response, err = handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			var respBody AlexaCfgGetResponse
			Expect(json.Unmarshal([]byte(response.Body), &respBody)).To(Succeed())
			Expect(respBody.ManufacturerName).To(Equal(alexa_skill.DefaultManufacturerName))
		})

		It("should report an updated manufacturer name rather than a cached one", func() {
			// Reads are cached, so storing has to invalidate the cache: otherwise this GET would
			// still report the brand the second POST replaced.
			getManufacturerName := func() string {
				request.HTTPMethod = "GET"
				request.Body = ""
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))

				var respBody AlexaCfgGetResponse
				Expect(json.Unmarshal([]byte(response.Body), &respBody)).To(Succeed())

				return respBody.ManufacturerName
			}

			postManufacturerName := func(manufacturerName string) {
				req.ManufacturerName = utils.Ptr(manufacturerName)
				requestBody, _ := json.Marshal(req)
				request.HTTPMethod = "POST"
				request.Body = string(requestBody)
				response, err := handleRequest(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(http.StatusOK))
			}

			postManufacturerName("First Brand")
			Expect(getManufacturerName()).To(Equal("First Brand"))

			postManufacturerName("Second Brand")
			Expect(getManufacturerName()).To(Equal("Second Brand"))
		})

		It("should return not found when Alexa is not configured", func() {
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("Alexa configuration not found"))
		})

		It("should fail when SSM parameter retrieval fails", func() {
			// Break only the Alexa client ID read: failing every read would also break the JWKS
			// read the request's own authorization depends on, testing nothing about this handler.
			awscommon.GetSSMClient().(*mock.MockSSM).GetParameterErrors[alexa_skill.AlexaSSMClientIDParam] = fmt.Errorf("simulated error")
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusNotFound))
			Expect(response.Body).To(ContainSubstring("Alexa configuration not found"))
		})
	})
})
