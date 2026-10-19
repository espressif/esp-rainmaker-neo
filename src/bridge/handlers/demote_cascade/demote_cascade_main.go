//go:build !scalable

// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/aws/aws-lambda-go/lambda"
)

// main starts the Lambda in direct-invocation mode. Core async-invokes it under the node-type-changed hook name when a re-claim changes or clears a node's non-empty node_type. See docs/en/specs/bridge.md §5.8.
func main() {
	lambda.Start(handleDemoteCascade)
}
