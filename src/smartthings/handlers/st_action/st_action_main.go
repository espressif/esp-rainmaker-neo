// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/smartthings"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/lambda"
)

// interactionTypesRequiringAuth lists interaction types that require OAuth token validation.
// grantCallbackAccess, integrationDeleted, and interactionResult do not require user auth
// as they are system-level interactions from SmartThings Cloud.
var interactionTypesRequiringAuth = map[string]bool{
	smartthings.InteractionDiscoveryRequest:    true,
	smartthings.InteractionStateRefreshRequest: true,
	smartthings.InteractionCommandRequest:      true,
}

func handler(ctx context.Context, raw json.RawMessage) (smartthings.STResponse, error) {
	var request smartthings.STRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to parse SmartThings request")
		return smartthings.STResponse{}, err
	}

	interactionType := request.Headers.InteractionType

	// Raw payload dumps for deep debugging. Logged at Trace so they stay silent unless the
	// Lambda's RLOG level is set to "trace". The actual interactionResult errors
	// (globalError/deviceError) are still surfaced at Error in HandleInteractionResult.
	if interactionType == smartthings.InteractionInteractionResult {
		rlog.Trace(ctx).RawJSON("interactionResultPayload", raw).Msg("SmartThings interactionResult received")
	}
	if interactionType == smartthings.InteractionCommandRequest {
		rlog.Trace(ctx).RawJSON("commandRequestPayload", raw).Msg("SmartThings commandRequest received")
	}

	// Validate token for interaction types that require authentication
	if interactionTypesRequiringAuth[interactionType] {
		if request.Authentication.Token == "" {
			rlog.Error(ctx).Msg("missing access token")
			return buildUnauthenticatedResponse(request), nil
		}

		_, err := smartthings.GetUserIDFromToken(ctx, request.Authentication.Token)
		if err != nil {
			rlog.Error(ctx).Err(err).Msg("token validation failed")
			return buildUnauthenticatedResponse(request), nil
		}
	}

	// Route by interaction type
	var resp smartthings.STResponse
	var err error

	switch interactionType {
	case smartthings.InteractionDiscoveryRequest:
		resp, err = smartthings.HandleDiscovery(ctx, request)
	case smartthings.InteractionStateRefreshRequest:
		resp, err = smartthings.HandleStateRefresh(ctx, request)
	case smartthings.InteractionCommandRequest:
		resp, err = smartthings.HandleCommand(ctx, request)
	case smartthings.InteractionGrantCallbackAccess:
		resp, err = HandleGrantCallbackAccess(ctx, request)
	case smartthings.InteractionIntegrationDeleted:
		resp, err = smartthings.HandleIntegrationDeleted(ctx, request)
	case smartthings.InteractionInteractionResult:
		resp, err = smartthings.HandleInteractionResult(ctx, request)
	default:
		rlog.Warn(ctx).Str("interactionType", interactionType).Msg("unrecognized interaction type")
		return smartthings.STResponse{
			Headers: smartthings.STHeaders{
				Schema:          request.Headers.Schema,
				Version:         request.Headers.Version,
				InteractionType: interactionType,
				RequestID:       request.Headers.RequestID,
			},
		}, nil
	}

	if err != nil {
		rlog.Error(ctx).Err(err).Str("interactionType", interactionType).Msg("handler error")
	} else {
		rlog.Trace(ctx).Interface("resp", resp).Send()
	}

	return resp, err
}

// buildUnauthenticatedResponse returns a response indicating the user is not authenticated.
// SmartThings Schema expects isAuthenticated: false in the response headers for invalid tokens.
func buildUnauthenticatedResponse(request smartthings.STRequest) smartthings.STResponse {
	return smartthings.STResponse{
		Headers: smartthings.STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: request.Headers.InteractionType,
			RequestID:       request.Headers.RequestID,
		},
		IsAuthenticated: boolPtr(false),
	}
}

func boolPtr(b bool) *bool {
	return &b
}

// HandleGrantCallbackAccess forwards to the package implementation, which exchanges the
// authorization code for callback tokens and stores them against the user.
func HandleGrantCallbackAccess(ctx context.Context, request smartthings.STRequest) (smartthings.STResponse, error) {
	return smartthings.HandleGrantCallbackAccess(ctx, request)
}

func main() {
	lambda.Start(handler)
}
