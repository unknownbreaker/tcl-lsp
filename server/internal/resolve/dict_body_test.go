package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// dict with and dict update run their body once, in place (tclsh 8.6 and 9.0), so
// calls there resolve like calls in an `if` body: a bare method-named call in a
// method is the method's, and a proc call is the proc's. Inside a custom wrapper's
// body they stay silent, as the wrapper's frame is unknown.
const dictBodySrc = `proc helper {} {}
proc m {} {}
itcl::class K {
    method m {} {}
    method viaWith {} { dict with d { m with; helper with } }
    method viaUpdate {} { dict update d k v { m update; helper update } }
    method viaWithKeys {} { dict with d a b { m keys } }
    method wrapped {} { with_lock { dict with d { m wrapped } } }
}
proc p {} { dict with d { helper inproc } }
dict with ::cfg { helper top }
`

func TestDictBodyCallsResolve(t *testing.T) {
	for _, file := range []string{"k.tcl", "p.rvt"} {
		t.Run(file, func(t *testing.T) {
			src := dictBodySrc
			if strings.HasSuffix(file, ".rvt") {
				src = "<h1>x</h1>\n<?\n" + dictBodySrc + "?>\n"
			}
			ix := index.New()
			ix.IndexFile(file, src)
			r := New(ix)
			at := func(sub string) int { return strings.Index(src, sub) }
			method := msite{file, at("m {} {}\n    method viaWith")}
			helper := msite{file, at("helper {} {}")}

			wantMethodSites(t, "References(method)", r.References(file, src, method.start),
				msite{file, at("m with")}, msite{file, at("m update")}, msite{file, at("m keys")})
			for _, call := range []string{"m with", "m update", "m keys"} {
				if d := r.Definition(file, src, at(call)); len(d) != 1 || d[0].NameStart != method.start {
					t.Errorf("Definition(%q) = %v, want the method at %d", call, d, method.start)
				}
			}
			for _, call := range []string{"helper with", "helper update", "helper inproc", "helper top"} {
				if d := r.Definition(file, src, at(call)); len(d) != 1 || d[0].NameStart != helper.start {
					t.Errorf("Definition(%q) = %v, want ::helper at %d", call, d, helper.start)
				}
			}
			if d := r.Definition(file, src, at("m wrapped")); len(d) != 0 {
				t.Errorf("Definition(m wrapped) = %v, want silence", d)
			}
		})
	}
}

// dict update writes each variable before its body runs, or unsets it when the key
// is missing (tclsh 8.6 and 9.0), so an earlier binding never reaches a use there.
// Both paths agree: the index (emitDefs) and reaching (localBindings).
func TestGotoDefDictUpdateVars(t *testing.T) {
	wrap := map[string]func(string) string{
		"a.tcl": func(body string) string { return "proc f {d} {\n" + body + "}\n" },
		"p.rvt": func(body string) string { return "<h1>x</h1>\n<?\nproc f {d} {\n" + body + "}\n?>\n" },
	}
	cases := []struct{ name, body, use, def string }{
		{"in body", "  set v 9\n  dict update d k v {\n    puts $v\n  }\n", "$v\n", "v {"},
		{"second pair", "  set w 9\n  dict update d k v j w {\n    puts $w\n  }\n", "$w\n", "w {"},
		{"after body", "  set v 9\n  dict update d k v {}\n  puts $v\n", "$v\n", "v {}"},
		{"rebound in body", "  dict update d k v {\n    set v 2\n  }\n  puts $v\n", "$v\n", "v 2"},
		{"braced var", "  set v 9\n  dict update d k {v} {\n    puts $v\n  }\n", "$v\n", "v} {"},
		{"quoted var", "  set v 9\n  dict update d k \"v\" {\n    puts $v\n  }\n", "$v\n", "v\" {"},
		{"with body binds", "  set x 0\n  dict with d {\n    set x 1\n  }\n  puts $x\n", "$x\n", "x 1"},
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
