// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package alexa_skill defines the wire types used by the Alexa Smart Home skill Lambda.
// External contract — all json tags in this file must remain camelCase to match the Alexa Smart Home spec.
package alexa_skill

import (
	"encoding/json"

	"github.com/golang-jwt/jwt/v5"
)

type Header struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	PayloadVersion string `json:"payloadVersion"`
	MessageID      string `json:"messageId"`
	CorrelationID  string `json:"correlationToken,omitempty"`
}

type Scope struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type Endpoint struct {
	EndpointID string                 `json:"endpointId"`
	Cookie     map[string]interface{} `json:"cookie,omitempty"`
	Scope      *Scope                 `json:"scope,omitempty"`
}

type Directive struct {
	Header   Header          `json:"header"`
	Endpoint *Endpoint       `json:"endpoint,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

type AlexaRequest struct {
	Directive Directive `json:"directive"`
}

type ContextProperty struct {
	NameSpace                 string      `json:"namespace"`
	Name                      string      `json:"name"`
	Value                     interface{} `json:"value"`
	TimeOfSample              string      `json:"timeOfSample"`
	UncertaintyInMilliseconds int64       `json:"uncertaintyInMilliseconds"`
}

type ContextPropertyList []ContextProperty

type Context struct {
	Properties ContextPropertyList `json:"properties"`
}

type Event struct {
	Header   Header       `json:"header"`
	Endpoint *Endpoint    `json:"endpoint,omitempty"`
	Payload  *interface{} `json:"payload"`
}

type AlexaResponse struct {
	Event   Event    `json:"event"`
	Context *Context `json:"context,omitempty"`
}

type GrantPayloadGrant struct {
	Type string `json:"type"`
	Code string `json:"code"`
}

type GranteePayloadGrantee struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type AcceptGrantPayload struct {
	Grant   GrantPayloadGrant     `json:"grant"`
	Grantee GranteePayloadGrantee `json:"grantee"`
}

type CapabilitySupported struct {
	Name string `json:"name"`
}

type CapabilityProperties struct {
	Supported           []CapabilitySupported `json:"supported"`
	ProactivelyReported bool                  `json:"proactivelyReported"`
	Retrievable         bool                  `json:"retrievable"`
}

type Capabilities struct {
	Type       string                `json:"type"`
	Interface  string                `json:"interface"`
	Version    string                `json:"version"`
	Properties *CapabilityProperties `json:"properties,omitempty"`
}

type DiscoveryEndpoint struct {
	EndpointID           string                 `json:"endpointId"`
	ManufacturerName     string                 `json:"manufacturerName"`
	Description          string                 `json:"description"`
	FriendlyName         string                 `json:"friendlyName"`
	DisplayCategories    []string               `json:"displayCategories"`
	Cookie               map[string]interface{} `json:"cookie"`
	Capabilities         []Capabilities         `json:"capabilities"`
	AdditionalAttributes map[string]interface{} `json:"additionalAttributes,omitempty"`
}

type DiscoveryPayload struct {
	Endpoints []DiscoveryEndpoint `json:"endpoints"`
}

// Add these new types for JWT handling
type TokenClaims struct {
	jwt.RegisteredClaims
	Username string `json:"username,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	// Our tests use this
	CognitoUsername string `json:"cognito:username,omitempty"`
}

const AlexaSSMClientIDParam = "/rmng/alexa/client_id"
const AlexaSSMClientSecretParam = "/rmng/alexa/client_secret"
const AlexaSSMSkillIDParam = "/rmng/alexa/skill_id"

// AlexaSSMManufacturerNameParam holds the brand advertised in discovery. It is deployment
// configuration rather than a build-time constant, so an OEM rebrand is a config API call
// instead of a stack redeploy.
const AlexaSSMManufacturerNameParam = "/rmng/alexa/manufacturer_name"

// DefaultManufacturerName is the brand advertised when a deployment has configured none.
// Alexa (WWA) rejects a placeholder manufacturer, so there must always be a real value.
const DefaultManufacturerName = "Espressif"

type ChangeReportCause struct {
	Type string `json:"type"`
}

type ChangeReportChange struct {
	Cause      ChangeReportCause   `json:"cause"`
	Properties ContextPropertyList `json:"properties"`
}

type ChangeReportPayload struct {
	Change ChangeReportChange `json:"change"`
}

// AddOrUpdateReportPayload is the payload of an Alexa.Discovery AddOrUpdateReport
// event, sent proactively when a node becomes discoverable (added to a group).
type AddOrUpdateReportPayload struct {
	Endpoints []DiscoveryEndpoint `json:"endpoints"`
	Scope     *Scope              `json:"scope,omitempty"`
}

// DeleteReportEndpoint identifies an endpoint to remove in a DeleteReport.
type DeleteReportEndpoint struct {
	EndpointID string `json:"endpointId"`
}

// DeleteReportPayload is the payload of an Alexa.Discovery DeleteReport event,
// sent proactively when a node is no longer discoverable (removed from a group).
type DeleteReportPayload struct {
	Endpoints []DeleteReportEndpoint `json:"endpoints"`
	Scope     *Scope                 `json:"scope,omitempty"`
}
