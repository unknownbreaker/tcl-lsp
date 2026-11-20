package tcl

import "testing"

// expr substitutes its unbraced words as command words, then substitutes the
// result again when it evaluates it. So a backslash that escapes a bracket, or
// spells one (`\x5b`), forms a script on the second pass, and that script may
// bind. Each source below leaves x=1 in tclsh 8.6 and 9.0, so reaching at the
// later $x stays silent.
func TestReachingExprEscapedBracketSilent(t *testing.T) {
	runSilentReachCases(t, []silentReachCase{
		{"escaped brackets in one word", "proc f {} {\n  set x 0\n  expr \"\\[set x 1\\]\"\n  puts $x\n}", "$x\n}"},
		{"escaped brackets over several words", "proc f {} {\n  set x 0\n  expr \"\\[set\" x 1\\]\n  puts $x\n}", "$x\n}"},
		{"hex-escaped brackets", "proc f {} {\n  set x 0\n  expr \"\\x5bset x 1\\x5d\"\n  puts $x\n}", "$x\n}"},
		{"bare escaped brackets", "proc f {} {\n  set x 0\n  expr 1 + \\[set x 1\\]\n  puts $x\n}", "$x\n}"},
		{"escaped expr in a bracket script", "proc f {} {\n  set x 0\n  set r [expr \"\\[set x 1\\]\"]\n  puts $x\n}", "$x\n}"},
		{"qualified expr", "proc f {} {\n  set x 0\n  ::expr \"\\[set x 1\\]\"\n  puts $x\n}", "$x\n}"},
		{"expanded command name", "proc f {} {\n  set x 0\n  {*}expr \"\\[set x 1\\]\"\n  puts $x\n}", "$x\n}"},
		{"expanded braced hex escapes", "proc f {} {\n  set x 0\n  expr {*}{\\x5bset x 1\\x5d}\n  puts $x\n}", "$x\n}"},
		{"use in a later loop pass", "proc f {} {\n  set x 0\n  while 1 {\n    puts $x\n    expr \"\\[set x 1\\]\"\n  }\n}", "$x\n    expr"},
	})
}

// A substituted value may hold the brackets: nothing in the source says it holds
// none, so reaching stays silent after an unbraced $var or [script] in an expr
// word too (#107). Each source leaves x=1 in tclsh 8.6 and 9.0.
func TestReachingExprSubstitutedValueSilent(t *testing.T) {
	runSilentReachCases(t, []silentReachCase{
		{"value holding a script", "proc f {} {\n  set x 0\n  set s {[set x 1]}\n  expr $s\n  puts $x\n}", "$x\n}"},
		{"values around a braced word", "proc f {} {\n  set x 0\n  set b {[}\n  set c {]}\n  expr $b {set x 1} $c\n  puts $x\n}", "$x\n}"},
		{"command result", "proc f {} {\n  set x 0\n  expr [g]\n  puts $x\n}", "$x\n}"},
		{"numeric-looking operands", "proc f {a b} {\n  set x 0\n  set r [expr $a + $b]\n  puts $x\n}", "$x\n}"},
		{"value in a quoted word", "proc f {s} {\n  set x 0\n  expr \"$s\"\n  puts $x\n}", "$x\n}"},
	})
}

// The uses in an unbraced expr word are read on the first pass, before any script
// it forms runs, so they keep their answers. A backslash outside expr is
// substituted once and forms no script, and a braced expr is not substituted
// before expr reads it.
func TestReachingExprResubstKept(t *testing.T) {
	runReachCases(t, []reachCase{
		{"use in the word", "proc f {} {\n  set x 0\n  expr $x + 1\n}", "$x +", []int{2}},
		{"use beside escaped brackets", "proc f {} {\n  set x 0\n  expr \"\\[set x 1\\] + $x\"\n}", "$x\"", []int{2}},
		{"use in a later word", "proc f {} {\n  set x 0\n  expr \"\\[set\" x 1\\] + $x\n}", "$x\n}", []int{2}},
		{"escaped brackets in puts", "proc f {} {\n  set x 0\n  puts \"\\[set x 1\\]\"\n  puts $x\n}", "$x\n}", []int{2}},
		{"literal unbraced expr", "proc f {} {\n  set x 0\n  expr 1 + 2\n  puts $x\n}", "$x\n}", []int{2}},
		{"braced expr", "proc f {a} {\n  set x 0\n  expr {$a + 1}\n  puts $x\n}", "$x\n}", []int{2}},
	})
}
