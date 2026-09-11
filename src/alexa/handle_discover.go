// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/group"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/parallel"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// vaDiscoveryFanout bounds the per-node workers one directive may run. High enough that a
// large account clears Alexa's 8s deadline, low enough that a single discovery does not
// push DynamoDB or IoT into throttling.
const vaDiscoveryFanout = 25

// nodeRef is a node together with the group it was reached through, which is what the
// per-node permission load needs.
type nodeRef struct {
	nodeID  string
	groupID string
}

// discoveryResult carries each node's outcome so one failure stays that node's own. done
// separates a worker that ran and produced nothing from one that never ran at all.
type discoveryResult struct {
	endpoints []DiscoveryEndpoint
	err       error
	done      bool
}

func HandleDiscovery(ctx context.Context, request AlexaRequest) (AlexaResponse, error) {
	// Extract token from payload
	var payload struct {
		Scope struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		} `json:"scope"`
	}

	if err := json.Unmarshal(request.Directive.Payload, &payload); err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "invalid payload format")
	}

	userID, err := user.GetUserIDFromToken(ctx, payload.Scope.Token)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to get identity id")
	}

	// Create context with user using identity ID
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	// Get user's groups
	groups, err := group.ListGroupForUser(rmngCtx, "", true)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to list groups")
	}

	// Flatten the group walk to one node list, then build the endpoints concurrently.
	// Alexa gives Discover 8 seconds and each node costs several sequential AWS round
	// trips (node config, the Alexa-enabled write and its MQTT notify, the permission
	// load, the shadow read). Serially that is well over the budget for even a modest
	// account, and the failure is silent: the Lambda's own timeout is far longer, so it
	// finishes, logs a valid response, and Alexa has already discarded it.
	// Group membership is in hand from ListGroupForUser(loadNodes=true), so pairing each
	// node with the group it was reached through costs nothing.
	var refs []nodeRef
	for _, grp := range groups {
		rlog.Debug(rmngCtx).Interface("group", grp).Send()
		for nodeID := range grp.NodeGroupEntries {
			refs = append(refs, nodeRef{nodeID: nodeID, groupID: grp.GroupID})
		}
	}

	// The fan-out inherits the caller's deadline, so dispatch stops once the Lambda's
	// budget is gone rather than building endpoints nobody will read.
	results, _, err := parallel.ProcessParallel(ctx, refs,
		func(ref nodeRef) discoveryResult {
			endpoints, err := createEndpointFromNode(rmngCtx, ref.nodeID, ref.groupID)
			return discoveryResult{endpoints: endpoints, err: err, done: true}
		},
		parallel.ParallelOptions{MaxRoutines: vaDiscoveryFanout, CollectResults: true},
	)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "discovery fan-out did not complete")
	}

	// One node must not cost the account its other endpoints: a failure is logged and
	// skipped, exactly as the serial loop did.
	var endpoints []DiscoveryEndpoint
	for i, result := range results {
		switch {
		case !result.done:
			// ProcessParallel recovers panics into a zero result, which would otherwise
			// drop the node with no trace at all.
			rlog.Error(rmngCtx).Str("nodeID", refs[i].nodeID).Msg("discovery worker did not run for node")
		case result.err != nil:
			rlog.Error(rmngCtx).Err(result.err).Str("nodeID", refs[i].nodeID).Send()
		default:
			rlog.Debug(rmngCtx).Interface("multi_endpoints", result.endpoints).Send()
			endpoints = append(endpoints, result.endpoints...)
		}
	}

	response := DiscoveryPayload{
		Endpoints: endpoints,
	}

	return CreateResponse(
		request.Directive.Header.MessageID,
		"Alexa.Discovery",
		"Discover.Response",
		response,
		"",
		nil,
	), nil
}

func getNodeCfg(ctx *rmngctx.RmngContext, nodeID string) (config.NodeCfg, error) {
	// Use the centralized ConfigService
	configService := config.NewConfigService()
	configData, err := configService.Get(ctx, nodeID)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to get node config from service")
	}

	// Marshal and unmarshal to convert interface{} to NodeCfg struct
	configBytes, err := json.Marshal(configData)
	if err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to marshal config")
	}

	nodeCfg := config.NodeCfg{}
	if err := json.Unmarshal(configBytes, &nodeCfg); err != nil {
		return config.NodeCfg{}, rmerror.NewRMError(err, "failed to unmarshal node config")
	}

	return nodeCfg, nil
}

