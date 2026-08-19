// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/integrationauth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/service/config"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/google/uuid"
	"golang.org/x/oauth2/google"
)

const (
	GVAPlatform = "gva"
)

type GVANotification struct {
	reportURI  string
	tokenURI   string
	isTestMode bool
}

func NewGVANotification(ctx context.Context, baseURL string) *GVANotification {
	if baseURL != "" {
		return &GVANotification{
			reportURI:  baseURL + "/v1/gva/data",
			tokenURI:   baseURL + "/v1/gva/token",
			isTestMode: true,
		}
	}
	return &GVANotification{
		reportURI: ReportStateEndpoint,
	}
}

// getServiceAccountCredentials reads the full service account JSON from SSM.
func getServiceAccountCredentials(ctx context.Context) ([]byte, error) {
	serviceAccountJSON, err := ssmutil.GetParameterWithCaching(ctx, GVASSMServiceAccountJSONParam, true)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get GVA service account config")
	}
	return []byte(serviceAccountJSON), nil
}

// GetGVAToken obtains a service account OAuth2 token for the HomeGraph API.
func GetGVAToken(ctx context.Context) (string, error) {
	data, err := getServiceAccountCredentials(ctx)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to get service account credentials")
	}

	conf, err := google.JWTConfigFromJSON(data, HomegraphOAuthScope)
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to create JWT config from service account")
	}

	token, err := conf.TokenSource(ctx).Token()
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to obtain HomeGraph token")
	}

	return "Bearer " + token.AccessToken, nil
}

// getMockToken fetches a token from the mock token endpoint for testing.
func (g *GVANotification) getMockToken() (string, error) {
	respBody, err := notification.MakeHTTPPostRequest([]byte("{}"), g.tokenURI, func(req *http.Request) error {
		req.Header.Set("Content-Type", "application/json")
		return nil
	})
	if err != nil {
		return "", rmerror.NewRMError(err, "failed to get mock GVA token")
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", rmerror.NewRMError(err, "failed to parse mock token response")
	}

	return "Bearer " + tokenResp.AccessToken, nil
}

func (g *GVANotification) GetName() string {
	return GVAPlatform
}

func (g *GVANotification) GetType() notification.NotificationServiceType {
	return notification.NotificationServiceTypeUserSpecific
}

// NotifyOnConnectivityChange opts GVA into connectivity-only shadow events:
// Report State must tell Google when a device goes online or offline even
// though notify.version did not move (a disconnect writes online only).
func (g *GVANotification) NotifyOnConnectivityChange() bool {
	return true
}

func (g *GVANotification) Send(notif interface{}) error {
	return rmerror.NewRMError(nil, "GVA notifications must be sent to specific users")
}

// Skips group members who never linked Google, so they cost no HomeGraph call. An unlinked user surfaces as the lookup's not-found error, so errors are not logged per user.
func filterLinkedUsers(userIDs []string) []string {
	linked := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if endpoints, err := integrationauth.GetAllOAuthEndpoints(userID, GVAPlatform); err == nil && len(endpoints) > 0 {
			linked = append(linked, userID)
		}
	}
	return linked
}

func (g *GVANotification) SendTo(notif interface{}, userIDs []string) error {
	userIDs = filterLinkedUsers(userIDs)
	if len(userIDs) == 0 {
		rlog.Info(context.TODO()).Msg("No GVA-linked users among the recipients, skipping")
		return nil
	}
	rlog.Info(context.TODO()).Msgf("Sending GVA notification to %d users", len(userIDs))

	switch data := notif.(type) {
	case GVANotificationBatch:
		if len(data.Reports) > 0 {
			if err := g.sendReportState(data.Reports, userIDs); err != nil {
				return err
			}
		}
		if data.RequestSync {
			return g.sendRequestSync([]GVARequestSyncRequest{{Async: false}}, userIDs)
		}
		return nil
	case []GVARequestSyncRequest:
		return g.sendRequestSync(data, userIDs)
	default:
		return rmerror.NewRMError(nil, "Failed to cast GVA notification to a known request type")
	}
}

