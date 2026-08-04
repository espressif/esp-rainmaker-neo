// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification/push"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
)

// Internal SNS platform values (what AWS SNS uses).
const (
	PlatformAPNS        = "APNS"
	PlatformAPNSSandbox = "APNS_SANDBOX"
	PlatformGCM         = "GCM"
)

// Public API integration_type values (what the swagger exposes — lowercase).
const (
	IntegrationTypeAPNS        = "apns"
	IntegrationTypeAPNSSandbox = "apns_sandbox"
	IntegrationTypeGCM         = "gcm"
)

// googleServiceAccount is the standard Google Cloud service-account JSON
// shape, used by both GCM (Firebase) and GVA (HomeGraph) integrations.
type googleServiceAccount struct {
	Type                    string `json:"type,omitempty"`
	ProjectID               string `json:"project_id,omitempty"`
	PrivateKeyID            string `json:"private_key_id,omitempty"`
	PrivateKey              string `json:"private_key,omitempty"`
	ClientEmail             string `json:"client_email,omitempty"`
	ClientID                string `json:"client_id,omitempty"`
	AuthURI                 string `json:"auth_uri,omitempty"`
	TokenURI                string `json:"token_uri,omitempty"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url,omitempty"`
	ClientX509CertURL       string `json:"client_x509_cert_url,omitempty"`
	UniverseDomain          string `json:"universe_domain,omitempty"`
}

// RegisterIntegrationRequest is the POST body for /v1/admin/integrations.
// integration_type comes from the query parameter; the body carries only
// credentials, in one of two shapes:
//   - APNS / APNS_SANDBOX: authentication_key + key_id + team_id + bundle_id
//   - GCM: a flat Google service-account JSON (same shape as GVA)
type RegisterIntegrationRequest struct {
	// Token-based auth for APNS / APNS_SANDBOX
	AuthenticationKey string `json:"authentication_key,omitempty"`
	KeyID             string `json:"key_id,omitempty"`
	TeamID            string `json:"team_id,omitempty"`
	BundleID          string `json:"bundle_id,omitempty"`

	// GCM service-account JSON (embedded — same shape as GVA)
	googleServiceAccount
}

// RegisterIntegrationResponse is the POST response.
type RegisterIntegrationResponse struct {
	IntegrationID string `json:"integration_id"`
}

// ListIntegrationsResponse is the GET (list) response.
// Each entry is the full per-integration detail shape — same as GET (one).
type ListIntegrationsResponse struct {
	Integrations []GetIntegrationResponse `json:"integrations"`
}

// PublicIntegrationSummary is one entry of the non-admin list response:
// just the identifiers an app needs to register an endpoint, no credentials.
type PublicIntegrationSummary struct {
	IntegrationID   string `json:"integration_id"`
	IntegrationType string `json:"integration_type"`
}

// ListPublicIntegrationsResponse is the GET /v1/integrations (non-admin) response.
type ListPublicIntegrationsResponse struct {
	Integrations []PublicIntegrationSummary `json:"integrations"`
}

// GetIntegrationResponse is the GET (one) response. Per-type fields are
// populated based on integration_type. For APNS, only the bundle_id is
// returned (auth_key is secret). For GCM, the full Google service-account
// JSON is returned verbatim (mirrors GVA's GetCfg response shape).
type GetIntegrationResponse struct {
	IntegrationID   string `json:"integration_id"`
	IntegrationType string `json:"integration_type"`

	// APNS / APNS_SANDBOX
	BundleID string `json:"bundle_id,omitempty"`

	// GCM: embed the full Google service-account JSON (matches the request shape).
	googleServiceAccount
}

type PlatformAttributesInput struct {
	PlatformType string
	PlatformName string

	// APNS
	AuthKey  string
	KeyID    string
	TeamID   string
	BundleID string

	// GCM (Google service-account)
	GSA googleServiceAccount
}

// integrationTypeToSNSPlatform maps the public lowercase integration_type
// to the uppercase SNS platform identifier used internally.
func integrationTypeToSNSPlatform(integrationType string) (string, error) {
	switch integrationType {
	case IntegrationTypeAPNS:
		return PlatformAPNS, nil
	case IntegrationTypeAPNSSandbox:
		return PlatformAPNSSandbox, nil
	case IntegrationTypeGCM:
		return PlatformGCM, nil
	default:
		return "", rmerror.NewRMError(nil, "Unsupported integration_type: "+integrationType)
	}
}

// snsPlatformToIntegrationType is the inverse of integrationTypeToSNSPlatform.
// Non-push platform values (alexa, web, gva, …) are returned unchanged.
func snsPlatformToIntegrationType(snsPlatform string) string {
	switch snsPlatform {
	case PlatformAPNS:
		return IntegrationTypeAPNS
	case PlatformAPNSSandbox:
		return IntegrationTypeAPNSSandbox
	case PlatformGCM:
		return IntegrationTypeGCM
	default:
		return snsPlatform
	}
}