func getAlexaCapability() Capabilities {
	return Capabilities{
		Type:      "AlexaInterface",
		Interface: "Alexa",
		Version:   "3",
	}
}

func getDeviceHealthCapability() Capabilities {
	return Capabilities{
		Type:      "AlexaInterface",
		Interface: "Alexa.EndpointHealth",
		Version:   "3.1",
		Properties: &CapabilityProperties{
			Supported:           []CapabilitySupported{{Name: "connectivity"}},
			ProactivelyReported: true,
			Retrievable:         true,
		},
	}
}

// createEndpointFromNode builds a node's Alexa discovery endpoints AND runs the
// interactive-discovery side effects: it marks the node Alexa-enabled and sends
// the getAlexaEn proactive notification to the device. Use this only on the
// user-driven discovery path (HandleDiscovery). For a side-effect-free build —
// e.g. a proactive AddOrUpdateReport triggered by a group-membership change —
// use buildDiscoveryEndpoints instead, so devices that aren't being discovered
// by an Alexa user are not spuriously notified.
func createEndpointFromNode(ctx *rmngctx.RmngContext, nodeID string, groupID string) ([]DiscoveryEndpoint, error) {
	nodeCfg, err := getNodeCfg(ctx, nodeID)
	if err != nil {
		return []DiscoveryEndpoint{}, rmerror.NewRMError(err, "failed to get node config")
	}

	rlog.Info(ctx).Interface("nodeCfg", nodeCfg).Str("nodeID", nodeID).Str("groupID", groupID).Send()

	if len(nodeCfg.Devices) > 0 {
		// Mark node as Alexa-enabled since it has discoverable devices
		if err := node.NewNode(nodeID).UpdateAlexaEnabled(ctx.Context, true); err != nil {
			// Continue with discovery even if we fail to update the Alexa enabled status
			rlog.Warn(ctx).Err(err).Str("nodeID", nodeID).Msg("failed to update Alexa enabled status")
		}

		// Send proactive notification to the device
		if err := node.NewNode(nodeID).SendAlexaEnabled(ctx.Context); err != nil {
			return []DiscoveryEndpoint{}, rmerror.NewRMError(err, "failed to send Alexa enabled notification")
		}
	}

	return buildDiscoveryEndpoints(ctx, nodeID, groupID, nodeCfg)
}

// resolveManufacturerName picks the brand to advertise for a node. A manufacturer reported by
// the device wins, so one firmware image can be shipped under several brands; otherwise the
// deployment's configured brand applies. Firmware is expected to leave this unset unless the
// OEM deliberately enables reporting it.
func resolveManufacturerName(ctx context.Context, nodeCfg config.NodeCfg) string {
	if nodeCfg.Info.Manufacturer != "" {
		return nodeCfg.Info.Manufacturer
	}

	return GetAlexaManufacturerName(ctx)
}

