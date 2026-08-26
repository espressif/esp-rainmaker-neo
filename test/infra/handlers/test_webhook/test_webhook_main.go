// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/espressif/esp-rainmaker-neo/src/utils"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngctx"
	"github.com/espressif/esp-rainmaker-neo/test/infra/webhook"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type tokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type pairRequest struct {
	FabricID      string `json:"fabricId"`
	DeviceNodeID  string `json:"deviceNodeId"`
	AdminVendorID string `json:"adminVendorId"`
	CSRNonce      string `json:"csrNonce"`
}

type pairResponse struct {
	EndpointID string `json:"endpointId"`
}

type commandRequest struct {
	Payload     string `json:"payload"`
	PayloadType int    `json:"payloadType"`
}

func newContext(ctx context.Context) *rmngctx.RmngContext {
	// The mock has no end user; run every request as the system actor.
	return rmngctx.NewRmngContextWithCtx(ctx, utils.NewSystemActor())
}

// bearerToken returns the token from the Authorization header, tolerating either
// header casing API Gateway may deliver. Empty when absent.
func bearerToken(request events.APIGatewayProxyRequest) string {
	const prefix = "Bearer "
	for k, v := range request.Headers {
		if http.CanonicalHeaderKey(k) == "Authorization" {
			if len(v) > len(prefix) && v[:len(prefix)] == prefix {
				return v[len(prefix):]
			}
		}
	}
	return ""
}

// header does a case-insensitive header lookup.
func header(request events.APIGatewayProxyRequest, name string) string {
	for k, v := range request.Headers {
		if http.CanonicalHeaderKey(k) == http.CanonicalHeaderKey(name) {
			return v
		}
	}
	return ""
}

// parseBody unmarshals the JSON request body into a generic map.
func parseBody(request events.APIGatewayProxyRequest) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if request.Body == "" {
		return body, nil
	}
	if err := json.Unmarshal([]byte(request.Body), &body); err != nil {
		return nil, err
	}
	return body, nil
}

// --- Token endpoints -------------------------------------------------------

func handleToken(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	refreshToken := extractRefreshToken(request)
	if refreshToken == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("refresh_token required")), nil
	}
	resp, err := webhook.IssueToken(refreshToken)
	if err != nil {
		rlog.Error(newContext(ctx)).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to issue token")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, resp), nil
}

// extractRefreshToken reads refresh_token from either a JSON body (generic/core
// flow) or a form-urlencoded body (Alexa's OAuth refresh sends the latter).
func extractRefreshToken(request events.APIGatewayProxyRequest) string {
	var req tokenRequest
	if json.Unmarshal([]byte(request.Body), &req) == nil && req.RefreshToken != "" {
		return req.RefreshToken
	}
	if form, err := url.ParseQuery(request.Body); err == nil {
		return form.Get("refresh_token")
	}
	return ""
}

func handleGVAToken(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	resp, err := webhook.IssueGVAToken()
	if err != nil {
		rlog.Error(newContext(ctx)).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to issue token")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, resp), nil
}

// handleSTToken answers a SmartThings accessTokenRequest. The Schema flow posts
// an envelope carrying either a code (grantCallbackAccess) or a refreshToken, and
// expects the tokens nested under callbackAuthentication. The code/refresh value
// is echoed back as the access token so a test can predict what the state
// callback will present, which is the key the capture endpoint stores under.
func handleSTToken(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, err := parseBody(request)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	auth, _ := body["callbackAuthentication"].(map[string]interface{})
	if auth == nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("callbackAuthentication is required")), nil
	}
	grant, _ := auth["code"].(string)
	if grant == "" {
		grant, _ = auth["refreshToken"].(string)
	}
	if grant == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("code or refreshToken is required")), nil
	}

	requestID := ""
	if headers, ok := body["headers"].(map[string]interface{}); ok {
		requestID, _ = headers["requestId"].(string)
	}

	return utils.APIGwRespJSON(http.StatusOK, webhook.IssueSTToken(requestID, grant, grant)), nil
}

// --- Capture (data) endpoints ---------------------------------------------

func handleCoreData(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if err := webhook.VerifyToken(bearerToken(request)); err != nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid or expired token")), nil
	}
	rmngCtx := newContext(ctx)

	body, err := parseBody(request)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	uuid, _ := body["uuid"].(string)
	if uuid == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("uuid is required")), nil
	}
	delete(body, "uuid")

	if err := webhook.StoreCoreData(rmngCtx, uuid, body); err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store data")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, nil), nil
}

func handleAlexaData(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if err := webhook.VerifyToken(bearerToken(request)); err != nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid or expired token")), nil
	}
	rmngCtx := newContext(ctx)

	uuid := header(request, "X-Alexa-UUID")
	if uuid == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("X-Alexa-UUID header is required")), nil
	}
	body, err := parseBody(request)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if err := webhook.StoreAlexaData(rmngCtx, uuid, body); err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store data")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, nil), nil
}

func handleGVAData(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	if err := webhook.VerifyToken(bearerToken(request)); err != nil {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Invalid or expired token")), nil
	}
	rmngCtx := newContext(ctx)

	body, err := parseBody(request)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	uuid, _ := body["agentUserId"].(string)
	if uuid == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("agentUserId is required in body")), nil
	}

	if err := webhook.StoreGVAData(rmngCtx, uuid, body); err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store data")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, nil), nil
}

