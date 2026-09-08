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

// st.healthCheck is the only thing that tells SmartThings a device is reachable, and without opting in the dispatcher skips this service before Marshal runs.
func (s *STNotification) NotifyOnConnectivityChange() bool {
	return true
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

	callbackPayload, ok := notif.(stCallbackPayload)
	if !ok {
		return rmerror.NewRMError(nil, "failed to cast notification to a SmartThings callback payload")
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

			if err := sendCallback(callbackPayload, tokenData.AccessToken, callbackURL); err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("failed to send callback, continuing with remaining users")
				continue
			}

			rlog.Debug(ctx).Str("userID", userID).Msg("successfully sent SmartThings callback")
		}
	}

	return nil
}

// Marshal converts a shadow update notification to a SmartThings state callback payload.
func (s *STNotification) Marshal(notif *notification.Notification) (interface{}, error) {
	if notif.NotificationType == notification.NotificationTypeGroupMembership {
		return marshalGroupMembership(notif)
	}

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

	// A connect or disconnect arrives as a delta carrying `online` alone, so the loop below matches no device and would return an empty payload; report healthCheck on its own instead, or reachability never reaches the app.
	connectivityOnly := shadowUpdate.Delta.Online != nil && len(shadowUpdate.Delta.Params) == 0

	var deviceStates []STDeviceState

	for _, device := range nodeCfg.Devices {
		if connectivityOnly {
			if !IsSTDiscoverable(&device) {
				continue
			}
			deviceStates = append(deviceStates, STDeviceState{
				ExternalDeviceID: GetDeviceID(shadowUpdate.NodeID, device.ID),
				States:           []STState{healthCheckState(online)},
			})
			continue
		}

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
		Headers:     newCallbackHeaders(InteractionStateCallback),
		DeviceState: deviceStates,
	}

	return payload, nil
}

// marshalDeviceStates maps device shadow parameters to SmartThings capability states
// and always includes st.healthCheck.
func marshalDeviceStates(deviceCfg *config.NodeCfgDevice, deviceData map[string]interface{}, online bool) []STState {
	states := mapShadowToSTStates(deviceCfg, deviceData)

	// Always include healthCheck
	return append(states, healthCheckState(online))
}

func healthCheckState(online bool) STState {
	healthStatus := "online"
	if !online {
		healthStatus = "offline"
	}
	return STState{
		Component:  ComponentMain,
		Capability: CapabilityHealthCheck,
		Attribute:  AttributeHealthStatus,
		Value:      healthStatus,
	}
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

// STDiscoveryCallbackPayload is the payload sent when devices become available to a user
// outside a discoveryRequest. SmartThings only rebuilds its device list from a discovery
// it initiates, so a node added to a group stays invisible until we push this.
type STDiscoveryCallbackPayload struct {
	Headers        STHeaders           `json:"headers"`
	Authentication STAuthentication    `json:"authentication"`
	Devices        []STDiscoveryDevice `json:"devices"`
}

// stCallbackPayload is the shape SendTo needs from any callback envelope: the per-user
// access token is only known once the recipient is resolved, so it is set just before the post.
type stCallbackPayload interface {
	setAuthentication(auth STAuthentication)
}

func (p *STStateCallbackPayload) setAuthentication(auth STAuthentication) { p.Authentication = auth }

func (p *STDiscoveryCallbackPayload) setAuthentication(auth STAuthentication) {
	p.Authentication = auth
}

// marshalGroupMembership builds the callback for a node's group-membership change:
// a discoveryCallback when the node was added, and a stateCallback carrying
// DEVICE-DELETED for each of its devices when it was removed. SmartThings has no
// deletion interaction — the error enum is what makes it drop the device.
func marshalGroupMembership(notif *notification.Notification) (interface{}, error) {
	if notif.GroupMembershipData == nil {
		return nil, rmerror.NewRMError(nil, "group membership data is nil")
	}

	nodeID := notif.GroupMembershipData.NodeID
	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())

	switch action := notif.GroupMembershipData.Action; action {
	case notification.GroupMembershipActionAdded:
		devices := buildSTDevices(rmngCtx, nodeID, notif.GroupID)
		if len(devices) == 0 {
			rlog.Info(rmngCtx).Str("nodeID", nodeID).Msg("no SmartThings devices for node, skipping discoveryCallback")
			return nil, nil
		}
		return &STDiscoveryCallbackPayload{
			Headers: newCallbackHeaders(InteractionDiscoveryCallback),
			Devices: devices,
		}, nil

	case notification.GroupMembershipActionRemoved:
		// The node config outlives group removal, so it still names the devices to drop.
		nodeCfg, err := getNodeConfig(rmngCtx, nodeID)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to get node configuration")
		}
		deviceStates := make([]STDeviceState, 0, len(nodeCfg.Devices))
		for _, device := range nodeCfg.Devices {
			deviceStates = append(deviceStates, STDeviceState{
				ExternalDeviceID: GetDeviceID(nodeID, device.ID),
				States:           []STState{},
				DeviceError:      []STDeviceError{{ErrorEnum: ErrorDeviceDeleted, Detail: "device removed from group"}},
			})
		}
		if len(deviceStates) == 0 {
			rlog.Info(rmngCtx).Str("nodeID", nodeID).Msg("no devices in node config, skipping delete callback")
			return nil, nil
		}
		return &STStateCallbackPayload{
			Headers:     newCallbackHeaders(InteractionStateCallback),
			DeviceState: deviceStates,
		}, nil

	default:
		return nil, rmerror.NewRMError(nil, "unsupported group membership action for SmartThings: "+action)
	}
}

func newCallbackHeaders(interactionType string) STHeaders {
	return STHeaders{
		Schema:          "st-schema",
		Version:         "1.0",
		InteractionType: interactionType,
		RequestID:       uuid.New().String(),
	}
}

// The refresh is its own interaction type: sending the refresh_token grant under accessTokenRequest is rejected with UNSUPPORTED-GRANT-TYPE. Client credentials come from SSM, skipped in test mode as exchangeCodeForTokens skips them.
func refreshCallbackToken(ctx context.Context, oauthTokenURL string) func(string) (*integrationauth.TokenResponse, error) {
	return func(refreshToken string) (*integrationauth.TokenResponse, error) {
		var clientID, clientSecret string
		if !isTestMode() {
			var err error
			clientID, clientSecret, err = getSTClientCredentials(ctx)
			if err != nil {
				return nil, err
			}
		}

		reqBody := accessTokenRequest{
			Headers: accessTokenRequestHeaders{
				Schema:          "st-schema",
				Version:         "1.0",
				InteractionType: InteractionRefreshAccessTokens,
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

// sendCallback sends a callback payload to the SmartThings callback URL.
func sendCallback(payload stCallbackPayload, accessToken string, callbackURL string) error {
	// Set the per-user callback access token in the envelope's authentication block.
	payload.setAuthentication(STAuthentication{
		TokenType: "Bearer",
		Token:     accessToken,
	})

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return rmerror.NewRMError(err, "failed to marshal callback payload")
	}

	rlog.Trace(context.TODO()).RawJSON("payload", jsonData).Str("url", callbackURL).Msg("sending SmartThings callback")

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
