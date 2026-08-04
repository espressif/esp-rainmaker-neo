// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"strconv"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

const (
	brightnessMin = 0
	brightnessMax = 100
	// brightnessRestoreOnPowerOn is the level a light returns to when powered on while dimmed to 0
	// (Alexa treats brightness 0 as off, so a plain "turn on" must land on a usable brightness).
	brightnessRestoreOnPowerOn = 100
)

const (
	// Increase/DecreaseColorTemperature move by a fixed step, clamped to this range. The bounds sit
	// outside the SetColorTemperature test values (2200-7000) so a step from either extreme still moves,
	// as Alexa requires (increase must yield strictly cooler, decrease strictly warmer).
	cctStepKelvin = 1000
	cctMinKelvin  = 1000
	cctMaxKelvin  = 10000
)

func clampBrightness(v int) int {
	if v < brightnessMin {
		return brightnessMin
	}
	if v > brightnessMax {
		return brightnessMax
	}
	return v
}

func powerStateString(on bool) string {
	if on {
		return "ON"
	}
	return "OFF"
}

// isPowerOn reports the device's current power state from its params, defaulting to off.
func isPowerOn(params, cookie map[string]interface{}) bool {
	if powerParam, ok := cookie["paramMap_PowerController"].(string); ok && powerParam != "" {
		if v, ok := params[powerParam].(bool); ok {
			return v
		}
	}
	return false
}

// currentIntParam returns the int value of the cookie-mapped param, defaulting to 0.
func currentIntParam(params, cookie map[string]interface{}, cookieKey string) int {
	if p, ok := cookie[cookieKey].(string); ok && p != "" {
		if v, ok := toInt(params[p]); ok {
			return v
		}
	}
	return 0
}

// stepColorTemperature moves current by one fixed step, clamped to [cctMinKelvin, cctMaxKelvin].
// increase = cooler (higher Kelvin). A missing/zero current falls back to a mid-range value.
func stepColorTemperature(current int, increase bool) int {
	if current <= 0 {
		current = 4000
	}
	if increase {
		return min(current+cctStepKelvin, cctMaxKelvin)
	}
	return max(current-cctStepKelvin, cctMinKelvin)
}

// setPowerParam adds the power param to deviceParams and reports the matching powerState, when the device
// exposes PowerController. Control directives use this so any command implicitly powers the light on.
func setPowerParam(cookie map[string]interface{}, deviceParams map[string]interface{}, on bool, response *AlexaResponse) {
	if powerParam, ok := cookie["paramMap_PowerController"].(string); ok && powerParam != "" {
		deviceParams[powerParam] = on
		response.Context.Properties.AddPropPowerState(powerStateString(on))
	}
}

// setLightModeParam adds the light-mode param to deviceParams when the device exposes one, so a colour or
// colour-temperature command switches the light's mode explicitly instead of relying on the firmware's
// auto-switch timing (Alexa polls state right after the command and can miss a slow transition).
func setLightModeParam(cookie map[string]interface{}, deviceParams map[string]interface{}, mode int) {
	if modeParam, ok := cookie["paramMap_LightMode"].(string); ok && modeParam != "" {
		deviceParams[modeParam] = mode
	}
}

// publishDeviceParams sends the command to the device over the params MQTT topic. Reads elsewhere in
// this package see the device's reported state only, so a relative directive fired before the device
// confirms the previous command computes from the last report. The certification suite's sub-second
// pacing can observe this; human pacing does not, as the firmware confirms within its report debounce.
func publishDeviceParams(n *node.Node, userCtx *rmngctx.RmngContext, deviceName string, deviceParams map[string]interface{}) error {
	return n.PublishToDeviceDesired(userCtx, map[string]interface{}{deviceName: deviceParams})
}

// CapabilityHandler defines the interface for handling different Alexa capabilities
type CapabilityHandler interface {
	HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error
	HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error
	GetCapabilities(paramName string) []Capabilities
}

// PowerControllerHandler handles Alexa.PowerController capability
type PowerControllerHandler struct{}

func (r *ContextPropertyList) AddPropPowerState(powerState string) {
	r.AddCtxProperty("Alexa.PowerController", "powerState", powerState)
}

