package tcl

import (
	"strings"
	"testing"
)

// exprVarOffsets returns the start offsets of the variable references named name
// that CommandRefs finds anywhere in src's top-level commands, including inside
// [command substitutions].
func exprVarOffsets(src, name string) []int {
	var out []int
	for _, c := range Parse(src) {
		for _, r := range CommandRefs(c) {
			if r.Kind == RefVariable && r.Name == name {
				out = append(out, r.Start)
			}
		}
	}
	return out
}

// expr substitutes the $vars of a braced expression itself, so each is a read of
// that variable. A {braced} operand is a literal; a "quoted" one is substituted.
// Checked against tclsh 8.6 and 9.0.
func TestCommandRefsFindVarsInBracedExpressions(t *testing.T) {
	cases := []struct{ name, src, mark string }{
		{"expr argument", "set y [expr {$x + 1}]", "$x"},
		{"if condition", "if {$x <= 1} {}", "$x"},
		{"elseif condition", "if {0} {} elseif {$x} {}", "$x"},
		{"while test", "while {$x < 3} {}", "$x"},
		{"for test", "for {set i 0} {$x < 3} {incr i} {}", "$x"},
		{"quoted operand", "expr {\"$x\" eq \"7\"}", "$x"},
		{"function argument", "expr {max($x,1)}", "$x"},
		{"qualified name", "expr {$::x + 1}", "$::x"},
		{"braced name", "expr {${x} + 1}", "${x}"},
		{"after an unconditional bracket", "expr {[set x 1] && $x}", "$x}"},
		{"after && with no bracket", "expr {$c && $x}", "$x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := strings.LastIndex(c.src, c.mark)
			name := strings.Trim(strings.TrimPrefix(c.mark, "$"), "{}")
			got := exprVarOffsets(c.src, name)
			found := false
			for _, off := range got {
				if off == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("refs to %q at %v, want one at %d in %q", name, got, want, c.src)
			}
		})
	}
}

// Uses that are not references: a braced literal operand, and a use after a
// bracket that runs only on some paths. Which bindings reach such a use depends on
// which operands ran, so the use stays silent rather than risk a binding that
// cannot reach it (`$c ? [set x 1] : $x` reads the value from before the bracket).
func TestCommandRefsSilentInBracedExpressions(t *testing.T) {
	cases := []struct{ name, src string }{
		{"braced literal operand", "expr {{$x} eq \"y\"}"},
		{"ternary false arm after a bracket", "expr {$c ? [set x 1] : $x}"},
		{"after a bracket that follows ||", "expr {$c || [set x 1] && $x}"},
		{"after a conditional bracket in an if condition", "if {$c && [set x 1] && $x} {}"},
		{"carried across expr words", "expr {$c} && {[set x 1]} || {$x}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := exprVarOffsets(c.src, "x"); len(got) != 0 {
				t.Fatalf("refs to x at %v, want none in %q", got, c.src)
			}
		})
	}
}

// Reaching answers a use in an expression with what reaches that point: a loop
// test sees the body's bindings through the back edge, and an if condition sees
// the bindings above the if. Checked against tclsh 8.6 and 9.0.
func TestReachingUsesInBracedExpressions(t *testing.T) {
	runReachCases(t, []reachCase{
		{"if condition", "proc f {n} {\n  set n 2\n  if {$n <= 1} {}\n}", "$n", []int{2}},
		{"expr argument", "proc f {} {\n  set x 1\n  return [expr {$x * 2}]\n}", "$x", []int{2}},
		{"while test sees the body", "proc f {} {\n  set i 0\n  while {$i < 3} {\n    set i [expr {$i + 1}]\n  }\n}", "$i <", []int{2, 4}},
		{"for test sees start and next", "proc f {} {\n  for {set i 0} {$i < 3} {\n    set i 5\n  } {}\n}", "$i <", []int{2, 3}},
		{"after an unconditional bracket", "proc f {} {\n  set x 0\n  expr {[set x 1] && $x}\n}", "$x}", []int{3}},
		{"elseif sees earlier condition brackets", "proc f {} {\n  set x 0\n  if {[set x 1]} {\n  } elseif {$x} {\n  }\n}", "$x}", []int{3}},
	})
}

// A multi-word expr substitutes its unbraced words before it evaluates the joined
// expression, so a braced $x sees an unbraced [script] written after it, and an
// unbraced $x does not see a braced one. An operator that arrives through a
// substitution may short-circuit. Checked against tclsh 8.6 and 9.0.
func TestReachingMultiWordExprOrder(t *testing.T) {
	runReachCases(t, []reachCase{
		{"braced use after a later unbraced bracket", "proc f {} {\n  set x 0\n  set y [expr {$x} + [\n    set x 1]]\n}", "$x}", []int{4}},
		{"braced use after a later unbraced incr", "proc f {} {\n  set x 0\n  return [expr {$x} + [\n    incr x]]\n}", "$x}", []int{4}},
		{"unbraced use before a braced conditional bracket", "proc f {c} {\n  set x 0\n  return [expr {$c && [\n    set x 1]} + $x]\n}", "$x]", []int{2}},
	})
}

func TestCommandRefsSilentAfterSubstitutedOperator(t *testing.T) {
	src := "expr {$c} $op {[set x 1]} : {$x}"
	if got := exprVarOffsets(src, "x"); len(got) != 0 {
		t.Fatalf("refs to x at %v, want none in %q", got, src)
	}
}
