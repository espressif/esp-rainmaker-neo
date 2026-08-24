// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSTAction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SmartThings Action Suite")
}

// switchParam, brightnessParam etc. build minimal config params for fixtures.
func param(id, typ string) config.NodeCfgDeviceParam {
	return config.NodeCfgDeviceParam{ID: id, Type: typ, DataType: "int"}
}

var _ = Describe("SmartThings capability mapping (GetSTCapabilities)", func() {
	// Property 2: st.healthCheck is always present, plus exactly the capabilities for the
	// param types present. Validates Requirements 2.2-2.8.

	It("always includes st.healthCheck, even for no params", func() {
		Expect(GetSTCapabilities(nil)).To(ConsistOf(CapabilityHealthCheck))
		Expect(GetSTCapabilities([]string{})).To(ConsistOf(CapabilityHealthCheck))
	})

	DescribeTable("maps each param type to the correct capability(ies)",
		func(paramTypes []string, expected []string) {
			Expect(GetSTCapabilities(paramTypes)).To(ConsistOf(expected))
		},
		Entry("power -> switch", []string{ParamTypePower},
			[]string{CapabilitySwitch, CapabilityHealthCheck}),
		Entry("brightness -> switchLevel", []string{ParamTypeBrightness},
			[]string{CapabilitySwitchLevel, CapabilityHealthCheck}),
		Entry("cct -> colorTemperature", []string{ParamTypeCCT},
			[]string{CapabilityColorTemperature, CapabilityHealthCheck}),
		Entry("speed -> fanSpeed", []string{ParamTypeSpeed},
			[]string{CapabilityFanSpeed, CapabilityHealthCheck}),
		Entry("hue and saturation both collapse to a single colorControl",
			[]string{ParamTypeHue, ParamTypeSaturation},
			[]string{CapabilityColorControl, CapabilityHealthCheck}),
		Entry("temperature -> thermostatMode + thermostatHeatingSetpoint",
			[]string{ParamTypeTemperature},
			[]string{CapabilityThermostatMode, CapabilityThermostatHeatingSetpoint, CapabilityHealthCheck}),
		Entry("setpoint-temperature -> thermostatMode + thermostatHeatingSetpoint",
			[]string{ParamTypeSetpointTemperature},
			[]string{CapabilityThermostatMode, CapabilityThermostatHeatingSetpoint, CapabilityHealthCheck}),
		Entry("a full RGBW bulb (power+brightness+hue+cct)",
			[]string{ParamTypePower, ParamTypeBrightness, ParamTypeHue, ParamTypeCCT},
			[]string{CapabilitySwitch, CapabilitySwitchLevel, CapabilityColorControl, CapabilityColorTemperature, CapabilityHealthCheck}),
	)

	It("is case-insensitive on the param type", func() {
		Expect(GetSTCapabilities([]string{"ESP.PARAM.POWER"})).
			To(ConsistOf(CapabilitySwitch, CapabilityHealthCheck))
	})

	It("ignores unsupported param types (only healthCheck remains)", func() {
		Expect(GetSTCapabilities([]string{"esp.param.unknown", "esp.param.ota"})).
			To(ConsistOf(CapabilityHealthCheck))
	})

	It("deduplicates repeated param types", func() {
		Expect(GetSTCapabilities([]string{ParamTypePower, ParamTypePower})).
			To(ConsistOf(CapabilitySwitch, CapabilityHealthCheck))
	})
})

