// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// alexa_auto_cfg creates an Alexa Smart Home skill over SMAPI and returns at once; the slow builds run in an async self-invocation, since they exceed API Gateway's 29s limit and Lambda freezes a goroutine once the response is sent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	alexa_skill "github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/alexa/smapi"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/lambdautil"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
)

const defaultPrivacyPolicyURL = "https://rainmaker.espressif.com/privacy"

const (
	pathAutoConfiguration = "/v1/admin/integrations/alexa/auto-configuration"
	pathLWAConnection     = "/v1/admin/integrations/alexa/lwa-connection"
)

// build waits run in the background worker, so there is no API Gateway 29s limit to
// respect; Alexa builds take 30-60s each. Vars so tests can shrink them.
var (
	buildTimeout      = 300 * time.Second
	buildPollInterval = 5 * time.Second
)

type autoCfgRequest struct {
	SkillName string `json:"skill_name"`
	// ManufacturerName follows manual setup: absent leaves the stored brand alone, "" restores the default.
	ManufacturerName *string `json:"manufacturer_name,omitempty"`
}

// lwaConnectionRequest connects a developer account with either the code LWA returned after consent, redeemed here because it expires within minutes, or a refresh token minted elsewhere.
type lwaConnectionRequest struct {
	LWAClientID          string `json:"lwa_client_id"`
	LWAClientSecret      string `json:"lwa_client_secret"`
	LWAAuthorizationCode string `json:"lwa_authorization_code"`
	LWARedirectURI       string `json:"lwa_redirect_uri"`
	LWARefreshToken      string `json:"lwa_refresh_token"`
}

type autoCfgResponse struct {
	SkillID string `json:"skill_id"`
	Status  string `json:"status"`
}

// configWorkerEvent is the payload the API handler async-invokes itself with to run the slow configuration pipeline in the background; the __worker flag distinguishes it from an API Gateway proxy event. It carries no LWA credentials because Lambda keeps async events queued for up to 6 h, so the worker reads the stored connection itself.
type configWorkerEvent struct {
	Worker           bool     `json:"__worker"`
	SkillID          string   `json:"skill_id"`
	SkillName        string   `json:"skill_name"`
	RedirectURIs     []string `json:"redirect_uris"`
	Existing         bool     `json:"existing"`
	ManufacturerName *string  `json:"manufacturer_name,omitempty"`
}

var errInvalidRequest = errors.New("invalid request")

// accountLinkingConfig builds the account-linking inputs for the ESP User OIDC
// provider: the authorize/token endpoints published in its discovery document and the
// credentials of Alexa's own client.
func accountLinkingConfig(ctx context.Context) (authURL, tokenURL, clientID, clientSecret string, err error) {
	apiURL := strings.TrimSuffix(os.Getenv("ESP_USER_API_URL"), "/")
	clientID = os.Getenv("OIDC_LINK_CLIENT_ID")
	secretParam := os.Getenv("OIDC_LINK_CLIENT_SECRET_PARAM")
	if apiURL == "" || clientID == "" || secretParam == "" {
		return "", "", "", "", rmerror.NewRMError(nil, "Account-linking config not set")
	}

	clientSecret, err = ssmutil.GetParameterWithCaching(ctx, secretParam, true)
	if err != nil || clientSecret == "" {
		return "", "", "", "", rmerror.NewRMError(err, "Account-linking config not set: the linking client's secret is unavailable")
	}

	return apiURL + "/oauth2/authorize", apiURL + "/oauth2/token", clientID, clientSecret, nil
}

// endpointARNs maps each Alexa region code to the skill Lambda ARN in the AWS region
// that backs it (fixed for Smart Home skills, and matching alexa_skill.AlexaSkillRegions).
func endpointARNs(accountID, rmngRegion string) map[string]string {
	fn := alexa_skill.AlexaSkillFunctionName(rmngRegion)
	arn := func(awsRegion string) string {
		return fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", awsRegion, accountID, fn)
	}
	return map[string]string{
		"NA": arn("us-east-1"),
		"EU": arn("eu-west-1"),
		"FE": arn("us-west-2"),
	}
}

