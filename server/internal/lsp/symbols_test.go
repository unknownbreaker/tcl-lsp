package lsp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// childNames returns the names of syms in order.
func childNames(syms []DocumentSymbol) []string {
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.Name)
	}
	return out
}

// A class declared between two procs appears between them (source order), not
// hoisted above them -- so an outline reflects the file top-to-bottom.
func TestBuildDocumentSymbolsSourceOrderRoot(t *testing.T) {
	src := "proc alpha {} {}\nitcl::class ::Widget {}\nproc omega {} {}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	got := childNames(syms)
	want := []string{"alpha", "::Widget", "omega"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root order = %v, want %v (source order)", got, want)
	}
}

// A nested namespace declared between two procs sorts BETWEEN them at root, not
// after both (sub-namespaces interleave with members by position).
func TestBuildDocumentSymbolsNamespaceInterleavedAtRoot(t *testing.T) {
	src := "proc a {} {}\nnamespace eval ::sub {\n  proc x {} {}\n}\nproc b {} {}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	got := childNames(syms)
	want := []string{"a", "::sub", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root order = %v, want %v (namespace interleaved by position)", got, want)
	}
}

// Same, one level deeper: a child namespace interleaves with members inside a
// namespace node.
func TestBuildDocumentSymbolsNamespaceInterleavedNested(t *testing.T) {
	src := "namespace eval ::app {\n  proc a {} {}\n  namespace eval ::app::inner {\n    proc x {} {}\n  }\n  proc b {} {}\n}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	app := findSym(syms, "::app")
	if app == nil {
		t.Fatalf("::app missing: %#v", syms)
	}
	got := childNames(app.Children)
	want := []string{"a", "::app::inner", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("::app child order = %v, want %v (child namespace interleaved)", got, want)
	}
}

// A namespace reopened in two blocks collapses to ONE node holding both members
// (no duplicate node, no dropped members). Its position follows its first block,
// so a top-level symbol textually between the blocks may sort after the whole
// node -- an accepted limit of one-node-per-namespace. This pins the invariants
// that matter (single node, both members, nothing dropped).
func TestBuildDocumentSymbolsReopenedNamespace(t *testing.T) {
	src := "namespace eval ::app { proc a {} {} }\nproc mid {} {}\nnamespace eval ::app { proc b {} {} }"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	count := 0
	for _, s := range syms {
		if s.Name == "::app" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("::app should appear exactly once, got %d: %#v", count, syms)
	}
	app := findSym(syms, "::app")
	if findChild(app, "a") == nil || findChild(app, "b") == nil {
		t.Fatalf("::app must hold both a and b: %#v", app.Children)
	}
	if findSym(syms, "mid") == nil {
		t.Fatalf("top-level 'mid' dropped: %#v", syms)
	}
}

// Same, one level down: members of a namespace node keep file order.
func TestBuildDocumentSymbolsSourceOrderInNamespace(t *testing.T) {
	src := "namespace eval ::app {\n  proc a {} {}\n  itcl::class C {}\n  proc b {} {}\n}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	app := findSym(syms, "::app")
	if app == nil {
		t.Fatalf("::app namespace node missing: %#v", syms)
	}
	got := childNames(app.Children)
	want := []string{"a", "C", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("::app child order = %v, want %v (source order)", got, want)
	}
}

// posLE reports whether position a is less than or equal to position b.
func posLE(a, b Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character <= b.Character)
}

func TestBuildDocumentSymbolsFlat(t *testing.T) {
	src := "proc render {} {}\nitcl::class ::C {}"
	defs := tcl.FileDefs(src)
	syms := buildDocumentSymbols(defs, src, false)
	// flatten for lookup: global symbols are at root
	byName := map[string]DocumentSymbol{}
	for _, s := range syms {
		byName[s.Name] = s
	}
	if byName["render"].Kind != SymKindFunction {
		t.Fatalf("render kind = %d", byName["render"].Kind)
	}
	if byName["::C"].Kind != SymKindClass {
		t.Fatalf("::C kind = %d", byName["::C"].Kind)
	}
	// range must contain selectionRange
	r := byName["render"]
	if !posLE(r.Range.Start, r.SelectionRange.Start) || !posLE(r.SelectionRange.End, r.Range.End) {
		t.Fatalf("range must contain selectionRange: %#v", r)
	}
}

func TestBuildDocumentSymbolsNested(t *testing.T) {
	src := "namespace eval ::app {\n  proc helper {} {}\n}\nitcl::class ::Disp {\n  method field {} {}\n  variable count 0\n}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	app := findSym(syms, "::app")
	if app == nil || app.Kind != SymKindNamespace || findChild(app, "helper") == nil {
		t.Fatalf("::app namespace node with helper child missing: %#v", syms)
	}
	disp := findSym(syms, "::Disp")
	if disp == nil || disp.Kind != SymKindClass {
		t.Fatalf("::Disp class node missing: %#v", syms)
	}
	if findChild(disp, "field") == nil || findChild(disp, "count") == nil {
		t.Fatalf("::Disp method/ivar children missing: %#v", disp.Children)
	}
}

