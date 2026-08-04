// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/utils/collections"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/integrationauth"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/google/uuid"
)

const (
	AlexaPlatform   = "alexa"
	AlexaRefreshURI = "https://api.amazon.com/auth/o2/token"
)

// Alexa Smart Home skills run only in these three AWS regions; each maps to the
// regional event gateway that accepts a user linked in that region.
var alexaEventGateways = map[string]string{
	"us-east-1": "https://api.amazonalexa.com/v3/events",
	"eu-west-1": "https://api.eu.amazonalexa.com/v3/events",
	"us-west-2": "https://api.fe.amazonalexa.com/v3/events",
}

func alexaEventGatewayForRegion(region string) (string, bool) {
	gw, ok := alexaEventGateways[region]
	return gw, ok
}

type AlexaNotification struct {
	alexa_uri         string
	alexa_refresh_uri string
}

var isTestMode = false

func NewAlexaNotification(ctx context.Context, baseURL string) *AlexaNotification {

	if baseURL != "" {
		// Mock mode: route to the test webhook. Data carries the userID in the
		// "x-alexa-uuid" header.
		isTestMode = true
		return &AlexaNotification{
			alexa_uri:         baseURL + "/v1/alexa/data",
			alexa_refresh_uri: baseURL + "/v1/alexa/token",
		}
	}
	// alexa_uri is unused in production -- the gateway is chosen per-user by region.
	return &AlexaNotification{
		alexa_refresh_uri: AlexaRefreshURI,
	}
}

func (a *AlexaNotification) RefreshToken(refreshToken string) (*integrationauth.TokenResponse, error) {

	clientId, clientSecret, err := GetAlexaClientDetails(context.Background())
	if err != nil {
		return nil, err
	}

	body := []byte(fmt.Sprintf(`grant_type=refresh_token&refresh_token=%s&client_id=%s&client_secret=%s`, refreshToken, clientId, clientSecret))
	respBody, err := notification.MakeHTTPPostRequest(body, a.alexa_refresh_uri, func(req *http.Request) error {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
		return nil
	})
	if err != nil {
		return nil, err
	}

	var tokenResp integrationauth.TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to decode refresh token response")
	}

	return &tokenResp, nil
}

func (a *AlexaNotification) GetName() string {
	return AlexaPlatform
}

func (a *AlexaNotification) GetType() notification.NotificationServiceType {
	return notification.NotificationServiceTypeUserSpecific
}

func (a *AlexaNotification) Send(notif interface{}) error {
	return rmerror.NewRMError(nil, "Alexa notifications must be sent to specific users")
}

func (a *AlexaNotification) SendTo(notif interface{}, userIDs []string) error {
	rlog.Info(context.TODO()).Msgf("Sending Alexa notification to users: %v\n", userIDs)

	changeReports, ok := notif.([]AlexaResponse)
	if !ok {
		return rmerror.NewRMError(nil, "Failed to cast notification to ChangeReport")
	}

	for _, userID := range userIDs {
		// Multi-endpoint fan-out: a Rainmaker user may have linked more than one Amazon account to the skill; send the ChangeReport to each linked endpoint.
		endpoints, err := integrationauth.GetAllOAuthEndpoints(userID, AlexaPlatform)
		if err != nil {
			rlog.Error(context.TODO()).Err(err).Msgf("Failed to list Alexa endpoints for user %s", userID)
			continue
		}

		for _, endpoint := range endpoints {
			tokenData, err := integrationauth.UpdateAndGetLatestToken(userID, AlexaPlatform, endpoint.EndpointID, a.RefreshToken)
			if err != nil {
				rlog.Error(context.TODO()).Err(err).Msgf("Failed to get token data for user %s endpoint %s", userID, endpoint.EndpointID)
				continue
			}

			gateway := a.alexa_uri // mock endpoint in test mode
			if !isTestMode {
				region := ""
				if endpoint.IntegrationToken != nil {
					region = endpoint.IntegrationToken.Region
				}
				gw, ok := alexaEventGatewayForRegion(region)
				if !ok {
					rlog.Error(context.TODO()).Msgf("No Alexa event gateway for user %s endpoint %s region %q, skipping", userID, endpoint.EndpointID, region)
					continue
				}
				gateway = gw
			}

			for _, changeReport := range changeReports {
				injectAlexaScopeToken(&changeReport, tokenData.AccessToken)

				jsonData, err := json.Marshal(changeReport)
				if err != nil {
					rlog.Error(context.TODO()).Err(err).Msg("Failed to marshal change report")
					continue
				}

				_, err = notification.MakeHTTPPostRequest(jsonData, gateway, func(req *http.Request) error {
					req.Header.Set("Content-Type", "application/json")
					rlog.Info(context.TODO()).Msgf("Sending Alexa notification to user %s endpoint %s", userID, endpoint.EndpointID)
					rlog.Info(context.TODO()).Msgf("the data is %s", string(jsonData))
					req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokenData.AccessToken))
					if isTestMode {
						req.Header.Set("x-alexa-uuid", userID)
					}
					return nil
				})

				if err != nil {
					rlog.Error(context.TODO()).Err(err).Msgf("Failed to send Alexa notification to user %s endpoint %s", userID, endpoint.EndpointID)
					continue
				}
			}
		}
		rlog.Info(context.TODO()).Msgf("Successfully sent Alexa notifications to user %s", userID)
	}

	return nil
}

