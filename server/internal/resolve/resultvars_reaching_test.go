package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// catch, try, gets, regsub, regexp, binary scan and scan result variables are bindings,
// so $msg after them answers. Both paths agree: the index (emitDefs) and reaching
// (localBindings). tclsh 8.6 and 9.0 confirm catch overwrites msg, a handler binds
// its variable list, and gets, regsub, regexp, binary scan and scan write theirs.
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
		{"scan", "  scan $s {%d %d} a b\n  puts $b\n", "$b\n", "b\n"},
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

// After `global a`, scan, regexp and binary scan only possibly write a, so the link
// still reaches $a and is chased to ::a's sites, which include the result var itself.
// Each location is listed once. (regsub, gets and catch always write, killing the
// link.) The top-level `set a 0` is ::a only in a .tcl file; an .rvt page runs in
// ::request.
func TestGotoDefResultVarAfterGlobalOnce(t *testing.T) {
	wrap := map[string]func(string) string{
		"a.tcl": func(body string) string { return body },
		"p.rvt": func(body string) string { return "<h1>x</h1>\n<?\n" + body + "?>\n" },
	}
	cases := []struct{ name, cmd string }{
		{"scan", "scan $s %d a"},
		{"regexp", "regexp {(x)} $s -> a"},
		{"binary scan", "binary scan $s c a"},
	}
	for file, w := range wrap {
		for _, c := range cases {
			for _, top := range []bool{false, true} {
				if top && file == "p.rvt" {
					continue
				}
				name := file + "/" + c.name
				body := "proc f {s} {\n  global a\n  " + c.cmd + "\n  puts $a\n}\n"
				if top {
					name += "/top-level set"
					body = "set a 0\n" + body
				}
				t.Run(name, func(t *testing.T) {
					src := w(body)
					ix := index.New()
					ix.IndexFile(file, src)
					locs := New(ix).Definition(file, src, strings.Index(src, "$a")+1)
					cmdAt := strings.Index(src, c.cmd)
					want := []int{cmdAt + strings.LastIndex(c.cmd, "a")}
					if top {
						want = append([]int{strings.Index(src, "a 0")}, want...)
					}
					var got []int
					for _, l := range locs {
						got = append(got, l.NameStart)
					}
					if len(got) != len(want) {
						t.Fatalf("goto-def on $a = %v, want %v", got, want)
					}
					for _, w := range want {
						found := false
						for _, g := range got {
							found = found || g == w
						}
						if !found {
							t.Fatalf("goto-def on $a = %v, want %v", got, want)
						}
					}
				})
			}
		}
	}
}
