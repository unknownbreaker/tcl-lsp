package tcl

import (
	"strings"
	"testing"
)

// controlHeads spells each head Tcl runs as the named builtin. Tcl names a command
// by its word's value, so a quoted, braced, qualified or expanded head runs the same
// if, while or for (#117). Each case below leaves the same values in tclsh 8.6 and
// 9.0 for every head. An expanded head counts by its one list element, so
// `{*}{ if }` and `{*}{{if}}` run if too.
func controlHeads(name string) []string {
	return []string{name, `"` + name + `"`, "{" + name + "}", "{*}{" + name + "}", "{*}" + name, "::" + name, "{*}::" + name,
		"{*}{ " + name + " }", "{*}{{" + name + "}}", "{*}{\t::" + name + " }"}
}

// An if body named by any literal head runs in this frame, so its bindings reach
// later uses, and its conditions are expressions.
func TestReachingIfHeads(t *testing.T) {
	for _, h := range controlHeads("if") {
		runReachCases(t, []reachCase{
			{h + "/body binds", "proc f {c} {\n  set x 0\n  " + h + " {$c} {set x 1}\n  puts $x\n}", "$x\n}", []int{2, 3}},
			{h + "/else body binds", "proc f {c} {\n  set x 0\n  " + h + " {$c} {set y 1} else {set x 2}\n  puts $x\n}", "$x\n}", []int{2, 3}},
			{h + "/elseif body binds", "proc f {c d} {\n  set x 0\n  " + h + " {$c} {} elseif {$d} {set x 3}\n  puts $x\n}", "$x\n}", []int{2, 3}},
			{h + "/braced condition script", "proc f {} {\n  set x 0\n  " + h + " {[set x 1]} {}\n  puts $x\n}", "$x\n}", []int{3}},
			{h + "/use in the body", "proc f {c} {\n  set x 0\n  " + h + " {$c} {\n    puts $x\n  }\n}", "$x\n  }", []int{2}},
		})
	}
}

// A while body named by any literal head loops: a binding late in the body reaches
// an earlier use on the next pass, and so does a binding in the test.
func TestReachingWhileHeads(t *testing.T) {
	for _, h := range controlHeads("while") {
		runReachCases(t, []reachCase{
			{h + "/back edge", "proc f {} {\n  set x 0\n  " + h + " {[incr n] < 3} {\n    puts $x\n    set x 1\n  }\n}", "$x\n    set", []int{2, 5}},
			{h + "/after the loop", "proc f {} {\n  set x 0\n  " + h + " {[incr n] < 3} {\n    set x 1\n  }\n  puts $x\n}", "$x\n}", []int{2, 4}},
			{h + "/test binds", "proc f {} {\n  set x 0\n  " + h + " {[set x 1] < 3} {\n    break\n  }\n  puts $x\n}", "$x\n}", []int{3}},
		})
	}
}

// A for named by any literal head runs its start once, then loops its test, body and
// next, so bindings in the body and next reach earlier uses on the next pass.
func TestReachingForHeads(t *testing.T) {
	for _, h := range controlHeads("for") {
		runReachCases(t, []reachCase{
			{h + "/back edge", "proc f {} {\n  set x 0\n  " + h + " {set n 0} {$n < 2} {incr n} {\n    puts $x\n    set x 1\n  }\n}", "$x\n    set", []int{2, 5}},
			{h + "/start binds", "proc f {} {\n  " + h + " {set n 0} {$n < 2} {} {\n    puts $n\n    break\n  }\n}", "$n\n", []int{2}},
			{h + "/next reaches the body", "proc f {} {\n  set n 0\n  " + h + " {} {$n < 2} {\n    set n 5\n  } {\n    puts $n\n  }\n}", "$n\n  }", []int{2, 4}},
		})
	}
}

