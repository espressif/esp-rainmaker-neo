// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package parallel

// The parallel processing utilities in this file enable efficient concurrent processing

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rmerror"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// ParallelOptions configures the behavior of parallel processing operations.
// It allows customization of concurrency limits, timeout duration, and result collection.
type ParallelOptions struct {
	MaxRoutines    uint          // Maximum number of concurrent goroutines (default: 10)
	Timeout        time.Duration // Optional timeout for processing, this is checked at the start of each item processing and hence should have a buffer to account for the total time taken to process one item
	CollectResults bool          // Whether to collect and return results (default: true)
}

// ProcessParallel processes a slice of items concurrently and returns the results.
// It supports generic types for both input items (T) and results (R).
//
// Parameters:
//   - ctx: Caller's context. The fan-out inherits its deadline/cancellation (e.g. the API-GW/Lambda time budget), so dispatch stops once the caller's budget expires. processFunc must honor this same deadline through the context it closes over, so an in-flight AWS call unwinds instead of pinning a worker.
//   - items: Slice of input items to process
//   - processFunc: Function that processes each item and returns a result. Make sure to handle panics in the function. Also, keep this function simple and fast.
//   - opts: Optional ParallelOptions to configure processing behavior
//
// Returns:
//   - results: Slice of processed results (nil if CollectResults is false)
//   - lastProcessed: The last successfully processed item
//   - err: Error from context timeout/cancellation if it occurred
//
// The function handles panics in individual goroutines gracefully, ensuring that a panic in one routine doesn't affect others.
// It also tracks the last successfully processed item even when not collecting results.
//
// If you change this function, run BenchmarkParallelProcessing to check that performance has not degraded.
func ProcessParallel[T, R any](ctx context.Context, items []T, processFunc func(T) R, opts ...ParallelOptions) (results []R, lastProcessed T, err error) {
	zerolog.Ctx(ctx).Info().Msg("Starting parallel processing")

	if len(items) == 0 {
		return nil, lastProcessed, nil
	}

	// Setup options
	opt := ParallelOptions{
		CollectResults: true,
	}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if uint(len(items)) < opt.MaxRoutines {
		opt.MaxRoutines = uint(len(items))
	}
	if opt.MaxRoutines == 0 {
		opt.MaxRoutines = uint(runtime.NumCPU() * 4) //As our current user case is more I/O bound rather than CPU bound, we can use more goroutines
	}

	// Derive the worker context from the caller's context so the fan-out inherits
	// the caller's deadline; an optional per-run Timeout further bounds it.
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Timeout) //Context timeout
		defer cancel()
	}

	// Initialize results if needed
	if opt.CollectResults {
		results = make([]R, len(items))
	}

	// Worker pool implementation
	type workItem struct {
		index int
	}

	workChan := make(chan workItem, opt.MaxRoutines)
	var wg sync.WaitGroup

	// Start workers
	for i := uint(0); i < opt.MaxRoutines; i++ {
		wg.Add(1)
		//Separate go routines per worker to avoid blocking the main thread
		go func() {
			defer wg.Done()
			for work := range workChan {
				// Process with panic recovery
				func() {
					defer func() {
						if r := recover(); r != nil {
							// A panicking worker leaves the zero result behind, which a
							// caller cannot tell apart from a legitimately empty one — the
							// item would vanish from the output with no trace. Log it here
							// so every caller gets that for free.
							rlog.Error(ctx).Interface("panic", r).
								Int("index", work.index).Msg("parallel worker panicked")
						}
					}()

					result := processFunc(items[work.index])
					if opt.CollectResults {
						results[work.index] = result
					}
				}()

				//Check for timeout after the processing is done
				if ctx.Err() != nil {
					return
				}
			}
		}()
	}

	//Separate go routines to send work to workers to avoid blocking the main thread.
	//The dispatcher is part of wg so its writes to lastProcessed happen-before the
	//read at return (wg.Wait); it is not a worker, so close(workChan) still runs first.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(workChan)
		for idx, item := range items {
			select {
			case <-ctx.Done(): //Check timeout before sending work to workers
				return
			case workChan <- workItem{index: idx}:
				lastProcessed = item
			}
		}
	}()

	// Workers exit after their current item once ctx is cancelled; a processFunc
	// that ignores the caller's deadline can still block here, so keep per-item
	// work bounded by that same context.
	wg.Wait()

	zerolog.Ctx(ctx).Info().Msg("Parallel processing completed")

	// Handle results based on context and CollectResults flag
	if !opt.CollectResults {
		return nil, lastProcessed, ctx.Err()
	}
	return results, lastProcessed, ctx.Err()
}

func processParallelTest[T, R any](ctx context.Context, items []T, processFunc func(T) R, opts ...ParallelOptions) (results []R, lastProcessed T, err error) {
	//For ease of benchmarking
	//Use this function to test the performance of the parallel processing against the original function
	//It is not used in the production code

	return results, lastProcessed, nil
}

// ProcessParallelErr is ProcessParallel for work that can fail: results come back in input order, and the first error, or a panic turned into one, cancels the context in-flight calls hold and starts no further items. Items {1, 2, 3} where 2 fails -> (nil, 2's error). CollectResults does not apply.
func ProcessParallelErr[T, R any](ctx context.Context, items []T, processFunc func(context.Context, T) (R, error), opts ...ParallelOptions) ([]R, error) {
	if len(items) == 0 {
		return nil, nil
	}
	var opt ParallelOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	limit := runtime.NumCPU() * 4
	if opt.MaxRoutines > 0 {
		limit = int(min(opt.MaxRoutines, uint(len(items)))) //nolint:gosec // G115: capped at len(items), so it fits an int
	}
	if opt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Timeout)
		defer cancel()
	}

	results := make([]R, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, item := range items {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() (err error) {
			// Go blocks for a free slot, so an earlier failure can land between the check above and this start.
			if err := gctx.Err(); err != nil {
				return err
			}
			defer func() {
				if r := recover(); r != nil {
					err = rmerror.NewRMError(nil, fmt.Sprintf("parallel worker panicked on item %d: %v", i, r))
				}
			}()
			results[i], err = processFunc(gctx, item)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
