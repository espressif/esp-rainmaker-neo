// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The two halves of this suite are equally load-bearing. The rejections are the point of the
// validator; the fail-open cases are what keeps it from bricking devices whose firmware reports a
// sparse config, which — since config ingest is never schema-checked — is most of them.

// richLight mirrors cli_data/node_config_va_multi.json: a colour light whose hue, saturation and
// brightness are named H, S and V. It is the motivating case — a model asked to make the light red
// will send "Hue", which is semantically right and completely wrong as a key.
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

// sparseLight is the shape most itest fixtures use: ids and semantic types, nothing else.
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

var _ = Describe("NodeCfg.ValidateParams", func() {
	Describe("rejecting what the config contradicts", func() {
		DescribeTable("refuses the write",
			func(cfg config.NodeCfg, params map[string]interface{}, kind config.ViolationKind, mentions string) {
				violations := cfg.ValidateParams(params)
				Expect(violations).To(HaveLen(1))
				Expect(violations[0].Kind).To(Equal(kind))
				Expect(config.ViolationsMessage("node-1", violations)).To(ContainSubstring(mentions))
			},
			// The hallucination the tool description was written to prevent, and could not.
			Entry("a device the node never declared", richLight(),
				map[string]interface{}{"OTA": map[string]interface{}{"Trigger": true}},
				config.ViolationUnknownDevice, "Colour Light"),
			Entry("a parameter the device never declared", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"Hue": 0}},
				config.ViolationUnknownParam, "H (int 0-360, hue)"),
			Entry("a parameter the config marks read-only", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"Temperature": 20.0}},
				config.ViolationReadOnly, "read-only"),
			Entry("a string where a bool is declared", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"Power": "on"}},
				config.ViolationWrongType, "a boolean (true/false)"),
			Entry("a fraction where a whole number is declared", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"V": 80.5}},
				config.ViolationWrongType, "a whole number"),
			// Not coerced. Firmware does not coerce either, so accepting it would publish a
			// message the device drops and report success — the failure being removed here.
			Entry("a numeric string where a number is declared", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"V": "80"}},
				config.ViolationWrongType, "a whole number"),
			Entry("a value above the declared maximum", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"V": 150.0}},
				config.ViolationOutOfBounds, "accepts int 0-100"),
			Entry("a value below the declared minimum", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{"H": -5.0}},
				config.ViolationOutOfBounds, "accepts int 0-360"),
			Entry("a device whose value is not a parameter object", richLight(),
				map[string]interface{}{"Colour Light": true},
				config.ViolationBadShape, "a parameter object"),
			// Publishes nothing yet reports success — the same silent no-op in another costume.
			Entry("a device with no parameters at all", richLight(),
				map[string]interface{}{"Colour Light": map[string]interface{}{}},
				config.ViolationBadShape, "at least one parameter"),
		)

		It("suggests the real name when the caller only got the case wrong", func() {
			violations := richLight().ValidateParams(
				map[string]interface{}{"Colour Light": map[string]interface{}{"power": true}})

			Expect(violations).To(HaveLen(1))
			Expect(violations[0].DidYouMean).To(Equal("Power"))
			Expect(config.ViolationsMessage("node-1", violations)).To(ContainSubstring(`did you mean "Power"?`))
		})

		It("reports every bad parameter, not just the first", func() {
			violations := richLight().ValidateParams(map[string]interface{}{
				"Colour Light": map[string]interface{}{"Hue": 0, "Saturation": 100, "Power": "on"},
			})
			Expect(violations).To(HaveLen(3))
		})
	})

	Describe("passing what the config permits", func() {
		It("accepts a well-formed write", func() {
			Expect(richLight().ValidateParams(map[string]interface{}{
				"Colour Light": map[string]interface{}{"Power": true, "H": 120.0, "V": 80.0},
			})).To(BeEmpty())
		})

		// JSON has one number type, so 80 and 80.0 decode to the identical float64. Judging
		// integrality on the literal rather than the value would reject every correct int write.
		It("accepts a whole-valued float for an int parameter", func() {
			Expect(richLight().ValidateParams(
				map[string]interface{}{"Colour Light": map[string]interface{}{"V": 80.0}})).To(BeEmpty())
		})

		It("accepts the bounds themselves", func() {
			Expect(richLight().ValidateParams(
				map[string]interface{}{"Colour Light": map[string]interface{}{"H": 0.0, "V": 100.0}})).To(BeEmpty())
		})

		// Reboot and factory reset live under Services, and set_params advertises them. A
		// validator that walked Devices only would refuse the reboot the tool promises.
		It("resolves a parameter declared under a service", func() {
			Expect(richLight().ValidateParams(
				map[string]interface{}{"System": map[string]interface{}{"Reboot": true}})).To(BeEmpty())
		})
	})

	Describe("failing open where the config cannot judge", func() {
		It("accepts anything when the config declares nothing", func() {
			Expect(config.NodeCfg{}.ValidateParams(
				map[string]interface{}{"Anything": map[string]interface{}{"At": "all"}})).To(BeEmpty())
		})

		// Matter nodes are hex endpoints and clusters with no device names or types, so a
		// name lookup would reject every legitimate write instead of catching a bad one.
		It("skips matter nodes entirely", func() {
			matter := config.NodeCfg{
				DataModel: "matter",
				Endpoints: map[string]config.MatterEndpoint{"0x1": {DeviceType: "0x0100"}},
			}
			Expect(matter.SkipValidation()).To(BeTrue())
			Expect(matter.ValidateParams(
				map[string]interface{}{"0x1": map[string]interface{}{"0x6": 1}})).To(BeEmpty())
		})

		It("checks the device name but not the parameters when a device declares none", func() {
			cfg := config.NodeCfg{Devices: []config.NodeCfgDevice{{ID: "Light"}}}
			Expect(cfg.ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Whatever": 1}})).To(BeEmpty())
			Expect(cfg.ValidateParams(
				map[string]interface{}{"Lamp": map[string]interface{}{"Whatever": 1}})).To(HaveLen(1))
		})

		// The shape most itest fixtures and much shipping firmware use.
		It("accepts any value when data_type is absent", func() {
			Expect(sparseLight().ValidateParams(map[string]interface{}{
				"Light": map[string]interface{}{"Power": "on", "Brightness": 9999.0},
			})).To(BeEmpty())
		})

		It("still catches an unknown parameter on a sparse config", func() {
			violations := sparseLight().ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Hue": 1}})
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].Kind).To(Equal(config.ViolationUnknownParam))
		})

		It("treats an absent properties list as writable", func() {
			Expect(sparseLight().ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Power": true}})).To(BeEmpty())
		})

		It("skips an unrecognised data_type rather than guessing", func() {
			cfg := config.NodeCfg{Devices: []config.NodeCfgDevice{{ID: "Light",
				Params: []config.NodeCfgDeviceParam{{ID: "Mode", DataType: "object"}}}}}
			Expect(cfg.ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Mode": "anything"}})).To(BeEmpty())
		})

		// Unchecked config ingest means min==max==0 reaches storage from buggy firmware.
		// Honouring it would refuse every write to a device that works perfectly well.
		It("ignores degenerate bounds", func() {
			cfg := config.NodeCfg{Devices: []config.NodeCfgDevice{{ID: "Light",
				Params: []config.NodeCfgDeviceParam{{ID: "Brightness", DataType: "int",
					Bounds: &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(0)}}}}}}
			Expect(cfg.ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Brightness": 50.0}})).To(BeEmpty())
		})

		It("leaves a null value alone", func() {
			Expect(richLight().ValidateParams(
				map[string]interface{}{"Colour Light": map[string]interface{}{"Power": nil}})).To(BeEmpty())
		})

		// esp.ui.hidden means do not render it, not do not write it — Name is the common case.
		It("never consults ui_type", func() {
			cfg := config.NodeCfg{Devices: []config.NodeCfgDevice{{ID: "Light",
				Params: []config.NodeCfgDeviceParam{{ID: "Name", DataType: "string",
					UIType: "esp.ui.hidden", Properties: []string{"read", "write"}}}}}}
			Expect(cfg.ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Name": "Lamp"}})).To(BeEmpty())
		})
	})

	Describe("the message a model is shown", func() {
		It("names the node, the alternatives and the fact that nothing changed", func() {
			message := config.ViolationsMessage("kZ8f2", richLight().ValidateParams(
				map[string]interface{}{"OTA": map[string]interface{}{"Trigger": true}}))

			Expect(message).To(ContainSubstring("kZ8f2"))
			Expect(message).To(ContainSubstring("Colour Light"))
			Expect(message).To(ContainSubstring("System"))
			Expect(message).To(ContainSubstring("Nothing was changed"))
			Expect(message).To(ContainSubstring("this device does not support it"))
		})

		It("stays within a length a model can afford to read", func() {
			params := make([]config.NodeCfgDeviceParam, 40)
			for index := range params {
				params[index] = config.NodeCfgDeviceParam{
					ID: strings.Repeat("p", 8) + string(rune('a'+index%26)), DataType: "int"}
			}
			cfg := config.NodeCfg{Devices: []config.NodeCfgDevice{{ID: "Light", Params: params}}}

			message := config.ViolationsMessage("node-1", cfg.ValidateParams(
				map[string]interface{}{"Light": map[string]interface{}{"Nope": 1}}))

			Expect(len(message)).To(BeNumerically("<=", 400))
			Expect(message).To(ContainSubstring("more"))
		})

		It("is empty when there is nothing to report", func() {
			Expect(config.ViolationsMessage("node-1", nil)).To(BeEmpty())
		})
	})

	Describe("lookup helpers", func() {
		It("finds a parameter by its semantic type, however firmware named it", func() {
			param, found := richLight().FindParamByType("Colour Light", "esp.param.hue")
			Expect(found).To(BeTrue())
			Expect(param.ID).To(Equal("H"))

			bare, found := richLight().FindParamByType("Colour Light", "brightness")
			Expect(found).To(BeTrue())
			Expect(bare.ID).To(Equal("V"))
		})

		It("puts the primary parameter first and leaves read-only ones out", func() {
			writable := richLight().WritableIDs("Colour Light")
			Expect(writable[0]).To(Equal("Power"))
			Expect(writable).NotTo(ContainElement("Temperature"))
		})

		It("summarises a device for a model with names, ranges and semantic types", func() {
			spec := richLight().ParamSpec()
			Expect(spec).To(HaveKey("Colour Light"))
			Expect(spec["Colour Light"]["H"]).To(Equal("int 0-360, hue"))
			Expect(spec["Colour Light"]["Power"]).To(Equal("bool, power"))
			Expect(spec["Colour Light"]).NotTo(HaveKey("Temperature"))
			Expect(spec["System"]["Reboot"]).To(Equal("bool, reboot"))
		})

		It("returns no spec at all when the config carries nothing to describe", func() {
			Expect(config.NodeCfg{}.ParamSpec()).To(BeNil())
		})
	})
})
