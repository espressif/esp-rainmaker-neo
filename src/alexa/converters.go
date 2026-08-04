// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
)

const (
	/* ESP Rainmaker Standard Device Types. */
	RMDeviceFan               = "esp.device.fan"
	RMDeviceSwitch            = "esp.device.switch"
	RMDeviceLight             = "esp.device.lightbulb"
	RMDeviceLightBulb         = "esp.device.light"
	RMDeviceSpeaker           = "esp.device.speaker"
	RMDeviceLock              = "esp.device.lock"
	RMDeviceSocket            = "esp.device.socket"
	RMDevicePlug              = "esp.device.plug"
	RMDeviceOutlet            = "esp.device.outlet"
	RMDeviceTemperatureSensor = "esp.device.temperature-sensor"
	RMDeviceInteriorBlind     = "esp.device.blinds-internal"
	RMDeviceExteriorBlind     = "esp.device.blinds-external"
	RMDeviceGarageDoor        = "esp.device.garage-door"
	RMDeviceGarageLock        = "esp.device.garage-door-lock"
	RMDeviceAirConditioner    = "esp.device.air-conditioner"
	RMDeviceThermostat        = "esp.device.thermostat"
	RMDeviceTV                = "esp.device.tv"
	RMDeviceSetTop            = "esp.device.set-top"
	RMDeviceWasher            = "esp.device.washer"
	RMDeviceRemote            = "esp.device.remote"
	RMDeviceContactSensor     = "esp.device.contact-sensor"
	RMDeviceMotionSensor      = "esp.device.motion-sensor"
	RMDeviceDoorBell          = "esp.device.doorbell"
	RMDeviceSecurityPanel     = "esp.device.security-panel"
	RMDeviceHeater            = "esp.device.water-heater"
	RMDeviceOther             = "esp.device.other"

	/* ESP Rainmaker Standard Params Types */
	RMParamName                    = "esp.param.name"
	RMParamPower                   = "esp.param.power"
	RMParamBrightness              = "esp.param.brightness"
	RMParamColor                   = "esp.param.color"
	RMParamHue                     = "esp.param.hue"
	RMParamSaturation              = "esp.param.saturation"
	RMParamIntensity               = "esp.param.intensity"
	RMParamColorTemperature        = "esp.param.cct"
	RMParamTempCelsius             = "esp.param.temperature"
	RMParamSetPointTempCelsius     = "esp.param.setpoint-temperature"
	RMParamSetPointLowTempCelsius  = "esp.param.setpoint-low-temperature"
	RMParamSetPointHighTempCelsius = "esp.param.setpoint-high-temperature"
	RMParamLockState               = "esp.param.lockstate"
	RMParamToggle                  = "esp.param.toggle"
	RMParamRange                   = "esp.param.range"
	RMParamBlindsPosition          = "esp.param.blinds-position"
	RMParamGaragePosition          = "esp.param.garage-position"
	RMParamMode                    = "esp.param.mode"
	RMParamSpeed                   = "esp.param.speed"
	RMParamLightMode               = "esp.param.light-mode"
	RMParamAppSelector             = "esp.param.app-selector"
	RMParamInputSelector           = "esp.param.input-selector"
	RMParamMediaActivityState      = "esp.param.media-activity-state"
	RMParamMediaPlaybackState      = "esp.param.media-playback-state"
	RMParamMediaActivityControl    = "esp.param.media-activity-control"
	RMParamVolume                  = "esp.param.volume"
	RMParamMute                    = "esp.param.mute"
	RMParamAcMode                  = "esp.param.ac-mode"
	RMParamAmbientHumidity         = "esp.param.humidity"
	RMParamFanMode                 = "esp.param.fan-mode"
	RMParamChannel                 = "esp.param.channel"
	RMParamChannelRelative         = "esp.param.channel-relative"
	RMParamContactDetectionState   = "esp.param.contact-detection-state"
	RMParamMotionDetectionState    = "esp.param.motion-detection-state"
	RMParamBellPressedState        = "esp.param.bell-pressed"
	RMParamArmState                = "esp.param.arm-state"
	RMParamBurglaryAlarm           = "esp.param.burglary-alarm"
	RMParamCarbonMonoxideAlarm     = "esp.param.carbon-monoxide-alarm"
	RMParamFireAlarm               = "esp.param.fire-alarm"
	RMParamWaterAlarm              = "esp.param.water-alarm"
	RMParamSensorState             = "esp.param.sensor-state"

	/* Subset of Display Categories we support. Feel free to add avs. */
	/* https://developer.amazon.com/en-US/docs/alexa/device-apis/alexa-discovery.html#display-categories */
	AVSDeviceFan            = "FAN"
	AVSDeviceSwitch         = "SWITCH"
	AVSDeviceTempSensor     = "TEMPERATURE_SENSOR"
	AVSDeviceLight          = "LIGHT"
	AVSDeviceSmartPlug      = "SMARTPLUG"
	AVSDeviceSpeaker        = "SPEAKER"
	AVSDeviceLock           = "SMARTLOCK"
	AVSDeviceInteriorBlind  = "INTERIOR_BLIND"
	AVSDeviceExteriorBlind  = "EXTERIOR_BLIND"
	AVSDeviceGarageDoor     = "GARAGE_DOOR"
	AVSDeviceSceneTrigger   = "SCENE_TRIGGER"
	AVSDeviceAirConditioner = "AIR_CONDITIONER"
	AVSDeviceThermostat     = "THERMOSTAT"
	AVSDeviceTV             = "TV"
	AVSDeviceWasher         = "WASHER"
	AVSDeviceContactSensor  = "CONTACT_SENSOR"
	AVSDeviceMotionSensor   = "MOTION_SENSOR"
	AVSDeviceDoorBell       = "DOORBELL"
	AVSDeviceSecurityPanel  = "SECURITY_PANEL"
	AVSDeviceHeater         = "WATER_HEATER"
	AVSDeviceOther          = "OTHER"

	/* AVS Namespaces */
	AVSNamespacePowerControl  = "Alexa.PowerController"
	AVSNamespaceToggleControl = "Alexa.ToggleController"
	AVSNameTurnOnRequest      = "TurnOn"
	AVSNameTurnOffRequest     = "TurnOff"

	AVSNamespaceModeControl = "Alexa.ModeController"
	AVSNameSetMode          = "SetMode"
	AVSNameAdjustMode       = "AdjustMode"

	AVSNamespaceRangeControl = "Alexa.RangeController"
	AVSNameSetRange          = "SetRangeValue"
	AVSNameAdjustRange       = "AdjustRangeValue"

	AVSNamespaceSpeedControl = "SpeedController" // not Alexa interface, added for internal processing

	AVSNamespaceBrightnessControl  = "Alexa.BrightnessController"
	AVSNameSetBrightnessRequest    = "SetBrightness"
	AVSNameAdjustBrightnessRequest = "AdjustBrightness"

	AVSNamespaceColorControl = "Alexa.ColorController"
	AVSNameSetColorRequest   = "SetColor"

	AVSNamespaceTemperatureSensor = "Alexa.TemperatureSensor"

	AVSNamespaceColorTemperatureControl = "Alexa.ColorTemperatureController"
	AVSNameSetColorTemperatureRequest   = "SetColorTemperature"

	AVSNamespaceLockControl = "Alexa.LockController"
	AVSNameSetLockState     = "Lock"
	AVSNameSetUnlockState   = "Unlock"

	AVSNamespaceContactSensor = "Alexa.ContactSensor"

	AVSNameSpaceMotionSensor = "Alexa.MotionSensor"

	AVSNameSpaceSecurityPanelController = "Alexa.SecurityPanelController"
	AVSNameSetArmState                  = "Arm"
	AVSNameDisArm                       = "Disarm"

	AVSNameSpaceDoorbellEventSource = "Alexa.DoorbellEventSource"
	AVSNameDoorbellPressEvent       = "DoorbellPress"

	AVSNameSpaceSceneControl   = "Alexa.SceneController"
	AVSNameActivate            = "Activate"
	AVSNameActivationStarted   = "ActivationStarted"
	AVSNameDeactivate          = "Deactivate"
	AVSNameDeactivationStarted = "DeactivationStarted"

	AVSNameSpaceThermostatControl  = "Alexa.ThermostatController"
	AVSNameSetTargetTemperature    = "SetTargetTemperature"
	AVSNameAdjustTargetTemperature = "AdjustTargetTemperature"
	AVSNameSetThermostatMode       = "SetThermostatMode"

	/* Alexa Capability attributes */
	AVSAttrPowerState          = "powerState"
	AVSAttrBrightness          = "brightness"
	AVSAttrHue                 = "hue"
	AVSAttrSaturation          = "saturation"
	AVSAttrIntensity           = "intensity"
	AVSAttrColorTemperature    = "colorTemperatureInKelvin"
	AVSAttrTemperature         = "temperature" // temp sensor
	AVSAttrTargetSetPoint      = "targetSetpoint"
	AVSAttrThermostatMode      = "thermostatMode"
	AVSAttrLockState           = "lock"
	AVSAttrToggleMode          = "toggleMode"
	AVSAttrRange               = "range"
	AVSAttrMode                = "mode"
	AVSAttrContactSensorState  = "contactSensorState"
	AVSAttrMotionSensorState   = "motionSensorState"
	AVSAttrArmState            = "armState"
	AVSAttrBurglaryAlarm       = "burglaryAlarm"
	AVSAttrCarbonMonoxideAlarm = "carbonMonoxideAlarm"
	AVSAttrFireAlarm           = "fireAlarm"
	AVSAttrWaterAlarm          = "waterAlarm"
	AVSAttrBellPressed         = "bellPressed"

	/* Custom attributes */
	CustomAttrLightMode = "lightMode"
)

