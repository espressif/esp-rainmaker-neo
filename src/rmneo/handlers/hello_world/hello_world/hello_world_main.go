// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"

	"github.com/espressif/esp-rainmaker-neo/src/utils"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	return utils.APIGwRespText(http.StatusOK, "Hello, World!"), nil
}

func main() {
	lambda.Start(handler)
}
