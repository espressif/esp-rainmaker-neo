// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node"
	"github.com/espressif/esp-rainmaker-neo/src/rmneo/node/node_reset_handler"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"

	"github.com/aws/aws-lambda-go/lambda"
)

func handleRequest(ctx context.Context, event node.NodeDataResetEvent) error {
	err := node_reset_handler.HandleNodeDataReset(ctx, event)
	if err != nil {
		rlog.Error(ctx).Err(err).Msg("failed to handle node data reset")
	}
	return err
}

func main() {
	lambda.Start(handleRequest)
}
