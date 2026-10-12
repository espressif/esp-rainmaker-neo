//go:build !scalable

// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/aws/aws-lambda-go/lambda"
)

// main starts the Lambda in direct-invocation mode. The cascade-delete
// Lambda is async-invoked when a bridge is removed from its group (DELETE
// API or node_reset) or moved to a new group (assoc+disassoc).
// See docs/en/specs/bridge.md §5.8.
// Build: go build -o handler ./src/bridge/cascade_delete/
func main() {
	lambda.Start(handleCascadeDelete)
}