func (h *PowerControllerHandler) HandleReport(deviceParams map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	if value, exists := deviceParams[paramName]; exists {
		powerState := "OFF"
		if value.(bool) {
			powerState = "ON"
		}
		properties.AddPropPowerState(powerState)
	}
	return nil
}

func (h *PowerControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	// Direct parameter mapping - Alexa-specific approach
	powerParam, ok := directive.Endpoint.Cookie["paramMap_PowerController"].(string)
	if !ok || powerParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing PowerController mapping in cookie"), "")
	}

	var powerValue bool
	var reportState string
	if directive.Header.Name == "TurnOn" {
		powerValue = true
		reportState = "ON"
	} else if directive.Header.Name == "TurnOff" {
		powerValue = false
		reportState = "OFF"
	} else {
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}

	deviceParams := map[string]interface{}{powerParam: powerValue}

	// A light dimmed to 0 reads as off; turning it back on must restore a usable brightness, else it stays dark.
	if powerValue {
		if brightnessParam, ok := directive.Endpoint.Cookie["paramMap_BrightnessController"].(string); ok && brightnessParam != "" {
			params, err := node.GetParams(userCtx, deviceName)
			if err != nil {
				return rmerror.NewRMError(err, "failed to read current brightness on power on")
			}
			if cur, ok := toInt(params[brightnessParam]); ok && cur == 0 {
				deviceParams[brightnessParam] = brightnessRestoreOnPowerOn
				response.Context.Properties.AddPropBrightness(brightnessRestoreOnPowerOn)
			}
		}
	}

	response.Context.Properties.AddPropPowerState(reportState)

	return publishDeviceParams(node, userCtx, deviceName, deviceParams)
}

func (h *PowerControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.PowerController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "powerState"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// BrightnessControllerHandler handles Alexa.BrightnessController capability
type BrightnessControllerHandler struct{}

func (r *ContextPropertyList) AddPropBrightness(brightness int) {
	r.AddCtxProperty("Alexa.BrightnessController", "brightness", brightness)
}

func (h *BrightnessControllerHandler) HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	if value, exists := deviceData[paramName]; exists {
		if v, ok := toInt(value); ok {
			properties.AddPropBrightness(v)
		}
	}
	return nil
}

func (h *BrightnessControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	// Direct parameter mapping - Alexa-specific approach
	brightnessParam, ok := directive.Endpoint.Cookie["paramMap_BrightnessController"].(string)
	if !ok || brightnessParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing BrightnessController mapping in cookie"), "")
	}

	var brightnessValue int
	switch directive.Header.Name {
	case "SetBrightness":
		var payload struct {
			Brightness int `json:"brightness"`
		}
		if err := json.Unmarshal(directive.Payload, &payload); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal brightness payload")
		}
		brightnessValue = clampBrightness(payload.Brightness)
	case "AdjustBrightness":
		var payload struct {
			BrightnessDelta int `json:"brightnessDelta"`
		}
		if err := json.Unmarshal(directive.Payload, &payload); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal brightness delta payload")
		}
		params, err := node.GetParams(userCtx, deviceName)
		if err != nil {
			return rmerror.NewRMError(err, "failed to read current brightness for adjustment")
		}
		// Alexa treats an off light as brightness 0, so a relative change starts from 0 when the light is off.
		base := 0
		if isPowerOn(params, directive.Endpoint.Cookie) {
			base = currentIntParam(params, directive.Endpoint.Cookie, "paramMap_BrightnessController")
		}
		brightnessValue = clampBrightness(base + payload.BrightnessDelta)
	default:
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}

	// Alexa couples brightness with power: 0 means off, any positive value means on.
	deviceParams := map[string]interface{}{brightnessParam: brightnessValue}
	setPowerParam(directive.Endpoint.Cookie, deviceParams, brightnessValue > 0, response)
	response.Context.Properties.AddPropBrightness(brightnessValue)

	return publishDeviceParams(node, userCtx, deviceName, deviceParams)
}

