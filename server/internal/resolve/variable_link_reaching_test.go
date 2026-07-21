package resolve

// Regression: `variable cfg` inside a proc LINKS the local name to the
// namespace variable, so goto-def on a later $cfg must chase through to the
// namespace variable's definition -- not stop at the `variable` line inside
// the proc itself. The defs path (defs.go) carried the Origin for this link;
// the reaching-defs path (reaching.go localBindings) had its own copy of the
// binding rules and silently dropped it, producing a WRONG jump (to the line
// above the use) instead of a silent absence.

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// Same file: the $cfg use must resolve to the namespace-level declaration,
// outside the proc body -- not to the proc's own `variable cfg` link line.
func TestVariableLinkChasesToNamespaceVarSameFile(t *testing.T) {
	src := "namespace eval ::app {\n" +
		"  variable cfg 1\n" +
		"}\n" +
		"proc ::app::show {} {\n" +
		"  variable cfg\n" +
		"  puts $cfg\n" +
		"}\n"
	ix := index.New()
	ix.IndexFile("a.tcl", src)
	r := New(ix)
	use := strings.Index(src, "$cfg")
	if use < 0 {
		t.Fatal("no $cfg in source")
	}
	locs := r.Definition("a.tcl", src, use)
	if len(locs) == 0 {
		t.Fatal("no definition found for $cfg")
	}
	// The in-proc `variable cfg` line may legitimately appear too (in TCL it
	// declares the ns var if absent, so it IS a def site). The bug was that the
	// resolution never ESCAPED the proc: assert the namespace-level declaration
	// is among the results.
	procStart := strings.Index(src, "proc ::app::show")
	escaped := false
	for _, l := range locs {
		if l.Name != "::app::cfg" {
			t.Errorf("resolved to %q, want ::app::cfg; locs=%#v", l.Name, locs)
		}
		if l.NameStart < procStart {
			escaped = true
		}
	}
	if !escaped {
		t.Errorf("no location at the namespace-level declaration (all inside the proc): %#v", locs)
	}
}

// Cross-file: the namespace variable is declared in another file; the link
// must chase through the workspace index.
func TestVariableLinkChasesToNamespaceVarCrossFile(t *testing.T) {
	def := "namespace eval ::app {\n  variable cfg 1\n}\n"
	use := "proc ::app::show {} {\n  variable cfg\n  puts $cfg\n}\n"
	locs := resolveUse(t, def, use, "$cfg")
	requireDefInFile(t, locs, "variable-link inside proc, ns var in other file")
}
