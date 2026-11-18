// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/espuser/clients"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// AlexaSkillRegions are the AWS regions where the Alexa skill Lambda is deployed.
// AWS requires a Smart Home skill's Lambda to live in these three fixed regions.
var AlexaSkillRegions = []string{"us-east-1", "eu-west-1", "us-west-2"}

// AlexaSkillFunctionName is the skill Lambda's name in every Alexa region. It must
// match alexa_stack.py (f"rmng-alexa-skill-{rmng_region}").
func AlexaSkillFunctionName(rmngRegion string) string {
	return "rmng-alexa-skill-" + rmngRegion
}

// NewRegionalLambdaClient builds a Lambda client for a given Alexa region (skill
// Lambdas are regional). Tests swap this for a stub that returns the global mock.
var NewRegionalLambdaClient = DefaultRegionalLambdaClient

func DefaultRegionalLambdaClient(ctx context.Context, region string) (awscommon.LambdaClientInterface, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return lambda.NewFromConfig(cfg), nil
}

// UpdateVAClientRedirectURIs registers redirectURIs on the OIDC va-client registry row,
// shared by Alexa, GVA and SmartThings (union semantics; other fields preserved). Account
// linking runs against the ESP User provider, so this is what makes Alexa's redirect back
// from the linking page acceptable.
func UpdateVAClientRedirectURIs(ctx context.Context, clientID string, redirectURIs []string) error {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	if _, err := svc.AddRedirectURIs(clientID, redirectURIs); err != nil {
		return rmerror.NewRMError(err, "failed to add OIDC va-client redirect URIs")
	}
	return nil
}

// VAClientRedirectURIs returns the va-client row's currently registered redirect URIs.
func VAClientRedirectURIs(ctx context.Context, clientID string) ([]string, error) {
	svc := clients.NewService(rmngctx.NewRmngContextWithCtx(ctx, nil))
	current, err := svc.Get(clientID)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to get OIDC va-client")
	}
	return current.RedirectURIs, nil
}

// AlexaInvokeStatementID is the skill Lambda's statement for the live skill, the one the stored credentials belong to.
const AlexaInvokeStatementID = "AlexaSkillInvoke"

const alexaSetupStatementPrefix = "AlexaSkillSetup-"

// AlexaSetupStatementID is the statement a skill still being set up invokes through, so the live skill keeps AlexaInvokeStatementID until the new one is proven: "amzn1.ask.skill.ab-12" -> "AlexaSkillSetup-amzn1-ask-skill-ab-12".
func AlexaSetupStatementID(skillID string) string {
	return alexaSetupStatementPrefix + strings.ReplaceAll(skillID, ".", "-")
}

// putAlexaTriggerInRegion replaces statementID on the skill Lambda in one region with an invoke grant scoped to skillID.
func putAlexaTriggerInRegion(ctx context.Context, region, functionName, statementID, skillID string) error {
	lambdaClient, err := NewRegionalLambdaClient(ctx, region)
	if err != nil {
		return err
	}

	_, _ = lambdaClient.RemovePermission(ctx, &lambda.RemovePermissionInput{
		FunctionName: aws.String(functionName),
		StatementId:  aws.String(statementID),
	})

	_, err = lambdaClient.AddPermission(ctx, &lambda.AddPermissionInput{
		Action:           aws.String("lambda:InvokeFunction"),
		FunctionName:     aws.String(functionName),
		Principal:        aws.String("alexa-connectedhome.amazon.com"),
		StatementId:      aws.String(statementID),
		EventSourceToken: aws.String(skillID),
	})
	return err
}

func putAlexaTriggerAllRegions(ctx context.Context, rmngRegion, statementID, skillID string) error {
	functionName := AlexaSkillFunctionName(rmngRegion)
	for _, region := range AlexaSkillRegions {
		if err := putAlexaTriggerInRegion(ctx, region, functionName, statementID, skillID); err != nil {
			return err
		}
	}
	return nil
}

// AddAlexaTriggerAllRegions makes skillID the live skill's invoke grant in every Alexa region, replacing the previous one.
func AddAlexaTriggerAllRegions(ctx context.Context, rmngRegion, skillID string) error {
	return putAlexaTriggerAllRegions(ctx, rmngRegion, AlexaInvokeStatementID, skillID)
}

// AddAlexaSetupTriggerAllRegions lets skillID invoke the skill Lambda during setup without touching the live skill's grant.
func AddAlexaSetupTriggerAllRegions(ctx context.Context, rmngRegion, skillID string) error {
	return putAlexaTriggerAllRegions(ctx, rmngRegion, AlexaSetupStatementID(skillID), skillID)
}