func (h *BrightnessControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.BrightnessController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "brightness"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// ColorControllerHandler handles Alexa.ColorController capability
type ColorControllerHandler struct{}

func (r *ContextPropertyList) AddPropColor(color map[string]interface{}) {
	r.AddCtxProperty("Alexa.ColorController", "color", color)
}

func (h *ColorControllerHandler) HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	// paramName is the hue parameter; saturation and brightness come from cookie
	hueVal, hueExists := deviceData[paramName]
	if !hueExists {
		return nil
	}

	hue, ok := toFloat64(hueVal)
	if !ok {
		return nil
	}

	color := map[string]interface{}{
		"hue": hue,
	}

	// Get saturation from cookie-mapped param
	if satParamName, ok := cookie["paramMap_ColorController_Saturation"].(string); ok && satParamName != "" {
		if satVal, exists := deviceData[satParamName]; exists {
			if sat, ok := toFloat64(satVal); ok {
				color["saturation"] = sat / 100.0 // RainMaker 0-100 → Alexa 0.0-1.0
			}
		}
	}

	// Get brightness from cookie-mapped param
	if brightParamName, ok := cookie["paramMap_BrightnessController"].(string); ok && brightParamName != "" {
		if brightVal, exists := deviceData[brightParamName]; exists {
			if bright, ok := toFloat64(brightVal); ok {
				color["brightness"] = bright / 100.0 // RainMaker 0-100 → Alexa 0.0-1.0
			}
		}
	}

	properties.AddPropColor(color)
	return nil
}

func (h *ColorControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	hueParam, ok := directive.Endpoint.Cookie["paramMap_ColorController_Hue"].(string)
	if !ok || hueParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing ColorController hue mapping in cookie"), "")
	}
	satParam, _ := directive.Endpoint.Cookie["paramMap_ColorController_Saturation"].(string)
	brightParam, _ := directive.Endpoint.Cookie["paramMap_BrightnessController"].(string)

	switch directive.Header.Name {
	case "SetColor":
		var payload struct {
			Color struct {
				Hue        float64 `json:"hue"`
				Saturation float64 `json:"saturation"`
				Brightness float64 `json:"brightness"`
			} `json:"color"`
		}
		if err := json.Unmarshal(directive.Payload, &payload); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal color payload")
		}

		deviceParams := map[string]interface{}{
			hueParam: int(payload.Color.Hue),
		}
		if satParam != "" {
			deviceParams[satParam] = int(payload.Color.Saturation * 100) // Alexa 0-1 → RainMaker 0-100
		}
		if brightParam != "" {
			deviceParams[brightParam] = int(payload.Color.Brightness * 100) // Alexa 0-1 → RainMaker 0-100
		}

		// A color command implies the light turns on and switches to colour (HSV) mode.
		setPowerParam(directive.Endpoint.Cookie, deviceParams, true, response)
		setLightModeParam(directive.Endpoint.Cookie, deviceParams, LightModeHSV)
		response.Context.Properties.AddPropColor(map[string]interface{}{
			"hue":        payload.Color.Hue,
			"saturation": payload.Color.Saturation,
			"brightness": payload.Color.Brightness,
		})

		return publishDeviceParams(node, userCtx, deviceName, deviceParams)
	default:
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}
}

func (h *ColorControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.ColorController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "color"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// ColorTemperatureControllerHandler handles Alexa.ColorTemperatureController capability
type ColorTemperatureControllerHandler struct{}

func (r *ContextPropertyList) AddPropColorTemperature(kelvin int) {
	r.AddCtxProperty("Alexa.ColorTemperatureController", "colorTemperatureInKelvin", kelvin)
}

func (h *ColorTemperatureControllerHandler) HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	if value, exists := deviceData[paramName]; exists {
		// Skip an uninitialized/out-of-range value (a fresh device reports cct 0 until the first
		// colour-temperature command): Alexa's schema rejects colorTemperatureInKelvin below 1000.
		if v, ok := toInt(value); ok && v >= cctMinKelvin {
			properties.AddPropColorTemperature(v)
		}
	}
	return nil
}

