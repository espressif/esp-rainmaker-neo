// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package smartthings

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/ssmutil"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/db/user_integration_db"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/notification"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/user"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"

	"github.com/google/uuid"
)

const (
	// stPlatform is the integration_id under which SmartThings callback tokens are stored in rmng-user-endpoints.
	stPlatform = "smartthings"
)

// accessTokenRequest is the request body sent to SmartThings to exchange a code or refresh token.
type accessTokenRequest struct {
	Headers                accessTokenRequestHeaders `json:"headers"`
	CallbackAuthentication accessTokenRequestAuth    `json:"callbackAuthentication"`
}

type accessTokenRequestHeaders struct {
	Schema          string `json:"schema"`
	Version         string `json:"version"`
	InteractionType string `json:"interactionType"`
	RequestID       string `json:"requestId"`
}

type accessTokenRequestAuth struct {
	GrantType    string `json:"grantType"`
	Code         string `json:"code,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// accessTokenResponse is the response from SmartThings containing the actual tokens.
type accessTokenResponse struct {
	Headers                accessTokenResponseHeaders `json:"headers"`
	CallbackAuthentication accessTokenResponseAuth    `json:"callbackAuthentication"`
}

type accessTokenResponseHeaders struct {
	Schema          string `json:"schema"`
	Version         string `json:"version"`
	InteractionType string `json:"interactionType"`
	RequestID       string `json:"requestId"`
}

type accessTokenResponseAuth struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

// HandleGrantCallbackAccess processes a grantCallbackAccess interaction from SmartThings.
// It exchanges the authorization code for actual tokens via the SmartThings token endpoint,
// then stores the tokens in the Users DynamoDB table keyed by user ID.
func HandleGrantCallbackAccess(ctx context.Context, request STRequest) (STResponse, error) {
	// Validate the user's access token and extract user ID
	userID, err := GetUserIDFromToken(ctx, request.Authentication.Token)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to get user ID from token for grantCallbackAccess")
	}

	// Validate that callback authentication data is present
	if request.CallbackAuthentication == nil {
		return STResponse{}, rmerror.NewRMError(nil, "callbackAuthentication is missing from request")
	}
	if request.CallbackURLs == nil {
		return STResponse{}, rmerror.NewRMError(nil, "callbackUrls is missing from request")
	}
	if request.CallbackAuthentication.Code == "" {
		return STResponse{}, rmerror.NewRMError(nil, "callbackAuthentication.code is missing from request")
	}
	if request.CallbackURLs.OAuthToken == "" {
		return STResponse{}, rmerror.NewRMError(nil, "callbackUrls.oauthToken is missing from request")
	}

	// Exchange the authorization code for actual tokens
	tokenResp, err := exchangeCodeForTokens(ctx, request.CallbackAuthentication.Code, request.CallbackURLs.OAuthToken)
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to exchange authorization code for tokens")
	}

	// Calculate expires_at from the response's expiresIn
	expiresAt := time.Now().Unix() + tokenResp.CallbackAuthentication.ExpiresIn

	// Store the callback bundle as one (user, smartthings, endpoint) row in rmng-user-endpoints.
	// The state-callback URL is the endpoint's natural identifier (like webhook integrations),
	// so re-linking against the same regional endpoint overwrites the same row.
	callingUser := user.NewUser(userID)
	rmngCtx := rmngctx.NewRmngContextWithCtx(ctx, callingUser)
	err = callingUser.RegisterClient(rmngCtx, user_integration_db.UserIntegrationEntry{
		IntegrationID: stPlatform,
		EndpointID:    user_integration_db.EncodeEndpointID(request.CallbackURLs.StateCallback),
		TokenCallbackURL: request.CallbackURLs.OAuthToken,
		IntegrationToken: &user_integration_db.IntegrationToken{
			AccessToken:  tokenResp.CallbackAuthentication.AccessToken,
			RefreshToken: tokenResp.CallbackAuthentication.RefreshToken,
			ExpiresAt:    expiresAt,
		},
	})
	if err != nil {
		return STResponse{}, rmerror.NewRMError(err, "failed to store callback tokens")
	}

	rlog.Info(ctx).Str("userID", userID).Msg("callback tokens stored successfully")

	return STResponse{
		Headers: STHeaders{
			Schema:          request.Headers.Schema,
			Version:         request.Headers.Version,
			InteractionType: InteractionGrantCallbackAccess,
			RequestID:       request.Headers.RequestID,
		},
	}, nil
}

// getSTClientCredentials fetches the SmartThings Client ID and Client Secret from SSM. These are
// the credentials issued by SmartThings during Schema App registration. They are required to
// build accessTokenRequest payloads — the grantCallbackAccess interaction echoes the clientId
// but never includes the clientSecret, so it must be read from our own configuration store.
func getSTClientCredentials(ctx context.Context) (clientID string, clientSecret string, err error) {
	clientID, err = ssmutil.GetParameterWithCaching(ctx, "/rmng/smartthings/client_id", false)
	if err != nil {
		return "", "", rmerror.NewRMError(err, "failed to get SmartThings client_id from SSM")
	}
	clientSecret, err = ssmutil.GetParameterWithCaching(ctx, "/rmng/smartthings/client_secret", true)
	if err != nil {
		return "", "", rmerror.NewRMError(err, "failed to get SmartThings client_secret from SSM")
	}
	return clientID, clientSecret, nil
}

// exchangeCodeForTokens sends an accessTokenRequest to SmartThings to exchange
// an authorization code for access and refresh tokens.
func exchangeCodeForTokens(ctx context.Context, code, oauthTokenURL string) (*accessTokenResponse, error) {
	clientID, clientSecret, err := getSTClientCredentials(ctx)
	if err != nil {
		return nil, err
	}

	reqBody := accessTokenRequest{
		Headers: accessTokenRequestHeaders{
			Schema:          "st-schema",
			Version:         "1.0",
			InteractionType: "accessTokenRequest",
			RequestID:       uuid.New().String(),
		},
		CallbackAuthentication: accessTokenRequestAuth{
			GrantType:    "authorization_code",
			Code:         code,
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
	}

	return postAccessTokenRequest(reqBody, oauthTokenURL)
}

// postAccessTokenRequest marshals and sends an accessTokenRequest, then parses the response.
func postAccessTokenRequest(reqBody accessTokenRequest, url string) (*accessTokenResponse, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to marshal accessTokenRequest")
	}

	rlog.Debug(context.TODO()).Str("url", url).Str("interactionType", reqBody.Headers.InteractionType).
		Str("grantType", reqBody.CallbackAuthentication.GrantType).Msg("sending accessTokenRequest to SmartThings")

	// Use the shared (injectable) HTTP client — consistent with the state-callback path and
	// unit-testable. MakeHTTPPostRequest returns an error for any non-2XX response.
	body, err := notification.MakeHTTPPostRequest(jsonData, url, func(req *http.Request) error {
		req.Header.Set("Content-Type", "application/json")
		return nil
	})
	if err != nil {
		return nil, rmerror.NewRMError(err, "failed to POST accessTokenRequest")
	}

	var tokenResp accessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, rmerror.NewRMError(err, "failed to parse accessTokenResponse")
	}

	if tokenResp.CallbackAuthentication.AccessToken == "" {
		return nil, rmerror.NewRMError(nil, "accessTokenResponse contains empty accessToken")
	}

	return &tokenResp, nil
}
