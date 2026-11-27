// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	alexa_skill "github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/alexa/smapi"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAlexaAutoCfg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alexa Auto Cfg Suite")
}

const (
	vaClientID    = "rainmaker-neo-alexa-client"
	testRegion    = "us-east-1"
	testSkillID   = "amzn1.ask.skill.test"
	adminUserID   = "51bbf520-70a1-70ac-627f-07d1af2a930c"
	vaSecretParam = "/rmng/alexa/esp-user-client-secret"
	skillFn       = "rmng-alexa-skill-" + testRegion
	dummyUserID   = "test-dummy-user-id"
)

var origRegionalLambdaClient = alexa_skill.NewRegionalLambdaClient

// liveGrantFailsIn is a regional Lambda client whose AddPermission refuses to make skillID live in one region, so activate can fail half-way through the regions.
type liveGrantFailsIn struct {
	*mock.LambdaMock
	region, failRegion, skillID string
}

func (c liveGrantFailsIn) AddPermission(ctx context.Context, params *lambda.AddPermissionInput, optFns ...func(*lambda.Options)) (*lambda.AddPermissionOutput, error) {
	if c.region == c.failRegion && aws.ToString(params.StatementId) == alexa_skill.AlexaInvokeStatementID && aws.ToString(params.EventSourceToken) == c.skillID {
		return nil, errors.New("simulated AddPermission failure")
	}
	return c.LambdaMock.AddPermission(ctx, params, optFns...)
}

// seedVAClient adds the espuser-oauth-clients table and seeds Alexa's own client row the
// worker registers Alexa's account-linking redirect URIs on.
func seedVAClient(ctx context.Context) {
	awscommon.GetDynamoDBClient().(*mock.DynamoDBMock).AddTable("espuser-oauth-clients", "client_id", "")
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	_, err := svc.Create(clients.CreateInput{
		ClientID:   vaClientID,
		ClientName: "Voice Assistant",
		ClientType: "confidential",
		GrantTypes: []string{"authorization_code", "refresh_token"},
		Scopes:     []string{"openid", "email", "phone", "profile", "offline_access"},
	})
	Expect(err).To(BeNil())
}

// vaClientURIs reads the seeded Alexa client row's currently registered redirect URIs.
func vaClientURIs(ctx context.Context) []string {
	c, err := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil)).Get(vaClientID)
	Expect(err).To(BeNil())
	return c.RedirectURIs
}

var _ = BeforeSuite(func() {
	os.Setenv("RMNG_REGION", testRegion)
	os.Setenv("ESP_USER_API_URL", "https://user.example.com/prod")
	os.Setenv("OIDC_VA_CLIENT_ID", vaClientID)
	os.Setenv("OIDC_VA_CLIENT_SECRET_PARAM", vaSecretParam)
	os.Setenv("AWS_LAMBDA_FUNCTION_NAME", "rmng-alexa-auto-cfg")
	// Shrink build waits so the worker's WaitForBuild returns fast in tests.
	buildTimeout = 50 * time.Millisecond
	buildPollInterval = 5 * time.Millisecond
	// Route regional AddPermission to the global mock (production uses a client per Alexa region).
	alexa_skill.NewRegionalLambdaClient = func(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
		return awscommon.GetLambdaClient(), nil
	}
})

var _ = AfterSuite(func() {
	alexa_skill.NewRegionalLambdaClient = origRegionalLambdaClient
})