// getToken returns the HomeGraph bearer token, using the mock endpoint in test mode.
func (g *GVANotification) getToken(ctx context.Context) (string, error) {
	if g.isTestMode {
		return g.getMockToken()
	}
	return GetGVAToken(ctx)
}

// Makes Google re-run SYNC to reconcile the user's device list after a membership change or rename.
func (g *GVANotification) sendRequestSync(requests []GVARequestSyncRequest, userIDs []string) error {
	ctx := context.Background()
	gvaToken, err := g.getToken(ctx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get GVA token for request sync")
	}

	// The mock exposes a single data endpoint.
	endpoint := RequestSyncEndpoint
	if g.isTestMode {
		endpoint = g.reportURI
	}

	for _, userID := range userIDs {
		for _, req := range requests {
			req.AgentUserID = userID
			jsonData, err := json.Marshal(req)
			if err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("Failed to marshal request sync")
				continue
			}
			_, err = notification.MakeHTTPPostRequest(jsonData, endpoint, func(r *http.Request) error {
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Authorization", gvaToken)
				return nil
			})
			if err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("Failed to send GVA request sync")
				continue
			}
			rlog.Info(ctx).Str("userID", userID).Msg("Successfully sent GVA request sync")
		}
	}
	return nil
}

func (g *GVANotification) sendReportState(reportStateRequests []GVAReportStateRequest, userIDs []string) error {
	// Get token - use mock token endpoint in test mode, Google OAuth otherwise
	ctx := context.Background()
	gvaToken, err := g.getToken(ctx)
	if err != nil {
		return rmerror.NewRMError(err, "failed to get GVA token for report state")
	}

	for _, userID := range userIDs {
		success := true
		for _, reportStateRequest := range reportStateRequests {
			reportStateRequest.AgentUserID = userID

			jsonData, err := json.Marshal(reportStateRequest)
			if err != nil {
				rlog.Error(ctx).Err(err).Msg("Failed to marshal report state request")
				success = false
				continue
			}

			rlog.Trace(ctx).Str("userID", userID).RawJSON("payload", jsonData).Msg("Sending GVA report state request")

			respBody, err := notification.MakeHTTPPostRequest(jsonData, g.reportURI, func(req *http.Request) error {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", gvaToken)
				return nil
			})
			if err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("Failed to send GVA report state")
				success = false
				continue
			}
			rlog.Trace(ctx).Str("userID", userID).Str("response", string(respBody)).Msg("GVA report state response")
		}
		if success {
			rlog.Info(ctx).Str("userID", userID).Msg("Successfully sent GVA report state")
		}
	}

	return nil
}

