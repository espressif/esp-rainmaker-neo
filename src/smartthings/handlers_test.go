// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/group_node_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ----------------------------------------------------------------------------
// Shared test config fixtures (config.NodeCfg values seeded via UpdateServiceData).
// ----------------------------------------------------------------------------

// stSwitchCfg is a switch with power + name params (qualifies: st.switch).
var stSwitchCfg = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Switch",
			Type: "esp.device.switch",
			Params: []config.NodeCfgDeviceParam{
				{ID: "power", DataType: "bool", Type: ParamTypePower},
				{ID: "name", DataType: "string", Type: "esp.param.name"},
			},
		},
	},
	Info: config.NodeCfgInfo{FWVersion: "1.0", Model: "esp.device.switch"},
}

// stLightCfg is a dimmable + CCT light (qualifies: switchLevel + colorTemperature).
var stLightCfg = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Light",
			Type: "esp.device.lightbulb",
			Params: []config.NodeCfgDeviceParam{
				{ID: "power", DataType: "bool", Type: ParamTypePower},
				{ID: "name", DataType: "string", Type: "esp.param.name"},
				{ID: "brightness", DataType: "int", Type: ParamTypeBrightness},
				{ID: "cct", DataType: "int", Type: ParamTypeCCT},
			},
		},
	},
	Info: config.NodeCfgInfo{FWVersion: "1.0", Model: "esp.device.lightbulb"},
}

// stUnsupportedCfg is a device with only an unsupported param (no qualifying capability).
var stUnsupportedCfg = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Sensor",
			Type: "esp.device.sensor",
			Params: []config.NodeCfgDeviceParam{
				{ID: "temp", DataType: "float", Type: "esp.param.temperature-sensor"},
			},
		},
	},
	Info: config.NodeCfgInfo{FWVersion: "1.0"},
}

// tokenHarness stands up the ESP User OIDC verification path so minted tokens
// pass user.GetUserIDFromToken; set up per spec in BeforeEach, closed in AfterEach.
var tokenHarness *test_utils.ESPUserTokenHarness

// stToken builds a valid access token that GetUserIDFromToken accepts (sub == userID).
func stToken(userID string) string {
	return tokenHarness.Mint(userID)
}

// seedNodeConfig stores a NodeCfg as the node's "config" service data.
func seedNodeConfig(nodeID string, cfg config.NodeCfg) {
	rmngNodeCtx := rmngctx.NewRmngContext(node.NewNode(nodeID))
	err := node_details_db.NewNodeDetailsDB(rmngNodeCtx).UpdateServiceData("config", cfg.ToMap())
	Expect(err).To(BeNil())
}

// seedReportedShadow writes a reported shadow for a node/group with the given
// device params, keeping any connectivity already set so the two seeding helpers
// can be called in either order.
func seedReportedShadow(nodeID, groupID string, params map[string]interface{}) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	shadowName := fmt.Sprintf("params-%s", groupID)

	var online *bool
	if existing, ok := iotDataClient.Shadows[nodeID][shadowName]; ok {
		var current node.IoTNodeShadow
		if json.Unmarshal(existing, &current) == nil && current.State != nil && current.State.Reported != nil {
			online = current.State.Reported.Online
		}
	}

	shadow := node.IoTNodeShadow{
		State: &node.ShadowState{
			Reported: &node.ReportedOrDesiredShadow{Params: params, Online: online},
		},
	}
	shadowJSON, _ := json.Marshal(shadow)
	iotDataClient.AddDirect(nodeID, shadowName, shadowJSON)
}

// setNodeOnline sets reported.online in the node's shadow, the connectivity
// source SmartThings reads. Any params already seeded are preserved.
func setNodeOnline(nodeID, groupID string, online bool) {
	iotDataClient := awscommon.GetIoTDataPlaneClient().(*mock.IoTDataPlaneMock)
	shadowName := fmt.Sprintf("params-%s", groupID)

	shadow := node.IoTNodeShadow{State: &node.ShadowState{Reported: &node.ReportedOrDesiredShadow{}}}
	if existing, ok := iotDataClient.Shadows[nodeID][shadowName]; ok {
		Expect(json.Unmarshal(existing, &shadow)).To(BeNil())
		if shadow.State == nil {
			shadow.State = &node.ShadowState{}
		}
		if shadow.State.Reported == nil {
			shadow.State.Reported = &node.ReportedOrDesiredShadow{}
		}
	}
	shadow.State.Reported.Online = utils.Ptr(online)

	shadowJSON, _ := json.Marshal(shadow)
	iotDataClient.AddDirect(nodeID, shadowName, shadowJSON)
}

