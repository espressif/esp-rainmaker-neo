// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

func HandleDisconnect(ctx context.Context, request GVARequest, accessToken string) (GVAResponse, error) {
	userID, err := user.GetUserIDFromToken(ctx, accessToken)
	if err != nil {
		return GVAResponse{}, rmerror.NewRMError(err, "failed to get identity id")
	}

	rlog.Info(ctx).Str("userID", userID).Msg("GVA disconnect requested for user")

	removeAccountLink(rmngctx.NewRmngContextWithCtx(ctx, user.NewUser(userID)))

	// Create empty response payload for disconnect
	payload := DisconnectPayload{}

	return CreateResponse(request.RequestID, payload), nil
}
