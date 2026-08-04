// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package integrationauth keeps the OAuth tokens we hold as a client of a user's linked
// third-party service fresh, so the notification senders can call out to Alexa, GVA, and
// webhooks. It is the outbound counterpart to espuser, which is the authorization server
// for tokens pointed at us.
package integrationauth

import (
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

// TokenResponse is the token bundle stored on a user-integration endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// GetAllOAuthEndpoints returns every OAuth-style endpoint a user has on one integration. Used by send-path code that needs to fan out to all linked accounts (e.g. all Alexa linkings for this Rainmaker user).
func GetAllOAuthEndpoints(userID, integrationID string) ([]user_integration_db.UserIntegrationEntry, error) {
	u := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(u)
	return user_integration_db.NewUserDB(ctx).GetUserEntriesByIntegration(integrationID)
}

// UpdateAndGetLatestToken fetches one specific endpoint's OAuth token, refreshes it if expired, persists the refreshed copy back to the same (user, integration, endpoint) row, and returns the latest token. Used by webhook integrations (alexa, gva, webhook_*) whose UserIntegrationEntry rows carry the OAuth bundle in typed columns.
func UpdateAndGetLatestToken(userID, integrationID, endpointID string, refreshToken func(string) (*TokenResponse, error)) (*TokenResponse, error) {
	u := user.NewUser(userID)
	ctx := rmngctx.NewRmngContext(u)
	userDB := user_integration_db.NewUserDB(ctx)

	userEntry, err := userDB.GetUserEntryByEndpoint(integrationID, endpointID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get user entry")
	}
	if userEntry.IntegrationToken == nil || (userEntry.IntegrationToken.RefreshToken == "" && userEntry.IntegrationToken.AccessToken == "") {
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("No OAuth bundle found for user %s on integration %s endpoint %s", userID, integrationID, endpointID))
	}

	// Refresh if expired (or about to expire — we treat any past-due expiry as needing refresh).
	if time.Now().Unix() > userEntry.IntegrationToken.ExpiresAt {
		rlog.Info(ctx).Msgf("Token expired for user %s endpoint %s, attempting refresh", userID, endpointID)
		newToken, err := refreshToken(userEntry.IntegrationToken.RefreshToken)
		if err != nil {
			return nil, rmerror.NewRMError(err, fmt.Sprintf("Failed to refresh token for user %s endpoint %s", userID, endpointID))
		}

		// TODO: If the refresh response omits expires_in, compute and add a reasonable default ourselves.
		userEntry.IntegrationToken.AccessToken = newToken.AccessToken
		userEntry.IntegrationToken.RefreshToken = newToken.RefreshToken
		userEntry.IntegrationToken.ExpiresAt = time.Now().Unix() + newToken.ExpiresIn
		userEntry.IntegrationToken.TokenType = newToken.TokenType

		if err := userDB.RegisterClient(*userEntry); err != nil {
			return nil, rmerror.NewRMError(err, fmt.Sprintf("Failed to update token for user %s endpoint %s", userID, endpointID))
		}

		rlog.Info(ctx).Msgf("Updated token for user %s endpoint %s", userID, endpointID)
	}

	return &TokenResponse{
		AccessToken:  userEntry.IntegrationToken.AccessToken,
		RefreshToken: userEntry.IntegrationToken.RefreshToken,
		ExpiresIn:    userEntry.IntegrationToken.ExpiresAt - time.Now().Unix(),
		TokenType:    userEntry.IntegrationToken.TokenType,
	}, nil
}
