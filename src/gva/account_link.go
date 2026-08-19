// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// Google exposes no link/unlink callback, so the link is tracked from the intents: recorded on SYNC, removed on DISCONNECT. Report State and Request Sync filter recipients by this row.

var gvaLinkEndpointID = user_integration_db.EncodeEndpointID(GVAPlatform)

// Best-effort: fulfillment must not fail on link bookkeeping. The read keeps repeat SYNCs to one cheap read once the row exists.
func ensureAccountLinkRecorded(ctx *rmngctx.RmngContext, accessToken string) {
	userDB := user_integration_db.NewUserDB(ctx)
	if _, err := userDB.GetUserEntryByEndpoint(GVAPlatform, gvaLinkEndpointID); err == nil {
		return
	}
	err := userDB.RegisterClient(user_integration_db.UserIntegrationEntry{
		IntegrationID: GVAPlatform,
		EndpointID:    gvaLinkEndpointID,
		// Unused (GVA authenticates with the shared service account), but the table's write validation requires an OAuth bundle on non-push rows.
		IntegrationToken: &user_integration_db.IntegrationToken{AccessToken: accessToken, TokenType: "Bearer"},
	})
	if err != nil {
		rlog.Warn(ctx).Err(err).Msg("failed to record GVA account link")
	}
}

// Best-effort: DISCONNECT must succeed regardless, Google has already unlinked on its side.
func removeAccountLink(ctx *rmngctx.RmngContext) {
	if err := user_integration_db.NewUserDB(ctx).UnregisterClient(GVAPlatform, gvaLinkEndpointID); err != nil {
		rlog.Warn(ctx).Err(err).Msg("failed to remove GVA account link")
	}
}
