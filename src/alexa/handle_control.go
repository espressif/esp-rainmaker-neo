// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

func HandleControlDirective(ctx context.Context, request AlexaRequest) (AlexaResponse, error) {
	userCtx, node, deviceName, err := GetUserNodeFromRequest(ctx, request)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "Failed to parse request")
	}

	response := CreateResponseFromReq(&request, "Alexa", "Response", map[string]interface{}{})
	response.Event.Endpoint.Scope.Token = request.Directive.Endpoint.Scope.Token
	response.Event.Endpoint.Scope.Type = "BearerToken"
	response.Context = &Context{}
	response.Context.Properties = ContextPropertyList{}

	rlog.Info(ctx).Interface("request", request).Send()
	if err := HandleCapabilityDirective(&request.Directive, node, userCtx, deviceName, &response); err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to handle control directive")
	}

	// Remove the cookie from the response, we don't want to update it
	response.Event.Endpoint.Cookie = nil

	return response, nil
}
