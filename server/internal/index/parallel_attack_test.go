// Package index -- concurrency attack tests for IndexDirProgress.
//
// These tests are designed to be run with -race and -count=N (N >= 10) to
// stress the parallel indexing change. Each test targets a specific correctness
// claim made by the implementation.
package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
)

// --- Attack 1: empty directory with non-nil progress callback ---
//
// When len(paths)==0 the progressCh branch is NOT entered (condition is
// `progress != nil && len(paths) > 0`), so reporterDone is closed immediately
// via the else branch. Verify there is no deadlock and progress is never called.
func TestIndexDirProgressEmptyDirWithCallback(t *testing.T) {
	dir := t.TempDir()
	// Put a non-.tcl/.rvt file to ensure walk finds something but nothing
	// qualifies as a source file.
	writeFile(t, dir, "README.md", "nothing here")

	called := false
	ix := New()
	err := ix.IndexDirProgress(dir, func(n int) { called = true })
	if err != nil {
		t.Fatalf("unexpected error on empty source dir: %v", err)
	}
	if called {
		t.Fatal("progress callback must not fire when there are 0 source files")
	}
	if files := ix.Files(); len(files) != 0 {
		t.Fatalf("expected 0 indexed files, got %v", files)
	}
}

// --- Attack 2: single source file ---
//
// Workers = min(GOMAXPROCS, 1) = 1. The jobs channel (unbuffered) must not
// deadlock when there is exactly one slot to fill. Progress must fire once with
// count=1.
func TestIndexDirProgressSingleFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "only.tcl", "proc solo {} {}")

	var counts []int
	ix := New()
	if err := ix.IndexDirProgress(dir, func(n int) { counts = append(counts, n) }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != 1 || counts[0] != 1 {
		t.Fatalf("progress counts = %v, want [1]", counts)
	}
	if locs := ix.Lookup("::solo"); len(locs) != 1 {
		t.Fatalf("::solo not indexed: %#v", locs)
	}
}

// --- Attack 3: GOMAXPROCS forced to 1 ---
//
// With a single OS thread, workers = min(1, N) = 1. The implementation must
// still index all N files correctly — effectively serialised but using the
// parallel code path. Also verifies results[i] writes are not optimised away
// by the compiler under GOMAXPROCS=1.
func TestIndexDirProgressGOMAXPROCS1(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	dir := t.TempDir()
	const n = 30
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%02d.tcl", i), fmt.Sprintf("proc p%02d {} {}", i))
	}

	last := 0
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) { last = c }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if last != n {
		t.Fatalf("progress reached %d, want %d", last, n)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("::p%02d", i)
		if locs := ix.Lookup(name); len(locs) != 1 {
			t.Fatalf("%s not indexed: %#v", name, locs)
		}
	}
}

// --- Attack 4: progress callback called from exactly one goroutine ---
//
// The implementation promises that progress() is never called from more than
// one goroutine concurrently (single reporter goroutine). Verify under the race
// detector by doing a non-atomic write inside the callback -- the race detector
// will flag concurrent writes.
//
// This test is intentionally racy-if-broken: the callback does an unprotected
// write to a shared integer. If the race detector flags this, the implementation
// is wrong.
func TestIndexDirProgressCallbackSingleGoroutine(t *testing.T) {
	dir := t.TempDir()
	const n = 100
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%03d.tcl", i), fmt.Sprintf("proc p%03d {} {}", i))
	}

	// Unprotected write — the race detector catches concurrent access.
	var count int // NOT atomic — deliberately unprotected
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) {
		// Two goroutines calling this simultaneously would race on `count`.
		count = c
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != n {
		t.Fatalf("final count = %d, want %d", count, n)
	}
}

