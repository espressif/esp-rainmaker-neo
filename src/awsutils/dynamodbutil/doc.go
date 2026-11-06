// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

// Package dynamodbutil holds the DynamoDB access patterns every DB layer shares: typed tables, paged queries that apply Limit after the filter, batches that retry throttled items, and transactions.
//
// From this repository it imports only awscommon, rmerror and parallel; authorisation stays with the caller.
//
// Atomic read-modify-write on one item: read it, then Update with WriteOpts{If: <attribute still holds what you read>} and retry when IsConditionFailed. Across items: TransactWrite, with CheckOp for anything that must hold but is not written.
package dynamodbutil
