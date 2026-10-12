// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/awscommon"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Batches", func() {
	var (
		ctx     context.Context
		m       *mock.DynamoDBMock
		singles *dynamodbutil.Table[single]
	)

	makeSingles := func(n int) []single {
		items := make([]single, n)
		for i := range items {
			items[i] = single{ID: fmt.Sprintf("id-%03d", i), Data: "d"}
		}
		return items
	}
	keysOf := func(items []single) []dynamodbutil.Key {
		keys := make([]dynamodbutil.Key, len(items))
		for i, it := range items {
			keys[i] = singles.Key(it.ID)
		}
		return keys
	}

	BeforeEach(func() {
		ctx = context.Background()
		m = newMock()
		singles = singlesOf(m)
		DeferCleanup(dynamodbutil.SetBackoffDelay(time.Millisecond))
	})

	It("BatchPut writes more items than one call allows", func() {
		items := makeSingles(60)
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		got, err := singles.BatchGet(ctx, keysOf(items))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ConsistOf(items))
	})

	It("BatchPut with Sequential writes every chunk after the one before it", func() {
		items := makeSingles(80)
		Expect(singles.BatchPut(ctx, items, dynamodbutil.BatchWriteOpts{Sequential: true})).To(Succeed())
		operations := m.ProfileGet().Accesses[singleTable].Operations
		Expect(operations).To(HaveLen(len(items)))
		for i, op := range operations {
			Expect(op.Details).To(Equal(items[i].ID+":"), "write %d out of input order", i)
		}
	})

	It("BatchPut retries items the service returns as unprocessed", func() {
		items := makeSingles(10)
		// The mock holds back one item fewer on each call, so this finishes on the last allowed attempt.
		m.NextBatchWriteUnprocessedCount = awscommon.SDK_MAX_ATTEMPTS - 1
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		got, err := singles.BatchGet(ctx, keysOf(items))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveLen(10))
	})

	It("BatchPut reports an item missing its key, without panicking or writing anything", func() {
		keyless := dynamodbutil.NewTable[omittedKey](m, dynamodbutil.Schema{Name: singleTable, HashKey: "id"})
		err := keyless.BatchPut(ctx, []omittedKey{{ID: "kept"}, {}})
		Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring(`no key attribute "id"`))
		Expect(singles.Scan(ctx)).To(HaveField("Items", BeEmpty()))
	})

	It("BatchGet gives up with UnprocessedError after the attempt cap", func() {
		Expect(singles.BatchPut(ctx, makeSingles(2))).To(Succeed())
		m.NextBatchGetUnprocessedCount = 100
		_, err := singles.BatchGet(ctx, keysOf(makeSingles(2)))
		var unprocessed *dynamodbutil.UnprocessedError
		Expect(errors.As(err, &unprocessed)).To(BeTrue(), "got %v", err)
		Expect(unprocessed.Table).To(Equal(singleTable))
		Expect(unprocessed.Remaining).To(Equal(2))
	})

	It("BatchPut gives up with UnprocessedError after the attempt cap", func() {
		m.NextBatchWriteUnprocessedCount = 100
		err := singles.BatchPut(ctx, makeSingles(2))
		var unprocessed *dynamodbutil.UnprocessedError
		Expect(errors.As(err, &unprocessed)).To(BeTrue())
		Expect(unprocessed.Table).To(Equal(singleTable))
		Expect(unprocessed.Remaining).To(Equal(2))
		Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("after %d attempts", awscommon.SDK_MAX_ATTEMPTS)))
	})

	It("stops retrying when the context is cancelled", func() {
		DeferCleanup(dynamodbutil.SetBackoffDelay(time.Hour))
		m.NextBatchWriteUnprocessedCount = 100
		cctx, cancel := context.WithCancel(ctx)
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		err := singles.BatchPut(cctx, makeSingles(2))
		Expect(err).To(MatchError(context.Canceled))
	})

	It("BatchGet reads more keys than one call allows and omits missing keys", func() {
		items := makeSingles(150)
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		keys := append(keysOf(items), singles.Key("absent"))
		got, err := singles.BatchGet(ctx, keys)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ConsistOf(items))
	})

	It("BatchGet retries unprocessed keys", func() {
		items := makeSingles(5)
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		m.NextBatchGetUnprocessedCount = 2
		got, err := singles.BatchGet(ctx, keysOf(items))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ConsistOf(items))
	})

	It("BatchGet returns nil for no keys", func() {
		got, err := singles.BatchGet(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("BatchGet and BatchDelete accept a key listed twice", func() {
		items := makeSingles(3)
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		keys := append(keysOf(items), singles.Key(items[0].ID))
		got, err := singles.BatchGet(ctx, keys)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ConsistOf(items))
		Expect(singles.BatchDelete(ctx, keys)).To(Succeed())
		got, err = singles.BatchGet(ctx, keysOf(items))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("BatchPut keeps the last of two items with the same key, as sequential puts would", func() {
		Expect(singles.BatchPut(ctx, []single{{ID: "dup", Data: "first"}, {ID: "other"}, {ID: "dup", Data: "last"}}, dynamodbutil.BatchWriteOpts{Sequential: true})).To(Succeed())
		got, found, err := singles.Get(ctx, singles.Key("dup"))
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(got.Data).To(Equal("last"))
		_, found, _ = singles.Get(ctx, singles.Key("other"))
		Expect(found).To(BeTrue())
	})

	It("BatchPut and BatchDelete do nothing for empty input", func() {
		Expect(singles.BatchPut(ctx, nil)).To(Succeed())
		Expect(singles.BatchDelete(ctx, nil)).To(Succeed())
	})

	It("BatchDelete removes every key, retrying unprocessed ones", func() {
		items := makeSingles(30)
		Expect(singles.BatchPut(ctx, items)).To(Succeed())
		m.NextBatchWriteUnprocessedCount = 2
		Expect(singles.BatchDelete(ctx, keysOf(items))).To(Succeed())
		got, err := singles.BatchGet(ctx, keysOf(items))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})