// Find-references sees the $vars and calls in the braced conditions and tests of
// any literal head, once each.
func TestCommandRefsControlHeadsExpressions(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"if", "{$x > [g]} {}"},
		{"while", "{$x > [g]} {}"},
		{"for", "{} {$x > [g]} {} {}"},
	} {
		for _, h := range controlHeads(c.name) {
			t.Run(h, func(t *testing.T) {
				src := h + " " + c.src
				want := strings.Index(src, "$x")
				if got := exprVarOffsets(src, "x"); len(got) != 1 || got[0] != want {
					t.Errorf("refs to x = %v, want [%d] in %s", got, want, src)
				}
				calls := 0
				for _, cmd := range Parse(src) {
					for _, r := range CommandRefs(cmd) {
						if r.Kind == RefCommand && r.Name == "g" {
							calls++
						}
					}
				}
				if calls != 1 {
					t.Errorf("calls to g = %d, want 1 in %s", calls, src)
				}
			})
		}
	}
}

// Every other command whose scripts the analysis models runs by its head's value
// too: catch, try, switch, eval, time and timerate scripts, foreach, lmap and dict
// loops, and the dict body forms bind in this frame, and return, break and continue
// end the path (#126). Each case leaves the same values in tclsh 8.6 and 9.0 for
// every head.
func TestReachingModeledHeads(t *testing.T) {
	scripts := []struct {
		name, args string
		want       []int
	}{
		{"catch", "{set x 1}", []int{2, 3}},
		{"try", "{set x 1}", []int{2, 3}},
		{"switch", "a {a {set x 1}}", []int{2, 3}},
		{"eval", "{set x 1}", []int{2, 3}},
		{"time", "{set x 1}", []int{2, 3}},
		{"timerate", "{set x 1}", []int{2, 3}},
	}
	for _, s := range scripts {
		for _, h := range controlHeads(s.name) {
			runReachCases(t, []reachCase{
				{h + "/script binds", "proc f {} {\n  set x 0\n  " + h + " " + s.args + "\n  puts $x\n}", "$x\n}", s.want},
			})
		}
	}
	loops := []struct{ name, args string }{
		{"foreach", "v {1 2}"},
		{"lmap", "v {1 2}"},
	}
	for _, l := range loops {
		for _, h := range controlHeads(l.name) {
			runReachCases(t, []reachCase{
				{h + "/back edge", "proc f {} {\n  set x 0\n  " + h + " " + l.args + " {\n    puts $x\n    set x 1\n  }\n}", "$x\n    set", []int{2, 5}},
			})
		}
	}
	for _, h := range controlHeads("dict") {
		runReachCases(t, []reachCase{
			{h + "/for back edge", "proc f {} {\n  set x 0\n  " + h + " for {k v} {a 1 b 2} {\n    puts $x\n    set x 1\n  }\n}", "$x\n    set", []int{2, 5}},
			{h + "/map back edge", "proc f {} {\n  set x 0\n  " + h + " map {k v} {a 1 b 2} {\n    puts $x\n    set x 1\n  }\n}", "$x\n    set", []int{2, 5}},
			{h + "/with body binds", "proc f {d} {\n  set x 0\n  " + h + " with d {set x 1}\n  puts $x\n}", "$x\n}", []int{3}},
			{h + "/update body binds", "proc f {d} {\n  set x 0\n  " + h + " update d a y {set x 1}\n  puts $x\n}", "$x\n}", []int{3}},
		})
	}
	for _, h := range controlHeads("return") {
		runReachCases(t, []reachCase{
			{h + "/ends the path", "proc f {c} {\n  set x 0\n  if {$c} {set x 1; " + h + "}\n  puts $x\n}", "$x\n}", []int{2}},
		})
	}
	for _, h := range controlHeads("break") {
		runReachCases(t, []reachCase{
			{h + "/ends the pass", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    " + h + "\n    set x 1\n  }\n  puts $x\n}", "$x\n}", []int{2}},
		})
	}
	for _, h := range controlHeads("continue") {
		runReachCases(t, []reachCase{
			{h + "/ends the pass", "proc f {} {\n  set x 0\n  while {[incr n] < 3} {\n    " + h + "\n    set x 1\n  }\n  puts $x\n}", "$x\n}", []int{2}},
		})
	}
}
