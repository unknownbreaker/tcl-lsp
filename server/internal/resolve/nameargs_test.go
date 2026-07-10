package resolve

// End-to-end: variable-NAME arguments of info exists / unset / array
// subcommands resolve and are counted as references. Regression for
// `if {[info exists ::something($stuff)]}` being invisible to goto-def and
// find-references.

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

func TestNameArgGotoDefAndReferences(t *testing.T) {
	def := "set ::something(a) 1\n"
	use := "proc check {stuff} {\n  if {[info exists ::something($stuff)]} {\n    puts yes\n  }\n}\n"
	ix := index.New()
	ix.IndexFile("def.tcl", def)
	ix.IndexFile("use.tcl", use)
	r := New(ix)

	// goto-def with the cursor on the base name inside the info-exists guard.
	locs := r.Definition("use.tcl", use, strings.Index(use, "::something"))
	found := false
	for _, l := range locs {
		if l.File == "def.tcl" {
			found = true
		}
	}
	if !found {
		t.Errorf("gd on info-exists name arg found no definition: %#v", locs)
	}

	// find-references from the definition lists the guard site.
	refs := r.References("def.tcl", def, strings.Index(def, "::something"))
	found = false
	for _, l := range refs {
		if l.File == "use.tcl" {
			found = true
		}
	}
	if !found {
		t.Errorf("grr from def missed the info-exists guard site: %#v", refs)
	}
}

// A BARE name argument follows TCL frame semantics: at top level it resolves to
// the global; inside a proc it stays local (never-wrong), chasing a `global`
// link when one is present.
func TestNameArgFrameSemantics(t *testing.T) {
	def := "set gvar 1\n"
	ix := index.New()
	ix.IndexFile("def.tcl", def)

	// Top level: bare name resolves to ::gvar.
	topUse := "if {[info exists gvar]} {}\n"
	ix.IndexFile("top.tcl", topUse)
	r := New(ix)
	locs := r.Definition("top.tcl", topUse, strings.Index(topUse, "gvar"))
	ok := false
	for _, l := range locs {
		if l.File == "def.tcl" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("top-level bare info-exists name should resolve to global: %#v", locs)
	}

	// In a proc with a global link: chases to the global.
	linkedUse := "proc p {} {\n  global gvar\n  if {[info exists gvar]} {}\n}\n"
	ix.IndexFile("linked.tcl", linkedUse)
	locs = r.Definition("linked.tcl", linkedUse, strings.Index(linkedUse, "gvar]"))
	ok = false
	for _, l := range locs {
		if l.File == "def.tcl" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("linked in-proc info-exists name should chase to global: %#v", locs)
	}

	// In a proc WITHOUT a link: stays local -- must NOT resolve to the global.
	bareUse := "proc p {} {\n  set gvar 0\n  if {[info exists gvar]} {}\n}\n"
	ix.IndexFile("bare.tcl", bareUse)
	locs = r.Definition("bare.tcl", bareUse, strings.Index(bareUse, "gvar]"))
	for _, l := range locs {
		if l.File == "def.tcl" {
			t.Errorf("bare in-proc info-exists name wrongly resolved to global: %#v", locs)
		}
	}
}
