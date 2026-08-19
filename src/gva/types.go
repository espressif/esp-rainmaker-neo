// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"encoding/json"

	"github.com/golang-jwt/jwt/v5"
)

// Google Assistant Smart Home Request structures.
// External contract — all json tags in this file must remain camelCase to match the Google Smart Home spec.
type Input struct {
	Intent  string          `json:"intent"`
	Payload json.RawMessage `json:"payload"`
}

type GVARequest struct {
	RequestID string  `json:"requestId"`
	Inputs    []Input `json:"inputs"`
}

// Google Assistant Smart Home Response structures
type Device struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	Traits          []string               `json:"traits"`
	Name            DeviceName             `json:"name"`
	WillReportState bool                   `json:"willReportState"`
	DeviceInfo      *DeviceInfo            `json:"deviceInfo,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
	CustomData      map[string]interface{} `json:"customData,omitempty"`
}

type DeviceName struct {
	DefaultNames []string `json:"defaultNames,omitempty"`
	Name         string   `json:"name"`
	Nicknames    []string `json:"nicknames,omitempty"`
}

type DeviceInfo struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	HwVersion    string `json:"hwVersion,omitempty"`
	SwVersion    string `json:"swVersion,omitempty"`
}

type SyncPayload struct {
	AgentUserID string   `json:"agentUserId"`
	Devices     []Device `json:"devices"`
}

type QueryPayload struct {
	Devices map[string]interface{} `json:"devices"`
}

type ExecutePayload struct {
	Commands []ExecuteCommand `json:"commands"`
}

type ExecuteCommand struct {
	IDs       []string               `json:"ids"`
	Status    string                 `json:"status"`
	States    map[string]interface{} `json:"states,omitempty"`
	ErrorCode string                 `json:"errorCode,omitempty"`
}

type DisconnectPayload struct {
	// Empty for disconnect requests
}

type GVAResponse struct {
	RequestID string      `json:"requestId"`
	Payload   interface{} `json:"payload"`
}

// OAuth and Authentication structures
type TokenClaims struct {
	jwt.RegisteredClaims
	Username string `json:"username,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	// Our tests use this
	CognitoUsername string `json:"cognito:username,omitempty"`
}

// SSM Parameter name for GVA service account JSON
const GVASSMServiceAccountJSONParam = "/rmng/gva/service_account_json"

// Google HomeGraph API
const (
	HomegraphOAuthScope = "https://www.googleapis.com/auth/homegraph"
	ReportStateEndpoint = "https://homegraph.googleapis.com/v1/devices:reportStateAndNotification"
	RequestSyncEndpoint = "https://homegraph.googleapis.com/v1/devices:requestSync"
)

// ServiceAccount JSON for HomeGraph Report State API.
// All fields are required to match the Google Cloud service account key format
// used by google.JWTConfigFromJSON.
type ServiceAccount struct {
	Type                    string `json:"type" validate:"required,eq=service_account"`
	ProjectID               string `json:"project_id" validate:"required"`
	PrivateKeyID            string `json:"private_key_id" validate:"required"`
	PrivateKey              string `json:"private_key" validate:"required"`
	ClientEmail             string `json:"client_email" validate:"required"`
	ClientID                string `json:"client_id" validate:"required"`
	AuthURI                 string `json:"auth_uri" validate:"required"`
	TokenURI                string `json:"token_uri" validate:"required"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url" validate:"required"`
	ClientX509CertURL       string `json:"client_x509_cert_url" validate:"required"`
	UniverseDomain          string `json:"universe_domain" validate:"required"`
}

// Google Assistant device types
const (
	DeviceTypeLight      = "action.devices.types.LIGHT"
	DeviceTypeSwitch     = "action.devices.types.SWITCH"
	DeviceTypeOutlet     = "action.devices.types.OUTLET"
	DeviceTypeFan        = "action.devices.types.FAN"
	DeviceTypeThermostat = "action.devices.types.THERMOSTAT"
)

// Google Assistant traits
const (
	TraitOnOff              = "action.devices.traits.OnOff"
	TraitBrightness         = "action.devices.traits.Brightness"
	TraitColorSetting       = "action.devices.traits.ColorSetting"
	TraitFanSpeed           = "action.devices.traits.FanSpeed"
	TraitTemperatureSetting = "action.devices.traits.TemperatureSetting"
	TraitModes              = "action.devices.traits.Modes"
)

// Google Assistant commands
const (
	CommandOnOff                         = "action.devices.commands.OnOff"
	CommandBrightnessAbsolute            = "action.devices.commands.BrightnessAbsolute"
	CommandColorAbsolute                 = "action.devices.commands.ColorAbsolute"
	CommandSetFanSpeed                   = "action.devices.commands.SetFanSpeed"
	CommandThermostatTemperatureSetpoint = "action.devices.commands.ThermostatTemperatureSetpoint"
	CommandSetModes                      = "action.devices.commands.SetModes"
)

// Intent types
const (
	IntentSync       = "action.devices.SYNC"
	IntentQuery      = "action.devices.QUERY"
	IntentExecute    = "action.devices.EXECUTE"
	IntentDisconnect = "action.devices.DISCONNECT"
)

// Request payload structures for different intents
type SyncRequest struct {
	// No additional payload for SYNC
}

type QueryRequest struct {
	Devices []QueryDevice `json:"devices"`
}

type QueryDevice struct {
	ID         string                 `json:"id"`
	CustomData map[string]interface{} `json:"customData,omitempty"`
}

type ExecuteRequest struct {
	Commands []ExecuteRequestCommand `json:"commands"`
}

type ExecuteRequestCommand struct {
	Devices   []ExecuteDevice    `json:"devices"`
	Execution []ExecuteExecution `json:"execution"`
}

type ExecuteDevice struct {
	ID         string                 `json:"id"`
	CustomData map[string]interface{} `json:"customData,omitempty"`
}

type ExecuteExecution struct {
	Command string                 `json:"command"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// Error codes
const (
	ErrorCodeDeviceOffline   = "deviceOffline"
	ErrorCodeDeviceNotFound  = "deviceNotFound"
	ErrorCodeValueOutOfRange = "valueOutOfRange"
	ErrorCodeNotSupported    = "notSupported"
	ErrorCodeProtocolError   = "protocolError"
	ErrorCodeUnknownError    = "unknownError"
)

// Status codes
const (
	StatusSuccess = "SUCCESS"
	StatusPending = "PENDING"
	StatusOffline = "OFFLINE"
	StatusError   = "ERROR"
)