// handleRequest is the API path: admin gate, then route to the setup or the account connection.
func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}

	// A caller the admin pool does not know leaves the accessor nil, so assert with the
	// comma-ok form: a bare assertion panics and the client sees a 502 rather than a 403.
	caller, ok := rctx.GetAccessor().(*user.User)
	if !ok || caller == nil || !caller.IsAdmin(rctx) {
		rlog.Error(ctx).Msg("User is not authorized")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	var response interface{}
	var err error
	switch {
	case request.Resource == pathAutoConfiguration && request.HTTPMethod == http.MethodPost:
		response, err = handleAutoConfiguration(ctx, request)
	case request.Resource == pathLWAConnection && request.HTTPMethod == http.MethodPut:
		response, err = handlePutLWAConnection(ctx, request)
	case request.Resource == pathLWAConnection && request.HTTPMethod == http.MethodDelete:
		response, err = handleDeleteLWAConnection(ctx)
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
	if err != nil {
		// A refusal by LWA or SMAPI is the admin's to fix (reconnect, fix the profile), so it must not page as a server error.
		if errors.Is(err, errInvalidRequest) || errors.Is(err, alexa_skill.ErrLWANotConnected) || smapi.IsCallerError(err) {
			rlog.Warn(ctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(rmerror.TopMessage(err))), nil
		}
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(rmerror.TopMessage(err))), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

// handleDeleteLWAConnection is idempotent: disconnecting an account that is not connected also succeeds.
func handleDeleteLWAConnection(ctx context.Context) (interface{}, error) {
	if err := alexa_skill.DeleteLWAConnection(ctx); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to disconnect the Amazon account")
	}
	return utils.NewAPIStatus("Amazon account disconnected"), nil
}

// handlePutLWAConnection verifies the developer account's credentials with LWA and stores them, so automatic setups need no further consent.
func handlePutLWAConnection(ctx context.Context, request events.APIGatewayProxyRequest) (interface{}, error) {
	var req lwaConnectionRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return nil, rmerror.NewRMError(errInvalidRequest, "Invalid request body")
	}
	if req.LWAClientID == "" || req.LWAClientSecret == "" {
		return nil, rmerror.NewRMError(errInvalidRequest, "Missing lwa_client_id or lwa_client_secret")
	}

	conn := alexa_skill.LWAConnection{ClientID: req.LWAClientID, ClientSecret: req.LWAClientSecret}
	switch {
	case req.LWAAuthorizationCode != "" && req.LWARefreshToken == "":
		if req.LWARedirectURI == "" {
			return nil, rmerror.NewRMError(errInvalidRequest, "Missing lwa_redirect_uri")
		}
		refreshToken, err := smapi.ExchangeAuthorizationCode(ctx, req.LWAClientID, req.LWAClientSecret, req.LWAAuthorizationCode, req.LWARedirectURI)
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to redeem the LWA authorization code")
		}
		conn.RefreshToken = refreshToken
	case req.LWARefreshToken != "" && req.LWAAuthorizationCode == "":
		if _, err := smapi.GetAccessToken(ctx, req.LWAClientID, req.LWAClientSecret, req.LWARefreshToken); err != nil {
			return nil, rmerror.NewRMError(err, "Failed to verify the LWA credentials")
		}
		conn.RefreshToken = req.LWARefreshToken
	default:
		return nil, rmerror.NewRMError(errInvalidRequest, "Send either lwa_authorization_code or lwa_refresh_token")
	}

	if err := alexa_skill.StoreLWAConnection(ctx, conn); err != nil {
		return nil, rmerror.NewRMError(err, "Failed to store the Amazon account")
	}
	return utils.NewAPIStatus("Amazon account connected"), nil
}

// connectedSMAPIClient signs in to SMAPI as the connected Amazon account. A token LWA refuses is revoked or expired, so the connection is dropped and the dashboard offers to reconnect instead of showing it as connected.
func connectedSMAPIClient(ctx context.Context) (*smapi.Client, error) {
	conn, err := alexa_skill.GetLWAConnection(ctx)
	if errors.Is(err, alexa_skill.ErrLWANotConnected) {
		return nil, rmerror.NewRMError(err, "No Amazon account connected")
	}
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to read the connected Amazon account")
	}
	token, err := smapi.GetAccessToken(ctx, conn.ClientID, conn.ClientSecret, conn.RefreshToken)
	if err != nil && smapi.IsCallerError(err) {
		if delErr := alexa_skill.DeleteLWAConnection(ctx); delErr != nil {
			rlog.Warn(ctx).Err(delErr).Send()
		}
		return nil, rmerror.NewRMError(err, "The connected Amazon account was refused, connect it again")
	}
	if err != nil {
		return nil, rmerror.NewRMError(err, "Failed to authenticate to SMAPI")
	}
	return smapi.NewClient(ctx, conn.ClientID, token), nil
}

