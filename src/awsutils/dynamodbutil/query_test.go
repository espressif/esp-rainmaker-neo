// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package dynamodbutil_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/espressif/esp-rainmaker-neo/src/awsutils/dynamodbutil"
	"github.com/espressif/esp-rainmaker-neo/src/test/mock"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Query", func() {
	var (
		ctx  context.Context
		m    *mock.DynamoDBMock
		rows *dynamodbutil.Table[row]
	)

	// seed writes 20 rows in partition "p" (sk 00..19); every fourth row is "failed", the rest "ok". Owner alternates o0/o1.
	seed := func() {
		for i := range 20 {
			status := "ok"
			if i%4 == 0 {
				status = "failed"
			}
			Expect(rows.Put(ctx, row{PK: "p", SK: fmt.Sprintf("%02d", i), Owner: fmt.Sprintf("o%d", i%2), Status: status})).To(Succeed())
		}
	}
	inP := expression.Key("pk").Equal(expression.Value("p"))
	failed := expression.Name("status").Equal(expression.Value("failed"))

	sks := func(items []row) []string {
		out := make([]string, 0, len(items))
		for _, r := range items {
			out = append(out, r.SK)
		}
		return out
	}

	// walk follows NextToken to the end and returns every SK seen, page by page.
	walk := func(keyCond expression.KeyConditionBuilder, o dynamodbutil.QueryOpts) [][]string {
		var pages [][]string
		for {
			page, err := rows.Query(ctx, keyCond, o)
			Expect(err).NotTo(HaveOccurred())
			pages = append(pages, sks(page.Items))
			if page.NextToken == "" {
				return pages
			}
			o.StartToken = page.NextToken
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		m = newMock()
		rows = rowsOf(m)
		seed()
	})

	It("returns every match when Limit is 0, across service pages", func() {
		m.MaxPageItems = 3
		page, err := rows.Query(ctx, inP)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Items).To(HaveLen(20))
		Expect(page.NextToken).To(BeEmpty())
	})

	It("returns nil Items when nothing matches", func() {
		page, err := rows.Query(ctx, expression.Key("pk").Equal(expression.Value("none")))
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Items).To(BeNil())
	})

	It("pages an unfiltered query by Limit and resumes exactly, filling a page across service calls", func() {
		m.MaxPageItems = 3
		Expect(walk(inP, dynamodbutil.QueryOpts{Limit: 8})).To(Equal([][]string{
			{"00", "01", "02", "03", "04", "05", "06", "07"},
			{"08", "09", "10", "11", "12", "13", "14", "15"},
			{"16", "17", "18", "19"},
		}))
	})

	// The service applies Limit before the filter; Query must count matches, not evaluated rows.
	It("returns Limit items that pass the filter, even when matches are sparse", func() {
		m.MaxPageItems = 3
		Expect(walk(inP, dynamodbutil.QueryOpts{Filter: failed, Limit: 2})).To(Equal([][]string{
			{"00", "04"},
			{"08", "12"},
			{"16"},
		}))
	})

	It("resumes after the last returned item when a service page overshoots Limit", func() {
		Expect(walk(inP, dynamodbutil.QueryOpts{Filter: expression.Name("status").Equal(expression.Value("ok")), Limit: 4})).To(Equal([][]string{
			{"01", "02", "03", "05"},
			{"06", "07", "09", "10"},
			{"11", "13", "14", "15"},
			{"17", "18", "19"},
		}))
	})

	It("pages a filtered query on an index, resuming from index keys", func() {
		m.MaxPageItems = 3
		Expect(walk(expression.Key("owner").Equal(expression.Value("o0")), dynamodbutil.QueryOpts{Index: byOwner, Filter: failed, Limit: 2})).To(Equal([][]string{{"00", "04"}, {"08", "12"}, {"16"}}))
	})

	It("still yields a usable token when Project leaves out the keys", func() {
		page, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Filter: failed, Limit: 2, Project: []string{"status"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(sks(page.Items)).To(Equal([]string{"00", "04"}))
		Expect(page.NextToken).NotTo(BeEmpty())
	})

	It("treats a Limit beyond int32 as every match", func() {
		page, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Limit: math.MaxInt32 + 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Items).To(HaveLen(20))
		Expect(page.NextToken).To(BeEmpty())
	})

	It("resumes at the next item when the token's item was deleted in between", func() {
		page, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Limit: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(sks(page.Items)).To(Equal([]string{"00", "01"}))
		Expect(rows.Delete(ctx, rows.Key("p", "01"))).To(Succeed())
		next, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Limit: 2, StartToken: page.NextToken})
		Expect(err).NotTo(HaveOccurred())
		Expect(sks(next.Items)).To(Equal([]string{"02", "03"}))
	})

	It("reads in descending order", func() {
		page, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Descending: true, Limit: 3})
		Expect(err).NotTo(HaveOccurred())
		Expect(sks(page.Items)).To(Equal([]string{"19", "18", "17"}))
	})

	It("rejects an index missing from the schema", func() {
		_, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Index: "undeclared"})
		Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring("not declared"))
	})

	It("queries a hash-only table", func() {
		singles := singlesOf(m)
		Expect(singles.Put(ctx, single{ID: "a", Data: "d"})).To(Succeed())
		Expect(singles.Put(ctx, single{ID: "b"})).To(Succeed())
		page, err := singles.Query(ctx, expression.Key("id").Equal(expression.Value("a")))
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Items).To(Equal([]single{{ID: "a", Data: "d"}}))
	})

	It("pages an index without a range key, even when Project leaves out the keys", func() {
		Expect(m.AddSecondaryIndex(byStatus, rowsTable, "status", "")).To(Succeed())
		byStatusRows := dynamodbutil.NewTable[row](m, dynamodbutil.Schema{
			Name: rowsTable, HashKey: "pk", RangeKey: "sk",
			Indexes: map[string]dynamodbutil.IndexKeys{byStatus: {HashKey: "status"}},
		})
		o := dynamodbutil.QueryOpts{Index: byStatus, Limit: 2, Project: []string{"owner"}}
		var seen []string
		for range 5 {
			page, err := byStatusRows.Query(ctx, expression.Key("status").Equal(expression.Value("failed")), o)
			Expect(err).NotTo(HaveOccurred())
			seen = append(seen, sks(page.Items)...)
			if page.NextToken == "" {
				break
			}
			o.StartToken = page.NextToken
		}
		Expect(seen).To(ConsistOf("00", "04", "08", "12", "16"))
	})

	It("rejects a token from another index as ErrInvalidToken", func() {
		page, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{Limit: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(page.NextToken).NotTo(BeEmpty())
		_, err = rows.Query(ctx, expression.Key("owner").Equal(expression.Value("o0")), dynamodbutil.QueryOpts{Index: byOwner, StartToken: page.NextToken})
		Expect(errors.Is(err, dynamodbutil.ErrInvalidToken)).To(BeTrue(), "got %v", err)
	})

	It("rejects a malformed start token", func() {
		_, err := rows.Query(ctx, inP, dynamodbutil.QueryOpts{StartToken: "%%%"})
		Expect(errors.Is(err, dynamodbutil.ErrInvalidToken)).To(BeTrue())
	})

	Describe("QueryEach", func() {
		It("visits every match across service pages", func() {
			m.MaxPageItems = 3
			var seen []string
			Expect(rows.QueryEach(ctx, inP, func(r row) error {
				seen = append(seen, r.SK)
				return nil
			}, dynamodbutil.QueryEachOpts{Filter: failed})).To(Succeed())
			Expect(seen).To(Equal([]string{"00", "04", "08", "12", "16"}))
		})

		It("stops at the first callback error and returns it unwrapped", func() {
			stop := errors.New("stop")
			calls := 0
			err := rows.QueryEach(ctx, inP, func(row) error {
				calls++
				return stop
			})
			Expect(err).To(BeIdenticalTo(stop))
			Expect(calls).To(Equal(1))
		})
	})

	It("counts matches across service pages", func() {
		m.MaxPageItems = 3
		n, err := rows.QueryCount(ctx, inP, dynamodbutil.QueryCountOpts{Filter: failed})
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))
	})

	It("QueryDelete deletes every match and nothing else", func() {
		m.MaxPageItems = 3
		Expect(rows.QueryDelete(ctx, inP, dynamodbutil.QueryDeleteOpts{Filter: failed})).To(Succeed())
		left, err := rows.Query(ctx, inP)
		Expect(err).NotTo(HaveOccurred())
		Expect(left.Items).To(HaveLen(15))
		for _, r := range left.Items {
			Expect(r.Status).To(Equal("ok"))
		}
	})

	It("QueryDelete deletes each page before reading the next, so it never holds every key", func() {
		m.MaxPageItems = 5
		spy := &callSpy{DynamoDBMock: m}
		table := dynamodbutil.NewTable[row](spy, dynamodbutil.Schema{Name: rowsTable, HashKey: "pk", RangeKey: "sk"})
		Expect(table.QueryDelete(ctx, inP)).To(Succeed())
		Expect(rows.QueryCount(ctx, inP)).To(BeZero())
		firstDelete, lastQuery := slices.Index(spy.calls, "BatchWriteItem"), -1
		for i, call := range spy.calls {
			if call == "Query" {
				lastQuery = i
			}
		}
		Expect(firstDelete).To(BeNumerically("<", lastQuery), "calls were %v", spy.calls)
	})

	It("QueryDelete passes Consistent to the query that finds the rows", func() {
		Expect(rows.QueryDelete(ctx, inP, dynamodbutil.QueryDeleteOpts{Filter: failed, Consistent: true})).To(Succeed())
		Expect(rows.QueryCount(ctx, inP)).To(Equal(15))
		// DynamoDB, and so the mock, refuses a consistent read on a GSI: the option reached the query.
		err := rows.QueryDelete(ctx, expression.Key("owner").Equal(expression.Value("o0")), dynamodbutil.QueryDeleteOpts{Index: byOwner, Consistent: true})
		Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring("Consistent reads are not supported"))
	})

	It("panics when given more than one options struct", func() {
		Expect(func() {
			_, _ = rows.Query(ctx, inP, dynamodbutil.QueryOpts{Limit: 1}, dynamodbutil.QueryOpts{Limit: 2})
		}).To(PanicWith("dynamodbutil: Query takes at most one dynamodbutil.QueryOpts"))
	})

	Describe("Scan", func() {
		// walkScan follows NextToken to the end and returns every SK seen, page by page. It gives up after more pages than the 20 rows could fill, so a token that never advances fails instead of hanging.
		walkScan := func(o dynamodbutil.ScanOpts) [][]string {
			var pages [][]string
			for range 21 {
				page, err := rows.Scan(ctx, o)
				Expect(err).NotTo(HaveOccurred())
				pages = append(pages, sks(page.Items))
				if page.NextToken == "" {
					return pages
				}
				o.StartToken = page.NextToken
			}
			Fail("Scan kept returning a NextToken; paging never reached the end")
			return nil
		}

		It("returns every item passing the filter when Limit is 0, across service pages", func() {
			m.MaxPageItems = 3
			page, err := rows.Scan(ctx, dynamodbutil.ScanOpts{Filter: failed})
			Expect(err).NotTo(HaveOccurred())
			Expect(sks(page.Items)).To(ConsistOf("00", "04", "08", "12", "16"))
			Expect(page.NextToken).To(BeEmpty())
			for _, r := range page.Items {
				Expect(r.Status).To(Equal("failed"), "an unprojected scan returns whole items")
				Expect(r.Owner).NotTo(BeEmpty(), "an unprojected scan returns whole items")
			}
		})

		It("returns exactly Limit items per page without a filter, filling a page across service calls", func() {
			m.MaxPageItems = 3
			pages := walkScan(dynamodbutil.ScanOpts{Limit: 7})
			Expect(pages).To(HaveLen(3))
			Expect(pages[0]).To(HaveLen(7))
			Expect(pages[1]).To(HaveLen(7))
			Expect(pages[2]).To(HaveLen(6))
			Expect(slices.Concat(pages...)).To(HaveLen(20))
			Expect(slices.Concat(pages...)).To(ConsistOf(sks(allRows(rows))))
		})

		It("pages through filtered matches with Limit and StartToken, skipping none and repeating none", func() {
			m.MaxPageItems = 3
			pages := walkScan(dynamodbutil.ScanOpts{Filter: failed, Limit: 2})
			Expect(pages).To(HaveLen(3))
			Expect(slices.Concat(pages...)).To(ConsistOf("00", "04", "08", "12", "16"))
		})

		It("returns only the projected attributes and still pages when Project leaves out the keys", func() {
			pages := walkScan(dynamodbutil.ScanOpts{Filter: failed, Limit: 2, Project: []string{"status"}})
			Expect(slices.Concat(pages...)).To(ConsistOf("00", "04", "08", "12", "16"))
			page, err := rows.Scan(ctx, dynamodbutil.ScanOpts{Filter: failed, Project: []string{"status"}})
			Expect(err).NotTo(HaveOccurred())
			for _, r := range page.Items {
				Expect(r.Status).To(Equal("failed"))
				Expect(r.Owner).To(BeEmpty(), "owner was not projected")
			}
		})

		It("asks DynamoDB for a strongly consistent read with Consistent", func() {
			spy := &scanSpy{DynamoDBMock: m}
			table := dynamodbutil.NewTable[row](spy, dynamodbutil.Schema{Name: rowsTable, HashKey: "pk", RangeKey: "sk"})
			_, err := table.Scan(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(spy.last.ConsistentRead).To(BeNil())
			_, err = table.Scan(ctx, dynamodbutil.ScanOpts{Consistent: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(spy.last.ConsistentRead).To(HaveValue(BeTrue()))
		})

		It("rejects a negative limit", func() {
			_, err := rows.Scan(ctx, dynamodbutil.ScanOpts{Limit: -1})
			Expect(rmerror.FormatErrorChain(err)).To(ContainSubstring("negative limit"))
		})

		It("rejects a malformed start token", func() {
			_, err := rows.Scan(ctx, dynamodbutil.ScanOpts{StartToken: "%%%"})
			Expect(errors.Is(err, dynamodbutil.ErrInvalidToken)).To(BeTrue())
		})

		It("ScanEach visits every item passing the filter, across service pages", func() {
			m.MaxPageItems = 3
			var seen []string
			Expect(rows.ScanEach(ctx, func(r row) error {
				seen = append(seen, r.SK)
				return nil
			}, dynamodbutil.ScanEachOpts{Filter: failed})).To(Succeed())
			Expect(seen).To(ConsistOf("00", "04", "08", "12", "16"))
		})
	})
})

// callSpy records the order of Query and BatchWriteItem calls.
type callSpy struct {
	*mock.DynamoDBMock
	mu    sync.Mutex
	calls []string
}

func (s *callSpy) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name)
}

func (s *callSpy) Query(ctx context.Context, in *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	s.record("Query")
	return s.DynamoDBMock.Query(ctx, in, optFns...)
}

func (s *callSpy) BatchWriteItem(ctx context.Context, in *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	s.record("BatchWriteItem")
	return s.DynamoDBMock.BatchWriteItem(ctx, in, optFns...)
}

// scanSpy records the last ScanInput so a test can see what the util asked DynamoDB for.
type scanSpy struct {
	*mock.DynamoDBMock
	last *dynamodb.ScanInput
}

func (s *scanSpy) Scan(ctx context.Context, in *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	s.last = in
	return s.DynamoDBMock.Scan(ctx, in, optFns...)
}

func allRows(t *dynamodbutil.Table[row]) []row {
	page, err := t.Query(context.Background(), expression.Key("pk").Equal(expression.Value("p")))
	Expect(err).NotTo(HaveOccurred())
	return page.Items
}