func (g *GVANotification) Marshal(notif *notification.Notification) (interface{}, error) {
	// Added and removed both map to a Request Sync. AgentUserID is filled per-user in SendTo.
	if notif.NotificationType == notification.NotificationTypeGroupMembership {
		if notif.GroupMembershipData == nil {
			return nil, rmerror.NewRMError(nil, "Group membership data is nil")
		}
		return []GVARequestSyncRequest{{Async: false}}, nil
	}

	if notif.NotificationType != "shadow_update" {
		return nil, rmerror.NewRMError(nil, "Unsupported notification type for GVA")
	}

	shadowUpdate := notif.ShadowUpdateData
	if shadowUpdate == nil {
		return nil, rmerror.NewRMError(nil, "Shadow update data is nil")
	}

	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())
	nodeCfg, err := getNodeCfg(rmngCtx, shadowUpdate.NodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get node configuration")
	}

	groupID, _, err := node.GetGroupIDFromShadowName(shadowUpdate.ShadowName)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get group ID from shadow name")
	}

	// Determine online status from shadow state
	online := true
	if shadowUpdate.State.Online != nil {
		online = *shadowUpdate.State.Online
	}

	// Only report state for devices that actually changed (per the delta),
	// matching Google's expected Report State behavior.
	changedDevices := make(map[string]bool)
	if shadowUpdate.Delta.Params != nil {
		for deviceName := range shadowUpdate.Delta.Params {
			changedDevices[deviceName] = true
		}
	}

	// A connectivity transition is an online-only delta with no params, so every device must report.
	onlineChanged := shadowUpdate.Delta.Online != nil

	deviceStates := make(map[string]interface{})
	for _, device := range nodeCfg.Devices {
		if !changedDevices[device.ID] && !onlineChanged {
			continue
		}

		deviceID := GetDeviceId(shadowUpdate.NodeID, device.ID)

		_, _, customData := GetDeviceCapabilities(device, groupID)

		if shadowUpdate.State.Params == nil {
			rlog.Warn(rmngCtx).Str("device", device.ID).Msg("No params in shadow state, skipping")
			continue
		}

		currDevState, ok := shadowUpdate.State.Params[device.ID]
		if !ok {
			rlog.Warn(rmngCtx).Str("device", device.ID).Msg("Device not found in current state, skipping")
			continue
		}

		currDevStateMap, ok := currDevState.(map[string]interface{})
		if !ok {
			rlog.Warn(rmngCtx).Str("device", device.ID).Msg("Device state is not a map, skipping")
			continue
		}

		deviceState, err := GenerateGVAStateReport(customData, currDevStateMap, online)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Str("device", device.ID).Msg("Failed to generate GVA state report")
			return nil, rmerror.NewRMError(err, "Failed to generate GVA state report")
		}
		deviceStates[deviceID] = deviceState
	}

	if len(deviceStates) == 0 {
		rlog.Info(rmngCtx).Msg("No changed device states to report, skipping Report State")
		return GVANotificationBatch{}, nil
	}

	return GVANotificationBatch{
		Reports: []GVAReportStateRequest{{
			RequestID: uuid.New().String(),
			Payload: GVAReportStatePayload{
				Devices: GVADeviceStates{
					States: deviceStates,
				},
			},
		}},
		RequestSync: deltaRenamesDevice(shadowUpdate.Delta.Params, nodeCfg.Devices),
	}, nil
}

// Google only picks a rename up through a fresh SYNC, so the sender fires RequestSync alongside the state report.
func deltaRenamesDevice(deltaParams map[string]interface{}, devices []config.NodeCfgDevice) bool {
	for _, device := range devices {
		deviceDelta, ok := deltaParams[device.ID].(map[string]interface{})
		if !ok {
			continue
		}
		for _, param := range device.Params {
			if param.Type != alexa_skill.RMParamName {
				continue
			}
			if _, changed := deviceDelta[param.ID]; changed {
				return true
			}
		}
	}
	return false
}

// Marshal hands this to SendTo: the reports plus whether a rename requires a RequestSync.
type GVANotificationBatch struct {
	Reports     []GVAReportStateRequest
	RequestSync bool
}

// External contract: json tags below must stay camelCase to match the Google Smart Home spec.
type GVAReportStateRequest struct {
	RequestID   string                `json:"requestId"`
	AgentUserID string                `json:"agentUserId"`
	Payload     GVAReportStatePayload `json:"payload"`
}

// Asks HomeGraph to re-run SYNC for a user.
type GVARequestSyncRequest struct {
	AgentUserID string `json:"agentUserId"`
	Async       bool   `json:"async"`
}

type GVAReportStatePayload struct {
	Devices GVADeviceStates `json:"devices"`
}

type GVADeviceStates struct {
	States map[string]interface{} `json:"states"`
}

// Uses the shared builder so Report State and QUERY always agree.
func GenerateGVAStateReport(customData map[string]interface{}, deviceData map[string]interface{}, online ...bool) (map[string]interface{}, error) {
	isOnline := true
	if len(online) > 0 {
		isOnline = online[0]
	}
	state := map[string]interface{}{
		"online": isOnline,
	}

	buildDeviceTraitStates(deviceData, customData, state)

	return state, nil
}
