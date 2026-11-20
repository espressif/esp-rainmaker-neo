// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	alexa_skill "github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/principal"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
)

/* Ideally this REST API should only be available to an administrative user. We do not have a concept of an administrative user yet.
   TODO: So currently this functionality is available to all users :-D, will be updated soon
*/

type AlexaCfgResponse struct {
	Message string `json:"message"`
}

type AlexaCfgRequest struct {
	RedirectURIs []string `json:"redirect_uris"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	SkillID      string   `json:"skill_id"`
	// ManufacturerName is the brand advertised in Alexa discovery, which WWA review requires to
	// be a real manufacturer. A pointer so that omitting the field leaves the stored brand
	// alone: callers that only rotate credentials must not silently reset an OEM's branding.
	// Sending it empty clears the override and restores the default brand.
	ManufacturerName *string `json:"manufacturer_name,omitempty"`
}

func handleAlexaCfg(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req AlexaCfgRequest
	err := rmngrequest.ExtractRequestStruct(request, &req)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	// Validate required fields
	if len(req.RedirectURIs) == 0 {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing redirect URIs")), nil
	}
	if req.ClientID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing client ID")), nil
	}
	if req.ClientSecret == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing client secret")), nil
	}
	if req.SkillID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing skill ID")), nil
	}

	// Register Alexa's redirect URIs against the OIDC va-client registry row.
	vaClientID := os.Getenv("OIDC_VA_CLIENT_ID")
	if vaClientID == "" {
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("OIDC_VA_CLIENT_ID not configured")), nil
	}

	err = alexa_skill.UpdateVAClientRedirectURIs(ctx, vaClientID, req.RedirectURIs)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to update OIDC va-client")), nil
	}

	// Store client ID and secret
	err = alexa_skill.StoreAlexaClientDetails(ctx, req.ClientID, req.ClientSecret, req.SkillID)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store client ID/Secret")), nil
	}

	// Store the discovery brand only when the caller supplied one, so a credentials-only update
	// leaves an existing OEM brand in place.
	if req.ManufacturerName != nil {
		err = alexa_skill.StoreAlexaManufacturerName(ctx, strings.TrimSpace(*req.ManufacturerName))
		if err != nil {
			rlog.Error(ctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store manufacturer name")), nil
		}
	}

	// Add Alexa trigger to alexa_skill_<RMNG_REGION> in each Alexa region (must match AlexaStack naming).
	rmngRegion := os.Getenv("RMNG_REGION")
	if rmngRegion == "" {
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("RMNG_REGION not configured")), nil
	}
	if err := alexa_skill.AddAlexaTriggerAllRegions(ctx, rmngRegion, req.SkillID); err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to add Alexa trigger: "+rmerror.TopMessage(err))), nil
	}

	alexa_skill.ClearSetupStatus(ctx)
	return utils.APIGwRespJSON(http.StatusOK, AlexaCfgResponse{Message: "success"}), nil
}

type AlexaCfgGetResponse struct {
	ClientID     string   `json:"client_id"`
	SkillID      string   `json:"skill_id"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	// ManufacturerName is the brand currently advertised in discovery — the configured value, or
	// the default brand when a deployment has set none. Never empty.
	ManufacturerName string `json:"manufacturer_name"`
	// ConfigStatus is the last automatic setup's status, absent when none has run.
	ConfigStatus *alexa_skill.SetupStatus `json:"config_status,omitempty"`
	// LWAClientID is the connected Amazon developer account's security profile, absent when none is connected. Its secret and refresh token are never returned.
	LWAClientID string `json:"lwa_client_id,omitempty"`
}

func handleGetAlexaCfg(ctx context.Context) (events.APIGatewayProxyResponse, error) {
	configStatus := alexa_skill.GetSetupStatus(ctx)
	lwaClientID := alexa_skill.ConnectedLWAClientID(ctx)

	// Get Alexa client details (only return client ID for security)
	clientID, _, err := alexa_skill.GetAlexaClientDetails(ctx)
	if err != nil {
		// A setup still running, or an account connected for the first one, has written no credentials yet, so answering 404 would hide exactly what the caller is waiting on.
		if configStatus != nil || lwaClientID != "" {
			return utils.APIGwRespJSON(http.StatusOK, AlexaCfgGetResponse{
				ManufacturerName: alexa_skill.GetAlexaManufacturerName(ctx),
				ConfigStatus:     configStatus,
				LWAClientID:      lwaClientID,
			}), nil
		}
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Alexa configuration not found")), nil
	}

	response := AlexaCfgGetResponse{
		ClientID:         clientID,
		ManufacturerName: alexa_skill.GetAlexaManufacturerName(ctx),
		ConfigStatus:     configStatus,
		LWAClientID:      lwaClientID,
	}

	// Get skill ID
	skillID, err := ssmutil.GetParameterWithCaching(ctx, alexa_skill.AlexaSSMSkillIDParam, true, false)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Failed to get Alexa skill ID")), nil
	} else {
		response.SkillID = skillID
	}

	// Get redirect URIs from the OIDC va-client registry row.
	vaClientID := os.Getenv("OIDC_VA_CLIENT_ID")
	if vaClientID != "" {
		callbackURLs, err := alexa_skill.VAClientRedirectURIs(ctx, vaClientID)
		if err != nil {
			rlog.Error(ctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to get OIDC va-client redirect URIs")), nil
		}
		response.RedirectURIs = callbackURLs
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := principal.FromAPIRequest(ctx, request)
	if rctx == nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	// A caller the admin pool does not know leaves the accessor nil; a bare assertion would panic and API Gateway would answer 502 instead of 403.
	caller, ok := rctx.GetAccessor().(*user.User)
	if !ok || caller == nil || !caller.IsAdmin(rctx) {
		rlog.Error(rctx).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	switch request.HTTPMethod {
	case "POST":
		return handleAlexaCfg(rctx, request)
	case "GET":
		return handleGetAlexaCfg(rctx)
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func main() {
	awslambda.Start(handleRequest)
}
