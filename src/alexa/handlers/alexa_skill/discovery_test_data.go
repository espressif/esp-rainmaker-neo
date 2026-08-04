// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/espressif/esp-rainmaker-neo/src/alexa"

// This file contains the smaller test data for the discovery tests. Larger test data is in the test_data/ directory.

var node_cfg_simple_switch_test_data = map[string]interface{}{
	"devices": []map[string]interface{}{
		{
			"id":      "Switch",
			"type":    "esp.device.switch",
			"primary": "power",
			"params": []map[string]interface{}{
				{
					"id":        "power",
					"data_type": "bool",
					"ui_type":   "esp.ui.toggle",
					"type":      "esp.param.power",
				},
				{
					"id":        "name",
					"data_type": "string",
					"type":      "esp.param.name",
				},
			},
		},
	},
	"info": map[string]interface{}{
		"fw_version": "1.0",
	},
}

// node_cfg_oem_switch_test_data is a node that reports its own brand and model, as firmware does
// when an OEM enables manufacturer reporting. The reported brand must win over the deployment's.
var node_cfg_oem_switch_test_data = map[string]interface{}{
	"devices": []map[string]interface{}{
		{
			"id":      "Switch",
			"type":    "esp.device.switch",
			"primary": "power",
			"params": []map[string]interface{}{
				{
					"id":        "power",
					"data_type": "bool",
					"ui_type":   "esp.ui.toggle",
					"type":      "esp.param.power",
				},
			},
		},
	},
	"info": map[string]interface{}{
		"fw_version":   "1.0",
		"model":        "ACME-SW-1",
		"manufacturer": "Acme Devices",
	},
}

var node_cfg_simple_switch_discovery_response = alexa_skill.DiscoveryPayload{
	Endpoints: []alexa_skill.DiscoveryEndpoint{
		{
			EndpointID:        alexa_skill.GetEndpointId("test-node1", "Switch"),
			ManufacturerName:  "Espressif",
			Description:       "Espressif smart home device",
			FriendlyName:      "Switch",
			DisplayCategories: []string{"SWITCH"},
			Cookie: map[string]interface{}{
				"paramMap_PowerController": "power",
			},
			Capabilities: []alexa_skill.Capabilities{
				{
					Type:       "AlexaInterface",
					Interface:  "Alexa",
					Version:    "3",
					Properties: nil,
				},
				{
					Type:      "AlexaInterface",
					Interface: "Alexa.EndpointHealth",
					Version:   "3.1",
					Properties: &alexa_skill.CapabilityProperties{
						Supported: []alexa_skill.CapabilitySupported{
							{Name: "connectivity"},
						},
						ProactivelyReported: true,
						Retrievable:         true,
					},
				},
				{
					Type:      "AlexaInterface",
					Interface: "Alexa.PowerController",
					Version:   "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported: []alexa_skill.CapabilitySupported{
							{Name: "powerState"},
						},
						ProactivelyReported: true,
						Retrievable:         true,
					},
				},
			},
			AdditionalAttributes: map[string]interface{}{
				"manufacturer":    "Espressif",
				"firmwareVersion": "1.0",
			},
		},
	},
}

var node_cfg_simple_light_test_data = map[string]interface{}{
	"devices": []map[string]interface{}{
		{
			"id":      "Light",
			"type":    "esp.device.lightbulb",
			"primary": "power",
			"params": []map[string]interface{}{
				{
					"id":        "power",
					"data_type": "bool",
					"ui_type":   "esp.ui.toggle",
					"type":      "esp.param.power",
				},
				{
					"id":        "name",
					"data_type": "string",
					"type":      "esp.param.name",
				},
				{
					"id":        "brightness",
					"data_type": "int",
					"ui_type":   "esp.ui.slider",
					"type":      "esp.param.brightness",
					"bounds":    map[string]interface{}{"min": 0, "max": 100},
				},
			},
		},
	},
	"info": map[string]interface{}{
		"fw_version": "1.0",
	},
}

var node_cfg_simple_light_discovery_response = alexa_skill.DiscoveryPayload{
	Endpoints: []alexa_skill.DiscoveryEndpoint{
		{
			EndpointID:        alexa_skill.GetEndpointId("test-node1", "Light"),
			ManufacturerName:  "Espressif",
			Description:       "Espressif smart home device",
			FriendlyName:      "Light",
			DisplayCategories: []string{"LIGHT"},
			Cookie: map[string]interface{}{
				"paramMap_PowerController":      "power",
				"paramMap_BrightnessController": "brightness",
			},
			Capabilities: []alexa_skill.Capabilities{
				{
					Type:       "AlexaInterface",
					Interface:  "Alexa",
					Version:    "3",
					Properties: nil,
				},
				{
					Type:      "AlexaInterface",
					Interface: "Alexa.EndpointHealth",
					Version:   "3.1",
					Properties: &alexa_skill.CapabilityProperties{
						Supported: []alexa_skill.CapabilitySupported{
							{Name: "connectivity"},
						},
						ProactivelyReported: true,
						Retrievable:         true,
					},
				},
				{
					Type:      "AlexaInterface",
					Interface: "Alexa.PowerController",
					Version:   "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported: []alexa_skill.CapabilitySupported{
							{Name: "powerState"},
						},
						ProactivelyReported: true,
						Retrievable:         true,
					},
				},
				{
					Type:      "AlexaInterface",
					Interface: "Alexa.BrightnessController",
					Version:   "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported: []alexa_skill.CapabilitySupported{
							{Name: "brightness"},
						},
						ProactivelyReported: true,
						Retrievable:         true,
					},
				},
			},
			AdditionalAttributes: map[string]interface{}{
				"manufacturer":    "Espressif",
				"firmwareVersion": "1.0",
			},
		},
	},
}