// seedCallbackTokens writes a SmartThings endpoint row for the user into rmng-user-endpoints.
// The state-callback URL becomes the row's endpoint_id (encoded), matching HandleGrantCallbackAccess.
func seedCallbackTokens(userID, callbackURL, oauthTokenURL string, token user_integration_db.IntegrationToken) {
	rmngCtx := rmngctx.NewRmngContext(user.NewUser(userID))
	err := user_integration_db.NewUserDB(rmngCtx).RegisterClient(user_integration_db.UserIntegrationEntry{
		IntegrationID:    stPlatform,
		EndpointID:       user_integration_db.EncodeEndpointID(callbackURL),
		TokenCallbackURL: oauthTokenURL,
		IntegrationToken: &token,
	})
	Expect(err).To(BeNil())
}

// readCallbackTokens reads back the user's SmartThings endpoint rows (nil if absent).
func readCallbackTokens(userID string) []user_integration_db.UserIntegrationEntry {
	rmngCtx := rmngctx.NewRmngContext(user.NewUser(userID))
	entries, err := user_integration_db.NewUserDB(rmngCtx).GetUserEntriesByIntegration(stPlatform)
	if err != nil {
		return nil
	}
	return entries
}

// stRequest builds a base STRequest with a valid auth token for the given user.
func stRequest(userID, interactionType string) STRequest {
	return STRequest{
		Headers: STHeaders{
			Schema:          "st-schema",
			Version:         "1.0",
			InteractionType: interactionType,
			RequestID:       "test-req-id",
		},
		Authentication: STAuthentication{TokenType: "Bearer", Token: stToken(userID)},
	}
}

// findDeviceState returns the device state matching the externalDeviceId, or nil.
func findDeviceState(states []STDeviceState, externalDeviceID string) *STDeviceState {
	for i := range states {
		if states[i].ExternalDeviceID == externalDeviceID {
			return &states[i]
		}
	}
	return nil
}

// stateValue returns the value of the first state matching capability+attribute.
func stateValue(states []STState, capability, attribute string) (interface{}, bool) {
	for _, s := range states {
		if s.Capability == capability && s.Attribute == attribute {
			return s.Value, true
		}
	}
	return nil, false
}

// ----------------------------------------------------------------------------

