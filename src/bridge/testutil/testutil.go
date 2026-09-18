// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package testutil holds shared test setup for the bridge module's Go tests.
package testutil

import (
	"github.com/espressif/esp-rainmaker-neo/src/bridge/db/bridge_children_db"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"

	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
)

// RegisterTestTables registers the bridge module's DynamoDB tables on the mock
// client that core's test_utils.TestSetup() installed. Call it after TestSetup()
// so bridge tests get their tables without core knowing about bridge resources.
func RegisterTestTables() {
	m := awscommon.GetDynamoDBClient().(*mock.DynamoDBMock)
	m.AddTable(bridge_children_db.BridgeChildrenTable, "parent_node_id", "child_node_id")
}
