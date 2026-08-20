// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// HandleStateRefresh processes a SmartThings stateRefreshRequest and returns the current
// state of the requested devices including st.healthCheck capability.
func HandleStateRefresh(ctx context.Context, request STRequest) (STResponse, error) {
	userID, err := GetUserIDFromToken(ctx, request.Authentication.Token)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to get user ID from token")
	}

	// Scoped to the caller, not a system actor: externalDeviceId comes from the request,
	// so without this any linked account could read any node's state in the deployment.
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	nodeGroups, err := UserNodeGroups(rmngCtx)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to resolve caller's nodes")
	}

	var deviceStates []STDeviceState

	for _, deviceRef := range request.Devices {
		deviceState := getDeviceState(rmngCtx, deviceRef.ExternalDeviceID, nodeGroups)
		deviceStates = append(deviceStates, deviceState)
	}

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: InteractionStateRefreshResponse,
			RequestID:       request.Headers.RequestID,
		},
		DeviceState: deviceStates,
	}, nil
}

// getDeviceState reads the IoT shadow and Nodes_Online table for a single device,
// returning its SmartThings capability states.
func getDeviceState(rmngCtx *rmngctx.RmngContext, externalDeviceID string, nodeGroups map[string]string) STDeviceState {
	nodeID, deviceName, err := ParseDeviceID(externalDeviceID)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("externalDeviceId", externalDeviceID).Msg("invalid device ID")
		return STDeviceState{
			ExternalDeviceID: externalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceDeleted,
				Detail:    "invalid device ID format",
			}},
		}
	}

	// A node the caller cannot reach is reported as deleted rather than unauthorized, so
	// the response does not confirm that someone else's node exists.
	if err := AuthorizeNode(rmngCtx, nodeGroups, nodeID); err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Msg("caller not authorized for node in state refresh")
		return STDeviceState{
			ExternalDeviceID: externalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceDeleted,
				Detail:    "device not found",
			}},
		}
	}

	// Read the device shadow. One read supplies both the params and the node's
	// reachability (reported.online), the platform's connectivity source of truth
	// — the same one Alexa, GVA and our own state callbacks use. The nodes_online
	// table is a session record for the presence handler, not a live status flag:
	// nothing ever writes a disconnected state into it.
	n := node.NewNode(nodeID)
	shadow, err := n.ReadFromReportedShadow(rmngCtx)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Str("device", deviceName).Msg("failed to read device shadow")
		return STDeviceState{
			ExternalDeviceID: externalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceError,
				Detail:    "failed to read device state",
			}},
		}
	}

	deviceDataMap := node.DeviceParamsFromShadow(shadow, deviceName)

	// Get node config to determine param types for capability mapping
	nodeCfg, err := getNodeConfig(rmngCtx, nodeID)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Msg("failed to get node config for state refresh")
		return STDeviceState{
			ExternalDeviceID: externalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceError,
				Detail:    "failed to read device configuration",
			}},
		}
	}

	// Find the device in the config
	var deviceCfg *config.NodeCfgDevice
	for i := range nodeCfg.Devices {
		if nodeCfg.Devices[i].ID == deviceName {
			deviceCfg = &nodeCfg.Devices[i]
			break
		}
	}

	if deviceCfg == nil {
		rlog.Warn(rmngCtx).Str("nodeID", nodeID).Str("device", deviceName).Msg("device not found in node config")
		return STDeviceState{
			ExternalDeviceID: externalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceDeleted,
				Detail:    "device not found in configuration",
			}},
		}
	}

	// Map shadow state to SmartThings capability attributes
	states := mapShadowToSTStates(deviceCfg, deviceDataMap)

	healthStatus := "online"
	if !node.ShadowOnline(shadow) {
		healthStatus = "offline"
	}
	states = append(states, STState{
		Component:  ComponentMain,
		Capability: CapabilityHealthCheck,
		Attribute:  AttributeHealthStatus,
		Value:      healthStatus,
	})

	return STDeviceState{
		ExternalDeviceID: externalDeviceID,
		States:           states,
	}
}

// mapShadowToSTStates maps device shadow parameters to SmartThings capability attribute states.
func mapShadowToSTStates(deviceCfg *config.NodeCfgDevice, deviceData map[string]interface{}) []STState {
	var states []STState

	for _, param := range deviceCfg.Params {
		value, exists := deviceData[param.ID]
		if !exists {
			continue
		}

		switch param.Type {
		case ParamTypePower:
			switchVal := "off"
			if boolVal, ok := value.(bool); ok && boolVal {
				switchVal = "on"
			}
			states = append(states, STState{
				Component:  ComponentMain,
				Capability: CapabilitySwitch,
				Attribute:  AttributeSwitch,
				Value:      switchVal,
			})

		case ParamTypeBrightness:
			if level, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilitySwitchLevel,
					Attribute:  AttributeLevel,
					Value:      int(level),
					Unit:       "%",
				})
			}

		case ParamTypeHue:
			if hue, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilityColorControl,
					Attribute:  AttributeHue,
					Value:      hue,
				})
			}

		case ParamTypeSaturation:
			if sat, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilityColorControl,
					Attribute:  AttributeSaturation,
					Value:      sat,
					Unit:       "%",
				})
			}

		case ParamTypeCCT:
			if cct, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilityColorTemperature,
					Attribute:  AttributeColorTemperature,
					Value:      int(cct),
					Unit:       "K",
				})
			}

		case ParamTypeSpeed:
			if speed, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilityFanSpeed,
					Attribute:  AttributeFanSpeed,
					Value:      int(speed),
				})
			}

		case ParamTypeTemperature, ParamTypeSetpointTemperature:
			if temp, ok := toNumericValue(value); ok {
				states = append(states, STState{
					Component:  ComponentMain,
					Capability: CapabilityThermostatHeatingSetpoint,
					Attribute:  AttributeHeatingSetpoint,
					Value:      temp,
					Unit:       "C",
				})
			}
		}
	}

	return states
}

// toNumericValue converts an interface{} to float64 for numeric shadow values.
func toNumericValue(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case float32:
		return float64(val), true
	default:
		return 0, false
	}
}