// RemoveAlexaSetupTriggerAllRegions drops skillID's setup grant in every Alexa region.
func RemoveAlexaSetupTriggerAllRegions(ctx context.Context, rmngRegion, skillID string) {
	functionName := AlexaSkillFunctionName(rmngRegion)
	for _, region := range AlexaSkillRegions {
		removeStatement(ctx, region, functionName, AlexaSetupStatementID(skillID))
	}
}

// RemoveStaleAlexaSetupTriggers drops every setup grant on the skill Lambda, including those of runs that died before their own cleanup (a Lambda timeout). It assumes one automatic setup at a time: a concurrent run would lose its grant and fail its manifest build.
func RemoveStaleAlexaSetupTriggers(ctx context.Context, rmngRegion string) {
	functionName := AlexaSkillFunctionName(rmngRegion)
	for _, region := range AlexaSkillRegions {
		sids, err := statementIDs(ctx, region, functionName)
		if err != nil {
			rlog.Warn(ctx).Err(err).Str("region", region).Msg("could not list skill Lambda grants")
			continue
		}
		for _, sid := range sids {
			if strings.HasPrefix(sid, alexaSetupStatementPrefix) {
				removeStatement(ctx, region, functionName, sid)
			}
		}
	}
}

// ActivateAlexaSkill makes skillID the deployment's live skill: it moves the invoke grant and stores the AcceptGrant credentials. On any failure it restores the previous skill's grant and credentials, so a deployment is never left split between two skills.
func ActivateAlexaSkill(ctx context.Context, rmngRegion, skillID, clientID, clientSecret string) error {
	// Only "never configured" may read as no previous skill: rolling back to nothing deletes the live grant.
	prevSkillID, err := ssmutil.GetParameterWithCaching(ctx, AlexaSSMSkillIDParam, true, false)
	if err != nil && !isParameterNotFound(err) {
		return rmerror.NewRMError(err, "failed to read the live skill id")
	}
	prevClientID, prevClientSecret, err := GetAlexaClientDetails(ctx)
	if err != nil && !isParameterNotFound(err) {
		return rmerror.NewRMError(err, "failed to read the live skill credentials")
	}

	err = AddAlexaTriggerAllRegions(ctx, rmngRegion, skillID)
	if err == nil {
		err = StoreAlexaClientDetails(ctx, clientID, clientSecret, skillID)
	}
	if err == nil {
		return nil
	}

	if prevSkillID == "" {
		functionName := AlexaSkillFunctionName(rmngRegion)
		for _, region := range AlexaSkillRegions {
			removeStatement(ctx, region, functionName, AlexaInvokeStatementID)
		}
		return err
	}
	if restoreErr := AddAlexaTriggerAllRegions(ctx, rmngRegion, prevSkillID); restoreErr != nil {
		rlog.Warn(ctx).Err(restoreErr).Str("skillID", prevSkillID).Msg("could not restore the previous skill's invoke grant")
	}
	if prevClientID != "" {
		if restoreErr := StoreAlexaClientDetails(ctx, prevClientID, prevClientSecret, prevSkillID); restoreErr != nil {
			rlog.Warn(ctx).Err(restoreErr).Str("skillID", prevSkillID).Msg("could not restore the previous skill's credentials")
		}
	}
	return err
}

// removeStatement drops one statement from a regional skill Lambda; a statement already gone is not an error, and any other failure is logged because it leaves a grant in place.
func removeStatement(ctx context.Context, region, functionName, statementID string) {
	lambdaClient, err := NewRegionalLambdaClient(ctx, region)
	if err == nil {
		_, err = lambdaClient.RemovePermission(ctx, &lambda.RemovePermissionInput{
			FunctionName: aws.String(functionName),
			StatementId:  aws.String(statementID),
		})
	}
	var notFound *lambdatypes.ResourceNotFoundException
	if err != nil && !errors.As(err, &notFound) {
		rlog.Warn(ctx).Err(err).Str("region", region).Str("statement", statementID).Msg("invoke grant not removed")
	}
}

// statementIDs lists the Sids in a regional skill Lambda's resource policy; a function with no policy has none.
func statementIDs(ctx context.Context, region, functionName string) ([]string, error) {
	lambdaClient, err := NewRegionalLambdaClient(ctx, region)
	if err != nil {
		return nil, err
	}
	out, err := lambdaClient.GetPolicy(ctx, &lambda.GetPolicyInput{FunctionName: aws.String(functionName)})
	var notFound *lambdatypes.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var policy struct {
		Statement []struct {
			Sid string `json:"Sid"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(aws.ToString(out.Policy)), &policy); err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse skill Lambda policy")
	}
	sids := make([]string, 0, len(policy.Statement))
	for _, st := range policy.Statement {
		sids = append(sids, st.Sid)
	}
	return sids, nil
}
