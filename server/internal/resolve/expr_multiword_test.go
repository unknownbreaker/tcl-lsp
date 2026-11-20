package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// A braced word of a multi-word expr is expression text only once joined:
// `expr "{" {$x} "}"` returns the literal " $x " and never reads $x, and a
// braced script may or may not run (tclsh 8.6 and 9.0). So find-references and
// goto-def stay silent there, while unbraced words still count, in .tcl and .rvt.
func TestMultiWordExprBracedWordsThroughResolver(t *testing.T) {
	body := "proc f {} {\n" +
		"  set x 0\n" +
		"  expr \"{\" {$x} \"}\"\n" +
		"  expr {\"} {[set x 1]} {\"}\n" +
		"  puts $x\n" +
		"  expr 1 + $x\n" +
		"}\n"
	for _, c := range []struct{ name, file, src string }{
		{"tcl proc", "a.tcl", body},
		{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
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
			braced, later := at("$x} \"}\""), at("$x\n  expr 1")
			for _, use := range []int{braced, later} {
				if locs := r.Definition(c.file, c.src, use); len(locs) != 0 {
					t.Errorf("Definition at %d = %v, want silent", use, locs)
				}
			}
			unbraced := at("$x\n}")
			refs := r.References(c.file, c.src, at("$x\n}"))
			sawUnbraced := false
			for _, l := range refs {
				if l.NameStart == braced-1 { // a reference starts at its '$'
					t.Errorf("References lists the braced $x at %d: %v", braced, refs)
				}
				if l.NameStart == unbraced-1 {
					sawUnbraced = true
				}
			}
			if !sawUnbraced {
				t.Errorf("References = %v, want the unbraced $x at %d", refs, unbraced)
			}
		})
	}
}