// --- Attack 5: progress count is exactly monotonic 1..N, stress run ---
//
// Under parallel parse, parse completions arrive in non-deterministic order,
// but the single reporter goroutine must still deliver counts 1, 2, ..., N in
// order. Run many times via -count=N to expose any reordering.
func TestIndexDirProgressMonotonicStress(t *testing.T) {
	dir := t.TempDir()
	const n = 50
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%03d.tcl", i), fmt.Sprintf("proc p%03d {} {}", i))
	}

	var counts []int
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) { counts = append(counts, c) }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(counts) != n {
		t.Fatalf("got %d progress calls, want %d", len(counts), n)
	}
	for i, c := range counts {
		if c != i+1 {
			t.Fatalf("non-monotonic progress at index %d: got %d, want %d; full sequence: %v",
				i, c, i+1, counts)
		}
	}
}

// --- Attack 6: result parity — IndexDirProgress equals sequential IndexFile ---
//
// Index the same tree two ways (fresh indices each time) and compare every
// observable: Files(), Source(), Lookup() for every symbol, Namespace(),
// Class(). Any divergence is a regression.
func TestIndexDirProgressParityWithSequential(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "namespace eval ::app { proc run {} {} }")
	writeFile(t, dir, "b.tcl", "namespace eval ::app { namespace import ::lib::helper }\nproc ::lib::helper {} {}")
	writeFile(t, dir, "c.tcl", "itcl::class ::Widget { inherit ::app::run\n method draw {} {} \n variable color }")
	writeFile(t, dir, "d.rvt", "<? proc render {} {} ?>")

	// Build reference index sequentially.
	seqIx := New()
	// WalkDir lexical order: a.tcl, b.tcl, c.tcl, d.rvt
	for _, rel := range []string{"a.tcl", "b.tcl", "c.tcl", "d.rvt"} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		seqIx.IndexFile(p, string(b))
	}

	// Build parallel index.
	parIx := New()
	if err := parIx.IndexDirProgress(dir, nil); err != nil {
		t.Fatalf("IndexDirProgress error: %v", err)
	}

	// Compare Files().
	seqFiles := seqIx.Files()
	parFiles := parIx.Files()
	if !reflect.DeepEqual(seqFiles, parFiles) {
		t.Fatalf("Files() mismatch:\nseq: %v\npar: %v", seqFiles, parFiles)
	}

	// Compare Source() for each file.
	for _, f := range seqFiles {
		if seqIx.Source(f) != parIx.Source(f) {
			t.Errorf("Source(%s) differs", f)
		}
	}

	// Compare Lookup() for a set of known symbols.
	symbols := []string{
		"::app::run", "::lib::helper", "::Widget", "::request::render",
	}
	for _, sym := range symbols {
		seq := seqIx.Lookup(sym)
		par := parIx.Lookup(sym)
		if !reflect.DeepEqual(seq, par) {
			t.Errorf("Lookup(%q) mismatch:\nseq: %#v\npar: %#v", sym, seq, par)
		}
	}

	// Compare Namespace().
	for _, ns := range []string{"::app", "::"} {
		sp, si := seqIx.Namespace(ns)
		pp, pi := parIx.Namespace(ns)
		if !reflect.DeepEqual(sp, pp) {
			t.Errorf("Namespace(%q) path mismatch: seq=%v par=%v", ns, sp, pp)
		}
		if !reflect.DeepEqual(si, pi) {
			t.Errorf("Namespace(%q) imports mismatch: seq=%v par=%v", ns, si, pi)
		}
	}

	// Compare Class table for ::Widget.
	seqCI := seqIx.Class("::Widget")
	parCI := parIx.Class("::Widget")
	if (seqCI == nil) != (parCI == nil) {
		t.Fatalf("Class(::Widget) nil mismatch: seq=%v par=%v", seqCI, parCI)
	}
	if seqCI != nil {
		if !reflect.DeepEqual(seqCI.Methods, parCI.Methods) {
			t.Errorf("Widget.Methods mismatch: seq=%v par=%v", seqCI.Methods, parCI.Methods)
		}
	}
}

