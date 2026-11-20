package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// Tcl runs expr for any head whose value is expr or ::expr (tclsh 8.6 and 9.0), so
// a braced word of each is an expression: its $vars are uses, its scripts run and
// their bindings reach, and its calls are references, in .tcl and .rvt (#114).
func TestExprHeadsThroughResolver(t *testing.T) {
	for _, h := range []string{"expr", "::expr", `"expr"`, "{*}expr"} {
		body := "proc helper {} { return 1 }\n" +
			"proc f {} {\n" +
			"  set x 0\n" +
			"  " + h + " {$x + [helper] + [set x 1]}\n" +
			"  puts $x\n" +
			"}\n"
		for _, c := range []struct{ name, file, src string }{
			{"tcl proc", "a.tcl", body},
			{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
		} {
			t.Run(h+"/"+c.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(c.file, c.src)
				r := New(ix)
				at := func(mark string) int {
					off := strings.Index(c.src, mark)
					if off < 0 {
						t.Fatalf("mark %q not in source", mark)
					}
					return off
				}
				if locs := r.Definition(c.file, c.src, at("$x +")+1); len(locs) != 1 || locs[0].NameStart != at("x 0") {
					t.Errorf("Definition in the expression = %v, want the set at %d", locs, at("x 0"))
				}
				if locs := r.Definition(c.file, c.src, at("$x\n}")+1); len(locs) != 1 || locs[0].NameStart != at("x 1") {
					t.Errorf("Definition after the expr = %v, want the set at %d", locs, at("x 1"))
				}
				use := at("$x +") // a variable reference starts at its $
				seen := false
				for _, l := range r.References(c.file, c.src, at("x 0")) {
					if l.NameStart == use {
						seen = true
					}
				}
				if !seen {
					t.Errorf("References to x miss the use at %d in the expression", use)
				}
				call := at("helper]")
				found := false
				for _, l := range r.References(c.file, c.src, at("helper")) {
					if l.NameStart == call {
						found = true
					}
				}
				if !found {
					t.Errorf("References to helper miss the call at %d", call)
				}
			})
		}
	}
}

// An expr whose words join into a string spelling `proc p {} ...` defines nothing
// (`info commands p` is empty in tclsh 8.6 and 9.0), so a call to p stays silent
// rather than jumping to a decorated proc definition.
func TestExprHeadsStringIsNotADecoratedProc(t *testing.T) {
	for _, h := range []string{"expr", "::expr"} {
		for _, c := range []struct{ name, file, pre, post string }{
			{"tcl", "a.tcl", "", ""},
			{"rvt", "p.rvt", "<h1>x</h1>\n<?\n", "?>\n"},
		} {
			t.Run(h+"/"+c.name, func(t *testing.T) {
				src := c.pre + h + " {\"} proc p {} {\"}\nproc g {} {\n  p\n}\n" + c.post
				ix := index.New()
				ix.IndexFile(c.file, src)
				off := strings.Index(src, "  p\n")
				if off < 0 {
					t.Fatal("mark missing")
				}
				if locs := New(ix).Definition(c.file, src, off+2); len(locs) != 0 {
					t.Errorf("Definition of p = %v, want none\n%s", locs, src)
				}
			})
		}
	}
}