/* Device Type Mapping */
var DeviceTypeMapping = map[string]string{
	RMDeviceSwitch:            AVSDeviceSwitch,
	RMDeviceLight:             AVSDeviceLight,
	RMDeviceLightBulb:         AVSDeviceLight,
	RMDeviceSocket:            AVSDeviceSmartPlug,
	RMDevicePlug:              AVSDeviceSmartPlug,
	RMDeviceOutlet:            AVSDeviceSmartPlug,
	RMDeviceLock:              AVSDeviceLock,
	RMDeviceFan:               AVSDeviceFan,
	RMDeviceTemperatureSensor: AVSDeviceTempSensor,
	RMDeviceInteriorBlind:     AVSDeviceInteriorBlind,
	RMDeviceExteriorBlind:     AVSDeviceExteriorBlind,
	RMDeviceGarageDoor:        AVSDeviceGarageDoor,
	RMDeviceGarageLock:        AVSDeviceLock,
	RMDeviceAirConditioner:    AVSDeviceAirConditioner,
	RMDeviceThermostat:        AVSDeviceThermostat,
	RMDeviceTV:                AVSDeviceTV,
	RMDeviceSpeaker:           AVSDeviceSpeaker,
	RMDeviceWasher:            AVSDeviceWasher,
	RMDeviceContactSensor:     AVSDeviceContactSensor,
	RMDeviceMotionSensor:      AVSDeviceMotionSensor,
	RMDeviceDoorBell:          AVSDeviceDoorBell,
	RMDeviceSecurityPanel:     AVSDeviceSecurityPanel,
	RMDeviceHeater:            AVSDeviceHeater,
	RMDeviceOther:             AVSDeviceOther,
}

