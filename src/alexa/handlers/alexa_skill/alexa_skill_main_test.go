// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"net/http"
	"os"
	"sort"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	profile      *mock.Profile
	timingFile   *os.File
	tokenHarness *test_utils.ESPUserTokenHarness
)

var _ = BeforeSuite(func() {
	var err error
	timingFile, err = test_utils.CreateCommonSummaryFile("alexa_skill.txt")
	Expect(err).NotTo(HaveOccurred(), "Failed to create timing summary file")
})

func TestAlexaSkill(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Alexa Skill Suite")
}

type DiscoveryRequestPayload struct {
	Scope alexa_skill.Scope `json:"scope"`
}

type reportStateTestCase struct {
	deviceName      string
	cookie          map[string]interface{}
	initialState    node.IoTNodeShadow
	expectedError   string
	expectedProps   []alexa_skill.ContextProperty
	tokenSub        string
	tokenIdentityID string
}

var _ = Describe("Alexa Skill", func() {
	var (
		ctx                     context.Context
		request                 alexa_skill.AlexaRequest
		discoveryRequestPayload DiscoveryRequestPayload
		testUser                *user.User
		rmngUserContext         *rmngctx.RmngContext
		testNode1               *node.Node
		testGroup               *group.Group
		testNodeID1             string
		testNodeID2             string
		mockHTTPClient          *mock.MockHTTPClient
		testAuthCode            string
		testGrantToken          string
	)
	userID := "26fd9a10-ca12-402f-97dd-0e6913cc2dba"

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		tokenHarness = test_utils.SetupESPUserTokenHarness(ctx)

		testToken := createTestToken(userID, "test-user@example.com")
		discoveryRequestPayload = DiscoveryRequestPayload{
			Scope: alexa_skill.Scope{
				Type:  "BearerToken",
				Token: testToken,
			},
		}

		ssmMock := awscommon.GetSSMClient()
		ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(alexa_skill.AlexaSSMClientIDParam),
			Value: aws.String("test-client-id"),
		})
		ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(alexa_skill.AlexaSSMClientSecretParam),
			Value: aws.String("test-client-secret"),
		})

		// Create test user
		testUser, rmngUserContext = test_utils.SetupTestUser(ctx, userID, "test-user@example.com")

		// Create a testGroup
		var err error
		testGroup, err = group.CreateGroupForUser(rmngUserContext, "Living Room")
		Expect(err).To(BeNil())

		// Create two nodes with different configurations
		testNodeID1 = "test-node1"
		testNode1 = node.NewNode(testNodeID1)
		rmngUserContext.SetAllow(utils.NodeAll, testNodeID1)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, testNodeID1)

		testNodeID2 = "test-node2"
		testNode2 := node.NewNode(testNodeID2)
		rmngUserContext.SetAllow(utils.NodeAll, testNodeID2)

		nodeConfigSwitch := node_cfg_simple_switch_test_data
		nodeConfigLight := node_cfg_simple_light_test_data

		// Store node configuration
		rmngNodeContext := rmngctx.NewRmngContext(testNode1)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
		err = nodeDetailsDB.UpdateServiceData("config", nodeConfigSwitch)
		Expect(err).To(BeNil())

		rmngNodeContext = rmngctx.NewRmngContext(testNode2)
		nodeDetailsDB = node_details_db.NewNodeDetailsDB(rmngNodeContext)
		err = nodeDetailsDB.UpdateServiceData("config", nodeConfigLight)
		Expect(err).To(BeNil())

		mockHTTPClient = mock.NewMockHTTPClient()
		httpclient.Set(mockHTTPClient)
		testAuthCode = "test-auth-code-123"
		testGrantToken = testToken
	})

	AfterEach(func() {
		tokenHarness.Close()
	})

	Describe("Authorization", func() {
		It("should handle AcceptGrant directive successfully", func() {
			// AcceptGrant runs in the regional Smart Home Lambda; its AWS region is
			// captured and stored to pick the right event gateway for ChangeReports.
			os.Setenv("AWS_REGION", "eu-west-1")
			DeferCleanup(func() { os.Unsetenv("AWS_REGION") })

			// Create a proper LWA token format (not a Cognito JWT) - LWA tokens typically start with "Atza|"
			// This format would fail Cognito validation, ensuring we use the grantee token instead
			lwaAccessToken := "Atza|test-lwa-access-token-12345"
			lwaRefreshToken := "Atzr|test-lwa-refresh-token-12345"

			// Setup mock HTTP response for token exchange - return LWA token format
			tokenResponse := fmt.Sprintf(`{"access_token": "%s", "refresh_token": "%s", "token_type": "bearer", "expires_in": 3600}`, lwaAccessToken, lwaRefreshToken)
			err := mockHTTPClient.RegisterResponse(
				alexa_skill.AwsTokenUrl,
				http.MethodPost,
				200,
				tokenResponse,
			)
			Expect(err).To(BeNil())

			request = createTestRequest("Alexa.Authorization", "AcceptGrant", alexa_skill.AcceptGrantPayload{
				Grant: alexa_skill.GrantPayloadGrant{
					Type: "OAuth2.AuthorizationCode",
					Code: testAuthCode,
				},
				Grantee: alexa_skill.GranteePayloadGrantee{
					Type:  "BearerToken",
					Token: testGrantToken,
				},
			})

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.Event.Header.Namespace).To(Equal("Alexa.Authorization"))
			Expect(response.Event.Header.Name).To(Equal("AcceptGrant.Response"))

			// Verify HTTP request was made correctly
			Expect(mockHTTPClient.Requests[0].Method).To(Equal(http.MethodPost))
			Expect(mockHTTPClient.Requests[0].URL.String()).To(Equal(alexa_skill.AwsTokenUrl))
			Expect(mockHTTPClient.Requests[0].Header.Get("Content-Type")).To(Equal("application/x-www-form-urlencoded"))

			// Read and verify request body
			body, err := io.ReadAll(mockHTTPClient.Requests[0].Body)
			Expect(err).To(BeNil())
			Expect(string(body)).To(ContainSubstring("grant_type=authorization_code"))
			Expect(string(body)).To(ContainSubstring("code=" + testAuthCode))

			// Verify LWA token was stored on the single per-user Alexa row (endpoint_id is the constant integration name).
			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
			storedEntry, err := user_integration_db.NewUserDB(rmngCtx).GetUserEntryByEndpoint(alexa_skill.AlexaPlatform, user_integration_db.EncodeEndpointID(alexa_skill.AlexaPlatform))
			Expect(err).To(BeNil())
			Expect(storedEntry.IntegrationToken.AccessToken).To(Equal(lwaAccessToken))
			Expect(storedEntry.IntegrationToken.RefreshToken).To(Equal(lwaRefreshToken))
			Expect(storedEntry.EndpointID).To(Equal(user_integration_db.EncodeEndpointID(alexa_skill.AlexaPlatform)))
			Expect(storedEntry.IntegrationToken.Region).To(Equal("eu-west-1"))
		})

		It("should handle invalid auth code", func() {
			err := mockHTTPClient.RegisterResponse(
				alexa_skill.AwsTokenUrl,
				http.MethodPost,
				400,
				`{"error": "invalid_grant"}`,
			)
			Expect(err).To(BeNil())

			request = createTestRequest("Alexa.Authorization", "AcceptGrant", alexa_skill.AcceptGrantPayload{
				Grant: alexa_skill.GrantPayloadGrant{
					Type: "OAuth2.AuthorizationCode",
					Code: "invalid-code",
				},
				Grantee: alexa_skill.GranteePayloadGrantee{
					Type:  "BearerToken",
					Token: testGrantToken,
				},
			})

			_, err = handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get access token"))
		})

		It("should handle invalid grantee token", func() {
			// Mock successful LWA token exchange
			lwaAccessToken := "Atza|test-lwa-access-token-12345"
			tokenResponse := fmt.Sprintf(`{"access_token": "%s", "refresh_token": "test-refresh-token", "token_type": "bearer", "expires_in": 3600}`, lwaAccessToken)
			err := mockHTTPClient.RegisterResponse(
				alexa_skill.AwsTokenUrl,
				http.MethodPost,
				200,
				tokenResponse,
			)
			Expect(err).To(BeNil())

			request = createTestRequest("Alexa.Authorization", "AcceptGrant", alexa_skill.AcceptGrantPayload{
				Grant: alexa_skill.GrantPayloadGrant{
					Type: "OAuth2.AuthorizationCode",
					Code: testAuthCode,
				},
				Grantee: alexa_skill.GranteePayloadGrantee{
					Type:  "BearerToken",
					Token: "invalid-token",
				},
			})

			_, err = handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get user id"))
		})

		It("should use grantee token (Cognito token) to identify user, not LWA token", func() {
			// This test ensures we use payload.Grantee.Token (Cognito token) to identify the user, not tokenResp.AccessToken (LWA token).
			// LWA tokens have format like "Atza|..." which would fail Cognito validation if used incorrectly.
			lwaAccessToken := "Atza|test-lwa-token-that-would-fail-cognito-validation"
			tokenResponse := fmt.Sprintf(`{"access_token": "%s", "refresh_token": "test-refresh-token", "token_type": "bearer", "expires_in": 3600}`, lwaAccessToken)
			err := mockHTTPClient.RegisterResponse(
				alexa_skill.AwsTokenUrl,
				http.MethodPost,
				200,
				tokenResponse,
			)
			Expect(err).To(BeNil())

			request = createTestRequest("Alexa.Authorization", "AcceptGrant", alexa_skill.AcceptGrantPayload{
				Grant: alexa_skill.GrantPayloadGrant{
					Type: "OAuth2.AuthorizationCode",
					Code: testAuthCode,
				},
				Grantee: alexa_skill.GranteePayloadGrantee{
					Type:  "BearerToken",
					Token: testGrantToken,
				},
			})

			// Should succeed because we use grantee token (Cognito token), not LWA token
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.Event.Header.Namespace).To(Equal("Alexa.Authorization"))
			Expect(response.Event.Header.Name).To(Equal("AcceptGrant.Response"))

			// Verify LWA token was stored on the single per-user Alexa row (endpoint_id is the constant integration name).
			testUser := user.NewUser(userID)
			rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, testUser)
			storedEntry, err := user_integration_db.NewUserDB(rmngCtx).GetUserEntryByEndpoint(alexa_skill.AlexaPlatform, user_integration_db.EncodeEndpointID(alexa_skill.AlexaPlatform))
			Expect(err).To(BeNil())
			Expect(storedEntry.IntegrationToken.AccessToken).To(Equal(lwaAccessToken))
		})

		It("should handle missing auth code", func() {
			request = createTestRequest("Alexa.Authorization", "AcceptGrant", alexa_skill.AcceptGrantPayload{
				Grant: alexa_skill.GrantPayloadGrant{
					Type: "OAuth2.AuthorizationCode",
					// Code is missing
				},
				Grantee: alexa_skill.GranteePayloadGrantee{
					Type:  "BearerToken",
					Token: testGrantToken,
				},
			})

			_, err := handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("authorization code absent"))
		})
	})

	Describe("Discovery", func() {
		It("should set Alexa enabled status and notify device during discovery", func() {
			// Store a basic node config
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
			err := nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_switch_test_data)
			Expect(err).To(BeNil())

			// Get IoT mock to verify messages
			iotMock := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			iotMock.PublishCalls = nil

			// Make discovery request
			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			_, err = handler(ctx, request)
			Expect(err).To(BeNil())

			// Verify the device received the getAlexaEn event
			Expect(iotMock.PublishCalls).To(HaveLen(1))
			publishInput := iotMock.PublishCalls[0]
			Expect(*publishInput.Topic).To(Equal(fmt.Sprintf("rainmaker/nodes/%s/from_cloud", testNodeID1)))
			var publishData map[string]interface{}
			err = json.Unmarshal(publishInput.Payload, &publishData)
			Expect(err).To(BeNil())
			Expect(publishData["event"]).To(ContainElement("getAlexaEn"))
			Expect(publishData["getAlexaEn"]).To(HaveKeyWithValue("enabled", true))

			// Verify the database update
			alexaEn, err := testNode1.GetAlexaEnStatus(ctx)
			Expect(err).To(BeNil())
			Expect(alexaEn).To(Not(BeNil()))
			Expect(*alexaEn).To(BeTrue())
		})

		It("should handler Discover responses correctly for various larger node configurations", func() {
			test_inputs := []struct {
				node_cfg_path           string
				discovery_response_path string
			}{
				{
					node_cfg_path:           "test_data/discovery/rainmaker_sample_test_node_cfg.json",
					discovery_response_path: "test_data/discovery/rainmaker_sample_test_discovery_response.json",
				},
			}

			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)

			for _, test_input := range test_inputs {
				fmt.Printf("Testing node config: %s\n", test_input.node_cfg_path)
				// Write this node config
				storeNodeCfgInDb(nodeDetailsDB, test_input.node_cfg_path)
				// Read the discovery response
				discovery_response_map := getDiscoveryResponse(test_input.discovery_response_path)
				// Test discovery
				request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
				response, err := handler(ctx, request)
				Expect(err).To(BeNil())

				payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
				for i := range payload.Endpoints {
					Expect(payload.Endpoints[i].Cookie["groupID"]).To(Equal(testGroup.GroupID))
					// Set to Nil so that the subsequent comparision works. Since the groupID is dynamic it is not part of the expected response in the static files
					delete(payload.Endpoints[i].Cookie, "groupID")
				}

				// Verify response
				response_json, err := json.Marshal(response)
				Expect(err).To(BeNil())
				response_map := make(map[string]interface{})
				err = json.Unmarshal(response_json, &response_map)
				Expect(err).To(BeNil())

				test_utils.AssertNormalizedEqual(discovery_response_map, response_map)
			}
		})

		It("should handle various shorter node configurations", func() {
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)

			for index, test_input := range short_test_data {
				fmt.Printf("Testing node config: %d\n", index)

				nodeDetailsDB.UpdateServiceData("config", test_input.NodeCfgTestData)
				discovery_response_map := test_input.DiscoveryResponse

				request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
				response, err := handler(ctx, request)
				Expect(err).To(BeNil())

				// Verify response
				payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
				for i := range payload.Endpoints {
					Expect(payload.Endpoints[i].Cookie["groupID"]).To(Equal(testGroup.GroupID))
					// Set to Nil so that the subsequent comparision works. Since the groupID is dynamic it is not part of the expected response in the static files
					delete(payload.Endpoints[i].Cookie, "groupID")
				}

				test_utils.AssertNormalizedEqual(discovery_response_map, payload)
			}
		})

		It("should use esp.param.name from shadow as friendly name", func() {
			// Store light config (has esp.param.name param) for testNode1
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
			nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_light_test_data)

			// Mock shadow with custom name value
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
			initialState := node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"Light": map[string]interface{}{
								"name": "Kitchen Light",
							},
						},
					},
				},
			}
			shadowJSON, _ := json.Marshal(initialState)
			iotDataClient.AddDirect(testNodeID1, shadowName, shadowJSON)

			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())

			payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
			// Find the Light endpoint
			var lightEndpoint *alexa_skill.DiscoveryEndpoint
			for i, ep := range payload.Endpoints {
				if ep.EndpointID == alexa_skill.GetEndpointId(testNodeID1, "Light") {
					lightEndpoint = &payload.Endpoints[i]
					break
				}
			}
			Expect(lightEndpoint).ToNot(BeNil(), "Light endpoint not found")
			Expect(lightEndpoint.FriendlyName).To(Equal("Kitchen Light"))
		})

		It("should fallback to device.Name when shadow has no name", func() {
			// Store light config for testNode1 but no shadow data
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
			nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_light_test_data)

			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())

			payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
			var lightEndpoint *alexa_skill.DiscoveryEndpoint
			for i, ep := range payload.Endpoints {
				if ep.EndpointID == alexa_skill.GetEndpointId(testNodeID1, "Light") {
					lightEndpoint = &payload.Endpoints[i]
					break
				}
			}
			Expect(lightEndpoint).ToNot(BeNil(), "Light endpoint not found")
			Expect(lightEndpoint.FriendlyName).To(Equal("Light"))
		})

		// Manufacturer resolution: the node's own report wins, else the deployment's configured
		// brand, else the default. Discovery must never advertise a placeholder — WWA rejects it.
		// TestSetup gives each spec a fresh SSM mock with no cached values, so a brand stored here
		// does not leak into the specs that expect the default.
		Describe("manufacturer name", func() {
			discoverSwitchEndpoint := func(nodeCfg map[string]interface{}) *alexa_skill.DiscoveryEndpoint {
				rmngNodeContext := rmngctx.NewRmngContext(testNode1)
				node_details_db.NewNodeDetailsDB(rmngNodeContext).UpdateServiceData("config", nodeCfg)

				request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
				response, err := handler(ctx, request)
				Expect(err).To(BeNil())

				payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
				for i, ep := range payload.Endpoints {
					if ep.EndpointID == alexa_skill.GetEndpointId(testNodeID1, "Switch") {
						return &payload.Endpoints[i]
					}
				}

				return nil
			}

			It("should advertise the default brand when the deployment has configured none", func() {
				endpoint := discoverSwitchEndpoint(node_cfg_simple_switch_test_data)
				Expect(endpoint).ToNot(BeNil(), "Switch endpoint not found")
				Expect(endpoint.ManufacturerName).To(Equal(alexa_skill.DefaultManufacturerName))
				Expect(endpoint.AdditionalAttributes["manufacturer"]).To(Equal(alexa_skill.DefaultManufacturerName))
			})

			It("should advertise the deployment's configured brand when the node reports none", func() {
				Expect(alexa_skill.StoreAlexaManufacturerName(ctx, "Rebranded Deployment")).To(BeNil())

				endpoint := discoverSwitchEndpoint(node_cfg_simple_switch_test_data)
				Expect(endpoint).ToNot(BeNil(), "Switch endpoint not found")
				Expect(endpoint.ManufacturerName).To(Equal("Rebranded Deployment"))
				Expect(endpoint.AdditionalAttributes["manufacturer"]).To(Equal("Rebranded Deployment"))
				Expect(endpoint.Description).To(Equal("Rebranded Deployment smart home device"))
			})

			It("should let a node's reported manufacturer override the configured brand", func() {
				Expect(alexa_skill.StoreAlexaManufacturerName(ctx, "Rebranded Deployment")).To(BeNil())

				endpoint := discoverSwitchEndpoint(node_cfg_oem_switch_test_data)
				Expect(endpoint).ToNot(BeNil(), "Switch endpoint not found")
				Expect(endpoint.ManufacturerName).To(Equal("Acme Devices"))
				Expect(endpoint.AdditionalAttributes["manufacturer"]).To(Equal("Acme Devices"))
				Expect(endpoint.AdditionalAttributes["model"]).To(Equal("ACME-SW-1"))
				Expect(endpoint.Description).To(Equal("Acme Devices ACME-SW-1"))
			})

			It("should fall back to the default brand when the configured value is empty", func() {
				Expect(alexa_skill.StoreAlexaManufacturerName(ctx, "")).To(BeNil())

				endpoint := discoverSwitchEndpoint(node_cfg_simple_switch_test_data)
				Expect(endpoint).ToNot(BeNil(), "Switch endpoint not found")
				Expect(endpoint.ManufacturerName).To(Equal(alexa_skill.DefaultManufacturerName))
			})
		})

		It("should handle Discover directive for nodes in multiple groups", func() {
			rmngUserContext := rmngctx.NewRmngContext(testUser)
			// Create another testGroup
			testGroup2, err := group.CreateGroupForUser(rmngUserContext, "Dining Room")
			Expect(err).To(BeNil())
			test_utils.ManuallyAddNodeToGroup(ctx, testGroup2.GroupID, testNodeID2)

			// Create two nodes with different configurations
			testNodeID1 = "test-node1"
			testNode1 = node.NewNode(testNodeID1)
			rmngUserContext.SetAllow(utils.NodeAll, testNodeID1)
			test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, testNodeID1)

			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.Event.Header.Namespace).To(Equal("Alexa.Discovery"))
			Expect(response.Event.Header.Name).To(Equal("Discover.Response"))

			// Verify payload contains endpoints
			payload, ok := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
			Expect(ok).To(BeTrue())
			Expect(len(payload.Endpoints)).To(Equal(2))
			expectedPayload := alexa_skill.DiscoveryPayload{
				Endpoints: []alexa_skill.DiscoveryEndpoint{
					node_cfg_simple_switch_discovery_response.Endpoints[0],
					node_cfg_simple_light_discovery_response.Endpoints[0],
				},
			}
			expectedPayload.Endpoints[1].Cookie["groupID"] = testGroup2.GroupID
			expectedPayload.Endpoints[0].Cookie["groupID"] = testGroup.GroupID
			// We apply the test data to test-node2, so the EndpointID should be updated manually to test-node2
			expectedPayload.Endpoints[1].EndpointID = alexa_skill.GetEndpointId(testNodeID2, "Light")
			test_utils.AssertNormalizedEqual(expectedPayload, payload)
		})

		It("should fail if user does not have access to the node", func() {
			testUsername := "invalid-user@example.com"
			invalidUserID := "dummy-sub-entry_invalid_user"
			// Seed the end user so the token resolves; they simply own no nodes.
			test_utils.SetupTestUser(ctx, invalidUserID, testUsername)
			token := createTestToken(invalidUserID, testUsername)

			invalidUserPayload := DiscoveryRequestPayload{
				Scope: alexa_skill.Scope{
					Type:  "BearerToken",
					Token: token,
				},
			}

			request = createTestRequest("Alexa.Discovery", "Discover", invalidUserPayload)
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.Event.Header.Namespace).To(Equal("Alexa.Discovery"))
			Expect(response.Event.Header.Name).To(Equal("Discover.Response"))
			Expect(len((*response.Event.Payload).(alexa_skill.DiscoveryPayload).Endpoints)).To(Equal(0))

			// Now try with a valid user
			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			response, err = handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.Event.Header.Namespace).To(Equal("Alexa.Discovery"))
			Expect(response.Event.Header.Name).To(Equal("Discover.Response"))
			Expect(len((*response.Event.Payload).(alexa_skill.DiscoveryPayload).Endpoints)).To(Equal(1))
		})

		// This test validates the Discovery.Response capability blocks against the Alexa Smart Home spec. The capability blocks are marshalled to JSON and checked as generic maps so missing fields that aren't part of the alexa_skill.Capabilities struct (instance, capabilityResources, configuration) still surface.
		// Spec refs:
		//   ModeController: developer.amazon.com/docs/alexa/device-apis/alexa-modecontroller.html
		//   ColorTemperatureController: developer.amazon.com/docs/alexa/device-apis/alexa-colortemperaturecontroller.html
		//   ColorController: developer.amazon.com/docs/alexa/device-apis/alexa-colorcontroller.html
		//   EndpointHealth: developer.amazon.com/docs/alexa/device-apis/alexa-endpointhealth.html
		It("should emit spec-compliant capability blocks for ColorLight", func() {
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
			nodeDetailsDB.UpdateServiceData("config", node_cfg_color_light_test_data)

			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())

			payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
			var colorLight *alexa_skill.DiscoveryEndpoint
			for i, ep := range payload.Endpoints {
				if ep.EndpointID == alexa_skill.GetEndpointId(testNodeID1, "ColorLight") {
					colorLight = &payload.Endpoints[i]
					break
				}
			}
			Expect(colorLight).ToNot(BeNil(), "ColorLight endpoint not in Discover.Response")

			// Marshal each capability to a generic map so we can inspect fields (instance, capabilityResources, configuration) that the typed Capabilities struct doesn't expose.
			capsByInterface := map[string]map[string]interface{}{}
			for _, cap := range colorLight.Capabilities {
				b, err := json.Marshal(cap)
				Expect(err).To(BeNil())
				var m map[string]interface{}
				Expect(json.Unmarshal(b, &m)).To(Succeed())
				capsByInterface[cap.Interface] = m
			}

			// Helper: every capability must have these top-level fields per spec.
			assertBaseFields := func(iface string, expectedSupported string) {
				cap := capsByInterface[iface]
				Expect(cap).ToNot(BeNil(), "missing capability %s", iface)
				Expect(cap["type"]).To(Equal("AlexaInterface"), "%s: type", iface)
				Expect(cap["interface"]).To(Equal(iface), "%s: interface", iface)
				Expect(cap["version"]).ToNot(BeEmpty(), "%s: version", iface)
				props, ok := cap["properties"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "%s: properties must be present", iface)
				supported, _ := props["supported"].([]interface{})
				Expect(supported).To(HaveLen(1), "%s: properties.supported", iface)
				first, _ := supported[0].(map[string]interface{})
				Expect(first["name"]).To(Equal(expectedSupported), "%s: properties.supported[0].name", iface)
			}

			// EndpointHealth — required: properties.supported = [{name:"connectivity"}]
			assertBaseFields("Alexa.EndpointHealth", "connectivity")

			// PowerController — required: properties.supported = [{name:"powerState"}]
			assertBaseFields("Alexa.PowerController", "powerState")

			// BrightnessController — required: properties.supported = [{name:"brightness"}]
			assertBaseFields("Alexa.BrightnessController", "brightness")

			// ColorController — required: properties.supported = [{name:"color"}]. configuration is NOT required per spec.
			assertBaseFields("Alexa.ColorController", "color")

			// ColorTemperatureController — required: properties.supported = [{name:"colorTemperatureInKelvin"}]. configuration is NOT required per spec (range is an optional enhancement).
			assertBaseFields("Alexa.ColorTemperatureController", "colorTemperatureInKelvin")

			// ModeController is intentionally NOT emitted (see capabilities.go paramToCapability for rationale). A malformed ModeController causes Alexa to drop the entire endpoint, so we omit it until the RM node config exposes enough metadata (supportedModes, friendlyNames per mode) to build a spec-compliant block. This assertion locks that decision in place: regenerating it requires also updating this expectation.
			_, hasMode := capsByInterface["Alexa.ModeController"]
			Expect(hasMode).To(BeFalse(),
				"Alexa.ModeController must NOT appear in Discover.Response "+
					"until a spec-compliant block (instance, capabilityResources, "+
					"configuration.supportedModes >= 2) can be constructed")
		})

		It("should handle missing token", func() {
			discoveryRequestPayload.Scope.Token = ""
			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)

			_, err := handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring("failed to get identity id"))
		})

		It("should handle invalid token", func() {
			discoveryRequestPayload.Scope.Token = "invalid-token"
			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)

			_, err := handler(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get identity id"))
		})

		It("should set Alexa enabled status to true for discovered devices", func() {
			rmngNodeContext := rmngctx.NewRmngContext(testNode1)
			nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)

			// Put a test node config with a device
			nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_switch_test_data)

			// Perform discovery
			request = createTestRequest("Alexa.Discovery", "Discover", discoveryRequestPayload)
			response, err := handler(ctx, request)
			Expect(err).To(BeNil())

			// Verify that discovery found the device
			payload := (*response.Event.Payload).(alexa_skill.DiscoveryPayload)
			Expect(len(payload.Endpoints)).To(Equal(1))

			// Verify that Alexa enabled status was set to true
			alexaEn, err := testNode1.GetAlexaEnStatus(ctx)
			Expect(err).To(BeNil())
			Expect(alexaEn).To(Not(BeNil()))
			Expect(*alexaEn).To(BeTrue())
		})
	})

	Context("Capability Tests", func() {
		var request alexa_skill.AlexaRequest

		BeforeEach(func() {
			// Setup mock IoT client
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)

			// Set initial shadow state
			initialState := node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"Switch": map[string]interface{}{
								"power": false,
							},
							"Light": map[string]interface{}{
								"brightness": 50,
							},
							"ColorLight": map[string]interface{}{
								"hue":        180,
								"saturation": 75,
								"brightness": 80,
								"cct":        4000,
								"power":      true,
								"toggle":     false,
								"mode":       2,
							},
						},
					},
				},
			}
			shadowJSON, _ := json.Marshal(initialState)
			iotDataClient.AddDirect(testNodeID1, shadowName, shadowJSON)
		})

		type testData struct {
			directive           string
			directiveName       string
			payload             interface{}
			deviceName          string
			paramName           string
			extraCookie         map[string]interface{} // additional cookie entries beyond the auto-generated one
			initialReported     map[string]interface{} // when set, re-seeds the device's reported shadow params before the directive
			expectedShadowState map[string]interface{}
			expectedError       string
			respPropertyName    string
			respPropertyValue   interface{}
			respNamespace       string
			respProperties      []alexa_skill.ContextProperty // when set, asserts the full property list instead of the single resp* fields
		}

		// validateControlDirective is a helper function to validate the control directives like PowerController, BrightnessController, etc.
		validateControlDirective := func(td testData) {
			cookie := map[string]interface{}{
				"groupID": testGroup.GroupID,
			}
			if td.paramName != "" {
				cookie[fmt.Sprintf("paramMap_%s", td.directive[6:])] = td.paramName
			}
			for k, v := range td.extraCookie {
				cookie[k] = v
			}

			// Optionally re-seed the device's reported shadow so relative directives
			// (Adjust*, Increase/Decrease*, power-on restore) read a known current state.
			if td.initialReported != nil {
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
				seed := node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{Params: td.initialReported},
					},
				}
				seedJSON, _ := json.Marshal(seed)
				iotDataClient.AddDirect(testNodeID1, shadowName, seedJSON)
			}

			token := createTestToken(userID, "test-user@example.com")

			request = createTestRequest(td.directive, td.directiveName, td.payload)
			request.Directive.Endpoint = &alexa_skill.Endpoint{
				EndpointID: alexa_skill.GetEndpointId(testNodeID1, td.deviceName),
				Cookie:     cookie,
				Scope: &alexa_skill.Scope{
					Type:  "BearerToken",
					Token: token,
				},
			}

			response, err := handler(ctx, request)
			if td.expectedError != "" {
				Expect(err).To(HaveOccurred())
				Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring(td.expectedError))
				return
			}

			Expect(err).To(BeNil())

			// Verify shadow state if expected state is provided
			if td.expectedShadowState != nil {
				dataDesired := test_utils.GetPublishedDataForNodeGroup(testNode1, group_node_db.NodesGroups{
					Group: testGroup.GroupID,
				})
				dataDesiredMap := make(map[string]interface{})
				err = json.Unmarshal(dataDesired, &dataDesiredMap)
				Expect(err).To(BeNil())
				test_utils.AssertNormalizedEqual(test_utils.ConvertAllFloatToInt(dataDesiredMap), test_utils.ConvertAllFloatToInt(td.expectedShadowState))
			}

			// Verify response
			expectedResponse := alexa_skill.CreateResponse("test-message-id", "Alexa", "Response", "", "", request.Directive.Endpoint)
			expectedResponse.Event.Endpoint.Scope.Token = request.Directive.Endpoint.Scope.Token
			expectedResponse.Event.Endpoint.Scope.Type = "BearerToken"
			var emptyPayload interface{} = map[string]interface{}{}
			expectedResponse.Event.Payload = &emptyPayload
			expectedProps := td.respProperties
			if expectedProps == nil {
				expectedProps = []alexa_skill.ContextProperty{
					{
						NameSpace:                 td.respNamespace,
						Name:                      td.respPropertyName,
						Value:                     td.respPropertyValue,
						UncertaintyInMilliseconds: 0,
					},
				}
			}
			expectedResponse.Context = &alexa_skill.Context{
				Properties: expectedProps,
			}
			// Clear timestamps for comparison (avoid wall-clock-tick flakes between production's time.Now() and the test's time.Now()).
			if response.Context != nil {
				for i := range response.Context.Properties {
					response.Context.Properties[i].TimeOfSample = ""
				}
			}
			test_utils.AssertNormalizedEqual(expectedResponse, response)
		}

		DescribeTable("PowerController Tests",
			validateControlDirective,
			Entry("should handle TurnOn directive", testData{
				directive:     "Alexa.PowerController",
				directiveName: "TurnOn",
				payload:       struct{}{},
				deviceName:    "Switch",
				paramName:     "power",
				expectedShadowState: map[string]interface{}{
					"Switch": map[string]interface{}{
						"power": true,
					},
				},
				respNamespace:     "Alexa.PowerController",
				respPropertyName:  "powerState",
				respPropertyValue: "ON",
			}),
			Entry("should handle TurnOff directive", testData{
				directive:     "Alexa.PowerController",
				directiveName: "TurnOff",
				payload:       struct{}{},
				deviceName:    "Switch",
				paramName:     "power",
				expectedShadowState: map[string]interface{}{
					"Switch": map[string]interface{}{
						"power": false,
					},
				},
				respNamespace:     "Alexa.PowerController",
				respPropertyName:  "powerState",
				respPropertyValue: "OFF",
			}),
			Entry("should handle missing PowerController mapping", testData{
				directive:     "Alexa.PowerController",
				directiveName: "TurnOn",
				payload:       struct{}{},
				deviceName:    "Switch",
				paramName:     "",
				expectedError: "missing PowerController mapping in cookie",
			}),
		)

		DescribeTable("BrightnessController Tests",
			validateControlDirective,
			Entry("should handle SetBrightness directive", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "SetBrightness",
				payload: map[string]interface{}{
					"brightness": 74,
				},
				deviceName: "Light",
				paramName:  "brightness",
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{
						"brightness": 74,
					},
				},
				respNamespace:     "Alexa.BrightnessController",
				respPropertyName:  "brightness",
				respPropertyValue: 74,
			}),
			Entry("should handle missing BrightnessController mapping", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "SetBrightness",
				payload: map[string]interface{}{
					"brightness": 75,
				},
				deviceName:    "Light",
				paramName:     "",
				expectedError: "missing BrightnessController mapping in cookie",
			}),
			Entry("should handle invalid brightness payload", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "SetBrightness",
				payload:       "invalid payload",
				deviceName:    "Light",
				paramName:     "brightness",
				expectedError: "failed to unmarshal brightness payload",
			}),
			Entry("should handle unsupported directive", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "UnsupportedDirective",
				payload:       struct{}{},
				deviceName:    "Light",
				paramName:     "brightness",
				expectedError: "unsupported directive: UnsupportedDirective",
			}),
		)

		DescribeTable("ColorController Tests",
			validateControlDirective,
			Entry("should handle SetColor directive", testData{
				directive:     "Alexa.ColorController",
				directiveName: "SetColor",
				payload: map[string]interface{}{
					"color": map[string]interface{}{
						"hue":        240.0,
						"saturation": 0.8,
						"brightness": 0.6,
					},
				},
				deviceName: "ColorLight",
				paramName:  "hue", // maps to paramMap_ColorController
				extraCookie: map[string]interface{}{
					"paramMap_ColorController_Hue":        "hue",
					"paramMap_ColorController_Saturation": "saturation",
					"paramMap_BrightnessController":       "brightness",
				},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"hue":        240,
						"saturation": 80, // 0.8 * 100
						"brightness": 60, // 0.6 * 100
					},
				},
				respNamespace:    "Alexa.ColorController",
				respPropertyName: "color",
				respPropertyValue: map[string]interface{}{
					"hue":        240.0,
					"saturation": 0.8,
					"brightness": 0.6,
				},
			}),
			Entry("should handle missing ColorController mapping", testData{
				directive:     "Alexa.ColorController",
				directiveName: "SetColor",
				payload: map[string]interface{}{
					"color": map[string]interface{}{
						"hue":        120.0,
						"saturation": 0.5,
						"brightness": 0.5,
					},
				},
				deviceName:    "ColorLight",
				paramName:     "",
				expectedError: "missing ColorController hue mapping in cookie",
			}),
		)

		DescribeTable("ColorTemperatureController Tests",
			validateControlDirective,
			Entry("should handle SetColorTemperature directive", testData{
				directive:     "Alexa.ColorTemperatureController",
				directiveName: "SetColorTemperature",
				payload: map[string]interface{}{
					"colorTemperatureInKelvin": 5000,
				},
				deviceName: "ColorLight",
				paramName:  "cct",
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"cct": 5000,
					},
				},
				respNamespace:     "Alexa.ColorTemperatureController",
				respPropertyName:  "colorTemperatureInKelvin",
				respPropertyValue: 5000,
			}),
			Entry("should handle missing ColorTemperatureController mapping", testData{
				directive:     "Alexa.ColorTemperatureController",
				directiveName: "SetColorTemperature",
				payload: map[string]interface{}{
					"colorTemperatureInKelvin": 3000,
				},
				deviceName:    "ColorLight",
				paramName:     "",
				expectedError: "missing ColorTemperatureController mapping in cookie",
			}),
		)

		// Scenarios mirroring the Alexa Smart Home certification suite: relative controls
		// (Adjust/Increase/Decrease) and the power coupling Alexa requires (control implies on,
		// brightness 0 means off, turning on restores a usable brightness).
		DescribeTable("Certification Scenarios",
			validateControlDirective,
			Entry("AdjustBrightness lowers brightness, keeps power on", testData{
				directive:       "Alexa.BrightnessController",
				directiveName:   "AdjustBrightness",
				payload:         map[string]interface{}{"brightnessDelta": -25},
				deviceName:      "Light",
				paramName:       "brightness",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"Light": map[string]interface{}{"power": true, "brightness": 80}},
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{"brightness": 55, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.BrightnessController", Name: "brightness", Value: 55, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("AdjustBrightness from off starts at 0 and powers on", testData{
				directive:       "Alexa.BrightnessController",
				directiveName:   "AdjustBrightness",
				payload:         map[string]interface{}{"brightnessDelta": 25},
				deviceName:      "Light",
				paramName:       "brightness",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"Light": map[string]interface{}{"power": false, "brightness": 100}},
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{"brightness": 25, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.BrightnessController", Name: "brightness", Value: 25, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("AdjustBrightness to zero powers off", testData{
				directive:       "Alexa.BrightnessController",
				directiveName:   "AdjustBrightness",
				payload:         map[string]interface{}{"brightnessDelta": -25},
				deviceName:      "Light",
				paramName:       "brightness",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"Light": map[string]interface{}{"power": true, "brightness": 25}},
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{"brightness": 0, "power": false},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "OFF", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.BrightnessController", Name: "brightness", Value: 0, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("AdjustBrightness rejects invalid payload", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "AdjustBrightness",
				payload:       "invalid payload",
				deviceName:    "Light",
				paramName:     "brightness",
				expectedError: "failed to unmarshal brightness delta payload",
			}),
			Entry("SetBrightness 0 powers off", testData{
				directive:     "Alexa.BrightnessController",
				directiveName: "SetBrightness",
				payload:       map[string]interface{}{"brightness": 0},
				deviceName:    "Light",
				paramName:     "brightness",
				extraCookie:   map[string]interface{}{"paramMap_PowerController": "power"},
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{"brightness": 0, "power": false},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "OFF", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.BrightnessController", Name: "brightness", Value: 0, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("IncreaseColorTemperature steps up (cooler) and powers on", testData{
				directive:       "Alexa.ColorTemperatureController",
				directiveName:   "IncreaseColorTemperature",
				payload:         struct{}{},
				deviceName:      "ColorLight",
				paramName:       "cct",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"ColorLight": map[string]interface{}{"power": true, "cct": 4000}},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"cct": 5000, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.ColorTemperatureController", Name: "colorTemperatureInKelvin", Value: 5000, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("IncreaseColorTemperature past the test ceiling still moves up", testData{
				directive:       "Alexa.ColorTemperatureController",
				directiveName:   "IncreaseColorTemperature",
				payload:         struct{}{},
				deviceName:      "ColorLight",
				paramName:       "cct",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"ColorLight": map[string]interface{}{"power": true, "cct": 7000}},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"cct": 8000, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.ColorTemperatureController", Name: "colorTemperatureInKelvin", Value: 8000, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("DecreaseColorTemperature below the test floor still moves down", testData{
				directive:       "Alexa.ColorTemperatureController",
				directiveName:   "DecreaseColorTemperature",
				payload:         struct{}{},
				deviceName:      "ColorLight",
				paramName:       "cct",
				extraCookie:     map[string]interface{}{"paramMap_PowerController": "power"},
				initialReported: map[string]interface{}{"ColorLight": map[string]interface{}{"power": true, "cct": 2200}},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"cct": 1200, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.ColorTemperatureController", Name: "colorTemperatureInKelvin", Value: 1200, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("SetColorTemperature powers the light on", testData{
				directive:     "Alexa.ColorTemperatureController",
				directiveName: "SetColorTemperature",
				payload:       map[string]interface{}{"colorTemperatureInKelvin": 2700},
				deviceName:    "ColorLight",
				paramName:     "cct",
				extraCookie:   map[string]interface{}{"paramMap_PowerController": "power"},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"cct": 2700, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.ColorTemperatureController", Name: "colorTemperatureInKelvin", Value: 2700, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("SetColor powers the light on", testData{
				directive:     "Alexa.ColorController",
				directiveName: "SetColor",
				payload: map[string]interface{}{
					"color": map[string]interface{}{"hue": 120.0, "saturation": 1.0, "brightness": 1.0},
				},
				deviceName: "ColorLight",
				paramName:  "hue",
				extraCookie: map[string]interface{}{
					"paramMap_ColorController_Hue":        "hue",
					"paramMap_ColorController_Saturation": "saturation",
					"paramMap_BrightnessController":       "brightness",
					"paramMap_PowerController":            "power",
				},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"hue": 120, "saturation": 100, "brightness": 100, "power": true},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.ColorController", Name: "color", Value: map[string]interface{}{"hue": 120.0, "saturation": 1.0, "brightness": 1.0}, UncertaintyInMilliseconds: 0},
				},
			}),
			Entry("SetColorTemperature switches the light to CCT mode", testData{
				directive:     "Alexa.ColorTemperatureController",
				directiveName: "SetColorTemperature",
				payload:       map[string]interface{}{"colorTemperatureInKelvin": 4000},
				deviceName:    "ColorLight",
				paramName:     "cct",
				extraCookie:   map[string]interface{}{"paramMap_LightMode": "mode"},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"cct": 4000, "mode": 2},
				},
				respNamespace:     "Alexa.ColorTemperatureController",
				respPropertyName:  "colorTemperatureInKelvin",
				respPropertyValue: 4000,
			}),
			Entry("SetColor switches the light to HSV mode", testData{
				directive:     "Alexa.ColorController",
				directiveName: "SetColor",
				payload: map[string]interface{}{
					"color": map[string]interface{}{"hue": 240.0, "saturation": 1.0, "brightness": 1.0},
				},
				deviceName: "ColorLight",
				paramName:  "hue",
				extraCookie: map[string]interface{}{
					"paramMap_ColorController_Hue": "hue",
					"paramMap_LightMode":           "mode",
				},
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{"hue": 240, "mode": 1},
				},
				respNamespace:    "Alexa.ColorController",
				respPropertyName: "color",
				respPropertyValue: map[string]interface{}{
					"hue":        240.0,
					"saturation": 1.0,
					"brightness": 1.0,
				},
			}),
			Entry("TurnOn restores brightness when the light was dimmed to 0", testData{
				directive:       "Alexa.PowerController",
				directiveName:   "TurnOn",
				payload:         struct{}{},
				deviceName:      "Light",
				paramName:       "power",
				extraCookie:     map[string]interface{}{"paramMap_BrightnessController": "brightness"},
				initialReported: map[string]interface{}{"Light": map[string]interface{}{"power": false, "brightness": 0}},
				expectedShadowState: map[string]interface{}{
					"Light": map[string]interface{}{"power": true, "brightness": 100},
				},
				respProperties: []alexa_skill.ContextProperty{
					{NameSpace: "Alexa.BrightnessController", Name: "brightness", Value: 100, UncertaintyInMilliseconds: 0},
					{NameSpace: "Alexa.PowerController", Name: "powerState", Value: "ON", UncertaintyInMilliseconds: 0},
				},
			}),
		)

		DescribeTable("ToggleController Tests",
			validateControlDirective,
			Entry("should handle TurnOn directive", testData{
				directive:     "Alexa.ToggleController",
				directiveName: "TurnOn",
				payload:       struct{}{},
				deviceName:    "ColorLight",
				paramName:     "toggle",
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"toggle": true,
					},
				},
				respNamespace:     "Alexa.ToggleController",
				respPropertyName:  "toggleState",
				respPropertyValue: "ON",
			}),
			Entry("should handle TurnOff directive", testData{
				directive:     "Alexa.ToggleController",
				directiveName: "TurnOff",
				payload:       struct{}{},
				deviceName:    "ColorLight",
				paramName:     "toggle",
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"toggle": false,
					},
				},
				respNamespace:     "Alexa.ToggleController",
				respPropertyName:  "toggleState",
				respPropertyValue: "OFF",
			}),
			Entry("should handle missing ToggleController mapping", testData{
				directive:     "Alexa.ToggleController",
				directiveName: "TurnOn",
				payload:       struct{}{},
				deviceName:    "ColorLight",
				paramName:     "",
				expectedError: "missing ToggleController mapping in cookie",
			}),
		)

		DescribeTable("ModeController Tests",
			validateControlDirective,
			Entry("should handle SetMode directive", testData{
				directive:     "Alexa.ModeController",
				directiveName: "SetMode",
				payload: map[string]interface{}{
					"mode": 3,
				},
				deviceName: "ColorLight",
				paramName:  "mode",
				expectedShadowState: map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"mode": 3,
					},
				},
				respNamespace:     "Alexa.ModeController",
				respPropertyName:  "mode",
				respPropertyValue: float64(3),
			}),
			Entry("should handle missing ModeController mapping", testData{
				directive:     "Alexa.ModeController",
				directiveName: "SetMode",
				payload: map[string]interface{}{
					"mode": "reading",
				},
				deviceName:    "ColorLight",
				paramName:     "",
				expectedError: "missing ModeController mapping in cookie",
			}),
		)

		DescribeTable("ReportState Tests",
			func(tc reportStateTestCase) {
				// Setup initial state
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)

				// Add groupID to cookie this cannot be done in the Entry clause in ginkgo, so have to enter it here
				if tc.cookie != nil {
					tc.cookie["groupID"] = testGroup.GroupID
				}

				shadowJSON, _ := json.Marshal(tc.initialState)
				iotDataClient.AddDirect(testNodeID1, shadowName, shadowJSON)

				// For the unauthorized-access case seed the end user so the token resolves; the failure then comes from node-permission loading, not identity resolution.
				if tc.expectedError == "failed to load node permissions" {
					test_utils.SetupTestUser(ctx, tc.tokenSub, tc.tokenIdentityID)
				}
				token := createTestToken(tc.tokenSub, tc.tokenIdentityID)

				request = createTestRequest("Alexa", "ReportState", struct{}{})
				request.Directive.Endpoint = &alexa_skill.Endpoint{
					EndpointID: alexa_skill.GetEndpointId(testNodeID1, tc.deviceName),
					Cookie:     tc.cookie,
					Scope: &alexa_skill.Scope{
						Type:  "BearerToken",
						Token: token,
					},
				}

				response, err := handler(ctx, request)
				if tc.expectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(rmerror.ErrorWithStack(err)).To(ContainSubstring(tc.expectedError))
					return
				}

				Expect(err).To(BeNil())

				// Clear timestamps for comparison
				for i := range response.Context.Properties {
					response.Context.Properties[i].TimeOfSample = ""
				}

				var s interface{} = ""
				expectedResponse := alexa_skill.AlexaResponse{
					Event: alexa_skill.Event{
						Header: alexa_skill.Header{
							Namespace:      "Alexa",
							Name:           "StateReport",
							PayloadVersion: "3",
							MessageID:      "test-message-id",
						},
						Endpoint: &alexa_skill.Endpoint{
							EndpointID: alexa_skill.GetEndpointId(testNodeID1, tc.deviceName),
						},
						Payload: &s,
					},
					Context: &alexa_skill.Context{
						Properties: tc.expectedProps,
					},
				}

				// Sort properties for consistent comparison
				sort.Slice(expectedResponse.Context.Properties, func(i, j int) bool {
					return expectedResponse.Context.Properties[i].Name < expectedResponse.Context.Properties[j].Name
				})
				sort.Slice(response.Context.Properties, func(i, j int) bool {
					return response.Context.Properties[i].Name < response.Context.Properties[j].Name
				})

				test_utils.AssertNormalizedEqual(response, expectedResponse)
			},
			Entry("should handle ReportState directive for a switch", reportStateTestCase{
				deviceName: "Switch",
				cookie: map[string]interface{}{
					// groupID field is dynamically inserted during test execution
					"paramMap_PowerController": "power",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(true),
							Params: map[string]interface{}{
								"Switch": map[string]interface{}{
									"power": true,
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "OK"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			Entry("should handle ReportState directive for a light with brightness", reportStateTestCase{
				deviceName: "Light",
				cookie: map[string]interface{}{
					// groupID field is dynamically inserted during test execution
					"paramMap_PowerController":      "Power",
					"paramMap_BrightnessController": "brightness",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(true),
							Params: map[string]interface{}{
								"Light": map[string]interface{}{
									"Power":      true,
									"brightness": 75.0,
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.BrightnessController",
						Name:                      "brightness",
						Value:                     75,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "OK"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			Entry("should handle ReportState for color light with all capabilities", reportStateTestCase{
				deviceName: "ColorLight",
				cookie: map[string]interface{}{
					"paramMap_PowerController":            "power",
					"paramMap_BrightnessController":       "brightness",
					"paramMap_ColorController_Hue":        "hue",
					"paramMap_ColorController_Saturation": "saturation",
					"paramMap_ColorTemperatureController": "cct",
					"paramMap_ToggleController":           "toggle",
					"paramMap_ModeController":             "mode",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(true),
							Params: map[string]interface{}{
								"ColorLight": map[string]interface{}{
									"power":      true,
									"brightness": 80.0,
									"hue":        180.0,
									"saturation": 75.0,
									"cct":        4000.0,
									"toggle":     false,
									"mode":       2.0,
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.BrightnessController",
						Name:                      "brightness",
						Value:                     80,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.ColorController",
						Name:      "color",
						Value: map[string]interface{}{
							"hue":        180.0,
							"saturation": 0.75,
							"brightness": 0.80,
						},
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.ColorTemperatureController",
						Name:                      "colorTemperatureInKelvin",
						Value:                     4000,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.ToggleController",
						Name:                      "toggleState",
						Value:                     "OFF",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.ModeController",
						Name:                      "mode",
						Value:                     2.0,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "OK"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			// Light Mode suppression: a colour-and-CCT bulb reports only the active controller (HSV or CCT). The mode value comes from the shadow param identified by cookie["paramMap_LightMode"].
			//
			// Light Mode = 1 (HSV) -> ColorController emitted, ColorTemperatureController suppressed.
			Entry("HSV light mode: emit ColorController, skip ColorTemperatureController", reportStateTestCase{
				deviceName: "ColorLight",
				cookie: map[string]interface{}{
					"paramMap_PowerController":            "power",
					"paramMap_BrightnessController":       "brightness",
					"paramMap_ColorController_Hue":        "hue",
					"paramMap_ColorController_Saturation": "saturation",
					"paramMap_ColorTemperatureController": "cct",
					"paramMap_LightMode":                  "lightMode",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(true),
							Params: map[string]interface{}{
								"ColorLight": map[string]interface{}{
									"power":      true,
									"brightness": 80.0,
									"hue":        180.0,
									"saturation": 75.0,
									"cct":        4000.0,
									"lightMode":  1.0, // HSV
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.BrightnessController",
						Name:                      "brightness",
						Value:                     80,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.ColorController",
						Name:      "color",
						Value: map[string]interface{}{
							"hue":        180.0,
							"saturation": 0.75,
							"brightness": 0.80,
						},
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "OK"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			// Light Mode = 2 (CCT) -> ColorTemperatureController emitted, ColorController suppressed.
			Entry("CCT light mode: emit ColorTemperatureController, skip ColorController", reportStateTestCase{
				deviceName: "ColorLight",
				cookie: map[string]interface{}{
					"paramMap_PowerController":            "power",
					"paramMap_BrightnessController":       "brightness",
					"paramMap_ColorController_Hue":        "hue",
					"paramMap_ColorController_Saturation": "saturation",
					"paramMap_ColorTemperatureController": "cct",
					"paramMap_LightMode":                  "lightMode",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(true),
							Params: map[string]interface{}{
								"ColorLight": map[string]interface{}{
									"power":      true,
									"brightness": 80.0,
									"hue":        180.0,
									"saturation": 75.0,
									"cct":        4000.0,
									"lightMode":  2.0, // CCT
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.BrightnessController",
						Name:                      "brightness",
						Value:                     80,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace:                 "Alexa.ColorTemperatureController",
						Name:                      "colorTemperatureInKelvin",
						Value:                     4000,
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "OK"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			Entry("should handle unauthorized access", reportStateTestCase{
				deviceName: "Switch",
				cookie:     map[string]interface{}{
					// groupID field is dynamically inserted during test execution
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{
								"Switch": map[string]interface{}{
									"power": true,
								},
							},
						},
					},
				},
				tokenSub:        "dummy-sub-entry_unauthorized",
				tokenIdentityID: "unauthorized@example.com",
				expectedError:   "failed to load node permissions",
			}),
			Entry("should handle invalid shadow data", reportStateTestCase{
				deviceName: "Switch",
				cookie:     map[string]interface{}{
					// groupID field is dynamically inserted during test execution
				},
				initialState:    node.IoTNodeShadow{}, // Invalid state: never-reported shadow, Online is nil
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "UNREACHABLE"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
			Entry("should report UNREACHABLE when the shadow reports the node offline", reportStateTestCase{
				deviceName: "Switch",
				cookie: map[string]interface{}{
					// groupID field is dynamically inserted during test execution
					"paramMap_PowerController": "power",
				},
				initialState: node.IoTNodeShadow{
					State: &node.ShadowState{
						Reported: &node.ReportedOrDesiredShadow{
							Online: utils.Ptr(false),
							Params: map[string]interface{}{
								"Switch": map[string]interface{}{
									"power": true,
								},
							},
						},
					},
				},
				tokenSub:        userID,
				tokenIdentityID: "test-user@example.com",
				expectedProps: []alexa_skill.ContextProperty{
					{
						NameSpace:                 "Alexa.PowerController",
						Name:                      "powerState",
						Value:                     "ON",
						UncertaintyInMilliseconds: 0,
					},
					{
						NameSpace: "Alexa.EndpointHealth",
						Name:      "connectivity",
						Value: struct {
							Value string `json:"value"`
						}{Value: "UNREACHABLE"},
						UncertaintyInMilliseconds: 0,
					},
				},
			}),
		)
	})
})

func createTestRequest(namespace, name string, payload interface{}) alexa_skill.AlexaRequest {
	payloadBytes, _ := json.Marshal(payload)
	return alexa_skill.AlexaRequest{
		Directive: alexa_skill.Directive{
			Header: alexa_skill.Header{
				Namespace:      namespace,
				Name:           name,
				PayloadVersion: "3",
				MessageID:      "test-message-id",
			},
			Payload: payloadBytes,
		},
	}
}

// createTestToken mints an ESP User RS256 access token (sub == user_id) via the OIDC harness.
// IdentityID is retained for call-site readability but is not part of the token.
func createTestToken(Sub, IdentityID string) string {
	return tokenHarness.Mint(Sub)
}

func storeNodeCfgInDb(db *node_details_db.NodeDetailsDB, node_cfg_path string) {
	node_cfg_bytes, err := os.ReadFile(node_cfg_path)
	Expect(err).To(BeNil())
	var node_cfg map[string]interface{}
	err = json.Unmarshal(node_cfg_bytes, &node_cfg)
	Expect(err).To(BeNil())
	err = db.UpdateServiceData("config", node_cfg)
	Expect(err).To(BeNil())
}

func getDiscoveryResponse(discovery_response_path string) map[string]interface{} {
	discovery_response_bytes, err := os.ReadFile(discovery_response_path)
	Expect(err).To(BeNil())
	discovery_response_map := make(map[string]interface{})
	err = json.Unmarshal(discovery_response_bytes, &discovery_response_map)
	Expect(err).To(BeNil())
	return discovery_response_map
}

var _ = AfterSuite(func() {
	if profile != nil {
		fmt.Fprintf(timingFile, "\n--- Alexa Skill (Turn On) ---\n")
		profile.Print(timingFile)
		fmt.Fprintf(timingFile, "-----------------------------\n\n")
	}
	timingFile.Close()
})