func (a *AlexaNotification) Marshal(notif *notification.Notification) (interface{}, error) {
	rlog.Debug(context.TODO()).Interface("notif", notif).Msg("In Marshalling")

	// Group membership change -> proactive Alexa.Discovery report.
	if notif.NotificationType == notification.NotificationTypeGroupMembership {
		return a.marshalGroupMembership(notif)
	}

	if notif.NotificationType != "shadow_update" {
		return nil, rmerror.NewRMError(nil, "Unsupported notification type for Alexa")
	}

	var shadowUpdate *notification.ShadowUpdateNotification
	var nodeID string

	// Alexa notifications only support shadow updates as they require device state information
	if notif.NotificationType != notification.NotificationTypeShadowUpdate {
		if notif.NotificationType == notification.NotificationTypeDirect {
			rlog.Info(context.TODO()).Msg("Direct notifications are not supported for Alexa service")
			return nil, rmerror.NewRMError(nil, "Direct notifications are not supported for Alexa service - use shadow updates instead")
		}
		return nil, rmerror.NewRMError(nil, fmt.Sprintf("Unsupported notification type for Alexa: %s", notif.NotificationType))
	}

	// Process shadow update notification
	if notif.ShadowUpdateData == nil {
		return nil, rmerror.NewRMError(nil, "Shadow update data is nil")
	}
	shadowUpdate = notif.ShadowUpdateData
	nodeID = shadowUpdate.NodeID

	// Get node configuration to determine supported properties
	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())
	nodeCfg, err := getNodeCfg(rmngCtx, nodeID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to get node configuration")
	}

	rlog.Info(rmngCtx).Msgf("Node configuration: %+v\n", nodeCfg)

	result := []AlexaResponse{}

	// Use the already-parsed group ID from the notification
	groupID := notif.GroupID

	// NOTE (WWA follow-up): this ChangeReport is only produced on a device param change (a
	// notify.version bump in the shadow). The connectivity value below is reported correctly
	// within that report, but a standalone *connectivity state report* — a ChangeReport pushed
	// proactively when the node itself goes offline/online (a reported.online transition, which
	// carries no param delta) — is NOT handled yet. That proactive push is required for WWA
	// (Works With Alexa) certification but is not required for Alexa skill certification, so it is
	// intentionally out of scope here. Adding it needs the shadow_notify_rule to fire on a
	// reported.online transition plus an online-only-delta branch in this loop.
	for _, device := range nodeCfg.Devices {
		changeReport, changeReportPayload := createEmptyChangeReport()
		var contextProperties ContextPropertyList
		changeProperties := ContextPropertyList{}

		changeReport.Event.Endpoint.EndpointID = GetEndpointId(shadowUpdate.NodeID, device.ID)
		rlog.Info(rmngCtx).Interface("Device", device).Msg("In Marshalling")
		rlog.Info(rmngCtx).Interface("State", shadowUpdate.State).Msg("In Marshalling")

		// Generate a cookie that is used to encode the device capabilities
		_, cookie, err := GetDeviceCapabilities(device, groupID)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Msgf("Failed to get device capabilities for device %s", device.ID)
			continue
		}
		rlog.Info(rmngCtx).Interface("cookie", cookie).Send()

		// Ecncode current device state
		if currDevState, ok := shadowUpdate.State.Params[device.ID]; ok {
			contextProperties, err = GenerateCapabilityReportForState(cookie, currDevState.(map[string]interface{}), GetEndpointConnectivity(shadowUpdate.State))
			if err != nil {
				rlog.Error(rmngCtx).Err(err).Msgf("Failed to generate capability report for device %s", device.ID)
				return nil, rmerror.NewRMError(err, "Failed to generate capability report")
			}
		} else {
			rlog.Error(rmngCtx).Msgf("Device %s not found in current state, skipping", device.ID)
			continue
		}

		// Encode delta device state - the "change"
		deltaDevState, ok := shadowUpdate.Delta.Params[device.ID]
		if ok {
			// Generate the Change Report from the delta state of the device
			ConvertCurrentStateToCtxProperty(deltaDevState.(map[string]interface{}), cookie, &changeProperties)
		} else {
			rlog.Error(rmngCtx).Msgf("Device %s not found in delta, skipping", device.ID)
			continue
		}

		changeReport.Context.Properties = contextProperties
		changeReportPayload.Change.Properties = changeProperties

		filteredProperties := collections.SubtractListBFromListA(changeReport.Context.Properties, changeReportPayload.Change.Properties)
		changeReport.Context.Properties = filteredProperties

		var changeReportPayloadInterface interface{} = changeReportPayload
		changeReport.Event.Payload = &changeReportPayloadInterface

		result = append(result, changeReport)
	}

	return result, nil
}

