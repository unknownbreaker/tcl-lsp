package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// catch, try, gets, regsub, regexp and binary scan result variables are bindings,
// so $msg after them answers. Both paths agree: the index (emitDefs) and reaching
// (localBindings). tclsh 8.6 and 9.0 confirm catch overwrites msg, a handler binds
// its variable list, and gets, regsub, regexp and binary scan write theirs.
func TestGotoDefCatchAndTryResultVars(t *testing.T) {
	wrap := map[string]func(string) string{
		"a.tcl": func(body string) string { return "proc f {} {\n" + body + "}\n" },
		"p.rvt": func(body string) string { return "<h1>x</h1>\n<?\nproc f {} {\n" + body + "}\n?>\n" },
	}
	cases := []struct{ name, body, use, def string }{
		{"catch", "  set msg 0\n  if {[catch {foo} msg]} {\n    puts $msg\n  }\n", "$msg\n", "msg]"},
		{"try handler", "  set e 0\n  try {foo} on error {e o} {\n    puts $e\n  }\n", "$e\n", "e o}"},
		{"gets", "  while {[gets $f line] >= 0} {\n    puts $line\n  }\n", "$line\n", "line]"},
		{"regexp", "  if {[regexp {^(\\w+)} $s -> word]} {\n    puts $word\n  }\n", "$word\n", "word]"},
		{"binary scan", "  binary scan $d cc a b\n  puts $b\n", "$b\n", "b\n"},
	}
	for file, w := range wrap {
		for _, c := range cases {
			t.Run(file+"/"+c.name, func(t *testing.T) {
				src := w(c.body)
				ix := index.New()
				ix.IndexFile(file, src)
				use := strings.Index(src, c.use) + 1
				locs := New(ix).Definition(file, src, use)
				want := strings.Index(src, c.def)
				if len(locs) != 1 || locs[0].NameStart != want {
					t.Fatalf("goto-def on %s = %#v, want one location at offset %d", c.use, locs, want)
				}
			})
		}
	}
}