// buildIntegrationID constructs the public-form integration_id.
// Format: {integration_type}_{platform_app_name} for push types,
// or just {integration_type} for non-push types.
func buildIntegrationID(integrationType, platformAppName string) string {
	if platformAppName == "" {
		return integrationType
	}
	return integrationType + "_" + platformAppName
}

// parseIntegrationID parses the public-form integration_id back to
// (integrationType, platformAppName). For non-push types (alexa, web, …)
// platformAppName is empty and integrationType is the whole id.
//
// Order matters: apns_sandbox_ must be checked before apns_.
func parseIntegrationID(integrationID string) (integrationType, platformAppName string, err error) {
	if integrationID == "" {
		return "", "", rmerror.NewRMError(nil, "Missing integrationId")
	}

	if strings.HasPrefix(integrationID, IntegrationTypeAPNSSandbox+"_") {
		name := integrationID[len(IntegrationTypeAPNSSandbox+"_"):]
		if name == "" {
			return "", "", rmerror.NewRMError(nil, "Invalid integrationId format: missing platform name after apns_sandbox_")
		}
		return IntegrationTypeAPNSSandbox, name, nil
	}

	if strings.HasPrefix(integrationID, IntegrationTypeAPNS+"_") {
		name := integrationID[len(IntegrationTypeAPNS+"_"):]
		if name == "" {
			return "", "", rmerror.NewRMError(nil, "Invalid integrationId format: missing platform name after apns_")
		}
		return IntegrationTypeAPNS, name, nil
	}

	if strings.HasPrefix(integrationID, IntegrationTypeGCM+"_") {
		name := integrationID[len(IntegrationTypeGCM+"_"):]
		if name == "" {
			return "", "", rmerror.NewRMError(nil, "Invalid integrationId format: missing platform name after gcm_")
		}
		return IntegrationTypeGCM, name, nil
	}

	return integrationID, "", nil
}

func validateAPNS(authKey, keyID, teamID, bundleID string) error {
	if authKey == "" || keyID == "" || teamID == "" || bundleID == "" {
		return rmerror.NewRMError(nil,
			"Authentication key, key ID, team ID, and bundle ID are required for APNS token-based authentication")
	}
	return nil
}

// validateGSA validates that all required Google service-account fields
// are present. Returns the marshalled JSON form for storage as the SNS
// PlatformCredential.
func validateGSA(sa googleServiceAccount) ([]byte, error) {
	if sa.Type == "" || sa.ProjectID == "" || sa.PrivateKeyID == "" ||
		sa.PrivateKey == "" || sa.ClientEmail == "" || sa.ClientID == "" ||
		sa.AuthURI == "" || sa.TokenURI == "" ||
		sa.AuthProviderX509CertURL == "" || sa.ClientX509CertURL == "" ||
		sa.UniverseDomain == "" {
		return nil, rmerror.NewRMError(nil,
			"All Google service-account fields are required for GCM (type, project_id, private_key_id, private_key, client_email, client_id, auth_uri, token_uri, auth_provider_x509_cert_url, client_x509_cert_url, universe_domain)")
	}

	jsonBytes, err := json.Marshal(sa)
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to marshal Google service-account JSON")
	}
	return jsonBytes, nil
}

// buildAPNSAttributes constructs the SNS attribute map for Apple platforms
func buildAPNSAttributes(authKey, keyID, teamID, bundleID string) map[string]string {
	return map[string]string{
		"PlatformCredential":    authKey,
		"PlatformPrincipal":     keyID,
		"ApplePlatformTeamID":   teamID,
		"ApplePlatformBundleID": bundleID,
	}
}

// buildGCMAttributes constructs the SNS attribute map for Google/Firebase platforms
func buildGCMAttributes(saJSON []byte) map[string]string {
	return map[string]string{
		"PlatformCredential": string(saJSON),
	}
}

func buildPlatformAttributes(input PlatformAttributesInput) (map[string]string, string, error) {
	switch input.PlatformType {

	case PlatformAPNS, PlatformAPNSSandbox:
		if err := validateAPNS(input.AuthKey, input.KeyID, input.TeamID, input.BundleID); err != nil {
			return nil, "", err
		}

		attrs := buildAPNSAttributes(
			input.AuthKey,
			input.KeyID,
			input.TeamID,
			input.BundleID,
		)

		return attrs, input.BundleID, nil

	case PlatformGCM:
		saJSON, err := validateGSA(input.GSA)
		if err != nil {
			return nil, "", err
		}

		// If platformName is supplied (PUT case) validate match
		if input.PlatformName != "" && input.GSA.ProjectID != input.PlatformName {
			return nil, "", rmerror.NewRMError(nil,
				"GCM project_id does not match existing integration")
		}

		attrs := buildGCMAttributes(saJSON)

		return attrs, input.GSA.ProjectID, nil

	default:
		return nil, "", rmerror.NewRMError(nil,
			"Unsupported platform: "+input.PlatformType)
	}
}

