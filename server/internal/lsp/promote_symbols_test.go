package lsp

import (
	"reflect"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// A promoted definition's NameStart EQUALS its DefLocal's NameStart. Mixing
// top-level procs, an init proc with global+set (which injects a promoted
// ::cfg def at the position of the `set` inside the proc body -- i.e. deep
// inside a nested position, not adjacent to top-level siblings in the
// flattened list order) with a class must not desync the outline: no
// duplicate/misplaced root symbols, and the promoted var must NOT leak into
// the proc's own symbol (DefNamespaceVar with Class=="" is skipped only when
// d.Class != "", so this also checks that the proc's local `set` doesn't
// itself become a rogue root-level "cfg" symbol via the DefLocal path, which
// buildDocumentSymbols must ignore).
func TestBuildDocumentSymbols_PromotedGlobalAmongProcsAndClass(t *testing.T) {
	src := "proc alpha {} {}\n" +
		"itcl::class ::Widget {}\n" +
		"proc init {} {\n" +
		"  global cfg\n" +
		"  set cfg 1\n" +
		"}\n" +
		"proc omega {} {}\n"
	defs := tcl.FileDefs(src)
	syms := buildDocumentSymbols(defs, src, false)
	got := childNames(syms)
	want := []string{"alpha", "::Widget", "init", "cfg", "omega"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root order = %v, want %v", got, want)
	}
}
