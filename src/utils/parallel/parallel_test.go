// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package parallel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestParallel(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Parallel Suite")
}

var _ = Describe("ProcessParallel", func() {
	Context("Basic Processing", func() {
		It("should process items in parallel", func() {
			items := []int{1, 2, 3, 4, 5}
			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				return x * 2
			})

			Expect(err).To(BeNil())
			Expect(results).To(Equal([]int{2, 4, 6, 8, 10}))
			Expect(lastProcessed).To(Equal(5)) // Last item processed
		})

		It("should respect MaxRoutines limit", func() {
			const itemCount = 100
			var activeRoutines int32
			var maxObservedRoutines int32

			items := make([]int, itemCount)
			for i := range items {
				items[i] = i
			}

			_, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				// Track active routines
				current := atomic.AddInt32(&activeRoutines, 1)
				if current > atomic.LoadInt32(&maxObservedRoutines) {
					atomic.StoreInt32(&maxObservedRoutines, current)
				}
				time.Sleep(10 * time.Millisecond) // Simulate work
				atomic.AddInt32(&activeRoutines, -1)
				return x
			}, ParallelOptions{
				MaxRoutines:    5,
				CollectResults: false,
			})

			Expect(err).To(BeNil())
			Expect(maxObservedRoutines).To(BeNumerically("<=", 5))
			Expect(lastProcessed).To(Equal(itemCount - 1))
		})
	})

	Context("Result Collection", func() {
		It("should not collect results when CollectResults is false", func() {
			items := []int{1, 2, 3}
			var processedCount int32

			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				atomic.AddInt32(&processedCount, 1)
				time.Sleep(10 * time.Millisecond) // Add small delay to ensure concurrent processing
				return x
			}, ParallelOptions{
				CollectResults: false,
				MaxRoutines:    2,
			})

			Expect(err).To(BeNil())
			Expect(results).To(BeNil())
			Eventually(func() int32 { return atomic.LoadInt32(&processedCount) }).Should(Equal(int32(3)))
			Expect(lastProcessed).To(Equal(3)) // Last item processed
		})

		It("should maintain result order", func() {
			items := []int{1, 2, 3, 4, 5}
			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				time.Sleep(time.Duration(5-x) * time.Millisecond) // Reverse sleep times
				return x
			})

			Expect(err).To(BeNil())
			Expect(results).To(Equal([]int{1, 2, 3, 4, 5}))
			Expect(lastProcessed).To(Equal(5))
		})
	})

	Context("Timeout Handling", func() {
		It("should return partial results on timeout", func() {
			items := make([]int, 10)
			for i := range items {
				items[i] = i + 1 // Values 1-10
			}
			var processed int32

			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				atomic.AddInt32(&processed, 1)
				// Sleep long enough that only first few items will be started
				// before timeout occurs
				time.Sleep(200 * time.Millisecond)
				return x
			}, ParallelOptions{
				Timeout:        100 * time.Millisecond,
				CollectResults: true,
				MaxRoutines:    2, // Limit concurrent processing
			})

			Expect(err).To(Equal(context.DeadlineExceeded))
			processedCount := atomic.LoadInt32(&processed)
			// With MaxRoutines=2, we expect 2-3 items to start before timeout
			// (3rd item might start just before timeout occurs)
			Expect(processedCount).To(BeNumerically(">=", 2))
			Expect(processedCount).To(BeNumerically("<=", 3))
			Expect(lastProcessed).To(BeNumerically(">", 0))
			Expect(results).To(HaveLen(10))
		})

		It("should not collect results on timeout when CollectResults is false", func() {
			items := make([]int, 10)
			for i := range items {
				items[i] = i + 1
			}
			var processed int32

			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				atomic.AddInt32(&processed, 1)
				time.Sleep(100 * time.Millisecond)
				return x
			}, ParallelOptions{
				Timeout:        250 * time.Millisecond,
				MaxRoutines:    3,
				CollectResults: false,
			})

			Expect(err).To(Equal(context.DeadlineExceeded))
			Expect(results).To(BeNil())
			Expect(atomic.LoadInt32(&processed)).To(BeNumerically(">", 0))
			Expect(lastProcessed).To(BeNumerically(">", 0))
		})

		It("should process all items when timeout is sufficient", func() {
			items := make([]int, 5)
			for i := range items {
				items[i] = i + 1
			}
			var processed int32

			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				atomic.AddInt32(&processed, 1)
				time.Sleep(50 * time.Millisecond)
				return x + 1
			}, ParallelOptions{
				Timeout:        1 * time.Second,
				CollectResults: true,
			})

			Expect(err).To(BeNil())
			Expect(results).To(HaveLen(5))
			Expect(atomic.LoadInt32(&processed)).To(Equal(int32(5)))
			Expect(lastProcessed).To(Equal(5))
		})
	})

	Context("Error Handling", func() {
		It("should handle panics in worker routines", func() {
			items := []int{1, 2, 3, 4, 5}
			var processed int32

			results, lastProcessed, err := ProcessParallel(context.Background(), items, func(x int) int {
				atomic.AddInt32(&processed, 1)
				if x == 2 || x == 4 {
					panic(fmt.Sprintf("panic on item %d", x))
				}
				return x
			})

			// Should complete processing other items despite panics
			Expect(err).To(BeNil())
			Expect(atomic.LoadInt32(&processed)).To(Equal(int32(5)))
			Expect(results).To(HaveLen(5))
			Expect(lastProcessed).To(Equal(5)) // Should be the last successful item
			// Items that caused panic should have zero values
			Expect(results[1]).To(Equal(0)) // index 1 is item 2
			Expect(results[3]).To(Equal(0)) // index 3 is item 4
			// Other items should be processed normally
			Expect(results[0]).To(Equal(1))
			Expect(results[2]).To(Equal(3))
			Expect(results[4]).To(Equal(5))
		})
	})
})