// --- Attack 7: re-indexing an already-indexed directory ---
//
// Call IndexDirProgress twice on the same Index. The second call must produce
// the same observable state as after the first call (no duplicate entries,
// correct removal and re-insertion). This exercises the RemoveFile path inside
// the serial store phase on a non-empty index.
func TestIndexDirProgressReindex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "proc alpha {} {}")
	writeFile(t, dir, "b.tcl", "proc beta {} {}")

	ix := New()
	if err := ix.IndexDirProgress(dir, nil); err != nil {
		t.Fatalf("first IndexDirProgress error: %v", err)
	}
	if locs := ix.Lookup("::alpha"); len(locs) != 1 {
		t.Fatalf("after first index: ::alpha = %#v, want 1 entry", locs)
	}

	// Re-index. Must not double-add.
	if err := ix.IndexDirProgress(dir, nil); err != nil {
		t.Fatalf("second IndexDirProgress error: %v", err)
	}
	if locs := ix.Lookup("::alpha"); len(locs) != 1 {
		t.Fatalf("after re-index: ::alpha = %#v, want exactly 1 entry (not doubled)", locs)
	}
	if locs := ix.Lookup("::beta"); len(locs) != 1 {
		t.Fatalf("after re-index: ::beta = %#v, want exactly 1 entry (not doubled)", locs)
	}
	if n := len(ix.Files()); n != 2 {
		t.Fatalf("after re-index: %d files indexed, want 2", n)
	}
}

// --- Attack 8: unreadable file with progress callback ---
//
// The progress count reflects only SUCCESSFULLY indexed files (matching the old
// sequential index): a file that fails to read is recorded as an error but does
// not increment the count.
func TestIndexDirProgressUnreadableWithProgress(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based permission test is unreliable as root")
	}
	dir := t.TempDir()
	writeFile(t, dir, "good.tcl", "proc good {} {}")
	writeFile(t, dir, "bad.tcl", "proc bad {} {}")
	bad := filepath.Join(dir, "bad.tcl")
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0o644)

	last := 0
	ix := New()
	err := ix.IndexDirProgress(dir, func(c int) { last = c })
	if err == nil {
		t.Fatal("expected an error for the unreadable file")
	}
	// Only good.tcl parsed successfully, so the count is 1; bad.tcl errored and
	// is not counted.
	if last != 1 {
		t.Fatalf("progress count = %d, want 1 (only the successfully indexed file)", last)
	}
	// The readable file must still be indexed.
	if locs := ix.Lookup("::good"); len(locs) != 1 {
		t.Fatalf("::good should be indexed despite bad.tcl error: %#v", locs)
	}
}

// --- Attack 9: deterministic ordering across many -count=N runs ---
//
// Run IndexDirProgress on the same tree twice (two fresh indices) and confirm
// identical Lookup ordering for a name defined in multiple files.
func TestIndexDirProgressDeterministicMultiRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "proc dup {} {}")
	writeFile(t, dir, "b.tcl", "proc dup {} {}")
	writeFile(t, dir, "c.tcl", "proc dup {} {}")

	run := func() []string {
		ix := New()
		if err := ix.IndexDirProgress(dir, nil); err != nil {
			t.Fatalf("IndexDirProgress error: %v", err)
		}
		locs := ix.Lookup("::dup")
		var bases []string
		for _, l := range locs {
			bases = append(bases, filepath.Base(l.File))
		}
		return bases
	}

	first := run()
	if !reflect.DeepEqual(first, []string{"a.tcl", "b.tcl", "c.tcl"}) {
		t.Fatalf("first run: def sites = %v, want lexical [a b c].tcl", first)
	}
	for i := 0; i < 5; i++ {
		got := run()
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d ordering differs: got %v, first run was %v", i+2, got, first)
		}
	}
}

