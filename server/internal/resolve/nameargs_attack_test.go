package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// ATTACK (point 5, frame/scope correctness): inside `namespace eval ::app`,
// a bare `info exists cfg` name-arg ref must resolve to ::app::cfg FIRST
// (namespace-qualified), not to an unrelated top-level ::cfg.
func TestNameArgNamespaceEvalScoping(t *testing.T) {
	src := "namespace eval ::app {\n" +
		"  variable cfg\n" +
		"  set cfg 1\n" +
		"  if {[info exists cfg]} { puts yes }\n" +
		"}\n" +
		"set ::cfg 999\n" // decoy top-level global with the same bare name
	ix := index.New()
	ix.IndexFile("app.tcl", src)
	r := New(ix)

	useOff := strings.Index(src, "cfg]") // the info-exists occurrence
	locs := r.Definition("app.tcl", src, useOff)
	if len(locs) == 0 {
		t.Fatalf("info exists cfg inside namespace eval ::app resolved to nothing: %#v", locs)
	}
	for _, l := range locs {
		if l.Name != "::app::cfg" {
			t.Errorf("info exists cfg inside namespace eval ::app should resolve to ::app::cfg, got %q (locs=%#v)", l.Name, locs)
		}
	}
}

// ATTACK (point 4, reaching-defs interplay): a bareword name-arg ref inside a
// proc must be claimed as a LOCAL by localAt/reaching-defs, and goto-def from
// the info-exists use must jump to the most recent reassignment reaching that
// point -- not fall through to any global of the same name.
func TestNameArgReachingDefsInProc(t *testing.T) {
	src := "proc p {} {\n" +
		"  set x 1\n" +
		"  set x 2\n" +
		"  if {[info exists x]} {\n" +
		"    puts $x\n" +
		"  }\n" +
		"}\n" +
		"set x 999\n" // decoy top-level global with the same bare name
	ix := index.New()
	ix.IndexFile("p.tcl", src)
	r := New(ix)

	useOff := strings.Index(src, "x]") // the info-exists x occurrence
	locs := r.Definition("p.tcl", src, useOff)
	if len(locs) == 0 {
		t.Fatalf("info exists x inside proc p resolved to nothing (expected the local 'set x 2'): %#v", locs)
	}
	secondSetX := strings.LastIndex(src[:strings.Index(src, "if {[info exists")], "set x 2")
	wantStart := secondSetX + len("set ")
	found := false
	for _, l := range locs {
		if l.File == "p.tcl" && l.NameStart == wantStart {
			found = true
		}
		if l.File != "p.tcl" {
			t.Errorf("info-exists local x wrongly resolved outside the proc's file/scope: %#v", l)
		}
	}
	if !found {
		t.Errorf("info exists x did not resolve to the reaching 'set x 2' binding: got %#v, want NameStart=%d", locs, wantStart)
	}
}

// ATTACK (point 2, double-counting in find-references): from the definition
// site, References() must list the info-exists guard site exactly ONCE, even
// though the guard's word is scanned by two independent code paths in
// CommandRefs (the ordinary word loop AND nameArgRefs, for the array case)
// or when the same file is asked twice via workspace + local file overlap.
func TestNameArgFindReferencesNoDuplicateSite(t *testing.T) {
	def := "set ::something(a) 1\n"
	use := "if {[info exists ::something($env(HOME))]} { puts hi }\n"
	ix := index.New()
	ix.IndexFile("def.tcl", def)
	ix.IndexFile("use.tcl", use)
	r := New(ix)

	refs := r.References("def.tcl", def, strings.Index(def, "::something"))
	count := 0
	wantStart := strings.Index(use, "::something")
	for _, l := range refs {
		if l.File == "use.tcl" && l.NameStart == wantStart {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 reference at the info-exists guard site, got %d: %#v", count, refs)
	}
}

// ATTACK (point 4, document highlight): highlighting a namespace-level array
// variable must include its `array exists` / `info exists` name-arg sites
// exactly once each, alongside the `array set` definition site.
func TestNameArgDocumentHighlightIncludesGuardSites(t *testing.T) {
	src := "namespace eval ::app {\n" +
		"  variable cfg\n" +
		"  array set cfg {a 1}\n" +
		"  if {[array exists cfg]} { puts yes }\n" +
		"  if {[info exists cfg(a)]} { puts yes2 }\n" +
		"}\n"
	ix := index.New()
	ix.IndexFile("app.tcl", src)
	r := New(ix)

	// Cursor on the array-set target.
	setOff := strings.Index(src, "array set cfg") + len("array set ")
	locs := r.FileHighlights("app.tcl", src, setOff)
	if len(locs) == 0 {
		t.Fatalf("document highlight on array-set target found nothing: %#v", locs)
	}
	arrayExistsOff := strings.Index(src, "array exists cfg") + len("array exists ")
	infoExistsOff := strings.Index(src, "info exists cfg(a)") + len("info exists ")
	arrayExistsCount, infoExistsCount := 0, 0
	for _, l := range locs {
		if l.NameStart == arrayExistsOff {
			arrayExistsCount++
		}
		if l.NameStart == infoExistsOff {
			infoExistsCount++
		}
	}
	if arrayExistsCount != 1 {
		t.Errorf("array exists cfg guard site: want 1 highlight, got %d: %#v", arrayExistsCount, locs)
	}
	if infoExistsCount != 1 {
		t.Errorf("info exists cfg(a) guard site: want 1 highlight, got %d: %#v", infoExistsCount, locs)
	}
}

// ATTACK (point 3, RVT seam): info exists inside a Rivet <? ?> script island
// must produce a correctly-translated SOURCE-coordinate reference, not a
// virtual/stitched-script offset.
func TestNameArgRVTSeamOffsets(t *testing.T) {
	src := "<html>\n<? if {[info exists ::pageVar]} { ?>\nshown\n<? } ?>\n</html>\n"
	ix := index.New()
	ix.IndexFile("page.rvt", src)
	r := New(ix)

	wantOff := strings.Index(src, "::pageVar")
	locs := r.Definition("page.rvt", src, wantOff)
	// No definition exists anywhere, so Definition() may be empty -- that's
	// fine; what must NOT happen is the reference reporting an offset outside
	// the .rvt source bounds, or References() finding it at the wrong spot.
	_ = locs

	def := "set ::pageVar 1\n"
	ix.IndexFile("def.tcl", def)
	r2 := New(ix)
	refs := r2.References("def.tcl", def, strings.Index(def, "::pageVar"))
	found := false
	for _, l := range refs {
		if l.File == "page.rvt" {
			if l.NameStart != wantOff || l.NameEnd != wantOff+len("::pageVar") {
				t.Errorf(".rvt info-exists ref offset wrong: got [%d,%d), want [%d,%d)", l.NameStart, l.NameEnd, wantOff, wantOff+len("::pageVar"))
			}
			found = true
		}
	}
	if !found {
		t.Errorf("info exists ::pageVar inside .rvt <? ?> block not found by workspace References(): %#v", refs)
	}
}