// buildDiscoveryEndpoints constructs a node's Alexa discovery endpoints from its
// config, resolving friendly names from the reported shadow when available. It
// has no side effects (no Alexa-enabled marking, no device notification), so it
// is safe to call from proactive/percolation paths.
func buildDiscoveryEndpoints(ctx *rmngctx.RmngContext, nodeID string, groupID string, nodeCfg config.NodeCfg) ([]DiscoveryEndpoint, error) {
	var endpoints []DiscoveryEndpoint

	// Best-effort: read reported shadow to resolve esp.param.name for friendly names
	var shadowParams map[string]interface{}
	if err := user.LoadNodePermissions(ctx, groupID, nodeID); err != nil {
		rlog.Debug(ctx).Err(err).Str("nodeID", nodeID).Msg("could not load node permissions for name resolution, using config names")
	} else {
		shadowData, shadowErr := node.NewNode(nodeID).ReadFromReportedShadow(ctx)
		if shadowErr != nil {
			rlog.Debug(ctx).Err(shadowErr).Str("nodeID", nodeID).Msg("could not read shadow for name resolution, using config names")
		} else {
			shadowParams = shadowData.Params
		}
	}

	manufacturerName := resolveManufacturerName(ctx, nodeCfg)
	// Model is reported per node in the node config. We advertise it only when
	// firmware reports a real model; nodeCfg.Info.Type is an internal node type
	// identifier (e.g. "smartlight-mtr-app"), not a marketing model name, so it
	// is intentionally not used as a fallback for the WWA-visible model.
	model := nodeCfg.Info.Model
	description := manufacturerName + " smart home device"
	if model != "" {
		description = manufacturerName + " " + model
	}

	for _, device := range nodeCfg.Devices {
		newEndpoint := DiscoveryEndpoint{}
		newEndpoint.EndpointID = GetEndpointId(nodeID, device.ID)
		newEndpoint.ManufacturerName = manufacturerName
		newEndpoint.Description = description
		newEndpoint.FriendlyName = resolveDeviceName(device, shadowParams)

		// WWA requires manufacturer and model in additionalAttributes.
		additionalAttributes := map[string]interface{}{
			"manufacturer": manufacturerName,
		}
		if model != "" {
			additionalAttributes["model"] = model
		}
		if nodeCfg.Info.FWVersion != "" {
			additionalAttributes["firmwareVersion"] = nodeCfg.Info.FWVersion
		}
		newEndpoint.AdditionalAttributes = additionalAttributes

		newEndpoint.DisplayCategories = []string{GetAVSDeviceType(&device.Type)}
		capabilities, cookie, err := GetDeviceCapabilities(device, groupID)
		if err != nil {
			return []DiscoveryEndpoint{}, rmerror.NewRMError(err, "failed to get device capabilities")
		}
		newEndpoint.Capabilities = capabilities
		newEndpoint.Cookie = cookie

		endpoints = append(endpoints, newEndpoint)
	}

	return endpoints, nil
}

func GetDeviceCapabilities(device config.NodeCfgDevice, groupID string) ([]Capabilities, map[string]interface{}, error) {
	cookie := map[string]interface{}{
		"groupID": groupID,
	}
	all_capabilities := []Capabilities{
		getAlexaCapability(),
		getDeviceHealthCapability(),
	}

	for _, param := range device.Params {
		capabilities := GetAVSCapabilities(&param.Type, &device.Type)
		for _, capability := range capabilities {
			// Add mapping between capability interface and parameter name
			switch capability.Interface {
			case "Alexa.PowerController":
				cookie["paramMap_PowerController"] = param.ID
			case "Alexa.BrightnessController":
				cookie["paramMap_BrightnessController"] = param.ID
			case "Alexa.ColorController":
				cookie["paramMap_ColorController_Hue"] = param.ID
			case "Alexa.ColorTemperatureController":
				cookie["paramMap_ColorTemperatureController"] = param.ID
			case "Alexa.ToggleController":
				cookie["paramMap_ToggleController"] = param.ID
			case "Alexa.ModeController":
				cookie["paramMap_ModeController"] = param.ID
			}
		}
		all_capabilities = append(all_capabilities, capabilities...)

		// Saturation doesn't trigger its own capability (it's part of ColorController),
		// but we need to record which param holds saturation for the ColorController handler
		if param.Type == RMParamSaturation {
			cookie["paramMap_ColorController_Saturation"] = param.ID
		}

		// esp.param.light-mode isn't exposed as an Alexa capability, but its current value is needed in ReportState to decide whether to report Color or ColorTemperature. Record the param ID so ConvertCurrentStateToCtxProperty can read the mode from the shadow.
		if param.Type == RMParamLightMode {
			cookie["paramMap_LightMode"] = param.ID
		}
	}

	return all_capabilities, cookie, nil
}

// resolveDeviceName returns the esp.param.name value from shadow if available,
// otherwise falls back to the device config name.
func resolveDeviceName(device config.NodeCfgDevice, shadowParams map[string]interface{}) string {
	if shadowParams == nil {
		return device.ID
	}
	deviceData, ok := shadowParams[device.ID]
	if !ok {
		return device.ID
	}
	deviceMap, ok := deviceData.(map[string]interface{})
	if !ok {
		return device.ID
	}
	for _, param := range device.Params {
		if param.Type == RMParamName {
			if nameVal, ok := deviceMap[param.ID].(string); ok && nameVal != "" {
				return nameVal
			}
			break
		}
	}
	return device.ID
}
