// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package params_test

import (
	"reflect"
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/node_details_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/params"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	test_utils "github.com/espressif/esp-rainmaker-neo/src/test/testutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestParams(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Params Service Suite")
}

var _ = Describe("ParamsService.Check", func() {
	const nodeID = "test-node-id"

	var (
		paramsService *params.ParamsService
		rmngCtx       *rmngctx.RmngContext
	)

	// Under the node's own context: config service data is written by the device, not by the user calling Check.
	declareConfig := func(nodeCfg map[string]interface{}) {
		nodeCtx := rmngctx.NewRmngContext(node.NewNode(nodeID))
		Expect(node_details_db.NewNodeDetailsDB(nodeCtx).UpdateServiceData("config", nodeCfg)).To(Succeed())
	}

	lightConfig := map[string]interface{}{
		"devices": []interface{}{map[string]interface{}{
			"id":      "Light",
			"type":    "esp.device.lightbulb",
			"primary": "Power",
			"params": []interface{}{
				map[string]interface{}{
					"id": "Power", "type": "esp.param.power", "data_type": "bool",
					"properties": []interface{}{"read", "write"},
				},
				map[string]interface{}{
					"id": "Brightness", "type": "esp.param.brightness", "data_type": "int",
					"properties": []interface{}{"read", "write"},
					"bounds":     map[string]interface{}{"min": 0, "max": 100},
				},
			},
		}},
		"info": map[string]interface{}{"fw_version": "1.0.0"},
	}

	BeforeEach(func() {
		test_utils.TestSetup()
		service.Initialize()
		paramsService = params.NewParamsService()

		testUser := user.NewUser("test-user-id")
		testUser.Permissions.SetAllow(utils.NodeGet.String(), nodeID)
		rmngCtx = rmngctx.NewRmngContext(testUser)
	})

	It("repairs a quoted boolean and a quoted number into the payload to publish", func() {
		declareConfig(lightConfig)

		payload, message := paramsService.Check(rmngCtx, nodeID,
			map[string]interface{}{"Light": map[string]interface{}{"Power": "true", "Brightness": "80"}})
		Expect(message).To(BeEmpty())
		Expect(payload["Light"]).To(Equal(map[string]interface{}{"Power": true, "Brightness": int64(80)}))
	})

	It("still refuses what the config contradicts, and says nothing changed", func() {
		declareConfig(lightConfig)

		payload, message := paramsService.Check(rmngCtx, nodeID,
			map[string]interface{}{"Light": map[string]interface{}{"Hue": 120}})
		Expect(message).To(ContainSubstring("Brightness"))
		Expect(message).To(ContainSubstring("Nothing was changed on node " + nodeID))
		Expect(payload).ToNot(BeNil())
	})

	It("refuses a repaired value the bounds contradict, rather than clamping it", func() {
		declareConfig(lightConfig)

		_, message := paramsService.Check(rmngCtx, nodeID,
			map[string]interface{}{"Light": map[string]interface{}{"Brightness": "150"}})
		Expect(message).To(ContainSubstring("0-100"))
	})

	It("passes a write through untouched when the node has no config", func() {
		asSent := map[string]interface{}{"Light": map[string]interface{}{"Power": "true"}}

		payload, message := paramsService.Check(rmngCtx, nodeID, asSent)
		Expect(message).To(BeEmpty())
		Expect(payload).To(Equal(asSent))
	})

	It("passes a write through untouched when the config declares no data type", func() {
		declareConfig(map[string]interface{}{
			"devices": []interface{}{map[string]interface{}{
				"id":     "Light",
				"params": []interface{}{map[string]interface{}{"id": "Power", "type": "esp.param.power"}},
			}},
			"info": map[string]interface{}{"fw_version": "1.0.0"},
		})
		asSent := map[string]interface{}{"Light": map[string]interface{}{"Power": "true"}}

		payload, message := paramsService.Check(rmngCtx, nodeID, asSent)
		Expect(message).To(BeEmpty())
		Expect(payload).To(Equal(asSent))
	})
})

func richLight() config.NodeCfg {
	return config.NodeCfg{
		Devices: []config.NodeCfgDevice{{
			ID:      "Colour Light",
			Type:    "esp.device.lightbulb",
			Primary: "Power",
			Params: []config.NodeCfgDeviceParam{
				{ID: "Name", Type: "esp.param.name", DataType: "string", Properties: []string{"read", "write"}},
				{ID: "Power", Type: "esp.param.power", DataType: "bool", Properties: []string{"read", "write"}},
				{ID: "H", Type: "esp.param.hue", DataType: "int", Properties: []string{"read", "write"},
					Bounds: &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(360), Step: utils.Ptr(1)}},
				{ID: "V", Type: "esp.param.brightness", DataType: "int", Properties: []string{"read", "write"},
					Bounds: &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(100), Step: utils.Ptr(1)}},
				{ID: "Temperature", Type: "esp.param.temperature", DataType: "float", Properties: []string{"read"}},
			},
		}},
		Services: []config.NodeCfgService{{
			ID:   "System",
			Type: "esp.service.system",
			Params: []config.NodeCfgDeviceParam{
				{ID: "Reboot", Type: "esp.param.reboot", DataType: "bool", Properties: []string{"read", "write"}},
			},
		}},
	}
}