var _ = Describe("External device ID (GetDeviceID / ParseDeviceID)", func() {
	It("round-trips nodeID and device name", func() {
		id := GetDeviceID("node123", "Light")
		Expect(id).To(Equal("node123#Light"))

		nodeID, deviceName, err := ParseDeviceID(id)
		Expect(err).NotTo(HaveOccurred())
		Expect(nodeID).To(Equal("node123"))
		Expect(deviceName).To(Equal("Light"))
	})

	It("preserves device names containing underscores", func() {
		nodeID, deviceName, err := ParseDeviceID(GetDeviceID("node123", "Living_Room_Fan"))
		Expect(err).NotTo(HaveOccurred())
		Expect(nodeID).To(Equal("node123"))
		Expect(deviceName).To(Equal("Living_Room_Fan"))
	})

	It("round-trips a node ID containing underscores", func() {
		// The original "_" separator parsed node_multi#Colour Light back to node "node",
		// so every command and state refresh for such a node failed.
		nodeID, deviceName, err := ParseDeviceID(GetDeviceID("node_multi", "Colour Light"))
		Expect(err).NotTo(HaveOccurred())
		Expect(nodeID).To(Equal("node_multi"))
		Expect(deviceName).To(Equal("Colour Light"))
	})

	It("preserves device names containing the separator (split on first only)", func() {
		nodeID, deviceName, err := ParseDeviceID(GetDeviceID("node123", "Light #2"))
		Expect(err).NotTo(HaveOccurred())
		Expect(nodeID).To(Equal("node123"))
		Expect(deviceName).To(Equal("Light #2"))
	})

	It("rejects an ID with no separator", func() {
		_, _, err := ParseDeviceID("nodeonly")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("SmartThings device handler type (getDeviceHandlerType)", func() {
	DescribeTable("selects the handler type from the capability set",
		func(caps []string, expected string) {
			Expect(getDeviceHandlerType(caps)).To(Equal(expected))
		},
		Entry("color + colorTemp -> rgbw bulb",
			[]string{CapabilityColorControl, CapabilityColorTemperature, CapabilityHealthCheck}, "c2c-rgbw-color-bulb"),
		Entry("color only -> rgb bulb",
			[]string{CapabilityColorControl, CapabilityHealthCheck}, "c2c-rgb-color-bulb"),
		Entry("colorTemp + level -> color temperature bulb",
			[]string{CapabilityColorTemperature, CapabilitySwitchLevel, CapabilityHealthCheck}, "c2c-color-temperature-bulb"),
		Entry("level only -> dimmer",
			[]string{CapabilitySwitchLevel, CapabilityHealthCheck}, "c2c-dimmer"),
		Entry("fanSpeed -> fan",
			[]string{CapabilityFanSpeed, CapabilityHealthCheck}, "c2c-fan"),
		Entry("thermostat -> thermostat",
			[]string{CapabilityThermostatHeatingSetpoint, CapabilityHealthCheck}, "c2c-thermostat"),
		Entry("switch only -> switch",
			[]string{CapabilitySwitch, CapabilityHealthCheck}, "c2c-switch"),
		Entry("only healthCheck -> defaults to switch",
			[]string{CapabilityHealthCheck}, "c2c-switch"),
	)
})

var _ = Describe("Shadow-to-SmartThings state mapping (mapShadowToSTStates)", func() {
	// Validates the core of state refresh (Property 14) and proactive callbacks (Property 7).

	powerDevice := func() *config.NodeCfgDevice {
		return &config.NodeCfgDevice{
			ID:     "Light",
			Params: []config.NodeCfgDeviceParam{param("Power", ParamTypePower)},
		}
	}

	It("maps power true to switch=on", func() {
		states := mapShadowToSTStates(powerDevice(), map[string]interface{}{"Power": true})
		Expect(states).To(HaveLen(1))
		Expect(states[0].Capability).To(Equal(CapabilitySwitch))
		Expect(states[0].Attribute).To(Equal("switch"))
		Expect(states[0].Value).To(Equal("on"))
	})

	It("maps power false to switch=off", func() {
		states := mapShadowToSTStates(powerDevice(), map[string]interface{}{"Power": false})
		Expect(states).To(HaveLen(1))
		Expect(states[0].Value).To(Equal("off"))
	})

	It("maps brightness to an integer level with a percent unit", func() {
		dev := &config.NodeCfgDevice{
			ID:     "Light",
			Params: []config.NodeCfgDeviceParam{param("Brightness", ParamTypeBrightness)},
		}
		states := mapShadowToSTStates(dev, map[string]interface{}{"Brightness": float64(75)})
		Expect(states).To(HaveLen(1))
		Expect(states[0].Capability).To(Equal(CapabilitySwitchLevel))
		Expect(states[0].Attribute).To(Equal("level"))
		Expect(states[0].Value).To(Equal(75))
		Expect(states[0].Unit).To(Equal("%"))
	})

	It("maps multiple params on one device", func() {
		dev := &config.NodeCfgDevice{
			ID: "Bulb",
			Params: []config.NodeCfgDeviceParam{
				param("Power", ParamTypePower),
				param("CCT", ParamTypeCCT),
			},
		}
		states := mapShadowToSTStates(dev, map[string]interface{}{"Power": true, "CCT": float64(2700)})
		Expect(states).To(HaveLen(2))
		Expect(states).To(ContainElement(HaveField("Capability", CapabilitySwitch)))
		Expect(states).To(ContainElement(And(
			HaveField("Capability", CapabilityColorTemperature),
			HaveField("Value", 2700),
			HaveField("Unit", "K"),
		)))
	})

	It("skips params that are absent from the shadow", func() {
		dev := &config.NodeCfgDevice{
			ID: "Bulb",
			Params: []config.NodeCfgDeviceParam{
				param("Power", ParamTypePower),
				param("Brightness", ParamTypeBrightness),
			},
		}
		// Only Power is present in the shadow data.
		states := mapShadowToSTStates(dev, map[string]interface{}{"Power": true})
		Expect(states).To(HaveLen(1))
		Expect(states[0].Capability).To(Equal(CapabilitySwitch))
	})

	It("skips a numeric param whose shadow value is non-numeric", func() {
		dev := &config.NodeCfgDevice{
			ID:     "Light",
			Params: []config.NodeCfgDeviceParam{param("Brightness", ParamTypeBrightness)},
		}
		states := mapShadowToSTStates(dev, map[string]interface{}{"Brightness": "not-a-number"})
		Expect(states).To(BeEmpty())
	})
})

var _ = Describe("Device state marshalling for callbacks (marshalDeviceStates)", func() {
	// Property 7 / 14: every payload must include st.healthCheck with the connectivity status.

	dev := &config.NodeCfgDevice{
		ID:     "Light",
		Params: []config.NodeCfgDeviceParam{param("Power", ParamTypePower)},
	}

	It("always appends healthCheck reporting online", func() {
		states := marshalDeviceStates(dev, map[string]interface{}{"Power": true}, true)
		Expect(states).To(ContainElement(And(
			HaveField("Capability", CapabilityHealthCheck),
			HaveField("Attribute", "healthStatus"),
			HaveField("Value", "online"),
		)))
		// the mapped switch state is also present
		Expect(states).To(ContainElement(HaveField("Capability", CapabilitySwitch)))
	})

	It("reports offline healthCheck when the device is offline", func() {
		states := marshalDeviceStates(dev, map[string]interface{}{"Power": true}, false)
		Expect(states).To(ContainElement(And(
			HaveField("Capability", CapabilityHealthCheck),
			HaveField("Value", "offline"),
		)))
	})

	It("includes healthCheck even when no params changed", func() {
		states := marshalDeviceStates(dev, map[string]interface{}{}, true)
		Expect(states).To(HaveLen(1))
		Expect(states[0].Capability).To(Equal(CapabilityHealthCheck))
	})
})

var _ = Describe("Friendly-name resolution (resolveSTDeviceName)", func() {
	const nameParamType = "esp.param.name"

	deviceWithName := config.NodeCfgDevice{
		ID: "Light",
		Params: []config.NodeCfgDeviceParam{
			param("Power", ParamTypePower),
			param("Name", nameParamType),
		},
	}

	It("returns the esp.param.name value from the shadow when present", func() {
		shadow := map[string]interface{}{
			"Light": map[string]interface{}{"Name": "Kitchen Light"},
		}
		Expect(resolveSTDeviceName(deviceWithName, shadow)).To(Equal("Kitchen Light"))
	})

	It("falls back to the device ID when the shadow is nil", func() {
		Expect(resolveSTDeviceName(deviceWithName, nil)).To(Equal("Light"))
	})

	It("falls back to the device ID when the device is absent from the shadow", func() {
		Expect(resolveSTDeviceName(deviceWithName, map[string]interface{}{"Other": map[string]interface{}{}})).
			To(Equal("Light"))
	})

	It("falls back to the device ID when there is no name param", func() {
		dev := config.NodeCfgDevice{
			ID:     "Light",
			Params: []config.NodeCfgDeviceParam{param("Power", ParamTypePower)},
		}
		shadow := map[string]interface{}{"Light": map[string]interface{}{"Power": true}}
		Expect(resolveSTDeviceName(dev, shadow)).To(Equal("Light"))
	})
})
