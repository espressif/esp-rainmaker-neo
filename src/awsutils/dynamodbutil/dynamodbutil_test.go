// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"testing"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDynamoDBUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dynamodbutil Suite")
}

const (
	rowsTable   = "rows"
	byOwner     = "by_owner"
	byStatus    = "by_status"
	singleTable = "singles"
)

// row lives in a composite-key table with a GSI on owner.
type row struct {
	PK     string `dynamodbav:"pk"`
	SK     string `dynamodbav:"sk"`
	Owner  string `dynamodbav:"owner,omitempty"`
	Status string `dynamodbav:"status,omitempty"`
	N      int    `dynamodbav:"n,omitempty"`
}

// single lives in a hash-only table.
type single struct {
	ID   string `dynamodbav:"id"`
	Data string `dynamodbav:"data,omitempty"`
}

func newMock() *mock.DynamoDBMock {
	m := mock.NewDynamoDBMock()
	m.AddTable(rowsTable, "pk", "sk")
	Expect(m.AddSecondaryIndex(byOwner, rowsTable, "owner", "sk")).To(Succeed())
	m.AddTable(singleTable, "id", "")
	return m
}

func rowsOf(m *mock.DynamoDBMock) *dynamodbutil.Table[row] {
	return dynamodbutil.NewTable[row](m, dynamodbutil.Schema{
		Name:     rowsTable,
		HashKey:  "pk",
		RangeKey: "sk",
		Indexes:  map[string]dynamodbutil.IndexKeys{byOwner: {HashKey: "owner", RangeKey: "sk"}},
	})
}

func singlesOf(m *mock.DynamoDBMock) *dynamodbutil.Table[single] {
	return dynamodbutil.NewTable[single](m, dynamodbutil.Schema{Name: singleTable, HashKey: "id"})
}
