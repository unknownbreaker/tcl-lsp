package index

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

func TestIndexLookupProcAndVar(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app {\n  variable count 0\n  proc run {} {}\n}")

	run := ix.Lookup("::app::run")
	if len(run) != 1 || run[0].Kind != tcl.DefProc || run[0].File != "a.tcl" {
		t.Fatalf("::app::run lookup = %#v", run)
	}
	count := ix.Lookup("::app::count")
	if len(count) != 1 || count[0].Kind != tcl.DefNamespaceVar {
		t.Fatalf("::app::count lookup = %#v", count)
	}
}

func TestIndexSkipsLocals(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "proc f {a} { set b 1 }")
	if locs := ix.Lookup("a"); len(locs) != 0 {
		t.Fatalf("param local should not be indexed: %#v", locs)
	}
	if locs := ix.Lookup("b"); len(locs) != 0 {
		t.Fatalf("set local should not be indexed: %#v", locs)
	}
	if locs := ix.Lookup("::f"); len(locs) != 1 {
		t.Fatalf("::f proc should be indexed: %#v", locs)
	}
}

func TestIndexLookupMissing(t *testing.T) {
	ix := New()
	if locs := ix.Lookup("::nope"); locs != nil {
		t.Fatalf("missing lookup should be nil, got %#v", locs)
	}
}

func TestIndexMultipleFilesSameName(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "proc dup {} {}")
	ix.IndexFile("b.tcl", "proc dup {} {}")
	if locs := ix.Lookup("::dup"); len(locs) != 2 {
		t.Fatalf("expected 2 def sites for ::dup, got %#v", locs)
	}
}

func TestIndexReindexReplaces(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "proc old {} {}")
	ix.IndexFile("a.tcl", "proc new {} {}") // re-index the same path
	if locs := ix.Lookup("::old"); len(locs) != 0 {
		t.Fatalf("old def should be gone after re-index: %#v", locs)
	}
	if locs := ix.Lookup("::new"); len(locs) != 1 {
		t.Fatalf("new def should be present: %#v", locs)
	}
}

func TestIndexRemoveFile(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "proc dup {} {}")
	ix.IndexFile("b.tcl", "proc dup {} {}")
	ix.RemoveFile("a.tcl")
	locs := ix.Lookup("::dup")
	if len(locs) != 1 || locs[0].File != "b.tcl" {
		t.Fatalf("after removing a.tcl, expected only b.tcl: %#v", locs)
	}
	// fully removing the last definer deletes the key
	ix.RemoveFile("b.tcl")
	if locs := ix.Lookup("::dup"); locs != nil {
		t.Fatalf("expected nil after all definers removed, got %#v", locs)
	}
}

func TestIndexFilesAndSource(t *testing.T) {
	ix := New()
	ix.IndexFile("b.tcl", "proc b {} {}")
	ix.IndexFile("a.tcl", "proc a {} {}")

	if files := ix.Files(); !reflect.DeepEqual(files, []string{"a.tcl", "b.tcl"}) {
		t.Fatalf("Files() = %#v, want sorted [a.tcl b.tcl]", files)
	}
	if got := ix.Source("a.tcl"); got != "proc a {} {}" {
		t.Fatalf("Source(a.tcl) = %q", got)
	}
	ix.RemoveFile("a.tcl")
	if got := ix.Source("a.tcl"); got != "" {
		t.Fatalf("Source after remove = %q, want empty", got)
	}
	if files := ix.Files(); !reflect.DeepEqual(files, []string{"b.tcl"}) {
		t.Fatalf("Files() after remove = %#v", files)
	}
}

func TestIndexDirMissing(t *testing.T) {
	ix := New()
	if err := ix.IndexDir("/nonexistent/path/for/test"); err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

func TestIndexParseGapStillIndexes(t *testing.T) {
	// A later parse gap must not prevent indexing the earlier, valid proc.
	ix := New()
	ix.IndexFile("a.tcl", "proc ok {} {}\nproc bad {oops")
	if locs := ix.Lookup("::ok"); len(locs) != 1 {
		t.Fatalf("expected ::ok indexed despite later parse gap: %#v", locs)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "x.tcl", "proc x {} {}")
	writeFile(t, dir, "sub/y.tcl", "namespace eval ::n { proc y {} {} }")
	writeFile(t, dir, "readme.md", "not tcl")

	ix := New()
	if err := ix.IndexDir(dir); err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}
	if locs := ix.Lookup("::x"); len(locs) != 1 {
		t.Fatalf("::x not indexed from dir: %#v", locs)
	}
	if locs := ix.Lookup("::n::y"); len(locs) != 1 {
		t.Fatalf("::n::y not indexed from subdir: %#v", locs)
	}
	if files := ix.Files(); len(files) != 2 {
		t.Fatalf("expected 2 .tcl files indexed (md skipped), got %#v", files)
	}
}

