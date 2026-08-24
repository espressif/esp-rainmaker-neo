// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

// SmartThings Schema interaction type constants
const (
	InteractionDiscoveryRequest    = "discoveryRequest"
	InteractionStateRefreshRequest = "stateRefreshRequest"
	InteractionCommandRequest      = "commandRequest"
	InteractionGrantCallbackAccess = "grantCallbackAccess"
	InteractionIntegrationDeleted  = "integrationDeleted"
	InteractionInteractionResult   = "interactionResult"
)

// SmartThings Schema response interaction type constants
const (
	InteractionDiscoveryResponse    = "discoveryResponse"
	InteractionStateRefreshResponse = "stateRefreshResponse"
	InteractionCommandResponse      = "commandResponse"
	InteractionStateCallback        = "stateCallback"
)

// SmartThings capability constants
const (
	CapabilitySwitch                    = "st.switch"
	CapabilitySwitchLevel               = "st.switchLevel"
	CapabilityColorControl              = "st.colorControl"
	CapabilityColorTemperature          = "st.colorTemperature"
	CapabilityFanSpeed                  = "st.fanSpeed"
	CapabilityThermostatMode            = "st.thermostatMode"
	CapabilityThermostatHeatingSetpoint = "st.thermostatHeatingSetpoint"
	CapabilityHealthCheck               = "st.healthCheck"
)

// SmartThings component and attribute constants. Every device we expose is
// single-component, so states are always reported on "main".
const (
	ComponentMain = "main"

	AttributeSwitch           = "switch"
	AttributeLevel            = "level"
	AttributeHue              = "hue"
	AttributeSaturation       = "saturation"
	AttributeColorTemperature = "colorTemperature"
	AttributeFanSpeed         = "fanSpeed"
	AttributeHeatingSetpoint  = "heatingSetpoint"
	AttributeHealthStatus     = "healthStatus"
)

// SmartThings error enum constants
const (
	ErrorDeviceError   = "DEVICE-ERROR"
	ErrorDeviceDeleted = "DEVICE-DELETED"
	ErrorOffline       = "OFFLINE"
)

// STRequest is the top-level SmartThings Schema request envelope
type STRequest struct {
	Headers                STHeaders            `json:"headers"`
	Authentication         STAuthentication     `json:"authentication"`
	Devices                []STCommandDevice    `json:"devices,omitempty"`
	CallbackAuthentication *STCallbackAuth      `json:"callbackAuthentication,omitempty"`
	CallbackURLs           *STCallbackURLs      `json:"callbackUrls,omitempty"`
	InteractionResult      *STInteractionResult `json:"interactionResult,omitempty"`
	// interactionResult fields are sent by SmartThings at the TOP LEVEL of the request (not nested):
	// originatingInteractionType identifies which response failed, globalError carries response-wide
	// errors, and deviceState[].deviceError carries per-device errors.
	OriginatingInteractionType string          `json:"originatingInteractionType,omitempty"`
	GlobalError                *STError        `json:"globalError,omitempty"`
	DeviceState                []STDeviceState `json:"deviceState,omitempty"`
}

// STHeaders contains metadata about the Schema interaction
type STHeaders struct {
	Schema          string `json:"schema"`
	Version         string `json:"version"`
	InteractionType string `json:"interactionType"`
	RequestID       string `json:"requestId"`
}

// STAuthentication contains the user's OAuth token
type STAuthentication struct {
	TokenType string `json:"tokenType"`
	Token     string `json:"token"`
}

// STCallbackAuth contains callback OAuth credentials provided by SmartThings
type STCallbackAuth struct {
	GrantType    string `json:"grantType"`
	Scope        string `json:"scope"`
	Code         string `json:"code,omitempty"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresIn    int    `json:"expiresIn,omitempty"`
}

// STCallbackURLs contains the SmartThings callback endpoints
type STCallbackURLs struct {
	OAuthToken    string `json:"oauthToken"`
	StateCallback string `json:"stateCallback"`
}

// STCommandDevice identifies a device in requests. For stateRefresh, only ExternalDeviceID
// is populated. For commandRequest, Commands contains the commands to execute.
type STCommandDevice struct {
	ExternalDeviceID string            `json:"externalDeviceId"`
	DeviceCookie     map[string]string `json:"deviceCookie,omitempty"`
	Commands         []STCommand       `json:"commands,omitempty"`
}

// STCommand represents a single command to execute on a device
type STCommand struct {
	Component  string        `json:"component"`
	Capability string        `json:"capability"`
	Command    string        `json:"command"`
	Arguments  []interface{} `json:"arguments,omitempty"`
}

// STInteractionResult contains the result of a previous interaction
type STInteractionResult struct {
	InteractionType string   `json:"interactionType"`
	RequestID       string   `json:"requestId"`
	Error           *STError `json:"error,omitempty"`
}

// STError represents an error in an interaction result
type STError struct {
	ErrorEnum string `json:"errorEnum"`
	Detail    string `json:"detail,omitempty"`
}

// STResponse is the top-level SmartThings Schema response envelope
type STResponse struct {
	Headers         STHeaders           `json:"headers"`
	IsAuthenticated *bool               `json:"isAuthenticated,omitempty"`
	Devices         []STDiscoveryDevice `json:"devices,omitempty"`
	DeviceState     []STDeviceState     `json:"deviceState,omitempty"`
	// RequestGrantCallbackAccess, when true on a discoveryResponse, asks SmartThings to send a
	// grantCallbackAccess interaction so we can obtain (or refresh) the callback tokens needed
	// for proactive state callbacks. Without it, SmartThings never grants callback access.
	RequestGrantCallbackAccess bool `json:"requestGrantCallbackAccess,omitempty"`
}

// STDiscoveryDevice represents a device in the discovery response
type STDiscoveryDevice struct {
	ExternalDeviceID  string              `json:"externalDeviceId"`
	DeviceCookie      map[string]string   `json:"deviceCookie,omitempty"`
	FriendlyName      string              `json:"friendlyName"`
	DeviceHandlerType string              `json:"deviceHandlerType"`
	ManufacturerInfo  *STManufacturerInfo `json:"manufacturerInfo,omitempty"`
	DeviceContext     *STDeviceContext    `json:"deviceContext,omitempty"`
}

// STManufacturerInfo contains manufacturer metadata for a device
type STManufacturerInfo struct {
	ManufacturerName string `json:"manufacturerName"`
	ModelName        string `json:"modelName"`
	SwVersion        string `json:"swVersion,omitempty"`
}

// STDeviceContext provides additional context about a device
type STDeviceContext struct {
	RoomName   string   `json:"roomName,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// STDeviceState represents device state in stateRefresh/command responses
type STDeviceState struct {
	ExternalDeviceID string          `json:"externalDeviceId"`
	States           []STState       `json:"states"`
	DeviceError      []STDeviceError `json:"deviceError,omitempty"`
}

// STState represents a single capability attribute state
type STState struct {
	Component  string      `json:"component"`
	Capability string      `json:"capability"`
	Attribute  string      `json:"attribute"`
	Value      interface{} `json:"value"`
	Unit       string      `json:"unit,omitempty"`
}

// STDeviceError represents an error for a specific device
type STDeviceError struct {
	ErrorEnum string `json:"errorEnum"`
	Detail    string `json:"detail,omitempty"`
}
