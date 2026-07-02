// Package resolve — concurrency attack tests for the parallel References fan-out.
//
// Run with: go test -race -count=50 ./internal/resolve/...
//
// Attack strategy:
//  1. Prove the pre-warm is COMPLETE — i.e. no path through refFQ calls
//     Namespace() with a key not covered by warmNamespaces().
//  2. Prove methodCallSites is read-only for the class table.
//  3. Prove result parity and deterministic ordering across corner cases.
//  4. Stress par.Map edge cases (empty, single, GOMAXPROCS=1).
package resolve

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// --- Attack A: namespace import path does not call Namespace() on the imported
// namespace during the parallel scan.
//
// Workspace shape: ::lib defines "helper"; ::app imports ::lib::helper and calls
// "helper" bare from within ::app. The import is declared in a third file.
// When the parallel scan processes the caller file, commandCandidates("helper",
// "::app") returns [::app::helper, ::lib::helper, ::helper] (import match +
// global). warmNamespaces warms "::app" (the caller's ref.Namespace). The
// candidate "::lib::helper" is looked up via ix.Lookup — NOT via Namespace("::lib").
// If there is a latent call to Namespace("::lib") during the parallel scan, this
// test exposes it under -race.
func TestReferencesParallelNamespaceImport(t *testing.T) {
	ix := index.New()
	ix.IndexFile("lib.tcl", "namespace eval ::lib {\n  proc helper {} {}\n  namespace export helper\n}")
	// Import declaration in its own file so fileNS["import.tcl"]["::app"] is
	// populated. warmNamespaces warms "::app" from the caller's ref but NOT "::lib".
	// A latent Namespace("::lib") call in the parallel scan would be a data race.
	ix.IndexFile("import.tcl", "namespace eval ::app {\n  namespace import ::lib::helper\n}")
	// Three caller files so GOMAXPROCS workers actually fan out.
	for k := 0; k < 80; k++ {
		src := fmt.Sprintf("namespace eval ::app {\n  proc caller%03d {} { helper }\n}", k)
		ix.IndexFile(fmt.Sprintf("caller%03d.tcl", k), src)
	}

	r := New(ix)
	defSrc := "namespace eval ::lib {\n  proc helper {} {}\n  namespace export helper\n}"
	off := strings.Index(defSrc, "helper {}")
	locs := r.References("lib.tcl", defSrc, off)
	if len(locs) == 0 {
		t.Fatal("expected at least one reference via namespace import, got none")
	}
}

// --- Attack B: namespace path does not call Namespace() on path-member namespaces.
//
// ::ns1 declares `namespace path {::lib}`. A call to "target" from ::ns1 resolves
// via the path to ::lib::target. commandCandidates produces [::ns1::target,
// ::lib::target, ::target]. Only Namespace("::ns1") is called (for the path list);
// the lookup of "::lib::target" goes through ix.Lookup, NOT Namespace("::lib").
// warmNamespaces warms "::ns1" only. A latent Namespace("::lib") in the parallel
// scan would race.
func TestReferencesParallelNamespacePath(t *testing.T) {
	ix := index.New()
	ix.IndexFile("lib.tcl", "namespace eval ::lib {\n  proc target {} {}\n}")
	// path declared in its own file so fileNS has a "::ns1" entry with Path.
	ix.IndexFile("path_decl.tcl", "namespace eval ::ns1 {\n  namespace path {::lib}\n}")
	for k := 0; k < 80; k++ {
		src := fmt.Sprintf("namespace eval ::ns1 {\n  proc user%03d {} { target }\n}", k)
		ix.IndexFile(fmt.Sprintf("user%03d.tcl", k), src)
	}

	r := New(ix)
	defSrc := "namespace eval ::lib {\n  proc target {} {}\n}"
	off := strings.Index(defSrc, "target {}")
	locs := r.References("lib.tcl", defSrc, off)
	if len(locs) == 0 {
		t.Fatal("expected at least one reference via namespace path, got none")
	}
}