func TestIndexDirProgress(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "proc a {} {}")
	writeFile(t, dir, "b.rvt", "<? proc b {} {} ?>")
	writeFile(t, dir, "readme.md", "not tcl")

	var counts []int
	ix := New()
	if err := ix.IndexDirProgress(dir, func(n int) { counts = append(counts, n) }); err != nil {
		t.Fatalf("IndexDirProgress error: %v", err)
	}
	// One callback per indexed file (the .md is skipped, so no callback), with a
	// monotonic running count ending at the file total.
	if len(counts) != 2 || counts[len(counts)-1] != 2 {
		t.Fatalf("progress callbacks = %v, want a running count ending at 2", counts)
	}
	for i, n := range counts {
		if n != i+1 {
			t.Fatalf("running count not monotonic from 1: %v", counts)
		}
	}
}

// Stress the parallel worker pool with many files and confirm every symbol is
// indexed and the progress count reaches the total. Run under -race, this is the
// main guard against a data race in the parallel parse.
func TestIndexDirProgressManyFiles(t *testing.T) {
	dir := t.TempDir()
	const n = 200
	for i := 0; i < n; i++ {
		writeFile(t, dir, fmt.Sprintf("f%03d.tcl", i), fmt.Sprintf("proc p%03d {} {}", i))
	}

	var last int
	ix := New()
	if err := ix.IndexDirProgress(dir, func(c int) { last = c }); err != nil {
		t.Fatalf("IndexDirProgress error: %v", err)
	}
	if last != n {
		t.Fatalf("progress reached %d, want %d", last, n)
	}
	for i := 0; i < n; i++ {
		if locs := ix.Lookup(fmt.Sprintf("::p%03d", i)); len(locs) != 1 {
			t.Fatalf("::p%03d not indexed exactly once: %#v", i, locs)
		}
	}
}

// The parallel store happens in walk (lexical) order, so a name defined in
// several files resolves to those files in a deterministic order -- identical to
// the old sequential index.
func TestIndexDirProgressDeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.tcl", "proc dup {} {}")
	writeFile(t, dir, "b.tcl", "proc dup {} {}")
	writeFile(t, dir, "c.tcl", "proc dup {} {}")

	ix := New()
	if err := ix.IndexDirProgress(dir, nil); err != nil {
		t.Fatalf("IndexDirProgress error: %v", err)
	}
	locs := ix.Lookup("::dup")
	var bases []string
	for _, l := range locs {
		bases = append(bases, filepath.Base(l.File))
	}
	if !reflect.DeepEqual(bases, []string{"a.tcl", "b.tcl", "c.tcl"}) {
		t.Fatalf("def sites = %v, want lexical [a b c].tcl (walk-order deterministic)", bases)
	}
}

func TestIndexDirContinuesPastUnreadableFile(t *testing.T) {
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
	defer os.Chmod(bad, 0o644) // restore so TempDir cleanup succeeds

	ix := New()
	err := ix.IndexDir(dir)
	if err == nil {
		t.Fatal("expected an aggregated error for the unreadable file")
	}
	// The readable file is still indexed despite the bad one (no abort).
	if locs := ix.Lookup("::good"); len(locs) != 1 {
		t.Fatalf("readable file should still be indexed, got %#v", locs)
	}
}

func TestIndexDirSkipsGit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "real.tcl", "proc real {} {}")
	writeFile(t, dir, ".git/hooks/sneaky.tcl", "proc sneaky {} {}")

	ix := New()
	if err := ix.IndexDir(dir); err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}
	if locs := ix.Lookup("::real"); len(locs) != 1 {
		t.Fatalf("::real should be indexed: %#v", locs)
	}
	if locs := ix.Lookup("::sneaky"); locs != nil {
		t.Fatalf("files under .git must be skipped, got %#v", locs)
	}
}

func TestIndexStoresFileRefs(t *testing.T) {
	// References are precomputed at index time so a references request does not
	// re-parse every workspace file. FileRefs returns the stored refs; RemoveFile
	// clears them.
	ix := New()
	ix.IndexFile("a.tcl", "greet\ngreet")
	refs := ix.FileRefs("a.tcl")
	if len(refs) < 2 {
		t.Fatalf("expected >=2 stored command refs for a.tcl, got %#v", refs)
	}
	ix.RemoveFile("a.tcl")
	if refs := ix.FileRefs("a.tcl"); refs != nil {
		t.Fatalf("expected nil refs after RemoveFile, got %#v", refs)
	}
}

func TestIndexNamespacePathAndImports(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app {\n  namespace path ::lib\n  namespace import ::p::pub\n}")
	path, imports := ix.Namespace("::app")
	if !reflect.DeepEqual(path, []string{"::lib"}) {
		t.Fatalf("path = %#v, want [::lib]", path)
	}
	if !reflect.DeepEqual(imports, []string{"::p::pub"}) {
		t.Fatalf("imports = %#v, want [::p::pub]", imports)
	}
}