var node_cfg_color_light_test_data = map[string]interface{}{
	"devices": []map[string]interface{}{
		{
			"id":      "ColorLight",
			"type":    "esp.device.lightbulb",
			"primary": "power",
			"params": []map[string]interface{}{
				{"id": "name", "data_type": "string", "type": "esp.param.name"},
				{"id": "power", "data_type": "bool", "ui_type": "esp.ui.toggle", "type": "esp.param.power"},
				{"id": "brightness", "data_type": "int", "ui_type": "esp.ui.slider", "type": "esp.param.brightness", "bounds": map[string]interface{}{"min": 0, "max": 100}},
				{"id": "hue", "data_type": "int", "type": "esp.param.hue", "bounds": map[string]interface{}{"min": 0, "max": 360}},
				{"id": "saturation", "data_type": "int", "type": "esp.param.saturation", "bounds": map[string]interface{}{"min": 0, "max": 100}},
				{"id": "cct", "data_type": "int", "type": "esp.param.cct", "bounds": map[string]interface{}{"min": 2700, "max": 6500}},
				{"id": "light-mode", "data_type": "int", "type": "esp.param.light-mode"},
			},
		},
	},
	"info": map[string]interface{}{"fw_version": "1.0"},
}

// node_cfg_color_light_discovery_response reflects what the lambda emits for a fully featured light. ModeController is intentionally omitted: see the rationale in capabilities.go paramToCapability.
var node_cfg_color_light_discovery_response = alexa_skill.DiscoveryPayload{
	Endpoints: []alexa_skill.DiscoveryEndpoint{
		{
			EndpointID:        alexa_skill.GetEndpointId("test-node1", "ColorLight"),
			ManufacturerName:  "Espressif",
			Description:       "Espressif smart home device",
			FriendlyName:      "ColorLight",
			DisplayCategories: []string{"LIGHT"},
			Cookie: map[string]interface{}{
				"paramMap_PowerController":            "power",
				"paramMap_BrightnessController":       "brightness",
				"paramMap_ColorController_Hue":        "hue",
				"paramMap_ColorController_Saturation": "saturation",
				"paramMap_ColorTemperatureController": "cct",
				"paramMap_LightMode":                  "light-mode",
			},
			Capabilities: []alexa_skill.Capabilities{
				{Type: "AlexaInterface", Interface: "Alexa", Version: "3", Properties: nil},
				{
					Type: "AlexaInterface", Interface: "Alexa.EndpointHealth", Version: "3.1",
					Properties: &alexa_skill.CapabilityProperties{
						Supported:           []alexa_skill.CapabilitySupported{{Name: "connectivity"}},
						ProactivelyReported: true, Retrievable: true,
					},
				},
				{
					Type: "AlexaInterface", Interface: "Alexa.PowerController", Version: "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported:           []alexa_skill.CapabilitySupported{{Name: "powerState"}},
						ProactivelyReported: true, Retrievable: true,
					},
				},
				{
					Type: "AlexaInterface", Interface: "Alexa.BrightnessController", Version: "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported:           []alexa_skill.CapabilitySupported{{Name: "brightness"}},
						ProactivelyReported: true, Retrievable: true,
					},
				},
				{
					Type: "AlexaInterface", Interface: "Alexa.ColorController", Version: "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported:           []alexa_skill.CapabilitySupported{{Name: "color"}},
						ProactivelyReported: true, Retrievable: true,
					},
				},
				{
					Type: "AlexaInterface", Interface: "Alexa.ColorTemperatureController", Version: "3",
					Properties: &alexa_skill.CapabilityProperties{
						Supported:           []alexa_skill.CapabilitySupported{{Name: "colorTemperatureInKelvin"}},
						ProactivelyReported: true, Retrievable: true,
					},
				},
			},
			AdditionalAttributes: map[string]interface{}{"manufacturer": "Espressif", "firmwareVersion": "1.0"},
		},
	},
}

var short_test_data = []struct {
	NodeCfgTestData   map[string]interface{}
	DiscoveryResponse alexa_skill.DiscoveryPayload
}{
	{
		NodeCfgTestData:   node_cfg_simple_switch_test_data,
		DiscoveryResponse: node_cfg_simple_switch_discovery_response,
	},
	{
		NodeCfgTestData:   node_cfg_simple_light_test_data,
		DiscoveryResponse: node_cfg_simple_light_discovery_response,
	},
	{
		NodeCfgTestData:   node_cfg_color_light_test_data,
		DiscoveryResponse: node_cfg_color_light_discovery_response,
	},
}
