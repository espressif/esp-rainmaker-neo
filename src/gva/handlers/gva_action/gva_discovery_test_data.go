// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/espressif/esp-rainmaker-neo/src/gva"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
)

// This file contains the smaller test data for the discovery tests. Larger test data is in the test_data/ directory.

var node_cfg_simple_switch_test_data = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Switch",
			Type: "esp.device.switch",
			Params: []config.NodeCfgDeviceParam{
				{
					ID:       "power",
					DataType: "bool",
					UIType:   "esp.ui.toggle",
					Type:     "esp.param.power",
				},
				{
					ID:       "name",
					DataType: "string",
					Type:     "esp.param.name",
				},
			},
		},
	},
	Info: config.NodeCfgInfo{
		FWVersion: "1.0",
	},
}

var node_cfg_simple_switch_discovery_response = gva.SyncPayload{
	Devices: []gva.Device{
		{
			ID:   "test-node1.Switch",
			Type: "action.devices.types.SWITCH",
			Traits: []string{
				"action.devices.traits.OnOff",
			},
			Name: gva.DeviceName{
				Name: "Switch",
			},
			WillReportState: true,
			Attributes: map[string]interface{}{
				"commandOnlyOnOff": false,
			},
			DeviceInfo: &gva.DeviceInfo{
				Manufacturer: "TEST",
				Model:        "esp.device.switch",
				HwVersion:    "1.0",
				SwVersion:    "1.0",
			},
			CustomData: map[string]interface{}{
				"paramMap": map[string]string{
					"OnOff": "power",
				},
			},
		},
	},
}

var node_cfg_simple_light_test_data = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Light",
			Type: "esp.device.lightbulb",
			Params: []config.NodeCfgDeviceParam{
				{
					ID:       "power",
					DataType: "bool",
					UIType:   "esp.ui.toggle",
					Type:     "esp.param.power",
				},
				{
					ID:       "name",
					DataType: "string",
					Type:     "esp.param.name",
				},
				{
					ID:       "brightness",
					DataType: "int",
					UIType:   "esp.ui.slider",
					Type:     "esp.param.brightness",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(100)},
				},
			},
		},
	},
	Info: config.NodeCfgInfo{
		FWVersion: "1.0",
	},
}

var node_cfg_simple_light_discovery_response = gva.SyncPayload{
	Devices: []gva.Device{
		{
			ID:   "test-node1.Light",
			Type: "action.devices.types.LIGHT",
			Traits: []string{
				"action.devices.traits.OnOff",
				"action.devices.traits.Brightness",
			},
			Name: gva.DeviceName{
				Name: "Light",
			},
			WillReportState: true,
			Attributes: map[string]interface{}{
				"commandOnlyOnOff":      false,
				"commandOnlyBrightness": false,
			},
			DeviceInfo: &gva.DeviceInfo{
				Manufacturer: "TEST",
				Model:        "esp.device.lightbulb",
				HwVersion:    "1.0",
				SwVersion:    "1.0",
			},
			CustomData: map[string]interface{}{
				"paramMap": map[string]string{
					"OnOff":      "power",
					"Brightness": "brightness",
				},
			},
		},
	},
}

var node_cfg_color_light_test_data = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "ColorLight",
			Type: "esp.device.lightbulb",
			Params: []config.NodeCfgDeviceParam{
				{
					ID:       "power",
					DataType: "bool",
					UIType:   "esp.ui.toggle",
					Type:     "esp.param.power",
				},
				{
					ID:       "name",
					DataType: "string",
					Type:     "esp.param.name",
				},
				{
					ID:       "brightness",
					DataType: "int",
					UIType:   "esp.ui.slider",
					Type:     "esp.param.brightness",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(100)},
				},
				{
					ID:       "hue",
					DataType: "int",
					UIType:   "esp.ui.hue-slider",
					Type:     "esp.param.hue",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(360)},
				},
				{
					ID:       "saturation",
					DataType: "int",
					UIType:   "esp.ui.slider",
					Type:     "esp.param.saturation",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(100)},
				},
				{
					ID:       "cct",
					DataType: "int",
					UIType:   "esp.ui.slider",
					Type:     "esp.param.cct",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(2700), Max: utils.Ptr(6500)},
				},
				{
					ID:       "mode",
					DataType: "int",
					UIType:   "esp.ui.dropdown",
					Type:     "esp.param.light-mode",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(3)},
				},
			},
		},
	},
	Info: config.NodeCfgInfo{
		FWVersion: "1.0",
	},
}

