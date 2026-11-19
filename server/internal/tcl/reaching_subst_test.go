package tcl

import (
	"strings"
	"testing"
)

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
		{"a loop body runs only when the test is true", "proc f {c} {\n  set x 0\n  while {$c && [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"after a loop whose test may skip the bracket", "proc f {c} {\n  set x 0\n  while {$c && [set x 1]} {\n    set c 0\n  }\n  puts $x\n}", "$x", []int{2, 3}},
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

// Expression text the parser does not model falls back to the flat scan, which is
// silent on these uses. Tcl decides what is right: 9.0 reads `#` as a comment to
// end of line (8.6 rejects it), and a `]` inside braces or quotes in a script does
// not end the script.
func TestReachingExpressionUnmodeledIsSilent(t *testing.T) {
	for _, c := range []struct{ name, src, mark string }{
		{"a Tcl 9 comment", "proc f {c} {\n  set x 0\n  if {$c # && [set x 1]\n    && $x} {}\n}", "$x}"},
		{"a bracket inside braces in a script", "proc f {c s} {\n  set x 0\n  if {$c && [string map {] [set x 1]} $s] ne {} && $x} {}\n}", "$x}"},
		{"a bracket inside quotes in a script", "proc f {c} {\n  set x 0\n  if {$c || [string length \"]\"; set x 1] && $x ne {\"}} {}\n}", "$x ne"},
	} {
		t.Run(c.name, func(t *testing.T) {
			off := strings.Index(c.src, c.mark)
			if off < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			if defs, _ := ReachingAt(c.src, off); len(defs) > 0 {
				t.Errorf("ReachingAt(%s) = %+v, want silent\nsource:\n%s", c.mark, defs, c.src)
			}
		})
	}
}

// The flat scan cannot tell where a bracket script ends when a `]` may sit in its
// quotes or braces, so it stops there, and the uses that script's bindings may
// reach get no answer: those after it, and all of them when it is in a loop. A
// use before it keeps its answer, and so does a script whose quotes and brackets
// balance. Tcl's answers are in research/experiments/08_quoted_bracket_flat.tcl.
func TestReachingFlatScanDistrustsHiddenBracket(t *testing.T) {
	for _, c := range []struct{ name, src, mark string }{
		{"a later operand", "proc f {} {\n  set x 0\n  if {[string length \"]\"; set x 1] && $x ne {\"}} {}\n}", "$x ne"},
		{"the next command", "proc f {} {\n  set x 0\n  if {[string length \"]\"; set x 1]} {}\n  puts $x\n}", "$x\n}"},
		{"a braced bracket", "proc f {s} {\n  set x 0\n  if {[string map {] [set x 1]} $s] ne {} && $x == 0} {}\n}", "$x =="},
		{"an earlier use in a loop", "proc f {c} {\n  set x 0\n  while {$c} {\n    puts $x\n    if {[string length \"]\"; set x 1]} {}\n  }\n}", "$x\n"},
		{"a use in the script", "proc f {} {\n  set x 0\n  if {[string length \"]\"; puts $x]} {}\n}", "$x]"},
		{"a quote inside a word is literal", "proc f {} {\n  set x 0\n  if {[list a\"b \"]\"; set x 1] && $x ne {\"}} {}\n}", "$x ne"},
		{"a backslash-newline starts a word", "proc f {} {\n  set x 0\n  if {[list a\\\n\"]\"; set x 1] ne {}} {}\n  puts $x\n}", "$x\n}"},
		{"an array index", "proc f {} {\n  set x 0; array set a {] 5}\n  if {[list $a(]); set x 1] ne {} && $x} {}\n}", "$x}"},
		{"the next command after an array index", "proc f {} {\n  set x 0; array set a {] 5}\n  if {[list $a(]); set x 1] ne {}} {}\n  puts $x\n}", "$x\n}"},
		{"an empty-named array index", "proc f {} {\n  set x 0; set (]) 5\n  if {[list $(]); set x 1] ne {}} {}\n  puts $x\n}", "$x\n}"},
		{"a qualified array index", "proc f {} {\n  set x 0; array set ::a {] 5}\n  if {[list $::a(]); set x 1] ne {}} {}\n  puts $x\n}", "$x\n}"},
		{"an unclosed array index", "proc f {} {\n  set x 0\n  if {[list $a(] ne {}} {}\n  puts $x\n}", "$x\n}"},
		{"a comment in the script", "proc f {} {\n  set x 0\n  if {[list a;# x]\n       set x 1] ne {} && $x} {}\n}", "$x}"},
	} {
		t.Run(c.name, func(t *testing.T) {
			off := strings.Index(c.src, c.mark)
			if off < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			if defs, _ := ReachingAt(c.src, off); len(defs) > 0 {
				t.Errorf("ReachingAt(%q) = %+v, want silent\nsource:\n%s", c.mark, defs, c.src)
			}
		})
	}
	runReachCases(t, []reachCase{
		{"a use before it", "proc f {} {\n  set x 0\n  puts $x\n  if {[string length \"]\"; set x 1]} {}\n}", "$x", []int{2}},
		{"a use before its loop", "proc f {c} {\n  set x 0\n  puts $x\n  while {$c} {\n    if {[string length \"]\"; set x 1]} {}\n  }\n}", "$x", []int{2}},
		{"a use between loops", "proc f {c} {\n  set x 0\n  foreach i {1 2} {}\n  puts $x\n  foreach i {1 2} {if {[string length \"]\"; set y 1]} {}}\n}", "$x", []int{2}},
		{"a bracket nested in an array index", "proc f {} {\n  set x 0; array set a {k 5}\n  if {[list $a([string trim k])] ne {} && $x == 0} {}\n}", "$x ==", []int{2}},
		{"a balanced script in a multi-word expr", "proc f {} {\n  set x 0\n  expr {[string length \"[set x 1]\"]} + {$x}\n}", "$x}", []int{3}},
	})
}

// expr runs an operand after && only when the earlier operands of its chain ran
// true, one after || only when they ran false, and a ternary arm only after its
// condition; a paren closes what is conditional. So a use sees exactly the
// operands that ran before it, and an if or loop body sees the condition's true
// paths. Each answer was checked against tclsh 8.6 and 9.0
// (research/experiments/08_and_chain_reaching.tcl).
func TestReachingExpressionStructure(t *testing.T) {
	runReachCases(t, []reachCase{
		{"an if body after an && chain", "proc f {c} {\n  set x 0\n  if {$c && [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"else after an && chain", "proc f {c} {\n  set x 0\n  if {$c && [set x 1]} {\n  } else {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"an if body after ||", "proc f {c} {\n  set x 0\n  if {$c || [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"an elseif body after an && chain", "proc f {a c} {\n  set x 0\n  if {$a} {\n  } elseif {$c && [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{4}},
		{"a later && operand", "proc f {c} {\n  set x 0\n  if {$c && [set x 1] && $x} {}\n}", "$x}", []int{3}},
		{"a later || operand", "proc f {c} {\n  set x 0\n  set r [expr {$c || [set x 1] || $x}]\n}", "$x}", []int{3}},
		{"a bracket in && under ||", "proc f {c} {\n  set x 0\n  set r [expr {$c || [set x 1] && [string length $x]}]\n}", "$x]", []int{3}},
		{"a ternary's false arm", "proc f {c} {\n  set x 0\n  set r [expr {$c ? [set x 1] : $x}]\n}", "$x}", []int{2}},
		{"a ternary's true arm sees its condition", "proc f {} {\n  set x 0\n  set r [expr {[set x 1] ? $x : 0}]\n}", "$x :", []int{3}},
		{"after a ternary", "proc f {c} {\n  set x 0\n  set r [expr {$c ? [set x 1] : 0}]\n  puts $x\n}", "$x", []int{2, 3}},
		{"a paren closes a ternary", "proc f {c} {\n  set x 0\n  set r [expr {($c ? 1 : 2) + [set x 1]}]\n  puts $x\n}", "$x", []int{3}},
		{"a paren closes an && chain", "proc f {c} {\n  set x 0\n  set r [expr {($c && 1) + [set x 1]}]\n  puts $x\n}", "$x", []int{3}},
		{"an operator in an array index", "proc f {c} {\n  set x 0\n  set r [expr {$a(?) + [set x 1]}]\n  puts $x\n}", "$x", []int{3}},
		{"a quote in an array index", "proc f {c} {\n  set x 0\n  set r [expr {$a(\") + [set x 1]}]\n  puts $x\n}", "$x", []int{3}},
		{"a bracket in an array index", "proc f {} {\n  set x 0\n  set r [expr {$a([set x 1]) + $x}]\n}", "$x}", []int{3}},
		{"negation is not the chain's truth", "proc f {c} {\n  set x 0\n  if {!($c && [set x 1])} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"a bare paren is the chain's truth", "proc f {c} {\n  set x 0\n  if {($c && [set x 1])} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"function arguments run in order", "proc f {c} {\n  set x 0\n  set r [expr {max($c && [set x 1], $x)}]\n}", "$x)", []int{2, 3}},
		{"a qualified function name is not a ternary", "proc f {c} {\n  set x 0\n  if {$c && [set x 1] && ::tcl::mathfunc::abs($x)} {}\n}", "$x)", []int{3}},
		{"nested three deep", "proc f {a b c} {\n  set x 0\n  if {$a || ($b && ($c ? [set x 1] && [set y $x] : 0))} {}\n}", "$x]", []int{3}},
		{"a for body after an && chain", "proc f {c} {\n  set x 0\n  for {} {$c && [set x 1]} {} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"a bracket that returns ends only its path", "proc f {c} {\n  set x 0\n  if {$c && [return]} {\n    set x 1\n  }\n  puts $x\n}", "$x", []int{2}},
		{"an else body without the else keyword", "proc f {c} {\n  set x 0\n  if {$c && [set x 1]} {} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"an elseif body after an unbraced condition", "proc f {c d} {\n  set x 0\n  if {$c && [set x 1]} {} elseif $d {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"an else body without the keyword after then", "proc f {c d} {\n  set x 0\n  if {$d} then {} elseif {$c && [set x 1]} then {} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
		{"an unbalanced paren falls back to the flat scan", "proc f {c} {\n  set x 0\n  if {($c && [set x 1]} {\n    puts $x\n  }\n}", "$x", []int{2, 3}},
	})
}
