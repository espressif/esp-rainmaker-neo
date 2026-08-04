// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func HandleSync(ctx context.Context, request GVARequest, accessToken string) (GVAResponse, error) {
	// Add request tracking for debugging intermittent issues
	startTime := time.Now()
	rlog.Info(ctx).Str("requestID", request.RequestID).Time("startTime", startTime).Msg("=== SYNC REQUEST START ===")

	userID, err := user.GetUserIDFromToken(ctx, accessToken)
	if err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to get identity id")
	}

	rlog.Info(ctx).Str("requestID", request.RequestID).Str("userID", userID).Msg("processing SYNC for user")

	// Create context with user using identity ID
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	ensureAccountLinkRecorded(rmngCtx, accessToken)

	// Get user's groups
	groups, err := group.ListGroupForUser(rmngCtx, "", true)
	if err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to list groups")
	}

	// Collect all accessible devices
	var devices []Device
	for _, grp := range groups {
		rlog.Debug(rmngCtx).Interface("group", grp).Send()
		// Get nodes in group
		for nodeID, _ := range grp.NodeGroupEntries {
			rlog.Debug(rmngCtx).Str("nodeID", nodeID).Send()
			nodeDevices, err := createDevicesFromNode(rmngCtx, nodeID, grp.GroupID)
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Send()
				continue
			}
			rlog.Debug(rmngCtx).Interface("nodeDevices", nodeDevices).Send()
			devices = append(devices, nodeDevices...)
		}
	}

	// Debug: Log all device IDs being returned
	var deviceIDs []string
	deviceIDCounts := make(map[string]int)
	for _, device := range devices {
		deviceIDs = append(deviceIDs, device.ID+" ("+device.Name.Name+")")
		deviceIDCounts[device.ID]++
	}

	// Check for duplicates
	var duplicates []string
	for deviceID, count := range deviceIDCounts {
		if count > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%s (x%d)", deviceID, count))
		}
	}

	duration := time.Since(startTime)
	rlog.Info(rmngCtx).
		Str("requestID", request.RequestID).
		Interface("deviceIDs", deviceIDs).
		Interface("duplicates", duplicates).
		Int("totalDevices", len(devices)).
		Int("uniqueDevices", len(deviceIDCounts)).
		Dur("duration", duration).
		Msg("=== SYNC RESPONSE COMPLETE ===")

	payload := SyncPayload{
		AgentUserID: userID,
		Devices:     devices,
	}

	return CreateResponse(request.RequestID, payload), nil
}

func getNodeCfg(ctx *rmngctx.RmngContext, nodeID string) (config.NodeCfg, error) {
	rlog.Info(ctx).Str("nodeID", nodeID).Msg("fetching node config using ConfigService")

	// Use the centralized ConfigService
	configService := config.NewConfigService()
	configData, err := configService.Get(ctx, nodeID)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to get node config from service")
	}

	// Marshal and unmarshal to convert interface{} to NodeCfg struct
	configBytes, err := json.Marshal(configData)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to marshal config")
	}

	// Debug: Log raw config data
	rlog.Debug(ctx).Str("nodeID", nodeID).RawJSON("rawConfig", configBytes).Msg("raw config from ConfigService")

	nodeCfg := config.NodeCfg{}
	if err := json.Unmarshal(configBytes, &nodeCfg); err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to unmarshal node config")
	}

	return nodeCfg, nil
}

