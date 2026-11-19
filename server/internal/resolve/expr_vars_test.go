package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// expr substitutes the $vars of a braced expression itself, so goto-def on one
// lands on what binds it: a proc parameter, a local, or a linked namespace
// variable. Checked against tclsh 8.6 and 9.0.
func TestGotoDefVarInBracedExpression(t *testing.T) {
	cases := []struct{ name, file, src, use, def string }{
		{"param in an if condition", "a.tcl",
			"proc fac {n} {\n  if {$n <= 1} {return 1}\n  return [expr {$n * [fac [expr {$n - 1}]]}]\n}\n",
			"$n <=", "n} {"},
		{"param in an expr argument", "a.tcl",
			"proc fac {n} {\n  if {$n <= 1} {return 1}\n  return [expr {$n * [fac [expr {$n - 1}]]}]\n}\n",
			"$n - 1", "n} {"},
		{"local in a while test", "a.tcl",
			"proc f {} {\n  set i 0\n  while {$i < 3} {incr i}\n}\n",
			"$i <", "i 0"},
		{"linked namespace variable", "a.tcl",
			"namespace eval ::app {\n  variable base 10\n  proc scale {n} {\n    variable base\n    return [expr {$n * $base}]\n  }\n}\n",
			"$base}", "base 10"},
		{"proc in an rvt page", "p.rvt",
			"<h1>x</h1>\n<?\nproc f {n} {\n  if {$n <= 1} {return 1}\n}\n?>\n",
			"$n <=", "n} {"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(c.file, c.src)
			use := strings.Index(c.src, c.use) + 1
			want := strings.Index(c.src, c.def)
			var starts []int
			for _, l := range New(ix).Definition(c.file, c.src, use) {
				starts = append(starts, l.NameStart)
			}
			if len(starts) == 0 || !containsInt(starts, want) {
				t.Fatalf("goto-def on %q = %v, want a location at %d", c.use, starts, want)
			}
		})
	}
}

// An expression's structure decides which bindings reach a use, in a proc in a
// .tcl file and in an .rvt page alike: the ternary's false arm reads the value from
// before the bracket, and an if body after an && chain only the chain's bracket.
// tclsh 8.6 and 9.0 (research/experiments/08_and_chain_reaching.tcl).
func TestGotoDefFollowsExpressionStructure(t *testing.T) {
	wrap := map[string]func(string) string{
		"a.tcl": func(body string) string { return "proc f {c} {\n" + body + "}\n" },
		"p.rvt": func(body string) string { return "<h1>x</h1>\n<?\nproc f {c} {\n" + body + "}\n?>\n" },
	}
	cases := []struct{ name, body, use, def string }{
		{"ternary false arm", "  set x 0\n  return [expr {$c ? [set x 1] : $x}]\n", "$x}", "x 0"},
		{"if body after an && chain", "  set x 0\n  if {$c && [set x 1]} {\n    puts $x\n  }\n", "$x\n", "x 1"},
		{"a later && operand", "  set x 0\n  if {$c && [set x 1] && $x} {}\n", "$x}", "x 1"},
	}
	for file, w := range wrap {
		for _, c := range cases {
			t.Run(file+"/"+c.name, func(t *testing.T) {
				src := w(c.body)
				ix := index.New()
				ix.IndexFile(file, src)
				use := strings.LastIndex(src, c.use) + 1
				want := strings.Index(src, c.def)
				locs := New(ix).Definition(file, src, use)
				if len(locs) != 1 || locs[0].NameStart != want {
					t.Fatalf("goto-def on %s = %#v, want one location at offset %d", c.use, locs, want)
				}
			})
		}
	}
}

// Tcl's else keyword is optional, so a last body with no condition before it
// runs when the condition is false: it sees the condition's false paths too.
func TestGotoDefImplicitElseSeesFalsePaths(t *testing.T) {
	for _, tc := range []struct{ file, src string }{
		{"/w/a.tcl", "proc f {c} {\n  set x 0\n  if {$c && [set x 1]} {} {puts $x}\n}\n"},
		{"/w/a.rvt", "<html>\n<?\nproc f {c} {\n  set x 0\n  if {$c && [set x 1]} {} {puts $x}\n}\n?>\n</html>\n"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(tc.file, tc.src)
			locs := New(ix).Definition(tc.file, tc.src, strings.Index(tc.src, "puts $x")+6)
			var got []int
			for _, l := range locs {
				got = append(got, l.NameStart)
			}
			x0, x1 := strings.Index(tc.src, "x 0"), strings.Index(tc.src, "x 1")
			if len(got) != 2 || !containsInt(got, x0) || !containsInt(got, x1) {
				t.Fatalf("goto-def on $x = %v, want %d and %d", got, x0, x1)
			}
		})
	}
}

// A `]` inside quotes in a bracket script does not end it, so Tcl runs `set x 1`
// before the use. The flat scan cannot tell where the script ends, so goto-def
// stays silent rather than answer `set x 0` alone. Checked against tclsh 8.6 and 9.0.
func TestGotoDefSilentAfterHiddenBracket(t *testing.T) {
	for _, tc := range []struct{ file, src string }{
		{"/w/a.tcl", "proc f {} {\n  set x 0\n  if {[string length \"]\"; set x 1]} {}\n  puts $x\n}\n"},
		{"/w/a.rvt", "<html>\n<?\nproc f {} {\n  set x 0\n  if {[string length \"]\"; set x 1]} {}\n  puts $x\n}\n?>\n</html>\n"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(tc.file, tc.src)
			if locs := New(ix).Definition(tc.file, tc.src, strings.Index(tc.src, "puts $x")+6); len(locs) > 0 {
				t.Fatalf("goto-def on $x = %+v, want silent", locs)
			}
		})
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
