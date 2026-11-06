// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"context"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Item operations", func() {
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

	Describe("Get", func() {
		It("returns the item and found=true", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "on"})).To(Succeed())
			got, found, err := rows.Get(ctx, rows.Key("a", "1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(got).To(Equal(row{PK: "a", SK: "1", Status: "on"}))
		})

		It("returns found=false and no error for a missing item", func() {
			got, found, err := rows.Get(ctx, rows.Key("a", "missing"))
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
			Expect(got).To(BeZero())
		})

		It("works on a hash-only table", func() {
			Expect(singles.Put(ctx, single{ID: "x", Data: "d"})).To(Succeed())
			got, found, err := singles.Get(ctx, singles.Key("x"))
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(got.Data).To(Equal("d"))
		})
	})

	Describe("Get with Project", func() {
		It("reads an item holding none of the projected attributes as not found", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Owner: "o"})).To(Succeed())
			_, found, err := rows.Get(ctx, rows.Key("a", "1"), dynamodbutil.ReadOpts{Project: []string{"status"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse(), "DynamoDB returns an empty item when no projected attribute exists")
		})
	})

	Describe("Create", func() {
		It("writes a new item", func() {
			Expect(rows.Create(ctx, row{PK: "a", SK: "1"})).To(Succeed())
			_, found, _ := rows.Get(ctx, rows.Key("a", "1"))
			Expect(found).To(BeTrue())
		})

		It("fails with a condition error when the key exists, leaving the item unchanged", func() {
			Expect(rows.Create(ctx, row{PK: "a", SK: "1", Status: "first"})).To(Succeed())
			err := rows.Create(ctx, row{PK: "a", SK: "1", Status: "second"})
			Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
			got, _, _ := rows.Get(ctx, rows.Key("a", "1"))
			Expect(got.Status).To(Equal("first"))
		})
	})

	Describe("Put", func() {
		It("replaces an existing item whole", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "on", N: 3})).To(Succeed())
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "off"})).To(Succeed())
			got, _, _ := rows.Get(ctx, rows.Key("a", "1"))
			Expect(got).To(Equal(row{PK: "a", SK: "1", Status: "off"}))
		})

		It("honours an If condition", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "on"})).To(Succeed())
			err := rows.Put(ctx, row{PK: "a", SK: "1", Status: "x"}, dynamodbutil.WriteOpts{If: expression.Name("status").Equal(expression.Value("off"))})
			Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
		})
	})

	Describe("Update", func() {
		BeforeEach(func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "on", N: 1})).To(Succeed())
		})

		It("applies the update and returns the whole item afterwards", func() {
			got, err := rows.Update(ctx, rows.Key("a", "1"), expression.Set(expression.Name("status"), expression.Value("off")))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(row{PK: "a", SK: "1", Status: "off", N: 1}))
		})

		It("fails with a condition error when the item does not exist", func() {
			_, err := rows.Update(ctx, rows.Key("a", "missing"), expression.Set(expression.Name("status"), expression.Value("off")))
			Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
			_, found, _ := rows.Get(ctx, rows.Key("a", "missing"))
			Expect(found).To(BeFalse())
		})

		It("creates a missing item with Upsert", func() {
			got, err := rows.Upsert(ctx, rows.Key("a", "2"), expression.Set(expression.Name("status"), expression.Value("new")))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(row{PK: "a", SK: "2", Status: "new"}))
		})

		It("ANDs an If condition with the existence check", func() {
			_, err := rows.Update(ctx, rows.Key("a", "1"), expression.Set(expression.Name("n"), expression.Value(2)),
				dynamodbutil.WriteOpts{If: expression.Name("n").Equal(expression.Value(7))})
			Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
		})

		It("attaches the current item on condition failure when asked", func() {
			_, err := rows.Update(ctx, rows.Key("a", "1"), expression.Set(expression.Name("n"), expression.Value(2)),
				dynamodbutil.WriteOpts{If: expression.Name("n").Equal(expression.Value(7)), ReturnOld: true})
			old, ok := dynamodbutil.OldItem[row](err)
			Expect(ok).To(BeTrue())
			Expect(old.N).To(Equal(1))
		})
	})

	Describe("UpdateFrom", func() {
		It("writes the zero value for a field without omitempty", func() {
			counts := dynamodbutil.NewTable[countedRow](m, dynamodbutil.Schema{Name: rowsTable, HashKey: "pk", RangeKey: "sk"})
			Expect(counts.Put(ctx, countedRow{PK: "a", SK: "1", Count: 5})).To(Succeed())
			_, err := counts.UpdateFrom(ctx, countedRow{PK: "a", SK: "1"})
			Expect(err).NotTo(HaveOccurred())
			got, found, err := counts.Get(ctx, counts.Key("a", "1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(got.Count).To(BeZero(), "an UpdateFrom with Count unset overwrites the stored 5")
		})

		It("sets every non-key attribute the item marshals and leaves omitted ones alone", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1", Status: "on", N: 5})).To(Succeed())
			got, err := rows.UpdateFrom(ctx, row{PK: "a", SK: "1", Owner: "o"})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(row{PK: "a", SK: "1", Owner: "o", Status: "on", N: 5}))
		})

		It("fails with a condition error when the item does not exist", func() {
			_, err := rows.UpdateFrom(ctx, row{PK: "a", SK: "missing", Owner: "o"})
			Expect(dynamodbutil.IsConditionFailed(err)).To(BeTrue())
		})

		It("refuses an item with nothing besides its key", func() {
			Expect(rows.Put(ctx, row{PK: "a", SK: "1"})).To(Succeed())
			_, err := rows.UpdateFrom(ctx, row{PK: "a", SK: "1"})
			Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring("no attributes besides its key"))
		})
	})

	Describe("Delete", func() {
		It("removes an existing item", func() {
			Expect(singles.Put(ctx, single{ID: "x"})).To(Succeed())
			Expect(singles.Delete(ctx, singles.Key("x"))).To(Succeed())
			_, found, _ := singles.Get(ctx, singles.Key("x"))
			Expect(found).To(BeFalse())
		})

		It("fails with a condition error when the item does not exist", func() {
			Expect(dynamodbutil.IsConditionFailed(singles.Delete(ctx, singles.Key("x")))).To(BeTrue())
		})

		It("succeeds on a missing item with DeleteAllowMissing", func() {
			Expect(singles.DeleteAllowMissing(ctx, singles.Key("x"))).To(Succeed())
		})
	})

	It("keeps the SDK error reachable through the wrap", func() {
		err := rows.Create(ctx, row{PK: "a", SK: "1"})
		Expect(err).NotTo(HaveOccurred())
		err = rows.Create(ctx, row{PK: "a", SK: "1"})
		Expect(err.Error()).To(ContainSubstring("dynamodbutil: create rows"))
	})
})

// countedRow has a field without omitempty, so its zero value is written like any other.
type countedRow struct {
	PK    string `dynamodbav:"pk"`
	SK    string `dynamodbav:"sk"`
	Count int    `dynamodbav:"count"`
}