func sparseLight() config.NodeCfg {
	return config.NodeCfg{
		Devices: []config.NodeCfgDevice{{
			ID: "Light",
			Params: []config.NodeCfgDeviceParam{
				{ID: "Power", Type: "esp.param.power"},
				{ID: "Brightness", Type: "esp.param.brightness"},
			},
		}},
	}
}

func thermostat() config.NodeCfg {
	return config.NodeCfg{
		Devices: []config.NodeCfgDevice{{
			ID:   "Thermostat",
			Type: "esp.device.thermostat",
			Params: []config.NodeCfgDeviceParam{
				{ID: "Setpoint", Type: "esp.param.setpoint", DataType: "float", Properties: []string{"read", "write"},
					Bounds: &config.NodeCfgParamBounds{Min: utils.Ptr(5), Max: utils.Ptr(35)}},
			},
		}},
	}
}

func sameMap(left, right map[string]interface{}) bool {
	return reflect.ValueOf(left).Pointer() == reflect.ValueOf(right).Pointer()
}

func lightParams(param string, value interface{}) map[string]interface{} {
	return map[string]interface{}{"Colour Light": map[string]interface{}{param: value}}
}

var _ = Describe("params.Repair", func() {
	Describe("repairing what the caller obviously meant", func() {
		DescribeTable("rewrites the value to the declared type",
			func(cfg config.NodeCfg, payload map[string]interface{}, device, param string, want interface{}) {
				repaired, changed := params.Repair(cfg, payload)
				Expect(changed).To(Equal([]string{device + "." + param}))
				Expect(repaired[device]).To(HaveKeyWithValue(param, want))
				Expect(cfg.ValidateParams(repaired)).To(BeEmpty())
			},
			Entry(`a quoted "true"`, richLight(), lightParams("Power", "true"), "Colour Light", "Power", true),
			Entry(`a quoted "false"`, richLight(), lightParams("Power", "false"), "Colour Light", "Power", false),
			Entry("a boolean quoted in capitals", richLight(), lightParams("Power", "TRUE"), "Colour Light", "Power", true),
			Entry("a boolean quoted with spaces", richLight(), lightParams("Power", " False "), "Colour Light", "Power", false),
			Entry(`"on" for a power parameter`, richLight(), lightParams("Power", "on"), "Colour Light", "Power", true),
			Entry(`"off" for a power parameter`, richLight(), lightParams("Power", "off"), "Colour Light", "Power", false),
			Entry(`"yes" for a boolean`, richLight(), lightParams("Power", "yes"), "Colour Light", "Power", true),
			Entry(`"no" for a boolean`, richLight(), lightParams("Power", "no"), "Colour Light", "Power", false),
			Entry(`a quoted "1" for a boolean`, richLight(), lightParams("Power", "1"), "Colour Light", "Power", true),
			Entry(`a quoted "0" for a boolean`, richLight(), lightParams("Power", "0"), "Colour Light", "Power", false),
			Entry("1 as a number for a boolean", richLight(), lightParams("Power", float64(1)), "Colour Light", "Power", true),
			Entry("0 as a number for a boolean", richLight(), lightParams("Power", 0), "Colour Light", "Power", false),
			Entry("a quoted whole number for an int", richLight(), lightParams("V", "50"), "Colour Light", "V", int64(50)),
			Entry("a quoted whole number written as a decimal", richLight(), lightParams("V", "50.0"), "Colour Light", "V", int64(50)),
			Entry("a quoted number for a float", thermostat(),
				map[string]interface{}{"Thermostat": map[string]interface{}{"Setpoint": "21.5"}},
				"Thermostat", "Setpoint", 21.5),
			Entry("a quoted boolean on a service parameter", richLight(),
				map[string]interface{}{"System": map[string]interface{}{"Reboot": "true"}},
				"System", "Reboot", true),
		)

		It("repairs several parameters in one call and reports them in a stable order", func() {
			payload := map[string]interface{}{"Colour Light": map[string]interface{}{"Power": "true", "V": "80", "H": "120"}}
			repaired, changed := params.Repair(richLight(), payload)
			Expect(changed).To(Equal([]string{"Colour Light.H", "Colour Light.Power", "Colour Light.V"}))
			Expect(repaired["Colour Light"]).To(Equal(map[string]interface{}{"Power": true, "V": int64(80), "H": int64(120)}))
		})

		It("leaves the caller's map untouched, because the fan-out shares it across nodes", func() {
			payload := lightParams("Power", "true")
			repaired, _ := params.Repair(richLight(), payload)
			Expect(payload["Colour Light"]).To(HaveKeyWithValue("Power", "true"))
			Expect(repaired["Colour Light"]).To(HaveKeyWithValue("Power", true))
		})

		It("carries values it did not touch into the repaired payload", func() {
			payload := map[string]interface{}{
				"Colour Light": map[string]interface{}{"Power": "true", "Name": "Desk"},
				"System":       map[string]interface{}{"Reboot": true},
			}
			repaired, changed := params.Repair(richLight(), payload)
			Expect(changed).To(Equal([]string{"Colour Light.Power"}))
			Expect(repaired["Colour Light"]).To(HaveKeyWithValue("Name", "Desk"))
			Expect(repaired["System"]).To(HaveKeyWithValue("Reboot", true))
		})
	})

	Describe("leaving alone what it cannot conclude", func() {
		DescribeTable("publishes the value as sent",
			func(cfg config.NodeCfg, payload map[string]interface{}) {
				repaired, changed := params.Repair(cfg, payload)
				Expect(changed).To(BeEmpty())
				// Identity, not equality: an untouched payload must not even be copied.
				Expect(sameMap(repaired, payload)).To(BeTrue(), "expected the caller's own map back, not a copy")
			},
			Entry("a boolean already of the right type", richLight(), lightParams("Power", true)),
			Entry("a number already of the right type", richLight(), lightParams("V", 50)),
			Entry("a string that is not a spelling of a boolean", richLight(), lightParams("Power", "red")),
			Entry("a number that is not 1 or 0 for a boolean", richLight(), lightParams("Power", 2)),
			Entry("a string that is not a number for an int", richLight(), lightParams("V", "high")),
			Entry("an empty string", richLight(), lightParams("V", "")),
			Entry("a fractional string for an int parameter", richLight(), lightParams("V", "50.5")),
			Entry("a null value", richLight(), lightParams("Power", nil)),
			Entry("a parameter the device never declared", richLight(), lightParams("Hue", "120")),
			Entry("a device the node never declared", richLight(),
				map[string]interface{}{"OTA": map[string]interface{}{"Trigger": "true"}}),
			Entry("a config that declares no data type", sparseLight(),
				map[string]interface{}{"Light": map[string]interface{}{"Power": "true"}}),
			Entry("a payload that is not the device -> param -> value shape", richLight(),
				map[string]interface{}{"Colour Light": true}),
			Entry("a config too sparse to judge", config.NodeCfg{DataModel: "matter"},
				map[string]interface{}{"Colour Light": map[string]interface{}{"Power": "true"}}),
		)

		It("repairs a value the bounds then refuse, rather than clamping it", func() {
			repaired, changed := params.Repair(richLight(), lightParams("V", "150"))
			Expect(changed).To(Equal([]string{"Colour Light.V"}))
			Expect(repaired["Colour Light"]).To(HaveKeyWithValue("V", int64(150)))

			violations := richLight().ValidateParams(repaired)
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].Kind).To(Equal(config.ViolationOutOfBounds))
		})

		It("does not make a read-only parameter writable", func() {
			repaired, _ := params.Repair(richLight(), lightParams("Temperature", "20.5"))
			violations := richLight().ValidateParams(repaired)
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].Kind).To(Equal(config.ViolationReadOnly))
		})
	})
})
