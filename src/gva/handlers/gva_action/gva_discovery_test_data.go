// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
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
