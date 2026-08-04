// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

type APIStatus struct {
	Message string `json:"message"`
}

func NewAPIStatus(message string) *APIStatus {
	return &APIStatus{Message: message}
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