// marshalGroupMembership builds an Alexa.Discovery report for a node's group
// membership change: AddOrUpdateReport when the node was added (now
// discoverable) and DeleteReport when it was removed. The per-endpoint bearer
// token is filled in later, per user, by SendTo (see injectAlexaScopeToken).
func (a *AlexaNotification) marshalGroupMembership(notif *notification.Notification) (interface{}, error) {
	if notif.GroupMembershipData == nil {
		return nil, rmerror.NewRMError(nil, "Group membership data is nil")
	}
	nodeID := notif.GroupMembershipData.NodeID
	action := notif.GroupMembershipData.Action
	groupID := notif.GroupID

	rmngCtx := rmngctx.NewRmngContextWithCtx(context.Background(), utils.NewSystemActor())

	switch action {
	case notification.GroupMembershipActionAdded:
		// Build endpoints WITHOUT the discovery-time side effects
		// (UpdateAlexaEnabled / SendAlexaEnabled). A group-membership change must
		// not push a getAlexaEn event to the device — only a user-initiated Alexa
		// discovery does that. Users without Alexa linked simply get nothing when
		// SendTo finds no Alexa endpoints.
		nodeCfg, err := getNodeCfg(rmngCtx, nodeID)
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to get node configuration")
		}
		endpoints, err := buildDiscoveryEndpoints(rmngCtx, nodeID, groupID, nodeCfg)
		if err != nil {
			return nil, rmerror.NewRMError(err, "failed to build discovery endpoints for AddOrUpdateReport")
		}
		if len(endpoints) == 0 {
			rlog.Info(rmngCtx).Str("nodeID", nodeID).Msg("No Alexa endpoints for node, skipping AddOrUpdateReport")
			return []AlexaResponse{}, nil
		}
		return []AlexaResponse{newDiscoveryReport("AddOrUpdateReport", &AddOrUpdateReportPayload{Endpoints: endpoints})}, nil

	case notification.GroupMembershipActionRemoved:
		nodeCfg, err := getNodeCfg(rmngCtx, nodeID)
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to get node configuration")
		}
		deleteEndpoints := make([]DeleteReportEndpoint, 0, len(nodeCfg.Devices))
		for _, device := range nodeCfg.Devices {
			deleteEndpoints = append(deleteEndpoints, DeleteReportEndpoint{EndpointID: GetEndpointId(nodeID, device.ID)})
		}
		if len(deleteEndpoints) == 0 {
			rlog.Info(rmngCtx).Str("nodeID", nodeID).Msg("No Alexa endpoints for node, skipping DeleteReport")
			return []AlexaResponse{}, nil
		}
		return []AlexaResponse{newDiscoveryReport("DeleteReport", &DeleteReportPayload{Endpoints: deleteEndpoints})}, nil

	default:
		return nil, rmerror.NewRMError(nil, "unsupported group membership action for Alexa: "+action)
	}
}

// newDiscoveryReport wraps a discovery payload (pointer, so SendTo can set the
// scope token in place) into an Alexa.Discovery AlexaResponse.
func newDiscoveryReport(name string, payload interface{}) AlexaResponse {
	payloadIface := payload
	return AlexaResponse{
		Event: Event{
			Header: Header{
				Namespace:      "Alexa.Discovery",
				Name:           name,
				MessageID:      uuid.New().String(),
				PayloadVersion: "3",
			},
			Payload: &payloadIface,
		},
	}
}

// injectAlexaScopeToken places the per-endpoint bearer token where each report
// type expects it: on the endpoint scope for a ChangeReport, or in the payload
// scope for an Alexa.Discovery AddOrUpdate/DeleteReport.
func injectAlexaScopeToken(report *AlexaResponse, token string) {
	if report.Event.Endpoint != nil && report.Event.Endpoint.Scope != nil {
		report.Event.Endpoint.Scope.Token = token
		return
	}
	if report.Event.Payload == nil {
		return
	}
	switch p := (*report.Event.Payload).(type) {
	case *AddOrUpdateReportPayload:
		p.Scope = &Scope{Type: "BearerToken", Token: token}
	case *DeleteReportPayload:
		p.Scope = &Scope{Type: "BearerToken", Token: token}
	}
}

func createEmptyChangeReport() (AlexaResponse, ChangeReportPayload) {

	// Create the ChangeReport
	changeReportPayload := ChangeReportPayload{
		Change: ChangeReportChange{
			Cause: ChangeReportCause{
				Type: "PHYSICAL_INTERACTION",
			},
			Properties: []ContextProperty{},
		},
	}
	changeReport := AlexaResponse{
		Event: Event{
			Header: Header{
				Namespace:      "Alexa",
				Name:           "ChangeReport",
				MessageID:      uuid.New().String(),
				PayloadVersion: "3",
			},
			Endpoint: &Endpoint{
				EndpointID: "will_be_overwritten",
				Scope: &Scope{
					Type:  "BearerToken",
					Token: "dummy-token", // Will be replaced with actual token in SendTo
				},
			},
		},
		Context: &Context{
			Properties: []ContextProperty{},
		},
	}
	return changeReport, changeReportPayload
}
