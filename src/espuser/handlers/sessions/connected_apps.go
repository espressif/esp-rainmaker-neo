// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/aws/aws-lambda-go/events"

	"github.com/espressif/esp-rainmaker-neo/src/espuser/auth"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/db/refresh_tokens_db"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/refreshtoken"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/signout"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/oidc"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type connectedAppsResponse struct {
	ConnectedApps []ClientView `json:"connected_apps"`
}

// connectedApps folds offline families into one entry per client: the earliest creation, the latest use and every scope granted, most recently used first.
func connectedApps(families []refresh_tokens_db.FamilyEntry) []ClientView {
	byClient := map[string]*ClientView{}
	for i := range families {
		family := &families[i]
		// A family minted before created_at was recorded has only rotated_at.
		createdAt := cmp.Or(family.CreatedAt, family.RotatedAt)
		app, seen := byClient[family.ClientID]
		if !seen {
			app = &ClientView{ClientID: family.ClientID, CreatedAt: createdAt, LastUsed: family.RotatedAt}
			byClient[family.ClientID] = app
		}
		app.CreatedAt = min(app.CreatedAt, createdAt)
		app.LastUsed = max(app.LastUsed, family.RotatedAt)
		app.Scopes = append(app.Scopes, strings.Fields(family.Scope)...)
	}
	apps := make([]ClientView, 0, len(byClient))
	for _, app := range byClient {
		app.Scopes = slices.Compact(slices.Sorted(slices.Values(app.Scopes)))
		apps = append(apps, *app)
	}
	slices.SortFunc(apps, func(a, b ClientView) int {
		return cmp.Or(cmp.Compare(b.LastUsed, a.LastUsed), cmp.Compare(a.ClientID, b.ClientID))
	})
	return apps
}

func handleListConnectedApps(ctx context.Context, rmngCtx *rmngctx.RmngContext, identity auth.TokenClaims) events.APIGatewayProxyResponse {
	families, err := refreshtoken.NewService(rmngCtx).ListOfflineFamilies(identity.Subject)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("connected apps: list offline grants")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
	apps := connectedApps(families)
	withDisplays(ctx, rmngCtx, apps)
	return utils.APIGwRespJSON(http.StatusOK, connectedAppsResponse{ConnectedApps: apps})
}

// handleDisconnectApp ends every login one client holds for the caller; a client the caller has not connected answers 404 whether or not anyone else has, so the endpoint reveals nothing about other people's grants.
func handleDisconnectApp(ctx context.Context, rmngCtx *rmngctx.RmngContext, identity auth.TokenClaims, clientID string) events.APIGatewayProxyResponse {
	switch err := signout.NewService(rmngCtx).DisconnectClient(identity.Subject, clientID); {
	case err == nil:
		return noContent()
	case errors.Is(err, signout.ErrNotConnected):
		return oidc.OAuthErrorResp(http.StatusNotFound, oidc.OAuthErrInvalidRequest, "No such connected app.")
	default:
		rlog.Error(ctx).Err(err).Str("client_id", clientID).Msg("connected apps: disconnect")
		return oidc.OAuthErrorResp(http.StatusInternalServerError, oidc.OAuthErrServerError, "Internal server error.")
	}
}
