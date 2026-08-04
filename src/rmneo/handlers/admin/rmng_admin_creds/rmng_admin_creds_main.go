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

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Request carries an optional session-name suffix so concurrent admin sessions
// are distinguishable in CloudTrail. The AWS actions the returned credentials
// may perform are fixed by the session policy below, never by the caller.
type Request struct {
	SessionSuffix string `json:"session_suffix,omitempty" validate:"omitempty,max=32,alphanum"`
}

type Response struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
	Expiration   *int   `json:"expiration"`
}

// lambdaSessionPolicy ceilings the vended credentials to a read of the account's Lambda
// concurrency limit, which the post-deployment page displays. Nothing here may change a setting —
// the page reports, and the operator acts in the AWS console. The call is not resource-scoped in
// IAM, hence "*".
const lambdaSessionPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "lambda:GetAccountSettings"
      ],
      "Resource": "*"
    }
  ]
}`

// handleRequest is reached via AWS_IAM (SigV4) auth using the dashboard's
// identity-pool credentials, mirroring rmneo/handlers/admin/iot_event_mode. Super-admin is
// resolved from the request identity, not from a Cognito authorizer context
// (there is none on an AWS_IAM route).
func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req Request
	if err := rmngrequest.ExtractRequestStruct(request, &req); err != nil {
		rlog.Error(ctx).Err(err).Msg("Failed to extract credentials request")
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	rctx := user.NewContextWithAPIRequest(ctx, request)
	if rctx == nil {
		rlog.Error(ctx).Msg("Failed to resolve user context for admin creds")
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Unauthorized")), nil
	}
	authedUser, ok := rctx.GetAccessor().(*user.User)
	if !ok || !authedUser.IsSuperAdmin(rctx) {
		rlog.Error(rctx).Msg("Non-super-admin attempted to fetch admin credentials")
		return utils.APIGwRespJSON(http.StatusForbidden, utils.NewAPIStatus("Super admin privileges required")), nil
	}

	roleArn := os.Getenv("ADMIN_CREDS_ROLE_ARN")
	if roleArn == "" {
		rlog.Error(rctx).Msg("ADMIN_CREDS_ROLE_ARN environment variable is not set")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Internal server error")), nil
	}

	resp, err := assumeAdminCredsRole(ctx, roleArn, req.SessionSuffix)
	if err != nil {
		rlog.Error(rctx).Err(err).Msg("Error assuming rmng admin-creds role")
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Error assuming role")), nil
	}

	return utils.APIGwRespJSON(http.StatusOK, resp), nil
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

	sessionName := "RmngAdminSession"
	if sessionSuffix != "" {
		sessionName += "-" + sessionSuffix
	}

	out, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleArn),
		RoleSessionName: aws.String(sessionName),
		Policy:          aws.String(lambdaSessionPolicy),
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
