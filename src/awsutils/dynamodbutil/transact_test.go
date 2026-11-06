// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"context"
	"errors"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TransactWrite", func() {
	var (
		ctx     context.Context
		m       *mock.DynamoDBMock
		rows    *dynamodbutil.Table[row]
		singles *dynamodbutil.Table[single]
	)

	BeforeEach(func() {
		ctx = context.Background()
		m = newMock()
		rows = rowsOf(m)
		singles = singlesOf(m)
	})

	It("applies every op across tables", func() {
		Expect(rows.Put(ctx, row{PK: "a", SK: "1", N: 1})).To(Succeed())
		Expect(singles.Put(ctx, single{ID: "gone"})).To(Succeed())

		Expect(dynamodbutil.TransactWrite(ctx, m,
			singles.CreateOp(single{ID: "new"}),
			rows.UpdateOp(rows.Key("a", "1"), expression.Set(expression.Name("n"), expression.Value(2))),
			singles.DeleteOp(singles.Key("gone")),
		)).To(Succeed())

		_, found, _ := singles.Get(ctx, singles.Key("new"))
		Expect(found).To(BeTrue())
		got, _, _ := rows.Get(ctx, rows.Key("a", "1"))
		Expect(got.N).To(Equal(2))
		_, found, _ = singles.Get(ctx, singles.Key("gone"))
		Expect(found).To(BeFalse())
	})

	It("applies nothing when one condition fails, and reports which op failed", func() {
		Expect(singles.Put(ctx, single{ID: "taken"})).To(Succeed())

		err := dynamodbutil.TransactWrite(ctx, m,
			rows.PutOp(row{PK: "a", SK: "1"}),
			singles.CreateOp(single{ID: "taken", Data: "overwrite"}),
		)
		Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
		var tce *types.TransactionCanceledException
		Expect(errors.As(err, &tce)).To(BeTrue())
		Expect(*tce.CancellationReasons[0].Code).To(Equal("None"))
		Expect(*tce.CancellationReasons[1].Code).To(Equal("ConditionalCheckFailed"))

		_, found, _ := rows.Get(ctx, rows.Key("a", "1"))
		Expect(found).To(BeFalse())
		got, _, _ := singles.Get(ctx, singles.Key("taken"))
		Expect(got.Data).To(BeEmpty())
	})

	It("gates the writes on a CheckOp without writing the checked item", func() {
		Expect(singles.Put(ctx, single{ID: "owner", Data: "alice"})).To(Succeed())
		isBob := expression.Name("data").Equal(expression.Value("bob"))

		err := dynamodbutil.TransactWrite(ctx, m,
			singles.CheckOp(singles.Key("owner"), isBob),
			rows.PutOp(row{PK: "a", SK: "1"}),
		)
		Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
		_, found, _ := rows.Get(ctx, rows.Key("a", "1"))
		Expect(found).To(BeFalse())
	})

	It("does nothing for no ops", func() {
		Expect(dynamodbutil.TransactWrite(ctx, m)).To(Succeed())
	})

	It("rejects more ops than DynamoDB allows before calling it", func() {
		ops := make([]dynamodbutil.TxOp, 101)
		for i := range ops {
			ops[i] = rows.PutOp(row{PK: "a", SK: string(rune('a' + i%26))})
		}
		Expect(dynamodbutil.TransactWrite(ctx, m, ops...)).To(MatchError(ContainSubstring("DynamoDB allows 100")))
	})

	It("surfaces an op's build error", func() {
		err := dynamodbutil.TransactWrite(ctx, m, singles.CheckOp(singles.Key("x"), expression.ConditionBuilder{}))
		Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring("CheckOp needs a condition"))
	})
})
