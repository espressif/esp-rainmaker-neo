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

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Request carries an optional session-name suffix so multiple concurrent admin
// sessions are distinguishable in CloudTrail. The AWS actions the returned
// credentials may perform are fixed by the session policy below, never by the
// caller.
type Request struct {
	SessionSuffix string `json:"session_suffix,omitempty" validate:"omitempty,max=32,alphanum"`
}

type Response struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
	Expiration   *int   `json:"expiration"`
}

// messagingSessionPolicy ceilings the vended credentials to what the post-deployment page
// does itself: reading whether SES and SMS are still sandboxed, reading the SMS spend limit,
// and managing the sandbox
// destination numbers, because verifying a number is the one step an operator can complete without
// leaving the dashboard. Leaving a sandbox or moving a spend limit is an AWS-side action, so no
// account-level write is vended. None of these calls is resource-scoped in IAM, hence "*".
// Accounts migrated to AWS End User Messaging serve the SNS SMS APIs through PinpointSmsVoiceV2,
// so each SNS action also needs its sms-voice counterpart or the proxied call is denied.
const messagingSessionPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ses:GetAccount",
        "sns:GetSMSAttributes",
        "sns:GetSMSSandboxAccountStatus",
        "sns:ListSMSSandboxPhoneNumbers",
        "sns:CreateSMSSandboxPhoneNumber",
        "sns:VerifySMSSandboxPhoneNumber",
        "sns:DeleteSMSSandboxPhoneNumber",
        "sms-voice:DescribeSpendLimits",
        "sms-voice:DescribeAccountAttributes",
        "sms-voice:DescribeVerifiedDestinationNumbers",
        "sms-voice:CreateVerifiedDestinationNumber",
        "sms-voice:SendDestinationNumberVerificationCode",
        "sms-voice:VerifyDestinationNumber",
        "sms-voice:DeleteVerifiedDestinationNumber"
      ],
      "Resource": "*"
    }
  ]
}`

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req Request
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract credentials request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if !isSuperAdmin(request) {
		rlog.Error(ctx).Msg("Non-super-admin attempted to fetch admin credentials")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Super admin privileges required")), nil
	}

	roleArn := os.Getenv("ADMIN_CREDS_ROLE_ARN")
	if roleArn == "" {
		rlog.Error(ctx).Msg("ADMIN_CREDS_ROLE_ARN environment variable is not set")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	resp, err := assumeAdminCredsRole(ctx, roleArn, req.SessionSuffix)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("Error assuming espuser admin-creds role")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error assuming role")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, resp), nil
}

// isSuperAdmin reads the custom:super_admin claim the admin Cognito authorizer
// injected into the request context. The authorizer has already validated the
// token, so the claim is trusted here without re-verification.
func isSuperAdmin(request events.APIGatewayProxyRequest) bool {
	if request.RequestContext.Authorizer == nil {
		return false
	}
	claims, ok := request.RequestContext.Authorizer["claims"].(map[string]interface{})
	if !ok {
		return false
	}
	return claims["custom:super_admin"] == "true"
}

func assumeAdminCredsRole(ctx context.Context, roleArn, sessionSuffix string) (Response, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return Response{}, rmerror.NewRMError(err, "failed to load default config")
	}

	stsClient := awscommon.GetSTSClient()
	if stsClient == nil {
		stsClient = sts.NewFromConfig(cfg)
	}

	sessionName := "EspUserAdminSession"
	if sessionSuffix != "" {
		sessionName += "-" + sessionSuffix
	}

	out, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleArn),
		RoleSessionName: aws.String(sessionName),
		Policy:          aws.String(messagingSessionPolicy),
	})
	if err != nil {
		return Response{}, rmerror.NewRMError(err, "failed to assume admin-creds role")
	}

	var expiration *int
	if out.Credentials.Expiration != nil {
		exp := int(out.Credentials.Expiration.Unix())
		expiration = &exp
	}
	return Response{
		AccessKey:    *out.Credentials.AccessKeyId,
		SecretKey:    *out.Credentials.SecretAccessKey,
		SessionToken: *out.Credentials.SessionToken,
		Expiration:   expiration,
	}, nil
}

func main() {
	lambda.Start(handleRequest)
}
