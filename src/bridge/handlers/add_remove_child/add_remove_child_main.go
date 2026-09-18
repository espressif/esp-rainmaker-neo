//go:build !scalable

// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/aws/aws-lambda-go/lambda"
)

// main starts the Lambda in direct-invocation mode (the IoT rule
// invokes this Lambda directly).
// Build: go build -o handler ./src/bridge/add_remove_child/
func main() {
	lambda.Start(handleBridgeEvent)
}
