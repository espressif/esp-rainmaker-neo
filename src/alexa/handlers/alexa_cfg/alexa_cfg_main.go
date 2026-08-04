// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"net/http"
	"os"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/alexa"
	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

/* Ideally this REST API should only be available to an administrative user. We do not have a concept of an administrative user yet.
   TODO: So currently this functionality is available to all users :-D, will be updated soon
*/

// alexaSkillRegions are the AWS regions where the Alexa skill Lambda is deployed (AWS requires skill to be deployed in 3 fixed regions).
const alexaSkillRegions = "us-east-1,eu-west-1,us-west-2"

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

// updateVAClientRedirectURIs registers redirectURIs on the OIDC va-client registry row, shared by Alexa and GVA (union semantics; other fields preserved).
func updateVAClientRedirectURIs(ctx context.Context, clientID string, redirectURIs []string) error {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if _, err := svc.AddRedirectURIs(clientID, redirectURIs); err != nil {
		return rmerror.NewRMError(err, "failed to add OIDC va-client redirect URIs")
	}
	return nil
}

// vaClientRedirectURIs returns the OIDC va-client registry row's currently registered redirect URIs.
func vaClientRedirectURIs(ctx context.Context, clientID string) ([]string, error) {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	current, err := svc.Get(clientID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get OIDC va-client")
	}
	return current.RedirectURIs, nil
}

// newRegionalLambdaClient creates a Lambda API client for the given AWS region (Alexa skill Lambdas are regional).
// Tests replace this with a stub that returns awscommon.GetLambdaClient() so AddPermission hits the mock.
var newRegionalLambdaClient = defaultRegionalLambdaClient

func defaultRegionalLambdaClient(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return lambda.NewFromConfig(cfg), nil
}

// addAlexaTriggerInRegion adds the Alexa invoke permission to the alexa_skill_<rmng_region> function in the given region.
func addAlexaTriggerInRegion(ctx context.Context, region, functionName, skillID string) error {
	lambdaClient, err := newRegionalLambdaClient(ctx, region)
	if err != nil {
		return err
	}

	// Remove existing permission first to allow updating the skill ID
	_, _ = lambdaClient.RemovePermission(ctx, &lambda.RemovePermissionInput{
		FunctionName: aws.String(functionName),
		StatementId:  aws.String("AlexaSkillInvoke"),
	})

	_, err = lambdaClient.AddPermission(ctx, &lambda.AddPermissionInput{
		Action:           aws.String("lambda:InvokeFunction"),
		FunctionName:     aws.String(functionName),
		Principal:        aws.String("alexa-connectedhome.amazon.com"),
		StatementId:      aws.String("AlexaSkillInvoke"),
		EventSourceToken: aws.String(skillID),
	})
	if err != nil {
		return err
	}
	return nil
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

	err = updateVAClientRedirectURIs(ctx, vaClientID, req.RedirectURIs)
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
	// Must match the Lambda name created by alexa_skill_stack.py
	// (function_name = f"rmng-alexa-skill-{rmng_region}").
	alexaSkillFunction := "rmng-alexa-skill-" + rmngRegion
	regions := strings.Split(alexaSkillRegions, ",")
	for i := range regions {
		regions[i] = strings.TrimSpace(regions[i])
	}
	for _, region := range regions {
		if region == "" {
			continue
		}
		err = addAlexaTriggerInRegion(ctx, region, alexaSkillFunction, req.SkillID)
		if err != nil {
			rlog.Error(ctx).Err(err).Str("region", region).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to add Alexa trigger: "+err.Error())), nil
		}
	}

	return utils.APIGwRespJSON(http.StatusOK, AlexaCfgResponse{Message: "success"}), nil
}

type AlexaCfgGetResponse struct {
	ClientID     string   `json:"client_id"`
	SkillID      string   `json:"skill_id"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	// ManufacturerName is the brand currently advertised in discovery — the configured value, or
	// the default brand when a deployment has set none. Never empty.
	ManufacturerName string `json:"manufacturer_name"`
}

func handleGetAlexaCfg(ctx context.Context) (events.APIGatewayProxyResponse, error) {
	// Get Alexa client details (only return client ID for security)
	clientID, _, err := alexa_skill.GetAlexaClientDetails(ctx)
	if err != nil {
		rlog.Error(ctx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Alexa configuration not found")), nil
	}

	response := AlexaCfgGetResponse{
		ClientID:         clientID,
		ManufacturerName: alexa_skill.GetAlexaManufacturerName(ctx),
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
		callbackURLs, err := vaClientRedirectURIs(ctx, vaClientID)
		if err != nil {
			rlog.Error(ctx).Err(err).Send()
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to get OIDC va-client redirect URIs")), nil
		}
		response.RedirectURIs = callbackURLs
	}

	return utils.APIGwRespJSON(http.StatusOK, response), nil
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)

	isAuthorized := rctx.GetAccessor().(*user.User).IsSuperAdmin(rctx)
	if !isAuthorized {
		rlog.Error(rctx).Bool("isAuthorized", isAuthorized).Msg("User is not authorized")
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