// buildAttributesForUpdate builds SNS attributes from RegisterIntegrationRequest for the given snsPlatform.
func buildAttributesForUpdate(snsPlatform, platformName string, body RegisterIntegrationRequest) (map[string]string, error) {
	attrs, _, err := buildPlatformAttributes(
		PlatformAttributesInput{
			PlatformType: snsPlatform,
			PlatformName: platformName,
			AuthKey:      body.AuthenticationKey,
			KeyID:        body.KeyID,
			TeamID:       body.TeamID,
			BundleID:     body.BundleID,
			GSA:          body.googleServiceAccount,
		},
	)

	return attrs, err
}

// handlePostIntegration handles POST /v1/admin/integrations?integration_type=...
func handlePostIntegration(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationType := request.QueryStringParameters["integration_type"]
	if integrationType == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integration_type query parameter")), nil
	}

	snsPlatform, err := integrationTypeToSNSPlatform(integrationType)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}

	var req RegisterIntegrationRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	attrs, appName, err := buildPlatformAttributes(
		PlatformAttributesInput{
			PlatformType: snsPlatform,
			AuthKey:      req.AuthenticationKey,
			KeyID:        req.KeyID,
			TeamID:       req.TeamID,
			BundleID:     req.BundleID,
			GSA:          req.googleServiceAccount,
		},
	)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	if _, err := push.CreatePlatformApplication(ctx, appName, snsPlatform, attrs); err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	return utils.APIGwRespJSON(http.StatusCreated, RegisterIntegrationResponse{
		IntegrationID: buildIntegrationID(integrationType, appName),
	}), nil
}

// loadIntegrationDetail fetches the per-integration detail for one (snsPlatform, platformAppName) pair: service-account fields for GCM, bundle_id for APNS.
// Secret material (APNS .p8 key, GCM private_key) never comes back from the push layer (see GetPlatformApplicationAttributes), so it cannot enter a response.
func loadIntegrationDetail(ctx context.Context, snsPlatform, platformAppName string) (GetIntegrationResponse, error) {
	integrationType := snsPlatformToIntegrationType(snsPlatform)
	resp := GetIntegrationResponse{
		IntegrationID:   buildIntegrationID(integrationType, platformAppName),
		IntegrationType: integrationType,
	}

	switch integrationType {
	case IntegrationTypeAPNS, IntegrationTypeAPNSSandbox:
		// bundle_id is derivable from the integration_id; no SNS round-trip needed.
		resp.BundleID = platformAppName

	case IntegrationTypeGCM:
		// Fetch the stored service-account metadata; the push layer strips private_key before returning the credential.
		attrs, err := push.GetPlatformApplicationAttributes(ctx, snsPlatform, platformAppName)
		if err != nil {
			return resp, err
		}
		if cred := attrs["PlatformCredential"]; cred != "" {
			var sa googleServiceAccount
			if err := json.Unmarshal([]byte(cred), &sa); err != nil {
				return resp, rmerror.NewRMError(err, "Failed to parse stored GCM service-account JSON")
			}
			resp.googleServiceAccount = sa
		}
		// If PlatformCredential is absent, project_id still falls out of the integration_id.
		if resp.ProjectID == "" {
			resp.ProjectID = platformAppName
		}
	}

	return resp, nil
}

// collectIntegrations lists every SNS platform application and returns the full
// detail for each. When matchPlatform/matchAppName are both non-empty, only the
// single matching integration is returned (used by the GET-one handler); when
// both are empty, all integrations are returned, optionally narrowed by typeFilter
// (used by the list handler). A platform whose detail fails to load is skipped.
func collectIntegrations(ctx context.Context, typeFilter, matchPlatform, matchAppName string) ([]GetIntegrationResponse, error) {
	platforms, err := push.ListPlatformApplications(ctx)
	if err != nil {
		return nil, err
	}

	integrations := []GetIntegrationResponse{}
	for _, p := range platforms {
		if matchPlatform != "" && (p.Platform != matchPlatform || p.PlatformAppName != matchAppName) {
			continue
		}
		if typeFilter != "" && snsPlatformToIntegrationType(p.Platform) != typeFilter {
			continue
		}
		detail, err := loadIntegrationDetail(ctx, p.Platform, p.PlatformAppName)
		if err != nil {
			rlog.Error(ctx).Err(err).Str("integration_id", detail.IntegrationID).Msg("Failed to load integration detail; skipping")
			continue
		}
		integrations = append(integrations, detail)
	}

	return integrations, nil
}

