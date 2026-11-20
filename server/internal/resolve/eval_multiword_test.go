package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// eval with more than one word runs its joined words as a script in this frame
// (tclsh 8.6 and 9.0: `set x 0; eval set x 1; puts $x` prints 1). Goto-definition
// after it stays silent rather than name only `set x 0`, while one-word eval keeps
// both bindings, in a .tcl proc and an .rvt page proc (#132).
func TestMultiWordEvalThroughResolver(t *testing.T) {
	for _, c := range []struct {
		name, line string
		want       []string
	}{
		{"bare words", "eval set x 1", nil},
		{"braced words", "eval {set x} {1}", nil},
		{"qualified head", "::eval set x 1", nil},
		{"one word", "eval {set x 1}", []string{"x 0", "x 1"}},
	} {
		body := "proc f {} {\n  set x 0\n  " + c.line + "\n  puts $x\n}\n"
		for _, f := range []struct{ name, file, src string }{
			{"tcl proc", "a.tcl", body},
			{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
		} {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(f.file, f.src)
				at := func(mark string) int {
					off := strings.Index(f.src, mark)
					if off < 0 {
						t.Fatalf("mark %q not in source", mark)
					}
					return off
				}
				locs := New(ix).Definition(f.file, f.src, at("$x\n}")+1)
				if len(locs) != len(c.want) {
					t.Fatalf("Definition at $x = %v, want %d bindings %v", locs, len(c.want), c.want)
				}
				got := map[int]bool{}
				for _, l := range locs {
					got[l.NameStart] = true
				}
				for _, m := range c.want {
					if !got[at(m)] {
						t.Errorf("Definition at $x = %v, missing the set at %q", locs, m)
					}
				}
			})
		}
	}
}
