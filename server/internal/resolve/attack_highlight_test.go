package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// Before the localAt fix, a QUALIFIED $::x inside a proc was claimed as a
// proc-local ref (RefVariable + FrameProc, matched by bare name comparison
// elsewhere) which risked document-highlight conflating it with an unrelated
// BARE local `x` in the same proc (same scope, coincidentally overlapping
// name once the leading "::" is stripped by any bare-name compare). Verify
// FileHighlights on $::x does NOT include the bare local `x` binding/uses, and
// FileHighlights on the bare local `x` does NOT include the $::x use.
func TestFileHighlights_QualifiedVarDoesNotConflateWithBareLocal(t *testing.T) {
	src := "set topvar 100\n" +
		"proc p {} {\n" +
		"  set x 1\n" + // bare local x: binding
		"  puts $x\n" + // bare local x: use
		"  puts $::x\n" + // qualified: must NOT be grouped with the local
		"}\n"
	ix := index.New()
	ix.IndexFile("f.tcl", src)
	r := New(ix)

	localBindOff := strings.Index(src, "set x 1") + len("set ")
	bareUseOff := strings.Index(src, "$x") + 1

	qualOff := strings.Index(src, "$::x") + 1
	qualHi := r.FileHighlights("f.tcl", src, qualOff)
	for _, l := range qualHi {
		if l.NameStart == localBindOff || l.NameStart == bareUseOff {
			t.Errorf("BUG: highlighting $::x included the unrelated bare local `x` (binding or use): %#v", qualHi)
		}
	}

	bareHi := r.FileHighlights("f.tcl", src, bareUseOff)
	for _, l := range bareHi {
		if l.NameStart == strings.Index(src, "$::x")+1 {
			t.Errorf("BUG: highlighting bare local `x` included the unrelated $::x qualified use: %#v", bareHi)
		}
	}
	// The bare local's own highlight set must still contain its binding+use pair.
	if len(bareHi) != 2 {
		t.Errorf("bare local `x` highlight set = %#v, want exactly 2 (binding + use)", bareHi)
	}
}