func createDevicesFromNode(ctx *rmngctx.RmngContext, nodeID string, groupID string) ([]Device, error) {
	nodeCfg, err := getNodeCfg(ctx, nodeID)
	if err != nil {
		return []Device{}, rmerror.NewRMError(err, "failed to get node config")
	}

	rlog.Info(ctx).Interface("nodeCfg", nodeCfg).Str("nodeID", nodeID).Str("groupID", groupID).Send()

	// Debug: Log device names in config to check for duplicates
	var deviceNames []string
	for _, device := range nodeCfg.Devices {
		deviceNames = append(deviceNames, device.ID)
	}
	rlog.Info(ctx).Interface("deviceNames", deviceNames).Str("nodeID", nodeID).Msg("node config device names")

	var devices []Device
	if len(nodeCfg.Devices) > 0 {
		// Mark node as GVA-enabled since it has discoverable devices
		n := node.NewNode(nodeID)
		err = n.UpdateGVAEnabled(ctx.Context, true)
		if err != nil {
			// Continue with discovery even if we fail to update the GVA enabled status
			rlog.Warn(ctx).Err(err).Msg("failed to update GVA enabled status")
		}

		// Send proactive notification to the device
		err = n.SendGVAEnabled(ctx.Context)
		if err != nil {
			// Continue with discovery even if notification fails
			rlog.Warn(ctx).Err(err).Msg("failed to send GVA enabled notification")
		}
	}

	// Best-effort: read reported shadow to resolve esp.param.name for device names
	var shadowParams map[string]interface{}
	err = user.LoadNodePermissions(ctx, groupID, nodeID)
	if err != nil {
		rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("could not load node permissions for name resolution, using config names")
	} else {
		n := node.NewNode(nodeID)
		shadowData, shadowErr := n.ReadFromReportedShadow(ctx)
		if shadowErr != nil {
			rlog.Debug(ctx).Err(shadowErr).Str("nodeID", nodeID).Msg("could not read shadow for name resolution, using config names")
		} else {
			shadowParams = shadowData.Params
		}
	}

	for i, device := range nodeCfg.Devices {
		rlog.Info(ctx).Int("deviceIndex", i).Str("deviceName", device.ID).Str("nodeID", nodeID).Msg("processing device")

		newDevice := Device{}
		newDevice.ID = GetDeviceId(nodeID, device.ID)
		newDevice.Type = GetGVADeviceType(&device.Type)
		newDevice.Name = DeviceName{
			Name: resolveDeviceName(device, shadowParams),
		}
		newDevice.WillReportState = true

		rlog.Info(ctx).Str("generatedDeviceID", newDevice.ID).Str("deviceName", device.ID).Msg("created device")

		// Set device info
		if nodeCfg.Info.FWVersion != "" {
			newDevice.DeviceInfo = &DeviceInfo{
				Manufacturer: "ESP32",
				Model:        "RainMaker Device",
				SwVersion:    nodeCfg.Info.FWVersion,
			}
		}

		// Get traits and attributes based on device parameters
		traits, attributes, customData := GetDeviceCapabilities(device, groupID)
		newDevice.Traits = traits
		newDevice.Attributes = attributes
		newDevice.CustomData = customData

		devices = append(devices, newDevice)
	}

	return devices, nil
}

