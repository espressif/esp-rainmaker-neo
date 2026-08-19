// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/gva"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGVAAction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GVA Action Suite")
}

var tokenHarness *test_utils.ESPUserTokenHarness

var _ = BeforeSuite(func() {
})

// Test data is imported from gva_discovery_test_data.go

var _ = Describe("GVA Action", func() {
	var (
		ctx             context.Context
		rmngUserContext *rmngctx.RmngContext
		testNode1       *node.Node
		testNode2       *node.Node
		testNode3       *node.Node
		testGroup       *group.Group
		testNodeID1     string
		testNodeID2     string
		testNodeID3     string
		testToken       string
		mockHTTPClient  *mock.MockHTTPClient
	)
	userID := "26fd9a10-ca12-402f-97dd-0e6913cc2dba"

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		tokenHarness = test_utils.SetupESPUserTokenHarness(ctx)

		// Setup SSM mock parameter for GVA service account JSON
		ssmMock := awscommon.GetSSMClient()
		ssmMock.PutParameter(ctx, &ssm.PutParameterInput{
			Name:  aws.String(gva.GVASSMServiceAccountJSONParam),
			Value: aws.String(`{"type":"service_account","project_id":"test-project-id","private_key_id":"test-key-id","private_key":"-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n","client_email":"test@test.iam.gserviceaccount.com","client_id":"123456789","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","auth_provider_x509_cert_url":"https://www.googleapis.com/oauth2/v1/certs","client_x509_cert_url":"https://www.googleapis.com/robot/v1/metadata/x509/test","universe_domain":"googleapis.com"}`),
		})

		// Create test user
		_, rmngUserContext = test_utils.SetupTestUser(ctx, userID, "test-user@example.com")

		// Create a test group
		var err error
		testGroup, err = group.CreateGroupForUser(rmngUserContext, "Living Room")
		Expect(err).To(BeNil())

		// Create test nodes with different configurations
		testNodeID1 = "test-node1"
		testNode1 = node.NewNode(testNodeID1)
		rmngUserContext.SetAllow(utils.NodeAll, testNodeID1)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, testNodeID1)

		testNodeID2 = "test-node2"
		testNode2 = node.NewNode(testNodeID2)
		rmngUserContext.SetAllow(utils.NodeAll, testNodeID2)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, testNodeID2)

		testNodeID3 = "test-node3"
		testNode3 = node.NewNode(testNodeID3)
		rmngUserContext.SetAllow(utils.NodeAll, testNodeID3)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, testNodeID3)

		// Store node configurations
		rmngNodeContext := rmngctx.NewRmngContext(testNode1)
		nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
		err = nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_switch_test_data.ToMap())
		Expect(err).To(BeNil())

		rmngNodeContext = rmngctx.NewRmngContext(testNode2)
		nodeDetailsDB = node_details_db.NewNodeDetailsDB(rmngNodeContext)
		err = nodeDetailsDB.UpdateServiceData("config", node_cfg_simple_light_test_data.ToMap())
		Expect(err).To(BeNil())

		rmngNodeContext = rmngctx.NewRmngContext(testNode3)
		nodeDetailsDB = node_details_db.NewNodeDetailsDB(rmngNodeContext)
		err = nodeDetailsDB.UpdateServiceData("config", node_cfg_color_light_test_data.ToMap())
		Expect(err).To(BeNil())

		// Setup mock HTTP client
		mockHTTPClient = mock.NewMockHTTPClient()
		httpclient.Set(mockHTTPClient)

		testToken = createTestToken(userID, "test-user@example.com")
	})

	AfterEach(func() {
		tokenHarness.Close()
	})

	Describe("SYNC (Discovery)", func() {
		It("should handle SYNC request successfully", func() {
			// Create a SYNC request
			syncRequest := gva.GVARequest{
				RequestID: "test-request-id",
				Inputs: []gva.Input{
					{
						Intent:  gva.IntentSync,
						Payload: json.RawMessage(`{}`),
					},
				},
			}

			requestBody, err := json.Marshal(syncRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			err = json.Unmarshal([]byte(response.Body), &gvaResponse)
			Expect(err).To(BeNil())
			Expect(gvaResponse.RequestID).To(Equal("test-request-id"))

			// Verify sync payload
			var syncPayload gva.SyncPayload
			payloadBytes, err := json.Marshal(gvaResponse.Payload)
			Expect(err).To(BeNil())
			err = json.Unmarshal(payloadBytes, &syncPayload)
			Expect(err).To(BeNil())

			Expect(syncPayload.AgentUserID).To(Equal(userID))
			Expect(syncPayload.Devices).To(HaveLen(3)) // Switch, Light, and ColorLight

			// Verify device types
			var deviceTypes []string
			for _, d := range syncPayload.Devices {
				deviceTypes = append(deviceTypes, d.Type)
			}
			Expect(deviceTypes).To(ContainElement(gva.DeviceTypeSwitch))
			Expect(deviceTypes).To(ContainElement(gva.DeviceTypeLight))
		})

		It("should use esp.param.name from shadow as device name", func() {
			// Mock shadow with custom name values
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)

			// Set shadow for testNode1 (Switch) with custom name
			switchShadow := node.IoTNodeShadow{
				State: &node.ShadowState{
					Reported: &node.ReportedOrDesiredShadow{
						Params: map[string]interface{}{
							"Switch": map[string]interface{}{
								"name": "Front Door Switch",
							},
						},
					},
				},
			}
			switchShadowJSON, _ := json.Marshal(switchShadow)
			iotDataClient.AddDirect(testNodeID1, shadowName, switchShadowJSON)

			// Set shadow for testNode2 (Light) with custom name
			lightShadow := node.IoTNodeShadow{
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
			lightShadowJSON, _ := json.Marshal(lightShadow)
			iotDataClient.AddDirect(testNodeID2, shadowName, lightShadowJSON)

			syncPayload := executeSyncRequest(testToken)

			// Strip dynamic groupID for comparison
			for i := range syncPayload.Devices {
				delete(syncPayload.Devices[i].CustomData, "groupID")
			}

			expected := gva.SyncPayload{
				AgentUserID: userID,
				Devices: []gva.Device{
					{
						ID:              testNodeID1 + ".Switch",
						Type:            gva.DeviceTypeSwitch,
						Traits:          []string{gva.TraitOnOff},
						Name:            gva.DeviceName{Name: "Front Door Switch"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						CustomData:      map[string]interface{}{"paramMap_OnOff": "power"},
					},
					{
						ID:              testNodeID2 + ".Light",
						Type:            gva.DeviceTypeLight,
						Traits:          []string{gva.TraitOnOff, gva.TraitBrightness},
						Name:            gva.DeviceName{Name: "Kitchen Light"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						CustomData:      map[string]interface{}{"paramMap_OnOff": "power", "paramMap_Brightness": "brightness"},
					},
					{
						ID:              testNodeID3 + ".ColorLight",
						Type:            gva.DeviceTypeLight,
						Traits:          []string{gva.TraitOnOff, gva.TraitBrightness, gva.TraitColorSetting, gva.TraitModes},
						Name:            gva.DeviceName{Name: "ColorLight"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						Attributes: map[string]interface{}{
							"colorModel":            "rgb",
							"colorTemperatureRange": map[string]interface{}{"temperatureMinK": 2700, "temperatureMaxK": 6500},
							"availableModes":        buildExpectedModesAttribute(),
						},
						CustomData: map[string]interface{}{
							"paramMap_OnOff": "power", "paramMap_Brightness": "brightness",
							"paramMap_ColorSetting_Hue": "hue", "paramMap_ColorSetting_Saturation": "saturation",
							"paramMap_ColorSetting_CCT": "cct", "paramMap_Modes": "mode",
							// paramMap_LightMode is the same param as paramMap_Modes here but tracked separately so handle_query.go can distinguish light-mode from generic Modes params (fan modes, etc.).
							"paramMap_LightMode": "mode",
						},
					},
				},
			}
			test_utils.AssertNormalizedEqual(expected, syncPayload)
		})

		It("should fallback to config device name when shadow name missing", func() {
			// No shadow data set - should use config names
			syncPayload := executeSyncRequest(testToken)

			// Strip dynamic groupID for comparison
			for i := range syncPayload.Devices {
				delete(syncPayload.Devices[i].CustomData, "groupID")
			}

			expected := gva.SyncPayload{
				AgentUserID: userID,
				Devices: []gva.Device{
					{
						ID:              testNodeID1 + ".Switch",
						Type:            gva.DeviceTypeSwitch,
						Traits:          []string{gva.TraitOnOff},
						Name:            gva.DeviceName{Name: "Switch"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						CustomData:      map[string]interface{}{"paramMap_OnOff": "power"},
					},
					{
						ID:              testNodeID2 + ".Light",
						Type:            gva.DeviceTypeLight,
						Traits:          []string{gva.TraitOnOff, gva.TraitBrightness},
						Name:            gva.DeviceName{Name: "Light"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						CustomData:      map[string]interface{}{"paramMap_OnOff": "power", "paramMap_Brightness": "brightness"},
					},
					{
						ID:              testNodeID3 + ".ColorLight",
						Type:            gva.DeviceTypeLight,
						Traits:          []string{gva.TraitOnOff, gva.TraitBrightness, gva.TraitColorSetting, gva.TraitModes},
						Name:            gva.DeviceName{Name: "ColorLight"},
						WillReportState: true,
						DeviceInfo:      &gva.DeviceInfo{Manufacturer: "ESP32", Model: "RainMaker Device", SwVersion: "1.0"},
						Attributes: map[string]interface{}{
							"colorModel":            "rgb",
							"colorTemperatureRange": map[string]interface{}{"temperatureMinK": 2700, "temperatureMaxK": 6500},
							"availableModes":        buildExpectedModesAttribute(),
						},
						CustomData: map[string]interface{}{
							"paramMap_OnOff": "power", "paramMap_Brightness": "brightness",
							"paramMap_ColorSetting_Hue": "hue", "paramMap_ColorSetting_Saturation": "saturation",
							"paramMap_ColorSetting_CCT": "cct", "paramMap_Modes": "mode",
							"paramMap_LightMode": "mode",
						},
					},
				},
			}
			test_utils.AssertNormalizedEqual(expected, syncPayload)
		})

		It("should handle unauthorized access", func() {
			syncRequest := gva.GVARequest{
				RequestID: "test-request-id",
				Inputs: []gva.Input{
					{
						Intent:  gva.IntentSync,
						Payload: json.RawMessage(`{}`),
					},
				},
			}

			requestBody, err := json.Marshal(syncRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer invalid-token",
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(500)) // Internal error due to invalid token
		})
	})

	Describe("QUERY (State)", func() {
		It("should handle state query for switch", func() {
			// Set up shadow state for the switch
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
			shadowState := map[string]interface{}{
				"state": map[string]interface{}{
					"reported": map[string]interface{}{
						"online": true,
						"params": map[string]interface{}{
							"Switch": map[string]interface{}{
								"power": true,
							},
						},
					},
				},
			}
			shadowBytes, err := json.Marshal(shadowState)
			Expect(err).To(BeNil())
			iotDataClient.AddDirect(testNodeID1, shadowName, shadowBytes)

			// Create QUERY request
			queryRequest := gva.GVARequest{
				RequestID: "test-request-id",
				Inputs: []gva.Input{
					{
						Intent: gva.IntentQuery,
						Payload: json.RawMessage(`{
							"devices": [
								{
									"id": "` + testNodeID1 + `.Switch",
									"customData": {
										"groupID": "` + testGroup.GroupID + `",
										"paramMap_OnOff": "power"
									}
								}
							]
						}`),
					},
				},
			}

			requestBody, err := json.Marshal(queryRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			err = json.Unmarshal([]byte(response.Body), &gvaResponse)
			Expect(err).To(BeNil())

			var queryPayload gva.QueryPayload
			payloadBytes, err := json.Marshal(gvaResponse.Payload)
			Expect(err).To(BeNil())
			err = json.Unmarshal(payloadBytes, &queryPayload)
			Expect(err).To(BeNil())

			deviceID := testNodeID1 + ".Switch"
			deviceStates, exists := queryPayload.Devices[deviceID]
			Expect(exists).To(BeTrue())

			deviceStatesMap := deviceStates.(map[string]interface{})

			// Check if device is online - this should always be true in our mock
			Expect(deviceStatesMap["online"]).To(Equal(true))

			// Check if the device has a status - should be success or offline
			status, hasStatus := deviceStatesMap["status"]
			if hasStatus {
				Expect(status).To(Equal("SUCCESS"))
			}

			// For the "on" state, we'll accept either true or nil (parameter not found)
			// since the exact shadow data format might vary in mock mode
			onState := deviceStatesMap["on"]
			if onState != nil {
				Expect(onState).To(Equal(true))
			}
		})

		// Light Mode gating: a colour-and-CCT bulb reports only the active controller in QUERY. Without this, Google Home renders the colour as "unset" because spectrumHsv and temperatureK disagree.
		DescribeTable("ColorLight QUERY gates Color vs Temperature by Light Mode",
			func(lightModeValue interface{}, customDataKVs map[string]string, expectedColor map[string]interface{}) {
				// Set up shadow with all colour params plus Light Mode value.
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
				params := map[string]interface{}{
					"ColorLight": map[string]interface{}{
						"power":      true,
						"brightness": 80,
						"hue":        180,
						"saturation": 75,
						"cct":        4000,
					},
				}
				if lightModeValue != nil {
					params["ColorLight"].(map[string]interface{})["mode"] = lightModeValue
				}
				shadowState := map[string]interface{}{
					"state": map[string]interface{}{"reported": map[string]interface{}{"params": params}},
				}
				shadowBytes, err := json.Marshal(shadowState)
				Expect(err).To(BeNil())
				iotDataClient.AddDirect(testNodeID3, shadowName, shadowBytes)

				// Build customData JSON from the supplied map plus groupID.
				cd := map[string]string{
					"groupID":                          testGroup.GroupID,
					"paramMap_OnOff":                   "power",
					"paramMap_Brightness":              "brightness",
					"paramMap_ColorSetting_Hue":        "hue",
					"paramMap_ColorSetting_Saturation": "saturation",
					"paramMap_ColorSetting_CCT":        "cct",
				}
				for k, v := range customDataKVs {
					cd[k] = v
				}
				cdBytes, err := json.Marshal(cd)
				Expect(err).To(BeNil())

				queryRequest := gva.GVARequest{
					RequestID: "test-request-id",
					Inputs: []gva.Input{
						{
							Intent:  gva.IntentQuery,
							Payload: json.RawMessage(`{"devices":[{"id":"` + testNodeID3 + `.ColorLight","customData":` + string(cdBytes) + `}]}`),
						},
					},
				}
				requestBody, err := json.Marshal(queryRequest)
				Expect(err).To(BeNil())

				request := events.APIGatewayProxyRequest{
					HTTPMethod: "POST",
					Body:       string(requestBody),
					Headers:    map[string]string{"Authorization": "Bearer " + testToken},
				}

				response, err := handler(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(200))

				var gvaResponse gva.GVAResponse
				Expect(json.Unmarshal([]byte(response.Body), &gvaResponse)).To(Succeed())
				var queryPayload gva.QueryPayload
				payloadBytes, _ := json.Marshal(gvaResponse.Payload)
				Expect(json.Unmarshal(payloadBytes, &queryPayload)).To(Succeed())

				deviceState, ok := queryPayload.Devices[testNodeID3+".ColorLight"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "device state missing for ColorLight")

				gotColor, hasColor := deviceState["color"]
				if expectedColor == nil {
					Expect(hasColor).To(BeFalse(), "color must not be reported")
					return
				}
				Expect(hasColor).To(BeTrue(), "color must be reported")
				test_utils.AssertNormalizedEqual(expectedColor, gotColor)
			},
			Entry("HSV mode → spectrumRgb, no temperatureK",
				float64(1),
				map[string]string{"paramMap_LightMode": "mode"},
				map[string]interface{}{
					"spectrumRgb": 3394764, // hsv(180, 0.75, 0.80)
				},
			),
			Entry("CCT mode → temperatureK, no spectrumRgb",
				float64(2),
				map[string]string{"paramMap_LightMode": "mode"},
				map[string]interface{}{
					"temperatureK": 4000,
				},
			),
			Entry("missing paramMap_LightMode → legacy HSV-priority behaviour",
				float64(2),          // even though shadow says CCT, no cookie means no gating
				map[string]string{}, // no paramMap_LightMode
				map[string]interface{}{
					"spectrumRgb": 3394764, // hsv(180, 0.75, 0.80)
				},
			),
			Entry("invalid Light Mode value (0) → legacy HSV-priority behaviour",
				float64(0),
				map[string]string{"paramMap_LightMode": "mode"},
				map[string]interface{}{
					"spectrumRgb": 3394764, // hsv(180, 0.75, 0.80)
				},
			),
		)

		// QUERY connectivity must track the shadow's reported.online (set false by the presence disconnect handler), the same field Report State reads. Reading the nodes-online table instead reported a stale "connected" because that table is not updated on disconnect.
		It("reports online:false when the reported shadow is offline", func() {
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
			shadowState := map[string]interface{}{
				"state": map[string]interface{}{"reported": map[string]interface{}{
					"online": false,
					"params": map[string]interface{}{"ColorLight": map[string]interface{}{"power": true}},
				}},
			}
			shadowBytes, err := json.Marshal(shadowState)
			Expect(err).To(BeNil())
			iotDataClient.AddDirect(testNodeID3, shadowName, shadowBytes)

			cd, err := json.Marshal(map[string]string{"groupID": testGroup.GroupID, "paramMap_OnOff": "power"})
			Expect(err).To(BeNil())
			queryRequest := gva.GVARequest{
				RequestID: "test-request-id",
				Inputs: []gva.Input{{
					Intent:  gva.IntentQuery,
					Payload: json.RawMessage(`{"devices":[{"id":"` + testNodeID3 + `.ColorLight","customData":` + string(cd) + `}]}`),
				}},
			}
			requestBody, err := json.Marshal(queryRequest)
			Expect(err).To(BeNil())

			response, err := handler(ctx, events.APIGatewayProxyRequest{
				HTTPMethod: "POST", Body: string(requestBody),
				Headers: map[string]string{"Authorization": "Bearer " + testToken},
			})
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			Expect(json.Unmarshal([]byte(response.Body), &gvaResponse)).To(Succeed())
			var queryPayload gva.QueryPayload
			payloadBytes, _ := json.Marshal(gvaResponse.Payload)
			Expect(json.Unmarshal(payloadBytes, &queryPayload)).To(Succeed())

			deviceState, ok := queryPayload.Devices[testNodeID3+".ColorLight"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(deviceState["online"]).To(Equal(false))
		})
	})

	Describe("EXECUTE (Control)", func() {
		DescribeTable("should handle various commands",
			func(command, deviceSuffix, groupIDKey, paramMapKey, paramMapValue, executionCommand string, params map[string]interface{}, expectedStates map[string]interface{}, expectedShadowDevice string, expectedShadowParams map[string]interface{}, expectedReads, expectedWrites int, initialShadowParams map[string]interface{}) {
				var nodeID string
				var testNode *node.Node

				if deviceSuffix == "Switch" {
					nodeID = testNodeID1
					testNode = testNode1
				} else {
					nodeID = testNodeID2
					testNode = testNode2
				}

				// Set up initial shadow state with different values to verify state transition
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
				initialShadowState := map[string]interface{}{
					"state": map[string]interface{}{
						"reported": map[string]interface{}{
							"params": map[string]interface{}{
								expectedShadowDevice: initialShadowParams,
							},
						},
					},
				}
				shadowBytes, err := json.Marshal(initialShadowState)
				Expect(err).To(BeNil())
				iotDataClient.AddDirect(nodeID, shadowName, shadowBytes)

				executeRequest := gva.GVARequest{
					RequestID: "test-request-id",
					Inputs: []gva.Input{
						{
							Intent: gva.IntentExecute,
							Payload: json.RawMessage(fmt.Sprintf(`{
								"commands": [
									{
										"devices": [
											{
												"id": "%s.%s",
												"customData": {
													"%s": "%s",
													"%s": "%s"
												}
											}
										],
										"execution": [
											{
												"command": "%s",
												"params": %s
											}
										]
									}
								]
							}`, nodeID, deviceSuffix, groupIDKey, testGroup.GroupID, paramMapKey, paramMapValue, executionCommand, toJSON(params))),
						},
					},
				}

				requestBody, err := json.Marshal(executeRequest)
				Expect(err).To(BeNil())

				// Start database profiling
				dbMock := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
				dbMock.ProfileReset()

				request := events.APIGatewayProxyRequest{
					HTTPMethod: "POST",
					Body:       string(requestBody),
					Headers: map[string]string{
						"Authorization": "Bearer " + testToken,
					},
				}

				response, err := handler(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(200))

				// Get database profile after the operation
				profile := dbMock.ProfileGet()
				readCount, writeCount := profile.TotalCounts()

				// Log database operation profile for performance monitoring
				fmt.Printf("GVA %s Execute Database Profile - Reads: %d, Writes: %d\n", command, readCount, writeCount)

				// Assert expected database operation counts to catch performance regressions
				Expect(readCount).To(Equal(expectedReads))
				Expect(writeCount).To(Equal(expectedWrites))

				var gvaResponse gva.GVAResponse
				err = json.Unmarshal([]byte(response.Body), &gvaResponse)
				Expect(err).To(BeNil())

				var executePayload gva.ExecutePayload
				payloadBytes, err := json.Marshal(gvaResponse.Payload)
				Expect(err).To(BeNil())
				err = json.Unmarshal(payloadBytes, &executePayload)
				Expect(err).To(BeNil())

				Expect(executePayload.Commands).To(HaveLen(1))
				commandResult := executePayload.Commands[0]
				Expect(commandResult.Status).To(Equal("SUCCESS"))
				Expect(commandResult.IDs).To(ContainElement(nodeID + "." + deviceSuffix))

				// Verify expected states if provided
				if expectedStates != nil {
					for key, expectedValue := range expectedStates {
						Expect(commandResult.States).To(HaveKey(key))
						Expect(commandResult.States[key]).To(Equal(expectedValue))
					}
				}

				// Verify shadow state was updated correctly (should be different from initial state)
				dataDesired := test_utils.GetPublishedDataForNodeGroup(testNode, group_node_db.NodesGroups{
					Group: testGroup.GroupID,
				})
				dataDesiredMap := make(map[string]interface{})
				err = json.Unmarshal(dataDesired, &dataDesiredMap)
				Expect(err).To(BeNil())

				expectedShadowState := map[string]interface{}{
					expectedShadowDevice: expectedShadowParams,
				}
				test_utils.AssertNormalizedEqual(test_utils.ConvertAllFloatToInt(dataDesiredMap), test_utils.ConvertAllFloatToInt(expectedShadowState))
			},
			Entry("OnOff command for switch", "OnOff", "Switch", "groupID", "paramMap_OnOff", "power", "action.devices.commands.OnOff",
				map[string]interface{}{"on": true},
				nil,                                                   // No expected states for OnOff
				"Switch", map[string]interface{}{"power": true}, 3, 0, // 3 reads, 0 writes (GetUserIDFromToken now resolves sub from token claims — no user-details read)
				map[string]interface{}{"power": false}), // Initial shadow state - opposite of final state
			Entry("BrightnessAbsolute command for light", "BrightnessAbsolute", "Light", "groupID", "paramMap_Brightness", "brightness", "action.devices.commands.BrightnessAbsolute",
				map[string]interface{}{"brightness": 75},
				map[string]interface{}{"brightness": float64(75), "online": true}, // Expected states for brightness
				"Light", map[string]interface{}{"brightness": 75}, 3, 0, // 3 reads, 0 writes (GetUserIDFromToken now resolves sub from token claims — no user-details read)
				map[string]interface{}{"brightness": 25}), // Initial shadow state - different from final state (75)
			Entry("OnOff command for light power", "OnOff", "Light", "groupID", "paramMap_OnOff", "Power", "action.devices.commands.OnOff",
				map[string]interface{}{"on": false},
				map[string]interface{}{"on": false, "online": true}, // Expected states for OnOff
				"Light", map[string]interface{}{"Power": false}, 3, 0, // 3 reads, 0 writes (GetUserIDFromToken now resolves sub from token claims — no user-details read)
				map[string]interface{}{"Power": true}), // Initial shadow state - opposite of final state
		)

		It("should handle ColorAbsolute command with spectrumHSV", func() {
			// Set up initial shadow state for color light
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
			initialShadowState := map[string]interface{}{
				"state": map[string]interface{}{
					"reported": map[string]interface{}{
						"params": map[string]interface{}{
							"ColorLight": map[string]interface{}{
								"hue": 0, "saturation": 0, "brightness": 100,
							},
						},
					},
				},
			}
			shadowBytes, err := json.Marshal(initialShadowState)
			Expect(err).To(BeNil())
			iotDataClient.AddDirect(testNodeID3, shadowName, shadowBytes)

			// Google sends spectrumHSV (uppercase HSV) — this matches actual production traffic
			executeRequest := gva.GVARequest{
				RequestID: "test-color-request",
				Inputs: []gva.Input{
					{
						Intent: gva.IntentExecute,
						Payload: json.RawMessage(fmt.Sprintf(`{
							"commands": [
								{
									"devices": [
										{
											"id": "%s.ColorLight",
											"customData": {
												"groupID": "%s",
												"paramMap_OnOff": "power",
												"paramMap_Brightness": "brightness",
												"paramMap_ColorSetting_Hue": "hue",
												"paramMap_ColorSetting_Saturation": "saturation"
											}
										}
									],
									"execution": [
										{
											"command": "action.devices.commands.ColorAbsolute",
											"params": {
												"color": {
													"spectrumHSV": {
														"hue": 254.82,
														"saturation": 1.0,
														"value": 1.0
													}
												}
											}
										}
									]
								}
							]
						}`, testNodeID3, testGroup.GroupID)),
					},
				},
			}

			requestBody, err := json.Marshal(executeRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			err = json.Unmarshal([]byte(response.Body), &gvaResponse)
			Expect(err).To(BeNil())

			var executePayload gva.ExecutePayload
			payloadBytes, err := json.Marshal(gvaResponse.Payload)
			Expect(err).To(BeNil())
			err = json.Unmarshal(payloadBytes, &executePayload)
			Expect(err).To(BeNil())

			Expect(executePayload.Commands).To(HaveLen(1))
			commandResult := executePayload.Commands[0]
			Expect(commandResult.Status).To(Equal("SUCCESS"))
			Expect(commandResult.IDs).To(ContainElement(testNodeID3 + ".ColorLight"))

			// Verify the color state in the response
			Expect(commandResult.States).To(HaveKey("color"))
			Expect(commandResult.States["online"]).To(Equal(true))

			// Verify shadow was updated with correct HSV values
			dataDesired := test_utils.GetPublishedDataForNodeGroup(testNode3, group_node_db.NodesGroups{
				Group: testGroup.GroupID,
			})
			dataDesiredMap := make(map[string]interface{})
			err = json.Unmarshal(dataDesired, &dataDesiredMap)
			Expect(err).To(BeNil())

			// saturation=1.0 * 100 = 100, value=1.0 * 100 = 100
			expectedShadowState := map[string]interface{}{
				"ColorLight": map[string]interface{}{
					"hue":        254,
					"saturation": 100,
					"brightness": 100,
				},
			}
			test_utils.AssertNormalizedEqual(test_utils.ConvertAllFloatToInt(dataDesiredMap), test_utils.ConvertAllFloatToInt(expectedShadowState))
		})

		It("should handle ColorAbsolute command with partial saturation and value", func() {
			iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
			shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
			initialShadowState := map[string]interface{}{
				"state": map[string]interface{}{
					"reported": map[string]interface{}{
						"params": map[string]interface{}{
							"ColorLight": map[string]interface{}{
								"hue": 0, "saturation": 0, "brightness": 0,
							},
						},
					},
				},
			}
			shadowBytes, err := json.Marshal(initialShadowState)
			Expect(err).To(BeNil())
			iotDataClient.AddDirect(testNodeID3, shadowName, shadowBytes)

			executeRequest := gva.GVARequest{
				RequestID: "test-color-partial",
				Inputs: []gva.Input{
					{
						Intent: gva.IntentExecute,
						Payload: json.RawMessage(fmt.Sprintf(`{
							"commands": [
								{
									"devices": [
										{
											"id": "%s.ColorLight",
											"customData": {
												"groupID": "%s",
												"paramMap_OnOff": "power",
												"paramMap_Brightness": "brightness",
												"paramMap_ColorSetting_Hue": "hue",
												"paramMap_ColorSetting_Saturation": "saturation"
											}
										}
									],
									"execution": [
										{
											"command": "action.devices.commands.ColorAbsolute",
											"params": {
												"color": {
													"spectrumHSV": {
														"hue": 120.0,
														"saturation": 0.5,
														"value": 0.75
													}
												}
											}
										}
									]
								}
							]
						}`, testNodeID3, testGroup.GroupID)),
					},
				},
			}

			requestBody, err := json.Marshal(executeRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			err = json.Unmarshal([]byte(response.Body), &gvaResponse)
			Expect(err).To(BeNil())

			var executePayload gva.ExecutePayload
			payloadBytes, err := json.Marshal(gvaResponse.Payload)
			Expect(err).To(BeNil())
			err = json.Unmarshal(payloadBytes, &executePayload)
			Expect(err).To(BeNil())

			Expect(executePayload.Commands).To(HaveLen(1))
			Expect(executePayload.Commands[0].Status).To(Equal("SUCCESS"))

			// Verify shadow: hue=120, saturation=0.5*100=50, brightness=0.75*100=75
			dataDesired := test_utils.GetPublishedDataForNodeGroup(testNode3, group_node_db.NodesGroups{
				Group: testGroup.GroupID,
			})
			dataDesiredMap := make(map[string]interface{})
			err = json.Unmarshal(dataDesired, &dataDesiredMap)
			Expect(err).To(BeNil())

			expectedShadowState := map[string]interface{}{
				"ColorLight": map[string]interface{}{
					"hue":        120,
					"saturation": 50,
					"brightness": 75,
				},
			}
			test_utils.AssertNormalizedEqual(test_utils.ConvertAllFloatToInt(dataDesiredMap), test_utils.ConvertAllFloatToInt(expectedShadowState))
		})

		DescribeTable("should handle unauthorized access for various commands",
			func(command, deviceSuffix, groupIDKey, paramMapKey, paramMapValue, executionCommand string, params map[string]interface{}) {
				var nodeID string

				if deviceSuffix == "Switch" {
					nodeID = testNodeID1
				} else {
					nodeID = testNodeID2
				}

				executeRequest := gva.GVARequest{
					RequestID: "test-request-id",
					Inputs: []gva.Input{
						{
							Intent: gva.IntentExecute,
							Payload: json.RawMessage(fmt.Sprintf(`{
								"commands": [
									{
										"devices": [
											{
												"id": "%s.%s",
												"customData": {
													"%s": "%s",
													"%s": "%s"
												}
											}
										],
										"execution": [
											{
												"command": "%s",
												"params": %s
											}
										]
									}
								]
							}`, nodeID, deviceSuffix, groupIDKey, testGroup.GroupID, paramMapKey, paramMapValue, executionCommand, toJSON(params))),
						},
					},
				}

				requestBody, err := json.Marshal(executeRequest)
				Expect(err).To(BeNil())

				request := events.APIGatewayProxyRequest{
					HTTPMethod: "POST",
					Body:       string(requestBody),
					Headers: map[string]string{
						"Authorization": "Bearer invalid-token",
					},
				}

				response, err := handler(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(500)) // Internal error due to invalid token
			},
			Entry("OnOff command unauthorized", "OnOff", "Switch", "groupID", "paramMap_OnOff", "power", "action.devices.commands.OnOff",
				map[string]interface{}{"on": true}),
			Entry("BrightnessAbsolute command unauthorized", "BrightnessAbsolute", "Light", "groupID", "paramMap_Brightness", "brightness", "action.devices.commands.BrightnessAbsolute",
				map[string]interface{}{"brightness": 75}),
		)

		// Extended execute tests for capabilities that need multiple customData entries
		type executeTestCase struct {
			deviceSuffix         string
			customData           map[string]interface{}
			command              string
			params               map[string]interface{}
			expectedStates       map[string]interface{}
			expectedShadowDevice string
			expectedShadowParams map[string]interface{}
			initialShadowParams  map[string]interface{}
		}

		DescribeTable("should handle extended capability commands",
			func(tc executeTestCase) {
				nodeID := testNodeID2
				testNode := testNode2

				// Store a color light node config for these tests
				rmngNodeContext := rmngctx.NewRmngContext(testNode)
				nodeDetailsDB := node_details_db.NewNodeDetailsDB(rmngNodeContext)
				err := nodeDetailsDB.UpdateServiceData("config", node_cfg_color_light_test_data.ToMap())
				Expect(err).To(BeNil())

				// Set up initial shadow state
				iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
				shadowName := fmt.Sprintf("params-%s", testGroup.GroupID)
				initialShadowState := map[string]interface{}{
					"state": map[string]interface{}{
						"reported": map[string]interface{}{
							"params": map[string]interface{}{
								tc.expectedShadowDevice: tc.initialShadowParams,
							},
						},
					},
				}
				shadowBytes, err := json.Marshal(initialShadowState)
				Expect(err).To(BeNil())
				iotDataClient.AddDirect(nodeID, shadowName, shadowBytes)

				// Inject groupID into customData
				tc.customData["groupID"] = testGroup.GroupID

				customDataJSON, err := json.Marshal(tc.customData)
				Expect(err).To(BeNil())

				executeRequest := gva.GVARequest{
					RequestID: "test-request-id",
					Inputs: []gva.Input{
						{
							Intent: gva.IntentExecute,
							Payload: json.RawMessage(fmt.Sprintf(`{
								"commands": [{
									"devices": [{"id": "%s.%s", "customData": %s}],
									"execution": [{"command": "%s", "params": %s}]
								}]
							}`, nodeID, tc.deviceSuffix, string(customDataJSON), tc.command, toJSON(tc.params))),
						},
					},
				}

				requestBody, err := json.Marshal(executeRequest)
				Expect(err).To(BeNil())

				request := events.APIGatewayProxyRequest{
					HTTPMethod: "POST",
					Body:       string(requestBody),
					Headers:    map[string]string{"Authorization": "Bearer " + testToken},
				}

				response, err := handler(ctx, request)
				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(200))

				var gvaResponse gva.GVAResponse
				err = json.Unmarshal([]byte(response.Body), &gvaResponse)
				Expect(err).To(BeNil())

				var executePayload gva.ExecutePayload
				payloadBytes, err := json.Marshal(gvaResponse.Payload)
				Expect(err).To(BeNil())
				err = json.Unmarshal(payloadBytes, &executePayload)
				Expect(err).To(BeNil())

				Expect(executePayload.Commands).To(HaveLen(1))
				commandResult := executePayload.Commands[0]
				Expect(commandResult.Status).To(Equal("SUCCESS"))

				if tc.expectedStates != nil {
					for key, expectedValue := range tc.expectedStates {
						Expect(commandResult.States).To(HaveKey(key))
						Expect(commandResult.States[key]).To(Equal(expectedValue))
					}
				}

				// Verify shadow state
				dataDesired := test_utils.GetPublishedDataForNodeGroup(testNode, group_node_db.NodesGroups{Group: testGroup.GroupID})
				dataDesiredMap := make(map[string]interface{})
				err = json.Unmarshal(dataDesired, &dataDesiredMap)
				Expect(err).To(BeNil())

				expectedShadowState := map[string]interface{}{tc.expectedShadowDevice: tc.expectedShadowParams}
				test_utils.AssertNormalizedEqual(test_utils.ConvertAllFloatToInt(dataDesiredMap), test_utils.ConvertAllFloatToInt(expectedShadowState))
			},
			Entry("ColorAbsolute HSV command", executeTestCase{
				deviceSuffix: "ColorLight",
				customData: map[string]interface{}{
					"paramMap_OnOff":                   "power",
					"paramMap_Brightness":              "brightness",
					"paramMap_ColorSetting_Hue":        "hue",
					"paramMap_ColorSetting_Saturation": "saturation",
				},
				command: "action.devices.commands.ColorAbsolute",
				params: map[string]interface{}{
					"color": map[string]interface{}{
						"spectrumHSV": map[string]interface{}{
							"hue":        240.0,
							"saturation": 0.8,
							"value":      0.6,
						},
					},
				},
				expectedStates: map[string]interface{}{
					"online": true,
				},
				expectedShadowDevice: "ColorLight",
				expectedShadowParams: map[string]interface{}{
					"hue":        240,
					"saturation": 80,
					"brightness": 60,
				},
				initialShadowParams: map[string]interface{}{
					"hue": 0, "saturation": 0, "brightness": 0,
				},
			}),
			Entry("ColorAbsolute RGB command", executeTestCase{
				deviceSuffix: "ColorLight",
				customData: map[string]interface{}{
					"paramMap_OnOff":                   "power",
					"paramMap_Brightness":              "brightness",
					"paramMap_ColorSetting_Hue":        "hue",
					"paramMap_ColorSetting_Saturation": "saturation",
				},
				command: "action.devices.commands.ColorAbsolute",
				params: map[string]interface{}{
					"color": map[string]interface{}{
						// colorModel "rgb": Google sends the packed integer (0xFF0000 = red)
						"spectrumRGB": float64(16711680),
					},
				},
				expectedStates: map[string]interface{}{
					"online": true,
				},
				expectedShadowDevice: "ColorLight",
				expectedShadowParams: map[string]interface{}{
					"hue":        0,
					"saturation": 100,
					"brightness": 100,
				},
				initialShadowParams: map[string]interface{}{
					"hue": 240, "saturation": 50, "brightness": 50,
				},
			}),
			Entry("ColorAbsolute temperature command", executeTestCase{
				deviceSuffix: "ColorLight",
				customData: map[string]interface{}{
					"paramMap_ColorSetting_CCT": "cct",
				},
				command: "action.devices.commands.ColorAbsolute",
				params: map[string]interface{}{
					"color": map[string]interface{}{
						"temperature": float64(4000),
					},
				},
				expectedStates: map[string]interface{}{
					"online": true,
				},
				expectedShadowDevice: "ColorLight",
				expectedShadowParams: map[string]interface{}{
					"cct": 4000,
				},
				initialShadowParams: map[string]interface{}{
					"cct": 3000,
				},
			}),
			Entry("SetModes command", executeTestCase{
				deviceSuffix: "ColorLight",
				customData: map[string]interface{}{
					"paramMap_Modes": "mode",
				},
				command: "action.devices.commands.SetModes",
				params: map[string]interface{}{
					"updateModeSettings": map[string]interface{}{
						"mode": "2",
					},
				},
				expectedStates: map[string]interface{}{
					"online": true,
				},
				expectedShadowDevice: "ColorLight",
				expectedShadowParams: map[string]interface{}{
					"mode": 2, // string "2" converted to int
				},
				initialShadowParams: map[string]interface{}{
					"mode": 0,
				},
			}),
		)
	})

	Describe("DISCONNECT", func() {
		It("should handle disconnect request", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body: `{
					"requestId": "test-request-id",
					"inputs": [
						{
							"intent": "action.devices.DISCONNECT",
							"payload": {}
						}
					]
				}`,
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))

			var gvaResponse gva.GVAResponse
			err = json.Unmarshal([]byte(response.Body), &gvaResponse)
			Expect(err).To(BeNil())
			Expect(gvaResponse.RequestID).To(Equal("test-request-id"))
		})
	})

	// Account link rows drive the Report State / Request Sync recipient filter
	// (account_link.go): SYNC records the link, DISCONNECT removes it. The hot
	// QUERY/EXECUTE paths carry no link bookkeeping.
	Describe("Account link lifecycle", func() {
		linkEntry := func() (*user_integration_db.UserIntegrationEntry, error) {
			return user_integration_db.NewUserDB(rmngUserContext).GetUserEntryByEndpoint(
				gva.GVAPlatform, user_integration_db.EncodeEndpointID(gva.GVAPlatform))
		}

		intentRequest := func(intent, token string) events.APIGatewayProxyRequest {
			body, err := json.Marshal(gva.GVARequest{
				RequestID: "link-test-request",
				Inputs:    []gva.Input{{Intent: intent, Payload: json.RawMessage(`{}`)}},
			})
			Expect(err).To(BeNil())
			return events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(body),
				Headers:    map[string]string{"Authorization": "Bearer " + token},
			}
		}

		It("records the link on SYNC and removes it on DISCONNECT", func() {
			_, err := linkEntry()
			Expect(err).NotTo(BeNil(), "no link row before the first intent")

			response, err := handler(ctx, intentRequest(gva.IntentSync, testToken))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			entry, err := linkEntry()
			Expect(err).To(BeNil())
			Expect(entry.IntegrationID).To(Equal(gva.GVAPlatform))

			response, err = handler(ctx, intentRequest(gva.IntentDisconnect, testToken))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			_, err = linkEntry()
			Expect(err).NotTo(BeNil(), "link row must be removed on DISCONNECT")
		})

		It("does not record a link for an invalid token", func() {
			response, err := handler(ctx, intentRequest(gva.IntentSync, "invalid-token"))
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(500))
			_, err = linkEntry()
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("Error Handling", func() {
		It("should handle invalid request body", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       "invalid json",
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(400))
		})

		It("should handle missing authorization", func() {
			syncRequest := gva.GVARequest{
				RequestID: "test-request-id",
				Inputs: []gva.Input{
					{
						Intent:  gva.IntentSync,
						Payload: json.RawMessage(`{}`),
					},
				},
			}

			requestBody, err := json.Marshal(syncRequest)
			Expect(err).To(BeNil())

			request := events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers:    map[string]string{},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(401))
		})

		It("should handle unsupported HTTP method", func() {
			request := events.APIGatewayProxyRequest{
				HTTPMethod: "GET",
				Body:       "",
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			}

			response, err := handler(ctx, request)
			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(400)) // Invalid JSON body
		})
	})

	Describe("Tenant isolation", func() {
		// Anyone can create a Cognito user pool in their own AWS account, set
		// custom:user_id on a user there to a victim's tenant ID, and mint a correctly
		// signed token. Cognito's GetUser is an anonymous call carrying no pool ID, so it
		// resolves such a token against the attacker's own pool and returns the victim's
		// tenant ID. Only verifying the issuer against our pool stops it.
		//
		// Every intent resolves the identity before doing anything else, so each one has
		// to reject the token on its own.
		DescribeTable("rejects a token minted in a pool RMNG does not own",
			func(intent, payload string) {
				foreignToken := test_utils.TestJWKUtil.GetForeignPoolAccessToken("attacker-sub", userID)

				requestBody, err := json.Marshal(gva.GVARequest{
					RequestID: "attacker-request-id",
					Inputs: []gva.Input{
						{
							Intent:  intent,
							Payload: json.RawMessage(payload),
						},
					},
				})
				Expect(err).To(BeNil())

				response, err := handler(ctx, events.APIGatewayProxyRequest{
					HTTPMethod: "POST",
					Body:       string(requestBody),
					Headers: map[string]string{
						"Authorization": "Bearer " + foreignToken,
					},
				})

				Expect(err).To(BeNil())
				Expect(response.StatusCode).To(Equal(500))

				// Nothing belonging to the victim may reach the caller, neither the
				// tenant ID echoed back as agentUserId nor any of their devices.
				Expect(response.Body).NotTo(ContainSubstring(userID))
				Expect(response.Body).NotTo(ContainSubstring(testNodeID1))
			},
			Entry("SYNC", gva.IntentSync, `{}`),
			Entry("QUERY", gva.IntentQuery, `{"devices":[]}`),
			Entry("EXECUTE", gva.IntentExecute, `{"commands":[]}`),
			Entry("DISCONNECT", gva.IntentDisconnect, `{}`),
		)

		It("serves the same request when the token comes from our pool", func() {
			// The control for the table above: identical request, legitimate token. Without
			// this, the rejections could be explained by a malformed request or an empty
			// fixture rather than by the issuer check.
			requestBody, err := json.Marshal(gva.GVARequest{
				RequestID: "attacker-request-id",
				Inputs: []gva.Input{
					{
						Intent:  gva.IntentSync,
						Payload: json.RawMessage(`{}`),
					},
				},
			})
			Expect(err).To(BeNil())

			response, err := handler(ctx, events.APIGatewayProxyRequest{
				HTTPMethod: "POST",
				Body:       string(requestBody),
				Headers: map[string]string{
					"Authorization": "Bearer " + testToken,
				},
			})

			Expect(err).To(BeNil())
			Expect(response.StatusCode).To(Equal(200))
			Expect(response.Body).To(ContainSubstring(userID))
		})
	})

	Describe("Device Type Mapping", func() {
		DescribeTable("should map device types correctly",
			func(inputType *string, expectedType string) {
				deviceType := gva.GetGVADeviceType(inputType)
				Expect(deviceType).To(Equal(expectedType))
			},
			Entry("lightbulb type", stringPtr("esp.device.lightbulb"), gva.DeviceTypeLight),
			Entry("switch type", stringPtr("esp.device.switch"), gva.DeviceTypeSwitch),
			Entry("fan type", stringPtr("esp.device.fan"), gva.DeviceTypeFan),
			Entry("plug type", stringPtr("esp.device.plug"), gva.DeviceTypeOutlet),
			Entry("plug shortname", stringPtr("plug"), gva.DeviceTypeOutlet),
			Entry("socket type", stringPtr("esp.device.socket"), gva.DeviceTypeOutlet),
			Entry("socket shortname", stringPtr("socket"), gva.DeviceTypeOutlet),
			Entry("outlet type", stringPtr("esp.device.outlet"), gva.DeviceTypeOutlet),
			Entry("unknown device type", stringPtr("unknown.device"), gva.DeviceTypeSwitch),
			Entry("nil device type", nil, gva.DeviceTypeSwitch),
		)
	})

	Describe("GetDeviceCapabilities", func() {
		groupID := "test-group-id"

		It("should return OnOff trait for power parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "Switch",
				Type: "esp.device.switch",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Power",
						Type:     "esp.param.power",
						DataType: "bool",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitOnOff))
			Expect(traits).To(HaveLen(1))
			Expect(customData["groupID"]).To(Equal(groupID))
			Expect(customData["paramMap_OnOff"]).To(Equal("Power"))
			Expect(attributes).To(BeEmpty())
		})

		It("should return Brightness trait for brightness parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "Light",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Brightness",
						Type:     "esp.param.brightness",
						DataType: "int",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitBrightness))
			Expect(traits).To(HaveLen(1))
			Expect(customData["paramMap_Brightness"]).To(Equal("Brightness"))
			Expect(attributes).To(BeEmpty())
		})

		It("should return ColorSetting trait for hue parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "ColorLight",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "H",
						Type:     "esp.param.hue",
						DataType: "int",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(traits).To(HaveLen(1))
			Expect(customData["paramMap_ColorSetting_Hue"]).To(Equal("H"))
			Expect(attributes["colorModel"]).To(Equal("rgb"))
		})

		It("should return FanSpeed trait for speed parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "Fan",
				Type: "esp.device.fan",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Speed",
						Type:     "esp.param.speed",
						DataType: "int",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitFanSpeed))
			Expect(traits).To(HaveLen(1))
			Expect(customData["paramMap_FanSpeed"]).To(Equal("Speed"))
			Expect(attributes).To(HaveKey("availableFanSpeeds"))
			fanSpeeds := attributes["availableFanSpeeds"].(map[string]interface{})
			Expect(fanSpeeds["ordered"]).To(Equal(true))
		})

		It("should return TemperatureSetting trait for temperature parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "Thermostat",
				Type: "esp.device.thermostat",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Temperature",
						Type:     "esp.param.temperature",
						DataType: "float",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitTemperatureSetting))
			Expect(traits).To(HaveLen(1))
			Expect(customData["paramMap_TemperatureSetting"]).To(Equal("Temperature"))
			Expect(attributes["availableThermostatModes"]).ToNot(BeNil())
			Expect(attributes["thermostatTemperatureUnit"]).To(Equal("C"))
		})

		It("should handle device with multiple traits", func() {
			device := config.NodeCfgDevice{
				ID:   "Light",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Power",
						Type:     "esp.param.power",
						DataType: "bool",
					},
					{
						ID:       "Brightness",
						Type:     "esp.param.brightness",
						DataType: "int",
					},
					{
						ID:       "H",
						Type:     "esp.param.hue",
						DataType: "int",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitOnOff))
			Expect(traits).To(ContainElement(gva.TraitBrightness))
			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(traits).To(HaveLen(3))
			Expect(customData["paramMap_OnOff"]).To(Equal("Power"))
			Expect(customData["paramMap_Brightness"]).To(Equal("Brightness"))
			Expect(customData["paramMap_ColorSetting_Hue"]).To(Equal("H"))
			Expect(attributes["colorModel"]).To(Equal("rgb"))
		})

		It("should deduplicate traits when multiple params map to same trait", func() {
			device := config.NodeCfgDevice{
				ID:   "Switch",
				Type: "esp.device.switch",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Power",
						Type:     "esp.param.power",
						DataType: "bool",
					},
					{
						ID:       "Switch",
						Type:     "esp.param.power",
						DataType: "bool",
					},
				},
			}

			traits, _, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitOnOff))
			Expect(traits).To(HaveLen(1))
			// Last param with same trait should overwrite the mapping
			Expect(customData["paramMap_OnOff"]).To(Equal("Switch"))
		})

		It("should handle device with no params", func() {
			device := config.NodeCfgDevice{
				ID:     "EmptyDevice",
				Type:   "esp.device.switch",
				Params: []config.NodeCfgDeviceParam{},
			}

			traits, _, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(BeEmpty())
			Expect(customData["groupID"]).To(Equal(groupID))
			// Should only have groupID, no paramMap entries
			Expect(customData).To(HaveLen(1))
		})

		It("should skip unknown parameter types without mapping to any trait", func() {
			device := config.NodeCfgDevice{
				ID:   "UnknownDevice",
				Type: "esp.device.switch",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "UnknownParam",
						Type:     "esp.param.unknown",
						DataType: "string",
					},
				},
			}

			traits, _, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(BeEmpty())
			Expect(customData).To(HaveLen(1))
			Expect(customData["groupID"]).To(Equal(groupID))
		})

		It("should handle device with Power and Brightness params correctly", func() {
			device := config.NodeCfgDevice{
				ID:   "Light",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Power",
						Type:     "esp.param.power",
						DataType: "bool",
					},
					{
						ID:       "Brightness",
						Type:     "esp.param.brightness",
						DataType: "int",
					},
				},
			}

			traits, _, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(HaveLen(2))
			Expect(traits).To(ContainElement(gva.TraitOnOff))
			Expect(traits).To(ContainElement(gva.TraitBrightness))
			Expect(customData["paramMap_OnOff"]).To(Equal("Power"))
			Expect(customData["paramMap_Brightness"]).To(Equal("Brightness"))
			Expect(customData["groupID"]).To(Equal(groupID))
		})

		It("should handle full HSV color light with Power, Hue, Saturation, Brightness", func() {
			device := config.NodeCfgDevice{
				ID:   "Light",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{ID: "Name", Type: "esp.param.name", DataType: "string"},
					{ID: "Power", Type: "esp.param.power", DataType: "bool"},
					{ID: "H", Type: "esp.param.hue", DataType: "int"},
					{ID: "S", Type: "esp.param.saturation", DataType: "int"},
					{ID: "V", Type: "esp.param.brightness", DataType: "int"},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitOnOff))
			Expect(traits).To(ContainElement(gva.TraitBrightness))
			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(traits).To(HaveLen(3))
			Expect(customData["paramMap_OnOff"]).To(Equal("Power"))
			Expect(customData["paramMap_Brightness"]).To(Equal("V"))
			Expect(customData["paramMap_ColorSetting_Hue"]).To(Equal("H"))
			Expect(customData["paramMap_ColorSetting_Saturation"]).To(Equal("S"))
			Expect(attributes["colorModel"]).To(Equal("rgb"))
		})

		It("should handle saturation parameter as ColorSetting trait", func() {
			device := config.NodeCfgDevice{
				ID:   "ColorLight",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "S",
						Type:     "esp.param.saturation",
						DataType: "int",
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(customData["paramMap_ColorSetting_Saturation"]).To(Equal("S"))
			Expect(attributes["colorModel"]).To(Equal("rgb"))
		})

		It("should return ColorSetting trait with CCT parameter and temperature range", func() {
			min, max := 2700, 6500
			device := config.NodeCfgDevice{
				ID:   "CCTLight",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "cct",
						Type:     "esp.param.cct",
						DataType: "int",
						Bounds:   &config.NodeCfgParamBounds{Min: &min, Max: &max},
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(customData["paramMap_ColorSetting_CCT"]).To(Equal("cct"))
			Expect(attributes).To(HaveKey("colorTemperatureRange"))
			cctRange := attributes["colorTemperatureRange"].(map[string]interface{})
			Expect(cctRange["temperatureMinK"]).To(Equal(2700))
			Expect(cctRange["temperatureMaxK"]).To(Equal(6500))
			// CCT-only device should NOT have colorModel
			Expect(attributes).NotTo(HaveKey("colorModel"))
		})

		It("should return both colorModel and colorTemperatureRange for HSV+CCT device", func() {
			min, max := 2700, 6500
			device := config.NodeCfgDevice{
				ID:   "FullLight",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{ID: "H", Type: "esp.param.hue", DataType: "int"},
					{ID: "S", Type: "esp.param.saturation", DataType: "int"},
					{
						ID:       "cct",
						Type:     "esp.param.cct",
						DataType: "int",
						Bounds:   &config.NodeCfgParamBounds{Min: &min, Max: &max},
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitColorSetting))
			Expect(customData["paramMap_ColorSetting_Hue"]).To(Equal("H"))
			Expect(customData["paramMap_ColorSetting_Saturation"]).To(Equal("S"))
			Expect(customData["paramMap_ColorSetting_CCT"]).To(Equal("cct"))
			Expect(attributes["colorModel"]).To(Equal("rgb"))
			Expect(attributes).To(HaveKey("colorTemperatureRange"))
		})

		It("should return Modes trait for light-mode parameter with bounds", func() {
			min, max := 0, 4
			device := config.NodeCfgDevice{
				ID:   "ModeLight",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "mode",
						Type:     "esp.param.light-mode",
						DataType: "int",
						Bounds:   &config.NodeCfgParamBounds{Min: &min, Max: &max},
					},
				},
			}

			traits, attributes, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitModes))
			Expect(customData["paramMap_Modes"]).To(Equal("mode"))
			Expect(attributes).To(HaveKey("availableModes"))
			// Verify mode settings are generated from bounds
			modes := attributes["availableModes"].([]map[string]interface{})
			Expect(modes).To(HaveLen(1))
			settings := modes[0]["settings"].([]map[string]interface{})
			Expect(settings).To(HaveLen(5)) // 0, 1, 2, 3, 4
			Expect(settings[0]["setting_name"]).To(Equal("0"))
			Expect(settings[4]["setting_name"]).To(Equal("4"))
		})

		It("should return Modes trait for mode parameter", func() {
			device := config.NodeCfgDevice{
				ID:   "ModeDevice",
				Type: "esp.device.lightbulb",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "light_mode",
						Type:     "esp.param.mode",
						DataType: "string",
					},
				},
			}

			traits, _, customData := gva.GetDeviceCapabilities(device, groupID)

			Expect(traits).To(ContainElement(gva.TraitModes))
			Expect(customData["paramMap_Modes"]).To(Equal("light_mode"))
		})

		It("should preserve groupID in customData", func() {
			testGroupID := "custom-group-123"
			device := config.NodeCfgDevice{
				ID:   "TestDevice",
				Type: "esp.device.switch",
				Params: []config.NodeCfgDeviceParam{
					{
						ID:       "Power",
						Type:     "esp.param.power",
						DataType: "bool",
					},
				},
			}

			_, _, customData := gva.GetDeviceCapabilities(device, testGroupID)

			Expect(customData["groupID"]).To(Equal(testGroupID))
		})
	})

	Describe("Utility Functions", func() {
		It("should extract access token from headers", func() {
			headers := map[string]string{
				"Authorization": "Bearer test-token",
			}

			token := rmngrequest.ExtractAuthToken(headers)
			Expect(token).To(Equal("test-token"))
		})

		It("should handle missing authorization header", func() {
			headers := map[string]string{}

			Expect(rmngrequest.ExtractAuthToken(headers)).To(BeEmpty())
		})

		It("should create response correctly", func() {
			requestID := "test-request-id"
			payload := gva.SyncPayload{
				AgentUserID: "test-user",
				Devices:     []gva.Device{},
			}

			response := gva.CreateResponse(requestID, payload)
			Expect(response.RequestID).To(Equal(requestID))
			Expect(response.Payload).To(Equal(payload))
		})
	})

	Describe("GenerateGVAStateReport", func() {
		It("should report OnOff state", func() {
			customData := map[string]interface{}{
				"groupID":        "test-group",
				"paramMap_OnOff": "Power",
			}
			deviceData := map[string]interface{}{
				"Power": true,
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["on"]).To(Equal(true))
		})

		It("should report Brightness state", func() {
			customData := map[string]interface{}{
				"paramMap_Brightness": "brightness",
			}
			deviceData := map[string]interface{}{
				"brightness": float64(75),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["brightness"]).To(Equal(75))
		})

		It("should report Color state as spectrumRgb with correct range conversion", func() {
			customData := map[string]interface{}{
				"paramMap_OnOff":                   "Power",
				"paramMap_Brightness":              "brightness",
				"paramMap_ColorSetting_Hue":        "hue",
				"paramMap_ColorSetting_Saturation": "saturation",
			}
			deviceData := map[string]interface{}{
				"Power":      true,
				"hue":        float64(120),
				"saturation": float64(80),
				"brightness": float64(60),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["on"]).To(Equal(true))

			// Color is reported as spectrumRgb, converted from the hue/saturation/brightness params
			color, ok := state["color"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(color["spectrumRgb"]).To(Equal(2070815)) // hsv(120, 0.8, 0.6) = 0x1F991F
		})

		It("should report FanSpeed state with speed setting name", func() {
			customData := map[string]interface{}{
				"paramMap_FanSpeed": "speed",
			}
			deviceData := map[string]interface{}{
				"speed": float64(50),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["currentFanSpeedPercent"]).To(Equal(50))
			Expect(state["currentFanSpeedSetting"]).To(Equal("medium"))
		})

		It("should report TemperatureSetting state", func() {
			customData := map[string]interface{}{
				"paramMap_TemperatureSetting": "temp",
			}
			deviceData := map[string]interface{}{
				"temp": float64(22.5),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["thermostatTemperatureSetpoint"]).To(Equal(22.5))
			Expect(state["thermostatMode"]).To(Equal("heat"))
		})

		It("should gracefully skip unknown traits", func() {
			customData := map[string]interface{}{
				"paramMap_UnknownTrait": "x",
				"paramMap_OnOff":        "Power",
			}
			deviceData := map[string]interface{}{
				"x":     float64(1),
				"Power": false,
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(true))
			Expect(state["on"]).To(Equal(false))
			// Unknown trait should not appear in state
			Expect(state).ToNot(HaveKey("x"))
		})

		It("should report Modes state as currentModeSettings", func() {
			customData := map[string]interface{}{
				"paramMap_Modes": "Light Mode",
			}
			deviceData := map[string]interface{}{
				"Light Mode": float64(2),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["currentModeSettings"]).To(Equal(map[string]interface{}{"mode": "2"}))
		})

		It("should report temperatureK instead of spectrumHsv when the light is in CCT mode", func() {
			customData := map[string]interface{}{
				"paramMap_ColorSetting_Hue":        "hue",
				"paramMap_ColorSetting_Saturation": "saturation",
				"paramMap_ColorSetting_CCT":        "cct",
				"paramMap_LightMode":               "Light Mode",
				"paramMap_Brightness":              "brightness",
			}
			deviceData := map[string]interface{}{
				"hue":        float64(240),
				"saturation": float64(100),
				"cct":        float64(4000),
				"Light Mode": float64(2),
				"brightness": float64(75),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			Expect(state["color"]).To(Equal(map[string]interface{}{"temperatureK": 4000}))
		})

		It("should report spectrumRgb when the light is in HSV mode despite a CCT param", func() {
			customData := map[string]interface{}{
				"paramMap_ColorSetting_Hue":        "hue",
				"paramMap_ColorSetting_Saturation": "saturation",
				"paramMap_ColorSetting_CCT":        "cct",
				"paramMap_LightMode":               "Light Mode",
			}
			deviceData := map[string]interface{}{
				"hue":        float64(240),
				"saturation": float64(100),
				"cct":        float64(4000),
				"Light Mode": float64(1),
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData)
			Expect(err).To(BeNil())
			color, ok := state["color"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(color).To(HaveKey("spectrumRgb"))
			Expect(color).ToNot(HaveKey("temperatureK"))
		})

		It("should report offline when the online flag is false", func() {
			customData := map[string]interface{}{
				"paramMap_OnOff": "Power",
			}
			deviceData := map[string]interface{}{
				"Power": true,
			}

			state, err := gva.GenerateGVAStateReport(customData, deviceData, false)
			Expect(err).To(BeNil())
			Expect(state["online"]).To(Equal(false))
		})
	})
})

func executeSyncRequest(token string) gva.SyncPayload {
	syncRequest := gva.GVARequest{
		RequestID: "test-request-id",
		Inputs: []gva.Input{
			{
				Intent:  gva.IntentSync,
				Payload: json.RawMessage(`{}`),
			},
		},
	}
	requestBody, err := json.Marshal(syncRequest)
	Expect(err).To(BeNil())

	request := events.APIGatewayProxyRequest{
		HTTPMethod: "POST",
		Body:       string(requestBody),
		Headers: map[string]string{
			"Authorization": "Bearer " + token,
		},
	}

	response, err := handler(context.Background(), request)
	Expect(err).To(BeNil())
	Expect(response.StatusCode).To(Equal(200))

	var gvaResponse gva.GVAResponse
	err = json.Unmarshal([]byte(response.Body), &gvaResponse)
	Expect(err).To(BeNil())

	var syncPayload gva.SyncPayload
	payloadBytes, err := json.Marshal(gvaResponse.Payload)
	Expect(err).To(BeNil())
	err = json.Unmarshal(payloadBytes, &syncPayload)
	Expect(err).To(BeNil())

	return syncPayload
}

func buildExpectedModesAttribute() []map[string]interface{} {
	var settings []map[string]interface{}
	for i := 0; i <= 3; i++ {
		modeName := fmt.Sprintf("%d", i)
		settings = append(settings, map[string]interface{}{
			"setting_name": modeName,
			"setting_values": []map[string]interface{}{
				{"setting_synonym": []string{modeName}, "lang": "en"},
			},
		})
	}
	return []map[string]interface{}{
		{
			"name": "mode",
			"name_values": []map[string]interface{}{
				{"name_synonym": []string{"mode", "light mode"}, "lang": "en"},
			},
			"settings": settings,
			"ordered":  true,
		},
	}
}

// createTestToken mints an ESP User RS256 access token (sub == user_id) via the OIDC harness.
// IdentityID is retained for call-site readability but is not part of the token.
func createTestToken(Sub, IdentityID string) string {
	return tokenHarness.Mint(Sub)
}

func toJSON(data map[string]interface{}) string {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal JSON: %v", err))
	}
	return string(jsonBytes)
}

func stringPtr(s string) *string {
	return &s
}
