package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// A backslash-escaped name word binds its decoded name, so a later $ab answers the
// write that reaches it. tclsh 8.6 and 9.0: `set a\x62 new` overwrites ab, so the
// earlier `set ab old` does not reach; scan's write is possible, so it joins.
// catch with an escaped word writes its options variable last.
func TestGotoDefEscapedVariableNames(t *testing.T) {
	wrap := map[string]func(string) string{
		"a.tcl": func(body string) string { return "proc f {s} {\n" + body + "}\n" },
		"p.rvt": func(body string) string { return "<h1>x</h1>\n<?\nproc f {s} {\n" + body + "}\n?>\n" },
	}
	cases := []struct {
		name, body, use string
		defs            []string
	}{
		{"set replaces", "  set ab old\n  set a\\x62 new\n  puts $ab\n", "$ab\n", []string{"a\\x62 new"}},
		{"scan joins", "  set ab old\n  scan $s %d a\\x62\n  puts $ab\n", "$ab\n", []string{"ab old", "a\\x62\n"}},
		{"foreach twice", "  set ab old\n  foreach a\\\\x62 {1} {}\n  puts $ab\n", "$ab\n", []string{"ab old", "a\\\\x62"}},
		{"array element", "  set a\\(k) 1\n  puts $a(k)\n", "$a(k)", []string{"a\\(k)"}},
		// A braced var-list element gets no substitution: it names `a\x62`, not ab.
		{"braced list element", "  set ab old\n  foreach {{a\\x62} c} {1 2} {}\n  puts $ab\n", "$ab\n", []string{"ab old"}},
		// With an escape on either word, catch writes the options last.
		{"catch options last", "  catch {error e} a\\x62 ab\n  puts $ab\n", "$ab\n", []string{"ab\n"}},
		{"catch same escaped", "  catch {error e} a\\x62 a\\x62\n  puts $ab\n", "$ab\n", []string{"a\\x62\n"}},
	}
	for file, w := range wrap {
		for _, c := range cases {
			t.Run(file+"/"+c.name, func(t *testing.T) {
				src := w(c.body)
				ix := index.New()
				ix.IndexFile(file, src)
				use := strings.Index(src, c.use) + 1
				locs := New(ix).Definition(file, src, use)
				var got []int
				for _, l := range locs {
					got = append(got, l.NameStart)
				}
				var want []int
				for _, d := range c.defs {
					want = append(want, strings.Index(src, d))
				}
				if len(got) != len(want) {
					t.Fatalf("goto-def on %s = %v, want %v", c.use, got, want)
				}
				for _, wnt := range want {
					found := false
					for _, g := range got {
						found = found || g == wnt
					}
					if !found {
						t.Fatalf("goto-def on %s = %v, want %v", c.use, got, want)
					}
				}
			})
		}
	}
}

// An escaped global, variable or upvar alias links the decoded name, so $ab answers
// what it answers for the plain spelling: the global or namespace variable, plus the
// proc's own `variable` declaration.
func TestGotoDefEscapedLinks(t *testing.T) {
	cases := []struct {
		name, src string
		defs      []string
	}{
		{"global", "set ab 1\nproc f {} {\n  global a\\x62\n  puts $ab\n}\n", []string{"ab 1"}},
		{"upvar #0", "set ab 1\nproc f {} {\n  upvar #0 a\\x62 a\\x62\n  puts $ab\n}\n", []string{"ab 1"}},
		{"variable", "namespace eval n {\n  variable ab 1\n  proc f {} {\n    variable a\\x62\n    puts $ab\n  }\n}\n",
			[]string{"ab 1", "a\\x62\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile("a.tcl", c.src)
			locs := New(ix).Definition("a.tcl", c.src, strings.Index(c.src, "$ab")+1)
			got := map[int]bool{}
			for _, l := range locs {
				got[l.NameStart] = true
			}
			for _, d := range c.defs {
				if !got[strings.Index(c.src, d)] {
					t.Fatalf("goto-def on $ab = %#v, want locations at %q", locs, c.defs)
				}
			}
			if len(got) != len(c.defs) {
				t.Fatalf("goto-def on $ab = %#v, want %d locations", locs, len(c.defs))
			}
		})
	}
}
