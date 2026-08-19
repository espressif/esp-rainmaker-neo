// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/gva"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type GVACfgStoreResponse struct {
	ProjectID    string   `json:"project_id"`
	RedirectURIs []string `json:"redirect_uris"`
	Message      string   `json:"message"`
}

type GVACfgResponse struct {
	Message string `json:"message"`
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rlog.Info(ctx).Interface("request", request).Send()

	rctx := user.NewContextWithAPIRequest(ctx, request)

	isAuthorized := rctx.GetAccessor().(*user.User).IsSuperAdmin(rctx)
	if !isAuthorized {
		rlog.Error(rctx).Bool("isAuthorized", isAuthorized).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	var response events.APIGatewayProxyResponse
	switch request.HTTPMethod {
	case "POST":
		response = handleStoreConfig(rctx, request)
	case "GET":
		response = handleGetConfig(rctx, request)
	case "DELETE":
		response = handleDeleteConfig(rctx, request)
	default:
		response = utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed"))
	}

	return response, nil
}

// updateVAClientRedirectURIs registers redirectURIs on the OIDC va-client registry row, shared by Alexa and GVA (union semantics; other fields preserved).
func updateVAClientRedirectURIs(ctx context.Context, clientID string, redirectURIs []string) error {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if _, err := svc.AddRedirectURIs(clientID, redirectURIs); err != nil {
		return rmerror.NewRMError(err, "failed to add OIDC va-client redirect URIs")
	}
	return nil
}

func handleStoreConfig(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	var configRequest gva.ServiceAccount
	if err := rmngrequest.ExtractRequestStruct(request, &configRequest); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to parse/validate config request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error()))
	}

	// Calculate redirect URI from project ID
	redirectURI := "https://oauth-redirect.googleusercontent.com/r/" + configRequest.ProjectID
	redirectURIs := []string{redirectURI}

	// Register GVA's redirect URI against the OIDC va-client registry row.
	vaClientID := os.Getenv("OIDC_VA_CLIENT_ID")
	if vaClientID == "" {
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("OIDC_VA_CLIENT_ID not configured"))
	}

	err := updateVAClientRedirectURIs(ctx, vaClientID, redirectURIs)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to update OIDC va-client")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to update OIDC va-client"))
	}

	// Store the entire service account JSON as a single SSM parameter
	err = ssmutil.StoreParameter(ctx, gva.GVASSMServiceAccountJSONParam, request.Body)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to store GVA service account config")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to store service account configuration"))
	}

	return utils.APIGwRespJSON(http.StatusOK, GVACfgStoreResponse{
		ProjectID:    configRequest.ProjectID,
		RedirectURIs: redirectURIs,
		Message:      "GVA client configuration stored successfully",
	})
}

// GVAGetCfgResponse deliberately carries no private_key field: the stored secret is write-only and must never leave the backend via GET (M-13).
type GVAGetCfgResponse struct {
	Type                    string   `json:"type"`
	ProjectID               string   `json:"project_id"`
	PrivateKeyID            string   `json:"private_key_id"`
	ClientEmail             string   `json:"client_email"`
	ClientID                string   `json:"client_id"`
	AuthURI                 string   `json:"auth_uri"`
	TokenURI                string   `json:"token_uri"`
	AuthProviderX509CertURL string   `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string   `json:"client_x509_cert_url"`
	UniverseDomain          string   `json:"universe_domain"`
	RedirectURIs            []string `json:"redirect_uris"`
}

func handleGetConfig(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	// Get GVA service account JSON from SSM
	serviceAccountJSON, err := ssmutil.GetParameterWithCaching(ctx, gva.GVASSMServiceAccountJSONParam, true, false)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to get GVA service account config")
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("GVA configuration not found"))
	}

	var saCfg GVAGetCfgResponse
	if err := json.Unmarshal([]byte(serviceAccountJSON), &saCfg); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to parse stored service account config")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to parse stored configuration"))
	}

	if saCfg.ProjectID != "" {
		saCfg.RedirectURIs = []string{"https://oauth-redirect.googleusercontent.com/r/" + saCfg.ProjectID}
	}

	return utils.APIGwRespJSON(http.StatusOK, saCfg)
}

func handleDeleteConfig(ctx context.Context, request events.APIGatewayProxyRequest) events.APIGatewayProxyResponse {
	// Delete GVA client details from SSM
	err := deleteGVAClientDetails(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to delete GVA client details")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("failed to delete configuration"))
	}

	return utils.APIGwRespJSON(http.StatusOK, GVACfgResponse{Message: "GVA client configuration deleted successfully"})
}

func deleteGVAClientDetails(ctx context.Context) error {
	err := ssmutil.DeleteParameter(ctx, gva.GVASSMServiceAccountJSONParam)
	if err != nil {
		return rmerror.NewRMError(err, "failed to delete GVA service account config")
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
