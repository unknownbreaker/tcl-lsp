package tcl

import "testing"

// A [command substitution] is a script that runs in this frame, left to right with
// the command's other substitutions, before the command itself. Expected answers
// were checked against tclsh 8.6 and 9.0.
func TestReachingCommandSubstitutionScripts(t *testing.T) {
	runReachCases(t, []reachCase{
		{"a use after a binding in the same bracketed catch", "proc f {} {\n  set y 0\n  if {[catch {\n    set y 1\n    puts $y\n  } err]} {}\n}", "$y", []int{4}},
		{"a bracket's binding reaches the code after the command", "proc f {} {\n  set y 0\n  set rc [catch {\n    set y 1\n  }]\n  puts $y\n}", "$y", []int{2, 4}},
		{"a use before the bracket in the same word", "proc f {} {\n  set y 0\n  puts \"$y [set y 2]\"\n}", "$y [", []int{2}},
		{"a use after the bracket in the same word", "proc f {} {\n  set y 0\n  puts \"[set y 2] $y\"\n}", "$y", []int{3}},
		{"an if body sees its condition's bracket", "proc f {} {\n  set y 0\n  if {[set y 1]} {\n    puts $y\n  }\n}", "$y", []int{3}},
		{"else sees every condition", "proc f {} {\n  set y 0\n  if {[set y 1]} {\n  } elseif {[set y 2]} {\n  } else {\n    puts $y\n  }\n}", "$y", []int{4}},
		{"fall-through sees every condition", "proc f {} {\n  set y 0\n  if {[set y 1]} {\n  } elseif {[set y 2]} {\n  }\n  puts $y\n}", "$y", []int{3, 4}},
		{"a first branch does not see a later condition", "proc f {} {\n  set y 0\n  if {$c} {\n    puts $y\n  } elseif {[set y 2]} {\n  }\n}", "$y", []int{2}},
		{"an unbraced condition runs before the if; catch may abort", "proc f {} {\n  set y 0\n  if [catch {set y 1}] {\n    puts $y\n  }\n}", "$y", []int{2, 3}},
		{"lmap in a bracket", "proc f {} {\n  set y 0\n  set r [lmap i {1 2} {\n    set y $i\n  }]\n  puts $y\n}", "$y", []int{2, 4}},
		{"a nested bracket", "proc f {} {\n  set y 0\n  set r [list [set y 1]]\n  puts $y\n}", "$y", []int{3}},
		{"a loop test's bracket reaches the body", "proc f {} {\n  set y 0\n  while {[set y 1]} {\n    puts $y\n  }\n}", "$y", []int{3}},
		{"a loop test's bracket reaches the code after the loop", "proc f {} {\n  set y 0\n  while {[set y 1]} {\n    set y 2\n  }\n  puts $y\n}", "$y", []int{3}},
		{"a catch body's bracket binding reaches after the catch", "proc f {} {\n  set y 0\n  catch {\n    puts [set y 1]\n  }\n  puts $y\n}", "$y", []int{2, 4}},
		{"an unknown body in a bracket sees the bracket's earlier binding", "proc f {} {\n  set y 0\n  set r [with {\n    puts $y\n  }]\n}", "$y", []int{2}},
		{"info exists in a bracket sees the bracket's earlier binding", "proc f {} {\n  set y 0\n  if {[set y 1; info exists y]} {}\n}", " y]", []int{3}},
		{"namespace level", "namespace eval ::n {\n  set y 0\n  set rc [catch {\n    set y 1\n    puts $y\n  }]\n}", "$y", []int{4}},
	})
}

// An expression evaluates a bracket after &&, || or ? only on some paths, so its
// bindings may reach without killing what came before. A bracket before any of them
// always runs. Checked against tclsh 8.6 and 9.0.
func TestReachingShortCircuitExpressionBrackets(t *testing.T) {
	runReachCases(t, []reachCase{
		{"after &&", "proc f {c} {\n  set x 0\n  if {$c && [set x 1]} {}\n  puts $x\n}", "$x", []int{2, 3}},
		{"after ||", "proc f {c} {\n  set x 0\n  if {$c || [set x 1]} {}\n  puts $x\n}", "$x", []int{2, 3}},
		{"after ?", "proc f {c} {\n  set x 0\n  set r [expr {$c ? [set x 1] : 5}]\n  puts $x\n}", "$x", []int{2, 3}},
		{"in a loop test", "proc f {c} {\n  set x 0\n  while {$c && [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"before && always runs", "proc f {c} {\n  set x 0\n  if {[set x 1] && $c} {}\n  puts $x\n}", "$x", []int{3}},
	})
}

// Review findings, each checked against tclsh 8.6 and 9.0.
func TestReachingExpressionLiteralsAndDeadBrackets(t *testing.T) {
	runReachCases(t, []reachCase{
		{"a braced literal in an expression is not a script", "proc f {s} {\n  set y 0\n  if {$s eq {[set y 1]}} {}\n  puts $y\n}", "$y", []int{2}},
		{"a ? in a quoted string is not an operator", "proc f {} {\n  set x 0\n  if {\"?\" eq [set x 1]} {}\n  puts $x\n}", "$x", []int{3}},
		{"&& between expr words", "proc f {c} {\n  set x 0\n  set r [expr {$c} && {[set x 1]}]\n  puts $x\n}", "$x", []int{2, 3}},
		{"a bracket that returns ends the path", "proc f {c} {\n  set y 0\n  if {$c} {\n    set z [return [set y 1]]\n  }\n  puts $y\n}", "$y", []int{2}},
	})
}