var _ = Describe("ProcessParallelErr", func() {
	double := func(_ context.Context, x int) (int, error) { return x * 2, nil }

	It("returns results in input order", func() {
		results, err := ProcessParallelErr(context.Background(), []int{1, 2, 3, 4, 5}, double, ParallelOptions{MaxRoutines: 3})
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(Equal([]int{2, 4, 6, 8, 10}))
	})

	It("returns nil for empty input", func() {
		results, err := ProcessParallelErr(context.Background(), []int{}, double)
		Expect(err).NotTo(HaveOccurred())
		Expect(results).To(BeNil())
	})

	It("returns the error and no results", func() {
		boom := errors.New("boom")
		results, err := ProcessParallelErr(context.Background(), []int{1, 2, 3}, func(_ context.Context, x int) (int, error) {
			if x == 2 {
				return 0, boom
			}
			return x, nil
		})
		Expect(err).To(MatchError(boom))
		Expect(results).To(BeNil())
	})

	It("starts nothing after a failure when sequential", func() {
		var mu sync.Mutex
		var started []int
		_, err := ProcessParallelErr(context.Background(), []int{1, 2, 3, 4, 5}, func(_ context.Context, x int) (int, error) {
			mu.Lock()
			started = append(started, x)
			mu.Unlock()
			if x == 2 {
				return 0, errors.New("boom")
			}
			return x, nil
		}, ParallelOptions{MaxRoutines: 1})
		Expect(err).To(HaveOccurred())
		Expect(started).To(Equal([]int{1, 2}))
	})

	It("cancels in-flight items on failure and returns the first error", func() {
		started, cancelled := make(chan struct{}), make(chan struct{})
		_, err := ProcessParallelErr(context.Background(), []int{1, 2}, func(ctx context.Context, x int) (int, error) {
			if x == 1 {
				<-started
				return 0, errors.New("boom")
			}
			close(started)
			select {
			case <-ctx.Done():
				close(cancelled)
				return 0, ctx.Err()
			case <-time.After(5 * time.Second):
				return 0, nil
			}
		}, ParallelOptions{MaxRoutines: 2})
		Expect(err).To(MatchError("boom"))
		Expect(cancelled).To(BeClosed(), "the in-flight item never saw its context cancelled")
	})

	It("turns a panic into an error", func() {
		_, err := ProcessParallelErr(context.Background(), []int{1, 2, 3}, func(_ context.Context, x int) (int, error) {
			if x == 2 {
				panic("kaboom")
			}
			return x, nil
		})
		Expect(err).To(MatchError(ContainSubstring("kaboom")))
	})

	It("caps concurrency at MaxRoutines", func() {
		var running, peak atomic.Int32
		_, err := ProcessParallelErr(context.Background(), make([]int, 20), func(_ context.Context, x int) (int, error) {
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
			return x, nil
		}, ParallelOptions{MaxRoutines: 3})
		Expect(err).NotTo(HaveOccurred())
		Expect(peak.Load()).To(BeNumerically("<=", 3))
	})

	It("returns the caller's cancellation without running anything", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var calls atomic.Int32
		_, err := ProcessParallelErr(ctx, []int{1, 2, 3}, func(_ context.Context, x int) (int, error) {
			calls.Add(1)
			return x, nil
		})
		Expect(err).To(MatchError(context.Canceled))
		Expect(calls.Load()).To(BeZero())
	})
})
