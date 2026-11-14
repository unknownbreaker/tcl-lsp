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

// A use after a bracket that runs on only some paths stays silent: in
// `$c ? [set x 1] : $x` the false arm reads the value from before the bracket.
func TestGotoDefSilentAfterConditionalBracketInExpression(t *testing.T) {
	src := "proc f {c} {\n  set x 0\n  return [expr {$c ? [set x 1] : $x}]\n}\n"
	ix := index.New()
	ix.IndexFile("a.tcl", src)
	use := strings.LastIndex(src, "$x") + 1
	if locs := New(ix).Definition("a.tcl", src, use); len(locs) != 0 {
		t.Fatalf("goto-def on $x after a conditional bracket = %#v, want none", locs)
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
