// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// HandleInteractionResult processes interactionResult interactions from SmartThings.
// SmartThings reports the outcome of a prior response here: a response-wide globalError and/or
// per-device deviceError, tagged with originatingInteractionType. We log every error so failures
// (e.g. a rejected commandResponse) are visible.
func HandleInteractionResult(ctx context.Context, request STRequest) (STResponse, error) {
	if request.GlobalError != nil {
		rlog.Warn(ctx).
			Str("originatingInteractionType", request.OriginatingInteractionType).
			Str("errorEnum", request.GlobalError.ErrorEnum).
			Str("detail", request.GlobalError.Detail).
			Msg("SmartThings interactionResult globalError")
	}

	for _, deviceState := range request.DeviceState {
		for _, devErr := range deviceState.DeviceError {
			rlog.Warn(ctx).
				Str("originatingInteractionType", request.OriginatingInteractionType).
				Str("externalDeviceId", deviceState.ExternalDeviceID).
				Str("errorEnum", devErr.ErrorEnum).
				Str("detail", devErr.Detail).
				Msg("SmartThings interactionResult deviceError")
		}
	}

	// Legacy/nested shape (kept for safety in case some interactions use it).
	if request.InteractionResult != nil && request.InteractionResult.Error != nil {
		rlog.Warn(ctx).
			Str("interactionType", request.InteractionResult.InteractionType).
			Str("errorEnum", request.InteractionResult.Error.ErrorEnum).
			Str("detail", request.InteractionResult.Error.Detail).
			Msg("interaction result error")
	}

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: InteractionInteractionResult,
			RequestID:       request.Headers.RequestID,
		},
	}, nil
}