var _ = Describe("Alexa Auto Cfg", func() {
	var (
		ctx   context.Context
		httpM *mock.MockHTTPClient
	)

	adminRequest := func(body map[string]interface{}) events.APIGatewayProxyRequest {
		raw, _ := json.Marshal(body)
		return events.APIGatewayProxyRequest{
			HTTPMethod: http.MethodPost,
			Resource:   "/v1/admin/integrations/alexa/auto-configuration",
			Body:       string(raw),
			RequestContext: events.APIGatewayProxyRequestContext{
				Identity: events.APIGatewayRequestIdentity{
					CognitoIdentityID:             adminUserID,
					CognitoAuthenticationProvider: ":CognitoSignIn:" + adminUserID,
				},
			},
		}
	}

	lwaRequest := func(method string, body map[string]interface{}) events.APIGatewayProxyRequest {
		req := adminRequest(body)
		req.HTTPMethod = method
		req.Resource = pathLWAConnection
		return req
	}

	storedConnection := func() *alexa_skill.LWAConnection {
		raw := awscommon.GetSSMClient().(*mock.MockSSM).Parameters[alexa_skill.AlexaSSMLWAConnectionParam]
		if raw == nil {
			return nil
		}
		Expect(raw.Type).To(Equal(ssmtypes.ParameterTypeSecureString))
		var conn alexa_skill.LWAConnection
		Expect(json.Unmarshal([]byte(*raw.Value), &conn)).To(Succeed())
		return &conn
	}

	validBody := func() map[string]interface{} {
		return map[string]interface{}{"skill_name": "ESP RainMaker"}
	}

	connectAmazonAccount := func() {
		Expect(alexa_skill.StoreLWAConnection(ctx, alexa_skill.LWAConnection{ClientID: "lwa-id", ClientSecret: "lwa-secret", RefreshToken: "lwa-refresh"})).To(Succeed())
	}

	createSkillCalls := func() int {
		n := 0
		for _, r := range httpM.Requests {
			if r.Method == http.MethodPost && r.URL.String() == smapi.BaseURL+"/v1/skills" {
				n++
			}
		}
		return n
	}

	workerEvent := func() configWorkerEvent {
		lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
		Expect(lambdaMock.InvokeCalls).To(HaveLen(1))
		var ev configWorkerEvent
		Expect(json.Unmarshal(lambdaMock.InvokeCalls[0].Payload, &ev)).To(Succeed())
		return ev
	}

	setupStatus := func() alexa_skill.SetupStatus {
		raw := awscommon.GetSSMClient().(*mock.MockSSM).Parameters[alexa_skill.AlexaSSMConfigStatusParam]
		Expect(raw).NotTo(BeNil())
		var status alexa_skill.SetupStatus
		Expect(json.Unmarshal([]byte(*raw.Value), &status)).To(Succeed())
		return status
	}

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		// Real SSM returns ciphertext for an undecrypted SecureString read; the mock does so only when asked.
		awscommon.GetSSMClient().(*mock.MockSSM).StrictDecryptionPrefix = "/rmng/alexa/"
		_, _ = test_utils.SetupTestAdminUser(ctx, adminUserID, "admin-email")
		_, _ = test_utils.SetupTestNonAdminUser(ctx, dummyUserID, "dummy-email")

		// Account linking needs Alexa's client row to register redirect URIs on, and the
		// client secret it authenticates with — both read at runtime, not from the env.
		seedVAClient(ctx)
		_, err := awscommon.GetSSMClient().PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(vaSecretParam),
			Value: aws.String("alexa-client-secret"),
			Type:  ssmtypes.ParameterTypeSecureString,
		})
		Expect(err).To(BeNil())

		// The API path async-invokes this same Lambda; the mock rejects an unknown
		// function name, so register a no-op standing in for that second invocation.
		// The payload it was called with is what the specs assert on.
		mock.LambdaHandlerMap[os.Getenv("AWS_LAMBDA_FUNCTION_NAME")] = func(ctx context.Context, payload []byte) ([]byte, error) {
			return []byte("{}"), nil
		}

		httpM = httpclient.Get().(*mock.MockHTTPClient)
		_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 200, `{"access_token":"Atza|tok"}`)
	})

	Describe("API path (create + hand off)", func() {
		BeforeEach(func() {
			connectAmazonAccount()
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[{"id":"M1ABC"}]}`)
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills", "POST", 200, `{"skillId":"`+testSkillID+`"}`)
		})

		It("creates the skill and fires the background worker", func() {
			resp, err := handleRequest(ctx, adminRequest(validBody()))
			Expect(err).To(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var out autoCfgResponse
			Expect(json.Unmarshal([]byte(resp.Body), &out)).To(Succeed())
			Expect(out.SkillID).To(Equal(testSkillID))
			Expect(out.Status).To(Equal("IN_PROGRESS"))

			// One async self-invoke carrying the worker payload.
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			Expect(lambdaMock.InvokeCalls).To(HaveLen(1))
			var ev configWorkerEvent
			Expect(json.Unmarshal(lambdaMock.InvokeCalls[0].Payload, &ev)).To(Succeed())
			Expect(ev.Worker).To(BeTrue())
			Expect(ev.SkillID).To(Equal(testSkillID))
			Expect(ev.RedirectURIs).To(ContainElement("https://pitangui.amazon.com/api/skill/link/M1ABC"))

			// The slow config has NOT run on the API path.
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(ssmMock.Parameters["/rmng/alexa/skill_id"]).To(BeNil())
		})

		// Status is the only reliable signal: the credentials keep the previous skill's values until the run's last step.
		It("publishes IN_PROGRESS with the skill id before handing off", func() {
			resp, _ := handleRequest(ctx, adminRequest(validBody()))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			raw := ssmMock.Parameters[alexa_skill.AlexaSSMConfigStatusParam]
			Expect(raw).NotTo(BeNil())

			// It holds no secret, so it is a plain String rather than a SecureString.
			Expect(raw.Type).To(Equal(ssmtypes.ParameterTypeString))

			var status alexa_skill.SetupStatus
			Expect(json.Unmarshal([]byte(*raw.Value), &status)).To(Succeed())
			Expect(status.Status).To(Equal(alexa_skill.SetupStatusInProgress))
			Expect(status.RefID).To(Equal(testSkillID))
			Expect(status.UpdatedAt).NotTo(BeEmpty())
		})

		Describe("with a live skill already configured", func() {
			const live = "amzn1.ask.skill.0a1b-2c3d-live"
			liveStatusURL := smapi.BaseURL + "/v1/skills/" + live + "/status"

			BeforeEach(func() {
				Expect(alexa_skill.StoreAlexaClientDetails(ctx, "live-client", "live-secret", live)).To(Succeed())
			})

			It("reconfigures it instead of creating another skill", func() {
				_ = httpM.RegisterResponse(liveStatusURL, "GET", 200, `{"manifest":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}`)
				before := createSkillCalls()
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(createSkillCalls()).To(Equal(before))

				ev := workerEvent()
				Expect(ev.SkillID).To(Equal(live))
				Expect(ev.Existing).To(BeTrue())
				Expect(setupStatus().Stage).To(Equal("updating"))
			})

			// Its redirect URIs may be registered only on a client that linked it before, so every run registers them on the linking client.
			It("registers the redirect URIs of the vendor that owns it", func() {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[{"id":"M1ABC"},{"id":"M2DEF"}]}`)
				for vendorID, skills := range map[string]string{"M1ABC": `{"skills":[]}`, "M2DEF": `{"skills":[{"skillId":"` + live + `"}]}`} {
					_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills?"+url.Values{"vendorId": {vendorID}, "skillId": {live}}.Encode(), "GET", 200, skills)
				}
				_ = httpM.RegisterResponse(liveStatusURL, "GET", 200, `{"manifest":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}`)
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(workerEvent().RedirectURIs).To(Equal(smapi.AccountLinkingRedirectURLs("M2DEF")))
			})

			It("refuses, starting no run, when no vendor of the connected account owns it", func() {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/vendors", "GET", 200, `{"vendors":[{"id":"M1ABC"},{"id":"M2DEF"}]}`)
				for _, vendorID := range []string{"M1ABC", "M2DEF"} {
					_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills?"+url.Values{"vendorId": {vendorID}, "skillId": {live}}.Encode(), "GET", 200, `{"skills":[]}`)
				}
				_ = httpM.RegisterResponse(liveStatusURL, "GET", 200, `{"manifest":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}`)
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(awscommon.GetLambdaClient().(*mock.LambdaMock).InvokeCalls).To(BeEmpty())
			})

			It("creates a new skill when these LWA credentials cannot reach the live one", func() {
				_ = httpM.RegisterResponse(liveStatusURL, "GET", 404, `{"message":"not found"}`)
				before := createSkillCalls()
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(createSkillCalls()).To(Equal(before + 1))
				Expect(workerEvent().Existing).To(BeFalse())
				Expect(setupStatus().Stage).To(ContainSubstring("previous skill is not reachable"))
			})

			// Reading "no live skill" on a transient error would create a duplicate.
			It("refuses, creating nothing, when the live skill id cannot be read", func() {
				awscommon.GetSSMClient().(*mock.MockSSM).GetParameterErrors[alexa_skill.AlexaSSMSkillIDParam] = errors.New("simulated SSM outage")
				before := createSkillCalls()
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
				Expect(createSkillCalls()).To(Equal(before))
			})
		})

		// The connected account's rejection is the admin's to fix by reconnecting; only an upstream fault may page as a 500.
		DescribeTable("maps an LWA or vendor failure to the party at fault",
			func(lwaStatus int, vendors string, want int) {
				_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", lwaStatus, `{"access_token":"Atza|tok","error":"invalid_grant"}`)
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/vendors", "GET", 200, vendors)
				before := createSkillCalls()
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(want))
				Expect(createSkillCalls()).To(Equal(before))
			},
			Entry("refresh token revoked (400 invalid_grant)", 400, `{"vendors":[{"id":"M1ABC"}]}`, http.StatusBadRequest),
			Entry("client rejected (401 invalid_client)", 401, `{"vendors":[{"id":"M1ABC"}]}`, http.StatusBadRequest),
			Entry("LWA down (503)", 503, `{"vendors":[{"id":"M1ABC"}]}`, http.StatusInternalServerError),
			Entry("profile has no vendor", 200, `{"vendors":[]}`, http.StatusBadRequest),
		)

		It("refuses before creating a skill when the linking client's secret parameter is not configured", func() {
			os.Unsetenv("OIDC_VA_CLIENT_SECRET_PARAM")
			DeferCleanup(os.Setenv, "OIDC_VA_CLIENT_SECRET_PARAM", vaSecretParam)
			before := createSkillCalls()
			resp, _ := handleRequest(ctx, adminRequest(validBody()))
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			Expect(resp.Body).To(ContainSubstring("Account-linking config not set"))
			Expect(createSkillCalls()).To(Equal(before))
		})

		DescribeTable("maps a CreateSkill failure to the party at fault, starting no run",
			func(smapiStatus, want int) {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills", "POST", smapiStatus, `{"message":"nope"}`)
				resp, _ := handleRequest(ctx, adminRequest(validBody()))
				Expect(resp.StatusCode).To(Equal(want))
				Expect(awscommon.GetLambdaClient().(*mock.LambdaMock).InvokeCalls).To(BeEmpty())
				Expect(awscommon.GetSSMClient().(*mock.MockSSM).Parameters).NotTo(HaveKey(alexa_skill.AlexaSSMConfigStatusParam))
			},
			Entry("SMAPI rejects the skill", 400, http.StatusBadRequest),
			Entry("SMAPI is down", 503, http.StatusInternalServerError),
		)

		// The skill exists at Amazon by now, so the status must say so even though the run never started.
		It("reports a failed hand-off to the worker as 500 and a FAILED status", func() {
			awscommon.GetLambdaClient().(*mock.LambdaMock).InvokeError = errors.New("simulated Invoke failure")
			resp, _ := handleRequest(ctx, adminRequest(validBody()))
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))

			status := setupStatus()
			Expect(status.Status).To(Equal(alexa_skill.SetupStatusFailed))
			Expect(status.RefID).To(Equal(testSkillID))
			Expect(status.Error).To(ContainSubstring("could not start configuration"))
		})

		It("routes an API Gateway event through dispatch to the API handler", func() {
			raw, _ := json.Marshal(adminRequest(validBody()))
			out, err := dispatch(ctx, raw)
			Expect(err).To(BeNil())
			resp, ok := out.(events.APIGatewayProxyResponse)
			Expect(ok).To(BeTrue())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("drops a malformed worker event without running or publishing anything", func() {
			out, err := dispatch(ctx, json.RawMessage(`{"__worker":true,"skill_id":5}`))
			Expect(err).To(BeNil())
			Expect(out).To(BeNil())
			Expect(awscommon.GetSSMClient().(*mock.MockSSM).Parameters).NotTo(HaveKey(alexa_skill.AlexaSSMConfigStatusParam))
			Expect(httpM.Requests).To(BeEmpty())
		})

		It("rejects a missing skill_name", func() {
			b := validBody()
			delete(b, "skill_name")
			resp, _ := handleRequest(ctx, adminRequest(b))
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(ContainSubstring("Missing skill_name"))
		})

		It("refuses when no Amazon account is connected", func() {
			Expect(alexa_skill.DeleteLWAConnection(ctx)).To(Succeed())
			resp, _ := handleRequest(ctx, adminRequest(validBody()))
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(resp.Body).To(ContainSubstring("No Amazon account connected"))
			Expect(httpM.Requests).To(BeEmpty())
		})

		// Lambda queues an async event for up to 6 h, so the account-wide refresh token must not travel in it.
		It("keeps the LWA credentials out of the worker event", func() {
			resp, _ := handleRequest(ctx, adminRequest(validBody()))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			payload := string(awscommon.GetLambdaClient().(*mock.LambdaMock).InvokeCalls[0].Payload)
			Expect(payload).NotTo(Or(ContainSubstring("lwa-secret"), ContainSubstring("lwa-refresh")))
		})

		Describe("with an Amazon account connected", func() {

			// A revoked token would refuse every later run, so the account must read as disconnected for the dashboard to offer reconnecting.
			It("disconnects the account when LWA refuses its token", func() {
				_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 400, `{"error":"invalid_grant"}`)
				resp, _ := handleRequest(ctx, adminRequest(map[string]interface{}{"skill_name": "ESP RainMaker"}))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(resp.Body).To(ContainSubstring("connect it again"))
				Expect(storedConnection()).To(BeNil())
				Expect(createSkillCalls()).To(BeZero())
			})

			It("keeps the account when LWA itself fails", func() {
				_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 503, `{}`)
				resp, _ := handleRequest(ctx, adminRequest(map[string]interface{}{"skill_name": "ESP RainMaker"}))
				Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
				Expect(storedConnection()).NotTo(BeNil())
			})
		})

		// Admin privilege is admin-pool membership, so the caller this must refuse is one
		// resolved through the OIDC end-user path — hence the federated provider string.
		It("forbids a non-admin user", func() {
			req := adminRequest(validBody())
			req.RequestContext.Identity.CognitoIdentityID = dummyUserID
			req.RequestContext.Identity.CognitoAuthenticationProvider = test_utils.OIDCAuthProvider(dummyUserID)
			resp, _ := handleRequest(ctx, req)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("rejects a non-POST method", func() {
			req := adminRequest(validBody())
			req.HTTPMethod = "GET"
			resp, _ := handleRequest(ctx, req)
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("Amazon account connection", func() {
		const redirectURI = "https://dashboard.example.com/alexa-lwa-callback"

		It("redeems the consent code and stores the account", func() {
			_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 200, `{"access_token":"Atza|tok","refresh_token":"Atzr|minted"}`)
			resp, _ := handleRequest(ctx, lwaRequest(http.MethodPut, map[string]interface{}{
				"lwa_client_id": "lwa-id", "lwa_client_secret": "lwa-secret",
				"lwa_authorization_code": "code", "lwa_redirect_uri": redirectURI,
			}))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Body).To(MatchJSON(`{"message":"Amazon account connected"}`))
			Expect(storedConnection()).To(Equal(&alexa_skill.LWAConnection{ClientID: "lwa-id", ClientSecret: "lwa-secret", RefreshToken: "Atzr|minted"}))
		})

		It("stores a refresh token only after LWA accepts it", func() {
			resp, _ := handleRequest(ctx, lwaRequest(http.MethodPut, map[string]interface{}{
				"lwa_client_id": "lwa-id", "lwa_client_secret": "lwa-secret", "lwa_refresh_token": "Atzr|pasted",
			}))
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(storedConnection().RefreshToken).To(Equal("Atzr|pasted"))
		})

		It("stores nothing when Amazon refuses the code", func() {
			_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 400, `{"error":"invalid_grant"}`)
			resp, _ := handleRequest(ctx, lwaRequest(http.MethodPut, map[string]interface{}{
				"lwa_client_id": "lwa-id", "lwa_client_secret": "lwa-secret",
				"lwa_authorization_code": "expired", "lwa_redirect_uri": redirectURI,
			}))
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(storedConnection()).To(BeNil())
		})

		DescribeTable("rejects an incomplete request without calling LWA",
			func(body map[string]interface{}, msg string) {
				resp, _ := handleRequest(ctx, lwaRequest(http.MethodPut, body))
				Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
				Expect(resp.Body).To(ContainSubstring(msg))
				Expect(httpM.Requests).To(BeEmpty())
			},
			Entry("no client secret", map[string]interface{}{"lwa_client_id": "id", "lwa_refresh_token": "t"}, "Missing lwa_client_id or lwa_client_secret"),
			Entry("a code without its redirect URI", map[string]interface{}{"lwa_client_id": "id", "lwa_client_secret": "s", "lwa_authorization_code": "c"}, "Missing lwa_redirect_uri"),
			Entry("neither a code nor a token", map[string]interface{}{"lwa_client_id": "id", "lwa_client_secret": "s"}, "Send either"),
			Entry("both a code and a token", map[string]interface{}{"lwa_client_id": "id", "lwa_client_secret": "s", "lwa_authorization_code": "c", "lwa_redirect_uri": redirectURI, "lwa_refresh_token": "t"}, "Send either"),
		)

		It("disconnects, and disconnecting again still succeeds", func() {
			Expect(alexa_skill.StoreLWAConnection(ctx, alexa_skill.LWAConnection{ClientID: "id", ClientSecret: "s", RefreshToken: "t"})).To(Succeed())
			for range 2 {
				resp, _ := handleRequest(ctx, lwaRequest(http.MethodDelete, nil))
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(resp.Body).To(MatchJSON(`{"message":"Amazon account disconnected"}`))
			}
			Expect(storedConnection()).To(BeNil())
		})

		It("rejects other methods", func() {
			resp, _ := handleRequest(ctx, lwaRequest(http.MethodPost, nil))
			Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
		})

		It("forbids a non-admin user", func() {
			req := lwaRequest(http.MethodDelete, nil)
			req.RequestContext.Identity.CognitoIdentityID = dummyUserID
			req.RequestContext.Identity.CognitoAuthenticationProvider = test_utils.OIDCAuthProvider(dummyUserID)
			resp, _ := handleRequest(ctx, req)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Describe("background worker (via dispatch)", func() {
		BeforeEach(func() {
			connectAmazonAccount()
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/status", "GET", 200,
				`{"manifest":{"lastUpdateRequest":{"status":"SUCCEEDED"}}}`)
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/stages/"+smapi.Stage+"/manifest", "PUT", 202, "")
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/stages/"+smapi.Stage+"/accountLinkingClient", "PUT", 204, "")
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/credentials", "GET", 200,
				`{"skillMessagingCredentials":{"clientId":"amzn1.app.acc","clientSecret":"shh"}}`)
		})

		It("configures the skill end to end", func() {
			ev := configWorkerEvent{
				Worker:       true,
				SkillID:      testSkillID,
				SkillName:    "ESP RainMaker",
				RedirectURIs: []string{"https://pitangui.amazon.com/api/skill/link/M1ABC"},
			}
			raw, _ := json.Marshal(ev)
			_, err := dispatch(ctx, raw)
			Expect(err).To(BeNil())

			// AcceptGrant creds persisted to SSM.
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(*ssmMock.Parameters["/rmng/alexa/client_id"].Value).To(Equal("amzn1.app.acc"))
			Expect(*ssmMock.Parameters["/rmng/alexa/skill_id"].Value).To(Equal(testSkillID))

			// The live invoke grant now names the skill, and the setup grant is gone.
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			perm := lambdaMock.Permissions[mock.PermissionKey(skillFn, alexa_skill.AlexaInvokeStatementID)]
			Expect(*perm.EventSourceToken).To(Equal(testSkillID))
			Expect(*perm.Principal).To(Equal("alexa-connectedhome.amazon.com"))
			Expect(lambdaMock.Permissions).NotTo(HaveKey(mock.PermissionKey(skillFn, alexa_skill.AlexaSetupStatementID(testSkillID))))

			// Redirect URLs reached Alexa's own OIDC client row.
			Expect(vaClientURIs(ctx)).To(
				ContainElement("https://pitangui.amazon.com/api/skill/link/M1ABC"))

			// DONE is what both pollers stop on.
			status := setupStatus()
			Expect(status.Status).To(Equal(alexa_skill.SetupStatusDone))
			Expect(status.RefID).To(Equal(testSkillID))
		})

		// Telling an operator to delete the skill they asked to repair would take down their live skill.
		DescribeTable("names the orphan only for a skill this run created",
			func(existing bool, wantHint bool) {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/credentials", "GET", 500, `{}`)
				raw, _ := json.Marshal(configWorkerEvent{
					Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker", Existing: existing,
				})
				_, err := dispatch(ctx, raw)
				Expect(err).To(BeNil())

				status := setupStatus()
				Expect(status.Status).To(Equal(alexa_skill.SetupStatusFailed))
				Expect(status.Stage).To(Equal("read credentials"))
				Expect(status.RefID).To(Equal(testSkillID))
				if wantHint {
					Expect(status.Error).To(ContainSubstring(testSkillID))
					Expect(status.Error).To(ContainSubstring("delete it at Amazon"))
				} else {
					Expect(status.Error).NotTo(ContainSubstring("delete it at Amazon"))
				}
			},
			Entry("new skill", false, true),
			Entry("existing skill", true, false),
		)

		DescribeTable("sets the manufacturer name only when the request carried one",
			func(name *string, want string) {
				Expect(alexa_skill.StoreAlexaManufacturerName(ctx, "Kept Brand")).To(Succeed())
				raw, _ := json.Marshal(configWorkerEvent{
					Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker", ManufacturerName: name,
				})
				_, _ = dispatch(ctx, raw)

				Expect(setupStatus().Status).To(Equal(alexa_skill.SetupStatusDone))
				Expect(alexa_skill.GetAlexaManufacturerName(ctx)).To(Equal(want))
			},
			Entry("absent keeps the stored brand", nil, "Kept Brand"),
			Entry("a value replaces it", aws.String(" Acme "), "Acme"),
			Entry("empty restores the default", aws.String(""), alexa_skill.DefaultManufacturerName),
		)

		It("fails the run when the Amazon account was disconnected after the hand-off", func() {
			Expect(alexa_skill.DeleteLWAConnection(ctx)).To(Succeed())
			raw, _ := json.Marshal(configWorkerEvent{Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker"})
			_, _ = dispatch(ctx, raw)

			status := setupStatus()
			Expect([]string{status.Status, status.Stage}).To(Equal([]string{alexa_skill.SetupStatusFailed, "authenticate"}))
			Expect(status.Error).To(ContainSubstring("No Amazon account connected"))
		})

		It("skips redirect-URI registration when the request carried none", func() {
			before := vaClientURIs(ctx)
			raw, _ := json.Marshal(configWorkerEvent{
				Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker", Existing: true,
			})
			_, _ = dispatch(ctx, raw)
			Expect(setupStatus().Status).To(Equal(alexa_skill.SetupStatusDone))
			Expect(vaClientURIs(ctx)).To(Equal(before))
		})

		It("leaves a working skill's grant and credentials alone when a new skill fails", func() {
			const liveSkill = "amzn1.ask.skill.live"
			Expect(alexa_skill.AddAlexaTriggerAllRegions(ctx, testRegion, liveSkill)).To(Succeed())
			Expect(alexa_skill.StoreAlexaClientDetails(ctx, "live-client", "live-secret", liveSkill)).To(Succeed())
			_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/stages/"+smapi.Stage+"/accountLinkingClient", "PUT", 500, "")

			raw, _ := json.Marshal(configWorkerEvent{
				Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker",
			})
			_, _ = dispatch(ctx, raw)

			Expect(setupStatus().Status).To(Equal(alexa_skill.SetupStatusFailed))
			lambdaMock := awscommon.GetLambdaClient().(*mock.LambdaMock)
			Expect(*lambdaMock.Permissions[mock.PermissionKey(skillFn, alexa_skill.AlexaInvokeStatementID)].EventSourceToken).To(Equal(liveSkill))
			Expect(lambdaMock.Permissions).NotTo(HaveKey(mock.PermissionKey(skillFn, alexa_skill.AlexaSetupStatementID(testSkillID))))
			ssmMock := awscommon.GetSSMClient().(*mock.MockSSM)
			Expect(*ssmMock.Parameters["/rmng/alexa/skill_id"].Value).To(Equal(liveSkill))
			Expect(*ssmMock.Parameters["/rmng/alexa/client_id"].Value).To(Equal("live-client"))
		})

		Describe("activate", func() {
			const liveSkill = "amzn1.ask.skill.live"
			var lambdaMock *mock.LambdaMock
			var ssmMock *mock.MockSSM

			liveGrant := func() string {
				return aws.ToString(lambdaMock.Permissions[mock.PermissionKey(skillFn, alexa_skill.AlexaInvokeStatementID)].EventSourceToken)
			}
			run := func() {
				raw, _ := json.Marshal(configWorkerEvent{
					Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker",
				})
				_, _ = dispatch(ctx, raw)
			}

			BeforeEach(func() {
				lambdaMock = awscommon.GetLambdaClient().(*mock.LambdaMock)
				ssmMock = awscommon.GetSSMClient().(*mock.MockSSM)
				Expect(alexa_skill.AddAlexaTriggerAllRegions(ctx, testRegion, liveSkill)).To(Succeed())
				Expect(alexa_skill.StoreAlexaClientDetails(ctx, "live-client", "live-secret", liveSkill)).To(Succeed())
			})

			It("puts the previous skill back when the swap fails in a later region", func() {
				stub := alexa_skill.NewRegionalLambdaClient
				DeferCleanup(func() { alexa_skill.NewRegionalLambdaClient = stub })
				alexa_skill.NewRegionalLambdaClient = func(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
					return liveGrantFailsIn{LambdaMock: lambdaMock, region: region, failRegion: alexa_skill.AlexaSkillRegions[1], skillID: testSkillID}, nil
				}

				run()

				status := setupStatus()
				Expect(status.Status).To(Equal(alexa_skill.SetupStatusFailed))
				Expect(status.Stage).To(Equal("activate"))
				Expect(liveGrant()).To(Equal(liveSkill))
				Expect(*ssmMock.Parameters["/rmng/alexa/skill_id"].Value).To(Equal(liveSkill))
				Expect(*ssmMock.Parameters["/rmng/alexa/client_id"].Value).To(Equal("live-client"))
			})

			// Reading the live skill id as "none" on a transient error would make the rollback delete the live grant.
			It("refuses to switch when it cannot read the live skill", func() {
				ssmMock.GetParameterErrors[alexa_skill.AlexaSSMSkillIDParam] = errors.New("simulated SSM outage")

				run()

				Expect(setupStatus().Stage).To(Equal("activate"))
				Expect(liveGrant()).To(Equal(liveSkill))
			})

			It("clears setup grants a dead run left behind, and nothing else", func() {
				const stale = "amzn1.ask.skill.stale"
				Expect(alexa_skill.AddAlexaSetupTriggerAllRegions(ctx, testRegion, stale)).To(Succeed())

				run()

				Expect(setupStatus().Status).To(Equal(alexa_skill.SetupStatusDone))
				Expect(lambdaMock.Permissions).NotTo(HaveKey(mock.PermissionKey(skillFn, alexa_skill.AlexaSetupStatementID(stale))))
				Expect(liveGrant()).To(Equal(testSkillID))
			})
		})

		// A repair targets a skill whose last build may have FAILED; waiting on that build first would end the run before the fix is pushed.
		It("pushes the manifest for an existing skill without waiting on its previous build", func() {
			raw, _ := json.Marshal(configWorkerEvent{
				Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker", Existing: true,
			})
			_, _ = dispatch(ctx, raw)

			first := func(method, url string) int {
				for i, r := range httpM.Requests {
					if r.Method == method && r.URL.String() == url {
						return i
					}
				}
				return -1
			}
			manifestPut := first("PUT", smapi.BaseURL+"/v1/skills/"+testSkillID+"/stages/"+smapi.Stage+"/manifest")
			Expect(manifestPut).To(BeNumerically(">=", 0))
			Expect(first("GET", smapi.BaseURL+"/v1/skills/"+testSkillID+"/status")).To(BeNumerically(">", manifestPut))
			Expect(setupStatus().Status).To(Equal(alexa_skill.SetupStatusDone))
		})

		DescribeTable("reports the stage it was in when a step fails",
			func(breakIt func(), wantStage, wantReason string) {
				breakIt()
				raw, _ := json.Marshal(configWorkerEvent{
					Worker: true, SkillID: testSkillID, SkillName: "ESP RainMaker",
				})
				_, _ = dispatch(ctx, raw)

				status := setupStatus()
				Expect(status.Status).To(Equal(alexa_skill.SetupStatusFailed))
				Expect(status.Stage).To(Equal(wantStage))
				Expect(status.Error).To(ContainSubstring(wantReason))
				Expect(status.Error).To(ContainSubstring("delete it at Amazon"))
			},
			Entry("LWA refuses the token", func() {
				_ = httpM.RegisterResponse(smapi.LWATokenURL, "POST", 400, `{"error":"invalid_grant"}`)
			}, "authenticate", "connect it again"),
			Entry("account-linking config missing", func() {
				os.Unsetenv("OIDC_VA_CLIENT_SECRET_PARAM")
				DeferCleanup(os.Setenv, "OIDC_VA_CLIENT_SECRET_PARAM", vaSecretParam)
			}, "set account linking", "account-linking config not set"),
			Entry("Alexa has minted no credentials", func() {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/credentials", "GET", 200, `{}`)
			}, "read credentials", "no messaging credentials"),
			Entry("the build fails", func() {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/status", "GET", 200,
					`{"manifest":{"lastUpdateRequest":{"status":"FAILED","errors":[{"message":"Endpoint is not invokable"}]}}}`)
			}, "initial build", "Endpoint is not invokable"),
			Entry("SMAPI rejects account linking", func() {
				_ = httpM.RegisterResponse(smapi.BaseURL+"/v1/skills/"+testSkillID+"/stages/"+smapi.Stage+"/accountLinkingClient", "PUT", 500, "")
			}, "set account linking", "returned 500"),
		)
	})
})