// handleAutoConfiguration creates or reuses the skill, kicks off the background configuration worker, and returns the skill id immediately.
func handleAutoConfiguration(ctx context.Context, request events.APIGatewayProxyRequest) (interface{}, error) {
	var req autoCfgRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return nil, rmerror.NewRMError(errInvalidRequest, "Invalid request body")
	}
	if req.SkillName == "" {
		return nil, rmerror.NewRMError(errInvalidRequest, "Missing skill_name")
	}

	// Fail fast if the config the background worker needs is missing, before creating an orphan skill it could never configure.
	if os.Getenv("AWS_ACCOUNT_ID") == "" || os.Getenv("RMNG_REGION") == "" {
		return nil, rmerror.NewRMError(nil, "AWS_ACCOUNT_ID or RMNG_REGION not configured")
	}
	if _, _, _, _, err := accountLinkingConfig(ctx); err != nil {
		return nil, err
	}

	client, err := connectedSMAPIClient(ctx)
	if err != nil {
		return nil, err
	}

	// Each POST creates or replaces: the deployment's live skill when this profile can reach it, else a new skill, so repeated runs do not pile skills up at Amazon.
	skillID, stage := "", "updating"
	live, err := ssmutil.GetParameterWithCaching(ctx, alexa_skill.AlexaSSMSkillIDParam, true, false)
	switch {
	case ssmutil.IsParameterNotFound(err):
	case err != nil:
		// Reading "none" on a transient error would create a duplicate skill.
		return nil, rmerror.NewRMError(err, "Failed to read the live skill")
	default:
		if _, _, err := client.GetBuildStatus(ctx, live); err == nil {
			skillID = live
		} else if !smapi.IsCallerError(err) {
			return nil, rmerror.NewRMError(err, "Failed to read the live skill")
		} else {
			// Set up manually from another Amazon account, or deleted there: this profile cannot reconfigure it.
			rlog.Warn(ctx).Err(err).Str("liveSkillID", live).Msg("live skill not reachable, creating a new one")
			stage = "created; the previous skill is not reachable with these LWA credentials"
		}
	}
	// A reused live skill keeps the redirect URIs registered when it went live; only a new one needs a vendor, the profile's first.
	existing := skillID != ""
	var redirectURIs []string
	if !existing {
		vendorID, err := client.GetVendorID(ctx)
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to resolve SMAPI vendor")
		}
		redirectURIs = smapi.AccountLinkingRedirectURLs(vendorID)
		skillID, err = client.CreateSkill(ctx, vendorID, smapi.MinimalSmartHomeManifest(req.SkillName, defaultPrivacyPolicyURL))
		if err != nil {
			return nil, rmerror.NewRMError(err, "Failed to create skill")
		}
		if stage == "updating" {
			stage = "created"
		}
	}

	alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusInProgress, skillID, stage, "")

	// Hand the slow build/config pipeline to a background invocation of this Lambda.
	if err := invokeConfigWorker(ctx, configWorkerEvent{
		SkillID: skillID, SkillName: req.SkillName, RedirectURIs: redirectURIs,
		Existing: existing, ManufacturerName: req.ManufacturerName,
	}); err != nil {
		alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusFailed, skillID, "",
			"could not start configuration: "+err.Error())
		return nil, rmerror.NewRMError(err, "Failed to start configuration")
	}

	return autoCfgResponse{SkillID: skillID, Status: "IN_PROGRESS"}, nil
}

// invokeConfigWorker async-invokes this Lambda with ev so the configuration pipeline runs in the background.
func invokeConfigWorker(ctx context.Context, ev configWorkerEvent) error {
	ev.Worker = true
	return lambdautil.InvokeAsync(ctx, os.Getenv("AWS_LAMBDA_FUNCTION_NAME"), ev)
}

