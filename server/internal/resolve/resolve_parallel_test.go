package resolve

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// References scans files in parallel. Each caller here makes an UNQUALIFIED call
// to a global proc from a DISTINCT namespace, so resolution calls
// index.Namespace(::nsK) with a fresh cache key per file. Without the
// single-threaded pre-warm, those concurrent nsCache misses would be a
// concurrent map write -- a Go fatal panic (and a -race failure). This is the
// guard that the warm-up makes the parallel scan touch nsCache read-only.
//
// Run under -race -count=N to stress the fan-out.
func TestReferencesParallelCrossNamespace(t *testing.T) {
	ix := index.New()
	defSrc := "proc target {} {}"
	ix.IndexFile("def.tcl", defSrc)

	const n = 100
	for k := 0; k < n; k++ {
		src := fmt.Sprintf("namespace eval ::ns%03d {\n  proc caller {} { target }\n}", k)
		ix.IndexFile(fmt.Sprintf("caller%03d.tcl", k), src)
	}

	r := New(ix)
	off := strings.Index(defSrc, "target")
	locs := r.References("def.tcl", defSrc, off)

	callers := 0
	for _, l := range locs {
		if strings.HasPrefix(filepath.Base(l.File), "caller") {
			callers++
		}
	}
	if callers != n {
		t.Fatalf("found %d caller references, want %d (parallel scan lost sites)", callers, n)
	}
}

// The parallel scan concatenates per-file results in work order (current file
// first, then Files() sorted), so the reference order is deterministic across
// runs -- identical to the old sequential scan.
func TestReferencesParallelDeterministicOrder(t *testing.T) {
	ix := index.New()
	defSrc := "proc target {} {}"
	ix.IndexFile("def.tcl", defSrc)
	ix.IndexFile("a.tcl", "proc ca {} { target }")
	ix.IndexFile("b.tcl", "proc cb {} { target }")
	ix.IndexFile("c.tcl", "proc cc {} { target }")

	r := New(ix)
	off := strings.Index(defSrc, "target")

	var first []string
	for run := 0; run < 20; run++ {
		var files []string
		for _, l := range r.References("def.tcl", defSrc, off) {
			files = append(files, filepath.Base(l.File))
		}
		if run == 0 {
			first = files
			continue
		}
		if strings.Join(files, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d order %v != run 0 order %v", run, files, first)
		}
	}
	// References returns usages, not the declaration, so def.tcl (which only
	// *defines* target) contributes nothing; the call sites come in sorted
	// Files() order.
	want := []string{"a.tcl", "b.tcl", "c.tcl"}
	if strings.Join(first, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", first, want)
	}
}