func GetAVSDeviceType(rainmakerDeviceType *string) string {
	if rainmakerDeviceType == nil {
		return ""
	}
	deviceType := strings.ToLower(*rainmakerDeviceType)
	if DeviceTypeMapping[deviceType] == "" {
		return AVSDeviceOther
	}

	return DeviceTypeMapping[deviceType]
}

func GetAVSCapabilities(rainmakerParam, deviceType *string) []Capabilities {
	if rainmakerParam == nil {
		return []Capabilities{}
	}
	return GetCapabilitiesForParam(*rainmakerParam)
}

const (
	/* Alexa.EndpointHealth connectivity values.
	   https://developer.amazon.com/en-US/docs/alexa/device-apis/alexa-endpointhealth.html */
	AVSConnectivityOK          = "OK"
	AVSConnectivityUnreachable = "UNREACHABLE"
)

// GetEndpointConnectivity maps a node's shadow reachability (node.ShadowOnline,
// the platform's source of truth, shared with the GVA sibling) to the Alexa
// connectivity value.
func GetEndpointConnectivity(shadow node.ReportedOrDesiredShadow) string {
	if !node.ShadowOnline(shadow) {
		return AVSConnectivityUnreachable
	}
	return AVSConnectivityOK
}

func AddAVSPropertyEndpointHealth(properties *ContextPropertyList, connectivity string) {
	var a struct {
		Value string `json:"value"`
	}
	a.Value = connectivity
	properties.AddCtxProperty("Alexa.EndpointHealth", "connectivity", a)
}
