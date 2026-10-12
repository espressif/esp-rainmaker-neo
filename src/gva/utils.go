// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// groupIDFromCustomData returns the group the device was synced with. customData is echoed
// back by the caller, so an absent or empty groupID must be an error: treating it as
// optional skips the LoadNodePermissions call that authorizes the node.
func groupIDFromCustomData(customData map[string]interface{}) (string, error) {
	groupID, ok := customData["groupID"].(string)
	if !ok || groupID == "" {
		return "", rmerror.NewRMError(nil, "missing groupID in device custom data")
	}
	return groupID, nil
}

// parseDeviceID splits a GVA device ID into its node ID and device name: "node123.Light" -> ("node123", "Light").
func parseDeviceID(deviceID string) (nodeID, deviceName string, err error) {
	parts := strings.Split(deviceID, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid deviceID: %s", deviceID)
	}
	return parts[0], parts[1], nil
}

func GetDeviceId(nodeID, deviceName string) string {
	return fmt.Sprintf("%s.%s", nodeID, deviceName)
}

func CreateResponse(requestID string, payload interface{}) GVAResponse {
	return GVAResponse{
		RequestID: requestID,
		Payload:   payload,
	}
}

// GetGVADeviceType maps RainMaker device types to Google Assistant device types
func GetGVADeviceType(deviceType *string) string {
	if deviceType == nil {
		return DeviceTypeSwitch
	}

	switch strings.ToLower(*deviceType) {
	case "esp.device.lightbulb", "lightbulb", "light":
		return DeviceTypeLight
	case "esp.device.switch", "switch":
		return DeviceTypeSwitch
	case "esp.device.outlet", "outlet", "esp.device.plug", "plug", "esp.device.socket", "socket":
		return DeviceTypeOutlet
	case "esp.device.fan", "fan":
		return DeviceTypeFan
	case "esp.device.thermostat", "thermostat":
		return DeviceTypeThermostat
	default:
		return DeviceTypeSwitch
	}
}

// GetGVATraits maps RainMaker parameter types to Google Assistant traits
func GetGVATraits(paramType *string, deviceType *string) []string {
	var traits []string

	if paramType == nil {
		return []string{TraitOnOff}
	}

	switch strings.ToLower(*paramType) {
	case "esp.param.name", "name":
		// Name is a metadata param, not a controllable trait - skip silently
	case "esp.param.power", "power", "switch":
		traits = append(traits, TraitOnOff)
	case "esp.param.brightness", "brightness":
		traits = append(traits, TraitBrightness)
	case "esp.param.hue", "esp.param.saturation", "saturation", "color", "esp.param.cct", "cct":
		traits = append(traits, TraitColorSetting)
	case "esp.param.speed", "fanspeed":
		traits = append(traits, TraitFanSpeed)
	case "esp.param.temperature", "temperature":
		traits = append(traits, TraitTemperatureSetting)
	case "esp.param.light-mode", "esp.param.mode", "light-mode", "mode":
		traits = append(traits, TraitModes)
	default:
		rlog.Warn(context.TODO()).Str("paramType", *paramType).Msg("unsupported parameter type")
	}

	return traits
}

// GetGVAAttributes returns device attributes based on the traits
func GetGVAAttributes(traits []string, deviceType string) map[string]interface{} {
	attributes := make(map[string]interface{})

	for _, trait := range traits {
		switch trait {
		case TraitBrightness:
			// Brightness can be controlled from 1-100
			// No additional attributes needed for basic brightness
		case TraitColorSetting:
			// ColorSetting attributes (colorModel, colorTemperatureRange) are set in
			// GetDeviceCapabilities based on actual device params
		case TraitFanSpeed:
			attributes["availableFanSpeeds"] = map[string]interface{}{
				"speeds": []map[string]interface{}{
					{"speed_name": "low", "speed_values": []map[string]interface{}{{"speed_synonym": []string{"low", "slow"}, "lang": "en"}}},
					{"speed_name": "medium", "speed_values": []map[string]interface{}{{"speed_synonym": []string{"medium", "mid"}, "lang": "en"}}},
					{"speed_name": "high", "speed_values": []map[string]interface{}{{"speed_synonym": []string{"high", "fast"}, "lang": "en"}}},
				},
				"ordered": true,
			}
		case TraitTemperatureSetting:
			attributes["availableThermostatModes"] = []string{"heat", "cool", "heatcool", "auto", "off"}
			attributes["thermostatTemperatureUnit"] = "C"
		}
	}

	return attributes
}
