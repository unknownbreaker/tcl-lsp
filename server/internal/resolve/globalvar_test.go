package resolve

// Cross-file resolution of GLOBAL (and namespace) variables, across the
// definition shapes real TCL code uses. Regression tests for a user report:
// goto-def found nothing for globals defined via `array set`, via
// `global x; set x ...` inside an init proc, or used as `$::x` inside a proc.

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// resolveUse indexes defSrc (def.tcl) and useSrc (use.tcl), then resolves the
// use at the first occurrence of atToken in useSrc.
func resolveUse(t *testing.T, defSrc, useSrc, atToken string) []index.Location {
	t.Helper()
	ix := index.New()
	ix.IndexFile("def.tcl", defSrc)
	ix.IndexFile("use.tcl", useSrc)
	r := New(ix)
	off := strings.Index(useSrc, atToken)
	if off < 0 {
		t.Fatalf("token %q not in use source", atToken)
	}
	return r.Definition("use.tcl", useSrc, off)
}

// requireDefInFile asserts at least one location lands in def.tcl.
func requireDefInFile(t *testing.T, locs []index.Location, shape string) {
	t.Helper()
	for _, l := range locs {
		if l.File == "def.tcl" {
			return
		}
	}
	t.Errorf("%s: no definition found in def.tcl; got %#v", shape, locs)
}

// Every definition SHAPE below must be found from a top-level `$::gvar` use.
func TestGlobalVarDefShapes(t *testing.T) {
	use := "puts $::gvar\n"
	shapes := []struct {
		name   string
		defSrc string
	}{
		{"top-level set", "set gvar 1\n"},
		{"top-level qualified set", "set ::gvar 1\n"},
		{"namespace eval :: set", "namespace eval :: {\n  set gvar 1\n}\n"},
		{"top-level array set", "array set gvar {a 1}\n"},
		{"top-level qualified array set", "array set ::gvar {a 1}\n"},
		{"global+set inside proc", "proc init {} {\n  global gvar\n  set gvar 1\n}\n"},
		{"global+array-element set inside proc", "proc init {} {\n  global gvar\n  set gvar(k) 1\n}\n"},
		{"global+array set inside proc", "proc init {} {\n  global gvar\n  array set gvar {a 1}\n}\n"},
		{"global+lappend inside proc", "proc init {} {\n  global gvar\n  lappend gvar x\n}\n"},
		{"upvar #0 alias set inside proc", "proc init {} {\n  upvar #0 gvar g\n  set g 1\n}\n"},
		{"qualified set inside proc", "proc init {} {\n  set ::gvar 1\n}\n"},
	}
	for _, c := range shapes {
		t.Run(c.name, func(t *testing.T) {
			requireDefInFile(t, resolveUse(t, c.defSrc, use, "::gvar"), c.name)
		})
	}
}

// Every USE SITE below must find a plain top-level definition.
func TestGlobalVarUseSites(t *testing.T) {
	def := "set gvar 1\n"
	uses := []struct {
		name    string
		useSrc  string
		atToken string
	}{
		{"qualified at top level", "puts $::gvar\n", "::gvar"},
		{"bare at top level", "puts $gvar\n", "gvar"},
		{"qualified INSIDE a proc", "proc show {} {\n  puts $::gvar\n}\n", "::gvar"},
		{"bare inside proc with global", "proc show {} {\n  global gvar\n  puts $gvar\n}\n", "$gvar"},
	}
	for _, c := range uses {
		t.Run(c.name, func(t *testing.T) {
			requireDefInFile(t, resolveUse(t, def, c.useSrc, c.atToken), c.name)
		})
	}
}

// Namespace variables get the same treatment via qualified names.
func TestNamespaceVarQualifiedFromProc(t *testing.T) {
	def := "namespace eval ::app {\n  variable avar 1\n}\n"
	use := "proc show {} {\n  puts $::app::avar\n}\n"
	requireDefInFile(t, resolveUse(t, def, use, "::app::avar"), "ns var from proc")
}

// The never-wrong boundary is preserved: a BARE $gvar inside a proc without a
// `global` link is a proc-local in TCL -- it must NOT resolve to the global.
func TestBareProcVarStaysLocal(t *testing.T) {
	def := "set gvar 1\n"
	use := "proc show {} {\n  set gvar 0\n  puts $gvar\n}\n"
	locs := resolveUse(t, def, use, "$gvar")
	for _, l := range locs {
		if l.File == "def.tcl" {
			t.Fatalf("bare in-proc $gvar wrongly resolved to the global: %#v", locs)
		}
	}
}

// The promoted definition keeps proc-local machinery intact: inside the init
// proc itself, $gvar still resolves locally (to the workspace def via the
// link-chase, or the local binding) -- and document-symbol/reference plumbing
// sees the promoted ::gvar def without disturbing local scope resolution.
func TestPromotionKeepsLocalBinding(t *testing.T) {
	src := "proc init {} {\n  global gvar\n  set gvar 1\n  puts $gvar\n}\n"
	ix := index.New()
	ix.IndexFile("def.tcl", src)
	r := New(ix)
	off := strings.Index(src, "$gvar")
	locs := r.Definition("def.tcl", src, off)
	if len(locs) == 0 {
		t.Fatalf("in-proc $gvar after global+set resolved to nothing")
	}
}
