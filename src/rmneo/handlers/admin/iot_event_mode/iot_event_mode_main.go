// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/admin_config_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	awslambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iot"
	iottypes "github.com/aws/aws-sdk-go-v2/service/iot/types"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"
)

const (
	presenceRuleName     = "node_disconnected_rule"
	publishInputRuleName = "node_to_cloud_rule"

	modeDirect = "direct"
	modeSQS    = "sqs"

	reapplyAction = "reapply"
)

// ruleConfig captures the deploy-time wiring needed to assemble either rule
// action at runtime. The lambda ARN/queue URL/IoT-rule role ARN are passed
// in as environment variables by the CDK construct.
type ruleConfig struct {
	name       string
	lambdaArn  string
	queueURL   string
	iotRoleArn string
}

type modeRequest struct {
	Mode string `json:"mode"`
}

type modeResponse struct {
	Presence     string `json:"presence"`
	PublishInput string `json:"publish_input"`
}

// reapplyRequest is the payload the AwsCustomResource sends on every stack
// update to invoke this lambda's drift-correction path.
type reapplyRequest struct {
	Action string `json:"action"`
}

// reapplyResponse summarises what the reapply pass did. Returned to the
// AwsCustomResource caller (and surfaced in CloudFormation logs).
type reapplyResponse struct {
	Status       string `json:"status"`
	Presence     string `json:"presence"`
	PublishInput string `json:"publish_input"`
}

func presenceConfig() ruleConfig {
	return ruleConfig{
		name:       presenceRuleName,
		lambdaArn:  os.Getenv("PRESENCE_LAMBDA_ARN"),
		queueURL:   os.Getenv("NODE_CONN_QUEUE_URL"),
		iotRoleArn: os.Getenv("PRESENCE_IOT_RULE_ROLE_ARN"),
	}
}

func publishInputConfig() ruleConfig {
	return ruleConfig{
		name:       publishInputRuleName,
		lambdaArn:  os.Getenv("PUBLISH_INPUT_LAMBDA_ARN"),
		queueURL:   os.Getenv("NODE_TO_CLOUD_QUEUE_URL"),
		iotRoleArn: os.Getenv("PUBLISH_INPUT_IOT_RULE_ROLE_ARN"),
	}
}

// detectMode reads the rule's first action and reports "sqs" or "direct".
// Source of truth is the live rule definition; no DB cache is kept.
func detectMode(ctx context.Context, ruleName string) (string, error) {
	out, err := awscommon.GetIoTClient().GetTopicRule(ctx, &iot.GetTopicRuleInput{
		RuleName: aws.String(ruleName),
	})
	if err != nil {
		return "", err
	}
	if out.Rule == nil || len(out.Rule.Actions) == 0 {
		return "", fmt.Errorf("rule %s has no actions", ruleName)
	}
	first := out.Rule.Actions[0]
	switch {
	case first.Sqs != nil:
		return modeSQS, nil
	case first.Lambda != nil:
		return modeDirect, nil
	default:
		return "", fmt.Errorf("rule %s has unsupported action type", ruleName)
	}
}

// buildAction returns the Action struct for the requested mode using the
// rule's deploy-time wiring.
func buildAction(cfg ruleConfig, mode string) (iottypes.Action, error) {
	switch mode {
	case modeDirect:
		if cfg.lambdaArn == "" {
			return iottypes.Action{}, fmt.Errorf("lambda ARN not configured for rule %s", cfg.name)
		}
		return iottypes.Action{
			Lambda: &iottypes.LambdaAction{
				FunctionArn: aws.String(cfg.lambdaArn),
			},
		}, nil
	case modeSQS:
		if cfg.queueURL == "" || cfg.iotRoleArn == "" {
			return iottypes.Action{}, fmt.Errorf("SQS wiring not configured for rule %s", cfg.name)
		}
		return iottypes.Action{
			Sqs: &iottypes.SqsAction{
				QueueUrl:  aws.String(cfg.queueURL),
				RoleArn:   aws.String(cfg.iotRoleArn),
				UseBase64: aws.Bool(false),
			},
		}, nil
	default:
		return iottypes.Action{}, fmt.Errorf("invalid mode %q", mode)
	}
}

// flipRule replaces the rule's action with the requested mode while preserving
// the existing SQL, description, SQL version, disabled flag, and error action.
func flipRule(ctx context.Context, cfg ruleConfig, mode string) error {
	action, err := buildAction(cfg, mode)
	if err != nil {
		return err
	}

	out, err := awscommon.GetIoTClient().GetTopicRule(ctx, &iot.GetTopicRuleInput{
		RuleName: aws.String(cfg.name),
	})
	if err != nil {
		return fmt.Errorf("get rule %s: %w", cfg.name, err)
	}
	if out.Rule == nil {
		return fmt.Errorf("rule %s not found", cfg.name)
	}

	payload := &iottypes.TopicRulePayload{
		Sql:              out.Rule.Sql,
		Description:      out.Rule.Description,
		AwsIotSqlVersion: out.Rule.AwsIotSqlVersion,
		RuleDisabled:     out.Rule.RuleDisabled,
		Actions:          []iottypes.Action{action},
		ErrorAction:      out.Rule.ErrorAction,
	}

	_, err = awscommon.GetIoTClient().ReplaceTopicRule(ctx, &iot.ReplaceTopicRuleInput{
		RuleName:         aws.String(cfg.name),
		TopicRulePayload: payload,
	})
	if err != nil {
		return fmt.Errorf("replace rule %s: %w", cfg.name, err)
	}
	return nil
}

