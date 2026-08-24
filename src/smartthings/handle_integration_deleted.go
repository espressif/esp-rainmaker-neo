// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// HandleIntegrationDeleted removes the user's stored SmartThings endpoint rows when
// SmartThings sends an integrationDeleted interaction. If no rows exist, the
// operation succeeds silently.
func HandleIntegrationDeleted(ctx context.Context, request STRequest) (STResponse, error) {
	userID, err := GetUserIDFromToken(ctx, request.Authentication.Token)
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("failed to get user ID for integrationDeleted")
		return STResponse{
			Headers: STHeaders{
				Schema:          request.Headers.Schema,
				Version:         request.Headers.Version,
				InteractionType: request.Headers.InteractionType,
				RequestID:       request.Headers.RequestID,
			},
		}, nil
	}

	callingUser := user.NewUser(userID)
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, callingUser)
	userDB := user_integration_db.NewUserDB(rmngCtx)

	// A user may hold one row per regional SmartThings endpoint — remove them all.
	entries, err := userDB.GetUserEntriesByIntegration(stPlatform)
	if err != nil {
		// "user entry not found" means there is nothing to delete — treat as success.
		rlog.Debug(ctx).Str("userID", userID).Msg("no SmartThings endpoints stored, nothing to delete")
	}
	for _, entry := range entries {
		if err := userDB.UnregisterClient(stPlatform, entry.EndpointID); err != nil {
			return STResponse{}, rmerror.NewRMError(err, "failed to remove callback tokens")
		}
	}

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: request.Headers.InteractionType,
			RequestID:       request.Headers.RequestID,
		},
	}, nil
}