var _ = Describe("SmartThings handlers", func() {
	const userID = "26fd9a10-ca12-402f-97dd-0e6913cc2dba"

	var (
		ctx          context.Context
		rmngUserCtx  *rmngctx.RmngContext
		testGroup    *group.Group
		switchNodeID string
		lightNodeID  string
		mockHTTP     *mock.MockHTTPClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		test_utils.TestSetup()
		tokenHarness = test_utils.SetupESPUserTokenHarness(ctx)

		_, rmngUserCtx = test_utils.SetupTestUser(ctx, userID, "test-user@example.com")

		var err error
		testGroup, err = group.CreateGroupForUser(rmngUserCtx, "Living Room")
		Expect(err).To(BeNil())

		switchNodeID = "st-switch-node"
		rmngUserCtx.SetAllow(utils.NodeAll, switchNodeID)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, switchNodeID)
		seedNodeConfig(switchNodeID, stSwitchCfg)

		lightNodeID = "st-light-node"
		rmngUserCtx.SetAllow(utils.NodeAll, lightNodeID)
		test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, lightNodeID)
		seedNodeConfig(lightNodeID, stLightCfg)

		mockHTTP = mock.NewMockHTTPClient()
		httpclient.Set(mockHTTP)
	})

	AfterEach(func() {
		tokenHarness.Close()
	})

	// ------------------------------------------------------------------
	// HandleDiscovery
	// ------------------------------------------------------------------
	Describe("HandleDiscovery", func() {
		It("returns qualifying devices with mapped capabilities and external device IDs", func() {
			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionDiscoveryResponse))

			ids := make([]string, 0, len(resp.Devices))
			for _, d := range resp.Devices {
				ids = append(ids, d.ExternalDeviceID)
			}
			Expect(ids).To(ContainElement(GetDeviceID(switchNodeID, "Switch")))
			Expect(ids).To(ContainElement(GetDeviceID(lightNodeID, "Light")))
			Expect(resp.Devices).To(HaveLen(2))

			// The light has switchLevel + colorTemperature -> color-temperature-bulb handler.
			lightDev := resp.Devices[0]
			for _, d := range resp.Devices {
				if d.ExternalDeviceID == GetDeviceID(lightNodeID, "Light") {
					lightDev = d
				}
			}
			Expect(lightDev.DeviceHandlerType).To(Equal("c2c-color-temperature-bulb"))
			Expect(lightDev.ManufacturerInfo).NotTo(BeNil())
			Expect(lightDev.ManufacturerInfo.ManufacturerName).To(Equal("Espressif"))

			// The cookie is what lets a later command skip the node-config read.
			switchDev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(switchDev).NotTo(BeNil())
			Expect(switchDev.DeviceCookie).To(HaveKeyWithValue(ParamTypePower, "power"))
		})

		It("always sends manufacturerInfo with a non-blank modelName", func() {
			// SmartThings requires manufacturerInfo on every device and rejects a blank
			// modelName, but both model and fw_version are optional in a RainMaker node
			// config. Either mistake fails the whole device with BAD-RESPONSE, which
			// presents as an account that links but shows no devices.
			noModelCfg := stSwitchCfg
			noModelCfg.Info = config.NodeCfgInfo{Type: "light"}
			seedNodeConfig(switchNodeID, noModelCfg)

			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			dev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(dev).NotTo(BeNil(), "the device must still be discovered")
			Expect(dev.ManufacturerInfo).NotTo(BeNil(), "manufacturerInfo is required by SmartThings")
			Expect(dev.ManufacturerInfo.ModelName).To(Equal("light"), "model falls back to the node type")
			Expect(dev.ManufacturerInfo.SwVersion).To(BeEmpty(), "swVersion is omitempty when fw_version is absent")
		})

		It("falls back to a default model when the node config supplies neither model nor type", func() {
			bareCfg := stSwitchCfg
			bareCfg.Info = config.NodeCfgInfo{}
			seedNodeConfig(switchNodeID, bareCfg)

			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			dev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(dev).NotTo(BeNil())
			Expect(dev.ManufacturerInfo.ModelName).ToNot(BeEmpty(),
				"a blank modelName is rejected by SmartThings for every device")
		})

		It("prefers a manufacturer the device reports for itself", func() {
			brandedCfg := stSwitchCfg
			brandedCfg.Info = config.NodeCfgInfo{Manufacturer: "Acme", Model: "ACME-1", FWVersion: "2.0"}
			seedNodeConfig(switchNodeID, brandedCfg)

			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			dev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(dev).NotTo(BeNil())
			Expect(dev.ManufacturerInfo.ManufacturerName).To(Equal("Acme"))
		})

		It("resolves friendlyName from the esp.param.name shadow value", func() {
			seedReportedShadow(switchNodeID, testGroup.GroupID, map[string]interface{}{
				"Switch": map[string]interface{}{"name": "Front Door"},
			})

			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			dev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(dev).NotTo(BeNil())
			Expect(dev.FriendlyName).To(Equal("Front Door"))
		})

		It("falls back to the device ID for friendlyName when no name in shadow", func() {
			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			dev := findDiscoveryDevice(resp.Devices, GetDeviceID(switchNodeID, "Switch"))
			Expect(dev).NotTo(BeNil())
			Expect(dev.FriendlyName).To(Equal("Switch"))
		})

		It("excludes devices that have no supported capability", func() {
			sensorNodeID := "st-sensor-node"
			rmngUserCtx.SetAllow(utils.NodeAll, sensorNodeID)
			test_utils.ManuallyAddNodeToGroup(ctx, testGroup.GroupID, sensorNodeID)
			seedNodeConfig(sensorNodeID, stUnsupportedCfg)

			resp, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			Expect(findDiscoveryDevice(resp.Devices, GetDeviceID(sensorNodeID, "Sensor"))).To(BeNil())
		})

		It("auto-enables st_en on a node with discoverable devices", func() {
			before, _ := node.NewNode(switchNodeID).GetSTEnStatus(ctx)
			Expect(before).To(BeNil())

			_, err := HandleDiscovery(ctx, stRequest(userID, InteractionDiscoveryRequest))
			Expect(err).To(BeNil())

			after, err := node.NewNode(switchNodeID).GetSTEnStatus(ctx)
			Expect(err).To(BeNil())
			Expect(after).NotTo(BeNil())
			Expect(*after).To(BeTrue())
		})

		It("returns an error for an empty token", func() {
			req := stRequest(userID, InteractionDiscoveryRequest)
			req.Authentication.Token = ""
			_, err := HandleDiscovery(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("returns an error for an invalid token", func() {
			req := stRequest(userID, InteractionDiscoveryRequest)
			req.Authentication.Token = "not-a-valid-jwt"
			_, err := HandleDiscovery(ctx, req)
			Expect(err).To(HaveOccurred())
		})
	})

	// ------------------------------------------------------------------
	// HandleStateRefresh
	// ------------------------------------------------------------------
	Describe("HandleStateRefresh", func() {
		It("maps shadow params to ST states and always includes healthCheck (online)", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, true)
			seedReportedShadow(switchNodeID, testGroup.GroupID, map[string]interface{}{
				"Switch": map[string]interface{}{"power": true},
			})

			req := stRequest(userID, InteractionStateRefreshRequest)
			req.Devices = []STCommandDevice{{ExternalDeviceID: GetDeviceID(switchNodeID, "Switch")}}

			resp, err := HandleStateRefresh(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionStateRefreshResponse))

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(BeEmpty())

			sw, ok := stateValue(ds.States, CapabilitySwitch, "switch")
			Expect(ok).To(BeTrue())
			Expect(sw).To(Equal("on"))

			health, ok := stateValue(ds.States, CapabilityHealthCheck, "healthStatus")
			Expect(ok).To(BeTrue())
			Expect(health).To(Equal("online"))
		})

		It("reports healthStatus offline when the node is not connected", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, false)
			seedReportedShadow(switchNodeID, testGroup.GroupID, map[string]interface{}{
				"Switch": map[string]interface{}{"power": false},
			})

			req := stRequest(userID, InteractionStateRefreshRequest)
			req.Devices = []STCommandDevice{{ExternalDeviceID: GetDeviceID(switchNodeID, "Switch")}}

			resp, err := HandleStateRefresh(ctx, req)
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			health, ok := stateValue(ds.States, CapabilityHealthCheck, "healthStatus")
			Expect(ok).To(BeTrue())
			Expect(health).To(Equal("offline"))
		})

		It("returns DEVICE-DELETED for an invalid externalDeviceId (no separator)", func() {
			req := stRequest(userID, InteractionStateRefreshRequest)
			req.Devices = []STCommandDevice{{ExternalDeviceID: "noseparator"}}

			resp, err := HandleStateRefresh(ctx, req)
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, "noseparator")
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceDeleted))
		})

		It("returns DEVICE-DELETED when the device is not present in the node config", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, true)
			seedReportedShadow(switchNodeID, testGroup.GroupID, map[string]interface{}{})

			extID := GetDeviceID(switchNodeID, "Nonexistent")
			req := stRequest(userID, InteractionStateRefreshRequest)
			req.Devices = []STCommandDevice{{ExternalDeviceID: extID}}

			resp, err := HandleStateRefresh(ctx, req)
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, extID)
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceDeleted))
		})
	})

	// ------------------------------------------------------------------
	// HandleCommand
	// ------------------------------------------------------------------
	Describe("HandleCommand", func() {
		// SmartThings stores the deviceCookie from discovery and sends it back with
		// every command, so the builder mirrors that: the cookie is built from the
		// same node config discovery would have read, and params are resolved from
		// it alone.
		cookieFor := func(extID string) map[string]string {
			// Malformed ids are a case under test; they simply carry no cookie.
			nodeID, deviceName, err := ParseDeviceID(extID)
			if err != nil {
				return map[string]string{}
			}
			cfg := stSwitchCfg
			if nodeID == lightNodeID {
				cfg = stLightCfg
			}
			for i := range cfg.Devices {
				if cfg.Devices[i].ID == deviceName {
					return paramsByType(&cfg.Devices[i])
				}
			}
			return map[string]string{}
		}

		buildCommandReq := func(extID string, cmd STCommand) STRequest {
			req := stRequest(userID, InteractionCommandRequest)
			req.Devices = []STCommandDevice{{
				ExternalDeviceID: extID,
				DeviceCookie:     cookieFor(extID),
				Commands:         []STCommand{cmd},
			}}
			return req
		}

		// assertPublishedParam checks the desired shadow published for the node/group
		// contains the device's param set to the expected value.
		assertPublishedParam := func(nodeID, deviceName, paramName string, expected interface{}) {
			data := test_utils.GetPublishedDataForNodeGroup(node.NewNode(nodeID), group_node_db.NodesGroups{Group: testGroup.GroupID})
			Expect(data).NotTo(BeNil())
			var published map[string]interface{}
			Expect(json.Unmarshal(data, &published)).To(BeNil())
			devMap, ok := published[deviceName].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(test_utils.ConvertAllFloatToInt(devMap[paramName])).To(Equal(expected))
		}

		DescribeTable("publishes the correct MQTT payload and reflects the commanded value",
			func(nodeIDKey, deviceName, paramName string, cmd STCommand, capability, attribute string, expectedParam, expectedState interface{}) {
				nodeID := switchNodeID
				if nodeIDKey == "light" {
					nodeID = lightNodeID
				}
				setNodeOnline(nodeID, testGroup.GroupID, true)

				resp, err := HandleCommand(ctx, buildCommandReq(GetDeviceID(nodeID, deviceName), cmd))
				Expect(err).To(BeNil())
				Expect(resp.Headers.InteractionType).To(Equal(InteractionCommandResponse))

				ds := findDeviceState(resp.DeviceState, GetDeviceID(nodeID, deviceName))
				Expect(ds).NotTo(BeNil())
				Expect(ds.DeviceError).To(BeEmpty())

				val, ok := stateValue(ds.States, capability, attribute)
				Expect(ok).To(BeTrue())
				Expect(val).To(Equal(expectedState))

				assertPublishedParam(nodeID, deviceName, paramName, expectedParam)
			},
			Entry("switch on", "switch", "Switch", "power",
				STCommand{Capability: CapabilitySwitch, Command: "on"},
				CapabilitySwitch, "switch", true, "on"),
			Entry("switch off", "switch", "Switch", "power",
				STCommand{Capability: CapabilitySwitch, Command: "off"},
				CapabilitySwitch, "switch", false, "off"),
			Entry("switchLevel setLevel", "light", "Light", "brightness",
				STCommand{Capability: CapabilitySwitchLevel, Command: "setLevel", Arguments: []interface{}{float64(75)}},
				CapabilitySwitchLevel, "level", 75, 75),
			Entry("colorTemperature setColorTemperature", "light", "Light", "cct",
				STCommand{Capability: CapabilityColorTemperature, Command: "setColorTemperature", Arguments: []interface{}{float64(4000)}},
				CapabilityColorTemperature, "colorTemperature", 4000, 4000),
		)

		It("returns DEVICE-ERROR/OFFLINE when the node is offline", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, false)

			resp, err := HandleCommand(ctx, buildCommandReq(GetDeviceID(switchNodeID, "Switch"),
				STCommand{Capability: CapabilitySwitch, Command: "on"}))
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceError))
			Expect(ds.DeviceError[0].Detail).To(Equal(ErrorOffline))
		})

		It("resolves params from the deviceCookie without reading the node config", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, true)
			// Config says this device does not exist. A command still succeeds when the
			// cookie carries the param map, which is only possible if the config was
			// not consulted.
			seedNodeConfig(switchNodeID, config.NodeCfg{Info: config.NodeCfgInfo{Model: "gone"}})

			req := stRequest(userID, InteractionCommandRequest)
			req.Devices = []STCommandDevice{{
				ExternalDeviceID: GetDeviceID(switchNodeID, "Switch"),
				DeviceCookie:     map[string]string{ParamTypePower: "power"},
				Commands:         []STCommand{{Capability: CapabilitySwitch, Command: "on"}},
			}}

			resp, err := HandleCommand(ctx, req)
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(BeEmpty())
			assertPublishedParam(switchNodeID, "Switch", "power", true)
		})

		It("returns DEVICE-ERROR when the cookie carries no param for the capability", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, true)

			req := stRequest(userID, InteractionCommandRequest)
			req.Devices = []STCommandDevice{{
				ExternalDeviceID: GetDeviceID(switchNodeID, "Switch"),
				DeviceCookie:     map[string]string{ParamTypeBrightness: "brightness"},
				Commands:         []STCommand{{Capability: CapabilitySwitch, Command: "on"}},
			}}

			resp, err := HandleCommand(ctx, req)
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceError))
		})

		It("returns DEVICE-DELETED for an invalid externalDeviceId", func() {
			resp, err := HandleCommand(ctx, buildCommandReq("noseparator",
				STCommand{Capability: CapabilitySwitch, Command: "on"}))
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, "noseparator")
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceDeleted))
		})

		It("returns DEVICE-ERROR when a command targets a capability the device lacks", func() {
			setNodeOnline(switchNodeID, testGroup.GroupID, true)
			// Switch has no brightness param; setLevel must fail.
			resp, err := HandleCommand(ctx, buildCommandReq(GetDeviceID(switchNodeID, "Switch"),
				STCommand{Capability: CapabilitySwitchLevel, Command: "setLevel", Arguments: []interface{}{float64(50)}}))
			Expect(err).To(BeNil())

			ds := findDeviceState(resp.DeviceState, GetDeviceID(switchNodeID, "Switch"))
			Expect(ds).NotTo(BeNil())
			Expect(ds.DeviceError).To(HaveLen(1))
			Expect(ds.DeviceError[0].ErrorEnum).To(Equal(ErrorDeviceError))
		})
	})

	// ------------------------------------------------------------------
	// HandleGrantCallbackAccess
	//
	// The token exchange (postAccessTokenRequest) goes through the injectable
	// notification.MakeHTTPPostRequest / httpclient.Get, so the successful
	// exchange+storage round-trip and the validation branches are both covered.
	// ------------------------------------------------------------------
	Describe("HandleGrantCallbackAccess", func() {
		BeforeEach(func() {
			awscommon.GetSSMClient().PutParameter(ctx, &ssm.PutParameterInput{
				Name:  aws.String("/rmng/smartthings/client_id"),
				Value: aws.String("test-client-id"),
			})
			awscommon.GetSSMClient().PutParameter(ctx, &ssm.PutParameterInput{
				Name:  aws.String("/rmng/smartthings/client_secret"),
				Value: aws.String("test-client-secret"),
			})
		})

		It("exchanges the code and stores the callback tokens", func() {
			mockHTTP := mock.NewMockHTTPClient()
			httpclient.Set(mockHTTP)
			tokenResp := `{"headers":{"schema":"st-schema","version":"1.0","interactionType":"accessTokenResponse","requestId":"r1"},` +
				`"callbackAuthentication":{"accessToken":"new-access-token","refreshToken":"new-refresh-token","expiresIn":86400}}`
			Expect(mockHTTP.RegisterResponse("https://st/token", "POST", 200, tokenResp)).To(BeNil())

			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.CallbackAuthentication = &STCallbackAuth{Code: "auth-code", ClientID: "st-client"}
			req.CallbackURLs = &STCallbackURLs{OAuthToken: "https://st/token", StateCallback: "https://st/cb"}

			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			// Round-trip: tokens returned by the (mocked) exchange are persisted and readable.
			entries := readCallbackTokens(userID)
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].IntegrationToken).NotTo(BeNil())
			Expect(entries[0].IntegrationToken.AccessToken).To(Equal("new-access-token"))
			Expect(entries[0].IntegrationToken.RefreshToken).To(Equal("new-refresh-token"))
			Expect(user_integration_db.DecodeEndpointID(entries[0].EndpointID)).To(Equal("https://st/cb"))
			Expect(entries[0].TokenCallbackURL).To(Equal("https://st/token"))
		})

		It("errors when callbackAuthentication is missing", func() {
			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.CallbackURLs = &STCallbackURLs{OAuthToken: "https://st/token", StateCallback: "https://st/cb"}
			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("errors when callbackUrls is missing", func() {
			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.CallbackAuthentication = &STCallbackAuth{Code: "auth-code"}
			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("errors when the authorization code is empty", func() {
			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.CallbackAuthentication = &STCallbackAuth{Code: ""}
			req.CallbackURLs = &STCallbackURLs{OAuthToken: "https://st/token", StateCallback: "https://st/cb"}
			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("errors when the oauthToken URL is missing", func() {
			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.CallbackAuthentication = &STCallbackAuth{Code: "auth-code"}
			req.CallbackURLs = &STCallbackURLs{StateCallback: "https://st/cb"}
			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("errors for an invalid user token before reaching the exchange", func() {
			req := stRequest(userID, InteractionGrantCallbackAccess)
			req.Authentication.Token = "bad-token"
			_, err := HandleGrantCallbackAccess(ctx, req)
			Expect(err).To(HaveOccurred())
		})
	})

	// ------------------------------------------------------------------
	// HandleIntegrationDeleted
	// ------------------------------------------------------------------
	Describe("HandleIntegrationDeleted", func() {
		It("removes stored callback tokens for the user", func() {
			seedCallbackTokens(userID, "https://st/cb", "https://st/token", user_integration_db.IntegrationToken{
				AccessToken:  "acc",
				RefreshToken: "ref",
				ExpiresAt:    9999999999,
			})
			entries := readCallbackTokens(userID)
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].IntegrationToken.AccessToken).To(Equal("acc"))

			resp, err := HandleIntegrationDeleted(ctx, stRequest(userID, InteractionIntegrationDeleted))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionIntegrationDeleted))

			Expect(readCallbackTokens(userID)).To(BeEmpty())
		})

		It("is a no-op (success) when no callback tokens exist", func() {
			resp, err := HandleIntegrationDeleted(ctx, stRequest(userID, InteractionIntegrationDeleted))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionIntegrationDeleted))
		})

		It("returns a clean response (no error) for an invalid token", func() {
			req := stRequest(userID, InteractionIntegrationDeleted)
			req.Authentication.Token = "bad-token"
			resp, err := HandleIntegrationDeleted(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionIntegrationDeleted))
		})
	})

	// ------------------------------------------------------------------
	// HandleInteractionResult
	// ------------------------------------------------------------------
	Describe("HandleInteractionResult", func() {
		It("handles a request carrying globalError and per-device deviceError cleanly", func() {
			req := stRequest(userID, InteractionInteractionResult)
			req.OriginatingInteractionType = InteractionCommandResponse
			req.GlobalError = &STError{ErrorEnum: ErrorDeviceError, Detail: "boom"}
			req.DeviceState = []STDeviceState{{
				ExternalDeviceID: GetDeviceID(switchNodeID, "Switch"),
				DeviceError:      []STDeviceError{{ErrorEnum: ErrorDeviceDeleted, Detail: "gone"}},
			}}

			resp, err := HandleInteractionResult(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionInteractionResult))
		})

		It("handles a request with no errors", func() {
			resp, err := HandleInteractionResult(ctx, stRequest(userID, InteractionInteractionResult))
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionInteractionResult))
		})

		It("handles the legacy nested interactionResult error shape", func() {
			req := stRequest(userID, InteractionInteractionResult)
			req.InteractionResult = &STInteractionResult{
				InteractionType: InteractionCommandResponse,
				Error:           &STError{ErrorEnum: ErrorDeviceError, Detail: "legacy"},
			}
			resp, err := HandleInteractionResult(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp.Headers.InteractionType).To(Equal(InteractionInteractionResult))
		})
	})

	// ------------------------------------------------------------------
	// STNotification
	// ------------------------------------------------------------------
	Describe("STNotification", func() {
		var stNotif *STNotification

		BeforeEach(func() {
			stNotif = NewSTNotification(ctx, "")
		})

		It("reports the correct name and type", func() {
			Expect(stNotif.GetName()).To(Equal("smartthings"))
			Expect(stNotif.GetType()).To(Equal(notification.NotificationServiceTypeUserSpecific))
		})

		It("Send (broadcast) returns an error since notifications are user-specific", func() {
			Expect(stNotif.Send(nil)).To(HaveOccurred())
		})

		Describe("Marshal", func() {
			It("produces a stateCallback envelope with mapped states including healthCheck", func() {
				online := true
				notif := &notification.Notification{
					NotificationType: notification.NotificationTypeShadowUpdate,
					ShadowUpdateData: &notification.ShadowUpdateNotification{
						NodeID: switchNodeID,
						Delta: node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{"Switch": map[string]interface{}{"power": true}},
						},
						State: node.ReportedOrDesiredShadow{
							Online: &online,
							Params: map[string]interface{}{"Switch": map[string]interface{}{"power": true}},
						},
					},
				}

				out, err := stNotif.Marshal(notif)
				Expect(err).To(BeNil())
				Expect(out).NotTo(BeNil())

				payload, ok := out.(*STStateCallbackPayload)
				Expect(ok).To(BeTrue())
				Expect(payload.Headers.Schema).To(Equal("st-schema"))
				Expect(payload.Headers.Version).To(Equal("1.0"))
				Expect(payload.Headers.InteractionType).To(Equal(InteractionStateCallback))

				ds := findDeviceState(payload.DeviceState, GetDeviceID(switchNodeID, "Switch"))
				Expect(ds).NotTo(BeNil())

				sw, ok := stateValue(ds.States, CapabilitySwitch, "switch")
				Expect(ok).To(BeTrue())
				Expect(sw).To(Equal("on"))

				health, ok := stateValue(ds.States, CapabilityHealthCheck, "healthStatus")
				Expect(ok).To(BeTrue())
				Expect(health).To(Equal("online"))
			})

			It("reports healthCheck offline when the shadow reports the node offline", func() {
				offline := false
				notif := &notification.Notification{
					NotificationType: notification.NotificationTypeShadowUpdate,
					ShadowUpdateData: &notification.ShadowUpdateNotification{
						NodeID: switchNodeID,
						Delta: node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{"Switch": map[string]interface{}{"power": false}},
						},
						State: node.ReportedOrDesiredShadow{
							Online: &offline,
							Params: map[string]interface{}{"Switch": map[string]interface{}{"power": false}},
						},
					},
				}

				out, err := stNotif.Marshal(notif)
				Expect(err).To(BeNil())
				payload := out.(*STStateCallbackPayload)
				ds := findDeviceState(payload.DeviceState, GetDeviceID(switchNodeID, "Switch"))
				Expect(ds).NotTo(BeNil())
				health, ok := stateValue(ds.States, CapabilityHealthCheck, "healthStatus")
				Expect(ok).To(BeTrue())
				Expect(health).To(Equal("offline"))
			})

			It("returns nil payload when no changed devices match the config", func() {
				online := true
				notif := &notification.Notification{
					NotificationType: notification.NotificationTypeShadowUpdate,
					ShadowUpdateData: &notification.ShadowUpdateNotification{
						NodeID: switchNodeID,
						Delta: node.ReportedOrDesiredShadow{
							Params: map[string]interface{}{"Unknown": map[string]interface{}{"x": 1}},
						},
						State: node.ReportedOrDesiredShadow{
							Online: &online,
							Params: map[string]interface{}{"Unknown": map[string]interface{}{"x": 1}},
						},
					},
				}
				out, err := stNotif.Marshal(notif)
				Expect(err).To(BeNil())
				Expect(out).To(BeNil())
			})

			It("returns an error for a non-shadow-update notification type", func() {
				notif := &notification.Notification{NotificationType: notification.NotificationTypeDirect}
				_, err := stNotif.Marshal(notif)
				Expect(err).To(HaveOccurred())
			})

			It("returns an error when shadow update data is nil", func() {
				notif := &notification.Notification{NotificationType: notification.NotificationTypeShadowUpdate}
				_, err := stNotif.Marshal(notif)
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("SendTo", func() {
			It("skips users with no stored callback tokens without erroring", func() {
				payload := &STStateCallbackPayload{
					Headers:     STHeaders{Schema: "st-schema", Version: "1.0", InteractionType: InteractionStateCallback},
					DeviceState: []STDeviceState{{ExternalDeviceID: GetDeviceID(switchNodeID, "Switch")}},
				}
				err := stNotif.SendTo(payload, []string{userID})
				Expect(err).To(BeNil())
				// No HTTP call should have been made for a user with no tokens.
				Expect(mockHTTP.Requests).To(BeEmpty())
			})

			It("returns nil (skips) when the payload is nil", func() {
				Expect(stNotif.SendTo(nil, []string{userID})).To(BeNil())
			})

			It("errors when the payload is not an *STStateCallbackPayload", func() {
				Expect(stNotif.SendTo("not-a-payload", []string{userID})).To(HaveOccurred())
			})

			It("sends the state callback via the injectable HTTP client for a user with valid tokens", func() {
				callbackURL := "https://st.example.com/callback"
				seedCallbackTokens(userID, callbackURL, "https://st.example.com/token", user_integration_db.IntegrationToken{
					AccessToken:  "valid-access",
					RefreshToken: "valid-refresh",
					ExpiresAt:    9999999999, // far future, no refresh needed
				})
				Expect(mockHTTP.RegisterResponse(callbackURL, "POST", 200, `{}`)).To(BeNil())

				payload := &STStateCallbackPayload{
					Headers: STHeaders{Schema: "st-schema", Version: "1.0", InteractionType: InteractionStateCallback},
					DeviceState: []STDeviceState{{
						ExternalDeviceID: GetDeviceID(switchNodeID, "Switch"),
						States: []STState{{
							Component: "main", Capability: CapabilityHealthCheck,
							Attribute: "healthStatus", Value: "online",
						}},
					}},
				}

				err := stNotif.SendTo(payload, []string{userID})
				Expect(err).To(BeNil())
				Expect(mockHTTP.Requests).To(HaveLen(1))
				Expect(mockHTTP.Requests[0].URL.String()).To(Equal(callbackURL))
				Expect(mockHTTP.Requests[0].Header.Get("Authorization")).To(Equal("Bearer valid-access"))
			})
		})
	})
})

// findDiscoveryDevice returns the discovery device matching the externalDeviceId, or nil.
func findDiscoveryDevice(devices []STDiscoveryDevice, externalDeviceID string) *STDiscoveryDevice {
	for i := range devices {
		if devices[i].ExternalDeviceID == externalDeviceID {
			return &devices[i]
		}
	}
	return nil
}