// handleSTData captures a SmartThings state callback. Unlike Alexa (uuid header)
// and GVA (agentUserId in the body), the ST envelope names no user: the callback
// URL and bearer token are what identify the recipient. The token is therefore
// the capture key, and the test reads back with the same value it seeded on the
// user's callback row.
func handleSTData(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	token := bearerToken(request)
	if token == "" {
		return utils.APIGwRespJSON(http.StatusUnauthorized, utils.NewAPIStatus("Bearer token is required")), nil
	}
	rmngCtx := newContext(ctx)

	body, err := parseBody(request)
	if err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}

	if err := webhook.StoreSTData(rmngCtx, token, body); err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to store data")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, nil), nil
}

// --- Validate (read-back) endpoints ---------------------------------------

func handleValidate(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rmngCtx := newContext(ctx)

	uuid := request.QueryStringParameters["uuid"]
	if uuid == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("uuid is required")), nil
	}

	var (
		payload json.RawMessage
		err     error
	)
	switch request.Resource {
	case "/v1/alexa/validate":
		payload, err = webhook.ValidateAlexaData(rmngCtx, uuid)
	case "/v1/gva/validate":
		payload, err = webhook.ValidateGVAData(rmngCtx, uuid)
	case "/v1/smartthings/validate":
		payload, err = webhook.ValidateSTData(rmngCtx, uuid)
	default:
		payload, err = webhook.ValidateCoreData(rmngCtx, uuid)
	}

	if err != nil {
		rlog.Error(rmngCtx).Err(err).Send()
		if errors.Is(err, webhook.ErrGone) {
			return utils.APIGwRespJSON(http.StatusGone, utils.NewAPIStatus("data expired or not found")), nil
		}
		if errors.Is(err, webhook.ErrInvalidChannel) {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("invalid data format")), nil
		}
		return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to read data")), nil
	}
	return utils.APIGwRespJSON(http.StatusOK, payload), nil
}

// --- Matter pair / command relay ------------------------------------------

func handlePair(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req pairRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("Invalid request body")), nil
	}
	if req.FabricID == "" || req.DeviceNodeID == "" || req.AdminVendorID == "" || req.CSRNonce == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("fabricId, deviceNodeId, adminVendorId, and csrNonce are required")), nil
	}
	endpointID := webhook.Pair(req.FabricID, req.DeviceNodeID, req.AdminVendorID, req.CSRNonce)
	return utils.APIGwRespJSON(http.StatusOK, pairResponse{EndpointID: endpointID}), nil
}

func handleCommand(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	endpointID := request.PathParameters["endpointId"]
	if endpointID == "" {
		return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("endpointId is required")), nil
	}
	rmngCtx := newContext(ctx)

	switch request.HTTPMethod {
	case "POST":
		var req commandRequest
		if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("payload must be valid JSON")), nil
		}
		if req.Payload == "" {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("payload is required")), nil
		}
		record, err := webhook.EnqueueCommand(rmngCtx, endpointID, req.Payload, req.PayloadType)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Send()
			if errors.Is(err, webhook.ErrInvalidPayload) || errors.Is(err, webhook.ErrMissingTopic) {
				return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("payload must contain a topic")), nil
			}
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to enqueue command")), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, record), nil

	case "GET":
		topic := request.QueryStringParameters["topic"]
		if topic == "" {
			return utils.APIGwRespJSON(http.StatusBadRequest, utils.NewAPIStatus("topic query parameter is required")), nil
		}
		record, err := webhook.DequeueCommand(rmngCtx, endpointID, topic)
		if err != nil {
			rlog.Error(rmngCtx).Err(err).Send()
			if errors.Is(err, webhook.ErrCommandNotFound) {
				return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("no command found for this endpoint and topic")), nil
			}
			return utils.APIGwRespJSON(http.StatusInternalServerError, utils.NewAPIStatus("Failed to read command")), nil
		}
		return utils.APIGwRespJSON(http.StatusOK, record), nil
	}
	return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
}

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	switch request.Resource {
	case "/v1/token", "/v1/alexa/token":
		if request.HTTPMethod == "POST" {
			return handleToken(ctx, request)
		}
	case "/v1/validate", "/v1/alexa/validate", "/v1/gva/validate", "/v1/smartthings/validate":
		if request.HTTPMethod == "GET" {
			return handleValidate(ctx, request)
		}
	case "/v1/data":
		if request.HTTPMethod == "POST" {
			return handleCoreData(ctx, request)
		}
	case "/v1/alexa/data":
		if request.HTTPMethod == "POST" {
			return handleAlexaData(ctx, request)
		}
	case "/v1/gva/token":
		if request.HTTPMethod == "POST" {
			return handleGVAToken(ctx, request)
		}
	case "/v1/gva/data":
		if request.HTTPMethod == "POST" {
			return handleGVAData(ctx, request)
		}
	case "/v1/smartthings/token":
		if request.HTTPMethod == "POST" {
			return handleSTToken(ctx, request)
		}
	case "/v1/smartthings/data":
		if request.HTTPMethod == "POST" {
			return handleSTData(ctx, request)
		}
	case "/v1/pair":
		if request.HTTPMethod == "POST" {
			return handlePair(ctx, request)
		}
	case "/v1/{endpointId}/command":
		if request.HTTPMethod == "GET" || request.HTTPMethod == "POST" {
			return handleCommand(ctx, request)
		}
	default:
		return utils.APIGwRespJSON(http.StatusNotFound, utils.NewAPIStatus("Not found")), nil
	}
	return utils.APIGwRespJSON(http.StatusMethodNotAllowed, utils.NewAPIStatus("Method not allowed")), nil
}

func main() {
	lambda.Start(handler)
}
