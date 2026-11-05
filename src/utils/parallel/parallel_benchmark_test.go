// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package parallel

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Benchmark comparing original vs improved parallel processing
// Example output:
// === RUN   BenchmarkParallelProcessing
// BenchmarkParallelProcessing
// === RUN   BenchmarkParallelProcessing/Original_100_items
// BenchmarkParallelProcessing/Original_100_items
// BenchmarkParallelProcessing/Original_100_items-8                   19932             64249 ns/op           20280 B/op        305 allocs/op
// === RUN   BenchmarkParallelProcessing/Improved_100_items
// BenchmarkParallelProcessing/Improved_100_items
// BenchmarkParallelProcessing/Improved_100_items-8                   23840             53905 ns/op            3320 B/op         30 allocs/op
// === RUN   BenchmarkParallelProcessing/Original_1000_items
// BenchmarkParallelProcessing/Original_1000_items
// BenchmarkParallelProcessing/Original_1000_items-8                   2054            605892 ns/op          200384 B/op       3005 allocs/op
// === RUN   BenchmarkParallelProcessing/Improved_1000_items
// BenchmarkParallelProcessing/Improved_1000_items
// BenchmarkParallelProcessing/Improved_1000_items-8                   2596            506721 ns/op           10646 B/op         30 allocs/op
// === RUN   BenchmarkParallelProcessing/Original_10000_items
// BenchmarkParallelProcessing/Original_10000_items
// BenchmarkParallelProcessing/Original_10000_items-8                   217           6305390 ns/op         2002163 B/op      30006 allocs/op
// === RUN   BenchmarkParallelProcessing/Improved_10000_items
// BenchmarkParallelProcessing/Improved_10000_items
// BenchmarkParallelProcessing/Improved_10000_items-8                   271           4885124 ns/op           84662 B/op         33 allocs/op
func BenchmarkParallelProcessing(b *testing.B) {
	// Test with different item counts
	itemCounts := []int{100, 1000, 10000}

	for _, itemCount := range itemCounts {
		b.Run(fmt.Sprintf("Original_%d_items", itemCount), func(b *testing.B) {
			benchmarkOriginalParallel(b, itemCount)
		})

		b.Run(fmt.Sprintf("Improved_%d_items", itemCount), func(b *testing.B) {
			benchmarkImprovedParallel(b, itemCount)
		})
	}
}

func benchmarkOriginalParallel(b *testing.B, itemCount int) {
	items := make([]int, itemCount)
	for i := 0; i < itemCount; i++ {
		items[i] = i
	}

	processFunc := func(x int) int {
		// Simulate some work
		time.Sleep(time.Microsecond)
		return x * 2
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = processParallelTest(context.Background(), items, processFunc, ParallelOptions{
			MaxRoutines:    10,
			CollectResults: true,
		})
	}
}

func benchmarkImprovedParallel(b *testing.B, itemCount int) {
	items := make([]int, itemCount)
	for i := 0; i < itemCount; i++ {
		items[i] = i
	}

	processFunc := func(x int) int {
		// Simulate some work
		time.Sleep(time.Microsecond)
		return x * 2
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = ProcessParallel(context.Background(), items, processFunc, ParallelOptions{
			MaxRoutines:    10,
			CollectResults: true,
		})
	}
}
