// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// HandleCommand processes a SmartThings commandRequest and publishes commands
// to devices via IoT Core MQTT, returning the commanded device state.
func HandleCommand(ctx context.Context, request STRequest) (STResponse, error) {
	userID, err := GetUserIDFromToken(ctx, request.Authentication.Token)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to get user ID from token")
	}

	// Scoped to the caller, not a system actor: externalDeviceId comes from the request,
	// so without this any linked account could command any node in the deployment.
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	nodeGroups, err := UserNodeGroups(rmngCtx)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to resolve caller's nodes")
	}

	var deviceStates []STDeviceState

	for _, device := range request.Devices {
		deviceState := executeDeviceCommands(rmngCtx, device, nodeGroups)
		deviceStates = append(deviceStates, deviceState)
	}

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: InteractionCommandResponse,
			RequestID:       request.Headers.RequestID,
		},
		DeviceState: deviceStates,
	}, nil
}

// executeDeviceCommands processes all commands for a single device, publishing each
// to IoT Core MQTT and returning the resulting device state.
func executeDeviceCommands(rmngCtx *rmngctx.RmngContext, device STCommandDevice, nodeGroups map[string]string) STDeviceState {
	nodeID, deviceName, err := ParseDeviceID(device.ExternalDeviceID)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("externalDeviceId", device.ExternalDeviceID).Msg("invalid device ID in command")
		return STDeviceState{
			ExternalDeviceID: device.ExternalDeviceID,
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
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Msg("caller not authorized for node in command")
		return STDeviceState{
			ExternalDeviceID: device.ExternalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceDeleted,
				Detail:    "device not found",
			}},
		}
	}

	// Reachability comes from the node's reported shadow, the platform's
	// connectivity source of truth. Publishing to a node that is not connected
	// is a silent no-op, so SmartThings is told OFFLINE rather than a success
	// the user will not see happen.
	shadow, err := node.NewNode(nodeID).ReadFromReportedShadow(rmngCtx)
	if err != nil {
		rlog.Warn(rmngCtx).Err(err).Str("nodeID", nodeID).Msg("failed to read shadow for command")
		return STDeviceState{
			ExternalDeviceID: device.ExternalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceError,
				Detail:    "failed to read device state",
			}},
		}
	}
	if !node.ShadowOnline(shadow) {
		return STDeviceState{
			ExternalDeviceID: device.ExternalDeviceID,
			States:           []STState{},
			DeviceError: []STDeviceError{{
				ErrorEnum: ErrorDeviceError,
				Detail:    ErrorOffline,
			}},
		}
	}

	// Param names come from the deviceCookie set at discovery, which SmartThings
	// stores and echoes back on every commandRequest. A device SmartThings knows
	// about has therefore always been through discovery and carries one; a request
	// without one is malformed, and the per-capability handlers below report the
	// missing param as a DEVICE-ERROR.
	params := device.DeviceCookie

	// Execute each command and collect resulting states
	var states []STState
	n := node.NewNode(nodeID)

	for _, cmd := range device.Commands {
		cmdStates, err := executeSingleCommand(rmngCtx, n, deviceName, params, cmd)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).
				Str("nodeID", nodeID).
				Str("device", deviceName).
				Str("capability", cmd.Capability).
				Str("command", cmd.Command).
				Msg("failed to execute command")
			return STDeviceState{
				ExternalDeviceID: device.ExternalDeviceID,
				States:           []STState{},
				DeviceError: []STDeviceError{{
					ErrorEnum: ErrorDeviceError,
					Detail:    "command execution failed",
				}},
			}
		}
		states = append(states, cmdStates...)
	}

	return STDeviceState{
		ExternalDeviceID: device.ExternalDeviceID,
		States:           states,
	}
}

// executeSingleCommand maps a SmartThings command to an MQTT payload, publishes it,
// and returns the resulting SmartThings state attributes.
func executeSingleCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	switch cmd.Capability {
	case CapabilitySwitch:
		return executeSwitchCommand(rmngCtx, n, deviceName, params, cmd)
	case CapabilitySwitchLevel:
		return executeSwitchLevelCommand(rmngCtx, n, deviceName, params, cmd)
	case CapabilityColorControl:
		return executeColorControlCommand(rmngCtx, n, deviceName, params, cmd)
	case CapabilityColorTemperature:
		return executeColorTemperatureCommand(rmngCtx, n, deviceName, params, cmd)
	case CapabilityFanSpeed:
		return executeFanSpeedCommand(rmngCtx, n, deviceName, params, cmd)
	case CapabilityThermostatHeatingSetpoint:
		return executeThermostatSetpointCommand(rmngCtx, n, deviceName, params, cmd)
	default:
		return nil, fmt.Errorf("unsupported capability: %s", cmd.Capability)
	}
}

// executeSwitchCommand handles st.switch on/off commands.
func executeSwitchCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	paramName := params[ParamTypePower]
	if paramName == "" {
		return nil, fmt.Errorf("device has no power parameter")
	}

	// SmartThings sends command "on" or "off" as the command name
	var powerValue bool
	switch cmd.Command {
	case "on":
		powerValue = true
	case "off":
		powerValue = false
	default:
		return nil, fmt.Errorf("invalid switch command: %s", cmd.Command)
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: powerValue,
		},
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish switch command")
	}

	switchVal := "off"
	if powerValue {
		switchVal = "on"
	}

	return []STState{{
		Component:  ComponentMain,
		Capability: CapabilitySwitch,
		Attribute:  AttributeSwitch,
		Value:      switchVal,
	}}, nil
}

