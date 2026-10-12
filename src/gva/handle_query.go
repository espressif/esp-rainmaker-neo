// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func HandleQuery(ctx context.Context, request GVARequest, accessToken string) (GVAResponse, error) {
	userID, err := user.GetUserIDFromToken(ctx, accessToken)
	if err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to get identity id")
	}

	// Create context with user using identity ID
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID))

	// Parse query request payload
	var queryRequest QueryRequest
	if len(request.Inputs) == 0 {
		return GVAResponse{}, rmerror.NewRMError(fmt.Errorf("no inputs in request"), "invalid request")
	}

	if err := json.Unmarshal(request.Inputs[0].Payload, &queryRequest); err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to parse query request")
	}

	deviceStates := make(map[string]interface{})

	for _, device := range queryRequest.Devices {
		deviceState, err := getDeviceState(rmngCtx, device.ID, device.CustomData)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Str("deviceID", device.ID).Msg("failed to get device state")

			// For any error during device state retrieval, return ERROR status
			// This includes invalid device IDs, parsing errors, etc.
			deviceStates[device.ID] = map[string]interface{}{
				"status":    StatusError,
				"errorCode": ErrorCodeDeviceNotFound,
			}
			continue
		}

		deviceStates[device.ID] = deviceState
	}

	payload := QueryPayload{
		Devices: deviceStates,
	}

	return CreateResponse(request.RequestID, payload), nil
}

func getDeviceState(rmngCtx *rmngctx.RmngContext, deviceID string, customData map[string]interface{}) (map[string]interface{}, error) {
	// Parse device ID to get node ID and device name
	nodeID, deviceName, err := parseDeviceID(deviceID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse device ID")
	}
	n := node.NewNode(nodeID)

	// Load node permissions using group ID from custom data.
	groupID, err := groupIDFromCustomData(customData)
	if err != nil {
		return nil, err
	}
	if err := user.LoadNodePermissions(rmngCtx, groupID, n.GetID()); err != nil {
		return nil, rmerror.NewRMError(err, "failed to load node permissions")
	}

	shadow, err := n.ReadFromReportedShadow(rmngCtx)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to read device shadow")
	}

	state := map[string]interface{}{
		"status": StatusSuccess,
		"online": node.ShadowOnline(shadow),
	}

	buildDeviceTraitStates(node.DeviceParamsFromShadow(shadow, deviceName), customData, state)

	return state, nil
}
