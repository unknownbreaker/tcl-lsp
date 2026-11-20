package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// #100: goto-definition on the method word of `$d field` inside a braced
// expression jumps to the method, in .tcl and .rvt, as it does in a bracket
// script; a distrusted expression stays silent. The mark is found per case.
func TestDefinitionObjMethodInBracedExpressions(t *testing.T) {
	ix := index.New()
	ix.IndexFile("disp.tcl", "itcl::class ::STDisplay {\n  method field {name} {}\n}")
	r := New(ix)
	for _, c := range []struct {
		name, file, src string
		want            bool
	}{
		{"if condition", "use.tcl", "proc f {} {\n  set d [::STDisplay #auto]\n  if {[$d field isbn]} {}\n}", true},
		{"while condition", "use.tcl", "proc f {} {\n  set d [::STDisplay #auto]\n  while {[$d field isbn]} {}\n}", true},
		{"expr argument", "use.tcl", "proc f {} {\n  set d [::STDisplay #auto]\n  set r [expr {1+[$d field isbn]}]\n}", true},
		{"rvt if condition", "page.rvt", "<h1>x</h1>\n<? set d [::STDisplay #auto]\nif {[$d field isbn]} { ?>y<? } ?>", true},
		{"rvt expr in a proc", "page.rvt", "<? proc f {} {\n  set d [::STDisplay #auto]\n  expr {1+[$d field isbn]}\n} ?>", true},
		{"distrusted expression", "use.tcl", "proc f {} {\n  set d [::STDisplay #auto]\n  if {[string length \"]\"] && [$d field isbn]} {}\n}", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			off := strings.Index(c.src, "field isbn")
			if off < 0 {
				t.Fatal("mark `field isbn` not in source")
			}
			locs := r.Definition(c.file, c.src, off)
			got := len(locs) == 1 && locs[0].File == "disp.tcl" && locs[0].Name == "field"
			if got != c.want || (!c.want && len(locs) != 0) {
				t.Fatalf("goto-def = %#v; want found=%v", locs, c.want)
			}
		})
	}
}
