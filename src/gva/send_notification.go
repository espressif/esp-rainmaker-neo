// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package gva

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	alexa_skill "github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/integrationauth"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/testmock"
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

type GVANotification struct{}

func NewGVANotification() *GVANotification {
	return &GVANotification{}
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

// getMockToken fetches a token from the itest webhook mock's token endpoint.
func getMockToken(ctx context.Context, tokenURI string) (string, error) {
	respBody, err := notification.MakeHTTPPostRequest(ctx, []byte("{}"), tokenURI, func(req *http.Request) error {
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

// gvaRoute is where a group of recipients' HomeGraph calls go.
type gvaRoute struct {
	reportURI string
	syncURI   string
	token     string
}

func resolveHomeGraphRoute(ctx context.Context) (gvaRoute, error) {
	token, err := GetGVAToken(ctx)
	if err != nil {
		return gvaRoute{}, rmerror.NewRMError(err, "failed to get HomeGraph token")
	}
	return gvaRoute{reportURI: ReportStateEndpoint, syncURI: RequestSyncEndpoint, token: token}, nil
}

// resolveMockRoute never falls back to HomeGraph: an itest recipient with no mock deployed is skipped.
func resolveMockRoute(ctx context.Context) (gvaRoute, error) {
	cfg, ok := testmock.Get(ctx)
	if !ok {
		return gvaRoute{}, rmerror.NewRMError(nil, "itest recipient but no itest webhook mock is deployed in this account")
	}
	token, err := getMockToken(ctx, cfg.URL("/v1/gva/token"))
	if err != nil {
		return gvaRoute{}, err
	}
	// The mock exposes a single data endpoint for both calls.
	return gvaRoute{reportURI: cfg.URL("/v1/gva/data"), syncURI: cfg.URL("/v1/gva/data"), token: token}, nil
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

func (g *GVANotification) Send(ctx context.Context, notif interface{}) error {
	return rmerror.NewRMError(nil, "GVA notifications must be sent to specific users")
}

// splitLinkedUsers drops users who never linked Google, so they cost no HomeGraph call, and splits the rest by whether an itest created their link. An unlinked user surfaces as the lookup's not-found error, so errors are not logged per user.
func splitLinkedUsers(userIDs []string) (homeGraph, itest []string) {
	for _, userID := range userIDs {
		endpoints, err := integrationauth.GetAllOAuthEndpoints(userID, GVAPlatform)
		if err != nil || len(endpoints) == 0 {
			continue
		}
		// One link row per user: SYNC and the endpoints API both write the fixed gvaLinkEndpointID.
		if endpoints[0].TestMock {
			itest = append(itest, userID)
		} else {
			homeGraph = append(homeGraph, userID)
		}
	}
	return homeGraph, itest
}

func (g *GVANotification) SendTo(ctx context.Context, notif interface{}, userIDs []string) error {
	var reports []GVAReportStateRequest
	var syncs []GVARequestSyncRequest
	switch data := notif.(type) {
	case GVANotificationBatch:
		reports = data.Reports
		if data.RequestSync {
			syncs = []GVARequestSyncRequest{{Async: false}}
		}
	case []GVARequestSyncRequest:
		syncs = data
	default:
		return rmerror.NewRMError(nil, "Failed to cast GVA notification to a known request type")
	}
	// Most shadow updates change nothing Google tracks, and each route resolution is an uncached token request.
	if len(reports) == 0 && len(syncs) == 0 {
		return nil
	}

	homeGraph, itest := splitLinkedUsers(userIDs)
	if len(homeGraph)+len(itest) == 0 {
		rlog.Info(ctx).Msg("No GVA-linked users among the recipients, skipping")
		return nil
	}
	rlog.Info(ctx).Msgf("Sending GVA notification to %d users", len(homeGraph)+len(itest))
	g.sendToGroup(ctx, homeGraph, resolveHomeGraphRoute, reports, syncs)
	g.sendToGroup(ctx, itest, resolveMockRoute, reports, syncs)
	return nil
}

// sendToGroup resolves the route once for the group, so a missing token or mock fails once per send rather than once per user.
func (g *GVANotification) sendToGroup(ctx context.Context, userIDs []string, resolve func(context.Context) (gvaRoute, error), reports []GVAReportStateRequest, syncs []GVARequestSyncRequest) {
	if len(userIDs) == 0 {
		return
	}
	route, err := resolve(ctx)
	if err != nil {
		rlog.Warn(ctx).Err(err).Int("users", len(userIDs)).Msg("Skipping GVA notification")
		return
	}
	if len(reports) > 0 {
		g.sendReportState(ctx, reports, userIDs, route)
	}
	if len(syncs) > 0 {
		g.sendRequestSync(ctx, syncs, userIDs, route)
	}
}

// Makes Google re-run SYNC to reconcile the user's device list after a membership change or rename.
func (g *GVANotification) sendRequestSync(ctx context.Context, requests []GVARequestSyncRequest, userIDs []string, route gvaRoute) {
	for _, userID := range userIDs {
		for _, req := range requests {
			req.AgentUserID = userID
			jsonData, err := json.Marshal(req)
			if err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("Failed to marshal request sync")
				continue
			}
			_, err = notification.MakeHTTPPostRequest(ctx, jsonData, route.syncURI, func(r *http.Request) error {
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Authorization", route.token)
				return nil
			})
			if err != nil {
				rlog.Error(ctx).Err(err).Str("userID", userID).Msg("Failed to send GVA request sync")
				continue
			}
			rlog.Info(ctx).Str("userID", userID).Msg("Successfully sent GVA request sync")
		}
	}
}

func (g *GVANotification) sendReportState(ctx context.Context, reportStateRequests []GVAReportStateRequest, userIDs []string, route gvaRoute) {
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

			respBody, err := notification.MakeHTTPPostRequest(ctx, jsonData, route.reportURI, func(req *http.Request) error {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", route.token)
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
