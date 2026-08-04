// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func HandleReportState(ctx context.Context, request AlexaRequest) (AlexaResponse, error) {
	userCtx, node, deviceName, err := GetUserNodeFromRequest(ctx, request)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "Failed to parse request")
	}

	// Overwrite the EndPoint field in the request to only include the EndpointID, erasing other fields
	// take a copy of the cookie
	cookie := request.Directive.Endpoint.Cookie
	endpointId := request.Directive.Endpoint.EndpointID
	request.Directive.Endpoint = &Endpoint{
		EndpointID: endpointId,
	}
	response := CreateResponseFromReq(&request, "Alexa", "StateReport", "")
	response.Context = &Context{}
	properties, err := GenerateCapabilityReport(userCtx, node, deviceName, cookie)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "Failed to generate capability report")
	}
	response.Context.Properties = properties

	return response, nil
}

// GenerateCapabilityReport generates a capability report for the device by querying the reported shadow.
// One shadow read supplies both the device params and the connectivity (reported.online), so StateReport
// reports the node's real reachability instead of a hardcoded value.
func GenerateCapabilityReport(userCtx *rmngctx.RmngContext, n *node.Node, deviceName string, cookie map[string]interface{}) (ContextPropertyList, error) {
	shadow, err := n.ReadFromReportedShadow(userCtx)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get device shadow")
	}

	connectivity := GetEndpointConnectivity(shadow)
	return GenerateCapabilityReportForState(cookie, node.DeviceParamsFromShadow(shadow, deviceName), connectivity)
}

// GenerateCapabilityReportForState generates a capability report for a given state of the device.
// connectivity is the Alexa.EndpointHealth value (see GetEndpointConnectivity).
func GenerateCapabilityReportForState(cookie map[string]interface{}, deviceParams map[string]interface{}, connectivity string) (ContextPropertyList, error) {
	properties := ContextPropertyList{}
	if err := ConvertCurrentStateToCtxProperty(deviceParams, cookie, &properties); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to handle capability report")
	}

	AddAVSPropertyEndpointHealth(&properties, connectivity)
	return properties, nil
}
