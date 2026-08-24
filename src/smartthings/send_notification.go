// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/integrationauth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/google/uuid"
)

const (
	stNotificationName = "smartthings"
)

// STNotification implements the NotificationService interface for SmartThings state callbacks.
type STNotification struct {
	isTestMode bool
	mockURL    string
}

// NewSTNotification creates a new SmartThings notification service instance.
// A non-empty baseURL switches the adapter into mock mode, routing state
// callbacks to the test webhook instead of the per-user callback URL.
func NewSTNotification(ctx context.Context, baseURL string) *STNotification {
	if baseURL != "" {
		return &STNotification{
			isTestMode: true,
			mockURL:    baseURL + "/v1/smartthings/data",
		}
	}
	return &STNotification{}
}

func (s *STNotification) GetName() string {
	return stNotificationName
}

func (s *STNotification) GetType() notification.NotificationServiceType {
	return notification.NotificationServiceTypeUserSpecific
}

func (s *STNotification) Send(notif interface{}) error {
	return rmerror.NewRMError(nil, "SmartThings notifications must be sent to specific users")
}

// SendTo retrieves stored callback tokens for each user, refreshes if expired,
// and sends the state callback payload to the SmartThings callback URL.
func (s *STNotification) SendTo(notif interface{}, userIDs []string) error {
	rlog.Debug(context.TODO()).Msgf("Sending SmartThings state callback to %d users", len(userIDs))

	if notif == nil {
		rlog.Debug(context.TODO()).Msg("No SmartThings state callback payload, skipping")
		return nil
	}

	callbackPayload, ok := notif.(*STStateCallbackPayload)
	if !ok {
		return rmerror.NewRMError(nil, "failed to cast notification to *STStateCallbackPayload")
	}

	ctx := context.Background()

	for _, userID := range userIDs {
		// One row per regional SmartThings endpoint the user has linked.
		endpoints, err := integrationauth.GetAllOAuthEndpoints(userID, stPlatform)
		if err != nil {
			rlog.Debug(ctx).Err(err).Str("userID", userID).Msg("no callback tokens found, skipping user")
			continue
		}

		for _, endpoint := range endpoints {
			// UpdateAndGetLatestToken refreshes an expired token (via the endpoint's own
			// token URL) and persists it back to the same row before returning it.
			tokenData, err := integrationauth.UpdateAndGetLatestToken(userID, stPlatform, endpoint.EndpointID,
				refreshCallbackToken(ctx, endpoint.TokenCallbackURL))
			if err != nil {
				rlog.Warn(ctx).Err(err).Str("userID", userID).Msg("failed to get callback token, skipping endpoint")
				continue
			}

			// The state-callback URL is the endpoint's natural identifier.
			callbackURL := user_integration_db.DecodeEndpointID(endpoint.EndpointID)
			if s.isTestMode {
				callbackURL = s.mockURL
			}

			if err := sendStateCallback(callbackPayload, tokenData.AccessToken, callbackURL); err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("failed to send state callback, continuing with remaining users")
				continue
			}

			rlog.Debug(ctx).Str("userID", userID).Msg("successfully sent SmartThings state callback")
		}
	}

	return nil
}

// Marshal converts a shadow update notification to a SmartThings state callback payload.
func (s *STNotification) Marshal(notif *notification.Notification) (interface{}, error) {
	if notif.NotificationType != notification.NotificationTypeShadowUpdate {
		return nil, rmerror.NewRMError(nil, "unsupported notification type for SmartThings")
	}

	shadowUpdate := notif.ShadowUpdateData
	if shadowUpdate == nil {
		return nil, rmerror.NewRMError(nil, "shadow update data is nil")
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())
	nodeCfg, err := getNodeConfig(rmngCtx, shadowUpdate.NodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get node configuration")
	}

	// Determine online status from shadow state
	online := true
	if shadowUpdate.State.Online != nil {
		online = *shadowUpdate.State.Online
	}

	// Only report state for devices that actually changed (per the delta)
	changedDevices := make(map[string]bool)
	if shadowUpdate.Delta.Params != nil {
		for deviceName := range shadowUpdate.Delta.Params {
			changedDevices[deviceName] = true
		}
	}

	var deviceStates []STDeviceState

	for _, device := range nodeCfg.Devices {
		if !changedDevices[device.ID] {
			continue
		}

		if shadowUpdate.State.Params == nil {
			rlog.Warn(context.TODO()).Str("device", device.ID).Msg("no params in shadow state, skipping")
			continue
		}

		currDevState, ok := shadowUpdate.State.Params[device.ID]
		if !ok {
			rlog.Warn(context.TODO()).Str("device", device.ID).Msg("device not found in current state, skipping")
			continue
		}

		currDevStateMap, ok := currDevState.(map[string]interface{})
		if !ok {
			rlog.Warn(context.TODO()).Str("device", device.ID).Msg("device state is not a map, skipping")
			continue
		}

		states := marshalDeviceStates(&device, currDevStateMap, online)
		if len(states) == 0 {
			continue
		}

		deviceStates = append(deviceStates, STDeviceState{
			ExternalDeviceID: GetDeviceID(shadowUpdate.NodeID, device.ID),
			States:           states,
		})
	}

	if len(deviceStates) == 0 {
		rlog.Debug(context.TODO()).Msg("no changed device states to report for SmartThings, skipping")
		return nil, nil
	}

	payload := &STStateCallbackPayload{
		Headers: STHeaders{
			Schema:          "st-schema",
			Version:         "1.0",
			InteractionType: InteractionStateCallback,
			RequestID:       uuid.New().String(),
		},
		DeviceState: deviceStates,
	}

	return payload, nil
}