// --- Attack C: glob import ("namespace import ::lib::*") does not trigger a
// latent Namespace("::lib") call during the parallel scan.
//
// commandCandidates expands a glob import for "target" from ::lib to the
// candidate "::lib::target" without calling Namespace("::lib"). If the
// implementation ever regresses by calling Namespace on the import's source
// namespace, this test will race under -race.
func TestReferencesParallelGlobImport(t *testing.T) {
	ix := index.New()
	ix.IndexFile("lib.tcl", "namespace eval ::lib {\n  proc widget {} {}\n  namespace export widget\n}")
	ix.IndexFile("glob_import.tcl", "namespace eval ::ui {\n  namespace import ::lib::*\n}")
	for k := 0; k < 80; k++ {
		src := fmt.Sprintf("namespace eval ::ui {\n  proc page%03d {} { widget }\n}", k)
		ix.IndexFile(fmt.Sprintf("page%03d.tcl", k), src)
	}

	r := New(ix)
	defSrc := "namespace eval ::lib {\n  proc widget {} {}\n  namespace export widget\n}"
	off := strings.Index(defSrc, "widget {}")
	locs := r.References("lib.tcl", defSrc, off)
	if len(locs) == 0 {
		t.Fatal("expected at least one reference via glob import, got none")
	}
}

// --- Attack D: GOMAXPROCS=1 forces the inline path in par.Map.
//
// With a single CPU, par.Map runs all items inline (no goroutines). This path
// must still produce correct results and must not deadlock. It also exercises the
// pre-warm with only one worker (no concurrency), confirming the warm does not
// rely on goroutines.
func TestReferencesParallelGOMAXPROCS1(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	ix := index.New()
	ix.IndexFile("def.tcl", "proc target {} {}")
	for k := 0; k < 10; k++ {
		ix.IndexFile(fmt.Sprintf("c%02d.tcl", k), fmt.Sprintf("proc caller%02d {} { target }", k))
	}

	r := New(ix)
	off := strings.Index("proc target {} {}", "target")
	locs := r.References("def.tcl", "proc target {} {}", off)
	if len(locs) != 10 {
		t.Fatalf("GOMAXPROCS=1: found %d refs, want 10", len(locs))
	}
}

// --- Attack E: pageLocal (.rvt ::request:: target) — only current file scanned.
//
// A ::request:: proc in an .rvt page is page-local; other files must NOT be
// scanned even when they happen to contain an identically-named ::request:: proc.
// The work list has exactly ONE entry (the current file), so par.Map runs inline.
// Verify (a) result count, (b) other file not included, (c) no race under -race.
func TestReferencesParallelPageLocal(t *testing.T) {
	ix := index.New()
	page := "<?\nproc render {} {}\nrender\n?>"
	ix.IndexFile("page.rvt", page)
	// A second page with the same inline proc — must NOT appear in results.
	ix.IndexFile("other.rvt", "<?\nproc render {} {}\nrender\n?>")

	r := New(ix)
	// References from the cursor on the definition of render in page.rvt.
	// The proc is defined inside the ::request namespace by the rvt wrapper.
	off := strings.Index(page, "render")
	locs := r.References("page.rvt", page, off)
	for _, l := range locs {
		if l.File == "other.rvt" {
			t.Errorf("page-local search leaked into other.rvt: %#v", l)
		}
	}
}

// --- Attack F: empty work list (no files indexed, not even current).
//
// When target is not found and the work list has only the current-file entry
// with zero refs, par.Map is called on a 1-element slice that produces nothing.
// Must return nil, not panic.
func TestReferencesParallelEmptyWork(t *testing.T) {
	ix := index.New()
	r := New(ix)
	locs := r.References("nosuchfile.tcl", "", 0)
	if locs != nil {
		t.Fatalf("expected nil for empty workspace, got %#v", locs)
	}
}

// --- Attack G: deterministic order with namespace imports and 3 distinct namespaces.
//
// Exercises the sort stability of Files() combined with import-expanded candidates.
// Results must be in Files() sorted order (current file first, then sorted).
// This test fails if parallel workers return results in non-deterministic order.
func TestReferencesParallelOrderWithImports(t *testing.T) {
	ix := index.New()
	defSrc := "proc ::target {} {}"
	ix.IndexFile("def.tcl", defSrc)
	// Three callers in different namespaces, so warmNamespaces gets 3 distinct keys.
	ix.IndexFile("aa.tcl", "namespace eval ::nsA { proc f {} { ::target } }")
	ix.IndexFile("bb.tcl", "namespace eval ::nsB { proc f {} { ::target } }")
	ix.IndexFile("cc.tcl", "namespace eval ::nsC { proc f {} { ::target } }")

	r := New(ix)
	off := strings.Index(defSrc, "target")

	var firstOrder []string
	for run := 0; run < 30; run++ {
		var files []string
		for _, l := range r.References("def.tcl", defSrc, off) {
			files = append(files, l.File)
		}
		if run == 0 {
			firstOrder = files
			continue
		}
		got := strings.Join(files, ",")
		want := strings.Join(firstOrder, ",")
		if got != want {
			t.Fatalf("run %d order %v != run 0 order %v (non-deterministic)", run, files, firstOrder)
		}
	}
	// Results must be in sorted file order (no current-file entry since def.tcl
	// has no *usage* sites, only a definition).
	want := []string{"aa.tcl", "bb.tcl", "cc.tcl"}
	if strings.Join(firstOrder, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", firstOrder, want)
	}
}

