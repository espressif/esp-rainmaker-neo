// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/espressif/esp-rainmaker-neo/src/gva"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmngrequest"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	rlog.Trace(ctx).Interface("event", rmngrequest.RedactForLog(event)).Send()

	accessToken := rmngrequest.ExtractAuthToken(event.Headers)
	if accessToken == "" {
		rlog.Error(ctx).Msg("failed to extract access token")
		return events.APIGatewayProxyResponse{
			StatusCode: 401,
			Body:       `{"error": "unauthorized"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// Parse the GVA request from the body
	var request gva.GVARequest
	if err := json.Unmarshal([]byte(event.Body), &request); err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to parse GVA request")
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       `{"error": "invalid request format"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	var resp gva.GVAResponse
	var handlerErr error

	// Route based on intent
	if len(request.Inputs) == 0 {
		handlerErr = fmt.Errorf("no inputs in request")
	} else {
		intent := request.Inputs[0].Intent
		switch intent {
		case gva.IntentSync:
			resp, handlerErr = gva.HandleSync(ctx, request, accessToken)
		case gva.IntentQuery:
			resp, handlerErr = gva.HandleQuery(ctx, request, accessToken)
		case gva.IntentExecute:
			resp, handlerErr = gva.HandleExecute(ctx, request, accessToken)
		case gva.IntentDisconnect:
			resp, handlerErr = gva.HandleDisconnect(ctx, request, accessToken)
		default:
			handlerErr = fmt.Errorf("unsupported intent: %s", intent)
		}
	}

	if handlerErr != nil {
		rlog.Error(ctx).Err(handlerErr).Send()
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       `{"error": "internal server error"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	// Marshal response to JSON
	responseBody, err := json.Marshal(resp)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to marshal response")
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       `{"error": "internal server error"}`,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		}, nil
	}

	rlog.Info(ctx).Interface("resp", resp).Send()

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(responseBody),
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}, nil
}

func main() {
	lambda.Start(handler)
}