// marshalDeviceStates maps device shadow parameters to SmartThings capability states
// and always includes st.healthCheck.
func marshalDeviceStates(deviceCfg *config.NodeCfgDevice, deviceData map[string]interface{}, online bool) []STState {
	states := mapShadowToSTStates(deviceCfg, deviceData)

	// Always include healthCheck
	healthStatus := "online"
	if !online {
		healthStatus = "offline"
	}
	states = append(states, STState{
		Component:  ComponentMain,
		Capability: CapabilityHealthCheck,
		Attribute:  AttributeHealthStatus,
		Value:      healthStatus,
	})

	return states
}

// STStateCallbackPayload is the payload sent to the SmartThings state callback endpoint.
// It must be a full ST Schema envelope: headers (with schema/version/interactionType) and
// authentication (the per-user callback access token), plus the device states. Omitting the
// headers causes SmartThings to reject the callback with "Invalid or unspecified schema".
type STStateCallbackPayload struct {
	Headers        STHeaders        `json:"headers"`
	Authentication STAuthentication `json:"authentication"`
	DeviceState    []STDeviceState  `json:"deviceState"`
}

// refreshCallbackToken returns an oauth refresh callback bound to one endpoint's token URL.
// It sends an accessTokenRequest with grantType "refresh_token" to the SmartThings token
// endpoint. The clientId and clientSecret are fetched from SSM since they are not available
// in the refresh context.
func refreshCallbackToken(ctx context.Context, oauthTokenURL string) func(string) (*integrationauth.TokenResponse, error) {
	return func(refreshToken string) (*integrationauth.TokenResponse, error) {
		clientID, clientSecret, err := getSTClientCredentials(ctx)
		if err != nil {
			return nil, err
		}

		reqBody := accessTokenRequest{
			Headers: accessTokenRequestHeaders{
				Schema:          "st-schema",
				Version:         "1.0",
				InteractionType: "accessTokenRequest",
				RequestID:       uuid.New().String(),
			},
			CallbackAuthentication: accessTokenRequestAuth{
				GrantType:    "refresh_token",
				RefreshToken: refreshToken,
				ClientID:     clientID,
				ClientSecret: clientSecret,
			},
		}

		tokenResp, err := postAccessTokenRequest(reqBody, oauthTokenURL)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to refresh callback token")
		}

		refreshed := &integrationauth.TokenResponse{
			AccessToken:  tokenResp.CallbackAuthentication.AccessToken,
			RefreshToken: tokenResp.CallbackAuthentication.RefreshToken,
			ExpiresIn:    tokenResp.CallbackAuthentication.ExpiresIn,
		}

		// If the refresh response doesn't include a new refresh token, keep the old one
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = refreshToken
		}

		return refreshed, nil
	}
}

// sendStateCallback sends the state callback payload to the SmartThings callback URL.
func sendStateCallback(payload *STStateCallbackPayload, accessToken string, callbackURL string) error {
	// Set the per-user callback access token in the envelope's authentication block.
	payload.Authentication = STAuthentication{
		TokenType: "Bearer",
		Token:     accessToken,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal state callback payload")
	}

	rlog.Trace(context.TODO()).RawJSON("payload", jsonData).Str("url", callbackURL).Msg("sending SmartThings state callback")

	_, err = notification.MakeHTTPPostRequest(jsonData, callbackURL, func(req *http.Request) error {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		return nil
	})
	if err != nil {
		return rmerror.NewRMError(err, "SmartThings callback endpoint returned error")
	}

	return nil
}