func GetDeviceCapabilities(device config.NodeCfgDevice, groupID string) ([]string, map[string]interface{}, map[string]interface{}) {
	customData := map[string]interface{}{
		"groupID": groupID,
	}

	var allTraits []string
	traitParamMap := make(map[string]string)

	for _, param := range device.Params {
		traits := GetGVATraits(&param.Type, &device.Type)
		for _, trait := range traits {
			// Avoid duplicate traits
			found := false
			for _, existingTrait := range allTraits {
				if existingTrait == trait {
					found = true
					break
				}
			}
			if !found {
				allTraits = append(allTraits, trait)
			}

			// Map trait to parameter name for control operations
			switch trait {
			case TraitOnOff:
				traitParamMap["OnOff"] = param.ID
			case TraitBrightness:
				traitParamMap["Brightness"] = param.ID
			case TraitColorSetting:
				paramTypeLower := strings.ToLower(param.Type)
				if strings.Contains(paramTypeLower, "hue") {
					traitParamMap["ColorSetting_Hue"] = param.ID
				} else if strings.Contains(paramTypeLower, "saturation") {
					traitParamMap["ColorSetting_Saturation"] = param.ID
				} else if strings.Contains(paramTypeLower, "cct") {
					traitParamMap["ColorSetting_CCT"] = param.ID
				}
			case TraitModes:
				traitParamMap["Modes"] = param.ID
				// Distinct key: light-mode also gates query-time colour (see device_state.go addColorSettingState) and must not be conflated with other Modes params.
				if strings.ToLower(param.Type) == "esp.param.light-mode" || strings.ToLower(param.Type) == "light-mode" {
					traitParamMap["LightMode"] = param.ID
				}
			case TraitFanSpeed:
				traitParamMap["FanSpeed"] = param.ID
			case TraitTemperatureSetting:
				traitParamMap["TemperatureSetting"] = param.ID
			}
		}
	}

	// Add trait to parameter mapping to custom data
	for trait, paramName := range traitParamMap {
		customData["paramMap_"+trait] = paramName
	}

	// Get attributes based on traits
	attributes := GetGVAAttributes(allTraits, device.Type)

	// Build ColorSetting attributes based on actual params present
	_, hasHue := traitParamMap["ColorSetting_Hue"]
	_, hasSat := traitParamMap["ColorSetting_Saturation"]
	// "rgb" pairs with the spectrumRgb reported by QUERY/Report State.
	if hasHue || hasSat {
		attributes["colorModel"] = "rgb"
	}
	if _, hasCCT := traitParamMap["ColorSetting_CCT"]; hasCCT {
		// Find the CCT param to read its bounds for temperature range
		for _, param := range device.Params {
			paramTypeLower := strings.ToLower(param.Type)
			if paramTypeLower == "esp.param.cct" || paramTypeLower == "cct" {
				if param.Bounds != nil && param.Bounds.Min != nil && param.Bounds.Max != nil {
					attributes["colorTemperatureRange"] = map[string]interface{}{
						"temperatureMinK": *param.Bounds.Min,
						"temperatureMaxK": *param.Bounds.Max,
					}
				} else {
					// Default range for typical CCT lights
					attributes["colorTemperatureRange"] = map[string]interface{}{
						"temperatureMinK": 2700,
						"temperatureMaxK": 6500,
					}
				}
				break
			}
		}
	}

	// Build Modes attributes if Modes trait is present
	if modesParamName, hasModes := traitParamMap["Modes"]; hasModes {
		for _, param := range device.Params {
			if param.ID == modesParamName {
				availableModes := buildModesAttribute(param)
				if availableModes != nil {
					attributes["availableModes"] = availableModes
				}
				break
			}
		}
	}

	return allTraits, attributes, customData
}

const maxModeSettings = 50

func buildModesAttribute(param config.NodeCfgDeviceParam) interface{} {
	// For int-type mode params, generate mode values from bounds
	if param.DataType == "int" && param.Bounds != nil && param.Bounds.Min != nil && param.Bounds.Max != nil {
		min := *param.Bounds.Min
		max := *param.Bounds.Max

		if max-min+1 > maxModeSettings {
			rlog.Warn(nil).Int("min", min).Int("max", max).Msg("mode range too large, skipping modes attribute")
			return nil
		}

		var settings []map[string]interface{}
		for i := min; i <= max; i++ {
			modeName := fmt.Sprintf("%d", i)
			settings = append(settings, map[string]interface{}{
				"setting_name": modeName,
				"setting_values": []map[string]interface{}{
					{
						"setting_synonym": []string{modeName},
						"lang":            "en",
					},
				},
			})
		}

		return []map[string]interface{}{
			{
				"name": "mode",
				"name_values": []map[string]interface{}{
					{
						"name_synonym": []string{"mode", "light mode"},
						"lang":         "en",
					},
				},
				"settings": settings,
				"ordered":  true,
			},
		}
	}

	return nil
}

// resolveDeviceName returns the esp.param.name value from shadow if available,
// otherwise falls back to the device config name.
func resolveDeviceName(device config.NodeCfgDevice, shadowParams map[string]interface{}) string {
	if shadowParams == nil {
		return device.ID
	}
	deviceData, ok := shadowParams[device.ID]
	if !ok {
		return device.ID
	}
	deviceMap, ok := deviceData.(map[string]interface{})
	if !ok {
		return device.ID
	}
	for _, param := range device.Params {
		if param.Type == alexa_skill.RMParamName {
			if nameVal, ok := deviceMap[param.ID].(string); ok && nameVal != "" {
				return nameVal
			}
			break
		}
	}
	return device.ID
}
