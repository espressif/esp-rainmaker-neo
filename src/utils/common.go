// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-lambda-go/events"
)

type APIStatus struct {
	Message string `json:"message"`
}

func NewAPIStatus(message string) *APIStatus {
	return &APIStatus{Message: message}
}

// APIErrorRespJSON is the API Gateway response a client sees for err: rmerror.NewRMError(nil, "x").WithClientStatus(403, "No access") -> 403 {"message":"No access"}; any other error -> 500 {"message":"Internal Server Error"}, so internal text never reaches the client.
func APIErrorRespJSON(err error) events.APIGatewayProxyResponse {
	return APIGwRespJSON(apiStatusFromError(err))
}

func apiStatusFromError(err error) (int, *APIStatus) {
	var rmErr *rmerror.RMError
	if !errors.As(err, &rmErr) {
		return http.StatusInternalServerError, NewAPIStatus(http.StatusText(http.StatusInternalServerError))
	}
	status, message := rmErr.ClientError()
	return status, NewAPIStatus(message)
}

// CreateAPIGatewayResponse creates a standardized API Gateway response
func APIGwRespJSON(statusCode int, body interface{}) events.APIGatewayProxyResponse {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		// If marshaling fails, return an error response
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Content-Type": "application/json"},
			Body:       `{"error": "Failed to marshal response body"}`,
		}
	}
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Content-Type": "application/json"},
		Body:       string(jsonBody),
	}
}

func APIGwRespText(statusCode int, body string) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers:    map[string]string{"Access-Control-Allow-Origin": "*", "X-Content-Type-Options": "nosniff", "Content-Type": "text/plain"},
		Body:       body,
	}
}
