package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// A use in a script inside [...] runs after the bindings made before it in that
// script, so goto-def must land on the nearest one, not a stale binding above the
// command. tclsh 8.6 and 9.0 print the value of `set fp [open $b w]` here.
func TestGotoDefUseInBracketedCatchScript(t *testing.T) {
	cases := []struct{ name, file, src string }{
		{"tcl proc", "a.tcl", "proc f {a b} {\n" +
			"  set fp [open $a r]\n" +
			"  if {[catch {\n" +
			"    set fp [open $b w]\n" +
			"    puts $fp hi\n" +
			"  } err]} {}\n" +
			"}\n"},
		{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\nproc f {a b} {\n" +
			"  set fp [open $a r]\n" +
			"  if {[catch {\n" +
			"    set fp [open $b w]\n" +
			"    puts $fp hi\n" +
			"  } err]} {}\n" +
			"}\n?>\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(c.file, c.src)
			use := strings.Index(c.src, "$fp hi") + 1
			locs := New(ix).Definition(c.file, c.src, use)
			want := strings.Index(c.src, "set fp [open $b") + len("set ")
			if len(locs) != 1 || locs[0].NameStart != want {
				t.Fatalf("goto-def on $fp = %#v, want one location at offset %d", locs, want)
			}
		})
	}
}