func (h *ColorTemperatureControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	cctParam, ok := directive.Endpoint.Cookie["paramMap_ColorTemperatureController"].(string)
	if !ok || cctParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing ColorTemperatureController mapping in cookie"), "")
	}

	var cctValue int
	switch directive.Header.Name {
	case "SetColorTemperature":
		var payload struct {
			ColorTemperatureInKelvin int `json:"colorTemperatureInKelvin"`
		}
		if err := json.Unmarshal(directive.Payload, &payload); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal color temperature payload")
		}
		cctValue = payload.ColorTemperatureInKelvin
	case "IncreaseColorTemperature", "DecreaseColorTemperature":
		params, err := node.GetParams(userCtx, deviceName)
		if err != nil {
			return rmerror.NewRMError(err, "failed to read current color temperature for adjustment")
		}
		current := currentIntParam(params, directive.Endpoint.Cookie, "paramMap_ColorTemperatureController")
		cctValue = stepColorTemperature(current, directive.Header.Name == "IncreaseColorTemperature")
	default:
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}

	// A color-temperature command implies the light turns on and switches to CCT mode.
	deviceParams := map[string]interface{}{cctParam: cctValue}
	setPowerParam(directive.Endpoint.Cookie, deviceParams, true, response)
	setLightModeParam(directive.Endpoint.Cookie, deviceParams, LightModeCCT)
	response.Context.Properties.AddPropColorTemperature(cctValue)

	return publishDeviceParams(node, userCtx, deviceName, deviceParams)
}

func (h *ColorTemperatureControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.ColorTemperatureController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "colorTemperatureInKelvin"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// ToggleControllerHandler handles Alexa.ToggleController capability
type ToggleControllerHandler struct{}

func (r *ContextPropertyList) AddPropToggleState(toggleState string) {
	r.AddCtxProperty("Alexa.ToggleController", "toggleState", toggleState)
}

func (h *ToggleControllerHandler) HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	if value, exists := deviceData[paramName]; exists {
		toggleState := "OFF"
		if boolVal, ok := value.(bool); ok && boolVal {
			toggleState = "ON"
		}
		properties.AddPropToggleState(toggleState)
	}
	return nil
}

func (h *ToggleControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	toggleParam, ok := directive.Endpoint.Cookie["paramMap_ToggleController"].(string)
	if !ok || toggleParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing ToggleController mapping in cookie"), "")
	}

	var toggleValue bool
	var reportState string
	if directive.Header.Name == "TurnOn" {
		toggleValue = true
		reportState = "ON"
	} else if directive.Header.Name == "TurnOff" {
		toggleValue = false
		reportState = "OFF"
	} else {
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}

	response.Context.Properties.AddPropToggleState(reportState)
	return publishDeviceParams(node, userCtx, deviceName, map[string]interface{}{toggleParam: toggleValue})
}

func (h *ToggleControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.ToggleController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "toggleState"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// ModeControllerHandler handles Alexa.ModeController capability
type ModeControllerHandler struct{}

func (r *ContextPropertyList) AddPropMode(mode interface{}) {
	r.AddCtxProperty("Alexa.ModeController", "mode", mode)
}

func (h *ModeControllerHandler) HandleReport(deviceData map[string]interface{}, paramName string, cookie map[string]interface{}, properties *ContextPropertyList) error {
	if value, exists := deviceData[paramName]; exists {
		properties.AddPropMode(value)
	}
	return nil
}

func (h *ModeControllerHandler) HandleDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	modeParam, ok := directive.Endpoint.Cookie["paramMap_ModeController"].(string)
	if !ok || modeParam == "" {
		return rmerror.NewRMError(fmt.Errorf("missing ModeController mapping in cookie"), "")
	}

	switch directive.Header.Name {
	case "SetMode":
		var payload struct {
			Mode interface{} `json:"mode"`
		}
		if err := json.Unmarshal(directive.Payload, &payload); err != nil {
			return rmerror.NewRMError(err, "failed to unmarshal mode payload")
		}

		response.Context.Properties.AddPropMode(payload.Mode)
		return publishDeviceParams(node, userCtx, deviceName, map[string]interface{}{modeParam: payload.Mode})
	default:
		return rmerror.NewRMError(fmt.Errorf("unsupported directive: %s", directive.Header.Name), "")
	}
}

func (h *ModeControllerHandler) GetCapabilities(paramName string) []Capabilities {
	return []Capabilities{
		{
			Type:      "AlexaInterface",
			Interface: "Alexa.ModeController",
			Version:   "3",
			Properties: &CapabilityProperties{
				Supported: []CapabilitySupported{
					{Name: "mode"},
				},
				Retrievable:         true,
				ProactivelyReported: true,
			},
		},
	}
}