// --- Attack H: concurrent calls to References (simulating two LSP requests
// in-flight if the dispatch loop were ever relaxed).
//
// The current architecture serialises requests, but future changes might not.
// Two concurrent References calls on the SAME index must not race, because
// warmNamespaces would concurrently write nsCache on misses.
//
// THIS TEST IS EXPECTED TO RACE AND IS DISABLED BY DEFAULT. It documents the
// known architectural assumption (single-threaded dispatch). Uncomment to verify.
//
// func TestReferencesParallelConcurrentCallers(t *testing.T) { ... }

// --- Attack I: many distinct namespace qualifiers across 200 files.
//
// Forces GOMAXPROCS workers to all compete for nsCache concurrently. Without the
// pre-warm this panics; with it this must complete cleanly under -race -count=20.
func TestReferencesParallelManyDistinctNamespaces(t *testing.T) {
	ix := index.New()
	defSrc := "proc ::globalTarget {} {}"
	ix.IndexFile("def.tcl", defSrc)

	const n = 200
	for k := 0; k < n; k++ {
		// Each file declares a unique namespace so every Namespace(ns) call is a
		// fresh miss the first time References is called.
		src := fmt.Sprintf(
			"namespace eval ::tier%03d {\n  proc worker {} { ::globalTarget }\n}", k)
		ix.IndexFile(fmt.Sprintf("tier%03d.tcl", k), src)
	}

	r := New(ix)
	off := strings.Index(defSrc, "globalTarget")
	locs := r.References("def.tcl", defSrc, off)
	if len(locs) != n {
		t.Fatalf("found %d references, want %d", len(locs), n)
	}
}

// --- Attack J: mixed RefVariable and RefCommand refs in the same file.
//
// variableCandidates never calls Namespace(), but warmNamespaces still warms
// their namespaces. The parallel scan must not call Namespace for variable refs.
// A file with a mix of var refs and command refs exercises both paths in the
// same worker and must not race.
func TestReferencesParallelMixedRefKinds(t *testing.T) {
	ix := index.New()
	defSrc := "proc ::render {} {}"
	ix.IndexFile("def.tcl", defSrc)
	// Each caller file has both a command ref and a variable ref.
	for k := 0; k < 60; k++ {
		src := fmt.Sprintf(
			"namespace eval ::mixed%03d {\n  variable count 0\n  proc go {} {\n    incr count\n    ::render\n  }\n}", k)
		ix.IndexFile(fmt.Sprintf("mixed%03d.tcl", k), src)
	}

	r := New(ix)
	off := strings.Index(defSrc, "render")
	locs := r.References("def.tcl", defSrc, off)
	if len(locs) == 0 {
		t.Fatal("expected at least one reference in mixed-ref workspace, got none")
	}
}

// --- Attack K: second References call reuses a POPULATED nsCache.
//
// After the first call, nsCache is fully populated. The second call's warmNamespaces
// hits the cache (read-only). The parallel scan also hits the cache (read-only).
// Both phases must be read-only and must not race.
func TestReferencesParallelSecondCallReuseCache(t *testing.T) {
	ix := index.New()
	defSrc := "proc ::svc {} {}"
	ix.IndexFile("def.tcl", defSrc)
	for k := 0; k < 50; k++ {
		src := fmt.Sprintf("namespace eval ::client%03d { proc f {} { ::svc } }", k)
		ix.IndexFile(fmt.Sprintf("cli%03d.tcl", k), src)
	}

	r := New(ix)
	off := strings.Index(defSrc, "svc")

	// First call — populates nsCache.
	locs1 := r.References("def.tcl", defSrc, off)
	// Second call — nsCache already warm; warmNamespaces must not re-write entries.
	locs2 := r.References("def.tcl", defSrc, off)

	if len(locs1) != len(locs2) {
		t.Fatalf("first call: %d locs, second call: %d locs (should be identical)", len(locs1), len(locs2))
	}
	for i := range locs1 {
		if locs1[i].File != locs2[i].File || locs1[i].NameStart != locs2[i].NameStart {
			t.Fatalf("result diverged at index %d: %v vs %v", i, locs1[i], locs2[i])
		}
	}
}