func findSym(syms []DocumentSymbol, name string) *DocumentSymbol {
	for i := range syms {
		if syms[i].Name == name {
			return &syms[i]
		}
		if got := findSym(syms[i].Children, name); got != nil {
			return got
		}
	}
	return nil
}

func findChild(s *DocumentSymbol, name string) *DocumentSymbol {
	for i := range s.Children {
		if s.Children[i].Name == name {
			return &s.Children[i]
		}
	}
	return nil
}

func TestBuildDocumentSymbolsRVTHoist(t *testing.T) {
	// Defs as produced for a .rvt page live in the ::request namespace.
	defs := source.Defs("page.rvt", "<? proc render {} {} ?>")
	syms := buildDocumentSymbols(defs, "<? proc render {} {} ?>", true)
	if findSym(syms, "render") == nil {
		t.Fatalf("render should be hoisted to root: %#v", syms)
	}
	// the ::request wrapper node must not appear at root
	for _, s := range syms {
		if s.Name == "::request" {
			t.Fatalf("::request wrapper should be elided, got %#v", syms)
		}
	}
}

func TestBuildDocumentSymbolsGlobalNamesSimple(t *testing.T) {
	// Members (procs, namespace vars) at global scope display their simple
	// name; a class declared as ::C keeps its qualified form.
	src := "proc render {} {}\nset gvar 0\nitcl::class ::C {}"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	if findSym(syms, "render") == nil {
		t.Fatalf("global proc should display as 'render': %#v", syms)
	}
	if findSym(syms, "gvar") == nil {
		t.Fatalf("global namespace var should display as 'gvar': %#v", syms)
	}
	if findSym(syms, "::C") == nil {
		t.Fatalf("global class should keep its declared form '::C': %#v", syms)
	}
}

func TestBuildDocumentSymbolsDeepNesting(t *testing.T) {
	src := "namespace eval ::a { namespace eval ::a::b { namespace eval ::a::b::c { proc deep {} {} } } }"
	syms := buildDocumentSymbols(tcl.FileDefs(src), src, false)
	d := findSym(syms, "deep")
	if d == nil {
		t.Fatalf("deeply nested proc 'deep' was dropped: %#v", syms)
	}
	a := findSym(syms, "::a")
	if a == nil {
		t.Fatalf("::a namespace node missing: %#v", syms)
	}
	// intermediate ancestor must have a real (non-empty) range
	if a.Range == (Range{}) {
		t.Fatalf("::a namespace node has empty range: %#v", a)
	}
}

// buildWorkspaceSymbols filters many entries in parallel; the result must be
// order-preserving and match only the query, identical to a sequential filter.
// Run under -race, it also guards the concurrent sourceOf reads.
func TestBuildWorkspaceSymbolsParallelParity(t *testing.T) {
	sourceOf := func(string) string { return "proc x {} {}\n" }

	var entries []index.SymbolEntry
	for i := 0; i < 500; i++ {
		name := "other"
		if i%3 == 0 {
			name = "match"
		}
		entries = append(entries, index.SymbolEntry{
			Name: name, Kind: tcl.DefProc, File: "f.tcl", NameStart: 5, NameEnd: 6,
		})
	}

	got := buildWorkspaceSymbols(entries, "match", sourceOf)

	want := 0
	for _, e := range entries {
		if strings.Contains(e.Name, "match") {
			want++
		}
	}
	if len(got) != want {
		t.Fatalf("got %d symbols, want %d matches", len(got), want)
	}
	for _, si := range got {
		if si.Name != "match" {
			t.Fatalf("non-matching symbol leaked through: %q", si.Name)
		}
	}
}

func TestSymbolKind(t *testing.T) {
	tests := []struct {
		kind   tcl.DefKind
		want   SymbolKind
		wantOK bool
	}{
		{tcl.DefProc, SymKindFunction, true},
		{tcl.DefMethod, SymKindMethod, true},
		{tcl.DefIvar, SymKindField, true},
		{tcl.DefClass, SymKindClass, true},
		{tcl.DefNamespaceVar, SymKindVariable, true},
		{tcl.DefLocal, 0, false},
		{tcl.DefGlobalLink, 0, false},
	}
	for _, tc := range tests {
		got, ok := symbolKind(tc.kind)
		if ok != tc.wantOK {
			t.Errorf("symbolKind(%d): ok = %v, want %v", tc.kind, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Errorf("symbolKind(%d): kind = %d, want %d", tc.kind, got, tc.want)
		}
	}
}
