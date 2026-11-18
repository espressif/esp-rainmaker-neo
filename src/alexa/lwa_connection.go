// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
)

// ErrLWANotConnected means no Amazon developer account is connected to this deployment.
var ErrLWANotConnected = errors.New("no Amazon developer account connected")

// LWAConnection is the connected Amazon developer account. The refresh token can create, change and delete every skill of that account, so it lives only in one SecureString and is never returned by an API.
type LWAConnection struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

// StoreLWAConnection replaces the connected account. One parameter, so the three values can never belong to different profiles.
func StoreLWAConnection(ctx context.Context, conn LWAConnection) error {
	payload, err := json.Marshal(conn) //nolint:gosec // G117: serialised only into the SecureString below
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal LWA connection")
	}
	return ssmutil.StoreParameter(ctx, AlexaSSMLWAConnectionParam, string(payload))
}

// GetLWAConnection returns the connected account, or ErrLWANotConnected.
func GetLWAConnection(ctx context.Context) (LWAConnection, error) {
	// Uncached: a disconnect or reconnect must take effect on the next request.
	raw, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMLWAConnectionParam, true, false)
	if ssmutil.IsParameterNotFound(err) {
		return LWAConnection{}, ErrLWANotConnected
	}
	if err != nil {
		return LWAConnection{}, rmerror.NewRMError(err, "failed to read LWA connection")
	}
	var conn LWAConnection
	if err := json.Unmarshal([]byte(raw), &conn); err != nil || conn.ClientID == "" || conn.RefreshToken == "" {
		return LWAConnection{}, rmerror.NewRMError(err, "stored LWA connection is unreadable")
	}
	return conn, nil
}

// DeleteLWAConnection disconnects the account. Already disconnected is not an error.
func DeleteLWAConnection(ctx context.Context) error {
	if err := ssmutil.DeleteParameter(ctx, AlexaSSMLWAConnectionParam); err != nil && !ssmutil.IsParameterNotFound(err) {
		return rmerror.NewRMError(err, "failed to delete LWA connection")
	}
	return nil
}

// ConnectedLWAClientID is the connected profile's client id, or "" when none is connected or it cannot be read; for status display only.
func ConnectedLWAClientID(ctx context.Context) string {
	conn, err := GetLWAConnection(ctx)
	if err != nil {
		if !errors.Is(err, ErrLWANotConnected) {
			rlog.Warn(ctx).Err(err).Msg("failed to read LWA connection")
		}
		return ""
	}
	return conn.ClientID
}
