// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"encoding/json"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// SmartThings requires a non-blank modelName on every device; these are the
// fallbacks when a node config does not supply its own.
const (
	defaultManufacturerName = "Espressif"
	defaultModelName        = "RainMaker Node"
)

// manufacturerName resolves the brand shown in the SmartThings app. A device that
// reports its own manufacturer wins, so one firmware image can ship under several
// brands; this mirrors Alexa's handle_discover.go.
func manufacturerName(nodeCfg config.NodeCfg) string {
	if nodeCfg.Info.Manufacturer != "" {
		return nodeCfg.Info.Manufacturer
	}
	return defaultManufacturerName
}

// modelName resolves a non-blank model for manufacturerInfo. SmartThings rejects a
// device whose modelName is empty, and model is optional in a RainMaker node config,
// so fall back to the node type before the generic default.
func modelName(nodeCfg config.NodeCfg) string {
	if nodeCfg.Info.Model != "" {
		return nodeCfg.Info.Model
	}
	if nodeCfg.Info.Type != "" {
		return nodeCfg.Info.Type
	}
	return defaultModelName
}

// HandleDiscovery processes a SmartThings discoveryRequest and returns all qualifying
// devices belonging to the authenticated user that have SmartThings enabled.
func HandleDiscovery(ctx context.Context, request STRequest) (STResponse, error) {
	userID, err := GetUserIDFromToken(ctx, request.Authentication.Token)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to get user ID from token")
	}

	// Create context with user
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	// Get user's groups
	groups, err := group.ListGroupForUser(rmngCtx, "", true)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to list groups")
	}

	// Collect all qualifying devices
	var devices []STDiscoveryDevice
	for _, grp := range groups {
		for nodeID := range grp.NodeGroupEntries {
			nodeDevices := discoverDevicesFromNode(rmngCtx, nodeID, grp.GroupID)
			devices = append(devices, nodeDevices...)
		}
	}

	rlog.Trace(ctx).Int("deviceCount", len(devices)).Msg("SmartThings discovery complete")

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: InteractionDiscoveryResponse,
			RequestID:       request.Headers.RequestID,
		},
		Devices: devices,
	}, nil
}

// discoverDevicesFromNode fetches config for a node and returns SmartThings discovery
// devices for its qualifying devices. When a node has at least one qualifying device it
// auto-enables the st_en flag and pushes getSTEn to the device (mirroring Alexa's
// HandleDiscover and GVA's HandleSync) — this makes the device start emitting
// "smartthings" in its shadow notify map, which is what triggers proactive state callbacks.
func discoverDevicesFromNode(ctx *rmngctx.RmngContext, nodeID string, groupID string) []STDiscoveryDevice {
	// Fetch node config
	nodeCfg, err := getNodeConfig(ctx, nodeID)
	if err != nil {
		rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("failed to get node config, skipping node")
		return nil
	}

	// Best-effort: read reported shadow to resolve device friendly names
	var shadowParams map[string]interface{}
	err = user.LoadNodePermissions(ctx, groupID, nodeID)
	if err != nil {
		rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("could not load node permissions for name resolution")
	} else {
		n := node.NewNode(nodeID)
		shadowData, shadowErr := n.ReadFromReportedShadow(ctx)
		if shadowErr != nil {
			rlog.Debug(ctx).Err(shadowErr).Str("nodeID", nodeID).Msg("could not read shadow for name resolution")
		} else {
			shadowParams = shadowData.Params
		}
	}

	var devices []STDiscoveryDevice
	for _, device := range nodeCfg.Devices {
		// Collect param types for capability mapping
		var paramTypes []string
		for _, param := range device.Params {
			paramTypes = append(paramTypes, param.Type)
		}

		capabilities := GetSTCapabilities(paramTypes)

		// Exclude devices with only healthCheck (no supported capability params)
		if len(capabilities) <= 1 {
			continue
		}

		friendlyName := resolveSTDeviceName(device, shadowParams)

		stDevice := STDiscoveryDevice{
			ExternalDeviceID:  GetDeviceID(nodeID, device.ID),
			FriendlyName:      friendlyName,
			DeviceHandlerType: getDeviceHandlerType(capabilities),
			// SmartThings stores this and echoes it back on every commandRequest,
			// which is how a command resolves the param to publish without reading
			// the node config again.
			DeviceCookie: paramsByType(&device),
		}

		// SmartThings requires manufacturerInfo on every device, and requires its
		// modelName to be non-blank (^(?!\s*$).+). Both model and fw_version are
		// optional in a RainMaker node config, so the block is always sent and
		// modelName falls back rather than going out empty: either mistake makes
		// SmartThings reject the device with BAD-RESPONSE, which surfaces to the
		// user as an account that links but shows no devices. swVersion is
		// omitempty, so a missing fw_version simply drops that field.
		stDevice.ManufacturerInfo = &STManufacturerInfo{
			ManufacturerName: manufacturerName(nodeCfg),
			ModelName:        modelName(nodeCfg),
			SwVersion:        nodeCfg.Info.FWVersion,
		}

		devices = append(devices, stDevice)
	}

	// Mark node as SmartThings-enabled since it has discoverable devices
	if len(devices) > 0 {
		n := node.NewNode(nodeID)
		err = n.UpdateSTEnabled(ctx.Context, true)
		if err != nil {
			rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("failed to update SmartThings enabled status")
		}
		n = node.NewNode(nodeID)
		err = n.SendSTEnabled(ctx.Context)
		if err != nil {
			rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("failed to send SmartThings enabled notification")
		}
	}

	return devices
}

// getNodeConfig fetches the node configuration via ConfigService.
func getNodeConfig(ctx *rmngctx.RmngContext, nodeID string) (config.NodeCfg, error) {
	configService := config.NewConfigService()
	configData, err := configService.Get(ctx, nodeID)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to get node config")
	}

	configBytes, err := json.Marshal(configData)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to marshal config")
	}

	var nodeCfg config.NodeCfg
	if err := json.Unmarshal(configBytes, &nodeCfg); err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to unmarshal node config")
	}

	return nodeCfg, nil
}

// resolveSTDeviceName returns the esp.param.name value from shadow if available,
// otherwise falls back to the device config name.
func resolveSTDeviceName(device config.NodeCfgDevice, shadowParams map[string]interface{}) string {
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

// getDeviceHandlerType determines the SmartThings device handler type based on capabilities.
func getDeviceHandlerType(capabilities []string) string {
	hasSwitch := false
	hasSwitchLevel := false
	hasColorControl := false
	hasColorTemp := false
	hasFanSpeed := false
	hasThermostat := false

	for _, cap := range capabilities {
		switch cap {
		case CapabilitySwitch:
			hasSwitch = true
		case CapabilitySwitchLevel:
			hasSwitchLevel = true
		case CapabilityColorControl:
			hasColorControl = true
		case CapabilityColorTemperature:
			hasColorTemp = true
		case CapabilityFanSpeed:
			hasFanSpeed = true
		case CapabilityThermostatMode, CapabilityThermostatHeatingSetpoint:
			hasThermostat = true
		}
	}

	switch {
	case hasColorControl && hasColorTemp:
		return "c2c-rgbw-color-bulb"
	case hasColorControl:
		return "c2c-rgb-color-bulb"
	case hasColorTemp && hasSwitchLevel:
		return "c2c-color-temperature-bulb"
	case hasSwitchLevel:
		return "c2c-dimmer"
	case hasFanSpeed:
		return "c2c-fan"
	case hasThermostat:
		return "c2c-thermostat"
	case hasSwitch:
		return "c2c-switch"
	default:
		return "c2c-switch"
	}
}
