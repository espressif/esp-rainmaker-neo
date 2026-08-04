// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"fmt"
	"math"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

var traitParamMappings = map[string]string{
	"OnOff":              "paramMap_OnOff",
	"Brightness":         "paramMap_Brightness",
	"FanSpeed":           "paramMap_FanSpeed",
	"TemperatureSetting": "paramMap_TemperatureSetting",
	"Modes":              "paramMap_Modes",
}

// Shared by QUERY and Report State so the two cannot drift apart.
func buildDeviceTraitStates(deviceData map[string]interface{}, customData map[string]interface{}, state map[string]interface{}) {
	for traitName, customDataKey := range traitParamMappings {
		paramName, ok := customData[customDataKey].(string)
		if !ok || paramName == "" {
			continue
		}
		if err := addTraitState(deviceData, traitName, paramName, state); err != nil {
			rlog.Warn(nil).Err(err).Str("trait", traitName).Str("param", paramName).Msg("failed to add trait state")
		}
	}

	addColorSettingState(deviceData, customData, state)
}

func addTraitState(deviceData map[string]interface{}, traitName string, paramName string, state map[string]interface{}) error {
	value, exists := deviceData[paramName]
	if !exists {
		return fmt.Errorf("parameter %s not found in device data", paramName)
	}

	switch traitName {
	case "OnOff":
		boolVal, ok := value.(bool)
		if !ok {
			return fmt.Errorf("invalid type for OnOff trait: %T", value)
		}
		state["on"] = boolVal

	case "Brightness":
		f, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("invalid type for Brightness trait: %T", value)
		}
		state["brightness"] = int(f)

	case "FanSpeed":
		f, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("invalid type for FanSpeed trait: %T", value)
		}
		speedPercentage := int(f)
		state["currentFanSpeedPercent"] = speedPercentage
		if speedPercentage <= 33 {
			state["currentFanSpeedSetting"] = "low"
		} else if speedPercentage <= 66 {
			state["currentFanSpeedSetting"] = "medium"
		} else {
			state["currentFanSpeedSetting"] = "high"
		}

	case "TemperatureSetting":
		f, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("invalid type for TemperatureSetting trait: %T", value)
		}
		state["thermostatTemperatureSetpoint"] = f
		state["thermostatMode"] = "heat" // Default mode, should be determined based on device state

	case "Modes":
		var mode string
		if strVal, ok := value.(string); ok {
			mode = strVal
		} else if f, ok := toFloat64(value); ok {
			mode = fmt.Sprintf("%d", int(f))
		} else {
			return fmt.Errorf("invalid type for Modes trait: %T", value)
		}
		state["currentModeSettings"] = map[string]interface{}{"mode": mode}

	default:
		return fmt.Errorf("unsupported trait: %s", traitName)
	}

	return nil
}

// LightMode values — must stay in sync with rmng-sdk's esp_rmaker_light_mode_t. A bulb is in exactly one of HSV or CCT at a time; reporting both leaves Google Home unable to display either.
const (
	lightModeInvalid = 0
	lightModeHSV     = 1
	lightModeCCT     = 2
)

func resolveLightMode(deviceData map[string]interface{}, customData map[string]interface{}) int {
	modeParam, ok := customData["paramMap_LightMode"].(string)
	if !ok || modeParam == "" {
		return lightModeInvalid
	}
	raw, ok := deviceData[modeParam]
	if !ok {
		return lightModeInvalid
	}
	f, ok := toFloat64(raw)
	if !ok {
		return lightModeInvalid
	}
	return int(f)
}

func addColorSettingState(deviceData map[string]interface{}, customData map[string]interface{}, state map[string]interface{}) {
	hueParam, _ := customData["paramMap_ColorSetting_Hue"].(string)
	satParam, _ := customData["paramMap_ColorSetting_Saturation"].(string)
	brightnessParam, _ := customData["paramMap_Brightness"].(string)
	cctParam, _ := customData["paramMap_ColorSetting_CCT"].(string)

	hasHSV := hueParam != "" || satParam != ""
	hasCCT := cctParam != ""

	// lightModeInvalid leaves both set, so HSV wins below.
	if hasHSV && hasCCT {
		switch resolveLightMode(deviceData, customData) {
		case lightModeCCT:
			hasHSV = false
		case lightModeHSV:
			hasCCT = false
		}
	}

	if !hasHSV {
		if hasCCT {
			if val, ok := deviceData[cctParam]; ok {
				if f, ok := toFloat64(val); ok {
					state["color"] = map[string]interface{}{
						"temperatureK": int(f),
					}
				}
			}
		}
		return
	}

	// HSV is reported as spectrumRgb: the ColorSetting schema requires exactly one of temperatureK, spectrumRgb, spectrumHsv.
	hue, hasHue := lookupFloat(deviceData, hueParam)
	sat, hasSat := lookupFloat(deviceData, satParam)
	bri, hasBri := lookupFloat(deviceData, brightnessParam)
	if !hasHue && !hasSat && !hasBri {
		return
	}
	if !hasSat {
		sat = 100
	}
	if !hasBri {
		bri = 100
	}
	state["color"] = map[string]interface{}{
		"spectrumRgb": hsvToRgbInt(hue, sat/100.0, bri/100.0),
	}
}

func lookupFloat(deviceData map[string]interface{}, param string) (float64, bool) {
	if param == "" {
		return 0, false
	}
	val, ok := deviceData[param]
	if !ok {
		return 0, false
	}
	return toFloat64(val)
}

// hsvToRgbInt converts hue in degrees and saturation/value in [0,1] to packed 0xRRGGBB.
func hsvToRgbInt(h, s, v float64) int {
	h = math.Mod(math.Mod(h, 360)+360, 360)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	toByte := func(f float64) int { return int(math.Round((f + m) * 255)) }
	return toByte(r)<<16 | toByte(g)<<8 | toByte(b)
}

// rgbIntToHsv converts packed 0xRRGGBB to hue in degrees and saturation/value in [0,1].
func rgbIntToHsv(rgb int) (h, s, v float64) {
	r := float64((rgb>>16)&0xFF) / 255
	g := float64((rgb>>8)&0xFF) / 255
	b := float64(rgb&0xFF) / 255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	v = max
	d := max - min
	if max > 0 {
		s = d / max
	}
	if d != 0 {
		switch max {
		case r:
			h = 60 * math.Mod((g-b)/d, 6)
		case g:
			h = 60 * ((b-r)/d + 2)
		default:
			h = 60 * ((r-g)/d + 4)
		}
		if h < 0 {
			h += 360
		}
	}
	return h, s, v
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
