// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"fmt"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// RMNG parameter type constants used for capability mapping
const (
	ParamTypePower               = "esp.param.power"
	ParamTypeBrightness          = "esp.param.brightness"
	ParamTypeHue                 = "esp.param.hue"
	ParamTypeSaturation          = "esp.param.saturation"
	ParamTypeCCT                 = "esp.param.cct"
	ParamTypeSpeed               = "esp.param.speed"
	ParamTypeTemperature         = "esp.param.temperature"
	ParamTypeSetpointTemperature = "esp.param.setpoint-temperature"
)

// GetUserIDFromToken validates the access token and returns the user ID. Account linking
// runs against the ESP User OIDC provider, so user.GetUserIDFromToken resolves it there
// when ESPUSER_ISSUER is configured, falling back to the Cognito admin service otherwise.
func GetUserIDFromToken(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("access token is empty")
	}
	userID, err := user.GetUserIDFromToken(ctx, token)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to validate SmartThings access token")
	}
	return userID, nil
}

// UserNodeGroups returns the nodes the caller may act on, mapped to the group each one
// belongs to. It walks the caller's own groups exactly as discovery does, so a node the
// caller has no access to is simply absent from the map.
//
// commandRequest and stateRefreshRequest act on an externalDeviceId supplied by the
// request rather than on anything the caller proved ownership of, so every such id must
// be checked against this map (and then through user.LoadNodePermissions) before the node
// is touched. Discovery does not need it: it only ever enumerates the caller's groups.
func UserNodeGroups(rmngCtx *rmngctx.RmngContext) (map[string]string, error) {
	groups, err := group.ListGroupForUser(rmngCtx, "", true)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to list groups for caller")
	}

	nodeGroups := make(map[string]string)
	for _, grp := range groups {
		for nodeID := range grp.NodeGroupEntries {
			nodeGroups[nodeID] = grp.GroupID
		}
	}
	return nodeGroups, nil
}

// AuthorizeNode reports whether the caller may act on nodeID, loading the node's
// permissions into rmngCtx on success. nodeGroups comes from UserNodeGroups.
func AuthorizeNode(rmngCtx *rmngctx.RmngContext, nodeGroups map[string]string, nodeID string) error {
	groupID, ok := nodeGroups[nodeID]
	if !ok {
		return fmt.Errorf("node %s is not accessible to the caller", nodeID)
	}
	if err := user.LoadNodePermissions(rmngCtx, groupID, nodeID); err != nil {
		return rmerror.NewRMError(err, "failed to load node permissions")
	}
	return nil
}

// deviceIDSeparator joins the node ID and device name in an externalDeviceId. It must be
// a character that cannot occur in a node ID, so splitting on the first occurrence always
// recovers the node: a device name may contain the separator, a node ID may not. "_" was
// used originally and broke for node IDs containing underscores, which parsed to the wrong
// node and failed every command and state refresh. Alexa uses "#" for the same reason
// (src/alexa/utils.go).
const deviceIDSeparator = "#"

// GetDeviceID generates a SmartThings external device ID from a node ID and device name.
// Format: <nodeID>#<deviceName>
func GetDeviceID(nodeID, deviceName string) string {
	return fmt.Sprintf("%s%s%s", nodeID, deviceIDSeparator, deviceName)
}

// ParseDeviceID splits a SmartThings external device ID into node ID and device name.
func ParseDeviceID(externalDeviceID string) (nodeID string, deviceName string, err error) {
	parts := strings.SplitN(externalDeviceID, deviceIDSeparator, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid externalDeviceId: %s", externalDeviceID)
	}
	return parts[0], parts[1], nil
}

// GetSTCapabilities maps RMNG device parameter types to SmartThings capabilities.
// It always includes st.healthCheck and deduplicates capabilities.
func GetSTCapabilities(paramTypes []string) []string {
	capabilitySet := make(map[string]bool)

	for _, paramType := range paramTypes {
		switch strings.ToLower(paramType) {
		case ParamTypePower:
			capabilitySet[CapabilitySwitch] = true
		case ParamTypeBrightness:
			capabilitySet[CapabilitySwitchLevel] = true
		case ParamTypeHue, ParamTypeSaturation:
			capabilitySet[CapabilityColorControl] = true
		case ParamTypeCCT:
			capabilitySet[CapabilityColorTemperature] = true
		case ParamTypeSpeed:
			capabilitySet[CapabilityFanSpeed] = true
		case ParamTypeTemperature, ParamTypeSetpointTemperature:
			capabilitySet[CapabilityThermostatMode] = true
			capabilitySet[CapabilityThermostatHeatingSetpoint] = true
		}
	}

	// Always include healthCheck
	capabilitySet[CapabilityHealthCheck] = true

	capabilities := make([]string, 0, len(capabilitySet))
	for cap := range capabilitySet {
		capabilities = append(capabilities, cap)
	}

	return capabilities
}
