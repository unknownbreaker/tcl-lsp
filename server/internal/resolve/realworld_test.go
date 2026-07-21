package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// refsFromDef indexes the given files, runs References from the proc/class
// definition named `sym` in defFile, and reports whether the .rvt page is among
// the results. files is a map of path -> content.
func refsFromDef(t *testing.T, files map[string]string, defFile, sym, wantRefFile string) (found bool, all []index.Location) {
	t.Helper()
	ix := index.New()
	for p, c := range files {
		ix.IndexFile(p, c)
	}
	r := New(ix)
	defSrc := files[defFile]
	defOff := strings.Index(defSrc, sym)
	all = r.References(defFile, defSrc, defOff)
	for _, l := range all {
		if l.File == wantRefFile {
			found = true
		}
	}
	return found, all
}

// Fixture A — namespaced+exported proc, absolute-qualified bracketed call (rivetweb, MOST common).
func TestRealNamespacedAbsoluteBracketed(t *testing.T) {
	files := map[string]string{
		"paths.tcl":  "namespace eval ::rivetweb {\n  proc makeCssPath {css_file {style \"\"}} { return \"/css/$style/$css_file\" }\n  namespace export makeCssPath\n}",
		"page_a.rvt": "<link href=\"<?= [::rivetweb::makeCssPath site.css] ?>\" />",
	}
	found, all := refsFromDef(t, files, "paths.tcl", "makeCssPath", "page_a.rvt")
	t.Logf("A refs: %#v", all)
	if !found {
		t.Errorf("A FAIL: absolute-qualified bracketed call not found")
	}
}

// Fixture B — namespaced proc, absolute-qualified bare call in statement position.
func TestRealNamespacedAbsoluteBare(t *testing.T) {
	files := map[string]string{
		"content.tcl": "namespace eval ::rivetweb {\n  proc contentType {} { return \"text/html\" }\n}",
		"page_b.rvt":  "<? puts [::rivetweb::contentType] ?>",
	}
	found, all := refsFromDef(t, files, "content.tcl", "contentType", "page_b.rvt")
	t.Logf("B refs: %#v", all)
	if !found {
		t.Errorf("B FAIL: absolute-qualified call not found")
	}
}

// Fixture C — package require + namespaced bracket call inside namespace eval ::demo.
func TestRealPackageRequireNamespacedCall(t *testing.T) {
	files := map[string]string{
		"st_client.tcl": "namespace eval ::stapi {\n  proc connect {uri args} { return [list connected $uri] }\n  namespace export connect\n}",
		"page_c.rvt":    "<?\npackage require st_client\nnamespace eval ::demo {\n  set table [::stapi::connect $::demo::uri -key disk]\n}\n?>",
	}
	found, all := refsFromDef(t, files, "st_client.tcl", "connect", "page_c.rvt")
	t.Logf("C refs: %#v", all)
	if !found {
		t.Errorf("C FAIL: ::stapi::connect call inside namespace eval ::demo not found")
	}
}

// Fixture D — plain global proc, bare unqualified call.
func TestRealGlobalBareCall(t *testing.T) {
	files := map[string]string{
		"disks_lib.tcl": "proc load_disks {ctable} {\n  $ctable set host [info hostname]\n}",
		"page_d.rvt":    "<?\npackage require disks_lib\nload_disks ::demo::table\n?>",
	}
	found, all := refsFromDef(t, files, "disks_lib.tcl", "load_disks", "page_d.rvt")
	t.Logf("D refs: %#v", all)
	if !found {
		t.Errorf("D FAIL: global bare call not found")
	}
}

// Fixture E — inline proc defined and called in the same .rvt.
func TestRealInlineProcSamePage(t *testing.T) {
	page := "<?\nproc failproc {arg1} { return $arg1 }\n::rivet::try {\n  failproc [::rivet::var_qs get cond]\n}\n?>"
	ix := index.New()
	ix.IndexFile("page_e.rvt", page)
	r := New(ix)
	defOff := strings.Index(page, "failproc") // the definition
	all := r.References("page_e.rvt", page, defOff)
	t.Logf("E refs: %#v", all)
	// References returns invocation sites (the declaration is added by the server
	// via includeDeclaration). The page-local call must be among them.
	callOff := strings.LastIndex(page, "failproc")
	var foundCall bool
	for _, l := range all {
		if l.File == "page_e.rvt" && l.NameStart == callOff {
			foundCall = true
		}
	}
	if !foundCall {
		t.Errorf("E FAIL: inline proc call site not found, got %#v", all)
	}
}

// Fixture H — absolute-qualified variable reads across namespaces.
func TestRealNamespacedVarRead(t *testing.T) {
	files := map[string]string{
		"state.tcl":  "namespace eval ::rivetweb {\n  variable page_title \"Home\"\n}",
		"page_h.rvt": "<title><? puts -nonewline $::rivetweb::page_title ?></title>",
	}
	found, all := refsFromDef(t, files, "state.tcl", "page_title", "page_h.rvt")
	t.Logf("H refs: %#v", all)
	if !found {
		t.Errorf("H FAIL: absolute-qualified var read not found")
	}
}