// systemContext returns an rmngctx with full system permissions, used for
// DB access on paths that have already authorised at the lambda layer
// (handlePut, after IsSuperAdmin) or that run as part of the deploy-time
// custom resource (handleReapply, no user involved).
func systemContext(ctx context.Context) *rmngctx.RmngContext {
	return rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
}

func handleGet(ctx context.Context) (events.APIGatewayProxyResponse, error) {
	presence, err := detectMode(ctx, presenceRuleName)
	if err != nil {
		rlog.Error(ctx).Err(err).Str("rule", presenceRuleName).Msg("Failed to read rule mode")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}
	publishInput, err := detectMode(ctx, publishInputRuleName)
	if err != nil {
		rlog.Error(ctx).Err(err).Str("rule", publishInputRuleName).Msg("Failed to read rule mode")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, modeResponse{
		Presence:     presence,
		PublishInput: publishInput,
	}), nil
}

func handlePut(ctx context.Context, request events.APIGatewayProxyRequest, updatedBy string) (events.APIGatewayProxyResponse, error) {
	var req modeRequest
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if req.Mode != modeDirect && req.Mode != modeSQS {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus(
			fmt.Sprintf("Invalid mode %q (expected %q or %q)", req.Mode, modeDirect, modeSQS),
		)), nil
	}

	// Persist first: if the DB write succeeds but a rule flip fails, the
	// stored mode is already correct and the next reapply will finish the
	// flip automatically. The reverse order (flip then persist) would leave
	// the rules in the new mode with no stored record, so a subsequent
	// deploy's reapply would see nil config and silently revert.
	//
	// IsSuperAdmin has already been verified by handleAPIRequest; the DB
	// call uses a SystemActor context so the in-DB IsAuthorized check is
	// satisfied regardless of the original caller's identity.
	adminDB := admin_config_db.NewAdminConfigDB(systemContext(ctx))
	if err := adminDB.SetIoTEventMode(req.Mode, req.Mode, updatedBy); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to persist runtime IoT event mode")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
	}

	for _, cfg := range []ruleConfig{presenceConfig(), publishInputConfig()} {
		if err := flipRule(ctx, cfg, req.Mode); err != nil {
			rlog.Error(ctx).Err(err).Str("rule", cfg.name).Str("mode", req.Mode).Msg("Failed to flip IoT rule mode")
			// DB row is already committed; the next reapply will retry the flip.
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus(err.Error())), nil
		}
	}

	return handleGet(ctx)
}

// handleReapply re-applies the durably-stored modes to both rules. Invoked
// by the AwsCustomResource on every stack create/update so a CloudFormation
// rewrite of the rule (forced by any non-action property change) is healed
// back to the runtime-set mode before the deploy completes.
func handleReapply(ctx context.Context) (reapplyResponse, error) {
	adminDB := admin_config_db.NewAdminConfigDB(systemContext(ctx))
	cfg, err := adminDB.GetIoTEventMode()
	if err != nil {
		return reapplyResponse{Status: "error"}, err
	}
	if cfg == nil {
		// Fresh stack, never flipped. CFN-synthesized default is direct;
		// nothing to reapply.
		rlog.Info(ctx).Msg("iot_event_mode reapply: no stored row; leaving CFN default")
		return reapplyResponse{
			Status:       "noop",
			Presence:     modeDirect,
			PublishInput: modeDirect,
		}, nil
	}

	presence := cfg.Presence
	if presence == "" {
		presence = modeDirect
	}
	publishInput := cfg.PublishInput
	if publishInput == "" {
		publishInput = modeDirect
	}

	rlog.Info(ctx).Str("presence", presence).Str("publish_input", publishInput).Msg("iot_event_mode reapply: restoring stored mode")
	if err := flipRule(ctx, presenceConfig(), presence); err != nil {
		return reapplyResponse{Status: "error"}, err
	}
	if err := flipRule(ctx, publishInputConfig(), publishInput); err != nil {
		return reapplyResponse{Status: "error"}, err
	}
	return reapplyResponse{
		Status:       "applied",
		Presence:     presence,
		PublishInput: publishInput,
	}, nil
}

// isReapplyEvent reports whether the raw payload is a `{"action":"reapply"}`
// invocation from the AwsCustomResource path (vs. an API Gateway request).
func isReapplyEvent(raw json.RawMessage) bool {
	var probe reapplyRequest
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Action == reapplyAction
}

// dispatch routes by payload shape: API Gateway proxy events go through
// the existing GET/PUT auth flow; `{"action":"reapply"}` events from the
// drift-correction custom resource go through handleReapply.
func dispatch(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	if isReapplyEvent(raw) {
		return handleReapply(ctx)
	}

	var request events.APIGatewayProxyRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, fmt.Errorf("unrecognised payload shape: %w", err)
	}
	return handleAPIRequest(ctx, request)
}

func handleAPIRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	authedUser, ok := rctx.GetAccessor().(*user.User)
	if !ok || !authedUser.IsSuperAdmin(rctx) {
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Forbidden")), nil
	}

	switch request.HTTPMethod {
	case "GET":
		return handleGet(ctx)
	case "PUT":
		return handlePut(ctx, request, authedUser.GetID())
	default:
		return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
	}
}

func main() {
	awslambda.Start(dispatch)
}