var node_cfg_color_light_discovery_response = gva.SyncPayload{
	Devices: []gva.Device{
		{
			ID:   "test-node1.ColorLight",
			Type: "action.devices.types.LIGHT",
			Traits: []string{
				"action.devices.traits.OnOff",
				"action.devices.traits.Brightness",
				"action.devices.traits.ColorSetting",
				"action.devices.traits.Modes",
			},
			Name: gva.DeviceName{
				Name: "ColorLight",
			},
			WillReportState: true,
			Attributes: map[string]interface{}{
				"commandOnlyOnOff":        false,
				"commandOnlyBrightness":   false,
				"commandOnlyColorSetting": false,
				"commandOnlyModes":        false,
				"colorModel":              "rgb",
				"colorTemperatureRange": map[string]interface{}{
					"temperatureMinK": 2700,
					"temperatureMaxK": 6500,
				},
				"availableModes": []map[string]interface{}{
					{
						"name": "mode",
						"name_values": []map[string]interface{}{
							{"name_synonym": []string{"mode", "light mode"}, "lang": "en"},
						},
						"settings": []map[string]interface{}{
							{"setting_name": "0", "setting_values": []map[string]interface{}{{"setting_synonym": []string{"0"}, "lang": "en"}}},
							{"setting_name": "1", "setting_values": []map[string]interface{}{{"setting_synonym": []string{"1"}, "lang": "en"}}},
							{"setting_name": "2", "setting_values": []map[string]interface{}{{"setting_synonym": []string{"2"}, "lang": "en"}}},
							{"setting_name": "3", "setting_values": []map[string]interface{}{{"setting_synonym": []string{"3"}, "lang": "en"}}},
						},
						"ordered": true,
					},
				},
			},
			DeviceInfo: &gva.DeviceInfo{
				Manufacturer: "TEST",
				Model:        "esp.device.lightbulb",
				HwVersion:    "1.0",
				SwVersion:    "1.0",
			},
			CustomData: map[string]interface{}{
				"paramMap": map[string]string{
					"OnOff":        "power",
					"Brightness":   "brightness",
					"ColorSetting": "hue,saturation,cct",
					"Modes":        "mode",
				},
			},
		},
	},
}

var node_cfg_fan_test_data = config.NodeCfg{
	Devices: []config.NodeCfgDevice{
		{
			ID:   "Fan",
			Type: "esp.device.fan",
			Params: []config.NodeCfgDeviceParam{
				{
					ID:       "power",
					DataType: "bool",
					UIType:   "esp.ui.toggle",
					Type:     "esp.param.power",
				},
				{
					ID:       "name",
					DataType: "string",
					Type:     "esp.param.name",
				},
				{
					ID:       "speed",
					DataType: "int",
					UIType:   "esp.ui.slider",
					Type:     "esp.param.speed",
					Bounds:   &config.NodeCfgParamBounds{Min: utils.Ptr(0), Max: utils.Ptr(5)},
				},
			},
		},
	},
	Info: config.NodeCfgInfo{
		FWVersion: "1.0",
	},
}

var node_cfg_fan_discovery_response = gva.SyncPayload{
	Devices: []gva.Device{
		{
			ID:   "test-node1.Fan",
			Type: "action.devices.types.FAN",
			Traits: []string{
				"action.devices.traits.OnOff",
				"action.devices.traits.FanSpeed",
			},
			Name: gva.DeviceName{
				Name: "Fan",
			},
			WillReportState: true,
			Attributes: map[string]interface{}{
				"commandOnlyOnOff":    false,
				"commandOnlyFanSpeed": false,
				"availableFanSpeeds": map[string]interface{}{
					"speeds": []map[string]interface{}{
						{
							"speed_name":   "0",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"off", "0"}, "lang": "en"}},
						},
						{
							"speed_name":   "1",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"low", "1"}, "lang": "en"}},
						},
						{
							"speed_name":   "2",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"medium-low", "2"}, "lang": "en"}},
						},
						{
							"speed_name":   "3",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"medium", "3"}, "lang": "en"}},
						},
						{
							"speed_name":   "4",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"medium-high", "4"}, "lang": "en"}},
						},
						{
							"speed_name":   "5",
							"speed_values": []map[string]interface{}{{"speed_synonym": []string{"high", "max", "5"}, "lang": "en"}},
						},
					},
					"ordered": true,
				},
			},
			DeviceInfo: &gva.DeviceInfo{
				Manufacturer: "TEST",
				Model:        "esp.device.fan",
				HwVersion:    "1.0",
				SwVersion:    "1.0",
			},
			CustomData: map[string]interface{}{
				"paramMap": map[string]string{
					"OnOff":    "power",
					"FanSpeed": "speed",
				},
			},
		},
	},
}

var short_test_data = []struct {
	NodeCfgTestData   config.NodeCfg
	DiscoveryResponse gva.SyncPayload
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
	{
		NodeCfgTestData:   node_cfg_fan_test_data,
		DiscoveryResponse: node_cfg_fan_discovery_response,
	},
}
