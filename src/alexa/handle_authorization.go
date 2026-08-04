// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package alexa_skill

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/httpclient"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	rmngctx "github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
)

type AlexaTokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Token_type   string `json:"token_type"`
	Expires_in   int64  `json:"expires_in"`
}

const AwsTokenUrl = "https://api.amazon.com/auth/o2/token"

func HandleAcceptGrant(ctx context.Context, request AlexaRequest) (AlexaResponse, error) {
	var payload AcceptGrantPayload
	if err := json.Unmarshal(request.Directive.Payload, &payload); err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "invalid payload format")
	}

	alexaClientID, alexaClientSecret, err := GetAlexaClientDetails(ctx)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to get client details")
	}

	// Get LWA access token using the auth code
	tokenResp, err := GetLWAAccessTokenfromAuthCode(payload.Grant.Code, alexaClientID, alexaClientSecret)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to get access token")
	}

	// User identity comes from the grantee (Cognito) token. We don't call LWA /user/profile: grant.code yields an Alexa event-gateway token (scope alexa::async_event:write only), which 401s on /user/profile. No Amazon user_id is available here.
	userID, err := user.GetUserIDFromToken(ctx, payload.Grantee.Token)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to get user id")
	}

	// AcceptGrant runs in the regional Smart Home Lambda, so its AWS region is the
	// user's Alexa region -- store it to pick the right event gateway for ChangeReports.
	region := os.Getenv("AWS_REGION")

	callingUser := user.NewUser(userID)
	err = StoreClientToken(callingUser, ctx, tokenResp, region)
	if err != nil {
		return AlexaResponse{}, rmerror.NewRMError(err, "failed to store client token")
	}

	return CreateResponse(
		request.Directive.Header.MessageID,
		"Alexa.Authorization",
		"AcceptGrant.Response",
		struct{}{},
		"",
		nil,
	), nil
}

func GetLWAAccessTokenfromAuthCode(authCode, alexaClientID, alexaClientSecret string) (AlexaTokenResp, error) {
	if authCode == "" {
		return AlexaTokenResp{}, fmt.Errorf("authorization code absent")
	}

	client := httpclient.Get()
	awsReqqueryString := "grant_type=authorization_code" + "&" + "code" + "=" + authCode + "&" + "client_id" + "=" + alexaClientID + "&" + "client_secret" + "=" + alexaClientSecret + "&" + "redirect_uri" + "=" + ""
	payload := strings.NewReader(awsReqqueryString)

	rlog.Info(context.TODO()).Str("method", http.MethodPost).Strs("query_params", []string{"grant_type=authorization_code", "client_id=" + alexaClientID}).Str("url", AwsTokenUrl).Send()

	req, err := http.NewRequest(http.MethodPost, AwsTokenUrl, payload)
	if err != nil {
		return AlexaTokenResp{}, rmerror.NewRMError(err, "error contacting the AWS server")
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return AlexaTokenResp{}, rmerror.NewRMError(err, "error contacting the AWS server")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return AlexaTokenResp{}, rmerror.NewRMError(err, "internal server error")
	}

	if res.StatusCode != 200 {
		return AlexaTokenResp{}, rmerror.NewRMError(fmt.Errorf("error contacting the AWS server"), "error contacting the AWS server")
	}

	t := AlexaTokenResp{}
	if err := json.Unmarshal(body, &t); err != nil {
		return t, rmerror.NewRMError(err, "error contacting the AWS server")
	}
	return t, nil
}

func StoreClientToken(callingUser *user.User, ctx context.Context, tokenResponse AlexaTokenResp, region string) error {
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, callingUser)
	// One Alexa endpoint per user (user_id is the PK), so endpoint_id is the constant integration name — re-linking overwrites in place, so the previous alexa app stops receiving updates.
	return callingUser.RegisterClient(rmngCtx, user_integration_db.UserIntegrationEntry{
		IntegrationID: AlexaPlatform,
		EndpointID:    user_integration_db.EncodeEndpointID(AlexaPlatform),
		IntegrationToken: &user_integration_db.IntegrationToken{
			AccessToken:  tokenResponse.AccessToken,
			RefreshToken: tokenResponse.RefreshToken,
			ExpiresAt:    time.Now().Unix() + tokenResponse.Expires_in,
			TokenType:    tokenResponse.Token_type,
			Region:       region,
		},
	})
}
