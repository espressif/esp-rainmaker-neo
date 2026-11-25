// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	alexa_skill "github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAlexaCfg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alexa Config Suite")
}

const vaClientID = "rainmaker-neo-alexa-client"

var region = "us-east-1"

// seedVAClient adds the espuser-oauth-clients table and seeds Alexa's confidential client row the handler updates.
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

// vaClientURIs reads the seeded client row's currently registered redirect URIs.
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
	alexa_skill.NewRegionalLambdaClient = func(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
		return awscommon.GetLambdaClient(), nil
	}
})

var _ = AfterSuite(func() {
	alexa_skill.NewRegionalLambdaClient = alexa_skill.DefaultRegionalLambdaClient
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
		// Real SSM returns ciphertext for an undecrypted SecureString read; the mock does so only when asked.
		awscommon.GetSSMClient().(*mock.MockSSM).StrictDecryptionPrefix = "/rmng/alexa/"
		seedVAClient(ctx)
		userID := "51bbf520-70a1-70ac-627f-07d1af2a930c"

		// Set up admin user using helper function
		_, _ = test_utils.SetupTestAdminUser(ctx, userID, "test-user-email")

		// Set up a federated end user (outside the admin pool) for the authorization test
		_, _ = test_utils.SetupTestNonAdminUser(ctx, "test-dummy-user-id", "test-dummy-user-email")

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
		// Left in place, a failed automatic run would keep showing as the deployment's state, and the itest would skip.
		It("should clear an earlier automatic setup's status", func() {
			alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusFailed, "amzn1.ask.skill.old", "set account linking", "boom")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(alexa_skill.GetSetupStatus(ctx)).To(BeNil())
		})

		It("should successfully configure Alexa settings", func() {
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))

			// Validate that the redirect URIs are registered on Alexa's OIDC client row
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
			lambdaPermission := lambdaMock.Permissions[mock.PermissionKey("rmng-alexa-skill-"+region, alexa_skill.AlexaInvokeStatementID)]
			Expect(*lambdaPermission.Action).To(Equal("lambda:InvokeFunction"))
			Expect(*lambdaPermission.Principal).To(Equal("alexa-connectedhome.amazon.com"))
			Expect(*lambdaPermission.EventSourceToken).To(Equal(req.SkillID))
		})

		It("refuses a redirect URI registration would refuse (negative)", func() {
			req.RedirectURIs = []string{"javascript:alert(1)"}
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
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

			// Verify the client's redirect URIs contain the new URI (unioned onto existing)
			Expect(vaClientURIs(ctx)).To(ContainElement("https://new-redirect.example.com/callback"))

			// Verify Lambda trigger was updated to new skill ID
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			lambdaPermission := lambdaMock.Permissions[mock.PermissionKey("rmng-alexa-skill-"+region, alexa_skill.AlexaInvokeStatementID)]
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

		It("should fail when Alexa's OIDC client cannot be resolved", func() {
			// Remove the seeded client row so the registry write fails.
			Expect(clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Delete(vaClientID)).To(BeNil())
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(response.Body).To(ContainSubstring("Failed to update the OIDC client"))
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

		It("should fail when the caller is not an admin", func() {
			request.RequestContext.Identity.CognitoIdentityID = "test-dummy-user-id"
			request.RequestContext.Identity.CognitoAuthenticationProvider = test_utils.OIDCAuthProvider("test-dummy-user-id")
			requestBody, _ := json.Marshal(req)
			request.Body = string(requestBody)

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusForbidden))
			Expect(response.Body).To(ContainSubstring("Forbidden"))
		})

		// Both used to panic in a bare type assertion, which API Gateway reports as 502.
		It("should answer 401 when the caller cannot be resolved", func() {
			request.RequestContext.Identity = events.APIGatewayRequestIdentity{}
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should answer 401 for a Cognito caller the admin pool does not know", func() {
			request.RequestContext.Identity.CognitoIdentityID = "unknown-admin-id"
			request.RequestContext.Identity.CognitoAuthenticationProvider = ":CognitoSignIn:unknown-admin-id"
			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusUnauthorized))
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
			// A deployment configured only by hand has no automatic-setup status, and the field is omitted rather than sent empty.
			Expect(response.Body).NotTo(ContainSubstring("config_status"))

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

		// A setup still running has stored no credentials, and a 404 here would hide its progress from both pollers.
		It("should return the setup status when an automatic setup has written no credentials yet", func() {
			const status = `{"status":"IN_PROGRESS","ref_id":"amzn1.ask.skill.pending","stage":"initial build","updated_at":"2026-10-26T00:00:00Z"}`
			_, err := awscommon.GetSSMClient().PutParameter(ctx, &ssm.PutParameterInput{
				Name:  aws.String(alexa_skill.AlexaSSMConfigStatusParam),
				Value: aws.String(status),
				Type:  ssmtypes.ParameterTypeString,
			})
			Expect(err).To(BeNil())
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			var body AlexaCfgGetResponse
			Expect(json.Unmarshal([]byte(response.Body), &body)).To(Succeed())
			Expect(body).To(Equal(AlexaCfgGetResponse{
				ManufacturerName: alexa_skill.DefaultManufacturerName,
				ConfigStatus: &alexa_skill.SetupStatus{
					Status: alexa_skill.SetupStatusInProgress, RefID: "amzn1.ask.skill.pending", Stage: "initial build", UpdatedAt: "2026-10-26T00:00:00Z",
				},
			}))
		})

		// An account connected for the first automatic setup is all there is before that setup runs, and the dashboard needs it to skip the consent step.
		It("should report the connected Amazon account, never its secrets, before Alexa is configured", func() {
			Expect(alexa_skill.StoreLWAConnection(ctx, alexa_skill.LWAConnection{ClientID: "lwa-id", ClientSecret: "lwa-secret", RefreshToken: "Atzr|secret"})).To(Succeed())
			request.HTTPMethod = "GET"
			request.Body = ""

			response, err := handleRequest(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			Expect(response.Body).NotTo(Or(ContainSubstring("lwa-secret"), ContainSubstring("Atzr|secret")))
			var body AlexaCfgGetResponse
			Expect(json.Unmarshal([]byte(response.Body), &body)).To(Succeed())
			Expect(body).To(Equal(AlexaCfgGetResponse{
				ManufacturerName: alexa_skill.DefaultManufacturerName,
				LWAClientID:      "lwa-id",
			}))
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
