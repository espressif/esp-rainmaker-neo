// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"math"
	"strconv"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func HandleExecute(ctx context.Context, request GVARequest, accessToken string) (GVAResponse, error) {
	userID, err := user.GetUserIDFromToken(ctx, accessToken)
	if err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to get identity id")
	}
	// Create context with user using identity ID
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	// Parse execute request payload
	var executeRequest ExecuteRequest
	if len(request.Inputs) == 0 {
		return GVAResponse{}, rmerror.NewRMError(fmt.Errorf("no inputs in request"), "invalid request")
	}

	if err := json.Unmarshal(request.Inputs[0].Payload, &executeRequest); err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to parse execute request")
	}

	var commands []ExecuteCommand

	for _, command := range executeRequest.Commands {
		for _, device := range command.Devices {
			for _, execution := range command.Execution {
				deviceResult, err := executeDeviceCommand(ctx, rmngCtx, device.ID, device.CustomData, execution, accessToken)
				if err != nil {
					rlog.Error(rmngCtx).Err(err).Str("deviceID", device.ID).Str("command", execution.Command).Msg("failed to execute device command")
					// Create error response for this device
					commands = append(commands, ExecuteCommand{
						IDs:       []string{device.ID},
						Status:    StatusError,
						ErrorCode: ErrorCodeUnknownError,
					})
					continue
				}

				commands = append(commands, deviceResult)
			}
		}
	}

	payload := ExecutePayload{
		Commands: commands,
	}

	return CreateResponse(request.RequestID, payload), nil
}

func executeDeviceCommand(ctx context.Context, rmngCtx *rmngctx.RmngContext, deviceID string, customData map[string]interface{}, execution ExecuteExecution, accessToken string) (ExecuteCommand, error) {
	// Parse device ID to get node ID and device name
	userCtx, n, deviceName, err := GetUserNodeFromRequest(ctx, GVARequest{}, deviceID, accessToken)
	if err != nil {
		return ExecuteCommand{}, rmerror.NewRMError(err, "failed to parse device ID")
	}

	// Override userCtx with our authenticated context
	userCtx = rmngCtx

	// Load node permissions using group ID from custom data.
	groupID, err := groupIDFromCustomData(customData)
	if err != nil {
		return ExecuteCommand{}, err
	}
	if err := user.LoadNodePermissions(userCtx, groupID, n.GetID()); err != nil {
		return ExecuteCommand{}, rmerror.NewRMError(err, "failed to load node permissions")
	}

	// A command published to a node that is not connected is a silent no-op, so
	// report OFFLINE rather than a success the user will not see happen. The
	// shadow's reported.online is the platform's connectivity source of truth,
	// the same one QUERY and the proactive Report State use.
	shadow, err := n.ReadFromReportedShadow(userCtx)
	if err != nil {
		return ExecuteCommand{
			IDs:       []string{deviceID},
			Status:    StatusError,
			ErrorCode: ErrorCodeUnknownError,
		}, rmerror.NewRMError(err, "failed to read device shadow")
	}
	if !node.ShadowOnline(shadow) {
		return ExecuteCommand{
			IDs:       []string{deviceID},
			Status:    StatusOffline,
			ErrorCode: ErrorCodeDeviceOffline,
			States:    map[string]interface{}{"online": false},
		}, nil
	}

	// Execute the command based on the command type
	states, err := handleCommand(ctx, userCtx, n, deviceName, execution, customData)
	if err != nil {
		return ExecuteCommand{
			IDs:       []string{deviceID},
			Status:    StatusError,
			ErrorCode: ErrorCodeUnknownError,
		}, err
	}

	// Reported once here rather than by each command handler: reaching this point
	// means the node was online when the command was published.
	states["online"] = true

	return ExecuteCommand{
		IDs:    []string{deviceID},
		Status: StatusSuccess,
		States: states,
	}, nil
}

func handleCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, execution ExecuteExecution, customData map[string]interface{}) (map[string]interface{}, error) {
	states := make(map[string]interface{})

	switch execution.Command {
	case CommandOnOff:
		return handleOnOffCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	case CommandBrightnessAbsolute:
		return handleBrightnessCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	case CommandColorAbsolute:
		return handleColorCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	case CommandSetFanSpeed:
		return handleFanSpeedCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	case CommandThermostatTemperatureSetpoint:
		return handleTemperatureCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	case CommandSetModes:
		return handleSetModesCommand(ctx, userCtx, node, deviceName, execution.Params, customData, states)

	default:
		return nil, rmerror.NewRMError(fmt.Errorf("unsupported command: %s", execution.Command), "unsupported command")
	}
}

func handleOnOffCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	// Direct parameter mapping - GVA-specific approach
	paramName, ok := customData["paramMap_OnOff"].(string)
	if !ok || paramName == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing OnOff parameter mapping"), "configuration error")
	}

	// Extract 'on' parameter
	on, ok := params["on"].(bool)
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid 'on' parameter"), "invalid parameter")
	}

	// Publish command to device
	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: on,
		},
	}

	err := node.PublishToDeviceDesired(userCtx, publishData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish command")
	}

	// Return current state
	states["on"] = on

	return states, nil
}

func handleBrightnessCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	// Direct parameter mapping - GVA-specific approach
	paramName, ok := customData["paramMap_Brightness"].(string)
	if !ok || paramName == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing Brightness parameter mapping"), "configuration error")
	}

	// Extract 'brightness' parameter
	brightness, ok := params["brightness"].(float64)
	if !ok {
		// Try int conversion
		if brightnessInt, ok := params["brightness"].(int); ok {
			brightness = float64(brightnessInt)
		} else {
			return nil, rmerror.NewRMError(fmt.Errorf("invalid 'brightness' parameter"), "invalid parameter")
		}
	}

	// Validate brightness range (0-100)
	if brightness < 0 || brightness > 100 {
		return nil, rmerror.NewRMError(fmt.Errorf("brightness out of range: %f", brightness), "value out of range")
	}

	// Publish command to device
	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: int(brightness),
		},
	}

	err := node.PublishToDeviceDesired(userCtx, publishData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish command")
	}

	// Return current state
	states["brightness"] = int(brightness)

	return states, nil
}

func handleColorCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	hueParam, _ := customData["paramMap_ColorSetting_Hue"].(string)
	satParam, _ := customData["paramMap_ColorSetting_Saturation"].(string)
	brightnessParam, _ := customData["paramMap_Brightness"].(string)
	cctParam, _ := customData["paramMap_ColorSetting_CCT"].(string)

	if hueParam == "" && satParam == "" && cctParam == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing ColorSetting parameter mapping"), "configuration error")
	}

	color, ok := params["color"].(map[string]interface{})
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid 'color' parameter"), "invalid parameter")
	}

	var hue, saturation, value float64

	if spectrumHsv, exists := color["spectrumHSV"].(map[string]interface{}); exists {
		if h, ok := spectrumHsv["hue"].(float64); ok {
			hue = h
		}
		if s, ok := spectrumHsv["saturation"].(float64); ok {
			saturation = s * 100
		}
		if v, ok := spectrumHsv["value"].(float64); ok {
			value = v * 100
		}

		deviceParams := map[string]interface{}{}
		if hueParam != "" {
			deviceParams[hueParam] = int(hue)
		}
		if satParam != "" {
			deviceParams[satParam] = int(saturation)
		}
		if brightnessParam != "" {
			deviceParams[brightnessParam] = int(value)
		}

		err := node.PublishToDeviceDesired(userCtx, map[string]interface{}{deviceName: deviceParams})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to publish command")
		}

		// Google stores EXECUTE response states in HomeGraph, so echo the single representation QUERY uses.
		states["color"] = map[string]interface{}{
			"spectrumRgb": hsvToRgbInt(hue, saturation/100.0, value/100.0),
		}
	} else if rgbVal, exists := color["spectrumRGB"].(float64); exists {
		// Google sends packed 0xRRGGBB; the device speaks HSV.
		hue, sat, val := rgbIntToHsv(int(rgbVal))

		deviceParams := map[string]interface{}{}
		if hueParam != "" {
			deviceParams[hueParam] = int(math.Round(hue))
		}
		if satParam != "" {
			deviceParams[satParam] = int(math.Round(sat * 100))
		}
		if brightnessParam != "" {
			deviceParams[brightnessParam] = int(math.Round(val * 100))
		}

		err := node.PublishToDeviceDesired(userCtx, map[string]interface{}{deviceName: deviceParams})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to publish command")
		}

		states["color"] = map[string]interface{}{
			"spectrumRgb": int(rgbVal),
		}
	} else if temperature, exists := color["temperature"].(float64); exists {
		// Color temperature command
		if cctParam == "" {
			return nil, rmerror.NewRMError(fmt.Errorf("missing CCT parameter mapping"), "configuration error")
		}

		err := node.PublishToDeviceDesired(userCtx, map[string]interface{}{
			deviceName: map[string]interface{}{
				cctParam: int(temperature),
			},
		})
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to publish command")
		}

		states["color"] = map[string]interface{}{
			"temperatureK": int(temperature),
		}
	} else {
		return nil, rmerror.NewRMError(fmt.Errorf("unsupported color format"), "invalid parameter")
	}

	return states, nil
}

func handleFanSpeedCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	// Direct parameter mapping - GVA-specific approach
	paramName, ok := customData["paramMap_FanSpeed"].(string)
	if !ok || paramName == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing FanSpeed parameter mapping"), "configuration error")
	}

	var speedPercentage int

	// Handle different fan speed command formats
	if fanSpeed, exists := params["fanSpeed"].(string); exists {
		// Named speed setting
		switch fanSpeed {
		case "low":
			speedPercentage = 25
		case "medium":
			speedPercentage = 50
		case "high":
			speedPercentage = 100
		default:
			return nil, rmerror.NewRMError(fmt.Errorf("unknown fan speed: %s", fanSpeed), "invalid parameter")
		}
	} else if fanSpeedPercent, exists := params["fanSpeedPercent"].(float64); exists {
		// Percentage-based speed
		speedPercentage = int(fanSpeedPercent)
	} else {
		return nil, rmerror.NewRMError(fmt.Errorf("missing fan speed parameter"), "invalid parameter")
	}

	// Validate speed range
	if speedPercentage < 0 || speedPercentage > 100 {
		return nil, rmerror.NewRMError(fmt.Errorf("fan speed out of range: %d", speedPercentage), "value out of range")
	}

	// Publish command to device
	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: speedPercentage,
		},
	}

	err := node.PublishToDeviceDesired(userCtx, publishData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish command")
	}

	// Determine speed setting name
	var speedSetting string
	if speedPercentage <= 33 {
		speedSetting = "low"
	} else if speedPercentage <= 66 {
		speedSetting = "medium"
	} else {
		speedSetting = "high"
	}

	// Return current state
	states["currentFanSpeedPercent"] = speedPercentage
	states["currentFanSpeedSetting"] = speedSetting

	return states, nil
}

func handleTemperatureCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	// Direct parameter mapping - GVA-specific approach
	paramName, ok := customData["paramMap_TemperatureSetting"].(string)
	if !ok || paramName == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing TemperatureSetting parameter mapping"), "configuration error")
	}

	// Extract temperature setpoint
	tempSetpoint, ok := params["thermostatTemperatureSetpoint"].(float64)
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid 'thermostatTemperatureSetpoint' parameter"), "invalid parameter")
	}

	// Publish command to device
	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: tempSetpoint,
		},
	}

	err := node.PublishToDeviceDesired(userCtx, publishData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish command")
	}

	// Return current state
	states["thermostatTemperatureSetpoint"] = tempSetpoint
	states["thermostatMode"] = "heat" // Default mode

	return states, nil
}

func handleSetModesCommand(ctx context.Context, userCtx *rmngctx.RmngContext, node *node.Node, deviceName string, params map[string]interface{}, customData map[string]interface{}, states map[string]interface{}) (map[string]interface{}, error) {
	paramName, ok := customData["paramMap_Modes"].(string)
	if !ok || paramName == "" {
		return nil, rmerror.NewRMError(fmt.Errorf("missing Modes parameter mapping"), "configuration error")
	}

	// Google sends: {"updateModeSettings": {"mode": "2"}}
	updateModeSettings, ok := params["updateModeSettings"].(map[string]interface{})
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid 'updateModeSettings' parameter"), "invalid parameter")
	}

	modeValue, ok := updateModeSettings["mode"].(string)
	if !ok {
		return nil, rmerror.NewRMError(fmt.Errorf("invalid mode value"), "invalid parameter")
	}

	// Convert string mode value to int if possible (mode settings are string-typed in GVA
	// but typically int-typed in RainMaker)
	var publishValue interface{}
	if intVal, err := strconv.Atoi(modeValue); err == nil {
		publishValue = intVal
	} else {
		publishValue = modeValue
	}

	publishData := map[string]interface{}{
		deviceName: map[string]interface{}{
			paramName: publishValue,
		},
	}

	err := node.PublishToDeviceDesired(userCtx, publishData)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to publish command")
	}

	states["currentModeSettings"] = map[string]interface{}{
		"mode": modeValue,
	}

	return states, nil
}