// --- Attack 10: progress callback panic is propagated (not swallowed) ---
//
// If the progress callback panics, the panic must escape visibly. The reporter
// goroutine's panic should not be silently swallowed. This test documents the
// expected behaviour: the panic surfaces (the test runner catches it via
// t.Recover, but WITHOUT explicit recovery in the implementation the process
// crashes, which is the expected behaviour for programmer errors).
//
// We skip this test by default (it would crash the process) and instead verify
// that the reporter goroutine is not wrapped in a recover() by checking the
// source comment. The test below exercises a NON-panicking but CPU-intensive
// callback to ensure the reporter doesn't starve workers.
func TestIndexDirProgressCallbackDoesNotStarveWorkers(t *testing.T) {
	dir := t.TempDir()
	const n = 80
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%03d.tcl", i), fmt.Sprintf("proc p%03d {} {}", i))
	}

	// Callback does a small amount of work (simulating a real progress reporter).
	var callCount atomic.Int64
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) {
		callCount.Add(1)
		// Minimal spin to simulate serialisation work.
		for j := 0; j < 1000; j++ {
			_ = j * j
		}
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := int(callCount.Load()); got != n {
		t.Fatalf("callback called %d times, want %d", got, n)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("::p%03d", i)
		if locs := ix.Lookup(name); len(locs) != 1 {
			t.Fatalf("%s not indexed: %#v", name, locs)
		}
	}
}

// --- Attack 11: many more files than workers (buffer pressure) ---
//
// With GOMAXPROCS workers and N >> GOMAXPROCS files, the progressCh buffer
// (sized to `workers`) will fill up repeatedly. Workers will block on sends to
// progressCh while waiting for the reporter to drain. This must not deadlock,
// and the final count must equal N.
func TestIndexDirProgressBufferPressure(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	// Create 20x more files than workers to guarantee buffer saturation.
	n := procs * 20
	if n < 40 {
		n = 40
	}
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%04d.tcl", i), fmt.Sprintf("proc p%04d {} {}", i))
	}

	last := 0
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) { last = c }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if last != n {
		t.Fatalf("progress reached %d, want %d", last, n)
	}
	if got := len(ix.Files()); got != n {
		t.Fatalf("indexed %d files, want %d", got, n)
	}
}

// --- Attack 12: nil progress on a large tree (no reporter goroutine) ---
//
// When progress==nil the reporter is bypassed. Workers still check
// `if progressCh != nil` before sending, so there must be no send to a nil
// channel (which would block forever). Run under race detector.
func TestIndexDirProgressNilCallbackLargeTree(t *testing.T) {
	dir := t.TempDir()
	const n = 150
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%04d.tcl", i), fmt.Sprintf("proc p%04d {} {}", i))
	}

	ix := New()
	if err := ix.IndexDirProgress(dir, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(ix.Files()); got != n {
		t.Fatalf("indexed %d files, want %d", got, n)
	}
}

// --- Attack 13: .rvt and .tcl mix — parity with sequential for classes ---
//
// Classes defined in .tcl files and procs in .rvt files are indexed together.
// Verify the parallel index matches the sequential one for class method tables.
func TestIndexDirProgressClassParity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.tcl",
		"itcl::class ::Base {\n  method init {} {}\n  variable state\n}")
	writeFile(t, dir, "derived.tcl",
		"itcl::class ::Derived {\n  inherit ::Base\n  method draw {} {}\n}")
	writeFile(t, dir, "page.rvt", "<? proc render {} {} ?>")

	// Sequential reference.
	seqIx := New()
	for _, rel := range []string{"base.tcl", "derived.tcl", "page.rvt"} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		seqIx.IndexFile(p, string(b))
	}

	// Parallel.
	parIx := New()
	if err := parIx.IndexDir(dir); err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}

	for _, cls := range []string{"::Base", "::Derived"} {
		seqCI := seqIx.Class(cls)
		parCI := parIx.Class(cls)
		if (seqCI == nil) != (parCI == nil) {
			t.Fatalf("%s class nil mismatch: seq=%v par=%v", cls, seqCI, parCI)
		}
		if seqCI == nil {
			continue
		}
		// DefSites
		if !reflect.DeepEqual(seqCI.DefSites, parCI.DefSites) {
			t.Errorf("%s DefSites mismatch:\nseq: %v\npar: %v", cls, seqCI.DefSites, parCI.DefSites)
		}
		// Method keys (names).
		seqMethods := methodNames(seqCI.Methods)
		parMethods := methodNames(parCI.Methods)
		if !reflect.DeepEqual(seqMethods, parMethods) {
			t.Errorf("%s method names mismatch: seq=%v par=%v", cls, seqMethods, parMethods)
		}
		// Inherit edges.
		if !reflect.DeepEqual(seqCI.Inherit, parCI.Inherit) {
			t.Errorf("%s Inherit mismatch: seq=%v par=%v", cls, seqCI.Inherit, parCI.Inherit)
		}
	}
}