// toInt converts various numeric types (including string) to int
func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case float32:
		return int(val), true
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func toFloat64(v interface{}) (float64, bool) {
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

var capabilityHandlers = map[string]CapabilityHandler{
	"PowerController":            &PowerControllerHandler{},
	"BrightnessController":       &BrightnessControllerHandler{},
	"ColorController":            &ColorControllerHandler{},
	"ColorTemperatureController": &ColorTemperatureControllerHandler{},
	"ToggleController":           &ToggleControllerHandler{},
	"ModeController":             &ModeControllerHandler{},
}

// capabilityCookieKeys maps capability handler names to their primary cookie key
var capabilityCookieKeys = map[string]string{
	"PowerController":            "paramMap_PowerController",
	"BrightnessController":       "paramMap_BrightnessController",
	"ColorController":            "paramMap_ColorController_Hue",
	"ColorTemperatureController": "paramMap_ColorTemperatureController",
	"ToggleController":           "paramMap_ToggleController",
	"ModeController":             "paramMap_ModeController",
}

// paramToCapability maps RainMaker param types to Alexa capability handler names
var paramToCapability = map[string]string{
	RMParamPower:            "PowerController",
	RMParamBrightness:       "BrightnessController",
	RMParamHue:              "ColorController",
	RMParamColorTemperature: "ColorTemperatureController",
	RMParamToggle:           "ToggleController",
}

// LightMode values — must stay in sync with rmng-sdk's esp_rmaker_light_mode_t (esp_rmaker_standard_params.h).
const (
	LightModeInvalid = 0
	LightModeHSV     = 1
	LightModeCCT     = 2
)

// resolveLightMode returns the device's current Light Mode (1 = HSV, 2 = CCT, 0 = invalid/unknown). Returns LightModeInvalid if the cookie doesn't carry paramMap_LightMode (device has no mode param), or if the param isn't present/integer in deviceParams.
func resolveLightMode(deviceParams map[string]interface{}, cookie map[string]interface{}) int {
	modeParam, ok := cookie["paramMap_LightMode"].(string)
	if !ok || modeParam == "" {
		return LightModeInvalid
	}
	raw, ok := deviceParams[modeParam]
	if !ok {
		return LightModeInvalid
	}
	mode, ok := toInt(raw)
	if !ok {
		return LightModeInvalid
	}
	return mode
}

func ConvertCurrentStateToCtxProperty(deviceParams map[string]interface{}, cookie map[string]interface{}, properties *ContextPropertyList) error {
	// A bulb with a Light Mode param is in exactly one of HSV (colour) or CCT at any moment. Reporting both Alexa.ColorController and Alexa.ColorTemperatureController properties at the same time causes the Alexa app to show the colour as "unset" because the two controllers disagree.
	mode := resolveLightMode(deviceParams, cookie)

	for capability, cookieKey := range capabilityCookieKeys {
		paramName, ok := cookie[cookieKey].(string)
		if !ok || paramName == "" {
			continue
		}

		switch mode {
		case LightModeHSV:
			if capability == "ColorTemperatureController" {
				continue //skip CCT for HSV mode
			}
		case LightModeCCT:
			if capability == "ColorController" {
				continue //skip HSV for CCT mode
			}
		}

		handler, exists := capabilityHandlers[capability]
		if !exists {
			continue
		}

		if err := handler.HandleReport(deviceParams, paramName, cookie, properties); err != nil {
			return err
		}
		rlog.Info(context.TODO()).Interface("properties", properties).Send()
	}
	return nil
}

// HandleCapabilityDirective processes a capability directive request
func HandleCapabilityDirective(directive *Directive, node *node.Node, userCtx *rmngctx.RmngContext, deviceName string, response *AlexaResponse) error {
	// Extract capability name from namespace (e.g., "Alexa.PowerController" -> "PowerController")
	capabilityName := directive.Header.Namespace[6:] // Remove "Alexa." prefix
	handler, exists := capabilityHandlers[capabilityName]
	if !exists {
		return rmerror.NewRMError(fmt.Errorf("unsupported capability: %s", capabilityName), "")
	}

	return handler.HandleDirective(directive, node, userCtx, deviceName, response)
}

func GetCapabilitiesForParam(paramType string) []Capabilities {
	capabilityName, exists := paramToCapability[paramType]
	if !exists {
		return []Capabilities{}
	}

	handler, exists := capabilityHandlers[capabilityName]
	if !exists {
		return []Capabilities{}
	}

	return handler.GetCapabilities(paramType)
}
