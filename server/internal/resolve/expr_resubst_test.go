package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// expr substitutes its unbraced words again when it evaluates them, so an escaped
// bracket or a substituted value may form a script that binds (tclsh 8.6 and 9.0).
// Goto-def stays silent after such an expr, while a use read on the first pass
// keeps its answer, in .tcl and .rvt.
func TestExprResubstThroughResolver(t *testing.T) {
	for _, shape := range []struct{ name, expr string }{
		{"escaped brackets", `expr "\[set x 1\] + $x"`},
		{"escaped over several words", `expr "\[set" x 1\] + $x`},
		{"substituted value", `expr $x + $s`},
		{"bare value", `expr $s`},
		{"values around a braced word", `expr $s {set x 1} $x`},
	} {
		body := "proc f {s} {\n" +
			"  set x 0\n" +
			"  " + shape.expr + "\n" +
			"  puts $x\n" +
			"}\n"
		for _, c := range []struct{ name, file, src string }{
			{"tcl proc", "a.tcl", body},
			{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
		} {
			t.Run(shape.name+"/"+c.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(c.file, c.src)
				r := New(ix)
				at := func(mark string) int {
					off := strings.Index(c.src, mark)
					if off < 0 {
						t.Fatalf("mark %q not in source", mark)
					}
					return off + 1 // the name, past '$'
				}
				if locs := r.Definition(c.file, c.src, at("$x\n}")); len(locs) != 0 {
					t.Errorf("Definition after the expr = %v, want silent", locs)
				}
				if !strings.Contains(shape.expr, "$x") {
					return
				}
				inWord := strings.Index(c.src, shape.expr) + strings.Index(shape.expr, "$x") + 1
				locs := r.Definition(c.file, c.src, inWord)
				bind := at("x 0") - 1
				if len(locs) != 1 || locs[0].NameStart != bind {
					t.Errorf("Definition in the expr word = %v, want the set at %d", locs, bind)
				}
			})
		}
	}
}