func TestIndexNamespaceMergedAcrossFiles(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app { namespace import ::p::a }")
	ix.IndexFile("b.tcl", "namespace eval ::app { namespace import ::q::b }")
	_, imports := ix.Namespace("::app")
	if !reflect.DeepEqual(imports, []string{"::p::a", "::q::b"}) {
		t.Fatalf("imports = %#v, want union sorted by file", imports)
	}
}

func TestIndexNamespaceCacheInvalidates(t *testing.T) {
	// Namespace memoizes its merged result; a mutation must invalidate it so a
	// later read reflects the change rather than a stale cached value.
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app { namespace import ::p::a }")
	if _, imports := ix.Namespace("::app"); !reflect.DeepEqual(imports, []string{"::p::a"}) {
		t.Fatalf("initial imports = %#v", imports) // primes the memo
	}
	// A second file adds another import to the same namespace.
	ix.IndexFile("b.tcl", "namespace eval ::app { namespace import ::q::b }")
	if _, imports := ix.Namespace("::app"); !reflect.DeepEqual(imports, []string{"::p::a", "::q::b"}) {
		t.Fatalf("after add, imports = %#v, want merged set", imports)
	}
	// Removing a file must also refresh the cache.
	ix.RemoveFile("b.tcl")
	if _, imports := ix.Namespace("::app"); !reflect.DeepEqual(imports, []string{"::p::a"}) {
		t.Fatalf("after remove, imports = %#v, want stale entry gone", imports)
	}
}

func TestIndexNamespaceClearedWithFile(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app { namespace path ::lib }")
	ix.RemoveFile("a.tcl")
	if path, _ := ix.Namespace("::app"); path != nil {
		t.Fatalf("namespace info should be gone after RemoveFile: %#v", path)
	}
}

func TestIndexDirIncludesRVT(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.rvt"), []byte("<? proc onlyinrvt {} {} ?>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.tcl"), []byte("namespace eval ::lib { proc helper {} {} }"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := New()
	if err := ix.IndexDir(dir); err != nil {
		t.Fatal(err)
	}
	if len(ix.Lookup("::request::onlyinrvt")) != 1 {
		t.Fatalf("IndexDir did not index the .rvt file")
	}
	if len(ix.Lookup("::lib::helper")) != 1 {
		t.Fatalf("IndexDir regressed .tcl indexing")
	}
}

func TestIndexFileRVTStoresRequestSymbol(t *testing.T) {
	ix := New()
	src := "<h1><? proc greet {} {} ?></h1>"
	ix.IndexFile("page.rvt", src)

	locs := ix.Lookup("::request::greet")
	if len(locs) != 1 {
		t.Fatalf("expected 1 def for ::request::greet, got %#v", locs)
	}
	if locs[0].File != "page.rvt" {
		t.Fatalf("file = %q, want page.rvt", locs[0].File)
	}
	if want := strings.Index(src, "greet"); locs[0].NameStart != want {
		t.Fatalf("NameStart = %d, want %d (.rvt coord)", locs[0].NameStart, want)
	}
}

func TestIndexClassLookup(t *testing.T) {
	ix := New()
	ix.IndexFile("disp.tcl", "itcl::class ::STDisplay {\n  method field {} {}\n}")
	locs := ix.Lookup("::STDisplay")
	if len(locs) != 1 || locs[0].Kind != tcl.DefClass || locs[0].File != "disp.tcl" {
		t.Fatalf("want DefClass ::STDisplay indexed, got %#v", locs)
	}
}

func TestIndexClassTable(t *testing.T) {
	ix := New()
	ix.IndexFile("c.tcl",
		"itcl::class ::Base { method common {} {} }\n"+
			"itcl::class ::Derived {\n  inherit ::Base\n  method field {} {}\n}")
	ci := ix.Class("::Derived")
	if ci == nil {
		t.Fatal("::Derived not in class table")
	}
	if len(ci.Methods["field"]) != 1 {
		t.Fatalf("Derived.field method site missing: %#v", ci.Methods)
	}
	if len(ci.Inherit) != 1 || ci.Inherit[0] != "::Base" {
		t.Fatalf("Derived inherit = %#v, want [::Base]", ci.Inherit)
	}
}

func TestIndexAllSymbols(t *testing.T) {
	ix := New()
	ix.IndexFile("a.tcl", "namespace eval ::app { proc run {} {} }\nitcl::class ::C { method field {} {} }")
	var names = map[string]SymbolEntry{}
	for _, e := range ix.AllSymbols() {
		names[e.Name] = e
	}
	if e, ok := names["run"]; !ok || e.Container != "::app" {
		t.Fatalf("run symbol = %#v", names)
	}
	if e, ok := names["field"]; !ok || e.Kind != tcl.DefMethod || e.Container != "::C" {
		t.Fatalf("field method symbol = %#v", names)
	}
}