// runConfigWorker runs the slow configuration pipeline for a skill, publishing every stage to config_status; there is no caller to return an error to.
func runConfigWorker(ctx context.Context, ev configWorkerEvent) {
	accountID := os.Getenv("AWS_ACCOUNT_ID")
	rmngRegion := os.Getenv("RMNG_REGION")

	// Publishing IN_PROGRESS on entry to every stage is what lets a poller print progress, and a stale updated_at tell it the worker died.
	step := func(stage string, fn func() error) bool {
		alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusInProgress, ev.SkillID, stage, "")
		err := fn()
		if err == nil {
			return true
		}
		rlog.Error(ctx).Err(err).Str("skillID", ev.SkillID).Str("stage", stage).Send()
		msg := err.Error()
		if !ev.Existing {
			msg = fmt.Sprintf("%s (skill %s was created; delete it at Amazon if abandoning)", msg, ev.SkillID)
		}
		alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusFailed, ev.SkillID, stage, msg)
		return false
	}

	var client *smapi.Client
	var alexaClientID, alexaClientSecret string
	ok := step("authenticate", func() error {
		var err error
		client, err = connectedSMAPIClient(ctx)
		return err
	}) &&
		// Only a new skill has a creation build to wait for; an existing skill's last build may have FAILED, which is what a repair is for.
		(ev.Existing || step("initial build", func() error {
			return client.WaitForBuild(ctx, ev.SkillID, buildTimeout, buildPollInterval)
		})) &&
		// Alexa validates endpoint invoke-ability during the manifest build, so the skill needs a grant first; a setup-only one leaves the live skill's grant alone until this run succeeds.
		step("grant setup invoke permission", func() error {
			alexa_skill.RemoveStaleAlexaSetupTriggers(ctx, rmngRegion)
			return alexa_skill.AddAlexaSetupTriggerAllRegions(ctx, rmngRegion, ev.SkillID)
		}) &&
		step("update manifest", func() error {
			return client.UpdateManifest(ctx, ev.SkillID, smapi.ManifestWithEventsAndEndpoints(ev.SkillName, defaultPrivacyPolicyURL, endpointARNs(accountID, rmngRegion)))
		}) &&
		step("manifest build", func() error {
			return client.WaitForBuild(ctx, ev.SkillID, buildTimeout, buildPollInterval)
		}) &&
		step("read credentials", func() error {
			var err error
			alexaClientID, alexaClientSecret, err = client.GetSkillCredentials(ctx, ev.SkillID)
			return err
		}) &&
		// None when reusing the live skill without a named vendor: its URIs were registered when it went live.
		(len(ev.RedirectURIs) == 0 || step("register the linking client's redirect URIs", func() error {
			return alexa_skill.UpdateVAClientRedirectURIs(ctx, os.Getenv("OIDC_LINK_CLIENT_ID"), ev.RedirectURIs)
		})) &&
		// After the final manifest build, so the build does not reset it.
		step("set account linking", func() error {
			authURL, tokenURL, vaClientID, vaClientSecret, err := accountLinkingConfig(ctx)
			if err != nil {
				return rmerror.NewRMError(err, "account-linking config not set on this deployment")
			}
			return client.UpdateAccountLinking(ctx, ev.SkillID, smapi.AccountLinkingPayload(authURL, tokenURL, vaClientID, vaClientSecret))
		}) &&
		// Last, so a failed run never takes the live grant or credentials from a working skill.
		step("activate", func() error {
			return alexa_skill.ActivateAlexaSkill(ctx, rmngRegion, ev.SkillID, alexaClientID, alexaClientSecret)
		}) &&
		// After activate so a failed run leaves the brand alone too; absent means "keep the stored brand", as in manual setup.
		(ev.ManufacturerName == nil || step("set manufacturer name", func() error {
			return alexa_skill.StoreAlexaManufacturerName(ctx, strings.TrimSpace(*ev.ManufacturerName))
		}))
	// The live grant (or nothing, on failure) is what remains; a setup grant left behind would let an abandoned skill invoke the Lambda.
	alexa_skill.RemoveAlexaSetupTriggerAllRegions(ctx, rmngRegion, ev.SkillID)
	if !ok {
		return
	}

	alexa_skill.PublishSetupStatus(ctx, alexa_skill.SetupStatusDone, ev.SkillID, "", "")
	rlog.Info(ctx).Str("skillID", ev.SkillID).Msg("alexa skill configured")
}

// dispatch routes the async background-worker invocation vs an API Gateway request.
func dispatch(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var probe struct {
		Worker bool `json:"__worker"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Worker {
		var ev configWorkerEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			rlog.Error(ctx).Err(err).Msg("invalid worker event")
			return nil, nil
		}
		runConfigWorker(ctx, ev)
		return nil, nil
	}

	var request events.APIGatewayProxyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request")), nil
	}
	return handleRequest(ctx, request)
}

func main() {
	awslambda.Start(dispatch)
}
