package tcl

import (
	"strings"
	"testing"
)

// exprHeads are the heads Tcl runs as the expr builtin. Tcl names a command by
// its word's value, so `::expr`, `"expr"`, `{expr}` and `{*}expr` all run expr
// (#114). Each case below leaves the same x in tclsh 8.6 and 9.0 for every head.
var exprHeads = []string{"expr", "::expr", `"expr"`, "{expr}", "{*}expr", "{*}::expr"}

// A braced word of any expr head is an expression: its scripts run, and their
// bindings reach later uses.
func TestReachingExprHeadsBracedWords(t *testing.T) {
	for _, h := range exprHeads {
		runReachCases(t, []reachCase{
			{h + "/script runs", "proc f {} {\n  set x 0\n  " + h + " {[set x 1]}\n  puts $x\n}", "$x\n}", []int{3}},
			{h + "/script may not run", "proc f {} {\n  set x 0\n  " + h + " {0 && [set x 1]}\n  puts $x\n}", "$x\n}", []int{2, 3}},
			{h + "/in a bracket script", "proc f {} {\n  set x 0\n  set r [" + h + " {[set x 1]}]\n  puts $x\n}", "$x\n}", []int{3}},
			{h + "/use after the script", "proc f {} {\n  set x 0\n  " + h + " {[set x 1] + $x}\n}", "$x}", []int{3}},
			{h + "/loop back edge", "proc f {} {\n  set x 0\n  while 1 {\n    puts $x\n    " + h + " {[set x 1]}\n  }\n}", "$x\n    ", []int{2, 5}},
		})
	}
}

// The braced words of a multi-word expr form one expression only once joined, so a
// bracket in one distrusts what follows, whatever the head.
func TestReachingExprHeadsJoinedSilent(t *testing.T) {
	for _, h := range exprHeads {
		runSilentReachCases(t, []silentReachCase{
			{h + "/joined braced script", "proc f {} {\n  set x 0\n  " + h + " {1} + {[set x 1]}\n  puts $x\n}", "$x\n}"},
			{h + "/operator carries", "proc f {c} {\n  set x 0\n  " + h + " $c || {[set x 1]}\n  puts $x\n}", "$x\n}"},
		})
	}
}

// Find-references sees the $vars and calls in a braced word of any expr head, once.
func TestCommandRefsExprHeadsBracedWords(t *testing.T) {
	for _, h := range exprHeads {
		t.Run(h, func(t *testing.T) {
			src := "set y [" + h + " {$x + [g]}]"
			want := strings.Index(src, "$x")
			if got := exprVarOffsets(src, "x"); len(got) != 1 || got[0] != want {
				t.Errorf("refs to x = %v, want [%d]", got, want)
			}
			calls := 0
			for _, c := range Parse(src) {
				for _, r := range CommandRefs(c) {
					if r.Kind == RefCommand && r.Name == "g" {
						calls++
					}
				}
			}
			if calls != 1 {
				t.Errorf("calls to g = %d, want 1", calls)
			}
		})
	}
}