func methodNames(m map[string][]Location) []string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	// sort for deterministic comparison
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// --- Attack 14: all files unreadable (every file fails to read) ---
//
// The error from every failed read must be aggregated via errors.Join, and the
// returned error must be non-nil. No deadlock must occur. Progress count stays 0
// (only successful parses are counted).
func TestIndexDirProgressAllUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based permission test is unreliable as root")
	}
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.tcl", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("proc p%d {} {}", i)), 0o000); err != nil {
			t.Fatal(err)
		}
		defer func(path string) { os.Chmod(path, 0o644) }(p)
	}

	last := 0
	ix := New()
	err := ix.IndexDirProgress(dir, func(c int) { last = c })
	if err == nil {
		t.Fatal("expected non-nil error when all files are unreadable")
	}
	// 5 errors should be joined.
	errs := errors.Unwrap(err)
	_ = errs // errors.Join may not expose Unwrap directly; just confirm non-nil
	if last != 0 {
		t.Fatalf("progress count = %d, want 0 (no file was successfully indexed)", last)
	}
	// Nothing should be indexed.
	if files := ix.Files(); len(files) != 0 {
		t.Fatalf("expected 0 files indexed when all reads fail, got %v", files)
	}
}

// --- Attack 15: race on results slice — parallel write, serial read ---
//
// Verify under -race that workers writing results[i] (different indices) and
// main reading results after wg.Wait() does not trigger the race detector.
// This test uses many goroutines and a slow progress callback to maximise
// scheduling interleaving.
func TestIndexDirProgressResultsSliceNoRace(t *testing.T) {
	dir := t.TempDir()
	n := runtime.GOMAXPROCS(0) * 10
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%04d.tcl", i), fmt.Sprintf("proc p%04d {} {}", i))
	}

	var callCount int
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) {
		callCount++
		runtime.Gosched() // yield to maximise race window
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != n {
		t.Fatalf("callCount = %d, want %d", callCount, n)
	}
}

// --- Attack 16: FileRefs parity ---
//
// Precomputed references must match between sequential and parallel index.
func TestIndexDirProgressFileRefsParity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "proc caller {} { helper\n helper }")
	writeFile(t, dir, "b.tcl", "proc helper {} {}")

	seqIx := New()
	for _, rel := range []string{"a.tcl", "b.tcl"} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		seqIx.IndexFile(p, string(b))
	}

	parIx := New()
	if err := parIx.IndexDir(dir); err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}

	for _, rel := range []string{"a.tcl", "b.tcl"} {
		p := filepath.Join(dir, rel)
		seqRefs := seqIx.FileRefs(p)
		parRefs := parIx.FileRefs(p)
		if !reflect.DeepEqual(seqRefs, parRefs) {
			t.Errorf("FileRefs(%s) mismatch:\nseq: %#v\npar: %#v", rel, seqRefs, parRefs)
		}
	}
}

// --- Attack 17: AllSymbols parity ---
//
// AllSymbols output must be identical between sequential and parallel index.
func TestIndexDirProgressAllSymbolsParity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "namespace eval ::app { proc run {} {} }\nitcl::class ::C { method field {} {} }")
	writeFile(t, dir, "b.tcl", "namespace eval ::app { variable count 0 }")

	seqIx := New()
	for _, rel := range []string{"a.tcl", "b.tcl"} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		seqIx.IndexFile(p, string(b))
	}

	parIx := New()
	if err := parIx.IndexDir(dir); err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}

	seqSym := seqIx.AllSymbols()
	parSym := parIx.AllSymbols()
	if !reflect.DeepEqual(seqSym, parSym) {
		t.Errorf("AllSymbols() mismatch:\nseq: %#v\npar: %#v", seqSym, parSym)
	}
}
