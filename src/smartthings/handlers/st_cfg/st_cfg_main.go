// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ssm_types "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const (
	STSSMClientIDParam     = "/rmng/smartthings/client_id"
	STSSMClientSecretParam = "/rmng/smartthings/client_secret"

	// The client id SmartThings issues is a UUID; 256 leaves ample room.
	maxClientIDLength = 256
	// The secret is 256 bytes hex-encoded, so 512 characters. The bound is set
	// well above that rather than at it, since only SmartThings decides the
	// format, and stays far below the 4096-character SSM Standard-tier limit
	// these values are stored under.
	maxClientSecretLength = 1024
)

// SmartThings cloud-to-cloud OAuth callback URLs, one per SmartThings geo.
// Registered on the shared OIDC va-client row so account linking works from
// any SmartThings region (union semantics; Alexa/GVA URIs are preserved).
var stRedirectURIs = []string{
	"https://c2c-us.smartthings.com/oauth/callback",
	"https://c2c-eu.smartthings.com/oauth/callback",
	"https://c2c-ap.smartthings.com/oauth/callback",
}

// updateVAClientRedirectURIs registers redirectURIs on the OIDC va-client registry row, shared by Alexa, GVA and SmartThings (union semantics; other fields preserved).
func updateVAClientRedirectURIs(ctx context.Context, clientID string, redirectURIs []string) error {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if _, err := svc.AddRedirectURIs(clientID, redirectURIs); err != nil {
		return rmerror.NewRMError(err, "failed to add OIDC va-client redirect URIs")
	}
	return nil
}

type STCfgRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type STCfgGetResponse struct {
	ClientID string `json:"client_id"`
}

type STCfgResponse struct {
	Message string `json:"message"`
}

type STCfgValidationError struct {
	Message string `json:"message"`
	Field   string `json:"field"`
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)

	isAuthorized := rctx.GetAccessor().(*user.User).IsSuperAdmin(rctx)
	if !isAuthorized {
		rlog.Error(ctx).Bool("isAuthorized", isAuthorized).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	var response events.APIGatewayProxyResponse
	switch request.HTTPMethod {
	case "POST":
		response = handleStoreConfig(ctx, request)
	case "GET":
		response = handleGetConfig(ctx)
	case "DELETE":
		response = handleDeleteConfig(ctx)
	default:
		response = utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"))
	}

	return response, nil
}

func validateField(value, fieldName string, maxLength int) *STCfgValidationError {
	if value == "" {
		return &STCfgValidationError{
			Message: fieldName + " must not be empty",
			Field:   fieldName,
		}
	}
	if len(value) > maxLength {
		return &STCfgValidationError{
			Message: fmt.Sprintf("%s must not exceed %d characters", fieldName, maxLength),
			Field:   fieldName,
		}
	}
	return nil
}

func handleStoreConfig(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	var cfgRequest STCfgRequest
	if err := json.Unmarshal([]byte(request.Body), &cfgRequest); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to parse config request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("invalid request body"))
	}

	// Validate Client ID
	if validationErr := validateField(cfgRequest.ClientID, "client_id", maxClientIDLength); validationErr != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, validationErr)
	}

	// Validate Client Secret
	if validationErr := validateField(cfgRequest.ClientSecret, "client_secret", maxClientSecretLength); validationErr != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, validationErr)
	}

	// Register the SmartThings callback URLs against the OIDC va-client registry row.
	vaClientID := os.Getenv("OIDC_VA_CLIENT_ID")
	if vaClientID == "" {
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("OIDC_VA_CLIENT_ID not configured"))
	}
	if err := updateVAClientRedirectURIs(ctx, vaClientID, stRedirectURIs); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to register SmartThings redirect URIs")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to store configuration"))
	}

	// Store Client ID as String type
	err := ssmutil.StoreParameterWithType(ctx, STSSMClientIDParam, cfgRequest.ClientID, ssm_types.ParameterTypeString)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to store SmartThings client ID")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to store configuration"))
	}

	// Store Client Secret as SecureString type
	err = ssmutil.StoreParameterWithType(ctx, STSSMClientSecretParam, cfgRequest.ClientSecret, ssm_types.ParameterTypeSecureString)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to store SmartThings client secret")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to store configuration"))
	}

	return utils.APIGwRespJSON(http.StatusOK, STCfgResponse{Message: "SmartThings configuration stored successfully"})
}

func handleGetConfig(ctx context.Context) events.APIGatewayProxyResponse {
	clientID, err := ssmutil.GetParameterWithCaching(ctx, STSSMClientIDParam, false, false)
	if err != nil {
		// An absent parameter means the integration has never been configured,
		// which is a 404 rather than a fault: clients (the dashboard among them)
		// distinguish "not set up yet" from "lookup failed" by the status code.
		if ssmutil.IsParameterNotFound(err) {
			return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("SmartThings configuration not found"))
		}
		rlog.Error(ctx).Err(err).Msg("failed to get SmartThings client ID")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to retrieve configuration"))
	}

	return utils.APIGwRespJSON(http.StatusOK, STCfgGetResponse{ClientID: clientID})
}

func handleDeleteConfig(ctx context.Context) events.APIGatewayProxyResponse {
	err := ssmutil.DeleteParameter(ctx, STSSMClientIDParam)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to delete SmartThings client ID")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to delete configuration"))
	}

	err = ssmutil.DeleteParameter(ctx, STSSMClientSecretParam)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to delete SmartThings client secret")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to delete configuration"))
	}

	return utils.APIGwRespJSON(http.StatusOK, STCfgResponse{Message: "SmartThings configuration deleted successfully"})
}

func main() {
	lambda.Start(handler)
}
