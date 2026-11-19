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

// A `]` inside quotes, braces or an array index in a bracket script does not end
// it. The flat scan cannot tell where the script ends, so goto-def stays silent
// rather than answer `set x 0` alone (the earliest-binding fallback would too).
// Checked against tclsh 8.6 and 9.0 in research/experiments/08_quoted_bracket_flat.tcl.
func TestGotoDefSilentAfterHiddenBracket(t *testing.T) {
	bodies := []struct{ name, body, mark string }{
		{"next command", "  set x 0\n  if {[string length \"]\"; set x 1]} {}\n  puts $x\n", "$x\n"},
		{"same condition", "  set x 0\n  if {[string length \"]\"; set x 1] && $x ne {\"}} {}\n", "$x ne"},
		{"braced", "  set x 0\n  if {[string map {] [set x 1]} $s] ne {} && $x == 0} {}\n", "$x =="},
		{"array index", "  set x 0; array set a {] 5}\n  if {[list $a(]); set x 1] ne {} && $x} {}\n", "$x}"},
	}
	for _, b := range bodies {
		proc := "proc f {s} {\n" + b.body + "}\n"
		for _, tc := range []struct{ file, src string }{
			{"/w/a.tcl", proc},
			{"/w/a.rvt", "<html>\n<?\n" + proc + "?>\n</html>\n"},
		} {
			t.Run(b.name+" "+tc.file, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(tc.file, tc.src)
				off := strings.Index(tc.src, b.mark)
				if locs := New(ix).Definition(tc.file, tc.src, off+1); len(locs) > 0 {
					t.Fatalf("goto-def on $x = %+v, want silent", locs)
				}
			})
		}
	}
}

// A command substitution in a command word ends where Tcl ends it, not at a `]`
// in its braces or quotes, so goto-def on a use after it reaches the bindings the
// script ran, and stays silent where Tcl 8.6 and 9.0 may read its end differently.
// Checked in research/experiments/08_quoted_bracket_command.tcl.
func TestGotoDefAfterHiddenBracketInCommandWord(t *testing.T) {
	bodies := []struct {
		name, body, mark, want string // want: the text the answer starts at, or "" for silent
	}{
		{"quoted word", "  set x 0\n  set r \"[list {]}; set x 1]\"\n  puts $x\n", "$x\n", "x 1]"},
		{"bare word", "  set x 0\n  puts [string length \"]\"; set x 1]\n  puts $x\n", "$x\n", "x 1]"},
		{"quoted if", "  set x 0\n  if \"[list {]}; set x 1] ne {} && \\$x\" {puts $x}\n", "$x}", "x 1]"},
		{"expanded word", "  set x 0\n  set r \"[list {*}{]} ; set x 1]\"\n  puts $x\n", "$x\n", "x 1]"},
		{"lone open bracket", "  set x 0\n  set i [string first {[} $s]\n  set x 2\n  puts $x\n", "$x\n", "x 2"},
		{"balanced", "  set x 0\n  set r \"[string length \"[set x 1]\"]\"\n  puts $x\n", "$x\n", "x 1]"},
		{"versions disagree", "  set x 0\n  set r \"[list ${a\\}] ; set x 1]\"\n  puts $x\n", "$x\n", ""},
	}
	for _, b := range bodies {
		proc := "proc f {s} {\n" + b.body + "}\n"
		for _, tc := range []struct{ file, src string }{
			{"/w/a.tcl", proc},
			{"/w/a.rvt", "<html>\n<?\n" + proc + "?>\n</html>\n"},
		} {
			t.Run(b.name+" "+tc.file, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(tc.file, tc.src)
				off := strings.Index(tc.src, b.mark)
				if off < 0 {
					t.Fatalf("mark %q not in source", b.mark)
				}
				locs := New(ix).Definition(tc.file, tc.src, off+1)
				if b.want == "" {
					if len(locs) > 0 {
						t.Fatalf("goto-def on $x = %+v, want silent", locs)
					}
					return
				}
				wantOff := strings.Index(tc.src, b.want)
				if wantOff < 0 {
					t.Fatalf("answer mark %q not in source", b.want)
				}
				if len(locs) != 1 || locs[0].NameStart != wantOff {
					t.Fatalf("goto-def on $x = %+v, want one answer at %d (%q)", locs, wantOff, b.want)
				}
			})
		}
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