// handleListIntegrations handles GET /v1/admin/integrations[?integration_type=...]
func handleListIntegrations(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrations, err := collectIntegrations(ctx, request.QueryStringParameters["integration_type"], "", "")
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list integrations")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, ListIntegrationsResponse{
		Integrations: integrations,
	}), nil
}

// handleListPublicIntegrations handles GET /v1/integrations[?integration_type=...],
// the non-admin list: id+type only, no credentials, so no per-integration SNS fetch.
func handleListPublicIntegrations(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	typeFilter := request.QueryStringParameters["integration_type"]

	platforms, err := push.ListPlatformApplications(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to list integrations")), nil
	}

	summaries := []PublicIntegrationSummary{}
	for _, p := range platforms {
		integrationType := snsPlatformToIntegrationType(p.Platform)
		if typeFilter != "" && integrationType != typeFilter {
			continue
		}
		summaries = append(summaries, PublicIntegrationSummary{
			IntegrationID:   buildIntegrationID(integrationType, p.PlatformAppName),
			IntegrationType: integrationType,
		})
	}

	return utils.APIGwRespJSON(http.StatusOK, ListPublicIntegrationsResponse{
		Integrations: summaries,
	}), nil
}

// handleGetIntegration handles GET /v1/admin/integrations/{integrationId}
func handleGetIntegration(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationID := request.PathParameters["integrationId"]
	if integrationID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integrationId")), nil
	}

	integrationType, platformAppName, err := parseIntegrationID(integrationID)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}

	snsPlatform, err := integrationTypeToSNSPlatform(integrationType)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid integrationId; must be apns, apns_sandbox, or gcm")), nil
	}

	integrations, err := collectIntegrations(ctx, "", snsPlatform, platformAppName)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to look up integration")), nil
	}
	if len(integrations) == 0 {
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Integration not found")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, integrations[0]), nil
}

// handlePutIntegration handles PUT /v1/admin/integrations/{integrationId}
func handlePutIntegration(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationID := request.PathParameters["integrationId"]
	if integrationID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integrationId")), nil
	}

	integrationType, platformAppName, err := parseIntegrationID(integrationID)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}

	snsPlatform, err := integrationTypeToSNSPlatform(integrationType)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid integrationId; must be apns, apns_sandbox, or gcm")), nil
	}

	if platformAppName == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid integrationId format")), nil
	}

	var body RegisterIntegrationRequest
	if err := rmngrequest.ExtractRequestStruct(request, &body); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	attributes, err := buildAttributesForUpdate(snsPlatform, platformAppName, body)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	if err := push.UpdatePlatformApplication(ctx, snsPlatform, platformAppName, attributes); err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

// handleDeleteIntegration handles DELETE /v1/admin/integrations/{integrationId}
func handleDeleteIntegration(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	integrationID := request.PathParameters["integrationId"]
	if integrationID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Missing integrationId")), nil
	}

	integrationType, platformAppName, err := parseIntegrationID(integrationID)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(err.Error())), nil
	}

	snsPlatform, err := integrationTypeToSNSPlatform(integrationType)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid integrationId; must be apns, apns_sandbox, or gcm")), nil
	}

	if platformAppName == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid integrationId format")), nil
	}

	if err := push.DeletePlatformApplication(ctx, snsPlatform, platformAppName); err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, utils.NewAPIStatus("success")), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		rlog.Error(ctx).Msg("Failed to create request context")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	// Public (non-admin) surface: GET /v1/integrations. Gate on the resource
	// template, not the resolved path, so {integrationId} doesn't match here.
	if !strings.Contains(request.Resource, "/admin/") {
		if request.HTTPMethod == "GET" {
			return handleListPublicIntegrations(rctx, request)
		}
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}

	isAuthorized := rctx.GetAccessor().(*user.User).IsSuperAdmin(rctx)
	if !isAuthorized {
		rlog.Error(rctx).Bool("isAuthorized", isAuthorized).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	// Dispatch by path: collection vs single-item operations.
	hasID := request.PathParameters["integrationId"] != ""

	if hasID {
		switch request.HTTPMethod {
		case "GET":
			return handleGetIntegration(rctx, request)
		case "PUT":
			return handlePutIntegration(rctx, request)
		case "DELETE":
			return handleDeleteIntegration(rctx, request)
		}
	} else {
		switch request.HTTPMethod {
		case "POST":
			return handlePostIntegration(rctx, request)
		case "GET":
			return handleListIntegrations(rctx, request)
		}
	}

	return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
}

func main() {
	awslambda.Start(handleRequest)
}
