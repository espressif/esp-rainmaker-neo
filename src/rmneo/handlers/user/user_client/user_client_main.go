// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Public API integration_type values (lowercase, matching the swagger contract).
const (
	IntegrationTypeAPNS        = "apns"
	IntegrationTypeAPNSSandbox = "apns_sandbox"
	IntegrationTypeGCM         = "gcm"
)

// Internal SNS platform values (uppercase, AWS-side). GCM is the only Firebase platform name the SNS API accepts (it never adopted Google's GCM→FCM rename), and it flows into the stored integration_id prefix (GCM_<project>) and platform application ARNs.
const (
	PlatformAPNS        = "APNS"
	PlatformAPNSSandbox = "APNS_SANDBOX"
	PlatformGCM         = "GCM"
)

// DeliveryCredentials is the structured credential bundle from the request body. Shape depends on the integration type — push uses app_token; OAuth-style integrations (alexa, gva, webhook_*) use access_token / refresh_token / expires_at / token_type.
type DeliveryCredentials struct {
	AppToken     string `json:"app_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// RegisterEndpointRequest is the PUT body for /v1/integrations/{integrationId}/endpoints.
// PUT is idempotent: re-sending the same body is a no-op; sending different
// credentials replaces the stored ones.
type RegisterEndpointRequest struct {
	DeliveryCredentials DeliveryCredentials `json:"delivery_credentials"`
	Locale              string              `json:"locale,omitempty"`
}

// EndpointStatusResponse is the response body for both PUT (register/replace) and DELETE (unregister). endpoint_id is the opaque per-endpoint identifier — callers must persist this value to later address (e.g. DELETE) the specific endpoint they registered. Per integration type: push integrations return the SNS Platform Endpoint ARN; alexa returns the Amazon user_id from LWA; webhook returns the URL or generated subscription id.
type EndpointStatusResponse struct {
	Status     string `json:"status"`
	EndpointID string `json:"endpoint_id,omitempty"`
}

// toInternalIntegrationID translates the public integration_id to the internal stored form — uppercase SNS-platform prefix for push types, unchanged otherwise. Returns (internalIntegrationID, isPush). apns_sandbox is tested before apns because they share a prefix.
func toInternalIntegrationID(integrationID string) (string, bool) {
	switch {
	case strings.HasPrefix(integrationID, IntegrationTypeAPNSSandbox+"_"):
		return PlatformAPNSSandbox + "_" + strings.TrimPrefix(integrationID, IntegrationTypeAPNSSandbox+"_"), true
	case strings.HasPrefix(integrationID, IntegrationTypeAPNS+"_"):
		return PlatformAPNS + "_" + strings.TrimPrefix(integrationID, IntegrationTypeAPNS+"_"), true
	case strings.HasPrefix(integrationID, IntegrationTypeGCM+"_"):
		return PlatformGCM + "_" + strings.TrimPrefix(integrationID, IntegrationTypeGCM+"_"), true
	default:
		return integrationID, false
	}
}

// snsPlatformParts splits an internal push integration_id into its SNS (platformType, platformAppName) pair. Returns "", "" for non-push integrations.
func snsPlatformParts(internalIntegrationID string) (platformType, platformAppName string) {
	switch {
	case strings.HasPrefix(internalIntegrationID, PlatformAPNSSandbox+"_"):
		return PlatformAPNSSandbox, strings.TrimPrefix(internalIntegrationID, PlatformAPNSSandbox+"_")
	case strings.HasPrefix(internalIntegrationID, PlatformAPNS+"_"):
		return PlatformAPNS, strings.TrimPrefix(internalIntegrationID, PlatformAPNS+"_")
	case strings.HasPrefix(internalIntegrationID, PlatformGCM+"_"):
		return PlatformGCM, strings.TrimPrefix(internalIntegrationID, PlatformGCM+"_")
	}
	return "", ""
}

func handleRegisterEndpoint(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationID := request.PathParameters["integrationId"]
	if integrationID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integrationId")), nil
	}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		rlog.Error(ctx).Msg("Failed to create request context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	if rctx.GetAccessor() == nil {
		rlog.Error(rctx).Msg("Failed to get user accessor from context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	var req RegisterEndpointRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	internalIntegrationID, isPush := toInternalIntegrationID(integrationID)

	if isPush && req.DeliveryCredentials.AppToken == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing delivery_credentials.app_token for push integration")), nil
	}
	if !isPush && req.DeliveryCredentials.AccessToken == "" && req.DeliveryCredentials.AppToken == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing delivery_credentials")), nil
	}

	entry := user_integration_db.UserIntegrationEntry{
		IntegrationID: internalIntegrationID,
		Locale:        req.Locale,
	}

	// Push types (snsPlatformByIntegrationType keys) → push branch; OAuth bundle in body (alexa, gva, webhook_*) → middle branch; raw app_token (ios-dummy, MOCK_* fixtures) → else. Normal Alexa registration happens in the AcceptGrant flow (handle_authorization.go), not here.
	if isPush {
		platformType, platformAppName := snsPlatformParts(internalIntegrationID)
		endpointArn, err := push.CreatePlatformEndpoint(ctx, platformType, platformAppName, req.DeliveryCredentials.AppToken)
		if err != nil {
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to create platform endpoint")), nil
		}
		entry.SNSEndpointARN = endpointArn
		entry.EndpointID = user_integration_db.EncodeEndpointID(endpointArn)
	} else if req.DeliveryCredentials.AccessToken != "" || req.DeliveryCredentials.RefreshToken != "" {
		entry.IntegrationToken = &user_integration_db.IntegrationToken{
			AccessToken:  req.DeliveryCredentials.AccessToken,
			RefreshToken: req.DeliveryCredentials.RefreshToken,
			ExpiresAt:    req.DeliveryCredentials.ExpiresAt,
			TokenType:    req.DeliveryCredentials.TokenType,
		}
		entry.EndpointID = user_integration_db.EncodeEndpointID(internalIntegrationID)
	} else {
		entry.SNSEndpointARN = req.DeliveryCredentials.AppToken
		entry.EndpointID = user_integration_db.EncodeEndpointID(req.DeliveryCredentials.AppToken)
	}

	if err := rctx.GetAccessor().(*user.User).RegisterClient(rctx, entry); err != nil {
		rlog.Error(rctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to register endpoint")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, EndpointStatusResponse{EndpointID: entry.EndpointID}), nil
}

func handleUnregisterEndpoint(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationID := request.PathParameters["integrationId"]
	endpointID := request.PathParameters["endpointId"]
	if integrationID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integrationId")), nil
	}
	if endpointID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing endpointId")), nil
	}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		rlog.Error(ctx).Msg("Failed to create request context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	if rctx.GetAccessor() == nil {
		rlog.Error(rctx).Msg("Failed to get user accessor from context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	internalIntegrationID, isPush := toInternalIntegrationID(integrationID)

	if isPush {
		userDB := user_integration_db.NewUserDB(rctx)
		userEntry, err := userDB.GetUserEntryByEndpoint(internalIntegrationID, endpointID)
		switch {
		// No row means no SNS endpoint left to delete; unregistering is idempotent so a retry can
		// still complete logout.
		case errors.Is(err, user_integration_db.ErrUserEntryNotFound):
			rlog.Info(rctx).Msg("no endpoint row to unregister; treating as already removed")
		case err != nil:
			rlog.Error(rctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to retrieve endpoint")), nil
		default:
			if err := push.DeletePlatformEndpoint(ctx, userEntry.SNSEndpointARN); err != nil {
				rlog.Error(rctx).Err(err).Send()
				return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to delete platform endpoint")), nil
			}
		}
	}

	if err := rctx.GetAccessor().(*user.User).UnregisterClient(rctx, internalIntegrationID, endpointID); err != nil {
		rlog.Error(rctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to unregister endpoint")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, EndpointStatusResponse{}), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	switch request.HTTPMethod {
	case "PUT":
		return handleRegisterEndpoint(ctx, request)
	case "DELETE":
		return handleUnregisterEndpoint(ctx, request)
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func main() {
	lambda.Start(handleRequest)
}
