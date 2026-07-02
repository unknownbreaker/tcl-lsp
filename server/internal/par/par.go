// Package par holds small generic helpers for parallelizing batch operations
// that run while the LSP dispatch loop is blocked on a single request -- so the
// shared index is not being mutated concurrently and read-only fan-out is safe.
// The mapped function must be safe to call concurrently (read-only shared state,
// or writes to disjoint locations).
package par

import (
	"runtime"
	"sync"
)

// Map applies fn to every item across up to GOMAXPROCS workers and returns the
// results in item order: results[i] == fn(items[i]). fn must be safe for
// concurrent calls. With 0 or 1 items (or a single CPU) it runs inline with no
// goroutines. Race-free by construction: each result slot is written by exactly
// one worker and read only after every worker has finished.
func Map[W, R any](items []W, fn func(W) R) []R {
	results := make([]R, len(items))
	workers := runtime.GOMAXPROCS(0)
	if workers > len(items) {
		workers = len(items)
	}
	if workers <= 1 {
		for i, it := range items {
			results[i] = fn(it)
		}
		return results
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = fn(items[i])
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// Chunk splits items into at most k contiguous, roughly equal groups (trailing
// empties omitted). Batch many cheap items into a few chunks before Map so the
// per-item channel overhead does not swamp the work.
func Chunk[W any](items []W, k int) [][]W {
	if len(items) == 0 {
		return nil
	}
	if k < 1 {
		k = 1
	}
	if k > len(items) {
		k = len(items)
	}
	size := (len(items) + k - 1) / k
	chunks := make([][]W, 0, k)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}
