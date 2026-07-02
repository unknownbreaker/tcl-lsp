package par

import (
	"reflect"
	"sync/atomic"
	"testing"
)

// Map returns results in item order and applies fn to every item exactly once.
func TestMapOrderAndCoverage(t *testing.T) {
	items := make([]int, 1000)
	for i := range items {
		items[i] = i
	}
	var calls atomic.Int64
	got := Map(items, func(n int) int {
		calls.Add(1)
		return n * n
	})
	if calls.Load() != int64(len(items)) {
		t.Fatalf("fn called %d times, want %d", calls.Load(), len(items))
	}
	for i := range items {
		if got[i] != i*i {
			t.Fatalf("results[%d] = %d, want %d (order not preserved)", i, got[i], i*i)
		}
	}
}

// Map handles the trivial sizes (the inline fast path) without goroutines.
func TestMapEmptyAndSingle(t *testing.T) {
	if got := Map([]int(nil), func(n int) int { return n }); len(got) != 0 {
		t.Fatalf("empty Map = %v, want empty", got)
	}
	if got := Map([]int{7}, func(n int) int { return n + 1 }); !reflect.DeepEqual(got, []int{8}) {
		t.Fatalf("single Map = %v, want [8]", got)
	}
}

func TestChunk(t *testing.T) {
	// 10 items into 3 chunks -> sizes 4,4,2, contiguous, covering everything.
	items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	chunks := Chunk(items, 3)
	var flat []int
	for _, c := range chunks {
		flat = append(flat, c...)
	}
	if !reflect.DeepEqual(flat, items) {
		t.Fatalf("chunks flattened = %v, want %v", flat, items)
	}
	if len(chunks) > 3 {
		t.Fatalf("got %d chunks, want <= 3", len(chunks))
	}
	if got := Chunk([]int{}, 4); got != nil {
		t.Fatalf("Chunk of empty = %v, want nil", got)
	}
	if got := Chunk([]int{1, 2}, 10); len(got) != 2 {
		t.Fatalf("Chunk with k>len should cap at len: got %d chunks", len(got))
	}
}