// executeSwitchLevelCommand handles st.switchLevel setLevel commands.
func executeSwitchLevelCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	paramName := params[ParamTypeBrightness]
	if paramName == "" {
		return nil, fmt.Errorf("device has no brightness parameter")
	}

	level, err := extractNumericArgument(cmd.Arguments, 0)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid switchLevel argument")
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: int(level),
		},
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish switchLevel command")
	}

	return []STState{{
		Component:  ComponentMain,
		Capability: CapabilitySwitchLevel,
		Attribute:  AttributeLevel,
		Value:      int(level),
		Unit:       "%",
	}}, nil
}

// executeColorControlCommand handles st.colorControl setColor commands.
func executeColorControlCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	hueParam := params[ParamTypeHue]
	satParam := params[ParamTypeSaturation]

	if hueParam == "" && satParam == "" {
		return nil, fmt.Errorf("device has no color control parameters")
	}

	// SmartThings sends setColor with a map argument: {"hue": 0-360, "saturation": 0-100}
	if len(cmd.Arguments) == 0 {
		return nil, fmt.Errorf("missing color arguments")
	}

	colorMap, ok := cmd.Arguments[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid color argument format")
	}

	deviceParams := map[string]interface{}{}
	var states []STState

	if hueParam != "" {
		if hue, ok := toNumericValue(colorMap["hue"]); ok {
			deviceParams[hueParam] = int(hue)
			states = append(states, STState{
				Component:  ComponentMain,
				Capability: CapabilityColorControl,
				Attribute:  AttributeHue,
				Value:      hue,
			})
		}
	}

	if satParam != "" {
		if sat, ok := toNumericValue(colorMap["saturation"]); ok {
			deviceParams[satParam] = int(sat)
			states = append(states, STState{
				Component:  ComponentMain,
				Capability: CapabilityColorControl,
				Attribute:  AttributeSaturation,
				Value:      sat,
				Unit:       "%",
			})
		}
	}

	if len(deviceParams) == 0 {
		return nil, fmt.Errorf("no valid color values in arguments")
	}

	publishData := map[string]interface{}{
		deviceName: deviceParams,
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish colorControl command")
	}

	return states, nil
}

// executeColorTemperatureCommand handles st.colorTemperature setColorTemperature commands.
func executeColorTemperatureCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	paramName := params[ParamTypeCCT]
	if paramName == "" {
		return nil, fmt.Errorf("device has no color temperature parameter")
	}

	cct, err := extractNumericArgument(cmd.Arguments, 0)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid colorTemperature argument")
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: int(cct),
		},
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish colorTemperature command")
	}

	return []STState{{
		Component:  ComponentMain,
		Capability: CapabilityColorTemperature,
		Attribute:  AttributeColorTemperature,
		Value:      int(cct),
		Unit:       "K",
	}}, nil
}

// executeFanSpeedCommand handles st.fanSpeed setFanSpeed commands.
func executeFanSpeedCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	paramName := params[ParamTypeSpeed]
	if paramName == "" {
		return nil, fmt.Errorf("device has no speed parameter")
	}

	speed, err := extractNumericArgument(cmd.Arguments, 0)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid fanSpeed argument")
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: int(speed),
		},
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish fanSpeed command")
	}

	return []STState{{
		Component:  ComponentMain,
		Capability: CapabilityFanSpeed,
		Attribute:  AttributeFanSpeed,
		Value:      int(speed),
	}}, nil
}

// executeThermostatSetpointCommand handles st.thermostatHeatingSetpoint setHeatingSetpoint commands.
func executeThermostatSetpointCommand(rmngCtx *rmngctx.RmngContext, n *node.Node, deviceName string, params map[string]string, cmd STCommand) ([]STState, error) {
	paramName := params[ParamTypeTemperature]
	if paramName == "" {
		paramName = params[ParamTypeSetpointTemperature]
	}
	if paramName == "" {
		return nil, fmt.Errorf("device has no temperature parameter")
	}

	temp, err := extractNumericArgument(cmd.Arguments, 0)
	if err != nil {
		return nil, rmerror.NewRMError(err, "invalid thermostatHeatingSetpoint argument")
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: temp,
		},
	}

	if err := n.PublishToDeviceDesired(rmngCtx, publishData); err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish thermostatHeatingSetpoint command")
	}

	return []STState{{
		Component:  ComponentMain,
		Capability: CapabilityThermostatHeatingSetpoint,
		Attribute:  AttributeHeatingSetpoint,
		Value:      temp,
		Unit:       "C",
	}}, nil
}

// paramsByType maps each supported RainMaker param type to the device's param
// name. It is what the deviceCookie carries, so a command can resolve the param
// to publish without re-reading the node config.
func paramsByType(deviceCfg *config.NodeCfgDevice) map[string]string {
	params := make(map[string]string, len(deviceCfg.Params))
	for _, param := range deviceCfg.Params {
		if _, seen := params[param.Type]; !seen {
			params[param.Type] = param.ID
		}
	}
	return params
}

// extractNumericArgument extracts a numeric value from the arguments slice at the given index.
func extractNumericArgument(arguments []interface{}, index int) (float64, error) {
	if len(arguments) <= index {
		return 0, fmt.Errorf("missing argument at index %d", index)
	}

	val, ok := toNumericValue(arguments[index])
	if !ok {
		return 0, fmt.Errorf("argument at index %d is not numeric", index)
	}

	return val, nil
}
